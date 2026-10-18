package fips

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"chrisschneider.dev/delivery-bundler/internal/compliance"
	"chrisschneider.dev/delivery-bundler/internal/document"
)

func sbomPackages(t *testing.T, path string) []compliance.Package {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := document.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return compliance.Packages(d.SBOM)
}

func ruleIDs(rs []compliance.SARIFResult) []string {
	var ids []string
	for _, r := range rs {
		ids = append(ids, r.RuleID)
	}
	slices.Sort(ids)
	return ids
}

func TestCheckSBOMs(t *testing.T) {
	for _, tc := range []struct {
		file string
		want []string
	}{
		// DevGuard SBOM of zendis/python3:3.14-oc.13-minimal: Debian OpenSSL 3.5
		// plus the legacy provider, no FIPS provider.
		{"testdata/python3.cdx.json", []string{RuleNoFIPSProvider, RuleLegacyProvider}},
		{"testdata/alpine.cdx.json", []string{RuleNoCryptoLibrary}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			got := ruleIDs(Check(compliance.Image{Name: "img"}, sbomPackages(t, tc.file)))
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("rules = %v, want %v", got, want)
			}
		})
	}
}

func pkg(purl string) compliance.Package {
	return compliance.Packages(&document.SBOM{Packages: []document.Package{{PURL: purl}}})[0]
}

func TestCheckRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		pkgs []string
		want []string
	}{
		{"fips provider present", []string{"pkg:apk/wolfi/libcrypto3@3.5.0", "pkg:apk/wolfi/openssl-provider-fips@3.1.2"}, nil},
		{"rhel openssl-libs ships fips.so", []string{"pkg:rpm/redhat/openssl-libs@1:3.0.7-27.el9"}, nil},
		{"openssl 1.1", []string{"pkg:deb/debian/libssl1.1@1.1.1n-0"}, []string{RuleOldOpenSSL}},
		{"bcprov", []string{"pkg:maven/org.bouncycastle/bcprov-jdk18on@1.78"}, []string{RuleBundledCrypto}},
		{"bc-fips is fine", []string{"pkg:maven/org.bouncycastle/bc-fips@2.0.0"}, []string{RuleNoCryptoLibrary}},
		{"pycryptodome", []string{"pkg:pypi/pycryptodome@3.20.0"}, []string{RuleBundledCrypto}},
		{"gnutls", []string{"pkg:deb/debian/libgnutls30t64@3.8.9"}, []string{RuleOtherCryptoLib}},
		{"go once", []string{"pkg:golang/stdlib@1.24.0", "pkg:golang/golang.org/x/net@0.30.0"}, []string{RuleGoBinary}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ps []compliance.Package
			for _, p := range tc.pkgs {
				ps = append(ps, pkg(p))
			}
			got := ruleIDs(Check(compliance.Image{Name: "img"}, ps))
			if !slices.Equal(got, tc.want) {
				t.Errorf("rules = %v, want %v", got, tc.want)
			}
		})
	}
}

// The hub must read the SARIF the scanner writes.
func TestSARIFReadByHub(t *testing.T) {
	c := &compliance.Component{
		Ref:    compliance.Ref{Repository: "ghcr.io/x", Component: "example.com/c", Version: "1.0.0"},
		Images: []compliance.Image{{Name: "python3"}, {Name: "nosbom"}},
		SBOMs: map[string][]compliance.SBOM{
			"python3": {{Source: "embedded python3-sbom", Packages: sbomPackages(t, "testdata/python3.cdx.json")}},
		},
	}
	b, err := json.Marshal(Scan(c))
	if err != nil {
		t.Fatal(err)
	}
	d, err := document.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	s := d.Summary()
	if s.Tool != Tool || s.Findings != 3 || s.ImportantFindings != 1 {
		t.Errorf("summary = %+v, want tool %s, 3 findings, 1 with level error", s, Tool)
	}
}
