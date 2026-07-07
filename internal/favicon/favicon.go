// Package favicon fetches a domain's icon for caching. Meant to be called
// server-side only and at most once per domain — the caller is expected to
// cache the result rather than fetch on every page view (this app tracks
// domains under active enforcement, so a browser-side favicon request would
// reveal an analyst's presence to the target's own server logs).
package favicon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxSize = 1 << 20 // 1MB safety cap on a favicon response

// Result is the favicon image worth caching for a domain.
type Result struct {
	ContentType string
	Data        []byte
}

// Fetcher fetches a domain's favicon. Exposed as a var type so tests can
// inject a fake instead of hitting the network.
type Fetcher func(ctx context.Context, domain string) (Result, error)

var client = &http.Client{Timeout: 10 * time.Second}

// Fetch is the default Fetcher. It tries domain's own conventional
// /favicon.ico first — most sites, including gov.my ones, serve one — and
// only falls back to Google's public favicon proxy (which handles
// non-standard icon paths but doesn't have every domain's real icon cached,
// silently substituting a generic placeholder) if that fails.
func Fetch(ctx context.Context, domain string) (Result, error) {
	if res, err := fetchDirect(ctx, domain); err == nil {
		return res, nil
	}
	return fetchGoogle(ctx, domain)
}

func fetchDirect(ctx context.Context, domain string) (Result, error) {
	return get(ctx, "https://"+domain+"/favicon.ico")
}

func fetchGoogle(ctx context.Context, domain string) (Result, error) {
	return get(ctx, "https://www.google.com/s2/favicons?domain="+domain+"&sz=64")
}

func get(ctx context.Context, url string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return Result{}, fmt.Errorf("favicon: non-image response (%s) from %s", contentType, url)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return Result{}, err
	}
	if len(data) == 0 {
		return Result{}, fmt.Errorf("favicon: empty response from %s", url)
	}
	return Result{ContentType: contentType, Data: data}, nil
}
