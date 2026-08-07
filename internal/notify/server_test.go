package notify

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/glebarez/sqlite"
	"github.com/hibiken/asynq"
)

func newTestStore(t *testing.T) db.Store {
	t.Helper()
	gormDB, err := db.Connect(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return db.NewStore(gormDB)
}

type triggerCall struct {
	triggeredBy string
	urls        []string
}

// fakeTriggerer simulates Scanner.Trigger's real shape: Trigger itself
// returns immediately (sets running, spawns a goroutine), IsRunning flips
// back to false once that goroutine finishes. afterTrigger runs inside that
// goroutine so a test can insert the ScanResult rows a real scan would have
// produced before the handler's IsRunning-poll loop moves on.
type fakeTriggerer struct {
	mu           sync.Mutex
	running      bool
	triggerErr   error
	afterTrigger func()
	calls        []triggerCall
}

func (f *fakeTriggerer) Trigger(_ context.Context, triggeredBy string, urls []string) error {
	f.mu.Lock()
	if f.triggerErr != nil {
		f.mu.Unlock()
		return f.triggerErr
	}
	f.calls = append(f.calls, triggerCall{triggeredBy, urls})
	f.running = true
	f.mu.Unlock()

	go func() {
		if f.afterTrigger != nil {
			f.afterTrigger()
		}
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()
	return nil
}

func (f *fakeTriggerer) IsRunning() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

func TestHandleDueDateCheck_CompliantOutcome(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, _ := store.CreateDepartment(ctx, "DueDept")
	u, _ := store.AddURLToWatchlist(ctx, dept.ID, "due-check-compliant.com")
	srv, _ := store.CreateDNSServer(ctx, db.DNSServer{ISP: "DueISP", Name: "Due DNS", Address: "9.9.9.3:53", Protocol: "udp"})

	var wantRunID uint
	trig := &fakeTriggerer{afterTrigger: func() {
		run, _ := store.CreateScanRun(ctx, "due-date-check")
		wantRunID = run.ID
		_ = store.InsertResult(ctx, db.ScanResult{
			ScanRunID: run.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
			Compliant: true, ScannedAt: time.Now(),
		})
	}}

	s := NewServer("unused:0", store, trig)

	payload, _ := json.Marshal(dueDatePayload{DepartmentID: dept.ID, URLID: u.ID})
	task := asynq.NewTask(TypeDueDateCheck, payload)

	if err := s.handleDueDateCheck(ctx, task); err != nil {
		t.Fatalf("handleDueDateCheck: %v", err)
	}

	if len(trig.calls) != 1 || trig.calls[0].urls[0] != "due-check-compliant.com" {
		t.Fatalf("expected a targeted trigger for the domain, got %+v", trig.calls)
	}

	notifications, total, err := store.ListNotificationsForDepartment(ctx, 1, 10, dept.ID)
	if err != nil {
		t.Fatalf("ListNotificationsForDepartment: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 notification, got %d", total)
	}
	n := notifications[0]
	if n.Type != "due_date_reached" {
		t.Fatalf("expected type due_date_reached, got %q", n.Type)
	}
	if n.Compliant == nil || !*n.Compliant {
		t.Fatalf("expected compliant=true, got %+v", n.Compliant)
	}
	if n.ScanRunID == nil || *n.ScanRunID != wantRunID {
		t.Fatalf("expected scan_run_id %d, got %+v", wantRunID, n.ScanRunID)
	}
	if n.ScannedAt == nil || n.ScannedAt.IsZero() {
		t.Fatal("expected a non-zero scanned_at")
	}
}

func TestHandleDueDateCheck_StillViolatingOutcome(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	dept, _ := store.CreateDepartment(ctx, "DueDept2")
	u, _ := store.AddURLToWatchlist(ctx, dept.ID, "due-check-violating.com")
	srv, _ := store.CreateDNSServer(ctx, db.DNSServer{ISP: "DueISP2", Name: "Due DNS 2", Address: "9.9.9.2:53", Protocol: "udp"})

	trig := &fakeTriggerer{afterTrigger: func() {
		run, _ := store.CreateScanRun(ctx, "due-date-check")
		_ = store.InsertResult(ctx, db.ScanResult{
			ScanRunID: run.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID,
			Compliant: false, ScannedAt: time.Now(),
		})
	}}

	s := NewServer("unused:0", store, trig)
	payload, _ := json.Marshal(dueDatePayload{DepartmentID: dept.ID, URLID: u.ID})
	task := asynq.NewTask(TypeDueDateCheck, payload)

	if err := s.handleDueDateCheck(ctx, task); err != nil {
		t.Fatalf("handleDueDateCheck: %v", err)
	}

	notifications, _, _ := store.ListNotificationsForDepartment(ctx, 1, 10, dept.ID)
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].Compliant == nil || *notifications[0].Compliant {
		t.Fatalf("expected compliant=false, got %+v", notifications[0].Compliant)
	}
}

func TestHandleDueDateCheck_TriggerErrorPropagates(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept, _ := store.CreateDepartment(ctx, "DueDept3")
	u, _ := store.AddURLToWatchlist(ctx, dept.ID, "due-check-busy.com")

	trig := &fakeTriggerer{triggerErr: context.Canceled}
	s := NewServer("unused:0", store, trig)
	payload, _ := json.Marshal(dueDatePayload{DepartmentID: dept.ID, URLID: u.ID})
	task := asynq.NewTask(TypeDueDateCheck, payload)

	if err := s.handleDueDateCheck(ctx, task); err == nil {
		t.Fatal("expected the trigger error to propagate — so asynq retries")
	}
}

func TestHandleDueDateCheck_NoResultsReturnsErrorNotFalseNotification(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dept, _ := store.CreateDepartment(ctx, "DueDept4")
	u, _ := store.AddURLToWatchlist(ctx, dept.ID, "due-check-noresults.com")

	// afterTrigger intentionally inserts nothing — simulates a scan that ran
	// but produced zero results (e.g. crawler down, or all DNS servers
	// disabled).
	trig := &fakeTriggerer{afterTrigger: func() {}}
	s := NewServer("unused:0", store, trig)
	payload, _ := json.Marshal(dueDatePayload{DepartmentID: dept.ID, URLID: u.ID})
	task := asynq.NewTask(TypeDueDateCheck, payload)

	if err := s.handleDueDateCheck(ctx, task); err == nil {
		t.Fatal("expected an error when the scan produces zero results — so asynq retries instead of recording a false verdict")
	}

	_, total, err := store.ListNotificationsForDepartment(ctx, 1, 10, dept.ID)
	if err != nil {
		t.Fatalf("ListNotificationsForDepartment: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected no notification to be created, got %d", total)
	}
}

func TestHandleResurfacedSweep_NotifiesOnceAndDedupsOnRerun(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	srv, _ := store.CreateDNSServer(ctx, db.DNSServer{ISP: "SweepISP", Name: "Sweep DNS", Address: "9.9.9.1:53", Protocol: "udp"})
	dept, _ := store.CreateDepartment(ctx, "SweepDept")
	u, _ := store.AddURLToWatchlist(ctx, dept.ID, "sweep-flip.com")

	t1 := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	run1, _ := store.CreateScanRun(ctx, "manual")
	run2, _ := store.CreateScanRun(ctx, "manual")
	_ = store.InsertResult(ctx, db.ScanResult{ScanRunID: run1.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID, Compliant: true, ScannedAt: t1})
	_ = store.InsertResult(ctx, db.ScanResult{ScanRunID: run2.ID, URLID: u.ID, URLValue: u.URL, DNSServerID: srv.ID, Compliant: false, ScannedAt: t2})

	s := NewServer("unused:0", store, &fakeTriggerer{})
	task := asynq.NewTask(TypeResurfacedSweep, nil)

	if err := s.handleResurfacedSweep(ctx, task); err != nil {
		t.Fatalf("handleResurfacedSweep (first run): %v", err)
	}
	notifications, total, _ := store.ListNotificationsForDepartment(ctx, 1, 10, dept.ID)
	if total != 1 {
		t.Fatalf("expected 1 notification after first sweep, got %d", total)
	}
	n := notifications[0]
	if n.ScanRunID != nil {
		t.Fatalf("expected no scan_run_id for a resurfaced notification, got %+v", n.ScanRunID)
	}
	if n.ScannedAt == nil || !n.ScannedAt.Equal(t2) {
		t.Fatalf("expected scanned_at %v (the flip time), got %+v", t2, n.ScannedAt)
	}

	if err := s.handleResurfacedSweep(ctx, task); err != nil {
		t.Fatalf("handleResurfacedSweep (second run): %v", err)
	}
	_, total, _ = store.ListNotificationsForDepartment(ctx, 1, 10, dept.ID)
	if total != 1 {
		t.Fatalf("expected dedup to keep total at 1 after a second sweep, got %d", total)
	}
}
