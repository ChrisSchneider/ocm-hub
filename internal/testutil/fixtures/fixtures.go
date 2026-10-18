// Package fixtures pushes OCM component versions into test registries using
// the OCM bindings (never the ocm CLI) and writes matching .ocmconfig files.
package fixtures

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemblob "ocm.software/open-component-model/bindings/go/blob/filesystem"
	inmemoryblob "ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocirepo "ocm.software/open-component-model/bindings/go/oci"
	ociurlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Target is a repository (registry host + optional sub path) to push into.
type Target struct {
	Host     string // host:port, plain HTTP
	SubPath  string
	Username string
	Password string
}

// Resource is one resource of a fixture component version. Exactly one of
// Access (external access), Path (local blob from a file) or Content (local
// blob from memory) is used, in that order of precedence.
type Resource struct {
	Name          string
	Version       string // defaults to the component version
	Type          string // e.g. "plainText", "blob", "ociImage", "helmChart"
	MediaType     string // local blobs only
	ExtraIdentity map[string]string
	Content       []byte
	Path          string
	Access        ocmruntime.Typed
	Labels        []descriptorruntime.Label
}

// Reference is a component reference of a pushed component.
type Reference struct {
	Name      string // reference name
	Component string // referenced component name
	Version   string
}

// Component is a fixture component version.
type Component struct {
	Name       string
	Version    string
	Provider   string // defaults to "acme.org"
	Resources  []Resource
	Labels     []descriptorruntime.Label
	Sources    []descriptorruntime.Source
	References []Reference
}

func authClient(t Target) *auth.Client {
	c := &auth.Client{Client: retry.DefaultClient, Cache: auth.NewCache()}
	if t.Username != "" {
		c.Credential = auth.StaticCredential(t.Host, auth.Credential{Username: t.Username, Password: t.Password})
	}
	return c
}

// Repository opens target as an OCM repository via the bindings.
func Repository(t testing.TB, target Target) *ocirepo.Repository {
	t.Helper()
	opts := []ociurlresolver.Option{
		ociurlresolver.WithBaseURL(target.Host),
		ociurlresolver.WithPlainHTTP(true),
		ociurlresolver.WithBaseClient(authClient(target)),
	}
	if target.SubPath != "" {
		opts = append(opts, ociurlresolver.WithSubPath(target.SubPath))
	}
	res, err := ociurlresolver.New(opts...)
	if err != nil {
		t.Fatalf("url resolver: %v", err)
	}
	repo, err := ocirepo.NewRepository(ocirepo.WithResolver(res), ocirepo.WithTempDir(t.TempDir()))
	if err != nil {
		t.Fatalf("oci repository: %v", err)
	}
	return repo
}

// Push adds c (and its local blobs) to target.
func Push(ctx context.Context, t testing.TB, target Target, c Component) {
	t.Helper()
	repo := Repository(t, target)
	provider := c.Provider
	if provider == "" {
		provider = "acme.org"
	}
	desc := &descriptorruntime.Descriptor{
		Meta: descriptorruntime.Meta{Version: "v2"},
		Component: descriptorruntime.Component{
			Provider: descriptorruntime.Provider{Name: provider},
			ComponentMeta: descriptorruntime.ComponentMeta{
				ObjectMeta: descriptorruntime.ObjectMeta{Name: c.Name, Version: c.Version},
			},
		},
	}
	desc.Component.Labels = c.Labels
	desc.Component.Sources = c.Sources
	for _, r := range c.References {
		desc.Component.References = append(desc.Component.References, descriptorruntime.Reference{
			ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: r.Name, Version: r.Version}},
			Component:   r.Component,
			// The v2 schema has a non-optional digest object; a dummy one keeps
			// the descriptor valid (nothing here verifies it).
			Digest: descriptorruntime.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "jsonNormalisation/v4alpha1",
				Value:                  strings.Repeat("0", 64),
			},
		})
	}
	for _, r := range c.Resources {
		version := r.Version
		if version == "" {
			version = c.Version
		}
		res := descriptorruntime.Resource{
			ElementMeta: descriptorruntime.ElementMeta{
				ObjectMeta:    descriptorruntime.ObjectMeta{Name: r.Name, Version: version},
				ExtraIdentity: ocmruntime.Identity(r.ExtraIdentity),
			},
			Type: r.Type,
		}
		res.Labels = r.Labels
		if r.Access != nil {
			res.Relation = descriptorruntime.ExternalRelation
			res.Access = rawAccess(t, r.Access)
			desc.Component.Resources = append(desc.Component.Resources, res)
			continue
		}
		res.Relation = descriptorruntime.LocalRelation
		var (
			b   blob.ReadOnlyBlob
			dig string
		)
		if r.Path != "" {
			fb, err := filesystemblob.GetBlobFromOSPath(r.Path)
			if err != nil {
				t.Fatalf("blob from %s: %v", r.Path, err)
			}
			b, dig = fb, fileDigest(t, r.Path)
		} else {
			b, dig = inmemoryblob.New(bytes.NewReader(r.Content)), digest.FromBytes(r.Content).String()
		}
		res.Access = &descriptorv2.LocalBlob{LocalReference: dig, MediaType: r.MediaType}
		added, err := repo.AddLocalResource(ctx, c.Name, c.Version, &res, b)
		if err != nil {
			t.Fatalf("add local resource %s: %v", r.Name, err)
		}
		desc.Component.Resources = append(desc.Component.Resources, *added)
	}
	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		t.Fatalf("add component version %s:%s: %v", c.Name, c.Version, err)
	}
}

// rawAccess encodes an external access spec as runtime.Raw. The OCI
// repository's default scheme only knows OCI and localBlob access types, so a
// typed Wget or Helm spec would fail to encode into the v2 descriptor.
func rawAccess(t testing.TB, access ocmruntime.Typed) *ocmruntime.Raw {
	t.Helper()
	data, err := json.Marshal(access)
	if err != nil {
		t.Fatalf("encode access %s: %v", access.GetType(), err)
	}
	return &ocmruntime.Raw{Type: access.GetType(), Data: data}
}

func fileDigest(t testing.TB, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// PushImage pushes a minimal single-layer OCI artifact to target.Host/repoName:tag
// and returns its reference.
func PushImage(ctx context.Context, t testing.TB, target Target, repoName, tag string, layer []byte) string {
	t.Helper()
	repo, err := remote.NewRepository(target.Host + "/" + repoName)
	if err != nil {
		t.Fatal(err)
	}
	repo.PlainHTTP = true
	repo.Client = authClient(target)
	layerDesc, err := oras.PushBytes(ctx, repo, "application/vnd.oci.image.layer.v1.tar", layer)
	if err != nil {
		t.Fatalf("push layer: %v", err)
	}
	manifest, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, "application/vnd.acme.test",
		oras.PackManifestOptions{Layers: []ocispec.Descriptor{layerDesc}})
	if err != nil {
		t.Fatalf("pack manifest: %v", err)
	}
	if err := repo.Tag(ctx, manifest, tag); err != nil {
		t.Fatalf("tag: %v", err)
	}
	return target.Host + "/" + repoName + ":" + tag
}

// PushImageConfig pushes an OCI image manifest with the given config blob and
// one layer per element of layers to target.Host/repoName:tag, and returns its
// descriptor.
func PushImageConfig(ctx context.Context, t testing.TB, target Target, repoName, tag string, config []byte, layers ...[]byte) ocispec.Descriptor {
	t.Helper()
	return PushImageConfigAs(ctx, t, target, repoName, tag, ocispec.MediaTypeImageConfig, config, layers...)
}

// PushImageConfigAs is PushImageConfig with the config pushed as configMediaType.
func PushImageConfigAs(ctx context.Context, t testing.TB, target Target, repoName, tag, configMediaType string, config []byte, layers ...[]byte) ocispec.Descriptor {
	t.Helper()
	repo := plainRepo(t, target, repoName)
	cfg, err := oras.PushBytes(ctx, repo, configMediaType, config)
	if err != nil {
		t.Fatalf("push config: %v", err)
	}
	descs := []ocispec.Descriptor{}
	for _, l := range layers {
		d, err := oras.PushBytes(ctx, repo, ocispec.MediaTypeImageLayerGzip, l)
		if err != nil {
			t.Fatalf("push layer: %v", err)
		}
		descs = append(descs, d)
	}
	m := ocispec.Manifest{Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest, Config: cfg, Layers: descs}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	d := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageManifest, Digest: digest.FromBytes(body), Size: int64(len(body))}
	if err := repo.PushReference(ctx, d, bytes.NewReader(body), tag); err != nil {
		t.Fatalf("push manifest: %v", err)
	}
	return d
}

// Delete removes the manifest that a component version's tag points to (and
// with it the tag). version must be a plain OCI tag (no '+').
func Delete(ctx context.Context, t testing.TB, target Target, component, version string) {
	t.Helper()
	name := "component-descriptors/" + component
	if target.SubPath != "" {
		name = target.SubPath + "/" + name
	}
	repo, err := remote.NewRepository(target.Host + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	repo.PlainHTTP = true
	repo.Client = authClient(target)
	desc, err := repo.Resolve(ctx, version)
	if err != nil {
		t.Fatalf("resolve %s:%s: %v", component, version, err)
	}
	if err := repo.Delete(ctx, desc); err != nil {
		t.Fatalf("delete %s:%s: %v", component, version, err)
	}
}

func plainRepo(t testing.TB, target Target, repoName string) *remote.Repository {
	t.Helper()
	repo, err := remote.NewRepository(target.Host + "/" + repoName)
	if err != nil {
		t.Fatal(err)
	}
	repo.PlainHTTP = true
	repo.Client = authClient(target)
	return repo
}

// PushReferrer pushes an OCI 1.1 artifact whose subject is subject, with one
// layer of layerMediaType (none if layer is nil), and returns its descriptor.
func PushReferrer(ctx context.Context, t testing.TB, target Target, repoName string, subject ocispec.Descriptor, artifactType string, annotations map[string]string, layerMediaType string, layer []byte) ocispec.Descriptor {
	t.Helper()
	return pushReferrer(ctx, t, plainRepo(t, target, repoName), subject, artifactType, annotations, layerMediaType, layer)
}

// PushReferrerTagSchema is PushReferrer for registries without the referrers
// API: the referrer is indexed under the "sha256-<hex>" fallback tag.
func PushReferrerTagSchema(ctx context.Context, t testing.TB, target Target, repoName string, subject ocispec.Descriptor, artifactType string, annotations map[string]string, layerMediaType string, layer []byte) ocispec.Descriptor {
	t.Helper()
	repo := plainRepo(t, target, repoName)
	if err := repo.SetReferrersCapability(false); err != nil {
		t.Fatal(err)
	}
	return pushReferrer(ctx, t, repo, subject, artifactType, annotations, layerMediaType, layer)
}

func pushReferrer(ctx context.Context, t testing.TB, repo oras.Target, subject ocispec.Descriptor, artifactType string, annotations map[string]string, layerMediaType string, layer []byte) ocispec.Descriptor {
	t.Helper()
	opts := oras.PackManifestOptions{Subject: &subject, ManifestAnnotations: annotations}
	if layer != nil {
		d, err := oras.PushBytes(ctx, repo, layerMediaType, layer)
		if err != nil {
			t.Fatalf("push layer: %v", err)
		}
		opts.Layers = []ocispec.Descriptor{d}
	}
	m, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, artifactType, opts)
	if err != nil {
		t.Fatalf("pack referrer: %v", err)
	}
	return m
}

// PushCosignAtt pushes a cosign-style "<alg>-<hex>.att" attestation manifest
// for subject with one DSSE layer per predicate type.
func PushCosignAtt(ctx context.Context, t testing.TB, target Target, repoName string, subject digest.Digest, predicateTypes ...string) {
	t.Helper()
	repo := plainRepo(t, target, repoName)
	var layers []ocispec.Descriptor
	for _, pt := range predicateTypes {
		body := []byte(`{"payloadType":"application/vnd.in-toto+json","payload":"e30=","signatures":[]}`)
		d := ocispec.Descriptor{
			MediaType:   "application/vnd.dsse.envelope.v1+json",
			Digest:      digest.FromBytes(body),
			Size:        int64(len(body)),
			Annotations: map[string]string{"predicateType": pt},
		}
		if err := repo.Push(ctx, d, bytes.NewReader(body)); err != nil {
			t.Fatalf("push att layer: %v", err)
		}
		layers = append(layers, d)
	}
	m, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_0, "", oras.PackManifestOptions{Layers: layers})
	if err != nil {
		t.Fatalf("pack att: %v", err)
	}
	if err := repo.Tag(ctx, m, strings.ReplaceAll(subject.String(), ":", "-")+".att"); err != nil {
		t.Fatalf("tag att: %v", err)
	}
}

// PushBuildxIndex pushes a buildx-style index: one image manifest per platform
// ("os/arch") plus one attestation manifest per platform with one in-toto layer
// per predicate type. It tags the index and returns its descriptor.
func PushBuildxIndex(ctx context.Context, t testing.TB, target Target, repoName, tag string, platforms []string, predicateTypes []string) ocispec.Descriptor {
	t.Helper()
	repo := plainRepo(t, target, repoName)
	var children []ocispec.Descriptor
	for _, p := range platforms {
		os_, arch, _ := strings.Cut(p, "/")
		img, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, "application/vnd.acme.test",
			oras.PackManifestOptions{ManifestAnnotations: map[string]string{"test.platform": p}})
		if err != nil {
			t.Fatal(err)
		}
		img.Platform = &ocispec.Platform{OS: os_, Architecture: arch}
		children = append(children, img)

		var layers []ocispec.Descriptor
		for _, pt := range predicateTypes {
			body := []byte(`{"_type":"https://in-toto.io/Statement/v0.1","predicateType":"` + pt + `","platform":"` + p + `"}`)
			d := ocispec.Descriptor{
				MediaType:   "application/vnd.in-toto+json",
				Digest:      digest.FromBytes(body),
				Size:        int64(len(body)),
				Annotations: map[string]string{"in-toto.io/predicate-type": pt},
			}
			if err := repo.Push(ctx, d, bytes.NewReader(body)); err != nil {
				t.Fatal(err)
			}
			layers = append(layers, d)
		}
		att, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, "application/vnd.acme.attestation",
			oras.PackManifestOptions{Layers: layers, ManifestAnnotations: map[string]string{
				"vnd.docker.reference.type":   "attestation-manifest",
				"vnd.docker.reference.digest": img.Digest.String(),
			}})
		if err != nil {
			t.Fatal(err)
		}
		att.Annotations = map[string]string{
			"vnd.docker.reference.type":   "attestation-manifest",
			"vnd.docker.reference.digest": img.Digest.String(),
		}
		att.Platform = &ocispec.Platform{OS: "unknown", Architecture: "unknown"}
		children = append(children, att)
	}
	idx := ocispec.Index{Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageIndex, Manifests: children}
	body, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	d := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageIndex, Digest: digest.FromBytes(body), Size: int64(len(body))}
	if err := repo.PushReference(ctx, d, bytes.NewReader(body), tag); err != nil {
		t.Fatalf("push index: %v", err)
	}
	return d
}
