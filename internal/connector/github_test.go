package connector

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

var ts = func(d int) string { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC).Format(time.RFC3339) }

// fakeGitHub: 3 packages over 2 pages; one is not a component descriptor.
func fakeGitHub(t *testing.T, rateLimitOnce bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var limited atomic.Bool
	calls := &atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orgs/acme/packages", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth = %q", got)
		}
		if r.URL.Query().Get("package_type") != "container" {
			t.Errorf("query = %v", r.URL.Query())
		}
		if rateLimitOnce && !limited.Swap(true) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprintf(w, `[{"name":"component-descriptors/acme.org/new","visibility":"public","updated_at":%q}]`, ts(20))
			return
		}
		next := "http://" + r.Host + "/orgs/acme/packages?package_type=container&per_page=100&page=2"
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next", <%s>; rel="last"`, next, next))
		fmt.Fprintf(w, `[{"name":"component-descriptors/acme.org/old","visibility":"public","updated_at":%q},{"name":"some-image","visibility":"public","updated_at":%q}]`, ts(2), ts(20))
	})
	mux.HandleFunc("GET /orgs/acme/packages/container/{pkg}/versions", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.PathValue("pkg") { // arrives percent-encoded as one segment
		case "component-descriptors/acme.org/old":
			fmt.Fprintf(w, `[{"created_at":%q,"metadata":{"container":{"tags":["1.0.0"]}}},
				{"created_at":%q,"metadata":{"container":{"tags":[]}}}]`, ts(1), ts(2))
		case "component-descriptors/acme.org/new":
			fmt.Fprintf(w, `[{"created_at":%q,"metadata":{"container":{"tags":["2.0.0","2.0"]}}}]`, ts(19))
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, calls
}

func collect(t *testing.T, g *GitHub, since time.Time) []ComponentVersions {
	t.Helper()
	return collectWith(t, g, since, nil)
}

func TestGitHubFull(t *testing.T) {
	srv, _ := fakeGitHub(t, false)
	g := NewGitHub("id1", Owner{Org: "acme"}, "tok", WithAPIURL(srv.URL))
	got := collect(t, g, time.Time{})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	old, nw := got[0], got[1]
	if old.Component != "acme.org/old" || old.Repository != "oci::ghcr.io/acme" || len(old.Versions) != 1 ||
		old.Versions[0].Name != "1.0.0" || old.Versions[0].PublishedAt.Format(time.RFC3339) != ts(1) {
		t.Fatalf("old = %+v", old)
	}
	if nw.Component != "acme.org/new" || len(nw.Versions) != 2 {
		t.Fatalf("new = %+v", nw)
	}
	if g.ID() != "id1" || g.Type() != "github/v1" {
		t.Fatal("id/type")
	}
}

func TestGitHubDeltaSkipsUnchangedPackages(t *testing.T) {
	srv, calls := fakeGitHub(t, false)
	g := NewGitHub("id1", Owner{Org: "acme"}, "tok", WithAPIURL(srv.URL))
	got := collect(t, g, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))
	if len(got) != 1 || got[0].Component != "acme.org/new" {
		t.Fatalf("got %+v", got)
	}
	if n := calls.Load(); n != 3 { // 2 package pages + 1 versions call
		t.Fatalf("calls = %d", n)
	}
}

func TestGitHubWaitsForRateLimitReset(t *testing.T) {
	srv, _ := fakeGitHub(t, true)
	var slept time.Duration
	g := NewGitHub("id1", Owner{Org: "acme"}, "tok", WithAPIURL(srv.URL),
		withSleep(func(_ context.Context, d time.Duration) error { slept = d; return nil }))
	if got := collect(t, g, time.Time{}); len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if slept < 59*time.Minute {
		t.Fatalf("slept %v", slept)
	}
}

func TestGitHubErrorStopsIteration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	g := NewGitHub("id1", Owner{Org: "acme"}, "", WithAPIURL(srv.URL))
	n := 0
	for _, err := range g.Changed(context.Background(), time.Time{}, nil) {
		n++
		if err == nil {
			t.Fatal("want error")
		}
	}
	if n != 1 {
		t.Fatalf("yields = %d", n)
	}
}

func TestPackagePathIsEscaped(t *testing.T) {
	if got := versionsPath(Owner{Org: "acme"}, "component-descriptors/acme.org/x"); got != "/orgs/acme/packages/container/"+url.PathEscape("component-descriptors/acme.org/x")+"/versions" {
		t.Fatalf("path = %s", got)
	}
}
