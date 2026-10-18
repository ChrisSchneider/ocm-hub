package document

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func load(t *testing.T, name string) Document {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	d, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return d
}

func pkgByName(s *SBOM, name string) (Package, bool) {
	for _, p := range s.Packages {
		if p.Name == name {
			return p, true
		}
	}
	return Package{}, false
}

func TestParseSPDX(t *testing.T) {
	d := load(t, "spdx.json")
	if d.Format != "SPDX 2.3" || d.SBOM == nil || len(d.SBOM.Packages) != 3 {
		t.Fatalf("got %q %+v", d.Format, d.SBOM)
	}
	left, ok := pkgByName(d.SBOM, "left-pad")
	if !ok || left.Version != "1.3.0" || left.License != "MIT" || left.Type != "npm" {
		t.Errorf("left-pad = %+v", left)
	}
	if musl, _ := pkgByName(d.SBOM, "musl"); musl.License != "" || musl.Type != "" {
		t.Errorf("NOASSERTION license and missing purl should be empty: %+v", musl)
	}
	if zlib, _ := pkgByName(d.SBOM, "zlib"); zlib.Type != "apk" {
		t.Errorf("zlib type = %q", zlib.Type)
	}
}

func TestParseCycloneDXNested(t *testing.T) {
	d := load(t, "cyclonedx.json")
	if d.Format != "CycloneDX 1.6" || d.SBOM == nil {
		t.Fatalf("got %q %+v", d.Format, d.SBOM)
	}
	inner, ok := pkgByName(d.SBOM, "alpha-inner")
	if !ok || inner.Type != "npm" {
		t.Errorf("nested component missing: %+v", d.SBOM.Packages)
	}
	if a, _ := pkgByName(d.SBOM, "alpha"); a.License != "Apache-2.0" {
		t.Errorf("alpha license = %q", a.License)
	}
	if b, _ := pkgByName(d.SBOM, "beta"); b.Type != "golang" {
		t.Errorf("beta type = %q", b.Type)
	}
	t.Logf("packages: %+v", d.SBOM.Packages)
}

// Analysed CycloneDX vulnerabilities are VEX statements, the others scan
// findings, next to the packages.
func TestParseCycloneDXVulnerabilities(t *testing.T) {
	d := load(t, "cyclonedx-vex.json")
	if d.Format != "CycloneDX 1.6" || d.SBOM == nil || d.VEX == nil || d.Scan == nil {
		t.Fatalf("got %q sbom=%v vex=%v scan=%v", d.Format, d.SBOM, d.VEX, d.Scan)
	}
	s := d.VEX.Statements
	if len(s) != 3 {
		t.Fatalf("statements = %+v", s)
	}
	if s[0].Vulnerability != "CVE-2026-1" || s[0].Status != "affected" || s[0].Action != "will_not_fix: No fix in trixie yet." ||
		!slices.Equal(s[0].Products, []string{"pkg:deb/debian/libexpat1@2.8.3"}) {
		t.Errorf("statement 0 = %+v", s[0])
	}
	if s[1].Status != "not_affected" || s[1].Justification != "false_positive" || !slices.Equal(s[1].Products, []string{"libuuid1@2.41.5", "inner@1.0"}) {
		t.Errorf("statement 1 = %+v", s[1])
	}
	if s[2].Status != "under_investigation" || !slices.Equal(s[2].Products, []string{"pkg:generic/unknown@1"}) {
		t.Errorf("statement 2 = %+v", s[2])
	}
	f := d.Scan.Findings
	if d.Scan.Tool != "DevGuard" || len(f) != 2 || f[0].Rule != "CVE-2026-4" || f[0].Severity != "critical" ||
		f[0].Location != "pkg:deb/debian/libexpat1@2.8.3" || f[0].Message != "scored" || f[1].Severity != "low" {
		t.Errorf("scan = %+v", d.Scan)
	}
}

// A CycloneDX VEX without components has no SBOM part.
func TestParseCycloneDXVEXOnly(t *testing.T) {
	in := `{"bomFormat":"CycloneDX","specVersion":"1.5","metadata":{"tools":[{"name":"old"}]},
"vulnerabilities":[{"id":"CVE-1","analysis":{"state":"resolved"}},{"id":"CVE-2","ratings":[{"severity":"high"}]}]}`
	d, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if d.SBOM != nil || d.VEX == nil || d.VEX.Statements[0].Status != "fixed" || d.Scan == nil || d.Scan.Tool != "old" {
		t.Errorf("got %+v %+v %+v", d.SBOM, d.VEX, d.Scan)
	}
}

func TestParseOpenVEX(t *testing.T) {
	d := load(t, "openvex.json")
	if d.Format != "OpenVEX 0.2" || d.VEX == nil || len(d.VEX.Statements) != 3 {
		t.Fatalf("got %q %+v", d.Format, d.VEX)
	}
	s := d.VEX.Statements
	if s[0].Vulnerability != "CVE-2026-0001" || s[0].Status != "not_affected" || s[0].Justification != "vulnerable_code_not_present" {
		t.Errorf("statement 0 = %+v", s[0])
	}
	if s[1].Action != "Upgrade to 2.0" {
		t.Errorf("statement 1 = %+v", s[1])
	}
	if len(s[2].Products) != 2 || s[2].Products[1] != "pkg:oci/other@sha256:bbb" {
		t.Errorf("statement 2 products = %v", s[2].Products)
	}
}

func TestParseOpenVEXLegacy(t *testing.T) {
	d := load(t, "openvex-v001.json")
	if d.Format != "OpenVEX" || d.VEX == nil || len(d.VEX.Statements) != 1 {
		t.Fatalf("got %q %+v", d.Format, d.VEX)
	}
	s := d.VEX.Statements[0]
	if s.Vulnerability != "CVE-2022-0001" || len(s.Products) != 1 || s.Products[0] != "pkg:apk/alpine/zlib@1.2" {
		t.Errorf("statement = %+v", s)
	}
}

func TestParseSARIFSeverity(t *testing.T) {
	d := load(t, "trivy.sarif.json")
	if d.Format != "SARIF 2.1.0" || d.Scan == nil || d.Scan.Tool != "Trivy" {
		t.Fatalf("got %q %+v", d.Format, d.Scan)
	}
	f := d.Scan.Findings[0]
	if f.Rule != "CVE-1" || f.Severity != "critical" || f.Level != "error" || f.Location != "lib/a" || f.Message != "finding 1" {
		t.Errorf("finding 0 = %+v", f)
	}
	// A result score wins over the rule's.
	if got := d.Scan.Findings[4].Severity; got != "low" {
		t.Errorf("finding 4 severity = %q, want low", got)
	}
}

// The rule's own severity text wins over the bucket of its score; the score
// still decides which findings are important.
func TestParseSARIFSeverityText(t *testing.T) {
	in := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"t","rules":[
{"id":"A","properties":{"severity":"Moderate","security-severity":"9.1"}},
{"id":"B","properties":{"problem.severity":"warning","security-severity":"1.0"}},
{"id":"C","properties":{"security-severity":"7.0"}}]}},
"results":[{"ruleId":"A","message":{"text":"a"}},{"ruleId":"B","message":{"text":"b"}},{"ruleId":"C","message":{"text":"c"}}]}]}`
	d, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range d.Scan.Findings {
		got = append(got, f.Severity)
	}
	if want := []string{"Moderate", "warning", "high"}; !slices.Equal(got, want) {
		t.Errorf("severities = %q, want %q", got, want)
	}
	if s := d.Summary(); s.ImportantFindings != 2 || s.ImportantBasis != "security-severity" {
		t.Errorf("summary = %+v", s)
	}
}

func TestParseSARIFLevelOnly(t *testing.T) {
	d := load(t, "level-only.sarif.json")
	if d.Scan.ImportantBasis != "level" {
		t.Fatalf("basis = %q", d.Scan.ImportantBasis)
	}
	f := d.Scan.Findings
	if f[0].Location != "pkg.Func" || f[0].Severity != "" {
		t.Errorf("logical location finding = %+v", f[0])
	}
}

func TestParseUnsupported(t *testing.T) {
	for _, in := range []string{"SPDXVersion: SPDX-2.3", "<bom/>", "{}", `{"version":"1.0.0","runs":[]}`} {
		if _, err := Parse([]byte(in)); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Parse(%q) err = %v, want ErrUnsupported", in, err)
		}
	}
}

func TestParseTooLarge(t *testing.T) {
	if _, err := Parse(make([]byte, MaxSize+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestSummary(t *testing.T) {
	cases := []struct {
		file string
		want Summary
	}{
		{"openvex.json", Summary{Format: "OpenVEX 0.2", Statements: 3, Affected: 1}},
		{"trivy.sarif.json", Summary{Format: "SARIF 2.1.0", Findings: 5, ImportantFindings: 2, ImportantBasis: "security-severity", Tool: "Trivy"}},
		{"level-only.sarif.json", Summary{Format: "SARIF 2.1.0", Findings: 4, ImportantFindings: 1, ImportantBasis: "level", Tool: "lint"}},
		{"spdx.json", Summary{Format: "SPDX 2.3", Packages: 3}},
		{"cyclonedx-vex.json", Summary{Format: "CycloneDX 1.6", Tool: "DevGuard", ImportantBasis: "security-severity",
			Packages: 3, Statements: 3, Affected: 1, Findings: 2, ImportantFindings: 1}},
	}
	for _, c := range cases {
		if got := load(t, c.file).Summary(); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.file, got, c.want)
		}
	}
}
