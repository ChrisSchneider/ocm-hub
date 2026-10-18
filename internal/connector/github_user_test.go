package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestGitHubUserOwner(t *testing.T) {
	var srv *httptest.Server
	file := func(w http.ResponseWriter, name, link string) {
		b, err := os.ReadFile(filepath.Join("testdata", "github", name))
		if err != nil {
			b = []byte("[]")
		}
		if link != "" {
			w.Header().Set("Link", link)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(b)
	}
	var paths []string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case r.URL.Path == "/users/jane-doe/packages":
			file(w, "user-packages.json", `<`+srv.URL+`/user/90000002/packages?package_type=container&per_page=100&page=2>; rel="next"`)
		case r.URL.Path == "/user/90000002/packages":
			file(w, "user-packages-page2.json", "")
		case r.URL.Path == "/users/jane-doe/packages/container/component-descriptors/github.com/jane-doe/tools/cli/versions":
			file(w, "user-versions-tools-cli.json", "")
		case r.URL.Path == "/users/jane-doe/packages/container/component-descriptors/github.com/jane-doe/tools/old/versions":
			file(w, "missing.json", "")
		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	g := NewGitHub("id", Owner{User: "jane-doe"}, "", WithAPIURL(srv.URL))
	var got []ComponentVersions
	for cv, err := range g.Changed(context.Background(), time.Time{}, nil) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, cv)
	}
	if len(got) != 2 || got[0].Component != "github.com/jane-doe/tools/cli" || got[0].Repository != "oci::ghcr.io/jane-doe" {
		t.Fatalf("got %+v", got)
	}
	var tags []string
	for _, v := range got[0].Versions {
		tags = append(tags, v.Name)
	}
	if !slices.Equal(tags, []string{"1.1.0", "1.0.0"}) {
		t.Fatalf("tags = %v", tags)
	}
	if !slices.Contains(paths, "/user/90000002/packages") {
		t.Fatalf("did not follow the user Link form: %v", paths)
	}
}

func TestGitHubOwnerRepositoryIsLowercase(t *testing.T) {
	if got := NewGitHub("id", Owner{User: "ChrisSchneider"}, "").repository(Owner{User: "ChrisSchneider"}, ""); got != "oci::ghcr.io/chrisschneider" {
		t.Fatal(got)
	}
}
