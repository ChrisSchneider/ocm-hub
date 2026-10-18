// Package ocm is the transport-agnostic core of the server: a thin layer over
// the OCM Go bindings, wired the same way the ocm CLI wires them.
package ocm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemconfigv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericconfigv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	ocmcredentials "ocm.software/open-component-model/bindings/go/credentials"
	credentialconfigruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	githubresourcerepository "ocm.software/open-component-model/bindings/go/github/repository/resource"
	githubcredentialsspec "ocm.software/open-component-model/bindings/go/github/spec/credentials"
	helmresourcerepository "ocm.software/open-component-model/bindings/go/helm/repository/resource"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpconfigv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ocicache "ocm.software/open-component-model/bindings/go/oci/cache"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	ocirepositoryprovider "ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ociresourcerepository "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	ocicredentialsspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	ociidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	pluginmanager "ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmrepository "ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	wgetresourcerepository "ocm.software/open-component-model/bindings/go/wget/repository"
	wgetcredentialsspec "ocm.software/open-component-model/bindings/go/wget/spec/credentials"
)

const userAgent = "ocm-server"

// LoadOCMConfig reads a standard .ocmconfig file.
func LoadOCMConfig(path string) (*genericconfigv1.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening ocm config: %w", err)
	}
	defer f.Close()
	var cfg genericconfigv1.Config
	if err := genericconfigv1.Scheme.Decode(f, &cfg); err != nil {
		return nil, fmt.Errorf("decoding ocm config %s: %w", path, err)
	}
	return genericconfigv1.FlatMap(&cfg), nil
}

// Bindings is the OCM machinery the server runs on. It mirrors the ocm CLI's
// setup (cli/cmd/setup, cli/internal/plugin/builtin), reduced to what reading
// component versions and downloading resources needs: no input methods,
// signing or transformers; the OCI blob cache is optional (WithBlobCache).
type Bindings struct {
	Config      *genericconfigv1.Config
	Plugins     *pluginmanager.PluginManager
	Credentials ocmcredentials.Resolver
	// HTTPClient is the client the bindings' OCI repositories use (built the
	// same way as in the OCI repository provider); the tag lister reuses it.
	HTTPClient *http.Client
}

// BindingsOption configures NewBindings.
type BindingsOption func(*bindingsOptions)

type bindingsOptions struct {
	blobCache *ocicache.Options
}

// WithBlobCache enables the bindings' OCI blob cache: manifests and
// descriptor blobs addressed by digest, on disk under opts.Dir/blobs.
func WithBlobCache(opts *ocicache.Options) BindingsOption {
	return func(o *bindingsOptions) { o.blobCache = opts }
}

// blobCacheTTL is effectively "no expiry": cached blobs are immutable, and
// ocicache replaces a TTL of 0 with its 10-minute default.
const blobCacheTTL = 10 * 365 * 24 * time.Hour

// BlobCacheOptions returns the bindings' blob cache options for the server.
func BlobCacheOptions(dir string, maxEntries int, maxBlobSize int64, remotePolicy string) *ocicache.Options {
	return &ocicache.Options{
		Dir:          dir,
		MaxEntries:   maxEntries,
		MaxBlobSize:  maxBlobSize,
		TTL:          blobCacheTTL,
		RemotePolicy: ocicache.RemotePolicy(remotePolicy),
	}
}

// NewBindings builds the plugin manager and credential graph from cfg.
// tempDir is used by plugins for temporary files.
func NewBindings(ctx context.Context, cfg *genericconfigv1.Config, tempDir string, opts ...BindingsOption) (*Bindings, error) {
	var o bindingsOptions
	for _, opt := range opts {
		opt(&o)
	}
	pm := pluginmanager.NewPluginManager(ctx)
	fsCfg := &filesystemconfigv1alpha1.Config{TempFolder: tempDir}
	httpCfg, err := httpconfigv1alpha1.ResolveHTTPConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolving http config: %w", err)
	}
	httpClient := ocmhttp.New(ocmhttp.WithConfig(httpCfg), ocmhttp.WithUserAgent(userAgent))
	if err := registerPlugins(pm, fsCfg, httpCfg, httpClient, o); err != nil {
		return nil, fmt.Errorf("registering plugins: %w", err)
	}

	credCfg, err := credentialconfigruntime.LookupCredentialConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("reading credential config: %w", err)
	}
	if credCfg == nil {
		credCfg = &credentialconfigruntime.Config{}
	}
	graph, err := ocmcredentials.ToGraph(ctx, credCfg, ocmcredentials.Options{
		RepositoryPluginProvider:       pm.CredentialRepositoryRegistry,
		CredentialPluginProvider:       pm.CredentialPluginRegistry,
		CredentialRepositoryTypeScheme: pm.CredentialRepositoryRegistry.RepositoryScheme(),
		CredentialTypeSchemeProvider:   pm.CredentialRepositoryRegistry,
	})
	if err != nil {
		return nil, fmt.Errorf("building credential graph: %w", err)
	}
	return &Bindings{Config: cfg, Plugins: pm, Credentials: graph, HTTPClient: httpClient}, nil
}

func registerPlugins(pm *pluginmanager.PluginManager, fsCfg *filesystemconfigv1alpha1.Config, httpCfg *httpconfigv1alpha1.Config, httpClient *http.Client, o bindingsOptions) error {
	pm.CredentialRepositoryRegistry.Register(ocicredentialsspec.CredentialTypeScheme)
	pm.CredentialRepositoryRegistry.Register(wgetcredentialsspec.Scheme)
	pm.CredentialRepositoryRegistry.Register(githubcredentialsspec.Scheme)
	providerOpts := []ocirepositoryprovider.Option{
		ocirepositoryprovider.WithTempDir(fsCfg.TempFolder),
		ocirepositoryprovider.WithUserAgent(userAgent),
		ocirepositoryprovider.WithHTTPConfig(httpCfg),
	}
	if o.blobCache != nil {
		providerOpts = append(providerOpts, ocirepositoryprovider.WithBlobCacheOptions(o.blobCache))
	}
	return errors.Join(
		pm.CredentialRepositoryRegistry.RegisterInternalCredentialRepositoryPlugin(
			&ocicredentials.OCICredentialRepository{}, []ocmruntime.Type{ociidentityv1.Type}),
		pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(
			ocirepositoryprovider.NewComponentVersionRepositoryProvider(providerOpts...)),
		pm.ResourcePluginRegistry.RegisterInternalResourcePlugin(
			ociresourcerepository.NewResourceRepository(fsCfg,
				ociresourcerepository.WithUserAgent(userAgent),
				ociresourcerepository.WithHTTPConfig(httpCfg))),
		pm.ResourcePluginRegistry.RegisterInternalResourcePlugin(
			helmresourcerepository.NewResourceRepository(fsCfg, helmresourcerepository.WithHTTPConfig(httpCfg))),
		pm.ResourcePluginRegistry.RegisterInternalResourcePlugin(
			wgetresourcerepository.NewResourceRepository(fsCfg, wgetresourcerepository.WithHTTPClient(httpClient))),
		pm.ResourcePluginRegistry.RegisterInternalResourcePlugin(
			githubresourcerepository.NewResourceRepository(githubresourcerepository.WithHTTPClient(httpClient))),
	)
}

// Close shuts the plugin manager down.
func (b *Bindings) Close(ctx context.Context) error { return b.Plugins.Shutdown(ctx) }

// Download returns the content of res, exactly as the ocm CLI's
// `download resource` obtains it (cli/cmd/download/shared.DownloadResourceData).
func (b *Bindings) Download(ctx context.Context, repo ocmrepository.ComponentVersionRepository, component, version string, res *descriptorruntime.Resource) (blob.ReadOnlyBlob, error) {
	if isLocal(res.Access) {
		data, _, err := repo.GetLocalResource(ctx, component, version, res.ToIdentity())
		return data, err
	}
	plugin, err := b.Plugins.ResourcePluginRegistry.GetResourcePlugin(ctx, res.Access)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrAccessTypeUnsupported, res.Access.GetType(), err)
	}
	var creds ocmruntime.Typed
	if id, idErr := plugin.GetResourceCredentialConsumerIdentity(ctx, res); idErr == nil {
		creds, err = b.Credentials.Resolve(ctx, id)
		if err != nil && !errors.Is(err, ocmcredentials.ErrNotFound) {
			return nil, fmt.Errorf("resolving credentials for resource %q: %w", res.Name, err)
		}
	}
	return plugin.DownloadResource(ctx, res, creds)
}

func isLocal(access ocmruntime.Typed) bool {
	if access == nil {
		return false
	}
	var local descriptorv2.LocalBlob
	return descriptorv2.Scheme.Convert(access, &local) == nil
}
