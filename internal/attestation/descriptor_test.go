package attestation

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"ocm.software/open-component-model/bindings/go/oci"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	dig1 = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	dig2 = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func desc(version string) *descriptorruntime.Descriptor {
	d := &descriptorruntime.Descriptor{}
	d.Component.Name, d.Component.Version = "acme.org/x", version
	return d
}

func res(name, version, typ string, extra map[string]string, access ocmruntime.Typed, labels ...descriptorruntime.Label) descriptorruntime.Resource {
	return descriptorruntime.Resource{
		ElementMeta: descriptorruntime.ElementMeta{
			ObjectMeta:    descriptorruntime.ObjectMeta{Name: name, Version: version, Labels: labels},
			ExtraIdentity: extra,
		},
		Type: typ, Access: access,
	}
}

func raw(typ, json string) ocmruntime.Typed {
	return &ocmruntime.Raw{Type: ocmruntime.NewUnversionedType(typ), Data: []byte(json)}
}

func image(ref string) ocmruntime.Typed {
	return raw("OCIImage", `{"type":"OCIImage/v1","imageReference":"`+ref+`"}`)
}

func blob(mediaType, ref string) ocmruntime.Typed {
	return raw("localBlob", `{"type":"localBlob/v1","localReference":"`+ref+`","mediaType":"`+mediaType+`"}`)
}

func lbl(name, version, value string) descriptorruntime.Label {
	return descriptorruntime.Label{Name: name, Version: version, Value: []byte(value)}
}

func TestFromDescriptorLevels(t *testing.T) {
	d := desc("1.0.0")
	d.Component.Resources = []descriptorruntime.Resource{
		res("api", "1.0.0", "ociImage", nil, image("ghcr.io/acme/api:1.0@"+dig1)),
		res("chart", "1.0.0", "helmChart", nil, blob("application/gzip", dig2)),
	}
	r, subj, warns := FromDescriptor(d, "oci::ghcr.io/acme")
	if len(warns) != 0 {
		t.Errorf("warnings: %v", warns)
	}
	if len(r.Levels) != 3 || len(subj) != 3 {
		t.Fatalf("levels = %d, subjects = %d", len(r.Levels), len(subj))
	}
	wantSubj := []Subject{
		{"ghcr.io/acme/component-descriptors/acme.org/x", "1.0.0"},
		{"ghcr.io/acme/api", dig1},
		{},
	}
	if !reflect.DeepEqual(subj, wantSubj) {
		t.Errorf("subjects = %+v", subj)
	}
	for i, want := range []Status{StatusOK, StatusOK, StatusNotOCI} {
		if r.Levels[i].Status != want {
			t.Errorf("level %d status = %s, want %s", i, r.Levels[i].Status, want)
		}
	}
	if r.Levels[0].Resource != nil || r.Levels[1].Resource.Name != "api" || r.Levels[2].Resource.Name != "chart" {
		t.Errorf("identities: %+v", r.Levels)
	}
}

func TestFromDescriptorLocalManifestAndTag(t *testing.T) {
	d := desc("1.0.0")
	d.Component.Resources = []descriptorruntime.Resource{
		res("img", "1", "ociImage", nil, blob("application/vnd.oci.image.manifest.v1+json", dig1)),
		res("tagged", "1", "ociImage", nil, image("ghcr.io/acme/t:2.0")),
	}
	_, subj, _ := FromDescriptor(d, "oci::ghcr.io/acme")
	if subj[1] != (Subject{"ghcr.io/acme/component-descriptors/acme.org/x", dig1}) {
		t.Errorf("local blob subject = %+v", subj[1])
	}
	if subj[2] != (Subject{"ghcr.io/acme/t", "2.0"}) {
		t.Errorf("tag subject = %+v", subj[2])
	}
}

func TestFromDescriptorEmbeddedSBOM(t *testing.T) {
	build := func() *descriptorruntime.Descriptor {
		d := desc("1.0.0")
		d.Component.Resources = []descriptorruntime.Resource{
			res("api", "1.0.0", "ociImage", nil, image("ghcr.io/acme/api@"+dig1)),
			res("sbom-api", "1.0.0", "sbom", nil, blob("application/vnd.cyclonedx+json", dig2),
				lbl(LabelArtifactReferences, "v1alpha1", `[{"identity":{"name":"api"}}]`)),
			res("sbom-all", "1.0.0", "sbom", nil, blob("application/spdx+json", dig2)),
		}
		return d
	}
	r, _, warns := FromDescriptor(build(), "oci::ghcr.io/acme")
	if len(warns) != 0 {
		t.Errorf("warnings: %v", warns)
	}
	if len(r.Levels) != 2 {
		t.Fatalf("sbom resources must not be levels: %d levels", len(r.Levels))
	}
	wantAPI := Entry{Kind: KindSBOM, Format: "CycloneDX", Type: "application/vnd.cyclonedx+json", Source: SourceEmbedded,
		Resource: &Identity{Name: "sbom-api", Version: "1.0.0"}, Signed: SignedNo}
	if got := r.Levels[1].Entries; !reflect.DeepEqual(got, []Entry{wantAPI}) {
		t.Errorf("api entries = %+v", got)
	}
	if got := r.Levels[0].Entries; len(got) != 1 || got[0].Resource.Name != "sbom-all" || got[0].Format != "SPDX" {
		t.Errorf("component entries = %+v", got)
	}

	d := build()
	d.Signatures = []descriptorruntime.Signature{{Name: "default"}}
	r, _, _ = FromDescriptor(d, "oci::ghcr.io/acme")
	if s := r.Levels[1].Entries[0].Signed; s != SignedByComponent {
		t.Errorf("signed = %s", s)
	}
}

func TestArtifactReferencesExactExtraIdentity(t *testing.T) {
	build := func(label descriptorruntime.Label) *descriptorruntime.Descriptor {
		d := desc("1.0.0")
		d.Component.Resources = []descriptorruntime.Resource{
			res("api", "1.0", "ociImage", map[string]string{"arch": "amd64"}, image("ghcr.io/acme/api:1")),
			res("api", "1.0", "ociImage", map[string]string{"arch": "arm64"}, image("ghcr.io/acme/api:2")),
			res("sbom", "1", "sbom", nil, blob("application/spdx+json", dig1), label),
		}
		return d
	}
	counts := func(r Result) []int {
		var n []int
		for _, l := range r.Levels {
			n = append(n, len(l.Entries))
		}
		return n
	}

	r, _, _ := FromDescriptor(build(lbl(LabelArtifactReferences, "v1alpha1", `[{"identity":{"name":"api","arch":"arm64"}}]`)), "oci::h/p")
	if got := counts(r); !reflect.DeepEqual(got, []int{0, 0, 1}) {
		t.Errorf("arm64 ref: entries per level = %v", got)
	}

	r, _, _ = FromDescriptor(build(lbl(LabelArtifactReferences, "v1alpha1", `[{"identity":{"name":"api","version":"2.0","arch":"arm64"}}]`)), "oci::h/p")
	if got := counts(r); !reflect.DeepEqual(got, []int{1, 0, 0}) {
		t.Errorf("version mismatch: entries per level = %v", got)
	}

	r, _, _ = FromDescriptor(build(lbl(LabelArtifactReferences, "v1alpha1", `[{"identity":{"name":"api"}}]`)), "oci::h/p")
	if got := counts(r); !reflect.DeepEqual(got, []int{1, 0, 0}) {
		t.Errorf("missing extra identity must not match: %v", got)
	}

	r, _, warns0 := FromDescriptor(build(lbl(LabelArtifactReferences, "", `[{"identity":{"name":"api","arch":"arm64"}}]`)), "oci::h/p")
	if got := counts(r); !reflect.DeepEqual(got, []int{0, 0, 1}) || len(warns0) != 0 {
		t.Errorf("unversioned label must match: %v, warnings %v", got, warns0)
	}

	r, _, warns := FromDescriptor(build(lbl(LabelArtifactReferences, "v1", `[{"identity":{"name":"api","arch":"arm64"}}]`)), "oci::h/p")
	if got := counts(r); !reflect.DeepEqual(got, []int{1, 0, 0}) {
		t.Errorf("v1 label must be ignored: %v", got)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], LabelArtifactReferences) {
		t.Errorf("warnings = %v", warns)
	}
}

func TestVEXLabel(t *testing.T) {
	const u = "https://security.access.redhat.com/data/csaf/v2/vex/"
	d := desc("1.0.0")
	d.Component.Labels = []descriptorruntime.Label{lbl(LabelVEX, "v1", `{"url":"`+u+`"}`)}
	d.Component.Resources = []descriptorruntime.Resource{
		res("chart", "1", "helmChart", nil, blob("application/gzip", dig1), lbl(LabelVEX, "", `{"url":"`+u+`"}`)),
	}
	r, _, warns := FromDescriptor(d, "oci::h/p")
	want := []Entry{{Kind: KindVEX, Source: SourceLabelLink, URL: u, Label: LabelVEX, Signed: SignedNo}}
	if len(warns) != 0 || !reflect.DeepEqual(r.Levels[0].Entries, want) || !reflect.DeepEqual(r.Levels[1].Entries, want) {
		t.Fatalf("entries = %+v / %+v, warnings = %v", r.Levels[0].Entries, r.Levels[1].Entries, warns)
	}
	if r.Levels[1].Status != StatusNotOCI {
		t.Errorf("chart status = %s", r.Levels[1].Status)
	}

	for name, l := range map[string]descriptorruntime.Label{
		"http":    lbl(LabelVEX, "v1", `{"url":"http://example.com/vex"}`),
		"no url":  lbl(LabelVEX, "v1", `{}`),
		"v2":      lbl(LabelVEX, "v2", `{"url":"`+u+`"}`),
		"no host": lbl(LabelVEX, "v1", `{"url":"https:///x"}`),
	} {
		d := desc("1.0.0")
		d.Component.Labels = []descriptorruntime.Label{l}
		r, _, warns := FromDescriptor(d, "oci::h/p")
		if len(r.Levels[0].Entries) != 0 || len(warns) != 1 {
			t.Errorf("%s: entries = %+v, warnings = %v", name, r.Levels[0].Entries, warns)
		}
	}
}

func TestComponentTagWithPlus(t *testing.T) {
	const v = "1.0.0+build.1"
	_, subj, _ := FromDescriptor(desc(v), "oci::ghcr.io/acme")
	want := oci.LooseSemverToOCITag(context.Background(), v)
	if want == v || subj[0].Reference != want {
		t.Errorf("reference = %q, want %q", subj[0].Reference, want)
	}
}

func TestParseImageReference(t *testing.T) {
	for ref, want := range map[string]Subject{
		"ghcr.io/acme/api:1.0":         {"ghcr.io/acme/api", "1.0"},
		"ghcr.io/acme/api:1.0@" + dig1: {"ghcr.io/acme/api", dig1},
		"localhost:5000/x":             {"localhost:5000/x", "latest"},
		"localhost/x:1":                {"localhost/x", "1"},
		"alpine:3":                     {"docker.io/library/alpine", "3"},
		"alpine":                       {"docker.io/library/alpine", "latest"},
		"bitnami/redis:7":              {"docker.io/bitnami/redis", "7"},
		"docker.io/library/alpine:3":   {"docker.io/library/alpine", "3"},
	} {
		got, err := parseImageReference(ref)
		if err != nil || got != want {
			t.Errorf("parseImageReference(%q) = %+v, %v; want %+v", ref, got, err, want)
		}
	}
}

func TestImageReferenceWithSchemeFails(t *testing.T) {
	for _, ref := range []string{"http://reg.example/acme/api:1", "https://reg.example/acme/api:1"} {
		d := desc("1.0.0")
		d.Component.Resources = []descriptorruntime.Resource{res("api", "1", "ociImage", nil, image(ref))}
		r, subj, _ := FromDescriptor(d, "oci::ghcr.io/acme")
		if l := r.Levels[1]; l.Status != StatusFailed || l.Error != "imageReference must not contain a scheme" {
			t.Errorf("%s: level = %+v", ref, l)
		}
		if subj[1] != (Subject{}) {
			t.Errorf("%s: subject = %+v, want none", ref, subj[1])
		}
	}
}

func TestLocationWithoutOCIPrefixIsNotOCI(t *testing.T) {
	d := desc("1.0.0")
	d.Component.Resources = []descriptorruntime.Resource{res("img", "1", "ociImage", nil, blob("application/vnd.oci.image.manifest.v1+json", dig1))}
	r, subj, _ := FromDescriptor(d, "ghcr.io/acme")
	if r.Levels[0].Status != StatusNotOCI || subj[0] != (Subject{}) {
		t.Errorf("component = %+v, subject %+v", r.Levels[0], subj[0])
	}
	if r.Levels[1].Status != StatusNotOCI {
		t.Errorf("local blob = %+v", r.Levels[1])
	}
}

func TestResourceSubject(t *testing.T) {
	d := desc("1.0.0")
	d.Component.Resources = []descriptorruntime.Resource{
		res("api", "1.0.0", "ociImage", map[string]string{"arch": "arm64"}, image("ghcr.io/acme/api:1.0@"+dig1)),
		res("api", "1.0.0", "ociImage", nil, image("ghcr.io/acme/api:1.0@"+dig2)),
		res("data", "1.0.0", "blob", nil, blob("application/gzip", dig2)),
	}
	const loc = "oci::ghcr.io/acme"
	got, err := ResourceSubject(d, loc, ocmruntime.Identity{"name": "api", "arch": "arm64"})
	if err != nil || got != (Subject{"ghcr.io/acme/api", dig1}) {
		t.Errorf("extra identity: %+v, %v", got, err)
	}
	got, err = ResourceSubject(d, loc, ocmruntime.Identity{"name": "api"})
	if err != nil || got != (Subject{"ghcr.io/acme/api", dig2}) {
		t.Errorf("no extra identity: %+v, %v", got, err)
	}
	if _, err := ResourceSubject(d, loc, ocmruntime.Identity{"name": "api", "arch": "amd64"}); !errors.Is(err, ErrResourceUnknown) {
		t.Errorf("wrong extra identity: %v", err)
	}
	if _, err := ResourceSubject(d, loc, ocmruntime.Identity{"name": "nope"}); !errors.Is(err, ErrResourceUnknown) {
		t.Errorf("unknown name: %v", err)
	}
	got, err = ResourceSubject(d, loc, ocmruntime.Identity{"name": "data"})
	if err != nil || got != (Subject{}) {
		t.Errorf("non-manifest blob: %+v, %v", got, err)
	}
}
