package index

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
)

const locA, locB = "oci::ghcr.io/a", "oci::ghcr.io/b"

func TestApplyReplacesVersionSetAndComputesLatest(t *testing.T) {
	w, _ := newWriter(t)
	runSync(t, w, "c1", 0, true, day(10), Found{Component: "acme.org/x", Repository: locA,
		Versions: []FoundVersion{v("1.0.0", 1), v("1.1.0", 3), pre("2.0.0-rc.1", 5)}})
	assertLatest(t, w, "acme.org/x", "1.1.0")

	// delta: complete new list without 1.1.0
	runSync(t, w, "c1", 0, false, day(11), Found{Component: "acme.org/x", Repository: locA,
		Versions: []FoundVersion{v("1.0.0", 1), pre("2.0.0-rc.1", 5)}})
	assertLatest(t, w, "acme.org/x", "1.0.0")
	if got := queryStrings(t, w, "SELECT version FROM component_versions ORDER BY version"); !slices.Equal(got, []string{"1.0.0", "2.0.0-rc.1"}) {
		t.Fatalf("versions = %v", got)
	}
}

func TestLatestUsesEarliestPublishAcrossLocations(t *testing.T) {
	w, _ := newWriter(t)
	// 1.0.0 mirrored to B much later; 1.1.0 only in A on day 5
	runSync(t, w, "c1", 0, true, day(20),
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1), v("1.1.0", 5)}},
		Found{Component: "acme.org/x", Repository: locB, Versions: []FoundVersion{v("1.0.0", 9)}})
	assertLatest(t, w, "acme.org/x", "1.1.0")
}

func TestFullSyncDropsUnseenAndCascades(t *testing.T) {
	w, _ := newWriter(t)
	runSync(t, w, "c1", 0, true, day(1),
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}},
		Found{Component: "acme.org/y", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}})
	// delta never deletes
	runSync(t, w, "c1", 0, false, day(2), Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}})
	if n := len(queryStrings(t, w, "SELECT name FROM components")); n != 2 {
		t.Fatalf("after delta: %d components", n)
	}
	// full without y
	runSync(t, w, "c1", 0, true, day(3), Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}})
	if got := queryStrings(t, w, "SELECT name FROM components"); !slices.Equal(got, []string{"acme.org/x"}) {
		t.Fatalf("components = %v", got)
	}
	// full with nothing: location and component gone
	runSync(t, w, "c1", 0, true, day(4))
	if n := len(queryStrings(t, w, "SELECT repository FROM locations")); n != 0 {
		t.Fatalf("locations left: %d", n)
	}
	if n := len(queryStrings(t, w, "SELECT name FROM components")); n != 0 {
		t.Fatalf("components left: %d", n)
	}
}

func TestTwoConnectorsShareALocation(t *testing.T) {
	w, _ := newWriter(t)
	f := Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}}
	runSync(t, w, "c1", 0, true, day(1), f)
	runSync(t, w, "c2", 1, true, day(1), f)
	runSync(t, w, "c1", 0, true, day(2)) // c1 no longer sees it
	if got := queryStrings(t, w, "SELECT name FROM components"); len(got) != 1 {
		t.Fatalf("component must survive while c2 reports it: %v", got)
	}
	runSync(t, w, "c2", 1, true, day(2))
	if got := queryStrings(t, w, "SELECT name FROM components"); len(got) != 0 {
		t.Fatalf("component must go when no connector reports it: %v", got)
	}
}

func TestOverviewLifecycle(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	runSync(t, w, "c2", 1, true, day(1), Found{Component: "acme.org/x", Repository: locB, Versions: []FoundVersion{v("1.0.0", 1)}})
	runSync(t, w, "c1", 0, true, day(1), Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 2)}},
		Found{Component: "acme.org/onlypre", Repository: locA, Versions: []FoundVersion{pre("1.0.0-rc.1", 1)}})
	targets, err := w.PendingOverviews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// c1 has position 0 → its location wins; a component with only pre-releases uses its newest pre-release
	want := []OverviewTarget{{Component: "acme.org/onlypre", Version: "1.0.0-rc.1", Repository: locA}, {Component: "acme.org/x", Version: "1.0.0", Repository: locA}}
	if !slices.Equal(targets, want) {
		t.Fatalf("targets = %+v", targets)
	}
	o := Overview{Title: "X", Description: "d", Icon: "lucide/box", Provider: "acme", Categories: []string{"tools", "cli"}}
	if err := w.SetOverview(ctx, targets[1], o); err != nil {
		t.Fatal(err)
	}
	if err := w.SetOverview(ctx, targets[0], Overview{Title: "Pre"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.PendingOverviews(ctx); len(got) != 0 {
		t.Fatalf("pending after set = %+v", got)
	}
	var cats string
	if err := w.db.QueryRow("SELECT to_json(categories)::VARCHAR FROM components WHERE name='acme.org/x'").Scan(&cats); err != nil || cats != `["tools","cli"]` {
		t.Fatalf("categories = %q (%v)", cats, err)
	}
	// unchanged latest: no refetch; new latest: refetch
	runSync(t, w, "c1", 0, false, day(3), Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 2)}})
	if got, _ := w.PendingOverviews(ctx); len(got) != 0 {
		t.Fatalf("refetch without latest change: %+v", got)
	}
	runSync(t, w, "c1", 0, false, day(4), Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 2), v("1.1.0", 4)}})
	if got, _ := w.PendingOverviews(ctx); len(got) != 1 || got[0].Version != "1.1.0" {
		t.Fatalf("pending after new latest = %+v", got)
	}
	if err := w.MarkOverviewFailed(ctx, "acme.org/x"); err != nil {
		t.Fatal(err)
	}
	var title string
	var desc sql.NullString
	w.db.QueryRow("SELECT title, description FROM components WHERE name='acme.org/x'").Scan(&title, &desc)
	if title != "acme.org/x" || desc.Valid {
		t.Fatalf("after failure title=%q desc=%v", title, desc)
	}
	if got, _ := w.PendingOverviews(ctx); len(got) != 1 {
		t.Fatal("a failed overview must stay pending")
	}
}

func TestConnectorStateAndErrors(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	if _, ok, err := w.ConnectorState(ctx, "c1"); ok || err != nil {
		t.Fatalf("fresh: ok=%v err=%v", ok, err)
	}
	runSync(t, w, "c1", 0, true, day(1))
	s, ok, err := w.ConnectorState(ctx, "c1")
	if !ok || err != nil || !s.LastFullSyncAt.Equal(day(1)) || !s.LastDeltaSyncAt.Equal(day(1)) {
		t.Fatalf("state = %+v ok=%v err=%v", s, ok, err)
	}
	if err := w.RecordError(ctx, "c1", "test/v1", Source{Key: "c1", Title: "c1"}, 0, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	s, _, _ = w.ConnectorState(ctx, "c1")
	if s.LastError != "boom" || !s.LastFullSyncAt.Equal(day(1)) {
		t.Fatalf("state = %+v", s)
	}
}

func TestRollbackKeepsPreviousData(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	runSync(t, w, "c1", 0, true, day(1), Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}})
	tx, err := w.BeginConnector(ctx, "c1", "test/v1", Source{Key: "c1", Title: "c1"}, 0, day(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Apply(ctx, Found{Component: "acme.org/x", Repository: locA, Owner: "test:c1", Visibility: "public"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := queryStrings(t, w, "SELECT version FROM component_versions"); len(got) != 1 {
		t.Fatalf("rollback lost data: %v", got)
	}
}

func TestOpenWriterSchemaMismatch(t *testing.T) {
	w, p := newWriter(t)
	if _, err := w.db.Exec("UPDATE schema_version SET version = 999"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if _, err := OpenWriter(p); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func assertLatest(t *testing.T, w *Writer, name, want string) {
	t.Helper()
	var got sql.NullString
	if err := w.db.QueryRow("SELECT latest_version FROM components WHERE name = ?", name).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got.String != want {
		t.Fatalf("latest(%s) = %q, want %q", name, got.String, want)
	}
}

func TestPruneConnectors(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	runSync(t, w, "a", 0, true, day(1), Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}})
	runSync(t, w, "b", 1, true, day(1), Found{Component: "acme.org/y", Repository: locB, Versions: []FoundVersion{v("1.0.0", 1)}})
	if err := w.PruneConnectors(ctx, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if got := queryStrings(t, w, "SELECT name FROM components"); !slices.Equal(got, []string{"acme.org/x"}) {
		t.Fatalf("components = %v", got)
	}
	if got := queryStrings(t, w, "SELECT id FROM connectors"); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("connectors = %v", got)
	}
	if err := w.PruneConnectors(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(queryStrings(t, w, "SELECT name FROM components")) + len(queryStrings(t, w, "SELECT id FROM connectors")); n != 0 {
		t.Fatalf("%d rows left after pruning all", n)
	}
}

func setOverview(t *testing.T, w *Writer, comp, version, repo string, o Overview) {
	t.Helper()
	if err := w.SetOverview(context.Background(), OverviewTarget{Component: comp, Version: version, Repository: repo}, o); err != nil {
		t.Fatal(err)
	}
}

func prefixFirstSegment(name string) string { return strings.SplitN(name, "/", 2)[0] }

func TestTopLevelAndReferences(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	const loc = "oci::ghcr.io/acme"
	found := func(name string) Found {
		return Found{Component: name, Repository: loc, Versions: []FoundVersion{v("1.0.0", 1)}}
	}
	runSync(t, w, "c", 0, true, day(10),
		found("ocm.software/cli"), found("ocm.software/plugins/ecr"), found("helm.sh/helm"),
		found("ocm.software/other-provider-child"), found("ocm.software/self"), found("ocm.software/orphan"))
	child := func(n string) ChildRef { return ChildRef{ReferenceName: n, ChildName: n, ChildVersion: "1.0.0"} }
	setOverview(t, w, "ocm.software/cli", "1.0.0", loc, Overview{Provider: "ocm.software", Signed: true,
		Children: []ChildRef{child("ocm.software/plugins/ecr"), child("helm.sh/helm"), child("ocm.software/other-provider-child")}})
	setOverview(t, w, "ocm.software/plugins/ecr", "1.0.0", loc, Overview{Provider: "ocm.software"})
	setOverview(t, w, "helm.sh/helm", "1.0.0", loc, Overview{Provider: "ocm.software"})
	setOverview(t, w, "ocm.software/other-provider-child", "1.0.0", loc, Overview{Provider: "someone-else"})
	setOverview(t, w, "ocm.software/self", "1.0.0", loc, Overview{Provider: "ocm.software", Children: []ChildRef{child("ocm.software/self")}})
	setOverview(t, w, "ocm.software/orphan", "1.0.0", loc, Overview{Provider: "ocm.software"})
	if err := w.UpdateFamilies(ctx, prefixFirstSegment); err != nil {
		t.Fatal(err)
	}
	got := queryStrings(t, w, `SELECT name FROM components WHERE NOT top_level ORDER BY name`)
	if want := []string{"ocm.software/plugins/ecr"}; !slices.Equal(got, want) {
		t.Fatalf("not top level = %v, want %v", got, want)
	}
	if got := queryStrings(t, w, `SELECT name FROM components WHERE signed`); !slices.Equal(got, []string{"ocm.software/cli"}) {
		t.Fatalf("signed = %v", got)
	}
	// a new overview replaces the references; a failed one clears them
	setOverview(t, w, "ocm.software/cli", "1.0.0", loc, Overview{Provider: "ocm.software", Children: []ChildRef{child("helm.sh/helm")}})
	if got := queryStrings(t, w, `SELECT child_name FROM component_references WHERE parent_name = 'ocm.software/cli'`); !slices.Equal(got, []string{"helm.sh/helm"}) {
		t.Fatalf("references after replace = %v", got)
	}
	if err := w.MarkOverviewFailed(ctx, "ocm.software/cli"); err != nil {
		t.Fatal(err)
	}
	if got := queryStrings(t, w, `SELECT child_name FROM component_references WHERE parent_name = 'ocm.software/cli'`); len(got) != 0 {
		t.Fatalf("references after failure = %v", got)
	}
	// removing the parent component removes its references
	setOverview(t, w, "ocm.software/cli", "1.0.0", loc, Overview{Provider: "ocm.software", Children: []ChildRef{child("helm.sh/helm")}})
	runSync(t, w, "c", 0, true, day(11), found("helm.sh/helm"))
	if got := queryStrings(t, w, `SELECT parent_name FROM component_references`); len(got) != 0 {
		t.Fatalf("references of deleted parent = %v", got)
	}
}

func TestDefaultVersionFallsBackToNewestPrerelease(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	const loc = "oci::ghcr.io/acme"
	runSync(t, w, "c", 0, true, day(10),
		Found{Component: "acme.org/pre", Repository: loc, Versions: []FoundVersion{pre("0.1.0-rc.1", 1), pre("0.2.0-rc.1", 2)}},
		Found{Component: "acme.org/mixed", Repository: loc, Versions: []FoundVersion{v("1.0.0", 1), pre("2.0.0-rc.1", 3)}})
	got := queryStrings(t, w, `SELECT name || '=' || default_version || '/' || coalesce(latest_version, '-') FROM components ORDER BY name`)
	if want := []string{"acme.org/mixed=1.0.0/1.0.0", "acme.org/pre=0.2.0-rc.1/-"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	targets, err := w.PendingOverviews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var tv []string
	for _, x := range targets {
		tv = append(tv, x.Component+":"+x.Version)
	}
	if want := []string{"acme.org/mixed:1.0.0", "acme.org/pre:0.2.0-rc.1"}; !slices.Equal(tv, want) {
		t.Fatalf("pending = %v, want %v", tv, want)
	}
	// a new pre-release changes the default version and makes the overview pending again
	setOverview(t, w, "acme.org/pre", "0.2.0-rc.1", loc, Overview{Title: "Pre"})
	runSync(t, w, "c", 0, true, day(11),
		Found{Component: "acme.org/pre", Repository: loc, Versions: []FoundVersion{pre("0.1.0-rc.1", 1), pre("0.2.0-rc.1", 2), pre("0.3.0-rc.1", 4)}},
		Found{Component: "acme.org/mixed", Repository: loc, Versions: []FoundVersion{v("1.0.0", 1), pre("2.0.0-rc.1", 3)}})
	if got := queryStrings(t, w, `SELECT default_version FROM components WHERE name = 'acme.org/pre' AND overview_fetched_at IS NULL`); !slices.Equal(got, []string{"0.3.0-rc.1"}) {
		t.Fatalf("after new pre-release: %v", got)
	}
}

func TestBeginConnectorStoresSource(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	src := Source{Key: "github:org:acme", Title: "Acme", AvatarURL: "https://avatars.example/a"}
	tx, err := w.BeginConnector(ctx, "c1", "test/v1", src, 0, day(1))
	must(t, err)
	must(t, tx.Commit(ctx, true))
	s, ok, err := w.ConnectorState(ctx, "c1")
	if err != nil || !ok || !reflect.DeepEqual(s.Source, src) {
		t.Fatalf("state = %+v ok=%v err=%v", s, ok, err)
	}
	// a later run without avatar clears it (sync resolves fallbacks before calling)
	tx, err = w.BeginConnector(ctx, "c1", "test/v1", Source{Key: "github:org:acme", Title: "Acme 2"}, 0, day(2))
	must(t, err)
	must(t, tx.Commit(ctx, false))
	s, _, _ = w.ConnectorState(ctx, "c1")
	if s.Source.Title != "Acme 2" || s.Source.AvatarURL != "" {
		t.Fatalf("state = %+v", s)
	}
}

func TestRecordErrorInsertsSource(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	src := Source{Key: "github:org:acme", Title: "acme"}
	must(t, w.RecordError(ctx, "c1", "test/v1", src, 0, errors.New("boom"))) // first run failed: no row yet
	s, ok, err := w.ConnectorState(ctx, "c1")
	if err != nil || !ok || !reflect.DeepEqual(s.Source, src) || s.LastError != "boom" {
		t.Fatalf("state = %+v ok=%v err=%v", s, ok, err)
	}
	// an existing row keeps its stored source
	must(t, w.RecordError(ctx, "c1", "test/v1", Source{Key: "github:org:acme", Title: "other"}, 0, errors.New("again")))
	s, _, _ = w.ConnectorState(ctx, "c1")
	if s.Source.Title != "acme" || s.LastError != "again" {
		t.Fatalf("state = %+v", s)
	}
}

func TestApplyStoresOwnerAndVisibility(t *testing.T) {
	w, _ := newWriter(t)
	runSync(t, w, "c1", 0, true, day(1), Found{Component: "acme.org/x", Repository: locA, Owner: "github:org:acme", Visibility: "internal", Versions: []FoundVersion{v("1.0.0", 1)}})
	if got := queryStrings(t, w, "SELECT owner_key || ' ' || visibility FROM discoveries"); !slices.Equal(got, []string{"github:org:acme internal"}) {
		t.Fatalf("discoveries = %v", got)
	}
}

func TestOwnerParentRule(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	begin := func(id string, src Source) {
		tx, err := w.BeginConnector(ctx, id, "github/v1", src, 0, day(1))
		must(t, err)
		must(t, tx.Commit(ctx, true))
	}
	ent := Source{Key: "github:enterprise:acme-enterprise", Title: "Acme Enterprise", Scope: "s1", Owners: []Owner{
		{Key: "github:org:acme-corp", Title: "Acme Corporation", Parent: "github:enterprise:acme-enterprise"},
		{Key: "github:org:acme-labs", Title: "Acme Labs", Parent: "github:enterprise:acme-enterprise"}}}
	begin("ent", ent)
	// an org entry for acme-corp must not clear its parent
	begin("org", Source{Key: "github:org:acme-corp", Title: "Corp (configured)", Owners: []Owner{{Key: "github:org:acme-corp", Title: "Corp (configured)"}}})
	if got := queryStrings(t, w, "SELECT key || '>' || coalesce(parent_key, '') FROM owners ORDER BY key"); !slices.Equal(got, []string{
		"github:org:acme-corp>github:enterprise:acme-enterprise", "github:org:acme-labs>github:enterprise:acme-enterprise"}) {
		t.Fatalf("owners = %v", got)
	}
	// acme-labs left the enterprise: its parent is cleared
	ent.Owners, ent.Scope = ent.Owners[:1], "s2"
	begin("ent", ent)
	if got := queryStrings(t, w, "SELECT key || '>' || coalesce(parent_key, '') FROM owners ORDER BY key"); !slices.Equal(got, []string{
		"github:org:acme-corp>github:enterprise:acme-enterprise", "github:org:acme-labs>"}) {
		t.Fatalf("owners after leave = %v", got)
	}
	s, _, _ := w.ConnectorState(ctx, "ent")
	if s.Source.Scope != "s2" {
		t.Fatalf("scope = %q", s.Source.Scope)
	}
	// unknown owners (nil) and scope ("") keep the stored values
	begin("ent", Source{Key: ent.Key, Title: ent.Title})
	s, _, _ = w.ConnectorState(ctx, "ent")
	if s.Source.Scope != "s2" || len(queryStrings(t, w, "SELECT key FROM owners WHERE parent_key IS NOT NULL")) != 1 {
		t.Fatalf("unknown describe changed stored data: %+v", s)
	}
}

func TestApplyCreatesMissingOwnerRow(t *testing.T) {
	w, _ := newWriter(t)
	runSync(t, w, "c1", 0, true, day(1), Found{Component: "acme.org/x", Repository: locA, Owner: "github:org:acme", Visibility: "public", Versions: []FoundVersion{v("1.0.0", 1)}})
	if got := queryStrings(t, w, "SELECT title FROM owners WHERE key = 'github:org:acme'"); !slices.Equal(got, []string{"acme"}) {
		t.Fatalf("owners = %v", got)
	}
}

func TestPageCacheRolledBackWithRun(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	tx, err := w.BeginConnector(ctx, "c1", "test/v1", Source{Key: "c1", Title: "c1"}, 0, day(1))
	must(t, err)
	tx.Store("https://api.example/p1", `W/"e1"`, "https://api.example/p2")
	if etag, next, ok := tx.Lookup("https://api.example/p1"); !ok || etag != `W/"e1"` || next != "https://api.example/p2" {
		t.Fatalf("lookup in tx = %q %q %v", etag, next, ok)
	}
	must(t, tx.Rollback())
	tx, err = w.BeginConnector(ctx, "c1", "test/v1", Source{Key: "c1", Title: "c1"}, 0, day(2))
	must(t, err)
	if _, _, ok := tx.Lookup("https://api.example/p1"); ok {
		t.Fatal("ETag of a rolled-back run survived")
	}
	tx.Store("https://api.example/p1", `W/"e2"`, "")
	must(t, tx.Commit(ctx, true))
	tx, err = w.BeginConnector(ctx, "c1", "test/v1", Source{Key: "c1", Title: "c1"}, 0, day(3))
	must(t, err)
	if etag, next, ok := tx.Lookup("https://api.example/p1"); !ok || etag != `W/"e2"` || next != "" {
		t.Fatalf("committed lookup = %q %q %v", etag, next, ok)
	}
	must(t, tx.ResetPageCache(ctx))
	if _, _, ok := tx.Lookup("https://api.example/p1"); ok {
		t.Fatal("ResetPageCache kept rows")
	}
	must(t, tx.Commit(ctx, true))
}

func TestPruneAndCleanupOwnersAndCache(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	tx, err := w.BeginConnector(ctx, "c1", "test/v1", Source{Key: "c1", Title: "c1", Owners: []Owner{{Key: "github:org:gone", Title: "Gone"}}}, 0, day(1))
	must(t, err)
	tx.Store("https://api.example/p1", `W/"e1"`, "")
	must(t, tx.Commit(ctx, true))
	must(t, w.PruneConnectors(ctx, nil))
	must(t, w.Cleanup(ctx))
	if n := len(queryStrings(t, w, "SELECT url FROM http_cache")); n != 0 {
		t.Fatalf("http_cache rows left: %d", n)
	}
	if n := len(queryStrings(t, w, "SELECT key FROM owners")); n != 0 {
		t.Fatalf("unreferenced owners left: %d", n)
	}
}

func TestApplyAndOwnersRejectEmptyKeys(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	tx, err := w.BeginConnector(ctx, "c1", "test/v1", Source{Key: "c1", Title: "c1"}, 0, day(1))
	must(t, err)
	if err := tx.Apply(ctx, Found{Component: "acme.org/x", Repository: locA, Visibility: "public"}); err == nil {
		t.Fatal("Apply accepted an empty owner")
	}
	must(t, tx.Rollback())
	tx, err = w.BeginConnector(ctx, "c1", "test/v1", Source{Key: "c1", Title: "c1"}, 0, day(1))
	must(t, err)
	if err := tx.Apply(ctx, Found{Component: "acme.org/x", Repository: locA, Owner: "test:c1"}); err == nil {
		t.Fatal("Apply accepted an empty visibility")
	}
	must(t, tx.Rollback())
	if _, err := w.BeginConnector(ctx, "c2", "test/v1", Source{Key: "c2", Title: "c2", Owners: []Owner{{Title: "x"}}}, 0, day(1)); err == nil {
		t.Fatal("BeginConnector accepted an owner without key")
	}
}

func TestCleanupClearsParentOfRemovedEnterprise(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	const ent = "github:enterprise:acme-enterprise"
	tx, err := w.BeginConnector(ctx, "ent", "github/v1", Source{Key: ent, Title: "E", Scope: "s", Owners: []Owner{{Key: "github:org:acme-corp", Title: "Corp", Parent: ent}}}, 0, day(1))
	must(t, err)
	must(t, tx.Commit(ctx, true))
	runSync(t, w, "org", 1, true, day(1), Found{Component: "acme.org/x", Repository: locA, Owner: "github:org:acme-corp", Visibility: "public", Versions: []FoundVersion{v("1.0.0", 1)}})
	must(t, w.PruneConnectors(ctx, []string{"org"}))
	must(t, w.Cleanup(ctx))
	if got := queryStrings(t, w, "SELECT key || '>' || coalesce(parent_key, '') FROM owners"); !slices.Equal(got, []string{"github:org:acme-corp>"}) {
		t.Fatalf("owners = %v", got)
	}
}

func TestOwnerAvatarKeptWhenMissing(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	begin := func(id string, src Source) {
		tx, err := w.BeginConnector(ctx, id, "github/v1", src, 0, day(1))
		must(t, err)
		must(t, tx.Commit(ctx, true))
	}
	const ent = "github:enterprise:acme-enterprise"
	begin("ent", Source{Key: ent, Title: "E", Scope: "s", Owners: []Owner{
		{Key: "github:org:acme-corp", Title: "Corp", AvatarURL: "https://avatars.example/corp", Parent: ent}}})
	begin("org", Source{Key: "github:org:acme-corp", Title: "Corp", Owners: []Owner{{Key: "github:org:acme-corp", Title: "Corp"}}})
	if got := queryStrings(t, w, "SELECT coalesce(avatar_url, '') FROM owners WHERE key = 'github:org:acme-corp'"); !slices.Equal(got, []string{"https://avatars.example/corp"}) {
		t.Fatalf("avatar = %v", got)
	}
}

func TestAttestationTargets(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	runSync(t, w, "c1", 0, true, day(5),
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1), v("1.1.0", 3)}},
		Found{Component: "acme.org/y", Repository: locA, Versions: []FoundVersion{v("2.0.0", 1)}})
	runSync(t, w, "c2", 1, true, day(5), Found{Component: "acme.org/x", Repository: locB, Versions: []FoundVersion{v("1.1.0", 4)}})
	setOverview(t, w, "acme.org/x", "1.1.0", locB, Overview{Title: "X"})
	// y has no overview location yet
	got, err := w.AttestationTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []AttestationTarget{{Component: "acme.org/x", Version: "1.1.0", Repository: locB}}
	if !slices.Equal(got, want) {
		t.Fatalf("targets = %+v", got)
	}
}

func TestCleanupDropsAttestationsOfRemovedComponents(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	runSync(t, w, "c1", 0, true, day(1),
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}},
		Found{Component: "acme.org/y", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}})
	runSync(t, w, "c2", 1, true, day(1), Found{Component: "acme.org/x", Repository: locB, Versions: []FoundVersion{v("1.0.0", 1)}})
	res := attestation.Result{Levels: []attestation.Level{{Status: attestation.StatusOK,
		Entries: []attestation.Entry{{Kind: attestation.KindSBOM, How: attestation.HowReferrer, Signed: attestation.SignedNo}}}}}
	for _, t0 := range []AttestationTarget{{"acme.org/x", "1.0.0", locA}, {"acme.org/x", "1.0.0", locB}, {"acme.org/y", "1.0.0", locA}} {
		if err := w.SetAttestations(ctx, t0, res, day(2)); err != nil {
			t.Fatal(err)
		}
	}
	count := func() []string {
		return queryStrings(t, w, `SELECT component || '@' || location_id FROM attestation_sets ORDER BY 1`)
	}
	if n := len(count()); n != 3 {
		t.Fatalf("sets = %v", count())
	}
	runSync(t, w, "c1", 0, true, day(2)) // y goes away, x stays at both locations through c2 only for locB
	if got := count(); len(got) != 1 || !strings.HasPrefix(got[0], "acme.org/x@") {
		t.Fatalf("sets after component removal = %v", got)
	}
	for _, table := range []string{"attestation_levels", "attestation_entries"} {
		if got := queryStrings(t, w, `SELECT component FROM `+table); len(got) != 1 {
			t.Fatalf("%s rows = %v", table, got)
		}
	}
	runSync(t, w, "c2", 1, true, day(3)) // locB goes away
	if got := count(); len(got) != 0 {
		t.Fatalf("sets after location removal = %v", got)
	}
}

func TestPendingChangelogs(t *testing.T) {
	w, _ := newWriter(t)
	ctx := context.Background()
	runSync(t, w, "c1", 0, true, day(30),
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1), v("1.1.0", 2), v("1.2.0", 3)}},
		Found{Component: "acme.org/x", Repository: locB, Versions: []FoundVersion{v("1.0.0", 9)}})
	must(t, w.SetOverview(ctx, OverviewTarget{"acme.org/x", "1.2.0", locB}, Overview{Title: "X"}))
	got, err := w.PendingChangelogs(ctx, 2, false)
	must(t, err)
	if want := []ChangelogTarget{{"acme.org/x", "1.2.0", locA}, {"acme.org/x", "1.1.0", locA}}; !slices.Equal(got, want) {
		t.Fatalf("pending = %+v, want %+v", got, want)
	}
	must(t, w.SetChangelog(ctx, "acme.org/x", "1.2.0", nil, ""))
	must(t, w.SetChangelog(ctx, "acme.org/x", "1.1.0", nil, "boom"))
	got, _ = w.PendingChangelogs(ctx, 10, false)
	if len(got) != 1 || got[0].Version != "1.0.0" {
		t.Fatalf("pending = %+v", got)
	}
	got, _ = w.PendingChangelogs(ctx, 10, true)
	if len(got) != 2 || got[0].Version != "1.1.0" {
		t.Fatalf("retry = %+v", got)
	}
}
