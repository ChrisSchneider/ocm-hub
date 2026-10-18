package rest_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"os"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"oras.land/oras-go/v2/registry/remote"

	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/connector"
	"chrisschneider.dev/delivery-bundler/internal/index"
	"chrisschneider.dev/delivery-bundler/internal/indexsync"
	"chrisschneider.dev/delivery-bundler/internal/observability"
	"chrisschneider.dev/delivery-bundler/internal/ocm"
	"chrisschneider.dev/delivery-bundler/internal/rest"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
	"chrisschneider.dev/delivery-bundler/internal/version"
)

type attEntry struct {
	Kind, Format, Type, How, Platform, Signed, URL string
	Resource                                       *struct{ Name string }
}

type attLevel struct {
	Resource *struct{ Name string }
	Status   string
	Error    string
	Entries  []attEntry
}

// TestExampleComponentAttestationsE2E pushes examples/components into zot (the
// images copied there with attestations attached), syncs the index with the real
// gatherer and checks the attestations route.
func TestExampleComponentAttestationsE2E(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	const name = "github.com/chrisschneider/example-with-opencode-images"
	sub := fmt.Sprintf("att-%d", time.Now().UnixNano())
	target := fixtures.Target{Host: reg.Host, SubPath: sub + "/main"}

	comps := fixtures.LoadConstructor(t, "../../examples/components/component-constructor.yaml",
		map[string]string{"VERSION": "1.1.0", "K8S_MAX": "1.34", "SOURCE_COMMIT": "0123456789abcdef"})
	if len(comps) != 1 || comps[0].Name != name {
		t.Fatalf("components: %+v", comps)
	}
	comp := comps[0]

	// Copies of the two images, with attestations attached.
	alpineRepo, pyRepo := sub+"/images/alpine", sub+"/images/python3"
	fixtures.PushImage(ctx, t, target, alpineRepo, "3.24.0", []byte("alpine layer"))
	ar, err := remote.NewRepository(reg.Host + "/" + alpineRepo)
	must(t, err)
	ar.PlainHTTP = true
	alpine, err := ar.Resolve(ctx, "3.24.0")
	must(t, err)
	fixtures.PushReferrer(ctx, t, target, alpineRepo, alpine, "application/vnd.dev.sigstore.bundle.v0.3+json",
		map[string]string{"dev.sigstore.bundle.predicateType": "https://slsa.dev/provenance/v1"},
		"application/vnd.dev.sigstore.bundle.v0.3+json", []byte(`{}`))
	py := fixtures.PushBuildxIndex(ctx, t, target, pyRepo, "3.14-minimal", []string{"linux/amd64"},
		[]string{"https://spdx.dev/Document", "https://slsa.dev/provenance/v0.2"})
	fixtures.PushCosignAtt(ctx, t, target, pyRepo, py.Digest, "https://cosign.sigstore.dev/attestation/vuln/v1")

	refs := map[string]string{
		"alpine":  reg.Host + "/" + alpineRepo + "@" + alpine.Digest.String(),
		"python3": reg.Host + "/" + pyRepo + "@" + py.Digest.String(),
	}
	for i, r := range comp.Resources {
		if ref, ok := refs[r.Name]; ok {
			comp.Resources[i].Access = &ociaccessv1.OCIImage{Type: ocmruntime.NewVersionedType("OCIImage", "v1"), ImageReference: ref}
		}
	}
	fixtures.Push(ctx, t, target, comp)

	cfg, err := ocm.LoadOCMConfig(fixtures.WriteOCMConfig(t, []fixtures.Resolver{{Pattern: "github.com/*", Host: reg.Host, SubPath: sub + "/main"}}, nil))
	must(t, err)
	b, err := ocm.NewBindings(ctx, cfg, t.TempDir())
	must(t, err)
	r, err := ocm.NewResolver(ctx, b)
	must(t, err)
	svc := ocm.NewService(b, r, 30*time.Second)
	loc, err := ocm.LocationKey("oci::http://" + reg.Host + "/" + sub + "/main")
	must(t, err)
	conn := staticConn{found: []connector.ComponentVersions{{Component: name, Repository: loc,
		Versions: []connector.Version{{Name: "1.1.0", PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}}}}
	db := filepath.Join(t.TempDir(), "hub.duckdb")
	cl, _ := version.NewClassifier(nil)
	rep, err := indexsync.Run(ctx, indexsync.Options{DBPath: db, Connectors: []connector.Connector{conn}, Classifier: cl, Fetcher: svc,
		Attestations: attestation.Gatherer{Open: b.RepositoryOpener()}})
	if err != nil || rep.Failed() || rep.AttestationFailures != 0 {
		t.Fatalf("sync: %+v %v", rep, err)
	}
	h := index.NewHandle(db)
	t.Cleanup(func() { h.Close() })
	srv := httptest.NewServer(observability.Middleware(rest.NewHandler(svc, h), observability.NewLogger(io.Discard, slog.LevelError)))
	t.Cleanup(srv.Close)

	spec, err := os.ReadFile("../../api/openapi.yaml")
	must(t, err)
	doc, err := libopenapi.NewDocument(spec)
	must(t, err)
	v, verrs := validator.NewValidator(doc)
	if len(verrs) > 0 {
		t.Fatal(verrs)
	}
	s := &stack{srv: srv, validator: v}
	resp, body := s.do(t, "GET", "/v1/components/github.com%2Fchrisschneider%2Fexample-with-opencode-images/attestations/1.1.0", nil, "", false)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	var set struct{ Levels []attLevel }
	must(t, json.Unmarshal(body, &set))
	levels := map[string]attLevel{"": {}}
	for _, l := range set.Levels {
		key := ""
		if l.Resource != nil {
			key = l.Resource.Name
		}
		levels[key] = l
		if l.Status == "failed" {
			t.Errorf("level %q failed: %s", key, l.Error)
		}
	}
	if len(set.Levels) != 5 {
		t.Fatalf("levels = %d, want 5 (component, alpine, python3, changelog, docs): %s", len(set.Levels), body)
	}
	const redhat = "https://security.access.redhat.com/data/csaf/v2/vex/"
	find := func(level string, match func(attEntry) bool) *attEntry {
		for _, e := range levels[level].Entries {
			if match(e) {
				return &e
			}
		}
		t.Errorf("level %q: no matching entry in %+v", level, levels[level].Entries)
		return nil
	}

	if e := find("", func(e attEntry) bool { return e.How == "labelLink" }); e != nil && (e.Kind != "vex" || e.URL != redhat) {
		t.Errorf("component VEX: %+v", e)
	}
	embedded := func(_, resource string) func(attEntry) bool {
		return func(e attEntry) bool { return e.How == "embedded" && e.Resource != nil && e.Resource.Name == resource }
	}
	if e := find("", embedded("", "component-sbom")); e != nil && (e.Kind != "sbom" || e.Format == "") {
		t.Errorf("component SBOM: %+v", e)
	}
	if e := find("alpine", embedded("alpine", "alpine-sbom")); e != nil && (e.Kind != "sbom" || e.Format != "CycloneDX") {
		t.Errorf("alpine SBOM: %+v", e)
	}
	if e := find("alpine", func(e attEntry) bool { return e.How == "referrer" }); e != nil && (e.Kind != "slsa" || e.Signed != "signed" || e.Type != "https://slsa.dev/provenance/v1") {
		t.Errorf("alpine SLSA: %+v", e)
	}

	if e := find("python3", func(e attEntry) bool { return e.How == "labelLink" }); e != nil && (e.Kind != "vex" || e.URL != "https://api.devguard.opencode.de/api/v1/public/edbe3483-e769-424c-bd25-b88b44164715/refs/oc-13/artifacts/pkg%3Aoci%2Fpython3%3Frepository_url%3Dregistry.opencode.de%2Foci-community%2Fimages%2Fzendis%2Fpython3%26arch%3Damd64%26tag%3D3.10-oc.13-amd64/vex.json/") {
		t.Errorf("python3 VEX: %+v", e)
	}
	var buildx []attEntry
	for _, e := range levels["python3"].Entries {
		if e.How == "buildx" {
			buildx = append(buildx, e)
		}
	}
	if len(buildx) != 2 {
		t.Errorf("python3 buildx entries: %+v", buildx)
	}
	kinds := map[string]bool{}
	for _, e := range buildx {
		if e.Platform != "linux/amd64" {
			t.Errorf("buildx platform %q", e.Platform)
		}
		kinds[e.Kind] = true
	}
	if !kinds["sbom"] || !kinds["slsa"] {
		t.Errorf("buildx kinds %v, want sbom and slsa", kinds)
	}
	if e := find("python3", func(e attEntry) bool { return e.How == "cosignTag" }); e != nil && (e.Kind != "cve" || e.Type != "https://cosign.sigstore.dev/attestation/vuln/v1") {
		t.Errorf("python3 CVE: %+v", e)
	}

	for _, n := range []string{"changelog", "docs"} {
		if l := levels[n]; l.Status != "notOci" || len(l.Entries) != 0 {
			t.Errorf("%s: %+v, want notOci without entries", n, l)
		}
	}
}
