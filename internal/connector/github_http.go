package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// get fetches u into out and returns the rel="next" link. When the rate
// limit is exhausted it waits for the reset and retries.
func (g *GitHub) get(ctx context.Context, u string, out any) (string, error) {
	resp, err := g.do(ctx, func() (*http.Request, error) { return http.NewRequest(http.MethodGet, u, nil) })
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := decodeOK(resp, u, out); err != nil {
		return "", err
	}
	return nextLink(resp.Header.Get("Link")), nil
}

// do sends req with the GitHub headers and waits out exhausted rate limits.
// The caller closes the response body.
func (g *GitHub) do(ctx context.Context, newReq func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := newReq()
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if g.token != "" {
			req.Header.Set("Authorization", "Bearer "+g.token)
		}
		resp, err := g.client.Do(req.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		limited := (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
			resp.Header.Get("X-RateLimit-Remaining") == "0"
		if !limited || attempt >= maxRateLimitWaits {
			return resp, nil
		}
		resp.Body.Close()
		reset, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
		if err := g.sleep(ctx, max(time.Unix(reset, 0).Sub(g.now()), time.Second)); err != nil {
			return nil, err
		}
	}
}

// listPage fetches one package list page. With a cached ETag it sends
// If-None-Match; a 304 yields no packages and the cached next link. A 200
// stores its ETag and next link in cache, except a last page holding exactly
// perPage items: it may gain a successor later and a 304 carries no Link
// header, so it is re-read every run.
func (g *GitHub) listPage(ctx context.Context, u string, cache PageCache) (pkgs []ghPackage, next string, err error) {
	var etag, cachedNext string
	var cached bool
	if cache != nil {
		etag, cachedNext, cached = cache.Lookup(u)
	}
	resp, err := g.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err == nil && cached {
			req.Header.Set("If-None-Match", etag)
		}
		return req, err
	})
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && cached {
		return nil, cachedNext, nil
	}
	if err := decodeOK(resp, u, &pkgs); err != nil {
		return nil, "", err
	}
	next = nextLink(resp.Header.Get("Link"))
	if e := resp.Header.Get("ETag"); cache != nil && e != "" && (next != "" || len(pkgs) < packagesPerPage) {
		cache.Store(u, e, next)
	}
	return pkgs, next, nil
}

func decodeOK(resp *http.Response, u string, out any) error {
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: %s: %s", resp.Request.Method, u, resp.Status, bytes.TrimSpace(b))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decoding: %w", resp.Request.Method, u, err)
	}
	return nil
}

// nextLink extracts the rel="next" URL of an RFC 8288 Link header.
func nextLink(h string) string {
	for _, part := range strings.Split(h, ",") {
		target, params, ok := strings.Cut(strings.TrimSpace(part), ";")
		if ok && strings.Contains(params, `rel="next"`) {
			return strings.Trim(strings.TrimSpace(target), "<>")
		}
	}
	return ""
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
