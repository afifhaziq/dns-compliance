# Scan Progress Section — Design Spec

**Date:** 2026-06-12  
**Status:** Approved

## Overview

Add a live scan progress section to the Results page that shows total URL completion over time (live line chart) and per-DNS-server progress (compact scrollable table). Also surface `scanned_at` per DNS sub-row in the existing results table.

Persists after the scan completes showing the final state of the last run.

---

## Backend

### New endpoint: `GET /api/scan/progress`

Returns the most recent scan run (active or last completed) plus completion counts per DNS server.

**Response shape:**
```json
{
  "scan_run": {
    "id": 5,
    "triggered_by": "manual",
    "status": "running",
    "started_at": "2026-06-12T08:00:00Z",
    "completed_at": null
  },
  "total_urls": 10,
  "per_dns": [
    { "dns_server_id": 1, "name": "Cloudflare DoT", "completed": 10 },
    { "dns_server_id": 2, "name": "Google UDP",     "completed": 3  },
    { "dns_server_id": 3, "name": "TM DNS",          "completed": 0  }
  ]
}
```

When no scan has ever run, returns `404`.

`per_dns` lists all configured DNS servers (even those with 0 completed) so the table is stable and doesn't jump as servers appear.

### New DB methods (`internal/db/store.go` + `internal/db/postgres.go`)

**`LastScanRun(ctx) (*ScanRun, error)`**  
Returns the most recent `ScanRun` by `started_at DESC` with no status filter. Returns `nil, nil` if none exists.

**`ScanProgress(ctx, runID uint) ([]ProgressEntry, error)`**  
Single query: left-joins all `dns_servers` against `scan_results WHERE scan_run_id = runID`, counts results per server. Returns `[]ProgressEntry{ DNSServerID, Name, Completed }` for every DNS server (completed = 0 if no results yet for that server in this run).

```go
type ProgressEntry struct {
    DNSServerID uint   `json:"dns_server_id"`
    Name        string `json:"name"`
    Completed   int    `json:"completed"`
}
```

### New handler + route

`ScanProgress` handler in `internal/server/handlers.go`:
1. Call `store.LastScanRun(ctx)` — 404 if nil
2. Call `store.ScanProgress(ctx, run.ID)`
3. Call `store.ListURLs(ctx)` for total count
4. Write JSON response

Route added to `internal/server/router.go`: `GET /api/scan/progress`

---

## Frontend

### 1. `scanned_at` in sub-rows

Add a **"Last scanned"** column to `SubRows` in `web/src/routes/results.tsx`.

- Formatted as relative time ("2 min ago") using `Intl.RelativeTimeFormat`
- Full ISO timestamp shown on hover via `title` attribute
- Added to the `<thead>` row as a new `<th>` between Evidence and the end

### 2. New API function (`web/src/api/scan.ts`)

```ts
export type ProgressEntry = { dns_server_id: number; name: string; completed: number }

export type ScanProgressResponse = {
  scan_run: ScanRun
  total_urls: number
  per_dns: ProgressEntry[]
}

export async function fetchScanProgress(): Promise<ScanProgressResponse> { ... }
```

### 3. `useScanProgress` hook (`web/src/routes/results.tsx`)

Owns:
- `progress: ScanProgressResponse | null`
- `chartData: { time: number; value: number }[]` — accumulated over polling lifetime, not reset between polls
- `latestValue: number` — latest total completed count

Behaviour:
- On mount: fetch once to restore persisted state; populate `chartData` with a single seed point
- While `scanning === true`: poll `fetchScanProgress` every 2 seconds, append `{ time: Date.now()/1000, value: totalCompleted }` to `chartData`
- When `scanning` transitions to `false`: fetch once more to capture final state, stop polling
- `chartData` is never reset mid-session so the full climb is visible even after completion

### 4. `ScanProgressSection` component (`web/src/routes/results.tsx`)

Rendered above the filter bar. Hidden only when `progress === null` (no scan ever run).

**Layout:**
```
┌─────────────────────────────────────────────────────┐
│ Scan Progress          [Running... ●] or [Completed ✓]│
│                                                      │
│  [LiveLineChart — total completed URLs over time]    │
│   window=60s, paused when scan not running           │
│                                                      │
│  DNS Server         Completed    Progress            │
│  ─────────────────────────────────────────────────  │
│  Cloudflare DoT     10 / 10      ████████████ ✓      │
│  Google UDP          3 / 10      ███░░░░░░░░░        │
│  TM DNS              0 / 10      ░░░░░░░░░░░░        │
│  ... (scrollable if many servers)                    │
└─────────────────────────────────────────────────────┘
```

**`LiveLineChart` props:**
- `data={chartData}` `value={latestValue}` `window={60}` `paused={!scanning}`
- Single `<LiveLine dataKey="value" pulse={scanning} />`
- `<LiveXAxis />` and `<LiveYAxis />` for axes

**DNS progress table:**
- Rendered as a scrollable `<div>` (max-height ~300px) containing a `<table>`
- One row per DNS server from `progress.per_dns`
- Columns: Name | `completed / total_urls` | inline CSS progress bar | ✓ when complete
- Sorted: completed-first (finished servers float to top), then alphabetical

---

## Installation

```bash
cd web && npx shadcn@latest add @bklit/live-line-chart
```

---

## Error handling

- `fetchScanProgress` returning non-200: swallow silently, keep showing last known state (progress section doesn't error-out the page)
- `total_urls === 0`: hide the progress section (no URLs configured means no scan makes sense)
- Chart with a single data point: renders as a flat line — acceptable

---

## Out of scope

- Per-URL granularity within a DNS server's progress (the gRPC submit sends all results for a server in bulk, so mid-server progress isn't available without a protocol change)
- Screenshot scan progress (only DNS-only scans tracked here)
