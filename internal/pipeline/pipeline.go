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
	URL         string
	Timestamp   time.Time
	DNSResolved bool
	ResolvedIP  string
	Compliant   bool   // true = unreachable (good), false = accessible (violation)
	Screenshot  []byte // nil if DNS failed or screenshot errored
	Error       string // populated on DNS failure, timeout, or screenshot error
}

// Config controls pipeline concurrency and injects the DNS and screenshot
// functions, enabling unit testing without real network or browser.
type Config struct {
	DNSWorkers        int
	ScreenshotWorkers int
	DNSTimeout        time.Duration
	ScreenshotTimeout time.Duration
	Resolve           func(ctx context.Context, host string) (string, error)
	Capture           func(ctx context.Context, rawURL string) ([]byte, error)
	OnResult          func(SiteResult) // called as each result is produced; may be nil
}

type dnsResult struct {
	rawURL     string
	resolvedIP string
	timestamp  time.Time
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
				result := checkDNS(siteCtx, rawURL, cfg.Resolve)
				cancel()
				if result.DNSResolved {
					screenshotCh <- dnsResult{
						rawURL:     rawURL,
						resolvedIP: result.ResolvedIP,
						timestamp:  result.Timestamp,
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

func checkDNS(ctx context.Context, rawURL string, resolve func(context.Context, string) (string, error)) SiteResult {
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

	ip, err := resolve(ctx, u.Hostname())
	if err != nil {
		return SiteResult{
			URL:       rawURL,
			Timestamp: time.Now(),
			Compliant: true,
			Error:     err.Error(),
		}
	}
	return SiteResult{
		URL:         rawURL,
		Timestamp:   time.Now(),
		DNSResolved: true,
		ResolvedIP:  ip,
		Compliant:   false,
	}
}

func takeScreenshot(ctx context.Context, dr dnsResult, capture func(context.Context, string) ([]byte, error)) SiteResult {
	buf, err := capture(ctx, dr.rawURL)
	if err != nil {
		return SiteResult{
			URL:         dr.rawURL,
			Timestamp:   dr.timestamp,
			DNSResolved: true,
			ResolvedIP:  dr.resolvedIP,
			Compliant:   false,
			Error:       err.Error(),
		}
	}
	return SiteResult{
		URL:         dr.rawURL,
		Timestamp:   dr.timestamp,
		DNSResolved: true,
		ResolvedIP:  dr.resolvedIP,
		Compliant:   false,
		Screenshot:  buf,
	}
}

func normalizeURL(raw string) string {
	if !strings.Contains(raw, "://") {
		return "https://" + raw
	}
	return raw
}
