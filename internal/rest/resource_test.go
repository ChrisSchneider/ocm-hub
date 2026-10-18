package rest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/rest/gen"
)

func openResult(content string) *ocm.Resource {
	return &ocm.Resource{Body: io.NopCloser(strings.NewReader(content)), Name: "bin", MediaType: "application/x-thing",
		Size: int64(len(content)), Digest: "sha256:abc", DigestMatchesBody: true}
}

func TestResourceDownload(t *testing.T) {
	f := &fakeService{resource: openResult("bits")}
	r := do(t, NewHandler(f, testIndex(t)), "GET", "/v1/components/acme.org/x/resources/1.0.0/bin?extraIdentity.arch=amd64&extraIdentity.os=linux", nil)
	if r.StatusCode != 200 || body(t, r) != "bits" {
		t.Fatalf("status %d", r.StatusCode)
	}
	h := r.Header
	if h.Get("Content-Type") != "application/x-thing" || h.Get("Content-Length") != "4" ||
		h.Get("OCM-Resource-Digest") != "sha256:abc" || h.Get("ETag") != `"sha256:abc"` ||
		h.Get("Content-Disposition") != "attachment; filename=bin" {
		t.Fatalf("headers = %v", h)
	}
	if f.gotBody == nil || !*f.gotBody {
		t.Fatal("GET must open the body")
	}
	want := map[string]string{"name": "bin", "arch": "amd64", "os": "linux"}
	if len(f.gotID) != len(want) {
		t.Fatalf("identity = %v", f.gotID)
	}
	for k, v := range want {
		if f.gotID[k] != v {
			t.Fatalf("identity = %v", f.gotID)
		}
	}
}

func TestResourceHeadAndDefaults(t *testing.T) {
	res := openResult("bits")
	res.MediaType, res.Size, res.Digest = "", -1, ""
	f := &fakeService{resource: res}
	r := do(t, NewHandler(f, testIndex(t)), "HEAD", "/v1/components/acme.org/x/resources/1.0.0/bin", nil)
	if r.StatusCode != 200 || body(t, r) != "" || r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("ETag") != "" {
		t.Fatalf("%d %v", r.StatusCode, r.Header)
	}
	if f.gotBody == nil || *f.gotBody {
		t.Fatal("HEAD must not open the body")
	}
}

func TestResourceHeadKeepsMetadata(t *testing.T) {
	f := &fakeService{resource: openResult("bits")}
	r := do(t, NewHandler(f, testIndex(t)), "HEAD", "/v1/components/acme.org/x/resources/1.0.0/bin", nil)
	h := r.Header
	if r.StatusCode != 200 || body(t, r) != "" || h.Get("Content-Length") != "4" || h.Get("ETag") != `"sha256:abc"` ||
		h.Get("OCM-Resource-Digest") != "sha256:abc" || h.Get("Content-Type") != "application/x-thing" {
		t.Fatalf("%d %v", r.StatusCode, h)
	}
	if f.gotBody == nil || *f.gotBody {
		t.Fatal("HEAD must not open the body")
	}
}

// TestResourceETagOnlyWhenDigestMatchesBody: for non-local access types the
// OCM digest is not a hash of the served bytes, so no ETag is sent, but the
// OCM digest is still exposed.
func TestResourceETagOnlyWhenDigestMatchesBody(t *testing.T) {
	res := openResult("bits")
	res.DigestMatchesBody = false
	for _, m := range []string{"GET", "HEAD"} {
		r := do(t, NewHandler(&fakeService{resource: res}, testIndex(t)), m, "/v1/components/acme.org/x/resources/1.0.0/bin", nil)
		if r.StatusCode != 200 || r.Header.Get("ETag") != "" || r.Header.Get("OCM-Resource-Digest") != "sha256:abc" {
			t.Fatalf("%s: %d %v", m, r.StatusCode, r.Header)
		}
	}
}

func TestResourceBadIdentity(t *testing.T) {
	r := do(t, NewHandler(&fakeService{resource: openResult("x")}, testIndex(t)), "GET", "/v1/components/acme.org/x/resources/1.0.0/bin?extraIdentity.name=other", nil)
	if r.StatusCode != 400 {
		t.Fatalf("status %d", r.StatusCode)
	}
}

func TestResourceLatestRedirect(t *testing.T) {
	r := do(t, NewHandler(&fakeService{}, testIndex(t)), "GET", "/v1/components/acme.org/x/resources/latest/bin?extraIdentity.arch=amd64", nil)
	if r.StatusCode != 307 || r.Header.Get("Location") != "/v1/components/acme.org%2Fx/resources/1.1.0/bin?extraIdentity.arch=amd64" {
		t.Fatalf("%d %q", r.StatusCode, r.Header.Get("Location"))
	}
}

// TestResourceLocation: the resource is read at the location the index (or
// the ?repository= pin) names; unknown versions and a missing index fail
// before the service is asked.
func TestResourceLocation(t *testing.T) {
	f := &fakeService{resource: openResult("bits")}
	h := NewHandler(f, testIndex(t))
	if r := do(t, h, "GET", "/v1/components/acme.org/x/resources/1.0.0/bin", nil); r.StatusCode != 200 || f.gotRef.Repository != testLoc {
		t.Fatalf("%d %q", r.StatusCode, f.gotRef.Repository)
	}
	if r := do(t, h, "GET", "/v1/components/acme.org/x/resources/1.0.0/bin?repository="+otherLoc, nil); r.StatusCode != 200 || f.gotRef.Repository != otherLoc {
		t.Fatalf("pinned: %d %q", r.StatusCode, f.gotRef.Repository)
	}
	if r := do(t, h, "GET", "/v1/components/acme.org/x/resources/9.9.9/bin", nil); r.StatusCode != 404 || errCode(t, r) != "VERSION_UNKNOWN" {
		t.Fatalf("unknown version: %d", r.StatusCode)
	}
	h = NewHandler(&fakeService{resource: openResult("bits"), allowed: map[string]bool{"oci::registry.example/ocm": true}}, noIndex(t))
	if r := do(t, h, "GET", "/v1/components/acme.org/x/resources/1.0.0/bin", nil); r.StatusCode != 503 {
		t.Fatalf("no index: %d", r.StatusCode)
	}
	if r := do(t, h, "GET", "/v1/components/acme.org/x/resources/1.0.0/bin?repository=oci::registry.example/ocm", nil); r.StatusCode != 200 {
		t.Fatalf("no index, pinned resolver repo: %d", r.StatusCode)
	}
}

func TestResourceConditional(t *testing.T) {
	newRes := func() *ocm.Resource {
		return &ocm.Resource{Body: io.NopCloser(strings.NewReader("data")), Name: "d", MediaType: "text/plain", Size: 4,
			Digest: "sha256:abc", DigestMatchesBody: true}
	}
	const p = "/v1/components/acme.org/x/resources/1.0.0/d"
	idx := testIndex(t)
	r := do(t, NewHandler(&fakeService{resource: newRes()}, idx), "GET", p, nil)
	if r.StatusCode != 200 || body(t, r) != "data" || r.Header.Get("Cache-Control") != "no-cache" || r.Header.Get("ETag") != `"sha256:abc"` {
		t.Fatalf("GET: %d CC %q ETag %q", r.StatusCode, r.Header.Get("Cache-Control"), r.Header.Get("ETag"))
	}
	r = do(t, NewHandler(&fakeService{resource: newRes()}, idx), "GET", p, http.Header{"If-None-Match": {`"sha256:abc"`}})
	if r.StatusCode != 304 || body(t, r) != "" || r.Header.Get("ETag") != `"sha256:abc"` || r.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("conditional GET: %d", r.StatusCode)
	}
}

func TestResourceAmbiguous(t *testing.T) {
	err := &ocm.AmbiguousResourceError{}
	r := do(t, NewHandler(&fakeService{err: err}, testIndex(t)), "GET", "/v1/components/acme.org/x/resources/1.0.0/bin", nil)
	if r.StatusCode != 409 || !strings.Contains(body(t, r), "candidates") {
		t.Fatalf("status %d", r.StatusCode)
	}
}

type failingReader struct{ sent bool }

func (f *failingReader) Read(p []byte) (int, error) {
	if !f.sent {
		f.sent = true
		return copy(p, "partial"), nil
	}
	return 0, errors.New("upstream broke")
}

// TestResourceMidStreamErrorAbortsConnection checks that a failure after the
// status was sent surfaces to the client as a broken body, never as success.
func TestResourceMidStreamErrorAbortsConnection(t *testing.T) {
	res := &ocm.Resource{Body: io.NopCloser(&failingReader{}), Name: "bin", Size: 100}
	srv := httptest.NewServer(NewHandler(&fakeService{resource: res}, testIndex(t)))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/v1/components/acme.org/x/resources/1.0.0/bin")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("expected a read error for an aborted download")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("blob fetch failed") }

func TestResourceFirstReadErrorIsErrorResponse(t *testing.T) {
	res := &ocm.Resource{Body: io.NopCloser(errReader{}), Name: "bin", Size: 10}
	r := do(t, NewHandler(&fakeService{resource: res}, testIndex(t)), "GET", "/v1/components/acme.org/x/resources/1.0.0/bin", nil)
	if r.StatusCode != 502 || !strings.Contains(body(t, r), "UPSTREAM_ERROR") {
		t.Fatalf("status %d", r.StatusCode)
	}
}

func TestResourceEmptyBody(t *testing.T) {
	r := do(t, NewHandler(&fakeService{resource: openResult("")}, testIndex(t)), "GET", "/v1/components/acme.org/x/resources/1.0.0/bin", nil)
	if r.StatusCode != 200 || body(t, r) != "" {
		t.Fatalf("status %d", r.StatusCode)
	}
}

type deadlineReader struct{}

func (deadlineReader) Read([]byte) (int, error) { return 0, context.DeadlineExceeded }

func TestResourceFirstReadTimeoutIs504(t *testing.T) {
	res := &ocm.Resource{Body: io.NopCloser(deadlineReader{}), Name: "bin", Size: 10}
	r := do(t, NewHandler(&fakeService{resource: res}, testIndex(t)), "GET", "/v1/components/acme.org/x/resources/1.0.0/bin", nil)
	if r.StatusCode != 504 || !strings.Contains(body(t, r), "UPSTREAM_TIMEOUT") {
		t.Fatalf("status %d", r.StatusCode)
	}
}

func TestResourceAsDocument(t *testing.T) {
	sarif, err := os.ReadFile("../document/testdata/trivy.sarif.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeService{resource: openResult(string(sarif))}
	r := do(t, NewHandler(f, testIndex(t)), "GET", "/v1/components/acme.org/x/resources/1.0.0/bin?as=document", nil)
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("status %d: %s", r.StatusCode, body(t, r))
	}
	d := decode[gen.Document](t, r)
	if d.Scan == nil || len(d.Scan.Findings) != 5 || d.Scan.Tool != "Trivy" || d.Scan.ImportantBasis != "security-severity" {
		t.Fatalf("document = %+v", d)
	}
	if f.gotID["as"] != "" {
		t.Errorf("identity = %v", f.gotID)
	}
	f = &fakeService{resource: openResult("not a document")}
	if r := do(t, NewHandler(f, testIndex(t)), "GET", "/v1/components/acme.org/x/resources/1.0.0/bin?as=document", nil); r.StatusCode != 422 || errCode(t, r) != "UNSUPPORTED_DOCUMENT" {
		t.Errorf("unsupported: %d", r.StatusCode)
	}
}
