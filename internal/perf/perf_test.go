//go:build perf

// Package perf holds performance tests: go test -tags perf -v -timeout 30m ./internal/perf/
package perf

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"

	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/observability"
	"chrisschneider.dev/delivery-bundler/internal/rest"
)

// synthetic index: 2000 components × 100 versions × 2 locations; each
// component references the next three (relevance has references to count).
func buildIndex(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	w, err := index.OpenWriter(p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tx, err := w.BeginConnector(ctx, "perf", "perf/v1", index.Source{Key: "perf", Title: "perf"}, 0, base)
	if err != nil {
		t.Fatal(err)
	}
	for c := range 2000 {
		name := fmt.Sprintf("acme.org/team-%02d/component-%04d", c%40, c)
		for l, loc := range []string{"oci::ghcr.io/acme", "oci::mirror.example/acme"} {
			f := index.Found{Component: name, Repository: loc, Owner: "perf", Visibility: "public"}
			for v := range 100 {
				f.Versions = append(f.Versions, index.FoundVersion{Name: fmt.Sprintf("1.%d.0", v),
					PublishedAt: base.Add(time.Duration(c*100+v+l*7) * time.Minute), Stable: v%10 != 9})
			}
			if err := tx.Apply(ctx, f); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := w.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	targets, err := w.PendingOverviews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, tg := range targets {
		var c int
		fmt.Sscanf(tg.Component[len(tg.Component)-4:], "%d", &c)
		var children []index.ChildRef
		for k := 1; k <= 3; k++ {
			child := fmt.Sprintf("acme.org/team-%02d/component-%04d", (c+k)%2000%40, (c+k)%2000)
			children = append(children, index.ChildRef{ReferenceName: child, ChildName: child, ChildVersion: "1.0.0"})
		}
		if err := w.SetOverview(ctx, tg, index.Overview{Title: "Component " + tg.Component, Provider: fmt.Sprintf("p%d", i%5),
			Categories: []string{fmt.Sprintf("cat%d", i%8)}, Children: children}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.UpdateFamilies(ctx, func(n string) string { return n[:strings.LastIndex(n, "/")] }); err != nil { // top_level, relevance
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPerfIndexQueries(t *testing.T) {
	h := index.NewHandle(buildIndex(t))
	defer h.Close()
	srv := httptest.NewServer(observability.Middleware(rest.NewHandler(nil, h), observability.NewLogger(io.Discard, slog.LevelError)))
	defer srv.Close()
	targets := map[string]string{
		"list":          "/v1/components",
		"search+facets": "/v1/components?q=team-07&location=oci::mirror.example/acme&category=cat3",
		"by name":       "/v1/components?sort=name&limit=200",
		"relevance 200": "/v1/components?sort=relevance&limit=200",
		"overview":      "/v1/components/acme.org%2Fteam-07%2Fcomponent-0007",
		"versions":      "/v1/components/acme.org%2Fteam-07%2Fcomponent-0007/versions?includePrerelease=true",
	}
	for name, path := range targets {
		t.Run(name, func(t *testing.T) {
			// vegeta sends no If-None-Match
			atk := vegeta.NewAttacker()
			var m vegeta.Metrics
			tg := vegeta.NewStaticTargeter(vegeta.Target{Method: "GET", URL: srv.URL + path})
			for res := range atk.Attack(tg, vegeta.Rate{Freq: 100, Per: time.Second}, 10*time.Second, name) {
				m.Add(res)
			}
			m.Close()
			t.Logf("%s: p50=%v p95=%v p99=%v success=%.2f", name, m.Latencies.P50, m.Latencies.P95, m.Latencies.P99, m.Success)
			if m.Success < 1 || m.Latencies.P95 > 50*time.Millisecond {
				t.Fatalf("%s: p95 %v (want < 50ms), success %.2f, errors %v", name, m.Latencies.P95, m.Success, m.Errors)
			}
			if code := m.StatusCodes[fmt.Sprint(http.StatusOK)]; code == 0 {
				t.Fatalf("no 200s: %v", m.StatusCodes)
			}
		})
	}
}
