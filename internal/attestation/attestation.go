// Package attestation models which attestations (SBOM, SLSA, VEX, ...) exist
// on an OCM component version and its OCI resources. This file and
// descriptor.go are pure: they read a descriptor and do no network access.
package attestation

import "chrisschneider.dev/delivery-bundler/internal/document"

// Kind is what an attestation attests.
type Kind string

const (
	KindSLSA    Kind = "slsa"
	KindSBOM    Kind = "sbom"
	KindVEX     Kind = "vex"
	KindCVE     Kind = "cve"
	KindFIPS    Kind = "fips"
	KindSTIG    Kind = "stig"
	KindSecrets Kind = "secrets"
	KindVirus   Kind = "virus"
	KindSources Kind = "sources"
	// KindCompliance is an assessment of requirements (OSCAL assessment results).
	KindCompliance Kind = "compliance"
	KindOther      Kind = "other"
)

// Source is where an entry was found.
type Source string

const (
	SourceEmbedded  Source = "embedded"
	SourceReferrer  Source = "referrer"
	SourceCosignTag Source = "cosignTag"
	SourceBuildx    Source = "buildx"
	SourceLabelLink Source = "labelLink"
)

// Signed says what covers an attestation. Nothing is verified.
type Signed string

const (
	SignedYes         Signed = "signed"
	SignedByComponent Signed = "component"
	SignedNo          Signed = "no"
)

// Status is the outcome of looking at one level.
type Status string

const (
	StatusOK     Status = "ok"
	StatusFailed Status = "failed"
	StatusNotOCI Status = "notOci"
)

// Identity names a resource within a component version.
type Identity struct {
	Name, Version string
	ExtraIdentity map[string]string
}

// Entry is one attestation.
type Entry struct {
	Kind     Kind
	Format   string // e.g. "CycloneDX 1.6"; "" unknown
	Type     string // raw predicate/artifact/media type; "" for label links
	Source   Source
	Platform string
	Signed   Signed
	Digest   string    // attestation manifest or layer digest (registry entries); sha256 of the linked document (label links)
	Resource *Identity // embedded: the sbom resource
	URL      string    // label link
	// BuildLevel is the SLSA build level a slsa entry claims; 0 unknown.
	BuildLevel int
	Builder    string            // SLSA provenance builder id; "" unknown
	Repository string            // oras repository ("host/path") holding Digest; registry entries only
	Label      string            // label name; labelLink entries only
	Summary    *document.Summary // nil when not read
}

// Level is the component (Resource nil) or one resource.
type Level struct {
	Resource *Identity
	Status   Status
	Error    string
	Entries  []Entry
}

// Result holds the component level first, then resources in descriptor order.
// sbom resources are not levels.
type Result struct{ Levels []Level }

// Subject is what to inspect in a registry: an oras repository reference
// ("host/path") and a tag or digest. The zero Subject means no lookup.
type Subject struct{ Repository, Reference string }
