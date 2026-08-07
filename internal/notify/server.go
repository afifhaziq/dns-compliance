package notify

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/hibiken/asynq"
)

// resurfacedSweepCron is a fixed constant — no admin-configurable setting
// was requested for this producer (see the design spec's Out of Scope).
const resurfacedSweepCron = "@every 15m"

// Triggerer is the subset of *server.Scanner the due-date task handler
// needs, narrowed to a small interface (matching internal/server's
// crawlerClient pattern) so this package never imports internal/server —
// *server.Scanner satisfies this structurally and is wired in from
// cmd/server/main.go.
type Triggerer interface {
	Trigger(ctx context.Context, triggeredBy string, urls []string) error
	IsRunning() bool
}

// Server runs the asynq worker (task handlers) and the periodic-task
// scheduler as background goroutines, embedded inside cmd/server alongside
// StartScheduler/StartWhoisRefresher — no separate binary.
type Server struct {
	store     db.Store
	trigger   Triggerer
	srv       *asynq.Server
	mux       *asynq.ServeMux
	scheduler *asynq.Scheduler
}

func NewServer(redisAddr string, store db.Store, trigger Triggerer) *Server {
	opt := asynq.RedisClientOpt{Addr: redisAddr}
	s := &Server{
		store:     store,
		trigger:   trigger,
		srv:       asynq.NewServer(opt, asynq.Config{Concurrency: 5}),
		scheduler: asynq.NewScheduler(opt, nil),
	}
	s.mux = asynq.NewServeMux()
	s.mux.HandleFunc(TypeResurfacedSweep, s.handleResurfacedSweep)
	s.mux.HandleFunc(TypeDueDateCheck, s.handleDueDateCheck)
	return s
}

// Start registers the periodic resurfaced-sweep task and runs both the
// worker and scheduler in background goroutines. Call once from main.go.
func (s *Server) Start() error {
	if _, err := s.scheduler.Register(resurfacedSweepCron, asynq.NewTask(TypeResurfacedSweep, nil)); err != nil {
		return err
	}
	go func() {
		if err := s.srv.Run(s.mux); err != nil {
			log.Printf("notify: asynq server: %v", err)
		}
	}()
	go func() {
		if err := s.scheduler.Run(); err != nil {
			log.Printf("notify: asynq scheduler: %v", err)
		}
	}()
	return nil
}

func (s *Server) Shutdown() {
	s.scheduler.Shutdown()
	s.srv.Shutdown()
}

// handleDueDateCheck fires a targeted scan for one domain, waits for it to
// finish, then records a due_date_reached notification with the outcome.
// asynq's built-in retry (bounded, exponential backoff by default) covers
// transient failures — including Trigger returning "scan already in
// progress" if another sweep is mid-flight.
// ponytail: asynq default retry policy (25 attempts, exponential backoff);
// tighten if this floods on a persistently-failing scan target.
func (s *Server) handleDueDateCheck(ctx context.Context, t *asynq.Task) error {
	var p dueDatePayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return err
	}

	u, err := s.store.GetURLByID(ctx, p.URLID)
	if err != nil {
		return err
	}
	if u == nil {
		// The URL was purged since this task was scheduled — nothing to do,
		// and returning nil (not an error) avoids a pointless retry loop.
		return nil
	}

	before := time.Now()
	if err := s.trigger.Trigger(ctx, "due-date-check", []string{u.URL}); err != nil {
		return err
	}
	for s.trigger.IsRunning() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	results, err := s.store.ResultsByURL(ctx, u.URL, before, time.Now())
	if err != nil {
		return err
	}

	compliant := len(results) > 0
	breakdown := make([]map[string]any, 0, len(results))
	for _, r := range results {
		compliant = compliant && r.Compliant
		breakdown = append(breakdown, map[string]any{
			"dns_server_id":   r.DNSServerID,
			"dns_server_name": r.DNSServer.Name,
			"compliant":       r.Compliant,
		})
	}

	_, err = s.store.CreateNotification(ctx, db.Notification{
		DepartmentID: p.DepartmentID,
		URLID:        u.ID,
		URLValue:     u.URL,
		Type:         "due_date_reached",
		Compliant:    &compliant,
		Details:      db.NotificationDetails{"servers": breakdown},
	})
	return err
}

// handleResurfacedSweep reuses store.ResurfacedDomainsForDepartment per
// department (rather than the global ResurfacedDomains) so each notification
// is naturally scoped to the department that actually watches the domain.
// Dedup via HasRecentResurfacedNotification stops the same regression event
// from spamming a new notification on every 15-minute tick.
func (s *Server) handleResurfacedSweep(ctx context.Context, _ *asynq.Task) error {
	depts, err := s.store.ListDepartments(ctx)
	if err != nil {
		return err
	}
	for _, dept := range depts {
		domains, err := s.store.ResurfacedDomainsForDepartment(ctx, dept.ID)
		if err != nil {
			log.Printf("notify: resurfaced sweep for department %d: %v", dept.ID, err)
			continue
		}
		for _, d := range domains {
			already, err := s.store.HasRecentResurfacedNotification(ctx, dept.ID, d.URLValue, d.ResurfacedAt)
			if err != nil {
				log.Printf("notify: dedup check for %s/%d: %v", d.URLValue, dept.ID, err)
				continue
			}
			if already {
				continue
			}
			u, err := s.store.GetURLByValue(ctx, d.URLValue)
			if err != nil || u == nil {
				continue
			}
			if _, err := s.store.CreateNotification(ctx, db.Notification{
				DepartmentID: dept.ID,
				URLID:        u.ID,
				URLValue:     d.URLValue,
				Type:         "resurfaced",
				Details:      db.NotificationDetails{"affected_servers": d.AffectedServers},
			}); err != nil {
				log.Printf("notify: create resurfaced notification for %s/%d: %v", d.URLValue, dept.ID, err)
			}
		}
	}
	return nil
}
