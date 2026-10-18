// Package rest serves the OCM component API over HTTP.
package rest

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	ocicompref "ocm.software/open-component-model/bindings/go/oci/compref"
)

// RouteKind identifies an endpoint.
type RouteKind int

const (
	RouteNotFound RouteKind = iota
	RouteHealth
	RouteBase
	RouteComponents
	RouteComponent
	RouteVersions
	RouteDescriptor
	RouteResource
	RouteDiscovery
	RouteOverview
	RouteSupplyChain
	RouteSupplyChainDocument
	RouteImage
	RouteImageDetails
)

func (k RouteKind) String() string {
	return [...]string{"not-found", "health", "base", "components", "component", "versions", "descriptor", "resource", "discovery", "overview", "supply-chain", "supply-chain-document", "image", "image-details"}[k]
}

// Route is a parsed request path.
type Route struct {
	Kind     RouteKind
	Name     string
	Version  string
	Resource string
	// Digest is the document digest of RouteSupplyChainDocument, or the
	// platform digest of RouteImageDetails.
	Digest string
}

// ErrNameInvalid is returned for paths whose component name (or another
// segment) is not valid.
var ErrNameInvalid = errors.New("invalid component name")

var componentName = regexp.MustCompile(ocicompref.ComponentRegex)

// ParseRoute parses an escaped URL path (r.URL.EscapedPath()). Component
// names may be sent raw or percent-encoded. Fixed suffixes are matched from
// the right, as OCI registries do.
func ParseRoute(escapedPath string) (Route, error) {
	switch escapedPath {
	case "/healthz":
		return Route{Kind: RouteHealth}, nil
	case "/v1", "/v1/":
		return Route{Kind: RouteBase}, nil
	case "/v1/components", "/v1/components/":
		return Route{Kind: RouteComponents}, nil
	case "/v1/discovery":
		return Route{Kind: RouteDiscovery}, nil
	}
	rest, ok := strings.CutPrefix(escapedPath, "/v1/components/")
	if !ok {
		return Route{Kind: RouteNotFound}, nil
	}
	seg := strings.Split(strings.TrimSuffix(rest, "/"), "/")
	for i, s := range seg {
		u, err := url.PathUnescape(s)
		if err != nil {
			return Route{}, fmt.Errorf("%w: %v", ErrNameInvalid, err)
		}
		seg[i] = u
	}
	whole := Route{Kind: RouteComponent, Name: strings.Join(seg, "/")}
	if r, nameEnd, matched := keywordRoute(seg); matched {
		r.Name = strings.Join(seg[:nameEnd], "/")
		// An unsafe version or resource segment is an error, never a
		// fallback to the overview.
		if err := validateSegment("version", r.Version); err != nil {
			return Route{}, err
		}
		if err := validateSegment("resource", r.Resource); err != nil {
			return Route{}, err
		}
		if err := validateSegment("digest", r.Digest); err != nil {
			return Route{}, err
		}
		if componentName.MatchString(r.Name) {
			return r, nil
		}
		// A keyword match with an invalid name falls through to the overview.
	}
	if componentName.MatchString(whole.Name) {
		return whole, nil
	}
	return Route{}, fmt.Errorf("%w: %q", ErrNameInvalid, whole.Name)
}

// validateSegment checks if a version or resource segment is safe after
// percent-decoding. It rejects empty, ".", "..", segments with "/" or NUL.
func validateSegment(kind, s string) error {
	if s == "" {
		return nil // Empty is allowed for optional segments
	}
	if s == "." || s == ".." {
		return fmt.Errorf("%w: %s=%q", ErrNameInvalid, kind, s)
	}
	if strings.Contains(s, "/") {
		return fmt.Errorf("%w: %s=%q", ErrNameInvalid, kind, s)
	}
	if strings.ContainsRune(s, '\x00') {
		return fmt.Errorf("%w: %s=%q", ErrNameInvalid, kind, s)
	}
	return nil
}

func keywordRoute(seg []string) (Route, int, bool) {
	n := len(seg)
	switch {
	case n >= 2 && seg[n-1] == "versions":
		return Route{Kind: RouteVersions}, n - 1, true
	case n >= 3 && seg[n-2] == "descriptors" && seg[n-1] != "":
		return Route{Kind: RouteDescriptor, Version: seg[n-1]}, n - 2, true
	case n >= 3 && seg[n-2] == "overview" && seg[n-1] != "":
		return Route{Kind: RouteOverview, Version: seg[n-1]}, n - 2, true
	// Before the set route: .../supply-chain/{version}/{digest}. Only a
	// digest-like last segment (with ':') matches, so the set route of a
	// component named .../supply-chain stays reachable.
	case n >= 4 && seg[n-3] == "supply-chain" && seg[n-2] != "" && strings.Contains(seg[n-1], ":"):
		return Route{Kind: RouteSupplyChainDocument, Version: seg[n-2], Digest: seg[n-1]}, n - 3, true
	case n >= 3 && seg[n-2] == "supply-chain" && seg[n-1] != "":
		return Route{Kind: RouteSupplyChain, Version: seg[n-1]}, n - 2, true
	// After the routes above, so a name ending in "images" keeps its overview,
	// descriptors, versions and supply-chain routes; before resources.
	// .../images/{version}/{resource}/{digest} matches only a digest-like last
	// segment, so the image route of a resource in a component named
	// .../images stays reachable.
	case n >= 5 && seg[n-4] == "images" && seg[n-3] != "" && seg[n-2] != "" && strings.Contains(seg[n-1], ":"):
		return Route{Kind: RouteImageDetails, Version: seg[n-3], Resource: seg[n-2], Digest: seg[n-1]}, n - 4, true
	case n >= 4 && seg[n-3] == "images" && seg[n-2] != "" && seg[n-1] != "":
		return Route{Kind: RouteImage, Version: seg[n-2], Resource: seg[n-1]}, n - 3, true
	case n >= 4 && seg[n-3] == "resources" && seg[n-2] != "" && seg[n-1] != "":
		return Route{Kind: RouteResource, Version: seg[n-2], Resource: seg[n-1]}, n - 3, true
	}
	return Route{}, 0, false
}
