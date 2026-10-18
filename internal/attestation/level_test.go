package attestation

import (
	"encoding/base64"
	"testing"
)

func TestBuildLevel(t *testing.T) {
	for _, tc := range []struct {
		name, typ, predicate string
		want                 int
	}{
		{"vsa passed", "https://slsa.dev/verification_summary/v1",
			`{"verificationResult":"PASSED","verifiedLevels":["SLSA_BUILD_LEVEL_2","SLSA_BUILD_LEVEL_3","SLSA_SOURCE_LEVEL_1"]}`, 3},
		{"vsa failed", "https://slsa.dev/verification_summary/v1",
			`{"verificationResult":"FAILED","verifiedLevels":["SLSA_BUILD_LEVEL_3"]}`, 0},
		{"vsa v0.2 policyLevel", "https://slsa.dev/verification_summary/v0.2",
			`{"verificationResult":"PASSED","policyLevel":"SLSA_LEVEL_2"}`, 2},
		{"vsa v0.2 failed", "https://slsa.dev/verification_summary/v0.2",
			`{"verificationResult":"FAILED","policyLevel":"SLSA_LEVEL_3"}`, 0},
		{"provenance v1 slsa-github-generator", "https://slsa.dev/provenance/v1",
			`{"runDetails":{"builder":{"id":"https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@refs/tags/v2.0.0"}}}`, 3},
		{"provenance v0.2 slsa-github-generator", "https://slsa.dev/provenance/v0.2",
			`{"builder":{"id":"https://github.com/slsa-framework/slsa-github-generator/.github/workflows/builder_go_slsa3.yml@refs/tags/v1.9.0"}}`, 3},
		{"provenance v1 github hosted", "https://slsa.dev/provenance/v1",
			`{"runDetails":{"builder":{"id":"https://github.com/actions/runner/github-hosted"}}}`, 2},
		{"provenance unknown builder", "https://slsa.dev/provenance/v1",
			`{"runDetails":{"builder":{"id":"https://example.com/builder"}}}`, 0},
		{"not slsa", "https://spdx.dev/Document", `{}`, 0},
		{"garbage", "https://slsa.dev/provenance/v1", `[`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := provenance(tc.typ, []byte(tc.predicate)); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestProvenanceBuilder(t *testing.T) {
	generator := "https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@refs/tags/v2.0.0"
	for _, tc := range []struct {
		name, typ, predicate, builder string
		level                         int
	}{
		{"v1 runDetails", "https://slsa.dev/provenance/v1", `{"runDetails":{"builder":{"id":"` + generator + `"}}}`, generator, 3},
		{"v0.2 builder", "https://slsa.dev/provenance/v0.2", `{"builder":{"id":"https://example.com/builder"}}`, "https://example.com/builder", 0},
		{"vsa", "https://slsa.dev/verification_summary/v1",
			`{"verificationResult":"PASSED","verifiedLevels":["SLSA_BUILD_LEVEL_2"],"verifier":{"id":"https://example.com/verifier"}}`, "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			level, builder := provenance(tc.typ, []byte(tc.predicate))
			if level != tc.level || builder != tc.builder {
				t.Fatalf("got (%d, %q), want (%d, %q)", level, builder, tc.level, tc.builder)
			}
		})
	}
}

func TestDecodeStatement(t *testing.T) {
	st := `{"_type":"https://in-toto.io/Statement/v1","predicateType":"https://slsa.dev/provenance/v1","predicate":{"a":1}}`
	payload := base64.StdEncoding.EncodeToString([]byte(st))
	for _, tc := range []struct{ name, mediaType, body string }{
		{"in-toto", "application/vnd.in-toto+json", st},
		{"dsse", "application/vnd.dsse.envelope.v1+json", `{"payloadType":"application/vnd.in-toto+json","payload":"` + payload + `"}`},
		{"sigstore bundle", "application/vnd.dev.sigstore.bundle.v0.3+json", `{"dsseEnvelope":{"payload":"` + payload + `"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := decodeStatement(tc.mediaType, []byte(tc.body))
			if !ok || got.PredicateType != "https://slsa.dev/provenance/v1" || string(got.Predicate) != `{"a":1}` {
				t.Fatalf("got %+v, %v", got, ok)
			}
		})
	}
	if _, ok := decodeStatement("application/octet-stream", []byte(st)); ok {
		t.Fatal("decoded an unknown media type")
	}
	if _, ok := decodeStatement("application/vnd.dsse.envelope.v1+json", []byte(`{"payload":"!!"}`)); ok {
		t.Fatal("decoded a bad payload")
	}
}

func TestClassifyVSA(t *testing.T) {
	if k, f := Classify("https://slsa.dev/verification_summary/v1"); k != KindSLSA || f != "SLSA VSA v1" {
		t.Fatalf("got %s %q", k, f)
	}
}
