// Package document normalises SBOM, VEX, scan and compliance assessment
// documents into one small model. Parsing is delegated to protobom, go-vex and
// go-sarif (CycloneDX vulnerabilities and OSCAL assessment results are read
// here); this package only detects the family,
// flattens the result and counts for summaries.
package document

import "errors"

// MaxSize caps the documents the hub reads.
const MaxSize = 32 << 20

var (
	// ErrUnsupported means the bytes are not a recognised SBOM, VEX or scan
	// document (or the library could not read them).
	ErrUnsupported = errors.New("unsupported document format")
	// ErrTooLarge means a document exceeds MaxSize.
	ErrTooLarge = errors.New("document too large")
)

// Scan importance bases.
const (
	basisSecuritySeverity = "security-severity"
	basisLevel            = "level"
)

// importantScore is the security-severity from which a finding counts as
// important.
const importantScore = 7.0

// Package is one flattened SBOM package.
type Package struct{ Name, Version, License, Type, PURL string }

// SBOM is the flattened package list of an SPDX or CycloneDX document.
type SBOM struct{ Packages []Package }

// Statement is one VEX statement.
type Statement struct {
	Vulnerability string
	Products      []string
	Status        string
	Justification string
	Action        string
}

// VEX is the statement list of an OpenVEX document, or the analysed
// vulnerabilities of a CycloneDX one.
type VEX struct{ Statements []Statement }

// Finding is one SARIF result or one CycloneDX vulnerability.
type Finding struct {
	Rule, Severity, Level, Location, Message string

	// score is the security-severity, 0 when the result and its rule have none.
	score float64
}

// Scan is the findings of a SARIF document's first run, or all the
// vulnerabilities of a CycloneDX one.
type Scan struct {
	Tool     string
	Findings []Finding
	// ImportantBasis is "security-severity" when any result has a score, else
	// "level".
	ImportantBasis string
}

// Document is a parsed document; at least one of SBOM, VEX, Scan and
// Assessment is set. A CycloneDX document may carry the first three.
type Document struct {
	Format     string
	SBOM       *SBOM
	VEX        *VEX
	Scan       *Scan
	Assessment *Assessment
}

// Summary is the counts a sync stores for a document.
type Summary struct {
	Format, Tool, ImportantBasis                                string
	Packages, Statements, Affected, Findings, ImportantFindings int
	// Requirements counts the findings of an assessment, NotSatisfied those
	// whose status is not satisfied.
	Requirements, NotSatisfied int
}

// Summary counts the document's packages, statements and findings.
func (d Document) Summary() Summary {
	s := Summary{Format: d.Format}
	if d.SBOM != nil {
		s.Packages = len(d.SBOM.Packages)
	}
	if d.VEX != nil {
		s.Statements = len(d.VEX.Statements)
		for _, st := range d.VEX.Statements {
			if st.Status == "affected" {
				s.Affected++
			}
		}
	}
	if d.Scan != nil {
		s.Tool = d.Scan.Tool
		s.ImportantBasis = d.Scan.ImportantBasis
		s.Findings = len(d.Scan.Findings)
		for _, f := range d.Scan.Findings {
			if d.Scan.ImportantBasis == basisSecuritySeverity {
				if f.score >= importantScore {
					s.ImportantFindings++
				}
			} else if f.Level == "error" {
				s.ImportantFindings++
			}
		}
	}
	if d.Assessment != nil {
		s.Requirements = len(d.Assessment.Findings)
		for _, f := range d.Assessment.Findings {
			if f.State != "satisfied" {
				s.NotSatisfied++
			}
		}
	}
	return s
}
