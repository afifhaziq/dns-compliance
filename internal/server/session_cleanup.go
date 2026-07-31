package server

import (
	"context"
	"log"
	"time"

	"github.com/afif/dns-tracking/internal/db"
)

// sessionCleanupInterval is how often expired sessions are swept. Sessions
// last 7 days (sessionDuration in auth.go) and GetSession already ignores
// expired rows, so this is bounded-growth housekeeping, not a security
// control — hourly is plenty.
const sessionCleanupInterval = time.Hour

// StartSessionCleanup deletes expired sessions on a fixed cadence until ctx
// is cancelled. Failures are logged and retried on the next tick; a database
// blip must not kill the loop.
func StartSessionCleanup(ctx context.Context, store db.SessionStore) {
	go func() {
		ticker := time.NewTicker(sessionCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n, err := store.DeleteExpiredSessions(ctx)
				if err != nil {
					log.Printf("session cleanup: %v", err)
					continue
				}
				if n > 0 {
					log.Printf("session cleanup: removed %d expired sessions", n)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
