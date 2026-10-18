package attestation

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
)

func TestMain(m *testing.M) { zot.Main(m) }

func plainOpener(client remote.Client) RepositoryOpener {
	return func(_ context.Context, repository string) (*remote.Repository, error) {
		repo, err := remote.NewRepository(repository)
		if err != nil {
			return nil, err
		}
		repo.PlainHTTP = true
		if client != nil {
			repo.Client = client
		}
		return repo, nil
	}
}

func resolve(t *testing.T, open RepositoryOpener, repository, ref string) ocispec.Descriptor {
	t.Helper()
	repo, err := open(t.Context(), repository)
	if err != nil {
		t.Fatal(err)
	}
	d, err := repo.Resolve(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func byDigest(t *testing.T, entries []Entry, dgst string) Entry {
	t.Helper()
	for _, e := range entries {
		if e.Digest == dgst {
			return e
		}
	}
	t.Fatalf("no entry with digest %s in %+v", dgst, entries)
	return Entry{}
}

func TestInspectReferrers(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushImage(ctx, t, target, "att/referrers", "v1", []byte("layer"))
	open := plainOpener(nil)
	subject := resolve(t, open, reg.Host+"/att/referrers", "v1")
	ref := fixtures.PushReferrer(ctx, t, target, "att/referrers", subject,
		"application/vnd.dev.sigstore.bundle.v0.3+json",
		map[string]string{"dev.sigstore.bundle.predicateType": "https://slsa.dev/provenance/v1"},
		"application/vnd.dev.sigstore.bundle.v0.3+json", []byte(`{}`))

	got, err := Inspect(ctx, open, Subject{reg.Host + "/att/referrers", "v1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{Kind: KindSLSA, Format: "SLSA v1", Type: "https://slsa.dev/provenance/v1", Source: SourceReferrer, Signed: SignedYes, Digest: ref.Digest.String(), Repository: reg.Host + "/att/referrers"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

type countingTransport struct {
	mu    sync.Mutex
	blobs []string
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/blobs/") {
		c.mu.Lock()
		c.blobs = append(c.blobs, r.URL.Path)
		c.mu.Unlock()
	}
	return retry.DefaultClient.Transport.RoundTrip(r)
}

func TestInspectReferrersFromStatement(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushImage(ctx, t, target, "att/statement", "v1", []byte("layer"))
	ct := &countingTransport{}
	open := plainOpener(&auth.Client{Client: &http.Client{Transport: ct}})
	subject := resolve(t, open, reg.Host+"/att/statement", "v1")

	small := []byte(`{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://spdx.dev/Document","subject":[],"predicate":{}}`)
	small = append(small, bytes.Repeat([]byte(" "), 200-len(small))...)
	big := append([]byte(`{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://spdx.dev/Document","pad":"`),
		append(bytes.Repeat([]byte("x"), 300<<10), []byte(`"}`)...)...)
	smallRef := fixtures.PushReferrer(ctx, t, target, "att/statement", subject, "application/vnd.in-toto+json", nil, "application/vnd.in-toto+json", small)
	bigRef := fixtures.PushReferrer(ctx, t, target, "att/statement", subject, "application/vnd.in-toto+json", nil, "application/vnd.in-toto+json", big)

	got, err := Inspect(ctx, open, Subject{reg.Host + "/att/statement", "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	s := byDigest(t, got, smallRef.Digest.String())
	if s.Kind != KindSBOM || s.Format != "SPDX" || s.Signed != SignedNo || s.Type != "https://spdx.dev/Document" {
		t.Errorf("small = %+v", s)
	}
	b := byDigest(t, got, bigRef.Digest.String())
	if b.Kind != KindOther || b.Type != "" || b.Signed != SignedNo {
		t.Errorf("big = %+v", b)
	}

	// The big layer must never be fetched.
	bigManifest := fetchManifestForTest(t, open, reg.Host+"/att/statement", bigRef)
	for _, p := range ct.blobs {
		if strings.HasSuffix(p, bigManifest.Layers[0].Digest.String()) {
			t.Errorf("large statement was fetched: %s", p)
		}
	}
}

func fetchManifestForTest(t *testing.T, open RepositoryOpener, repository string, d ocispec.Descriptor) ocispec.Manifest {
	t.Helper()
	repo, _ := open(t.Context(), repository)
	m, err := fetchManifest(t.Context(), repo, d)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestInspectTagFallback(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushImage(ctx, t, target, "att/fallback", "v1", []byte("layer"))
	open := plainOpener(nil)
	subject := resolve(t, open, reg.Host+"/att/fallback", "v1")
	ref := fixtures.PushReferrerTagSchema(ctx, t, target, "att/fallback", subject,
		"application/vnd.dev.sigstore.bundle.v0.3+json",
		map[string]string{"dev.sigstore.bundle.predicateType": "https://slsa.dev/provenance/v1"},
		"application/vnd.dev.sigstore.bundle.v0.3+json", []byte(`{}`))

	// A registry without the referrers API answers 404 for it.
	upstream, _ := url.Parse("http://" + reg.Host)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	var apiCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/referrers/") {
			apiCalls.Add(1)
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	got, err := Inspect(ctx, open, Subject{host + "/att/fallback", "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if apiCalls.Load() == 0 {
		t.Error("referrers API was not tried")
	}
	if len(got) != 1 || got[0].Digest != ref.Digest.String() || got[0].Kind != KindSLSA {
		t.Fatalf("got %+v", got)
	}
}

func TestInspectCosignTag(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushImage(ctx, t, target, "att/cosign", "v1", []byte("layer"))
	open := plainOpener(nil)
	subject := resolve(t, open, reg.Host+"/att/cosign", "v1")
	fixtures.PushCosignAtt(ctx, t, target, "att/cosign", subject.Digest, "https://spdx.dev/Document", "https://slsa.dev/provenance/v0.2")

	got, err := Inspect(ctx, open, Subject{reg.Host + "/att/cosign", "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	kinds := map[Kind]bool{}
	for _, e := range got {
		if e.Source != SourceCosignTag || e.Signed != SignedYes {
			t.Errorf("entry %+v", e)
		}
		kinds[e.Kind] = true
	}
	if !kinds[KindSBOM] || !kinds[KindSLSA] {
		t.Errorf("kinds = %v", kinds)
	}
}

func TestInspectBuildx(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushBuildxIndex(ctx, t, target, "att/buildx", "v1",
		[]string{"linux/amd64", "linux/arm64"},
		[]string{"https://spdx.dev/Document", "https://slsa.dev/provenance/v0.2"})

	got, err := Inspect(ctx, plainOpener(nil), Subject{reg.Host + "/att/buildx", "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %+v", got)
	}
	count := map[string]int{}
	for _, e := range got {
		if e.Source != SourceBuildx || e.Signed != SignedNo {
			t.Errorf("entry %+v", e)
		}
		count[e.Platform+"|"+string(e.Kind)]++
	}
	for _, p := range []string{"linux/amd64", "linux/arm64"} {
		for _, k := range []Kind{KindSBOM, KindSLSA} {
			if count[p+"|"+string(k)] != 1 {
				t.Errorf("count[%s %s] = %d; all: %v", p, k, count[p+"|"+string(k)], count)
			}
		}
	}
}

func TestInspectBuildLevel(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushImage(ctx, t, target, "att/level", "v1", []byte("layer"))
	open := plainOpener(nil)
	subject := resolve(t, open, reg.Host+"/att/level", "v1")
	st := `{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://slsa.dev/verification_summary/v1",` +
		`"predicate":{"verificationResult":"PASSED","verifiedLevels":["SLSA_BUILD_LEVEL_3"]}}`
	bundle := `{"dsseEnvelope":{"payloadType":"application/vnd.in-toto+json","payload":"` + base64.StdEncoding.EncodeToString([]byte(st)) + `"}}`
	fixtures.PushReferrer(ctx, t, target, "att/level", subject, "application/vnd.dev.sigstore.bundle.v0.3+json",
		map[string]string{"dev.sigstore.bundle.predicateType": "https://slsa.dev/verification_summary/v1"},
		"application/vnd.dev.sigstore.bundle.v0.3+json", []byte(bundle))

	got, err := Inspect(ctx, open, Subject{reg.Host + "/att/level", "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != KindSLSA || got[0].Format != "SLSA VSA v1" || got[0].BuildLevel != 3 {
		t.Fatalf("got %+v", got)
	}
}

// cdxTwo is a CycloneDX 1.6 document of two components.
const cdxTwo = `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[` +
	`{"type":"library","bom-ref":"a","name":"alpha","version":"1.0.0"},` +
	`{"type":"library","bom-ref":"b","name":"beta","version":"2.0.0"}]}`

// cdxStatement wraps cdxTwo in an in-toto statement.
const cdxStatement = `{"_type":"https://in-toto.io/Statement/v1","subject":[],"predicateType":"https://cyclonedx.org/bom","predicate":` + cdxTwo + `}`

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "document", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// pushCosignLayer pushes a cosign "<alg>-<hex>.att" manifest with one layer.
func pushCosignLayer(t *testing.T, open RepositoryOpener, repository string, subject ocispec.Descriptor, layer ocispec.Descriptor, body []byte) {
	t.Helper()
	ctx := t.Context()
	repo, err := open(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	layer.Digest, layer.Size = digest.FromBytes(body), int64(len(body))
	if err := repo.Push(ctx, layer, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	m, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_0, "", oras.PackManifestOptions{Layers: []ocispec.Descriptor{layer}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Tag(ctx, m, strings.ReplaceAll(subject.Digest.String(), ":", "-")+".att"); err != nil {
		t.Fatal(err)
	}
}

func TestInspectSBOMSummary(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushImage(ctx, t, target, "att/sbomsum", "v1", []byte("layer"))
	open := plainOpener(nil)
	subject := resolve(t, open, reg.Host+"/att/sbomsum", "v1")
	fixtures.PushReferrer(ctx, t, target, "att/sbomsum", subject, "application/vnd.in-toto+json",
		map[string]string{"in-toto.io/predicate-type": "https://cyclonedx.org/bom"}, "application/vnd.in-toto+json", []byte(cdxStatement))

	got, err := Inspect(ctx, open, Subject{reg.Host + "/att/sbomsum", "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if s := got[0].Summary; s == nil || *s != (document.Summary{Format: "CycloneDX 1.6", Packages: 2}) {
		t.Errorf("summary = %+v", s)
	}
	if got[0].Repository != reg.Host+"/att/sbomsum" {
		t.Errorf("repository = %q", got[0].Repository)
	}
}

func TestInspectSARIFSummary(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushImage(ctx, t, target, "att/sarifsum", "v1", []byte("layer"))
	open := plainOpener(nil)
	subject := resolve(t, open, reg.Host+"/att/sarifsum", "v1")
	fixtures.PushReferrer(ctx, t, target, "att/sarifsum", subject, "application/sarif+json", nil,
		"application/sarif+json", readTestdata(t, "trivy.sarif.json"))

	got, err := Inspect(ctx, open, Subject{reg.Host + "/att/sarifsum", "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if s := got[0].Summary; s == nil || s.Tool != "Trivy" || s.ImportantFindings != 2 {
		t.Errorf("summary = %+v", s)
	}
}

func TestInspectSLSABuilder(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushImage(ctx, t, target, "att/builder", "v1", []byte("layer"))
	open := plainOpener(nil)
	subject := resolve(t, open, reg.Host+"/att/builder", "v1")
	const builder = "https://example.com/builders/ci@v1"
	st := `{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://slsa.dev/provenance/v1",` +
		`"predicate":{"runDetails":{"builder":{"id":"` + builder + `"}}}}`
	envelope := `{"payloadType":"application/vnd.in-toto+json","payload":"` + base64.StdEncoding.EncodeToString([]byte(st)) + `","signatures":[]}`
	pushCosignLayer(t, open, reg.Host+"/att/builder", subject, ocispec.Descriptor{
		MediaType:   "application/vnd.dsse.envelope.v1+json",
		Annotations: map[string]string{"predicateType": "https://slsa.dev/provenance/v1"},
	}, []byte(envelope))

	got, err := Inspect(ctx, open, Subject{reg.Host + "/att/builder", "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != KindSLSA || got[0].Source != SourceCosignTag || got[0].Builder != builder || got[0].Summary != nil {
		t.Fatalf("got %+v", got)
	}
}

// A buildx child without os or arch has no platform, never "/" or "linux/".
func TestPlatformString(t *testing.T) {
	for _, tc := range []struct {
		p    ocispec.Platform
		want string
	}{
		{ocispec.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}, "linux/arm64/v8"},
		{ocispec.Platform{OS: "linux", Architecture: "amd64"}, "linux/amd64"},
		{ocispec.Platform{OS: "linux"}, ""},
		{ocispec.Platform{Architecture: "amd64"}, ""},
		{ocispec.Platform{}, ""},
	} {
		if got := platformString(&tc.p); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.p, got, tc.want)
		}
	}
}
