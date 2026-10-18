package image

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"oras.land/oras-go/v2/registry/remote"

	"chrisschneider.dev/delivery-bundler/internal/attestation"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
)

func TestMain(m *testing.M) { zot.Main(m) }

func plainOpener(_ context.Context, repository string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(repository)
	if err != nil {
		return nil, err
	}
	repo.PlainHTTP = true
	return repo, nil
}

func TestPlatformsIndex(t *testing.T) {
	reg := zot.Shared(t)
	target := fixtures.Target{Host: reg.Host}
	d := fixtures.PushBuildxIndex(t.Context(), t, target, "img/index", "v1",
		[]string{"linux/amd64", "linux/arm64"}, []string{"https://spdx.dev/Document"})
	idx, err := Platforms(t.Context(), plainOpener, attestation.Subject{Repository: reg.Host + "/img/index", Reference: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if idx.Repository != reg.Host+"/img/index" || idx.Digest != d.Digest.String() || idx.MediaType != d.MediaType {
		t.Errorf("index = %+v", idx)
	}
	if len(idx.Platforms) != 2 || idx.Platforms[0].Platform != "linux/amd64" || idx.Platforms[1].Platform != "linux/arm64" {
		t.Errorf("platforms = %+v", idx.Platforms)
	}
}

func TestPlatformsSingleManifest(t *testing.T) {
	reg := zot.Shared(t)
	d := fixtures.PushImageConfig(t.Context(), t, fixtures.Target{Host: reg.Host}, "img/single", "v1",
		[]byte(`{"os":"linux","architecture":"arm64"}`), []byte("layer"))
	idx, err := Platforms(t.Context(), plainOpener, attestation.Subject{Repository: reg.Host + "/img/single", Reference: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Platform{{Platform: "linux/arm64", Digest: d.Digest.String()}}
	if !reflect.DeepEqual(idx.Platforms, want) {
		t.Errorf("platforms = %+v, want %+v", idx.Platforms, want)
	}
}

// Index children without an os or architecture are left out. A single
// manifest always is one platform, "" when its config lacks os or arch.
func TestPlatformsWithoutOSOrArch(t *testing.T) {
	reg := zot.Shared(t)
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushBuildxIndex(t.Context(), t, target, "img/noplat", "v1", []string{"/", "linux/", "/amd64", "linux/arm64"}, nil)
	idx, err := Platforms(t.Context(), plainOpener, attestation.Subject{Repository: reg.Host + "/img/noplat", Reference: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Platforms) != 1 || idx.Platforms[0].Platform != "linux/arm64" {
		t.Errorf("index platforms = %+v", idx.Platforms)
	}
	for _, cfg := range []string{`{}`, `{"os":"linux"}`, `{"architecture":"amd64"}`} {
		d := fixtures.PushImageConfig(t.Context(), t, target, "img/noplat", "single", []byte(cfg))
		s := attestation.Subject{Repository: reg.Host + "/img/noplat", Reference: "single"}
		idx, err := Platforms(t.Context(), plainOpener, s)
		if err != nil {
			t.Fatal(err)
		}
		want := []Platform{{Platform: "", Digest: d.Digest.String()}}
		if !reflect.DeepEqual(idx.Platforms, want) {
			t.Errorf("config %s: platforms = %+v, want %+v", cfg, idx.Platforms, want)
		}
		got, err := Inspect(t.Context(), plainOpener, s, d.Digest.String())
		if err != nil || got.Digest != d.Digest.String() || got.Platform != "" {
			t.Errorf("config %s: inspect = %+v, %v", cfg, got, err)
		}
		if b, err := Raw(t.Context(), plainOpener, s, d.Digest.String(), "config"); err != nil || string(b) != cfg {
			t.Errorf("config %s: raw = %q, %v", cfg, b, err)
		}
	}
}

func TestInspectDetails(t *testing.T) {
	reg := zot.Shared(t)
	config := []byte(`{"os":"linux","architecture":"amd64","created":"2026-01-02T03:04:05Z",
"config":{"User":"1000","WorkingDir":"/app","Entrypoint":["/bin/app"],"Cmd":["--serve"],
"Env":["A=1","B=2"],"ExposedPorts":{"8080/tcp":{},"53/udp":{}},"Labels":{"org.opencontainers.image.title":"x"}},
"history":[{"created_by":"ADD rootfs"},{"created_by":"ENV A=1","empty_layer":true},{"created_by":"COPY app /app"}]}`)
	l1, l2 := []byte("layer-one"), []byte("layer-two!")
	d := fixtures.PushImageConfig(t.Context(), t, fixtures.Target{Host: reg.Host}, "img/details", "v1", config, l1, l2)
	s := attestation.Subject{Repository: reg.Host + "/img/details", Reference: "v1"}
	got, err := Inspect(t.Context(), plainOpener, s, d.Digest.String())
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != d.Digest.String() || got.Platform != "linux/amd64" || got.Created != "2026-01-02T03:04:05Z" ||
		got.User != "1000" || got.WorkingDir != "/app" {
		t.Errorf("details = %+v", got)
	}
	if got.Size != int64(len(config)+len(l1)+len(l2)) {
		t.Errorf("size = %d", got.Size)
	}
	if !reflect.DeepEqual(got.Entrypoint, []string{"/bin/app"}) || !reflect.DeepEqual(got.Cmd, []string{"--serve"}) ||
		!reflect.DeepEqual(got.Env, []string{"A=1", "B=2"}) || !reflect.DeepEqual(got.ExposedPorts, []string{"53/udp", "8080/tcp"}) ||
		got.Labels["org.opencontainers.image.title"] != "x" {
		t.Errorf("config = %+v", got)
	}
	if len(got.Layers) != 2 || got.Layers[0].CreatedBy != "ADD rootfs" || got.Layers[1].CreatedBy != "COPY app /app" ||
		got.Layers[0].Size != int64(len(l1)) || got.Layers[1].Digest == "" {
		t.Errorf("layers = %+v", got.Layers)
	}

	b, err := Raw(t.Context(), plainOpener, s, d.Digest.String(), "config")
	if err != nil || string(b) != string(config) {
		t.Errorf("raw config: %q %v", b, err)
	}
	b, err = Raw(t.Context(), plainOpener, s, d.Digest.String(), "manifest")
	if err != nil || len(b) == 0 {
		t.Errorf("raw manifest: %q %v", b, err)
	}
	if _, err := Raw(t.Context(), plainOpener, s, d.Digest.String(), "bogus"); err == nil {
		t.Error("unknown part accepted")
	}
}

func TestInspectUnknownDigest(t *testing.T) {
	reg := zot.Shared(t)
	target := fixtures.Target{Host: reg.Host}
	fixtures.PushBuildxIndex(t.Context(), t, target, "img/unknown", "v1", []string{"linux/amd64"}, []string{"https://spdx.dev/Document"})
	other := fixtures.PushImageConfig(t.Context(), t, target, "img/unknown", "other", []byte(`{"os":"linux","architecture":"amd64"}`))
	s := attestation.Subject{Repository: reg.Host + "/img/unknown", Reference: "v1"}
	for _, dg := range []string{other.Digest.String(), "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"} {
		if _, err := Inspect(t.Context(), plainOpener, s, dg); !errors.Is(err, ErrUnknownPlatform) {
			t.Errorf("Inspect(%s) = %v", dg, err)
		}
		if _, err := Raw(t.Context(), plainOpener, s, dg, "config"); !errors.Is(err, ErrUnknownPlatform) {
			t.Errorf("Raw(%s) = %v", dg, err)
		}
	}
}
