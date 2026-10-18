package index

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func build(t *testing.T, path string, components ...string) {
	t.Helper()
	w, err := OpenWriter(path)
	must(t, err)
	var found []Found
	for _, c := range components {
		found = append(found, Found{Component: c, Repository: locA, Versions: []FoundVersion{v("1.0.0", 1)}})
	}
	runSync(t, w, "c1", 0, true, day(1), found...)
	must(t, w.Close())
}

func TestHandleMissingAndMismatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hub.duckdb")
	h := NewHandle(p)
	defer h.Close()
	if _, _, err := h.Acquire(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing: %v", err)
	}
	w, err := OpenWriter(p)
	must(t, err)
	_, err = w.db.Exec("UPDATE schema_version SET version = 999")
	must(t, err)
	must(t, w.Close())
	h.forceCheck()
	if _, _, err := h.Acquire(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("mismatch: %v", err)
	}
}

func TestHandleSwapKeepsInFlightStore(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hub.duckdb")
	build(t, p, "acme.org/old")
	h := NewHandle(p)
	defer h.Close()
	old, releaseOld, err := h.Acquire()
	must(t, err)
	gen := old.Generation()

	tmp := p + ".tmp"
	build(t, tmp, "acme.org/new")
	time.Sleep(10 * time.Millisecond) // distinct mtime on coarse filesystems
	must(t, os.Rename(tmp, p))
	h.forceCheck()

	cur, release, err := h.Acquire()
	must(t, err)
	defer release()
	if cur.Generation() == gen {
		t.Fatal("generation must change after a swap")
	}
	if _, err := cur.Component(context.Background(), "acme.org/new"); err != nil {
		t.Fatalf("new store: %v", err)
	}
	// the old store still answers until released
	if _, err := old.Component(context.Background(), "acme.org/old"); err != nil {
		t.Fatalf("old store closed while in use: %v", err)
	}
	releaseOld()
}

func TestHandleFailingNewGenerationKeepsCurrent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	build(t, p, "acme.org/old")
	h := NewHandle(p)
	defer h.Close()
	_, rel, err := h.Acquire()
	must(t, err)
	rel()

	tmp := p + ".tmp"
	build(t, tmp, "acme.org/new")
	w, err := OpenWriter(tmp)
	must(t, err)
	_, err = w.db.Exec("UPDATE schema_version SET version = 999")
	must(t, err)
	must(t, w.Close())
	time.Sleep(10 * time.Millisecond)
	must(t, os.Rename(tmp, p))
	h.forceCheck()

	s, release, err := h.Acquire()
	must(t, err)
	defer release()
	if _, err := s.Component(context.Background(), "acme.org/old"); err != nil {
		t.Fatalf("old store dropped: %v", err)
	}
}

func TestHandleLinkFailureFallsBackToDirectOpen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hub.duckdb")
	build(t, p, "acme.org/old")
	orig := linkFile
	linkFile = func(string, string) error { return errors.New("read-only file system") }
	t.Cleanup(func() { linkFile = orig })
	h := NewHandle(p)
	defer h.Close()
	s, release, err := h.Acquire()
	must(t, err)
	defer release()
	if _, err := s.Component(context.Background(), "acme.org/old"); err != nil {
		t.Fatalf("direct open: %v", err)
	}
}
