package attestation

import "strings"

// rule maps a type family, matched by prefix, to a kind and format. A format
// ending in a space takes the remaining version suffix.
type rule struct {
	prefix string
	kind   Kind
	format string
}

// Ordered: the first matching prefix wins.
var rules = []rule{
	{"https://slsa.dev/provenance/", KindSLSA, "SLSA "},
	{"https://slsa.dev/verification_summary/", KindSLSA, "SLSA VSA "},
	{"https://spdx.dev/", KindSBOM, "SPDX"},
	{"application/spdx", KindSBOM, "SPDX"},
	{"text/spdx", KindSBOM, "SPDX"},
	{"https://cyclonedx.org/bom/v", KindSBOM, "CycloneDX "},
	{"https://cyclonedx.org/bom", KindSBOM, "CycloneDX"},
	{"application/vnd.cyclonedx", KindSBOM, "CycloneDX"},
	{"application/vnd.syft", KindSBOM, "Syft"},
	{"application/swid", KindSBOM, "SWID"},
	{"https://openvex.dev/ns", KindVEX, "OpenVEX"},
	{"application/csaf", KindVEX, "CSAF"},
	{"https://cosign.sigstore.dev/attestation/vuln/", KindCVE, "cosign vuln "},
	{"https://docker.com/dhi/fips/", KindFIPS, ""},
	{"https://docker.com/dhi/source/", KindSources, ""},
}

// Classify maps a predicate, artifact or media type to a kind and format.
// Unknown types are KindOther with no format.
//
// Docker Hardened Images STIG, secrets and virus scan predicate types are not
// listed: docs.docker.com/dhi does not publish their URIs.
func Classify(typ string) (Kind, string) {
	for _, r := range rules {
		rest, ok := strings.CutPrefix(typ, r.prefix)
		if !ok {
			continue
		}
		if strings.HasSuffix(r.format, " ") {
			return r.kind, r.format + rest
		}
		return r.kind, r.format
	}
	return KindOther, ""
}
