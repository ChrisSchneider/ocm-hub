package document

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/openvex/go-vex/pkg/vex"
	"github.com/owenrumney/go-sarif/v3/pkg/report/v210/sarif"
	"github.com/protobom/protobom/pkg/reader"
	protosbom "github.com/protobom/protobom/pkg/sbom"
)

// Parse detects the document family by content and parses it. Anything that
// is not a recognised SPDX, CycloneDX, OpenVEX, SARIF 2.1 or OSCAL
// assessment-results JSON document
// (including invalid JSON and XML) fails with ErrUnsupported. CycloneDX
// vulnerabilities are scan findings, and VEX statements too when analysed.
func Parse(b []byte) (Document, error) {
	if len(b) > MaxSize {
		return Document{}, ErrTooLarge
	}
	var head struct {
		SPDXVersion string          `json:"spdxVersion"`
		BOMFormat   string          `json:"bomFormat"`
		SpecVersion string          `json:"specVersion"`
		Context     string          `json:"@context"`
		Version     json.RawMessage `json:"version"`
		Runs        json.RawMessage `json:"runs"`
		Assessment  json.RawMessage `json:"assessment-results"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	switch {
	case head.SPDXVersion != "":
		return parseSBOM(b, "SPDX "+strings.TrimPrefix(head.SPDXVersion, "SPDX-"))
	case head.BOMFormat == "CycloneDX":
		return parseCycloneDX(b, "CycloneDX "+head.SpecVersion)
	case strings.HasPrefix(head.Context, "https://openvex.dev/ns"):
		return parseVEX(b, head.Context)
	case head.Runs != nil && strings.HasPrefix(strings.Trim(string(head.Version), `"`), "2.1"):
		return parseSARIF(b)
	case head.Assessment != nil:
		return parseOSCAL(head.Assessment)
	}
	return Document{}, ErrUnsupported
}

// parseSBOM reads SPDX or CycloneDX through protobom and flattens its
// package nodes.
func parseSBOM(b []byte, format string) (Document, error) {
	doc, err := reader.New().ParseStream(bytes.NewReader(b))
	if err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	s := &SBOM{}
	for _, n := range doc.GetNodeList().GetNodes() {
		if n.GetType() != protosbom.Node_PACKAGE {
			continue
		}
		s.Packages = append(s.Packages, Package{
			Name:    n.GetName(),
			Version: n.GetVersion(),
			License: nodeLicense(n),
			Type:    purlType(string(n.Purl())),
			PURL:    string(n.Purl()),
		})
	}
	return Document{Format: format, SBOM: s}, nil
}

// cdxComponent is the part of a CycloneDX component read to name the
// products of a vulnerability.
type cdxComponent struct {
	BOMRef     string         `json:"bom-ref"`
	Name       string         `json:"name"`
	Version    string         `json:"version"`
	Purl       string         `json:"purl"`
	Components []cdxComponent `json:"components"`
}

type cdxVulnerability struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Ratings     []struct {
		Score    *float64 `json:"score"`
		Severity string   `json:"severity"`
	} `json:"ratings"`
	Analysis *struct {
		State         string   `json:"state"`
		Justification string   `json:"justification"`
		Response      []string `json:"response"`
		Detail        string   `json:"detail"`
	} `json:"analysis"`
	Affects []struct {
		Ref string `json:"ref"`
	} `json:"affects"`
}

// cdxStatus maps CycloneDX analysis states to OpenVEX statuses; unknown
// states are kept as they are.
var cdxStatus = map[string]string{
	"exploitable":            "affected",
	"in_triage":              "under_investigation",
	"not_affected":           "not_affected",
	"false_positive":         "not_affected",
	"resolved":               "fixed",
	"resolved_with_pedigree": "fixed",
}

// severityScore stands in for a missing score, so a severity word alone
// still decides importance: the lower bound of its CVSS band.
var severityScore = map[string]float64{"critical": 9, "high": 7, "medium": 4, "low": 0.1}

// parseCycloneDX reads the packages through protobom and the vulnerabilities
// itself: each is a scan finding (what was found), an analysed one also a VEX
// statement (what the vendor says about it). A
// document without components but with vulnerabilities has no SBOM part.
func parseCycloneDX(b []byte, format string) (Document, error) {
	d, err := parseSBOM(b, format)
	if err != nil {
		return d, err
	}
	var bom struct {
		Metadata struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"metadata"`
		Components      []cdxComponent     `json:"components"`
		Vulnerabilities []cdxVulnerability `json:"vulnerabilities"`
	}
	if err := json.Unmarshal(b, &bom); err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	if len(bom.Vulnerabilities) == 0 {
		return d, nil
	}
	if len(bom.Components) == 0 {
		d.SBOM = nil
	}
	refs := map[string]string{}
	var walk func([]cdxComponent)
	walk = func(cs []cdxComponent) {
		for _, c := range cs {
			if c.BOMRef != "" {
				refs[c.BOMRef] = cmp.Or(c.Purl, strings.TrimSuffix(c.Name+"@"+c.Version, "@"))
			}
			walk(c.Components)
		}
	}
	walk(bom.Components)
	for _, v := range bom.Vulnerabilities {
		var products []string
		for _, a := range v.Affects {
			products = append(products, cmp.Or(refs[a.Ref], a.Ref))
		}
		if a := v.Analysis; a != nil && a.State != "" {
			if d.VEX == nil {
				d.VEX = &VEX{}
			}
			justification := a.Justification
			if justification == "" && a.State == "false_positive" {
				justification = "false_positive"
			}
			action := strings.Join(slices.DeleteFunc([]string{strings.Join(a.Response, ", "), a.Detail}, func(s string) bool { return s == "" }), ": ")
			d.VEX.Statements = append(d.VEX.Statements, Statement{Vulnerability: v.ID, Products: products,
				Status: cmp.Or(cdxStatus[a.State], a.State), Justification: justification, Action: action})
		}
		if d.Scan == nil {
			d.Scan = &Scan{Tool: cdxTool(bom.Metadata.Tools), ImportantBasis: basisLevel}
		}
		f := Finding{Rule: v.ID, Message: v.Description}
		if len(products) > 0 {
			f.Location = products[0]
		}
		if len(v.Ratings) > 0 {
			r := v.Ratings[0]
			f.Severity = r.Severity
			if r.Score != nil {
				f.score = *r.Score
			} else {
				f.score = severityScore[strings.ToLower(r.Severity)]
			}
			if f.Severity == "" {
				f.Severity = severityBucket(f.score)
			}
			if f.score > 0 {
				d.Scan.ImportantBasis = basisSecuritySeverity
			}
		}
		d.Scan.Findings = append(d.Scan.Findings, f)
	}
	return d, nil
}

// cdxTool is the first tool's name, from the CycloneDX 1.5+ object form
// ({components, services}) or the older list.
func cdxTool(raw json.RawMessage) string {
	type named struct {
		Name string `json:"name"`
	}
	var list []named
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return list[0].Name
	}
	var obj struct{ Components, Services []named }
	if json.Unmarshal(raw, &obj) == nil {
		for _, t := range append(obj.Components, obj.Services...) {
			if t.Name != "" {
				return t.Name
			}
		}
	}
	return ""
}

// nodeLicense is the concluded license, else the first listed one; the
// placeholders NOASSERTION and NONE count as no license.
func nodeLicense(n *protosbom.Node) string {
	for _, l := range append([]string{n.GetLicenseConcluded()}, n.GetLicenses()...) {
		if l != "" && l != "NOASSERTION" && l != "NONE" {
			return l
		}
	}
	return ""
}

// purlType is the type of a package URL: pkg:npm/x@1 gives npm.
func purlType(purl string) string {
	rest, ok := strings.CutPrefix(purl, "pkg:")
	if !ok {
		return ""
	}
	t, _, _ := strings.Cut(rest, "/")
	return t
}

// parseVEX reads OpenVEX with go-vex. go-vex converts the legacy v0.0.x
// forms (string vulnerabilities and products) only when it opens a file, so
// those documents take a detour through a temporary one.
func parseVEX(b []byte, context string) (Document, error) {
	var (
		v   *vex.VEX
		err error
	)
	if context == vex.ContextLocator() {
		v, err = vex.Parse(b)
	} else {
		v, err = openLegacyVEX(b)
	}
	if err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	out := &VEX{}
	for _, st := range v.Statements {
		s := Statement{
			Vulnerability: string(st.Vulnerability.Name),
			Status:        string(st.Status),
			Justification: string(st.Justification),
			Action:        st.ActionStatement,
		}
		for _, p := range st.Products {
			s.Products = append(s.Products, p.ID)
		}
		out.Statements = append(out.Statements, s)
	}
	format := "OpenVEX"
	if rest, ok := strings.CutPrefix(context, "https://openvex.dev/ns/v"); ok {
		if parts := strings.SplitN(rest, ".", 3); len(parts) >= 2 {
			format += " " + parts[0] + "." + parts[1]
		}
	}
	return Document{Format: format, VEX: out}, nil
}

func openLegacyVEX(b []byte) (*vex.VEX, error) {
	f, err := os.CreateTemp("", "openvex-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	return vex.Open(f.Name())
}

// parseSARIF reads SARIF 2.1 with go-sarif, one Finding per result of the
// first run.
func parseSARIF(b []byte) (Document, error) {
	r, err := sarif.FromBytes(b)
	if err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	out := &Scan{ImportantBasis: basisLevel}
	if len(r.Runs) == 0 || r.Runs[0] == nil {
		return Document{Format: "SARIF 2.1.0", Scan: out}, nil
	}
	run := r.Runs[0]
	if run.Tool != nil && run.Tool.Driver != nil {
		out.Tool = deref(run.Tool.Driver.Name)
	}
	rules := map[string]*sarif.ReportingDescriptor{}
	if run.Tool != nil && run.Tool.Driver != nil {
		for _, rule := range run.Tool.Driver.Rules {
			if rule != nil && rule.ID != nil {
				rules[*rule.ID] = rule
			}
		}
	}
	for _, res := range run.Results {
		if res == nil {
			continue
		}
		f := Finding{Rule: deref(res.RuleID), Level: res.Level, Location: resultLocation(res), Message: messageText(res.Message)}
		rule := rules[f.Rule]
		if f.Level == "" && rule != nil && rule.DefaultConfiguration != nil {
			f.Level = rule.DefaultConfiguration.Level
		}
		score, scored := securitySeverity(res.Properties)
		if !scored && rule != nil {
			score, scored = securitySeverity(rule.Properties)
		}
		// The document's own severity text wins; the score is bucketed only
		// where there is none, and alone decides importance.
		if rule != nil {
			f.Severity = ruleSeverityText(rule.Properties)
		}
		if scored {
			f.score = score
			if f.Severity == "" {
				f.Severity = severityBucket(score)
			}
			out.ImportantBasis = basisSecuritySeverity
		}
		out.Findings = append(out.Findings, f)
	}
	return Document{Format: "SARIF 2.1.0", Scan: out}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func messageText(m *sarif.Message) string {
	if m == nil {
		return ""
	}
	return deref(m.Text)
}

// resultLocation is the first location's file URI, else its first logical
// location's qualified name.
func resultLocation(res *sarif.Result) string {
	for _, l := range res.Locations {
		if l == nil {
			continue
		}
		if l.PhysicalLocation != nil && l.PhysicalLocation.ArtifactLocation != nil {
			if uri := deref(l.PhysicalLocation.ArtifactLocation.URI); uri != "" {
				return uri
			}
		}
		for _, ll := range l.LogicalLocations {
			if ll != nil && deref(ll.FullyQualifiedName) != "" {
				return *ll.FullyQualifiedName
			}
		}
		// Only the first location is read.
		break
	}
	return ""
}

// securitySeverity reads the security-severity property, which tools write
// as a string or a number.
func securitySeverity(p *sarif.PropertyBag) (float64, bool) {
	if p == nil {
		return 0, false
	}
	switch v := p.Properties["security-severity"].(type) {
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	case float64:
		return v, true
	}
	return 0, false
}

func severityBucket(score float64) string {
	switch {
	case score >= 9:
		return "critical"
	case score >= 7:
		return "high"
	case score >= 4:
		return "medium"
	case score > 0:
		return "low"
	}
	return ""
}

// ruleSeverityText is the rule's own severity word.
func ruleSeverityText(p *sarif.PropertyBag) string {
	if p == nil {
		return ""
	}
	for _, k := range []string{"severity", "problem.severity"} {
		if s, ok := p.Properties[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
