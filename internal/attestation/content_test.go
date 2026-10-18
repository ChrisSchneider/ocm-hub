package attestation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"

	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
)

func noFetch(t *testing.T) document.FetchFunc {
	return func(_ context.Context, url string) ([]byte, error) {
		t.Errorf("fetched %s", url)
		return nil, errors.New("unexpected")
	}
}

func wantTwoPackages(t *testing.T, b []byte, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	d, err := document.Parse(b)
	if err != nil {
		t.Fatalf("parse %q: %v", b, err)
	}
	if d.SBOM == nil || len(d.SBOM.Packages) != 2 {
		t.Fatalf("document = %+v", d)
	}
}

func TestContentReferrer(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushImage(ctx, t, target, "att/contentref", "v1", []byte("layer"))
	open := plainOpener(nil)
	subject := resolve(t, open, reg.Host+"/att/contentref", "v1")
	fixtures.PushReferrer(ctx, t, target, "att/contentref", subject, "application/vnd.in-toto+json",
		map[string]string{"in-toto.io/predicate-type": "https://cyclonedx.org/bom"}, "application/vnd.in-toto+json", []byte(cdxStatement))

	got, err := Inspect(ctx, open, Subject{reg.Host + "/att/contentref", "v1"})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	b, err := Content(ctx, open, noFetch(t), got[0])
	wantTwoPackages(t, b, err)
}

// pushBuildxLayer pushes a one-platform buildx index whose attestation
// manifest has a single in-toto layer holding body.
func pushBuildxLayer(t *testing.T, open RepositoryOpener, repository, tag, predicateType string, body []byte) {
	t.Helper()
	ctx := t.Context()
	repo, err := open(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	img, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, "application/vnd.acme.test", oras.PackManifestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	img.Platform = &ocispec.Platform{OS: "linux", Architecture: "amd64"}
	layer := ocispec.Descriptor{MediaType: mediaTypeInToto, Digest: digest.FromBytes(body), Size: int64(len(body)),
		Annotations: map[string]string{"in-toto.io/predicate-type": predicateType}}
	if err := repo.Push(ctx, layer, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	ann := map[string]string{"vnd.docker.reference.type": "attestation-manifest", "vnd.docker.reference.digest": img.Digest.String()}
	att, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, "application/vnd.acme.attestation",
		oras.PackManifestOptions{Layers: []ocispec.Descriptor{layer}, ManifestAnnotations: ann})
	if err != nil {
		t.Fatal(err)
	}
	att.Annotations = ann
	idx, err := json.Marshal(ocispec.Index{Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{img, att}})
	if err != nil {
		t.Fatal(err)
	}
	d := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageIndex, Digest: digest.FromBytes(idx), Size: int64(len(idx))}
	if err := repo.PushReference(ctx, d, bytes.NewReader(idx), tag); err != nil {
		t.Fatal(err)
	}
}

func TestContentBuildx(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	open := plainOpener(nil)
	pushBuildxLayer(t, open, reg.Host+"/att/contentbx", "v1", "https://cyclonedx.org/bom", []byte(cdxStatement))

	got, err := Inspect(ctx, open, Subject{reg.Host + "/att/contentbx", "v1"})
	if err != nil || len(got) != 1 || got[0].How != HowBuildx {
		t.Fatalf("got %+v, %v", got, err)
	}
	if s := got[0].Summary; s == nil || s.Packages != 2 {
		t.Errorf("summary = %+v", s)
	}
	b, err := Content(ctx, open, noFetch(t), got[0])
	wantTwoPackages(t, b, err)
}

func labelEntry(b []byte) Entry {
	sum := sha256.Sum256(b)
	return Entry{Kind: KindVEX, How: HowLabelLink, URL: "https://example.com/doc.json", Label: LabelVEX,
		Signed: SignedNo, Digest: "sha256:" + hex.EncodeToString(sum[:])}
}

func TestContentLabelLink(t *testing.T) {
	doc := []byte(cdxTwo)
	b, err := Content(t.Context(), nil, func(_ context.Context, url string) ([]byte, error) {
		if url != "https://example.com/doc.json" {
			t.Errorf("fetched %s", url)
		}
		return doc, nil
	}, labelEntry(doc))
	wantTwoPackages(t, b, err)
}

func TestContentLabelLinkChanged(t *testing.T) {
	_, err := Content(t.Context(), nil, func(context.Context, string) ([]byte, error) {
		return []byte(`{"changed":true}`), nil
	}, labelEntry([]byte(cdxTwo)))
	if !errors.Is(err, ErrDocumentChanged) {
		t.Fatalf("err = %v", err)
	}
}

func TestContentNoContent(t *testing.T) {
	for name, e := range map[string]Entry{
		"embedded":           {Kind: KindSBOM, How: HowEmbedded, Resource: &Identity{Name: "sbom"}},
		"label without hash": {Kind: KindVEX, How: HowLabelLink, URL: "https://example.com/doc.json"},
		"referrer no digest": {Kind: KindSBOM, How: HowReferrer, Repository: "h/p"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Content(t.Context(), nil, noFetch(t), e); !errors.Is(err, ErrNoContent) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
