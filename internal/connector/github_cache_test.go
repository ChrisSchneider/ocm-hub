package connector

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// allVisibilities indexes public, internal and private packages (the default is public only).
var allVisibilities = WithVisibility(Public, Internal, Private)

// mapCache is an in-memory PageCache.
type mapCache map[string][2]string

func (m mapCache) Lookup(u string) (string, string, bool) { v, ok := m[u]; return v[0], v[1], ok }
func (m mapCache) Store(u, etag, next string)             { m[u] = [2]string{etag, next} }

// pagedOrg serves /orgs/acme/packages in two pages with ETags "p1"/"p2";
// page 2 holds one descriptor package. Requests carrying the matching
// If-None-Match get 304 without a Link header, as GitHub does.
func pagedOrg(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	versions := &atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orgs/acme/packages", func(w http.ResponseWriter, r *http.Request) {
		etag := `W/"p1"`
		if r.URL.Query().Get("page") == "2" {
			etag = `W/"p2"`
		}
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `[{"name":"component-descriptors/acme.org/b","visibility":"private","updated_at":"2026-01-02T00:00:00Z"}]`)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/orgs/acme/packages?package_type=container&per_page=100&page=2>; rel="next"`, r.Host))
		fmt.Fprint(w, `[{"name":"component-descriptors/acme.org/a","visibility":"public","updated_at":"2026-01-01T00:00:00Z"}]`)
	})
	mux.HandleFunc("GET /orgs/acme/packages/container/{pkg}/versions", func(w http.ResponseWriter, r *http.Request) {
		versions.Add(1)
		fmt.Fprint(w, `[{"created_at":"2026-01-01T00:00:00Z","metadata":{"container":{"tags":["1.0.0"]}}}]`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, versions
}

func collectWith(t *testing.T, g *GitHub, since time.Time, cache PageCache) []ComponentVersions {
	t.Helper()
	var out []ComponentVersions
	for cv, err := range g.Changed(t.Context(), since, cache) {
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, cv)
	}
	return out
}

func TestChangedReportsOwnerAndVisibility(t *testing.T) {
	srv, _ := pagedOrg(t)
	if k := (Owner{Org: "ACME"}).key(); k != "github:org:acme" {
		t.Fatalf("key = %q", k)
	}
	got := collectWith(t, NewGitHub("id", Owner{Org: "acme"}, "", WithAPIURL(srv.URL), allVisibilities), time.Time{}, nil)
	if len(got) != 2 || got[0].Owner != "github:org:acme" || got[0].Visibility != Public || got[1].Visibility != Private {
		t.Fatalf("got %+v", got)
	}
}

func TestPageCacheStoresETagsAndNext(t *testing.T) {
	srv, _ := pagedOrg(t)
	cache := mapCache{}
	collectWith(t, NewGitHub("id", Owner{Org: "acme"}, "", WithAPIURL(srv.URL), allVisibilities), time.Time{}, cache)
	first := srv.URL + "/orgs/acme/packages?package_type=container&per_page=100"
	if v := cache[first]; v[0] != `W/"p1"` || v[1] == "" {
		t.Fatalf("page 1 cache = %v (all: %v)", v, cache)
	}
	if v := cache[cache[first][1]]; v[0] != `W/"p2"` || v[1] != "" {
		t.Fatalf("page 2 cache = %v", v)
	}
}

func TestPageCacheSkips304AndFollowsStoredNext(t *testing.T) {
	srv, versions := pagedOrg(t)
	cache := mapCache{}
	g := NewGitHub("id", Owner{Org: "acme"}, "", WithAPIURL(srv.URL), allVisibilities)
	collectWith(t, g, time.Time{}, cache)
	versions.Store(0)
	// second run: both pages answer 304; nothing is yielded and no /versions call is made
	if got := collectWith(t, g, time.Time{}, cache); len(got) != 0 || versions.Load() != 0 {
		t.Fatalf("got %+v, versions calls %d", got, versions.Load())
	}
	// drop page 2's entry: page 1 is 304, page 2 (reached via the stored next) is re-read
	delete(cache, cache[srv.URL+"/orgs/acme/packages?package_type=container&per_page=100"][1])
	if got := collectWith(t, g, time.Time{}, cache); len(got) != 1 || got[0].Component != "acme.org/b" {
		t.Fatalf("got %+v", got)
	}
}

func TestVisibilityFilter(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/orgs/acme/packages" {
			gotQuery = r.URL.Query().Get("visibility")
			fmt.Fprint(w, `[{"name":"component-descriptors/acme.org/a","visibility":"public","updated_at":"2026-01-01T00:00:00Z"},
				{"name":"component-descriptors/acme.org/b","visibility":"internal","updated_at":"2026-01-01T00:00:00Z"},
				{"name":"component-descriptors/acme.org/c","visibility":"private","updated_at":"2026-01-01T00:00:00Z"}]`)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	t.Cleanup(srv.Close)
	// one value: server-side filter (the fake server ignores it, so all three come back)
	collectWith(t, NewGitHub("id", Owner{Org: "acme"}, "", WithAPIURL(srv.URL), WithVisibility(Public)), time.Time{}, nil)
	if gotQuery != "public" {
		t.Fatalf("visibility query = %q", gotQuery)
	}
	// two values: no query parameter, filtered client-side
	got := collectWith(t, NewGitHub("id", Owner{Org: "acme"}, "", WithAPIURL(srv.URL), WithVisibility(Public, Internal)), time.Time{}, nil)
	if gotQuery != "" || len(got) != 2 || got[0].Component != "acme.org/a" || got[1].Component != "acme.org/b" {
		t.Fatalf("query %q, got %+v", gotQuery, got)
	}
	// no option, or an empty one: public packages only
	for name, opts := range map[string][]GitHubOption{"none": nil, "empty": {WithVisibility()}} {
		gotQuery = ""
		got = collectWith(t, NewGitHub("id", Owner{Org: "acme"}, "", append(opts, WithAPIURL(srv.URL))...), time.Time{}, nil)
		if gotQuery != "public" || len(got) != 1 || got[0].Component != "acme.org/a" {
			t.Fatalf("%s: query %q, got %+v", name, gotQuery, got)
		}
	}
}

// A last page holding exactly per_page items may gain a successor; a 304 carries
// no Link header, so such a page must not be cached.
func TestPageCacheSkipsFullLastPage(t *testing.T) {
	var grown atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orgs/acme/packages", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.Header().Set("ETag", `W/"p2"`)
			fmt.Fprint(w, `[{"name":"component-descriptors/acme.org/new","visibility":"public","updated_at":"2026-01-02T00:00:00Z"}]`)
			return
		}
		if r.Header.Get("If-None-Match") == `W/"p1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `W/"p1"`)
		if grown.Load() {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/orgs/acme/packages?package_type=container&per_page=100&page=2>; rel="next"`, r.Host))
		}
		var items []string
		items = append(items, `{"name":"component-descriptors/acme.org/a","visibility":"public","updated_at":"2026-01-01T00:00:00Z"}`)
		for i := 1; i < 100; i++ {
			items = append(items, fmt.Sprintf(`{"name":"other-%d","visibility":"public","updated_at":"2026-01-01T00:00:00Z"}`, i))
		}
		fmt.Fprint(w, "["+strings.Join(items, ",")+"]")
	})
	mux.HandleFunc("GET /orgs/acme/packages/container/{pkg}/versions", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"created_at":"2026-01-01T00:00:00Z","metadata":{"container":{"tags":["1.0.0"]}}}]`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cache := mapCache{}
	g := NewGitHub("id", Owner{Org: "acme"}, "", WithAPIURL(srv.URL))
	if got := collectWith(t, g, time.Time{}, cache); len(got) != 1 {
		t.Fatalf("run 1: %+v", got)
	}
	grown.Store(true)
	var found bool
	for _, cv := range collectWith(t, g, time.Time{}, cache) {
		found = found || cv.Component == "acme.org/new"
	}
	if !found {
		t.Fatal("new package on page 2 was missed")
	}
}
