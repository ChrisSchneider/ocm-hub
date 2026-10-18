package rest

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/image"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/overview"
	"chrisschneider.dev/delivery-bundler/internal/rest/gen"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// overviewImageTimeout bounds the live image platform reads of an overview.
const overviewImageTimeout = 3 * time.Second

// versionOverview answers GET /v1/components/{name}/overview/{version}: the
// descriptor read live (like the descriptor route) and turned into an overview.
func (h *handler) versionOverview(w http.ResponseWriter, r *http.Request, st *index.Store, route Route) {
	if route.Version == ocm.Latest {
		v, err := h.latest(r.Context(), st, route.Name)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		redirect(w, r, route.Name, "overview/"+url.PathEscape(v))
		return
	}
	loc, err := h.locate(r.Context(), st, route.Name, route.Version, r.URL.Query().Get("repository"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	d, err := h.svc.GetComponentVersion(r.Context(), ocm.Ref{Component: route.Name, Version: route.Version, Repository: loc})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ov := overview.FromDescriptor(d, h.ov)
	for _, msg := range ov.Warnings {
		slog.DebugContext(r.Context(), "label skipped", slog.String("component", route.Name), slog.String("version", route.Version), slog.String("warning", msg))
	}
	archs, read := h.architectures(r.Context(), d, loc, ov.Architectures)
	out, err := genOverview(route.Name, route.Version, loc, ov, d)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "converting descriptor: "+err.Error(), nil)
		return
	}
	out.ImagePlatformsRead = read
	for _, a := range archs {
		from := make([]gen.ArchitectureFrom, len(a.From))
		for i, f := range a.From {
			from[i] = gen.ArchitectureFrom(f)
		}
		out.Architectures = append(out.Architectures, gen.Architecture{Platform: a.Platform, From: from})
	}
	if st != nil {
		if t, ok, err := st.VersionPublishedAt(r.Context(), route.Name, route.Version); err != nil {
			writeServiceError(w, r, err)
			return
		} else if ok {
			out.PublishedAt = &t
		}
	}
	body, err := json.Marshal(out)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "encoding overview: "+err.Error(), nil)
		return
	}
	writeCacheable(w, r, mediaJSON, body)
}

// architectures merges the platforms of d's OCI images, read within
// overviewImageTimeout, into the descriptor's. read is false when any image
// read failed or no registry is configured; failed images add nothing.
func (h *handler) architectures(ctx context.Context, d *descriptorruntime.Descriptor, loc string, ocm []overview.Architecture) ([]overview.Architecture, bool) {
	if h.open == nil {
		return overview.MergeArchitectures(ocm, nil), false
	}
	ctx, cancel := context.WithTimeout(ctx, overviewImageTimeout)
	defer cancel()
	images, errs := image.ResourcePlatforms(ctx, h.open, d, loc)
	for _, err := range errs {
		slog.DebugContext(ctx, "image platforms not read", slog.String("component", d.Component.Name), slog.String("version", d.Component.Version), slog.String("error", err.Error()))
	}
	return overview.MergeArchitectures(ocm, images), len(errs) == 0
}

// genOverview builds the response; resources, references, sources and
// signatures are the v2 descriptor's, unchanged (OCM binding types).
func genOverview(name, version, loc string, ov overview.Overview, d *descriptorruntime.Descriptor) (gen.ComponentVersionOverview, error) {
	v2, err := toV2(d, false)
	if err != nil {
		return gen.ComponentVersionOverview{}, err
	}
	disp := ov.Display.WithFallbackTitle(name)
	out := gen.ComponentVersionOverview{
		Name: name, Version: version, Repository: loc, Title: disp.Title,
		Description: optional(disp.Description), Icon: optional(disp.Icon), Categories: optionalSlice(disp.Categories),
		Provider: optional(ov.Provider), Signed: ov.Signed,
		Responsibles:  make([]gen.Responsible, 0, len(ov.Responsibles)),
		Docs:          make([]gen.DocLink, 0, len(ov.Docs)),
		Contents:      gen.ContentsSummary{Resources: make([]gen.TypeCount, 0, len(ov.Resources)), References: len(ov.References)},
		Architectures: []gen.Architecture{},
		Resources:     nonNil(v2.Component.Resources),
		References:    nonNil(v2.Component.References),
		Sources:       nonNil(v2.Component.Sources),
		Signatures:    nonNil(v2.Signatures),
	}
	for _, rs := range ov.Responsibles {
		out.Responsibles = append(out.Responsibles, gen.Responsible{
			Type: gen.ResponsibleType(rs.Type), Username: optional(rs.Username), Teamname: optional(rs.Teamname), GithubHostname: optional(rs.GitHubHostname)})
	}
	for _, dl := range ov.Docs {
		var kind *gen.DocLinkKind
		if dl.Kind != "" {
			k := gen.DocLinkKind(dl.Kind)
			kind = &k
		}
		out.Docs = append(out.Docs, gen.DocLink{Title: dl.Title, Url: dl.URL, Kind: kind})
	}
	for _, rc := range ov.Resources {
		out.Contents.Resources = append(out.Contents.Resources, gen.TypeCount{Type: rc.Type, Count: rc.Count})
	}
	if k := ov.Kubernetes; k != nil {
		out.Kubernetes = &gen.KubernetesCompatibility{Constraint: k.Constraint, Versions: nonNil(k.Versions), Distributions: nonNil(k.Distributions)}
	}
	if len(ov.Storage) > 0 {
		storage := make([]gen.StorageNeed, 0, len(ov.Storage))
		for _, n := range ov.Storage {
			storage = append(storage, gen.StorageNeed{AccessMode: gen.StorageNeedAccessMode(n.AccessMode), VolumeMode: gen.StorageNeedVolumeMode(n.VolumeMode), Purpose: optional(n.Purpose)})
		}
		out.Storage = &storage
	}
	if rp := ov.RiskProfile; rp != nil {
		out.RiskProfile = &gen.RiskProfile{
			NetworkExposure:            enumPtr[gen.RiskProfileNetworkExposure](rp.NetworkExposure),
			AuthenticationEnforced:     rp.AuthenticationEnforced,
			UserInteraction:            enumPtr[gen.RiskProfileUserInteraction](rp.UserInteraction),
			ConfidentialityRequirement: enumPtr[gen.RiskProfileConfidentialityRequirement](rp.ConfidentialityRequirement),
			IntegrityRequirement:       enumPtr[gen.RiskProfileIntegrityRequirement](rp.IntegrityRequirement),
			AvailabilityRequirement:    enumPtr[gen.RiskProfileAvailabilityRequirement](rp.AvailabilityRequirement),
			Comment:                    optional(rp.Comment),
		}
	}
	return out, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// enumPtr converts a string to an enum type, nil when empty.
func enumPtr[E ~string](s string) *E {
	if s == "" {
		return nil
	}
	e := E(s)
	return &e
}
