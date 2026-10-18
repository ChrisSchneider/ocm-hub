package connector

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type entServer struct {
	srv          *httptest.Server
	graphqlCalls atomic.Int32
	failOrg      string       // org whose package list answers 403
	corpListed   atomic.Int32 // requests to acme-corp's package list
	graphqlBody  string       // when set, served instead of the fixtures
}

func newEntServer(t *testing.T) *entServer {
	t.Helper()
	e := &entServer{}
	fixture := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", "github", "enterprise", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		e.graphqlCalls.Add(1)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &req); err != nil || req.Variables["slug"] != "acme-enterprise" {
			t.Errorf("graphql request %s", b)
		}
		if e.graphqlBody != "" {
			io.WriteString(w, e.graphqlBody)
			return
		}
		if req.Variables["after"] == "Y3Vyc29yOjE=" {
			w.Write(fixture("graphql-page2.json"))
			return
		}
		w.Write(fixture("graphql-page1.json"))
	})
	mux.HandleFunc("GET /orgs/{org}/packages", func(w http.ResponseWriter, r *http.Request) {
		org := r.PathValue("org")
		if org == "acme-corp" {
			e.corpListed.Add(1)
		}
		if org == e.failOrg {
			http.Error(w, `{"message":"Resource protected by organization SAML enforcement."}`, http.StatusForbidden)
			return
		}
		w.Write(fixture(org + "-packages.json"))
	})
	mux.HandleFunc("GET /orgs/{org}/packages/container/{pkg}/versions", func(w http.ResponseWriter, r *http.Request) {
		w.Write(fixture("versions.json"))
	})
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

func (e *entServer) gh() *GitHub {
	return NewGitHub("id", Owner{Enterprise: "acme-enterprise"}, "tok", WithAPIURL(e.srv.URL), allVisibilities)
}

func TestEnterpriseDescribe(t *testing.T) {
	e := newEntServer(t)
	src, err := e.gh().Describe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if src.Title != "Acme Enterprise" || !strings.Contains(src.AvatarURL, "90000010") || e.graphqlCalls.Load() != 2 {
		t.Fatalf("src = %+v, calls %d", src, e.graphqlCalls.Load())
	}
	want := []OwnerInfo{
		{Key: "github:org:acme-labs", Title: "Acme Labs", AvatarURL: "https://avatars.githubusercontent.com/u/90000012?s=32&v=4", Parent: "github:enterprise:acme-enterprise"},
		{Key: "github:org:acme-corp", Title: "Acme Corporation", AvatarURL: "https://avatars.githubusercontent.com/u/90000001?s=32&v=4", Parent: "github:enterprise:acme-enterprise"},
	}
	if len(src.Owners) != 2 || src.Owners[0] != want[0] || src.Owners[1] != want[1] {
		t.Fatalf("owners = %+v", src.Owners)
	}
	if src.Scope != scopeOf([]string{"acme-corp", "acme-labs"}) || src.Scope == "" {
		t.Fatalf("scope = %q", src.Scope)
	}
}

func TestEnterpriseScopeIgnoresOrder(t *testing.T) {
	if scopeOf([]string{"b", "A"}) != scopeOf([]string{"a", "b"}) {
		t.Fatal("scope depends on order or case")
	}
}

func TestEnterpriseChangedListsEveryOrg(t *testing.T) {
	e := newEntServer(t)
	g := e.gh()
	if _, err := g.Describe(t.Context()); err != nil {
		t.Fatal(err)
	}
	calls := e.graphqlCalls.Load()
	got := collectWith(t, g, time.Time{}, nil)
	if len(got) != 2 || e.graphqlCalls.Load() != calls {
		t.Fatalf("got %+v (graphql calls after Describe: %d)", got, e.graphqlCalls.Load()-calls)
	}
	byOwner := map[string]ComponentVersions{}
	for _, cv := range got {
		byOwner[cv.Owner] = cv
	}
	if c := byOwner["github:org:acme-corp"]; c.Repository != "oci::ghcr.io/acme-corp" || c.Component != "acme.example/corp-tool" || c.Visibility != Public {
		t.Fatalf("acme-corp = %+v", c)
	}
	if c := byOwner["github:org:acme-labs"]; c.Repository != "oci::ghcr.io/acme-labs" || c.Visibility != Internal {
		t.Fatalf("acme-labs = %+v", c)
	}
}

func TestEnterpriseChangedResolvesOrgsWithoutDescribe(t *testing.T) {
	e := newEntServer(t)
	if got := collectWith(t, e.gh(), time.Time{}, nil); len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestEnterpriseNotVisible(t *testing.T) {
	e := newEntServer(t)
	e.graphqlBody = `{"data":{"enterprise":null}}`
	_, err := e.gh().Describe(t.Context())
	if err == nil || err.Error() != "enterprise acme-enterprise not visible: the GitHub token needs read:enterprise and enterprise membership" {
		t.Fatalf("err = %v", err)
	}
	for _, err := range e.gh().Changed(t.Context(), time.Time{}, nil) {
		if err == nil || !strings.Contains(err.Error(), "not visible") {
			t.Fatalf("Changed err = %v", err)
		}
		return
	}
	t.Fatal("Changed yielded nothing")
}

func TestEnterpriseGraphQLErrors(t *testing.T) {
	e := newEntServer(t)
	e.graphqlBody = `{"data":{"enterprise":null},"errors":[{"type":"INSUFFICIENT_SCOPES","message":"Your token has not been granted the required scopes"}]}`
	if _, err := e.gh().Describe(t.Context()); err == nil || !strings.Contains(err.Error(), "required scopes") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnterpriseOrgFailureFailsRun(t *testing.T) {
	e := newEntServer(t)
	e.failOrg = "acme-labs" // first in GraphQL order
	var last error
	for _, err := range e.gh().Changed(t.Context(), time.Time{}, nil) {
		last = err
	}
	if last == nil || !strings.HasPrefix(last.Error(), "org acme-labs: ") {
		t.Fatalf("err = %v", last)
	}
	if e.corpListed.Load() != 0 {
		t.Fatal("run continued to acme-corp after the failure")
	}
}

func TestOrgDescribeReportsItsOwner(t *testing.T) {
	srv := describeServer(t) // from github_describe_test.go
	src, err := NewGitHub("id", Owner{Org: "acme-corp"}, "", WithAPIURL(srv.URL), WithVisibility(Public)).Describe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if src.Scope != "" || len(src.Owners) != 1 || src.Owners[0].Key != "github:org:acme-corp" || src.Owners[0].Parent != "" ||
		src.Owners[0].Title != "Acme Corporation" || len(src.Visibility) != 1 {
		t.Fatalf("src = %+v", src)
	}
}

func TestEnterprisePagingMustAdvance(t *testing.T) {
	e := newEntServer(t)
	e.graphqlBody = `{"data":{"enterprise":{"name":"Acme Enterprise","avatarUrl":"","organizations":{"nodes":[{"login":"acme-corp","name":null,"avatarUrl":""}],"pageInfo":{"hasNextPage":true,"endCursor":null}}}}}`
	_, err := e.gh().Describe(t.Context())
	if err == nil || err.Error() != "enterprise acme-enterprise: GraphQL paging did not advance" {
		t.Fatalf("err = %v", err)
	}
}
