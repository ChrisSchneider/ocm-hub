package rest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Masterminds/semver/v3"

	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/rest/gen"
)

const (
	listLimitDefault, listLimitMax         = 50, 200
	versionsLimitDefault, versionsLimitMax = 100, 1000
)

// indexETag is a weak validator of an index response: the file generation
// plus the request. A repeat request without a sync is answered with 304
// before any query runs.
func indexETag(st *index.Store, r *http.Request) string {
	sum := sha256.Sum256([]byte(r.URL.EscapedPath() + "?" + r.URL.RawQuery))
	return `W/"` + st.Generation() + "-" + hex.EncodeToString(sum[:8]) + `"`
}

// notModified writes ETag and Cache-Control and reports whether a 304 was sent.
func notModified(w http.ResponseWriter, r *http.Request, etag string) bool {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, etag) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

func badParam(w http.ResponseWriter, r *http.Request, format string, args ...any) {
	writeError(w, r, http.StatusBadRequest, "PARAMETER_INVALID", fmt.Sprintf(format, args...), nil)
}

func parseLimit(q url.Values, def, maxv int) (int, error) {
	s := q.Get("limit")
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > maxv {
		return 0, fmt.Errorf("limit=%q: must be 1..%d", s, maxv)
	}
	return n, nil
}

func parseBool(q url.Values, key string) (bool, error) {
	switch s := q.Get(key); s {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("%s=%q: must be true or false", key, s)
	}
}

func parseCursor(q url.Values) (*index.Cursor, error) {
	s := q.Get("cursor")
	if s == "" {
		return nil, nil
	}
	c, err := index.DecodeCursor(s)
	if err != nil {
		return nil, fmt.Errorf("cursor: %w", err)
	}
	return &c, nil
}

func (h *handler) listComponents(w http.ResponseWriter, r *http.Request, st *index.Store) {
	if notModified(w, r, indexETag(st, r)) {
		return
	}
	q := r.URL.Query()
	lq := index.ListQuery{Q: q.Get("q"), Locations: q["location"], Connectors: q["connector"], Provider: q.Get("provider"), Categories: q["category"], Architectures: q["architecture"], Replication: q.Get("replication"), Sort: q.Get("sort")}
	var err error
	if lq.Limit, err = parseLimit(q, listLimitDefault, listLimitMax); err != nil {
		badParam(w, r, "%v", err)
		return
	}
	if lq.IncludePrerelease, err = parseBool(q, "includePrerelease"); err != nil {
		badParam(w, r, "%v", err)
		return
	}
	if lq.TopLevel, err = parseBool(q, "topLevel"); err != nil {
		badParam(w, r, "%v", err)
		return
	}
	if lq.Replication != "" && lq.Replication != "single" && lq.Replication != "replicated" {
		badParam(w, r, "replication=%q: must be single or replicated", lq.Replication)
		return
	}
	if lq.Sort == "" {
		lq.Sort = "relevance"
	}
	if lq.Sort != "relevance" && lq.Sort != "updated" && lq.Sort != "name" {
		badParam(w, r, "sort=%q: must be relevance, updated or name", lq.Sort)
		return
	}
	if s := q.Get("updatedSince"); s != "" {
		if lq.UpdatedSince, err = time.Parse(time.RFC3339, s); err != nil {
			badParam(w, r, "updatedSince=%q: must be RFC 3339", s)
			return
		}
	}
	if lq.Cursor, err = parseCursor(q); err != nil {
		badParam(w, r, "%v", err)
		return
	}
	res, err := st.ListComponents(r.Context(), lq)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	out := gen.ComponentList{Components: make([]gen.ComponentSummary, 0, len(res.Components)), Total: res.Total, Facets: genFacets(res.Facets)}
	for _, c := range res.Components {
		out.Components = append(out.Components, genSummary(c))
	}
	if res.Next != nil {
		next := index.EncodeCursor(*res.Next)
		out.Next = &next
	}
	writeJSON(w, r, out)
}

func (h *handler) component(w http.ResponseWriter, r *http.Request, st *index.Store, name string) {
	if notModified(w, r, indexETag(st, r)) {
		return
	}
	d, err := st.Component(r.Context(), name)
	if errors.Is(err, index.ErrNotFound) {
		err = fmt.Errorf("%w: %s", ocm.ErrComponentUnknown, name)
	}
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	s := genSummary(d.ComponentSummary)
	base := "/v1/components/" + escapeName(name)
	out := gen.Component{Name: s.Name, Latest: s.Latest, LatestPublishedAt: s.LatestPublishedAt, Title: s.Title,
		Description: s.Description, Icon: s.Icon, Categories: s.Categories, Provider: s.Provider, VersionCount: s.VersionCount,
		Locations: make([]gen.LocationSummary, 0, len(d.LocationDetails)),
		Sources:   make([]gen.Source, 0, len(d.Sources)), Links: gen.ComponentLinks{Versions: base + "/versions"},
		Signed: s.Signed, DefaultVersion: s.DefaultVersion, OwnerAvatarUrl: s.OwnerAvatarUrl, ReferencedBy: make([]gen.ParentRef, 0, len(d.ReferencedBy)), ReferencedByCount: d.ReferencedByCount}
	for _, p := range d.ReferencedBy {
		out.ReferencedBy = append(out.ReferencedBy, gen.ParentRef{Name: p.Name, Title: p.Title, Latest: optional(p.Latest)})
	}
	for _, l := range d.LocationDetails {
		out.Locations = append(out.Locations, gen.LocationSummary{Repository: l.Repository, Latest: optional(l.Latest), VersionCount: l.VersionCount})
	}
	for _, src := range d.Sources {
		groups := make([]gen.SourceGroup, 0, len(src.Groups))
		for _, g := range src.Groups {
			groups = append(groups, gen.SourceGroup{Key: g.Key, Title: g.Title, AvatarUrl: optional(g.AvatarURL), Locations: g.Locations})
		}
		out.Sources = append(out.Sources, gen.Source{Key: src.Key, Title: src.Title, Type: src.Type, AvatarUrl: optional(src.AvatarURL), Locations: src.Locations, Groups: groups})
	}
	if d.Latest != "" {
		link := base + "/descriptors/" + ocm.Latest
		out.Links.Latest = &link
	}
	writeJSON(w, r, out)
}

func (h *handler) versions(w http.ResponseWriter, r *http.Request, st *index.Store, name string) {
	if notModified(w, r, indexETag(st, r)) {
		return
	}
	q := r.URL.Query()
	vq := index.VersionsQuery{Component: name, Locations: q["location"]}
	var err error
	if vq.Limit, err = parseLimit(q, versionsLimitDefault, versionsLimitMax); err != nil {
		badParam(w, r, "%v", err)
		return
	}
	if vq.IncludePrerelease, err = parseBool(q, "includePrerelease"); err != nil {
		badParam(w, r, "%v", err)
		return
	}
	if s := q.Get("semverConstraint"); s != "" {
		if vq.Constraint, err = semver.NewConstraint(s); err != nil {
			badParam(w, r, "semverConstraint=%q: %v", s, err)
			return
		}
	}
	if vq.Cursor, err = parseCursor(q); err != nil {
		badParam(w, r, "%v", err)
		return
	}
	res, err := st.Versions(r.Context(), vq)
	if errors.Is(err, index.ErrNotFound) {
		err = fmt.Errorf("%w: %s", ocm.ErrComponentUnknown, name)
	}
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	out := gen.VersionList{Name: name, Versions: make([]gen.VersionEntry, 0, len(res.Versions))}
	for _, v := range res.Versions {
		out.Versions = append(out.Versions, gen.VersionEntry{Version: v.Version, PublishedAt: v.PublishedAt, Stable: v.Stable, Locations: v.Locations, HasChangelog: v.HasChangelog})
	}
	if res.Next != nil {
		next := index.EncodeCursor(*res.Next)
		out.Next = &next
	}
	writeJSON(w, r, out)
}

func genSummary(c index.ComponentSummary) gen.ComponentSummary {
	s := gen.ComponentSummary{Name: c.Name, Title: c.Title, Description: optional(c.Description), Icon: optional(c.Icon),
		Provider: optional(c.Provider), Categories: optionalSlice(c.Categories), VersionCount: c.VersionCount, Locations: c.Locations,
		Latest: optional(c.Latest), Signed: c.Signed, DefaultVersion: c.DefaultVersion, OwnerAvatarUrl: optional(c.OwnerAvatarURL)}
	if s.Locations == nil {
		s.Locations = []string{}
	}
	if !c.LatestPublishedAt.IsZero() {
		t := c.LatestPublishedAt
		s.LatestPublishedAt = &t
	}
	return s
}

func genFacets(f index.Facets) gen.Facets {
	conv := func(vs []index.FacetValue) []gen.FacetValue {
		out := make([]gen.FacetValue, 0, len(vs))
		for _, v := range vs {
			out = append(out, gen.FacetValue{Value: v.Value, Count: v.Count})
		}
		return out
	}
	conns := make([]gen.ConnectorFacet, 0, len(f.Connectors))
	for _, c := range f.Connectors {
		groups := make([]gen.GroupFacet, 0, len(c.Groups))
		for _, g := range c.Groups {
			groups = append(groups, gen.GroupFacet{Key: g.Key, Title: g.Title, AvatarUrl: optional(g.AvatarURL), Count: g.Count})
		}
		conns = append(conns, gen.ConnectorFacet{Key: c.Key, Title: c.Title, Type: c.Type, AvatarUrl: optional(c.AvatarURL), Count: c.Count, Groups: groups})
	}
	return gen.Facets{Connectors: conns, Providers: conv(f.Providers), Categories: conv(f.Categories), Architectures: conv(f.Architectures), Replication: conv(f.Replication)}
}

// writeJSON writes a 200 JSON body (ETag/Cache-Control already set).
func writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "encoding response failed", nil)
		return
	}
	writeBody(w, r, http.StatusOK, mediaJSON, body)
}
