package overview

import (
	"slices"
	"strings"
)

// DefaultCodeHosts are hosts whose first path segment is an owner (org or
// user), so the name prefix includes it.
var DefaultCodeHosts = []string{"github.com", "gitlab.com", "bitbucket.org"}

// NamePrefix is the family part of a component name: its first segment, or
// on a code host the first two (github.com/acme). Components of one family
// with the same provider hide their children from the top level.
func NamePrefix(name string, codeHosts []string) string {
	segs := strings.Split(name, "/")
	if len(segs) >= 2 && slices.Contains(codeHosts, segs[0]) {
		return segs[0] + "/" + segs[1]
	}
	return segs[0]
}
