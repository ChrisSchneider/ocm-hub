package document

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// maxRedirects bounds the redirects followed for one fetch.
const maxRedirects = 5

// defaultTimeout bounds one Fetch when Fetcher.Timeout is 0.
const defaultTimeout = 30 * time.Second

// FetchFunc fetches a label-linked document.
type FetchFunc func(ctx context.Context, url string) ([]byte, error)

// Fetcher fetches documents whose URLs are on its allow list, including
// every redirect hop. The list is the only gate: a listed URL may resolve to
// a private address.
type Fetcher struct {
	Client  *http.Client  // nil: a shared client without proxy
	Timeout time.Duration // bounds one Fetch, redirects and body included; 0: 30s
	Allow   []string      // see ValidateAllow; empty: nothing is fetched

	once  sync.Once
	allow allowList
	err   error
}

// Fetch returns the body at rawURL, at most MaxSize bytes, within f.Timeout
// (or ctx's deadline, if sooner). A URL off the allow list fails with
// ErrNotAllowed before any request is made.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	f.once.Do(func() { f.allow, f.err = compileAllow(f.Allow) })
	if f.err != nil {
		return nil, f.err
	}
	ctx, cancel := context.WithTimeout(ctx, f.timeout())
	defer cancel()
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if !f.allow.allows(u) {
		return nil, fmt.Errorf("%w: %s", ErrNotAllowed, rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("fetching %s: %s", rawURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxSize {
		return nil, ErrTooLarge
	}
	return body, nil
}

func (f *Fetcher) timeout() time.Duration {
	if f.Timeout > 0 {
		return f.Timeout
	}
	return defaultTimeout
}

// sharedTransport is built once and shared by every Fetcher without a
// Client, so connections are pooled. It uses no proxy.
var sharedTransport = sync.OnceValue(func() *http.Transport {
	return &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConns:        16,
	}
})

// client returns a copy of the configured (or the shared) client with the
// allow list and redirect cap applied to every hop.
func (f *Fetcher) client() *http.Client {
	var c http.Client
	if f.Client != nil {
		c = *f.Client
	} else {
		c.Transport = sharedTransport()
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if !f.allow.allows(req.URL) {
			return fmt.Errorf("refusing redirect: %w: %s", ErrNotAllowed, req.URL)
		}
		return nil
	}
	return &c
}
