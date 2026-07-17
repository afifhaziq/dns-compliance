package server

import (
	"context"
	"log"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/afif/dns-tracking/internal/whois"
)

// whoisRefreshBatchSize caps how many domains one refresh tick fetches —
// RDAP servers rate-limit aggressively, so this bounds worst-case load per
// tick rather than draining the whole stale set at once.
const whoisRefreshBatchSize = 20

// whoisRefreshPause is the fixed delay between fetches within a tick.
// ponytail: sequential + fixed pause, not a real worker pool with
// backoff/jitter — upgrade if RDAP 429s show up in practice.
const whoisRefreshPause = 2 * time.Second

// StartWhoisRefresher launches a background goroutine that re-fetches RDAP
// data for domains whose DomainWhois row is missing or older than
// staleDays. Mirrors StartScheduler but on its own, much slower cadence.
func StartWhoisRefresher(ctx context.Context, store db.EnrichmentStore, fetch whois.Fetcher, interval time.Duration, staleDays int) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				refreshStaleDomains(ctx, store, fetch, staleDays)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func refreshStaleDomains(ctx context.Context, store db.EnrichmentStore, fetch whois.Fetcher, staleDays int) {
	olderThan := time.Now().AddDate(0, 0, -staleDays)
	domains, err := store.ListStaleDomains(ctx, olderThan, whoisRefreshBatchSize)
	if err != nil {
		log.Printf("whois refresh: list stale domains: %v", err)
		return
	}
	for _, u := range domains {
		fetchAndStoreWhois(store, fetch, u.ID, u.URL)
		select {
		case <-ctx.Done():
			return
		case <-time.After(whoisRefreshPause):
		}
	}
}
