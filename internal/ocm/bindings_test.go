package ocm

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
	"ocm.software/open-component-model/bindings/go/blob"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	ocicache "ocm.software/open-component-model/bindings/go/oci/cache"
	ocicompref "ocm.software/open-component-model/bindings/go/oci/compref"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocirepositoryspec "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ocirepositoryv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	componentresolvers "ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

func newTestBindings(t *testing.T, ocmconfigPath string, opts ...BindingsOption) *Bindings {
	t.Helper()
	cfg, err := LoadOCMConfig(ocmconfigPath)
	if err != nil {
		t.Fatalf("LoadOCMConfig: %v", err)
	}
	b, err := NewBindings(t.Context(), cfg, t.TempDir(), opts...)
	if err != nil {
		t.Fatalf("NewBindings: %v", err)
	}
	t.Cleanup(func() { _ = b.Close(context.Background()) })
	return b
}

func readAll(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	if err != nil {
		t.Fatalf("ReadCloser: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return data
}

// tarEntries returns the regular files of a tar archive by cleaned name.
func tarEntries(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	entries := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar entry %s: %v", hdr.Name, err)
		}
		entries[path.Clean(hdr.Name)] = content
	}
}

func TestLoadOCMConfigFindsResolvers(t *testing.T) {
	path := fixtures.WriteOCMConfig(t, []fixtures.Resolver{{Pattern: "acme.org/*", Host: "127.0.0.1:5000", SubPath: "x"}}, nil)
	cfg, err := LoadOCMConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	fallback, matchers, err := componentresolvers.ExtractResolvers(cfg, ocirepositoryspec.Scheme)
	if err != nil {
		t.Fatal(err)
	}
	if len(fallback) != 0 || len(matchers) != 1 || matchers[0].ComponentNamePattern != "acme.org/*" {
		t.Fatalf("fallback=%d matchers=%+v", len(fallback), matchers)
	}
}

// TestParseRepositoryPlainHTTP pins how the bindings parse ?repository= values.
func TestParseRepositoryPlainHTTP(t *testing.T) {
	spec, err := ocicompref.ParseRepository("oci::http://localhost:5000/ocm")
	if err != nil {
		t.Fatalf("ParseRepository: %v", err)
	}
	var repo ocirepositoryv1.Repository
	if err := ocirepositoryspec.Scheme.Convert(spec, &repo); err != nil {
		t.Fatalf("convert: %v", err)
	}
	if repo.BaseUrl != "http://localhost:5000" || repo.SubPath != "ocm" {
		t.Fatalf("BaseUrl=%q SubPath=%q, want http://localhost:5000 and ocm", repo.BaseUrl, repo.SubPath)
	}
}

// TestDownloadAccessTypes verifies that every access type the spec promises
// downloads through the bindings wiring, from a plain-HTTP baseUrl.
func TestDownloadAccessTypes(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host, SubPath: "verify"}

	imageRef := fixtures.PushImage(ctx, t, fixtures.Target{Host: reg.Host}, "images/hello", "1.0.0", []byte("layer"))
	files := fixtures.FileServer(t, map[string][]byte{"/file.txt": []byte("from wget")})
	helm := fixtures.HelmRepo(t, "mychart", "0.1.0")

	fixtures.Push(ctx, t, target, fixtures.Component{
		Name: "acme.org/verify", Version: "1.0.0",
		Resources: []fixtures.Resource{
			{Name: "local", Type: "plainText", MediaType: "text/plain", Content: []byte("from local blob")},
			// Plain-HTTP registries need the scheme in the image reference: the
			// OCI URL resolver only switches to HTTP when ref.Scheme == "http".
			{Name: "image", Type: "ociImage", Access: &ociaccessv1.OCIImage{
				Type:           ocmruntime.NewVersionedType(ociaccessv1.OCIImageType, ociaccessv1.Version),
				ImageReference: "http://" + imageRef,
			}},
			{Name: "remote", Type: "blob", Access: &wgetaccessv1.Wget{
				Type: ocmruntime.NewVersionedType("Wget", wgetaccessv1.Version),
				URL:  files.URL + "/file.txt",
			}},
			{Name: "chart", Type: "helmChart", Access: &helmaccessv1.Helm{
				Type:           ocmruntime.NewVersionedType(helmaccessv1.Type, helmaccessv1.Version),
				HelmRepository: helm.URL,
				HelmChart:      "mychart",
				Version:        "0.1.0",
			}},
		},
	})

	b := newTestBindings(t, fixtures.WriteOCMConfig(t, []fixtures.Resolver{{Host: reg.Host, SubPath: "verify"}}, nil))
	fallback, matchers, err := componentresolvers.ExtractResolvers(b.Config, ocirepositoryspec.Scheme)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := componentresolvers.New(ctx, componentresolvers.Options{
		RepoProvider: b.Plugins.ComponentVersionRepositoryRegistry, CredentialGraph: b.Credentials,
		PathMatchers: matchers, FallbackResolvers: fallback,
	}, nil)
	if err != nil {
		t.Fatalf("resolvers.New: %v", err)
	}
	repo, err := chain.GetComponentVersionRepositoryForComponent(ctx, "acme.org/verify", "1.0.0")
	if err != nil {
		t.Fatalf("repository for component: %v", err)
	}
	desc, err := repo.GetComponentVersion(ctx, "acme.org/verify", "1.0.0")
	if err != nil {
		t.Fatalf("GetComponentVersion: %v", err)
	}

	want := map[string]func(*testing.T, []byte) bool{
		"local":  func(_ *testing.T, d []byte) bool { return string(d) == "from local blob" },
		"remote": func(_ *testing.T, d []byte) bool { return string(d) == "from wget" },
		// The OCI plugin returns the image as an OCI layout archive.
		"image": func(_ *testing.T, d []byte) bool { return len(d) > 0 },
		// The helm plugin returns a reproducible tar of its download directory,
		// which holds the packaged chart (plus the .prov file when one exists).
		"chart": func(t *testing.T, d []byte) bool {
			entries := tarEntries(t, d)
			t.Logf("chart tar entries: %v", slices.Sorted(maps.Keys(entries)))
			return bytes.HasPrefix(entries["mychart-0.1.0.tgz"], []byte{0x1f, 0x8b}) // gzip
		},
	}
	for i := range desc.Component.Resources {
		res := &desc.Component.Resources[i]
		t.Run(res.Name, func(t *testing.T) {
			data, err := b.Download(ctx, repo, "acme.org/verify", "1.0.0", res)
			if err != nil {
				t.Fatalf("Download: %v", err)
			}
			got := readAll(t, data)
			if !want[res.Name](t, got) {
				t.Fatalf("unexpected content (%d bytes): %.80q", len(got), got)
			}
		})
	}
}

// TestCredentialsFromOCMConfig verifies that an OCIRegistry consumer identity
// (hostname + port) in .ocmconfig authenticates against a plain-HTTP registry.
func TestCredentialsFromOCMConfig(t *testing.T) {
	zot.Shared(t) // skip in -short mode
	ctx := t.Context()
	reg, err := zot.Start(ctx, zot.WithHtpasswd("alice", "secret"))
	if err != nil {
		t.Fatalf("zot: %v", err)
	}
	t.Cleanup(func() { _ = reg.Stop(context.Background()) })

	target := fixtures.Target{Host: reg.Host, SubPath: "auth", Username: reg.Username, Password: reg.Password}
	fixtures.Push(ctx, t, target, fixtures.Component{
		Name: "acme.org/auth", Version: "1.0.0",
		Resources: []fixtures.Resource{{Name: "local", Type: "plainText", MediaType: "text/plain", Content: []byte("secret blob")}},
	})

	resolver := []fixtures.Resolver{{Host: reg.Host, SubPath: "auth"}}
	for name, creds := range map[string][]fixtures.Credential{
		"without credentials": nil,
		"with credentials":    {{Host: reg.Host, Username: reg.Username, Password: reg.Password}},
	} {
		t.Run(name, func(t *testing.T) {
			b := newTestBindings(t, fixtures.WriteOCMConfig(t, resolver, creds))
			_, matchers, err := componentresolvers.ExtractResolvers(b.Config, ocirepositoryspec.Scheme)
			if err != nil {
				t.Fatal(err)
			}
			id, err := b.Plugins.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, matchers[0].Repository)
			if err != nil {
				t.Fatalf("consumer identity: %v", err)
			}
			t.Logf("consumer identity: %v", id)
			chain, err := componentresolvers.New(ctx, componentresolvers.Options{
				RepoProvider: b.Plugins.ComponentVersionRepositoryRegistry, CredentialGraph: b.Credentials,
				PathMatchers: matchers,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			repo, err := chain.GetComponentVersionRepositoryForComponent(ctx, "acme.org/auth", "1.0.0")
			if err != nil {
				t.Fatalf("repository for component: %v", err)
			}
			desc, err := repo.GetComponentVersion(ctx, "acme.org/auth", "1.0.0")
			if creds == nil {
				if err == nil {
					t.Fatal("GetComponentVersion succeeded without credentials")
				}
				t.Logf("without credentials: %v", err)
				return
			}
			if err != nil {
				t.Fatalf("GetComponentVersion: %v", err)
			}
			data, err := b.Download(ctx, repo, "acme.org/auth", "1.0.0", &desc.Component.Resources[0])
			if err != nil {
				t.Fatalf("Download: %v", err)
			}
			if got := readAll(t, data); string(got) != "secret blob" {
				t.Fatalf("got %q", got)
			}
		})
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func TestBlobCacheEnabled(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host, SubPath: "blobcache"}
	fixtures.Push(ctx, t, target, fixtures.Component{Name: "acme.org/blobcache", Version: "1.0.0",
		Resources: []fixtures.Resource{{Name: "d", Type: "plainText", MediaType: "text/plain", Content: []byte("x")}}})
	dir := t.TempDir()
	b := newTestBindings(t, fixtures.WriteOCMConfig(t, []fixtures.Resolver{{Host: reg.Host, SubPath: "blobcache"}}, nil),
		WithBlobCache(BlobCacheOptions(dir, 100, 4<<20, "IfNotPresent")))
	if b.HTTPClient == nil {
		t.Fatal("HTTPClient not set")
	}
	r, err := NewResolver(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(b, r, 30*time.Second)
	ref := Ref{Component: "acme.org/blobcache", Version: "1.0.0"}
	first, err := s.GetComponentVersion(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if n := countFiles(t, filepath.Join(dir, "blobs")); n == 0 {
		t.Fatal("no blobs cached under dir/blobs")
	}
	second, err := s.GetComponentVersion(ctx, ref)
	if err != nil || second.Component.Name != first.Component.Name || second.Component.Version != "1.0.0" {
		t.Fatalf("second read: %v %v", second, err)
	}
}

func TestBlobCacheOptions(t *testing.T) {
	o := BlobCacheOptions("/c", 5, 7, "Always")
	if o.Dir != "/c" || o.MaxEntries != 5 || o.MaxBlobSize != 7 || o.RemotePolicy != ocicache.RemotePolicyAlways || o.TTL < 365*24*time.Hour {
		t.Fatalf("options = %+v", o)
	}
}

// TestRepositoryOpenerUsesOCMConfigCredentials verifies the opener authenticates
// against a plain-HTTP registry with htpasswd from .ocmconfig, and is anonymous
// (not an error to open) without credentials.
func TestRepositoryOpenerUsesOCMConfigCredentials(t *testing.T) {
	zot.Shared(t) // skip in -short mode
	ctx := t.Context()
	reg, err := zot.Start(ctx, zot.WithHtpasswd("alice", "secret"))
	if err != nil {
		t.Fatalf("zot: %v", err)
	}
	t.Cleanup(func() { _ = reg.Stop(context.Background()) })
	target := fixtures.Target{Host: reg.Host, SubPath: "opener", Username: reg.Username, Password: reg.Password}
	fixtures.Push(ctx, t, target, fixtures.Component{Name: "acme.org/opener", Version: "1.0.0"})

	resolver := []fixtures.Resolver{{Host: reg.Host, SubPath: "opener"}}
	repoName := reg.Host + "/opener/component-descriptors/acme.org/opener"
	for name, tc := range map[string]struct {
		creds   []fixtures.Credential
		wantErr bool
	}{
		"without credentials": {nil, true},
		"with credentials":    {[]fixtures.Credential{{Host: reg.Host, Username: reg.Username, Password: reg.Password}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			b := newTestBindings(t, fixtures.WriteOCMConfig(t, resolver, tc.creds))
			repo, err := b.RepositoryOpener()(ctx, repoName)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			_, err = repo.Resolve(ctx, "1.0.0")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Resolve error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
