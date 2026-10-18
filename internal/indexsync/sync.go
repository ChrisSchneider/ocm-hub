// Package indexsync runs one sync: it writes a copy of the index and
// atomically replaces the original, so readers never see a partial state.
package indexsync

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/connector"
	"chrisschneider.dev/delivery-bundler/internal/image"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/overview"
	"chrisschneider.dev/delivery-bundler/internal/version"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// deltaOverlap re-reads a little before the last run against clock skew.
const deltaOverlap = time.Minute

// overviewConcurrency bounds parallel descriptor fetches.
const overviewConcurrency = 8

// changelogsPerRun bounds the descriptors read per sync to flag changelogs.
const changelogsPerRun = 200

// Fetcher reads a descriptor at a known location (ocm.Service).
type Fetcher interface {
	DescriptorAt(ctx context.Context, component, version, location string) (*descriptorruntime.Descriptor, error)
}

// AttestationGatherer lists the attestations of a component version
// (attestation.Gatherer).
type AttestationGatherer interface {
	Gather(ctx context.Context, d *descriptorruntime.Descriptor, location string) (attestation.Result, []string)
}

// Options configure Run.
type Options struct {
	DBPath     string
	Connectors []connector.Connector // in hub.yaml order
	Classifier *version.Classifier
	Fetcher    Fetcher
	// Attestations gathers attestations of default versions; nil: none are gathered.
	Attestations AttestationGatherer
	// Registry opens OCI repositories to read the image platforms of default
	// versions; nil: only the descriptor's architectures are stored.
	Registry attestation.RepositoryOpener
	// RegistryTimeout bounds the image reads of one version; 0: 30s.
	RegistryTimeout time.Duration
	CodeHosts       []string // name prefix code hosts; nil: overview.DefaultCodeHosts
	// KubernetesVersions are the known Kubernetes minors for compatibility labels; nil: overview default.
	KubernetesVersions []string
	Full               bool // force a full sync of every connector
	Now                func() time.Time
	Logger             *slog.Logger
}

// Report is the outcome of a run that produced a new index file.
type Report struct {
	ConnectorErrors  map[string]error // by connector ID
	Full             map[string]bool  // connectors that ran full
	OverviewFailures int
	// AttestationFailures counts failed attestation levels; not part of Failed().
	AttestationFailures int
}

// Failed reports whether anything failed (cmd/sync exits 1).
func (r Report) Failed() bool { return len(r.ConnectorErrors) > 0 || r.OverviewFailures > 0 }

// Run performs one sync. A returned error means the old file is untouched.
func Run(ctx context.Context, o Options) (Report, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	rep := Report{ConnectorErrors: map[string]error{}, Full: map[string]bool{}}
	tmp := o.DBPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(o.DBPath), 0o755); err != nil {
		return rep, err
	}
	// One run at a time: an overlapping run would delete this run's tmp file.
	lockPath := o.DBPath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return rep, err
	}
	defer lock.Close() // closing also releases the flock
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return rep, fmt.Errorf("sync already running (lock %s)", lockPath)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	for _, f := range []string{tmp, tmp + ".wal"} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return rep, err
		}
	}
	if err := ctx.Err(); err != nil {
		return rep, err
	}
	w, forceFull, err := openCopy(o.DBPath, tmp, o.Logger)
	if err != nil {
		return rep, err
	}
	ok := false
	defer func() {
		if !ok {
			w.Close()
			os.Remove(tmp)
			os.Remove(tmp + ".wal")
		}
	}()
	runStart := o.Now().UTC()
	// Prune first: an edited entry keeps its key under a new ID, and keys are unique.
	keep := make([]string, len(o.Connectors))
	for i, c := range o.Connectors {
		keep[i] = c.ID()
	}
	if err := w.PruneConnectors(ctx, keep); err != nil {
		return rep, err
	}
	srcs := make([]index.Source, len(o.Connectors))
	descs := make([]connector.Source, len(o.Connectors))
	described := make([]bool, len(o.Connectors))
	for i, c := range o.Connectors {
		st, _, err := w.ConnectorState(ctx, c.ID())
		if err != nil {
			return rep, err
		}
		descs[i], described[i] = describe(ctx, c, o.Logger)
		srcs[i] = resolveSource(c, descs[i], st.Source)
	}
	warnOverlaps(o.Connectors, descs, described, o.Logger)
	for pos, c := range o.Connectors {
		src := srcs[pos]
		full, err := runConnector(ctx, w, c, src, pos, o, runStart, forceFull)
		rep.Full[c.ID()] = full
		if err != nil {
			if ctx.Err() != nil {
				return rep, ctx.Err()
			}
			o.Logger.Error("connector failed", slog.String("connector", c.ID()), slog.String("type", c.Type()), slog.String("error", err.Error()))
			rep.ConnectorErrors[c.ID()] = err
			if rerr := w.RecordError(ctx, c.ID(), c.Type(), src, pos, err); rerr != nil {
				return rep, rerr
			}
		}
	}
	if err := w.Cleanup(ctx); err != nil {
		return rep, err
	}
	n, af, err := fetchOverviews(ctx, w, o)
	if err != nil {
		return rep, err
	}
	rep.OverviewFailures, rep.AttestationFailures = n, af
	if o.Fetcher != nil {
		if err := fetchChangelogs(ctx, w, o); err != nil {
			return rep, err
		}
	}
	hosts := o.CodeHosts
	if hosts == nil {
		hosts = overview.DefaultCodeHosts
	}
	if err := w.UpdateFamilies(ctx, func(n string) string { return overview.NamePrefix(n, hosts) }); err != nil {
		return rep, err
	}
	if err := ctx.Err(); err != nil {
		return rep, err
	}
	ok = true
	if err := w.Close(); err != nil {
		return rep, err
	}
	if err := os.Rename(tmp, o.DBPath); err != nil {
		return rep, err
	}
	return rep, nil
}

// openCopy copies the current file to tmp and opens it. A missing file or a
// schema mismatch starts from an empty index and forces full syncs.
func openCopy(path, tmp string, log *slog.Logger) (*index.Writer, bool, error) {
	fresh := false
	if err := copyFile(path, tmp); errors.Is(err, os.ErrNotExist) {
		fresh = true
	} else if err != nil {
		return nil, false, err
	}
	w, err := index.OpenWriter(tmp)
	if errors.Is(err, index.ErrSchemaMismatch) {
		log.Warn("index schema changed; discarding the old index and running a full sync", slog.String("error", err.Error()))
		for _, f := range []string{tmp, tmp + ".wal"} {
			if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, false, err
			}
		}
		w, err = index.OpenWriter(tmp)
		fresh = true
	}
	return w, fresh, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func runConnector(ctx context.Context, w *index.Writer, c connector.Connector, src index.Source, pos int, o Options, runStart time.Time, forceFull bool) (full bool, err error) {
	state, known, err := w.ConnectorState(ctx, c.ID())
	if err != nil {
		return false, err
	}
	full = o.Full || forceFull || !known || state.LastFullSyncAt.IsZero()
	// A changed scope (e.g. the enterprise's organization set) invalidates what a delta can see.
	if src.Scope != "" && src.Scope != state.Source.Scope {
		full = true
	}
	since := time.Time{}
	if !full {
		since = state.LastDeltaSyncAt.Add(-deltaOverlap)
	}
	tx, err := w.BeginConnector(ctx, c.ID(), c.Type(), src, pos, runStart)
	if err != nil {
		return full, err
	}
	var cache connector.PageCache = tx
	if full {
		if err := tx.ResetPageCache(ctx); err != nil {
			tx.Rollback()
			return full, err
		}
		cache = writeOnlyCache{tx}
	}
	count := 0
	for cv, err := range c.Changed(ctx, since, cache) {
		if err != nil {
			tx.Rollback()
			return full, err
		}
		f := index.Found{Component: cv.Component, Repository: cv.Repository,
			Owner: cmp.Or(cv.Owner, c.Key()), Visibility: string(cmp.Or(cv.Visibility, connector.Private))}
		for _, v := range cv.Versions {
			f.Versions = append(f.Versions, index.FoundVersion{Name: v.Name, PublishedAt: v.PublishedAt, Stable: o.Classifier.Stable(v.Name)})
		}
		if err := tx.Apply(ctx, f); err != nil {
			tx.Rollback()
			return full, err
		}
		count++
	}
	if full {
		if err := tx.DropUnseen(ctx); err != nil {
			tx.Rollback()
			return full, err
		}
	}
	o.Logger.Info("connector synced", slog.String("connector", c.ID()), slog.String("type", c.Type()),
		slog.Bool("full", full), slog.Int("changed", count))
	return full, tx.Commit(ctx, full)
}

// writeOnlyCache stores ETags but never answers lookups (full runs must see every page).
type writeOnlyCache struct{ connector.PageCache }

func (writeOnlyCache) Lookup(string) (string, string, bool) { return "", "", false }

// describe asks c for its owner profile; ok is true only when Describe succeeded.
// A failed Describe is only a warning.
func describe(ctx context.Context, c connector.Connector, log *slog.Logger) (connector.Source, bool) {
	d, isDescriber := c.(connector.Describer)
	if !isDescriber {
		return connector.Source{}, false
	}
	desc, err := d.Describe(ctx)
	if err != nil {
		log.Warn("connector owner profile unavailable", slog.String("connector", c.ID()), slog.String("error", err.Error()))
		return connector.Source{}, false
	}
	return desc, true
}

// resolveSource is how c is shown: title from hub.yaml name:, else the owner's
// display name, else the stored title, else the key's last segment; avatar
// from the owner, else the stored one.
func resolveSource(c connector.Connector, desc connector.Source, prev index.Source) index.Source {
	key := c.Key()
	title := cmp.Or(c.Title(), desc.Title, prev.Title, key[strings.LastIndex(key, ":")+1:])
	return index.Source{Key: key, Title: title, AvatarURL: cmp.Or(desc.AvatarURL, prev.AvatarURL), Scope: desc.Scope, Owners: owners(desc.Owners)}
}

func owners(in []connector.OwnerInfo) []index.Owner {
	if in == nil {
		return nil
	}
	out := make([]index.Owner, 0, len(in))
	for _, o := range in {
		out = append(out, index.Owner{Key: o.Key, Title: o.Title, AvatarURL: o.AvatarURL, Parent: o.Parent})
	}
	return out
}

// warnOverlaps logs each owner that more than one described connector lists.
func warnOverlaps(conns []connector.Connector, descs []connector.Source, ok []bool, log *slog.Logger) {
	by := map[string][]int{}
	var order []string
	for i, d := range descs {
		if !ok[i] {
			continue
		}
		for _, o := range d.Owners {
			if len(by[o.Key]) == 0 {
				order = append(order, o.Key)
			}
			by[o.Key] = append(by[o.Key], i)
		}
	}
	for _, owner := range order {
		idx := by[owner]
		if len(idx) < 2 {
			continue
		}
		keys := make([]string, len(idx))
		vis := map[string]bool{}
		for j, i := range idx {
			keys[j] = conns[i].Key()
			vs := slices.Sorted(slices.Values(descs[i].Visibility))
			vis[fmt.Sprint(vs)] = true
		}
		msg := "owner listed by several sources; its packages are fetched by each"
		if len(vis) > 1 {
			msg += " (with different visibility)"
		}
		log.Warn(msg, slog.String("owner", owner), slog.String("sources", strings.Join(keys, ", ")))
	}
}

// attestationSet is one gathered attestation set, ready to store.
type attestationSet struct {
	t        index.AttestationTarget
	res      attestation.Result
	warnings []string
}

// fetchOverviews fills display data for components whose latest changed and
// gathers attestations for them; with o.Full it also re-gathers the other
// stored attestation sets. It returns how many overview fetches and how many
// attestation levels failed.
func fetchOverviews(ctx context.Context, w *index.Writer, o Options) (int, int, error) {
	targets, err := w.PendingOverviews(ctx)
	if err != nil {
		return 0, 0, err
	}
	type result struct {
		t        index.OverviewTarget
		ov       index.Overview
		warnings []string
		att      *attestationSet
		err      error
	}
	results := make([]result, len(targets))
	sem := make(chan struct{}, overviewConcurrency)
	var wg sync.WaitGroup
	for i, t := range targets {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			d, err := o.Fetcher.DescriptorAt(ctx, t.Component, t.Version, t.Repository)
			if err != nil {
				results[i] = result{t: t, err: err}
				return
			}
			ov := overview.FromDescriptor(d, overview.Options{KubernetesVersions: o.KubernetesVersions})
			info := ov.Display.WithFallbackTitle(t.Component)
			children := make([]index.ChildRef, 0, len(ov.References))
			for _, r := range ov.References {
				children = append(children, index.ChildRef{ReferenceName: r.Name, ChildName: r.Component, ChildVersion: r.Version})
			}
			results[i] = result{t: t, warnings: ov.Warnings, ov: index.Overview{Title: info.Title, Description: info.Description,
				Icon: info.Icon, Categories: info.Categories, Architectures: architectures(ctx, o, d, t, ov.Architectures),
				Provider: ov.Provider, Signed: ov.Signed, Children: children}}
			if o.Attestations != nil {
				res, warns := o.Attestations.Gather(ctx, d, t.Repository)
				results[i].att = &attestationSet{t: index.AttestationTarget{Component: t.Component, Version: t.Version, Repository: t.Repository}, res: res, warnings: warns}
			}
		})
	}
	wg.Wait()
	failed, attFailed := 0, 0
	handled := map[index.AttestationTarget]bool{}
	for _, r := range results {
		if r.err != nil {
			failed++
			o.Logger.Warn("overview fetch failed", slog.String("component", r.t.Component), slog.String("version", r.t.Version),
				slog.String("location", r.t.Repository), slog.String("error", r.err.Error()))
			if err := w.MarkOverviewFailed(ctx, r.t.Component); err != nil {
				return failed, attFailed, err
			}
			continue
		}
		for _, msg := range r.warnings {
			o.Logger.Warn("label skipped", slog.String("component", r.t.Component), slog.String("version", r.t.Version), slog.String("warning", msg))
		}
		if err := w.SetOverview(ctx, r.t, r.ov); err != nil {
			return failed, attFailed, fmt.Errorf("storing overview of %s: %w", r.t.Component, err)
		}
		if r.att != nil {
			handled[r.att.t] = true
			n, err := storeAttestations(ctx, w, o, *r.att)
			attFailed += n
			if err != nil {
				return failed, attFailed, err
			}
		}
	}
	if o.Attestations != nil && o.Full {
		n, err := regatherAttestations(ctx, w, o, handled)
		attFailed += n
		if err != nil {
			return failed, attFailed, err
		}
	}
	return failed, attFailed, nil
}

// fetchChangelogs flags up to changelogsPerRun versions (newest first) that
// have a changelog resource. A version without one but with references is
// stored unknown: the changelog may live in a referenced component.
func fetchChangelogs(ctx context.Context, w *index.Writer, o Options) error {
	targets, err := w.PendingChangelogs(ctx, changelogsPerRun, o.Full)
	if err != nil {
		return err
	}
	type result struct {
		has *bool
		err error
	}
	results := make([]result, len(targets))
	sem := make(chan struct{}, overviewConcurrency)
	var wg sync.WaitGroup
	for i, t := range targets {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			d, err := o.Fetcher.DescriptorAt(ctx, t.Component, t.Version, t.Repository)
			if err != nil {
				results[i] = result{err: err}
				return
			}
			has := slices.ContainsFunc(d.Component.Resources, func(r descriptorruntime.Resource) bool { return r.Type == "changelog" })
			if has || len(d.Component.References) == 0 {
				results[i] = result{has: &has}
			}
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	for i, t := range targets {
		r := results[i]
		msg := ""
		if r.err != nil {
			msg = r.err.Error()
			o.Logger.Warn("changelog check failed", slog.String("component", t.Component), slog.String("version", t.Version),
				slog.String("location", t.Repository), slog.String("error", msg))
		}
		if err := w.SetChangelog(ctx, t.Component, t.Version, r.has, msg); err != nil {
			return fmt.Errorf("storing changelog flag of %s %s: %w", t.Component, t.Version, err)
		}
	}
	return nil
}

// architectures merges the image platforms of d's OCI resources (with
// o.Registry) into the descriptor's; failed reads are only logged.
func architectures(ctx context.Context, o Options, d *descriptorruntime.Descriptor, t index.OverviewTarget, ocm []overview.Architecture) []string {
	var images []string
	if o.Registry != nil {
		ctx, cancel := context.WithTimeout(ctx, cmp.Or(o.RegistryTimeout, 30*time.Second))
		defer cancel()
		var errs []error
		images, errs = image.ResourcePlatforms(ctx, o.Registry, d, t.Repository)
		for _, err := range errs {
			o.Logger.Debug("image platforms not read", slog.String("component", t.Component), slog.String("version", t.Version), slog.String("error", err.Error()))
		}
	}
	merged := overview.MergeArchitectures(ocm, images)
	out := make([]string, len(merged))
	for i, a := range merged {
		out[i] = a.Platform
	}
	return out
}

// regatherAttestations gathers all current targets (AttestationTargets) not
// already gathered in this run. A failed descriptor fetch keeps the old set.
func regatherAttestations(ctx context.Context, w *index.Writer, o Options, handled map[index.AttestationTarget]bool) (int, error) {
	all, err := w.AttestationTargets(ctx)
	if err != nil {
		return 0, err
	}
	var targets []index.AttestationTarget
	for _, t := range all {
		if !handled[t] {
			targets = append(targets, t)
		}
	}
	sets := make([]*attestationSet, len(targets))
	sem := make(chan struct{}, overviewConcurrency)
	var wg sync.WaitGroup
	for i, t := range targets {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			d, err := o.Fetcher.DescriptorAt(ctx, t.Component, t.Version, t.Repository)
			if err != nil {
				o.Logger.Warn("attestation descriptor fetch failed; keeping the stored set", slog.String("component", t.Component),
					slog.String("version", t.Version), slog.String("location", t.Repository), slog.String("error", err.Error()))
				return
			}
			res, warns := o.Attestations.Gather(ctx, d, t.Repository)
			sets[i] = &attestationSet{t: t, res: res, warnings: warns}
		})
	}
	wg.Wait()
	failed := 0
	for _, s := range sets {
		if s == nil {
			continue
		}
		n, err := storeAttestations(ctx, w, o, *s)
		failed += n
		if err != nil {
			return failed, err
		}
	}
	return failed, nil
}

// storeAttestations logs the set's warnings and failed levels, stores it and
// returns the number of failed levels.
func storeAttestations(ctx context.Context, w *index.Writer, o Options, s attestationSet) (int, error) {
	for _, msg := range s.warnings {
		o.Logger.Warn("label skipped", slog.String("component", s.t.Component), slog.String("version", s.t.Version), slog.String("warning", msg))
	}
	failed := 0
	for _, lv := range s.res.Levels {
		if lv.Status == attestation.StatusFailed {
			failed++
			name := ""
			if lv.Resource != nil {
				name = lv.Resource.Name
			}
			o.Logger.Warn("attestation lookup failed", slog.String("component", s.t.Component), slog.String("version", s.t.Version),
				slog.String("resource", name), slog.String("error", lv.Error))
		}
	}
	if err := w.SetAttestations(ctx, s.t, s.res, o.Now().UTC()); err != nil {
		return failed, fmt.Errorf("storing attestations of %s: %w", s.t.Component, err)
	}
	return failed, nil
}
