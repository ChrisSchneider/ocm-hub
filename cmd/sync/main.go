// Command sync fills the hub's DuckDB index from the discovery connectors
// in hub.yaml. It runs once; schedule it externally (cron, CI).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/config"
	"chrisschneider.dev/delivery-bundler/internal/connector"
	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/indexsync"
	"chrisschneider.dev/delivery-bundler/internal/observability"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	failed, err := run(ctx, os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "sync:", err)
		os.Exit(1)
	}
	if failed {
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) (bool, error) {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	configPath := fs.String("config", "", "hub config file (required)")
	full := fs.Bool("full", false, "full sync of every connector (also detects deletions)")
	logLevel := fs.String("log-level", "info", "log level: debug, info, warn, error")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	if *configPath == "" {
		return false, errors.New("--config is required")
	}
	level, err := observability.ParseLevel(*logLevel)
	if err != nil {
		return false, err
	}
	logger := observability.NewLogger(os.Stderr, level)
	slog.SetDefault(logger)
	cfg, err := config.Load(*configPath)
	if err != nil {
		return false, err
	}
	ocmCfg, err := ocm.LoadOCMConfig(cfg.OCMConfig)
	if err != nil {
		return false, err
	}
	tempDir, err := os.MkdirTemp("", "ocm-sync-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tempDir)
	var bopts []ocm.BindingsOption
	if cfg.Cache.Dir != "" {
		if err := os.MkdirAll(cfg.Cache.Dir, 0o700); err == nil {
			bopts = append(bopts, ocm.WithBlobCache(ocm.BlobCacheOptions(cfg.Cache.Dir, cfg.Cache.Blobs.MaxEntries,
				int64(cfg.Cache.Blobs.MaxBlobSize), cfg.Cache.Blobs.RemotePolicy)))
		}
	}
	b, err := ocm.NewBindings(ctx, ocmCfg, tempDir, bopts...)
	if err != nil {
		return false, err
	}
	defer b.Close(context.Background())
	r, err := ocm.NewResolver(ctx, b)
	if err != nil {
		return false, err
	}
	svc := ocm.NewService(b, r, time.Duration(cfg.Timeouts.Upstream))
	classifier, err := version.NewClassifier(cfg.Versions.PrereleaseMarkers)
	if err != nil {
		return false, err
	}
	token, err := b.RegistryPassword(ctx, "ghcr.io")
	if err != nil {
		return false, err
	}
	if token == "" {
		logger.Warn("no credentials for ghcr.io in .ocmconfig: public packages only, low GitHub API rate limit; descriptors of private packages cannot be read")
	}
	var conns []connector.Connector
	for _, d := range cfg.Discovery {
		vis := make([]connector.Visibility, len(d.Visibility))
		for i, v := range d.Visibility {
			vis[i] = connector.Visibility(v)
		}
		conns = append(conns, connector.NewGitHub(d.ID(), connector.Owner{Org: d.Org, User: d.User, Enterprise: d.Enterprise}, token,
			connector.WithKey(d.Key()), connector.WithTitle(d.Name), connector.WithVisibility(vis...)))
	}
	rep, err := indexsync.Run(ctx, indexsync.Options{DBPath: cfg.DB.Path, Connectors: conns, Classifier: classifier,
		Fetcher: svc, Attestations: attestation.Gatherer{Open: b.RepositoryOpener(), Timeout: time.Duration(cfg.Timeouts.Upstream), Fetch: (&document.Fetcher{Timeout: time.Duration(cfg.Timeouts.Upstream), Allow: cfg.Documents.Allow}).Fetch, Logger: logger}, Registry: b.RepositoryOpener(), RegistryTimeout: time.Duration(cfg.Timeouts.Upstream), CodeHosts: cfg.Catalog.CodeHosts, KubernetesVersions: cfg.Catalog.KubernetesVersions, Full: *full, Logger: logger})
	if err != nil {
		return false, err
	}
	logger.Info("sync done", slog.String("db", cfg.DB.Path), slog.Int("connectorErrors", len(rep.ConnectorErrors)),
		slog.Int("overviewFailures", rep.OverviewFailures), slog.Int("attestationFailures", rep.AttestationFailures))
	return rep.Failed(), nil
}
