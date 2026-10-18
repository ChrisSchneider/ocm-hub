package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	genericconfigv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
)

const resourceTypeImage = "ociImage"

// timeout bounds each registry lookup and document read.
const timeout = 60 * time.Second

// Ref is "<repository>//<component>:<version>", as the ocm CLI writes it.
type Ref struct{ Repository, Component, Version string }

func (r Ref) String() string { return r.Repository + "//" + r.Component + ":" + r.Version }

// ParseRef parses "<repository>//<component>:<version>".
func ParseRef(s string) (Ref, error) {
	repo, cv, ok := strings.Cut(s, "//")
	name, version, ok2 := strings.Cut(cv, ":")
	if !ok || !ok2 || repo == "" || name == "" || version == "" {
		return Ref{}, fmt.Errorf("component reference %q: want <repository>//<component>:<version>", s)
	}
	return Ref{repo, name, version}, nil
}

// Image is an ociImage resource of the component.
type Image struct{ Name, Version, Reference string }

// SBOM is one SBOM the hub's attestation discovery found for an image:
// embedded (an sbom resource whose artifact-references label names the
// image) or in the registry (referrer, cosign tag, buildx).
type SBOM struct {
	Source   string // e.g. "embedded alpine-sbom", "referrer sha256:…"
	Platform string // buildx SBOMs only
	Packages []Package
	Err      error // read or parse failure; Packages is then empty
}

// Component is a component version as the checks see it.
type Component struct {
	Ref          Ref
	Descriptor   *descriptorruntime.Descriptor
	Images       []Image
	SBOMs        map[string][]SBOM // by image name
	Attestations attestation.Result
	// Open opens registry repositories with the .ocmconfig credentials.
	Open attestation.RepositoryOpener

	close func()
}

// Close releases the OCM bindings.
func (c *Component) Close() { c.close() }

// Load reads ref with the credentials of the .ocmconfig at configPath and
// reads the SBOMs of its images.
func Load(ctx context.Context, configPath string, ref Ref) (*Component, error) {
	cfg, err := ocm.LoadOCMConfig(configPath)
	if err != nil {
		return nil, err
	}
	if err := dropSigningCredentials(cfg); err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "ocm-compliance-")
	if err != nil {
		return nil, err
	}
	b, err := ocm.NewBindings(ctx, cfg, tempDir)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, err
	}
	closeAll := func() {
		b.Close(context.Background())
		os.RemoveAll(tempDir)
	}
	c, err := load(ctx, b, ref)
	if err != nil {
		closeAll()
		return nil, err
	}
	c.close = closeAll
	return c, nil
}

func load(ctx context.Context, b *ocm.Bindings, ref Ref) (*Component, error) {
	location, err := ocm.LocationKey(ref.Repository)
	if err != nil {
		return nil, err
	}
	r, err := ocm.NewResolver(ctx, b)
	if err != nil {
		return nil, err
	}
	repo, err := r.At(ctx, ref.Component, ref.Version, ref.Repository)
	if err != nil {
		return nil, err
	}
	d, err := repo.CVR.GetComponentVersion(ctx, ref.Component, ref.Version)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", ref, err)
	}
	open := b.RepositoryOpener()
	c := &Component{Ref: ref, Descriptor: d, Open: open, SBOMs: map[string][]SBOM{}}
	for i := range d.Component.Resources {
		res := &d.Component.Resources[i]
		if res.Type == resourceTypeImage {
			c.Images = append(c.Images, Image{Name: res.Name, Version: res.Version, Reference: imageReference(res)})
		}
	}
	// Label links are not read: the example links only VEX documents.
	c.Attestations, _ = attestation.Gatherer{Open: open, Timeout: timeout}.Gather(ctx, d, location)
	for _, lv := range c.Attestations.Levels {
		if lv.Resource == nil {
			continue // component level: not an image's SBOM
		}
		for _, e := range lv.Entries {
			if e.Kind != attestation.KindSBOM {
				continue
			}
			s := SBOM{Platform: e.Platform}
			var raw []byte
			if e.Source == attestation.SourceEmbedded {
				s.Source = "embedded " + e.Resource.Name
				raw, s.Err = downloadResource(ctx, b, repo, ref, d, e.Resource.Name)
			} else {
				s.Source = string(e.Source) + " " + e.Digest
				rctx, cancel := context.WithTimeout(ctx, timeout)
				raw, s.Err = attestation.Content(rctx, open, nil, e)
				cancel()
			}
			if s.Err == nil {
				var doc document.Document
				if doc, s.Err = document.Parse(raw); s.Err == nil {
					if doc.SBOM == nil {
						s.Err = fmt.Errorf("%s: no SBOM in the document", s.Source)
					}
					s.Packages = Packages(doc.SBOM)
				}
			}
			c.SBOMs[lv.Resource.Name] = append(c.SBOMs[lv.Resource.Name], s)
		}
	}
	return c, nil
}

// downloadResource reads the resource name of d, up to document.MaxSize.
func downloadResource(ctx context.Context, b *ocm.Bindings, repo ocm.Repo, ref Ref, d *descriptorruntime.Descriptor, name string) ([]byte, error) {
	var res *descriptorruntime.Resource
	for i := range d.Component.Resources {
		if d.Component.Resources[i].Name == name {
			res = &d.Component.Resources[i]
			break
		}
	}
	if res == nil {
		return nil, fmt.Errorf("resource %s not found", name)
	}
	data, err := b.Download(ctx, repo.CVR, ref.Component, ref.Version, res)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	rc, err := data.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, document.MaxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if len(raw) > document.MaxSize {
		return nil, document.ErrTooLarge
	}
	return raw, nil
}

// dropSigningCredentials removes credential consumers whose credentials are
// Sigstore OIDC token providers: the ocm CLI signs with them, the bindings the
// commands use have no plugin for them and would refuse the whole config.
func dropSigningCredentials(cfg *genericconfigv1.Config) error {
	for _, raw := range cfg.Configurations {
		if !strings.HasPrefix(raw.Type.String(), "credentials.config.ocm.software") {
			continue
		}
		var c map[string]any
		if err := json.Unmarshal(raw.Data, &c); err != nil {
			return fmt.Errorf("decode %s: %w", raw.Type, err)
		}
		consumers, _ := c["consumers"].([]any)
		kept := consumers[:0]
		for _, cons := range consumers {
			if !hasOIDCCredential(cons) {
				kept = append(kept, cons)
			}
		}
		if len(kept) == len(consumers) {
			continue
		}
		c["consumers"] = kept
		b, err := json.Marshal(c)
		if err != nil {
			return err
		}
		raw.Data = b
	}
	return nil
}

func hasOIDCCredential(consumer any) bool {
	m, _ := consumer.(map[string]any)
	creds, _ := m["credentials"].([]any)
	for _, cr := range creds {
		if t, _ := cr.(map[string]any)["type"].(string); strings.HasPrefix(t, "OIDCIdentityTokenProvider") {
			return true
		}
	}
	return false
}

// imageReference is the access' imageReference, "" for other access types.
func imageReference(res *descriptorruntime.Resource) string {
	if res.Access == nil {
		return ""
	}
	raw, err := json.Marshal(res.Access)
	if err != nil {
		return ""
	}
	var a struct {
		ImageReference string `json:"imageReference"`
	}
	_ = json.Unmarshal(raw, &a)
	return a.ImageReference
}
