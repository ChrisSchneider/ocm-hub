package index

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"time"
)

// recheckEvery bounds how often Acquire stats the file.
const recheckEvery = time.Second

// Handle serves the current index file read-only and switches to a new file
// when cmd/sync renames one over it. Stores still in use are closed only
// after their last release.
type Handle struct {
	path    string
	mu      sync.Mutex
	cur     *refStore
	stamp   string // inode-mtime of cur's file
	checked time.Time
	openErr error
	warned  bool
}

type refStore struct {
	*Store
	wg sync.WaitGroup
}

// NewHandle returns a handle for path. Nothing is opened until Acquire.
func NewHandle(path string) *Handle { return &Handle{path: path} }

// Acquire returns the current store and a release func that must be called
// when the request is done. Without a usable file it returns ErrUnavailable.
func (h *Handle) Acquire() (*Store, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if time.Since(h.checked) >= recheckEvery {
		h.refresh()
	}
	if h.cur == nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, h.openErr)
	}
	rs := h.cur
	rs.wg.Add(1)
	return rs.Store, sync.OnceFunc(rs.wg.Done), nil
}

// forceCheck makes the next Acquire stat the file (tests).
func (h *Handle) forceCheck() {
	h.mu.Lock()
	h.checked = time.Time{}
	h.mu.Unlock()
}

// refresh reopens when the file changed. Called with h.mu held.
func (h *Handle) refresh() {
	h.checked = time.Now()
	st, err := os.Stat(h.path)
	if errors.Is(err, fs.ErrNotExist) {
		h.replace(nil, "", err)
		return
	}
	if err != nil {
		h.openErr = err
		return
	}
	stamp := fileStamp(st)
	if stamp == h.stamp && h.cur != nil {
		return
	}
	db, err := h.openReadOnly(stamp)
	if err != nil {
		if h.cur == nil {
			h.stamp = stamp
		}
		h.openErr = err // a failing new generation keeps the current store
		return
	}
	h.replace(&refStore{Store: &Store{db: db, gen: stamp}}, stamp, nil)
}

func (h *Handle) replace(next *refStore, stamp string, err error) {
	if old := h.cur; old != nil {
		go func() { old.wg.Wait(); old.db.Close() }()
	}
	h.cur, h.stamp, h.openErr = next, stamp, err
}

// Close closes the current store once it is released.
func (h *Handle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.replace(nil, "", errors.New("closed"))
	return nil
}

func fileStamp(st os.FileInfo) string {
	var ino uint64
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		ino = s.Ino
	}
	return fmt.Sprintf("%x-%x", ino, st.ModTime().UnixNano())
}

// linkFile creates the per-generation name (a variable so tests can fail it).
var linkFile = os.Link

// openReadOnly opens the file read-only under a unique hard-link name. DuckDB
// caches database instances by path within a process, so reopening path after
// a rename would silently return the instance of the replaced file. The link
// is removed once the file is open; the open descriptor keeps the contents.
// If the link cannot be created (read-only mount, directory not writable) the
// file is opened by its own name, which is only correct for the first open.
func (h *Handle) openReadOnly(stamp string) (*sql.DB, error) {
	name := h.path
	link := fmt.Sprintf("%s.gen-%s", h.path, stamp)
	os.Remove(link)
	if err := linkFile(h.path, link); err != nil {
		if !h.warned {
			h.warned = true
			slog.Warn("index: cannot hard-link the index file, opening it directly; a swapped file may not be picked up until restart", "error", err)
		}
	} else {
		name = link
		defer os.Remove(link)
	}
	db, err := sql.Open("duckdb", name+"?access_mode=read_only")
	if err != nil {
		return nil, err
	}
	if _, err := checkSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
