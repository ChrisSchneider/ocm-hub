package fixtures

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// FileServer serves files by path (e.g. "/file.txt") for wget resources.
func FileServer(t testing.TB, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// HelmRepo serves a classic Helm chart repository with one chart.
func HelmRepo(t testing.TB, chart, version string) *httptest.Server {
	t.Helper()
	tgz := chartArchive(t, chart, version)
	file := fmt.Sprintf("%s-%s.tgz", chart, version)
	index := fmt.Sprintf(`apiVersion: v1
entries:
  %[1]s:
  - apiVersion: v2
    name: %[1]s
    version: %[2]s
    urls:
    - %[3]s
`, chart, version, file)
	return FileServer(t, map[string][]byte{"/index.yaml": []byte(index), "/" + file: tgz})
}

func chartArchive(t testing.TB, chart, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	chartYAML := []byte(fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\n", chart, version))
	if err := tw.WriteHeader(&tar.Header{Name: chart + "/Chart.yaml", Mode: 0o644, Size: int64(len(chartYAML))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(chartYAML); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
