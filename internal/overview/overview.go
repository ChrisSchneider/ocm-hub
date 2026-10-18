// Package overview derives the overview of a component version from its
// descriptor: display labels, Kubernetes compatibility, responsibles, risk
// profile, docs links, signed, contents, architectures and references.
// cmd/sync and the server share it so the label rules exist once.
package overview

import (
	"maps"
	"slices"

	"chrisschneider.dev/delivery-bundler/internal/display"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// DefaultKubernetesVersions are the minors a compatibility constraint is
// checked against unless hub.yaml sets catalog.kubernetesVersions.
var DefaultKubernetesVersions = []string{"1.24", "1.25", "1.26", "1.27", "1.28", "1.29", "1.30",
	"1.31", "1.32", "1.33", "1.34", "1.35", "1.36"}

// Options configure FromDescriptor.
type Options struct {
	KubernetesVersions []string // nil: DefaultKubernetesVersions
}

// Kubernetes is the kubernetes part of the compatibility label; Versions are
// the known minors the constraint admits.
type Kubernetes struct {
	Constraint    string
	Versions      []string
	Distributions []string
}

// StorageNeed is a volume the component needs: an access mode (RWO, RWX,
// ROX or RWOP) and a volume mode (Filesystem or Block).
type StorageNeed struct {
	AccessMode, VolumeMode, Purpose string
}

// Responsible is one entry of odg.ocm.software/responsibles.
type Responsible struct {
	Type           string // githubUser, githubTeam or codeowners
	Username       string // githubUser
	Teamname       string // githubTeam, org/team
	GitHubHostname string // optional
}

// RiskProfile is security.ocm.software/risk-profile; empty fields are unknown.
type RiskProfile struct {
	NetworkExposure            string `json:"network_exposure,omitempty"`
	AuthenticationEnforced     *bool  `json:"authentication_enforced,omitempty"`
	UserInteraction            string `json:"user_interaction,omitempty"`
	ConfidentialityRequirement string `json:"confidentiality_requirement,omitempty"`
	IntegrityRequirement       string `json:"integrity_requirement,omitempty"`
	AvailabilityRequirement    string `json:"availability_requirement,omitempty"`
	Comment                    string `json:"comment,omitempty"`
}

// DocLink is one entry of the docs label; Kind may be empty.
type DocLink struct {
	Title, URL, Kind string
}

// TypeCount is how many resources of one type a version has.
type TypeCount struct {
	Type  string
	Count int
}

// Reference is one entry of the descriptor's references (a child).
type Reference struct {
	Name, Component, Version string
}

// Architecture is one platform a version is built for; From lists where it
// was seen ("image", "ocm"), sorted.
type Architecture struct {
	Platform string
	From     []string
}

// Overview of one component version. Optional blocks are nil when absent.
type Overview struct {
	Display      display.Info // no fallback title applied
	Provider     string
	Signed       bool // at least one signature; not verified
	Kubernetes   *Kubernetes
	Storage      []StorageNeed // label order
	Responsibles []Responsible
	RiskProfile  *RiskProfile
	Docs         []DocLink
	Resources    []TypeCount // sorted by type
	// Architectures are the resources' extraIdentity platforms (from OCM only), sorted by Platform.
	Architectures []Architecture
	References    []Reference // descriptor order
	Warnings      []string    // one per skipped label or entry
}

// FromDescriptor builds the overview of d.
func FromDescriptor(d *descriptorruntime.Descriptor, o Options) Overview {
	if o.KubernetesVersions == nil {
		o.KubernetesVersions = DefaultKubernetesVersions
	}
	ov := Overview{
		Display:  display.FromLabels(d.Component.Labels),
		Provider: d.Component.Provider.Name,
		Signed:   len(d.Signatures) > 0,
	}
	parseLabels(&ov, d.Component.Labels, o)
	counts := map[string]int{}
	for _, r := range d.Component.Resources {
		counts[r.Type]++
	}
	for typ, n := range counts {
		ov.Resources = append(ov.Resources, TypeCount{Type: typ, Count: n})
	}
	slices.SortFunc(ov.Resources, func(a, b TypeCount) int { return cmpString(a.Type, b.Type) })
	var platforms []string
	for _, r := range d.Component.Resources {
		if p := resourcePlatform(r.ExtraIdentity); p != "" {
			platforms = append(platforms, p)
		}
	}
	if platforms != nil {
		ov.Architectures = mergePlatforms(nil, platforms, "ocm")
	}
	for _, r := range d.Component.References {
		ov.References = append(ov.References, Reference{Name: r.Name, Component: r.Component, Version: r.Version})
	}
	return ov
}

// resourcePlatform is extraIdentity os/architecture, architecture alone
// without os, "" without architecture.
func resourcePlatform(extra map[string]string) string {
	arch := extra["architecture"]
	if arch == "" {
		return ""
	}
	if os := extra["os"]; os != "" {
		return os + "/" + arch
	}
	return arch
}

// MergeArchitectures adds the image platforms to the OCM architectures; the
// result is sorted by Platform and never nil.
func MergeArchitectures(ocm []Architecture, image []string) []Architecture {
	return mergePlatforms(ocm, image, "image")
}

func mergePlatforms(archs []Architecture, platforms []string, from string) []Architecture {
	by := map[string][]string{}
	for _, a := range archs {
		by[a.Platform] = slices.Clone(a.From)
	}
	for _, p := range platforms {
		if !slices.Contains(by[p], from) {
			by[p] = append(by[p], from)
		}
	}
	out := make([]Architecture, 0, len(by))
	for _, p := range slices.Sorted(maps.Keys(by)) {
		slices.Sort(by[p])
		out = append(out, Architecture{Platform: p, From: by[p]})
	}
	return out
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
