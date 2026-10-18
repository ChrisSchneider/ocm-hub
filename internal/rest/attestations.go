package rest

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/rest/gen"
)

// supplyChain answers GET /v1/components/{name}/supply-chain/{version}: the
// set gathered by a sync, from the index only. Sets are looked up exactly at
// ?repository=; without it the route redirects (307) to the location the
// other version routes resolve, so a URL always names one set. `latest`
// redirects to the concrete version, adding the location in the same step.
func (h *handler) supplyChain(w http.ResponseWriter, r *http.Request, st *index.Store, route Route) {
	if h.redirectSupplyChain(w, r, st, route) {
		return
	}
	if notModified(w, r, indexETag(st, r)) {
		return
	}
	set, ok, err := h.attestationSet(r, st, route)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !ok {
		h.notCollected(w, r, st, route)
		return
	}
	writeJSON(w, r, genSupplyChain(route.Name, set))
}

// redirectSupplyChain answers the 307s of the supply-chain routes and
// reports whether it did: without ?repository= to the location the other
// version routes resolve, and `latest` to the concrete version (adding the
// location in the same step). The digest of a content route is kept.
func (h *handler) redirectSupplyChain(w http.ResponseWriter, r *http.Request, st *index.Store, route Route) bool {
	pinned := r.URL.Query().Get("repository")
	version := route.Version
	if version == ocm.Latest {
		v, err := h.latest(r.Context(), st, route.Name)
		if err != nil {
			writeServiceError(w, r, err)
			return true
		}
		version = v
	}
	if pinned != "" && version == route.Version {
		return false
	}
	q := r.URL.Query()
	if pinned == "" {
		loc, err := h.locate(r.Context(), st, route.Name, version, "")
		if errors.Is(err, ocm.ErrVersionUnknown) {
			h.notCollected(w, r, st, Route{Kind: route.Kind, Name: route.Name, Version: version})
			return true
		}
		if err != nil {
			writeServiceError(w, r, err)
			return true
		}
		q.Set("repository", loc)
	}
	path := "/v1/components/" + escapeName(route.Name) + "/supply-chain/" + url.PathEscape(version)
	if route.Digest != "" {
		path += "/" + url.PathEscape(route.Digest)
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Location", path+"?"+q.Encode())
	w.WriteHeader(http.StatusTemporaryRedirect)
	return true
}

// attestationSet reads the set stored exactly at ?repository=; ok is false
// when none is stored there.
func (h *handler) attestationSet(r *http.Request, st *index.Store, route Route) (set index.AttestationSet, ok bool, err error) {
	repo, err := h.locate(r.Context(), st, route.Name, route.Version, r.URL.Query().Get("repository"))
	if err != nil {
		return index.AttestationSet{}, false, err
	}
	return st.Attestations(r.Context(), route.Name, route.Version, repo)
}

// notCollected answers 404: COMPONENT_UNKNOWN for an unknown component, else
// NOT_COLLECTED listing what has been gathered.
func (h *handler) notCollected(w http.ResponseWriter, r *http.Request, st *index.Store, route Route) {
	if _, err := st.Component(r.Context(), route.Name); errors.Is(err, index.ErrNotFound) {
		writeServiceError(w, r, fmt.Errorf("%w: %s", ocm.ErrComponentUnknown, route.Name))
		return
	} else if err != nil {
		writeServiceError(w, r, err)
		return
	}
	targets, err := st.CollectedAttestations(r.Context(), route.Name)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	collected := make([]map[string]string, 0, len(targets))
	for _, t := range targets {
		collected = append(collected, map[string]string{"version": t.Version, "repository": t.Repository})
	}
	writeError(w, r, http.StatusNotFound, "NOT_COLLECTED",
		fmt.Sprintf("supply chain documents of %s:%s are not collected; only the default version is gathered", route.Name, route.Version),
		map[string]any{"collected": collected})
}

func genSupplyChain(name string, s index.AttestationSet) gen.SupplyChain {
	out := gen.SupplyChain{Name: name, Version: s.Version, Repository: s.Repository, CheckedAt: s.CheckedAt,
		Levels: make([]gen.SupplyChainLevel, 0, len(s.Result.Levels))}
	for _, l := range s.Result.Levels {
		gl := gen.SupplyChainLevel{Resource: genIdentity(l.Resource), Status: gen.SupplyChainLevelStatus(l.Status),
			Error: optional(l.Error), Entries: make([]gen.SupplyChainEntry, 0, len(l.Entries))}
		for _, e := range l.Entries {
			gl.Entries = append(gl.Entries, gen.SupplyChainEntry{
				Kind: gen.SupplyChainEntryKind(e.Kind), Source: gen.SupplyChainEntrySource(e.Source), Signed: gen.SupplyChainEntrySigned(e.Signed),
				Format: optional(e.Format), Type: optional(e.Type), Platform: optional(e.Platform), Digest: optional(e.Digest),
				Resource: genIdentity(e.Resource), Url: optional(e.URL), BuildLevel: optional(e.BuildLevel),
				Builder: optional(e.Builder), Label: optional(e.Label), Summary: genDocumentSummary(e.Summary)})
		}
		out.Levels = append(out.Levels, gl)
	}
	return out
}

func genIdentity(id *attestation.Identity) *gen.ResourceIdentity {
	if id == nil {
		return nil
	}
	out := &gen.ResourceIdentity{Name: id.Name, Version: optional(id.Version)}
	if len(id.ExtraIdentity) > 0 {
		out.ExtraIdentity = &id.ExtraIdentity
	}
	return out
}

func genDocumentSummary(s *document.Summary) *gen.DocumentSummary {
	if s == nil {
		return nil
	}
	return &gen.DocumentSummary{Format: s.Format, Tool: optional(s.Tool), ImportantBasis: optional(s.ImportantBasis),
		Packages: optional(s.Packages), Statements: optional(s.Statements), Affected: optional(s.Affected),
		Findings: optional(s.Findings), ImportantFindings: optional(s.ImportantFindings),
		Requirements: optional(s.Requirements), NotSatisfied: optional(s.NotSatisfied)}
}
