// Package fips is step 1 of the FIPS scanner: heuristics on an image's SBOM
// that find images which are definitely not FIPS 140 compliant. A clean
// result proves nothing; the image filesystem (step 2) and a runtime self-test
// (step 3) are not looked at.
package fips

import (
	"fmt"
	"slices"
	"strings"

	"chrisschneider.dev/delivery-bundler/internal/compliance"
)

// Tool is the SARIF driver name.
const Tool = "ocm-fipscheck"

// PredicateType is the in-toto predicate type of the SARIF attestation; the
// hub classifies it as a FIPS scan.
const PredicateType = "https://chrisschneider.dev/attestation/fips-scan/v1"

// Rule IDs.
const (
	RuleNoSBOM          = "FIPS-000"
	RuleNoFIPSProvider  = "FIPS-001"
	RuleOldOpenSSL      = "FIPS-002"
	RuleLegacyProvider  = "FIPS-003"
	RuleBundledCrypto   = "FIPS-004"
	RuleOtherCryptoLib  = "FIPS-005"
	RuleGoBinary        = "FIPS-006"
	RuleNoCryptoLibrary = "FIPS-007"
	RuleSBOMUnreadable  = "FIPS-008"
)

// Rules are the SARIF rule definitions, in ID order.
var Rules = []compliance.SARIFRule{
	rule(RuleNoSBOM, "NoSBOM", compliance.LevelWarning, "Image has no SBOM",
		"The component carries no SBOM for this image (no sbom resource whose ocm.software/artifact-references label names it), so its cryptography cannot be assessed.",
		"Add an SBOM resource for the image with an ocm.software/artifact-references label."),
	rule(RuleNoFIPSProvider, "OpenSSLWithoutFIPSProvider", compliance.LevelError, "OpenSSL 3 without a FIPS provider",
		"The image ships OpenSSL 3 but no FIPS provider package (fips.so). Without it OpenSSL cannot run in FIPS mode, so every consumer of the system OpenSSL uses non-validated cryptography.",
		"Use a FIPS image variant whose OpenSSL ships a CMVP-validated FIPS provider and activates it in openssl.cnf."),
	rule(RuleOldOpenSSL, "OpenSSLBefore3", compliance.LevelError, "OpenSSL before 3.0",
		"OpenSSL 1.x has no FIPS 140-3 validated module; the 1.0.2 FIPS Object Module is historical.",
		"Move to OpenSSL 3 with a validated FIPS provider."),
	rule(RuleLegacyProvider, "OpenSSLLegacyProvider", compliance.LevelWarning, "OpenSSL legacy provider installed",
		"The OpenSSL legacy provider (MD4, DES, RC4, ...) is installed. It offers non-approved algorithms and has no place in a FIPS image.",
		"Remove the legacy provider package."),
	rule(RuleBundledCrypto, "BundledCrypto", compliance.LevelError, "Crypto library that bypasses the system OpenSSL",
		"A language package brings its own cryptography instead of the system OpenSSL, so a FIPS provider in the image would not cover it.",
		"Replace it with a FIPS-capable alternative (e.g. bc-fips instead of bcprov) or one built against the system OpenSSL."),
	rule(RuleOtherCryptoLib, "OtherCryptoLibrary", compliance.LevelWarning, "Other crypto library",
		"The image ships a crypto library besides OpenSSL. Whether it runs a validated module in FIPS mode cannot be told from the SBOM.",
		"Check that the library is a validated build used in FIPS mode, or remove it."),
	rule(RuleGoBinary, "GoBinary", compliance.LevelNote, "Go binaries in the image",
		"The SBOM lists Go modules. Go binaries are FIPS-capable only when built with GOFIPS140 or boringcrypto; this needs the binary itself (step 2).",
		"Build Go binaries with the Go FIPS 140-3 module (GOFIPS140) and verify the build info."),
	rule(RuleNoCryptoLibrary, "NoCryptoLibrary", compliance.LevelNote, "No crypto library in the SBOM",
		"The SBOM lists no known crypto library, so nothing could be disproven. The SBOM may be incomplete.",
		"Check that the SBOM covers all packages of the image."),
	rule(RuleSBOMUnreadable, "SBOMUnreadable", compliance.LevelWarning, "SBOM could not be read",
		"The image's SBOM could not be downloaded or parsed (CycloneDX and SPDX JSON are supported).",
		"Attach the SBOM as CycloneDX or SPDX JSON."),
}

func rule(id, name, level, short, full, help string) compliance.SARIFRule {
	return compliance.SARIFRule{ID: id, Name: name, ShortDescription: compliance.SARIFText{Text: short},
		FullDescription: compliance.SARIFText{Text: full}, Help: compliance.SARIFText{Text: help},
		DefaultConfiguration: compliance.SARIFRuleConfig{Level: level}, Properties: &compliance.SARIFRuleProps{Tags: []string{"fips", "security"}}}
}

func levelOf(id string) string {
	for _, r := range Rules {
		if r.ID == id {
			return r.DefaultConfiguration.Level
		}
	}
	return compliance.LevelNote
}

// Scan checks every image of c and returns the SARIF log.
func Scan(c *compliance.Component) compliance.SARIF {
	var results []compliance.SARIFResult
	for _, img := range c.Images {
		sboms := c.SBOMs[img.Name]
		if len(sboms) == 0 {
			results = append(results, result(RuleNoSBOM, img, nil, fmt.Sprintf("%s: no SBOM in the component or the registry, FIPS cannot be assessed.", img.Name)))
			continue
		}
		// Platforms and duplicate SBOMs are merged: one result per package.
		var pkgs []compliance.Package
		seen := map[string]bool{}
		read := 0
		for _, s := range sboms {
			if s.Err != nil {
				results = append(results, result(RuleSBOMUnreadable, img, nil, fmt.Sprintf("%s: SBOM %s: %v", img.Name, s.Source, s.Err)))
				continue
			}
			read++
			for _, p := range s.Packages {
				if !seen[p.Ref()] {
					seen[p.Ref()] = true
					pkgs = append(pkgs, p)
				}
			}
		}
		if read > 0 {
			results = append(results, Check(img, pkgs)...)
		}
	}
	props := map[string]string{"component": c.Ref.Component, "version": c.Ref.Version, "repository": c.Ref.Repository, "step": "1 (SBOM only)"}
	return compliance.NewSARIF(compliance.SARIFDriver{Name: Tool, Version: "0.1.0", Rules: Rules}, results, props)
}

func result(id string, img compliance.Image, p *compliance.Package, msg string) compliance.SARIFResult {
	loc := compliance.SARIFLogicalLocation{Name: img.Name, FullyQualifiedName: img.Name, Kind: "resource"}
	props := map[string]string{"image": img.Name}
	if img.Reference != "" {
		props["imageReference"] = img.Reference
	}
	if p != nil {
		loc = compliance.SARIFLogicalLocation{Name: p.BaseName, FullyQualifiedName: img.Name + " › " + p.Ref(), Kind: "package"}
		props["package"] = p.Ref()
	}
	return compliance.SARIFResult{RuleID: id, Level: levelOf(id), Message: compliance.SARIFText{Text: msg},
		Locations: []compliance.SARIFLocation{{LogicalLocations: []compliance.SARIFLogicalLocation{loc}}}, Properties: props}
}

// Check applies the SBOM rules to the packages of one image.
func Check(img compliance.Image, pkgs []compliance.Package) []compliance.SARIFResult {
	var out []compliance.SARIFResult
	var openssl3 []compliance.Package
	hasProvider, sawCrypto, sawGo := false, false, false
	for _, p := range pkgs {
		name := strings.ToLower(p.BaseName)
		switch {
		case isFIPSProvider(p):
			hasProvider, sawCrypto = true, true
		case isOpenSSLLib(p):
			sawCrypto = true
			if openSSLMajor(p) >= 3 {
				// RHEL-family openssl-libs ship fips.so themselves.
				if p.Type == "rpm" && name == "openssl-libs" {
					hasProvider = true
				}
				openssl3 = append(openssl3, p)
			} else {
				out = append(out, result(RuleOldOpenSSL, img, &p, fmt.Sprintf("%s ships %s %s: OpenSSL 1.x has no FIPS 140-3 validated module.", img.Name, p.BaseName, p.Version)))
			}
		case name == "openssl-provider-legacy" || name == "openssl-legacy-provider" || name == "libcrypto3-legacy":
			sawCrypto = true
			out = append(out, result(RuleLegacyProvider, img, &p, fmt.Sprintf("%s ships %s %s: non-approved algorithms (MD4, DES, RC4, ...) are installed.", img.Name, p.BaseName, p.Version)))
		case bundledCrypto(p) != "":
			sawCrypto = true
			out = append(out, result(RuleBundledCrypto, img, &p, fmt.Sprintf("%s ships %s %s: %s", img.Name, p.BaseName, p.Version, bundledCrypto(p))))
		case isOtherCryptoLib(p):
			sawCrypto = true
			out = append(out, result(RuleOtherCryptoLib, img, &p, fmt.Sprintf("%s ships %s %s: FIPS mode of this library cannot be told from the SBOM.", img.Name, p.BaseName, p.Version)))
		case p.Type == "golang" && !sawGo:
			sawGo = true
			out = append(out, result(RuleGoBinary, img, &p, fmt.Sprintf("%s contains Go modules (e.g. %s): FIPS depends on how the binaries were built.", img.Name, p.Ref())))
		}
	}
	if len(openssl3) > 0 && !hasProvider {
		p := openssl3[0]
		out = append(out, result(RuleNoFIPSProvider, img, &p, fmt.Sprintf("%s ships %s %s but no OpenSSL FIPS provider package: OpenSSL cannot run in FIPS mode.", img.Name, p.BaseName, p.Version)))
	}
	if !sawCrypto && !sawGo {
		out = append(out, result(RuleNoCryptoLibrary, img, nil, fmt.Sprintf("%s: the SBOM (%d packages) lists no known crypto library; nothing could be disproven.", img.Name, len(pkgs))))
	}
	return out
}

// isOpenSSLLib matches the libcrypto/libssl packages of the common distros.
func isOpenSSLLib(p compliance.Package) bool {
	n := strings.ToLower(p.BaseName)
	switch p.Type {
	case "deb":
		return strings.HasPrefix(n, "libssl1") || strings.HasPrefix(n, "libssl3")
	case "apk":
		return strings.HasPrefix(n, "libcrypto1") || strings.HasPrefix(n, "libcrypto3") || strings.HasPrefix(n, "libssl1") || strings.HasPrefix(n, "libssl3")
	case "rpm":
		return n == "openssl-libs" || n == "openssl1.1"
	case "":
		return n == "openssl" || n == "libssl" || n == "libcrypto" || n == "openssl-libs"
	}
	return false
}

func openSSLMajor(p compliance.Package) int {
	n := strings.ToLower(p.BaseName)
	switch {
	case strings.Contains(n, "1.1") || strings.Contains(n, "1.0") || strings.HasPrefix(n, "libssl1") || strings.HasPrefix(n, "libcrypto1"):
		return 1
	case strings.HasSuffix(n, "3") || strings.Contains(n, "ssl3") || strings.Contains(n, "crypto3"):
		return 3
	}
	v := p.Version
	if _, rest, ok := strings.Cut(v, ":"); ok { // deb/rpm epoch
		v = rest
	}
	if v != "" && v[0] >= '0' && v[0] <= '9' {
		return int(v[0] - '0')
	}
	return 3
}

// isFIPSProvider matches OpenSSL FIPS provider packages (openssl-provider-fips,
// openssl-fips-provider, Chainguard's openssl-config-fipshardened, ...).
func isFIPSProvider(p compliance.Package) bool {
	n := strings.ToLower(p.BaseName)
	return strings.Contains(n, "fips") && (strings.Contains(n, "openssl") || strings.Contains(n, "libcrypto") || strings.Contains(n, "provider"))
}

// bundledCrypto explains why a language package bypasses the system OpenSSL;
// "" when it does not.
func bundledCrypto(p compliance.Package) string {
	n := strings.ToLower(p.BaseName)
	switch p.Type {
	case "pypi":
		switch n {
		case "pycryptodome", "pycryptodomex", "pycrypto":
			return "pure Python/C cryptography, never a validated module."
		case "pynacl":
			return "wraps libsodium, which has no FIPS validation."
		case "cryptography":
			return "PyPI wheels bundle their own OpenSSL; the system FIPS provider does not apply."
		}
	case "maven":
		if p.Namespace == "org.bouncycastle" && strings.HasPrefix(n, "bcprov") && !strings.Contains(n, "fips") {
			return "the regular Bouncy Castle provider is not FIPS validated (bc-fips is)."
		}
	case "cargo":
		switch n {
		case "ring", "boring", "boring-sys", "openssl-src", "rustls-rustcrypto":
			return "Rust crate with its own crypto backend, not a validated module."
		}
	case "npm":
		switch n {
		case "crypto-js", "node-forge", "tweetnacl", "elliptic":
			return "JavaScript cryptography, never a validated module."
		}
	case "golang":
		if strings.HasPrefix(p.Namespace, "github.com/cloudflare/circl") || n == "circl" {
			return "Go crypto outside the standard library is not covered by the Go FIPS module."
		}
	}
	return ""
}

var otherCryptoLibs = []string{"libgnutls", "libnettle", "libhogweed", "libgcrypt", "libsodium", "libnss3", "nss", "libmbedcrypto", "mbedtls", "libwolfssl", "wolfssl", "libbotan", "libtomcrypt"}

func isOtherCryptoLib(p compliance.Package) bool {
	if p.Type != "deb" && p.Type != "apk" && p.Type != "rpm" && p.Type != "" {
		return false
	}
	n := strings.ToLower(p.BaseName)
	return slices.ContainsFunc(otherCryptoLibs, func(prefix string) bool { return strings.HasPrefix(n, prefix) })
}
