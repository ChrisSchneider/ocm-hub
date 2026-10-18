package ocm

import (
	"context"
	"fmt"
	"slices"
	"strings"

	ocicompref "ocm.software/open-component-model/bindings/go/oci/compref"
	ocirepositoryspec "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ocirepositoryv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocmrepository "ocm.software/open-component-model/bindings/go/repository"
	componentresolvers "ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Resolver picks the repository for a component using the bindings' resolver
// chain, built from the .ocmconfig exactly like the ocm CLI builds it.
type Resolver struct {
	b       *Bindings
	chain   componentresolvers.ComponentVersionRepositoryResolver
	allowed []string // repository keys of all configured resolvers
}

// NewResolver builds the resolver chain. It fails if no resolvers are
// configured or a resolver uses a repository type other than OCI.
func NewResolver(ctx context.Context, b *Bindings) (*Resolver, error) {
	fallback, matchers, err := componentresolvers.ExtractResolvers(b.Config, ocirepositoryspec.Scheme)
	if err != nil {
		return nil, fmt.Errorf("reading resolvers: %w", err)
	}
	var specs []ocmruntime.Typed
	for _, m := range matchers {
		specs = append(specs, m.Repository)
	}
	for _, f := range fallback {
		specs = append(specs, f.Repository)
	}
	var allowed []string
	for _, spec := range specs {
		repo, err := toOCI(spec)
		if err != nil {
			return nil, err
		}
		allowed = append(allowed, repositoryKey(repo))
	}
	if len(specs) == 0 {
		return &Resolver{b: b}, nil
	}
	chain, err := componentresolvers.New(ctx, componentresolvers.Options{
		RepoProvider:      b.Plugins.ComponentVersionRepositoryRegistry,
		CredentialGraph:   b.Credentials,
		PathMatchers:      matchers,
		FallbackResolvers: fallback,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("building resolver chain: %w", err)
	}
	return &Resolver{b: b, chain: chain, allowed: allowed}, nil
}

// Repo is a resolved repository.
type Repo struct {
	Key  string                                   // repositoryKey, for display and cache keys
	Spec *ocirepositoryv1.Repository              // OCI spec, for the tag lister
	CVR  ocmrepository.ComponentVersionRepository // the bindings' repository
}

// Repository returns the repository for component and its display key.
// version may be "" for version-less lookups (constrained resolvers are then
// skipped, as the bindings implement it). pinned is the ?repository= value.
func (r *Resolver) Repository(ctx context.Context, component, version, pinned string) (ocmrepository.ComponentVersionRepository, string, error) {
	repo, err := r.Resolve(ctx, component, version, pinned)
	if err != nil {
		return nil, "", err
	}
	return repo.CVR, repo.Key, nil
}

// Resolve is Repository returning the full resolution.
func (r *Resolver) Resolve(ctx context.Context, component, version, pinned string) (Repo, error) {
	if pinned != "" {
		return r.pinned(ctx, component, version, pinned)
	}
	if r.chain == nil {
		return Repo{}, fmt.Errorf("%w: no resolvers configured for %s", ErrComponentUnknown, component)
	}
	spec, err := r.chain.GetRepositorySpecificationForComponent(ctx, component, version)
	if err != nil {
		return Repo{}, fmt.Errorf("%w: no resolver matches %s: %v", ErrComponentUnknown, component, err)
	}
	oci, err := toOCI(spec)
	if err != nil {
		return Repo{}, upstreamError(err)
	}
	cvr, err := r.chain.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return Repo{}, upstreamError(err)
	}
	return Repo{Key: repositoryKey(oci), Spec: oci, CVR: cvr}, nil
}

// CheckRepository validates a ?repository= value: it must parse and name one
// of the configured repositories.
func (r *Resolver) CheckRepository(pinned string) error {
	_, err := r.allowedRepository(pinned)
	return err
}

func (r *Resolver) allowedRepository(pinned string) (*ocirepositoryv1.Repository, error) {
	spec, err := ocicompref.ParseRepository(pinned)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRepositoryInvalid, err)
	}
	oci, err := toOCI(spec)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRepositoryInvalid, err)
	}
	if key := repositoryKey(oci); !slices.Contains(r.allowed, key) {
		return nil, fmt.Errorf("%w: %s", ErrRepositoryNotAllowed, key)
	}
	return oci, nil
}

func (r *Resolver) pinned(ctx context.Context, component, version, pinned string) (Repo, error) {
	oci, err := r.allowedRepository(pinned)
	if err != nil {
		return Repo{}, err
	}
	key := repositoryKey(oci)
	// ComponentPatterns puts the pinned repository ahead of everything else —
	// the mechanism the ocm CLI uses for an explicit repository.
	chain, err := componentresolvers.New(ctx, componentresolvers.Options{
		RepoProvider:      r.b.Plugins.ComponentVersionRepositoryRegistry,
		CredentialGraph:   r.b.Credentials,
		ComponentPatterns: []string{component},
	}, oci)
	if err != nil {
		return Repo{}, upstreamError(err)
	}
	repo, err := chain.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return Repo{}, upstreamError(err)
	}
	return Repo{Key: key, Spec: oci, CVR: repo}, nil
}

func toOCI(spec ocmruntime.Typed) (*ocirepositoryv1.Repository, error) {
	obj, err := ocirepositoryspec.Scheme.NewObject(spec.GetType())
	if err != nil {
		return nil, fmt.Errorf("repository type %s not supported yet", spec.GetType())
	}
	repo, ok := obj.(*ocirepositoryv1.Repository)
	if !ok {
		return nil, fmt.Errorf("repository type %s not supported yet", spec.GetType())
	}
	if err := ocirepositoryspec.Scheme.Convert(spec, repo); err != nil {
		return nil, fmt.Errorf("reading repository spec: %w", err)
	}
	return repo, nil
}

// repositoryKey normalises an OCI repository spec for comparison and display.
// It is the SSRF gate for ?repository=, so it is deliberately conservative:
// only the scheme and host are case-folded (DNS names are case-insensitive),
// https is the default scheme and is dropped, http is kept, any other scheme
// is kept verbatim, ports are never defaulted (":443" differs from none), and
// path segments keep their case. A path in baseUrl and a subPath are
// equivalent.
func repositoryKey(r *ocirepositoryv1.Repository) string {
	base := r.BaseUrl
	scheme := ""
	if i := strings.Index(base, "://"); i >= 0 {
		scheme, base = strings.ToLower(base[:i+3]), base[i+3:]
	}
	if scheme == "https://" {
		scheme = ""
	}
	host, path, _ := strings.Cut(base, "/")
	var parts []string
	for _, p := range []string{path, r.SubPath} {
		if p = strings.Trim(p, "/"); p != "" {
			parts = append(parts, p)
		}
	}
	key := scheme + strings.ToLower(host)
	if len(parts) > 0 {
		key += "/" + strings.Join(parts, "/")
	}
	return key
}
