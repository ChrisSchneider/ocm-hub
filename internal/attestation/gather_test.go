package attestation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"oras.land/oras-go/v2/registry/remote"

	"chrisschneider.dev/delivery-bundler/internal/document"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
)

func TestGatherFailedLevelKeepsOthers(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host, SubPath: "g"}
	fixtures.Push(ctx, t, target, fixtures.Component{Name: "acme.org/g", Version: "1.0.0+build.1"})
	fixtures.PushImage(ctx, t, target, "images/ok", "v1", []byte("layer"))
	fixtures.PushImage(ctx, t, target, "images/none", "v1", []byte("other"))

	// Attach an attestation to the component manifest, which has a '+' version.
	open := plainOpener(nil)
	compRepo := reg.Host + "/g/component-descriptors/acme.org/g"
	tag := oci.LooseSemverToOCITag(context.Background(), "1.0.0+build.1")
	comp := resolve(t, open, compRepo, tag)
	fixtures.PushReferrer(ctx, t, target, "g/component-descriptors/acme.org/g", comp,
		"application/vnd.dev.sigstore.bundle.v0.3+json",
		map[string]string{"dev.sigstore.bundle.predicateType": "https://slsa.dev/provenance/v1"},
		"application/vnd.dev.sigstore.bundle.v0.3+json", []byte(`{}`))
	okSubject := resolve(t, open, reg.Host+"/images/ok", "v1")
	fixtures.PushReferrer(ctx, t, target, "images/ok", okSubject, "application/vnd.acme.sbom",
		map[string]string{"in-toto.io/predicate-type": "https://spdx.dev/Document"}, "application/vnd.in-toto+json", []byte(`{}`))

	d := desc("1.0.0+build.1")
	d.Component.Name = "acme.org/g"
	d.Component.Resources = append(d.Component.Resources,
		res("ok", "1", "ociImage", nil, image(reg.Host+"/images/ok:v1")),
		res("none", "1", "ociImage", nil, image(reg.Host+"/images/none:v1")),
		res("denied", "1", "ociImage", nil, image(reg.Host+"/private/img:v1")),
		res("file", "1", "blob", nil, blob("text/plain", "sha256:cc")),
	)
	g := Gatherer{Open: func(ctx context.Context, repository string) (*remote.Repository, error) {
		if strings.Contains(repository, "/private/") {
			return nil, errors.New("401 Unauthorized")
		}
		return open(ctx, repository)
	}}
	r, warns := g.Gather(ctx, d, "oci::"+reg.Host+"/g")
	if len(warns) != 0 {
		t.Errorf("warnings: %v", warns)
	}
	if len(r.Levels) != 5 {
		t.Fatalf("levels = %+v", r.Levels)
	}
	if l := r.Levels[0]; l.Status != StatusOK || len(l.Entries) != 1 || l.Entries[0].Kind != KindSLSA {
		t.Errorf("component = %+v", l)
	}
	if l := r.Levels[1]; l.Status != StatusOK || len(l.Entries) != 1 || l.Entries[0].Kind != KindSBOM {
		t.Errorf("ok = %+v", l)
	}
	// TestGatherNothingFound: ok, empty and non-nil.
	if l := r.Levels[2]; l.Status != StatusOK || l.Entries == nil || len(l.Entries) != 0 {
		t.Errorf("none = %+v", l)
	}
	if l := r.Levels[3]; l.Status != StatusFailed || !strings.Contains(l.Error, "401") {
		t.Errorf("denied = %+v", l)
	}
	if l := r.Levels[4]; l.Status != StatusNotOCI {
		t.Errorf("file = %+v", l)
	}
}

func TestGatherNothingFound(t *testing.T) {
	reg := zot.Shared(t)
	fixtures.PushImage(t.Context(), t, fixtures.Target{Host: reg.Host}, "images/bare", "v1", []byte("bare"))
	d := desc("1.0.0")
	d.Component.Resources = append(d.Component.Resources, res("bare", "1", "ociImage", nil, image(reg.Host+"/images/bare:v1")))
	r, _ := Gatherer{Open: plainOpener(nil)}.Gather(t.Context(), d, "")
	if l := r.Levels[1]; l.Status != StatusOK || l.Entries == nil || len(l.Entries) != 0 {
		t.Errorf("level = %+v", l)
	}
}

func TestGatherSchemeImageReferenceMakesNoRequest(t *testing.T) {
	d := desc("1.0.0")
	d.Component.Resources = append(d.Component.Resources, res("api", "1", "ociImage", nil, image("http://evil.example/acme/api:1")))
	g := Gatherer{Open: func(_ context.Context, repository string) (*remote.Repository, error) {
		t.Errorf("opened %s", repository)
		return nil, errors.New("unexpected")
	}}
	r, _ := g.Gather(t.Context(), d, "")
	if l := r.Levels[1]; l.Status != StatusFailed || l.Error != "imageReference must not contain a scheme" {
		t.Errorf("level = %+v", l)
	}
}

func TestGatherTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	host := strings.TrimPrefix(srv.URL, "http://")

	d := desc("1.0.0")
	d.Component.Resources = append(d.Component.Resources, res("api", "1", "ociImage", nil, image(host+"/acme/api:1")))
	start := time.Now()
	r, _ := Gatherer{Open: plainOpener(nil), Timeout: 200 * time.Millisecond}.Gather(t.Context(), d, "")
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("gather took %s", el)
	}
	if l := r.Levels[1]; l.Status != StatusFailed || !strings.Contains(l.Error, "deadline exceeded") {
		t.Errorf("level = %+v", l)
	}
}

func TestGatherVEXLabel(t *testing.T) {
	const u = "https://example.com/vex.json"
	vex := readTestdata(t, "openvex.json")
	reg := zot.Shared(t)
	fixtures.Push(t.Context(), t, fixtures.Target{Host: reg.Host, SubPath: "vexlabel"}, fixtures.Component{Name: "acme.org/vex", Version: "1.0.0"})
	location := "oci::" + reg.Host + "/vexlabel"
	d := desc("1.0.0")
	d.Component.Name = "acme.org/vex"
	d.Component.Labels = []descriptorruntime.Label{lbl(LabelVEX, "v1", `{"url":"`+u+`"}`)}

	var fetched []string
	g := Gatherer{Open: plainOpener(nil), Fetch: func(_ context.Context, url string) ([]byte, error) {
		fetched = append(fetched, url)
		return vex, nil
	}}
	r, _ := g.Gather(t.Context(), d, location)
	if len(fetched) != 1 || fetched[0] != u {
		t.Errorf("fetched %v", fetched)
	}
	sum := sha256.Sum256(vex)
	es := r.Levels[0].Entries
	if r.Levels[0].Status != StatusOK || len(es) != 1 {
		t.Fatalf("level = %+v", r.Levels[0])
	}
	e := es[0]
	if e.Label != "catalog.chrisschneider.dev/vex" || e.Digest != "sha256:"+hex.EncodeToString(sum[:]) || e.Signed != SignedNo ||
		e.Summary == nil || e.Summary.Statements != 3 {
		t.Errorf("entry = %+v (summary %+v)", e, e.Summary)
	}

	g.Fetch = func(context.Context, string) ([]byte, error) { return nil, errors.New("boom") }
	r, _ = g.Gather(t.Context(), d, location)
	if l := r.Levels[0]; l.Status != StatusOK || len(l.Entries) != 1 || l.Entries[0].Digest != "" || l.Entries[0].Summary != nil {
		t.Errorf("failed fetch: level = %+v", l)
	}

	// Off the allow list: still listed, without digest or summary, logged at debug.
	var logs bytes.Buffer
	g.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	g.Fetch = func(_ context.Context, url string) ([]byte, error) { return nil, fmt.Errorf("%w: %s", document.ErrNotAllowed, url) }
	r, _ = g.Gather(t.Context(), d, location)
	if l := r.Levels[0]; l.Status != StatusOK || len(l.Entries) != 1 || l.Entries[0].Digest != "" || l.Entries[0].Summary != nil {
		t.Errorf("not allowed: level = %+v", l)
	}
	if out := logs.String(); !strings.Contains(out, "level=DEBUG") || !strings.Contains(out, u) {
		t.Errorf("log = %q", out)
	}
}
