package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"sigs.k8s.io/yaml"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/observability"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/overview"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Service is what the REST layer needs from internal/ocm.
type Service interface {
	GetComponentVersion(ctx context.Context, ref ocm.Ref) (*descriptorruntime.Descriptor, error)
	// DescriptorTreeAt returns ref's descriptor (read at ref.Repository) and,
	// recursively, its references, each with the location it was read from.
	DescriptorTreeAt(ctx context.Context, ref ocm.Ref, has ocm.HasVersionAt) ([]ocm.Located, error)
	// OpenResource opens a resource; withBody false (HEAD) returns metadata
	// only, with Body nil.
	OpenResource(ctx context.Context, ref ocm.Ref, identity ocmruntime.Identity, withBody bool) (*ocm.Resource, error)
	// CheckRepository validates a ?repository= against the configured resolvers.
	CheckRepository(repo string) error
}

// Index is the read side of the DuckDB index (*index.Handle).
type Index interface {
	Acquire() (*index.Store, func(), error)
}

type handler struct {
	svc   Service
	idx   Index
	ov    overview.Options
	open  attestation.RepositoryOpener
	fetch document.FetchFunc
}

// Option configures NewHandler.
type Option func(*handler)

// WithOverviewOptions sets how version overviews are derived (hub.yaml catalog).
func WithOverviewOptions(o overview.Options) Option { return func(h *handler) { h.ov = o } }

// WithRegistry sets how attestation content is read from OCI registries;
// without it registry-held attestations answer 501.
func WithRegistry(open attestation.RepositoryOpener) Option {
	return func(h *handler) { h.open = open }
}

// WithFetcher sets how label-linked documents are fetched; without it they
// answer 501.
func WithFetcher(fetch document.FetchFunc) Option { return func(h *handler) { h.fetch = fetch } }

// NewHandler returns the REST API handler.
func NewHandler(svc Service, idx Index, opts ...Option) http.Handler {
	h := &handler{svc: svc, idx: idx}
	for _, o := range opts {
		o(h)
	}
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route, err := ParseRoute(r.URL.EscapedPath())
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "NAME_INVALID", err.Error(), nil)
		return
	}
	allowed := []string{http.MethodGet, http.MethodHead}
	if route.Kind == RouteDiscovery {
		allowed = []string{http.MethodPost}
	}
	if !slices.Contains(allowed, r.Method) {
		allow := strings.Join(allowed, ", ")
		w.Header().Set("Allow", allow)
		writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "allowed: "+allow, nil)
		return
	}
	span := trace.SpanFromContext(r.Context())
	span.SetName(r.Method + " " + route.Kind.String())
	span.SetAttributes(attribute.String("ocm.component", route.Name), attribute.String("ocm.version", route.Version))
	observability.Annotate(r.Context(),
		slog.String("route", route.Kind.String()),
		slog.String("component", route.Name),
		slog.String("version", route.Version),
		slog.String("resource", route.Resource),
		slog.String("repository", r.URL.Query().Get("repository")),
	)
	switch route.Kind {
	case RouteHealth:
		writeBody(w, r, http.StatusOK, "text/plain", []byte("ok"))
	case RouteBase:
		writeBody(w, r, http.StatusOK, mediaJSON, []byte("{}"))
	case RouteDiscovery:
		writeError(w, r, http.StatusNotImplemented, "NOT_IMPLEMENTED", "discovery is not implemented yet (see TODOS.md)", nil)
	case RouteComponents, RouteComponent, RouteVersions, RouteDescriptor, RouteResource, RouteOverview, RouteAttestations, RouteAttestationContent, RouteImage, RouteImageDetails:
		h.withStore(w, r, route)
	default:
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "no such endpoint", nil)
	}
}

// withStore acquires the index for the request. Descriptor and resource
// routes pinned with ?repository= work without an index (resolver repos).
func (h *handler) withStore(w http.ResponseWriter, r *http.Request, route Route) {
	st, release, err := h.idx.Acquire()
	if err != nil {
		pinned := r.URL.Query().Get("repository") != ""
		if !pinned || (route.Kind != RouteDescriptor && route.Kind != RouteResource && route.Kind != RouteOverview && route.Kind != RouteImage && route.Kind != RouteImageDetails) {
			writeError(w, r, http.StatusServiceUnavailable, "INDEX_UNAVAILABLE", "the index is not built yet: run cmd/sync ("+err.Error()+")", nil)
			return
		}
		st, release = nil, func() {}
	}
	defer release()
	switch route.Kind {
	case RouteComponents:
		h.listComponents(w, r, st)
	case RouteComponent:
		h.component(w, r, st, route.Name)
	case RouteVersions:
		h.versions(w, r, st, route.Name)
	case RouteDescriptor:
		h.descriptor(w, r, st, route)
	case RouteResource:
		h.resource(w, r, st, route)
	case RouteOverview:
		h.versionOverview(w, r, st, route)
	case RouteAttestations:
		h.attestations(w, r, st, route)
	case RouteAttestationContent:
		h.attestationContent(w, r, st, route)
	case RouteImage, RouteImageDetails:
		h.image(w, r, st, route)
	}
}

// locate picks the location for name:version: a validated ?repository= pin,
// else the overview location, else the first location that has it. st is nil
// only for pinned requests without an index.
func (h *handler) locate(ctx context.Context, st *index.Store, name, version, pinned string) (string, error) {
	if pinned != "" {
		key, err := ocm.LocationKey(pinned)
		if err != nil {
			return "", err
		}
		if st != nil {
			if ok, err := st.IsLocation(ctx, key); err != nil {
				return "", err
			} else if ok {
				return key, nil
			}
		}
		if err := h.svc.CheckRepository(pinned); err != nil {
			return "", fmt.Errorf("%w: %s is neither an indexed location nor a configured resolver repository", ocm.ErrRepositoryNotAllowed, key)
		}
		return key, nil
	}
	locs, err := st.VersionLocations(ctx, name, version)
	if err != nil {
		return "", err
	}
	if len(locs) == 0 {
		if _, err := st.Component(ctx, name); errors.Is(err, index.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ocm.ErrComponentUnknown, name)
		} else if err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: %s:%s", ocm.ErrVersionUnknown, name, version)
	}
	return locs[0], nil
}

// latest resolves the `latest` keyword via the index.
func (h *handler) latest(ctx context.Context, st *index.Store, name string) (string, error) {
	if st == nil {
		return "", fmt.Errorf("%w: latest needs the index", ocm.ErrVersionUnknown)
	}
	d, err := st.Component(ctx, name)
	if errors.Is(err, index.ErrNotFound) {
		return "", fmt.Errorf("%w: %s", ocm.ErrComponentUnknown, name)
	}
	if err != nil {
		return "", err
	}
	if d.Latest == "" {
		return "", fmt.Errorf("%w: %s has no stable version", ocm.ErrVersionUnknown, name)
	}
	return d.Latest, nil
}

func (h *handler) descriptor(w http.ResponseWriter, r *http.Request, st *index.Store, route Route) {
	q := r.URL.Query()
	recursive, err := parseRecursive(q)
	var compact bool
	if err == nil {
		compact, err = parseCompact(q)
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "PARAMETER_INVALID", err.Error(), nil)
		return
	}
	if route.Version == ocm.Latest {
		v, err := h.latest(r.Context(), st, route.Name)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		redirect(w, r, route.Name, "descriptors/"+url.PathEscape(v))
		return
	}
	loc, err := h.locate(r.Context(), st, route.Name, route.Version, q.Get("repository"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ref := ocm.Ref{Component: route.Name, Version: route.Version, Repository: loc}
	// The representation depends on Accept; shared caches must key on it.
	w.Header().Set("Vary", "Accept")
	format, ok := negotiate(r.Header.Get("Accept"))
	if !ok {
		writeError(w, r, http.StatusNotAcceptable, "NOT_ACCEPTABLE", "descriptors are available as application/yaml or application/json", nil)
		return
	}
	var v any
	if recursive == -1 {
		has := ocm.HasVersionAt(func(context.Context, string, string, string) (bool, error) { return false, nil })
		if st != nil {
			has = st.HasVersionAt
		}
		located, err := h.svc.DescriptorTreeAt(r.Context(), ref, has)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		list := make([]locatedV2, 0, len(located))
		for _, l := range located {
			d, err := toV2(l.Descriptor, compact)
			if err != nil {
				writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "converting descriptor: "+err.Error(), nil)
				return
			}
			list = append(list, locatedV2{Location: l.Location, Descriptor: d})
		}
		v = list
	} else {
		desc, err := h.svc.GetComponentVersion(r.Context(), ref)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		d, err := toV2(desc, compact)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "converting descriptor: "+err.Error(), nil)
			return
		}
		v = d
	}
	body, err := json.Marshal(v)
	if err == nil && format == mediaYAML {
		body, err = yaml.JSONToYAML(body)
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "encoding descriptor: "+err.Error(), nil)
		return
	}
	writeCacheable(w, r, format, body)
}

// locatedV2 is one entry of a recursive descriptor response.
type locatedV2 struct {
	Location   string                   `json:"location"`
	Descriptor *descriptorv2.Descriptor `json:"descriptor"`
}

// parseRecursive reads ?recursive= like the ocm CLI's --recursive: 0 (the
// default) or -1 (unlimited); depths above 0 are not implemented yet.
func parseRecursive(q url.Values) (int, error) {
	s := q.Get("recursive")
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	switch {
	case err != nil || n < -1:
		return 0, fmt.Errorf("recursive=%q: must be -1 (unlimited) or 0", s)
	case n > 0:
		return 0, errors.New("recursion depth > 0 is not implemented yet")
	}
	return n, nil
}

// parseCompact reads ?compact=: true, false or absent.
func parseCompact(q url.Values) (bool, error) {
	switch s := q.Get("compact"); s {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("compact=%q: must be true or false", s)
	}
}

// toV2 converts a descriptor for output; compact trims it to what a
// component tree needs (name, version, provider, labels, references). The
// trimmed parts are dropped before converting, so they are never converted.
func toV2(d *descriptorruntime.Descriptor, compact bool) (*descriptorv2.Descriptor, error) {
	if compact {
		c := *d
		c.Component.Resources = nil
		c.Component.Sources = nil
		c.Component.RepositoryContexts = nil
		c.Signatures = nil
		d = &c
	}
	return descriptorruntime.ConvertToV2(descriptorv2.Scheme, d)
}
