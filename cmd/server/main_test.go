package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/config"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
)

func writeServerConfig(t *testing.T, ocmconfig string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "server.yaml")
	content := "listen: 127.0.0.1:0\nocmconfig: " + ocmconfig + "\ncache:\n  dir: " + filepath.Join(dir, "cache") + "\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunFailsFast(t *testing.T) {
	ctx := t.Context()
	if err := run(ctx, nil, io.Discard); err == nil || !strings.Contains(err.Error(), "--config") {
		t.Fatalf("no config: %v", err)
	}
	ctf := filepath.Join(t.TempDir(), ".ocmconfig")
	os.WriteFile(ctf, []byte(`type: generic.config.ocm.software/v1
configurations:
- type: resolvers.config.ocm.software/v1alpha1
  resolvers:
  - repository: {type: CommonTransportFormat/v1, filePath: /tmp/a}
`), 0o600)
	if err := run(ctx, []string{"--config", writeServerConfig(t, ctf)}, io.Discard); err == nil || !strings.Contains(err.Error(), "not supported yet") {
		t.Fatalf("ctf: %v", err)
	}
}

// TestRunServesAndShutsDown starts the server against an unreachable
// registry: startup must not contact registries.
func TestRunServesAndShutsDown(t *testing.T) {
	ocmconfig := fixtures.WriteOCMConfig(t, []fixtures.Resolver{{Host: "127.0.0.1:1", SubPath: "none"}}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pr, pw := io.Pipe()
	cfgPath := writeServerConfig(t, ocmconfig)
	done := make(chan error, 1)
	go func() {
		err := run(ctx, []string{"--config", cfgPath}, pw)
		pw.Close()
		done <- err
	}()

	addrCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		sent := false
		for sc.Scan() { // keeps draining logs after the address is found
			if sent {
				continue
			}
			var line map[string]any
			if json.Unmarshal(sc.Bytes(), &line) == nil && line["msg"] == "listening" {
				addr, _ := line["addr"].(string)
				addrCh <- addr
				sent = true
			}
		}
	}()

	var addr string
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("run returned before listening: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for listening log line")
	}
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", resp, err)
	}
	resp.Body.Close()
	if resp.Header.Get("X-Request-Id") == "" {
		t.Fatal("middleware not installed")
	}
	// No index file yet: index routes answer 503, the server still runs.
	for _, p := range []string{"/v1/components", "/v1/components/acme.org%2Fx/versions"} {
		resp, err := http.Get("http://" + addr + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s without index: %d, want 503", p, resp.StatusCode)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfgPath), "cache")); err != nil {
		t.Fatalf("blob cache dir not initialised: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func TestRunFailsOnUnusableCacheDir(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ocmconfig := fixtures.WriteOCMConfig(t, []fixtures.Resolver{{Host: "127.0.0.1:1", SubPath: "none"}}, nil)
	p := filepath.Join(dir, "server.yaml")
	content := "listen: 127.0.0.1:0\nocmconfig: " + ocmconfig + "\ncache:\n  dir: " + blocker + "\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"--config", p}, io.Discard); err == nil || !strings.Contains(err.Error(), "cache") {
		t.Fatalf("err = %v, want cache error", err)
	}
}

func TestCacheDirDefaultedFallsBackToTemp(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{"unusable": filepath.Join(blocker, "sub"), "empty": ""} {
		t.Run(name, func(t *testing.T) {
			got, err := cacheDir(config.Cache{Dir: dir, DirDefaulted: true}, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(got) })
			fi, err := os.Stat(got)
			if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 || got == dir {
				t.Fatalf("dir %q: %v, %v", got, fi, err)
			}
		})
	}
}

func TestCacheDirExplicitUnusableFails(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cacheDir(config.Cache{Dir: filepath.Join(blocker, "sub")}, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("want error")
	}
	dir := filepath.Join(t.TempDir(), "ok")
	if got, err := cacheDir(config.Cache{Dir: dir}, slog.New(slog.DiscardHandler)); err != nil || got != dir {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestNewServerTimeouts(t *testing.T) {
	srv := newServer(http.NotFoundHandler())
	if srv.IdleTimeout != 120*time.Second || srv.ReadHeaderTimeout != 10*time.Second || srv.WriteTimeout != 0 || srv.Handler == nil {
		t.Fatalf("server = idle %v readHeader %v write %v", srv.IdleTimeout, srv.ReadHeaderTimeout, srv.WriteTimeout)
	}
}

// startServer serves h on a loopback listener and returns the server and its URL.
func startServer(t *testing.T, h http.Handler) (*http.Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := newServer(h)
	go srv.Serve(ln)
	return srv, "http://" + ln.Addr().String()
}

func TestShutdownIdle(t *testing.T) {
	srv, _ := startServer(t, http.NotFoundHandler())
	if err := shutdown(srv, time.Second); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

// TestShutdownGraceExpired: a request outliving the grace period is cut off
// and the expiry is reported as an error.
func TestShutdownGraceExpired(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	srv, url := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	clientErr := make(chan error, 1)
	go func() {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
		}
		clientErr <- err
	}()
	<-started
	err := shutdown(srv, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "grace period") {
		t.Fatalf("shutdown err = %v, want grace period expiry", err)
	}
	select {
	case <-clientErr: // connection was closed
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request was not cut off after the grace period")
	}
}

func TestRoutes(t *testing.T) {
	tag := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, name) })
	}
	h := routes(tag("api"), tag("ui"))
	for path, want := range map[string]string{
		"/healthz": "api", "/v1": "api", "/v1/": "api", "/v1/components": "api", "/v1/components/acme.org%2Fx/versions": "api",
		"/": "ui", "/components/acme.org/x": "ui", "/assets/a.js": "ui", "/v1x": "ui",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if got := rec.Body.String(); got != want {
			t.Errorf("%s → %s, want %s", path, got, want)
		}
	}
	rec := httptest.NewRecorder()
	routes(tag("api"), nil).ServeHTTP(rec, httptest.NewRequest("GET", "/components/x", nil))
	if rec.Body.String() != "api" {
		t.Errorf("ui disabled: %s", rec.Body.String())
	}
}
