package server

import (
	"context"
	"log"
	"time"

	"github.com/afif/dns-tracking/internal/db"
)

// StartScheduler launches a background goroutine that calls sc.Trigger on a
// cadence read from db.Store.GetScanInterval — admin-configurable via the
// admin panel, re-checked before every wait so a change takes effect on the
// next cycle. defaultInterval is used if the setting can't be read (e.g. a
// transient DB error). GetScanEnabled is checked right before each trigger —
// disabling the schedule skips that sweep without stopping the loop, so
// re-enabling takes effect on the next cycle same as an interval change. On
// a read error it fails open (treated as enabled), consistent with the
// interval fallback above. sc.scheduleReset (signaled by
// Scanner.NotifyScheduleChanged, called from the admin panel's scan-settings
// save) abandons whatever is left of the current wait and restarts it with
// the freshly-saved interval, so a save actually starts the cadence from
// that moment rather than finishing out the stale one. It stops when ctx is
// cancelled.
//
// Each sweep excludes URLs StartSLAScheduler is actively covering (see
// below) — those are scanned on the separate, typically shorter, SLA
// cadence instead, so this loop isn't scanning them redundantly every
// round. If every watched URL is currently SLA-active, the sweep is
// skipped entirely rather than falling back to Trigger's "nil means scan
// everything" default.
func StartScheduler(ctx context.Context, sc *Scanner, store db.Store, defaultInterval time.Duration) {
	go func() {
		for {
			interval := defaultInterval
			if minutes, err := store.GetScanInterval(ctx); err == nil && minutes > 0 {
				interval = time.Duration(minutes) * time.Minute
			}
			select {
			case <-time.After(interval):
				if enabled, err := store.GetScanEnabled(ctx); err == nil && !enabled {
					continue
				}
				urls, err := watchedURLsExcludingSLAActive(ctx, store)
				if err != nil {
					log.Printf("scheduler: %v", err)
					continue
				}
				if len(urls) == 0 {
					continue
				}
				if err := sc.Trigger(ctx, "scheduled", urls); err != nil {
					log.Printf("scheduler: %v", err)
				}
			case <-sc.scheduleReset:
				continue
			case <-ctx.Done():
				return
			}
		}
	}()
}

// watchedURLsExcludingSLAActive returns the watched-URL values StartScheduler
// should sweep this round — everything on the watchlist except URLs
// StartSLAScheduler's cadence is already covering.
func watchedURLsExcludingSLAActive(ctx context.Context, store db.Store) ([]string, error) {
	watched, err := store.ListWatchedURLs(ctx)
	if err != nil {
		return nil, err
	}
	threshold, err := store.GetSLAStreakThreshold(ctx)
	if err != nil {
		return nil, err
	}
	active, err := store.SLAActiveURLs(ctx, threshold)
	if err != nil {
		return nil, err
	}
	activeSet := make(map[string]bool, len(active))
	for _, u := range active {
		activeSet[u] = true
	}
	urls := make([]string, 0, len(watched))
	for _, u := range watched {
		if !activeSet[u.URL] {
			urls = append(urls, u.URL)
		}
	}
	return urls, nil
}

// StartSLAScheduler launches a background goroutine, structurally identical
// to StartScheduler, that instead sweeps only URLs currently under active
// SLA tracking (db.Store.SLAActiveURLs — DueDate has passed and at least
// one enabled DNS server hasn't yet reached SLAStreakThreshold consecutive
// compliant scans) on a separate, typically shorter, cadence
// (GetSLAInterval). A sweep with nothing to cover is skipped rather than
// triggered with an empty list, since Trigger treats nil/empty as "scan
// everything." Uses its own reset channel (sc.slaScheduleReset) so an admin
// settings save restarts this loop independently of StartScheduler's.
func StartSLAScheduler(ctx context.Context, sc *Scanner, store db.Store, defaultInterval time.Duration) {
	go func() {
		for {
			interval := defaultInterval
			if minutes, err := store.GetSLAInterval(ctx); err == nil && minutes > 0 {
				interval = time.Duration(minutes) * time.Minute
			}
			select {
			case <-time.After(interval):
				if enabled, err := store.GetScanEnabled(ctx); err == nil && !enabled {
					continue
				}
				threshold, err := store.GetSLAStreakThreshold(ctx)
				if err != nil {
					log.Printf("sla scheduler: %v", err)
					continue
				}
				urls, err := store.SLAActiveURLs(ctx, threshold)
				if err != nil {
					log.Printf("sla scheduler: %v", err)
					continue
				}
				if len(urls) == 0 {
					continue
				}
				if err := sc.Trigger(ctx, "scheduled-sla", urls); err != nil {
					log.Printf("sla scheduler: %v", err)
				}
			case <-sc.slaScheduleReset:
				continue
			case <-ctx.Done():
				return
			}
		}
	}()
}
