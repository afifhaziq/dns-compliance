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
