// Package oscal writes a BSI-flavoured OSCAL 1.1.3 assessment result for a
// component version (prototype). Requirements are a small local list that
// cites the BSI documents they are taken from; there is no OSCAL catalog
// behind them yet. Each check result becomes an observation, each
// requirement a finding with
//
//   - status satisfied, implementation-status implemented: every image passes;
//   - status not-satisfied (reason fail), implementation-status partial: some
//     images pass ("teilweise");
//   - status not-satisfied (reason fail): no image passes;
//   - status not-satisfied (reason other): nothing was disproven, but the
//     heuristic cannot confirm the requirement either.
package oscal

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"chrisschneider.dev/delivery-bundler/internal/compliance"
)

const oscalVersion = "1.1.3"

// PredicateType is the in-toto predicate type of the assessment result
// attestation.
const PredicateType = "https://chrisschneider.dev/attestation/oscal-assessment-results/v1"

// Requirement is one assessed requirement.
type Requirement struct {
	ID, Title, Source string
	// Confirmable is false for heuristics that can only disprove: a pass is
	// then "not disproven", not satisfied.
	Confirmable bool
}

var (
	SBOMPerImage = Requirement{ID: "sbom-1", Title: "Every container image has an SBOM",
		Source: "BSI TR-03183-2 (Software Bill of Materials)", Confirmable: true}
	FIPSCrypto = Requirement{ID: "crypto-1", Title: "Container images use FIPS 140 validated cryptography",
		Source: "BSI IT-Grundschutz CON.1 Kryptokonzept; FIPS 140-3", Confirmable: false}
)

// Check is the outcome of one requirement for one image.
type Check struct {
	Requirement Requirement
	Image       string
	Pass        bool
	Detail      string
	Evidence    string // href of the evidence (e.g. the SARIF file), optional
}

// Checks derives the image checks from the component and, when fipsLog is
// non-nil, from the fipscheck SARIF (evidence: fipsHref).
func Checks(c *compliance.Component, fipsLog *compliance.SARIF, fipsHref string) []Check {
	var out []Check
	for _, img := range c.Images {
		n := len(c.SBOMs[img.Name])
		detail := fmt.Sprintf("%d SBOM(s) found", n)
		for i, s := range c.SBOMs[img.Name] {
			if i == 0 {
				detail += ": "
			} else {
				detail += ", "
			}
			detail += s.Source
		}
		out = append(out, Check{Requirement: SBOMPerImage, Image: img.Name, Pass: n > 0, Detail: detail})
	}
	if fipsLog == nil || len(fipsLog.Runs) == 0 {
		return out
	}
	for _, img := range c.Images {
		var errs, others []string
		for _, r := range fipsLog.Runs[0].Results {
			if r.Properties["image"] != img.Name {
				continue
			}
			if r.Level == compliance.LevelError {
				errs = append(errs, r.RuleID+": "+r.Message.Text)
			} else {
				others = append(others, r.RuleID+" ("+r.Level+")")
			}
		}
		ch := Check{Requirement: FIPSCrypto, Image: img.Name, Pass: len(errs) == 0, Evidence: fipsHref}
		switch {
		case len(errs) > 0:
			ch.Detail = fmt.Sprintf("Definitely not compliant: %v", errs)
		case len(others) > 0:
			ch.Detail = fmt.Sprintf("Not disproven; open points: %v", others)
		default:
			ch.Detail = "Not disproven"
		}
		out = append(out, ch)
	}
	return out
}

// AssessmentResults is the OSCAL document root.
type AssessmentResults struct {
	AR ar `json:"assessment-results"`
}

type ar struct {
	UUID     string   `json:"uuid"`
	Metadata metadata `json:"metadata"`
	ImportAP href     `json:"import-ap"`
	Results  []result `json:"results"`
}

type metadata struct {
	Title        string `json:"title"`
	LastModified string `json:"last-modified"`
	Version      string `json:"version"`
	OSCALVersion string `json:"oscal-version"`
	Props        []prop `json:"props,omitempty"`
	Remarks      string `json:"remarks,omitempty"`
}

type href struct {
	Href    string `json:"href"`
	Remarks string `json:"remarks,omitempty"`
}

type prop struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	NS    string `json:"ns,omitempty"`
}

type result struct {
	UUID             string           `json:"uuid"`
	Title            string           `json:"title"`
	Description      string           `json:"description"`
	Start            string           `json:"start"`
	End              string           `json:"end"`
	Props            []prop           `json:"props,omitempty"`
	ReviewedControls reviewedControls `json:"reviewed-controls"`
	Observations     []observation    `json:"observations,omitempty"`
	Findings         []finding        `json:"findings,omitempty"`
}

type reviewedControls struct {
	ControlSelections []controlSelection `json:"control-selections"`
}

type controlSelection struct {
	IncludeControls []controlID `json:"include-controls"`
}

type controlID struct {
	ControlID string `json:"control-id"`
}

type observation struct {
	UUID             string     `json:"uuid"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Props            []prop     `json:"props,omitempty"`
	Methods          []string   `json:"methods"`
	Types            []string   `json:"types"`
	RelevantEvidence []evidence `json:"relevant-evidence,omitempty"`
	Collected        string     `json:"collected"`
}

type evidence struct {
	Href        string `json:"href"`
	Description string `json:"description"`
}

type finding struct {
	UUID                string       `json:"uuid"`
	Title               string       `json:"title"`
	Description         string       `json:"description"`
	Props               []prop       `json:"props,omitempty"`
	Target              target       `json:"target"`
	RelatedObservations []relatedObs `json:"related-observations,omitempty"`
}

type target struct {
	Type                 string      `json:"type"`
	TargetID             string      `json:"target-id"`
	Title                string      `json:"title"`
	Status               status      `json:"status"`
	ImplementationStatus *implStatus `json:"implementation-status,omitempty"`
}

type status struct {
	State   string `json:"state"`
	Reason  string `json:"reason"`
	Remarks string `json:"remarks,omitempty"`
}

type implStatus struct {
	State string `json:"state"`
}

type relatedObs struct {
	ObservationUUID string `json:"observation-uuid"`
}

// ns marks the prototype's own props.
const ns = "https://chrisschneider.dev/ns/oscal"

// Build turns checks into an assessment result for ref, at now.
func Build(ref compliance.Ref, checks []Check, now time.Time) AssessmentResults {
	ts := now.UTC().Format(time.RFC3339)
	res := result{
		UUID: uuid.NewString(), Title: "Compliance pipeline: " + ref.Component + ":" + ref.Version,
		Description: "Automated checks of OCM component version " + ref.String() + ". Heuristics can only disprove compliance.",
		Start:       ts, End: ts,
		Props: []prop{{Name: "component", Value: ref.Component, NS: ns}, {Name: "version", Value: ref.Version, NS: ns}},
	}
	var reqs []Requirement
	byReq := map[string][]int{} // requirement id -> observation indexes
	for _, ch := range checks {
		if _, ok := byReq[ch.Requirement.ID]; !ok {
			reqs = append(reqs, ch.Requirement)
		}
		o := observation{UUID: uuid.NewString(), Title: ch.Requirement.Title + ": " + ch.Image, Description: ch.Detail,
			Props:   []prop{{Name: "image", Value: ch.Image, NS: ns}, {Name: "result", Value: map[bool]string{true: "pass", false: "fail"}[ch.Pass], NS: ns}},
			Methods: []string{"TEST"}, Types: []string{"finding"}, Collected: ts}
		if ch.Evidence != "" {
			o.RelevantEvidence = []evidence{{Href: ch.Evidence, Description: "SARIF report"}}
		}
		byReq[ch.Requirement.ID] = append(byReq[ch.Requirement.ID], len(res.Observations))
		res.Observations = append(res.Observations, o)
	}
	var ids []controlID
	for _, r := range reqs {
		ids = append(ids, controlID{ControlID: r.ID})
	}
	res.ReviewedControls.ControlSelections = []controlSelection{{IncludeControls: ids}}
	for _, r := range reqs {
		pass, total := 0, len(byReq[r.ID])
		f := finding{UUID: uuid.NewString(), Title: r.Title, Props: []prop{{Name: "source", Value: r.Source, NS: ns}}}
		for _, i := range byReq[r.ID] {
			f.RelatedObservations = append(f.RelatedObservations, relatedObs{res.Observations[i].UUID})
			if res.Observations[i].Props[1].Value == "pass" {
				pass++
			}
		}
		t := target{Type: "objective-id", TargetID: r.ID, Title: r.Title}
		switch {
		case pass == total && r.Confirmable:
			t.Status = status{State: "satisfied", Reason: "pass"}
			t.ImplementationStatus = &implStatus{"implemented"}
		case pass == total:
			t.Status = status{State: "not-satisfied", Reason: "other", Remarks: "Not disproven for any image; the heuristic cannot confirm the requirement."}
		case pass > 0:
			t.Status = status{State: "not-satisfied", Reason: "fail"}
			t.ImplementationStatus = &implStatus{"partial"}
		default:
			t.Status = status{State: "not-satisfied", Reason: "fail"}
		}
		f.Description = fmt.Sprintf("%d of %d image(s) pass.", pass, total)
		if !r.Confirmable {
			f.Description = fmt.Sprintf("%d of %d image(s) not disproven.", pass, total)
		}
		f.Target = t
		res.Findings = append(res.Findings, f)
	}
	return AssessmentResults{AR: ar{
		UUID: uuid.NewString(),
		Metadata: metadata{Title: "Compliance assessment of " + ref.Component + ":" + ref.Version, LastModified: ts, Version: "0.1.0", OSCALVersion: oscalVersion,
			Remarks: "Prototype: requirements cite BSI documents but are not taken from an OSCAL catalog."},
		ImportAP: href{Href: "#", Remarks: "No assessment plan yet (prototype)."},
		Results:  []result{res},
	}}
}
