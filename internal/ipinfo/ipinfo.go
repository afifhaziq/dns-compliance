// Package ipinfo resolves an IP's ASN + network operator via the free
// ipinfo.io API. Keyed by IP (not domain) and meant to be called at most
// once per distinct IP — callers are expected to cache the result rather
// than fetch on every scan.
package ipinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Result is the ASN + org data worth caching for an IP.
type Result struct {
	ASN uint
	Org string
}

// Fetcher looks up ASN/org data for ip. Exposed as a var type so tests can
// inject a fake instead of hitting the network.
type Fetcher func(ctx context.Context, ip string) (Result, error)

// NewFetcher returns a Fetcher backed by ipinfo.io. token is optional — an
// empty string uses ipinfo's unauthenticated (lower rate limit) tier.
func NewFetcher(token string) Fetcher {
	client := &http.Client{Timeout: 10 * time.Second}
	return func(ctx context.Context, ip string) (Result, error) {
		url := "https://ipinfo.io/" + ip + "/json"
		if token != "" {
			url += "?token=" + token
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return Result{}, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return Result{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return Result{}, fmt.Errorf("ipinfo: HTTP %d for %s", resp.StatusCode, ip)
		}
		var body struct {
			Org string `json:"org"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return Result{}, err
		}
		return parseOrg(body.Org), nil
	}
}

// parseOrg splits ipinfo's "AS15169 Google LLC" org field into ASN + name.
// ponytail: ipinfo-specific format assumption; if the org field ever lacks
// the "AS<number> " prefix, ASN is left 0 and the raw string kept as Org.
func parseOrg(raw string) Result {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Result{}
	}
	parts := strings.SplitN(raw, " ", 2)
	if num, ok := strings.CutPrefix(parts[0], "AS"); ok {
		if asn, err := strconv.ParseUint(num, 10, 64); err == nil {
			org := ""
			if len(parts) == 2 {
				org = parts[1]
			}
			return Result{ASN: uint(asn), Org: org}
		}
	}
	return Result{Org: raw}
}
