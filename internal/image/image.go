// Package image reads the platforms and configuration of an OCI image from
// its registry. Nothing is verified.
package image

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"oras.land/oras-go/v2/registry/remote"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
)

// maxSize caps the manifests and image configs read.
const maxSize = 4 << 20

// ErrUnknownPlatform means the digest is not one Platforms returns.
var ErrUnknownPlatform = errors.New("digest is not a platform of this image")

// Platform is one runnable image: Platform is "os/arch[/variant]", or "" for a
// single manifest whose config lacks os or arch.
type Platform struct{ Platform, Digest string }

// Index is the image a subject names and the platforms it offers.
type Index struct {
	Repository, Digest, MediaType string
	Platforms                     []Platform
}

// Layer is one layer of an image.
type Layer struct {
	Digest    string
	Size      int64
	CreatedBy string
}

// Details is what an image manifest and its config say.
type Details struct {
	Digest, Platform, Created, User, WorkingDir string
	Size                                        int64 // config + layers, compressed
	Entrypoint, Cmd, ExposedPorts, Env          []string
	Labels                                      map[string]string
	Layers                                      []Layer
}

type imageConfig struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
	Created      string `json:"created"`
	Config       struct {
		User         string              `json:"User"`
		WorkingDir   string              `json:"WorkingDir"`
		Entrypoint   []string            `json:"Entrypoint"`
		Cmd          []string            `json:"Cmd"`
		Env          []string            `json:"Env"`
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		Labels       map[string]string   `json:"Labels"`
	} `json:"config"`
	History []struct {
		CreatedBy  string `json:"created_by"`
		EmptyLayer bool   `json:"empty_layer"`
	} `json:"history"`
}

// Platforms lists the platforms of the image s names: the children of an
// index (skipping buildx attestation manifests, unknown/unknown and children
// without an os or an architecture), or a single manifest as exactly one
// platform with the os/arch of its config ("" when os or arch is missing).
func Platforms(ctx context.Context, open attestation.RepositoryOpener, s attestation.Subject) (Index, error) {
	idx, _, err := platforms(ctx, open, s)
	return idx, err
}

func platforms(ctx context.Context, open attestation.RepositoryOpener, s attestation.Subject) (Index, *repoHandle, error) {
	repo, err := open(ctx, s.Repository)
	if err != nil {
		return Index{}, nil, err
	}
	h := &repoHandle{ctx: ctx, repo: repo}
	b, desc, err := h.fetch(s.Reference)
	if err != nil {
		return Index{}, nil, fmt.Errorf("fetch %s:%s: %w", s.Repository, s.Reference, err)
	}
	out := Index{Repository: s.Repository, Digest: desc.Digest.String(), MediaType: desc.MediaType}
	switch desc.MediaType {
	case ocispec.MediaTypeImageIndex, "application/vnd.docker.distribution.manifest.list.v2+json":
		var idx ocispec.Index
		if err := json.Unmarshal(b, &idx); err != nil {
			return Index{}, nil, fmt.Errorf("decode index %s: %w", desc.Digest, err)
		}
		for _, c := range idx.Manifests {
			p := c.Platform
			if p == nil || c.Annotations["vnd.docker.reference.type"] != "" || (p.OS == "unknown" && p.Architecture == "unknown") {
				continue
			}
			if ps, ok := platformString(p.OS, p.Architecture, p.Variant); ok {
				out.Platforms = append(out.Platforms, Platform{ps, c.Digest.String()})
			}
		}
	default:
		var m ocispec.Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			return Index{}, nil, fmt.Errorf("decode manifest %s: %w", desc.Digest, err)
		}
		cfg, err := h.config(m)
		if err != nil {
			return Index{}, nil, err
		}
		ps, _ := platformString(cfg.OS, cfg.Architecture, cfg.Variant)
		out.Platforms = []Platform{{ps, desc.Digest.String()}}
	}
	return out, h, nil
}

// Inspect reads the manifest and config of one platform digest of s.
func Inspect(ctx context.Context, open attestation.RepositoryOpener, s attestation.Subject, digest string) (Details, error) {
	h, mb, plat, err := platformManifest(ctx, open, s, digest)
	if err != nil {
		return Details{}, err
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return Details{}, fmt.Errorf("decode manifest %s: %w", digest, err)
	}
	cfg, err := h.config(m)
	if err != nil {
		return Details{}, err
	}
	d := Details{
		Digest: digest, Platform: plat, Created: cfg.Created,
		User: cfg.Config.User, WorkingDir: cfg.Config.WorkingDir,
		Size:       m.Config.Size,
		Entrypoint: cfg.Config.Entrypoint, Cmd: cfg.Config.Cmd, Env: cfg.Config.Env,
		Labels: cfg.Config.Labels,
	}
	for p := range cfg.Config.ExposedPorts {
		d.ExposedPorts = append(d.ExposedPorts, p)
	}
	slices.Sort(d.ExposedPorts)
	var created []string
	for _, e := range cfg.History {
		if !e.EmptyLayer {
			created = append(created, e.CreatedBy)
		}
	}
	for i, l := range m.Layers {
		layer := Layer{Digest: l.Digest.String(), Size: l.Size}
		if i < len(created) {
			layer.CreatedBy = created[i]
		}
		d.Layers = append(d.Layers, layer)
		d.Size += l.Size
	}
	return d, nil
}

// Raw returns the manifest or config ("manifest" or "config") of one platform
// digest of s.
func Raw(ctx context.Context, open attestation.RepositoryOpener, s attestation.Subject, digest, part string) ([]byte, error) {
	if part != "manifest" && part != "config" {
		return nil, fmt.Errorf("unknown part %q", part)
	}
	h, mb, _, err := platformManifest(ctx, open, s, digest)
	if err != nil {
		return nil, err
	}
	if part == "manifest" {
		return mb, nil
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("decode manifest %s: %w", digest, err)
	}
	b, err := h.fetchBlob(m.Config)
	if err != nil {
		return nil, fmt.Errorf("fetch config %s: %w", m.Config.Digest, err)
	}
	return b, nil
}

// platformManifest checks digest is a platform of s and returns its manifest
// and platform string.
func platformManifest(ctx context.Context, open attestation.RepositoryOpener, s attestation.Subject, digest string) (*repoHandle, []byte, string, error) {
	idx, h, err := platforms(ctx, open, s)
	if err != nil {
		return nil, nil, "", err
	}
	i := slices.IndexFunc(idx.Platforms, func(p Platform) bool { return p.Digest == digest })
	if i < 0 {
		return nil, nil, "", ErrUnknownPlatform
	}
	b, _, err := h.fetch(digest)
	if err != nil {
		return nil, nil, "", fmt.Errorf("fetch manifest %s: %w", digest, err)
	}
	return h, b, idx.Platforms[i].Platform, nil
}

// platformString is "os/arch[/variant]"; false when os or arch is empty.
func platformString(os, arch, variant string) (string, bool) {
	if os == "" || arch == "" {
		return "", false
	}
	s := os + "/" + arch
	if variant != "" {
		s += "/" + variant
	}
	return s, true
}

// repoHandle reads size-capped blobs and manifests of one repository.
type repoHandle struct {
	ctx  context.Context
	repo *remote.Repository
}

// fetch reads a manifest by tag or digest, up to maxSize.
func (h *repoHandle) fetch(ref string) ([]byte, ocispec.Descriptor, error) {
	desc, rc, err := h.repo.Manifests().FetchReference(h.ctx, ref)
	if err != nil {
		return nil, ocispec.Descriptor{}, err
	}
	return readCapped(ref, desc, rc)
}

// fetchBlob reads a blob, up to maxSize.
func (h *repoHandle) fetchBlob(desc ocispec.Descriptor) ([]byte, error) {
	rc, err := h.repo.Blobs().Fetch(h.ctx, desc)
	if err != nil {
		return nil, err
	}
	b, _, err := readCapped(desc.Digest.String(), desc, rc)
	return b, err
}

func readCapped(ref string, desc ocispec.Descriptor, rc io.ReadCloser) ([]byte, ocispec.Descriptor, error) {
	defer rc.Close()
	if desc.Size > maxSize {
		return nil, desc, fmt.Errorf("%s is %d bytes, over the %d byte limit", ref, desc.Size, maxSize)
	}
	b, err := io.ReadAll(io.LimitReader(rc, maxSize+1))
	if err != nil {
		return nil, desc, err
	}
	if len(b) > maxSize {
		return nil, desc, fmt.Errorf("%s is over the %d byte limit", ref, maxSize)
	}
	return b, desc, nil
}

func (h *repoHandle) config(m ocispec.Manifest) (imageConfig, error) {
	var cfg imageConfig
	b, err := h.fetchBlob(m.Config)
	if err != nil {
		return cfg, fmt.Errorf("fetch config %s: %w", m.Config.Digest, err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("decode config %s: %w", m.Config.Digest, err)
	}
	return cfg, nil
}
