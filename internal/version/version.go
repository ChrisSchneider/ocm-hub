// Package version classifies OCM versions as stable or pre-release.
package version

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultMarkers are the pre-release markers used when the config sets none.
var DefaultMarkers = []string{"alpha", "beta", "rc", "pre", "preview", "dev", "snapshot", "nightly"}

var markerSyntax = regexp.MustCompile(`^[A-Za-z]+$`)

// Classifier decides whether a version is a pre-release. A version is a
// pre-release only if the part after its first "-" (build metadata after "+"
// removed) starts with a marker, optionally followed by digits (".1", "-2",
// "3"), and then ends or continues with ".", "-" or "+". Everything else —
// "1.2", "1.2-fix3", "1.2-123123" — is stable.
type Classifier struct {
	re *regexp.Regexp
}

// NewClassifier builds a classifier for the given markers (letters only).
func NewClassifier(markers []string) (*Classifier, error) {
	if len(markers) == 0 {
		markers = DefaultMarkers
	}
	quoted := make([]string, len(markers))
	for i, m := range markers {
		if !markerSyntax.MatchString(m) {
			return nil, fmt.Errorf("pre-release marker %q: only letters are allowed", m)
		}
		quoted[i] = regexp.QuoteMeta(m)
	}
	re := regexp.MustCompile(`(?i)^(?:` + strings.Join(quoted, "|") + `)(?:[.-]?[0-9]+)*(?:[.+-]|$)`)
	return &Classifier{re: re}, nil
}

// Stable reports whether v is a release (not a pre-release).
func (c *Classifier) Stable(v string) bool {
	v, _, _ = strings.Cut(v, "+")
	_, pre, ok := strings.Cut(v, "-")
	if !ok {
		return true
	}
	return !c.re.MatchString(pre)
}
