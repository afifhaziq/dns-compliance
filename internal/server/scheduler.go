package server

import (
	"context"
	"log"
	"time"
)

// StartScheduler launches a background goroutine that calls sc.Trigger every
// interval. It stops when ctx is cancelled.
func StartScheduler(ctx context.Context, sc *Scanner, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := sc.Trigger(ctx, "scheduled"); err != nil {
					log.Printf("scheduler: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
