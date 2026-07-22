package pipeline

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SiteResult holds the outcome of checking one website.
type SiteResult struct {
	URL          string
	Timestamp    time.Time
	DNSServer    string // name of the DNS server used; empty means system resolver
	DNSResolved  bool
	ResolvedIP   string
	ResolvedIPv6 string // AAAA record, if any; informational only, never affects Compliant
	Compliant    bool   // true = unreachable (good), false = accessible (violation)
	Screenshot   []byte // nil if DNS failed or screenshot errored
	Error        string // populated on DNS failure, timeout, or screenshot error
	LatencyMs    int64  // DNS round-trip latency in milliseconds; 0 on failure
}

// Config controls pipeline concurrency and injects the DNS and screenshot
// functions, enabling unit testing without real network or browser.
type Config struct {
	DNSWorkers        int
	ScreenshotWorkers int
	DNSTimeout        time.Duration
	ScreenshotTimeout time.Duration
	Resolve           func(ctx context.Context, host string) (string, int64, error)
	ResolveIPv6       func(ctx context.Context, host string) (string, error) // optional; nil = skip AAAA lookup
	Capture           func(ctx context.Context, rawURL string) ([]byte, error)
	OnResult          func(SiteResult) // called as each result is produced; may be nil
	CompliantIPs      []string         // IPs treated as compliant even when DNS resolves (e.g. MCMC block-page IP)
}

type dnsResult struct {
	rawURL       string
	resolvedIP   string
	resolvedIPv6 string
	timestamp    time.Time
	latencyMs    int64
	compliant    bool // from checkDNS's CompliantIPs match; must survive into takeScreenshot's result
}

// Run executes one full sweep over urls and returns one SiteResult per URL.
// Results may arrive in any order.
func Run(ctx context.Context, urls []string, cfg Config) ([]SiteResult, error) {
	urlCh := make(chan string, len(urls))
	for _, u := range urls {
		urlCh <- u
	}
	close(urlCh)

	screenshotCh := make(chan dnsResult, cfg.DNSWorkers*2)
	resultCh := make(chan SiteResult, (cfg.DNSWorkers+cfg.ScreenshotWorkers)*2)

	// DNS worker pool
	var dnsWg sync.WaitGroup
	for i := 0; i < cfg.DNSWorkers; i++ {
		dnsWg.Add(1)
		go func() {
			defer dnsWg.Done()
			for rawURL := range urlCh {
				siteCtx, cancel := context.WithTimeout(ctx, cfg.DNSTimeout)
				result := checkDNS(siteCtx, rawURL, cfg.Resolve, cfg.ResolveIPv6, cfg.CompliantIPs)
				cancel()
				if result.DNSResolved {
					screenshotCh <- dnsResult{
						rawURL:       rawURL,
						resolvedIP:   result.ResolvedIP,
						resolvedIPv6: result.ResolvedIPv6,
						timestamp:    result.Timestamp,
						latencyMs:    result.LatencyMs,
						compliant:    result.Compliant,
					}
				} else {
					resultCh <- result
				}
			}
		}()
	}

	go func() {
		dnsWg.Wait()
		close(screenshotCh)
	}()

	// Screenshot worker pool
	var ssWg sync.WaitGroup
	for i := 0; i < cfg.ScreenshotWorkers; i++ {
		ssWg.Add(1)
		go func() {
			defer ssWg.Done()
			for dr := range screenshotCh {
				siteCtx, cancel := context.WithTimeout(ctx, cfg.ScreenshotTimeout)
				resultCh <- takeScreenshot(siteCtx, dr, cfg.Capture)
				cancel()
			}
		}()
	}

	go func() {
		ssWg.Wait()
		close(resultCh)
	}()

	results := make([]SiteResult, 0, len(urls))
	for r := range resultCh {
		results = append(results, r)
		if cfg.OnResult != nil {
			cfg.OnResult(r)
		}
	}
	return results, nil
}

func checkDNS(ctx context.Context, rawURL string, resolve func(context.Context, string) (string, int64, error), resolveIPv6 func(context.Context, string) (string, error), compliantIPs []string) SiteResult {
	normalized := normalizeURL(rawURL)
	u, err := url.Parse(normalized)
	if err != nil || u.Hostname() == "" {
		return SiteResult{
			URL:       rawURL,
			Timestamp: time.Now(),
			Compliant: true,
			Error:     "invalid URL: " + rawURL,
		}
	}

	ip, latencyMs, err := resolve(ctx, u.Hostname())
	if err != nil {
		return SiteResult{
			URL:       rawURL,
			Timestamp: time.Now(),
			Compliant: true,
			Error:     err.Error(),
		}
	}

	compliant := false
	for _, cip := range compliantIPs {
		if ip == cip {
			compliant = true
			break
		}
	}

	var ipv6 string
	if resolveIPv6 != nil {
		// Informational only — error ignored, never affects Compliant.
		ipv6, _ = resolveIPv6(ctx, u.Hostname())
	}

	return SiteResult{
		URL:          rawURL,
		Timestamp:    time.Now(),
		DNSResolved:  true,
		ResolvedIP:   ip,
		ResolvedIPv6: ipv6,
		Compliant:    compliant,
		LatencyMs:    latencyMs,
	}
}

func takeScreenshot(ctx context.Context, dr dnsResult, capture func(context.Context, string) ([]byte, error)) SiteResult {
	buf, err := capture(ctx, dr.rawURL)
	if err != nil {
		return SiteResult{
			URL:          dr.rawURL,
			Timestamp:    dr.timestamp,
			DNSResolved:  true,
			ResolvedIP:   dr.resolvedIP,
			ResolvedIPv6: dr.resolvedIPv6,
			Compliant:    dr.compliant,
			Error:        err.Error(),
			LatencyMs:    dr.latencyMs,
		}
	}
	return SiteResult{
		URL:          dr.rawURL,
		Timestamp:    dr.timestamp,
		DNSResolved:  true,
		ResolvedIP:   dr.resolvedIP,
		ResolvedIPv6: dr.resolvedIPv6,
		Compliant:    dr.compliant,
		Screenshot:   buf,
		LatencyMs:    dr.latencyMs,
	}
}

func normalizeURL(raw string) string {
	if !strings.Contains(raw, "://") {
		return "https://" + raw
	}
	return raw
}
