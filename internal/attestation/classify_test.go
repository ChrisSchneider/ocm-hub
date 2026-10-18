package attestation

import "testing"

func TestClassify(t *testing.T) {
	for _, c := range []struct {
		typ    string
		kind   Kind
		format string
	}{
		{"https://slsa.dev/provenance/v0.2", KindSLSA, "SLSA v0.2"},
		{"https://slsa.dev/provenance/v1", KindSLSA, "SLSA v1"},
		{"https://slsa.dev/provenance/v0.1", KindSLSA, "SLSA v0.1"},
		{"https://spdx.dev/Document", KindSBOM, "SPDX"},
		{"application/spdx+json", KindSBOM, "SPDX"},
		{"text/spdx", KindSBOM, "SPDX"},
		{"https://cyclonedx.org/bom", KindSBOM, "CycloneDX"},
		{"https://cyclonedx.org/bom/v1.6", KindSBOM, "CycloneDX 1.6"},
		{"application/vnd.cyclonedx+json", KindSBOM, "CycloneDX"},
		{"application/vnd.syft+json", KindSBOM, "Syft"},
		{"application/swid+xml", KindSBOM, "SWID"},
		{"https://openvex.dev/ns/v0.2.0", KindVEX, "OpenVEX"},
		{"https://openvex.dev/ns", KindVEX, "OpenVEX"},
		{"application/csaf+json", KindVEX, "CSAF"},
		{"https://cosign.sigstore.dev/attestation/vuln/v1", KindCVE, "cosign vuln v1"},
		{"https://docker.com/dhi/fips/v0.1", KindFIPS, ""},
		{"https://docker.com/dhi/source/v0.1", KindSources, ""},
		{"https://example.com/custom/v1", KindOther, ""},
		{"", KindOther, ""},
	} {
		if k, f := Classify(c.typ); k != c.kind || f != c.format {
			t.Errorf("Classify(%q) = %s, %q; want %s, %q", c.typ, k, f, c.kind, c.format)
		}
	}
}
