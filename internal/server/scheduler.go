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
func StartScheduler(ctx context.Context, sc *Scanner, store db.ScanSettingsStore, defaultInterval time.Duration) {
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
				if err := sc.Trigger(ctx, "scheduled", nil); err != nil {
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
