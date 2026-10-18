package attestation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"

	"chrisschneider.dev/delivery-bundler/internal/document"
)

// maxManifestSize caps the attestation manifests Content reads.
const maxManifestSize = 4 << 20

var (
	// ErrNoContent means an entry has nothing Content can read: an embedded
	// SBOM (a resource) or an entry without a digest.
	ErrNoContent = errors.New("attestation has no content")
	// ErrDocumentChanged means a label link no longer serves the document
	// whose digest the sync recorded.
	ErrDocumentChanged = errors.New("linked document changed since the sync")
)

// Content returns the document an entry holds: the predicate when the bytes
// are an in-toto statement (plain, DSSE or sigstore bundle), else the bytes
// as stored. Referrers are read from the first layer of manifest e.Digest,
// cosign and buildx entries from blob e.Digest, label links from e.URL with
// fetch (and must still hash to e.Digest). Documents over document.MaxSize
// fail with document.ErrTooLarge. Nothing is verified.
func Content(ctx context.Context, open RepositoryOpener, fetch document.FetchFunc, e Entry) ([]byte, error) {
	if e.Digest == "" {
		return nil, ErrNoContent
	}
	switch e.How {
	case HowLabelLink:
		b, err := fetch(ctx, e.URL)
		if err != nil {
			return nil, err
		}
		if len(b) > document.MaxSize {
			return nil, document.ErrTooLarge
		}
		sum := sha256.Sum256(b)
		if "sha256:"+hex.EncodeToString(sum[:]) != e.Digest {
			return nil, ErrDocumentChanged
		}
		return b, nil
	case HowReferrer, HowCosignTag, HowBuildx:
		dgst, err := digest.Parse(e.Digest)
		if err != nil {
			return nil, fmt.Errorf("digest %q: %w", e.Digest, err)
		}
		repo, err := open(ctx, e.Repository)
		if err != nil {
			return nil, err
		}
		var layer ocispec.Descriptor
		if e.How == HowReferrer {
			if layer, err = firstLayer(ctx, repo, dgst); err != nil {
				return nil, err
			}
		} else if layer, err = repo.Blobs().Resolve(ctx, dgst.String()); err != nil {
			return nil, fmt.Errorf("resolve blob %s: %w", dgst, err)
		}
		return readDocument(ctx, repo, layer)
	}
	return nil, ErrNoContent
}

// firstLayer returns the first layer of manifest dgst.
func firstLayer(ctx context.Context, repo *remote.Repository, dgst digest.Digest) (ocispec.Descriptor, error) {
	d, err := repo.Resolve(ctx, dgst.String())
	if err != nil {
		return d, fmt.Errorf("resolve manifest %s: %w", dgst, err)
	}
	if d.Size > maxManifestSize {
		return d, fmt.Errorf("manifest %s is larger than %d bytes", dgst, maxManifestSize)
	}
	m, err := fetchManifest(ctx, repo, d)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	if len(m.Layers) == 0 {
		return ocispec.Descriptor{}, ErrNoContent
	}
	return m.Layers[0], nil
}

// readDocument fetches a layer and unwraps an in-toto statement. A blob
// resolved by digest carries no media type, so the statement forms are
// tried by content.
func readDocument(ctx context.Context, repo *remote.Repository, l ocispec.Descriptor) ([]byte, error) {
	if l.Size > document.MaxSize {
		return nil, document.ErrTooLarge
	}
	b, err := content.FetchAll(ctx, repo, l)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", l.Digest, err)
	}
	for _, mt := range []string{l.MediaType, mediaTypeInToto, mediaTypeDSSE} {
		if st, ok := decodeStatement(mt, b); ok {
			return st.Predicate, nil
		}
	}
	return b, nil
}
