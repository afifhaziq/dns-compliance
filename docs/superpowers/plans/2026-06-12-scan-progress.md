# Scan Progress Section Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a live scan-progress section to the Results page (single climbing chart + per-DNS table) and surface `scanned_at` in each DNS sub-row.

**Architecture:** New `GET /api/scan/progress` endpoint queries completed result counts per DNS server for the most recent scan run. The frontend polls it during a scan, accumulates chart points, and freezes the chart on completion. All changes are additive — no existing behaviour is broken.

**Tech Stack:** Go/GORM (backend), React 19 + TypeScript, `@bklit/live-line-chart` (via shadcn registry), Tailwind CSS.

---

## File Map

| File | Change |
|---|---|
| `internal/db/models.go` | Add `ProgressEntry` struct |
| `internal/db/store.go` | Add `LastScanRun` and `ScanProgress` to `Store` interface |
| `internal/db/postgres.go` | Implement `LastScanRun` and `ScanProgress` |
| `internal/db/postgres_test.go` | Tests for both new methods |
| `internal/server/handlers.go` | Add `ScanProgress` handler |
| `internal/server/handlers_test.go` | Update mock + add handler tests |
| `internal/server/router.go` | Register `GET /api/scan/progress` |
| `web/src/components/ui/live-line-chart.tsx` | Created by shadcn install |
| `web/src/api/scan.ts` | Add `ProgressEntry`, `ScanProgressResponse` types, `fetchScanProgress` |
| `web/src/routes/results.tsx` | Add `scanned_at` sub-row column, `useScanProgress` hook, `ScanProgressSection` component |
| `web/src/index.css` | Progress section styles |

---

## Task 1: DB — `ProgressEntry` type and `Store` interface additions

**Files:**
- Modify: `internal/db/models.go`
- Modify: `internal/db/store.go`

- [ ] **Step 1: Add `ProgressEntry` to models.go**

In `internal/db/models.go`, append after the existing structs:

```go
type ProgressEntry struct {
	DNSServerID uint   `json:"dns_server_id"`
	Name        string `json:"name"`
	Completed   int    `json:"completed"`
}
```

- [ ] **Step 2: Extend `Store` interface in store.go**

In `internal/db/store.go`, add two methods to the `Store` interface after `ActiveScanRun`:

```go
LastScanRun(ctx context.Context) (*ScanRun, error)
ScanProgress(ctx context.Context, runID uint) ([]ProgressEntry, error)
```

- [ ] **Step 3: Verify the code compiles (store interface is unsatisfied — expected)**

```bash
go build ./...
```

Expected: compile error — `postgresStore does not implement Store (missing LastScanRun method)`. This confirms the interface change is wired correctly.

---

## Task 2: DB — implement `LastScanRun` and `ScanProgress`

**Files:**
- Modify: `internal/db/postgres.go`
- Modify: `internal/db/postgres_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/db/postgres_test.go`:

```go
func TestLastScanRun_None(t *testing.T) {
	s := newTestStore(t)
	run, err := s.LastScanRun(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run != nil {
		t.Fatalf("expected nil, got %+v", run)
	}
}

func TestLastScanRun_ReturnsLatest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, _ := s.CreateScanRun(ctx, "scheduled")
	_ = s.CompleteScanRun(ctx, first.ID, "completed", time.Now())
	time.Sleep(2 * time.Millisecond)
	second, _ := s.CreateScanRun(ctx, "manual")

	got, err := s.LastScanRun(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID != second.ID {
		t.Fatalf("expected run id=%d, got %+v", second.ID, got)
	}
}

func TestScanProgress_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.CreateDNSServer(ctx, db.DNSServer{Name: "CF", Address: "1.1.1.1:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	entries, err := s.ScanProgress(ctx, run.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Completed != 0 {
		t.Fatalf("expected 0 completed, got %d", entries[0].Completed)
	}
}

func TestScanProgress_WithResults(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv1, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "CF", Address: "1.1.1.1:53", Protocol: "udp"})
	srv2, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "Google", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	for _, url := range []string{"https://a.com", "https://b.com"} {
		_ = s.InsertResult(ctx, db.ScanResult{
			ScanRunID: run.ID, URLValue: url, DNSServerID: srv1.ID,
			Compliant: false, ScannedAt: time.Now(),
		})
	}
	_ = s.InsertResult(ctx, db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://a.com", DNSServerID: srv2.ID,
		Compliant: false, ScannedAt: time.Now(),
	})

	entries, err := s.ScanProgress(ctx, run.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byName := make(map[string]int)
	for _, e := range entries {
		byName[e.Name] = e.Completed
	}
	if byName["CF"] != 2 {
		t.Fatalf("expected CF=2, got %d", byName["CF"])
	}
	if byName["Google"] != 1 {
		t.Fatalf("expected Google=1, got %d", byName["Google"])
	}
}
```

- [ ] **Step 2: Run failing tests**

```bash
go test ./internal/db/... -run "TestLastScanRun|TestScanProgress" -v
```

Expected: compilation failure — methods not yet implemented.

- [ ] **Step 3: Implement `LastScanRun` and `ScanProgress` in postgres.go**

Append to `internal/db/postgres.go` (before the last closing brace is not needed — just add at end of file):

```go
func (s *postgresStore) LastScanRun(ctx context.Context) (*ScanRun, error) {
	var run ScanRun
	err := s.db.WithContext(ctx).Order("started_at DESC").First(&run).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

type progressRow struct {
	DNSServerID uint
	Name        string
	Completed   int
}

func (s *postgresStore) ScanProgress(ctx context.Context, runID uint) ([]ProgressEntry, error) {
	var rows []progressRow
	err := s.db.WithContext(ctx).
		Model(&DNSServer{}).
		Select("dns_servers.id as dns_server_id, dns_servers.name, COUNT(scan_results.id) as completed").
		Joins("LEFT JOIN scan_results ON scan_results.dns_server_id = dns_servers.id AND scan_results.scan_run_id = ?", runID).
		Group("dns_servers.id, dns_servers.name").
		Order("dns_servers.name").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	entries := make([]ProgressEntry, len(rows))
	for i, r := range rows {
		entries[i] = ProgressEntry{DNSServerID: r.DNSServerID, Name: r.Name, Completed: r.Completed}
	}
	return entries, nil
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/db/... -run "TestLastScanRun|TestScanProgress" -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Run full DB test suite**

```bash
go test ./internal/db/... -v
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/db/models.go internal/db/store.go internal/db/postgres.go internal/db/postgres_test.go
git commit -m "feat(db): add LastScanRun and ScanProgress store methods"
```

---

## Task 3: HTTP handler + route

**Files:**
- Modify: `internal/server/handlers.go`
- Modify: `internal/server/handlers_test.go`
- Modify: `internal/server/router.go`

- [ ] **Step 1: Add mock methods to `fullMockStore` in handlers_test.go**

In `internal/server/handlers_test.go`, add two fields to `fullMockStore`:

```go
type fullMockStore struct {
	urls       []db.URL
	dnsServers []db.DNSServer
	results    []db.ScanResult
	activeRun  *db.ScanRun
	lastRun    *db.ScanRun
	progress   []db.ProgressEntry
}
```

Then add the two new methods (add after the `ActiveScanRun` mock):

```go
func (m *fullMockStore) LastScanRun(_ context.Context) (*db.ScanRun, error) {
	return m.lastRun, nil
}
func (m *fullMockStore) ScanProgress(_ context.Context, _ uint) ([]db.ProgressEntry, error) {
	return m.progress, nil
}
```

- [ ] **Step 2: Write failing handler tests**

Append to `internal/server/handlers_test.go`:

```go
func TestScanProgressNotFound(t *testing.T) {
	r := setupRouter(&fullMockStore{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/progress", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestScanProgressWithRun(t *testing.T) {
	now := time.Now()
	store := &fullMockStore{
		urls: []db.URL{
			{ID: 1, URL: "https://a.com"},
			{ID: 2, URL: "https://b.com"},
		},
		lastRun: &db.ScanRun{ID: 3, Status: "completed", StartedAt: now},
		progress: []db.ProgressEntry{
			{DNSServerID: 1, Name: "CF", Completed: 2},
			{DNSServerID: 2, Name: "Google", Completed: 1},
		},
	}
	r := setupRouter(store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/scan/progress", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ScanRun   map[string]any   `json:"scan_run"`
		TotalURLs int              `json:"total_urls"`
		PerDNS    []map[string]any `json:"per_dns"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TotalURLs != 2 {
		t.Fatalf("expected total_urls=2, got %d", resp.TotalURLs)
	}
	if len(resp.PerDNS) != 2 {
		t.Fatalf("expected 2 per_dns entries, got %d", len(resp.PerDNS))
	}
}
```

- [ ] **Step 3: Run tests — expect fail (handler not yet registered)**

```bash
go test ./internal/server/... -run "TestScanProgress" -v
```

Expected: FAIL — 404 route not found (chi returns 405/404).

- [ ] **Step 4: Add `ScanProgress` handler in handlers.go**

In `internal/server/handlers.go`, add after the `ScanStatus` handler:

```go
type scanProgressResponse struct {
	ScanRun   *db.ScanRun        `json:"scan_run"`
	TotalURLs int                `json:"total_urls"`
	PerDNS    []db.ProgressEntry `json:"per_dns"`
}

func (h *Handlers) ScanProgress(w http.ResponseWriter, r *http.Request) {
	run, err := h.store.LastScanRun(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "no scan run found")
		return
	}
	progress, err := h.store.ScanProgress(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	urls, err := h.store.ListURLs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, scanProgressResponse{
		ScanRun:   run,
		TotalURLs: len(urls),
		PerDNS:    progress,
	})
}
```

- [ ] **Step 5: Register the route in router.go**

In `internal/server/router.go`, add inside the `/api` block after `r.Get("/scan/status", h.ScanStatus)`:

```go
r.Get("/scan/progress", h.ScanProgress)
```

- [ ] **Step 6: Run handler tests — expect pass**

```bash
go test ./internal/server/... -run "TestScanProgress" -v
```

Expected: both tests PASS.

- [ ] **Step 7: Run full server test suite**

```bash
go test ./internal/server/... -v
```

Expected: all tests PASS.

- [ ] **Step 8: Run full test suite**

```bash
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/server/handlers.go internal/server/handlers_test.go internal/server/router.go
git commit -m "feat(server): add GET /api/scan/progress endpoint"
```

---

## Task 4: Install bklit live-line-chart

**Files:**
- Create: `web/src/components/ui/live-line-chart.tsx` (created by shadcn)

- [ ] **Step 1: Install the component**

```bash
cd web && npx shadcn@latest add https://bklit.com/r/live-line-chart.json
```

If prompted to proceed, confirm yes. The component will be installed to `web/src/components/ui/live-line-chart.tsx`.

- [ ] **Step 2: Confirm the file exists and find the export names**

```bash
grep "^export" web/src/components/ui/live-line-chart.tsx | head -20
```

Expected output includes: `LiveLineChart`, `LiveLine`, `LiveXAxis`, `LiveYAxis` (and possibly `ChartTooltip`). Note the exact export names — use them verbatim in Task 6.

- [ ] **Step 3: Verify the frontend still builds**

```bash
cd web && npm run build 2>&1 | tail -10
```

Expected: build succeeds (or only pre-existing warnings).

- [ ] **Step 4: Commit**

```bash
git add web/src/components/ui/live-line-chart.tsx web/package.json web/package-lock.json
git commit -m "feat(web): install bklit live-line-chart component"
```

---

## Task 5: Frontend — API types and `fetchScanProgress`

**Files:**
- Modify: `web/src/api/scan.ts`

- [ ] **Step 1: Add types and fetch function**

In `web/src/api/scan.ts`, add after the existing imports:

```typescript
import type { ScanRun } from './types'
```

Then append at the end of the file:

```typescript
export type ProgressEntry = {
  dns_server_id: number
  name: string
  completed: number
}

export type ScanProgressResponse = {
  scan_run: ScanRun
  total_urls: number
  per_dns: ProgressEntry[]
}

export async function fetchScanProgress(): Promise<ScanProgressResponse> {
  const res = await fetch('/api/scan/progress')
  if (res.status === 404) throw Object.assign(new Error('no_run'), { code: 'no_run' })
  if (!res.ok) throw new Error(`Failed to load progress: ${res.status}`)
  return res.json()
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit 2>&1 | head -20
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/api/scan.ts
git commit -m "feat(web): add ScanProgressResponse types and fetchScanProgress"
```

---

## Task 6: Frontend — `scanned_at` in sub-rows

**Files:**
- Modify: `web/src/routes/results.tsx`
- Modify: `web/src/index.css`

- [ ] **Step 1: Add `relativeTime` helper near the top of results.tsx**

In `web/src/routes/results.tsx`, add after the imports block (before `type StatusFilter`):

```typescript
function relativeTime(isoString: string): string {
  const diff = (Date.now() - new Date(isoString).getTime()) / 1000
  const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
  if (diff < 60) return rtf.format(-Math.round(diff), 'second')
  if (diff < 3600) return rtf.format(-Math.round(diff / 60), 'minute')
  if (diff < 86400) return rtf.format(-Math.round(diff / 3600), 'hour')
  return rtf.format(-Math.round(diff / 86400), 'day')
}
```

- [ ] **Step 2: Add "Last scanned" cell to `SubRows`**

In `SubRows`, replace the existing `<tr>` content to add a `col-last-scanned` cell after `col-evidence`:

```tsx
<tr
  key={r.id}
  className={`sub-row${!r.compliant ? ' violation-row' : ''}`}
>
  <td className="col-expand" />
  <td className="col-domain">
    <span className="dns-name">{r.dns_server.name}</span>
    {r.error && (
      <span
        style={{ marginLeft: 8, fontSize: '0.75rem', color: 'var(--violation-text)' }}
        title={r.error}
      >
        (error)
      </span>
    )}
  </td>
  <td className="col-status">
    <StatusDot compliant={r.compliant} />
  </td>
  <td className="col-ip">
    {r.resolved_ip ? (
      <span className="ip-value">{r.resolved_ip}</span>
    ) : (
      <span className="empty-cell" aria-label="Not resolved">—</span>
    )}
  </td>
  <td className="col-evidence">
    {r.screenshot_url ? (
      <a
        href={r.screenshot_url}
        target="_blank"
        rel="noopener noreferrer"
        className="screenshot-link"
        aria-label={`View screenshot for ${r.dns_server.name}`}
      >
        View screenshot
      </a>
    ) : (
      <span className="empty-cell" aria-label="No screenshot">—</span>
    )}
  </td>
  <td className="col-last-scanned">
    {r.scanned_at ? (
      <span title={new Date(r.scanned_at).toLocaleString()}>{relativeTime(r.scanned_at)}</span>
    ) : (
      <span className="empty-cell">—</span>
    )}
  </td>
</tr>
```

- [ ] **Step 3: Add "Last scanned" header and empty cell to the table**

In the `<thead>` row inside `ResultsPage`, add a new `<th>` after the Evidence column:

```tsx
<th className="col-last-scanned" scope="col">Last scanned</th>
```

In `URLGroupRow`, add an empty `<td className="col-last-scanned" />` after the `col-evidence` td.

In `SkeletonRows`, add an empty `<td className="col-last-scanned" />` after the `col-evidence` td.

- [ ] **Step 4: Add CSS for the new column**

In `web/src/index.css`, add after the `.col-evidence` rule:

```css
.col-last-scanned {
  width: 130px;
  white-space: nowrap;
  color: var(--muted-foreground, #888);
  font-size: 0.8rem;
}
```

- [ ] **Step 5: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit 2>&1 | head -20
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/routes/results.tsx web/src/index.css
git commit -m "feat(web): show last scanned time in DNS sub-rows"
```

---

## Task 7: Frontend — `useScanProgress` hook and `ScanProgressSection`

**Files:**
- Modify: `web/src/routes/results.tsx`
- Modify: `web/src/index.css`

- [ ] **Step 1: Add imports to results.tsx**

At the top of `web/src/routes/results.tsx`, add to the existing React import:

```typescript
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
```

Add new imports below the existing imports:

```typescript
import { fetchScanProgress } from '../api/scan'
import type { ProgressEntry, ScanProgressResponse } from '../api/scan'
import { LiveLineChart, LiveLine, LiveXAxis, LiveYAxis } from '@/components/ui/live-line-chart'
```

> Note: Use the exact export names you confirmed in Task 4 Step 2.

- [ ] **Step 2: Add `useScanProgress` hook**

Add this hook in `results.tsx` before the `ResultsPage` function:

```typescript
type ChartPoint = { time: number; value: number }

function useScanProgress() {
  const { scanning } = useScan()
  const [progress, setProgress] = useState<ScanProgressResponse | null>(null)
  const [chartData, setChartData] = useState<ChartPoint[]>([])
  const [latestValue, setLatestValue] = useState(0)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const prevScanningRef = useRef(scanning)

  const appendPoint = useCallback((total: number) => {
    setChartData(prev => [...prev.slice(-500), { time: Date.now() / 1000, value: total }])
    setLatestValue(total)
  }, [])

  const fetchAndUpdate = useCallback(async () => {
    try {
      const data = await fetchScanProgress()
      setProgress(data)
      const total = data.per_dns.reduce((sum, d) => sum + d.completed, 0)
      appendPoint(total)
    } catch (err) {
      if (err instanceof Error && err.message === 'no_run') return
    }
  }, [appendPoint])

  // Mount: restore persisted state
  useEffect(() => { fetchAndUpdate() }, [fetchAndUpdate])

  // Polling while scanning; final fetch on completion
  useEffect(() => {
    const wasScanning = prevScanningRef.current
    prevScanningRef.current = scanning

    if (scanning) {
      pollRef.current = setInterval(fetchAndUpdate, 2000)
      return () => {
        if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null }
      }
    } else if (wasScanning) {
      fetchAndUpdate()
    }
  }, [scanning, fetchAndUpdate])

  return { progress, chartData, latestValue }
}
```

- [ ] **Step 3: Add `DNSProgressTable` component**

Add before `ResultsPage`:

```tsx
function DNSProgressTable({ perDns, totalUrls }: { perDns: ProgressEntry[]; totalUrls: number }) {
  const sorted = [...perDns].sort((a, b) => {
    const aDone = totalUrls > 0 && a.completed === totalUrls
    const bDone = totalUrls > 0 && b.completed === totalUrls
    if (aDone && !bDone) return -1
    if (!aDone && bDone) return 1
    return a.name.localeCompare(b.name)
  })

  return (
    <div className="progress-dns-wrap">
      <table className="progress-dns-table" aria-label="Per-DNS scan progress">
        <thead>
          <tr>
            <th scope="col">DNS Server</th>
            <th scope="col">Completed</th>
            <th scope="col">Progress</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map(d => {
            const pct = totalUrls > 0 ? Math.round((d.completed / totalUrls) * 100) : 0
            const done = totalUrls > 0 && d.completed === totalUrls
            return (
              <tr key={d.dns_server_id} className={done ? 'progress-row-done' : ''}>
                <td>{d.name}</td>
                <td className="progress-count">{d.completed} / {totalUrls}</td>
                <td className="progress-bar-cell">
                  <div className="progress-bar-wrap" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
                    <div className="progress-bar-fill" style={{ width: `${pct}%` }} />
                  </div>
                  {done && <span className="progress-check" aria-label="Complete">✓</span>}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
```

- [ ] **Step 4: Add `ScanProgressSection` component**

Add before `ResultsPage`:

```tsx
function ScanProgressSection() {
  const { scanning } = useScan()
  const { progress, chartData, latestValue } = useScanProgress()

  if (!progress || progress.total_urls === 0) return null

  const { scan_run, total_urls, per_dns } = progress
  const isRunning = scan_run.status === 'running'

  return (
    <section className="progress-section" aria-label="Scan progress">
      <div className="progress-section-header">
        <h2 className="progress-section-title">Scan Progress</h2>
        {isRunning ? (
          <span className="progress-status-badge running" role="status">
            <span className="scan-banner-dot" aria-hidden="true" />
            Running…
          </span>
        ) : (
          <span className="progress-status-badge completed">Completed ✓</span>
        )}
      </div>

      <div className="progress-chart-wrap">
        <LiveLineChart data={chartData} value={latestValue} window={60} paused={!scanning}>
          <LiveLine dataKey="value" pulse={scanning} formatValue={(v: number) => `${Math.round(v)} URLs`} />
          <LiveXAxis />
          <LiveYAxis position="left" formatValue={(v: number) => String(Math.round(v))} />
        </LiveLineChart>
      </div>

      <DNSProgressTable perDns={per_dns} totalUrls={total_urls} />
    </section>
  )
}
```

- [ ] **Step 5: Render `ScanProgressSection` in `ResultsPage`**

In `ResultsPage`, add `<ScanProgressSection />` immediately before `{/* Filter bar */}`:

```tsx
<ScanProgressSection />

{/* Filter bar */}
<div className="filter-bar">
```

- [ ] **Step 6: Add CSS for the progress section**

Append to `web/src/index.css`:

```css
/* ── Scan Progress Section ────────────────────────────────────────────── */

.progress-section {
  margin-bottom: 1.5rem;
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  overflow: hidden;
}

.progress-section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.75rem 1rem;
  border-bottom: 1px solid var(--border);
}

.progress-section-title {
  font-size: 0.875rem;
  font-weight: 600;
  margin: 0;
}

.progress-status-badge {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.75rem;
  font-weight: 500;
  padding: 0.2rem 0.6rem;
  border-radius: 9999px;
}

.progress-status-badge.running {
  background: color-mix(in srgb, var(--chart-1, #3b82f6) 15%, transparent);
  color: var(--chart-1, #3b82f6);
}

.progress-status-badge.completed {
  background: color-mix(in srgb, var(--chart-2, #22c55e) 15%, transparent);
  color: var(--chart-2, #22c55e);
}

.progress-chart-wrap {
  height: 180px;
  padding: 0.5rem 1rem;
}

.progress-dns-wrap {
  max-height: 280px;
  overflow-y: auto;
  border-top: 1px solid var(--border);
}

.progress-dns-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.8125rem;
}

.progress-dns-table th {
  padding: 0.4rem 1rem;
  text-align: left;
  font-weight: 500;
  color: var(--muted-foreground, #888);
  background: var(--muted, #f5f5f5);
  position: sticky;
  top: 0;
  z-index: 1;
}

.progress-dns-table td {
  padding: 0.4rem 1rem;
  border-top: 1px solid var(--border);
  vertical-align: middle;
}

.progress-row-done td {
  opacity: 0.65;
}

.progress-count {
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
  width: 80px;
}

.progress-bar-cell {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.progress-bar-wrap {
  flex: 1;
  height: 6px;
  border-radius: 3px;
  background: var(--border);
  overflow: hidden;
}

.progress-bar-fill {
  height: 100%;
  border-radius: 3px;
  background: var(--chart-1, #3b82f6);
  transition: width 0.4s ease;
}

.progress-row-done .progress-bar-fill {
  background: var(--chart-2, #22c55e);
}

.progress-check {
  font-size: 0.75rem;
  color: var(--chart-2, #22c55e);
  flex-shrink: 0;
}
```

- [ ] **Step 7: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit 2>&1 | head -20
```

Expected: no errors.

- [ ] **Step 8: Verify dev build runs without errors**

```bash
cd web && npm run build 2>&1 | tail -15
```

Expected: build succeeds.

- [ ] **Step 9: Commit**

```bash
git add web/src/routes/results.tsx web/src/index.css
git commit -m "feat(web): add live scan progress section with chart and per-DNS table"
```

---

## Self-Review

**Spec coverage:**
- ✅ `scanned_at` per sub-row — Task 6
- ✅ `GET /api/scan/progress` endpoint — Tasks 1–3
- ✅ All DNS servers shown even with 0 completed — `ScanProgress` LEFT JOIN
- ✅ Persists after scan completes — `LastScanRun` (no status filter) + `paused={!scanning}`
- ✅ `total_urls === 0` guard — `ScanProgressSection` early return
- ✅ Final fetch on scan completion — `prevScanningRef` transition logic in `useScanProgress`
- ✅ 404 when no scan ever run — `LastScanRun` nil check in handler
- ✅ Completed DNS rows sorted to top — `DNSProgressTable` sort
- ✅ bklit install — Task 4

**Type consistency check:**
- `ProgressEntry` defined in `internal/db/models.go` → used in `store.go`, `postgres.go`, `handlers.go` ✅
- `ProgressEntry` (frontend) defined in `web/src/api/scan.ts` → used in `results.tsx` ✅
- `ScanProgressResponse` defined in `scan.ts` → used in `useScanProgress` ✅
- `ChartPoint` local to `useScanProgress` ✅
- `scanProgressResponse` local struct in `handlers.go` ✅
