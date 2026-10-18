package document

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ErrNotAllowed means a URL matches no pattern of the allow list.
var ErrNotAllowed = errors.New("document URL is not on the allow list")

// allowRule is one compiled allow pattern: host[:port] + path, the host
// lowercased.
type allowRule struct {
	pattern string
	http    bool // the pattern started with http://: http is allowed too
}

type allowList []allowRule

// ValidateAllow reports the first pattern compileAllow rejects.
func ValidateAllow(patterns []string) error {
	_, err := compileAllow(patterns)
	return err
}

// compileAllow parses patterns such as "example.com/**/*.vex",
// "*.example.com/vex/*" or "http://intranet.corp/*.json". A pattern
// without a scheme (or with https://) allows https only; http:// allows
// http and https.
func compileAllow(patterns []string) (allowList, error) {
	l := make(allowList, 0, len(patterns))
	for i, p := range patterns {
		r := allowRule{pattern: p}
		if scheme, rest, ok := strings.Cut(p, "://"); ok {
			switch strings.ToLower(scheme) {
			case "http":
				r.http = true
			case "https":
			default:
				return nil, fmt.Errorf("allow[%d]: %q: scheme must be http or https", i, p)
			}
			r.pattern = rest
		}
		host, rest, ok := strings.Cut(r.pattern, "/")
		if host == "" || !ok {
			return nil, fmt.Errorf("allow[%d]: %q: a host and a path are required, such as example.com/**", i, p)
		}
		r.pattern = strings.ToLower(host) + "/" + rest
		if !doublestar.ValidatePattern(r.pattern) {
			return nil, fmt.Errorf("allow[%d]: %q is not a valid pattern", i, p)
		}
		l = append(l, r)
	}
	return l, nil
}

// allows reports whether some rule matches u's host[:port] and path as sent
// (query and fragment are ignored). https only, unless the rule allows http.
// A path with dot segments (also %2e forms), an encoded / or \ (%2F, %5C)
// or a backslash is refused: a server may resolve it outside the match.
func (l allowList) allows(u *url.URL) bool {
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return false
	}
	if !plainPath(u) {
		return false
	}
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	target := strings.ToLower(u.Host) + p
	for _, r := range l {
		if scheme == "http" && !r.http {
			continue
		}
		if ok, _ := doublestar.Match(r.pattern, target); ok {
			return true
		}
	}
	return false
}

// plainPath reports whether u's path has no dot segments, encoded slashes or
// backslashes. u.Path is decoded, so %2e%2e counts as "..".
func plainPath(u *url.URL) bool {
	raw := strings.ToLower(u.EscapedPath())
	if strings.Contains(raw, "%2f") || strings.Contains(raw, "%5c") || strings.Contains(u.Path, `\`) {
		return false
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
