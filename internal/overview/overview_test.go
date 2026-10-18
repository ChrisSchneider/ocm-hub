package overview

import (
	"reflect"
	"strings"
	"testing"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func label(name, version, value string) descriptorruntime.Label {
	return descriptorruntime.Label{Name: name, Version: version, Value: []byte(value)}
}

func descWith(labels ...descriptorruntime.Label) *descriptorruntime.Descriptor {
	d := &descriptorruntime.Descriptor{}
	d.Component.Name, d.Component.Version = "github.com/acme/app", "1.0.0"
	d.Component.Provider.Name = "acme"
	d.Component.Labels = labels
	return d
}

func TestFromDescriptorFull(t *testing.T) {
	d := descWith(
		label("catalog.chrisschneider.dev/title", "", `"App"`),
		label(LabelKubernetes, "v1", `{"constraint": ">=1.28 <1.33, !=1.30", "distributions": ["gardener", "", "eks"]}`),
		label(LabelDocs, "v1", `[{"title": "Docs", "url": "https://example.com/docs", "kind": "docs"},
			{"title": "Ref", "url": "https://example.com/ref", "kind": "weird"},
			{"title": "", "url": "https://example.com/x"}, {"title": "Insecure", "url": "http://example.com"}]`),
		label(LabelResponsibles, "v1", `[{"type": "githubUser", "username": "jane"},
			{"type": "githubTeam", "teamname": "acme/team", "github_hostname": "github.example"},
			{"type": "codeowners"}, {"type": "githubUser"}, {"type": "email", "address": "x"}]`),
		label(LabelRiskProfile, "v1", `{"network_exposure": "public", "authentication_enforced": true, "comment": "edge"}`),
	)
	d.Component.Resources = []descriptorruntime.Resource{{Type: "ociImage"}, {Type: "changelog"}, {Type: "ociImage"}}
	d.Component.References = []descriptorruntime.Reference{{ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: "lib", Version: "2.0.0"}}, Component: "github.com/acme/lib"}}
	d.Signatures = []descriptorruntime.Signature{{Name: "default"}}

	got := FromDescriptor(d, Options{KubernetesVersions: []string{"1.27", "1.28", "1.29", "1.30", "1.31", "1.32", "1.33"}})

	if got.Display.Title != "App" || got.Provider != "acme" || !got.Signed {
		t.Fatalf("display/provider/signed: %+v", got)
	}
	wantK := &Kubernetes{Constraint: ">=1.28 <1.33, !=1.30", Versions: []string{"1.28", "1.29", "1.31", "1.32"}, Distributions: []string{"gardener", "eks"}}
	if !reflect.DeepEqual(got.Kubernetes, wantK) {
		t.Errorf("kubernetes = %+v, want %+v", got.Kubernetes, wantK)
	}
	wantDocs := []DocLink{{Title: "Docs", URL: "https://example.com/docs", Kind: "docs"}, {Title: "Ref", URL: "https://example.com/ref"}}
	if !reflect.DeepEqual(got.Docs, wantDocs) {
		t.Errorf("docs = %+v", got.Docs)
	}
	wantR := []Responsible{{Type: "githubUser", Username: "jane"}, {Type: "githubTeam", Teamname: "acme/team", GitHubHostname: "github.example"}, {Type: "codeowners"}}
	if !reflect.DeepEqual(got.Responsibles, wantR) {
		t.Errorf("responsibles = %+v", got.Responsibles)
	}
	if got.RiskProfile == nil || got.RiskProfile.NetworkExposure != "public" || got.RiskProfile.AuthenticationEnforced == nil || !*got.RiskProfile.AuthenticationEnforced || got.RiskProfile.Comment != "edge" {
		t.Errorf("risk profile = %+v", got.RiskProfile)
	}
	if want := []TypeCount{{"changelog", 1}, {"ociImage", 2}}; !reflect.DeepEqual(got.Resources, want) {
		t.Errorf("resources = %+v", got.Resources)
	}
	if want := []Reference{{Name: "lib", Component: "github.com/acme/lib", Version: "2.0.0"}}; !reflect.DeepEqual(got.References, want) {
		t.Errorf("references = %+v", got.References)
	}
	// dropped: 2 docs entries, 2 responsibles entries, unknown docs kind is not a warning
	if len(got.Warnings) != 4 {
		t.Errorf("warnings = %q", got.Warnings)
	}
}

func TestFromDescriptorSkipsBadLabels(t *testing.T) {
	cases := map[string]descriptorruntime.Label{
		"wrong version":    label(LabelKubernetes, "v2", `{"constraint": ">=1.28"}`),
		"bad constraint":   label(LabelKubernetes, "v1", `{"constraint": "not a constraint"}`),
		"empty constraint": label(LabelKubernetes, "v1", `{"distributions": ["eks"]}`),
		"docs not a list":  label(LabelDocs, "v1", `"https://example.com"`),
		"responsibles obj": label(LabelResponsibles, "v1", `{"type": "codeowners"}`),
		"bad exposure":     label(LabelRiskProfile, "v1", `{"network_exposure": "internet"}`),
		"bad requirement":  label(LabelRiskProfile, "v1", `{"integrity_requirement": "medium"}`),
		"bad interaction":  label(LabelRiskProfile, "v1", `{"user_interaction": "robot"}`),
		"risk not object":  label(LabelRiskProfile, "v1", `[1]`),
	}
	for name, l := range cases {
		t.Run(name, func(t *testing.T) {
			got := FromDescriptor(descWith(l), Options{})
			if got.Kubernetes != nil || got.Docs != nil || got.Responsibles != nil || got.RiskProfile != nil {
				t.Fatalf("label not skipped: %+v", got)
			}
			if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], l.Name) {
				t.Fatalf("warnings = %q", got.Warnings)
			}
		})
	}
}

func TestFromDescriptorAcceptsUnversionedLabels(t *testing.T) {
	got := FromDescriptor(descWith(
		label(LabelKubernetes, "", `{"constraint": ">=1.35"}`),
		label(LabelDocs, "", `[{"title": "Docs", "url": "https://example.com"}]`),
	), Options{})
	if want := []string{"1.35", "1.36"}; got.Kubernetes == nil || !reflect.DeepEqual(got.Kubernetes.Versions, want) {
		t.Fatalf("kubernetes = %+v", got.Kubernetes)
	}
	if len(got.Docs) != 1 {
		t.Fatalf("docs = %+v", got.Docs)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %q", got.Warnings)
	}
}

func TestFromDescriptorEmpty(t *testing.T) {
	got := FromDescriptor(descWith(), Options{})
	if got.Signed || got.Kubernetes != nil || got.Resources != nil || got.References != nil || got.Warnings != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestDefaultKubernetesVersionsUsed(t *testing.T) {
	got := FromDescriptor(descWith(label(LabelKubernetes, "v1", `{"constraint": ">=1.35"}`)), Options{})
	if want := []string{"1.35", "1.36"}; got.Kubernetes == nil || !reflect.DeepEqual(got.Kubernetes.Versions, want) {
		t.Fatalf("got %+v", got.Kubernetes)
	}
}

func resource(name, typ string, extra map[string]string) descriptorruntime.Resource {
	return descriptorruntime.Resource{ElementMeta: descriptorruntime.ElementMeta{
		ObjectMeta: descriptorruntime.ObjectMeta{Name: name, Version: "1.0.0"}, ExtraIdentity: extra}, Type: typ}
}

func TestArchitecturesFromExtraIdentity(t *testing.T) {
	d := descWith()
	d.Component.Resources = []descriptorruntime.Resource{
		resource("cli", "executable", map[string]string{"os": "linux", "architecture": "arm64"}),
		resource("cli", "executable", map[string]string{"os": "linux", "architecture": "amd64"}),
		resource("cli-s390x", "executable", map[string]string{"architecture": "s390x"}),
		resource("cli-dup", "executable", map[string]string{"os": "linux", "architecture": "amd64"}),
		resource("chart", "helmChart", nil),
		resource("docs", "blob", map[string]string{"os": "linux"}),
	}
	got := FromDescriptor(d, Options{})
	want := []Architecture{{"linux/amd64", []string{"ocm"}}, {"linux/arm64", []string{"ocm"}}, {"s390x", []string{"ocm"}}}
	if !reflect.DeepEqual(got.Architectures, want) {
		t.Fatalf("architectures = %+v, want %+v", got.Architectures, want)
	}
}

func TestMergeArchitectures(t *testing.T) {
	ocm := []Architecture{{"linux/amd64", []string{"ocm"}}, {"s390x", []string{"ocm"}}}
	got := MergeArchitectures(ocm, []string{"linux/arm64", "linux/amd64", "linux/arm64"})
	want := []Architecture{{"linux/amd64", []string{"image", "ocm"}}, {"linux/arm64", []string{"image"}}, {"s390x", []string{"ocm"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %+v, want %+v", got, want)
	}
	if got := MergeArchitectures(nil, nil); got == nil || len(got) != 0 {
		t.Fatalf("empty merge = %#v, want empty non-nil", got)
	}
}

func TestCompatibilityLabel(t *testing.T) {
	known := Options{KubernetesVersions: []string{"1.28", "1.29", "1.30"}}
	got := FromDescriptor(descWith(label(LabelCompatibility, "v1", `{
		"kubernetes": {"constraint": ">=1.29", "distributions": ["gardener"]},
		"storage": [
			{"accessMode": "ReadWriteMany", "purpose": "shared uploads"},
			{"accessMode": "RWO", "volumeMode": "Block"},
			{"accessMode": "RWM"},
			{"accessMode": "RWO", "volumeMode": "Raw"}
		]}`)), known)
	wantK := &Kubernetes{Constraint: ">=1.29", Versions: []string{"1.29", "1.30"}, Distributions: []string{"gardener"}}
	if !reflect.DeepEqual(got.Kubernetes, wantK) {
		t.Errorf("kubernetes = %+v", got.Kubernetes)
	}
	wantS := []StorageNeed{{AccessMode: "RWX", VolumeMode: "Filesystem", Purpose: "shared uploads"}, {AccessMode: "RWO", VolumeMode: "Block"}}
	if !reflect.DeepEqual(got.Storage, wantS) {
		t.Errorf("storage = %+v", got.Storage)
	}
	if len(got.Warnings) != 2 {
		t.Errorf("warnings = %q", got.Warnings)
	}

	// The deprecated label still works alone; next to the new kubernetes part it is ignored.
	got = FromDescriptor(descWith(label(LabelKubernetes, "v1", `{"constraint": ">=1.30"}`)), known)
	if got.Kubernetes == nil || got.Kubernetes.Constraint != ">=1.30" || len(got.Warnings) != 0 {
		t.Errorf("legacy alone = %+v, %q", got.Kubernetes, got.Warnings)
	}
	got = FromDescriptor(descWith(label(LabelKubernetes, "v1", `{"constraint": ">=1.30"}`), label(LabelCompatibility, "v1", `{"kubernetes": {"constraint": ">=1.28"}}`)), known)
	if got.Kubernetes == nil || got.Kubernetes.Constraint != ">=1.28" || len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "superseded") {
		t.Errorf("both = %+v, %q", got.Kubernetes, got.Warnings)
	}
	// Storage only keeps the deprecated label's kubernetes.
	got = FromDescriptor(descWith(label(LabelKubernetes, "v1", `{"constraint": ">=1.30"}`), label(LabelCompatibility, "v1", `{"storage": [{"accessMode": "RWO"}]}`)), known)
	if got.Kubernetes == nil || got.Kubernetes.Constraint != ">=1.30" || len(got.Storage) != 1 || len(got.Warnings) != 0 {
		t.Errorf("storage + legacy = %+v, %+v, %q", got.Kubernetes, got.Storage, got.Warnings)
	}
	got = FromDescriptor(descWith(label(LabelCompatibility, "v1", `{}`)), known)
	if got.Kubernetes != nil || got.Storage != nil || len(got.Warnings) != 1 {
		t.Errorf("empty = %+v, %q", got, got.Warnings)
	}
}
