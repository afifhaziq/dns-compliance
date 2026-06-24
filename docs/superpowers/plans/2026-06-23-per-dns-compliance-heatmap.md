# Per-DNS Compliance Heatmap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the synthetic-data placeholder heatmap on `/results/$url` with one real, per-DNS-server calendar-year heatmap (Jan 1–Dec 31, with prev/next year navigation), colored by that day's compliance outcome for that (domain, DNS server) pair.

**Architecture:** A small backend addition (`until` query param on the existing results-by-URL endpoint) enables fetching a bounded calendar-year window. A new pure-function module computes per-day compliance buckets and builds the calendar grid shape the existing `HeatmapChart` library expects. The per-URL route fetches that year's data independently of its existing 7-day table fetch, and renders one heatmap per DNS server.

**Tech Stack:** Go (chi router, GORM/Postgres) for the backend; React + TypeScript + the existing `@visx`-based `HeatmapChart` component family for the frontend. No new dependencies.

## Global Constraints

- Backend change must be additive — the existing `since`-only caller (the 7-day results table, and the gRPC screenshot-lookup call) must keep working unchanged.
- No new CSS hues — reuse the existing `--compliant` / `--violation` / `--violation-subtle` / `--stone-panel` tokens (DESIGN.md's one-accent rule: gray = compliant, red = violation, no other hue).
- No new frontend dependencies; no JS/TS test runner exists in this repo (confirmed during design) — frontend verification is `tsc -b` (type-check) plus manual check via `npm run dev`, not automated tests. Backend verification is real `go test`.
- Per-day cell color: any violation that day → red, graded by `violations/totalScansThatDay` (≤⅓ light, ≤⅔ medium, else deep); zero violations → solid compliant gray; zero scans → neutral/no-data.

---

### Task 1: Backend — add `until` bound to `ResultsByURL`

**Files:**
- Modify: `internal/db/store.go:28`
- Modify: `internal/db/postgres.go:77-85`
- Modify: `internal/server/handlers.go:187-207`
- Modify: `internal/server/grpc.go:71`
- Modify: `internal/server/handlers_test.go:78-86` (mock), add new test after `internal/server/handlers_test.go:299`
- Modify: `internal/server/grpc_test.go:41`
- Modify: `internal/db/postgres_test.go:267`, `:294`, add new test after `:301`

**Interfaces:**
- Produces: `db.Store.ResultsByURL(ctx context.Context, urlValue string, since, until time.Time) ([]ScanResult, error)` — `until` zero-value means unbounded (matches existing `since` zero-value convention already documented at `grpc.go:70`).
- Produces: `GET /api/results/*url` accepts an additional optional `?until=<RFC3339>` query param alongside the existing `?since=`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/db/postgres_test.go` (after `TestResultsByURL_ZeroTimeReturnsAll`, currently ending at line 301):

```go
func TestResultsByURL_FiltersUntil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	srv, _ := s.CreateDNSServer(ctx, db.DNSServer{Name: "G", Address: "8.8.8.8:53", Protocol: "udp"})
	run, _ := s.CreateScanRun(ctx, "manual")

	inWindow := db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID,
		Compliant: true, ScannedAt: time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
	}
	afterWindow := db.ScanResult{
		ScanRunID: run.ID, URLValue: "https://example.com", DNSServerID: srv.ID,
		Compliant: false, ScannedAt: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	if err := s.InsertResult(ctx, inWindow); err != nil {
		t.Fatalf("InsertResult inWindow: %v", err)
	}
	if err := s.InsertResult(ctx, afterWindow); err != nil {
		t.Fatalf("InsertResult afterWindow: %v", err)
	}

	since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	results, err := s.ResultsByURL(ctx, "https://example.com", since, until)
	if err != nil {
		t.Fatalf("ResultsByURL: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result within 2025 window, got %d", len(results))
	}
	if !results[0].Compliant {
		t.Fatalf("expected the in-window 2025 result, got %+v", results[0])
	}
}
```

Also update the two existing calls in that file to pass a third argument (they'll fail to compile otherwise — that's expected for this step):
- `internal/db/postgres_test.go:267`: `s.ResultsByURL(ctx, "https://example.com", since)` → `s.ResultsByURL(ctx, "https://example.com", since, time.Time{})`
- `internal/db/postgres_test.go:294`: `s.ResultsByURL(ctx, "https://example.com", time.Time{})` → `s.ResultsByURL(ctx, "https://example.com", time.Time{}, time.Time{})`

Add to `internal/server/handlers_test.go` (after `TestResultsByURL_ExplicitSince`, currently ending at line 299):

```go
func TestResultsByURL_ExplicitUntil(t *testing.T) {
	store := &fullMockStore{results: []db.ScanResult{
		{ID: 1, URLValue: "https://example.com", ScannedAt: time.Now().AddDate(-1, 0, 0)},
		{ID: 2, URLValue: "https://example.com", ScannedAt: time.Now()},
	}}
	r := setupRouter(store, nil)
	since := url.QueryEscape(time.Now().AddDate(-2, 0, 0).Format(time.RFC3339))
	until := url.QueryEscape(time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, "/api/results/https://example.com?since="+since+"&until="+until, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []db.ScanResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Fatalf("expected 1 result within since/until window, got %d", len(results))
	}
	if results[0].ID != 1 {
		t.Fatalf("expected the older result (id=1), got id=%d", results[0].ID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/db/... ./internal/server/... 2>&1 | head -40`
Expected: compile errors — `not enough arguments in call to s.ResultsByURL` (and similar) — because the interface/implementation/mocks don't accept `until` yet.

- [ ] **Step 3: Update the interface and Postgres implementation**

`internal/db/store.go:28`, change:
```go
	ResultsByURL(ctx context.Context, urlValue string, since time.Time) ([]ScanResult, error)
```
to:
```go
	ResultsByURL(ctx context.Context, urlValue string, since, until time.Time) ([]ScanResult, error)
```

`internal/db/postgres.go:77-85`, change:
```go
func (s *postgresStore) ResultsByURL(ctx context.Context, urlValue string, since time.Time) ([]ScanResult, error) {
	var results []ScanResult
	err := s.db.WithContext(ctx).
		Where("url_value = ? AND scanned_at >= ?", urlValue, since).
		Preload("DNSServer").
		Order("scanned_at desc").
		Find(&results).Error
	return results, err
}
```
to:
```go
func (s *postgresStore) ResultsByURL(ctx context.Context, urlValue string, since, until time.Time) ([]ScanResult, error) {
	var results []ScanResult
	q := s.db.WithContext(ctx).Where("url_value = ? AND scanned_at >= ?", urlValue, since)
	if !until.IsZero() {
		q = q.Where("scanned_at <= ?", until)
	}
	err := q.Preload("DNSServer").Order("scanned_at desc").Find(&results).Error
	return results, err
}
```

- [ ] **Step 4: Update all call sites and mocks**

`internal/server/handlers.go:187-207`, change:
```go
	since := time.Now().AddDate(0, 0, -7)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
			since = parsed
		}
	}

	results, err := h.store.ResultsByURL(r.Context(), urlValue, since)
```
to:
```go
	since := time.Now().AddDate(0, 0, -7)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
			since = parsed
		}
	}

	var until time.Time
	if raw := r.URL.Query().Get("until"); raw != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
			until = parsed
		}
	}

	results, err := h.store.ResultsByURL(r.Context(), urlValue, since, until)
```

`internal/server/grpc.go:71`, change:
```go
			results, err := s.store.ResultsByURL(ctx, r.Url, time.Time{})
```
to:
```go
			results, err := s.store.ResultsByURL(ctx, r.Url, time.Time{}, time.Time{})
```

`internal/server/handlers_test.go:78-86`, change:
```go
func (m *fullMockStore) ResultsByURL(_ context.Context, u string, since time.Time) ([]db.ScanResult, error) {
	var out []db.ScanResult
	for _, r := range m.results {
		if r.URLValue == u && !r.ScannedAt.Before(since) {
			out = append(out, r)
		}
	}
	return out, nil
}
```
to:
```go
func (m *fullMockStore) ResultsByURL(_ context.Context, u string, since, until time.Time) ([]db.ScanResult, error) {
	var out []db.ScanResult
	for _, r := range m.results {
		if r.URLValue != u || r.ScannedAt.Before(since) {
			continue
		}
		if !until.IsZero() && r.ScannedAt.After(until) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
```

`internal/server/grpc_test.go:41`, change:
```go
func (m *mockStore) ResultsByURL(_ context.Context, _ string, _ time.Time) ([]db.ScanResult, error) {
	return m.insertedResults, nil
}
```
to:
```go
func (m *mockStore) ResultsByURL(_ context.Context, _ string, _, _ time.Time) ([]db.ScanResult, error) {
	return m.insertedResults, nil
}
```

- [ ] **Step 5: Run the full build and test suite**

Run: `go build ./... && go test ./...`
Expected: build succeeds; all tests pass, including the two new ones (`TestResultsByURL_FiltersUntil`, `TestResultsByURL_ExplicitUntil`).

- [ ] **Step 6: Commit**

```bash
git add internal/db/store.go internal/db/postgres.go internal/db/postgres_test.go \
        internal/server/handlers.go internal/server/handlers_test.go \
        internal/server/grpc.go internal/server/grpc_test.go
git commit -m "feat(server): add until bound to ResultsByURL for bounded date-range queries"
```

---

### Task 2: Frontend — `fetchResultsByUrlAndYear` API function

**Files:**
- Modify: `web/src/api/results.ts`

**Interfaces:**
- Consumes: `GET /api/results/*url?since=&until=` (Task 1).
- Produces: `fetchResultsByUrlAndYear(url: string, year: number): Promise<ScanResult[]>`, exported from `web/src/api/results.ts`.

- [ ] **Step 1: Add the function**

In `web/src/api/results.ts`, after the existing `fetchResultsByUrl` function (currently lines 9-14):

```ts
export async function fetchResultsByUrlAndYear(url: string, year: number): Promise<ScanResult[]> {
  const since = new Date(year, 0, 1).toISOString()
  const until = new Date(year, 11, 31, 23, 59, 59, 999).toISOString()
  const res = await fetch(`/api/results/${encodeURIComponent(url)}?since=${encodeURIComponent(since)}&until=${encodeURIComponent(until)}`)
  if (!res.ok) throw new Error(`Failed to load results: ${res.status}`)
  return res.json()
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && npx tsc -b`
Expected: no errors (this repo has no JS test runner, so type-checking is the available automated check for this step; behavior is verified end-to-end in Task 4).

- [ ] **Step 3: Commit**

```bash
git add web/src/api/results.ts
git commit -m "feat(web): add fetchResultsByUrlAndYear for calendar-year result queries"
```

---

### Task 3: Frontend — heatmap year-grid data helpers

**Files:**
- Create: `web/src/lib/heatmap-year.ts`

**Interfaces:**
- Consumes: `ScanResult` type (`web/src/api/types.ts`); `HeatmapBin`, `HeatmapColumn` types (`@/components/charts/heatmap/heatmap-context`); `HeatmapLevelColors` type (`@/components/charts/heatmap/heatmap-colors`).
- Produces (all exported from `web/src/lib/heatmap-year.ts`):
  - `type DayStats = { compliant: number; total: number }`
  - `const HEATMAP_LEVEL_COLORS: HeatmapLevelColors`
  - `function heatmapYearColorScale(count?: number | null): string`
  - `function dateKey(d: Date): string`
  - `function buildDayStats(results: ScanResult[]): Map<string, DayStats>`
  - `function dayLevel(stats: DayStats | undefined): number`
  - `function buildYearHeatmapColumns(year: number, dayStats: Map<string, DayStats>): HeatmapColumn[]`
  - `function compliancePercent(results: ScanResult[]): number | null`

- [ ] **Step 1: Create the file**

```ts
import type { HeatmapBin, HeatmapColumn } from '@/components/charts/heatmap/heatmap-context'
import type { HeatmapLevelColors } from '@/components/charts/heatmap/heatmap-colors'
import type { ScanResult } from '@/api/types'

export type DayStats = { compliant: number; total: number }

// Reuses the app's existing compliant/violation design tokens — no new hues.
// Index 0 = no scans that day, 1 = fully compliant, 2-4 = increasing violation severity.
export const HEATMAP_LEVEL_COLORS: HeatmapLevelColors = [
  'var(--stone-panel)',
  'var(--compliant)',
  'color-mix(in oklch, var(--violation) 33%, var(--violation-subtle))',
  'color-mix(in oklch, var(--violation) 66%, var(--violation-subtle))',
  'var(--violation)',
]

export function heatmapYearColorScale(count?: number | null): string {
  const level = Math.min(Math.max(count ?? 0, 0), 4)
  return HEATMAP_LEVEL_COLORS[level]
}

export function dateKey(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function buildDayStats(results: ScanResult[]): Map<string, DayStats> {
  const map = new Map<string, DayStats>()
  for (const r of results) {
    const key = dateKey(new Date(r.scanned_at))
    const existing = map.get(key) ?? { compliant: 0, total: 0 }
    existing.total += 1
    if (r.compliant) existing.compliant += 1
    map.set(key, existing)
  }
  return map
}

// Any violation that day forces the red scale, graded by how much of that
// day's scans failed; zero violations is the solid compliant color.
export function dayLevel(stats: DayStats | undefined): number {
  if (!stats || stats.total === 0) return 0
  const violations = stats.total - stats.compliant
  if (violations === 0) return 1
  const rate = violations / stats.total
  if (rate <= 1 / 3) return 2
  if (rate <= 2 / 3) return 3
  return 4
}

// Builds a full Jan 1-Dec 31 grid (Sunday-first weeks, padded at both ends
// to whole weeks) regardless of how much scan history actually exists.
export function buildYearHeatmapColumns(year: number, dayStats: Map<string, DayStats>): HeatmapColumn[] {
  const jan1 = new Date(year, 0, 1)
  const dec31 = new Date(year, 11, 31)
  const gridStart = new Date(jan1)
  gridStart.setDate(jan1.getDate() - jan1.getDay())
  const gridEnd = new Date(dec31)
  gridEnd.setDate(dec31.getDate() + (6 - dec31.getDay()))

  const columns: HeatmapColumn[] = []
  let columnIndex = 0
  for (
    const weekStart = new Date(gridStart);
    weekStart.getTime() <= gridEnd.getTime();
    weekStart.setDate(weekStart.getDate() + 7)
  ) {
    const bins: HeatmapBin[] = []
    for (let d = 0; d < 7; d++) {
      const cellDate = new Date(weekStart)
      cellDate.setDate(weekStart.getDate() + d)
      const inYear = cellDate.getFullYear() === year
      const displayDate = inYear ? cellDate : (cellDate.getTime() < jan1.getTime() ? jan1 : dec31)
      const stats = inYear ? dayStats.get(dateKey(cellDate)) : undefined
      bins.push({ bin: d, date: displayDate, count: dayLevel(stats) })
    }
    columns.push({ bin: columnIndex, bins })
    columnIndex++
  }
  return columns
}

export function compliancePercent(results: ScanResult[]): number | null {
  if (results.length === 0) return null
  const compliant = results.filter(r => r.compliant).length
  return Math.round((compliant / results.length) * 100)
}
```

Note on the `for (const weekStart = ...; ...; weekStart.setDate(...))` loop: `weekStart` is declared `const` but it's a `Date` object — `.setDate()` mutates the object in place rather than reassigning the binding, so `const` is valid here (and intentional: nothing in the loop body reassigns `weekStart` itself, only its internal date value).

- [ ] **Step 2: Type-check**

Run: `cd web && npx tsc -b`
Expected: no errors.

- [ ] **Step 3: Manually trace one example to confirm the bucketing logic**

This module has no test runner to exercise it (confirmed during design — no JS test infra in this repo). Before moving on, trace through `dayLevel` by hand for the cases that matter:
- `dayLevel(undefined)` → `0` (no data)
- `dayLevel({ compliant: 10, total: 10 })` → `0` violations → `1` (fully compliant)
- `dayLevel({ compliant: 9, total: 10 })` → 1 violation, rate `0.1 ≤ 1/3` → `2` (light red)
- `dayLevel({ compliant: 5, total: 10 })` → 5 violations, rate `0.5`, `1/3 < 0.5 ≤ 2/3` → `3` (medium red)
- `dayLevel({ compliant: 1, total: 10 })` → 9 violations, rate `0.9 > 2/3` → `4` (deep red)

If any of these don't match what the code produces, fix the implementation before continuing — this logic is the core of the feature and is exercised visually (not by an automated test) in Task 4.

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/heatmap-year.ts
git commit -m "feat(web): add per-day compliance bucketing helpers for the year heatmap"
```

---

### Task 4: Frontend — wire per-DNS heatmaps into the per-URL history page

**Files:**
- Modify: `web/src/routes/results.$url.tsx`
- Modify: `web/src/index.css`

**Interfaces:**
- Consumes: `fetchResultsByUrlAndYear` (Task 2); `buildDayStats`, `buildYearHeatmapColumns`, `compliancePercent`, `dateKey`, `HEATMAP_LEVEL_COLORS`, `heatmapYearColorScale` (Task 3); existing `HeatmapChart`, `HeatmapChartLoading`, `HeatmapCells`, `HeatmapXAxis`, `HeatmapYAxis`, `HeatmapTooltip`, `HeatmapLegend` components.
- Produces: the rendered page — no other file depends on this one.

- [ ] **Step 1: Update imports**

In `web/src/routes/results.$url.tsx`, replace lines 1-20:

```tsx
import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { ArrowLeftIcon } from 'lucide-react'
import { fetchResultsByUrl } from '../api/results'
import type { ScanResult } from '../api/types'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import { curveStepAfter } from '@visx/curve'
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'
import { Grid } from '@/components/charts/grid'
import { XAxis } from '@/components/charts/x-axis'
import { ChartTooltip } from '@/components/charts/tooltip'
import { decimateTimeSeries } from '@/components/charts/decimate-time-series'
import { HeatmapChart } from '@/components/charts/heatmap'
import { HeatmapCells } from '@/components/charts/heatmap/heatmap-cells'
import { HeatmapXAxis } from '@/components/charts/heatmap/heatmap-x-axis'
import { HeatmapYAxis } from '@/components/charts/heatmap/heatmap-y-axis'
import { HeatmapTooltip } from '@/components/charts/heatmap/heatmap-tooltip'
import { HeatmapLegend } from '@/components/charts/heatmap/heatmap-legend'
import type { HeatmapColumn } from '@/components/charts/heatmap/heatmap-context'
```

with:

```tsx
import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { ArrowLeftIcon, ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { fetchResultsByUrl, fetchResultsByUrlAndYear } from '../api/results'
import type { ScanResult } from '../api/types'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import { HeatmapChart } from '@/components/charts/heatmap'
import { HeatmapChartLoading } from '@/components/charts/heatmap/heatmap-chart-loading'
import { HeatmapCells } from '@/components/charts/heatmap/heatmap-cells'
import { HeatmapXAxis } from '@/components/charts/heatmap/heatmap-x-axis'
import { HeatmapYAxis } from '@/components/charts/heatmap/heatmap-y-axis'
import { HeatmapTooltip } from '@/components/charts/heatmap/heatmap-tooltip'
import { HeatmapLegend } from '@/components/charts/heatmap/heatmap-legend'
import {
  buildDayStats,
  buildYearHeatmapColumns,
  compliancePercent,
  dateKey,
  HEATMAP_LEVEL_COLORS,
  heatmapYearColorScale,
} from '@/lib/heatmap-year'
```

`curveStepAfter`, `LineChart`, `Line`, `Grid`, `XAxis`, `ChartTooltip`, `decimateTimeSeries`, and the `HeatmapColumn` type import are all dropped — they existed only to feed the commented-out `LineChart` block and `generateSyntheticHeatmapData`, both of which are deleted in Steps 2-4 below. Leaving any of them in place would fail `tsc -b` with the same "declared but never read" errors seen during design (TS6133), since nothing would consume them anymore.

- [ ] **Step 2: Remove the dead line-chart scaffolding and synthetic heatmap data, add year-view constants**

Replace (currently lines 72-107 — the `MAX_CHART_POINTS` comment+const, `generateSyntheticHeatmapData`, the `ChartPoint` type, and `pivotByRun`):

```tsx
// Caps rendered chart points regardless of scan frequency — decimateTimeSeries
// (LTTB) below picks the most visually significant points rather than every
// Nth one, so brief compliance flips are more likely to survive than with
// naive uniform sampling or a frequency-coupled bucket size (e.g. "daily").
const MAX_CHART_POINTS = 240

// Placeholder GitHub-style calendar grid (weeks x days) until the heatmap is
// wired to real violation counts — `count` drives the cell color level (0-4).
function generateSyntheticHeatmapData(weeks = 12): HeatmapColumn[] {
  const today = new Date()
  return Array.from({ length: weeks }, (_, w) => ({
    bin: w,
    bins: Array.from({ length: 7 }, (_, d) => {
      const daysAgo = (weeks - 1 - w) * 7 + (6 - d)
      const date = new Date(today)
      date.setDate(date.getDate() - daysAgo)
      return { bin: d, count: Math.floor(Math.random() * 5), date }
    }),
  }))
}

type ChartPoint = { date: Date; [dnsServerName: string]: number | Date }

function pivotByRun(results: ScanResult[]): ChartPoint[] {
  const runs = new Map<number, { date: Date; values: Record<string, number> }>()
  for (const r of results) {
    const existing = runs.get(r.scan_run_id)
    const values = existing?.values ?? {}
    values[r.dns_server.name] = r.compliant ? 1 : 0
    const date = existing?.date ?? new Date(r.scanned_at)
    runs.set(r.scan_run_id, { date, values })
  }
  return Array.from(runs.values())
    .sort((a, b) => a.date.getTime() - b.date.getTime())
    .map(({ date, values }) => ({ date, ...values }))
}
```

with:

```tsx
const currentYear = new Date().getFullYear()

const heatmapTooltipDateFmt = new Intl.DateTimeFormat('en-US', {
  month: 'long',
  day: 'numeric',
  year: 'numeric',
})
```

`MAX_CHART_POINTS` and `pivotByRun`/`ChartPoint` were only ever consumed by `chartData` (removed in Step 3) and the commented-out `LineChart` block (removed in Step 4) — nothing else in the file uses them.

- [ ] **Step 3: Add year-fetch state and the per-server DNS list**

After the existing `dnsServers` memo (currently lines 134-138, `const dnsServers = useMemo(...)`), add:

```tsx
  const [selectedYear, setSelectedYear] = useState(currentYear)
  const [yearResults, setYearResults] = useState<ScanResult[]>([])
  const [yearLoading, setYearLoading] = useState(true)

  const loadYear = useCallback(async (year: number) => {
    try {
      setYearLoading(true)
      const raw = await fetchResultsByUrlAndYear(url, year)
      setYearResults(raw)
    } catch {
      setYearResults([])
    } finally {
      setYearLoading(false)
    }
  }, [url])

  useEffect(() => { loadYear(selectedYear) }, [loadYear, selectedYear])

  const heatmapDnsServers = useMemo(() => {
    const seen = new Map<string, string>()
    for (const r of yearResults) seen.set(r.dns_server.name, r.dns_server.name)
    for (const r of results) seen.set(r.dns_server.name, r.dns_server.name)
    return Array.from(seen.values()).sort()
  }, [yearResults, results])
```

Then delete the two now-unused memos further down in the component body (currently around lines 149-154):

```tsx
  const chartData = useMemo(
    () => decimateTimeSeries(pivotByRun(filtered), MAX_CHART_POINTS, dnsServers),
    [filtered, dnsServers],
  )

  const heatmapData = useMemo(() => generateSyntheticHeatmapData(), [])
```

Both are fully deleted, no replacement — `chartData` only ever fed the commented-out `LineChart`, and `heatmapData` only ever fed the synthetic placeholder, both removed in Step 4.

- [ ] **Step 4: Replace the heatmap JSX block**

Replace the block currently at lines 168-192:

```tsx
      {!loading && !error && results.length > 0 && (
        // <div className="dash-section">
        //   <LineChart data={chartData} xDataKey="date" aspectRatio="16 / 6">
        //     <Grid horizontal />
        //     <XAxis />
        //     {dnsServers.map((name, i) => (
        //       <Line
        //         key={name}
        //         dataKey={name}
        //         curve={curveStepAfter}
        //         stroke={`var(--chart-${5 - (i % 5)})`}
        //       />
        //     ))}
        //     <ChartTooltip />
        //   </LineChart>
        <div className="dash-section flex-1 px-6">
        <HeatmapChart data={heatmapData} gap={3} layout="fluid">
          <HeatmapCells cornerRadius={999} />
          <HeatmapXAxis />
          <HeatmapYAxis />
          <HeatmapTooltip />
          <HeatmapLegend align="center" cornerRadius={999} gap={3} />
        </HeatmapChart>
        </div>
      )}
```

with:

```tsx
      {heatmapDnsServers.length > 0 && (
        <div className="dash-section">
          <div className="heatmap-year-nav">
            <button
              type="button"
              className="heatmap-year-nav-btn"
              onClick={() => setSelectedYear(y => y - 1)}
              aria-label="Previous year"
            >
              <ChevronLeftIcon className="w-4 h-4" />
            </button>
            <span className="heatmap-year-label">{selectedYear}</span>
            <button
              type="button"
              className="heatmap-year-nav-btn"
              onClick={() => setSelectedYear(y => y + 1)}
              disabled={selectedYear >= currentYear}
              aria-label="Next year"
            >
              <ChevronRightIcon className="w-4 h-4" />
            </button>
          </div>

          {heatmapDnsServers.map(name => {
            const serverResults = yearResults.filter(r => r.dns_server.name === name)
            const dayStats = buildDayStats(serverResults)
            const columns = buildYearHeatmapColumns(selectedYear, dayStats)
            const pct = compliancePercent(serverResults)

            return (
              <div key={name} className="heatmap-server-block">
                <div className="heatmap-server-header">
                  <p className="dash-label">{name}</p>
                  {pct !== null && !yearLoading && (
                    <span className="heatmap-compliance-badge">{pct}% compliant</span>
                  )}
                </div>

                {yearLoading ? (
                  <HeatmapChartLoading data={columns} gap={3} cornerRadius={999} />
                ) : (
                  <HeatmapChart data={columns} gap={3} layout="fluid" levelColors={HEATMAP_LEVEL_COLORS}>
                    <HeatmapCells cornerRadius={999} />
                    <HeatmapXAxis />
                    <HeatmapYAxis />
                    <HeatmapTooltip
                      formatLabel={(_count, date) => {
                        const stats = dayStats.get(dateKey(date))
                        if (!stats) return `No scans · ${heatmapTooltipDateFmt.format(date)}`
                        return `${stats.compliant} of ${stats.total} compliant · ${heatmapTooltipDateFmt.format(date)}`
                      }}
                    />
                    <HeatmapLegend
                      align="center"
                      cornerRadius={999}
                      gap={3}
                      lessLabel="Compliant"
                      moreLabel="More violations"
                      colorScale={heatmapYearColorScale}
                    />
                  </HeatmapChart>
                )}
              </div>
            )
          })}
        </div>
      )}
```

- [ ] **Step 5: Add the new CSS classes**

In `web/src/index.css`, after the `.scan-banner-dot` rule (currently ending around line 483, just before the `/* ─── Results Table ─── */` section header), add:

```css
/* ─── Compliance Heatmap (per-URL history) ─────────────────────────────── */

.heatmap-year-nav {
  @apply flex items-center justify-center gap-3 pb-4;
}

.heatmap-year-nav-btn {
  @apply flex items-center justify-center w-7 h-7 rounded-md bg-transparent border-none text-stone-muted cursor-pointer
    transition-colors duration-150 ease-snappy hover:bg-stone-panel hover:text-foreground
    disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent
    focus-visible:outline focus-visible:outline-2 focus-visible:outline-ink focus-visible:outline-offset-2;
}

.heatmap-year-label {
  @apply text-sm font-semibold text-foreground min-w-[4ch] text-center [font-variant-numeric:tabular-nums];
}

.heatmap-server-block {
  @apply mb-8;
}

.heatmap-server-header {
  @apply flex items-center justify-between gap-3;
}

.heatmap-compliance-badge {
  @apply text-[13px] font-medium text-stone-muted [font-variant-numeric:tabular-nums];
}
```

- [ ] **Step 6: Type-check**

Run: `cd web && npx tsc -b`
Expected: no errors. If `ChevronLeftIcon`/`ChevronRightIcon` aren't valid exports from the installed `lucide-react` version, this is where it surfaces — check `node_modules/lucide-react`'s exports and substitute the correct names (e.g. `ChevronLeft`/`ChevronRight` without the `Icon` suffix) if needed.

- [ ] **Step 7: Manual verification via dev server**

Run: `go run ./cmd/server/ --http-addr :8080 --grpc-addr :50051` (separate terminal, needs Postgres/MinIO per `CLAUDE.md`) and `cd web && npm run dev`, then open a URL's history page (`/results/$url` via the history icon on the Overview table) for a domain with some scan history. Check, for at least one DNS server with a mix of compliant and violation results:

- [ ] Heatmap renders Jan–Dec for the current year, with month labels across the top and Mon/Wed/Fri labels down the side.
- [ ] A day with zero scans is the neutral/no-data color; a day with scans and zero violations is solid compliant gray; a day with some violations is a lighter red than a day where every scan that day was a violation.
- [ ] Hovering a day cell shows "`N of M compliant · <Month> <Day>, <Year>`" (or "No scans · ...") in the tooltip.
- [ ] The legend reads "Compliant" / swatches / "More violations", and the swatch colors match the cells.
- [ ] The `{pct}% compliant` badge next to the server name matches a manual count for that server (compliant scans ÷ total scans for the year).
- [ ] Clicking `<` moves to the previous year and the grid reloads (`HeatmapChartLoading` shimmer briefly visible); clicking `>` is disabled once back at the current year.
- [ ] If there are multiple DNS servers, each gets its own heatmap block, stacked vertically.
- [ ] Toggle dark mode (per the earlier `.page`/`bg-stone-bg` fix) — the red/gray cell colors and badge text remain legible and don't introduce any hue outside red/gray.
- [ ] The existing 7-day table below (filters, rows, screenshot links) still works exactly as before — this change didn't touch that code path.

- [ ] **Step 8: Commit**

```bash
git add web/src/routes/results.$url.tsx web/src/index.css
git commit -m "feat(web): wire per-DNS calendar-year compliance heatmaps into per-URL history page"
```
