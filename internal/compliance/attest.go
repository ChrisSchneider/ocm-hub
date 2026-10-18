package compliance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	ocmoci "ocm.software/open-component-model/bindings/go/oci"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry/remote"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
)

const (
	mediaTypeInToto = "application/vnd.in-toto+json"
	inTotoStatement = "https://in-toto.io/Statement/v1"
	// annotationPredicateType is read by the hub (internal/attestation).
	annotationPredicateType = "in-toto.io/predicate-type"
)

// componentRepository is where OCM stores a component's descriptor in an OCI
// registry: "<base>/component-descriptors/<component>".
func componentRepository(ref Ref) string {
	return strings.TrimSuffix(ref.Repository, "/") + "/component-descriptors/" + ref.Component
}

// descriptorManifest opens the component's repository and resolves the
// version's descriptor manifest, the subject of the attestations.
func (c *Component) descriptorManifest(ctx context.Context) (string, *remote.Repository, ocispec.Descriptor, error) {
	repoName := componentRepository(c.Ref)
	repo, err := c.Open(ctx, repoName)
	if err != nil {
		return "", nil, ocispec.Descriptor{}, err
	}
	// Registries without the referrers API (ghcr.io) get the tag-schema
	// index; ghcr.io refuses to delete the replaced index.
	repo.SkipReferrersGC = true
	tag := ocmoci.LooseSemverToOCITag(ctx, c.Ref.Version)
	subject, err := repo.Resolve(ctx, tag)
	if err != nil {
		return "", nil, ocispec.Descriptor{}, fmt.Errorf("resolve %s:%s: %w", repoName, tag, err)
	}
	return repoName, repo, subject, nil
}

// LatestAttestation returns the predicate of the newest referrer of the
// component version with predicateType (by its creation annotation) and its
// "repository@digest"; ok is false when there is none. The predicate is
// read with the hub's attestation.Content.
func (c *Component) LatestAttestation(ctx context.Context, predicateType string) (predicate []byte, at string, ok bool, err error) {
	repoName, repo, subject, err := c.descriptorManifest(ctx)
	if err != nil {
		return nil, "", false, err
	}
	var latest *ocispec.Descriptor
	err = repo.Referrers(ctx, subject, mediaTypeInToto, func(page []ocispec.Descriptor) error {
		for i := range page {
			d := page[i]
			if d.Annotations[annotationPredicateType] != predicateType {
				continue
			}
			if latest == nil || d.Annotations[ocispec.AnnotationCreated] > latest.Annotations[ocispec.AnnotationCreated] {
				latest = &d
			}
		}
		return nil
	})
	if err != nil {
		return nil, "", false, fmt.Errorf("list referrers of %s: %w", repoName, err)
	}
	if latest == nil {
		return nil, "", false, nil
	}
	e := attestation.Entry{Source: attestation.SourceReferrer, Digest: latest.Digest.String(), Repository: repoName}
	b, err := attestation.Content(ctx, c.Open, nil, e)
	if err != nil {
		return nil, "", false, err
	}
	return b, repoName + "@" + latest.Digest.String(), true, nil
}

// Attest pushes predicate as an unsigned in-toto statement and attaches it as
// an OCI referrer to the component version's descriptor manifest. It returns
// the "repository@digest" of the referrer manifest.
func (c *Component) Attest(ctx context.Context, predicateType string, predicate []byte) (string, error) {
	repoName, repo, subject, err := c.descriptorManifest(ctx)
	if err != nil {
		return "", err
	}
	statement, err := json.Marshal(map[string]any{
		"_type": inTotoStatement,
		"subject": []map[string]any{{
			"name":   c.Ref.Component + ":" + c.Ref.Version,
			"digest": map[string]string{subject.Digest.Algorithm().String(): subject.Digest.Encoded()},
		}},
		"predicateType": predicateType,
		"predicate":     json.RawMessage(predicate),
	})
	if err != nil {
		return "", err
	}
	layer := ocispec.Descriptor{MediaType: mediaTypeInToto, Digest: digest.FromBytes(statement), Size: int64(len(statement)),
		Annotations: map[string]string{annotationPredicateType: predicateType}}
	if err := repo.Push(ctx, layer, bytes.NewReader(statement)); err != nil {
		return "", fmt.Errorf("push statement: %w", err)
	}
	m, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, mediaTypeInToto, oras.PackManifestOptions{
		Subject: &subject,
		Layers:  []ocispec.Descriptor{layer},
		ManifestAnnotations: map[string]string{
			annotationPredicateType:   predicateType,
			ocispec.AnnotationCreated: time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return "", fmt.Errorf("push referrer manifest: %w", err)
	}
	return repoName + "@" + m.Digest.String(), nil
}
