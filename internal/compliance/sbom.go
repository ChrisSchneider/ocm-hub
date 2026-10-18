// Package compliance holds the pieces the standalone compliance commands
// (cmd/fipscheck, cmd/assess) share: reading a component version and its
// SBOMs, SARIF output and pushing attestations. It is a prototype: checks
// only disprove compliance, they never prove it.
package compliance

import (
	"net/url"
	"strings"

	"chrisschneider.dev/delivery-bundler/internal/document"
)

// Package is one SBOM package as the checks see it. Type, Namespace and
// BaseName come from the purl ("pkg:deb/debian/libssl3t64@3.5.7" is deb,
// debian, libssl3t64); without a purl Type is "" and BaseName is Name.
type Package struct {
	Name, Version, PURL       string
	Type, Namespace, BaseName string
}

// Packages converts the packages of a parsed SBOM.
func Packages(s *document.SBOM) []Package {
	if s == nil {
		return nil
	}
	out := make([]Package, 0, len(s.Packages))
	for _, p := range s.Packages {
		out = append(out, newPackage(p.Name, p.Version, p.PURL))
	}
	return out
}

func newPackage(name, version, purl string) Package {
	p := Package{Name: name, Version: version, PURL: purl, BaseName: name}
	rest, ok := strings.CutPrefix(purl, "pkg:")
	if !ok {
		return p
	}
	rest, _, _ = strings.Cut(rest, "#")
	rest, _, _ = strings.Cut(rest, "?")
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		if p.Version == "" {
			p.Version, _ = url.PathUnescape(rest[i+1:])
		}
		rest = rest[:i]
	}
	segs := strings.Split(rest, "/")
	p.Type = strings.ToLower(segs[0])
	if len(segs) > 1 {
		p.BaseName, _ = url.PathUnescape(segs[len(segs)-1])
	}
	if len(segs) > 2 {
		p.Namespace, _ = url.PathUnescape(strings.Join(segs[1:len(segs)-1], "/"))
	}
	return p
}

// Ref is the purl, else "name@version".
func (p Package) Ref() string {
	if p.PURL != "" {
		return p.PURL
	}
	if p.Version == "" {
		return p.Name
	}
	return p.Name + "@" + p.Version
}
