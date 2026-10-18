package ocm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	ocmcredentials "ocm.software/open-component-model/bindings/go/credentials"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocicompref "ocm.software/open-component-model/bindings/go/oci/compref"
	ocicredentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	ociidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ocirepositoryv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	componentresolvers "ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// LocationKey normalises a repository (ocm CLI syntax) to the location string
// the index uses: "oci::" + repositoryKey.
func LocationKey(repo string) (string, error) {
	spec, err := ocicompref.ParseRepository(repo)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRepositoryInvalid, err)
	}
	oci, err := toOCI(spec)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRepositoryInvalid, err)
	}
	// ParseRepository accepts almost any string; require a real host.
	base := oci.BaseUrl
	if _, rest, ok := strings.Cut(base, "://"); ok {
		base = rest
	}
	if u, err := url.Parse("oci://" + base); err != nil || !strings.ContainsFunc(u.Hostname(), func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		return "", fmt.Errorf("%w: %q has no valid host", ErrRepositoryInvalid, repo)
	}
	return "oci::" + repositoryKey(oci), nil
}

// At opens location for component without consulting resolvers or the
// allow-list: callers pass locations from the index or ones they validated.
func (r *Resolver) At(ctx context.Context, component, version, location string) (Repo, error) {
	spec, err := ocicompref.ParseRepository(location)
	if err != nil {
		return Repo{}, fmt.Errorf("%w: %v", ErrRepositoryInvalid, err)
	}
	oci, err := toOCI(spec)
	if err != nil {
		return Repo{}, fmt.Errorf("%w: %v", ErrRepositoryInvalid, err)
	}
	chain, err := componentresolvers.New(ctx, componentresolvers.Options{
		RepoProvider:      r.b.Plugins.ComponentVersionRepositoryRegistry,
		CredentialGraph:   r.b.Credentials,
		ComponentPatterns: []string{component},
	}, oci)
	if err != nil {
		return Repo{}, upstreamError(err)
	}
	cvr, err := chain.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return Repo{}, upstreamError(err)
	}
	return Repo{Key: repositoryKey(oci), Spec: oci, CVR: cvr}, nil
}

// CheckRepository validates a ?repository= against the configured resolvers.
func (s *Service) CheckRepository(repo string) error { return s.r.CheckRepository(repo) }

// DescriptorAt fetches component:version from location.
func (s *Service) DescriptorAt(ctx context.Context, component, version, location string) (_ *descriptorruntime.Descriptor, err error) {
	ref := Ref{Component: component, Version: version, Repository: location}
	ctx, end := startSpan(ctx, "DescriptorAt", ref)
	defer end(&err)
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	repo, err := s.r.At(ctx, component, version, location)
	if err != nil {
		return nil, err
	}
	return getDescriptor(ctx, repo.CVR, ref)
}

// Located is a descriptor and the location it was read from.
type Located struct {
	Location   string
	Descriptor *descriptorruntime.Descriptor
}

// HasVersionAt reports whether the index has component:version at location.
type HasVersionAt func(ctx context.Context, component, version, location string) (bool, error)

// DescriptorTreeAt returns the descriptor of ref, read at ref.Repository
// (required), and recursively of every component version it references:
// depth-first in componentReferences order (the order of `ocm get cv
// --recursive`), each name:version once; a cycle ends at the first repeat.
// Each reference is read from the root's location when has says it is
// there, otherwise through the resolvers; Location tells which.
//
// The bindings' dag/sync.GraphDiscoverer is not used: it deadlocks on cycles.
func (s *Service) DescriptorTreeAt(ctx context.Context, ref Ref, has HasVersionAt) (_ []Located, err error) {
	ctx, end := startSpan(ctx, "DescriptorTreeAt", ref)
	defer end(&err)
	if ref.Repository == "" {
		return nil, errors.New("DescriptorTreeAt: repository is required")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	selected, err := LocationKey(ref.Repository)
	if err != nil {
		return nil, err
	}
	root, err := s.locatedDescriptor(ctx, ref.Component, ref.Version, selected, nil)
	if err != nil {
		return nil, err
	}
	var out []Located
	seen := map[string]bool{}
	var walk func(l Located) error
	walk = func(l Located) error {
		d := l.Descriptor
		id := d.Component.Name + ":" + d.Component.Version
		if seen[id] {
			return nil
		}
		seen[id] = true
		out = append(out, l)
		refs := d.Component.References
		children := make([]*Located, len(refs))
		errs := make([]error, len(refs))
		forEachLimit(len(refs), treeConcurrency, func(i int) {
			rf := refs[i]
			if seen[rf.Component+":"+rf.Version] {
				return
			}
			c, err := s.locatedDescriptor(ctx, rf.Component, rf.Version, selected, has)
			if err != nil {
				errs[i] = fmt.Errorf("reference %q (%s:%s) of %s: %w", rf.Name, rf.Component, rf.Version, id, err)
				return
			}
			children[i] = &c
		})
		for _, e := range errs {
			if e != nil {
				return e
			}
		}
		for _, c := range children {
			if c != nil {
				if err := walk(*c); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return out, nil
}

// locatedDescriptor reads component:version from selected when has is nil
// (the root) or says it is there, else through the resolvers.
func (s *Service) locatedDescriptor(ctx context.Context, component, version, selected string, has HasVersionAt) (Located, error) {
	there := has == nil
	if !there {
		ok, err := has(ctx, component, version, selected)
		if err != nil {
			return Located{}, err
		}
		there = ok
	}
	var repo Repo
	var err error
	if there {
		repo, err = s.r.At(ctx, component, version, selected)
	} else {
		repo, err = s.r.Resolve(ctx, component, version, "")
	}
	if err != nil {
		return Located{}, err
	}
	d, err := getDescriptor(ctx, repo.CVR, Ref{Component: component, Version: version})
	if err != nil {
		return Located{}, err
	}
	return Located{Location: "oci::" + repo.Key, Descriptor: d}, nil
}

// RegistryPassword returns the password (or access token) .ocmconfig holds
// for an OCI registry host, "" if there is none. cmd/sync uses it as the
// GitHub API token for ghcr.io.
func (b *Bindings) RegistryPassword(ctx context.Context, host string) (string, error) {
	consumer, err := ociidentityv1.IdentityFromOCIRepository(&ocirepositoryv1.Repository{BaseUrl: "https://" + host})
	if err != nil {
		return "", err
	}
	typed, err := b.Credentials.Resolve(ctx, consumer)
	if errors.Is(err, ocmcredentials.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolving credentials for %s: %w", host, err)
	}
	creds, err := ocicredentialsv1.ConvertToOCICredentials(typed)
	if err != nil {
		return "", err
	}
	if creds.AccessToken != "" {
		return creds.AccessToken, nil
	}
	return creds.Password, nil
}

// RepositoryOpener opens oras repositories ("host/path", optionally with an
// http:// or https:// prefix as in index locations) for attestation lookups.
// Credentials come from .ocmconfig per host, as in RegistryPassword; none
// means anonymous. Hosts given as http:// and loopback hosts use plain HTTP.
// Only trusted index locations carry a scheme: the attestation package
// rejects descriptor imageReferences that have one.
func (b *Bindings) RepositoryOpener() attestation.RepositoryOpener {
	return func(ctx context.Context, repository string) (*remote.Repository, error) {
		plain := false
		if rest, ok := strings.CutPrefix(repository, "http://"); ok {
			repository, plain = rest, true
		} else {
			repository = strings.TrimPrefix(repository, "https://")
		}
		repo, err := remote.NewRepository(repository)
		if err != nil {
			return nil, err
		}
		host := repo.Reference.Registry
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
			plain = true
		}
		repo.PlainHTTP = plain
		scheme := "https://"
		if plain {
			scheme = "http://"
		}
		cred, err := b.registryCredential(ctx, scheme+repo.Reference.Registry)
		if err != nil {
			return nil, err
		}
		repo.Client = &auth.Client{
			Client:     retry.DefaultClient,
			Cache:      auth.NewCache(),
			Credential: hostCredential(repo.Reference.Host(), cred),
		}
		return repo, nil
	}
}

// hostCredential returns cred only for registry (the hostport oras asks for
// when talking to the repository), the empty credential for any other host.
func hostCredential(registry string, cred auth.Credential) auth.CredentialFunc {
	return func(_ context.Context, hostport string) (auth.Credential, error) {
		if hostport != registry {
			return auth.EmptyCredential, nil
		}
		return cred, nil
	}
}

// registryCredential resolves the .ocmconfig credentials for a registry base
// URL; the zero credential (anonymous) when none are configured.
func (b *Bindings) registryCredential(ctx context.Context, baseURL string) (auth.Credential, error) {
	consumer, err := ociidentityv1.IdentityFromOCIRepository(&ocirepositoryv1.Repository{BaseUrl: baseURL})
	if err != nil {
		return auth.EmptyCredential, err
	}
	typed, err := b.Credentials.Resolve(ctx, consumer)
	if errors.Is(err, ocmcredentials.ErrNotFound) {
		return auth.EmptyCredential, nil
	}
	if err != nil {
		return auth.EmptyCredential, fmt.Errorf("resolving credentials for %s: %w", baseURL, err)
	}
	creds, err := ocicredentialsv1.ConvertToOCICredentials(typed)
	if err != nil {
		return auth.EmptyCredential, err
	}
	return auth.Credential{Username: creds.Username, Password: creds.Password,
		RefreshToken: creds.RefreshToken, AccessToken: creds.AccessToken}, nil
}
