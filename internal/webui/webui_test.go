package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, method, path string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	res := rec.Result()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

var built = fstest.MapFS{
	"index.html":          {Data: []byte("<!doctype html><title>OCM Hub</title>")},
	"assets/index-abc.js": {Data: []byte("console.log(1)")},
	"favicon.svg":         {Data: []byte("<svg/>")},
	".keep":               {Data: nil},
}

func TestUIServesIndexForClientRoutes(t *testing.T) {
	h := newHandler(built)
	for _, p := range []string{"/", "/index.html", "/components/ocm.software/how-to/product", "/components/x?version=1.0"} {
		res, body := get(t, h, "GET", p)
		if res.StatusCode != 200 || !strings.Contains(body, "OCM Hub") || res.Header.Get("Cache-Control") != "no-cache" ||
			!strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
			t.Errorf("%s: %d %q %v", p, res.StatusCode, body, res.Header)
		}
	}
}

func TestUIAssetsAreImmutable(t *testing.T) {
	res, body := get(t, newHandler(built), "GET", "/assets/index-abc.js")
	if res.StatusCode != 200 || body != "console.log(1)" || res.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("%d %q %v", res.StatusCode, body, res.Header)
	}
	res, _ = get(t, newHandler(built), "GET", "/favicon.svg")
	if res.StatusCode != 200 || res.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("favicon: %d %v", res.StatusCode, res.Header)
	}
}

func TestUIMissingAssetIs404(t *testing.T) {
	if res, _ := get(t, newHandler(built), "GET", "/assets/index-old.js"); res.StatusCode != 404 {
		t.Fatalf("stale asset: %d", res.StatusCode)
	}
}

func TestUIPlaceholderWithoutBuild(t *testing.T) {
	res, body := get(t, newHandler(fstest.MapFS{".keep": {}}), "GET", "/")
	if res.StatusCode != 200 || !strings.Contains(body, "UI not built") {
		t.Fatalf("%d %q", res.StatusCode, body)
	}
}

func TestUIMethods(t *testing.T) {
	if res, body := get(t, newHandler(built), "HEAD", "/"); res.StatusCode != 200 || body != "" {
		t.Fatalf("HEAD: %d %q", res.StatusCode, body)
	}
	if res, _ := get(t, newHandler(built), "POST", "/"); res.StatusCode != 405 || res.Header.Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST: %d", res.StatusCode)
	}
}

func TestEmbeddedHandlerServes(t *testing.T) {
	// Works with or without a build: either the app or the placeholder.
	if res, _ := get(t, Handler(), "GET", "/"); res.StatusCode != 200 {
		t.Fatalf("%d", res.StatusCode)
	}
}
