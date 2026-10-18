package index

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func day(n int) time.Time { return t0.AddDate(0, 0, n) }

func newWriter(t *testing.T) (*Writer, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	w, err := OpenWriter(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w, p
}

// runSync applies one connector run (not named sync: handle.go imports package sync).
func runSync(t *testing.T, w *Writer, id string, pos int, full bool, run time.Time, found ...Found) {
	t.Helper()
	ctx := context.Background()
	tx, err := w.BeginConnector(ctx, id, "test/v1", Source{Key: id, Title: id}, pos, run)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		if f.Owner == "" {
			f.Owner = "test:" + id
		}
		if f.Visibility == "" {
			f.Visibility = "public"
		}
		if err := tx.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	if full {
		if err := tx.DropUnseen(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx, full); err != nil {
		t.Fatal(err)
	}
	if err := w.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
}

func v(name string, d int) FoundVersion {
	return FoundVersion{Name: name, PublishedAt: day(d), Stable: true}
}
func pre(name string, d int) FoundVersion {
	return FoundVersion{Name: name, PublishedAt: day(d), Stable: false}
}

func queryStrings(t *testing.T, w *Writer, q string, args ...any) []string {
	t.Helper()
	rows, err := w.db.Query(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}
