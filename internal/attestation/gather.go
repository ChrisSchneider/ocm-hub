package attestation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"

	"chrisschneider.dev/delivery-bundler/internal/document"
)

// Gatherer collects the attestations of a component version: those the
// descriptor declares and those found in the registry.
type Gatherer struct {
	Open        RepositoryOpener
	Concurrency int                // default 4
	Timeout     time.Duration      // per level lookup and label link; default 30s
	Fetch       document.FetchFunc // label links; nil: not read
	Logger      *slog.Logger       // nil: slog.Default()
}

// Gather reads d via FromDescriptor, then inspects the registry for every
// level with a lookup subject, each within Timeout. A failed lookup marks
// only its level failed. With Fetch, each label link is read within Timeout
// for its digest and summary; a failed read (or a URL off the allow list)
// leaves both empty.
// The warnings are those of FromDescriptor.
func (g Gatherer) Gather(ctx context.Context, d *descriptorruntime.Descriptor, location string) (Result, []string) {
	res, subjects, warnings := FromDescriptor(d, location)
	n := g.Concurrency
	if n <= 0 {
		n = 4
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	sem := make(chan struct{}, n)
	found := make([][]Entry, len(subjects))
	errs := make([]error, len(subjects))
	var wg sync.WaitGroup
	for i, s := range subjects {
		if s == (Subject{}) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			found[i], errs[i] = Inspect(ctx, g.Open, s)
		}()
	}
	if g.Fetch != nil {
		for i := range res.Levels {
			for j := range res.Levels[i].Entries {
				e := &res.Levels[i].Entries[j]
				if e.Source != SourceLabelLink {
					continue
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					ctx, cancel := context.WithTimeout(ctx, timeout)
					defer cancel()
					g.readLink(ctx, e)
				}()
			}
		}
	}
	wg.Wait()
	for i := range res.Levels {
		lv := &res.Levels[i]
		if errs[i] != nil {
			lv.Status, lv.Error = StatusFailed, errs[i].Error()
		}
		lv.Entries = append(lv.Entries, found[i]...)
		if lv.Entries == nil && lv.Status == StatusOK {
			lv.Entries = []Entry{}
		}
	}
	return res, warnings
}

// readLink sets the digest and summary of a label link from its document.
func (g Gatherer) readLink(ctx context.Context, e *Entry) {
	b, err := g.Fetch(ctx, e.URL)
	if errors.Is(err, document.ErrNotAllowed) {
		log := g.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Debug("linked document not on the allow list; listed without digest", slog.String("url", e.URL))
	}
	if err != nil {
		return
	}
	sum := sha256.Sum256(b)
	e.Digest = "sha256:" + hex.EncodeToString(sum[:])
	if d, err := document.Parse(b); err == nil {
		s := d.Summary()
		e.Summary = &s
	}
}
