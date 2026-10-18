// Command ocm-server serves OCM component descriptors and resources over HTTP.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/config"
	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/observability"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/overview"
	"chrisschneider.dev/delivery-bundler/internal/rest"
	"chrisschneider.dev/delivery-bundler/internal/webui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Restore default signal handling once shutdown starts, so a second
	// signal terminates immediately instead of waiting for the grace period.
	context.AfterFunc(ctx, stop)
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ocm-server:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ocm-server", flag.ContinueOnError)
	configPath := fs.String("config", "", "server config file (required)")
	listen := fs.String("listen", "", "listen address, overrides the config file")
	defaultLevel := os.Getenv("LOG_LEVEL")
	if defaultLevel == "" {
		defaultLevel = "info"
	}
	logLevel := fs.String("log-level", defaultLevel, "log level: debug, info, warn, error")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return errors.New("--config is required")
	}
	level, err := observability.ParseLevel(*logLevel)
	if err != nil {
		return fmt.Errorf("--log-level: %w", err)
	}
	logger := observability.NewLogger(stdout, level)
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *listen != "" {
		cfg.Listen = *listen
	}

	shutdownOTel, err := observability.Setup(ctx)
	if err != nil {
		return fmt.Errorf("opentelemetry: %w", err)
	}
	defer func() {
		// Bounded: an unreachable collector must not stall process exit.
		otelCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if err := shutdownOTel(otelCtx); err != nil {
			logger.Warn("opentelemetry shutdown", slog.String("error", err.Error()))
		}
	}()

	ocmCfg, err := ocm.LoadOCMConfig(cfg.OCMConfig)
	if err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp("", "ocm-server-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	// The bindings live as long as the process; their base context must not
	// be the signal context, which is cancelled when shutdown begins while
	// in-flight requests still use them.
	longLived := context.WithoutCancel(ctx)
	var bindingOpts []ocm.BindingsOption
	dir, err := cacheDir(cfg.Cache, logger)
	if err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	cfg.Cache.Dir = dir
	bindingOpts = append(bindingOpts, ocm.WithBlobCache(ocm.BlobCacheOptions(
		cfg.Cache.Dir, cfg.Cache.Blobs.MaxEntries, int64(cfg.Cache.Blobs.MaxBlobSize), cfg.Cache.Blobs.RemotePolicy)))
	bindings, err := ocm.NewBindings(longLived, ocmCfg, tempDir, bindingOpts...)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		_ = bindings.Close(closeCtx)
	}()
	resolver, err := ocm.NewResolver(longLived, bindings)
	if err != nil {
		return err
	}
	svc := ocm.NewService(bindings, resolver, time.Duration(cfg.Timeouts.Upstream))
	idx := index.NewHandle(cfg.DB.Path)
	defer idx.Close()
	if _, release, err := idx.Acquire(); err != nil {
		logger.Warn("index not available yet; run cmd/sync", slog.String("db", cfg.DB.Path), slog.String("error", err.Error()))
	} else {
		release()
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	var ui http.Handler
	if cfg.UI.Enabled {
		ui = webui.Handler()
	}
	srv := newServer(observability.Middleware(routes(rest.NewHandler(svc, idx, rest.WithOverviewOptions(overview.Options{KubernetesVersions: cfg.Catalog.KubernetesVersions}),
		rest.WithRegistry(bindings.RepositoryOpener()), rest.WithFetcher((&document.Fetcher{Timeout: time.Duration(cfg.Timeouts.Upstream), Allow: cfg.Documents.Allow}).Fetch)), ui), logger))
	logger.Info("listening", slog.String("addr", ln.Addr().String()))
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	return shutdown(srv, time.Duration(cfg.Timeouts.ShutdownGrace))
}

// routes sends /healthz and /v1… to the API and everything else to the UI;
// ui nil (disabled) sends everything to the API.
func routes(api, ui http.Handler) http.Handler {
	if ui == nil {
		return api
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/healthz" || p == "/v1" || strings.HasPrefix(p, "/v1/") {
			api.ServeHTTP(w, r)
			return
		}
		ui.ServeHTTP(w, r)
	})
}

const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
	cleanupTimeout    = 5 * time.Second // OTel flush, plugin shutdown
)

// newServer builds the HTTP server. There is deliberately no WriteTimeout:
// resource downloads may legitimately take minutes.
func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// shutdown stops accepting connections and lets in-flight requests finish
// within grace; after that the remaining connections are closed and an error
// reports the expired grace period.
func shutdown(srv *http.Server, grace time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	err := srv.Shutdown(ctx)
	if err == nil {
		return nil
	}
	closeErr := srv.Close()
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(fmt.Errorf("shutdown grace period of %s expired, closed remaining connections", grace), closeErr)
	}
	return errors.Join(fmt.Errorf("shutdown: %w", err), closeErr)
}

// cacheDir ensures the cache directory exists with mode 0700. A defaulted
// directory that cannot be used (read-only HOME, no user cache dir) falls back
// to a private random temp directory, which does not survive restarts; an
// explicitly configured directory that is unusable is an error.
func cacheDir(c config.Cache, logger *slog.Logger) (string, error) {
	var err error
	if c.Dir == "" {
		err = errors.New("no user cache directory")
	} else if err = os.MkdirAll(c.Dir, 0o700); err == nil {
		return c.Dir, nil
	}
	if !c.DirDefaulted {
		return "", err
	}
	dir, terr := os.MkdirTemp("", "ocm-server-cache-")
	if terr != nil {
		return "", fmt.Errorf("%w; temp fallback: %v", err, terr)
	}
	logger.Warn("default cache directory unusable; using a temporary one that will not survive restarts",
		slog.String("dir", dir), slog.String("error", err.Error()))
	return dir, nil
}
