package rest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/index"
)

const testLoc = "oci::ghcr.io/acme"
const otherLoc = "oci::ghcr.io/other"

// testIndex builds an index with acme.org/x (1.0.0 day 1, 1.1.0 day 2,
// 2.0.0-rc.1 day 3) at testLoc and acme.org/x 1.0.0 at otherLoc.
func testIndex(t *testing.T) *index.Handle { return testIndexWith(t, nil) }

// testIndexWith is testIndex plus extra, called with the open writer.
func testIndexWith(t *testing.T, extra func(ctx context.Context, w *index.Writer)) *index.Handle {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	w, err := index.OpenWriter(p)
	if err != nil {
		t.Fatal(err)
	}
	d := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }
	ctx := t.Context()
	tx, err := w.BeginConnector(ctx, "c", "test/v1", index.Source{Key: "test:c", Title: "Test source"}, 0, d(10))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []index.Found{
		{Component: "acme.org/x", Repository: testLoc, Owner: "test:c", Visibility: "public", Versions: []index.FoundVersion{
			{Name: "1.0.0", PublishedAt: d(1), Stable: true}, {Name: "1.1.0", PublishedAt: d(2), Stable: true},
			{Name: "2.0.0-rc.1", PublishedAt: d(3)}}},
		{Component: "acme.org/x", Repository: otherLoc, Owner: "test:c", Visibility: "public", Versions: []index.FoundVersion{{Name: "1.0.0", PublishedAt: d(5), Stable: true}}},
		{Component: "acme.org/x/versions", Repository: testLoc, Owner: "test:c", Visibility: "public", Versions: []index.FoundVersion{{Name: "0.1.0", PublishedAt: d(1), Stable: true}}},
	} {
		if err := tx.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := w.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err = w.BeginConnector(ctx, "ent", "test/v1", index.Source{Key: "test:ent", Title: "Test Enterprise", Scope: "s", Owners: []index.Owner{
		{Key: "test:org-a", Title: "Org A", AvatarURL: "https://avatars.example/a", Parent: "test:ent"}}}, 1, d(10))
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Apply(ctx, index.Found{Component: "acme.org/x", Repository: "oci::ghcr.io/org-a", Owner: "test:org-a", Visibility: "public",
		Versions: []index.FoundVersion{{Name: "1.0.0", PublishedAt: d(4), Stable: true}}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := w.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.SetOverview(ctx, index.OverviewTarget{Component: "acme.org/x", Version: "1.1.0", Repository: testLoc},
		index.Overview{Title: "X", Provider: "acme", Categories: []string{"tools"}}); err != nil {
		t.Fatal(err)
	}
	if extra != nil {
		extra(ctx, w)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	h := index.NewHandle(p)
	t.Cleanup(func() { h.Close() })
	return h
}

// noIndex is an index whose file does not exist.
func noIndex(t *testing.T) *index.Handle {
	return index.NewHandle(filepath.Join(t.TempDir(), "missing.duckdb"))
}
