package indexsync

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/connector"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
	"chrisschneider.dev/delivery-bundler/internal/version"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"oras.land/oras-go/v2/registry/remote"
)

type fakeConn struct {
	id     string
	key    string // "" → "fake:"+id
	title  string
	found  []connector.ComponentVersions
	err    error
	sinces []time.Time

	onChanged func(connector.PageCache)
	lookups   []map[string]string
}

func (f *fakeConn) ID() string   { return f.id }
func (f *fakeConn) Type() string { return "fake/v1" }
func (f *fakeConn) Key() string {
	if f.key != "" {
		return f.key
	}
	return "fake:" + f.id
}
func (f *fakeConn) Title() string { return f.title }

// describingConn adds an owner profile (or its error) to fakeConn.
type describingConn struct {
	*fakeConn
	src connector.Source
	err error
}

func (d describingConn) Describe(context.Context) (connector.Source, error) { return d.src, d.err }
func (f *fakeConn) Changed(_ context.Context, since time.Time, cache connector.PageCache) iter.Seq2[connector.ComponentVersions, error] {
	f.sinces = append(f.sinces, since)
	m := map[string]string{}
	if cache != nil {
		if etag, _, ok := cache.Lookup("https://api.example/p1"); ok {
			m["https://api.example/p1"] = etag
		}
	}
	f.lookups = append(f.lookups, m)
	if f.onChanged != nil {
		f.onChanged(cache)
	}
	return func(yield func(connector.ComponentVersions, error) bool) {
		for _, cv := range f.found {
			if !yield(cv, nil) {
				return
			}
		}
		if f.err != nil {
			yield(connector.ComponentVersions{}, f.err)
		}
	}
}

type fakeFetcher struct {
	fail  bool
	descs map[string]*descriptorruntime.Descriptor // by component name; nil: default
}

func (f fakeFetcher) DescriptorAt(_ context.Context, c, v, loc string) (*descriptorruntime.Descriptor, error) {
	if f.fail {
		return nil, errors.New("registry down")
	}
	if d, ok := f.descs[c]; ok {
		return d, nil
	}
	d := &descriptorruntime.Descriptor{}
	d.Component.Name, d.Component.Version = c, v
	d.Component.Provider.Name = "acme"
	d.Component.Labels = []descriptorruntime.Label{{Name: "catalog.chrisschneider.dev/title", Value: []byte(`"Nice ` + c + `"`)}}
	return d, nil
}

var day = func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }

func opts(t *testing.T, p string, conns ...connector.Connector) Options {
	cl, _ := version.NewClassifier(nil)
	return Options{DBPath: p, Connectors: conns, Classifier: cl, Fetcher: fakeFetcher{}, Now: func() time.Time { return day(28) }}
}

func storeComponent(t *testing.T, p, name string) index.ComponentDetail {
	t.Helper()
	h := index.NewHandle(p)
	defer h.Close()
	s, release, err := h.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	d, err := s.Component(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRunBuildsIndexAndSwaps(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data", "hub.duckdb")
	c := &fakeConn{id: "c1", found: []connector.ComponentVersions{{Component: "acme.org/x", Repository: "oci::ghcr.io/acme",
		Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}, {Name: "2.0.0-rc.1", PublishedAt: day(2)}}}}}
	rep, err := Run(context.Background(), opts(t, p, c))
	if err != nil || rep.Failed() || !rep.Full["c1"] {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	d := storeComponent(t, p, "acme.org/x")
	if d.Latest != "1.0.0" || d.Title != "Nice acme.org/x" || d.Provider != "acme" {
		t.Fatalf("detail = %+v", d)
	}
	for _, leftover := range []string{p + ".tmp", p + ".tmp.wal", p + ".wal"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s left behind", leftover)
		}
	}
	// second run is a delta from the last run minus overlap
	if _, err := Run(context.Background(), opts(t, p, c)); err != nil {
		t.Fatal(err)
	}
	if len(c.sinces) != 2 || !c.sinces[0].IsZero() || !c.sinces[1].Equal(day(28).Add(-time.Minute)) {
		t.Fatalf("sinces = %v", c.sinces)
	}
}

func TestRunConnectorErrorKeepsOthersAndOldData(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	good := &fakeConn{id: "good", found: []connector.ComponentVersions{{Component: "acme.org/g", Repository: "oci::ghcr.io/g",
		Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}}}}}
	bad := &fakeConn{id: "bad", found: []connector.ComponentVersions{{Component: "acme.org/b", Repository: "oci::ghcr.io/b",
		Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}}}}}
	if _, err := Run(context.Background(), opts(t, p, good, bad)); err != nil {
		t.Fatal(err)
	}
	bad.err = errors.New("api down")
	o := opts(t, p, good, bad)
	o.Full = true
	rep, err := Run(context.Background(), o)
	if err != nil || !rep.Failed() || rep.ConnectorErrors["bad"] == nil {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	storeComponent(t, p, "acme.org/b") // rolled back: still there
	storeComponent(t, p, "acme.org/g")
}

func TestRunOverviewFailureIsReportedAndRetried(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	c := &fakeConn{id: "c1", found: []connector.ComponentVersions{{Component: "acme.org/x", Repository: "oci::ghcr.io/acme",
		Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}}}}}
	o := opts(t, p, c)
	o.Fetcher = fakeFetcher{fail: true}
	rep, err := Run(context.Background(), o)
	if err != nil || rep.OverviewFailures != 1 {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	if d := storeComponent(t, p, "acme.org/x"); d.Title != "acme.org/x" {
		t.Fatalf("title = %q", d.Title)
	}
	c.found = nil // delta reports nothing new; overview must still be retried
	rep, _ = Run(context.Background(), opts(t, p, c))
	if rep.OverviewFailures != 0 || storeComponent(t, p, "acme.org/x").Title != "Nice acme.org/x" {
		t.Fatalf("retry failed: %+v", rep)
	}
}

func TestRunDiscardsMismatchedSchemaAndStaleTmp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hub.duckdb")
	if err := os.WriteFile(p+".tmp", []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+".tmp.wal", []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &fakeConn{id: "c1", found: []connector.ComponentVersions{{Component: "acme.org/x", Repository: "oci::ghcr.io/acme",
		Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}}}}}
	if _, err := Run(context.Background(), opts(t, p, c)); err != nil {
		t.Fatal(err)
	}
	w, err := index.OpenWriter(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.ForceSchemaVersion(999); err != nil { // test hook, see Step 3
		t.Fatal(err)
	}
	w.Close()
	rep, err := Run(context.Background(), opts(t, p, c))
	if err != nil || !rep.Full["c1"] {
		t.Fatalf("mismatch must force a full sync: rep=%+v err=%v", rep, err)
	}
	storeComponent(t, p, "acme.org/x")
}

func TestRunFatalErrorLeavesOldFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hub.duckdb")
	c := &fakeConn{id: "c1", found: []connector.ComponentVersions{{Component: "acme.org/x", Repository: "oci::ghcr.io/acme",
		Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}}}}}
	if _, err := Run(context.Background(), opts(t, p, c)); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(p)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // every step fails
	if _, err := Run(ctx, opts(t, p, c)); err == nil {
		t.Fatal("want error")
	}
	after, _ := os.Stat(p)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("old file was replaced")
	}
}

func TestRunPrunesRemovedConnectors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	mk := func(id, comp string) *fakeConn {
		return &fakeConn{id: id, found: []connector.ComponentVersions{{Component: comp, Repository: "oci::ghcr.io/" + id,
			Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}}}}}
	}
	a, b := mk("a", "acme.org/a"), mk("b", "acme.org/b")
	if _, err := Run(context.Background(), opts(t, p, a, b)); err != nil {
		t.Fatal(err)
	}
	storeComponent(t, p, "acme.org/b")
	if _, err := Run(context.Background(), opts(t, p, a)); err != nil { // delta: b was removed from the config
		t.Fatal(err)
	}
	storeComponent(t, p, "acme.org/a")
	h := index.NewHandle(p)
	defer h.Close()
	s, release, err := h.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := s.Component(context.Background(), "acme.org/b"); err == nil {
		t.Fatal("component of the removed connector is still indexed")
	}
}

func TestRunFailsFastWhenLocked(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	c := &fakeConn{id: "c1", found: []connector.ComponentVersions{{Component: "acme.org/x", Repository: "oci::ghcr.io/x",
		Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}}}}}
	if _, err := Run(context.Background(), opts(t, p, c)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(p+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), opts(t, p, c)); err == nil || !strings.Contains(err.Error(), "sync already running") {
		t.Fatalf("err = %v", err)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("hub.duckdb changed")
	}
	if _, err := os.Stat(p + ".tmp"); err == nil {
		t.Fatal("tmp created")
	}
}

func conn(names ...string) *fakeConn {
	c := &fakeConn{id: "c1"}
	for _, n := range names {
		c.found = append(c.found, connector.ComponentVersions{Component: n, Repository: "oci::ghcr.io/acme",
			Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}}})
	}
	return c
}

func listTopLevel(t *testing.T, p string) []string {
	t.Helper()
	h := index.NewHandle(p)
	defer h.Close()
	s, release, err := h.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	r, err := s.ListComponents(context.Background(), index.ListQuery{TopLevel: true, Sort: "name"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range r.Components {
		names = append(names, c.Name)
	}
	return names
}

func TestRunStoresSignedReferencesAndTopLevel(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	parent := &descriptorruntime.Descriptor{Signatures: []descriptorruntime.Signature{{Name: "default"}}}
	parent.Component.Name, parent.Component.Version = "github.com/acme/app", "1.0.0"
	parent.Component.Provider.Name = "acme"
	parent.Component.References = []descriptorruntime.Reference{{
		ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: "lib", Version: "1.0.0"}},
		Component:   "github.com/acme/lib"}}
	o := opts(t, p, conn("github.com/acme/app", "github.com/acme/lib", "github.com/other/lib"))
	o.Fetcher = fakeFetcher{descs: map[string]*descriptorruntime.Descriptor{"github.com/acme/app": parent}}
	if rep, err := Run(context.Background(), o); err != nil || rep.Failed() {
		t.Fatalf("%+v %v", rep, err)
	}
	app := storeComponent(t, p, "github.com/acme/app")
	if !app.Signed {
		t.Fatal("app should be signed")
	}
	lib := storeComponent(t, p, "github.com/acme/lib")
	if len(lib.ReferencedBy) != 1 || lib.ReferencedBy[0].Name != "github.com/acme/app" {
		t.Fatalf("lib referenced by = %+v", lib.ReferencedBy)
	}
	top := listTopLevel(t, p)
	if want := []string{"github.com/acme/app", "github.com/other/lib"}; !slices.Equal(top, want) {
		t.Fatalf("top level = %v, want %v", top, want)
	}
}

func connectorSource(t *testing.T, p, id string) index.Source {
	t.Helper()
	w, err := index.OpenWriter(p)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s, ok, err := w.ConnectorState(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("state of %s: ok=%v err=%v", id, ok, err)
	}
	return s.Source
}

func oneComponent(id string) []connector.ComponentVersions {
	return []connector.ComponentVersions{{Component: "acme.org/" + id, Repository: "oci::ghcr.io/" + id,
		Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}}}}
}

func TestRunStoresDescribedSource(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	c := describingConn{fakeConn: &fakeConn{id: "a", key: "github:org:acme", found: oneComponent("a")},
		src: connector.Source{Title: "Acme Inc", AvatarURL: "https://avatars.example/a"}}
	if _, err := Run(context.Background(), opts(t, p, c)); err != nil {
		t.Fatal(err)
	}
	if got := connectorSource(t, p, "a"); !reflect.DeepEqual(got, index.Source{Key: "github:org:acme", Title: "Acme Inc", AvatarURL: "https://avatars.example/a"}) {
		t.Fatalf("source = %+v", got)
	}
}

func TestRunSourceTitleFallbacks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	fc := &fakeConn{id: "a", key: "github:org:acme", found: oneComponent("a")}
	// no display name: last key segment
	if _, err := Run(context.Background(), opts(t, p, describingConn{fakeConn: fc, src: connector.Source{AvatarURL: "https://avatars.example/a"}})); err != nil {
		t.Fatal(err)
	}
	if got := connectorSource(t, p, "a"); got.Title != "acme" || got.AvatarURL != "https://avatars.example/a" {
		t.Fatalf("source = %+v", got)
	}
	// Describe fails: previous values are kept, the run does not fail
	rep, err := Run(context.Background(), opts(t, p, describingConn{fakeConn: fc, err: errors.New("rate limited")}))
	if err != nil || rep.Failed() {
		t.Fatalf("rep = %+v err = %v", rep, err)
	}
	if got := connectorSource(t, p, "a"); got.Title != "acme" || got.AvatarURL != "https://avatars.example/a" {
		t.Fatalf("source after failed describe = %+v", got)
	}
	// name: override wins over everything
	fc.title = "Configured"
	if _, err := Run(context.Background(), opts(t, p, describingConn{fakeConn: fc, src: connector.Source{Title: "Acme Inc"}})); err != nil {
		t.Fatal(err)
	}
	if got := connectorSource(t, p, "a"); got.Title != "Configured" {
		t.Fatalf("source = %+v", got)
	}
}

func TestRunEditedEntryKeepsKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	if _, err := Run(context.Background(), opts(t, p, &fakeConn{id: "old", key: "github:org:acme", found: oneComponent("a")})); err != nil {
		t.Fatal(err)
	}
	// the entry was edited: new ID, same key (the old row must be pruned before the new one is written)
	rep, err := Run(context.Background(), opts(t, p, &fakeConn{id: "new", key: "github:org:acme", found: oneComponent("a")}))
	if err != nil || rep.Failed() {
		t.Fatalf("rep = %+v err = %v", rep, err)
	}
	storeComponent(t, p, "acme.org/a")
}

func TestRunScopeChangeForcesFull(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	fc := &fakeConn{id: "e", key: "github:enterprise:acme", found: oneComponent("a")}
	run := func(scope string) {
		t.Helper()
		c := describingConn{fakeConn: fc, src: connector.Source{Title: "Acme", Scope: scope}}
		if _, err := Run(context.Background(), opts(t, p, c)); err != nil {
			t.Fatal(err)
		}
	}
	run("s1") // first run: full
	run("s1") // same scope: delta
	run("s2") // scope changed: full
	if len(fc.sinces) != 3 || !fc.sinces[0].IsZero() || fc.sinces[1].IsZero() || !fc.sinces[2].IsZero() {
		t.Fatalf("sinces = %v", fc.sinces)
	}
}

func TestRunUnknownScopeDoesNotForceFull(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	fc := &fakeConn{id: "e", key: "github:enterprise:acme", found: oneComponent("a")}
	if _, err := Run(context.Background(), opts(t, p, describingConn{fakeConn: fc, src: connector.Source{Scope: "s1"}})); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), opts(t, p, describingConn{fakeConn: fc, err: errors.New("rate limited")})); err != nil {
		t.Fatal(err)
	}
	if fc.sinces[1].IsZero() {
		t.Fatal("failed Describe forced a full run")
	}
}

func TestRunFullIgnoresCacheButStoresETags(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	fc := &fakeConn{id: "a", found: oneComponent("a")}
	fc.onChanged = func(cache connector.PageCache) { cache.Store("https://api.example/p1", `W/"e1"`, "") }
	if _, err := Run(context.Background(), opts(t, p, fc)); err != nil { // first run: full
		t.Fatal(err)
	}
	fc.onChanged = nil
	if _, err := Run(context.Background(), opts(t, p, fc)); err != nil { // delta
		t.Fatal(err)
	}
	if len(fc.lookups[0]) != 0 {
		t.Fatalf("full run's cache answered a lookup: %v", fc.lookups[0])
	}
	if etag, ok := fc.lookups[1]["https://api.example/p1"]; !ok || etag != `W/"e1"` {
		t.Fatalf("delta lookup = %q %v (the full run must store ETags)", etag, ok)
	}
}

func TestRunWarnsAboutOverlap(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	var buf bytes.Buffer
	o := opts(t, p,
		describingConn{fakeConn: &fakeConn{id: "e", key: "github:enterprise:acme"}, src: connector.Source{Scope: "s", Visibility: []connector.Visibility{connector.Public},
			Owners: []connector.OwnerInfo{{Key: "github:org:acme-corp", Parent: "github:enterprise:acme"}, {Key: "github:org:acme-labs", Parent: "github:enterprise:acme"}}}},
		describingConn{fakeConn: &fakeConn{id: "o", key: "github:org:acme-corp"}, src: connector.Source{Owners: []connector.OwnerInfo{{Key: "github:org:acme-corp"}}}},
		describingConn{fakeConn: &fakeConn{id: "u", key: "github:user:jane"}, src: connector.Source{Owners: []connector.OwnerInfo{{Key: "github:user:jane"}}}})
	o.Logger = slog.New(slog.NewTextHandler(&buf, nil))
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "listed by several sources") != 1 || !strings.Contains(out, "github:org:acme-corp") ||
		!strings.Contains(out, "github:enterprise:acme") || !strings.Contains(out, "with different visibility") || strings.Contains(out, "github:user:jane") {
		t.Fatalf("log = %s", out)
	}
}

// fakeGatherer returns a fixed result and counts calls.
type fakeGatherer struct {
	mu     sync.Mutex
	calls  int
	result attestation.Result
}

func (g *fakeGatherer) Gather(context.Context, *descriptorruntime.Descriptor, string) (attestation.Result, []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	return g.result, nil
}

// countingFetcher counts DescriptorAt calls.
type countingFetcher struct {
	fakeFetcher
	n *atomic.Int32
}

func (f countingFetcher) DescriptorAt(ctx context.Context, c, v, loc string) (*descriptorruntime.Descriptor, error) {
	f.n.Add(1)
	return f.fakeFetcher.DescriptorAt(ctx, c, v, loc)
}

var okResult = attestation.Result{Levels: []attestation.Level{{Status: attestation.StatusOK, Entries: []attestation.Entry{
	{Kind: "sbom", How: "referrer", Signed: "unsigned"}}}}}

func storedSet(t *testing.T, p string) (index.AttestationSet, bool) {
	t.Helper()
	h := index.NewHandle(p)
	defer h.Close()
	s, release, err := h.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	set, ok, err := s.Attestations(context.Background(), "acme.org/x", "1.0.0", "oci::ghcr.io/acme")
	if err != nil {
		t.Fatal(err)
	}
	return set, ok
}

func overviewFetchedAt(t *testing.T, p string) time.Time {
	t.Helper()
	db, err := sql.Open("duckdb", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var at time.Time
	if err := db.QueryRow(`SELECT overview_fetched_at FROM components WHERE name = 'acme.org/x'`).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

func TestRunGathersAttestationsWithOverview(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	o := opts(t, p, conn("acme.org/x"))
	g := &fakeGatherer{result: okResult}
	o.Attestations = g
	if rep, err := Run(context.Background(), o); err != nil || rep.Failed() || rep.AttestationFailures != 0 {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	set, ok := storedSet(t, p)
	if !ok || len(set.Result.Levels) != 1 || len(set.Result.Levels[0].Entries) != 1 || !set.CheckedAt.Equal(day(28)) {
		t.Fatalf("set = %+v ok=%v", set, ok)
	}
}

func TestRunFullRegathersAttestationsOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	var n atomic.Int32
	g := &fakeGatherer{result: okResult}
	o := opts(t, p, conn("acme.org/x"))
	o.Attestations, o.Fetcher = g, countingFetcher{n: &n}
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	fetchedAt := overviewFetchedAt(t, p)
	fetches := n.Load()

	o.Full = true
	o.Now = func() time.Time { return day(29) }
	o.Connectors = []connector.Connector{conn("acme.org/x")} // same default version again
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if g.calls != 2 || n.Load() != fetches+1 {
		t.Fatalf("gather calls=%d fetches=%d (before %d), want 2 and one more fetch", g.calls, n.Load(), fetches)
	}
	if got := overviewFetchedAt(t, p); !got.Equal(fetchedAt) {
		t.Fatalf("overview_fetched_at changed: %v -> %v", fetchedAt, got)
	}
	if set, ok := storedSet(t, p); !ok || !set.CheckedAt.Equal(day(29)) {
		t.Fatalf("set = %+v ok=%v", set, ok)
	}
}

func TestRunDeltaDoesNotRegather(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	g := &fakeGatherer{result: okResult}
	o := opts(t, p, conn("acme.org/x"))
	o.Attestations = g
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	o.Connectors = []connector.Connector{conn()}
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if g.calls != 1 {
		t.Fatalf("gather calls = %d, want 1", g.calls)
	}
}

func TestRunAttestationFailuresDoNotFail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	o := opts(t, p, conn("acme.org/x"))
	o.Attestations = &fakeGatherer{result: attestation.Result{Levels: []attestation.Level{
		{Status: attestation.StatusOK, Entries: []attestation.Entry{}},
		{Resource: &attestation.Identity{Name: "img", Version: "1"}, Status: attestation.StatusFailed, Error: "401 unauthorized"},
	}}}
	rep, err := Run(context.Background(), o)
	if err != nil || rep.AttestationFailures != 1 || rep.Failed() {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	if set, ok := storedSet(t, p); !ok || set.Result.Levels[1].Status != attestation.StatusFailed {
		t.Fatalf("set = %+v ok=%v", set, ok)
	}
}

// TestRunKeepsOlderVersionSet: a new default version is gathered, and the
// set of the previous default stays: sets are kept per version and location.
func TestRunKeepsOlderVersionSet(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	g := &fakeGatherer{result: okResult}
	o := opts(t, p, conn("acme.org/x"))
	o.Attestations = g
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	c := &fakeConn{id: "c1", found: []connector.ComponentVersions{{Component: "acme.org/x", Repository: "oci::ghcr.io/acme",
		Versions: []connector.Version{{Name: "1.1.0", PublishedAt: day(2)}}}}}
	o.Connectors = []connector.Connector{c}
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if g.calls != 2 {
		t.Fatalf("gather calls = %d, want 2", g.calls)
	}
	h := index.NewHandle(p)
	defer h.Close()
	s, release, err := h.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, v := range []string{"1.0.0", "1.1.0"} {
		if _, ok, err := s.Attestations(context.Background(), "acme.org/x", v, "oci::ghcr.io/acme"); err != nil || !ok {
			t.Errorf("set of %s: ok=%v err=%v", v, ok, err)
		}
	}
	col, err := s.CollectedAttestations(context.Background(), "acme.org/x")
	if err != nil || len(col) != 2 || col[0].Version != "1.1.0" {
		t.Errorf("collected = %+v err=%v", col, err)
	}
}

func TestRunWithoutGatherer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	o := opts(t, p, conn("acme.org/x"))
	o.Full = true
	if rep, err := Run(context.Background(), o); err != nil || rep.Failed() {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	if _, ok := storedSet(t, p); ok {
		t.Fatal("set stored without a gatherer")
	}
}

func plainOpener(_ context.Context, repository string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(repository)
	if err != nil {
		return nil, err
	}
	repo.PlainHTTP = true
	return repo, nil
}

func imageResource(name, typ, ref string, extra map[string]string) descriptorruntime.Resource {
	return descriptorruntime.Resource{
		ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: name, Version: "1.0.0"}, ExtraIdentity: extra},
		Type:        typ,
		Access:      &ocmruntime.Raw{Type: ocmruntime.NewUnversionedType("OCIImage"), Data: []byte(`{"type":"OCIImage/v1","imageReference":"` + ref + `"}`)},
	}
}

func storedArchitectures(t *testing.T, p, name string) []string {
	t.Helper()
	db, err := sql.Open("duckdb", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw sql.NullString
	if err := db.QueryRow(`SELECT to_json(architectures)::VARCHAR FROM components WHERE name = ?`, name).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var out []string
	if raw.Valid {
		if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestSyncStoresArchitectures(t *testing.T) {
	reg := zot.Shared(t)
	fixtures.PushBuildxIndex(t.Context(), t, fixtures.Target{Host: reg.Host}, "sync/archs", "v1",
		[]string{"linux/amd64", "linux/arm64"}, []string{"https://spdx.dev/Document"})
	fixtures.PushImageConfig(t.Context(), t, fixtures.Target{Host: reg.Host}, "sync/archs", "noplat", []byte(`{}`))
	d := &descriptorruntime.Descriptor{}
	d.Component.Name, d.Component.Version = "acme.org/x", "1.0.0"
	d.Component.Resources = []descriptorruntime.Resource{
		imageResource("img", "ociImage", reg.Host+"/sync/archs:v1", nil),
		imageResource("plain", "ociImage", reg.Host+"/sync/archs:noplat", nil), // platform "": not an architecture
		imageResource("gone", "ociArtifact", "127.0.0.1:1/none:v1", nil), // unreachable: ignored
		imageResource("cli", "executable", reg.Host+"/sync/archs:v1", map[string]string{"os": "linux", "architecture": "s390x"}),
	}
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	o := opts(t, p, conn("acme.org/x", "acme.org/y"))
	o.Fetcher = fakeFetcher{descs: map[string]*descriptorruntime.Descriptor{"acme.org/x": d}}
	o.Registry = plainOpener
	if rep, err := Run(t.Context(), o); err != nil || rep.Failed() {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	if got, want := storedArchitectures(t, p, "acme.org/x"), []string{"linux/amd64", "linux/arm64", "linux/s390x"}; !slices.Equal(got, want) {
		t.Fatalf("architectures = %v, want %v", got, want)
	}
	if got := storedArchitectures(t, p, "acme.org/y"); len(got) != 0 {
		t.Fatalf("y architectures = %v", got)
	}
}

// versionFetcher serves descriptors by "component@version" and counts reads.
type versionFetcher struct {
	descs map[string]*descriptorruntime.Descriptor
	n     *atomic.Int32
}

func (f versionFetcher) DescriptorAt(_ context.Context, c, v, _ string) (*descriptorruntime.Descriptor, error) {
	f.n.Add(1)
	if d, ok := f.descs[c+"@"+v]; ok {
		return d, nil
	}
	d := &descriptorruntime.Descriptor{}
	d.Component.Name, d.Component.Version = c, v
	return d, nil
}

// storedChangelogs returns version → has_changelog (nil: NULL) of the side table.
func storedChangelogs(t *testing.T, p string) map[string]*bool {
	t.Helper()
	db, err := sql.Open("duckdb", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT version, has_changelog FROM version_changelogs`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]*bool{}
	for rows.Next() {
		var v string
		var has sql.NullBool
		if err := rows.Scan(&v, &has); err != nil {
			t.Fatal(err)
		}
		if has.Valid {
			out[v] = &has.Bool
		} else {
			out[v] = nil
		}
	}
	return out
}

func TestSyncChangelogFlags(t *testing.T) {
	desc := func(v string, withChangelog, withRef bool) *descriptorruntime.Descriptor {
		d := &descriptorruntime.Descriptor{}
		d.Component.Name, d.Component.Version = "acme.org/x", v
		if withChangelog {
			d.Component.Resources = []descriptorruntime.Resource{{ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: "notes", Version: v}}, Type: "changelog"}}
		}
		if withRef {
			d.Component.References = []descriptorruntime.Reference{{ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: "lib", Version: "2.0.0"}}, Component: "acme.org/lib"}}
		}
		return d
	}
	c := &fakeConn{id: "c1", found: []connector.ComponentVersions{{Component: "acme.org/x", Repository: "oci::ghcr.io/acme",
		Versions: []connector.Version{{Name: "1.0.0", PublishedAt: day(1)}, {Name: "1.1.0", PublishedAt: day(2)}, {Name: "1.2.0", PublishedAt: day(3)}}}}}
	var n atomic.Int32
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	o := opts(t, p, c)
	o.Fetcher = versionFetcher{n: &n, descs: map[string]*descriptorruntime.Descriptor{
		"acme.org/x@1.0.0": desc("1.0.0", true, false),
		"acme.org/x@1.1.0": desc("1.1.0", false, false),
		"acme.org/x@1.2.0": desc("1.2.0", false, true),
	}}
	if rep, err := Run(t.Context(), o); err != nil || rep.Failed() {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	got := storedChangelogs(t, p)
	yes, no := true, false
	want := map[string]*bool{"1.0.0": &yes, "1.1.0": &no, "1.2.0": nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changelogs = %v, want %v", got, want)
	}
	n.Store(0)
	if rep, err := Run(t.Context(), o); err != nil || rep.Failed() {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	if n.Load() != 0 {
		t.Fatalf("second run read %d descriptors", n.Load())
	}
}

func TestSyncChangelogBudget(t *testing.T) {
	cv := connector.ComponentVersions{Component: "acme.org/x", Repository: "oci::ghcr.io/acme"}
	for i := range 250 {
		cv.Versions = append(cv.Versions, connector.Version{Name: fmt.Sprintf("1.0.%d", i), PublishedAt: day(1).Add(time.Duration(i) * time.Hour)})
	}
	var n atomic.Int32
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	o := opts(t, p, &fakeConn{id: "c1", found: []connector.ComponentVersions{cv}})
	o.Fetcher = versionFetcher{n: &n}
	if rep, err := Run(t.Context(), o); err != nil || rep.Failed() {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	got := storedChangelogs(t, p)
	if len(got) != 200 {
		t.Fatalf("rows = %d, want 200", len(got))
	}
	for i := range 250 {
		_, ok := got[fmt.Sprintf("1.0.%d", i)]
		if want := i >= 50; ok != want {
			t.Fatalf("version 1.0.%d stored = %v, want %v", i, ok, want)
		}
	}
}
