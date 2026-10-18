package document

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// everything allows any path on ts's host and port.
func everything(ts *httptest.Server) []string {
	_, hostPort, _ := strings.Cut(ts.URL, "://")
	return []string{hostPort + "/**"}
}

func TestFetcherReadsHTTPS(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer ts.Close()
	b, err := (&Fetcher{Client: ts.Client(), Allow: everything(ts)}).Fetch(context.Background(), ts.URL+"/a/b.vex")
	if err != nil || string(b) != "hello" {
		t.Fatalf("got %q, %v", b, err)
	}
}

func TestFetcherRefusesUnlisted(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer ts.Close()
	_, hostPort, _ := strings.Cut(ts.URL, "://")
	for name, allow := range map[string][]string{
		"empty list":   nil,
		"other path":   {hostPort + "/vex/*.json"},
		"other host":   {"example.com/**"},
		"deeper match": {hostPort + "/*"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := (&Fetcher{Client: ts.Client(), Allow: allow}).Fetch(context.Background(), ts.URL+"/docs/a.vex")
			if !errors.Is(err, ErrNotAllowed) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatal("server was hit")
	}
}

// http needs a pattern starting with http://. A listed URL may resolve to a
// private address: the shared transport (no Client) dials 127.0.0.1.
func TestFetcherHTTPOnlyWhenListed(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("plain"))
	}))
	defer ts.Close()
	_, hostPort, _ := strings.Cut(ts.URL, "://")
	for _, p := range []string{hostPort + "/**", "https://" + hostPort + "/**"} {
		if _, err := (&Fetcher{Allow: []string{p}}).Fetch(context.Background(), ts.URL+"/x.json"); !errors.Is(err, ErrNotAllowed) {
			t.Fatalf("%s: got %v", p, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("server was hit")
	}
	b, err := (&Fetcher{Allow: []string{"http://" + hostPort + "/*.json"}}).Fetch(context.Background(), ts.URL+"/x.json")
	if err != nil || string(b) != "plain" {
		t.Fatalf("got %q, %v", b, err)
	}
}

func TestAllowListMatch(t *testing.T) {
	l, err := compileAllow([]string{
		"example.com/**/*.vex",
		"*.example.com/**/*.vex",
		"http://security.internal.corp/vex/*.json",
		"https://Secure.Example.org/a/*",
		"ports.example.net:8443/*",
	})
	if err != nil {
		t.Fatal(err)
	}
	for u, want := range map[string]bool{
		"https://example.com/a.vex":                       true,
		"https://example.com/a/b/c.vex":                   true,
		"https://EXAMPLE.com/a/b.vex?x=1#frag":            true,
		"https://example.com/a/b.json":                    false,
		"https://example.com/a/../../x.vex":               false, // dot segments are refused, not cleaned
		"https://example.com/a/./x.vex":                   false,
		"https://example.com/a/%2e%2e/x.vex":              false,
		"https://example.com/a/..%2F..%2Fvex/x.vex":       false,
		"https://example.com/a/..%2f/x.vex":               false,
		"https://example.com/a%5Cb/x.vex":                 false,
		"https://example.com/a/%61.vex":                   true, // matched as sent
		"https://example.com./a.vex":                      false,
		"https://example.com:443/a.vex":                   false,
		"https://example.com/vex/../secret.json":          false,
		"https://a.example.com/x.vex":                     true,
		"https://a.b.example.com/x.vex":                   true,
		"https://evilexample.com/x.vex":                   false,
		"http://example.com/a.vex":                        false,
		"http://security.internal.corp/vex/a.json":        true,
		"https://security.internal.corp/vex/a.json":       true,
		"http://security.internal.corp/vex/a/b.json":      false,
		"https://secure.example.org/a/b":                  true,
		"http://secure.example.org/a/b":                   false,
		"https://ports.example.net:8443/x":                true,
		"https://ports.example.net/x":                     false,
		"ftp://example.com/a.vex":                         false,
		"https://user@example.com/a.vex":                  true,
		"https://example.com.attacker.net/a.vex":          false,
		"https://example.com%2F@attacker.net/example.vex": false,
	} {
		if got := l.allows(mustParse(t, u)); got != want {
			t.Errorf("%s: allowed = %v, want %v", u, got, want)
		}
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// A wildcard host never matches the apex.
func TestAllowListWildcardHostOnly(t *testing.T) {
	l, err := compileAllow([]string{"*.example.com/**"})
	if err != nil {
		t.Fatal(err)
	}
	if l.allows(mustParse(t, "https://example.com/x.vex")) || !l.allows(mustParse(t, "https://a.example.com/x.vex")) {
		t.Error("*.example.com must match subdomains only")
	}
}

// The path as sent is what is matched: a server would resolve
// /docs/..%2Fsecret outside /docs/*.
func TestFetcherRefusesEncodedTraversal(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer ts.Close()
	_, hostPort, _ := strings.Cut(ts.URL, "://")
	f := &Fetcher{Client: ts.Client(), Allow: []string{hostPort + "/docs/**"}}
	for _, p := range []string{"/docs/..%2Fsecret", "/docs/../secret", "/docs/%2e%2e/secret"} {
		if _, err := f.Fetch(context.Background(), ts.URL+p); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: got %v", p, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("server was hit")
	}
}

func TestCompileAllowErrors(t *testing.T) {
	for _, p := range []string{"", "example.com/[a", "ftp://example.com/x", "https://", "example.com"} {
		if err := ValidateAllow([]string{p}); err == nil || !strings.Contains(err.Error(), "allow[0]") {
			t.Errorf("%q: %v", p, err)
		}
	}
}

func TestFetcherRefusesHTTPRedirect(t *testing.T) {
	var hits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer plain.Close()
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer ts.Close()
	_, plainHost, _ := strings.Cut(plain.URL, "://")
	allow := append(everything(ts), plainHost+"/**") // https only for plain
	if _, err := (&Fetcher{Client: ts.Client(), Allow: allow}).Fetch(context.Background(), ts.URL); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("got %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("redirect target was hit")
	}
}

func TestFetcherRefusesUnlistedRedirect(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/secret" {
			hits.Add(1)
			return
		}
		http.Redirect(w, r, "/secret", http.StatusFound)
	}))
	defer ts.Close()
	_, hostPort, _ := strings.Cut(ts.URL, "://")
	_, err := (&Fetcher{Client: ts.Client(), Allow: []string{hostPort + "/docs/*"}}).Fetch(context.Background(), ts.URL+"/docs/a.vex")
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("got %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("redirect target was hit")
	}
}

func TestFetcherTooLarge(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", MaxSize+1)))
	}))
	defer ts.Close()
	_, err := (&Fetcher{Client: ts.Client(), Allow: everything(ts)}).Fetch(context.Background(), ts.URL)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestFetcherStatus(t *testing.T) {
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	defer ts.Close()
	_, err := (&Fetcher{Client: ts.Client(), Allow: everything(ts)}).Fetch(context.Background(), ts.URL)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("got %v", err)
	}
}

func TestFetcherRedirectCap(t *testing.T) {
	var hits atomic.Int32
	var ts *httptest.Server
	ts = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, ts.URL, http.StatusFound)
	}))
	defer ts.Close()
	_, err := (&Fetcher{Client: ts.Client(), Allow: everything(ts)}).Fetch(context.Background(), ts.URL)
	if err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Fatalf("got %v", err)
	}
	if hits.Load() != 6 {
		t.Fatalf("hits = %d, want 6 (1 + 5 redirects)", hits.Load())
	}
}

func TestFetcherTimeout(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer ts.Close()
	defer close(release)
	start := time.Now()
	_, err := (&Fetcher{Client: ts.Client(), Allow: everything(ts), Timeout: 50 * time.Millisecond}).Fetch(context.Background(), ts.URL)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("got %v after %v", err, time.Since(start))
	}
	if got := (&Fetcher{}).timeout(); got != 30*time.Second {
		t.Errorf("default timeout = %v", got)
	}
}

// The transport is built once and shared, so connections are reused. It
// ignores the environment's proxy settings.
func TestFetcherSharesTransport(t *testing.T) {
	a, b := (&Fetcher{}).client(), (&Fetcher{Timeout: time.Second}).client()
	if a.Transport == nil || a.Transport != b.Transport {
		t.Fatalf("transports %p %p", a.Transport, b.Transport)
	}
	tr := a.Transport.(*http.Transport)
	if tr.IdleConnTimeout == 0 {
		t.Error("no idle connection timeout")
	}
	if tr.Proxy != nil {
		t.Error("proxy from the environment")
	}
}
