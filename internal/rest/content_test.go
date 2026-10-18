package rest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"oras.land/oras-go/v2/registry/remote"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/rest/gen"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
)

const cdxTwo = `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[` +
	`{"type":"library","bom-ref":"a","name":"alpha","version":"1.0.0"},` +
	`{"type":"library","bom-ref":"b","name":"beta","version":"2.0.0"}]}`

const cdxStatement = `{"_type":"https://in-toto.io/Statement/v1","subject":[],"predicateType":"https://cyclonedx.org/bom","predicate":` + cdxTwo + `}`

const vexURL = "https://example.com/vex.json"

var testChecked = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

// plainOpener opens repositories over plain HTTP (zot in tests).
func plainOpener() attestation.RepositoryOpener {
	return func(_ context.Context, repository string) (*remote.Repository, error) {
		repo, err := remote.NewRepository(repository)
		if err != nil {
			return nil, err
		}
		repo.PlainHTTP = true
		return repo, nil
	}
}

// pushReferrer pushes an image to zot with one referrer whose single layer is
// layer, and returns the referrer entry as a sync stores it.
func pushReferrer(t *testing.T, repoName, layerMediaType string, layer []byte) attestation.Entry {
	t.Helper()
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushImage(ctx, t, target, repoName, "v1", []byte("layer"))
	repo, err := plainOpener()(ctx, reg.Host+"/"+repoName)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := repo.Resolve(ctx, "v1")
	if err != nil {
		t.Fatal(err)
	}
	d := fixtures.PushReferrer(ctx, t, target, repoName, subject, layerMediaType, nil, layerMediaType, layer)
	return attestation.Entry{Kind: attestation.KindSBOM, How: attestation.HowReferrer, Signed: attestation.SignedNo,
		Digest: d.Digest.String(), Repository: reg.Host + "/" + repoName}
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// vexEntry is a label-link entry recorded with doc's digest.
func vexEntry(doc []byte) attestation.Entry {
	return attestation.Entry{Kind: attestation.KindVEX, How: attestation.HowLabelLink, Signed: attestation.SignedByComponent,
		URL: vexURL, Label: attestation.LabelVEX, Digest: sha(doc)}
}

// countingFetch serves body and counts calls.
type countingFetch struct {
	body  []byte
	calls int
}

func (f *countingFetch) fetch(_ context.Context, url string) ([]byte, error) {
	f.calls++
	if url != vexURL {
		return nil, errors.New("unexpected url " + url)
	}
	return f.body, nil
}

// contentHandler stores entries as acme.org/x 1.1.0 at testLoc and serves
// them with zot as the registry and fetch as the fetcher.
func contentHandler(t *testing.T, fetch *countingFetch, entries ...attestation.Entry) *handler {
	t.Helper()
	res := attestation.Result{Levels: []attestation.Level{{Status: attestation.StatusOK, Entries: entries}}}
	idx := testIndexWith(t, func(ctx context.Context, w *index.Writer) {
		if err := w.SetAttestations(ctx, index.AttestationTarget{Component: "acme.org/x", Version: "1.1.0", Repository: testLoc}, res, testChecked); err != nil {
			t.Fatal(err)
		}
	})
	return NewHandler(&fakeService{}, idx, WithRegistry(plainOpener()), WithFetcher(fetch.fetch)).(*handler)
}

func contentPath(digest, query string) string {
	p := "/v1/components/acme.org%2Fx/attestations/1.1.0/" + digest + "?repository=" + url.QueryEscape(testLoc)
	if query != "" {
		p += "&" + query
	}
	return p
}

func TestAttestationContentReferrer(t *testing.T) {
	e := pushReferrer(t, "rest/contentref", "application/vnd.in-toto+json", []byte(cdxStatement))
	h := contentHandler(t, &countingFetch{}, e)
	r := do(t, h, "GET", contentPath(e.Digest, ""), nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d: %s", r.StatusCode, body(t, r))
	}
	want := `attachment; filename="` + e.Digest[len("sha256:"):][:12] + `.json"`
	if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Content-Disposition") != want ||
		r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers = %v", r.Header)
	}
	if got := body(t, r); got != cdxTwo {
		t.Errorf("body = %s", got)
	}
}

func TestAttestationContentDocument(t *testing.T) {
	e := pushReferrer(t, "rest/contentdoc", "application/vnd.in-toto+json", []byte(cdxStatement))
	h := contentHandler(t, &countingFetch{}, e)
	r := do(t, h, "GET", contentPath(e.Digest, "as=document"), nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d: %s", r.StatusCode, body(t, r))
	}
	d := decode[gen.Document](t, r)
	if d.Sbom == nil || len(d.Sbom.Packages) != 2 || d.Format == "" {
		t.Fatalf("document = %+v", d)
	}
	if p := d.Sbom.Packages[0]; p.Name != "alpha" || p.Version == nil || *p.Version != "1.0.0" {
		t.Errorf("package = %+v", p)
	}
	if r := do(t, h, "GET", contentPath(e.Digest, "as=html"), nil); r.StatusCode != 400 || errCode(t, r) != "PARAMETER_INVALID" {
		t.Errorf("as=html: %d", r.StatusCode)
	}
}

func TestAttestationContentUnknownDigest(t *testing.T) {
	doc := []byte(`{"x":1}`)
	f := &countingFetch{body: doc}
	embedded := attestation.Entry{Kind: attestation.KindSBOM, How: attestation.HowEmbedded, Signed: attestation.SignedByComponent,
		Resource: &attestation.Identity{Name: "sbom"}, Digest: "sha256:" + hex.EncodeToString(make([]byte, 32))}
	h := contentHandler(t, f, vexEntry(doc), embedded)
	for _, p := range []string{
		contentPath("sha256:"+hex.EncodeToString(make([]byte, 31))+"ff", ""),
		contentPath(embedded.Digest, ""),
		"/v1/components/acme.org%2Fx/attestations/1.0.0/" + vexEntry(doc).Digest + "?repository=" + url.QueryEscape(testLoc),
	} {
		if r := do(t, h, "GET", p, nil); r.StatusCode != 404 || errCode(t, r) != "NOT_FOUND" {
			t.Errorf("%s: %d", p, r.StatusCode)
		}
	}
	if f.calls != 0 {
		t.Errorf("fetched %d times", f.calls)
	}
}

func TestAttestationContentRedirects(t *testing.T) {
	doc := []byte(`{"x":1}`)
	h := contentHandler(t, &countingFetch{body: doc}, vexEntry(doc))
	dgst := vexEntry(doc).Digest
	r := do(t, h, "GET", "/v1/components/acme.org%2Fx/attestations/latest/"+dgst+"?as=document", nil)
	if r.StatusCode != 307 {
		t.Fatalf("status %d", r.StatusCode)
	}
	if loc := r.Header.Get("Location"); loc != "/v1/components/acme.org%2Fx/attestations/1.1.0/"+dgst+"?as=document&repository="+url.QueryEscape(testLoc) {
		t.Errorf("location = %s", loc)
	}
}

func TestAttestationContentNotConfigured(t *testing.T) {
	doc := []byte(`{"x":1}`)
	res := attestation.Result{Levels: []attestation.Level{{Status: attestation.StatusOK, Entries: []attestation.Entry{vexEntry(doc)}}}}
	idx := testIndexWith(t, func(ctx context.Context, w *index.Writer) {
		if err := w.SetAttestations(ctx, index.AttestationTarget{Component: "acme.org/x", Version: "1.1.0", Repository: testLoc}, res, testChecked); err != nil {
			t.Fatal(err)
		}
	})
	r := do(t, NewHandler(&fakeService{}, idx), "GET", contentPath(vexEntry(doc).Digest, ""), nil)
	if r.StatusCode != 501 || errCode(t, r) != "NOT_IMPLEMENTED" {
		t.Fatalf("status %d", r.StatusCode)
	}
}

// TestAttestationContentNoRegistry: a referrer entry without WithRegistry
// answers 501 (Content would panic on a nil opener).
func TestAttestationContentNoRegistry(t *testing.T) {
	ref := attestation.Entry{Kind: attestation.KindSBOM, How: attestation.HowReferrer, Signed: attestation.SignedNo,
		Digest: "sha256:" + hex.EncodeToString(make([]byte, 32)), Repository: "localhost:1/none"}
	res := attestation.Result{Levels: []attestation.Level{{Status: attestation.StatusOK, Entries: []attestation.Entry{ref}}}}
	idx := testIndexWith(t, func(ctx context.Context, w *index.Writer) {
		if err := w.SetAttestations(ctx, index.AttestationTarget{Component: "acme.org/x", Version: "1.1.0", Repository: testLoc}, res, testChecked); err != nil {
			t.Fatal(err)
		}
	})
	f := &countingFetch{}
	r := do(t, NewHandler(&fakeService{}, idx, WithFetcher(f.fetch)), "GET", contentPath(ref.Digest, ""), nil)
	if r.StatusCode != 501 || errCode(t, r) != "NOT_IMPLEMENTED" {
		t.Fatalf("status %d", r.StatusCode)
	}
}

func TestAttestationContentLabelLink(t *testing.T) {
	doc, err := os.ReadFile("../document/testdata/openvex.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &countingFetch{body: doc}
	h := contentHandler(t, f, vexEntry(doc))
	r := do(t, h, "GET", contentPath(vexEntry(doc).Digest, "as=document"), nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d: %s", r.StatusCode, body(t, r))
	}
	if d := decode[gen.Document](t, r); d.Vex == nil || len(d.Vex.Statements) == 0 || d.Vex.Statements[0].Vulnerability == "" {
		t.Fatalf("document = %+v", d)
	}
	if f.calls != 1 {
		t.Errorf("fetched %d times", f.calls)
	}
}

func TestAttestationContentChanged(t *testing.T) {
	f := &countingFetch{body: []byte(`{"changed":true}`)}
	h := contentHandler(t, f, vexEntry([]byte(`{"x":1}`)))
	r := do(t, h, "GET", contentPath(vexEntry([]byte(`{"x":1}`)).Digest, ""), nil)
	if r.StatusCode != 409 {
		t.Fatalf("status %d", r.StatusCode)
	}
	er := decode[gen.ErrorResponse](t, r)
	if er.Errors[0].Code != "DOCUMENT_CHANGED" || er.Errors[0].Detail["url"] != vexURL {
		t.Fatalf("error = %+v", er)
	}
}

func TestAttestationContentUnsupported(t *testing.T) {
	e := pushReferrer(t, "rest/contentxml", "application/vnd.cyclonedx+xml", []byte("<bom/>"))
	h := contentHandler(t, &countingFetch{}, e)
	if r := do(t, h, "GET", contentPath(e.Digest, "as=document"), nil); r.StatusCode != 422 || errCode(t, r) != "UNSUPPORTED_DOCUMENT" {
		t.Errorf("as=document: %d", r.StatusCode)
	}
	r := do(t, h, "GET", contentPath(e.Digest, ""), nil)
	if r.StatusCode != 200 || body(t, r) != "<bom/>" {
		t.Errorf("raw: %d", r.StatusCode)
	}
}

// TestAttestationContentNotAllowed: a label link whose URL is no longer on
// the allow list answers 403 and nothing is fetched.
func TestAttestationContentNotAllowed(t *testing.T) {
	doc := []byte(`{"x":1}`)
	res := attestation.Result{Levels: []attestation.Level{{Status: attestation.StatusOK, Entries: []attestation.Entry{vexEntry(doc)}}}}
	idx := testIndexWith(t, func(ctx context.Context, w *index.Writer) {
		if err := w.SetAttestations(ctx, index.AttestationTarget{Component: "acme.org/x", Version: "1.1.0", Repository: testLoc}, res, testChecked); err != nil {
			t.Fatal(err)
		}
	})
	var calls int
	f := &document.Fetcher{Allow: []string{"other.example.com/**"}, Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("fetched")
	})}}
	r := do(t, NewHandler(&fakeService{}, idx, WithFetcher(f.Fetch)), "GET", contentPath(vexEntry(doc).Digest, "as=document"), nil)
	if r.StatusCode != 403 {
		t.Fatalf("status %d", r.StatusCode)
	}
	er := decode[gen.ErrorResponse](t, r)
	if er.Errors[0].Code != "DOCUMENT_NOT_ALLOWED" || er.Errors[0].Detail["url"] != vexURL {
		t.Fatalf("error = %+v", er)
	}
	if calls != 0 {
		t.Errorf("fetched %d times", calls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
