package ocm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"oras.land/oras-go/v2/registry/remote/errcode"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmrepository "ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Ref addresses a component (and optionally a version) and an optional
// location to read it from (e.g. "oci::ghcr.io/acme"). The location is used
// as given — callers take it from the index or validate it first; without
// one the resolvers choose.
type Ref struct {
	Component  string
	Version    string
	Repository string
}

// Latest is the version keyword for the newest stable version.
const Latest = "latest"

// Resource is an opened resource download. The caller must close Body when it
// is non-nil; a resource opened without body (see OpenResource) has Body nil
// and holds no resources.
type Resource struct {
	Body      io.ReadCloser
	Name      string
	MediaType string // empty if unknown
	Size      int64  // -1 if unknown
	// Digest is the OCM descriptor digest of the resource, e.g.
	// "sha256:<hex>"; empty if the descriptor has none. For non-local access
	// types it is OCM's (possibly normalised) digest, not a hash of the
	// served bytes.
	Digest string
	// DigestMatchesBody reports whether Digest is the hash of exactly the
	// bytes in Body (true for localBlob only), i.e. usable as a strong ETag.
	DigestMatchesBody bool
}

// Service is the transport-agnostic API of the server.
type Service struct {
	b       *Bindings
	r       *Resolver
	timeout time.Duration
}

// ServiceOption configures NewService.
type ServiceOption func(*Service)

// NewService creates a Service. upstreamTimeout bounds metadata calls
// (descriptor, version list); resource bodies are not bounded.
func NewService(b *Bindings, r *Resolver, upstreamTimeout time.Duration, opts ...ServiceOption) *Service {
	s := &Service{b: b, r: r, timeout: upstreamTimeout}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// GetComponentVersion returns the component descriptor of ref.
func (s *Service) GetComponentVersion(ctx context.Context, ref Ref) (_ *descriptorruntime.Descriptor, err error) {
	ctx, end := startSpan(ctx, "GetComponentVersion", ref)
	defer end(&err)
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	desc, _, err := s.descriptor(ctx, ref)
	return desc, err
}

// descriptor reads ref at ref.Repository (a location the caller validated or
// took from the index) or, without one, through the resolvers.
func (s *Service) descriptor(ctx context.Context, ref Ref) (*descriptorruntime.Descriptor, ocmrepository.ComponentVersionRepository, error) {
	var repo Repo
	var err error
	if ref.Repository != "" {
		repo, err = s.r.At(ctx, ref.Component, ref.Version, ref.Repository)
	} else {
		repo, err = s.r.Resolve(ctx, ref.Component, ref.Version, "")
	}
	if err != nil {
		return nil, nil, err
	}
	desc, err := getDescriptor(ctx, repo.CVR, ref)
	if err != nil {
		return nil, nil, err
	}
	return desc, repo.CVR, nil
}

// getComponentVersion calls the bindings and turns a panic into an error:
// bindings v0.0.8 dereference nil when a tag names a manifest that is not a
// component descriptor (e.g. an OCI referrers index).
func getComponentVersion(ctx context.Context, repo ocmrepository.ComponentVersionRepository, ref Ref) (desc *descriptorruntime.Descriptor, err error) {
	defer func() {
		if r := recover(); r != nil {
			desc, err = nil, fmt.Errorf("%s:%s is not a component descriptor (bindings panicked: %v)", ref.Component, ref.Version, r)
		}
	}()
	return repo.GetComponentVersion(ctx, ref.Component, ref.Version)
}

// getDescriptor fetches the descriptor and classifies not-found as unknown
// component or unknown version; every other failure is an upstream error.
func getDescriptor(ctx context.Context, repo ocmrepository.ComponentVersionRepository, ref Ref) (*descriptorruntime.Descriptor, error) {
	desc, err := getComponentVersion(ctx, repo, ref)
	if err == nil {
		return desc, nil
	}
	if !isNotFound(err) {
		return nil, upstreamError(err)
	}
	versions, lerr := repo.ListComponentVersions(ctx, ref.Component)
	switch {
	case lerr != nil && !isNotFound(lerr):
		return nil, upstreamError(lerr)
	case lerr != nil || len(versions) == 0:
		return nil, fmt.Errorf("%w: %s", ErrComponentUnknown, ref.Component)
	}
	return nil, fmt.Errorf("%w: %s:%s", ErrVersionUnknown, ref.Component, ref.Version)
}

// OpenResource selects the resource matching identity (name plus extra
// identity, subset match as in the ocm CLI) and opens its content.
//
// With withBody false (HEAD) the content is not read: only metadata the blob
// provides cheaply is returned, Body is nil and the blob is released before
// returning. Note that for ociImage and helm the bindings' download itself
// already materialises the content (see the spec's "Verified binding
// behaviour").
func (s *Service) OpenResource(ctx context.Context, ref Ref, identity ocmruntime.Identity, withBody bool) (_ *Resource, err error) {
	ctx, end := startSpan(ctx, "OpenResource", ref)
	defer end(&err)
	metaCtx, cancel := context.WithTimeout(ctx, s.timeout)
	desc, repo, err := s.descriptor(metaCtx, ref)
	cancel()
	if err != nil {
		return nil, err
	}
	res, err := selectResource(desc.Component.Resources, identity)
	if err != nil {
		return nil, err
	}
	// The download uses the request context: large bodies must not be cut off
	// by the metadata timeout; client disconnects still cancel it.
	data, err := s.b.Download(ctx, repo, ref.Component, ref.Version, res)
	if err != nil {
		if errors.Is(err, ErrAccessTypeUnsupported) {
			return nil, err
		}
		return nil, upstreamError(err)
	}
	out := &Resource{Name: res.Name, Size: -1, Digest: resourceDigest(res), DigestMatchesBody: isLocal(res.Access)}
	if m, ok := data.(blob.MediaTypeAware); ok {
		if mt, known := m.MediaType(); known {
			out.MediaType = mt
		}
	}
	if sz, ok := data.(blob.SizeAware); ok {
		if n := sz.Size(); n >= 0 {
			out.Size = n
		}
	}
	if !withBody {
		if err := closeBlob(data); err != nil {
			// The metadata is complete; a failed cleanup is not the client's problem.
			slog.WarnContext(ctx, "releasing downloaded resource failed", slog.String("error", err.Error()))
		}
		return out, nil
	}
	rc, err := data.ReadCloser()
	if err != nil {
		return nil, upstreamError(errors.Join(err, closeBlob(data)))
	}
	out.Body = rc
	if c, ok := data.(io.Closer); ok {
		// Some blobs own resources (wget: a temp file); release them with
		// the body instead of leaving it to the garbage collector.
		out.Body = &blobBody{ReadCloser: rc, blob: c}
	}
	return out, nil
}

// closeBlob releases what a downloaded blob owns, if anything.
func closeBlob(data blob.ReadOnlyBlob) error {
	if c, ok := data.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// blobBody closes the reader and then the blob it reads from.
type blobBody struct {
	io.ReadCloser
	blob io.Closer
}

func (b *blobBody) Close() error {
	return errors.Join(b.ReadCloser.Close(), b.blob.Close())
}

// isNotFound reports whether err means the component does not exist. For a
// missing component the OCI binding's ListComponentVersions returns not
// repository.ErrNotFound but a wrapped registry error response with HTTP 404
// (NAME_UNKNOWN), so both are recognised by type.
func isNotFound(err error) bool {
	if errors.Is(err, ocmrepository.ErrNotFound) {
		return true
	}
	var resp *errcode.ErrorResponse
	return errors.As(err, &resp) && resp.StatusCode == http.StatusNotFound
}

func selectResource(resources []descriptorruntime.Resource, requested ocmruntime.Identity) (*descriptorruntime.Resource, error) {
	var matches []*descriptorruntime.Resource
	for i := range resources {
		if requested.Match(resources[i].ToIdentity(), ocmruntime.IdentityMatchingChainFn(ocmruntime.IdentitySubset)) {
			matches = append(matches, &resources[i])
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w: %v", ErrResourceUnknown, requested)
	case 1:
		return matches[0], nil
	default:
		amb := &AmbiguousResourceError{}
		for _, m := range matches {
			amb.Candidates = append(amb.Candidates, m.ToIdentity())
		}
		return nil, amb
	}
}

func resourceDigest(res *descriptorruntime.Resource) string {
	if res.Digest == nil || res.Digest.Value == "" {
		return ""
	}
	alg := strings.ToLower(strings.ReplaceAll(res.Digest.HashAlgorithm, "-", ""))
	return alg + ":" + res.Digest.Value
}
