// Package config loads the server's own config file. Everything OCM-related
// (resolvers, credentials, HTTP settings) lives in the referenced .ocmconfig.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/overview"
	"chrisschneider.dev/delivery-bundler/internal/version"
	"github.com/Masterminds/semver/v3"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

// Duration is a time.Duration written as a string such as "30s".
type Duration time.Duration

// UnmarshalJSON parses a duration string.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"30s\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// ByteSize is a size written as a Kubernetes-style quantity such as "4Mi"
// (a plain number of bytes is accepted too).
type ByteSize int64

// UnmarshalJSON parses a quantity string or a number.
func (b *ByteSize) UnmarshalJSON(data []byte) error {
	var n int64
	if json.Unmarshal(data, &n) == nil {
		*b = ByteSize(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("size must be a quantity like \"4Mi\": %w", err)
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return fmt.Errorf("size %q: %w", s, err)
	}
	*b = ByteSize(q.Value())
	return nil
}

// BlobCache configures the OCM bindings' OCI blob cache.
type BlobCache struct {
	MaxEntries   int      `json:"maxEntries"`
	MaxBlobSize  ByteSize `json:"maxBlobSize"`
	RemotePolicy string   `json:"remotePolicy"` // IfNotPresent | Always
}

// Timeouts bounds upstream metadata calls and graceful shutdown.
type Timeouts struct {
	Upstream      Duration `json:"upstream"`
	ShutdownGrace Duration `json:"shutdownGrace"`
}

// UI configures the embedded web UI.
type UI struct {
	Enabled bool `json:"enabled"`
}

// Cache configures the OCM bindings' on-disk blob cache (descriptors and
// small blobs). It is always on.
type Cache struct {
	Dir string `json:"dir"`
	// DirDefaulted is true when Dir was not configured. Dir may then be empty
	// (no user cache dir); the caller falls back to a private temp dir.
	DirDefaulted bool      `json:"-"`
	Blobs        BlobCache `json:"blobs"`
}

// DB locates the DuckDB index file.
type DB struct {
	Path string `json:"path"` // relative to the config file
}

// Versions configures version classification.
type Versions struct {
	PrereleaseMarkers []string `json:"prereleaseMarkers"`
}

// Catalog configures how labels and names become catalog data.
type Catalog struct {
	KubernetesVersions []string `json:"kubernetesVersions"` // known minors for the compatibility label
	CodeHosts          []string `json:"codeHosts"`          // hosts whose first segment is an owner (name prefix)
}

// Documents configures which label-linked documents the hub fetches.
type Documents struct {
	// Allow lists URL patterns (host[:port]/path, doublestar globs); https
	// only unless a pattern starts with http://. Empty: nothing is fetched.
	Allow []string `json:"allow"`
}

// ConnectorGitHubV1 lists an organization's or user's OCM components via the GitHub Packages API.
const ConnectorGitHubV1 = "github/v1"

// Discovery is one connector entry of `discovery:`.
type Discovery struct {
	Connector  string   `json:"connector"`
	Org        string   `json:"org,omitempty"`
	User       string   `json:"user,omitempty"`
	Enterprise string   `json:"enterprise,omitempty"`
	Visibility []string `json:"visibility,omitempty"` // public, internal, private; empty: public only
	Name       string   `json:"name,omitempty"`       // title override; not part of ID or Key
}

// discoveryIdentity is the part of a Discovery that ID hashes. Its fields and
// tags must stay as they are: they fix the IDs of existing entries.
type discoveryIdentity struct {
	Connector  string   `json:"connector"`
	Org        string   `json:"org,omitempty"`
	User       string   `json:"user,omitempty"`
	Enterprise string   `json:"enterprise,omitempty"`
	Visibility []string `json:"visibility,omitempty"`
}

// ID is a stable identity of the entry: sha256 (hex) of the canonical JSON of
// its source fields (Name is left out).
func (d Discovery) ID() string {
	vis := slices.Sorted(slices.Values(d.Visibility))
	b, _ := json.Marshal(discoveryIdentity{Connector: d.Connector, Org: d.Org, User: d.User, Enterprise: d.Enterprise, Visibility: vis}) // struct field order is fixed: canonical
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Key is the readable identity of the entry used by API and UI, e.g.
// "github:org:open-component-model". GitHub logins are case-insensitive.
func (d Discovery) Key() string {
	// The "github:" prefix holds because only github/v1 is accepted; derive it from Connector when another type arrives.
	kind, owner := "org", d.Org
	switch {
	case d.User != "":
		kind, owner = "user", d.User
	case d.Enterprise != "":
		kind, owner = "enterprise", d.Enterprise
	}
	return "github:" + kind + ":" + strings.ToLower(owner)
}

// Config is the hub config file.
type Config struct {
	Listen    string      `json:"listen"`
	OCMConfig string      `json:"ocmconfig"`
	DB        DB          `json:"db"`
	Timeouts  Timeouts    `json:"timeouts"`
	Cache     Cache       `json:"cache"`
	Versions  Versions    `json:"versions"`
	Catalog   Catalog     `json:"catalog"`
	Documents Documents   `json:"documents"`
	Discovery []Discovery `json:"discovery"`
	UI        UI          `json:"ui"`
}

// Default returns the defaults applied before the file is read.
func Default() Config {
	return Config{
		Listen:   ":8080",
		DB:       DB{Path: filepath.Join("data", "hub.duckdb")},
		Timeouts: Timeouts{Upstream: Duration(30 * time.Second), ShutdownGrace: Duration(30 * time.Second)},
		Cache:    Cache{Blobs: BlobCache{MaxEntries: 10000, MaxBlobSize: 4 << 20, RemotePolicy: "IfNotPresent"}},
		Versions: Versions{PrereleaseMarkers: slices.Clone(version.DefaultMarkers)},
		Catalog:  Catalog{KubernetesVersions: slices.Clone(overview.DefaultKubernetesVersions), CodeHosts: slices.Clone(overview.DefaultCodeHosts)},
		UI:       UI{Enabled: true},
	}
}

// Load reads and validates the config file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading config: %w", err)
	}
	cfg := Default()
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config %s: %w", path, err)
	}
	base := filepath.Dir(path)
	if cfg.OCMConfig == "" {
		return Config{}, errors.New("config: ocmconfig is required")
	}
	cfg.OCMConfig = relativeTo(base, cfg.OCMConfig)
	cfg.DB.Path = relativeTo(base, cfg.DB.Path)
	if cfg.Timeouts.Upstream <= 0 || cfg.Timeouts.ShutdownGrace <= 0 {
		return Config{}, errors.New("config: timeouts must be positive")
	}
	if err := resolveCache(&cfg.Cache, base); err != nil {
		return Config{}, err
	}
	if _, err := version.NewClassifier(cfg.Versions.PrereleaseMarkers); err != nil {
		return Config{}, fmt.Errorf("config: versions: %w", err)
	}
	if err := validateCatalog(cfg.Catalog); err != nil {
		return Config{}, err
	}
	if err := validateDiscovery(cfg.Discovery); err != nil {
		return Config{}, err
	}
	if err := document.ValidateAllow(cfg.Documents.Allow); err != nil {
		return Config{}, fmt.Errorf("config: documents.%w", err)
	}
	return cfg, nil
}

func validateCatalog(c Catalog) error {
	if len(c.KubernetesVersions) == 0 {
		return errors.New("config: catalog.kubernetesVersions must not be empty")
	}
	for _, v := range c.KubernetesVersions {
		if _, err := semver.NewVersion(v); err != nil {
			return fmt.Errorf("config: catalog.kubernetesVersions: %q is not a version", v)
		}
	}
	for _, h := range c.CodeHosts {
		if strings.TrimSpace(h) == "" {
			return errors.New("config: catalog.codeHosts must not contain empty entries")
		}
	}
	return nil
}

func relativeTo(base, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// resolveCache fills the default directory and validates the blob cache.
func resolveCache(c *Cache, base string) error {
	if c.Dir == "" {
		c.DirDefaulted = true
		if dir, err := os.UserCacheDir(); err == nil {
			c.Dir = filepath.Join(dir, "ocm-server")
		}
	} else {
		c.Dir = relativeTo(base, c.Dir)
	}
	switch {
	case c.Blobs.MaxEntries <= 0:
		return errors.New("config: cache.blobs.maxEntries must be positive")
	case c.Blobs.MaxBlobSize <= 0:
		return errors.New("config: cache.blobs.maxBlobSize must be positive")
	case c.Blobs.RemotePolicy != "IfNotPresent" && c.Blobs.RemotePolicy != "Always":
		return fmt.Errorf("config: cache.blobs.remotePolicy must be IfNotPresent or Always, got %q", c.Blobs.RemotePolicy)
	}
	return nil
}

func validateDiscovery(ds []Discovery) error {
	seen := map[string]bool{}
	for i, d := range ds {
		switch {
		case d.Connector != ConnectorGitHubV1:
			return fmt.Errorf("config: discovery[%d]: connector %q is not supported (only %s)", i, d.Connector, ConnectorGitHubV1)
		case owners(d) != 1:
			return fmt.Errorf("config: discovery[%d]: exactly one of org, user or enterprise is required for %s", i, ConnectorGitHubV1)
		case seen[d.Key()]:
			return fmt.Errorf("config: discovery[%d]: duplicate source %s", i, d.Key())
		}
		vis := map[string]bool{}
		for _, v := range d.Visibility {
			if v != "public" && v != "internal" && v != "private" {
				return fmt.Errorf("config: discovery[%d]: visibility %q must be public, internal or private", i, v)
			}
			if vis[v] {
				return fmt.Errorf("config: discovery[%d]: visibility %q listed twice", i, v)
			}
			vis[v] = true
		}
		seen[d.Key()] = true
	}
	return nil
}

// owners counts the owner fields set on a discovery entry.
func owners(d Discovery) int {
	n := 0
	for _, s := range []string{d.Org, d.User, d.Enterprise} {
		if s != "" {
			n++
		}
	}
	return n
}
