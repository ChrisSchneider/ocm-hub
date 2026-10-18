package oscal

import (
	"encoding/json"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/compliance"
	"chrisschneider.dev/delivery-bundler/internal/document"
)

func TestBuild(t *testing.T) {
	c := &compliance.Component{
		Ref:    compliance.Ref{Repository: "ghcr.io/x", Component: "example.com/c", Version: "1.0.0"},
		Images: []compliance.Image{{Name: "alpine"}, {Name: "python3"}, {Name: "nosbom"}},
		SBOMs: map[string][]compliance.SBOM{
			"alpine":  {{Source: "embedded alpine-sbom"}},
			"python3": {{Source: "embedded python3-sbom"}},
		},
	}
	fips := compliance.NewSARIF(compliance.SARIFDriver{Name: "t"}, []compliance.SARIFResult{
		{RuleID: "FIPS-007", Level: compliance.LevelNote, Properties: map[string]string{"image": "alpine"}},
		{RuleID: "FIPS-001", Level: compliance.LevelError, Properties: map[string]string{"image": "python3"}},
		{RuleID: "FIPS-000", Level: compliance.LevelWarning, Properties: map[string]string{"image": "nosbom"}},
	}, nil)
	doc := Build(c.Ref, Checks(c, &fips, "fips.sarif"), time.Unix(0, 0))
	r := doc.AR.Results[0]
	if len(r.Observations) != 6 {
		t.Fatalf("observations = %d, want 6", len(r.Observations))
	}
	type want struct{ state, reason, impl string }
	got := map[string]want{}
	for _, f := range r.Findings {
		w := want{f.Target.Status.State, f.Target.Status.Reason, ""}
		if f.Target.ImplementationStatus != nil {
			w.impl = f.Target.ImplementationStatus.State
		}
		got[f.Target.TargetID] = w
	}
	// 2 of 3 images have an SBOM: partial. python3 fails FIPS, the others
	// are not disproven: partial too.
	for id, w := range map[string]want{
		SBOMPerImage.ID: {"not-satisfied", "fail", "partial"},
		FIPSCrypto.ID:   {"not-satisfied", "fail", "partial"},
	} {
		if got[id] != w {
			t.Errorf("%s = %+v, want %+v", id, got[id], w)
		}
	}

	// The hub's viewer reads it back.
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	d, err := document.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if s := d.Summary(); s.Requirements != 2 || s.NotSatisfied != 2 || len(d.Assessment.Findings[1].Observations) != 3 {
		t.Errorf("parsed = %+v, %+v", s, d.Assessment)
	}
}

func TestBuildAllPass(t *testing.T) {
	c := &compliance.Component{Images: []compliance.Image{{Name: "a"}}, SBOMs: map[string][]compliance.SBOM{"a": {{Source: "x"}}}}
	fips := compliance.NewSARIF(compliance.SARIFDriver{Name: "t"}, nil, nil)
	r := Build(c.Ref, Checks(c, &fips, ""), time.Unix(0, 0)).AR.Results[0]
	for _, f := range r.Findings {
		st := f.Target.Status
		switch f.Target.TargetID {
		case SBOMPerImage.ID:
			if st.State != "satisfied" || f.Target.ImplementationStatus == nil || f.Target.ImplementationStatus.State != "implemented" {
				t.Errorf("sbom-1 = %+v", f.Target)
			}
		case FIPSCrypto.ID: // a heuristic never confirms
			if st.State != "not-satisfied" || st.Reason != "other" {
				t.Errorf("crypto-1 = %+v", f.Target)
			}
		}
	}
}
