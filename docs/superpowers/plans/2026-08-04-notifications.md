# Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a bell-icon notification center backed by Redis + `hibiken/asynq`, covering a periodic resurfaced-domain sweep and a one-shot per-domain due-date scan, surfaced via a paginated notifications API and a navbar bell with an unread-count badge.

**Architecture:** A new `internal/notify/` package owns all asynq wiring: an `Enqueuer` (client + inspector) that the `PATCH /api/urls/{id}` handler calls to schedule/cancel a due-date task, and a `Server` (asynq worker + periodic scheduler) that runs embedded inside `cmd/server` as extra goroutines, exactly like `StartScheduler`/`StartWhoisRefresher` today. Both task handlers write to a new `db.Notification` table via a new `NotificationStore` sub-interface, following this codebase's established admin-global/department-scoped read pattern (see `ResurfacedDomains`/`ISPStats`). The frontend polls `GET /api/notifications/unread-count` every 30s (same pattern `scan-results.tsx` already uses for scan status).

**Tech Stack:** Go 1.26, `github.com/hibiken/asynq` (new dependency, pulls `github.com/redis/go-redis/v9`), GORM/PostgreSQL, chi router, React 19 + TypeScript, TanStack Router.

## Global Constraints

- The sibling worktree `url-compliance-case-fields` is concurrently renaming `DepartmentURL.OrderedAt` → `DueDate`. That rename has **not** landed on this branch (confirmed: `internal/db/models.go`, `store.go`, `postgres.go`, `handlers.go` all still say `OrderedAt`/`ordered_at`). This plan implements everything against the current `OrderedAt` field/name and leaves a `// TODO(url-compliance-case-fields): ...` comment at each due-date integration point. Do not block on or attempt the rename yourself.
- Do not merge or push to `main`. Do not touch other worktrees.
- Update the Orca worktree comment at meaningful checkpoints via the `orca` CLI (after each task lands, per the orchestrating session's instructions).
- Run `go test ./...` and `cd web && npm run build && npm run lint` before considering the work done (final task).
- Add a `redis` service to `docker-compose.yml` and `docker-compose.dev.yml`.
- Update `CLAUDE.md` with a new section describing the notification system, matching the existing `docs: update CLAUDE.md` commit convention (final task, separate commit).
- Every new store method/interface follows this repo's sub-interface convention (`internal/db/store.go`): a consumer that only touches one aggregate takes that narrow sub-interface, not the full `db.Store`.
- Every new admin-vs-department-scoped list/count endpoint follows the exact `if user.IsAdmin { ... } else { require DepartmentID; ...ForDepartment(...) }` branch shape already used by `ISPStats`/`ResurfacedDomains` (`internal/server/handlers.go`).

---

## File Structure

New files:
- `internal/db/models.go` (edit) — `Notification` model, `NotificationDetails` type
- `internal/db/store.go` (edit) — `NotificationStore` sub-interface, embed in `Store`, add `GetURLByID` to `URLStore`
- `internal/db/postgres.go` (edit) — `postgresStore` implementations
- `internal/db/db.go` (edit) — AutoMigrate `&Notification{}`
- `internal/db/postgres_test.go` (edit) — store-layer tests
- `internal/notify/payload.go` (new) — task type constants + `dueDatePayload`
- `internal/notify/enqueue.go` (new) — `Enqueuer`, `queueClient` interface, `RescheduleDueDate`
- `internal/notify/enqueue_test.go` (new)
- `internal/notify/server.go` (new) — `Server` (asynq worker + scheduler), `Triggerer` interface, the two task handlers
- `internal/notify/server_test.go` (new)
- `cmd/server/main.go` (edit) — `--redis-addr` flag, wire `notify.NewEnqueuer`/`notify.NewServer`
- `internal/server/handlers.go` (edit) — `dueDateRescheduler` interface, `Handlers.notify` field, reschedule calls in `ToggleURL`/`RemoveFromWatchlist`
- `internal/server/notification_handlers.go` (new) — the three notification REST handlers, mirroring `admin_handlers.go`'s per-feature-file split
- `internal/server/router.go` (edit) — new routes, `RegisterRoutes` param
- `internal/server/handlers_test.go` (edit) — `fullMockStore` additions, new tests
- `docker-compose.yml`, `docker-compose.dev.yml` (edit) — `redis` service
- `dev.sh` (edit) — start/wait for redis, pass `--redis-addr`
- `web/src/api/types.ts` (edit) — `Notification`, `NotificationsResponse`
- `web/src/api/notifications.ts` (new)
- `web/src/components/notification-bell.tsx` (new)
- `web/src/routes/__root.tsx` (edit) — mount `<NotificationBell />`
- `CLAUDE.md` (edit) — new Notifications section

---

### Task 1: `db.Notification` model, store interface, and PostgreSQL implementation

**Files:**
- Modify: `internal/db/models.go`
- Modify: `internal/db/store.go`
- Modify: `internal/db/postgres.go`
- Modify: `internal/db/db.go`
- Test: `internal/db/postgres_test.go`

**Interfaces:**
- Produces: `db.Notification` struct, `db.NotificationDetails` (`map[string]any`), `db.NotificationStore` interface with `CreateNotification`, `ListNotifications`/`ListNotificationsForDepartment`, `UnreadCount`/`UnreadCountForDepartment`, `GetNotification`, `MarkNotificationRead`, `HasRecentResurfacedNotification`; `URLStore.GetURLByID`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/db/postgres_test.go`:

```go
func TestNotifications_CreateListUnreadMarkRead(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "NotifyDept")
	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "notify-flow.com")

	n, err := s.CreateNotification(ctx, db.Notification{
		DepartmentID: dept.ID,
		URLID:        u.ID,
		URLValue:     u.URL,
		Type:         "resurfaced",
		Details:      db.NotificationDetails{"note": "test"},
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if n.ID == 0 {
		t.Fatal("expected a non-zero ID after create")
	}

	list, total, err := s.ListNotificationsForDepartment(ctx, 1, 10, dept.ID)
	if err != nil {
		t.Fatalf("ListNotificationsForDepartment: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("expected 1 notification, got total=%d len=%d", total, len(list))
	}
	if list[0].Details["note"] != "test" {
		t.Fatalf("expected details to round-trip, got %+v", list[0].Details)
	}

	count, err := s.UnreadCountForDepartment(ctx, dept.ID)
	if err != nil {
		t.Fatalf("UnreadCountForDepartment: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected unread count 1, got %d", count)
	}

	got, err := s.GetNotification(ctx, n.ID)
	if err != nil || got == nil {
		t.Fatalf("GetNotification: got=%v err=%v", got, err)
	}
	if got.ReadAt != nil {
		t.Fatal("expected ReadAt nil before marking read")
	}

	if err := s.MarkNotificationRead(ctx, n.ID); err != nil {
		t.Fatalf("MarkNotificationRead: %v", err)
	}
	count, err = s.UnreadCountForDepartment(ctx, dept.ID)
	if err != nil {
		t.Fatalf("UnreadCountForDepartment after read: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected unread count 0 after marking read, got %d", count)
	}

	// Admin-global scope sees the same row.
	globalTotal := 0
	if _, total, err := s.ListNotifications(ctx, 1, 10); err != nil {
		t.Fatalf("ListNotifications: %v", err)
	} else {
		globalTotal = total
	}
	if globalTotal != 1 {
		t.Fatalf("expected global total 1, got %d", globalTotal)
	}
}

func TestNotifications_GetNotificationReturnsNilForUnknownID(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetNotification(context.Background(), 999999)
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unknown id, got %+v", got)
	}
}

func TestHasRecentResurfacedNotification(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dept, _ := s.CreateDepartment(ctx, "DedupDept")
	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "dedup.com")

	resurfacedAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	has, err := s.HasRecentResurfacedNotification(ctx, dept.ID, u.URL, resurfacedAt)
	if err != nil {
		t.Fatalf("HasRecentResurfacedNotification (none yet): %v", err)
	}
	if has {
		t.Fatal("expected false before any notification exists")
	}

	if _, err := s.CreateNotification(ctx, db.Notification{
		DepartmentID: dept.ID, URLID: u.ID, URLValue: u.URL, Type: "resurfaced",
	}); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	has, err = s.HasRecentResurfacedNotification(ctx, dept.ID, u.URL, resurfacedAt)
	if err != nil {
		t.Fatalf("HasRecentResurfacedNotification (after create): %v", err)
	}
	if !has {
		t.Fatal("expected true — the just-created row's CreatedAt is after resurfacedAt (2026-07-02)")
	}
}

func TestGetURLByID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dept, _ := s.CreateDepartment(ctx, "GetByIDDept")
	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "get-by-id.com")

	got, err := s.GetURLByID(ctx, u.ID)
	if err != nil || got == nil {
		t.Fatalf("GetURLByID: got=%v err=%v", got, err)
	}
	if got.URL != "get-by-id.com" {
		t.Fatalf("expected get-by-id.com, got %q", got.URL)
	}

	missing, err := s.GetURLByID(ctx, 999999)
	if err != nil {
		t.Fatalf("GetURLByID (missing): %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for unknown id, got %+v", missing)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail (compile error is expected — types don't exist yet)**

Run: `go test ./internal/db/... -run 'TestNotifications|TestHasRecentResurfacedNotification|TestGetURLByID' -v`
Expected: FAIL — build error, `db.Notification`/`db.NotificationDetails` undefined, `GetURLByID` undefined.

- [ ] **Step 3: Add the model to `internal/db/models.go`**

Append after the `ResurfacedDomain` struct (after line 308):

```go
// NotificationDetails is a free-form per-notification-type payload (e.g.
// resurfaced's affected-server list, due_date_reached's per-server
// breakdown) — stored as jsonb via GORM's json serializer, same pattern
// Citation.Parsed already uses.
type NotificationDetails map[string]any

// Notification is a queued alert for a department about a domain event —
// either a resurfaced (compliant->violating) regression or a due-date scan
// outcome. URLID/URLValue mirror ScanResult's dual FK+denormalized-value
// pattern: URLID cascades on URL purge, URLValue is the read/query key so
// list/dedup queries don't need a join.
type Notification struct {
	ID           uint                `gorm:"primaryKey" json:"id"`
	DepartmentID uint                `gorm:"not null;index:idx_notifications_dept_read,priority:1" json:"department_id"`
	URLID        uint                `gorm:"not null;index" json:"url_id"`
	URL          URL                 `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	URLValue     string              `gorm:"not null;index" json:"url"`
	Type         string              `gorm:"not null" json:"type"` // "resurfaced" | "due_date_reached"
	// Compliant is always nil for "resurfaced" (the type itself is the
	// signal) and always set for "due_date_reached" (the scan outcome).
	Compliant *bool               `json:"compliant,omitempty"`
	Details   NotificationDetails `gorm:"type:jsonb;serializer:json" json:"details,omitempty"`
	ReadAt    *time.Time          `gorm:"index:idx_notifications_dept_read,priority:2" json:"read_at,omitempty"`
	CreatedAt time.Time           `gorm:"index" json:"created_at"`
}
```

- [ ] **Step 4: Add `GetURLByID` to `URLStore` and `NotificationStore` to `internal/db/store.go`**

In `URLStore` (after the `GetURLByValue` line, `internal/db/store.go:16`):

```go
	GetURLByID(ctx context.Context, id uint) (*URL, error)          // nil, nil if id is unknown
```

Append a new sub-interface after `LegalCitationStore` (after line 218), and add `NotificationStore` to the `Store` composition:

```go
// NotificationStore covers the notification-center table — same
// admin-global/department-scoped read pattern as ResultStore's
// ListDomainSummaries/ForDepartment. CreateNotification and
// HasRecentResurfacedNotification are called only from internal/notify's
// task handlers, not from any HTTP handler directly.
type NotificationStore interface {
	CreateNotification(ctx context.Context, n Notification) (Notification, error)
	ListNotifications(ctx context.Context, page, pageSize int) ([]Notification, int, error)
	ListNotificationsForDepartment(ctx context.Context, page, pageSize int, departmentID uint) ([]Notification, int, error)
	UnreadCount(ctx context.Context) (int, error)
	UnreadCountForDepartment(ctx context.Context, departmentID uint) (int, error)
	GetNotification(ctx context.Context, id uint) (*Notification, error) // nil, nil if not found
	MarkNotificationRead(ctx context.Context, id uint) error

	// HasRecentResurfacedNotification is the dedup check for the periodic
	// resurfaced sweep: true if a "resurfaced" notification for
	// (departmentID, urlValue) already has CreatedAt >= sinceResurfacedAt,
	// meaning this specific regression event was already notified.
	HasRecentResurfacedNotification(ctx context.Context, departmentID uint, urlValue string, sinceResurfacedAt time.Time) (bool, error)
}
```

Add `NotificationStore` to the `Store` interface embedding list (`internal/db/store.go:226-240`):

```go
type Store interface {
	URLStore
	DNSServerStore
	ScanRunStore
	ResultStore
	ISPStatsStore
	DepartmentStore
	UserStore
	SessionStore
	CompliantIPStore
	ISPLogoStore
	ScanSettingsStore
	EnrichmentStore
	LegalCitationStore
	NotificationStore
}
```

- [ ] **Step 5: Implement in `internal/db/postgres.go`**

Add `GetURLByID` right after `GetURLByValue` (after line 1238):

```go
func (s *postgresStore) GetURLByID(ctx context.Context, id uint) (*URL, error) {
	var u URL
	if err := s.db.WithContext(ctx).First(&u, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}
```

Append at the end of `internal/db/postgres.go`:

```go
func (s *postgresStore) CreateNotification(ctx context.Context, n Notification) (Notification, error) {
	return n, s.db.WithContext(ctx).Create(&n).Error
}

func (s *postgresStore) ListNotifications(ctx context.Context, page, pageSize int) ([]Notification, int, error) {
	return s.listNotifications(ctx, page, pageSize, nil)
}

func (s *postgresStore) ListNotificationsForDepartment(ctx context.Context, page, pageSize int, departmentID uint) ([]Notification, int, error) {
	return s.listNotifications(ctx, page, pageSize, &departmentID)
}

func (s *postgresStore) listNotifications(ctx context.Context, page, pageSize int, departmentID *uint) ([]Notification, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 25
	}
	q := s.db.WithContext(ctx).Model(&Notification{})
	if departmentID != nil {
		q = q.Where("department_id = ?", *departmentID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Notification
	err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, int(total), err
}

func (s *postgresStore) UnreadCount(ctx context.Context) (int, error) {
	return s.unreadCount(ctx, nil)
}

func (s *postgresStore) UnreadCountForDepartment(ctx context.Context, departmentID uint) (int, error) {
	return s.unreadCount(ctx, &departmentID)
}

func (s *postgresStore) unreadCount(ctx context.Context, departmentID *uint) (int, error) {
	q := s.db.WithContext(ctx).Model(&Notification{}).Where("read_at IS NULL")
	if departmentID != nil {
		q = q.Where("department_id = ?", *departmentID)
	}
	var count int64
	err := q.Count(&count).Error
	return int(count), err
}

func (s *postgresStore) GetNotification(ctx context.Context, id uint) (*Notification, error) {
	var n Notification
	if err := s.db.WithContext(ctx).First(&n, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &n, nil
}

func (s *postgresStore) MarkNotificationRead(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).
		Model(&Notification{}).
		Where("id = ?", id).
		Update("read_at", time.Now()).Error
}

func (s *postgresStore) HasRecentResurfacedNotification(ctx context.Context, departmentID uint, urlValue string, sinceResurfacedAt time.Time) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&Notification{}).
		Where("department_id = ? AND url_value = ? AND type = ? AND created_at >= ?", departmentID, urlValue, "resurfaced", sinceResurfacedAt).
		Count(&count).Error
	return count > 0, err
}
```

- [ ] **Step 6: Register `Notification` in `AutoMigrate` (`internal/db/db.go:27-30`)**

```go
	if err := database.AutoMigrate(
		&Department{}, &User{}, &Session{}, &DNSServer{}, &URL{}, &DepartmentURL{}, &ScanRun{}, &ScanResult{}, &CompliantIP{}, &DomainWhois{}, &IPInfo{}, &Favicon{}, &ScanSettings{}, &SubdomainScan{}, &ISPLogo{},
		&Instrument{}, &Citation{}, &Category{}, &Element{}, &URLOffence{},
		&Notification{},
	); err != nil {
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/db/... -run 'TestNotifications|TestHasRecentResurfacedNotification|TestGetURLByID' -v`
Expected: PASS

- [ ] **Step 8: Run the full db package suite to check for regressions**

Run: `go test ./internal/db/...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/db/models.go internal/db/store.go internal/db/postgres.go internal/db/db.go internal/db/postgres_test.go
git commit -m "feat(db): add Notification model and NotificationStore"
```

---

### Task 2: `internal/notify.Enqueuer` — schedule/cancel due-date tasks

**Files:**
- Create: `internal/notify/payload.go`
- Create: `internal/notify/enqueue.go`
- Test: `internal/notify/enqueue_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (standalone package).
- Produces: `notify.TypeDueDateCheck`, `notify.TypeResurfacedSweep` string constants; `notify.dueDatePayload{DepartmentID, URLID uint}` (unexported, JSON-tagged `department_id`/`url_id`); `notify.Enqueuer` with `NewEnqueuer(redisAddr string) *Enqueuer` and `(e *Enqueuer) RescheduleDueDate(departmentID, urlID uint, dueDate *time.Time) error`. Task 5 (server-package wiring) depends on this exact method signature — it's what the `dueDateRescheduler` interface there requires.

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/hibiken/asynq`
Run: `go mod tidy`

- [ ] **Step 2: Write `internal/notify/payload.go`**

```go
package notify

// Task type names registered on the asynq ServeMux (see server.go).
const (
	TypeResurfacedSweep = "notify:resurfaced_sweep"
	TypeDueDateCheck    = "notify:due_date_check"
)

// dueDatePayload is the JSON body of a TypeDueDateCheck task — just enough
// to look the URL and owning department back up when the task fires.
type dueDatePayload struct {
	DepartmentID uint `json:"department_id"`
	URLID        uint `json:"url_id"`
}

// dueDateQueue is the single asynq queue this package uses — no priority
// tiers needed at this scale.
const dueDateQueue = "default"
```

- [ ] **Step 3: Write the failing tests — `internal/notify/enqueue_test.go`**

```go
package notify

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

type deleteCall struct{ queue, id string }

// fakeQueueClient is a minimal in-memory double for what Enqueuer needs from
// *asynq.Client + *asynq.Inspector — real network/Redis is neither available
// nor desired in unit tests (see internal/notify's testing note in the plan).
type fakeQueueClient struct {
	mu         sync.Mutex
	deleted    []deleteCall
	deleteErr  error
	enqueued   []*asynq.Task
	enqueueErr error
}

func (f *fakeQueueClient) DeleteTask(queue, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, deleteCall{queue, id})
	return f.deleteErr
}

func (f *fakeQueueClient) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enqueueErr != nil {
		return nil, f.enqueueErr
	}
	f.enqueued = append(f.enqueued, task)
	return &asynq.TaskInfo{}, nil
}

func TestRescheduleDueDate_FutureDate_DeletesThenEnqueues(t *testing.T) {
	q := &fakeQueueClient{}
	e := &Enqueuer{q: q}

	due := time.Now().Add(24 * time.Hour)
	if err := e.RescheduleDueDate(7, 42, &due); err != nil {
		t.Fatalf("RescheduleDueDate: %v", err)
	}

	if len(q.deleted) != 1 || q.deleted[0].queue != dueDateQueue || q.deleted[0].id != "due:7:42" {
		t.Fatalf("expected 1 delete for due:7:42, got %+v", q.deleted)
	}
	if len(q.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued task, got %d", len(q.enqueued))
	}
	got := q.enqueued[0]
	if got.Type() != TypeDueDateCheck {
		t.Fatalf("expected type %q, got %q", TypeDueDateCheck, got.Type())
	}
	var p dueDatePayload
	if err := json.Unmarshal(got.Payload(), &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.DepartmentID != 7 || p.URLID != 42 {
		t.Fatalf("expected payload {7 42}, got %+v", p)
	}
}

func TestRescheduleDueDate_NilDate_OnlyDeletes(t *testing.T) {
	q := &fakeQueueClient{}
	e := &Enqueuer{q: q}

	if err := e.RescheduleDueDate(1, 2, nil); err != nil {
		t.Fatalf("RescheduleDueDate: %v", err)
	}
	if len(q.deleted) != 1 {
		t.Fatalf("expected 1 delete, got %d", len(q.deleted))
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueues for a nil date, got %d", len(q.enqueued))
	}
}

func TestRescheduleDueDate_PastDate_OnlyDeletes(t *testing.T) {
	q := &fakeQueueClient{}
	e := &Enqueuer{q: q}

	past := time.Now().Add(-time.Hour)
	if err := e.RescheduleDueDate(1, 2, &past); err != nil {
		t.Fatalf("RescheduleDueDate: %v", err)
	}
	if len(q.enqueued) != 0 {
		t.Fatalf("expected 0 enqueues for a past date, got %d", len(q.enqueued))
	}
}

func TestRescheduleDueDate_DeleteErrorIsIgnored(t *testing.T) {
	q := &fakeQueueClient{deleteErr: asynq.ErrTaskNotFound}
	e := &Enqueuer{q: q}

	due := time.Now().Add(time.Hour)
	if err := e.RescheduleDueDate(1, 2, &due); err != nil {
		t.Fatalf("expected delete error to be ignored, got: %v", err)
	}
	if len(q.enqueued) != 1 {
		t.Fatal("expected enqueue to still happen after an ignored delete error")
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/notify/... -v`
Expected: FAIL — `Enqueuer`/`RescheduleDueDate` undefined.

- [ ] **Step 5: Write `internal/notify/enqueue.go`**

```go
package notify

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// queueClient is the subset of *asynq.Client + *asynq.Inspector Enqueuer
// needs, narrowed to a small interface so RescheduleDueDate is unit
// testable without a live Redis — same pattern as internal/server's
// crawlerClient.
type queueClient interface {
	Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	DeleteTask(queue, id string) error
}

type asynqQueueClient struct {
	client    *asynq.Client
	inspector *asynq.Inspector
}

func (c *asynqQueueClient) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return c.client.Enqueue(task, opts...)
}

func (c *asynqQueueClient) DeleteTask(queue, id string) error {
	return c.inspector.DeleteTask(queue, id)
}

// Enqueuer schedules/cancels the one-shot due-date-reached task. Called from
// the PATCH /api/urls/{id} handler whenever DepartmentURL.OrderedAt changes
// — see internal/server's dueDateRescheduler.
// TODO(url-compliance-case-fields): once that branch's OrderedAt -> DueDate
// rename merges, this comment (and the callers below) should say DueDate.
type Enqueuer struct {
	q queueClient
}

func NewEnqueuer(redisAddr string) *Enqueuer {
	opt := asynq.RedisClientOpt{Addr: redisAddr}
	return &Enqueuer{q: &asynqQueueClient{
		client:    asynq.NewClient(opt),
		inspector: asynq.NewInspector(opt),
	}}
}

func taskID(departmentID, urlID uint) string {
	return fmt.Sprintf("due:%d:%d", departmentID, urlID)
}

// RescheduleDueDate deletes any previously-scheduled due-date task for this
// (department, url) pair, then enqueues a fresh one if dueDate is non-nil
// and in the future. Clearing the date (dueDate == nil) or removing the URL
// from the watchlist just deletes, per the design spec's Producer 2 section.
//
// ponytail: DeleteTask errors — including "not found", the common case for
// a URL that never had a due date — are ignored outright rather than
// checked against a specific sentinel error. Deletion here is best-effort
// cleanup before Enqueue; if it silently failed for a real reason, the
// deterministic TaskID makes the subsequent Enqueue call fail with a
// conflict instead, which still surfaces (via the caller's error log).
func (e *Enqueuer) RescheduleDueDate(departmentID, urlID uint, dueDate *time.Time) error {
	id := taskID(departmentID, urlID)
	_ = e.q.DeleteTask(dueDateQueue, id)

	if dueDate == nil || !dueDate.After(time.Now()) {
		return nil
	}

	payload, err := json.Marshal(dueDatePayload{DepartmentID: departmentID, URLID: urlID})
	if err != nil {
		return err
	}
	task := asynq.NewTask(TypeDueDateCheck, payload)
	_, err = e.q.Enqueue(task, asynq.TaskID(id), asynq.Queue(dueDateQueue), asynq.ProcessAt(*dueDate))
	return err
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/notify/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/notify/payload.go internal/notify/enqueue.go internal/notify/enqueue_test.go
git commit -m "feat(notify): add Enqueuer for due-date task scheduling"
```

---

### Task 3: `internal/notify.Server` — asynq worker + periodic scheduler, task handlers

**Files:**
- Create: `internal/notify/server.go`
- Test: `internal/notify/server_test.go`

**Interfaces:**
- Consumes: `db.Store` (Task 1), `TypeResurfacedSweep`/`TypeDueDateCheck`/`dueDatePayload` (Task 2's `payload.go`).
- Produces: `notify.Triggerer` interface (`Trigger(ctx, triggeredBy string, urls []string) error`, `IsRunning() bool` — `*server.Scanner` satisfies this structurally, no import needed here); `notify.NewServer(redisAddr string, store db.Store, trigger Triggerer) *Server`; `(*Server) Start() error`; `(*Server) Shutdown()`. Task 4 (main.go wiring) depends on this exact constructor + method set.

- [ ] **Step 1: Write the failing tests — `internal/notify/server_test.go`**

```go
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

	trig := &fakeTriggerer{afterTrigger: func() {
		run, _ := store.CreateScanRun(ctx, "due-date-check")
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
	_, total, _ := store.ListNotificationsForDepartment(ctx, 1, 10, dept.ID)
	if total != 1 {
		t.Fatalf("expected 1 notification after first sweep, got %d", total)
	}

	if err := s.handleResurfacedSweep(ctx, task); err != nil {
		t.Fatalf("handleResurfacedSweep (second run): %v", err)
	}
	_, total, _ = store.ListNotificationsForDepartment(ctx, 1, 10, dept.ID)
	if total != 1 {
		t.Fatalf("expected dedup to keep total at 1 after a second sweep, got %d", total)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/notify/... -run 'TestHandleDueDateCheck|TestHandleResurfacedSweep' -v`
Expected: FAIL — `Server`/`NewServer`/`handleDueDateCheck`/`handleResurfacedSweep` undefined.

- [ ] **Step 3: Write `internal/notify/server.go`**

```go
package notify

import (
	"context"
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
```

Add `"encoding/json"` to the import block (used by `handleDueDateCheck`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/notify/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/notify/server.go internal/notify/server_test.go
git commit -m "feat(notify): add asynq worker/scheduler and the two task handlers"
```

---

### Task 4: Wire Redis + notify into `cmd/server/main.go`, docker-compose, dev.sh

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `docker-compose.yml`
- Modify: `docker-compose.dev.yml`
- Modify: `dev.sh`

**Interfaces:**
- Consumes: `notify.NewEnqueuer`, `notify.NewServer`, `(*notify.Server).Start`/`.Shutdown` (Tasks 2 & 3).
- Produces: the `--redis-addr` flag; a `notifyEnqueuer` value passed into `server.RegisterRoutes` (Task 5 extends that signature to accept it).

This task has no isolated unit test of its own (it's wiring) — it's verified by Task 5/6/7's tests still passing and the final `dev.sh` manual check.

- [ ] **Step 1: Add the flag (`cmd/server/main.go`, alongside the other flags after line 51)**

```go
	redisAddr := flag.String("redis-addr", envOr("REDIS_ADDR", "localhost:6379"), "Redis address for the notification task queue (host:port)")
```

- [ ] **Step 2: Import `internal/notify`**

Add to the import block:

```go
	"github.com/afif/dns-tracking/internal/notify"
```

- [ ] **Step 3: Construct the enqueuer + notify server, start it, defer shutdown**

Insert after `sc := server.NewScanner(...)` (line 148) and before the gRPC server setup:

```go
	// Notification center: Redis + asynq task queue for the resurfaced-
	// domain periodic sweep and per-domain due-date one-shot scans. See
	// docs/superpowers/specs/2026-08-04-notifications-design.md.
	notifyEnqueuer := notify.NewEnqueuer(*redisAddr)
	notifySrv := notify.NewServer(*redisAddr, store, sc)
	if err := notifySrv.Start(); err != nil {
		log.Fatalf("notify: %v", err)
	}
	defer notifySrv.Shutdown()
```

- [ ] **Step 4: Pass the enqueuer into `RegisterRoutes`**

Update the call at line 192:

```go
	server.RegisterRoutes(r, store, sc, broadcaster, *cookieSecure, whois.Fetch, favicon.Fetch, subfinderFetch, ipFetch, whois.FetchIP, notifyEnqueuer)
```

(This will not compile until Task 5 adds the new trailing parameter to `RegisterRoutes` — that's expected; Tasks 4 and 5 land together in review before either is considered done. If executing tasks strictly in order, `go build ./...` is expected to fail after this step until Task 5's Step 3 lands; do not treat that as this task's failure.)

- [ ] **Step 5: Add `redis` to `docker-compose.yml`**

Add `REDIS_ADDR` to `server.environment` and a new `redis` service:

```yaml
services:
  server:
    build: .
    ports:
      - "8080:8080"
      - "50051:50051"
    environment:
      DB_URL: "${DB_URL}"
      MINIO_ENDPOINT: "minio:9000"
      MINIO_ACCESS_KEY: "minioadmin"
      MINIO_SECRET_KEY: "minioadmin"
      MINIO_BUCKET: "screenshots"
      CRAWLER_ADDR: "crawler:50052"
      CRAWLER_TOKEN: "${CRAWLER_TOKEN}"
      REDIS_ADDR: "redis:6379"
    depends_on:
      minio:
        condition: service_started
      crawler:
        condition: service_started
      redis:
        condition: service_started
    restart: unless-stopped

  # ... crawler/minio/minio-init unchanged ...

  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes
    volumes:
      - redisdata:/data
    restart: unless-stopped

volumes:
  miniodata:
  redisdata:
```

- [ ] **Step 6: Add `redis` to `docker-compose.dev.yml`**

```yaml
services:
  server:
    environment:
      DB_URL: "host=postgres user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable"
      COOKIE_SECURE: "false"
      REDIS_ADDR: "redis:6379"
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy

  # ... postgres unchanged ...

  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes
    ports:
      - "6379:6379"
    volumes:
      - redisdata:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 5s
      retries: 5

volumes:
  pgdata:
  redisdata:
```

- [ ] **Step 7: Start/wait for redis in `dev.sh` and pass `--redis-addr`**

After the "Starting PostgreSQL" block (before "Starting MinIO"):

```bash
echo "==> Starting Redis..."
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d redis
echo -n "    Waiting for Redis"
until docker compose -f docker-compose.yml -f docker-compose.dev.yml exec -T redis redis-cli ping 2>/dev/null | grep -q PONG; do
  echo -n "."
  sleep 1
done
echo " ready"
```

Update the `cleanup()` function's stop line and the `go run ./cmd/server/` invocation:

```bash
  docker compose -f docker-compose.yml -f docker-compose.dev.yml stop postgres minio redis
```

```bash
echo "==> Starting server on :8080..."
go run ./cmd/server/ \
  --db-url "$DB_URL" \
  --http-addr :8080 \
  --grpc-addr :50051 \
  --crawler-addr localhost:50052 \
  --crawler-token "$CRAWLER_TOKEN" \
  --redis-addr localhost:6379 \
  --subfinder-path "$(go env GOPATH)/bin/subfinder" \
  --cookie-secure=false \
  --bootstrap-admin-username admin \
  --bootstrap-admin-password admin \
  --seed-dns dns-server.yaml > >(sed -u 's/^/[server] /') 2>&1 &
SERVER_PID=$!
```

- [ ] **Step 8: Commit**

```bash
git add cmd/server/main.go docker-compose.yml docker-compose.dev.yml dev.sh
git commit -m "feat(server): wire Redis + notify task queue into cmd/server and dev.sh"
```

(This commit intentionally leaves `go build` red until Task 5 lands — see the note in Step 4. If your workflow requires green-at-every-commit, squash Tasks 4 and 5 into one commit instead.)

---

### Task 5: Reschedule due-date tasks from `PATCH`/`DELETE /api/urls/{id}`

**Files:**
- Modify: `internal/server/handlers.go`
- Modify: `internal/server/router.go`
- Modify: `internal/server/handlers_test.go`

**Interfaces:**
- Consumes: `notify.Enqueuer.RescheduleDueDate` (Task 2) structurally, via a new local `dueDateRescheduler` interface — no import of `internal/notify` in this package.
- Produces: `Handlers.notify dueDateRescheduler` field; `NewHandlers`'s and `RegisterRoutes`'s new trailing parameter (this is what Task 4's `main.go` call and the existing `handlers_test.go:961` call site both need updated for).

- [ ] **Step 1: Write the failing test — append to `internal/server/handlers_test.go`**

```go
type rescheduleCall struct {
	departmentID, urlID uint
	dueDate              *time.Time
}

type fakeNotifier struct {
	mu    sync.Mutex
	calls []rescheduleCall
}

func (f *fakeNotifier) RescheduleDueDate(departmentID, urlID uint, dueDate *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, rescheduleCall{departmentID, urlID, dueDate})
	return nil
}

func TestToggleURL_ReschedulesDueDateTask(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	notifier := &fakeNotifier{}
	r := chi.NewRouter()
	server.RegisterRoutes(r, store, nil, nil, false, nil, nil, nil, nil, nil, notifier)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Header.Set("X-Requested-With", "fetch")
		r.ServeHTTP(w, req)
	})

	body, _ := json.Marshal(map[string]string{"ordered_at": "2026-01-15T00:00:00Z"})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.calls) != 1 {
		t.Fatalf("expected 1 RescheduleDueDate call, got %d", len(notifier.calls))
	}
	call := notifier.calls[0]
	if call.departmentID != deptID || call.urlID != 1 {
		t.Fatalf("unexpected call args: %+v", call)
	}
	if call.dueDate == nil {
		t.Fatal("expected a non-nil due date")
	}
}

func TestRemoveFromWatchlist_CancelsDueDateTask(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	notifier := &fakeNotifier{}
	r := chi.NewRouter()
	server.RegisterRoutes(r, store, nil, nil, false, nil, nil, nil, nil, nil, notifier)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Header.Set("X-Requested-With", "fetch")
		r.ServeHTTP(w, req)
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/urls/1", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.calls) != 1 {
		t.Fatalf("expected 1 RescheduleDueDate call, got %d", len(notifier.calls))
	}
	if notifier.calls[0].dueDate != nil {
		t.Fatal("expected a nil due date on removal (cancel-only)")
	}
}
```

Add `"sync"` to the test file's imports if not already present (it is — `fullMockStore.scheduleMu` already uses it).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/... -run 'TestToggleURL_ReschedulesDueDateTask|TestRemoveFromWatchlist_CancelsDueDateTask' -v`
Expected: FAIL — build error, `RegisterRoutes` called with too many args.

- [ ] **Step 3: Add `dueDateRescheduler`, thread it through `Handlers`/`NewHandlers` (`internal/server/handlers.go`)**

```go
// dueDateRescheduler is the subset of internal/notify.Enqueuer ToggleURL and
// RemoveFromWatchlist need, narrowed to a local interface (same pattern as
// Scanner's crawlerClient) so this package never imports internal/notify and
// handler tests can inject a fake instead of a real asynq/Redis client.
// TODO(url-compliance-case-fields): rename the callers' "ordered date"
// language to "due date" once that branch's DepartmentURL.OrderedAt ->
// DueDate rename merges.
type dueDateRescheduler interface {
	RescheduleDueDate(departmentID, urlID uint, dueDate *time.Time) error
}

type Handlers struct {
	store          db.Store
	scanner        *Scanner
	broadcaster    *Broadcaster
	whoisFetch     whois.Fetcher     // nil disables the lazy on-add fetch (e.g. in tests)
	faviconFetch   favicon.Fetcher   // nil disables on-demand favicon fetching (e.g. in tests)
	subfinderFetch subfinder.Fetcher // nil disables the lazy on-add + refresh subdomain enumeration (e.g. in tests)
	ipFetch        ipinfo.Fetcher    // nil disables the on-demand hosting-info refresh (e.g. in tests)
	netnameFetch   whois.IPFetcher   // nil disables the NetName/abuse-email half of a hosting-info refresh
	notify         dueDateRescheduler // nil disables due-date task scheduling (e.g. in tests that don't care)
}

func NewHandlers(store db.Store, scanner *Scanner, broadcaster *Broadcaster, whoisFetch whois.Fetcher, faviconFetch favicon.Fetcher, subfinderFetch subfinder.Fetcher, ipFetch ipinfo.Fetcher, netnameFetch whois.IPFetcher, notify dueDateRescheduler) *Handlers {
	return &Handlers{store: store, scanner: scanner, broadcaster: broadcaster, whoisFetch: whoisFetch, faviconFetch: faviconFetch, subfinderFetch: subfinderFetch, ipFetch: ipFetch, netnameFetch: netnameFetch, notify: notify}
}
```

- [ ] **Step 4: Call it from `ToggleURL` (after the `OrderedAt` branch succeeds, before the final `found` check — insert right after line 281's `found = found || f`, still inside the `if body.OrderedAt != nil` block)**

```go
		if body.OrderedAt != nil {
			var orderedAt *time.Time
			if *body.OrderedAt != "" {
				t, err := time.Parse(time.RFC3339, *body.OrderedAt)
				if err != nil {
					writeError(w, http.StatusBadRequest, "invalid ordered_at, expected RFC3339")
					return
				}
				orderedAt = &t
			}
			f, err := h.store.SetURLOrderedAt(r.Context(), *user.DepartmentID, uint(id), orderedAt)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			found = found || f
			if f && h.notify != nil {
				if err := h.notify.RescheduleDueDate(*user.DepartmentID, uint(id), orderedAt); err != nil {
					log.Printf("notify: reschedule due-date task for department=%d url=%d: %v", *user.DepartmentID, id, err)
				}
			}
		}
```

- [ ] **Step 5: Call it from `RemoveFromWatchlist` (after `removed` is confirmed true, before the `204` write — `internal/server/handlers.go:213-222`)**

```go
	removed, err := h.store.RemoveURLFromWatchlist(r.Context(), *user.DepartmentID, uint(id))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, "url not on this department's watchlist")
		return
	}
	if h.notify != nil {
		if err := h.notify.RescheduleDueDate(*user.DepartmentID, uint(id), nil); err != nil {
			log.Printf("notify: cancel due-date task for department=%d url=%d: %v", *user.DepartmentID, id, err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
```

- [ ] **Step 6: Update `RegisterRoutes` (`internal/server/router.go`)**

```go
func RegisterRoutes(r chi.Router, store db.Store, scanner *Scanner, broadcaster *Broadcaster, cookieSecure bool, whoisFetch whois.Fetcher, faviconFetch favicon.Fetcher, subfinderFetch subfinder.Fetcher, ipFetch ipinfo.Fetcher, netnameFetch whois.IPFetcher, notify dueDateRescheduler) {
	h := NewHandlers(store, scanner, broadcaster, whoisFetch, faviconFetch, subfinderFetch, ipFetch, netnameFetch, notify)
```

- [ ] **Step 7: Update the existing call sites**

`internal/server/handlers_test.go:961` (`setupRouter`):

```go
	server.RegisterRoutes(r, store, sc, nil, false, nil, nil, nil, nil, nil, nil)
```

`cmd/server/main.go:192` was already updated in Task 4 Step 4 to pass `notifyEnqueuer` as the new final argument — confirm it now compiles.

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/server/... -v`
Expected: PASS (full package, to catch any other call site this plan missed)

- [ ] **Step 9: Build the whole module to confirm Task 4 + 5 now compile together**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 10: Commit**

```bash
git add internal/server/handlers.go internal/server/router.go internal/server/handlers_test.go
git commit -m "feat(server): reschedule due-date tasks on watchlist PATCH/DELETE"
```

---

### Task 6: Notification REST endpoints

**Files:**
- Create: `internal/server/notification_handlers.go`
- Modify: `internal/server/router.go`
- Modify: `internal/server/handlers_test.go`

**Interfaces:**
- Consumes: `db.NotificationStore` (Task 1), `userFromContext`/`writeJSON`/`writeError`/`writeInternalError` (existing `handlers.go` helpers).
- Produces: `GET /api/notifications`, `GET /api/notifications/unread-count`, `PATCH /api/notifications/{id}/read` — consumed by Task 7's frontend `api/notifications.ts`.

- [ ] **Step 1: Add `NotificationStore` fields/methods to `fullMockStore` (`internal/server/handlers_test.go`)**

Add a field to the struct (near `urlOffences`):

```go
	notifications  []db.Notification
```

Append these methods anywhere in the file (grouped near the other simple-table mocks, e.g. after the `ISPLogo` mock methods):

```go
func (m *fullMockStore) GetURLByID(_ context.Context, id uint) (*db.URL, error) {
	for i := range m.urls {
		if m.urls[i].ID == id {
			return &m.urls[i], nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) CreateNotification(_ context.Context, n db.Notification) (db.Notification, error) {
	n.ID = uint(len(m.notifications) + 1)
	n.CreatedAt = time.Now()
	m.notifications = append(m.notifications, n)
	return n, nil
}

func (m *fullMockStore) ListNotifications(_ context.Context, page, pageSize int) ([]db.Notification, int, error) {
	return m.listNotifications(page, pageSize, nil)
}

func (m *fullMockStore) ListNotificationsForDepartment(_ context.Context, page, pageSize int, departmentID uint) ([]db.Notification, int, error) {
	return m.listNotifications(page, pageSize, &departmentID)
}

func (m *fullMockStore) listNotifications(page, pageSize int, departmentID *uint) ([]db.Notification, int, error) {
	var filtered []db.Notification
	for _, n := range m.notifications {
		if departmentID == nil || n.DepartmentID == *departmentID {
			filtered = append(filtered, n)
		}
	}
	total := len(filtered)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return filtered[start:end], total, nil
}

func (m *fullMockStore) UnreadCount(_ context.Context) (int, error) { return m.unreadCount(nil), nil }

func (m *fullMockStore) UnreadCountForDepartment(_ context.Context, departmentID uint) (int, error) {
	return m.unreadCount(&departmentID), nil
}

func (m *fullMockStore) unreadCount(departmentID *uint) int {
	count := 0
	for _, n := range m.notifications {
		if n.ReadAt == nil && (departmentID == nil || n.DepartmentID == *departmentID) {
			count++
		}
	}
	return count
}

func (m *fullMockStore) GetNotification(_ context.Context, id uint) (*db.Notification, error) {
	for i := range m.notifications {
		if m.notifications[i].ID == id {
			return &m.notifications[i], nil
		}
	}
	return nil, nil
}

func (m *fullMockStore) MarkNotificationRead(_ context.Context, id uint) error {
	for i := range m.notifications {
		if m.notifications[i].ID == id {
			now := time.Now()
			m.notifications[i].ReadAt = &now
			return nil
		}
	}
	return nil
}

func (m *fullMockStore) HasRecentResurfacedNotification(_ context.Context, departmentID uint, urlValue string, since time.Time) (bool, error) {
	for _, n := range m.notifications {
		if n.DepartmentID == departmentID && n.URLValue == urlValue && n.Type == "resurfaced" && !n.CreatedAt.Before(since) {
			return true, nil
		}
	}
	return false, nil
}
```

(`GetURLByID` was needed by Task 1's `URLStore` addition but not yet added to the mock — Task 5's tests happened to not exercise it. Adding it here, grouped with the rest of this task's additions, keeps the mock buildable against `db.Store`.)

- [ ] **Step 2: Write the failing tests — append to `internal/server/handlers_test.go`**

```go
func TestListNotifications_ScopesToOwnDepartment(t *testing.T) {
	deptA, deptB := uint(1), uint(2)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptA, URLValue: "a.com", Type: "resurfaced"},
			{ID: 2, DepartmentID: deptB, URLValue: "b.com", Type: "resurfaced"},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/notifications", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Notifications []db.Notification `json:"notifications"`
		Total         int                `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 1 || len(body.Notifications) != 1 || body.Notifications[0].URLValue != "a.com" {
		t.Fatalf("expected only deptA's notification, got %+v", body)
	}
}

func TestUnreadNotificationCount(t *testing.T) {
	deptA := uint(1)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptA, URLValue: "a.com", Type: "resurfaced"},
			{ID: 2, DepartmentID: deptA, URLValue: "b.com", Type: "resurfaced", ReadAt: ptrTime(time.Now())},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/notifications/unread-count", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Count int `json:"count"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Count != 1 {
		t.Fatalf("expected count 1, got %d", body.Count)
	}
}

func TestMarkNotificationRead_404sForOtherDepartment(t *testing.T) {
	deptA, deptB := uint(1), uint(2)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptB, URLValue: "b.com", Type: "resurfaced"},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/notifications/1/read", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for another department's notification, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMarkNotificationRead_Success(t *testing.T) {
	deptA := uint(1)
	store := &fullMockStore{
		notifications: []db.Notification{
			{ID: 1, DepartmentID: deptA, URLValue: "a.com", Type: "resurfaced"},
		},
	}
	cookie := deptCookie(store, deptA)
	r := setupRouter(store, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/notifications/1/read", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.notifications[0].ReadAt == nil {
		t.Fatal("expected ReadAt to be set")
	}
}
```

Add a small helper near the other test helpers if one doesn't already exist:

```go
func ptrTime(t time.Time) *time.Time { return &t }
```

(Check first — grep the file for `func ptrTime` or an equivalent before adding; several handler tests likely already need a `*time.Time` literal helper. Reuse it if present instead of duplicating.)

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/server/... -run 'TestListNotifications|TestUnreadNotificationCount|TestMarkNotificationRead' -v`
Expected: FAIL — handlers undefined / routes 404.

- [ ] **Step 4: Write `internal/server/notification_handlers.go`**

```go
package server

import (
	"net/http"
	"strconv"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/go-chi/chi/v5"
)

const (
	defaultNotificationPageSize = 20
	maxNotificationPageSize     = 100
)

// ListNotifications — GET /api/notifications?page=&page_size= — same
// admin-global/department-scoped branch shape as ResurfacedDomains/ISPStats.
func (h *Handlers) ListNotifications(w http.ResponseWriter, r *http.Request) {
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	pageSize := defaultNotificationPageSize
	if ps, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && ps > 0 && ps <= maxNotificationPageSize {
		pageSize = ps
	}

	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var notifications []db.Notification
	var total int
	var err error
	if user.IsAdmin {
		notifications, total, err = h.store.ListNotifications(r.Context(), page, pageSize)
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		notifications, total, err = h.store.ListNotificationsForDepartment(r.Context(), page, pageSize, *user.DepartmentID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": notifications, "total": total})
}

// UnreadNotificationCount — GET /api/notifications/unread-count
func (h *Handlers) UnreadNotificationCount(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var count int
	var err error
	if user.IsAdmin {
		count, err = h.store.UnreadCount(r.Context())
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		count, err = h.store.UnreadCountForDepartment(r.Context(), *user.DepartmentID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

// MarkNotificationRead — PATCH /api/notifications/{id}/read. 404s (not
// 403) for a notification owned by another department, matching the
// requireDomainOwnership convention used by /api/results etc. — avoids
// confirming the notification exists to a department that can't see it.
func (h *Handlers) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	n, err := h.store.GetNotification(r.Context(), uint(id))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if n == nil {
		writeError(w, http.StatusNotFound, "notification not found")
		return
	}
	if !user.IsAdmin && (user.DepartmentID == nil || *user.DepartmentID != n.DepartmentID) {
		writeError(w, http.StatusNotFound, "notification not found")
		return
	}
	if err := h.store.MarkNotificationRead(r.Context(), uint(id)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Register the routes (`internal/server/router.go`, inside the `requireAuth` group, alongside `/resurfaced` at line 77)**

```go
			r.Get("/resurfaced", h.ResurfacedDomains)
			r.Get("/notifications", h.ListNotifications)
			r.Get("/notifications/unread-count", h.UnreadNotificationCount)
			r.Patch("/notifications/{id}/read", h.MarkNotificationRead)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/server/notification_handlers.go internal/server/router.go internal/server/handlers_test.go
git commit -m "feat(server): add notification list/unread-count/mark-read endpoints"
```

---

### Task 7: Frontend — types, API module, navbar bell

**Files:**
- Modify: `web/src/api/types.ts`
- Create: `web/src/api/notifications.ts`
- Create: `web/src/components/notification-bell.tsx`
- Modify: `web/src/routes/__root.tsx`

**Interfaces:**
- Consumes: `GET /api/notifications`, `GET /api/notifications/unread-count`, `PATCH /api/notifications/{id}/read` (Task 6); `api` client from `web/src/api/client.ts`.
- Produces: `<NotificationBell />` mounted in the root layout's navbar actions.

This task has no automated test (no existing frontend test runner in this repo — verification is `npm run build`/`npm run lint` plus manual browser check, both in Task 8).

- [ ] **Step 1: Add types to `web/src/api/types.ts`** (append at the end)

```ts
export type Notification = {
  id: number
  department_id: number
  url_id: number
  url: string
  type: 'resurfaced' | 'due_date_reached'
  compliant?: boolean
  details?: Record<string, unknown>
  read_at?: string
  created_at: string
}

export type NotificationsResponse = { notifications: Notification[]; total: number }
```

- [ ] **Step 2: Write `web/src/api/notifications.ts`**

```ts
import { api } from './client'
import type { NotificationsResponse } from './types'

export async function fetchUnreadCount(): Promise<number> {
  const data = await api.get<{ count: number }>('/notifications/unread-count')
  return data.count
}

export async function fetchNotifications(page = 1, pageSize = 20): Promise<NotificationsResponse> {
  const data = await api.get<NotificationsResponse>(`/notifications?page=${page}&page_size=${pageSize}`)
  return {
    notifications: Array.isArray(data.notifications) ? data.notifications : [],
    total: data.total ?? 0,
  }
}

export async function markNotificationRead(id: number): Promise<void> {
  await api.patch<void>(`/notifications/${id}/read`, {})
}
```

- [ ] **Step 3: Write `web/src/components/notification-bell.tsx`**

```tsx
import { useCallback, useEffect, useRef, useState } from 'react'
import { Bell } from 'lucide-react'
import { useNavigate } from '@tanstack/react-router'
import { fetchUnreadCount, fetchNotifications, markNotificationRead } from '@/api/notifications'
import type { Notification } from '@/api/types'

const POLL_MS = 30000

export function NotificationBell() {
  const [unread, setUnread] = useState(0)
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<Notification[]>([])
  const navigate = useNavigate()
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  const refreshCount = useCallback(() => {
    fetchUnreadCount().then(setUnread).catch(() => {})
  }, [])

  useEffect(() => {
    refreshCount()
    pollRef.current = setInterval(refreshCount, POLL_MS)
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [refreshCount])

  useEffect(() => {
    if (!open) return
    const onClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onClickOutside)
    return () => document.removeEventListener('mousedown', onClickOutside)
  }, [open])

  const toggleOpen = () => {
    const next = !open
    setOpen(next)
    if (next) {
      fetchNotifications(1, 10).then(res => setItems(res.notifications)).catch(() => {})
    }
  }

  const handleClick = async (n: Notification) => {
    setOpen(false)
    if (!n.read_at) {
      setUnread(c => Math.max(0, c - 1))
      try {
        await markNotificationRead(n.id)
      } catch {
        // best-effort — the badge will self-correct on the next poll
      }
    }
    navigate({ to: '/domain/$url', params: { url: n.url } })
  }

  return (
    <div ref={containerRef} style={{ position: 'relative' }}>
      <button
        type="button"
        className="btn-ghost"
        aria-label={unread > 0 ? `Notifications, ${unread} unread` : 'Notifications'}
        onClick={toggleOpen}
        style={{ position: 'relative', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 36, height: 36 }}
      >
        <Bell size={18} />
        {unread > 0 && (
          <span
            style={{
              position: 'absolute', top: 2, right: 2, minWidth: 15, height: 15, borderRadius: 999,
              background: 'var(--accent, #4338ca)', color: 'white', fontSize: 10, lineHeight: '15px',
              textAlign: 'center', padding: '0 3px',
            }}
          >
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>

      {open && (
        <div
          className="absolute right-0 z-50 rounded-lg border border-stone-border bg-background shadow-[0_4px_20px_rgba(0,0,0,0.12)] dark:shadow-[0_4px_20px_rgba(0,0,0,0.4)] overflow-y-auto"
          style={{ top: 'calc(100% + 8px)', width: 340, maxHeight: 400 }}
        >
          {items.length === 0 ? (
            <p style={{ padding: 16, fontSize: '0.85rem', opacity: 0.6 }}>No notifications yet.</p>
          ) : (
            items.map(n => (
              <button
                key={n.id}
                type="button"
                onClick={() => handleClick(n)}
                className="block w-full text-left px-3 py-2 text-sm bg-transparent border-none cursor-pointer font-[inherit] hover:bg-stone-panel transition-colors duration-100"
                style={{ borderBottom: '1px solid var(--stone-border, rgba(0,0,0,0.08))', opacity: n.read_at ? 0.6 : 1 }}
              >
                <div style={{ fontWeight: 600 }}>{n.url}</div>
                <div style={{ fontSize: '0.75rem', opacity: 0.7 }}>
                  {n.type === 'resurfaced'
                    ? 'Resurfaced — blocked domain is resolving again'
                    : `Due-date scan: ${n.compliant ? 'compliant' : 'still violating'}`}
                </div>
              </button>
            ))
          )}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Mount it in `web/src/routes/__root.tsx`**

Add the import (alongside the other component imports near the top):

```ts
import { NotificationBell } from '@/components/notification-bell'
```

Add `<NotificationBell />` into the `actions` fragment, before `<ThemeSwitch .../>` (`internal/server` route file, around line 396):

```tsx
              <NotificationBell />
              <ThemeSwitch className="bg-transparent"/>
              <LogoutButton />
```

- [ ] **Step 5: Manual browser check**

Run `./dev.sh`, log in, confirm the bell renders in the navbar with no console errors and the dropdown opens/closes (empty state is fine — no notifications exist yet until Task 8's manual verification creates some). This is a visual smoke check, not a substitute for Task 8's full manual verification.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/types.ts web/src/api/notifications.ts web/src/components/notification-bell.tsx web/src/routes/__root.tsx
git commit -m "feat(web): add notification bell with unread badge to navbar"
```

---

### Task 8: CLAUDE.md documentation + full verification

**Files:**
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: nothing new — this is documentation + verification only.

- [ ] **Step 1: Add the `--redis-addr` flag to the Server flags list in `CLAUDE.md`'s Commands section**

Insert alongside the other server flags (near `--crawler-addr`):

```
--redis-addr localhost:6379        # env: REDIS_ADDR — Redis address for the notification task queue (asynq); see "Notifications" below
```

- [ ] **Step 2: Mention the redis service in the Docker section's comment block**

Add one line noting the new service, next to the existing MinIO description (e.g. after the `ENTRYPOINT is /app/server...` paragraph): `docker-compose.yml`/`docker-compose.dev.yml` also run a `redis` service (with AOF persistence) backing the notification task queue — see "Notifications" below.

- [ ] **Step 3: Add a new `### Notifications` subsection**, placed after "### Domain normalization & watchlists" and before "### Storage":

```markdown
### Notifications (`internal/notify/`, `db.Notification`)

- A bell-icon notification center covering two event types, both backed by Redis + [`hibiken/asynq`](https://github.com/hibiken/asynq) rather than a plain DB-polling sweep, chosen for precise per-domain due-date timing (some SLAs are hours, not "whenever the next periodic sweep runs") plus built-in retry/scheduling. The asynq worker + scheduler run embedded inside `cmd/server` as extra goroutines (`notify.Server`, started/stopped alongside `StartScheduler`/`StartWhoisRefresher` in `main.go`) — no third binary. See `docs/superpowers/specs/2026-08-04-notifications-design.md` for the full design rationale.
- **Producer 1 — resurfaced domains** (`notify.Server.handleResurfacedSweep`, a periodic asynq task registered at a fixed `@every 15m`, no admin setting): iterates every department, reuses `store.ResurfacedDomainsForDepartment` (see "Resurfaced domains" above), and inserts a `db.Notification{Type: "resurfaced"}` row per (department, url) not already notified for that specific resurface event — deduped via `store.HasRecentResurfacedNotification` (an existing row's `CreatedAt` at/after the resurface's `ResurfacedAt` means it was already reported).
- **Producer 2 — due-date reached** (`notify.Enqueuer.RescheduleDueDate`, called from `PATCH /api/urls/{id}` in `internal/server/handlers.go`'s `ToggleURL` whenever `DepartmentURL.OrderedAt` changes, and from `RemoveFromWatchlist` on removal): deletes any previously-scheduled task for that `(department, url)` pair (deterministic `asynq.TaskID` = `due:<department_id>:<url_id>`) then, if the new date is non-nil and in the future, enqueues a one-shot task via `asynq.ProcessAt`. Clearing the date or removing the watchlist entry just deletes, without a replacement. TODO(url-compliance-case-fields): this integrates against `OrderedAt` — rename to `DueDate` once that branch's field rename merges.
- When a due-date task fires (`notify.Server.handleDueDateCheck`): calls `Scanner.Trigger(ctx, "due-date-check", []string{url})` (the same targeted-scan mechanism "Scan Selected"/`TriggerScreenshot` use), polls `Triggerer.IsRunning()` until the scan completes, reads the resulting `ScanResult` rows via `store.ResultsByURL`, and inserts one `db.Notification{Type: "due_date_reached", Compliant: <all servers compliant>, Details: <per-server breakdown>}` row. asynq's default retry (bounded exponential backoff) covers transient failures, including `Scanner.Trigger` returning "scan already in progress."
- `db.Notification` (`department_id`, `url_id`+`url_value` — mirroring `ScanResult`'s dual FK+denormalized-value pattern, `type`, `compliant` nullable, `details` jsonb, `read_at`, `created_at`, indexed on `(department_id, read_at)`) is exposed via `NotificationStore` — `GET /api/notifications` (paginated, admin-global/department-scoped like `/api/resurfaced`), `GET /api/notifications/unread-count`, `PATCH /api/notifications/{id}/read` (404s, not 403s, for another department's notification — `requireDomainOwnership`'s pattern).
- Frontend: `web/src/components/notification-bell.tsx` (`NotificationBell`) — a bell icon + unread badge mounted in `glass-navbar.tsx`'s actions slot via `__root.tsx`, polling `GET /api/notifications/unread-count` every 30s (same lightweight pattern `scan-results.tsx` uses for scan status) and opening a small dropdown (`GET /api/notifications`, first page) on click; clicking an entry marks it read and navigates to `/domain/$url`. `web/src/api/notifications.ts` owns the three fetchers.
- Out of scope for now: per-user notification preferences, email/Slack delivery, an admin-configurable resurfaced-sweep interval, and real-time (SSE) push — the badge is polling-only.
```

- [ ] **Step 4: Run the full backend test suite**

Run: `go test ./...`
Expected: PASS (note: `internal/dns` makes real network calls to `8.8.8.8`/`google.com` per existing repo convention — failures there only if genuinely offline, not a regression from this work)

- [ ] **Step 5: Run the frontend build + lint**

Run: `cd web && npm run build && npm run lint`
Expected: both succeed (build includes `tsc --noEmit`)

- [ ] **Step 6: Manual `dev.sh` verification** (per the design spec's Testing section)

Run `./dev.sh`. With the stack up:
1. Add a domain to the watchlist, set its order/due date via the `urls.tsx` date picker to 1-2 minutes in the future.
2. Wait for the due date to pass; confirm a targeted scan fires (visible in server logs / `/results`) and a notification appears in the bell dropdown with the correct compliant/violating outcome.
3. Force a resurfaced-domain scenario: scan a domain compliant, then make it resolve again (e.g. via a DNS server that doesn't block it) and scan again; confirm a "resurfaced" notification appears within 15 minutes (or trigger the sweep manually if you have Redis CLI access, by inspecting the `asynq:scheduler` state — not required, waiting is fine for a manual check).
4. Confirm the unread badge count matches, and clicking a notification marks it read (badge decrements) and navigates to the domain page.

- [ ] **Step 7: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add Notifications section to CLAUDE.md"
```

- [ ] **Step 8: Update the Orca worktree comment**

Post a final status update via the `orca` CLI noting the feature is implemented, tested, and awaiting manual review/merge by the orchestrating session — do not merge or push yourself.

---

## Self-Review Notes

- **Spec coverage:** Infrastructure (redis in both compose files + `--redis-addr`) → Task 4. Producer 1 (periodic resurfaced sweep + dedup) → Task 3. Producer 2 (schedule/cancel on `OrderedAt` change, deterministic TaskID, delete-then-maybe-enqueue) → Task 2, wired at the handler level in Task 5. Due-date task firing (targeted scan, outcome read-back, notification insert) → Task 3. Notification table → Task 1. Delivery (bell + polling + dropdown) → Task 7. Error handling (asynq default retry, documented via a `ponytail:` comment) → Task 3. Testing (unit tests against fakes, SQLite-backed integration tests, manual `dev.sh` checklist) → Tasks 1-3 write the automated tests; Task 8 Step 6 is the manual checklist verbatim from the spec's Testing section. Out of scope items are explicitly not built (no email/SSE/per-user prefs/admin-configurable sweep interval) and are called out in the CLAUDE.md addition in Task 8.
- **OrderedAt/DueDate rename:** every integration point (`Enqueuer` doc comment, `dueDateRescheduler` doc comment, CLAUDE.md section) carries a `TODO(url-compliance-case-fields)` marker per the orchestrating session's instruction, without blocking on that sibling branch.
- **No import cycle:** `internal/notify` never imports `internal/server`; `internal/server` never imports `internal/notify` either — it only declares the small `dueDateRescheduler` interface locally (Go's structural typing lets `*notify.Enqueuer` satisfy it without either package naming the other). Only `cmd/server/main.go` imports both, to wire the concrete value in.
