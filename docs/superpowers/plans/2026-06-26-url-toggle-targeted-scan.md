# URL Toggle & Targeted Scan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-department enabled/disabled toggle for each URL in the watchlist and a "Scan Selected" mode that scans a chosen subset of domains (plus ad-hoc entries) instead of the full watched list.

**Architecture:** `DepartmentURL` gains an `Enabled` boolean; `ListWatchedURLs` filters to `enabled = true` with DISTINCT so a domain watched by multiple departments is still scanned once. An Admin department is seeded and bootstrap admin users are migrated to it, making admin URL management uniform with department users. A new `PATCH /api/urls/:id` endpoint toggles the flag. The Run Scan button in the navbar gains a "Scan Selected" dialog that passes an explicit URL list to `POST /api/scan`; the scanner skips `ListWatchedURLs` when a list is provided and normalises/deduplicates inline.

**Tech Stack:** Go 1.26, GORM (PostgreSQL), chi v5, React 19, TypeScript, TanStack Router, shadcn (`@iconiq/r-switch`), Tailwind CSS.

## Global Constraints

- Module path: `github.com/afif/dns-tracking` (not the directory name)
- `URLEntry` (with `enabled`) replaces `URL` everywhere a department-scoped URL list is returned — never mutate the shared `URL` model
- `ListWatchedURLs` must remain `DISTINCT` — a URL enabled by any one department enters the scan pool exactly once
- Admin department name is exactly `"Admin"` (case-sensitive); seeding is idempotent via `FirstOrCreate`
- `is_admin` remains the authoritative admin flag — `DepartmentID` being non-nil no longer implies non-admin
- Switch component installed via: `cd web && npx shadcn@latest add @iconiq/r-switch`

---

## File Map

| File | Change |
|------|--------|
| `internal/db/models.go` | Add `Enabled bool` to `DepartmentURL`; add `URLEntry` struct |
| `internal/db/db.go` | Add `MigrateAdminDepartments` (idempotent) |
| `internal/db/auth.go` | `SeedAdmin` assigns Admin department |
| `internal/db/store.go` | `ListDepartmentURLs` → `[]URLEntry`; add `SetURLEnabled` |
| `internal/db/postgres.go` | Implement `SetURLEnabled`; update `ListDepartmentURLs`, `ListWatchedURLs` |
| `internal/db/postgres_test.go` | Tests for new store methods |
| `cmd/server/main.go` | Call `MigrateAdminDepartments` before `SeedAdmin` |
| `internal/server/handlers.go` | `ToggleURL`; simplify `ListURLs`, `AddToWatchlist`, `RemoveFromWatchlist`; update `TriggerScan` |
| `internal/server/router.go` | Add `PATCH /api/urls/{id}` |
| `internal/server/handlers_test.go` | Update `fullMockStore`; add `ToggleURL` tests |
| `internal/server/scanner.go` | `Trigger(ctx, by, urls []string)`; `run` deduplicates provided list |
| `internal/server/scheduler.go` | Pass `nil` urls to `Trigger` |
| `internal/server/scanner_test.go` | Update call sites |
| `web/src/components/ui/switch.tsx` | Created by shadcn install |
| `web/src/api/types.ts` | `URLEntry` gains `enabled: boolean` |
| `web/src/api/urls.ts` | Add `setUrlEnabled`; remove `department_id` from `createUrl` |
| `web/src/api/scan.ts` | `triggerScan(urls?: string[])` |
| `web/src/routes/urls.tsx` | Switch column; remove admin department picker |
| `web/src/routes/__root.tsx` | Scan Selected dialog; `handleScanClick` accepts optional url list |

---

## Task 1: DB models + Admin department migration

**Files:**
- Modify: `internal/db/models.go`
- Modify: `internal/db/db.go`
- Modify: `internal/db/auth.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Produces: `db.URLEntry`, `db.DepartmentURL.Enabled`, `db.MigrateAdminDepartments`

- [ ] **Step 1: Add `Enabled` to `DepartmentURL` and add `URLEntry` in `internal/db/models.go`**

```go
// DepartmentURL — add Enabled field
type DepartmentURL struct {
    DepartmentID uint      `gorm:"primaryKey;autoIncrement:false" json:"department_id"`
    URLID        uint      `gorm:"primaryKey;autoIncrement:false" json:"url_id"`
    URL          URL       `gorm:"foreignKey:URLID;constraint:OnDelete:CASCADE" json:"-"`
    Enabled      bool      `gorm:"not null;default:true" json:"enabled"`
    CreatedAt    time.Time `json:"created_at"`
}

// URLEntry is the department-scoped view of a URL, carrying the watchlist
// enabled flag that the shared URL model does not have.
type URLEntry struct {
    ID        uint      `json:"id"`
    URL       string    `json:"url"`
    Enabled   bool      `json:"enabled"`
    CreatedAt time.Time `json:"created_at"`
}
```

GORM `AutoMigrate` adds the `enabled` column with `DEFAULT true`; existing rows get `enabled = true` automatically.

- [ ] **Step 2: Add `MigrateAdminDepartments` to `internal/db/db.go`**

```go
// MigrateAdminDepartments ensures an "Admin" department exists and
// updates any admin users whose DepartmentID is nil to point to it.
// Idempotent — safe to call on every startup.
func MigrateAdminDepartments(database *gorm.DB) error {
    var adminDept Department
    if err := database.
        Where("name = ?", "Admin").
        FirstOrCreate(&adminDept, Department{Name: "Admin"}).Error; err != nil {
        return fmt.Errorf("ensure admin department: %w", err)
    }
    return database.Model(&User{}).
        Where("is_admin = ? AND department_id IS NULL", true).
        Update("department_id", adminDept.ID).Error
}
```

- [ ] **Step 3: Update `SeedAdmin` in `internal/db/auth.go` to assign the Admin department**

Replace the existing `SeedAdmin` body:

```go
func SeedAdmin(database *gorm.DB, username, password string) error {
    var count int64
    if err := database.Model(&User{}).Count(&count).Error; err != nil {
        return err
    }
    if count > 0 {
        return nil
    }
    if username == "" || password == "" {
        return fmt.Errorf("db: bootstrap admin username and password are required when the users table is empty")
    }
    var adminDept Department
    if err := database.Where("name = ?", "Admin").First(&adminDept).Error; err != nil {
        return fmt.Errorf("db: admin department not found (run MigrateAdminDepartments first): %w", err)
    }
    hash, err := HashPassword(password)
    if err != nil {
        return err
    }
    return database.Create(&User{
        Username:     username,
        PasswordHash: hash,
        IsAdmin:      true,
        DepartmentID: &adminDept.ID,
    }).Error
}
```

- [ ] **Step 4: Call `MigrateAdminDepartments` in `cmd/server/main.go` before `SeedAdmin`**

In `main()`, after `db.SeedDepartments(gormDB)`:

```go
if err := db.MigrateAdminDepartments(gormDB); err != nil {
    log.Fatalf("migrate admin departments: %v", err)
}
```

- [ ] **Step 5: Build to verify no compilation errors**

```bash
go build ./...
```

Expected: no output (clean build).

- [ ] **Step 6: Commit**

```bash
git add internal/db/models.go internal/db/db.go internal/db/auth.go cmd/server/main.go
git commit -m "feat(db): add DepartmentURL.Enabled, URLEntry, and Admin department migration"
```

---

## Task 2: Store layer — SetURLEnabled, ListDepartmentURLs, ListWatchedURLs

**Files:**
- Modify: `internal/db/store.go`
- Modify: `internal/db/postgres.go`
- Modify: `internal/db/postgres_test.go`

**Interfaces:**
- Consumes: `db.URLEntry`, `db.DepartmentURL.Enabled` (Task 1)
- Produces: `store.SetURLEnabled`, `store.ListDepartmentURLs() []URLEntry`

- [ ] **Step 1: Update `store.go` interface**

Replace the two relevant lines in `Store`:

```go
// Department watchlists
ListDepartmentURLs(ctx context.Context, departmentID uint) ([]URLEntry, error)
AddURLToWatchlist(ctx context.Context, departmentID uint, rawURL string) (URL, error)
RemoveURLFromWatchlist(ctx context.Context, departmentID, urlID uint) (bool, error)
SetURLEnabled(ctx context.Context, departmentID, urlID uint, enabled bool) (bool, error) // false = URL not on watchlist
ListWatchedURLs(ctx context.Context) ([]URL, error)
ListUnassignedURLs(ctx context.Context) ([]URL, error)
URLOwnedByDepartment(ctx context.Context, departmentID uint, urlValue string) (bool, error)
```

- [ ] **Step 2: Write failing tests in `internal/db/postgres_test.go`**

```go
func TestSetURLEnabled(t *testing.T) {
    db := openTestDB(t)
    store := NewStore(db)
    ctx := context.Background()

    dept := Department{Name: "TestDept"}
    db.Create(&dept)
    u, _ := store.CreateURL(ctx, "example.com")
    _, err := store.AddURLToWatchlist(ctx, dept.ID, "example.com")
    if err != nil {
        t.Fatalf("AddURLToWatchlist: %v", err)
    }

    // disable
    found, err := store.SetURLEnabled(ctx, dept.ID, u.ID, false)
    if err != nil || !found {
        t.Fatalf("SetURLEnabled(false): found=%v err=%v", found, err)
    }

    // ListWatchedURLs must exclude it
    urls, err := store.ListWatchedURLs(ctx)
    if err != nil {
        t.Fatalf("ListWatchedURLs: %v", err)
    }
    for _, wu := range urls {
        if wu.ID == u.ID {
            t.Fatal("disabled URL should not appear in ListWatchedURLs")
        }
    }

    // re-enable
    found, err = store.SetURLEnabled(ctx, dept.ID, u.ID, true)
    if err != nil || !found {
        t.Fatalf("SetURLEnabled(true): found=%v err=%v", found, err)
    }
    urls, _ = store.ListWatchedURLs(ctx)
    var seen bool
    for _, wu := range urls {
        if wu.ID == u.ID {
            seen = true
        }
    }
    if !seen {
        t.Fatal("re-enabled URL should appear in ListWatchedURLs")
    }
}

func TestSetURLEnabledNotOnWatchlist(t *testing.T) {
    db := openTestDB(t)
    store := NewStore(db)
    ctx := context.Background()

    dept := Department{Name: "TestDept2"}
    db.Create(&dept)
    u, _ := store.CreateURL(ctx, "notlinked.com")

    found, err := store.SetURLEnabled(ctx, dept.ID, u.ID, false)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if found {
        t.Fatal("expected found=false for URL not on watchlist")
    }
}

func TestListDepartmentURLsReturnsEnabled(t *testing.T) {
    db := openTestDB(t)
    store := NewStore(db)
    ctx := context.Background()

    dept := Department{Name: "TestDept3"}
    db.Create(&dept)
    _, _ = store.AddURLToWatchlist(ctx, dept.ID, "alpha.com")
    u2, _ := store.CreateURL(ctx, "beta.com")
    _, _ = store.AddURLToWatchlist(ctx, dept.ID, "beta.com")
    _, _ = store.SetURLEnabled(ctx, dept.ID, u2.ID, false)

    entries, err := store.ListDepartmentURLs(ctx, dept.ID)
    if err != nil {
        t.Fatalf("ListDepartmentURLs: %v", err)
    }
    if len(entries) != 2 {
        t.Fatalf("want 2 entries, got %d", len(entries))
    }
    for _, e := range entries {
        if e.URL == "alpha.com" && !e.Enabled {
            t.Error("alpha.com should be enabled")
        }
        if e.URL == "beta.com" && e.Enabled {
            t.Error("beta.com should be disabled")
        }
    }
}

func TestListWatchedURLsDeduplicatesAcrossDepartments(t *testing.T) {
    db := openTestDB(t)
    store := NewStore(db)
    ctx := context.Background()

    d1 := Department{Name: "D1"}
    d2 := Department{Name: "D2"}
    db.Create(&d1)
    db.Create(&d2)
    _, _ = store.AddURLToWatchlist(ctx, d1.ID, "shared.com")
    _, _ = store.AddURLToWatchlist(ctx, d2.ID, "shared.com")

    urls, err := store.ListWatchedURLs(ctx)
    if err != nil {
        t.Fatalf("ListWatchedURLs: %v", err)
    }
    count := 0
    for _, u := range urls {
        if u.URL == "shared.com" {
            count++
        }
    }
    if count != 1 {
        t.Fatalf("want shared.com once, got %d times", count)
    }
}
```

- [ ] **Step 3: Run tests — expect FAIL (methods not yet implemented)**

```bash
go test ./internal/db/... -run "TestSetURLEnabled|TestListDepartment|TestListWatchedURLs"
```

Expected: compile error or FAIL.

- [ ] **Step 4: Implement in `internal/db/postgres.go`**

Replace `ListDepartmentURLs`:

```go
func (s *postgresStore) ListDepartmentURLs(ctx context.Context, departmentID uint) ([]URLEntry, error) {
    var entries []URLEntry
    err := s.db.WithContext(ctx).
        Table("urls").
        Select("urls.id, urls.url, urls.created_at, du.enabled").
        Joins("JOIN department_urls du ON du.url_id = urls.id AND du.department_id = ?", departmentID).
        Order("urls.created_at asc").
        Scan(&entries).Error
    return entries, err
}
```

Add `SetURLEnabled`:

```go
func (s *postgresStore) SetURLEnabled(ctx context.Context, departmentID, urlID uint, enabled bool) (bool, error) {
    res := s.db.WithContext(ctx).
        Model(&DepartmentURL{}).
        Where("department_id = ? AND url_id = ?", departmentID, urlID).
        Update("enabled", enabled)
    return res.RowsAffected > 0, res.Error
}
```

Update `ListWatchedURLs` to filter by `enabled = true`:

```go
func (s *postgresStore) ListWatchedURLs(ctx context.Context) ([]URL, error) {
    var urls []URL
    err := s.db.WithContext(ctx).
        Distinct().
        Joins("JOIN department_urls du ON du.url_id = urls.id AND du.enabled = true").
        Order("urls.created_at asc").
        Find(&urls).Error
    return urls, err
}
```

- [ ] **Step 5: Run tests — expect PASS**

```bash
go test ./internal/db/... -run "TestSetURLEnabled|TestListDepartment|TestListWatchedURLs"
```

Expected: PASS.

- [ ] **Step 6: Build**

```bash
go build ./...
```

Expected: no output. If you see compile errors about `ListDepartmentURLs` return type mismatches in `internal/server/`, that is expected — they will be fixed in Task 3.

- [ ] **Step 7: Commit**

```bash
git add internal/db/store.go internal/db/postgres.go internal/db/postgres_test.go
git commit -m "feat(db): SetURLEnabled, ListDepartmentURLs returns URLEntry, ListWatchedURLs filters enabled"
```

---

## Task 3: API — ToggleURL handler + simplify ListURLs/AddToWatchlist/RemoveFromWatchlist

**Files:**
- Modify: `internal/server/handlers.go`
- Modify: `internal/server/router.go`
- Modify: `internal/server/handlers_test.go`

**Interfaces:**
- Consumes: `store.SetURLEnabled`, `store.ListDepartmentURLs() []URLEntry` (Task 2)
- Produces: `PATCH /api/urls/{id}` → 204/404; `GET /api/urls` returns `[]URLEntry` for all users

- [ ] **Step 1: Update `fullMockStore` in `internal/server/handlers_test.go`**

Update `ListDepartmentURLs` to return `[]db.URLEntry`:

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
                    ID:        u.ID,
                    URL:       u.URL,
                    Enabled:   du.Enabled,
                    CreatedAt: u.CreatedAt,
                })
            }
        }
    }
    return out, nil
}
```

Add `SetURLEnabled` to the mock:

```go
func (m *fullMockStore) SetURLEnabled(_ context.Context, departmentID, urlID uint, enabled bool) (bool, error) {
    for i, du := range m.departmentURLs {
        if du.DepartmentID == departmentID && du.URLID == urlID {
            m.departmentURLs[i].Enabled = enabled
            return true, nil
        }
    }
    return false, nil
}
```

Update `AddURLToWatchlist` in the mock to set `Enabled: true` on new links:

```go
func (m *fullMockStore) AddURLToWatchlist(ctx context.Context, departmentID uint, rawURL string) (db.URL, error) {
    u, err := m.CreateURL(ctx, rawURL)
    if err != nil {
        return db.URL{}, err
    }
    for _, du := range m.departmentURLs {
        if du.DepartmentID == departmentID && du.URLID == u.ID {
            return u, nil
        }
    }
    m.departmentURLs = append(m.departmentURLs, db.DepartmentURL{
        DepartmentID: departmentID,
        URLID:        u.ID,
        Enabled:      true,
        CreatedAt:    time.Now(),
    })
    return u, nil
}
```

Update `ListWatchedURLs` in the mock to filter by `Enabled`:

```go
func (m *fullMockStore) ListWatchedURLs(_ context.Context) ([]db.URL, error) {
    watchedIDs := make(map[uint]bool)
    for _, du := range m.departmentURLs {
        if du.Enabled {
            watchedIDs[du.URLID] = true
        }
    }
    var out []db.URL
    for _, u := range m.urls {
        if watchedIDs[u.ID] {
            out = append(out, u)
        }
    }
    return out, nil
}
```

- [ ] **Step 2: Write failing handler tests**

```go
func TestToggleURL(t *testing.T) {
    deptID := uint(1)
    urlID  := uint(1)
    store := &fullMockStore{
        urls: []db.URL{{ID: urlID, URL: "example.com"}},
        departmentURLs: []db.DepartmentURL{
            {DepartmentID: deptID, URLID: urlID, Enabled: true},
        },
    }
    h := server.NewHandlers(store, nil, nil)
    r := chi.NewRouter()
    r.Use(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
            ctx := context.WithValue(req.Context(), server.UserContextKey, &db.User{
                ID: 99, DepartmentID: &deptID,
            })
            next.ServeHTTP(w, req.WithContext(ctx))
        })
    })
    r.Patch("/urls/{id}", h.ToggleURL)

    body := `{"enabled":false}`
    req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/urls/%d", urlID), strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()
    r.ServeHTTP(rr, req)

    if rr.Code != http.StatusNoContent {
        t.Fatalf("want 204, got %d: %s", rr.Code, rr.Body.String())
    }
    // verify state changed in mock
    if store.departmentURLs[0].Enabled {
        t.Fatal("expected Enabled=false after toggle")
    }
}

func TestToggleURLNotOnWatchlist(t *testing.T) {
    deptID := uint(1)
    store := &fullMockStore{
        urls: []db.URL{{ID: 1, URL: "example.com"}},
    }
    h := server.NewHandlers(store, nil, nil)
    r := chi.NewRouter()
    r.Use(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
            ctx := context.WithValue(req.Context(), server.UserContextKey, &db.User{
                ID: 99, DepartmentID: &deptID,
            })
            next.ServeHTTP(w, req.WithContext(ctx))
        })
    })
    r.Patch("/urls/{id}", h.ToggleURL)

    req := httptest.NewRequest(http.MethodPatch, "/urls/1", strings.NewReader(`{"enabled":false}`))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()
    r.ServeHTTP(rr, req)

    if rr.Code != http.StatusNotFound {
        t.Fatalf("want 404, got %d", rr.Code)
    }
}
```

Note: `server.UserContextKey` is the unexported context key used in `auth.go`. Check the actual key name in `internal/server/auth.go` and use whatever key the existing handler tests use to inject the user (copy the pattern from existing tests in that file).

- [ ] **Step 3: Run failing tests**

```bash
go test ./internal/server/... -run "TestToggleURL"
```

Expected: compile error (method `ToggleURL` not yet defined).

- [ ] **Step 4: Implement `ToggleURL` and update `ListURLs`, `AddToWatchlist`, `RemoveFromWatchlist` in `internal/server/handlers.go`**

Replace `ListURLs`:

```go
func (h *Handlers) ListURLs(w http.ResponseWriter, r *http.Request) {
    user, ok := userFromContext(r.Context())
    if !ok {
        writeError(w, http.StatusUnauthorized, "not authenticated")
        return
    }
    if user.DepartmentID == nil {
        writeError(w, http.StatusInternalServerError, "user has no department")
        return
    }
    urls, err := h.store.ListDepartmentURLs(r.Context(), *user.DepartmentID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, urls)
}
```

Replace `AddToWatchlist` (remove the `department_id` body field and admin special-case):

```go
func (h *Handlers) AddToWatchlist(w http.ResponseWriter, r *http.Request) {
    user, ok := userFromContext(r.Context())
    if !ok {
        writeError(w, http.StatusUnauthorized, "not authenticated")
        return
    }
    if user.DepartmentID == nil {
        writeError(w, http.StatusForbidden, "user has no department")
        return
    }
    var body struct {
        URL string `json:"url"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
        writeError(w, http.StatusBadRequest, "url is required")
        return
    }
    u, err := h.store.AddURLToWatchlist(r.Context(), *user.DepartmentID, body.URL)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusCreated, db.URLEntry{ID: u.ID, URL: u.URL, Enabled: true, CreatedAt: u.CreatedAt})
}
```

Replace `RemoveFromWatchlist` (remove `?department_id=` admin param):

```go
func (h *Handlers) RemoveFromWatchlist(w http.ResponseWriter, r *http.Request) {
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
    removed, err := h.store.RemoveURLFromWatchlist(r.Context(), *user.DepartmentID, uint(id))
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    if !removed {
        writeError(w, http.StatusNotFound, "url not on watchlist")
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
```

Add `ToggleURL`:

```go
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
        Enabled bool `json:"enabled"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        writeError(w, http.StatusBadRequest, "invalid body")
        return
    }
    found, err := h.store.SetURLEnabled(r.Context(), *user.DepartmentID, uint(id), body.Enabled)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    if !found {
        writeError(w, http.StatusNotFound, "url not on watchlist")
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Register route in `internal/server/router.go`**

Add after `r.Delete("/urls/{id}", h.RemoveFromWatchlist)`:

```go
r.Patch("/urls/{id}", h.ToggleURL)
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/server/... -run "TestToggleURL"
```

Expected: PASS.

- [ ] **Step 7: Build**

```bash
go build ./...
```

Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add internal/server/handlers.go internal/server/router.go internal/server/handlers_test.go
git commit -m "feat(api): ToggleURL endpoint, simplify AddToWatchlist/RemoveFromWatchlist for admin-as-department"
```

---

## Task 4: Scanner — targeted scan with optional URL list

**Files:**
- Modify: `internal/server/scanner.go`
- Modify: `internal/server/scheduler.go`
- Modify: `internal/server/handlers.go` (only `TriggerScan`)
- Modify: `internal/server/scanner_test.go`

**Interfaces:**
- Produces: `Scanner.Trigger(ctx, triggeredBy, urls []string)` — nil/empty urls = full sweep

- [ ] **Step 1: Write failing test for targeted scan**

Add to `internal/server/scanner_test.go`:

```go
func TestScannerTargetedURLs(t *testing.T) {
    crawlerPath := writeFakeCrawler(t)
    store := &completionCapture{}
    sc := server.NewScanner(crawlerPath, "localhost:50051", store)

    if err := sc.Trigger(context.Background(), "manual", []string{"example.com", "https://EXAMPLE.COM"}); err != nil {
        t.Fatalf("Trigger: %v", err)
    }

    waitUntil(t, func() bool { return !sc.IsRunning() }, 3*time.Second)

    if len(store.completed) == 0 {
        t.Fatal("expected CompleteScanRun to be called")
    }
}
```

Also update the two existing tests to pass `nil` as the third argument:

```go
// In TestScannerTriggerRunsAndCompletes:
if err := sc.Trigger(context.Background(), "manual", nil); err != nil {

// In TestScannerRejectsConcurrentRun:
_ = sc.Trigger(context.Background(), "manual", nil)
err := sc.Trigger(context.Background(), "manual", nil)
```

- [ ] **Step 2: Run tests — expect compile error**

```bash
go test ./internal/server/... -run "TestScanner"
```

Expected: compile error (`Trigger` has wrong number of arguments).

- [ ] **Step 3: Update `Scanner.Trigger` and `run` in `internal/server/scanner.go`**

Add import at top of file:

```go
"github.com/afif/dns-tracking/internal/urlnorm"
```

Replace `Trigger`:

```go
func (sc *Scanner) Trigger(ctx context.Context, triggeredBy string, urls []string) error {
    sc.mu.Lock()
    if sc.running {
        sc.mu.Unlock()
        return errors.New("scan already in progress")
    }
    sc.running = true
    sc.mu.Unlock()
    go sc.run(context.WithoutCancel(ctx), triggeredBy, urls)
    return nil
}
```

Replace `run`:

```go
func (sc *Scanner) run(ctx context.Context, triggeredBy string, requestedURLs []string) {
    defer sc.setRunning(false)

    var urlObjs []db.URL
    if len(requestedURLs) == 0 {
        var err error
        urlObjs, err = sc.store.ListWatchedURLs(ctx)
        if err != nil || len(urlObjs) == 0 {
            log.Printf("scanner: no URLs to scan (err=%v)", err)
            return
        }
    } else {
        seen := make(map[string]bool)
        for _, raw := range requestedURLs {
            norm, err := urlnorm.Normalize(raw)
            if err != nil {
                continue
            }
            if !seen[norm] {
                seen[norm] = true
                urlObjs = append(urlObjs, db.URL{URL: norm})
            }
        }
        if len(urlObjs) == 0 {
            log.Printf("scanner: no valid URLs in targeted scan request")
            return
        }
    }

    servers, err := sc.store.ListDNSServers(ctx)
    if err != nil {
        log.Printf("scanner: load DNS servers: %v", err)
        return
    }

    urlFile, err := writeTempLines(urlObjs)
    if err != nil {
        log.Printf("scanner: write url file: %v", err)
        return
    }
    defer os.Remove(urlFile)

    dnsFile, err := sc.writeDNSYAML(servers)
    if err != nil {
        log.Printf("scanner: write dns yaml: %v", err)
        return
    }
    defer os.Remove(dnsFile)

    run, err := sc.store.CreateScanRun(ctx, triggeredBy)
    if err != nil {
        log.Printf("scanner: create scan run: %v", err)
        return
    }

    args := []string{
        "--sites", urlFile,
        "--dns-servers", dnsFile,
        "--grpc-addr", sc.grpcAddr,
    }
    sc.execCrawler(ctx, args, run.ID)
}
```

- [ ] **Step 4: Update scheduler call in `internal/server/scheduler.go`**

```go
if err := sc.Trigger(ctx, "scheduled", nil); err != nil {
```

- [ ] **Step 5: Update `TriggerScan` in `internal/server/handlers.go`**

Replace the existing `TriggerScan` body:

```go
func (h *Handlers) TriggerScan(w http.ResponseWriter, r *http.Request) {
    var body struct {
        URLs []string `json:"urls"`
    }
    json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck — body is optional

    if err := h.scanner.Trigger(r.Context(), "manual", body.URLs); err != nil {
        writeError(w, http.StatusConflict, err.Error())
        return
    }
    writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/server/... -run "TestScanner"
```

Expected: PASS.

- [ ] **Step 7: Build**

```bash
go build ./...
```

Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add internal/server/scanner.go internal/server/scheduler.go internal/server/handlers.go internal/server/scanner_test.go
git commit -m "feat(scanner): Trigger accepts optional URL list for targeted scans"
```

---

## Task 5: Frontend — switch component + URL toggle

**Files:**
- Create: `web/src/components/ui/switch.tsx` (via shadcn)
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/urls.ts`
- Modify: `web/src/routes/urls.tsx`

**Interfaces:**
- Produces: `setUrlEnabled(id, enabled)` API call; Switch rendered per row with optimistic state

- [ ] **Step 1: Install the switch component**

```bash
cd web && npx shadcn@latest add @iconiq/r-switch
```

Expected: creates `web/src/components/ui/switch.tsx`. Accept any prompts about overwriting with `y`.

- [ ] **Step 2: Add `enabled` to `URLEntry` in `web/src/api/types.ts`**

```ts
export type URLEntry = { id: number; url: string; enabled: boolean; created_at: string }
```

- [ ] **Step 3: Update `web/src/api/urls.ts`**

Add `setUrlEnabled` and simplify `createUrl` (remove `departmentId` — server derives from session):

```ts
import { api } from './client'
import type { URLEntry } from './types'

export async function fetchUrls(): Promise<URLEntry[]> {
  const data = await api.get<URLEntry[]>('/urls')
  return Array.isArray(data) ? data : []
}

export async function fetchUrlCount(): Promise<number> {
  return (await fetchUrls()).length
}

export async function createUrl(url: string): Promise<URLEntry> {
  return api.post<URLEntry>('/urls', { url })
}

export async function deleteUrl(id: number): Promise<void> {
  await api.delete<void>(`/urls/${id}`)
}

export async function setUrlEnabled(id: number, enabled: boolean): Promise<void> {
  await api.patch<void>(`/urls/${id}`, { enabled })
}
```

Note: `api.patch` may not exist yet on the `client.ts` api object. Check `web/src/api/client.ts` — if there is no `patch` method, add one following the same pattern as `post`:

```ts
patch: <T>(path: string, body?: unknown) =>
  request<T>(path, { method: 'PATCH', body: body ? JSON.stringify(body) : undefined }),
```

- [ ] **Step 4: Update `web/src/routes/urls.tsx`**

Add import at top:

```tsx
import { Switch } from '@/components/ui/switch'
import { setUrlEnabled } from '../api/urls'
```

Remove `departmentId` / departments state from `AddUrlDialog` (no longer needed — server derives from session):

```tsx
function AddUrlDialog({ open, onClose, onAdded }: {
  open: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [value, setValue] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setValue(''); setError(null) }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const domains = value.split('\n').map(s => s.trim()).filter(Boolean)
    if (domains.length === 0) { setError('At least one domain is required'); return }
    setLoading(true)
    setError(null)
    try {
      await Promise.all(domains.map(d => createUrl(d)))
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add domain')
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Add Domain</DialogTitle>
          <DialogDescription>
            Enter one or more domains to monitor. One per line.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="add-url-input">Domain</label>
            <textarea
              id="add-url-input"
              className="form-input"
              placeholder={'example.com\nexample2.com'}
              value={value}
              onChange={e => setValue(e.target.value)}
              autoFocus
              disabled={loading}
              rows={4}
              style={{ resize: 'vertical', fontFamily: 'inherit' }}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>Cancel</button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add Domain'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
```

In `URLsPage`, handle optimistic toggle in the url list. Change `urls` state to `URLEntry[]` (already is) and add the toggle handler. In the table row, replace the existing `TableCell` for the delete button area with two cells — one for the switch, one for delete:

```tsx
// In URLsPage, add toggle handler after the load callback:
const handleToggle = useCallback(async (id: number, enabled: boolean) => {
  setUrls(prev => prev.map(u => u.id === id ? { ...u, enabled } : u))
  try {
    await setUrlEnabled(id, enabled)
  } catch {
    setUrls(prev => prev.map(u => u.id === id ? { ...u, enabled: !enabled } : u))
  }
}, [])
```

In the table row (replace the single action cell):

```tsx
<TableCell className="col-status" style={{ textAlign: 'center' }}>
  <Switch
    checked={u.enabled}
    onCheckedChange={checked => handleToggle(u.id, checked)}
    aria-label={`${u.enabled ? 'Disable' : 'Enable'} ${u.url}`}
  />
</TableCell>
<TableCell className="col-evidence" style={{ textAlign: 'right' }}>
  <button
    className="btn-row-delete"
    onClick={() => setDeleteTarget(u)}
    aria-label={`Delete ${u.url}`}
  >
    Delete
  </button>
</TableCell>
```

Also update the `AddUrlDialog` usage — remove `isAdmin` prop since it's no longer used:

```tsx
<AddUrlDialog
  open={addOpen}
  onClose={() => setAddOpen(false)}
  onAdded={load}
/>
```

- [ ] **Step 5: Start dev stack and verify toggle works**

```bash
./dev.sh
```

Navigate to `http://localhost:5173/urls`. Each domain row should show a Switch. Toggle one off — verify the switch flips. Trigger a scan (`Run Scan`) — the disabled domain should not appear in scan progress.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/ui/switch.tsx web/src/api/types.ts web/src/api/urls.ts web/src/routes/urls.tsx
git commit -m "feat(web): URL enable/disable switch per row, remove admin department picker from add dialog"
```

---

## Task 6: Frontend — Scan Selected dialog

**Files:**
- Modify: `web/src/api/scan.ts`
- Modify: `web/src/routes/__root.tsx`

**Interfaces:**
- Consumes: `triggerScan(urls?: string[])` — empty/absent = scan all
- Produces: "Run Scan" → dropdown with "Scan All" and "Scan Selected"; Scan Selected dialog with URL checklist + ad-hoc textarea

- [ ] **Step 1: Update `triggerScan` in `web/src/api/scan.ts`**

```ts
export async function triggerScan(urls?: string[]): Promise<void> {
  const body = urls && urls.length > 0 ? JSON.stringify({ urls }) : undefined
  const res = await fetch('/api/scan', {
    method: 'POST',
    credentials: 'same-origin',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body,
  })
  if (!res.ok && res.status !== 409) throw new Error(`Failed to start scan: ${res.status}`)
}
```

- [ ] **Step 2: Add the Scan Selected dialog and split button in `web/src/routes/__root.tsx`**

Add imports after existing ones:

```tsx
import { useState, useEffect as useEffectScan } from 'react'
import { fetchUrls } from '../api/urls'
import type { URLEntry } from '../api/types'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
```

Add `ScanSelectedDialog` component (place before the root route definition):

```tsx
function ScanSelectedDialog({
  open,
  onClose,
  onStart,
}: {
  open: boolean
  onClose: () => void
  onStart: (urls: string[]) => void
}) {
  const [watchlistUrls, setWatchlistUrls] = useState<URLEntry[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [adhoc, setAdhoc] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffectScan(() => {
    if (!open) return
    fetchUrls().then(urls => {
      setWatchlistUrls(urls)
      setSelected(new Set(urls.filter(u => u.enabled).map(u => u.url)))
    }).catch(() => {})
  }, [open])

  const toggle = (url: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      next.has(url) ? next.delete(url) : next.add(url)
      return next
    })
  }

  const handleStart = () => {
    const adhocList = adhoc.split('\n').map(s => s.trim()).filter(Boolean)
    const all = [...Array.from(selected), ...adhocList]
    if (all.length === 0) { setError('Select at least one domain'); return }
    onStart(all)
    setAdhoc('')
    setError(null)
    onClose()
  }

  const handleClose = () => { setAdhoc(''); setError(null); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 480 }}>
        <DialogHeader>
          <DialogTitle>Scan Selected Domains</DialogTitle>
          <DialogDescription>
            Choose domains from your watchlist or enter additional ones. Each domain is scanned once regardless of how many departments watch it.
          </DialogDescription>
        </DialogHeader>

        {watchlistUrls.length > 0 && (
          <div style={{ maxHeight: 220, overflowY: 'auto', marginBottom: 12 }}>
            {watchlistUrls.map(u => (
              <label
                key={u.id}
                style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 0', cursor: 'pointer', fontSize: '0.875rem' }}
              >
                <input
                  type="checkbox"
                  checked={selected.has(u.url)}
                  onChange={() => toggle(u.url)}
                />
                <span>{u.url}</span>
              </label>
            ))}
          </div>
        )}

        <div className="form-field">
          <label className="form-label" htmlFor="scan-adhoc-input">
            Additional domains <span style={{ color: 'var(--stone-muted)', fontWeight: 400 }}>(one per line, not saved to watchlist)</span>
          </label>
          <textarea
            id="scan-adhoc-input"
            className="form-input"
            placeholder={'adhoc-domain.com\nanother.com'}
            value={adhoc}
            onChange={e => setAdhoc(e.target.value)}
            rows={3}
            style={{ resize: 'vertical', fontFamily: 'inherit' }}
            disabled={loading}
          />
        </div>

        {error && <p className="form-error">{error}</p>}

        <DialogFooter>
          <button className="btn-ghost" onClick={handleClose} disabled={loading}>Cancel</button>
          <button className="btn-primary" onClick={handleStart} disabled={loading}>
            Start Scan
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
```

Update `handleScanClick` and add `scanSelectedOpen` state in the root layout component:

```tsx
const [scanSelectedOpen, setScanSelectedOpen] = useState(false)

const handleScanAll = useCallback(async () => {
  if (scanning) return
  try {
    await triggerScan()
    setScanning(true)
    startPolling()
  } catch (err) {
    console.error('Scan trigger failed:', err)
  }
}, [scanning, startPolling])

const handleScanSelected = useCallback(async (urls: string[]) => {
  if (scanning) return
  try {
    await triggerScan(urls)
    setScanning(true)
    startPolling()
  } catch (err) {
    console.error('Targeted scan trigger failed:', err)
  }
}, [scanning, startPolling])
```

Update `ScanContext` to expose `handleScanAll` as `handleScanClick` (keeping the existing context shape intact) and expose `setScanSelectedOpen`.

Replace the navbar `ShimmerButton` with two adjacent buttons:

```tsx
<div style={{ display: 'flex', gap: 4 }}>
  <ShimmerButton
    text="Scan All"
    scanning={scanning}
    disabled={scanning}
    onClick={handleScanAll}
    ariaLabel={scanning ? 'Scan in progress' : 'Scan all watched domains'}
  />
  <button
    className="btn-ghost"
    disabled={scanning}
    onClick={() => setScanSelectedOpen(true)}
    aria-label="Scan selected domains"
    style={{ fontSize: '0.8125rem' }}
  >
    Scan Selected
  </button>
</div>
```

Below the navbar, before `<main>`, add the dialog:

```tsx
<ScanSelectedDialog
  open={scanSelectedOpen}
  onClose={() => setScanSelectedOpen(false)}
  onStart={handleScanSelected}
/>
```

Update `ScanContext` value to keep `handleScanClick` pointing to `handleScanAll` (other pages may call `handleScanClick` from context — keep it working):

```tsx
<ScanContext value={{ scanning, refreshSignal, handleScanClick: handleScanAll }}>
```

- [ ] **Step 3: Verify in browser**

Run `./dev.sh`. Navigate to `http://localhost:5173`. The navbar should show "Scan All" and "Scan Selected" buttons. Click "Scan Selected" — the dialog opens with checkboxes for your watchlist URLs (enabled ones pre-checked). Add an ad-hoc domain in the textarea. Click "Start Scan". Confirm the scan starts (banner appears). Check server logs for `[server]` lines showing the crawler running.

- [ ] **Step 4: Commit**

```bash
git add web/src/api/scan.ts web/src/routes/__root.tsx
git commit -m "feat(web): Scan Selected dialog with watchlist checklist and ad-hoc URL input"
```

---

## Self-Review Checklist

- [x] `DepartmentURL.Enabled` added with `default:true` — existing rows unaffected
- [x] `MigrateAdminDepartments` is idempotent — safe to call every startup on both fresh and existing DBs
- [x] `SeedAdmin` calls `MigrateAdminDepartments` implicitly via startup order (Task 1 step 4)
- [x] `ListWatchedURLs` uses `DISTINCT` + `enabled = true` — one scan per domain across all departments
- [x] `SetURLEnabled` returns `(bool, error)` — handler maps false → 404
- [x] `fullMockStore.ListDepartmentURLs` updated to return `[]db.URLEntry` (Task 3 step 1)
- [x] `fullMockStore.ListWatchedURLs` updated to filter by `Enabled` (Task 3 step 1)
- [x] `Scanner.Trigger` signature change propagated to scheduler and all test call sites
- [x] `triggerScan(urls?)` — body only sent when urls is non-empty (no body for Scan All)
- [x] `ScanContext.handleScanClick` preserved — existing consumers of `useScan()` unaffected
- [x] `createUrl` no longer sends `department_id` — server uses session department for all users
- [x] `api.patch` may need adding to `client.ts` — noted in Task 5 step 3
