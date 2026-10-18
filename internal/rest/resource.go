package rest

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

var bytesStreamed, _ = otel.Meter("chrisschneider.dev/delivery-bundler/internal/rest").
	Int64Counter("ocm_server.resource.bytes_streamed", metric.WithUnit("By"))

func (h *handler) resource(w http.ResponseWriter, r *http.Request, st *index.Store, route Route) {
	if route.Version == ocm.Latest {
		v, err := h.latest(r.Context(), st, route.Name)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		redirect(w, r, route.Name, "resources/"+url.PathEscape(v)+"/"+url.PathEscape(route.Resource))
		return
	}
	identity, err := resourceIdentity(route.Resource, r.URL.Query())
	var asDocument bool
	if err == nil {
		asDocument, err = parseAs(r.URL.Query())
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "PARAMETER_INVALID", err.Error(), nil)
		return
	}
	loc, err := h.locate(r.Context(), st, route.Name, route.Version, r.URL.Query().Get("repository"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ref := ocm.Ref{Component: route.Name, Version: route.Version, Repository: loc}
	if asDocument {
		h.resourceDocument(w, r, ref, identity)
		return
	}
	// HEAD does not open the body: only metadata is needed.
	withBody := r.Method != http.MethodHead
	res, err := h.svc.OpenResource(r.Context(), ref, identity, withBody)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if res.Body != nil {
		defer res.Body.Close()
	}

	// Peek before committing the status so a failure on the first read is a
	// normal error response; io.EOF just means an empty resource.
	var src io.Reader = res.Body
	if withBody {
		br := bufio.NewReader(res.Body)
		if _, err := br.Peek(1); err != nil && !errors.Is(err, io.EOF) {
			writeServiceError(w, r, err)
			return
		}
		src = br
	}

	hdr := w.Header()
	contentType := res.MediaType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	hdr.Set("Content-Type", contentType)
	if res.Size >= 0 {
		hdr.Set("Content-Length", strconv.FormatInt(res.Size, 10))
	}
	hdr.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": res.Name}))
	if res.Digest != "" {
		// The OCM descriptor digest; for non-local access types it is not a
		// hash of the served bytes, so it is a strong ETag only for localBlob.
		hdr.Set("OCM-Resource-Digest", res.Digest)
		if res.DigestMatchesBody {
			hdr.Set("ETag", `"`+res.Digest+`"`)
		}
	}
	hdr.Set("Cache-Control", "no-cache")
	if etag := hdr.Get("ETag"); etag != "" {
		if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, etag) {
			hdr.Del("Content-Length")
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	if !withBody {
		return
	}
	// Send the headers now so a later mid-stream abort reaches the client as a
	// broken body rather than a failed request.
	_ = http.NewResponseController(w).Flush()
	n, err := io.Copy(w, src)
	bytesStreamed.Add(r.Context(), n)
	if err != nil {
		// The status is already sent: abort the connection so the client sees
		// a broken download instead of a short "successful" one.
		slog.ErrorContext(r.Context(), "resource download aborted mid-stream", slog.String("error", err.Error()))
		panic(http.ErrAbortHandler)
	}
}

// resourceDocument answers ?as=document: the resource read whole (at most
// document.MaxSize) and parsed. HEAD reads it too, for the same headers.
func (h *handler) resourceDocument(w http.ResponseWriter, r *http.Request, ref ocm.Ref, identity ocmruntime.Identity) {
	res, err := h.svc.OpenResource(r.Context(), ref, identity, true)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	defer res.Body.Close()
	// One byte over the cap lets Parse report ErrTooLarge.
	b, err := io.ReadAll(io.LimitReader(res.Body, document.MaxSize+1))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeDocument(w, r, b)
}

// resourceIdentity builds the requested identity from the resource name and
// extraIdentity.<key>=<value> query parameters.
func resourceIdentity(name string, q url.Values) (ocmruntime.Identity, error) {
	id := ocmruntime.Identity{"name": name}
	for k, vs := range q {
		key, ok := strings.CutPrefix(k, "extraIdentity.")
		if !ok {
			continue
		}
		if key == "" || key == "name" || len(vs) != 1 {
			return nil, fmt.Errorf("invalid parameter %q", k)
		}
		id[key] = vs[0]
	}
	return id, nil
}
