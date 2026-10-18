package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/opencontainers/go-digest"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/rest/gen"
)

// supplyChainDocument answers GET /v1/components/{name}/supply-chain/{version}/{digest}:
// the document of one entry of the stored set, as bytes or (?as=document)
// parsed. The digest is looked up in the set first, so only what a sync
// recorded is ever read or fetched.
func (h *handler) supplyChainDocument(w http.ResponseWriter, r *http.Request, st *index.Store, route Route) {
	asDocument, err := parseAs(r.URL.Query())
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "PARAMETER_INVALID", err.Error(), nil)
		return
	}
	if h.redirectSupplyChain(w, r, st, route) {
		return
	}
	set, ok, err := h.attestationSet(r, st, route)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	e, found := findEntry(set.Result, route.Digest)
	if !ok || !found {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND",
			fmt.Sprintf("%s:%s has no supply chain document %s at %s", route.Name, route.Version, route.Digest, r.URL.Query().Get("repository")), nil)
		return
	}
	// Content panics without the opener or fetcher an entry needs.
	switch e.Source {
	case attestation.SourceLabelLink:
		if h.fetch == nil {
			writeError(w, r, http.StatusNotImplemented, "NOT_IMPLEMENTED", "fetching linked documents is not configured", nil)
			return
		}
	case attestation.SourceReferrer, attestation.SourceCosignTag, attestation.SourceBuildx:
		if h.open == nil {
			writeError(w, r, http.StatusNotImplemented, "NOT_IMPLEMENTED", "reading attestations from registries is not configured", nil)
			return
		}
	}
	b, err := attestation.Content(r.Context(), h.open, h.fetch, e)
	if err != nil {
		writeContentError(w, r, err, e.URL)
		return
	}
	if asDocument {
		writeDocument(w, r, b)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+contentFilename(e.Digest)+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writeCacheable(w, r, mediaJSON, b)
}

// findEntry returns the first entry of res with digest dgst.
func findEntry(res attestation.Result, dgst string) (attestation.Entry, bool) {
	for _, l := range res.Levels {
		for _, e := range l.Entries {
			if e.Digest == dgst {
				return e, true
			}
		}
	}
	return attestation.Entry{}, false
}

// contentFilename is "<first 12 hex of the digest>.json". Content has
// already parsed the digest (or matched a computed sha256), so it is hex.
func contentFilename(dgst string) string {
	hex := digest.Digest(dgst).Encoded()
	if len(hex) > 12 {
		hex = hex[:12]
	}
	return hex + ".json"
}

// parseAs reads ?as=: absent (the bytes) or document.
func parseAs(q url.Values) (bool, error) {
	switch s := q.Get("as"); s {
	case "":
		return false, nil
	case "document":
		return true, nil
	default:
		return false, fmt.Errorf("as=%q: must be document", s)
	}
}

// writeDocument parses b and answers the Document schema.
func writeDocument(w http.ResponseWriter, r *http.Request, b []byte) {
	d, err := document.Parse(b)
	if err != nil {
		writeContentError(w, r, err, "")
		return
	}
	body, err := json.Marshal(genDocument(d))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "encoding document: "+err.Error(), nil)
		return
	}
	writeCacheable(w, r, mediaJSON, body)
}

// writeContentError maps document and content errors; anything else is an
// upstream error.
func writeContentError(w http.ResponseWriter, r *http.Request, err error, link string) {
	switch {
	case errors.Is(err, attestation.ErrNoContent):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
	case errors.Is(err, attestation.ErrDocumentChanged):
		writeError(w, r, http.StatusConflict, "DOCUMENT_CHANGED", err.Error(), map[string]any{"url": link})
	case errors.Is(err, document.ErrNotAllowed):
		writeError(w, r, http.StatusForbidden, "DOCUMENT_NOT_ALLOWED", "the linked document is not on the allow list", map[string]any{"url": link})
	case errors.Is(err, document.ErrTooLarge):
		writeError(w, r, http.StatusUnprocessableEntity, "DOCUMENT_TOO_LARGE", fmt.Sprintf("%v: the limit is %d bytes", err, document.MaxSize), nil)
	case errors.Is(err, document.ErrUnsupported):
		writeError(w, r, http.StatusUnprocessableEntity, "UNSUPPORTED_DOCUMENT", err.Error(), nil)
	default:
		writeServiceError(w, r, err)
	}
}

func genDocument(d document.Document) gen.Document {
	out := gen.Document{Format: d.Format}
	if d.SBOM != nil {
		s := gen.DocumentSBOM{Packages: make([]gen.SBOMPackage, 0, len(d.SBOM.Packages))}
		for _, p := range d.SBOM.Packages {
			s.Packages = append(s.Packages, gen.SBOMPackage{Name: p.Name, Version: optional(p.Version),
				License: optional(p.License), Type: optional(p.Type)})
		}
		out.Sbom = &s
	}
	if d.VEX != nil {
		v := gen.DocumentVEX{Statements: make([]gen.VEXStatement, 0, len(d.VEX.Statements))}
		for _, st := range d.VEX.Statements {
			products := st.Products
			if products == nil {
				products = []string{}
			}
			v.Statements = append(v.Statements, gen.VEXStatement{Vulnerability: st.Vulnerability, Products: products,
				Status: st.Status, Justification: optional(st.Justification), Action: optional(st.Action)})
		}
		out.Vex = &v
	}
	if d.Scan != nil {
		s := gen.DocumentScan{Tool: d.Scan.Tool, ImportantBasis: d.Scan.ImportantBasis,
			Findings: make([]gen.ScanFinding, 0, len(d.Scan.Findings))}
		for _, f := range d.Scan.Findings {
			s.Findings = append(s.Findings, gen.ScanFinding{Rule: f.Rule, Severity: optional(f.Severity),
				Level: optional(f.Level), Location: optional(f.Location), Message: f.Message})
		}
		out.Scan = &s
	}
	if d.Assessment != nil {
		a := gen.DocumentAssessment{Title: optional(d.Assessment.Title), Findings: make([]gen.AssessmentFinding, 0, len(d.Assessment.Findings))}
		for _, f := range d.Assessment.Findings {
			gf := gen.AssessmentFinding{Target: optional(f.Target), Title: f.Title, Description: optional(f.Description),
				State: f.State, Reason: optional(f.Reason), Remarks: optional(f.Remarks), Implementation: optional(f.Implementation),
				Source: optional(f.Source), Observations: make([]gen.AssessmentObservation, 0, len(f.Observations))}
			for _, o := range f.Observations {
				evidence := o.Evidence
				if evidence == nil {
					evidence = []string{}
				}
				gf.Observations = append(gf.Observations, gen.AssessmentObservation{Title: o.Title,
					Description: optional(o.Description), Result: optional(o.Result), Evidence: evidence})
			}
			a.Findings = append(a.Findings, gf)
		}
		out.Assessment = &a
	}
	return out
}
