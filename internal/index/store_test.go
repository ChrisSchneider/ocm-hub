package index

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/document"
	"github.com/Masterminds/semver/v3"
)

// seed: x at A+B, y at A only (pre-release only), z at B; overview for x and z.
func seededStore(t *testing.T) *Store {
	t.Helper()
	w, p := newWriter(t)
	ctx := context.Background()
	runSync(t, w, "c1", 0, true, day(30),
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1), v("1.1.0", 5), pre("2.0.0-rc.1", 6)}},
		Found{Component: "acme.org/y", Repository: locA, Versions: []FoundVersion{pre("0.1.0-alpha", 2)}},
		Found{Component: "acme.org/x", Repository: locB, Versions: []FoundVersion{v("1.0.0", 9)}},
		Found{Component: "other.org/z", Repository: locB, Versions: []FoundVersion{v("3.0.0", 3)}})
	must(t, w.SetOverview(ctx, OverviewTarget{"acme.org/x", "1.1.0", locA}, Overview{Title: "X Tool", Provider: "acme", Categories: []string{"tools", "cli"},
		Architectures: []string{"linux/amd64", "linux/arm64"}}))
	must(t, w.SetOverview(ctx, OverviewTarget{"other.org/z", "3.0.0", locB}, Overview{Title: "Zed", Provider: "other", Categories: []string{"tools"},
		Architectures: []string{"linux/amd64"}}))
	must(t, w.Close())
	h := NewHandle(p)
	t.Cleanup(func() { h.Close() })
	s, release, err := h.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return s
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func names(cs []ComponentSummary) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func TestListDefaultOrderAndFields(t *testing.T) {
	s := seededStore(t)
	r, err := s.ListComponents(context.Background(), ListQuery{})
	must(t, err)
	// y has no stable version → hidden; x latest 1.1.0 (day 5) before z (day 3)
	if got := names(r.Components); !slices.Equal(got, []string{"acme.org/x", "other.org/z"}) {
		t.Fatalf("names = %v", got)
	}
	x := r.Components[0]
	if x.Latest != "1.1.0" || !x.LatestPublishedAt.Equal(day(5)) || x.Title != "X Tool" || x.VersionCount != 3 ||
		!slices.Equal(x.Locations, []string{locA, locB}) || !slices.Equal(x.Categories, []string{"tools", "cli"}) {
		t.Fatalf("x = %+v", x)
	}
	if r.Next != nil {
		t.Fatalf("next = %+v", r.Next)
	}
}

func TestListFiltersAndFacets(t *testing.T) {
	s := seededStore(t)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		q    ListQuery
		want []string
	}{
		"q name":       {ListQuery{Q: "OTHER"}, []string{"other.org/z"}},
		"q title":      {ListQuery{Q: "x tool"}, []string{"acme.org/x"}},
		"location":     {ListQuery{Locations: []string{locB}}, []string{"acme.org/x", "other.org/z"}},
		"location A":   {ListQuery{Locations: []string{locA}}, []string{"acme.org/x"}},
		"provider":     {ListQuery{Provider: "other"}, []string{"other.org/z"}},
		"categories":   {ListQuery{Categories: []string{"tools", "cli"}}, []string{"acme.org/x"}},
		"prerelease":   {ListQuery{IncludePrerelease: true, Sort: "name"}, []string{"acme.org/x", "acme.org/y", "other.org/z"}},
		"since":        {ListQuery{UpdatedSince: day(4)}, []string{"acme.org/x"}},
		"sort by name": {ListQuery{Sort: "name"}, []string{"acme.org/x", "other.org/z"}},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := s.ListComponents(ctx, tc.q)
			must(t, err)
			if got := names(r.Components); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	// facets ignore their own dimension
	r, err := s.ListComponents(ctx, ListQuery{Provider: "other"})
	must(t, err)
	if !slices.Equal(r.Facets.Providers, []FacetValue{{"acme", 1}, {"other", 1}}) {
		t.Fatalf("providers = %+v", r.Facets.Providers)
	}
	if !slices.Equal(r.Facets.Categories, []FacetValue{{"tools", 1}}) {
		t.Fatalf("facets = %+v", r.Facets)
	}
}

func TestListArchitectureFilter(t *testing.T) {
	s := seededStore(t)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		archs []string
		want  []string
	}{
		"shared":  {[]string{"linux/amd64"}, []string{"acme.org/x", "other.org/z"}},
		"one":     {[]string{"linux/arm64"}, []string{"acme.org/x"}},
		"all":     {[]string{"linux/amd64", "linux/arm64"}, []string{"acme.org/x"}},
		"unknown": {[]string{"linux/amd64", "s390x"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := s.ListComponents(ctx, ListQuery{Architectures: tc.archs})
			must(t, err)
			if got := names(r.Components); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	// the facet ignores its own dimension but honours the others
	r, err := s.ListComponents(ctx, ListQuery{Architectures: []string{"linux/arm64"}})
	must(t, err)
	if want := []FacetValue{{"linux/amd64", 2}, {"linux/arm64", 1}}; !slices.Equal(r.Facets.Architectures, want) {
		t.Fatalf("architectures facet = %+v, want %+v", r.Facets.Architectures, want)
	}
	r, err = s.ListComponents(ctx, ListQuery{Provider: "other"})
	must(t, err)
	if want := []FacetValue{{"linux/amd64", 1}}; !slices.Equal(r.Facets.Architectures, want) {
		t.Fatalf("architectures facet (provider other) = %+v, want %+v", r.Facets.Architectures, want)
	}
}

func TestListReplicationAndTotal(t *testing.T) {
	s := seededStore(t)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		q    ListQuery
		want []string
	}{
		"single":     {ListQuery{Replication: "single", IncludePrerelease: true, Sort: "name"}, []string{"acme.org/y", "other.org/z"}},
		"replicated": {ListQuery{Replication: "replicated"}, []string{"acme.org/x"}},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := s.ListComponents(ctx, tc.q)
			must(t, err)
			if got := names(r.Components); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	// the facet ignores its own dimension; total counts all pages
	r, err := s.ListComponents(ctx, ListQuery{Replication: "replicated", Limit: 1})
	must(t, err)
	if want := []FacetValue{{"single", 1}, {"replicated", 1}}; !slices.Equal(r.Facets.Replication, want) {
		t.Fatalf("replication facet = %+v, want %+v", r.Facets.Replication, want)
	}
	if r.Total != 1 {
		t.Fatalf("total = %d", r.Total)
	}
	r, err = s.ListComponents(ctx, ListQuery{Limit: 1})
	must(t, err)
	if len(r.Components) != 1 || r.Next == nil || r.Total != 2 {
		t.Fatalf("page = %v, next = %v, total = %d", names(r.Components), r.Next, r.Total)
	}
}

func TestListPaging(t *testing.T) {
	s := seededStore(t)
	ctx := context.Background()
	for _, sort := range []string{"updated", "name"} {
		var got []string
		var cur *Cursor
		for range 5 {
			r, err := s.ListComponents(ctx, ListQuery{IncludePrerelease: true, Sort: sort, Limit: 1, Cursor: cur})
			must(t, err)
			got = append(got, names(r.Components)...)
			if r.Next == nil {
				break
			}
			cur = r.Next
		}
		if len(got) != 3 || len(slices.Compact(slices.Sorted(slices.Values(got)))) != 3 {
			t.Fatalf("sort=%s pages = %v", sort, got)
		}
	}
}

func TestComponentDetail(t *testing.T) {
	s := seededStore(t)
	d, err := s.Component(context.Background(), "acme.org/x")
	must(t, err)
	want := []LocationSummary{{Repository: locA, Latest: "1.1.0", VersionCount: 3}, {Repository: locB, Latest: "1.0.0", VersionCount: 1}}
	if !slices.Equal(d.LocationDetails, want) || d.Latest != "1.1.0" {
		t.Fatalf("detail = %+v", d)
	}
	if _, err := s.Component(context.Background(), "nope.org/x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestVersions(t *testing.T) {
	s := seededStore(t)
	ctx := context.Background()
	r, err := s.Versions(ctx, VersionsQuery{Component: "acme.org/x"})
	must(t, err)
	// newest first; 1.0.0 once, earliest date (day 1), both locations; rc hidden
	if len(r.Versions) != 2 || r.Versions[0].Version != "1.1.0" || r.Versions[1].Version != "1.0.0" ||
		!r.Versions[1].PublishedAt.Equal(day(1)) || !slices.Equal(r.Versions[1].Locations, []string{locA, locB}) {
		t.Fatalf("versions = %+v", r.Versions)
	}
	r, _ = s.Versions(ctx, VersionsQuery{Component: "acme.org/x", IncludePrerelease: true})
	if len(r.Versions) != 3 || r.Versions[0].Version != "2.0.0-rc.1" || r.Versions[0].Stable {
		t.Fatalf("with pre = %+v", r.Versions)
	}
	c, _ := semver.NewConstraint("< 1.1.0")
	r, _ = s.Versions(ctx, VersionsQuery{Component: "acme.org/x", Constraint: c})
	if len(r.Versions) != 1 || r.Versions[0].Version != "1.0.0" {
		t.Fatalf("constraint = %+v", r.Versions)
	}
	r, _ = s.Versions(ctx, VersionsQuery{Component: "acme.org/x", Locations: []string{locB}})
	if len(r.Versions) != 1 || r.Versions[0].Version != "1.0.0" {
		t.Fatalf("location = %+v", r.Versions)
	}
	r, _ = s.Versions(ctx, VersionsQuery{Component: "acme.org/x", Limit: 1})
	r2, _ := s.Versions(ctx, VersionsQuery{Component: "acme.org/x", Limit: 1, Cursor: r.Next})
	if r.Next == nil || len(r2.Versions) != 1 || r2.Versions[0].Version != "1.0.0" || r2.Next != nil {
		t.Fatalf("paging = %+v / %+v", r, r2)
	}
	if _, err := s.Versions(ctx, VersionsQuery{Component: "nope.org/x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestLocationLookups(t *testing.T) {
	s := seededStore(t)
	ctx := context.Background()
	// overview location (A) first
	locs, err := s.VersionLocations(ctx, "acme.org/x", "1.0.0")
	must(t, err)
	if !slices.Equal(locs, []string{locA, locB}) {
		t.Fatalf("locs = %v", locs)
	}
	if ok, _ := s.HasVersionAt(ctx, "acme.org/x", "1.1.0", locB); ok {
		t.Fatal("1.1.0 is not at B")
	}
	if ok, _ := s.HasVersionAt(ctx, "acme.org/x", "1.1.0", locA); !ok {
		t.Fatal("1.1.0 is at A")
	}
	if ok, _ := s.IsLocation(ctx, locB); !ok {
		t.Fatal("B is a location")
	}
	if ok, _ := s.IsLocation(ctx, "oci::evil.example"); ok {
		t.Fatal("unknown location")
	}
}

func TestVersionPublishedAt(t *testing.T) {
	s := seededStore(t)
	ctx := context.Background()
	got, ok, err := s.VersionPublishedAt(ctx, "acme.org/x", "1.0.0")
	must(t, err)
	if !ok || got.IsZero() {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok, err := s.VersionPublishedAt(ctx, "acme.org/x", "9.9.9"); err != nil || ok {
		t.Fatalf("unknown: %v %v", ok, err)
	}
}

func TestLocationFilterIgnoresDiscoveriesWithoutVersions(t *testing.T) {
	w, p := newWriter(t)
	runSync(t, w, "c1", 0, true, day(30),
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}},
		Found{Component: "acme.org/x", Repository: locB}) // discovered at B, no versions there
	must(t, w.Close())
	h := NewHandle(p)
	t.Cleanup(func() { h.Close() })
	s, release, err := h.Acquire()
	must(t, err)
	t.Cleanup(release)
	r, err := s.ListComponents(context.Background(), ListQuery{})
	must(t, err)
	r, err = s.ListComponents(context.Background(), ListQuery{Locations: []string{locB}})
	must(t, err)
	if len(r.Components) != 0 {
		t.Fatalf("location B matched %v", names(r.Components))
	}
}

func TestStoreTopLevelSignedReferencedBy(t *testing.T) {
	w, p := newWriter(t)
	ctx := context.Background()
	const loc = "oci::ghcr.io/acme"
	found := func(name string) Found {
		return Found{Component: name, Repository: loc, Versions: []FoundVersion{v("1.0.0", 1)}}
	}
	runSync(t, w, "c", 0, true, day(10), found("acme.org/app"), found("acme.org/lib"), found("acme.org/tool"))
	setOverview(t, w, "acme.org/app", "1.0.0", loc, Overview{Title: "App", Provider: "acme", Signed: true,
		Children: []ChildRef{{ReferenceName: "lib", ChildName: "acme.org/lib", ChildVersion: "1.0.0"}}})
	setOverview(t, w, "acme.org/tool", "1.0.0", loc, Overview{Title: "Tool", Provider: "acme",
		Children: []ChildRef{{ReferenceName: "lib", ChildName: "acme.org/lib", ChildVersion: "1.0.0"}}})
	setOverview(t, w, "acme.org/lib", "1.0.0", loc, Overview{Title: "Lib", Provider: "acme"})
	if err := w.UpdateFamilies(ctx, prefixFirstSegment); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s := openStore(t, p)
	all, err := s.ListComponents(ctx, ListQuery{})
	if err != nil || len(all.Components) != 3 {
		t.Fatalf("all: %+v %v", all, err)
	}
	top, err := s.ListComponents(ctx, ListQuery{TopLevel: true, Sort: "name"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range top.Components {
		names = append(names, c.Name)
	}
	if !slices.Equal(names, []string{"acme.org/app", "acme.org/tool"}) {
		t.Fatalf("top level = %v", names)
	}
	if top.Components[0].DefaultVersion != "1.0.0" {
		t.Fatalf("default version = %q", top.Components[0].DefaultVersion)
	}
	if !top.Components[0].Signed || top.Components[1].Signed {
		t.Fatalf("signed flags: %+v", top.Components)
	}
	d, err := s.Component(ctx, "acme.org/lib")
	if err != nil {
		t.Fatal(err)
	}
	want := []ParentRef{{Name: "acme.org/app", Title: "App", Latest: "1.0.0"}, {Name: "acme.org/tool", Title: "Tool", Latest: "1.0.0"}}
	if !reflect.DeepEqual(d.ReferencedBy, want) || d.ReferencedByCount != 2 {
		t.Fatalf("referenced by = %+v (%d)", d.ReferencedBy, d.ReferencedByCount)
	}
}

func openStore(t *testing.T, p string) *Store {
	t.Helper()
	h := NewHandle(p)
	t.Cleanup(func() { h.Close() })
	s, release, err := h.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return s
}

// nToNStore: c1 (position 0) finds x at locA and y at locB; c2 (position 1)
// also finds x at locA and z at locB without versions there.
func nToNStore(t *testing.T) *Store {
	t.Helper()
	w, p := newWriter(t)
	ctx := context.Background()
	run := func(id string, pos int, src Source, found ...Found) {
		tx, err := w.BeginConnector(ctx, id, "github/v1", src, pos, day(30))
		must(t, err)
		for _, f := range found {
			f.Owner, f.Visibility = src.Key, "public"
			must(t, tx.Apply(ctx, f))
		}
		must(t, tx.Commit(ctx, true))
	}
	run("c1", 0, Source{Key: "github:org:acme", Title: "Acme", AvatarURL: "https://avatars.example/acme"},
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}},
		Found{Component: "acme.org/y", Repository: locB, Versions: []FoundVersion{v("1.0.0", 2)}})
	run("c2", 1, Source{Key: "github:user:jane", Title: "jane"},
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}},
		Found{Component: "other.org/z", Repository: locB}) // discovered, no versions
	must(t, w.Cleanup(ctx))
	must(t, w.Close())
	h := NewHandle(p)
	t.Cleanup(func() { h.Close() })
	s, release, err := h.Acquire()
	must(t, err)
	t.Cleanup(release)
	return s
}

func TestConnectorFacetNToN(t *testing.T) {
	s := nToNStore(t)
	ctx := context.Background()
	r, err := s.ListComponents(ctx, ListQuery{})
	must(t, err)
	want := []ConnectorFacet{
		{Key: "github:org:acme", Title: "Acme", Type: "github/v1", AvatarURL: "https://avatars.example/acme", Count: 2, Groups: []GroupFacet{}},
		{Key: "github:user:jane", Title: "jane", Type: "github/v1", Count: 1, Groups: []GroupFacet{}},
	}
	if !reflect.DeepEqual(r.Facets.Connectors, want) {
		t.Fatalf("connectors facet = %+v", r.Facets.Connectors)
	}
	for name, tc := range map[string]struct {
		keys []string
		want []string
	}{
		"one":     {[]string{"github:user:jane"}, []string{"acme.org/x"}},
		"both":    {[]string{"github:org:acme", "github:user:jane"}, []string{"acme.org/x", "acme.org/y"}},
		"unknown": {[]string{"github:org:nobody"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := s.ListComponents(ctx, ListQuery{Connectors: tc.keys, Sort: "name"})
			must(t, err)
			if got := names(r.Components); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	// the facet ignores its own dimension but honours the others
	r, err = s.ListComponents(ctx, ListQuery{Connectors: []string{"github:user:jane"}, Q: "acme.org/y"})
	must(t, err)
	acmeOnlyY := want[0]
	acmeOnlyY.Count = 1 // only acme.org/y matches q
	if !reflect.DeepEqual(r.Facets.Connectors, []ConnectorFacet{acmeOnlyY}) {
		t.Fatalf("filtered facet = %+v", r.Facets.Connectors)
	}
}

func TestComponentSources(t *testing.T) {
	s := nToNStore(t)
	d, err := s.Component(context.Background(), "acme.org/x")
	must(t, err)
	want := []SourceSummary{
		{Key: "github:org:acme", Title: "Acme", Type: "github/v1", AvatarURL: "https://avatars.example/acme", Locations: []string{locA}, Groups: []GroupSummary{}},
		{Key: "github:user:jane", Title: "jane", Type: "github/v1", Locations: []string{locA}, Groups: []GroupSummary{}},
	}
	if !reflect.DeepEqual(d.Sources, want) {
		t.Fatalf("sources = %+v", d.Sources)
	}
}

// enterpriseStore: "ent" (enterprise, position 0) finds x at corp (acme-corp)
// and y at labs (acme-labs, internal); "org" (position 1, explicit org entry)
// also finds x at corp.
func enterpriseStore(t *testing.T) *Store {
	t.Helper()
	w, p := newWriter(t)
	ctx := context.Background()
	const ent = "github:enterprise:acme-enterprise"
	corp, labs := "oci::ghcr.io/acme-corp", "oci::ghcr.io/acme-labs"
	run := func(id string, pos int, src Source, found ...Found) {
		tx, err := w.BeginConnector(ctx, id, "github/v1", src, pos, day(30))
		must(t, err)
		for _, f := range found {
			must(t, tx.Apply(ctx, f))
		}
		must(t, tx.Commit(ctx, true))
	}
	run("ent", 0, Source{Key: ent, Title: "Acme Enterprise", Scope: "s", Owners: []Owner{
		{Key: "github:org:acme-corp", Title: "Acme Corporation", AvatarURL: "https://avatars.example/corp", Parent: ent},
		{Key: "github:org:acme-labs", Title: "Acme Labs", Parent: ent}}},
		Found{Component: "acme.org/x", Repository: corp, Owner: "github:org:acme-corp", Visibility: "public", Versions: []FoundVersion{v("1.0.0", 1)}},
		Found{Component: "acme.org/y", Repository: labs, Owner: "github:org:acme-labs", Visibility: "internal", Versions: []FoundVersion{v("1.0.0", 2)}})
	run("org", 1, Source{Key: "github:org:acme-corp", Title: "Acme Corporation", Owners: []Owner{{Key: "github:org:acme-corp", Title: "Acme Corporation"}}},
		Found{Component: "acme.org/x", Repository: corp, Owner: "github:org:acme-corp", Visibility: "public", Versions: []FoundVersion{v("1.0.0", 1)}})
	must(t, w.Cleanup(ctx))
	must(t, w.Close())
	h := NewHandle(p)
	t.Cleanup(func() { h.Close() })
	s, release, err := h.Acquire()
	must(t, err)
	t.Cleanup(release)
	return s
}

func TestOwnerKeyMatchesAcrossEntries(t *testing.T) {
	s := enterpriseStore(t)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		keys []string
		want []string
	}{
		"enterprise": {[]string{"github:enterprise:acme-enterprise"}, []string{"acme.org/x", "acme.org/y"}},
		"org owner":  {[]string{"github:org:acme-corp"}, []string{"acme.org/x"}},
		"labs":       {[]string{"github:org:acme-labs"}, []string{"acme.org/y"}},
		"mixed":      {[]string{"github:org:acme-corp", "github:org:acme-labs"}, []string{"acme.org/x", "acme.org/y"}},
		"unknown":    {[]string{"github:org:nobody"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := s.ListComponents(ctx, ListQuery{Connectors: tc.keys, Sort: "name"})
			must(t, err)
			if got := names(r.Components); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConnectorFacetGroups(t *testing.T) {
	s := enterpriseStore(t)
	r, err := s.ListComponents(context.Background(), ListQuery{})
	must(t, err)
	if len(r.Facets.Connectors) != 2 {
		t.Fatalf("facet = %+v", r.Facets.Connectors)
	}
	entF, orgF := r.Facets.Connectors[0], r.Facets.Connectors[1]
	wantGroups := []GroupFacet{
		{Key: "github:org:acme-corp", Title: "Acme Corporation", AvatarURL: "https://avatars.example/corp", Count: 1},
		{Key: "github:org:acme-labs", Title: "Acme Labs", Count: 1},
	}
	if entF.Key != "github:enterprise:acme-enterprise" || entF.Count != 2 || !slices.Equal(entF.Groups, wantGroups) {
		t.Fatalf("enterprise facet = %+v", entF)
	}
	if orgF.Key != "github:org:acme-corp" || orgF.Count != 1 || orgF.Groups == nil || len(orgF.Groups) != 0 {
		t.Fatalf("org facet = %+v", orgF)
	}
	// groups honour other filters and drop zero counts
	r, err = s.ListComponents(context.Background(), ListQuery{Q: "acme.org/y"})
	must(t, err)
	if g := r.Facets.Connectors[0].Groups; len(g) != 1 || g[0].Key != "github:org:acme-labs" {
		t.Fatalf("filtered groups = %+v", g)
	}
}

func TestComponentSourceGroups(t *testing.T) {
	s := enterpriseStore(t)
	d, err := s.Component(context.Background(), "acme.org/x")
	must(t, err)
	if len(d.Sources) != 2 {
		t.Fatalf("sources = %+v", d.Sources)
	}
	want := []GroupSummary{{Key: "github:org:acme-corp", Title: "Acme Corporation", AvatarURL: "https://avatars.example/corp", Locations: []string{"oci::ghcr.io/acme-corp"}}}
	if !reflect.DeepEqual(d.Sources[0].Groups, want) || len(d.Sources[1].Groups) != 0 || d.Sources[1].Groups == nil {
		t.Fatalf("groups = %+v / %+v", d.Sources[0].Groups, d.Sources[1].Groups)
	}
}

func TestOwnerAvatarOfComponents(t *testing.T) {
	s := enterpriseStore(t)
	ctx := context.Background()
	r, err := s.ListComponents(ctx, ListQuery{Sort: "name"})
	must(t, err)
	got := map[string]string{}
	for _, c := range r.Components {
		got[c.Name] = c.OwnerAvatarURL
	}
	if got["acme.org/x"] != "https://avatars.example/corp" || got["acme.org/y"] != "" {
		t.Fatalf("owner avatars = %v", got)
	}
	d, err := s.Component(ctx, "acme.org/x")
	must(t, err)
	if d.OwnerAvatarURL != "https://avatars.example/corp" {
		t.Fatalf("detail owner avatar = %q", d.OwnerAvatarURL)
	}
}

func TestOwnerAvatarIgnoresUserAccounts(t *testing.T) {
	w, p := newWriter(t)
	ctx := context.Background()
	tx, err := w.BeginConnector(ctx, "u", "github/v1", Source{Key: "github:user:jane", Title: "Jane", Owners: []Owner{{Key: "github:user:jane", Title: "Jane", AvatarURL: "https://avatars.example/jane"}}}, 0, day(30))
	must(t, err)
	must(t, tx.Apply(ctx, Found{Component: "jane.dev/tool", Repository: "oci::ghcr.io/jane", Owner: "github:user:jane", Visibility: "public", Versions: []FoundVersion{v("1.0.0", 1)}}))
	must(t, tx.Commit(ctx, true))
	must(t, w.Cleanup(ctx))
	must(t, w.Close())
	h := NewHandle(p)
	t.Cleanup(func() { h.Close() })
	s, release, err := h.Acquire()
	must(t, err)
	t.Cleanup(release)
	d, err := s.Component(ctx, "jane.dev/tool")
	must(t, err)
	if d.OwnerAvatarURL != "" {
		t.Fatalf("a personal account's avatar must not be used: %q", d.OwnerAvatarURL)
	}
}

func attestationFixture(t *testing.T) (*Writer, string) {
	t.Helper()
	w, p := newWriter(t)
	runSync(t, w, "c1", 0, true, day(5),
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1), v("1.1.0", 3)}},
		Found{Component: "acme.org/x", Repository: locB, Versions: []FoundVersion{v("1.1.0", 4)}})
	setOverview(t, w, "acme.org/x", "1.1.0", locB, Overview{Title: "X"})
	return w, p
}

func TestSetAttestationsRoundTrip(t *testing.T) {
	w, p := attestationFixture(t)
	ctx := context.Background()
	res := attestation.Result{Levels: []attestation.Level{
		{Status: attestation.StatusOK, Entries: []attestation.Entry{
			{Kind: attestation.KindVEX, Source: attestation.SourceLabelLink, Signed: attestation.SignedByComponent, URL: "https://example.com/vex.json"},
			{Kind: attestation.KindSBOM, Format: "CycloneDX 1.6", Type: "application/vnd.cyclonedx+json", Source: attestation.SourceEmbedded,
				Signed: attestation.SignedByComponent, Resource: &attestation.Identity{Name: "sbom", Version: "1.0.0", ExtraIdentity: map[string]string{"arch": "amd64"}}},
		}},
		{Resource: &attestation.Identity{Name: "img", Version: "1.0.0", ExtraIdentity: map[string]string{"os": "linux", "arch": "arm64"}},
			Status: attestation.StatusOK, Entries: []attestation.Entry{
				{Kind: attestation.KindSLSA, Format: "SLSA Provenance v1", Type: "https://slsa.dev/provenance/v1", Source: attestation.SourceBuildx,
					Platform: "linux/arm64", Signed: attestation.SignedYes, Digest: "sha256:abc", BuildLevel: 3,
					Builder: "https://github.com/actions/runner", Repository: "ghcr.io/acme/x", Label: "provenance"},
				{Kind: attestation.KindCVE, Format: "SARIF 2.1.0", Source: attestation.SourceReferrer, Signed: attestation.SignedNo,
					Summary: &document.Summary{Format: "SARIF 2.1.0", Tool: "Trivy", Findings: 37, ImportantFindings: 4, ImportantBasis: "security-severity"}},
			}},
		{Resource: &attestation.Identity{Name: "img", Version: "1.0.0"}, Status: attestation.StatusFailed, Error: "401 unauthorized", Entries: []attestation.Entry{}},
		{Resource: &attestation.Identity{Name: "blob"}, Status: attestation.StatusNotOCI, Entries: []attestation.Entry{}},
	}}
	checked := day(6).Add(1500 * time.Millisecond)
	if err := w.SetAttestations(ctx, AttestationTarget{"acme.org/x", "1.1.0", locB}, res, checked); err != nil {
		t.Fatal(err)
	}
	must(t, w.Close())
	s := openStore(t, p)
	got, ok, err := s.Attestations(ctx, "acme.org/x", "1.1.0", locB)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.Version != "1.1.0" || got.Repository != locB || !got.CheckedAt.Equal(checked) {
		t.Fatalf("set = %+v", got)
	}
	if !reflect.DeepEqual(got.Result, res) {
		t.Fatalf("result =\n%+v\nwant\n%+v", got.Result, res)
	}
	if got.Result.Levels[2].Entries == nil {
		t.Fatal("empty Entries must be non-nil")
	}
	// Lookups are exact: no set at locA, and the repository is required.
	if _, ok, err := s.Attestations(ctx, "acme.org/x", "1.1.0", locA); err != nil || ok {
		t.Fatalf("other location: ok=%v err=%v", ok, err)
	}
	if _, _, err := s.Attestations(ctx, "acme.org/x", "1.1.0", ""); err == nil {
		t.Fatal("empty repository must be an error")
	}
}

func TestSetAttestationsReplaces(t *testing.T) {
	w, p := attestationFixture(t)
	ctx := context.Background()
	entry := func(k attestation.Kind) attestation.Result {
		return attestation.Result{Levels: []attestation.Level{{Status: attestation.StatusOK,
			Entries: []attestation.Entry{{Kind: k, Source: attestation.SourceReferrer, Signed: attestation.SignedNo}}}}}
	}
	t110 := AttestationTarget{"acme.org/x", "1.1.0", locB}
	t100 := AttestationTarget{"acme.org/x", "1.0.0", locA}
	must(t, w.SetAttestations(ctx, t110, entry(attestation.KindSBOM), day(6)))
	must(t, w.SetAttestations(ctx, t100, entry(attestation.KindVEX), day(6)))
	must(t, w.SetAttestations(ctx, t110, entry(attestation.KindSLSA), day(7)))
	must(t, w.Close())
	s := openStore(t, p)
	got, _, err := s.Attestations(ctx, "acme.org/x", "1.1.0", locB)
	must(t, err)
	if len(got.Result.Levels) != 1 || len(got.Result.Levels[0].Entries) != 1 || got.Result.Levels[0].Entries[0].Kind != attestation.KindSLSA {
		t.Fatalf("not replaced: %+v", got.Result)
	}
	other, ok, err := s.Attestations(ctx, "acme.org/x", "1.0.0", locA)
	must(t, err)
	if !ok || other.Result.Levels[0].Entries[0].Kind != attestation.KindVEX {
		t.Fatalf("other version lost: %+v", other)
	}
	col, err := s.CollectedAttestations(ctx, "acme.org/x")
	must(t, err)
	// The current target (default 1.1.0 at the overview location) first.
	if want := []AttestationTarget{t110, t100}; !reflect.DeepEqual(col, want) {
		t.Fatalf("collected = %+v", col)
	}
}

func TestAttestationsUnknownVersionNotFound(t *testing.T) {
	w, p := attestationFixture(t)
	must(t, w.Close())
	s := openStore(t, p)
	_, ok, err := s.Attestations(context.Background(), "acme.org/x", "9.9.9", locA)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

// relevanceStore: prod bundles x1, x2, x3; app includes x1; solo stands alone.
// Scores: prod 3 (bundles 3), x1 2 (included twice), app 1, x2 1, x3 1, solo 0.
func relevanceStore(t *testing.T) *Store {
	t.Helper()
	w, p := newWriter(t)
	const loc = "oci::ghcr.io/acme"
	found := func(name string, d int) Found {
		return Found{Component: name, Repository: loc, Versions: []FoundVersion{v("1.0.0", d)}}
	}
	runSync(t, w, "c", 0, true, day(30), found("acme.org/prod", 1), found("lib.org/x1", 2), found("lib.org/x2", 4), found("lib.org/x3", 3),
		found("other.org/app", 5), found("solo.org/z", 9))
	ref := func(child string) ChildRef {
		return ChildRef{ReferenceName: child, ChildName: child, ChildVersion: "1.0.0"}
	}
	setOverview(t, w, "acme.org/prod", "1.0.0", loc, Overview{Title: "Prod", Children: []ChildRef{ref("lib.org/x1"), ref("lib.org/x2"), ref("lib.org/x3")}})
	setOverview(t, w, "other.org/app", "1.0.0", loc, Overview{Title: "App", Children: []ChildRef{ref("lib.org/x1")}})
	for _, n := range []string{"lib.org/x1", "lib.org/x2", "lib.org/x3", "solo.org/z"} {
		setOverview(t, w, n, "1.0.0", loc, Overview{Title: n})
	}
	must(t, w.UpdateFamilies(context.Background(), prefixFirstSegment)) // computes top_level and relevance
	must(t, w.Close())
	return openStore(t, p)
}

func TestListRelevance(t *testing.T) {
	s := relevanceStore(t)
	ctx := context.Background()
	r, err := s.ListComponents(ctx, ListQuery{Sort: "relevance"})
	must(t, err)
	// ties (score 1: app day 5, x2 day 4, x3 day 3) go by most recently published
	want := []string{"acme.org/prod", "lib.org/x1", "other.org/app", "lib.org/x2", "lib.org/x3", "solo.org/z"}
	if got := names(r.Components); !slices.Equal(got, want) {
		t.Fatalf("relevance order = %v, want %v", got, want)
	}
	// paging one by one yields the same order
	var got []string
	var cur *Cursor
	for range 10 {
		r, err := s.ListComponents(ctx, ListQuery{Sort: "relevance", Limit: 1, Cursor: cur})
		must(t, err)
		got = append(got, names(r.Components)...)
		if r.Next == nil {
			break
		}
		cur = r.Next
	}
	if !slices.Equal(got, want) {
		t.Fatalf("paged relevance = %v, want %v", got, want)
	}
	if _, err := s.ListComponents(ctx, ListQuery{Sort: "relevance", Cursor: &Cursor{Key: "garbage", Name: "x"}}); !errors.Is(err, ErrBadCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}
}

func TestVersionsHasChangelog(t *testing.T) {
	w, p := newWriter(t)
	ctx := context.Background()
	runSync(t, w, "c1", 0, true, day(30),
		Found{Component: "acme.org/x", Repository: locA, Versions: []FoundVersion{v("1.0.0", 1), v("1.1.0", 2), v("1.2.0", 3)}})
	yes, no := true, false
	must(t, w.SetChangelog(ctx, "acme.org/x", "1.0.0", &yes, ""))
	must(t, w.SetChangelog(ctx, "acme.org/x", "1.1.0", &no, ""))
	must(t, w.SetChangelog(ctx, "acme.org/x", "1.2.0", nil, ""))
	must(t, w.Close())
	h := NewHandle(p)
	t.Cleanup(func() { h.Close() })
	s, release, err := h.Acquire()
	must(t, err)
	defer release()
	r, err := s.Versions(ctx, VersionsQuery{Component: "acme.org/x"})
	must(t, err)
	if len(r.Versions) != 3 || r.Versions[2].HasChangelog == nil || !*r.Versions[2].HasChangelog ||
		r.Versions[1].HasChangelog == nil || *r.Versions[1].HasChangelog || r.Versions[0].HasChangelog != nil {
		t.Fatalf("versions = %+v", r.Versions)
	}
}
