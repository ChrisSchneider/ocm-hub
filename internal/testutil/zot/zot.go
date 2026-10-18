// Package zot runs a zot OCI registry in a container for integration tests.
package zot

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"
)

// Image is the zot image used by all tests. It is pinned by tag, not digest:
// the locally cached image has no RepoDigest to verify a digest pin against
// (see the spec's Testing section).
const Image = "ghcr.io/project-zot/zot:v2.1.21"

// Registry is a running zot instance reachable over plain HTTP.
type Registry struct {
	Host     string // host:port
	Username string // empty unless started WithHtpasswd
	Password string

	container testcontainers.Container
	configDir string
}

type options struct{ user, pass string }

// Option configures Start.
type Option func(*options)

// WithHtpasswd enables basic auth with a single user.
func WithHtpasswd(user, pass string) Option {
	return func(o *options) { o.user, o.pass = user, pass }
}

// Start starts a zot container and waits until it serves /v2/.
func Start(ctx context.Context, opts ...Option) (*Registry, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	dir, err := os.MkdirTemp("", "zot-config-")
	if err != nil {
		return nil, err
	}
	httpCfg := map[string]any{"address": "0.0.0.0", "port": "5000"}
	var files []testcontainers.ContainerFile
	if o.user != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(o.pass), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		htpasswd := filepath.Join(dir, "htpasswd")
		if err := os.WriteFile(htpasswd, []byte(o.user+":"+string(hash)+"\n"), 0o644); err != nil {
			return nil, err
		}
		httpCfg["auth"] = map[string]any{"htpasswd": map[string]any{"path": "/etc/zot/htpasswd"}}
		files = append(files, testcontainers.ContainerFile{HostFilePath: htpasswd, ContainerFilePath: "/etc/zot/htpasswd", FileMode: 0o644})
	}
	cfg, err := json.Marshal(map[string]any{
		"distSpecVersion": "1.1.1",
		"storage":         map[string]any{"rootDirectory": "/var/lib/registry"},
		"http":            httpCfg,
		"log":             map[string]any{"level": "warn"},
	})
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		return nil, err
	}
	files = append(files, testcontainers.ContainerFile{HostFilePath: cfgPath, ContainerFilePath: "/etc/zot/config.json", FileMode: 0o644})

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        Image,
			ExposedPorts: []string{"5000/tcp"},
			Files:        files,
			Cmd:          []string{"serve", "/etc/zot/config.json"},
			WaitingFor: wait.ForHTTP("/v2/").WithPort("5000/tcp").
				WithStatusCodeMatcher(func(s int) bool { return s == 200 || s == 401 }),
		},
		Started: true,
	})
	if err != nil {
		return nil, fmt.Errorf("starting zot: %w", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		return nil, err
	}
	port, err := c.MappedPort(ctx, "5000/tcp")
	if err != nil {
		return nil, err
	}
	return &Registry{
		Host: net.JoinHostPort(host, port.Port()), Username: o.user, Password: o.pass,
		container: c, configDir: dir,
	}, nil
}

// Stop terminates the container.
func (r *Registry) Stop(ctx context.Context) error {
	defer os.RemoveAll(r.configDir)
	return testcontainers.TerminateContainer(r.container)
}

var shared *Registry

// Main runs a test package with one shared registry. In -short mode no
// registry is started and Shared skips. If the registry cannot start (e.g. no
// Docker), the package fails.
func Main(m *testing.M, opts ...Option) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	reg, err := Start(ctx, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zot: %v (Docker is required; use -short to skip integration tests)\n", err)
		os.Exit(1)
	}
	shared = reg
	code := m.Run()
	_ = reg.Stop(ctx)
	os.Exit(code)
}

// Shared returns the package's registry, or skips the test in -short mode.
func Shared(t testing.TB) *Registry {
	t.Helper()
	if shared == nil {
		t.Skip("integration test: skipped in -short mode")
	}
	return shared
}
