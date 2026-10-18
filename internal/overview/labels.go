package overview

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"

	"chrisschneider.dev/delivery-bundler/internal/display"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Label names read by FromDescriptor (version v1, or no version: the OCM CLI drops label versions).
const (
	LabelCompatibility = display.LabelPrefix + "compatibility"
	LabelDocs          = display.LabelPrefix + "docs"
	LabelResponsibles  = "odg.ocm.software/responsibles"
	LabelRiskProfile   = "security.ocm.software/risk-profile"
)

var docKinds = []string{"docs", "api", "tutorial", "issues", "website"}

// accessModes maps the Kubernetes access mode names and their short forms to the short form.
var accessModes = map[string]string{
	"RWO": "RWO", "ReadWriteOnce": "RWO",
	"RWX": "RWX", "ReadWriteMany": "RWX",
	"ROX": "ROX", "ReadOnlyMany": "ROX",
	"RWOP": "RWOP", "ReadWriteOncePod": "RWOP",
}

var (
	exposures    = []string{"private", "protected", "public"}
	interactions = []string{"operator", "end-user"}
	requirements = []string{"none", "low", "high"}
)

func parseLabels(ov *Overview, labels []descriptorruntime.Label, o Options) {
	warn := func(name, format string, args ...any) {
		ov.Warnings = append(ov.Warnings, name+": "+fmt.Sprintf(format, args...))
	}
	for _, l := range labels {
		switch l.Name {
		case LabelCompatibility, LabelDocs, LabelResponsibles, LabelRiskProfile:
		default:
			continue
		}
		if l.Version != "" && l.Version != "v1" {
			warn(l.Name, "version %q is not supported (only v1 or no version is supported); label skipped", l.Version)
			continue
		}
		var err error
		switch l.Name {
		case LabelCompatibility:
			ov.Kubernetes, ov.Storage, err = parseCompatibility(l.Value, o.KubernetesVersions, func(f string, a ...any) { warn(l.Name, f, a...) })
		case LabelDocs:
			ov.Docs, err = parseDocs(l.Value, func(f string, a ...any) { warn(l.Name, f, a...) })
		case LabelResponsibles:
			ov.Responsibles, err = parseResponsibles(l.Value, func(f string, a ...any) { warn(l.Name, f, a...) })
		case LabelRiskProfile:
			ov.RiskProfile, err = parseRiskProfile(l.Value)
		}
		if err != nil {
			warn(l.Name, "%v; label skipped", err)
		}
	}
}

func parseCompatibility(raw []byte, known []string, warn func(string, ...any)) (*Kubernetes, []StorageNeed, error) {
	var v struct {
		Kubernetes json.RawMessage `json:"kubernetes"`
		Storage    []struct {
			AccessMode string `json:"accessMode"`
			VolumeMode string `json:"volumeMode"`
			Purpose    string `json:"purpose"`
		} `json:"storage"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, nil, err
	}
	var k *Kubernetes
	var kerr error
	if len(v.Kubernetes) > 0 && string(v.Kubernetes) != "null" {
		k, kerr = parseKubernetes(v.Kubernetes, known)
	}
	var storage []StorageNeed
	for i, e := range v.Storage {
		mode, ok := accessModes[strings.TrimSpace(e.AccessMode)]
		if !ok {
			warn("storage entry %d: accessMode %q must be RWO, RWX, ROX or RWOP; entry skipped", i, e.AccessMode)
			continue
		}
		vm := strings.TrimSpace(e.VolumeMode)
		if vm == "" {
			vm = "Filesystem"
		}
		if vm != "Filesystem" && vm != "Block" {
			warn("storage entry %d: volumeMode %q must be Filesystem or Block; entry skipped", i, e.VolumeMode)
			continue
		}
		storage = append(storage, StorageNeed{AccessMode: mode, VolumeMode: vm, Purpose: strings.TrimSpace(e.Purpose)})
	}
	switch {
	case kerr != nil && storage == nil:
		return nil, nil, fmt.Errorf("kubernetes: %w", kerr)
	case kerr != nil:
		warn("kubernetes: %v; skipped", kerr)
	case k == nil && storage == nil:
		return nil, nil, fmt.Errorf("neither kubernetes nor storage declared")
	}
	return k, storage, nil
}

func parseKubernetes(raw []byte, known []string) (*Kubernetes, error) {
	var v struct {
		Constraint    string   `json:"constraint"`
		Distributions []string `json:"distributions"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if strings.TrimSpace(v.Constraint) == "" {
		return nil, fmt.Errorf("constraint is required")
	}
	c, err := semver.NewConstraint(v.Constraint)
	if err != nil {
		return nil, fmt.Errorf("constraint %q: %w", v.Constraint, err)
	}
	k := &Kubernetes{Constraint: v.Constraint, Versions: []string{}, Distributions: []string{}}
	for _, m := range known {
		if sv, err := semver.NewVersion(m); err == nil && c.Check(sv) {
			k.Versions = append(k.Versions, m)
		}
	}
	for _, d := range v.Distributions {
		if d = strings.TrimSpace(d); d != "" {
			k.Distributions = append(k.Distributions, d)
		}
	}
	return k, nil
}

func parseDocs(raw []byte, warn func(string, ...any)) ([]DocLink, error) {
	var v []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	out := []DocLink{}
	for i, e := range v {
		u, err := url.Parse(e.URL)
		switch {
		case strings.TrimSpace(e.Title) == "":
			warn("entry %d: title is required; entry skipped", i)
			continue
		case err != nil || u.Scheme != "https" || u.Host == "":
			warn("entry %d: url %q must be an https URL; entry skipped", i, e.URL)
			continue
		}
		kind := e.Kind
		if !slices.Contains(docKinds, kind) {
			kind = ""
		}
		out = append(out, DocLink{Title: e.Title, URL: e.URL, Kind: kind})
	}
	return out, nil
}

func parseResponsibles(raw []byte, warn func(string, ...any)) ([]Responsible, error) {
	var v []struct {
		Type           string `json:"type"`
		Username       string `json:"username"`
		Teamname       string `json:"teamname"`
		GitHubHostname string `json:"github_hostname"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	out := []Responsible{}
	for i, e := range v {
		r := Responsible{Type: e.Type, GitHubHostname: e.GitHubHostname}
		switch e.Type {
		case "githubUser":
			if e.Username == "" {
				warn("entry %d: githubUser needs username; entry skipped", i)
				continue
			}
			r.Username = e.Username
		case "githubTeam":
			if org, team, ok := strings.Cut(e.Teamname, "/"); !ok || org == "" || team == "" {
				warn("entry %d: githubTeam needs teamname org/team; entry skipped", i)
				continue
			}
			r.Teamname = e.Teamname
		case "codeowners":
		default:
			warn("entry %d: unknown type %q; entry skipped", i, e.Type)
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func parseRiskProfile(raw []byte) (*RiskProfile, error) {
	var p RiskProfile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	for _, f := range []struct {
		name, value string
		allowed     []string
	}{
		{"network_exposure", p.NetworkExposure, exposures},
		{"user_interaction", p.UserInteraction, interactions},
		{"confidentiality_requirement", p.ConfidentialityRequirement, requirements},
		{"integrity_requirement", p.IntegrityRequirement, requirements},
		{"availability_requirement", p.AvailabilityRequirement, requirements},
	} {
		if f.value != "" && !slices.Contains(f.allowed, f.value) {
			return nil, fmt.Errorf("%s %q must be one of %s", f.name, f.value, strings.Join(f.allowed, ", "))
		}
	}
	return &p, nil
}
