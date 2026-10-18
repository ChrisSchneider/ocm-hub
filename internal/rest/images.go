package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/image"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/rest/gen"
)

// image answers GET /v1/components/{name}/images/{version}/{resource}[/{digest}]:
// the platforms of the OCI image a resource names, or one platform's details
// (?raw= its manifest or config). The registry is read on request, never
// from the index; the reference comes from the descriptor, not the client.
func (h *handler) image(w http.ResponseWriter, r *http.Request, st *index.Store, route Route) {
	q := r.URL.Query()
	raw := q.Get("raw")
	switch {
	case raw != "" && raw != "manifest" && raw != "config":
		badParam(w, r, "raw=%q: must be manifest or config", raw)
		return
	case raw != "" && route.Kind != RouteImageDetails:
		badParam(w, r, "raw applies to a platform digest")
		return
	}
	if route.Version == ocm.Latest {
		v, err := h.latest(r.Context(), st, route.Name)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		suffix := "images/" + url.PathEscape(v) + "/" + url.PathEscape(route.Resource)
		if route.Digest != "" {
			suffix += "/" + url.PathEscape(route.Digest)
		}
		w.Header().Set("Cache-Control", "no-cache")
		redirect(w, r, route.Name, suffix)
		return
	}
	identity, err := resourceIdentity(route.Resource, q)
	if err != nil {
		badParam(w, r, "%v", err)
		return
	}
	loc, err := h.locate(r.Context(), st, route.Name, route.Version, q.Get("repository"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	desc, err := h.svc.GetComponentVersion(r.Context(), ocm.Ref{Component: route.Name, Version: route.Version, Repository: loc})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	subject, err := attestation.ResourceSubject(desc, loc, identity)
	switch {
	case errors.Is(err, attestation.ErrResourceUnknown):
		writeError(w, r, http.StatusNotFound, "RESOURCE_UNKNOWN", fmt.Sprintf("%s:%s has no resource %v", route.Name, route.Version, identity), nil)
		return
	case err != nil:
		writeServiceError(w, r, err)
		return
	case subject == (attestation.Subject{}):
		writeError(w, r, http.StatusUnprocessableEntity, "NOT_OCI", fmt.Sprintf("resource %q is not an OCI image", route.Resource), nil)
		return
	case h.open == nil:
		writeError(w, r, http.StatusNotImplemented, "NOT_IMPLEMENTED", "reading images from registries is not configured", nil)
		return
	}
	switch {
	case route.Kind == RouteImage:
		idx, err := image.Platforms(r.Context(), h.open, subject)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		out := gen.ImageIndex{Repository: idx.Repository, Digest: idx.Digest, MediaType: idx.MediaType,
			Platforms: make([]gen.ImagePlatform, 0, len(idx.Platforms))}
		for _, p := range idx.Platforms {
			out.Platforms = append(out.Platforms, gen.ImagePlatform{Platform: p.Platform, Digest: p.Digest})
		}
		writeCacheableJSON(w, r, out)
	case raw != "":
		b, err := image.Raw(r.Context(), h.open, subject, route.Digest, raw)
		if err != nil {
			writeImageError(w, r, err)
			return
		}
		// The registry's media type is publisher-chosen: always a JSON
		// attachment, never sniffed, so nothing renders in the hub's origin.
		w.Header().Set("Content-Disposition", `attachment; filename="`+rawFilename(route.Digest, raw)+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		writeCacheable(w, r, mediaJSON, b)
	default:
		d, err := image.Inspect(r.Context(), h.open, subject, route.Digest)
		if err != nil {
			writeImageError(w, r, err)
			return
		}
		writeCacheableJSON(w, r, genImageDetails(d))
	}
}

// rawFilename is "<first 12 hex of the digest>-<part>.json". Raw has matched
// the digest to a platform the registry returned.
func rawFilename(dgst, part string) string {
	return strings.TrimSuffix(contentFilename(dgst), ".json") + "-" + part + ".json"
}

// writeImageError maps an unknown platform digest to 404; anything else is
// an upstream error.
func writeImageError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, image.ErrUnknownPlatform) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
		return
	}
	writeServiceError(w, r, err)
}

func genImageDetails(d image.Details) gen.ImageDetails {
	out := gen.ImageDetails{Digest: d.Digest, Platform: d.Platform, Size: d.Size,
		Created: optional(d.Created), User: optional(d.User), WorkingDir: optional(d.WorkingDir),
		Entrypoint: optionalSlice(d.Entrypoint), Cmd: optionalSlice(d.Cmd),
		ExposedPorts: optionalSlice(d.ExposedPorts), Env: optionalSlice(d.Env),
		Layers: make([]gen.ImageLayer, 0, len(d.Layers))}
	if len(d.Labels) > 0 {
		out.Labels = &d.Labels
	}
	for _, l := range d.Layers {
		out.Layers = append(out.Layers, gen.ImageLayer{Digest: l.Digest, Size: l.Size, CreatedBy: optional(l.CreatedBy)})
	}
	return out
}

// writeCacheableJSON answers v as JSON with a body ETag.
func writeCacheableJSON(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "encoding response failed", nil)
		return
	}
	writeCacheable(w, r, mediaJSON, body)
}
