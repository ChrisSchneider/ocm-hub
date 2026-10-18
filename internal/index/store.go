package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/document"
)

// Store answers API queries from one read-only index file.
type Store struct {
	db  *sql.DB
	gen string
}

// Generation identifies the file contents (changes with every swap).
func (s *Store) Generation() string { return s.gen }

// ListQuery filters and pages GET /v1/components.
type ListQuery struct {
	Q                 string
	Locations         []string
	Connectors        []string // source keys
	Provider          string
	Categories        []string
	Architectures     []string // all must match
	IncludePrerelease bool
	TopLevel          bool   // only components without a parent of the same family
	Replication       string // "single" (one location), "replicated" (2+ locations) or "" (any)
	UpdatedSince      time.Time
	Sort              string // "updated" (default), "name" or "relevance"
	Limit             int    // 0: 50
	Cursor            *Cursor
}

// ComponentSummary is one catalog entry.
type ComponentSummary struct {
	Name, Latest                       string
	LatestPublishedAt                  time.Time // zero: no stable version
	Title, Description, Icon, Provider string
	Categories                         []string
	Signed                             bool
	DefaultVersion                     string // newest stable, else newest pre-release
	VersionCount                       int
	Locations                          []string
	OwnerAvatarURL                     string // avatar of the GitHub org hosting it, never a personal account ("" if none); the UI's icon fallback
}

// FacetValue is a value of a facet and how many components have it.
type FacetValue struct {
	Value string
	Count int
}

// Facets are counted over all filters except the facet's own dimension.
type Facets struct {
	Providers, Categories, Architectures []FacetValue
	Replication                          []FacetValue     // "single", "replicated"; values without components are left out
	Connectors                           []ConnectorFacet // in hub.yaml order
}

// ConnectorFacet is a source and how many components it found.
type ConnectorFacet struct {
	Key, Title, Type, AvatarURL string
	Count                       int
	Groups                      []GroupFacet // owners below this source; never nil
}

// GroupFacet is an owner below a source and how many components it holds.
type GroupFacet struct {
	Key, Title, AvatarURL string
	Count                 int
}

// ListResult is a page of components.
type ListResult struct {
	Components []ComponentSummary
	Next       *Cursor
	Total      int // components matching the filters, over all pages
	Facets     Facets
}

const epoch = `TIMESTAMPTZ '1970-01-01 00:00:00+00'`

type dimension int

const (
	dimNone dimension = iota
	dimLocation
	dimConnector
	dimProvider
	dimCategory
	dimArchitecture
	dimReplication
)

// locationCount counts the distinct repositories holding versions of c.
const locationCount = `(SELECT count(DISTINCT l.repository) FROM component_versions v JOIN locations l ON l.id = v.location_id WHERE v.component = c.name)`

// where builds the filter of q, leaving out skip's dimension.
func (q ListQuery) where(skip dimension) (string, []any) {
	var conds []string
	var args []any
	if !q.IncludePrerelease {
		conds = append(conds, `c.latest_version IS NOT NULL`)
	}
	if q.TopLevel {
		conds = append(conds, "c.top_level")
	}
	if s := strings.ToLower(strings.TrimSpace(q.Q)); s != "" {
		conds = append(conds, `(contains(lower(c.name), ?) OR contains(lower(c.title), ?))`)
		args = append(args, s, s)
	}
	if len(q.Locations) > 0 && skip != dimLocation {
		conds = append(conds, `c.name IN (SELECT v.component FROM component_versions v JOIN locations l ON l.id = v.location_id
			WHERE l.repository IN (`+placeholders(len(q.Locations))+`))`)
		for _, l := range q.Locations {
			args = append(args, l)
		}
	}
	if len(q.Connectors) > 0 && skip != dimConnector {
		in := placeholders(len(q.Connectors))
		conds = append(conds, `c.name IN (SELECT d.component FROM discoveries d
			JOIN component_versions v ON v.component = d.component AND v.location_id = d.location_id
			WHERE d.owner_key IN (`+in+`) OR d.owner_key IN (SELECT o.key FROM owners o WHERE o.parent_key IN (`+in+`)))`)
		for range 2 {
			for _, k := range q.Connectors {
				args = append(args, k)
			}
		}
	}
	if q.Provider != "" && skip != dimProvider {
		conds = append(conds, `c.provider = ?`)
		args = append(args, q.Provider)
	}
	if skip != dimCategory {
		for _, cat := range q.Categories {
			conds = append(conds, `list_contains(c.categories, ?)`)
			args = append(args, cat)
		}
	}
	if skip != dimArchitecture {
		for _, a := range q.Architectures {
			conds = append(conds, `list_contains(c.architectures, ?)`)
			args = append(args, a)
		}
	}
	if skip != dimReplication {
		switch q.Replication {
		case "single":
			conds = append(conds, locationCount+` = 1`)
		case "replicated":
			conds = append(conds, locationCount+` >= 2`)
		}
	}
	if !q.UpdatedSince.IsZero() {
		conds = append(conds, `c.latest_published_at >= ?`)
		args = append(args, q.UpdatedSince)
	}
	if len(conds) == 0 {
		return "TRUE", nil
	}
	return strings.Join(conds, " AND "), args
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?, ", n), ", ") }

const published = `coalesce(c.latest_published_at, ` + epoch + `)`

// ListComponents returns one page of components plus facets.
func (s *Store) ListComponents(ctx context.Context, q ListQuery) (ListResult, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	where, args := q.where(dimNone)
	var out ListResult
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM components c WHERE `+where, args...).Scan(&out.Total); err != nil {
		return ListResult{}, err
	}
	order := published + ` DESC, c.name ASC`
	switch q.Sort {
	case "name":
		order = `c.name ASC`
	case "relevance":
		order = `c.relevance DESC, ` + published + ` DESC, c.name ASC`
	}
	if q.Cursor != nil {
		switch q.Sort {
		case "name":
			where += ` AND c.name > ?`
			args = append(args, q.Cursor.Name)
		case "relevance":
			score, at, ok := strings.Cut(q.Cursor.Key, "|")
			n, err := strconv.Atoi(score)
			key, terr := time.Parse(time.RFC3339Nano, at)
			if !ok || err != nil || terr != nil {
				return ListResult{}, ErrBadCursor
			}
			where += ` AND (c.relevance < ? OR (c.relevance = ? AND (` + published + ` < ? OR (` + published + ` = ? AND c.name > ?))))`
			args = append(args, n, n, key, key, q.Cursor.Name)
		default:
			key, err := time.Parse(time.RFC3339Nano, q.Cursor.Key)
			if err != nil {
				return ListResult{}, ErrBadCursor
			}
			where += ` AND (` + published + ` < ? OR (` + published + ` = ? AND c.name > ?))`
			args = append(args, key, key, q.Cursor.Name)
		}
	}
	rows, err := s.db.QueryContext(ctx, summarySelect+` WHERE `+where+` ORDER BY `+order+` LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return ListResult{}, err
	}
	if out.Components, err = scanSummaries(rows); err != nil {
		return ListResult{}, err
	}
	if len(out.Components) > limit {
		out.Components = out.Components[:limit]
		last := out.Components[limit-1]
		key := ""
		if q.Sort != "name" {
			t := last.LatestPublishedAt
			if t.IsZero() {
				t = time.Unix(0, 0).UTC()
			}
			key = t.UTC().Format(time.RFC3339Nano)
		}
		if q.Sort == "relevance" {
			var n int
			if err := s.db.QueryRowContext(ctx, `SELECT relevance FROM components WHERE name = ?`, last.Name).Scan(&n); err != nil {
				return ListResult{}, err
			}
			key = strconv.Itoa(n) + "|" + key
		}
		out.Next = &Cursor{Key: key, Name: last.Name}
	}
	if err := s.fillCounts(ctx, out.Components); err != nil {
		return ListResult{}, err
	}
	out.Facets, err = s.facets(ctx, q)
	return out, err
}

// summarySelect selects the columns scanSummaries reads.
const summarySelect = `SELECT c.name, coalesce(c.latest_version, ''), c.latest_published_at, c.title,
	coalesce(c.description, ''), coalesce(c.icon, ''), coalesce(c.provider, ''),
	coalesce(to_json(c.categories)::VARCHAR, '[]'), c.signed, coalesce(c.default_version, ''),
	coalesce((SELECT o.avatar_url FROM discoveries d JOIN owners o ON o.key = d.owner_key
		WHERE d.component = c.name AND o.key LIKE 'github:org:%' AND o.avatar_url IS NOT NULL
		ORDER BY (d.location_id IS NOT DISTINCT FROM c.overview_location_id) DESC, o.key LIMIT 1), '')
	FROM components c`

// scanSummaries reads summarySelect rows and closes them.
func scanSummaries(rows *sql.Rows) ([]ComponentSummary, error) {
	defer rows.Close()
	var out []ComponentSummary
	for rows.Next() {
		var c ComponentSummary
		var published sql.NullTime
		var cats string
		if err := rows.Scan(&c.Name, &c.Latest, &published, &c.Title, &c.Description, &c.Icon, &c.Provider, &cats, &c.Signed, &c.DefaultVersion, &c.OwnerAvatarURL); err != nil {
			return nil, err
		}
		c.LatestPublishedAt = published.Time
		if err := json.Unmarshal([]byte(cats), &c.Categories); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// fillCounts sets VersionCount and Locations of the page's components.
func (s *Store) fillCounts(ctx context.Context, cs []ComponentSummary) error {
	if len(cs) == 0 {
		return nil
	}
	idx := map[string]*ComponentSummary{}
	args := make([]any, len(cs))
	for i := range cs {
		idx[cs[i].Name] = &cs[i]
		args[i] = cs[i].Name
	}
	in := placeholders(len(cs))
	rows, err := s.db.QueryContext(ctx, `SELECT component, count(DISTINCT version) FROM component_versions
		WHERE component IN (`+in+`) GROUP BY component`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			rows.Close()
			return err
		}
		idx[name].VersionCount = n
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, `SELECT DISTINCT v.component, l.repository FROM component_versions v
		JOIN locations l ON l.id = v.location_id WHERE v.component IN (`+in+`) ORDER BY 1, 2`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, repo string
		if err := rows.Scan(&name, &repo); err != nil {
			return err
		}
		idx[name].Locations = append(idx[name].Locations, repo)
	}
	return rows.Err()
}

// connectorFacet counts the components each source found (with versions at
// the location it found them), in hub.yaml order; sources without any are left out.
func (s *Store) connectorFacet(ctx context.Context, where string, args []any) ([]ConnectorFacet, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT k.source_key, k.title, k.type, coalesce(k.avatar_url, ''), count(DISTINCT d.component)
		FROM discoveries d JOIN connectors k ON k.id = d.connector_id
		WHERE EXISTS (SELECT 1 FROM component_versions v WHERE v.component = d.component AND v.location_id = d.location_id)
			AND d.component IN (SELECT c.name FROM components c WHERE `+where+`)
		GROUP BY k.source_key, k.title, k.type, k.avatar_url, k.position ORDER BY k.position`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConnectorFacet{}
	for rows.Next() {
		var f ConnectorFacet
		if err := rows.Scan(&f.Key, &f.Title, &f.Type, &f.AvatarURL, &f.Count); err != nil {
			return nil, err
		}
		f.Groups = []GroupFacet{}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	gs, err := s.db.QueryContext(ctx, `SELECT k.source_key, o.key, o.title, coalesce(o.avatar_url, ''), count(DISTINCT d.component)
		FROM discoveries d JOIN connectors k ON k.id = d.connector_id
			JOIN owners o ON o.key = d.owner_key AND o.parent_key = k.source_key
		WHERE EXISTS (SELECT 1 FROM component_versions v WHERE v.component = d.component AND v.location_id = d.location_id)
			AND d.component IN (SELECT c.name FROM components c WHERE `+where+`)
		GROUP BY 1, 2, 3, 4 ORDER BY 5 DESC, 3`, args...)
	if err != nil {
		return nil, err
	}
	defer gs.Close()
	for gs.Next() {
		var src string
		var g GroupFacet
		if err := gs.Scan(&src, &g.Key, &g.Title, &g.AvatarURL, &g.Count); err != nil {
			return nil, err
		}
		for i := range out {
			if out[i].Key == src {
				out[i].Groups = append(out[i].Groups, g)
			}
		}
	}
	return out, gs.Err()
}

func (s *Store) facets(ctx context.Context, q ListQuery) (Facets, error) {
	var f Facets
	var err error
	w, a := q.where(dimConnector)
	if f.Connectors, err = s.connectorFacet(ctx, w, a); err != nil {
		return f, err
	}
	w, a = q.where(dimProvider)
	if f.Providers, err = s.facet(ctx, `SELECT c.provider, count(*) FROM components c
		WHERE c.provider IS NOT NULL AND `+w+` GROUP BY 1 ORDER BY 1`, a); err != nil {
		return f, err
	}
	w, a = q.where(dimCategory)
	if f.Categories, err = s.facet(ctx, `SELECT cat, count(*) FROM (SELECT unnest(c.categories) AS cat FROM components c
		WHERE `+w+`) GROUP BY 1 ORDER BY 1`, a); err != nil {
		return f, err
	}
	w, a = q.where(dimArchitecture)
	if f.Architectures, err = s.facet(ctx, `SELECT arch, count(*) FROM (SELECT unnest(c.architectures) AS arch FROM components c
		WHERE `+w+`) GROUP BY 1 ORDER BY 1`, a); err != nil {
		return f, err
	}
	w, a = q.where(dimReplication)
	f.Replication, err = s.facet(ctx, `SELECT r, count(*) FROM (SELECT CASE WHEN `+locationCount+` >= 2 THEN 'replicated' ELSE 'single' END AS r
		FROM components c WHERE `+w+`) GROUP BY 1 ORDER BY 1 DESC`, a)
	return f, err
}

func (s *Store) facet(ctx context.Context, query string, args []any) ([]FacetValue, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FacetValue{}
	for rows.Next() {
		var v FacetValue
		if err := rows.Scan(&v.Value, &v.Count); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LocationSummary is a component at one location.
type LocationSummary struct {
	Repository, Latest string // Latest: newest stable at this location, "" if none
	VersionCount       int
}

// ParentRef is a component that references another one (its parent).
type ParentRef struct {
	Name, Title, Latest string // Latest "" if none
}

// referencedByLimit caps ComponentDetail.ReferencedBy.
const referencedByLimit = 50

// ComponentDetail is the overview of one component.
type ComponentDetail struct {
	ComponentSummary
	LocationDetails   []LocationSummary
	Sources           []SourceSummary // in hub.yaml order
	ReferencedBy      []ParentRef     // at most referencedByLimit, by name
	ReferencedByCount int             // all parents
}

// SourceSummary is a source that found a component, with the locations it found it at.
type SourceSummary struct {
	Key, Title, Type, AvatarURL string
	Locations                   []string       // by repository
	Groups                      []GroupSummary // never nil
}

// GroupSummary is an owner below a source with the locations it holds the component at.
type GroupSummary struct {
	Key, Title, AvatarURL string
	Locations             []string
}

// Component returns the overview of name, or ErrNotFound.
func (s *Store) Component(ctx context.Context, name string) (ComponentDetail, error) {
	rows, err := s.db.QueryContext(ctx, summarySelect+` WHERE c.name = ?`, name)
	if err != nil {
		return ComponentDetail{}, err
	}
	cs, err := scanSummaries(rows)
	if err != nil {
		return ComponentDetail{}, err
	}
	if len(cs) == 0 {
		return ComponentDetail{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err := s.fillCounts(ctx, cs); err != nil {
		return ComponentDetail{}, err
	}
	d := ComponentDetail{ComponentSummary: cs[0]}
	rows, err = s.db.QueryContext(ctx, `SELECT l.repository, coalesce(arg_max(v.version, v.published_at) FILTER (WHERE v.stable), ''),
			count(DISTINCT v.version)
		FROM component_versions v JOIN locations l ON l.id = v.location_id
		WHERE v.component = ? GROUP BY l.repository ORDER BY l.repository`, name)
	if err != nil {
		return ComponentDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var l LocationSummary
		if err := rows.Scan(&l.Repository, &l.Latest, &l.VersionCount); err != nil {
			return ComponentDetail{}, err
		}
		d.LocationDetails = append(d.LocationDetails, l)
	}
	if err := rows.Err(); err != nil {
		return ComponentDetail{}, err
	}
	rows.Close()
	if d.Sources, err = s.sources(ctx, name); err != nil {
		return ComponentDetail{}, err
	}
	d.ReferencedBy = []ParentRef{}
	prows, err := s.db.QueryContext(ctx, `SELECT p.name, p.title, coalesce(p.latest_version, ''), count(*) OVER ()
		FROM components p WHERE p.name <> ? AND p.name IN (SELECT parent_name FROM component_references WHERE child_name = ?)
		ORDER BY p.name LIMIT ?`, name, name, referencedByLimit)
	if err != nil {
		return ComponentDetail{}, err
	}
	defer prows.Close()
	for prows.Next() {
		var p ParentRef
		if err := prows.Scan(&p.Name, &p.Title, &p.Latest, &d.ReferencedByCount); err != nil {
			return ComponentDetail{}, err
		}
		d.ReferencedBy = append(d.ReferencedBy, p)
	}
	return d, prows.Err()
}

// VersionsQuery filters and pages GET /v1/components/{name}/versions.
type VersionsQuery struct {
	Component         string
	IncludePrerelease bool
	Locations         []string
	Constraint        *semver.Constraints // drops non-semver versions
	Limit             int                 // 0: 100
	Cursor            *Cursor
}

// VersionEntry is one version across all its locations.
type VersionEntry struct {
	Version      string
	PublishedAt  time.Time // earliest across locations
	Stable       bool
	Locations    []string
	HasChangelog *bool // nil: unknown
}

// VersionsResult is a page of versions, newest first.
type VersionsResult struct {
	Versions []VersionEntry
	Next     *Cursor
}

// Versions returns the component's versions, newest first. Filters other than
// prerelease are applied in Go: a component has at most a few thousand
// versions, and semver constraints cannot run in SQL.
func (s *Store) Versions(ctx context.Context, q VersionsQuery) (VersionsResult, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT v.version, min(v.published_at), bool_and(v.stable),
			string_agg(DISTINCT l.repository, chr(10) ORDER BY l.repository),
			any_value(s.has_changelog)
		FROM component_versions v JOIN locations l ON l.id = v.location_id
		LEFT JOIN version_changelogs s ON s.component = v.component AND s.version = v.version
		WHERE v.component = ? GROUP BY v.version ORDER BY min(v.published_at) DESC, v.version DESC`, q.Component)
	if err != nil {
		return VersionsResult{}, err
	}
	defer rows.Close()
	var all []VersionEntry
	for rows.Next() {
		var e VersionEntry
		var locs string
		var has sql.NullBool
		if err := rows.Scan(&e.Version, &e.PublishedAt, &e.Stable, &locs, &has); err != nil {
			return VersionsResult{}, err
		}
		if has.Valid {
			e.HasChangelog = &has.Bool
		}
		e.Locations = strings.Split(locs, "\n")
		all = append(all, e)
	}
	if err := rows.Err(); err != nil {
		return VersionsResult{}, err
	}
	if len(all) == 0 {
		return VersionsResult{}, fmt.Errorf("%w: %s", ErrNotFound, q.Component)
	}
	var keep []VersionEntry
	for _, e := range all {
		if !q.IncludePrerelease && !e.Stable {
			continue
		}
		if len(q.Locations) > 0 && !slices.ContainsFunc(e.Locations, func(l string) bool { return slices.Contains(q.Locations, l) }) {
			continue
		}
		if q.Constraint != nil {
			sv, err := semver.NewVersion(e.Version)
			if err != nil || !q.Constraint.Check(sv) {
				continue
			}
		}
		keep = append(keep, e)
	}
	if q.Cursor != nil {
		i := slices.IndexFunc(keep, func(e VersionEntry) bool { return e.Version == q.Cursor.Name })
		if i < 0 {
			return VersionsResult{}, ErrBadCursor
		}
		keep = keep[i+1:]
	}
	var out VersionsResult
	if len(keep) > limit {
		keep = keep[:limit]
		out.Next = &Cursor{Key: keep[limit-1].PublishedAt.UTC().Format(time.RFC3339Nano), Name: keep[limit-1].Version}
	}
	out.Versions = keep
	if out.Versions == nil {
		out.Versions = []VersionEntry{}
	}
	return out, nil
}

// VersionLocations lists where component:version lives, the component's
// overview location first, then by repository.
func (s *Store) VersionLocations(ctx context.Context, component, version string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.repository FROM component_versions v
		JOIN locations l ON l.id = v.location_id JOIN components c ON c.name = v.component
		WHERE v.component = ? AND v.version = ?
		ORDER BY (l.id IS NOT DISTINCT FROM c.overview_location_id) DESC, l.repository`, component, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// VersionPublishedAt returns the earliest publish time of component:version
// across all locations; false when the index does not know the version.
func (s *Store) VersionPublishedAt(ctx context.Context, component, version string) (time.Time, bool, error) {
	var t sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT min(published_at) FROM component_versions WHERE component = ? AND version = ?`,
		component, version).Scan(&t)
	if err != nil || !t.Valid {
		return time.Time{}, false, err
	}
	return t.Time.UTC(), true, nil
}

// HasVersionAt reports whether component:version is at repository.
func (s *Store) HasVersionAt(ctx context.Context, component, version, repository string) (bool, error) {
	return s.exists(ctx, `SELECT 1 FROM component_versions v JOIN locations l ON l.id = v.location_id
		WHERE v.component = ? AND v.version = ? AND l.repository = ?`, component, version, repository)
}

// IsLocation reports whether repository is an indexed location.
func (s *Store) IsLocation(ctx context.Context, repository string) (bool, error) {
	return s.exists(ctx, `SELECT 1 FROM locations WHERE repository = ?`, repository)
}

func (s *Store) exists(ctx context.Context, q string, args ...any) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, q+` LIMIT 1`, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) sources(ctx context.Context, name string) ([]SourceSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT k.source_key, k.title, k.type, coalesce(k.avatar_url, ''), l.repository,
			d.owner_key, coalesce(o.title, ''), coalesce(o.avatar_url, ''), coalesce(o.parent_key, '')
		FROM discoveries d JOIN connectors k ON k.id = d.connector_id JOIN locations l ON l.id = d.location_id
			LEFT JOIN owners o ON o.key = d.owner_key
		WHERE d.component = ? AND EXISTS (SELECT 1 FROM component_versions v WHERE v.component = d.component AND v.location_id = d.location_id)
		ORDER BY k.position, o.title, l.repository`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceSummary{}
	for rows.Next() {
		var src SourceSummary
		var repo, ownerKey, ownerTitle, ownerAvatar, parent string
		if err := rows.Scan(&src.Key, &src.Title, &src.Type, &src.AvatarURL, &repo, &ownerKey, &ownerTitle, &ownerAvatar, &parent); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].Key != src.Key {
			src.Locations, src.Groups = []string{}, []GroupSummary{}
			out = append(out, src)
		}
		cur := &out[len(out)-1]
		if !slices.Contains(cur.Locations, repo) {
			cur.Locations = append(cur.Locations, repo)
		}
		if parent != cur.Key {
			continue
		}
		gi := slices.IndexFunc(cur.Groups, func(g GroupSummary) bool { return g.Key == ownerKey })
		if gi < 0 {
			cur.Groups = append(cur.Groups, GroupSummary{Key: ownerKey, Title: ownerTitle, AvatarURL: ownerAvatar, Locations: []string{}})
			gi = len(cur.Groups) - 1
		}
		if g := &cur.Groups[gi]; !slices.Contains(g.Locations, repo) {
			g.Locations = append(g.Locations, repo)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		slices.Sort(out[i].Locations)
		for j := range out[i].Groups {
			slices.Sort(out[i].Groups[j].Locations)
		}
	}
	return out, nil
}

// AttestationSet is the attestations gathered for one component version at one location.
type AttestationSet struct {
	Version, Repository string
	CheckedAt           time.Time
	Result              attestation.Result
}

// scanIdentity builds an Identity from nullable columns; nil when name is NULL.
func scanIdentity(name, version, extra sql.NullString) (*attestation.Identity, error) {
	if !name.Valid {
		return nil, nil
	}
	id := &attestation.Identity{Name: name.String, Version: version.String}
	if extra.Valid && extra.String != "" {
		if err := json.Unmarshal([]byte(extra.String), &id.ExtraIdentity); err != nil {
			return nil, err
		}
	}
	return id, nil
}

// Attestations returns the attestation set of component:version at
// repository (required, a location string); false when none is stored there.
func (s *Store) Attestations(ctx context.Context, component, version, repository string) (AttestationSet, bool, error) {
	if repository == "" {
		return AttestationSet{}, false, errors.New("attestations: repository is required")
	}
	set := AttestationSet{Version: version}
	var locID int
	var checked time.Time
	err := s.db.QueryRowContext(ctx, `SELECT a.location_id, l.repository, a.checked_at
		FROM attestation_sets a JOIN locations l ON l.id = a.location_id
		WHERE a.component = ? AND a.version = ? AND l.repository = ?`,
		component, version, repository).Scan(&locID, &set.Repository, &checked)
	if errors.Is(err, sql.ErrNoRows) {
		return AttestationSet{}, false, nil
	}
	if err != nil {
		return AttestationSet{}, false, err
	}
	set.CheckedAt = checked.UTC()

	rows, err := s.db.QueryContext(ctx, `SELECT position, resource_name, resource_version, extra_identity::VARCHAR, status, error
		FROM attestation_levels WHERE component = ? AND version = ? AND location_id = ? ORDER BY position`,
		component, version, locID)
	if err != nil {
		return AttestationSet{}, false, err
	}
	defer rows.Close()
	byPos := map[int]int{}
	for rows.Next() {
		var pos int
		var name, ver, extra, errMsg sql.NullString
		var l attestation.Level
		var status string
		if err := rows.Scan(&pos, &name, &ver, &extra, &status, &errMsg); err != nil {
			return AttestationSet{}, false, err
		}
		if l.Resource, err = scanIdentity(name, ver, extra); err != nil {
			return AttestationSet{}, false, err
		}
		l.Status, l.Error, l.Entries = attestation.Status(status), errMsg.String, []attestation.Entry{}
		byPos[pos] = len(set.Result.Levels)
		set.Result.Levels = append(set.Result.Levels, l)
	}
	if err := rows.Err(); err != nil {
		return AttestationSet{}, false, err
	}

	erows, err := s.db.QueryContext(ctx, `SELECT level_position, kind, format, type, how, platform, signed, digest,
			resource_name, resource_version, resource_extra_identity::VARCHAR, url, build_level,
			repository, builder, label, summary::VARCHAR
		FROM attestation_entries WHERE component = ? AND version = ? AND location_id = ? ORDER BY level_position, position`,
		component, version, locID)
	if err != nil {
		return AttestationSet{}, false, err
	}
	defer erows.Close()
	for erows.Next() {
		var lp int
		var kind, how, signed string
		var format, typ, platform, digest, name, ver, extra, url, repo, builder, label, summary sql.NullString
		var level sql.NullInt64
		if err := erows.Scan(&lp, &kind, &format, &typ, &how, &platform, &signed, &digest, &name, &ver, &extra, &url, &level, &repo, &builder, &label, &summary); err != nil {
			return AttestationSet{}, false, err
		}
		e := attestation.Entry{Kind: attestation.Kind(kind), Format: format.String, Type: typ.String, How: attestation.How(how),
			Platform: platform.String, Signed: attestation.Signed(signed), Digest: digest.String, URL: url.String, BuildLevel: int(level.Int64),
			Repository: repo.String, Builder: builder.String, Label: label.String}
		if summary.Valid {
			e.Summary = &document.Summary{}
			if err := json.Unmarshal([]byte(summary.String), e.Summary); err != nil {
				return AttestationSet{}, false, err
			}
		}
		if e.Resource, err = scanIdentity(name, ver, extra); err != nil {
			return AttestationSet{}, false, err
		}
		if i, ok := byPos[lp]; ok {
			set.Result.Levels[i].Entries = append(set.Result.Levels[i].Entries, e)
		}
	}
	return set, true, erows.Err()
}

// CollectedAttestations lists every (version, location) with a gathered set
// for component: the current target (default version at the overview
// location) first, then by version and repository.
func (s *Store) CollectedAttestations(ctx context.Context, component string) ([]AttestationTarget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.version, l.repository FROM attestation_sets a
		JOIN locations l ON l.id = a.location_id JOIN components c ON c.name = a.component
		WHERE a.component = ?
		ORDER BY (a.version IS NOT DISTINCT FROM c.default_version AND a.location_id IS NOT DISTINCT FROM c.overview_location_id) DESC,
			a.version, l.repository`, component)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AttestationTarget
	for rows.Next() {
		t := AttestationTarget{Component: component}
		if err := rows.Scan(&t.Version, &t.Repository); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
