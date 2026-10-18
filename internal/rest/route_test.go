package rest

import "testing"

func TestParseRoute(t *testing.T) {
	for path, want := range map[string]Route{
		"/healthz":                           {Kind: RouteHealth},
		"/v1/":                               {Kind: RouteBase},
		"/v1/components":                     {Kind: RouteComponents},
		"/v1/components/":                    {Kind: RouteComponents},
		"/v1/discovery":                      {Kind: RouteDiscovery},
		"/v1/components/acme.org/x":          {Kind: RouteComponent, Name: "acme.org/x"},
		"/v1/components/acme.org%2Fx":        {Kind: RouteComponent, Name: "acme.org/x"},
		"/v1/components/acme.org/x/versions": {Kind: RouteVersions, Name: "acme.org/x"},
		"/v1/components/acme.org/x/versions/versions":   {Kind: RouteVersions, Name: "acme.org/x/versions"},
		"/v1/components/acme.org/x/descriptors/1.0.0":   {Kind: RouteDescriptor, Name: "acme.org/x", Version: "1.0.0"},
		"/v1/components/acme.org/x/overview/1.0.0":      {Kind: RouteOverview, Name: "acme.org/x", Version: "1.0.0"},
		"/v1/components/acme.org%2Fx/overview/latest":   {Kind: RouteOverview, Name: "acme.org/x", Version: "latest"},
		"/v1/components/acme.org/x/supply-chain/1.0.0":  {Kind: RouteSupplyChain, Name: "acme.org/x", Version: "1.0.0"},
		"/v1/components/acme.org/x/supply-chain":        {Kind: RouteComponent, Name: "acme.org/x/supply-chain"},
		"/v1/components/acme.org/x/resources/1.0.0/bin": {Kind: RouteResource, Name: "acme.org/x", Version: "1.0.0", Resource: "bin"},
		"/v1/_catalog":   {Kind: RouteNotFound},
		"/v1/acme.org/x": {Kind: RouteNotFound},
		"/v2/":           {Kind: RouteNotFound},
	} {
		got, err := ParseRoute(path)
		if err != nil || got != want {
			t.Errorf("ParseRoute(%q) = %+v, %v; want %+v", path, got, err, want)
		}
	}
	for _, bad := range []string{"/v1/components/NOT VALID", "/v1/components/acme.org/x/descriptors/..", "/v1/components/acme.org/x/resources/1.0.0/%2E%2E"} {
		if _, err := ParseRoute(bad); err == nil {
			t.Errorf("ParseRoute(%q): want error", bad)
		}
	}
}

func TestParseRouteSupplyChainDocument(t *testing.T) {
	for path, want := range map[string]Route{
		"/v1/components/acme.org/x/supply-chain/1.0.0/sha256:abc":               {Kind: RouteSupplyChainDocument, Name: "acme.org/x", Version: "1.0.0", Digest: "sha256:abc"},
		"/v1/components/acme.org%2Fx/supply-chain/1.0.0/sha256%3Aabc":           {Kind: RouteSupplyChainDocument, Name: "acme.org/x", Version: "1.0.0", Digest: "sha256:abc"},
		"/v1/components/acme.org/supply-chain/supply-chain/1.0.0/sha256:abc":    {Kind: RouteSupplyChainDocument, Name: "acme.org/supply-chain", Version: "1.0.0", Digest: "sha256:abc"},
		"/v1/components/acme.org/x/supply-chain/supply-chain/1.0.0":             {Kind: RouteSupplyChain, Name: "acme.org/x/supply-chain", Version: "1.0.0"},
		"/v1/components/acme.org/supply-chain/x/supply-chain/1.0.0":             {Kind: RouteSupplyChain, Name: "acme.org/supply-chain/x", Version: "1.0.0"},
		"/v1/components/acme.org/supply-chain/x/supply-chain/latest/sha256:abc": {Kind: RouteSupplyChainDocument, Name: "acme.org/supply-chain/x", Version: "latest", Digest: "sha256:abc"},
	} {
		got, err := ParseRoute(path)
		if err != nil || got != want {
			t.Errorf("ParseRoute(%q) = %+v, %v; want %+v", path, got, err, want)
		}
	}
}

func TestParseRouteImages(t *testing.T) {
	for path, want := range map[string]Route{
		"/v1/components/acme.org/x/images/1.0.0/img":                 {Kind: RouteImage, Name: "acme.org/x", Version: "1.0.0", Resource: "img"},
		"/v1/components/acme.org%2Fx/images/1.0.0/img/sha256:abc":    {Kind: RouteImageDetails, Name: "acme.org/x", Version: "1.0.0", Resource: "img", Digest: "sha256:abc"},
		"/v1/components/acme.org%2Fx/images/latest/img/sha256%3Aabc": {Kind: RouteImageDetails, Name: "acme.org/x", Version: "latest", Resource: "img", Digest: "sha256:abc"},
		// Names with an "images" segment keep their other routes.
		"/v1/components/acme.org/x/images/overview/1.0.0":                {Kind: RouteOverview, Name: "acme.org/x/images", Version: "1.0.0"},
		"/v1/components/acme.org/x/images/descriptors/1.0.0":             {Kind: RouteDescriptor, Name: "acme.org/x/images", Version: "1.0.0"},
		"/v1/components/acme.org/x/images/supply-chain/1.0.0":            {Kind: RouteSupplyChain, Name: "acme.org/x/images", Version: "1.0.0"},
		"/v1/components/acme.org/x/images/supply-chain/1.0.0/sha256:abc": {Kind: RouteSupplyChainDocument, Name: "acme.org/x/images", Version: "1.0.0", Digest: "sha256:abc"},
		"/v1/components/acme.org/x/images/versions":                      {Kind: RouteVersions, Name: "acme.org/x/images"},
		"/v1/components/acme.org/images/resources/1.0.0/bin":             {Kind: RouteResource, Name: "acme.org/images", Version: "1.0.0", Resource: "bin"},
		"/v1/components/acme.org/images/images/1.0.0/img":                {Kind: RouteImage, Name: "acme.org/images", Version: "1.0.0", Resource: "img"},
		"/v1/components/acme.org/images":                                 {Kind: RouteComponent, Name: "acme.org/images"},
	} {
		got, err := ParseRoute(path)
		if err != nil || got != want {
			t.Errorf("ParseRoute(%q) = %+v, %v; want %+v", path, got, err, want)
		}
	}
	if _, err := ParseRoute("/v1/components/acme.org/x/images/1.0.0/img/%2E%2E"); err == nil {
		t.Error("digest .. accepted")
	}
}
