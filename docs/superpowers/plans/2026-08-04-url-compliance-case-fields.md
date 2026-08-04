# URL Compliance Case Fields Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename `DepartmentURL.OrderedAt` to `DueDate` (via an idempotent, data-preserving column rename run before `AutoMigrate`) and add five Excel-sourced case-metadata fields (`Agency`, `ReferenceNumber`, `RequestingDept`, `Status`, `RequestedAt`) to a department's watchlist entry, threaded end-to-end through the store, `PATCH /api/urls/{id}`, and the `urls.tsx` watchlist UI.

**Architecture:** `internal/db` gets the model rename/additions plus one combined `UpdateDepartmentURLFields` store method (replacing the old single-purpose `SetURLOrderedAt`) so the handler doesn't need six near-duplicate setters. `internal/server`'s `ToggleURL` handler decodes an extended partial-update body and forwards to that one store call. The frontend mirrors the same shape: one `setUrlFields` API function, six new/renamed inline-editable cells in the watchlist table.

**Tech Stack:** Go + GORM (SQLite in-memory for `internal/db`/`internal/server` tests) · React 19 + TypeScript (Vite) for the frontend.

## Global Constraints

- New `DepartmentURL` columns: `Agency string`, `ReferenceNumber string`, `RequestingDept string`, `Status string` (plain, non-pointer — empty string means "unset"), `RequestedAt *time.Time` (nullable, same shape as the renamed `DueDate`).
- `Status` allow-list, validated server-side in the handler (not a DB enum): `""` (unset), `"requested"`, `"uplift"`, `"suspended"`.
- The `ordered_at` → `due_date` rename must run in `internal/db/db.go`'s `Connect`, **before** `AutoMigrate`, and must be idempotent (safe on a fresh DB with neither column, and safe to run on every restart after the first).
- `RequestingDept` is deliberately free text, not a foreign key to the app's own `Department` RBAC model (see spec background).
- No admin-curated lookup table for `Agency`/`RequestingDept`, and no bulk Excel import — out of scope for this plan.
- JSON wire field names: `due_date`, `agency`, `reference_number`, `requesting_dept`, `status`, `requested_at` (all replacing/joining the existing `ordered_at`/`enabled` body of `PATCH /api/urls/{id}`).

---

## Task 1: Backend data layer — rename + new fields (`internal/db`)

**Files:**
- Modify: `internal/db/models.go`
- Modify: `internal/db/db.go`
- Modify: `internal/db/store.go`
- Modify: `internal/db/postgres.go`
- Modify: `internal/db/postgres_test.go`
- Create: `internal/db/migrate_test.go`

**Interfaces:**
- Produces: `db.DepartmentURL{DueDate *time.Time, Agency string, ReferenceNumber string, RequestingDept string, Status string, RequestedAt *time.Time}`; `db.URLEntry` mirrors the same fields; `db.DepartmentURLFields{DueDate **time.Time, Agency *string, ReferenceNumber *string, RequestingDept *string, Status *string, RequestedAt **time.Time}`; `db.Store.UpdateDepartmentURLFields(ctx, departmentID, urlID uint, fields DepartmentURLFields) (bool, error)`; `db.ISPTimingResult.WithDueDateCount`.
- Consumes: nothing outside this package.

- [ ] **Step 1: Update `internal/db/models.go` — rename `DepartmentURL`/`URLEntry` fields, add the new columns, add `DepartmentURLFields`**

Replace the `DepartmentURL` and `URLEntry` block:

```go
type DepartmentURL struct {
	DepartmentID uint       `gorm:"primaryKey;autoIncrement:false" json:"department_id"`
	URLID        uint       `gorm:"primaryKey;autoIncrement:false" json:"url_id"`
	URL          URL        `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	Enabled      bool       `gorm:"not null;default:true" json:"enabled"`
	OrderedAt    *time.Time `json:"ordered_at,omitempty"` // optional: when the takedown order was issued for this domain, set at add-time or later
	CreatedAt    time.Time  `json:"created_at"`
}

// URLEntry is the department-scoped view of a URL, carrying the watchlist
// enabled flag and order date that the shared URL model does not have.
type URLEntry struct {
	ID        uint       `json:"id"`
	URL       string     `json:"url"`
	Enabled   bool       `json:"enabled"`
	OrderedAt *time.Time `json:"ordered_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}
```

with:

```go
type DepartmentURL struct {
	DepartmentID uint   `gorm:"primaryKey;autoIncrement:false" json:"department_id"`
	URLID        uint   `gorm:"primaryKey;autoIncrement:false" json:"url_id"`
	URL          URL    `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
	Enabled      bool   `gorm:"not null;default:true" json:"enabled"`
	// DueDate is the takedown-order SLA deadline (carries time-of-day —
	// some orders require blocking within 6h/24h). Renamed from OrderedAt;
	// see internal/db/db.go's Connect for the column-rename migration.
	DueDate *time.Time `json:"due_date,omitempty"`
	// Agency/ReferenceNumber/RequestingDept/Status/RequestedAt are
	// Excel-sourced case metadata, per-department-per-URL — plain columns,
	// no admin-curated lookup table. RequestingDept is deliberately free
	// text (e.g. a ministry name), distinct from the app's own CMOD/CRD
	// RBAC Department model.
	Agency          string `json:"agency,omitempty"`
	ReferenceNumber string `json:"reference_number,omitempty"`
	RequestingDept  string `json:"requesting_dept,omitempty"`
	// Status is requested | uplift | suspended, validated server-side
	// (internal/server/handlers.go) — independent of the derived Compliant
	// field; blocked/not-blocked already comes from scan results.
	Status      string     `json:"status,omitempty"`
	RequestedAt *time.Time `json:"requested_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// DepartmentURLFields is a partial update for one department's watchlist
// entry. Every field is optional (nil = leave untouched) — one flexible
// update path instead of a SetURLX method per column. DueDate/RequestedAt
// are double pointers so "clear" (set to NULL) is distinguishable from "not
// present in this update": outer nil = don't touch, outer non-nil pointing
// at a nil inner = clear, outer non-nil pointing at &t = set.
type DepartmentURLFields struct {
	DueDate         **time.Time
	Agency          *string
	ReferenceNumber *string
	RequestingDept  *string
	Status          *string
	RequestedAt     **time.Time
}

// URLEntry is the department-scoped view of a URL, carrying the watchlist
// case-management fields the shared URL model does not have.
type URLEntry struct {
	ID              uint       `json:"id"`
	URL             string     `json:"url"`
	Enabled         bool       `json:"enabled"`
	DueDate         *time.Time `json:"due_date,omitempty"`
	Agency          string     `json:"agency,omitempty"`
	ReferenceNumber string     `json:"reference_number,omitempty"`
	RequestingDept  string     `json:"requesting_dept,omitempty"`
	Status          string     `json:"status,omitempty"`
	RequestedAt     *time.Time `json:"requested_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}
```

Then find the `ISPTimingResult` struct (same file) and replace:

```go
// ISPTimingResult is the response shape for GET /api/isps/{isp}/timing.
// Median/avg are computed only over domains with Blocked=true; domains with
// no recorded order date are excluded entirely (WithOrderDateCount tracks
// coverage against TotalDomains so the figure isn't silently misleading).
type ISPTimingResult struct {
	ISP                string         `json:"isp"`
	MedianDaysToBlock  float64        `json:"median_days_to_block"`
	AvgDaysToBlock     float64        `json:"avg_days_to_block"`
	BlockedCount       int            `json:"blocked_count"`
	StillOpenCount     int            `json:"still_open_count"`
	WithOrderDateCount int            `json:"with_order_date_count"`
	TotalDomains       int            `json:"total_domains"`
	Slowest            []DomainTiming `json:"slowest"` // top 5 by days-to-block, blocked and still-open combined
}
```

with:

```go
// ISPTimingResult is the response shape for GET /api/isps/{isp}/timing.
// Median/avg are computed only over domains with Blocked=true; domains with
// no recorded due date are excluded entirely (WithDueDateCount tracks
// coverage against TotalDomains so the figure isn't silently misleading).
type ISPTimingResult struct {
	ISP               string         `json:"isp"`
	MedianDaysToBlock float64        `json:"median_days_to_block"`
	AvgDaysToBlock    float64        `json:"avg_days_to_block"`
	BlockedCount      int            `json:"blocked_count"`
	StillOpenCount    int            `json:"still_open_count"`
	WithDueDateCount  int            `json:"with_due_date_count"`
	TotalDomains      int            `json:"total_domains"`
	Slowest           []DomainTiming `json:"slowest"` // top 5 by days-to-block, blocked and still-open combined
}
```

- [ ] **Step 2: Add the idempotent rename migration to `internal/db/db.go`**

In `Connect`, insert the rename block between `gorm.Open` and `AutoMigrate`:

```go
	database, err := gorm.Open(dialector, &gorm.Config{Logger: gormLogger})
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}
	// DepartmentURL.OrderedAt was renamed to DueDate. AutoMigrate only adds
	// columns matching current struct tags — it never renames or drops — so
	// without this, an already-deployed instance would silently orphan the
	// column and its data (a new empty due_date column, stale ordered_at).
	// Idempotent: a no-op once due_date exists, including on a fresh DB
	// where ordered_at never existed either.
	if database.Migrator().HasColumn(&DepartmentURL{}, "ordered_at") && !database.Migrator().HasColumn(&DepartmentURL{}, "due_date") {
		if err := database.Migrator().RenameColumn(&DepartmentURL{}, "ordered_at", "due_date"); err != nil {
			return nil, fmt.Errorf("renaming ordered_at to due_date: %w", err)
		}
	}
	if err := database.AutoMigrate(
```

(The rest of `Connect` — the `AutoMigrate` call and everything after — is unchanged.)

- [ ] **Step 3: Write the migration test — `internal/db/migrate_test.go`**

```go
package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/db"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// legacyDepartmentURL mimics the pre-rename DepartmentURL shape (ordered_at,
// not due_date) to simulate an already-deployed database's schema before
// this migration runs.
type legacyDepartmentURL struct {
	DepartmentID uint `gorm:"primaryKey;autoIncrement:false"`
	URLID        uint `gorm:"primaryKey;autoIncrement:false"`
	Enabled      bool `gorm:"not null;default:true"`
	OrderedAt    *time.Time
	CreatedAt    time.Time
}

func (legacyDepartmentURL) TableName() string { return "department_urls" }

// TestConnect_RenamesOrderedAtToDueDate simulates an existing deployment: a
// department_urls table with the old ordered_at column, seeded with data.
// db.Connect must rename the column (not drop/recreate it) so the data
// survives under due_date.
func TestConnect_RenamesOrderedAtToDueDate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate.db")

	// Build the old schema directly (bypassing db.Connect, which only knows
	// about the current — already renamed — struct) and seed a row.
	oldDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open old schema db: %v", err)
	}
	if err := oldDB.AutoMigrate(&legacyDepartmentURL{}); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	seeded := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	if err := oldDB.Create(&legacyDepartmentURL{DepartmentID: 1, URLID: 1, Enabled: true, OrderedAt: &seeded}).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	oldSQLDB, err := oldDB.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	if err := oldSQLDB.Close(); err != nil {
		t.Fatalf("close old connection: %v", err)
	}

	// Reopen through the real db.Connect, which must detect ordered_at,
	// rename it to due_date, then AutoMigrate the rest of the current
	// schema (including the new case-metadata columns) on top.
	newDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}

	var got struct{ DueDate *time.Time }
	if err := newDB.Table("department_urls").
		Select("due_date").
		Where("department_id = ? AND url_id = ?", 1, 1).
		Scan(&got).Error; err != nil {
		t.Fatalf("query due_date: %v", err)
	}
	if got.DueDate == nil || !got.DueDate.Equal(seeded) {
		t.Fatalf("expected due_date to carry over the seeded ordered_at value, got %+v", got.DueDate)
	}
	if newDB.Migrator().HasColumn(&db.DepartmentURL{}, "ordered_at") {
		t.Fatal("expected ordered_at column to be gone after rename")
	}
}

// TestConnect_RenameIsIdempotent runs db.Connect twice against the same
// already-migrated database (the normal case for every restart after the
// first) and confirms it doesn't error or touch existing due_date data.
func TestConnect_RenameIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate_idempotent.db")

	firstDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("first db.Connect: %v", err)
	}
	due := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if err := firstDB.Table("department_urls").Create(map[string]interface{}{
		"department_id": 1, "url_id": 1, "enabled": true, "due_date": due,
	}).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}
	firstSQLDB, err := firstDB.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	if err := firstSQLDB.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}

	secondDB, err := db.Connect(sqlite.Open(dbPath))
	if err != nil {
		t.Fatalf("second db.Connect: %v", err)
	}
	var got struct{ DueDate *time.Time }
	if err := secondDB.Table("department_urls").
		Select("due_date").
		Where("department_id = ? AND url_id = ?", 1, 1).
		Scan(&got).Error; err != nil {
		t.Fatalf("query due_date: %v", err)
	}
	if got.DueDate == nil || !got.DueDate.Equal(due) {
		t.Fatalf("expected due_date to survive a second Connect call, got %+v", got.DueDate)
	}
}
```

- [ ] **Step 4: Run the new migration tests to verify they pass**

Run: `go test ./internal/db/... -run TestConnect_Rename -v`
Expected: PASS for both `TestConnect_RenamesOrderedAtToDueDate` and `TestConnect_RenameIsIdempotent`.

- [ ] **Step 5: Update `internal/db/store.go` — replace `SetURLOrderedAt` with `UpdateDepartmentURLFields`**

In the `URLStore` interface, replace this line:

```go
	SetURLOrderedAt(ctx context.Context, departmentID, urlID uint, orderedAt *time.Time) (bool, error) // nil clears the order date; false if the URL is not on that watchlist
```

with:

```go
	UpdateDepartmentURLFields(ctx context.Context, departmentID, urlID uint, fields DepartmentURLFields) (bool, error) // only non-nil fields in `fields` are applied; false if the URL is not on that watchlist
```

(`SetURLEnabled` stays unchanged — it's the one field that's always present as a plain bool, never optionally-cleared, so it doesn't need folding into the combined struct.)

- [ ] **Step 6: Update `internal/db/postgres.go` — `ListDepartmentURLs`, `SetURLOrderedAt` → `UpdateDepartmentURLFields`**

Replace `ListDepartmentURLs`:

```go
func (s *postgresStore) ListDepartmentURLs(ctx context.Context, departmentID uint) ([]URLEntry, error) {
	var entries []URLEntry
	err := s.db.WithContext(ctx).
		Table("urls").
		Select("urls.id, urls.url, urls.created_at, du.enabled, du.ordered_at").
		Joins("JOIN department_urls du ON du.url_id = urls.id AND du.department_id = ?", departmentID).
		Order("urls.created_at asc").
		Scan(&entries).Error
	return entries, err
}
```

with:

```go
func (s *postgresStore) ListDepartmentURLs(ctx context.Context, departmentID uint) ([]URLEntry, error) {
	var entries []URLEntry
	err := s.db.WithContext(ctx).
		Table("urls").
		Select("urls.id, urls.url, urls.created_at, du.enabled, du.due_date, du.agency, du.reference_number, du.requesting_dept, du.status, du.requested_at").
		Joins("JOIN department_urls du ON du.url_id = urls.id AND du.department_id = ?", departmentID).
		Order("urls.created_at asc").
		Scan(&entries).Error
	return entries, err
}
```

Replace `SetURLOrderedAt`:

```go
// SetURLOrderedAt sets or clears (orderedAt == nil) the takedown-order date
// for one department's watchlist entry. Optional field — leaving it unset
// just excludes the domain from time-to-compliance aggregates.
func (s *postgresStore) SetURLOrderedAt(ctx context.Context, departmentID, urlID uint, orderedAt *time.Time) (bool, error) {
	res := s.db.WithContext(ctx).
		Model(&DepartmentURL{}).
		Where("department_id = ? AND url_id = ?", departmentID, urlID).
		Update("ordered_at", orderedAt)
	return res.RowsAffected > 0, res.Error
}
```

with:

```go
// UpdateDepartmentURLFields applies a partial update to one department's
// watchlist entry's case-metadata fields — only non-nil fields in `fields`
// are touched, via one map-based UPDATE rather than one setter method per
// column.
func (s *postgresStore) UpdateDepartmentURLFields(ctx context.Context, departmentID, urlID uint, fields DepartmentURLFields) (bool, error) {
	updates := map[string]interface{}{}
	if fields.DueDate != nil {
		updates["due_date"] = *fields.DueDate
	}
	if fields.Agency != nil {
		updates["agency"] = *fields.Agency
	}
	if fields.ReferenceNumber != nil {
		updates["reference_number"] = *fields.ReferenceNumber
	}
	if fields.RequestingDept != nil {
		updates["requesting_dept"] = *fields.RequestingDept
	}
	if fields.Status != nil {
		updates["status"] = *fields.Status
	}
	if fields.RequestedAt != nil {
		updates["requested_at"] = *fields.RequestedAt
	}
	if len(updates) == 0 {
		return false, nil
	}
	res := s.db.WithContext(ctx).
		Model(&DepartmentURL{}).
		Where("department_id = ? AND url_id = ?", departmentID, urlID).
		Updates(updates)
	return res.RowsAffected > 0, res.Error
}
```

- [ ] **Step 7: Update `internal/db/postgres.go` — `ispComplianceTiming`**

Replace the entire `ispComplianceTiming` function body (from `func (s *postgresStore) ispComplianceTiming(ctx context.Context, isp string, departmentID *uint) (ISPTimingResult, error) {` through its closing `}` right before `func (s *postgresStore) ISPComplianceTiming(ctx context.Context, isp string) (ISPTimingResult, error) {`) with:

```go
// ispComplianceTiming computes time-to-block stats for one ISP, optionally
// scoped to one department's watchlist. Aggregated in Go, following the same
// SQLite-portability reasoning as dailyTrend/DailyComplianceByURL.
func (s *postgresStore) ispComplianceTiming(ctx context.Context, isp string, departmentID *uint) (ISPTimingResult, error) {
	// Plain (non-aggregated) column read, reduced to a min-per-url_id map in
	// Go: SQLite's driver can't scan a SQL MIN() of a datetime column
	// directly into time.Time, so the reduction happens here instead.
	type deptURLDueRow struct {
		URLID   uint
		DueDate time.Time
	}
	dueQuery := s.db.WithContext(ctx).
		Table("department_urls").
		Select("url_id, due_date").
		Where("due_date IS NOT NULL")
	if departmentID != nil {
		dueQuery = dueQuery.Where("department_id = ?", *departmentID)
	}
	var deptURLDueRows []deptURLDueRow
	if err := dueQuery.Scan(&deptURLDueRows).Error; err != nil {
		return ISPTimingResult{}, err
	}
	dueDateByURL := make(map[uint]time.Time, len(deptURLDueRows))
	for _, r := range deptURLDueRows {
		existing, ok := dueDateByURL[r.URLID]
		if !ok || r.DueDate.Before(existing) {
			dueDateByURL[r.URLID] = r.DueDate
		}
	}
	dueRows := make([]deptURLDueRow, 0, len(dueDateByURL))
	for urlID, dueDate := range dueDateByURL {
		dueRows = append(dueRows, deptURLDueRow{URLID: urlID, DueDate: dueDate})
	}

	// Total monitored domains in this scope — the denominator for the
	// "N domains have a recorded due date" coverage figure.
	totalQuery := s.db.WithContext(ctx).Table("department_urls").Select("COUNT(DISTINCT url_id)")
	if departmentID != nil {
		totalQuery = totalQuery.Where("department_id = ?", *departmentID)
	}
	var totalDomains int64
	if err := totalQuery.Scan(&totalDomains).Error; err != nil {
		return ISPTimingResult{}, err
	}

	// Compliant scans for this ISP, oldest first, so the first hit per url_id
	// found below is the first-observed-compliant timestamp.
	type complianceRow struct {
		URLID     uint
		URLValue  string
		ScannedAt time.Time
	}
	complianceQuery := s.db.WithContext(ctx).
		Table("scan_results").
		Select("scan_results.url_id, urls.url as url_value, scan_results.scanned_at").
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Joins("JOIN urls ON urls.id = scan_results.url_id").
		Where("dns_servers.isp = ? AND scan_results.compliant = true", isp).
		Order("scan_results.scanned_at asc")
	if departmentID != nil {
		complianceQuery = complianceQuery.Joins("JOIN department_urls du2 ON du2.url_id = scan_results.url_id AND du2.department_id = ?", *departmentID)
	}
	var complianceRows []complianceRow
	if err := complianceQuery.Scan(&complianceRows).Error; err != nil {
		return ISPTimingResult{}, err
	}

	firstCompliantAfterDue := make(map[uint]time.Time)
	domainNameByURL := make(map[uint]string)
	for _, r := range complianceRows {
		domainNameByURL[r.URLID] = r.URLValue
		dueDate, hasDue := dueDateByURL[r.URLID]
		if !hasDue {
			continue
		}
		if _, already := firstCompliantAfterDue[r.URLID]; already {
			continue
		}
		if r.ScannedAt.Before(dueDate) {
			continue // compliant scan predates the recorded due date — not this order's block event
		}
		firstCompliantAfterDue[r.URLID] = r.ScannedAt
	}

	// Domain names for due URLs that have no compliant scan yet (still open).
	var missingIDs []uint
	for _, r := range dueRows {
		if _, known := domainNameByURL[r.URLID]; !known {
			missingIDs = append(missingIDs, r.URLID)
		}
	}
	if len(missingIDs) > 0 {
		var missing []URL
		if err := s.db.WithContext(ctx).Where("id IN ?", missingIDs).Find(&missing).Error; err != nil {
			return ISPTimingResult{}, err
		}
		for _, u := range missing {
			domainNameByURL[u.ID] = u.URL
		}
	}

	now := time.Now()
	timings := make([]DomainTiming, 0, len(dueRows))
	var blockedDays []float64
	for _, r := range dueRows {
		domain := domainNameByURL[r.URLID]
		if firstCompliant, blocked := firstCompliantAfterDue[r.URLID]; blocked {
			days := firstCompliant.Sub(r.DueDate).Hours() / 24
			if days < 0 {
				days = 0 // due date recorded after the domain was already observed compliant
			}
			timings = append(timings, DomainTiming{Domain: domain, DaysToBlock: int(days + 0.5), Blocked: true})
			blockedDays = append(blockedDays, days)
		} else {
			waited := now.Sub(r.DueDate).Hours() / 24
			if waited < 0 {
				waited = 0
			}
			timings = append(timings, DomainTiming{Domain: domain, DaysToBlock: int(waited + 0.5), Blocked: false})
		}
	}

	sort.Slice(timings, func(i, j int) bool { return timings[i].DaysToBlock > timings[j].DaysToBlock })
	slowest := timings
	if len(slowest) > 5 {
		slowest = slowest[:5]
	}

	sort.Float64s(blockedDays)
	var median, avg float64
	if n := len(blockedDays); n > 0 {
		if n%2 == 1 {
			median = blockedDays[n/2]
		} else {
			median = (blockedDays[n/2-1] + blockedDays[n/2]) / 2
		}
		sum := 0.0
		for _, d := range blockedDays {
			sum += d
		}
		avg = sum / float64(n)
	}

	return ISPTimingResult{
		ISP:               isp,
		MedianDaysToBlock: median,
		AvgDaysToBlock:    avg,
		BlockedCount:      len(blockedDays),
		StillOpenCount:    len(dueRows) - len(blockedDays),
		WithDueDateCount:  len(dueRows),
		TotalDomains:      int(totalDomains),
		Slowest:           slowest,
	}, nil
}
```

- [ ] **Step 8: Update `internal/db/postgres_test.go`**

Replace `TestSetURLOrderedAt`:

```go
func TestSetURLOrderedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "TestDept4")
	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "example.com")

	orderedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	found, err := s.SetURLOrderedAt(ctx, dept.ID, u.ID, &orderedAt)
	if err != nil || !found {
		t.Fatalf("SetURLOrderedAt(set): found=%v err=%v", found, err)
	}

	entries, _ := s.ListDepartmentURLs(ctx, dept.ID)
	if len(entries) != 1 || entries[0].OrderedAt == nil || !entries[0].OrderedAt.Equal(orderedAt) {
		t.Fatalf("expected ordered_at to be set, got %+v", entries)
	}

	// Clear it
	found, err = s.SetURLOrderedAt(ctx, dept.ID, u.ID, nil)
	if err != nil || !found {
		t.Fatalf("SetURLOrderedAt(clear): found=%v err=%v", found, err)
	}
	entries, _ = s.ListDepartmentURLs(ctx, dept.ID)
	if len(entries) != 1 || entries[0].OrderedAt != nil {
		t.Fatalf("expected ordered_at to be cleared, got %+v", entries)
	}
}
```

with:

```go
func TestUpdateDepartmentURLFields_DueDate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "TestDept4")
	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "example.com")

	dueDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	duePtr := &dueDate
	found, err := s.UpdateDepartmentURLFields(ctx, dept.ID, u.ID, db.DepartmentURLFields{DueDate: &duePtr})
	if err != nil || !found {
		t.Fatalf("UpdateDepartmentURLFields(set due_date): found=%v err=%v", found, err)
	}

	entries, _ := s.ListDepartmentURLs(ctx, dept.ID)
	if len(entries) != 1 || entries[0].DueDate == nil || !entries[0].DueDate.Equal(dueDate) {
		t.Fatalf("expected due_date to be set, got %+v", entries)
	}

	// Clear it
	var nilTime *time.Time
	found, err = s.UpdateDepartmentURLFields(ctx, dept.ID, u.ID, db.DepartmentURLFields{DueDate: &nilTime})
	if err != nil || !found {
		t.Fatalf("UpdateDepartmentURLFields(clear due_date): found=%v err=%v", found, err)
	}
	entries, _ = s.ListDepartmentURLs(ctx, dept.ID)
	if len(entries) != 1 || entries[0].DueDate != nil {
		t.Fatalf("expected due_date to be cleared, got %+v", entries)
	}
}

func TestUpdateDepartmentURLFields_CaseMetadata(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "TestDept6")
	u, _ := s.AddURLToWatchlist(ctx, dept.ID, "casefields.com")

	agency, ref, reqDept, status := "MCMC", "REF-001", "Ministry of X", "requested"
	found, err := s.UpdateDepartmentURLFields(ctx, dept.ID, u.ID, db.DepartmentURLFields{
		Agency: &agency, ReferenceNumber: &ref, RequestingDept: &reqDept, Status: &status,
	})
	if err != nil || !found {
		t.Fatalf("UpdateDepartmentURLFields(case metadata): found=%v err=%v", found, err)
	}

	entries, _ := s.ListDepartmentURLs(ctx, dept.ID)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Agency != agency || e.ReferenceNumber != ref || e.RequestingDept != reqDept || e.Status != status {
		t.Fatalf("expected case metadata to be set, got %+v", e)
	}

	// Updating only Status must not clobber the other fields already set.
	newStatus := "uplift"
	found, err = s.UpdateDepartmentURLFields(ctx, dept.ID, u.ID, db.DepartmentURLFields{Status: &newStatus})
	if err != nil || !found {
		t.Fatalf("UpdateDepartmentURLFields(status only): found=%v err=%v", found, err)
	}
	entries, _ = s.ListDepartmentURLs(ctx, dept.ID)
	e = entries[0]
	if e.Status != newStatus {
		t.Fatalf("expected status to be updated, got %q", e.Status)
	}
	if e.Agency != agency || e.ReferenceNumber != ref || e.RequestingDept != reqDept {
		t.Fatalf("expected other case fields to remain untouched, got %+v", e)
	}
}
```

Replace `TestSetURLOrderedAtNotOnWatchlist`:

```go
func TestSetURLOrderedAtNotOnWatchlist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "TestDept5")
	u, _ := s.CreateURL(ctx, "notlinked2.com")

	orderedAt := time.Now()
	found, err := s.SetURLOrderedAt(ctx, dept.ID, u.ID, &orderedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for URL not on watchlist")
	}
}
```

with:

```go
func TestUpdateDepartmentURLFields_NotOnWatchlist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dept, _ := s.CreateDepartment(ctx, "TestDept5")
	u, _ := s.CreateURL(ctx, "notlinked2.com")

	dueDate := time.Now()
	duePtr := &dueDate
	found, err := s.UpdateDepartmentURLFields(ctx, dept.ID, u.ID, db.DepartmentURLFields{DueDate: &duePtr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for URL not on watchlist")
	}
}
```

In `TestISPComplianceTiming_BlockedAndStillOpen`, replace:

```go
	if _, err := s.SetURLOrderedAt(ctx, dept.ID, blocked.ID, &blockedOrder); err != nil {
		t.Fatalf("SetURLOrderedAt blocked: %v", err)
	}
	if _, err := s.SetURLOrderedAt(ctx, dept.ID, stillOpen.ID, &openOrder); err != nil {
		t.Fatalf("SetURLOrderedAt open: %v", err)
	}
```

with:

```go
	blockedOrderPtr, openOrderPtr := &blockedOrder, &openOrder
	if _, err := s.UpdateDepartmentURLFields(ctx, dept.ID, blocked.ID, db.DepartmentURLFields{DueDate: &blockedOrderPtr}); err != nil {
		t.Fatalf("UpdateDepartmentURLFields blocked: %v", err)
	}
	if _, err := s.UpdateDepartmentURLFields(ctx, dept.ID, stillOpen.ID, db.DepartmentURLFields{DueDate: &openOrderPtr}); err != nil {
		t.Fatalf("UpdateDepartmentURLFields open: %v", err)
	}
```

and, further down in the same test:

```go
	if timing.WithOrderDateCount != 2 {
		t.Fatalf("expected 2 domains with an order date, got %d", timing.WithOrderDateCount)
	}
```

with:

```go
	if timing.WithDueDateCount != 2 {
		t.Fatalf("expected 2 domains with a due date, got %d", timing.WithDueDateCount)
	}
```

In `TestISPComplianceTiming_NegativeClampedToZero`, replace:

```go
	if _, err := s.SetURLOrderedAt(ctx, dept.ID, u.ID, &orderedAt); err != nil {
		t.Fatalf("SetURLOrderedAt: %v", err)
	}
```

with:

```go
	orderedAtPtr := &orderedAt
	if _, err := s.UpdateDepartmentURLFields(ctx, dept.ID, u.ID, db.DepartmentURLFields{DueDate: &orderedAtPtr}); err != nil {
		t.Fatalf("UpdateDepartmentURLFields: %v", err)
	}
```

- [ ] **Step 9: Run the full `internal/db` test suite**

Run: `go test ./internal/db/... -v`
Expected: PASS for all tests, including the renamed/new ones from Step 8 and the migration tests from Step 3.

- [ ] **Step 10: Commit**

```bash
git add internal/db/models.go internal/db/db.go internal/db/store.go internal/db/postgres.go internal/db/postgres_test.go internal/db/migrate_test.go
git commit -m "db: rename DepartmentURL.OrderedAt to DueDate, add case-metadata fields"
```

---

## Task 2: Backend handler layer (`internal/server`)

**Files:**
- Modify: `internal/server/handlers.go`
- Modify: `internal/server/handlers_test.go`
- Modify: `internal/server/legal_handlers_test.go`

**Interfaces:**
- Consumes: `db.DepartmentURLFields`, `db.Store.UpdateDepartmentURLFields` (Task 1).
- Produces: extended `PATCH /api/urls/{id}` body handling — no new exported Go symbols outside the handler itself.

- [ ] **Step 1: Update `internal/server/handlers.go` — `ToggleURL`**

Replace the whole `ToggleURL` function (including its doc comment):

```go
// ToggleURL updates a URL in the caller's department watchlist: the enabled
// flag and/or the optional order date. Does not affect other departments
// watching the same domain. Only fields present in the body are touched —
// omit "enabled" to change only "ordered_at" and vice versa.
func (h *Handlers) ToggleURL(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if user.DepartmentID == nil {
		writeError(w, http.StatusForbidden, "user has no department")
		return
	}

	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var body struct {
		Enabled *bool `json:"enabled"`
		// OrderedAt is RFC3339 when setting a date, or "" to clear it.
		// Omit the key entirely to leave the order date untouched.
		OrderedAt *string `json:"ordered_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	found := false
	if body.Enabled != nil {
		f, err := h.store.SetURLEnabled(r.Context(), *user.DepartmentID, uint(id), *body.Enabled)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		found = found || f
	}
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
	}
	if !found {
		writeError(w, http.StatusNotFound, "url not on this department's watchlist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

with:

```go
// urlStatusAllowed is the server-side allow-list for DepartmentURL.Status —
// a free string column (not a DB enum), matching this codebase's existing
// string-enum convention (Instrument.Type, ScanRun.Status, etc). "" clears
// the field.
var urlStatusAllowed = map[string]bool{"": true, "requested": true, "uplift": true, "suspended": true}

// parseOptionalRFC3339 parses an RFC3339 timestamp, or returns nil for an
// empty string (clears the field).
func parseOptionalRFC3339(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ToggleURL updates a URL in the caller's department watchlist: the enabled
// flag and/or the optional case-metadata fields (due date, agency,
// reference number, requesting department, status, requested-at). Does not
// affect other departments watching the same domain. Only fields present in
// the body are touched — omit a key to leave it untouched.
func (h *Handlers) ToggleURL(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if user.DepartmentID == nil {
		writeError(w, http.StatusForbidden, "user has no department")
		return
	}

	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var body struct {
		Enabled *bool `json:"enabled"`
		// DueDate/RequestedAt are RFC3339 when setting a value, or "" to
		// clear. Omit the key entirely to leave the field untouched.
		DueDate         *string `json:"due_date"`
		Agency          *string `json:"agency"`
		ReferenceNumber *string `json:"reference_number"`
		RequestingDept  *string `json:"requesting_dept"`
		Status          *string `json:"status"`
		RequestedAt     *string `json:"requested_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	found := false
	if body.Enabled != nil {
		f, err := h.store.SetURLEnabled(r.Context(), *user.DepartmentID, uint(id), *body.Enabled)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		found = found || f
	}

	var fields db.DepartmentURLFields
	hasFields := false
	if body.DueDate != nil {
		dueDate, err := parseOptionalRFC3339(*body.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date, expected RFC3339")
			return
		}
		fields.DueDate = &dueDate
		hasFields = true
	}
	if body.Agency != nil {
		fields.Agency = body.Agency
		hasFields = true
	}
	if body.ReferenceNumber != nil {
		fields.ReferenceNumber = body.ReferenceNumber
		hasFields = true
	}
	if body.RequestingDept != nil {
		fields.RequestingDept = body.RequestingDept
		hasFields = true
	}
	if body.Status != nil {
		if !urlStatusAllowed[*body.Status] {
			writeError(w, http.StatusBadRequest, "invalid status, expected one of: requested, uplift, suspended")
			return
		}
		fields.Status = body.Status
		hasFields = true
	}
	if body.RequestedAt != nil {
		requestedAt, err := parseOptionalRFC3339(*body.RequestedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid requested_at, expected RFC3339")
			return
		}
		fields.RequestedAt = &requestedAt
		hasFields = true
	}
	if hasFields {
		f, err := h.store.UpdateDepartmentURLFields(r.Context(), *user.DepartmentID, uint(id), fields)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		found = found || f
	}

	if !found {
		writeError(w, http.StatusNotFound, "url not on this department's watchlist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 2: Update `internal/server/handlers_test.go` — mock store**

Replace `ListDepartmentURLs`:

```go
func (m *fullMockStore) ListDepartmentURLs(_ context.Context, departmentID uint) ([]db.URLEntry, error) {
	var out []db.URLEntry
	for _, du := range m.departmentURLs {
		if du.DepartmentID != departmentID {
			continue
		}
		for _, u := range m.urls {
			if u.ID == du.URLID {
				out = append(out, db.URLEntry{ID: u.ID, URL: u.URL, Enabled: du.Enabled, OrderedAt: du.OrderedAt, CreatedAt: u.CreatedAt})
			}
		}
	}
	return out, nil
}
```

with:

```go
func (m *fullMockStore) ListDepartmentURLs(_ context.Context, departmentID uint) ([]db.URLEntry, error) {
	var out []db.URLEntry
	for _, du := range m.departmentURLs {
		if du.DepartmentID != departmentID {
			continue
		}
		for _, u := range m.urls {
			if u.ID == du.URLID {
				out = append(out, db.URLEntry{
					ID: u.ID, URL: u.URL, Enabled: du.Enabled, DueDate: du.DueDate,
					Agency: du.Agency, ReferenceNumber: du.ReferenceNumber, RequestingDept: du.RequestingDept,
					Status: du.Status, RequestedAt: du.RequestedAt, CreatedAt: u.CreatedAt,
				})
			}
		}
	}
	return out, nil
}
```

Replace `SetURLOrderedAt`:

```go
func (m *fullMockStore) SetURLOrderedAt(_ context.Context, departmentID, urlID uint, orderedAt *time.Time) (bool, error) {
	for i, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == urlID {
			m.departmentURLs[i].OrderedAt = orderedAt
			return true, nil
		}
	}
	return false, nil
}
```

with:

```go
func (m *fullMockStore) UpdateDepartmentURLFields(_ context.Context, departmentID, urlID uint, fields db.DepartmentURLFields) (bool, error) {
	for i, du := range m.departmentURLs {
		if du.DepartmentID == departmentID && du.URLID == urlID {
			if fields.DueDate != nil {
				m.departmentURLs[i].DueDate = *fields.DueDate
			}
			if fields.Agency != nil {
				m.departmentURLs[i].Agency = *fields.Agency
			}
			if fields.ReferenceNumber != nil {
				m.departmentURLs[i].ReferenceNumber = *fields.ReferenceNumber
			}
			if fields.RequestingDept != nil {
				m.departmentURLs[i].RequestingDept = *fields.RequestingDept
			}
			if fields.Status != nil {
				m.departmentURLs[i].Status = *fields.Status
			}
			if fields.RequestedAt != nil {
				m.departmentURLs[i].RequestedAt = *fields.RequestedAt
			}
			return true, nil
		}
	}
	return false, nil
}
```

- [ ] **Step 3: Update `internal/server/handlers_test.go` — rename/update `ToggleURL` tests**

Replace `TestToggleURL_SetsOrderedAtWithoutTouchingEnabled`:

```go
func TestToggleURL_SetsOrderedAtWithoutTouchingEnabled(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"ordered_at": "2026-01-15T00:00:00Z"})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if !store.departmentURLs[0].Enabled {
		t.Fatal("expected Enabled to remain untouched by an ordered_at-only body")
	}
	if store.departmentURLs[0].OrderedAt == nil {
		t.Fatal("expected ordered_at to be set")
	}

	// Clearing with an empty string
	clearBody, _ := json.Marshal(map[string]string{"ordered_at": ""})
	req2 := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(clearBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("want 204 on clear, got %d: %s", w2.Code, w2.Body.String())
	}
	if store.departmentURLs[0].OrderedAt != nil {
		t.Fatal("expected ordered_at to be cleared by an empty string")
	}
}
```

with:

```go
func TestToggleURL_SetsDueDateWithoutTouchingEnabled(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"due_date": "2026-01-15T00:00:00Z"})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if !store.departmentURLs[0].Enabled {
		t.Fatal("expected Enabled to remain untouched by a due_date-only body")
	}
	if store.departmentURLs[0].DueDate == nil {
		t.Fatal("expected due_date to be set")
	}

	// Clearing with an empty string
	clearBody, _ := json.Marshal(map[string]string{"due_date": ""})
	req2 := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(clearBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("want 204 on clear, got %d: %s", w2.Code, w2.Body.String())
	}
	if store.departmentURLs[0].DueDate != nil {
		t.Fatal("expected due_date to be cleared by an empty string")
	}
}

func TestToggleURL_UpdatesCaseFieldsWithoutClobbering(t *testing.T) {
	deptID := uint(1)
	due := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true, DueDate: &due}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"status": "uplift"})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.departmentURLs[0].Status != "uplift" {
		t.Fatalf("expected status to be set to uplift, got %q", store.departmentURLs[0].Status)
	}
	if store.departmentURLs[0].DueDate == nil || !store.departmentURLs[0].DueDate.Equal(due) {
		t.Fatal("expected due_date to remain untouched by a status-only body")
	}

	body2, _ := json.Marshal(map[string]string{
		"agency": "MCMC", "reference_number": "REF-123", "requesting_dept": "Ministry of X",
	})
	req2 := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w2.Code, w2.Body.String())
	}
	du := store.departmentURLs[0]
	if du.Agency != "MCMC" || du.ReferenceNumber != "REF-123" || du.RequestingDept != "Ministry of X" {
		t.Fatalf("expected agency/reference_number/requesting_dept to be set, got %+v", du)
	}
	if du.Status != "uplift" {
		t.Fatal("expected status from the previous request to remain untouched")
	}
}

func TestToggleURL_InvalidStatusReturns400(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"status": "bogus"})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid status, got %d: %s", w.Code, w.Body.String())
	}
}
```

Replace `TestToggleURL_InvalidOrderedAtReturns400`:

```go
func TestToggleURL_InvalidOrderedAtReturns400(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"ordered_at": "not-a-date"})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid ordered_at, got %d: %s", w.Code, w.Body.String())
	}
}
```

with:

```go
func TestToggleURL_InvalidDueDateReturns400(t *testing.T) {
	deptID := uint(1)
	store := &fullMockStore{
		urls:           []db.URL{{ID: 1, URL: "example.com"}},
		departmentURLs: []db.DepartmentURL{{DepartmentID: deptID, URLID: 1, Enabled: true}},
	}
	cookie := deptCookie(store, deptID)
	r := setupRouter(store, nil)

	body, _ := json.Marshal(map[string]string{"due_date": "not-a-date"})
	req := httptest.NewRequest(http.MethodPatch, "/api/urls/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid due_date, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 4: Update the stale comment in `internal/server/legal_handlers_test.go`**

Replace:

```go
// AttachOffence is routine per-domain bookkeeping, open to any authenticated
// role within the owning department — not admin-gated, matching PATCH
// /api/urls/{id}'s ordered_at.
```

with:

```go
// AttachOffence is routine per-domain bookkeeping, open to any authenticated
// role within the owning department — not admin-gated, matching PATCH
// /api/urls/{id}'s due_date.
```

- [ ] **Step 5: Run the full `internal/server` test suite**

Run: `go test ./internal/server/... -v`
Expected: PASS for all tests, including the renamed/new ones from Step 3.

- [ ] **Step 6: Commit**

```bash
git add internal/server/handlers.go internal/server/handlers_test.go internal/server/legal_handlers_test.go
git commit -m "server: extend PATCH /api/urls/{id} with due_date + case-metadata fields"
```

---

## Task 3: Frontend types + API (`web/src/api`)

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/urls.ts`
- Modify: `web/src/components/isp-bento-grid.tsx`
- Modify: `web/src/routes/isps.$isp.tsx`

**Interfaces:**
- Produces: `URLEntry` (extended), `DepartmentURLFields` type, `setUrlFields(id, fields)` (replaces `setUrlOrderedAt`).
- Consumes: `PATCH /api/urls/{id}` (Task 2).

- [ ] **Step 1: Update `web/src/api/types.ts`**

Replace:

```ts
export type URLEntry = { id: number; url: string; enabled: boolean; ordered_at?: string; created_at: string }
```

with:

```ts
export type URLEntry = {
  id: number
  url: string
  enabled: boolean
  due_date?: string
  agency?: string
  reference_number?: string
  requesting_dept?: string
  status?: string
  requested_at?: string
  created_at: string
}
```

Replace:

```ts
export type ISPTiming = {
  isp: string
  median_days_to_block: number
  avg_days_to_block: number
  blocked_count: number
  still_open_count: number
  with_order_date_count: number
  total_domains: number
  slowest: DomainTiming[]
}
```

with:

```ts
export type ISPTiming = {
  isp: string
  median_days_to_block: number
  avg_days_to_block: number
  blocked_count: number
  still_open_count: number
  with_due_date_count: number
  total_domains: number
  slowest: DomainTiming[]
}
```

- [ ] **Step 2: Update `web/src/api/urls.ts`**

Replace:

```ts
export async function setUrlEnabled(id: number, enabled: boolean): Promise<void> {
  await api.patch<void>(`/urls/${id}`, { enabled })
}

// orderedAt is an RFC3339 string; pass null to clear a previously set order date.
export async function setUrlOrderedAt(id: number, orderedAt: string | null): Promise<void> {
  await api.patch<void>(`/urls/${id}`, { ordered_at: orderedAt ?? '' })
}
```

with:

```ts
export async function setUrlEnabled(id: number, enabled: boolean): Promise<void> {
  await api.patch<void>(`/urls/${id}`, { enabled })
}

export type DepartmentURLFields = {
  due_date?: string | null
  agency?: string
  reference_number?: string
  requesting_dept?: string
  status?: string
  requested_at?: string | null
}

// Partial update of the case-metadata fields on one watchlist entry — only
// keys present in `fields` are sent, mirroring the backend's
// UpdateDepartmentURLFields. Pass null on due_date/requested_at to clear them.
export async function setUrlFields(id: number, fields: DepartmentURLFields): Promise<void> {
  const body: Record<string, string> = {}
  if ('due_date' in fields) body.due_date = fields.due_date ?? ''
  if (fields.agency !== undefined) body.agency = fields.agency
  if (fields.reference_number !== undefined) body.reference_number = fields.reference_number
  if (fields.requesting_dept !== undefined) body.requesting_dept = fields.requesting_dept
  if (fields.status !== undefined) body.status = fields.status
  if ('requested_at' in fields) body.requested_at = fields.requested_at ?? ''
  await api.patch<void>(`/urls/${id}`, body)
}
```

- [ ] **Step 3: Update `web/src/components/isp-bento-grid.tsx`**

Replace:

```tsx
      {timing.with_order_date_count > 0 && (
```

with:

```tsx
      {timing.with_due_date_count > 0 && (
```

- [ ] **Step 4: Update `web/src/routes/isps.$isp.tsx`**

Replace:

```tsx
      {!loading && timing && timing.with_order_date_count > 0 && (
```

with:

```tsx
      {!loading && timing && timing.with_due_date_count > 0 && (
```

Replace:

```tsx
            <div>
              <p className="server-count" style={{ color: 'var(--ink)' }}>{timing.with_order_date_count} / {timing.total_domains}</p>
              <p className="dash-label">Domains with order date</p>
            </div>
```

with:

```tsx
            <div>
              <p className="server-count" style={{ color: 'var(--ink)' }}>{timing.with_due_date_count} / {timing.total_domains}</p>
              <p className="dash-label">Domains with due date</p>
            </div>
```

- [ ] **Step 5: Type-check the frontend**

Run: `cd web && npx tsc --noEmit`
Expected: no errors. (This will fail until Task 4 also updates `urls.tsx`'s now-broken import of `setUrlOrderedAt` — if so, proceed to Task 4 before treating this step as blocking.)

- [ ] **Step 6: Commit**

```bash
git add web/src/api/types.ts web/src/api/urls.ts web/src/components/isp-bento-grid.tsx web/src/routes/isps.\$isp.tsx
git commit -m "web: rename ordered_at to due_date, add case-metadata field types"
```

---

## Task 4: Frontend UI (`web/src/routes/urls.tsx`)

**Files:**
- Modify: `web/src/routes/urls.tsx`

**Interfaces:**
- Consumes: `setUrlFields`, `DepartmentURLFields`, extended `URLEntry` (Task 3).

- [ ] **Step 1: Update the import line**

Replace:

```ts
import { fetchUrls, createUrl, deleteUrl, setUrlEnabled, setUrlOrderedAt } from '../api/urls'
```

with:

```ts
import { fetchUrls, createUrl, deleteUrl, setUrlEnabled, setUrlFields } from '../api/urls'
```

- [ ] **Step 2: Add `STATUS_OPTIONS` and the `CaseTextField` type below `PAGE_SIZE`**

Replace:

```ts
const PAGE_SIZE = 25
```

with:

```ts
const PAGE_SIZE = 25

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '—' },
  { value: 'requested', label: 'Requested' },
  { value: 'uplift', label: 'Uplift' },
  { value: 'suspended', label: 'Suspended' },
]

type CaseTextField = 'agency' | 'reference_number' | 'requesting_dept'
```

- [ ] **Step 3: Replace `SkeletonRows` to match the new 9-column layout**

Replace:

```tsx
function SkeletonRows() {
  return (
    <>
      {[200, 160, 240].map((w, i) => (
        <TableRow key={i} className="skeleton-row">
          <TableCell className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </TableCell>
          <TableCell className="col-status">
            <span className="skeleton" style={{ width: 90, height: 14 }} />
          </TableCell>
          <TableCell className="col-status">
            <span className="skeleton" style={{ width: 90, height: 14 }} />
          </TableCell>
          <TableCell style={{ width: 52 }} />
          <TableCell className="col-evidence" />
        </TableRow>
      ))}
    </>
  )
}
```

with:

```tsx
function SkeletonRows() {
  return (
    <>
      {[200, 160, 240].map((w, i) => (
        <TableRow key={i} className="skeleton-row">
          <TableCell className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </TableCell>
          {Array.from({ length: 6 }).map((_, j) => (
            <TableCell key={j} className="col-status">
              <span className="skeleton" style={{ width: 90, height: 14 }} />
            </TableCell>
          ))}
          <TableCell style={{ width: 52 }} />
          <TableCell className="col-evidence" />
        </TableRow>
      ))}
    </>
  )
}
```

- [ ] **Step 4: Add `fieldOriginalRef` alongside the other `URLsPage` hooks**

Replace:

```tsx
  const [page, setPage] = useState(1)

  const load = useCallback(async () => {
```

with:

```tsx
  const [page, setPage] = useState(1)
  // Snapshots a text field's pre-edit value on focus so handleTextBlur can
  // roll back to it if the commit fails.
  const fieldOriginalRef = useRef<Record<string, string>>({})

  const load = useCallback(async () => {
```

- [ ] **Step 5: Replace `handleOrderedAtChange` with the new field handlers**

Replace:

```tsx
  const handleOrderedAtChange = useCallback(async (id: number, dateStr: string) => {
    const previous = urls.find(u => u.id === id)?.ordered_at
    const orderedAt = dateStr ? new Date(dateStr).toISOString() : null
    setUrls(prev => prev.map(u => u.id === id ? { ...u, ordered_at: orderedAt ?? undefined } : u))
    try {
      await setUrlOrderedAt(id, orderedAt)
    } catch {
      setUrls(prev => prev.map(u => u.id === id ? { ...u, ordered_at: previous } : u))
    }
  }, [urls])
```

with:

```tsx
  const handleDueDateChange = useCallback(async (id: number, dateStr: string) => {
    const previous = urls.find(u => u.id === id)?.due_date
    const dueDate = dateStr ? new Date(dateStr).toISOString() : null
    setUrls(prev => prev.map(u => u.id === id ? { ...u, due_date: dueDate ?? undefined } : u))
    try {
      await setUrlFields(id, { due_date: dueDate })
    } catch {
      setUrls(prev => prev.map(u => u.id === id ? { ...u, due_date: previous } : u))
    }
  }, [urls])

  const handleStatusChange = useCallback(async (id: number, status: string) => {
    const previous = urls.find(u => u.id === id)?.status
    setUrls(prev => prev.map(u => u.id === id ? { ...u, status } : u))
    try {
      await setUrlFields(id, { status })
    } catch {
      setUrls(prev => prev.map(u => u.id === id ? { ...u, status: previous } : u))
    }
  }, [urls])

  // Text fields commit on blur (not per keystroke) to avoid a PATCH per
  // character — fieldOriginalRef snapshots the pre-edit value on focus so a
  // failed commit can roll back to it.
  const handleTextFocus = useCallback((id: number, field: CaseTextField, value: string) => {
    fieldOriginalRef.current[`${id}:${field}`] = value
  }, [])

  const handleTextChange = useCallback((id: number, field: CaseTextField, value: string) => {
    setUrls(prev => prev.map(u => u.id === id ? { ...u, [field]: value } : u))
  }, [])

  const handleTextBlur = useCallback(async (id: number, field: CaseTextField) => {
    const key = `${id}:${field}`
    const original = fieldOriginalRef.current[key] ?? ''
    const current = urls.find(u => u.id === id)?.[field] ?? ''
    if (current === original) return
    try {
      await setUrlFields(id, { [field]: current })
    } catch {
      setUrls(prev => prev.map(u => u.id === id ? { ...u, [field]: original } : u))
    }
  }, [urls])
```

- [ ] **Step 6: Replace the table header**

Replace:

```tsx
            <TableHeader>
              <TableRow>
                <TableHead className="col-domain th-left" scope="col">Domain</TableHead>
                <TableHead className="col-status" scope="col">Added</TableHead>
                <TableHead className="col-status" scope="col">Order Date</TableHead>
                <TableHead scope="col" style={{ width: 52, textAlign: 'center' }}>Scan</TableHead>
                <TableHead className="col-evidence" scope="col" />
              </TableRow>
            </TableHeader>
```

with:

```tsx
            <TableHeader>
              <TableRow>
                <TableHead className="col-domain th-left" scope="col">Domain</TableHead>
                <TableHead className="col-status" scope="col">Added</TableHead>
                <TableHead className="col-status" scope="col">Agency</TableHead>
                <TableHead className="col-status" scope="col">Reference No.</TableHead>
                <TableHead className="col-status" scope="col">Requesting Dept.</TableHead>
                <TableHead className="col-status" scope="col">Status</TableHead>
                <TableHead className="col-status" scope="col">Due Date</TableHead>
                <TableHead scope="col" style={{ width: 52, textAlign: 'center' }}>Scan</TableHead>
                <TableHead className="col-evidence" scope="col" />
              </TableRow>
            </TableHeader>
```

- [ ] **Step 7: Replace the row cells between "Added" and the enable `Switch`**

Replace:

```tsx
                    <TableCell className="col-status text-center">
                      <span className="dns-name">
                        {DATE_FMT.format(new Date(u.created_at))}
                      </span>
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <input
                        type="date"
                        className="form-input"
                        style={{ width: 140 }}
                        value={u.ordered_at ? u.ordered_at.slice(0, 10) : ''}
                        onChange={e => handleOrderedAtChange(u.id, e.target.value)}
                        aria-label={`Order date for ${u.url}`}
                      />
                    </TableCell>
                    <TableCell style={{ textAlign: 'center' }}>
```

with:

```tsx
                    <TableCell className="col-status text-center">
                      <span className="dns-name">
                        {DATE_FMT.format(new Date(u.created_at))}
                      </span>
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <input
                        type="text"
                        className="form-input"
                        style={{ width: 120 }}
                        value={u.agency ?? ''}
                        onFocus={e => handleTextFocus(u.id, 'agency', e.target.value)}
                        onChange={e => handleTextChange(u.id, 'agency', e.target.value)}
                        onBlur={() => handleTextBlur(u.id, 'agency')}
                        aria-label={`Agency for ${u.url}`}
                      />
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <input
                        type="text"
                        className="form-input"
                        style={{ width: 120 }}
                        value={u.reference_number ?? ''}
                        onFocus={e => handleTextFocus(u.id, 'reference_number', e.target.value)}
                        onChange={e => handleTextChange(u.id, 'reference_number', e.target.value)}
                        onBlur={() => handleTextBlur(u.id, 'reference_number')}
                        aria-label={`Reference number for ${u.url}`}
                      />
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <input
                        type="text"
                        className="form-input"
                        style={{ width: 140 }}
                        value={u.requesting_dept ?? ''}
                        onFocus={e => handleTextFocus(u.id, 'requesting_dept', e.target.value)}
                        onChange={e => handleTextChange(u.id, 'requesting_dept', e.target.value)}
                        onBlur={() => handleTextBlur(u.id, 'requesting_dept')}
                        aria-label={`Requesting department for ${u.url}`}
                      />
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <Select
                        value={u.status ?? ''}
                        onValueChange={v => handleStatusChange(u.id, v)}
                      >
                        <SelectTrigger aria-label={`Status for ${u.url}`} className="w-full" />
                        <SelectContent>
                          {STATUS_OPTIONS.map((opt, i) => (
                            <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <input
                        type="datetime-local"
                        className="form-input"
                        style={{ width: 180 }}
                        value={u.due_date ? u.due_date.slice(0, 16) : ''}
                        onChange={e => handleDueDateChange(u.id, e.target.value)}
                        aria-label={`Due date for ${u.url}`}
                      />
                    </TableCell>
                    <TableCell style={{ textAlign: 'center' }}>
```

- [ ] **Step 8: Type-check and lint the frontend**

Run: `cd web && npx tsc --noEmit && npm run lint`
Expected: no errors.

- [ ] **Step 9: Manual smoke test via `dev.sh`**

Run: `./dev.sh` (from repo root), log in, go to the Watchlist page, and confirm:
- Agency / Reference No. / Requesting Dept. text fields save on blur (edit, tab away, refresh — value persists).
- Status select saves immediately (change it, refresh — value persists).
- Due Date accepts a date + time and persists across refresh.
- Toggling the Scan switch still works and doesn't clobber the other fields on the same row.

Stop the dev stack with Ctrl+C when done.

- [ ] **Step 10: Commit**

```bash
git add web/src/routes/urls.tsx
git commit -m "web: add inline-editable case fields and due-date-with-time to the watchlist table"
```

---

## Task 5: Documentation + final verification

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update the `urls.tsx` route bullet in `CLAUDE.md`**

Replace:

```
each row has an animated enable/disable switch (`r-switch.tsx`) plus a native `<input type="date">` for the optional takedown-order date, both calling `PATCH /api/urls/{id}` with optimistic updates that roll back on failure; the remove button is `DELETE /api/urls/{id}`
```

with:

```
each row has an animated enable/disable switch (`r-switch.tsx`), a native `<input type="datetime-local">` for the optional due-date SLA deadline (renamed from an order-date-only `<input type="date">` — DueDate now carries time-of-day), and inline-editable Agency/Reference Number/Requesting Department text fields plus a 3-option Status select (Requested/Uplift/Suspended) — all calling `PATCH /api/urls/{id}` via the combined `setUrlFields` with optimistic updates that roll back on failure (text fields commit on blur, not per keystroke); the remove button is `DELETE /api/urls/{id}`
```

- [ ] **Step 2: Update the `urls.ts` bullet in `CLAUDE.md`**

Replace:

```
`urls.ts` exports `fetchUrlCount()`, `fetchUrlsRequestedThisMonth()` (`GET /api/urls/requested-count`), `createUrl(url)` (no department_id — all users use their own department), `deleteUrl()`, `setUrlEnabled(id, enabled)`, and `setUrlOrderedAt(id, orderedAt)` (all three `PATCH /api/urls/{id}` with different body fields).
```

with:

```
`urls.ts` exports `fetchUrlCount()`, `fetchUrlsRequestedThisMonth()` (`GET /api/urls/requested-count`), `createUrl(url)` (no department_id — all users use their own department), `deleteUrl()`, `setUrlEnabled(id, enabled)`, and `setUrlFields(id, fields)` (a combined partial-update setter for `due_date`/`agency`/`reference_number`/`requesting_dept`/`status`/`requested_at`, mirroring the backend's `UpdateDepartmentURLFields`; all `PATCH /api/urls/{id}` with different body fields).
```

- [ ] **Step 3: Update the `PATCH /api/urls/{id}` route bullet in `CLAUDE.md`**

Replace:

```
  - `PATCH /api/urls/{id}` — updates the calling user's department watchlist entry; body accepts `{ "enabled"?: bool, "ordered_at"?: string }` and only touches fields present in the body. `enabled` toggles `DepartmentURL.Enabled` (`ListWatchedURLs` only includes enabled URLs, so disabling a URL removes it from future scans without deleting watchlist membership). `ordered_at` sets `DepartmentURL.OrderedAt` — RFC3339 to set, `""` to clear — the optional takedown-order date used by the time-to-compliance metric (see "ISP stats scoping" below)
```

with:

```
  - `PATCH /api/urls/{id}` — updates the calling user's department watchlist entry; body accepts `{ "enabled"?: bool, "due_date"?: string, "agency"?: string, "reference_number"?: string, "requesting_dept"?: string, "status"?: string, "requested_at"?: string }` and only touches fields present in the body. `enabled` toggles `DepartmentURL.Enabled` (`ListWatchedURLs` only includes enabled URLs, so disabling a URL removes it from future scans without deleting watchlist membership). `due_date` sets `DepartmentURL.DueDate` — RFC3339 to set, `""` to clear — the SLA deadline (carries time-of-day) used by the time-to-compliance metric (see "ISP stats scoping" below); renamed from `ordered_at`/`OrderedAt`. `agency`/`reference_number`/`requesting_dept` are free-text Excel-sourced case metadata (plain strings, no lookup table); `status` is server-validated against `requested`/`uplift`/`suspended` (400 on anything else, `""` clears it) and is independent of the derived `Compliant` field; `requested_at` is RFC3339/`""` like `due_date`. All six optional fields (beyond `enabled`) route through one combined store call, `db.Store.UpdateDepartmentURLFields`, rather than one setter per field.
```

- [ ] **Step 4: Update the `ISPTiming` route bullet in `CLAUDE.md`**

Replace:

```
Domains with no `OrderedAt` are excluded, not defaulted to some other date — see "ISP stats scoping" below
```

with:

```
Domains with no `DueDate` are excluded, not defaulted to some other date — see "ISP stats scoping" below
```

(This is inside the larger `GET /api/isps/{isp}/timing` bullet — also replace the `DepartmentURL.OrderedAt` mention just before it in the same bullet with `DepartmentURL.DueDate`.)

- [ ] **Step 5: Update the "Time-to-compliance" section in `CLAUDE.md`**

Replace:

```
- **Time-to-compliance** (`ispComplianceTiming` in `internal/db/postgres.go`): `DepartmentURL.OrderedAt` is an optional, nullable takedown-order date — settable at add-time or later via `PATCH /api/urls/{id}`, never inferred from `CreatedAt`. For a given ISP, a domain's "days to block" is its first compliant scan by that ISP's servers minus `OrderedAt`, clamped to 0 if the compliant scan predates the order (already-blocked-before-order, not a negative duration). Domains with no `OrderedAt` are excluded from the aggregate entirely — `WithOrderDateCount`/`TotalDomains` in the response exists so the UI can show real coverage instead of silently blending in a proxy date. Admin scope uses the earliest `OrderedAt` across departments per domain; aggregation happens in Go (not SQL `MIN()`) because the SQLite test driver can't scan a `MIN()` of a datetime column into `time.Time`.
```

with:

```
- **Time-to-compliance** (`ispComplianceTiming` in `internal/db/postgres.go`): `DepartmentURL.DueDate` (renamed from `OrderedAt`) is an optional, nullable SLA deadline — settable at add-time or later via `PATCH /api/urls/{id}`, never inferred from `CreatedAt`. For a given ISP, a domain's "days to block" is its first compliant scan by that ISP's servers minus `DueDate`, clamped to 0 if the compliant scan predates the deadline (already-blocked-before-due, not a negative duration). Domains with no `DueDate` are excluded from the aggregate entirely — `WithDueDateCount`/`TotalDomains` in the response exists so the UI can show real coverage instead of silently blending in a proxy date. Admin scope uses the earliest `DueDate` across departments per domain; aggregation happens in Go (not SQL `MIN()`) because the SQLite test driver can't scan a `MIN()` of a datetime column into `time.Time`.
```

- [ ] **Step 6: Update the Database models section in `CLAUDE.md`**

Replace:

```
`DepartmentURL` is a composite-PK (`department_id`, `url_id`) join table with an `Enabled bool` field and an optional `OrderedAt *time.Time` (the takedown-order date used by time-to-compliance, see "ISP stats scoping" above) — see "Domain normalization & watchlists" below. `URLEntry` is a read-side struct pairing a `*URL` with the calling department's `Enabled` flag and `OrderedAt`; `ListDepartmentURLs` returns `[]URLEntry`. `SetURLEnabled(ctx, deptID, urlID, enabled)` updates `DepartmentURL.Enabled` in place; `SetURLOrderedAt(ctx, deptID, urlID, orderedAt)` does the same for `OrderedAt` (nil clears it); `ListWatchedURLs` only returns URLs where `enabled = true`, so disabling a URL silently excludes it from scans.
```

with:

```
`DepartmentURL` is a composite-PK (`department_id`, `url_id`) join table with an `Enabled bool` field, an optional `DueDate *time.Time` (renamed from `OrderedAt` — the SLA deadline used by time-to-compliance, see "ISP stats scoping" above; the rename is a data-preserving column rename run in `db.Connect` before `AutoMigrate`, see "Domain normalization & watchlists" below), and five Excel-sourced case-metadata fields: `Agency string`, `ReferenceNumber string`, `RequestingDept string` (free text, distinct from the app's own CMOD/CRD RBAC `Department` model), `Status string` (server-validated against `requested`/`uplift`/`suspended`, independent of the derived `Compliant` field), and `RequestedAt *time.Time`. `URLEntry` is a read-side struct mirroring the same fields alongside `*URL`; `ListDepartmentURLs` returns `[]URLEntry`. `SetURLEnabled(ctx, deptID, urlID, enabled)` updates `DepartmentURL.Enabled` in place; `UpdateDepartmentURLFields(ctx, deptID, urlID, fields)` applies a partial update to `DueDate`/`Agency`/`ReferenceNumber`/`RequestingDept`/`Status`/`RequestedAt` in one call (`db.DepartmentURLFields`, only non-nil fields touched) rather than one setter per column; `ListWatchedURLs` only returns URLs where `enabled = true`, so disabling a URL silently excludes it from scans.
```

- [ ] **Step 7: Run the full backend test suite**

Run: `go test ./...`
Expected: PASS across all packages (screenshot tests skip without Chrome; `internal/dns` needs network access — both as documented in the Commands section).

- [ ] **Step 8: Run the frontend build and lint**

Run: `cd web && npm run build && npm run lint`
Expected: both succeed with no errors.

- [ ] **Step 9: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for DueDate rename and watchlist case fields"
```

---

## Out of Scope (unchanged from the design spec)

- Any admin-curated lookup table / CRUD UI for Agency or Requesting Department.
- A bulk Excel import pipeline.
- Feature 3's notification system (the SLA-deadline scan-and-notify check that will consume `DueDate`) — this plan only adds the field.
