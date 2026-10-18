package attestation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"

	"chrisschneider.dev/delivery-bundler/internal/document"
)

// maxStatementSize is the largest in-toto statement fetched to learn its
// predicate type or SLSA details. SBOM, VEX and scan documents are read up to
// document.MaxSize to summarise them.
const maxStatementSize = 256 << 10

const (
	mediaTypeInToto = "application/vnd.in-toto+json"
	mediaTypeDSSE   = "application/vnd.dsse.envelope.v1+json"
	typeSigstore    = "application/vnd.dev.sigstore.bundle"
)

// Annotation keys that name a predicate type, in order of preference.
var predicateAnnotations = []string{
	"dev.sigstore.bundle.predicateType",
	"in-toto.io/predicate-type",
	"predicateType",
}

// RepositoryOpener opens an oras repository ("host/path") for reading.
type RepositoryOpener func(ctx context.Context, repository string) (*remote.Repository, error)

// Inspect lists the attestations a registry holds for s: OCI referrers (with
// the tag-schema fallback handled by oras), a cosign "<alg>-<hex>.att" tag and
// buildx attestation manifests of an image index. Nothing is verified.
func Inspect(ctx context.Context, open RepositoryOpener, s Subject) ([]Entry, error) {
	repo, err := open(ctx, s.Repository)
	if err != nil {
		return nil, err
	}
	subject, err := repo.Resolve(ctx, s.Reference)
	if err != nil {
		return nil, fmt.Errorf("resolve %s:%s: %w", s.Repository, s.Reference, err)
	}

	entries := []Entry{}
	var refs []ocispec.Descriptor
	err = repo.Referrers(ctx, subject, "", func(page []ocispec.Descriptor) error {
		refs = append(refs, page...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list referrers: %w", err)
	}
	for _, r := range refs {
		e, err := referrerEntry(ctx, repo, r)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}

	att, err := cosignEntries(ctx, repo, subject)
	if err != nil {
		return nil, err
	}
	entries = append(entries, att...)

	if subject.MediaType == ocispec.MediaTypeImageIndex || subject.MediaType == "application/vnd.docker.distribution.manifest.list.v2+json" {
		bx, err := buildxEntries(ctx, repo, subject)
		if err != nil {
			return nil, err
		}
		entries = append(entries, bx...)
	}
	for i := range entries {
		entries[i].Repository = s.Repository
	}
	return entries, nil
}

func newEntry(typ string, src Source, signed Signed, dgst string) Entry {
	kind, format := Classify(typ)
	return Entry{Kind: kind, Format: format, Type: typ, Source: src, Signed: signed, Digest: dgst}
}

func fetchManifest(ctx context.Context, repo *remote.Repository, d ocispec.Descriptor) (ocispec.Manifest, error) {
	var m ocispec.Manifest
	b, err := content.FetchAll(ctx, repo, d)
	if err != nil {
		return m, fmt.Errorf("fetch manifest %s: %w", d.Digest, err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("decode manifest %s: %w", d.Digest, err)
	}
	return m, nil
}

func annotatedType(a map[string]string) string {
	for _, k := range predicateAnnotations {
		if v := a[k]; v != "" {
			return v
		}
	}
	return ""
}

func isSignedMedia(mt string) bool {
	return mt == mediaTypeDSSE || strings.HasPrefix(mt, typeSigstore)
}

// layerType returns the predicate type of an attestation layer: from its
// annotations, else from the in-toto statement if it is small enough. ok is
// false when the layer was a candidate but its type could not be learned.
func layerType(ctx context.Context, repo *remote.Repository, l ocispec.Descriptor) (typ string, ok bool, err error) {
	if t := annotatedType(l.Annotations); t != "" {
		return t, true, nil
	}
	if l.MediaType != mediaTypeInToto || l.Size > maxStatementSize {
		return "", false, nil
	}
	b, err := content.FetchAll(ctx, repo, l)
	if err != nil {
		return "", false, fmt.Errorf("fetch statement %s: %w", l.Digest, err)
	}
	st, ok := decodeStatement(l.MediaType, b)
	if !ok {
		return "", false, nil
	}
	return st.PredicateType, true, nil
}

// summarised are the kinds whose document withDetails reads for a summary.
var summarised = map[Kind]bool{
	KindSBOM: true, KindVEX: true, KindCVE: true, KindFIPS: true, KindSTIG: true, KindSecrets: true, KindVirus: true, KindCompliance: true, KindOther: true,
}

// withDetails reads what an entry's layers say beyond their type. slsa: the
// build level and builder of the first statement (up to maxStatementSize)
// that names either. Documents (summarised kinds of known type): the
// summary of the first JSON layer, read up to document.MaxSize. Errors are
// skipped: details are not worth failing the level lookup for.
func withDetails(ctx context.Context, repo *remote.Repository, e Entry, layers ...ocispec.Descriptor) Entry {
	switch {
	case e.Kind == KindSLSA:
		for _, l := range layers {
			if l.Size > maxStatementSize || (l.MediaType != mediaTypeInToto && !isSignedMedia(l.MediaType)) {
				continue
			}
			b, err := content.FetchAll(ctx, repo, l)
			if err != nil {
				continue
			}
			if st, ok := decodeStatement(l.MediaType, b); ok {
				if n, id := provenance(st.PredicateType, st.Predicate); n > 0 || id != "" {
					e.BuildLevel, e.Builder = n, id
					return e
				}
			}
		}
	case summarised[e.Kind] && e.Type != "":
		// An unknown type ("") is a statement too large to learn it from;
		// it is not read for a summary either.
		for _, l := range layers {
			if !strings.Contains(l.MediaType, "json") {
				continue
			}
			if l.Size <= document.MaxSize {
				e.Summary = summarise(ctx, repo, l)
			}
			return e
		}
	}
	return e
}

// summarise parses a layer, through its predicate when it is a statement.
// nil when it cannot be read or parsed.
func summarise(ctx context.Context, repo *remote.Repository, l ocispec.Descriptor) *document.Summary {
	b, err := content.FetchAll(ctx, repo, l)
	if err != nil {
		return nil
	}
	if st, ok := decodeStatement(l.MediaType, b); ok {
		b = st.Predicate
	}
	d, err := document.Parse(b)
	if err != nil {
		return nil
	}
	sum := d.Summary()
	return &sum
}

func referrerEntry(ctx context.Context, repo *remote.Repository, r ocispec.Descriptor) (Entry, error) {
	m, err := fetchManifest(ctx, repo, r)
	if err != nil {
		return Entry{}, err
	}
	signed := SignedNo
	if isSignedMedia(r.ArtifactType) {
		signed = SignedYes
	}
	for _, l := range m.Layers {
		if isSignedMedia(l.MediaType) {
			signed = SignedYes
		}
	}

	typ := annotatedType(r.Annotations)
	if typ == "" {
		typ = annotatedType(m.Annotations)
	}
	if typ == "" {
		if k, _ := Classify(r.ArtifactType); k != KindOther {
			typ = r.ArtifactType
		}
	}
	if typ == "" {
		for _, l := range m.Layers {
			t, ok, err := layerType(ctx, repo, l)
			if err != nil {
				return Entry{}, err
			}
			if ok {
				typ = t
				break
			}
		}
	}
	if typ == "" {
		// Unknown: keep the artifact type unless we only had a statement to go by.
		typ = r.ArtifactType
		for _, l := range m.Layers {
			if l.MediaType == mediaTypeInToto {
				typ = ""
			}
		}
	}
	return withDetails(ctx, repo, newEntry(typ, SourceReferrer, signed, r.Digest.String()), m.Layers...), nil
}

func cosignEntries(ctx context.Context, repo *remote.Repository, subject ocispec.Descriptor) ([]Entry, error) {
	tag := strings.ReplaceAll(subject.Digest.String(), ":", "-") + ".att"
	d, err := repo.Resolve(ctx, tag)
	if errors.Is(err, errdef.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", tag, err)
	}
	m, err := fetchManifest(ctx, repo, d)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, l := range m.Layers {
		t, _, err := layerType(ctx, repo, l)
		if err != nil {
			return nil, err
		}
		out = append(out, withDetails(ctx, repo, newEntry(t, SourceCosignTag, SignedYes, l.Digest.String()), l))
	}
	return out, nil
}

func buildxEntries(ctx context.Context, repo *remote.Repository, index ocispec.Descriptor) ([]Entry, error) {
	b, err := content.FetchAll(ctx, repo, index)
	if err != nil {
		return nil, fmt.Errorf("fetch index %s: %w", index.Digest, err)
	}
	var idx ocispec.Index
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, fmt.Errorf("decode index %s: %w", index.Digest, err)
	}
	platforms := map[string]string{}
	for _, c := range idx.Manifests {
		if c.Platform != nil && c.Annotations["vnd.docker.reference.type"] == "" {
			platforms[c.Digest.String()] = platformString(c.Platform)
		}
	}
	var out []Entry
	for _, c := range idx.Manifests {
		if c.Annotations["vnd.docker.reference.type"] != "attestation-manifest" {
			continue
		}
		m, err := fetchManifest(ctx, repo, c)
		if err != nil {
			return nil, err
		}
		platform := platforms[c.Annotations["vnd.docker.reference.digest"]]
		for _, l := range m.Layers {
			t, _, err := layerType(ctx, repo, l)
			if err != nil {
				return nil, err
			}
			e := withDetails(ctx, repo, newEntry(t, SourceBuildx, SignedNo, l.Digest.String()), l)
			e.Platform = platform
			out = append(out, e)
		}
	}
	return out, nil
}

// platformString is "os/arch[/variant]"; "" when os or arch is missing.
func platformString(p *ocispec.Platform) string {
	if p.OS == "" || p.Architecture == "" {
		return ""
	}
	s := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		s += "/" + p.Variant
	}
	return s
}
