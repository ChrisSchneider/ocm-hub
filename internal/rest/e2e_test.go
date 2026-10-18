package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"

	"chrisschneider.dev/delivery-bundler/internal/connector"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/indexsync"
	"chrisschneider.dev/delivery-bundler/internal/observability"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/rest"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
	"chrisschneider.dev/delivery-bundler/internal/version"
)

type staticConn struct{ found []connector.ComponentVersions }

func (s staticConn) ID() string    { return "static" }
func (s staticConn) Type() string  { return "static/v1" }
func (s staticConn) Key() string   { return "static" }
func (s staticConn) Title() string { return "" }
func (s staticConn) Changed(context.Context, time.Time, connector.PageCache) iter.Seq2[connector.ComponentVersions, error] {
	return func(yield func(connector.ComponentVersions, error) bool) {
		for _, f := range s.found {
			if !yield(f, nil) {
				return
			}
		}
	}
}

type stack struct {
	srv       *httptest.Server
	validator validator.Validator
	main      string // location of the indexed sub path
}

// newStack: e2e, e2e-parent in "main" (indexed); e2e-dep only in "deps",
// reachable through the resolver → a foreign reference.
func newStack(t *testing.T) *stack {
	t.Helper()
	reg := zot.Shared(t)
	ctx := t.Context()
	sub := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	mainT := fixtures.Target{Host: reg.Host, SubPath: sub + "/main"}
	depsT := fixtures.Target{Host: reg.Host, SubPath: sub + "/deps"}
	data := []fixtures.Resource{{Name: "data", Type: "plainText", MediaType: "text/plain", Content: []byte("payload")}}
	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0-rc.1"} {
		fixtures.Push(ctx, t, mainT, fixtures.Component{Name: "acme.org/e2e", Version: v, Resources: data,
			Labels: []descriptorruntime.Label{{Name: "catalog.chrisschneider.dev/title", Value: []byte(`"E2E"`)}}})
	}
	fixtures.Push(ctx, t, depsT, fixtures.Component{Name: "acme.org/e2e-dep", Version: "1.0.0"})
	fixtures.Push(ctx, t, mainT, fixtures.Component{Name: "acme.org/e2e-parent", Version: "1.0.0",
		References: []fixtures.Reference{{Name: "dep", Component: "acme.org/e2e-dep", Version: "1.0.0"}}})

	cfg, err := ocm.LoadOCMConfig(fixtures.WriteOCMConfig(t, []fixtures.Resolver{{Pattern: "acme.org/*", Host: reg.Host, SubPath: sub + "/deps"}}, nil))
	must(t, err)
	b, err := ocm.NewBindings(ctx, cfg, t.TempDir())
	must(t, err)
	r, err := ocm.NewResolver(ctx, b)
	must(t, err)
	svc := ocm.NewService(b, r, 30*time.Second)
	main, err := ocm.LocationKey("oci::http://" + reg.Host + "/" + sub + "/main")
	must(t, err)
	d := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }
	conn := staticConn{found: []connector.ComponentVersions{
		{Component: "acme.org/e2e", Repository: main, Versions: []connector.Version{{Name: "1.0.0", PublishedAt: d(1)}, {Name: "1.1.0", PublishedAt: d(2)}, {Name: "2.0.0-rc.1", PublishedAt: d(3)}}},
		{Component: "acme.org/e2e-parent", Repository: main, Versions: []connector.Version{{Name: "1.0.0", PublishedAt: d(1)}}},
	}}
	db := filepath.Join(t.TempDir(), "hub.duckdb")
	cl, _ := version.NewClassifier(nil)
	rep, err := indexsync.Run(ctx, indexsync.Options{DBPath: db, Connectors: []connector.Connector{conn}, Classifier: cl, Fetcher: svc})
	if err != nil || rep.Failed() {
		t.Fatalf("sync: %+v %v", rep, err)
	}
	h := index.NewHandle(db)
	t.Cleanup(func() { h.Close() })
	srv := httptest.NewServer(observability.Middleware(rest.NewHandler(svc, h), observability.NewLogger(io.Discard, slog.LevelError)))
	t.Cleanup(srv.Close)
	spec, err := os.ReadFile("../../api/openapi.yaml")
	must(t, err)
	doc, err := libopenapi.NewDocument(spec)
	must(t, err)
	v, errs := validator.NewValidator(doc)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return &stack{srv: srv, validator: v, main: main}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// get performs a GET and validates request and response against the OpenAPI
// contract. Path templates cannot match names containing "%2F", so validation
// uses a request whose component segment is replaced by a slash-free placeholder.
func (s *stack) get(t *testing.T, path string, hdr http.Header) (*http.Response, []byte) {
	t.Helper()
	return s.do(t, "GET", path, hdr, "", false)
}

// do performs a request, optionally without following redirects, and validates
// request and response against the OpenAPI contract.
func (s *stack) do(t *testing.T, method, path string, hdr http.Header, body string, noRedirect bool) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.srv.URL+path, rd)
	must(t, err)
	for k, v := range hdr {
		req.Header[k] = v
	}
	client := http.DefaultClient
	if noRedirect {
		client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(req)
	must(t, err)
	rb, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	must(t, err)
	vreq := req.Clone(context.Background())
	segs := strings.Split(req.URL.EscapedPath(), "/")
	if len(segs) > 3 && segs[2] == "components" && strings.Contains(segs[3], "%2F") {
		segs[3] = "component"
	}
	if p, err := url.PathUnescape(strings.Join(segs, "/")); err == nil {
		vreq.URL.Path, vreq.URL.RawPath = p, ""
	}
	if ok, errs := s.validator.ValidateHttpRequest(vreq); !ok {
		for _, e := range errs {
			t.Errorf("contract request: %s: %s", e.Message, e.Reason)
		}
	}
	vresp := *resp
	vresp.Body = io.NopCloser(bytes.NewReader(rb))
	if ok, errs := s.validator.ValidateHttpResponse(vreq, &vresp); !ok {
		for _, e := range errs {
			t.Errorf("contract response: %s: %s", e.Message, e.Reason)
		}
	}
	return resp, rb
}

func TestE2EList(t *testing.T) {
	s := newStack(t)
	r, b := s.get(t, "/v1/components?q=e2e", nil)
	if r.StatusCode != 200 || !strings.Contains(string(b), `"title":"E2E"`) || !strings.Contains(string(b), `"latest":"1.1.0"`) {
		t.Fatalf("%d %s", r.StatusCode, b)
	}
}

func TestE2EVersionsNewestFirst(t *testing.T) {
	s := newStack(t)
	_, b := s.get(t, "/v1/components/acme.org%2Fe2e/versions?includePrerelease=true", nil)
	var vl struct {
		Versions []struct{ Version string } `json:"versions"`
	}
	must(t, json.Unmarshal(b, &vl))
	var got []string
	for _, v := range vl.Versions {
		got = append(got, v.Version)
	}
	if want := []string{"2.0.0-rc.1", "1.1.0", "1.0.0"}; !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v: %s", got, want, b)
	}
}

func TestE2ELatestAndDescriptor(t *testing.T) {
	s := newStack(t)
	resp, _ := s.do(t, "GET", "/v1/components/acme.org%2Fe2e/descriptors/latest", nil, "", true)
	if resp.StatusCode != 307 || !strings.HasSuffix(resp.Header.Get("Location"), "/descriptors/1.1.0") {
		t.Fatalf("%d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	r, b := s.get(t, "/v1/components/acme.org%2Fe2e/descriptors/1.1.0", http.Header{"Accept": {"application/json"}})
	if r.StatusCode != 200 || !strings.Contains(string(b), `"version":"1.1.0"`) {
		t.Fatalf("%d %s", r.StatusCode, b)
	}
}

func TestE2ETreeMarksForeignLocation(t *testing.T) {
	s := newStack(t)
	r, b := s.get(t, "/v1/components/acme.org%2Fe2e-parent/descriptors/1.0.0?recursive=-1&compact=true", http.Header{"Accept": {"application/json"}})
	if r.StatusCode != 200 {
		t.Fatalf("%d %s", r.StatusCode, b)
	}
	var list []struct {
		Location   string `json:"location"`
		Descriptor struct {
			Component struct{ Name string } `json:"component"`
		} `json:"descriptor"`
	}
	must(t, json.Unmarshal(b, &list))
	loc := map[string]string{}
	for _, e := range list {
		loc[e.Descriptor.Component.Name] = e.Location
	}
	parent, dep := loc["acme.org/e2e-parent"], loc["acme.org/e2e-dep"]
	if parent != s.main || !strings.HasSuffix(dep, "/deps") || parent == dep {
		t.Fatalf("parent %q (want %q), dep %q (want .../deps, different): %s", parent, s.main, dep, b)
	}
}

func TestE2EResource(t *testing.T) {
	s := newStack(t)
	r, b := s.get(t, "/v1/components/acme.org%2Fe2e/resources/1.0.0/data", nil)
	if r.StatusCode != 200 || string(b) != "payload" {
		t.Fatalf("%d %q", r.StatusCode, b)
	}
}

func TestE2EDiscoveryStub(t *testing.T) {
	s := newStack(t)
	resp, _ := s.do(t, "POST", "/v1/discovery", http.Header{"Content-Type": {"application/json"}}, `{}`, false)
	if resp.StatusCode != 501 {
		t.Fatalf("%d", resp.StatusCode)
	}
}

func TestE2EOverviewAndTopLevel(t *testing.T) {
	s := newStack(t)
	r, b := s.get(t, "/v1/components/acme.org%2Fe2e/overview/1.1.0", nil)
	if r.StatusCode != 200 {
		t.Fatalf("overview: %d %s", r.StatusCode, b)
	}
	var ov struct {
		Title      string
		Resources  []struct{ Name, Type string }
		Signatures []json.RawMessage
	}
	must(t, json.Unmarshal(b, &ov))
	if ov.Title != "E2E" || len(ov.Resources) != 1 || ov.Resources[0].Name != "data" || ov.Resources[0].Type != "plainText" || ov.Signatures == nil {
		t.Fatalf("overview %q resources=%v: %s", ov.Title, ov.Resources, b)
	}
	r, b = s.get(t, "/v1/components?topLevel=true", nil)
	if r.StatusCode != 200 || !strings.Contains(string(b), `"name":"acme.org/e2e-parent"`) {
		t.Fatalf("topLevel: %d %s", r.StatusCode, b)
	}
}
