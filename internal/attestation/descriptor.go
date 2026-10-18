package attestation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strings"

	"ocm.software/open-component-model/bindings/go/oci"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	repopath "ocm.software/open-component-model/bindings/go/oci/spec/repository/path"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Label names read by FromDescriptor.
const (
	LabelVEX                = "catalog.chrisschneider.dev/vex"
	LabelArtifactReferences = "ocm.software/artifact-references"
)

const resourceTypeSBOM = "sbom"

var ociManifestTypes = map[string]bool{
	"application/vnd.oci.image.manifest.v1+json":                true,
	"application/vnd.oci.image.index.v1+json":                   true,
	"application/vnd.docker.distribution.manifest.v2+json":      true,
	"application/vnd.docker.distribution.manifest.list.v2+json": true,
}

// FromDescriptor reads the attestations a descriptor declares itself: embedded
// sbom resources and VEX label links. It returns one Subject per Level (zero:
// no registry lookup) and warnings for labels it skipped. location is the
// index location, "oci::host/path".
func FromDescriptor(d *descriptorruntime.Descriptor, location string) (Result, []Subject, []string) {
	var warnings []string
	warn := func(name, format string, args ...any) {
		warnings = append(warnings, name+": "+fmt.Sprintf(format, args...))
	}
	component := &d.Component

	compRepo := componentRepository(location, component.Name)
	compLevel := Level{Status: StatusNotOCI}
	compSubject := Subject{}
	if compRepo != "" {
		compLevel.Status = StatusOK
		compSubject = Subject{compRepo, oci.LooseSemverToOCITag(context.Background(), component.Version)}
	}

	r := Result{Levels: []Level{compLevel}}
	subjects := []Subject{compSubject}
	var sboms []descriptorruntime.Resource
	for _, res := range component.Resources {
		if res.Type == resourceTypeSBOM {
			sboms = append(sboms, res)
			continue
		}
		lv := Level{Resource: identity(res.ElementMeta)}
		sub, err := resourceSubject(res.Access, compRepo)
		switch {
		case err != nil:
			lv.Status, lv.Error = StatusFailed, err.Error()
		case sub != (Subject{}):
			lv.Status = StatusOK
		default:
			lv.Status = StatusNotOCI
		}
		r.Levels = append(r.Levels, lv)
		subjects = append(subjects, sub)
	}

	signed := SignedNo
	if len(d.Signatures) > 0 {
		signed = SignedByComponent
	}
	for _, res := range sboms {
		e := Entry{Kind: KindSBOM, Source: SourceEmbedded, Resource: identity(res.ElementMeta), Signed: signed}
		e.Type = mediaType(res.Access)
		if k, f := Classify(e.Type); k == KindSBOM {
			e.Format = f
		}
		placed := false
		for _, l := range res.Labels {
			if l.Name != LabelArtifactReferences {
				continue
			}
			// bindings v0.0.8 drops Label.Version when converting descriptors,
			// so "no version" must be accepted alongside v1alpha1.
			if l.Version != "" && l.Version != "v1alpha1" {
				warn(l.Name, "version %q is not supported (only v1alpha1 or no version is supported); label skipped", l.Version)
				continue
			}
			refs, err := parseArtifactReferences(l.Value)
			if err != nil {
				warn(l.Name, "%v; label skipped", err)
				continue
			}
			for i := 1; i < len(r.Levels); i++ {
				if matchesAny(refs, r.Levels[i].Resource) {
					r.Levels[i].Entries = append(r.Levels[i].Entries, e)
					placed = true
				}
			}
		}
		if !placed {
			r.Levels[0].Entries = append(r.Levels[0].Entries, e)
		}
	}

	addVEX := func(lv *Level, labels []descriptorruntime.Label) {
		for _, l := range labels {
			if l.Name != LabelVEX {
				continue
			}
			if l.Version != "" && l.Version != "v1" {
				warn(l.Name, "version %q is not supported (only v1 or no version is supported); label skipped", l.Version)
				continue
			}
			u, err := VEXURL(l.Value)
			if err != nil {
				warn(l.Name, "%v; label skipped", err)
				continue
			}
			lv.Entries = append(lv.Entries, Entry{Kind: KindVEX, Source: SourceLabelLink, URL: u, Label: LabelVEX, Signed: SignedNo})
		}
	}
	addVEX(&r.Levels[0], component.Labels)
	i := 1
	for _, res := range component.Resources {
		if res.Type == resourceTypeSBOM {
			continue
		}
		addVEX(&r.Levels[i], res.Labels)
		i++
	}
	return r, subjects, warnings
}

// componentRepository is the registry repository holding a component's
// descriptors, "" when location is not an "oci::" location.
func componentRepository(location, name string) string {
	base, ok := strings.CutPrefix(location, "oci::")
	if !ok || base == "" {
		return ""
	}
	return strings.TrimSuffix(base, "/") + "/" + repopath.DefaultComponentDescriptorPath + "/" + name
}

// ErrResourceUnknown means no resource of the descriptor has the identity.
var ErrResourceUnknown = errors.New("resource not in descriptor")

// ResourceSubject is the registry subject of the resource with identity id
// (name plus exact extra identity); the zero Subject means not OCI.
func ResourceSubject(d *descriptorruntime.Descriptor, location string, id ocmruntime.Identity) (Subject, error) {
	extra := maps.Clone(id)
	delete(extra, "name")
	for _, res := range d.Component.Resources {
		if res.Name != id["name"] {
			continue
		}
		if len(extra) == 0 && len(res.ExtraIdentity) == 0 || maps.Equal(extra, res.ExtraIdentity) {
			return resourceSubject(res.Access, componentRepository(location, d.Component.Name))
		}
	}
	return Subject{}, ErrResourceUnknown
}

func identity(m descriptorruntime.ElementMeta) *Identity {
	return &Identity{Name: m.Name, Version: m.Version, ExtraIdentity: maps.Clone(m.ExtraIdentity)}
}

func localBlob(access ocmruntime.Typed) (descriptorv2.LocalBlob, bool) {
	var lb descriptorv2.LocalBlob
	if access == nil || descriptorv2.Scheme.Convert(access, &lb) != nil {
		return lb, false
	}
	return lb, true
}

func mediaType(access ocmruntime.Typed) string {
	lb, _ := localBlob(access)
	return lb.MediaType
}

// resourceSubject returns the registry subject of an OCI resource: an OCIImage
// access, or a local blob holding an OCI manifest or index. The zero Subject
// means not OCI; an error means the access cannot be looked up.
func resourceSubject(access ocmruntime.Typed, compRepo string) (Subject, error) {
	if access == nil {
		return Subject{}, nil
	}
	var img ociaccessv1.OCIImage
	if ociaccess.Scheme.Convert(access, &img) == nil {
		return parseImageReference(img.ImageReference)
	}
	if lb, ok := localBlob(access); ok && compRepo != "" && ociManifestTypes[lb.MediaType] && strings.Contains(lb.LocalReference, ":") {
		return Subject{compRepo, lb.LocalReference}, nil
	}
	return Subject{}, nil
}

// errImageReferenceScheme rejects "http://..." image references: whether a
// registry is spoken to over plain HTTP must never come from descriptor content.
var errImageReferenceScheme = errors.New("imageReference must not contain a scheme")

// parseImageReference splits "[host/]path[:tag][@digest]"; a digest wins over
// a tag. A reference without a registry host is normalised as docker does:
// "alpine" is docker.io/library/alpine.
func parseImageReference(ref string) (Subject, error) {
	if strings.Contains(ref, "://") {
		return Subject{}, errImageReferenceScheme
	}
	name, digest, hasDigest := strings.Cut(ref, "@")
	tag := ""
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		name, tag = name[:i], name[i+1:]
	}
	if name == "" {
		return Subject{}, nil
	}
	name = normaliseRepository(name)
	switch {
	case hasDigest && digest != "":
		return Subject{name, digest}, nil
	case tag != "":
		return Subject{name, tag}, nil
	}
	return Subject{name, "latest"}, nil
}

// normaliseRepository prefixes docker.io (and library/ for single-segment
// names) when the first segment is not a registry host, the way docker does.
func normaliseRepository(name string) string {
	first, rest, ok := strings.Cut(name, "/")
	if ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return name
	}
	if !ok {
		return "docker.io/library/" + first
	}
	return "docker.io/" + first + "/" + rest
}

// Matching follows the "Lookup Algorithm" of ocm-spec 06-conventions.md. The
// binding bindings/go/descriptor/runtime/labels/artifactref/v1alpha1 is not in
// bindings v0.0.8; switch to it once the module is upgraded.
func parseArtifactReferences(raw []byte) ([]map[string]string, error) {
	var v []struct {
		Identity map[string]string `json:"identity"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	var refs []map[string]string
	for _, e := range v {
		if e.Identity["name"] != "" {
			refs = append(refs, e.Identity)
		}
	}
	return refs, nil
}

// matchesAny: name equal, version equal if set, and the remaining keys exactly
// equal to the subject's extraIdentity.
func matchesAny(refs []map[string]string, id *Identity) bool {
	for _, ref := range refs {
		if ref["name"] != id.Name {
			continue
		}
		if v, ok := ref["version"]; ok && v != id.Version {
			continue
		}
		extra := maps.Clone(ref)
		delete(extra, "name")
		delete(extra, "version")
		if len(extra) == 0 && len(id.ExtraIdentity) == 0 || maps.Equal(extra, id.ExtraIdentity) {
			return true
		}
	}
	return false
}

// VEXURL returns the https URL of a VEX label value.
func VEXURL(raw []byte) (string, error) {
	var v struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	u, err := url.Parse(v.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("url %q must be an https URL", v.URL)
	}
	return v.URL, nil
}
