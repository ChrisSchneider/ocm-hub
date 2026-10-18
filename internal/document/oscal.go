package document

import (
	"encoding/json"
	"fmt"
)

// Assessment is the findings of an OSCAL assessment-results document, each
// with its related observations, across all results.
type Assessment struct {
	Title    string
	Findings []AssessmentFinding
}

// AssessmentFinding is one OSCAL finding: the assessed objective or control
// and its status.
type AssessmentFinding struct {
	Target, Title, Description string
	// State is satisfied or not-satisfied; Reason pass, fail or other.
	State, Reason, Remarks string
	// Implementation is the target's implementation-status (e.g. partial).
	Implementation string
	Source         string // the finding's "source" prop
	Observations   []Observation
}

// Observation is one OSCAL observation of a finding.
type Observation struct {
	Title, Description string
	Result             string // the observation's "result" prop
	Evidence           []string
}

type oscalProp struct{ Name, Value string }

func propValue(props []oscalProp, name string) string {
	for _, p := range props {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}

// parseOSCAL reads an OSCAL assessment-results document (JSON).
func parseOSCAL(raw json.RawMessage) (Document, error) {
	var ar struct {
		Metadata struct {
			Title        string `json:"title"`
			OSCALVersion string `json:"oscal-version"`
		} `json:"metadata"`
		Results []struct {
			Observations []struct {
				UUID             string      `json:"uuid"`
				Title            string      `json:"title"`
				Description      string      `json:"description"`
				Props            []oscalProp `json:"props"`
				RelevantEvidence []struct {
					Href string `json:"href"`
				} `json:"relevant-evidence"`
			} `json:"observations"`
			Findings []struct {
				Title       string      `json:"title"`
				Description string      `json:"description"`
				Props       []oscalProp `json:"props"`
				Target      struct {
					TargetID string `json:"target-id"`
					Title    string `json:"title"`
					Status   struct {
						State   string `json:"state"`
						Reason  string `json:"reason"`
						Remarks string `json:"remarks"`
					} `json:"status"`
					ImplementationStatus *struct {
						State string `json:"state"`
					} `json:"implementation-status"`
				} `json:"target"`
				RelatedObservations []struct {
					UUID string `json:"observation-uuid"`
				} `json:"related-observations"`
			} `json:"findings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &ar); err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	a := &Assessment{Title: ar.Metadata.Title}
	for _, r := range ar.Results {
		obs := map[string]Observation{}
		for _, o := range r.Observations {
			ob := Observation{Title: o.Title, Description: o.Description, Result: propValue(o.Props, "result")}
			for _, e := range o.RelevantEvidence {
				ob.Evidence = append(ob.Evidence, e.Href)
			}
			obs[o.UUID] = ob
		}
		for _, f := range r.Findings {
			af := AssessmentFinding{Target: f.Target.TargetID, Title: f.Title, Description: f.Description,
				State: f.Target.Status.State, Reason: f.Target.Status.Reason, Remarks: f.Target.Status.Remarks,
				Source: propValue(f.Props, "source")}
			if af.Title == "" {
				af.Title = f.Target.Title
			}
			if f.Target.ImplementationStatus != nil {
				af.Implementation = f.Target.ImplementationStatus.State
			}
			for _, ro := range f.RelatedObservations {
				if o, ok := obs[ro.UUID]; ok {
					af.Observations = append(af.Observations, o)
				}
			}
			a.Findings = append(a.Findings, af)
		}
	}
	format := "OSCAL assessment results"
	if v := ar.Metadata.OSCALVersion; v != "" {
		format = "OSCAL " + v + " assessment results"
	}
	return Document{Format: format, Assessment: a}, nil
}
