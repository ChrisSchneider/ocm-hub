package rest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/overview"
	"chrisschneider.dev/delivery-bundler/internal/rest/gen"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"oras.land/oras-go/v2/registry/remote"
)

var (
	_ Service = (*ocm.Service)(nil)
	_ Index   = (*index.Handle)(nil)
)

type fakeService struct {
	desc       *descriptorruntime.Descriptor
	tree       []ocm.Located
	resource   *ocm.Resource
	err        error
	allowed    map[string]bool // CheckRepository
	gotRef     ocm.Ref
	gotID      ocmruntime.Identity
	gotBody    *bool
	gotHasCall bool
}

func (f *fakeService) GetComponentVersion(_ context.Context, ref ocm.Ref) (*descriptorruntime.Descriptor, error) {
	f.gotRef = ref
	return f.desc, f.err
}

func (f *fakeService) DescriptorTreeAt(ctx context.Context, ref ocm.Ref, has ocm.HasVersionAt) ([]ocm.Located, error) {
	f.gotRef = ref
	if has != nil {
		ok, _ := has(ctx, "acme.org/x", "1.0.0", otherLoc)
		f.gotHasCall = ok
	}
	return f.tree, f.err
}

func (f *fakeService) OpenResource(_ context.Context, ref ocm.Ref, id ocmruntime.Identity, withBody bool) (*ocm.Resource, error) {
	f.gotRef, f.gotID, f.gotBody = ref, id, &withBody
	if f.err != nil || f.resource == nil {
		return nil, f.err
	}
	return f.resource, nil
}

func (f *fakeService) CheckRepository(repo string) error {
	if f.allowed[repo] {
		return nil
	}
	return ocm.ErrRepositoryNotAllowed
}

func do(t *testing.T, h http.Handler, method, target string, hdr http.Header) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func body(t *testing.T, r *http.Response) string {
	t.Helper()
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

func decode[T any](t *testing.T, r *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func errCode(t *testing.T, r *http.Response) string {
	t.Helper()
	v := decode[struct{ Errors []struct{ Code string } }](t, r)
	if len(v.Errors) == 0 {
		return ""
	}
	return v.Errors[0].Code
}

func TestListComponents(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	r := do(t, h, "GET", "/v1/components?q=x&location="+testLoc, nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d: %s", r.StatusCode, body(t, r))
	}
	v := decode[struct {
		Components []struct {
			Name, Latest, Title string
			VersionCount        int
			Locations           []string
		}
		Next   *string
		Facets struct {
			Connectors []struct {
				Key, Title string
				Count      int
			}
		}
	}](t, r)
	if len(v.Components) != 2 || v.Components[0].Name != "acme.org/x" || v.Components[0].Latest != "1.1.0" ||
		v.Components[0].VersionCount != 3 || len(v.Components[0].Locations) != 3 || v.Next != nil {
		t.Fatalf("body = %+v", v)
	}
}

func TestListComponentsPagingAndETag(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	r := do(t, h, "GET", "/v1/components?limit=1&sort=name", nil)
	v := decode[struct{ Next string }](t, r)
	if v.Next == "" {
		t.Fatal("want next cursor")
	}
	etag := r.Header.Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) || r.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("headers %v", r.Header)
	}
	r = do(t, h, "GET", "/v1/components?limit=1&sort=name", http.Header{"If-None-Match": {etag}})
	if r.StatusCode != 304 {
		t.Fatalf("status %d", r.StatusCode)
	}
	r = do(t, h, "GET", "/v1/components?limit=1&sort=name&cursor="+v.Next, nil)
	if r.StatusCode != 200 {
		t.Fatalf("page 2: %d", r.StatusCode)
	}
}

func TestListComponentsBadParams(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	for _, q := range []string{"limit=0", "limit=201", "limit=x", "sort=size", "cursor=!!", "updatedSince=yesterday", "includePrerelease=maybe"} {
		r := do(t, h, "GET", "/v1/components?"+q, nil)
		if r.StatusCode != 400 || errCode(t, r) != "PARAMETER_INVALID" {
			t.Errorf("%s: %d", q, r.StatusCode)
		}
	}
}

func TestComponentOverview(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	r := do(t, h, "GET", "/v1/components/acme.org%2Fx", nil)
	v := decode[struct {
		Name, Latest, Title, Provider string
		Locations                     []struct{ Repository, Latest string }
		Links                         struct{ Versions, Latest string }
	}](t, r)
	if v.Latest != "1.1.0" || v.Title != "X" || len(v.Locations) != 3 ||
		v.Links.Versions != "/v1/components/acme.org%2Fx/versions" || v.Links.Latest != "/v1/components/acme.org%2Fx/descriptors/latest" {
		t.Fatalf("body = %+v", v)
	}
	r = do(t, h, "GET", "/v1/components/nope.org/x", nil)
	if r.StatusCode != 404 || errCode(t, r) != "COMPONENT_UNKNOWN" {
		t.Fatalf("unknown: %d", r.StatusCode)
	}
}

func TestVersions(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	r := do(t, h, "GET", "/v1/components/acme.org/x/versions?includePrerelease=true", nil)
	v := decode[struct {
		Name     string
		Versions []struct {
			Version   string
			Stable    bool
			Locations []string
		}
	}](t, r)
	if v.Name != "acme.org/x" || len(v.Versions) != 3 || v.Versions[0].Version != "2.0.0-rc.1" || v.Versions[0].Stable {
		t.Fatalf("body = %+v", v)
	}
	r = do(t, h, "GET", "/v1/components/acme.org/x/versions?semverConstraint=>=bad", nil)
	if r.StatusCode != 400 {
		t.Fatalf("bad constraint: %d", r.StatusCode)
	}
}

func TestDescriptorLatestRedirectAndLocation(t *testing.T) {
	f := &fakeService{desc: descriptorOf("acme.org/x", "1.0.0")}
	h := NewHandler(f, testIndex(t))
	r := do(t, h, "GET", "/v1/components/acme.org/x/descriptors/latest?compact=true", nil)
	if r.StatusCode != 307 || r.Header.Get("Location") != "/v1/components/acme.org%2Fx/descriptors/1.1.0?compact=true" {
		t.Fatalf("redirect %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	r = do(t, h, "GET", "/v1/components/acme.org/x/descriptors/1.0.0", http.Header{"Accept": {"application/json"}})
	if r.StatusCode != 200 || f.gotRef.Repository != testLoc { // overview location first
		t.Fatalf("status %d repo %q", r.StatusCode, f.gotRef.Repository)
	}
	r = do(t, h, "GET", "/v1/components/acme.org/x/descriptors/9.9.9", nil)
	if r.StatusCode != 404 || errCode(t, r) != "VERSION_UNKNOWN" {
		t.Fatalf("unknown version: %d", r.StatusCode)
	}
}

func TestDescriptorRepositoryPin(t *testing.T) {
	f := &fakeService{desc: descriptorOf("acme.org/x", "1.0.0"), allowed: map[string]bool{"oci::registry.example/ocm": true}}
	h := NewHandler(f, testIndex(t))
	for target, want := range map[string]int{
		"/v1/components/acme.org/x/descriptors/1.0.0?repository=" + otherLoc:                  200, // indexed location
		"/v1/components/acme.org/nope/descriptors/1.0.0?repository=oci::registry.example/ocm": 200, // resolver repo, not indexed
		"/v1/components/acme.org/x/descriptors/1.0.0?repository=oci::evil.example/x":          400,
		"/v1/components/acme.org/x/descriptors/1.0.0?repository=::::":                         400,
	} {
		r := do(t, h, "GET", target, nil)
		if r.StatusCode != want {
			t.Errorf("%s: %d, want %d", target, r.StatusCode, want)
		}
	}
}

func TestDescriptorTreeShape(t *testing.T) {
	d := descriptorOf("acme.org/x", "1.0.0")
	f := &fakeService{tree: []ocm.Located{{Location: testLoc, Descriptor: d}}}
	h := NewHandler(f, testIndex(t))
	r := do(t, h, "GET", "/v1/components/acme.org/x/descriptors/1.0.0?recursive=-1", http.Header{"Accept": {"application/json"}})
	v := decode[[]struct {
		Location   string
		Descriptor struct{ Component struct{ Name string } }
	}](t, r)
	if len(v) != 1 || v[0].Location != testLoc || v[0].Descriptor.Component.Name != "acme.org/x" {
		t.Fatalf("body = %+v", v)
	}
	if !f.gotHasCall {
		t.Fatal("handler must pass the index's HasVersionAt")
	}
}

func TestIndexUnavailable(t *testing.T) {
	h := NewHandler(&fakeService{}, noIndex(t))
	for _, p := range []string{"/v1/components", "/v1/components/acme.org/x", "/v1/components/acme.org/x/versions",
		"/v1/components/acme.org/x/descriptors/1.0.0"} {
		r := do(t, h, "GET", p, nil)
		if r.StatusCode != 503 || errCode(t, r) != "INDEX_UNAVAILABLE" {
			t.Errorf("%s: %d", p, r.StatusCode)
		}
	}
}

func TestDiscoveryStubAndMethods(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	r := do(t, h, "POST", "/v1/discovery", nil)
	if r.StatusCode != 501 || errCode(t, r) != "NOT_IMPLEMENTED" {
		t.Fatalf("discovery: %d", r.StatusCode)
	}
	r = do(t, h, "GET", "/v1/discovery", nil)
	if r.StatusCode != 405 || r.Header.Get("Allow") != "POST" {
		t.Fatalf("GET discovery: %d %q", r.StatusCode, r.Header.Get("Allow"))
	}
	r = do(t, h, "POST", "/v1/components", nil)
	if r.StatusCode != 405 || r.Header.Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST components: %d", r.StatusCode)
	}
}

// The tests below are carried over from the pre-index handler tests: the
// descriptor encoding, conditional requests and error mapping are unchanged.

func descriptorOf(name, version string) *descriptorruntime.Descriptor {
	return &descriptorruntime.Descriptor{
		Meta: descriptorruntime.Meta{Version: "v2"},
		Component: descriptorruntime.Component{
			ComponentMeta: descriptorruntime.ComponentMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: name, Version: version}},
			Provider:      descriptorruntime.Provider{Name: "acme.org"},
		},
	}
}

func etagOf(b string) string {
	sum := sha256.Sum256([]byte(b))
	return `"sha256:` + hex.EncodeToString(sum[:]) + `"`
}

func TestBaseAndHealth(t *testing.T) {
	h := NewHandler(&fakeService{}, noIndex(t))
	if r := do(t, h, "GET", "/v1/", nil); r.StatusCode != 200 || body(t, r) != "{}" {
		t.Fatalf("base: %d", r.StatusCode)
	}
	if r := do(t, h, "GET", "/healthz", nil); r.StatusCode != 200 {
		t.Fatalf("health: %d", r.StatusCode)
	}
	if r := do(t, h, "POST", "/v1/", nil); r.StatusCode != 405 || errCode(t, r) != "METHOD_NOT_ALLOWED" {
		t.Fatalf("post: %d", r.StatusCode)
	}
	for _, p := range []string{"/nope", "/v1/_catalog", "/v1/acme.org/x/versions/list"} {
		if r := do(t, h, "GET", p, nil); r.StatusCode != 404 {
			t.Fatalf("%s: %d", p, r.StatusCode)
		}
	}
	if r := do(t, h, "GET", "/v1/components/NOPE/x", nil); r.StatusCode != 400 || errCode(t, r) != "NAME_INVALID" {
		t.Fatalf("invalid name: %d", r.StatusCode)
	}
}

func TestDescriptorNegotiationAndConditional(t *testing.T) {
	h := NewHandler(&fakeService{desc: descriptorOf("acme.org/x", "1.0.0")}, testIndex(t))
	const p = "/v1/components/acme.org/x/descriptors/1.0.0"
	r := do(t, h, "GET", p, nil)
	b := body(t, r)
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/yaml" || !strings.Contains(b, "name: acme.org/x") ||
		r.Header.Get("ETag") != etagOf(b) || r.Header.Get("Vary") != "Accept" || r.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("yaml: %d %v %s", r.StatusCode, r.Header, b)
	}
	yamlETag := r.Header.Get("ETag")
	jh := http.Header{"Accept": {"application/json"}}
	r = do(t, h, "GET", p, jh)
	if jb := body(t, r); r.Header.Get("Content-Type") != "application/json" || !strings.HasPrefix(jb, "{") || r.Header.Get("ETag") == yamlETag {
		t.Fatalf("json: %q %s", r.Header.Get("Content-Type"), jb)
	}
	if r := do(t, h, "GET", p, http.Header{"Accept": {"text/html"}}); r.StatusCode != 406 || errCode(t, r) != "NOT_ACCEPTABLE" {
		t.Fatalf("html: %d", r.StatusCode)
	}
	r = do(t, h, "HEAD", p, nil)
	if r.StatusCode != 200 || body(t, r) != "" || r.Header.Get("Content-Length") == "" || r.Header.Get("ETag") != yamlETag {
		t.Fatalf("head: %d %v", r.StatusCode, r.Header)
	}
	for _, inm := range []string{yamlETag, "W/" + yamlETag, `"other", ` + yamlETag, "*"} {
		r := do(t, h, "GET", p, http.Header{"If-None-Match": {inm}})
		if r.StatusCode != 304 || body(t, r) != "" || r.Header.Get("ETag") != yamlETag {
			t.Fatalf("If-None-Match %q: %d", inm, r.StatusCode)
		}
	}
	if r := do(t, h, "GET", p, http.Header{"If-None-Match": {yamlETag}, "Accept": {"application/json"}}); r.StatusCode != 200 {
		t.Fatalf("YAML ETag must not match JSON: %d", r.StatusCode)
	}
}

func TestDescriptorParams(t *testing.T) {
	h := NewHandler(&fakeService{desc: descriptorOf("acme.org/x", "1.0.0")}, testIndex(t))
	for _, q := range []string{"recursive=1", "recursive=-2", "recursive=x", "compact=yes"} {
		r := do(t, h, "GET", "/v1/components/acme.org/x/descriptors/1.0.0?"+q, nil)
		if r.StatusCode != 400 || errCode(t, r) != "PARAMETER_INVALID" {
			t.Errorf("%s: %d", q, r.StatusCode)
		}
	}
	r := do(t, h, "GET", "/v1/components/acme.org/x/descriptors/latest?recursive=-1", nil)
	if r.StatusCode != 307 || r.Header.Get("Location") != "/v1/components/acme.org%2Fx/descriptors/1.1.0?recursive=-1" ||
		r.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("latest: %d %v", r.StatusCode, r.Header)
	}
	if r := do(t, h, "GET", "/v1/components/nope.org/x/descriptors/latest", nil); r.StatusCode != 404 || errCode(t, r) != "COMPONENT_UNKNOWN" {
		t.Fatalf("latest of unknown: %d", r.StatusCode)
	}
}

func TestDescriptorCompact(t *testing.T) {
	full := descriptorOf("acme.org/x", "1.0.0")
	full.Component.Resources = []descriptorruntime.Resource{{ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: "r", Version: "1.0.0"}}, Type: "blob", Access: &ocmruntime.Raw{Type: ocmruntime.NewUnversionedType("localBlob"), Data: []byte(`{"type":"localBlob"}`)}}}
	full.Component.Labels = []descriptorruntime.Label{{Name: "catalog.chrisschneider.dev/title", Value: []byte(`"P"`)}}
	h := NewHandler(&fakeService{desc: full, tree: []ocm.Located{{Location: testLoc, Descriptor: full}}}, testIndex(t))
	jh := http.Header{"Accept": {"application/json"}}
	b := body(t, do(t, h, "GET", "/v1/components/acme.org/x/descriptors/1.0.0?compact=true", jh))
	if strings.Contains(b, `"resources":[`) || !strings.Contains(b, `"labels":[`) {
		t.Errorf("compact: %s", b)
	}
	b = body(t, do(t, h, "GET", "/v1/components/acme.org/x/descriptors/1.0.0?compact=true&recursive=-1", jh))
	if strings.Contains(b, `"resources":[`) || !strings.HasPrefix(b, `[{"location":`) {
		t.Errorf("compact tree: %s", b)
	}
	if len(full.Component.Resources) != 1 {
		t.Error("compact mutated the service's descriptor")
	}
	if b := body(t, do(t, h, "GET", "/v1/components/acme.org/x/descriptors/1.0.0", jh)); !strings.Contains(b, `"resources":[{`) {
		t.Errorf("full: %s", b)
	}
}

func TestErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		code   string
	}{
		{ocm.ErrComponentUnknown, 404, "COMPONENT_UNKNOWN"},
		{ocm.ErrVersionUnknown, 404, "VERSION_UNKNOWN"},
		{ocm.ErrRepositoryNotAllowed, 400, "REPOSITORY_NOT_ALLOWED"},
		{ocm.ErrRepositoryInvalid, 400, "REPOSITORY_INVALID"},
		{ocm.ErrUpstreamTimeout, 504, "UPSTREAM_TIMEOUT"},
		{fmt.Errorf("first read: %w", context.DeadlineExceeded), 504, "UPSTREAM_TIMEOUT"},
		{ocm.ErrUpstream, 502, "UPSTREAM_ERROR"},
		{errors.New("anything else"), 502, "UPSTREAM_ERROR"},
		{ocm.ErrAccessTypeUnsupported, 501, "ACCESS_TYPE_UNSUPPORTED"},
		{ocm.ErrResourceUnknown, 404, "RESOURCE_UNKNOWN"},
		{&ocm.AmbiguousResourceError{}, 409, "RESOURCE_AMBIGUOUS"},
		{index.ErrBadCursor, 400, "PARAMETER_INVALID"},
	} {
		r := do(t, NewHandler(&fakeService{err: tt.err}, testIndex(t)), "GET", "/v1/components/acme.org/x/descriptors/1.0.0", nil)
		if r.StatusCode != tt.status || errCode(t, r) != tt.code {
			t.Errorf("%v: %d", tt.err, r.StatusCode)
		}
	}
	r := do(t, NewHandler(&fakeService{err: ocm.ErrComponentUnknown}, testIndex(t)), "HEAD", "/v1/components/acme.org/x/descriptors/1.0.0", nil)
	if r.StatusCode != 404 || body(t, r) != "" || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("HEAD error must have no body: %d", r.StatusCode)
	}
}

func TestRequestID(t *testing.T) {
	inner := NewHandler(&fakeService{}, noIndex(t))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-1")
		inner.ServeHTTP(w, r)
	})
	v := decode[struct {
		Errors []struct{ Detail map[string]any }
	}](t, do(t, h, "GET", "/v1/components", nil))
	if len(v.Errors) != 1 || v.Errors[0].Detail["requestId"] != "req-1" {
		t.Fatalf("errors = %+v", v.Errors)
	}
}

func TestVersionOverview(t *testing.T) {
	d := descriptorOf("acme.org/x", "1.1.0")
	d.Signatures = []descriptorruntime.Signature{{
		Name:      "default",
		Digest:    descriptorruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "jsonNormalisation/v4alpha1", Value: "abc123"},
		Signature: descriptorruntime.SignatureInfo{Algorithm: "RSASSA-PSS", Value: "c2ln", MediaType: "application/vnd.ocm.signature.rsa"},
	}}
	d.Component.Labels = []descriptorruntime.Label{
		{Name: "catalog.chrisschneider.dev/title", Value: []byte(`"X"`)},
		{Name: "catalog.chrisschneider.dev/kubernetes-compatibility", Version: "v1", Value: []byte(`{"constraint": ">=1.30 <1.32"}`)},
		{Name: "security.ocm.software/risk-profile", Version: "v1", Value: []byte(`{"network_exposure": "public"}`)},
		{Name: "odg.ocm.software/responsibles", Version: "v1", Value: []byte(`[{"type": "codeowners"}]`)},
	}
	d.Component.Resources = []descriptorruntime.Resource{
		{ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: "image", Version: "1.1.0"}, ExtraIdentity: map[string]string{"arch": "arm64"}}, Type: "ociImage", Access: &ocmruntime.Raw{Type: ocmruntime.NewUnversionedType("ociArtifact"), Data: []byte(`{"type":"ociArtifact","imageReference":"ghcr.io/acme/x:1.1.0"}`)}, Relation: descriptorruntime.ExternalRelation},
		{ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: "image", Version: "1.1.0"}, ExtraIdentity: map[string]string{"arch": "amd64"}}, Type: "ociImage", Access: &ocmruntime.Raw{Type: ocmruntime.NewUnversionedType("ociArtifact"), Data: []byte(`{"type":"ociArtifact","imageReference":"ghcr.io/acme/x:1.1.0"}`)}, Relation: descriptorruntime.ExternalRelation},
	}
	d.Component.Sources = []descriptorruntime.Source{
		{ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: "src", Version: "1.1.0"}}, Type: "git", Access: &ocmruntime.Raw{Type: ocmruntime.NewUnversionedType("gitHub"), Data: []byte(`{"type":"gitHub","repoUrl":"https://github.com/acme/x","commit":"abc"}`)}},
	}
	d.Component.References = []descriptorruntime.Reference{
		{ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: "db", Version: "2.0.0"}}, Component: "acme.org/db"},
	}
	svc := &fakeService{desc: d}
	h := NewHandler(svc, testIndex(t), WithOverviewOptions(overview.Options{KubernetesVersions: []string{"1.29", "1.30", "1.31", "1.32"}}))

	r := do(t, h, "GET", "/v1/components/acme.org%2Fx/overview/latest", nil)
	if r.StatusCode != 307 || r.Header.Get("Location") != "/v1/components/acme.org%2Fx/overview/1.1.0" {
		t.Fatalf("latest: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	r = do(t, h, "GET", "/v1/components/acme.org%2Fx/overview/1.1.0", nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d: %s", r.StatusCode, body(t, r))
	}
	got := decode[map[string]any](t, r)
	if got["title"] != "X" || got["signed"] != true || got["repository"] != testLoc || got["version"] != "1.1.0" {
		t.Fatalf("got %v", got)
	}
	if got["publishedAt"] != "2026-01-02T00:00:00Z" {
		t.Fatalf("publishedAt = %v", got["publishedAt"])
	}
	k := got["kubernetes"].(map[string]any)
	if fmt.Sprint(k["versions"]) != "[1.30 1.31]" {
		t.Fatalf("kubernetes = %v", k)
	}
	if got["riskProfile"].(map[string]any)["networkExposure"] != "public" {
		t.Fatalf("riskProfile = %v", got["riskProfile"])
	}
	c := got["contents"].(map[string]any)
	if fmt.Sprint(c["resources"]) != "[map[count:2 type:ociImage]]" || c["references"] != float64(1) {
		t.Fatalf("contents = %v", c)
	}
	rs := got["resources"].([]any)
	if len(rs) != 2 {
		t.Fatalf("resources = %v", got["resources"])
	}
	r0 := rs[0].(map[string]any)
	if r0["name"] != "image" || r0["type"] != "ociImage" || r0["relation"] != "external" || fmt.Sprint(r0["extraIdentity"]) != "map[arch:arm64]" {
		t.Fatalf("resources[0] = %v", r0)
	}
	refs := got["references"].([]any)
	if len(refs) != 1 {
		t.Fatalf("references = %v", got["references"])
	}
	if f0 := refs[0].(map[string]any); f0["name"] != "db" || f0["version"] != "2.0.0" || f0["componentName"] != "acme.org/db" {
		t.Fatalf("references[0] = %v", f0)
	}
	srcs := got["sources"].([]any)
	if len(srcs) != 1 {
		t.Fatalf("sources = %v", got["sources"])
	}
	if s0 := srcs[0].(map[string]any); s0["name"] != "src" || s0["type"] != "git" || s0["access"].(map[string]any)["repoUrl"] != "https://github.com/acme/x" {
		t.Fatalf("sources[0] = %v", s0)
	}
	sigs := got["signatures"].([]any)
	if len(sigs) != 1 {
		t.Fatalf("signatures = %v", got["signatures"])
	}
	s0 := sigs[0].(map[string]any)
	dg, si := s0["digest"].(map[string]any), s0["signature"].(map[string]any)
	if s0["name"] != "default" || dg["hashAlgorithm"] != "SHA-256" || dg["normalisationAlgorithm"] != "jsonNormalisation/v4alpha1" || dg["value"] != "abc123" ||
		si["algorithm"] != "RSASSA-PSS" || si["value"] != "c2ln" || si["mediaType"] != "application/vnd.ocm.signature.rsa" {
		t.Fatalf("signatures[0] = %v", s0)
	}
	if _, ok := si["issuer"]; ok {
		t.Fatalf("empty issuer must be omitted: %v", si)
	}
	if fmt.Sprint(got["architectures"]) != "[]" || got["imagePlatformsRead"] != false {
		t.Fatalf("without a registry: architectures=%v imagePlatformsRead=%v", got["architectures"], got["imagePlatformsRead"])
	}
	if svc.gotRef.Repository != testLoc {
		t.Fatalf("read from %q", svc.gotRef.Repository)
	}
	// pinned to another indexed location
	do(t, h, "GET", "/v1/components/acme.org%2Fx/overview/1.0.0?repository="+url.QueryEscape(otherLoc), nil)
	if svc.gotRef.Repository != otherLoc {
		t.Fatalf("pinned: read from %q", svc.gotRef.Repository)
	}
	// unknown version
	if r := do(t, h, "GET", "/v1/components/acme.org%2Fx/overview/9.9.9", nil); r.StatusCode != 404 {
		t.Fatalf("unknown version: %d", r.StatusCode)
	}
}

func executableRes(name string, extra map[string]string) descriptorruntime.Resource {
	return descriptorruntime.Resource{ElementMeta: descriptorruntime.ElementMeta{
		ObjectMeta: descriptorruntime.ObjectMeta{Name: name, Version: "1.1.0"}, ExtraIdentity: extra}, Type: "executable",
		Access: &ocmruntime.Raw{Type: ocmruntime.NewUnversionedType("localBlob"), Data: []byte(`{"type":"localBlob/v1","localReference":"sha256:abc","mediaType":"application/octet-stream"}`)}}
}

// overviewArchitectures reads the overview of acme.org/x 1.1.0 from h.
func overviewArchitectures(t *testing.T, h http.Handler) (string, any) {
	t.Helper()
	r := do(t, h, "GET", "/v1/components/acme.org%2Fx/overview/1.1.0", nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d: %s", r.StatusCode, body(t, r))
	}
	got := decode[map[string]any](t, r)
	return fmt.Sprint(got["architectures"]), got["imagePlatformsRead"]
}

func TestOverviewArchitectures(t *testing.T) {
	reg := zot.Shared(t)
	fixtures.PushBuildxIndex(t.Context(), t, fixtures.Target{Host: reg.Host}, "rest/ovarch", "v1",
		[]string{"linux/amd64", "linux/arm64"}, []string{"https://spdx.dev/Document"})
	d := descriptorOf("acme.org/x", "1.1.0")
	d.Component.Resources = []descriptorruntime.Resource{imageRes("img", nil, ociImage(reg.Host+"/rest/ovarch:v1")),
		executableRes("cli", map[string]string{"os": "linux", "architecture": "amd64"}),
		executableRes("cli", map[string]string{"architecture": "s390x"})}
	h := NewHandler(&fakeService{desc: d}, testIndex(t), WithRegistry(plainOpener()))
	archs, read := overviewArchitectures(t, h)
	if want := "[map[from:[image ocm] platform:linux/amd64] map[from:[image] platform:linux/arm64] map[from:[ocm] platform:s390x]]"; archs != want || read != true {
		t.Fatalf("architectures = %s (read %v), want %s", archs, read, want)
	}
}

func TestOverviewArchitecturesImageFailure(t *testing.T) {
	d := descriptorOf("acme.org/x", "1.1.0")
	d.Component.Resources = []descriptorruntime.Resource{
		imageRes("img", map[string]string{"os": "linux", "architecture": "arm64"}, ociImage("ghcr.io/acme/private:v1")),
		executableRes("cli", map[string]string{"os": "linux", "architecture": "amd64"}),
	}
	unauthorized := func(context.Context, string) (*remote.Repository, error) { return nil, errors.New("401 Unauthorized") }
	h := NewHandler(&fakeService{desc: d}, testIndex(t), WithRegistry(unauthorized))
	archs, read := overviewArchitectures(t, h)
	if want := "[map[from:[ocm] platform:linux/amd64] map[from:[ocm] platform:linux/arm64]]"; archs != want || read != false {
		t.Fatalf("architectures = %s (read %v), want %s", archs, read, want)
	}
}

func TestListComponentsArchitecture(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndexWith(t, func(ctx context.Context, w *index.Writer) {
		if err := w.SetOverview(ctx, index.OverviewTarget{Component: "acme.org/x", Version: "1.1.0", Repository: testLoc},
			index.Overview{Title: "X", Provider: "acme", Architectures: []string{"linux/amd64", "linux/arm64"}}); err != nil {
			t.Fatal(err)
		}
	}))
	type list struct {
		Components []struct{ Name string }
		Facets     struct{ Architectures []gen.FacetValue }
	}
	v := decode[list](t, do(t, h, "GET", "/v1/components?architecture=linux/amd64&architecture=linux/arm64", nil))
	if len(v.Components) != 1 || v.Components[0].Name != "acme.org/x" ||
		fmt.Sprint(v.Facets.Architectures) != "[{1 linux/amd64} {1 linux/arm64}]" {
		t.Fatalf("both = %+v", v)
	}
	if v := decode[list](t, do(t, h, "GET", "/v1/components?architecture=linux/amd64&architecture=s390x", nil)); len(v.Components) != 0 {
		t.Fatalf("all must match: %+v", v)
	}
}

func TestVersionOverviewPinnedWithoutIndex(t *testing.T) {
	svc := &fakeService{desc: descriptorOf("acme.org/x", "1.0.0"), allowed: map[string]bool{"oci::ghcr.io/resolver": true}}
	h := NewHandler(svc, noIndex(t))
	r := do(t, h, "GET", "/v1/components/acme.org%2Fx/overview/1.0.0?repository="+url.QueryEscape("oci::ghcr.io/resolver"), nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d: %s", r.StatusCode, body(t, r))
	}
	got := decode[map[string]any](t, r)
	if _, ok := got["publishedAt"]; ok {
		t.Fatal("publishedAt without index")
	}
	if fmt.Sprint(got["resources"]) != "[]" || fmt.Sprint(got["signatures"]) != "[]" {
		t.Fatalf("empty arrays expected, got resources=%v signatures=%v", got["resources"], got["signatures"])
	}
}

func TestListTopLevelAndSigned(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	r := do(t, h, "GET", "/v1/components?topLevel=true", nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d", r.StatusCode)
	}
	if r := do(t, h, "GET", "/v1/components?topLevel=maybe", nil); r.StatusCode != 400 {
		t.Fatalf("bad topLevel: %d", r.StatusCode)
	}
	got := decode[map[string]any](t, do(t, h, "GET", "/v1/components/acme.org%2Fx", nil))
	if got["signed"] != false || got["defaultVersion"] != "1.1.0" || fmt.Sprint(got["referencedBy"]) != "[]" || got["referencedByCount"] != float64(0) {
		t.Fatalf("component = %v", got)
	}
}

func TestListComponentsBySource(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	r := do(t, h, "GET", "/v1/components?includePrerelease=true", nil)
	v := decode[struct {
		Facets map[string]json.RawMessage
	}](t, r)
	if _, ok := v.Facets["locations"]; ok {
		t.Fatal("facets.locations is still present")
	}
	var conns []struct {
		Key, Title, Type string
		Count            int
	}
	if err := json.Unmarshal(v.Facets["connectors"], &conns); err != nil || len(conns) != 2 ||
		conns[0].Key != "test:c" || conns[0].Title != "Test source" || conns[0].Type != "test/v1" || conns[0].Count == 0 {
		t.Fatalf("connectors = %s (%v)", v.Facets["connectors"], err)
	}
	r = do(t, h, "GET", "/v1/components?connector=test:other", nil)
	if n := len(decode[struct{ Components []struct{ Name string } }](t, r).Components); n != 0 {
		t.Fatalf("unknown source matched %d components", n)
	}
	r = do(t, h, "GET", "/v1/components?connector=test:c", nil)
	if n := len(decode[struct{ Components []struct{ Name string } }](t, r).Components); n == 0 {
		t.Fatal("source matched nothing")
	}
}

func TestComponentSources(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	r := do(t, h, "GET", "/v1/components/acme.org/x", nil)
	v := decode[struct {
		Sources []struct {
			Key, Title string
			AvatarURL  *string `json:"avatarUrl"`
			Locations  []string
		}
	}](t, r)
	wantLocs := slices.Sorted(slices.Values([]string{testLoc, otherLoc})) // by repository
	if len(v.Sources) != 2 || v.Sources[0].Key != "test:c" || v.Sources[0].AvatarURL != nil ||
		!slices.Equal(v.Sources[0].Locations, wantLocs) {
		t.Fatalf("sources = %+v", v.Sources)
	}
}

func TestFacetAndSourceGroups(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	r := do(t, h, "GET", "/v1/components?includePrerelease=true", nil)
	v := decode[struct {
		Facets struct {
			Connectors []struct {
				Key    string
				Groups []struct {
					Key, Title string
					AvatarURL  *string `json:"avatarUrl"`
					Count      int
				}
			}
		}
	}](t, r)
	var ent, plain bool
	for _, c := range v.Facets.Connectors {
		switch c.Key {
		case "test:ent":
			ent = len(c.Groups) == 1 && c.Groups[0].Key == "test:org-a" && c.Groups[0].Title == "Org A" && c.Groups[0].Count == 1 &&
				c.Groups[0].AvatarURL != nil && *c.Groups[0].AvatarURL == "https://avatars.example/a"
		case "test:c":
			plain = c.Groups != nil && len(c.Groups) == 0
		}
	}
	if !ent || !plain {
		t.Fatalf("connectors = %+v", v.Facets.Connectors)
	}
	r = do(t, h, "GET", "/v1/components?connector=test:org-a", nil)
	if got := decode[struct{ Components []struct{ Name string } }](t, r).Components; len(got) != 1 || got[0].Name != "acme.org/x" {
		t.Fatalf("owner filter = %+v", got)
	}
	r = do(t, h, "GET", "/v1/components/acme.org/x", nil)
	d := decode[struct {
		Sources []struct {
			Key    string
			Groups []struct {
				Key       string
				Locations []string
			}
		}
	}](t, r)
	var found bool
	for _, s := range d.Sources {
		if s.Key == "test:ent" {
			found = len(s.Groups) == 1 && s.Groups[0].Key == "test:org-a" && slices.Equal(s.Groups[0].Locations, []string{"oci::ghcr.io/org-a"})
		}
	}
	if !found {
		t.Fatalf("sources = %+v", d.Sources)
	}
}

func TestAttestations(t *testing.T) {
	checked := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	res := attestation.Result{Levels: []attestation.Level{
		{Status: attestation.StatusOK, Entries: []attestation.Entry{{Kind: attestation.KindVEX, How: attestation.HowLabelLink, Signed: attestation.SignedByComponent, URL: "https://example.com/vex.json"},
			{Kind: attestation.KindSLSA, How: attestation.HowReferrer, Signed: attestation.SignedYes, BuildLevel: 3,
				Builder: "https://github.com/actions/runner", Repository: "ghcr.io/acme/x", Label: "provenance",
				Summary: &document.Summary{Format: "SARIF 2.1.0", Tool: "Trivy", Findings: 37, ImportantFindings: 4, ImportantBasis: "security-severity"}}}},
		{Resource: &attestation.Identity{Name: "image", Version: "1.1.0", ExtraIdentity: map[string]string{"arch": "amd64"}}, Status: attestation.StatusFailed, Error: "401", Entries: []attestation.Entry{}},
	}}
	idx := testIndexWith(t, func(ctx context.Context, w *index.Writer) {
		if err := w.SetAttestations(ctx, index.AttestationTarget{Component: "acme.org/x", Version: "1.1.0", Repository: testLoc}, res, checked); err != nil {
			t.Fatal(err)
		}
	})
	h := NewHandler(&fakeService{}, idx)
	base := "/v1/components/acme.org%2Fx/attestations/"
	pin := "?repository=" + url.QueryEscape(testLoc)

	r := do(t, h, "GET", base+"1.1.0"+pin, nil)
	if r.StatusCode != 200 || r.Header.Get("Cache-Control") != "no-cache" || r.Header.Get("ETag") == "" {
		t.Fatalf("known: %d %v", r.StatusCode, r.Header)
	}
	etag := r.Header.Get("ETag")
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"builder":"https://github.com/actions/runner"`, `"label":"provenance"`,
		// JSON object keys are emitted sorted.
		`"summary":{"findings":37,"format":"SARIF 2.1.0","importantBasis":"security-severity","importantFindings":4,"tool":"Trivy"}`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("body lacks %s: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), `"repository":"ghcr.io/acme/x"`) {
		t.Errorf("entry repository exposed: %s", raw)
	}
	var got gen.AttestationSet
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "acme.org/x" || got.Version != "1.1.0" || got.Repository != testLoc || !got.CheckedAt.Equal(checked) || len(got.Levels) != 2 {
		t.Fatalf("got %+v", got)
	}
	if l := got.Levels[1]; l.Status != gen.Failed || l.Resource == nil || (*l.Resource.ExtraIdentity)["arch"] != "amd64" || l.Error == nil || len(l.Entries) != 0 {
		t.Fatalf("level %+v", l)
	}
	if e := got.Levels[0].Entries[0]; e.Kind != gen.Vex || e.How != gen.LabelLink || e.Signed != gen.AttestationEntrySignedComponent || *e.Url != "https://example.com/vex.json" {
		t.Fatalf("entry %+v", e)
	}
	if e := got.Levels[0].Entries; e[0].BuildLevel != nil || e[1].BuildLevel == nil || *e[1].BuildLevel != 3 {
		t.Fatalf("build levels %+v", e)
	}
	if r := do(t, h, "GET", base+"1.1.0"+pin, http.Header{"If-None-Match": {etag}}); r.StatusCode != 304 {
		t.Fatalf("etag: %d", r.StatusCode)
	}
	if r := do(t, h, "GET", base+"1.1.0?repository="+url.QueryEscape("ghcr.io/acme"), nil); r.StatusCode != 200 {
		t.Fatalf("normalized repository: %d", r.StatusCode)
	}

	r = do(t, h, "GET", base+"latest?repository=ghcr.io/acme", nil)
	if r.StatusCode != 307 || r.Header.Get("Location") != base+"1.1.0?repository="+url.QueryEscape("ghcr.io/acme") {
		t.Fatalf("latest: %d %s", r.StatusCode, r.Header.Get("Location"))
	}

	for _, target := range []string{base + "1.0.0" + pin, base + "1.1.0?repository=" + url.QueryEscape(otherLoc)} {
		r = do(t, h, "GET", target, nil)
		if r.StatusCode != 404 {
			t.Fatalf("%s: %d", target, r.StatusCode)
		}
		e := decode[struct {
			Errors []struct {
				Code   string
				Detail struct {
					Collected []struct{ Version, Repository string }
				}
			}
		}](t, r).Errors[0]
		if e.Code != "NOT_COLLECTED" || len(e.Detail.Collected) != 1 || e.Detail.Collected[0].Version != "1.1.0" || e.Detail.Collected[0].Repository != testLoc {
			t.Fatalf("%s: %+v", target, e)
		}
	}
	if r := do(t, h, "GET", base+"1.1.0?repository=%25bad", nil); r.StatusCode != 400 || errCode(t, r) != "REPOSITORY_INVALID" {
		t.Fatalf("bad repository: %d", r.StatusCode)
	}
	if r := do(t, h, "GET", "/v1/components/nope.org%2Fx/attestations/1.0.0", nil); r.StatusCode != 404 || errCode(t, r) != "COMPONENT_UNKNOWN" {
		t.Fatalf("unknown: %d", r.StatusCode)
	}
	if r := do(t, h, "GET", "/v1/components/nope.org%2Fx/attestations/latest", nil); r.StatusCode != 404 || errCode(t, r) != "COMPONENT_UNKNOWN" {
		t.Fatalf("unknown latest: %d", r.StatusCode)
	}
	none := NewHandler(&fakeService{}, noIndex(t))
	if r := do(t, none, "GET", base+"1.1.0"+pin, nil); r.StatusCode != 503 || errCode(t, r) != "INDEX_UNAVAILABLE" {
		t.Fatalf("no index: %d", r.StatusCode)
	}
	if r := do(t, none, "GET", base+"1.1.0", nil); r.StatusCode != 503 {
		t.Fatalf("no index unpinned: %d", r.StatusCode)
	}
}

// TestAttestationsExactLocation: sets are looked up at exactly ?repository=;
// without it the route redirects to the location the other version routes
// resolve. An older version's stored set is served.
func TestAttestationsExactLocation(t *testing.T) {
	older := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	res := attestation.Result{Levels: []attestation.Level{{Status: attestation.StatusOK, Entries: []attestation.Entry{}}}}
	idx := testIndexWith(t, func(ctx context.Context, w *index.Writer) {
		for _, tg := range []index.AttestationTarget{
			{Component: "acme.org/x", Version: "1.0.0", Repository: otherLoc}, // 1.0.0 resolves to testLoc
			{Component: "acme.org/x", Version: "1.1.0", Repository: testLoc},  // not the default version
		} {
			if err := w.SetAttestations(ctx, tg, res, older); err != nil {
				t.Fatal(err)
			}
		}
	})
	h := NewHandler(&fakeService{}, idx)
	base := "/v1/components/acme.org%2Fx/attestations/"
	pin := func(loc string) string { return "?repository=" + url.QueryEscape(loc) }

	for target, want := range map[string]string{
		base + "1.1.0":                  base + "1.1.0" + pin(testLoc),
		base + "1.0.0":                  base + "1.0.0" + pin(testLoc),
		base + "latest":                 base + "1.1.0" + pin(testLoc),
		base + "latest" + pin(otherLoc): base + "1.1.0" + pin(otherLoc),
		base + "1.0.0?x=1":              base + "1.0.0" + pin(testLoc) + "&x=1",
	} {
		for _, method := range []string{"GET", "HEAD"} {
			r := do(t, h, method, target, nil)
			if r.StatusCode != 307 || r.Header.Get("Location") != want || r.Header.Get("Cache-Control") != "no-cache" {
				t.Errorf("%s %s: %d %s %q", method, target, r.StatusCode, r.Header.Get("Location"), r.Header.Get("Cache-Control"))
			}
		}
	}

	r := do(t, h, "GET", base+"1.1.0"+pin(testLoc), nil)
	if r.StatusCode != 200 {
		t.Fatalf("older version: %d", r.StatusCode)
	}
	if got := decode[gen.AttestationSet](t, r); got.Repository != testLoc || !got.CheckedAt.Equal(older) {
		t.Fatalf("older version: %+v", got)
	}
	if r := do(t, h, "GET", base+"1.0.0"+pin(testLoc), nil); r.StatusCode != 404 || errCode(t, r) != "NOT_COLLECTED" {
		t.Fatalf("no fallback to another location: %d", r.StatusCode)
	}
	r = do(t, h, "GET", base+"1.0.0"+pin(otherLoc), nil)
	if r.StatusCode != 200 {
		t.Fatalf("pinned: %d", r.StatusCode)
	}
	if got := decode[gen.AttestationSet](t, r); got.Repository != otherLoc {
		t.Fatalf("pinned: %+v", got)
	}
	if r := do(t, h, "GET", base+"9.9.9", nil); r.StatusCode != 404 || errCode(t, r) != "NOT_COLLECTED" {
		t.Fatalf("unknown version: %d", r.StatusCode)
	}
}

func TestListComponentsSort(t *testing.T) {
	h := NewHandler(&fakeService{}, testIndex(t))
	for _, s := range []string{"", "relevance", "updated", "name"} {
		if r := do(t, h, "GET", "/v1/components?sort="+s, nil); r.StatusCode != 200 {
			t.Fatalf("sort=%q: status %d: %s", s, r.StatusCode, body(t, r))
		}
	}
	r := do(t, h, "GET", "/v1/components?sort=bogus", nil)
	if r.StatusCode != 400 || !strings.Contains(body(t, r), "relevance, updated or name") {
		t.Fatalf("bogus sort: %d", r.StatusCode)
	}
}

func TestVersionsHasChangelog(t *testing.T) {
	yes, no := true, false
	h := NewHandler(&fakeService{}, testIndexWith(t, func(ctx context.Context, w *index.Writer) {
		for v, has := range map[string]*bool{"1.0.0": &yes, "1.1.0": &no} {
			if err := w.SetChangelog(ctx, "acme.org/x", v, has, ""); err != nil {
				t.Fatal(err)
			}
		}
	}))
	r := do(t, h, "GET", "/v1/components/acme.org/x/versions", nil)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Versions []map[string]any
	}
	if err := json.Unmarshal(body, &v); err != nil || len(v.Versions) != 2 {
		t.Fatalf("body = %s (%v)", body, err)
	}
	// newest first: 1.1.0 (false), 1.0.0 (true)
	if got, ok := v.Versions[0]["hasChangelog"]; !ok || got != false {
		t.Fatalf("1.1.0 = %v", v.Versions[0])
	}
	if got, ok := v.Versions[1]["hasChangelog"]; !ok || got != true {
		t.Fatalf("1.0.0 = %v", v.Versions[1])
	}
	r = do(t, h, "GET", "/v1/components/acme.org/x/versions?includePrerelease=true", nil)
	body, _ = io.ReadAll(r.Body)
	if !strings.Contains(string(body), `"version":"2.0.0-rc.1"`) || strings.Count(string(body), "hasChangelog") != 2 {
		t.Fatalf("unknown version must omit the field: %s", body)
	}
}
