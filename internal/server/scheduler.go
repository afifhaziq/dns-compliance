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
// transient DB error). It stops when ctx is cancelled.
func StartScheduler(ctx context.Context, sc *Scanner, store db.ScanSettingsStore, defaultInterval time.Duration) {
	go func() {
		for {
			interval := defaultInterval
			if minutes, err := store.GetScanInterval(ctx); err == nil && minutes > 0 {
				interval = time.Duration(minutes) * time.Minute
			}
			select {
			case <-time.After(interval):
				if err := sc.Trigger(ctx, "scheduled", nil); err != nil {
					log.Printf("scheduler: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
