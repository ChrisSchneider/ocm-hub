package attestation

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// statement is the part of an in-toto statement we read.
type statement struct {
	PredicateType string          `json:"predicateType"`
	Predicate     json.RawMessage `json:"predicate"`
}

// decodeStatement reads an in-toto statement, plain, in a DSSE envelope or in
// a sigstore bundle. Signatures are not checked.
func decodeStatement(mediaType string, b []byte) (statement, bool) {
	var st statement
	switch {
	case mediaType == mediaTypeInToto:
	case mediaType == mediaTypeDSSE, strings.HasPrefix(mediaType, typeSigstore):
		var env struct {
			Payload      string `json:"payload"`
			DSSEEnvelope *struct {
				Payload string `json:"payload"`
			} `json:"dsseEnvelope"`
		}
		if json.Unmarshal(b, &env) != nil {
			return st, false
		}
		p := env.Payload
		if env.DSSEEnvelope != nil {
			p = env.DSSEEnvelope.Payload
		}
		var err error
		if b, err = base64.StdEncoding.DecodeString(p); err != nil {
			return st, false
		}
	default:
		return st, false
	}
	if json.Unmarshal(b, &st) != nil || st.PredicateType == "" {
		return st, false
	}
	return st, true
}

var vsaLevel = regexp.MustCompile(`^SLSA_(?:BUILD_)?LEVEL_(\d)$`)

// Builders whose provenance implies a build level, matched by prefix:
// slsa-github-generator documents Build L3, GitHub-hosted runners with
// artifact attestations Build L2.
var builderLevels = []struct {
	prefix string
	level  int
}{
	{"https://github.com/slsa-framework/slsa-github-generator/", 3},
	{"https://github.com/actions/runner/github-hosted", 2},
}

// provenance reads a SLSA statement: the build level it claims (the highest
// passed level of a verification summary, or the level a known builder
// implies for provenance; 0 unknown) and the builder id of provenance ("" for
// verification summaries or when absent). Nothing is verified.
func provenance(predicateType string, predicate []byte) (level int, builder string) {
	switch {
	case strings.HasPrefix(predicateType, "https://slsa.dev/verification_summary/"):
		var p struct {
			Result string   `json:"verificationResult"`
			Levels []string `json:"verifiedLevels"` // v1
			Policy string   `json:"policyLevel"`    // v0.2
		}
		if json.Unmarshal(predicate, &p) != nil || p.Result != "PASSED" {
			return 0, ""
		}
		best := 0
		for _, l := range append(p.Levels, p.Policy) {
			if m := vsaLevel.FindStringSubmatch(l); m != nil {
				n, _ := strconv.Atoi(m[1])
				best = max(best, n)
			}
		}
		return best, ""
	case strings.HasPrefix(predicateType, "https://slsa.dev/provenance/"):
		var p struct {
			Builder    struct{ ID string } `json:"builder"`
			RunDetails struct {
				Builder struct{ ID string } `json:"builder"`
			} `json:"runDetails"`
		}
		if json.Unmarshal(predicate, &p) != nil {
			return 0, ""
		}
		id := p.RunDetails.Builder.ID
		if id == "" {
			id = p.Builder.ID
		}
		for _, b := range builderLevels {
			if id != "" && strings.HasPrefix(id, b.prefix) {
				return b.level, id
			}
		}
		return 0, id
	}
	return 0, ""
}
