package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
)

// FoundVersion is a version a connector reported at one location.
type FoundVersion struct {
	Name        string
	PublishedAt time.Time
	Stable      bool
}

// Found is the complete version list of one component at one location.
type Found struct {
	Component, Repository string
	Owner                 string // owner key
	Visibility            string
	Versions              []FoundVersion
}

// Owner is a namespace that components belong to (org, user, enterprise).
type Owner struct{ Key, Title, AvatarURL, Parent string }

// Source is how a connector is shown: its readable key, title and avatar.
type Source struct {
	Key, Title, AvatarURL string  // AvatarURL may be empty
	Scope                 string  // "" = none/unknown (not written)
	Owners                []Owner // nil = unknown (stored rows kept)
}

// ConnectorState is a connector's bookkeeping row.
type ConnectorState struct {
	ID, Type                        string
	Source                          Source
	Position                        int
	LastDeltaSyncAt, LastFullSyncAt time.Time // zero: never
	LastError                       string
}

// ChildRef is one reference of a component's default version (OCM:
// references[].name, .componentName, .version).
type ChildRef struct {
	ReferenceName, ChildName, ChildVersion string
}

// Overview is the display data stored per component.
type Overview struct {
	Title, Description, Icon, Provider string
	Categories                         []string
	Architectures                      []string // platforms, sorted
	Signed                             bool
	Children                           []ChildRef
}

// OverviewTarget is a component whose overview must be (re)fetched from
// Version at Repository.
type OverviewTarget struct {
	Component, Version, Repository string
}

// Writer writes the index. It is used by one process (cmd/sync) at a time.
type Writer struct {
	db *sql.DB
}

// OpenWriter opens (or creates) the file at path read-write. A new or empty
// file gets the schema; a file with another schema version is rejected with
// ErrSchemaMismatch.
func OpenWriter(path string) (*Writer, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fresh, err := checkSchema(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	if fresh {
		for _, stmt := range ddl {
			if _, err := db.Exec(stmt); err != nil {
				db.Close()
				return nil, fmt.Errorf("creating schema: %w", err)
			}
		}
	}
	return &Writer{db: db}, nil
}

func nullable[T comparable](v T) any {
	var zero T
	if v == zero {
		return nil
	}
	return v
}

// Close checkpoints (no WAL file is left behind) and closes the database.
func (w *Writer) Close() error {
	_, cerr := w.db.Exec(`CHECKPOINT`)
	return errors.Join(cerr, w.db.Close())
}

// ConnectorState returns the connector's row; ok is false if it never ran.
func (w *Writer) ConnectorState(ctx context.Context, id string) (ConnectorState, bool, error) {
	var s ConnectorState
	var delta, full sql.NullTime
	var lastErr sql.NullString
	err := w.db.QueryRowContext(ctx, `SELECT id, type, source_key, title, coalesce(avatar_url, ''), coalesce(scope, ''), position, last_delta_sync_at, last_full_sync_at, last_error
		FROM connectors WHERE id = ?`, id).Scan(&s.ID, &s.Type, &s.Source.Key, &s.Source.Title, &s.Source.AvatarURL, &s.Source.Scope, &s.Position, &delta, &full, &lastErr)
	if errors.Is(err, sql.ErrNoRows) {
		return ConnectorState{}, false, nil
	}
	if err != nil {
		return ConnectorState{}, false, err
	}
	s.LastDeltaSyncAt, s.LastFullSyncAt, s.LastError = delta.Time, full.Time, lastErr.String
	return s, true, nil
}

// ConnectorTx is one connector's run inside a transaction.
type ConnectorTx struct {
	tx       *sql.Tx
	ctx      context.Context
	id       string
	runStart time.Time
	cacheErr error // first page-cache error, reported by Commit
}

// BeginConnector starts a connector run; src is stored as the connector's
// source (replacing the previous one), runStart marks what it sees.
//
// The Writer has a single database connection, which the returned tx holds:
// the tx must be Committed or Rolled back before any other Writer method is
// called, or that call deadlocks.
func (w *Writer) BeginConnector(ctx context.Context, id, typ string, src Source, position int, runStart time.Time) (*ConnectorTx, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO connectors (id, type, source_key, title, avatar_url, scope, position) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET type = excluded.type, source_key = excluded.source_key, title = excluded.title,
			avatar_url = excluded.avatar_url, scope = coalesce(excluded.scope, connectors.scope), position = excluded.position`,
		id, typ, src.Key, src.Title, nullable(src.AvatarURL), nullable(src.Scope), position); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := writeOwners(ctx, tx, src); err != nil {
		tx.Rollback()
		return nil, err
	}
	return &ConnectorTx{tx: tx, ctx: ctx, id: id, runStart: runStart}, nil
}

// writeOwners upserts the source's owners. Only an enterprise source (owners
// with a Parent) sets parent_key, and it clears it on owners that left.
func writeOwners(ctx context.Context, tx *sql.Tx, src Source) error {
	if src.Owners == nil {
		return nil
	}
	for _, o := range src.Owners {
		if o.Key == "" {
			return errors.New("owner without key")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO owners (key, title, avatar_url, parent_key) VALUES (?, ?, ?, ?)
			ON CONFLICT (key) DO UPDATE SET title = excluded.title, avatar_url = coalesce(excluded.avatar_url, owners.avatar_url),
				parent_key = coalesce(excluded.parent_key, owners.parent_key)`,
			o.Key, o.Title, nullable(o.AvatarURL), nullable(o.Parent)); err != nil {
			return fmt.Errorf("owner %s: %w", o.Key, err)
		}
	}
	if len(src.Owners) == 0 {
		if src.Scope == "" {
			return nil
		}
		// an enterprise without organizations
		_, err := tx.ExecContext(ctx, `UPDATE owners SET parent_key = NULL WHERE parent_key = ?`, src.Key)
		return err
	}
	if !slices.ContainsFunc(src.Owners, func(o Owner) bool { return o.Parent == src.Key }) {
		return nil // not an enterprise source
	}
	keys := make([]any, 0, len(src.Owners)+1)
	keys = append(keys, src.Key)
	for _, o := range src.Owners {
		keys = append(keys, o.Key)
	}
	_, err := tx.ExecContext(ctx, `UPDATE owners SET parent_key = NULL WHERE parent_key = ? AND key NOT IN (`+placeholders(len(src.Owners))+`)`, keys...)
	return err
}

// Apply records that the connector sees f and replaces f's version set at
// its location.
func (c *ConnectorTx) Apply(ctx context.Context, f Found) error {
	if f.Owner == "" || f.Visibility == "" {
		return fmt.Errorf("%s at %s: owner and visibility are required", f.Component, f.Repository)
	}
	if _, err := c.tx.ExecContext(ctx, `INSERT INTO locations (repository) VALUES (?) ON CONFLICT (repository) DO NOTHING`, f.Repository); err != nil {
		return fmt.Errorf("location %s: %w", f.Repository, err)
	}
	var loc int64
	if err := c.tx.QueryRowContext(ctx, `SELECT id FROM locations WHERE repository = ?`, f.Repository).Scan(&loc); err != nil {
		return err
	}
	if _, err := c.tx.ExecContext(ctx, `INSERT INTO owners (key, title) VALUES (?, ?) ON CONFLICT (key) DO NOTHING`,
		f.Owner, f.Owner[strings.LastIndex(f.Owner, ":")+1:]); err != nil {
		return fmt.Errorf("owner %s: %w", f.Owner, err)
	}
	stmts := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM discoveries WHERE connector_id = ? AND location_id = ? AND component = ?`, []any{c.id, loc, f.Component}},
		{`INSERT INTO discoveries (connector_id, location_id, component, owner_key, visibility, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
			[]any{c.id, loc, f.Component, f.Owner, f.Visibility, c.runStart}},
		{`DELETE FROM component_versions WHERE component = ? AND location_id = ?`, []any{f.Component, loc}},
	}
	for _, s := range stmts {
		if _, err := c.tx.ExecContext(ctx, s.q, s.args...); err != nil {
			return fmt.Errorf("%s at %s: %w", f.Component, f.Repository, err)
		}
	}
	if len(f.Versions) == 0 {
		return nil
	}
	ins, err := c.tx.PrepareContext(ctx, `INSERT INTO component_versions VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ins.Close()
	seen := map[string]bool{}
	for _, v := range f.Versions {
		if seen[v.Name] {
			continue // a connector may list one version twice (two tags of one image)
		}
		seen[v.Name] = true
		if _, err := ins.ExecContext(ctx, f.Component, loc, v.Name, v.PublishedAt, v.Stable); err != nil {
			return fmt.Errorf("%s:%s at %s: %w", f.Component, v.Name, f.Repository, err)
		}
	}
	return nil
}

// DropUnseen (full runs only) forgets everything this connector reported
// before but not in this run.
func (c *ConnectorTx) DropUnseen(ctx context.Context) error {
	_, err := c.tx.ExecContext(ctx, `DELETE FROM discoveries WHERE connector_id = ? AND last_seen_at < ?`, c.id, c.runStart)
	return err
}

// Lookup implements connector.PageCache.
func (c *ConnectorTx) Lookup(u string) (string, string, bool) {
	var etag string
	var next sql.NullString
	err := c.tx.QueryRowContext(c.ctx, `SELECT etag, next_url FROM http_cache WHERE connector_id = ? AND url = ?`, c.id, u).Scan(&etag, &next)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && c.cacheErr == nil {
			c.cacheErr = err
		}
		return "", "", false
	}
	return etag, next.String, true
}

// Store implements connector.PageCache.
func (c *ConnectorTx) Store(u, etag, next string) {
	for _, q := range []struct {
		s    string
		args []any
	}{
		{`DELETE FROM http_cache WHERE connector_id = ? AND url = ?`, []any{c.id, u}},
		{`INSERT INTO http_cache VALUES (?, ?, ?, ?)`, []any{c.id, u, etag, nullable(next)}},
	} {
		if _, err := c.tx.ExecContext(c.ctx, q.s, q.args...); err != nil && c.cacheErr == nil {
			c.cacheErr = err
		}
	}
}

// ResetPageCache forgets the connector's cached pages (full runs).
func (c *ConnectorTx) ResetPageCache(ctx context.Context) error {
	_, err := c.tx.ExecContext(ctx, `DELETE FROM http_cache WHERE connector_id = ?`, c.id)
	return err
}

// Commit records the run time and commits. A page-cache error of the run is
// returned after rolling back.
func (c *ConnectorTx) Commit(ctx context.Context, full bool) error {
	if c.cacheErr != nil {
		c.tx.Rollback()
		return fmt.Errorf("page cache: %w", c.cacheErr)
	}
	q := `UPDATE connectors SET last_delta_sync_at = ?, last_error = NULL WHERE id = ?`
	if full {
		q = `UPDATE connectors SET last_delta_sync_at = ?, last_full_sync_at = ?, last_error = NULL WHERE id = ?`
	}
	args := []any{c.runStart, c.id}
	if full {
		args = []any{c.runStart, c.runStart, c.id}
	}
	if _, err := c.tx.ExecContext(ctx, q, args...); err != nil {
		c.tx.Rollback()
		return err
	}
	return c.tx.Commit()
}

// Rollback discards the run.
func (c *ConnectorTx) Rollback() error { return c.tx.Rollback() }

// RecordError stores a failed run's error, keeping the previous data. src is
// only used if the connector has no row yet (its first run failed).
func (w *Writer) RecordError(ctx context.Context, id, typ string, src Source, position int, runErr error) error {
	_, err := w.db.ExecContext(ctx, `INSERT INTO connectors (id, type, source_key, title, avatar_url, position, last_error) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET last_error = excluded.last_error`,
		id, typ, src.Key, src.Title, nullable(src.AvatarURL), position, runErr.Error())
	return err
}

// PruneConnectors deletes the discoveries and rows of every connector not in
// keep (removed or edited hub.yaml entries); an empty keep prunes all.
func (w *Writer) PruneConnectors(ctx context.Context, keep []string) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	where := ""
	args := make([]any, len(keep))
	if len(keep) > 0 {
		where = " WHERE %s NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(keep)), ",") + ")"
		for i, k := range keep {
			args[i] = k
		}
	}
	for _, q := range []string{
		"DELETE FROM discoveries" + strings.Replace(where, "%s", "connector_id", 1),
		"DELETE FROM http_cache" + strings.Replace(where, "%s", "connector_id", 1),
		"DELETE FROM connectors" + strings.Replace(where, "%s", "id", 1),
	} {
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("prune connectors: %w", err)
		}
	}
	return tx.Commit()
}

// Cleanup removes what no connector points to any more, adds new components
// and recomputes latest and default versions (newest stable, else newest
// pre-release). A changed default version clears overview_fetched_at so
// PendingOverviews returns it.
func (w *Writer) Cleanup(ctx context.Context) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM component_versions v WHERE NOT EXISTS (
			SELECT 1 FROM discoveries d WHERE d.location_id = v.location_id AND d.component = v.component)`,
		`DELETE FROM locations l WHERE NOT EXISTS (SELECT 1 FROM discoveries d WHERE d.location_id = l.id)`,
		`DELETE FROM components c WHERE NOT EXISTS (SELECT 1 FROM component_versions v WHERE v.component = c.name)`,
		`INSERT INTO components (name, title)
			SELECT DISTINCT component, component FROM component_versions v
			WHERE NOT EXISTS (SELECT 1 FROM components c WHERE c.name = v.component)`,
		`UPDATE components SET
			latest_version = n.version,
			latest_published_at = n.published_at,
			default_version = coalesce(n.version, a.version),
			overview_fetched_at = CASE WHEN components.default_version IS NOT DISTINCT FROM coalesce(n.version, a.version)
				THEN components.overview_fetched_at ELSE NULL END
		FROM (
			SELECT c.name, s.version, s.published_at
			FROM components c LEFT JOIN (
				SELECT component, version, min(published_at) AS published_at
				FROM component_versions WHERE stable
				GROUP BY component, version
				QUALIFY row_number() OVER (PARTITION BY component ORDER BY min(published_at) DESC, version DESC) = 1
			) s ON s.component = c.name
		) n LEFT JOIN (
			SELECT component, version
			FROM component_versions
			GROUP BY component, version
			QUALIFY row_number() OVER (PARTITION BY component ORDER BY min(published_at) DESC, version DESC) = 1
		) a ON a.component = n.name
		WHERE components.name = n.name`,
		`UPDATE owners SET parent_key = NULL WHERE parent_key IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM connectors k WHERE k.source_key = owners.parent_key)`,
		`DELETE FROM owners o WHERE NOT EXISTS (SELECT 1 FROM discoveries d WHERE d.owner_key = o.key)
			AND NOT EXISTS (SELECT 1 FROM connectors k WHERE k.source_key = o.key)`,
		`DELETE FROM component_references r WHERE NOT EXISTS (SELECT 1 FROM components c WHERE c.name = r.parent_name)`,
		`DELETE FROM attestation_sets a WHERE NOT EXISTS (SELECT 1 FROM components c WHERE c.name = a.component)
			OR NOT EXISTS (SELECT 1 FROM locations l WHERE l.id = a.location_id)`,
		`DELETE FROM attestation_levels a WHERE NOT EXISTS (SELECT 1 FROM attestation_sets s
			WHERE s.component = a.component AND s.version = a.version AND s.location_id = a.location_id)`,
		`DELETE FROM attestation_entries a WHERE NOT EXISTS (SELECT 1 FROM attestation_sets s
			WHERE s.component = a.component AND s.version = a.version AND s.location_id = a.location_id)`,
		`DELETE FROM version_changelogs c WHERE NOT EXISTS (SELECT 1 FROM component_versions v
			WHERE v.component = c.component AND v.version = c.version)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
	}
	return tx.Commit()
}

// PendingOverviews lists components whose default version's overview is
// missing, each with the location to fetch it from: the first
// one by connector position, then repository name.
func (w *Writer) PendingOverviews(ctx context.Context) ([]OverviewTarget, error) {
	rows, err := w.db.QueryContext(ctx, `
		SELECT c.name, c.default_version, l.repository
		FROM components c
		JOIN component_versions v ON v.component = c.name AND v.version = c.default_version
		JOIN locations l ON l.id = v.location_id
		JOIN discoveries d ON d.location_id = l.id AND d.component = c.name
		JOIN connectors k ON k.id = d.connector_id
		WHERE c.default_version IS NOT NULL AND c.overview_fetched_at IS NULL
		QUALIFY row_number() OVER (PARTITION BY c.name ORDER BY k.position, l.repository) = 1
		ORDER BY c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OverviewTarget
	for rows.Next() {
		var t OverviewTarget
		if err := rows.Scan(&t.Component, &t.Version, &t.Repository); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetOverview stores the display data fetched for t and replaces the
// component's references.
func (w *Writer) SetOverview(ctx context.Context, t OverviewTarget, o Overview) error {
	cats, err := json.Marshal(nonNil(o.Categories))
	if err != nil {
		return err
	}
	archs, err := json.Marshal(nonNil(o.Architectures))
	if err != nil {
		return err
	}
	title := o.Title
	if title == "" {
		title = t.Component
	}
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE components SET
			title = ?, description = nullif(?, ''), icon = nullif(?, ''), provider = nullif(?, ''),
			categories = from_json(?::JSON, '["VARCHAR"]'), architectures = from_json(?::JSON, '["VARCHAR"]'), signed = ?,
			overview_location_id = (SELECT id FROM locations WHERE repository = ?),
			overview_fetched_at = now()
		WHERE name = ?`, title, o.Description, o.Icon, o.Provider, string(cats), string(archs), o.Signed, t.Repository, t.Component); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM component_references WHERE parent_name = ?`, t.Component); err != nil {
		return err
	}
	ins, err := tx.PrepareContext(ctx, `INSERT INTO component_references VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ins.Close()
	for _, c := range o.Children {
		if _, err := ins.ExecContext(ctx, t.Component, t.Version, c.ReferenceName, c.ChildName, c.ChildVersion); err != nil {
			return fmt.Errorf("reference %s of %s: %w", c.ReferenceName, t.Component, err)
		}
	}
	return tx.Commit()
}

// MarkOverviewFailed resets the display data to the name and drops the
// component's references; the overview stays pending and is retried by the
// next sync.
func (w *Writer) MarkOverviewFailed(ctx context.Context, component string) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE components SET title = name, description = NULL, icon = NULL,
		provider = NULL, categories = NULL, architectures = NULL, signed = false, overview_location_id = NULL, overview_fetched_at = NULL WHERE name = ?`, component); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM component_references WHERE parent_name = ?`, component); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateFamilies recomputes every component's name prefix and top-level flag:
// a component is top level unless a parent other than itself has the same
// provider and the same name prefix. A reference cycle inside one family
// (A -> B, B -> A, same provider and prefix) hides both from the top level.
func (w *Writer) UpdateFamilies(ctx context.Context, prefix func(name string) string) error {
	names, err := w.componentNames(ctx)
	if err != nil {
		return err
	}
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	upd, err := tx.PrepareContext(ctx, `UPDATE components SET name_prefix = ? WHERE name = ?`)
	if err != nil {
		return err
	}
	defer upd.Close()
	for _, n := range names {
		if _, err := upd.ExecContext(ctx, prefix(n), n); err != nil {
			return fmt.Errorf("name prefix of %s: %w", n, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE components SET top_level = NOT EXISTS (
			SELECT 1 FROM component_references r JOIN components p ON p.name = r.parent_name
			WHERE r.child_name = components.name AND p.name <> components.name
				AND p.provider IS NOT DISTINCT FROM components.provider
				AND p.name_prefix IS NOT DISTINCT FROM components.name_prefix)`); err != nil {
		return fmt.Errorf("top level: %w", err)
	}
	// relevance: how many other components include it, plus, for a top-level
	// component, how many components it bundles (default versions' references).
	if _, err := tx.ExecContext(ctx, `UPDATE components SET relevance =
			(SELECT count(DISTINCT r.parent_name) FROM component_references r JOIN components p ON p.name = r.parent_name
				WHERE r.child_name = components.name AND r.parent_name <> components.name)
			+ CASE WHEN components.top_level THEN (SELECT count(DISTINCT r.child_name) FROM component_references r
				WHERE r.parent_name = components.name AND r.child_name <> components.name) ELSE 0 END`); err != nil {
		return fmt.Errorf("relevance: %w", err)
	}
	return tx.Commit()
}

func (w *Writer) componentNames(ctx context.Context) ([]string, error) {
	rows, err := w.db.QueryContext(ctx, `SELECT name FROM components ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ForceSchemaVersion overwrites the stored schema version (tests only).
func (w *Writer) ForceSchemaVersion(v int) error {
	_, err := w.db.Exec(`UPDATE schema_version SET version = ?`, v)
	return err
}

// AttestationTarget names one attestation set: a component version at a
// location (Repository is the location string, e.g. "oci::ghcr.io/acme").
type AttestationTarget struct{ Component, Version, Repository string }

// AttestationTargets lists every component's default version at its overview
// location, the only places attestations are gathered.
func (w *Writer) AttestationTargets(ctx context.Context) ([]AttestationTarget, error) {
	rows, err := w.db.QueryContext(ctx, `
		SELECT c.name, c.default_version, l.repository
		FROM components c JOIN locations l ON l.id = c.overview_location_id
		WHERE c.default_version IS NOT NULL
		ORDER BY c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AttestationTarget
	for rows.Next() {
		var t AttestationTarget
		if err := rows.Scan(&t.Component, &t.Version, &t.Repository); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// identityJSON encodes extraIdentity; nil for an empty map.
func identityJSON(m map[string]string) (any, error) {
	if len(m) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// identityColumns maps a resource identity to its name, version and
// extraIdentity columns; all NULL for nil.
func identityColumns(id *attestation.Identity) (name, version, extra any, err error) {
	if id == nil {
		return nil, nil, nil, nil
	}
	extra, err = identityJSON(id.ExtraIdentity)
	return id.Name, nullable(id.Version), extra, err
}

// SetAttestations replaces the attestation set of (component, version,
// location) with r.
func (w *Writer) SetAttestations(ctx context.Context, t AttestationTarget, r attestation.Result, checkedAt time.Time) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var loc int
	if err := tx.QueryRowContext(ctx, `SELECT id FROM locations WHERE repository = ?`, t.Repository).Scan(&loc); err != nil {
		return fmt.Errorf("attestation location %q: %w", t.Repository, err)
	}
	for _, table := range []string{"attestation_sets", "attestation_levels", "attestation_entries"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE component = ? AND version = ? AND location_id = ?`,
			t.Component, t.Version, loc); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO attestation_sets VALUES (?, ?, ?, ?)`,
		t.Component, t.Version, loc, checkedAt); err != nil {
		return err
	}
	for i, l := range r.Levels {
		rname, rver, extra, err := identityColumns(l.Resource)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO attestation_levels VALUES (?, ?, ?, ?, ?, ?, ?::JSON, ?, ?)`,
			t.Component, t.Version, loc, i, rname, rver, extra, string(l.Status), nullable(l.Error)); err != nil {
			return err
		}
		for j, e := range l.Entries {
			ername, erver, eextra, err := identityColumns(e.Resource)
			if err != nil {
				return err
			}
			var summary any
			if e.Summary != nil {
				b, err := json.Marshal(e.Summary)
				if err != nil {
					return err
				}
				summary = string(b)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO attestation_entries VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::JSON, ?, ?, ?, ?, ?, ?::JSON)`,
				t.Component, t.Version, loc, i, j, string(e.Kind), nullable(e.Format), nullable(e.Type), string(e.How),
				nullable(e.Platform), string(e.Signed), nullable(e.Digest), ername, erver, eextra, nullable(e.URL), nullable(e.BuildLevel),
				nullable(e.Repository), nullable(e.Builder), nullable(e.Label), summary); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ChangelogTarget is a component version whose changelog flag is missing.
// Repository is the component's overview location if it holds the version,
// else its first location.
type ChangelogTarget struct{ Component, Version, Repository string }

// PendingChangelogs lists up to limit versions without a stored changelog
// flag, newest published first. With retryFailed, versions whose last read
// failed are listed again.
func (w *Writer) PendingChangelogs(ctx context.Context, limit int, retryFailed bool) ([]ChangelogTarget, error) {
	rows, err := w.db.QueryContext(ctx, `
		SELECT component, version, repository FROM (
			SELECT v.component, v.version, l.repository,
				min(v.published_at) OVER (PARTITION BY v.component, v.version) AS published_at,
				row_number() OVER (PARTITION BY v.component, v.version
					ORDER BY (l.id IS NOT DISTINCT FROM c.overview_location_id) DESC, k.position, l.repository) AS rn
			FROM component_versions v
			JOIN components c ON c.name = v.component
			JOIN locations l ON l.id = v.location_id
			JOIN discoveries d ON d.location_id = l.id AND d.component = v.component
			JOIN connectors k ON k.id = d.connector_id
			LEFT JOIN version_changelogs s ON s.component = v.component AND s.version = v.version
			WHERE s.component IS NULL OR (? AND s.error IS NOT NULL))
		WHERE rn = 1
		ORDER BY published_at DESC, version DESC, component
		LIMIT ?`, retryFailed, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChangelogTarget
	for rows.Next() {
		var t ChangelogTarget
		if err := rows.Scan(&t.Component, &t.Version, &t.Repository); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetChangelog stores whether a version has a changelog: nil is unknown. A
// non-empty errMsg records a failed read (retried on full syncs).
func (w *Writer) SetChangelog(ctx context.Context, component, version string, has *bool, errMsg string) error {
	var h any
	if has != nil {
		h = *has
	}
	_, err := w.db.ExecContext(ctx, `
		INSERT INTO version_changelogs (component, version, has_changelog, error, checked_at)
		VALUES (?, ?, ?, ?, now())
		ON CONFLICT (component, version) DO UPDATE SET
			has_changelog = excluded.has_changelog, error = excluded.error, checked_at = excluded.checked_at`,
		component, version, h, nullable(errMsg))
	return err
}
