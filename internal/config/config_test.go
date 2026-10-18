package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/overview"
)

// orgOnlyID is the ID of Discovery{Connector: "github/v1", Org: "acme"} before
// the User field existed; it must stay stable.
const orgOnlyID = "aa6d645ff3eafae82d4c6a070434f8b2a856b1c5b0374eabf7a9cb4d2f99238b"

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hub.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaults(t *testing.T) {
	p := write(t, "ocmconfig: .ocmconfig\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(p)
	if cfg.DB.Path != filepath.Join(dir, "data", "hub.duckdb") {
		t.Errorf("db.path = %q", cfg.DB.Path)
	}
	if cfg.OCMConfig != filepath.Join(dir, ".ocmconfig") {
		t.Errorf("ocmconfig = %q", cfg.OCMConfig)
	}
	if len(cfg.Versions.PrereleaseMarkers) != 8 || cfg.Versions.PrereleaseMarkers[0] != "alpha" {
		t.Errorf("markers = %v", cfg.Versions.PrereleaseMarkers)
	}
	if cfg.Cache.Blobs.RemotePolicy != "IfNotPresent" || time.Duration(cfg.Timeouts.Upstream) != 30*time.Second {
		t.Errorf("defaults = %+v", cfg)
	}
	if len(cfg.Discovery) != 0 || !cfg.UI.Enabled {
		t.Errorf("discovery/ui = %+v %+v", cfg.Discovery, cfg.UI)
	}
}

func TestLoadDiscoveryAndDB(t *testing.T) {
	p := write(t, `ocmconfig: /abs/.ocmconfig
db:
  path: /var/hub/x.duckdb
versions:
  prereleaseMarkers: [wip]
discovery:
  - connector: github/v1
    org: open-component-model
  - connector: github/v1
    org: other
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Path != "/var/hub/x.duckdb" || cfg.Versions.PrereleaseMarkers[0] != "wip" {
		t.Errorf("cfg = %+v", cfg)
	}
	if len(cfg.Discovery) != 2 || cfg.Discovery[0].Org != "open-component-model" {
		t.Fatalf("discovery = %+v", cfg.Discovery)
	}
	if a, b := cfg.Discovery[0].ID(), cfg.Discovery[1].ID(); a == b || len(a) != 64 {
		t.Errorf("ids %q %q", a, b)
	}
	if cfg.Discovery[0].ID() != (Discovery{Connector: "github/v1", Org: "open-component-model"}).ID() {
		t.Error("ID must depend only on the entry's content")
	}
}

func TestLoadErrors(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"no ocmconfig":       {"listen: :1\n", "ocmconfig is required"},
		"unknown field":      {"ocmconfig: x\nbogus: {}\n", "unknown field"},
		"unknown connector":  {"ocmconfig: x\ndiscovery: [{connector: gcr/v1}]\n", `connector "gcr/v1" is not supported`},
		"github without org": {"ocmconfig: x\ndiscovery: [{connector: github/v1}]\n", "exactly one of org, user or enterprise"},
		"org and enterprise": {"ocmconfig: x\ndiscovery: [{connector: github/v1, org: a, enterprise: b}]\n", "exactly one of org, user or enterprise"},
		"duplicate":          {"ocmconfig: x\ndiscovery: [{connector: github/v1, org: a}, {connector: github/v1, org: a}]\n", "duplicate"},
		"bad marker":         {"ocmconfig: x\nversions: {prereleaseMarkers: [\"r c\"]}\n", "pre-release marker"},
		"bad policy":         {"ocmconfig: x\ncache: {blobs: {remotePolicy: Never}}\n", "remotePolicy"},
		"bad timeout":        {"ocmconfig: x\ntimeouts: {upstream: 0s}\n", "timeouts must be positive"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCatalogDefaults(t *testing.T) {
	cfg, err := Load(write(t, "ocmconfig: .ocmconfig\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Catalog.KubernetesVersions, overview.DefaultKubernetesVersions) || !slices.Equal(cfg.Catalog.CodeHosts, overview.DefaultCodeHosts) {
		t.Fatalf("catalog = %+v", cfg.Catalog)
	}
}

func TestCatalogValidation(t *testing.T) {
	for name, y := range map[string]string{
		"bad k8s version": "catalog:\n  kubernetesVersions: [\"1.x\"]\n",
		"empty k8s list":  "catalog:\n  kubernetesVersions: []\n",
		"empty host":      "catalog:\n  codeHosts: [\"\"]\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, "ocmconfig: .ocmconfig\n"+y)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestDiscoveryOrgXorUser(t *testing.T) {
	ok := "discovery:\n  - connector: github/v1\n    user: jane-doe\n"
	cfg, err := Load(write(t, "ocmconfig: .ocmconfig\n"+ok))
	if err != nil || cfg.Discovery[0].User != "jane-doe" {
		t.Fatalf("user entry: %+v %v", cfg.Discovery, err)
	}
	for name, y := range map[string]string{
		"both":    "discovery:\n  - connector: github/v1\n    org: acme\n    user: jane\n",
		"neither": "discovery:\n  - connector: github/v1\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, "ocmconfig: .ocmconfig\n"+y)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	// adding the User field must not change the ID of existing org entries
	if got := (Discovery{Connector: "github/v1", Org: "acme"}).ID(); got != orgOnlyID {
		t.Fatalf("org entry ID changed: %s", got)
	}
}

func TestDiscoveryKeyAndName(t *testing.T) {
	y := "discovery:\n  - connector: github/v1\n    org: Open-Component-Model\n    name: OCM\n  - connector: github/v1\n    user: jane-doe\n"
	cfg, err := Load(write(t, "ocmconfig: .ocmconfig\n"+y))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Discovery[0].Key(); got != "github:org:open-component-model" {
		t.Fatalf("org key = %q", got)
	}
	if cfg.Discovery[0].Name != "OCM" {
		t.Fatalf("name = %q", cfg.Discovery[0].Name)
	}
	if got := cfg.Discovery[1].Key(); got != "github:user:jane-doe" {
		t.Fatalf("user key = %q", got)
	}
	// a title override must not change the ID (renaming must not force a full sync)
	if got := (Discovery{Connector: "github/v1", Org: "acme", Name: "Acme"}).ID(); got != orgOnlyID {
		t.Fatalf("name changed the ID: %s", got)
	}
}

func TestDiscoveryDuplicateKey(t *testing.T) {
	y := "discovery:\n  - connector: github/v1\n    org: acme\n  - connector: github/v1\n    org: ACME\n    name: other title\n"
	_, err := Load(write(t, "ocmconfig: .ocmconfig\n"+y))
	if err == nil || !strings.Contains(err.Error(), "duplicate source github:org:acme") {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoveryEnterpriseAndVisibility(t *testing.T) {
	y := "discovery:\n  - connector: github/v1\n    enterprise: Acme-Enterprise\n    visibility: [public, internal]\n"
	cfg, err := Load(write(t, "ocmconfig: .ocmconfig\n"+y))
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.Discovery[0]
	if d.Key() != "github:enterprise:acme-enterprise" || !slices.Equal(d.Visibility, []string{"public", "internal"}) {
		t.Fatalf("entry = %+v key %q", d, d.Key())
	}
	// visibility is part of the ID (order-insensitive), not of the key
	a := Discovery{Connector: "github/v1", Org: "acme", Visibility: []string{"public", "internal"}}
	b := Discovery{Connector: "github/v1", Org: "acme", Visibility: []string{"internal", "public"}}
	if a.ID() != b.ID() || a.ID() == orgOnlyID || a.Key() != "github:org:acme" {
		t.Fatalf("ids %s %s", a.ID(), b.ID())
	}
	if got := (Discovery{Connector: "github/v1", Org: "acme"}).ID(); got != orgOnlyID {
		t.Fatalf("unset visibility changed the ID: %s", got)
	}
}

func TestDiscoveryOwnerAndVisibilityErrors(t *testing.T) {
	for name, y := range map[string]string{
		"org and enterprise": "discovery:\n  - connector: github/v1\n    org: acme\n    enterprise: acme-enterprise\n",
		"bad visibility":     "discovery:\n  - connector: github/v1\n    org: acme\n    visibility: [secret]\n",
		"dup visibility":     "discovery:\n  - connector: github/v1\n    org: acme\n    visibility: [public, public]\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, "ocmconfig: .ocmconfig\n"+y)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestDocumentsAllow(t *testing.T) {
	y := "documents:\n  allow:\n    - example.com/**/*.vex\n    - \"*.example.com/**/*.vex\"\n    - http://security.internal.corp/vex/*.json\n"
	cfg, err := Load(write(t, "ocmconfig: .ocmconfig\n"+y))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Documents.Allow) != 3 || cfg.Documents.Allow[2] != "http://security.internal.corp/vex/*.json" {
		t.Fatalf("allow = %v", cfg.Documents.Allow)
	}
	for name, tc := range map[string]struct{ pattern, want string }{
		"bad glob":   {"example.com/[a", `documents.allow[0]: "example.com/[a"`},
		"bad scheme": {"ftp://example.com/x", `documents.allow[0]: "ftp://example.com/x"`},
		"empty":      {"", "documents.allow[0]"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, "ocmconfig: .ocmconfig\ndocuments:\n  allow: [\""+tc.pattern+"\"]\n"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
