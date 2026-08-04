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
