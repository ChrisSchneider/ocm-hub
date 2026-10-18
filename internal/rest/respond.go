package rest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/rest/gen"
)

const (
	mediaYAML = "application/yaml"
	mediaJSON = "application/json"
)

// writeBody writes a complete body with Content-Length; HEAD gets headers only.
func writeBody(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

// etagFor is the strong ETag of a response body.
func etagFor(body []byte) string {
	sum := sha256.Sum256(body)
	return `"sha256:` + hex.EncodeToString(sum[:]) + `"`
}

// etagMatches implements If-None-Match's weak comparison (RFC 9110 13.1.2):
// "*" matches anything, otherwise any listed tag equal to etag ignoring W/.
func etagMatches(ifNoneMatch, etag string) bool {
	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, c := range strings.Split(ifNoneMatch, ",") {
		if strings.TrimPrefix(strings.TrimSpace(c), "W/") == want {
			return true
		}
	}
	return false
}

// writeCacheable writes a 200 with a strong ETag (the body's sha256) and
// Cache-Control: no-cache, or 304 Not Modified when If-None-Match names the
// same representation.
func writeCacheable(w http.ResponseWriter, r *http.Request, contentType string, body []byte) {
	etag := etagFor(body)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeBody(w, r, http.StatusOK, contentType, body)
}

// writeError writes the OCI error envelope. HEAD responses carry no body.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, detail map[string]any) {
	w.Header().Set("Cache-Control", "no-store")
	if detail == nil {
		detail = map[string]any{}
	}
	detail["requestId"] = w.Header().Get("X-Request-Id")
	if r.Method == http.MethodHead {
		w.WriteHeader(status)
		return
	}
	body, _ := json.Marshal(gen.ErrorResponse{Errors: []gen.ErrorItem{{
		Code: gen.ErrorCode(code), Message: message, Detail: detail,
	}}})
	writeBody(w, r, status, mediaJSON, body)
}

func statusFor(err error) (int, string) {
	switch {
	case errors.Is(err, index.ErrBadCursor):
		return http.StatusBadRequest, "PARAMETER_INVALID"
	case errors.Is(err, ocm.ErrRepositoryInvalid):
		return http.StatusBadRequest, "REPOSITORY_INVALID"
	case errors.Is(err, ocm.ErrRepositoryNotAllowed):
		return http.StatusBadRequest, "REPOSITORY_NOT_ALLOWED"
	case errors.Is(err, ocm.ErrComponentUnknown):
		return http.StatusNotFound, "COMPONENT_UNKNOWN"
	case errors.Is(err, ocm.ErrVersionUnknown):
		return http.StatusNotFound, "VERSION_UNKNOWN"
	case errors.Is(err, ocm.ErrResourceUnknown):
		return http.StatusNotFound, "RESOURCE_UNKNOWN"
	case errors.Is(err, ocm.ErrResourceAmbiguous):
		return http.StatusConflict, "RESOURCE_AMBIGUOUS"
	case errors.Is(err, ocm.ErrAccessTypeUnsupported):
		return http.StatusNotImplemented, "ACCESS_TYPE_UNSUPPORTED"
	case errors.Is(err, ocm.ErrUpstreamTimeout), errors.Is(err, context.DeadlineExceeded):
		// DeadlineExceeded: e.g. the first read of a resource body timing out.
		return http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT"
	default:
		return http.StatusBadGateway, "UPSTREAM_ERROR"
	}
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := statusFor(err)
	var detail map[string]any
	var amb *ocm.AmbiguousResourceError
	if errors.As(err, &amb) {
		detail = map[string]any{"candidates": amb.Candidates}
	}
	writeError(w, r, status, code, err.Error(), detail)
}

// negotiate picks the descriptor encoding from Accept: YAML by default.
func negotiate(accept string) (string, bool) {
	if strings.TrimSpace(accept) == "" {
		return mediaYAML, true
	}
	for _, part := range strings.Split(accept, ",") {
		switch strings.TrimSpace(strings.SplitN(part, ";", 2)[0]) {
		case mediaYAML, "application/x-yaml", "text/yaml", "*/*", "application/*":
			return mediaYAML, true
		case mediaJSON:
			return mediaJSON, true
		}
	}
	return "", false
}

// escapeName encodes a component name as one path segment ("/" as %2F),
// the form the API documents for clients.
func escapeName(name string) string { return url.PathEscape(name) }

// redirect answers 307 to /v1/components/<name>/<suffix>, keeping the query
// string. The name is sent percent-encoded as one segment.
func redirect(w http.ResponseWriter, r *http.Request, name, suffix string) {
	loc := "/v1/components/" + escapeName(name) + "/" + suffix
	if r.URL.RawQuery != "" {
		loc += "?" + r.URL.RawQuery
	}
	w.Header().Set("Location", loc)
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// optional returns nil for the zero value, so the field is omitted.
func optional[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// optionalSlice returns nil for an empty slice, so the field is omitted.
func optionalSlice[T any](v []T) *[]T {
	if len(v) == 0 {
		return nil
	}
	return &v
}
