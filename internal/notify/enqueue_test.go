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
