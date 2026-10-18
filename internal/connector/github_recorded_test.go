package connector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The fixtures in testdata/github are synthetic GitHub Packages API responses
// in the exact shape GitHub returns (see testdata/github/README.md). The server below replays them, including
// GitHub's paging behaviour: the Link header GitHub sends points at an
// /organizations/<id>/... path, not the /orgs/<name>/... one that was requested.

const recordedOrg = "acme-corp"

func readFixture(t *testing.T, name string, out any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "github", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func recordedGitHub(t *testing.T, page2 *atomic.Int32) *httptest.Server {
	t.Helper()
	const (
		orgPrefix = "/orgs/" + recordedOrg + "/packages"
		orgPaging = "/organizations/90000001/packages/container/"
		pkgPath   = "/packages/container/"
	)
	serve := func(w http.ResponseWriter, file string, link string) {
		b, err := os.ReadFile(filepath.Join("testdata", "github", file))
		if err != nil {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if link != "" {
			w.Header().Set("Link", link)
		}
		w.Write(b)
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("non-GET request %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		p := r.URL.EscapedPath()
		if p == orgPrefix {
			if r.URL.Query().Get("package_type") != "container" {
				t.Errorf("package_type = %q", r.URL.Query().Get("package_type"))
			}
			serve(w, "packages.json", "")
			return
		}
		var rest string
		switch {
		case strings.HasPrefix(p, orgPrefix+"/container/"):
			rest = strings.TrimPrefix(p, orgPrefix+"/container/")
		case strings.HasPrefix(p, orgPaging):
			rest = strings.TrimPrefix(p, orgPaging)
		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		escaped, ok := strings.CutSuffix(rest, "/versions")
		if !ok || strings.Contains(escaped, "/") {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		pkg, err := url.PathUnescape(escaped)
		if err != nil || !isDescriptorPackage(pkg) {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		base := fixtureBase(pkg)
		if pkg == "component-descriptors/acme.example/platform/gateway" {
			// the only multi-page listing; page 1 links to page 2 as GitHub does
			if r.URL.Query().Get("page") == "2" {
				page2.Add(1)
				serve(w, base+"-page2.json", "")
				return
			}
			next := srv.URL + orgPaging + escaped + "/versions?per_page=100&page=2"
			serve(w, base+".json", `<`+next+`>; rel="next", <`+next+`>; rel="last"`)
			return
		}
		serve(w, base+".json", "")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func isDescriptorPackage(pkg string) bool {
	return strings.HasPrefix(pkg, "component-descriptors/") || strings.Contains(pkg, "/component-descriptors/")
}

// fixtureBase names the versions fixture of a package: the name with "/" as "-"
// (a leading component-descriptors/ is dropped).
func fixtureBase(pkg string) string {
	return "versions-" + strings.ReplaceAll(strings.TrimPrefix(pkg, "component-descriptors/"), "/", "-")
}

type recordedPackage struct {
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updated_at"`
}

func tagCount(t *testing.T, files ...string) int {
	t.Helper()
	n := 0
	for _, f := range files {
		var vs []ghVersion
		readFixture(t, f, &vs)
		for _, v := range vs {
			n += len(v.Metadata.Container.Tags)
		}
	}
	return n
}

func TestGitHubRecordedResponses(t *testing.T) {
	var page2 atomic.Int32
	srv := recordedGitHub(t, &page2)
	g := NewGitHub("id", Owner{Org: recordedOrg}, "tok", WithAPIURL(srv.URL), allVisibilities)

	var pkgs []recordedPackage
	readFixture(t, "packages.json", &pkgs)
	type want struct {
		pkg, repo string
		updated   time.Time
	}
	wantComps := map[string]want{} // component -> expectation, derived from the package names
	nDesc := 0
	for _, p := range pkgs {
		if !isDescriptorPackage(p.Name) {
			continue
		}
		nDesc++
		i := strings.Index(p.Name, "component-descriptors/")
		repo := "oci::ghcr.io/acme-corp"
		if i > 0 {
			repo += "/" + strings.TrimSuffix(p.Name[:i], "/")
		}
		wantComps[p.Name[i+len("component-descriptors/"):]] = want{p.Name, repo, p.UpdatedAt}
	}
	if len(wantComps) != 10 || nDesc != 10 {
		t.Fatalf("fixture has %d descriptor packages (%d distinct components), want 10", nDesc, len(wantComps))
	}
	nested := 0
	for _, w := range wantComps {
		if w.repo != "oci::ghcr.io/acme-corp" {
			nested++
		}
	}
	if nested != 3 || len(pkgs) <= nDesc {
		t.Fatalf("fixture must hold 3 nested descriptor packages and non-descriptor packages (nested=%d)", nested)
	}

	got := map[string]ComponentVersions{}
	for _, cv := range collect(t, g, time.Time{}) {
		if _, dup := got[cv.Component]; dup {
			t.Errorf("component %s yielded twice", cv.Component)
		}
		got[cv.Component] = cv
		w, ok := wantComps[cv.Component]
		if !ok {
			t.Errorf("unexpected component %q (non-descriptor package leaked)", cv.Component)
		}
		if cv.Owner != "github:org:acme-corp" {
			t.Errorf("%s: owner = %q", cv.Component, cv.Owner)
		}
		if wantVis := map[bool]Visibility{true: Private, false: Public}[cv.Component == "acme.example/shop/backend"]; cv.Visibility != wantVis {
			t.Errorf("%s: visibility = %q, want %q", cv.Component, cv.Visibility, wantVis)
		}
		if cv.Repository != w.repo {
			t.Errorf("%s: repository = %q, want %q", cv.Component, cv.Repository, w.repo)
		}
	}
	for comp := range wantComps {
		if _, ok := got[comp]; !ok {
			t.Errorf("component %q missing", comp)
		}
	}

	// untagged versions are skipped: version count equals tag count in the fixtures
	for comp, cv := range got {
		base := fixtureBase(wantComps[comp].pkg)
		files := []string{base + ".json"}
		if comp == "acme.example/platform/gateway" {
			files = append(files, base+"-page2.json") // paging is followed
		}
		if want := tagCount(t, files...); len(cv.Versions) != want {
			t.Errorf("%s: %d versions, want %d (tags in fixture)", comp, len(cv.Versions), want)
		}
		for _, v := range cv.Versions {
			if v.Name == "" || strings.HasPrefix(v.Name, "sha256:") || v.PublishedAt.IsZero() {
				t.Errorf("%s: bad version %+v", comp, v)
			}
		}
	}

	if page2.Load() == 0 {
		t.Error("gateway page 2 (Link rel=next) was never requested")
	}

	// nested descriptor packages carry their sub path in the repository
	if cv := got["acme.example/tools/installer"]; cv.Repository != "oci::ghcr.io/acme-corp/ocm" || len(cv.Versions) == 0 {
		t.Errorf("installer = %+v", cv)
	}
	if cv := got["acme.example/platform/agent"]; cv.Repository != "oci::ghcr.io/acme-corp/team-a/ocm" ||
		len(cv.Versions) != 1 || cv.Versions[0].Name != "0.3.0-dev" {
		t.Errorf("agent = %+v", cv)
	}

	// known values from the fixtures
	if pod := got["acme.example/examples/demo"].Versions; len(pod) != 1 ||
		pod[0] != (Version{Name: "1.0.0", PublishedAt: time.Date(2024, 4, 12, 14, 5, 42, 0, time.UTC)}) {
		t.Errorf("demo versions = %+v", pod)
	}
	found := false
	for _, v := range got["acme.example/tools/cli"].Versions {
		if v.Name == "1.5.0-rc.1" {
			found = true
			if want := time.Date(2026, 9, 22, 12, 15, 58, 0, time.UTC); !v.PublishedAt.Equal(want) {
				t.Errorf("cli 1.5.0-rc.1 published %s, want %s", v.PublishedAt, want)
			}
		}
	}
	if !found {
		t.Error("cli 1.5.0-rc.1 missing")
	}
	if hi := got["acme.example/platform/gateway"].Versions; len(hi) != 1 || hi[0].Name != "2.0.0" {
		t.Errorf("gateway versions = %+v", hi)
	}

	// delta: only packages updated after since
	since := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	wantDelta := map[string]bool{}
	for comp, w := range wantComps {
		if w.updated.After(since) {
			wantDelta[comp] = true
		}
	}
	if len(wantDelta) == 0 || len(wantDelta) == len(wantComps) {
		t.Fatalf("since %s does not split the fixture (%d of %d)", since, len(wantDelta), len(wantComps))
	}
	gotDelta := map[string]bool{}
	for _, cv := range collect(t, g, since) {
		gotDelta[cv.Component] = true
	}
	if len(gotDelta) != len(wantDelta) {
		t.Errorf("delta = %v, want %v", gotDelta, wantDelta)
	}
	for c := range wantDelta {
		if !gotDelta[c] {
			t.Errorf("delta missing %s", c)
		}
	}
	if gotDelta["acme.example/examples/demo"] || !gotDelta["acme.example/tools/cli"] {
		t.Errorf("delta = %v", gotDelta)
	}
}

func TestSplitDescriptorPackage(t *testing.T) {
	for _, tc := range []struct {
		name, subPath, comp string
		ok                  bool
	}{
		{"component-descriptors/acme.example/tools/cli", "", "acme.example/tools/cli", true},
		{"ocm/component-descriptors/acme.example/tools/installer", "ocm", "acme.example/tools/installer", true},
		{"team-a/ocm/component-descriptors/acme.example/x", "team-a/ocm", "acme.example/x", true},
		{"a/component-descriptors/b/component-descriptors/c", "a", "b/component-descriptors/c", true},
		{"my-component-descriptors/x", "", "", false},
		{"ocm/my-component-descriptors/x", "", "", false},
		{"component-descriptors/", "", "", false},
		{"ocm/component-descriptors/", "", "", false},
		{"/component-descriptors/x", "", "", false},
		{"cli", "", "", false},
	} {
		sub, comp, ok := splitDescriptorPackage(tc.name)
		if sub != tc.subPath || comp != tc.comp || ok != tc.ok {
			t.Errorf("%q = (%q, %q, %v), want (%q, %q, %v)", tc.name, sub, comp, ok, tc.subPath, tc.comp, tc.ok)
		}
	}
}
