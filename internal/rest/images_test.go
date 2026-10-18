package rest

import (
	"net/http"
	"net/url"
	"testing"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"

	"chrisschneider.dev/delivery-bundler/internal/rest/gen"
	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
)

func imageRes(name string, extra map[string]string, access ocmruntime.Typed) descriptorruntime.Resource {
	return descriptorruntime.Resource{
		ElementMeta: descriptorruntime.ElementMeta{
			ObjectMeta:    descriptorruntime.ObjectMeta{Name: name, Version: "1.1.0"},
			ExtraIdentity: extra,
		},
		Type: "ociImage", Access: access,
	}
}

func ociImage(ref string) ocmruntime.Typed {
	return &ocmruntime.Raw{Type: ocmruntime.NewUnversionedType("OCIImage"), Data: []byte(`{"type":"OCIImage/v1","imageReference":"` + ref + `"}`)}
}

// imageHandler serves a descriptor whose resources are res.
func imageHandler(t *testing.T, res ...descriptorruntime.Resource) *handler {
	t.Helper()
	d := &descriptorruntime.Descriptor{}
	d.Component.Name, d.Component.Version = "acme.org/x", "1.1.0"
	d.Component.Resources = res
	return NewHandler(&fakeService{desc: d}, testIndex(t), WithRegistry(plainOpener())).(*handler)
}

func imagePath(resource, digest, query string) string {
	p := "/v1/components/acme.org%2Fx/images/1.1.0/" + url.PathEscape(resource)
	if digest != "" {
		p += "/" + digest
	}
	p += "?repository=" + url.QueryEscape(testLoc)
	if query != "" {
		p += "&" + query
	}
	return p
}

const imageConfigJSON = `{"os":"linux","architecture":"amd64","created":"2026-01-02T03:04:05Z",` +
	`"config":{"User":"1000","Entrypoint":["/bin/app"],"ExposedPorts":{"8080/tcp":{}},"Labels":{"a":"b"}},` +
	`"history":[{"created_by":"ADD rootfs"}]}`

func TestImagePlatforms(t *testing.T) {
	reg := zot.Shared(t)
	d := fixtures.PushBuildxIndex(t.Context(), t, fixtures.Target{Host: reg.Host}, "rest/imgidx", "v1",
		[]string{"linux/amd64", "linux/arm64"}, []string{"https://spdx.dev/Document"})
	h := imageHandler(t, imageRes("img", nil, ociImage(reg.Host+"/rest/imgidx:v1")))
	r := do(t, h, "GET", imagePath("img", "", ""), nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d: %s", r.StatusCode, body(t, r))
	}
	idx := decode[gen.ImageIndex](t, r)
	if idx.Repository != reg.Host+"/rest/imgidx" || idx.Digest != d.Digest.String() || idx.MediaType != d.MediaType ||
		len(idx.Platforms) != 2 || idx.Platforms[0].Platform != "linux/amd64" || idx.Platforms[1].Platform != "linux/arm64" ||
		idx.Platforms[0].Digest == "" {
		t.Fatalf("index = %+v", idx)
	}
	if r := do(t, h, "GET", imagePath("img", "", "raw=config"), nil); r.StatusCode != 400 || errCode(t, r) != "PARAMETER_INVALID" {
		t.Errorf("raw on the index: %d", r.StatusCode)
	}
}

func TestImageDetails(t *testing.T) {
	reg := zot.Shared(t)
	m := fixtures.PushImageConfig(t.Context(), t, fixtures.Target{Host: reg.Host}, "rest/imgdet", "v1", []byte(imageConfigJSON), []byte("layer"))
	h := imageHandler(t,
		imageRes("img", map[string]string{"arch": "amd64"}, ociImage(reg.Host+"/rest/imgdet:v1")),
		imageRes("img", map[string]string{"arch": "arm64"}, ociImage(reg.Host+"/rest/other:v1")))
	r := do(t, h, "GET", imagePath("img", m.Digest.String(), "extraIdentity.arch=amd64"), nil)
	if r.StatusCode != 200 {
		t.Fatalf("status %d: %s", r.StatusCode, body(t, r))
	}
	d := decode[gen.ImageDetails](t, r)
	if d.Digest != m.Digest.String() || d.Platform != "linux/amd64" || d.Size != int64(len(imageConfigJSON)+len("layer")) ||
		d.User == nil || *d.User != "1000" || d.Created == nil || *d.Created != "2026-01-02T03:04:05Z" ||
		d.Entrypoint == nil || (*d.Entrypoint)[0] != "/bin/app" || d.ExposedPorts == nil || (*d.ExposedPorts)[0] != "8080/tcp" ||
		d.Labels == nil || (*d.Labels)["a"] != "b" || len(d.Layers) != 1 || d.Layers[0].CreatedBy == nil || *d.Layers[0].CreatedBy != "ADD rootfs" {
		t.Fatalf("details = %+v", d)
	}
	// Without the extra identity the name alone matches no resource.
	if r := do(t, h, "GET", imagePath("img", m.Digest.String(), ""), nil); r.StatusCode != 404 || errCode(t, r) != "RESOURCE_UNKNOWN" {
		t.Errorf("no extra identity: %d", r.StatusCode)
	}
}

func TestImageRawConfig(t *testing.T) {
	reg := zot.Shared(t)
	m := fixtures.PushImageConfig(t.Context(), t, fixtures.Target{Host: reg.Host}, "rest/imgraw", "v1", []byte(imageConfigJSON), []byte("layer"))
	h := imageHandler(t, imageRes("img", nil, ociImage(reg.Host+"/rest/imgraw:v1")))
	hex12 := m.Digest.Encoded()[:12]
	r := do(t, h, "GET", imagePath("img", m.Digest.String(), "raw=config"), nil)
	if r.StatusCode != 200 || !rawHeaders(r, hex12+"-config.json") || body(t, r) != imageConfigJSON {
		t.Fatalf("config: %d %v", r.StatusCode, r.Header)
	}
	r = do(t, h, "GET", imagePath("img", m.Digest.String(), "raw=manifest"), nil)
	if r.StatusCode != 200 || !rawHeaders(r, hex12+"-manifest.json") {
		t.Fatalf("manifest: %d %v", r.StatusCode, r.Header)
	}
	if r := do(t, h, "GET", imagePath("img", m.Digest.String(), "raw=layers"), nil); r.StatusCode != 400 || errCode(t, r) != "PARAMETER_INVALID" {
		t.Errorf("raw=layers: %d", r.StatusCode)
	}
}

// rawHeaders reports whether r is served as a JSON attachment named filename
// that browsers must not sniff.
func rawHeaders(r *http.Response, filename string) bool {
	return r.Header.Get("Content-Type") == "application/json" &&
		r.Header.Get("Content-Disposition") == `attachment; filename="`+filename+`"` &&
		r.Header.Get("X-Content-Type-Options") == "nosniff"
}

// A config the publisher pushed as text/html is still served as a JSON
// attachment, never rendered.
func TestImageRawPublisherMediaType(t *testing.T) {
	reg := zot.Shared(t)
	html := `{"os":"linux","architecture":"amd64","x":"<script>alert(1)</script>"}`
	m := fixtures.PushImageConfigAs(t.Context(), t, fixtures.Target{Host: reg.Host}, "rest/imghtml", "v1", "text/html", []byte(html))
	h := imageHandler(t, imageRes("img", nil, ociImage(reg.Host+"/rest/imghtml:v1")))
	r := do(t, h, "GET", imagePath("img", m.Digest.String(), "raw=config"), nil)
	if r.StatusCode != 200 || !rawHeaders(r, m.Digest.Encoded()[:12]+"-config.json") || body(t, r) != html {
		t.Fatalf("config: %d %v", r.StatusCode, r.Header)
	}
}

func TestImageNotOCI(t *testing.T) {
	lb := &ocmruntime.Raw{Type: ocmruntime.NewUnversionedType("localBlob"), Data: []byte(`{"type":"localBlob/v1","localReference":"sha256:abc","mediaType":"application/gzip"}`)}
	h := imageHandler(t, imageRes("chart", nil, lb))
	for _, p := range []string{imagePath("chart", "", ""), imagePath("chart", "sha256:abc", "")} {
		if r := do(t, h, "GET", p, nil); r.StatusCode != 422 || errCode(t, r) != "NOT_OCI" {
			t.Errorf("%s: %d", p, r.StatusCode)
		}
	}
	if r := do(t, h, "GET", imagePath("nope", "", ""), nil); r.StatusCode != 404 || errCode(t, r) != "RESOURCE_UNKNOWN" {
		t.Errorf("unknown resource: %d", r.StatusCode)
	}
}

func TestImageUnknownDigest(t *testing.T) {
	reg := zot.Shared(t)
	fixtures.PushBuildxIndex(t.Context(), t, fixtures.Target{Host: reg.Host}, "rest/imgunk", "v1", []string{"linux/amd64"}, []string{"https://spdx.dev/Document"})
	h := imageHandler(t, imageRes("img", nil, ociImage(reg.Host+"/rest/imgunk:v1")))
	r := do(t, h, "GET", imagePath("img", "sha256:0000000000000000000000000000000000000000000000000000000000000000", ""), nil)
	if r.StatusCode != 404 || errCode(t, r) != "NOT_FOUND" {
		t.Errorf("status %d", r.StatusCode)
	}
}

func TestImageRegistryErrors(t *testing.T) {
	h := imageHandler(t, imageRes("img", nil, ociImage("127.0.0.1:1/none:v1")))
	if r := do(t, h, "GET", imagePath("img", "", ""), nil); r.StatusCode != 502 || errCode(t, r) != "UPSTREAM_ERROR" {
		t.Errorf("unreachable: %d", r.StatusCode)
	}
	d := &descriptorruntime.Descriptor{}
	d.Component.Resources = []descriptorruntime.Resource{imageRes("img", nil, ociImage("127.0.0.1:1/none:v1"))}
	h = NewHandler(&fakeService{desc: d}, testIndex(t)).(*handler)
	if r := do(t, h, "GET", imagePath("img", "", ""), nil); r.StatusCode != 501 || errCode(t, r) != "NOT_IMPLEMENTED" {
		t.Errorf("no registry: %d", r.StatusCode)
	}
}

func TestImageLatestRedirect(t *testing.T) {
	h := imageHandler(t, imageRes("img", nil, ociImage("127.0.0.1:1/none:v1")))
	r := do(t, h, "GET", "/v1/components/acme.org%2Fx/images/latest/img/sha256:abc?raw=config", nil)
	if r.StatusCode != 307 || r.Header.Get("Location") != "/v1/components/acme.org%2Fx/images/1.1.0/img/sha256:abc?raw=config" {
		t.Errorf("%d %s", r.StatusCode, r.Header.Get("Location"))
	}
}
