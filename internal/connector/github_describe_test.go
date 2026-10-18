package connector

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func describeServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	serve := func(path, file string) {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			b, err := os.ReadFile(filepath.Join("testdata", "github", file))
			if err != nil {
				t.Fatal(err)
			}
			w.Write(b)
		})
	}
	serve("/orgs/acme-corp", "org-acme-corp.json")
	serve("/users/jane-doe", "user-jane-doe.json")
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGitHubDescribeOrg(t *testing.T) {
	srv := describeServer(t)
	g := NewGitHub("id", Owner{Org: "acme-corp"}, "", WithAPIURL(srv.URL), WithKey("github:org:acme-corp"), WithTitle("Acme"))
	src, err := g.Describe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if src.Title != "Acme Corporation" || src.AvatarURL != "https://avatars.githubusercontent.com/u/90000001?s=32&v=4" {
		t.Fatalf("src = %+v", src)
	}
	if g.Key() != "github:org:acme-corp" || g.Title() != "Acme" {
		t.Fatalf("key/title = %q/%q", g.Key(), g.Title())
	}
}

func TestGitHubDescribeUserWithoutName(t *testing.T) {
	srv := describeServer(t)
	src, err := NewGitHub("id", Owner{User: "jane-doe"}, "", WithAPIURL(srv.URL)).Describe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if src.Title != "" || src.AvatarURL == "" || len(src.Owners) != 1 || src.Owners[0].Title != "jane-doe" {
		t.Fatalf("src = %+v", src)
	}
}

func TestGitHubDescribeNotFound(t *testing.T) {
	srv := describeServer(t)
	if _, err := NewGitHub("id", Owner{Org: "nobody"}, "", WithAPIURL(srv.URL)).Describe(t.Context()); err == nil {
		t.Fatal("expected an error")
	}
}

var _ Describer = (*GitHub)(nil)

func TestGitHubDescribeDropsNonHTTPSAvatar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "X", "avatar_url": "http://avatars.example/u/1?v=4"}`))
	}))
	t.Cleanup(srv.Close)
	src, err := NewGitHub("id", Owner{Org: "x"}, "", WithAPIURL(srv.URL)).Describe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if src.AvatarURL != "" {
		t.Fatalf("AvatarURL = %q, want empty", src.AvatarURL)
	}
}
