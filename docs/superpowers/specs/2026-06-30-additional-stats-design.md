# Additional Stats Design

**Date:** 2026-06-30
**Branch:** feat/isp-grouping-latency-scan-results (extending before merge)

## Overview

Four new stats features across two pages:

1. **DNS Error Type** — classify raw Go error strings into NXDOMAIN / Timeout / Server Error / Other; display as badge in scan-results table
2. **Worst ISP** — new summary stat in `/scan-results` showing the ISP with the most violations across scanned domains
3. **Newly Violating Domains** — highlight domains that were compliant in the last scan but are now violating; shown in scan-results summary + domain card headers
4. **Violation Trend Sparkline** — 30-day compliance trend on the ISP detail page, using `LineChart` + `Line` from the existing `@bklit/line-chart` infrastructure

## Domain Semantics Reminder

DNS fails = compliant (block is working). DNS resolves = violation (site still up). `ScanResult.error` is only populated on compliant rows (DNS did not resolve).

---

## Feature 1: DNS Error Type Classification

### Utility (`web/src/lib/dns-error.ts`)

Pure string-matching function with no dependencies:

```typescript
export type DnsErrorType = 'nxdomain' | 'timeout' | 'servfail' | 'other' | 'none'

export function classifyDNSError(error: string): DnsErrorType {
  if (!error) return 'none'
  const e = error.toLowerCase()
  if (e.includes('no such host') || e.includes('nxdomain')) return 'nxdomain'
  if (e.includes('timeout') || e.includes('i/o timeout') || e.includes('deadline exceeded')) return 'timeout'
  if (e.includes('server misbehaving') || e.includes('connection refused') || e.includes('servfail')) return 'servfail'
  return 'other'
}

export function dnsErrorLabel(type: DnsErrorType): string {
  switch (type) {
    case 'nxdomain': return 'NXDOMAIN'
    case 'timeout':  return 'Timeout'
    case 'servfail': return 'Server Error'
    case 'other':    return 'Error'
    case 'none':     return ''
  }
}
```

Error string origins (from `internal/pipeline/pipeline.go` line 132: `err.Error()`):
- NXDOMAIN: Go `net.DNSError` → "lookup X on Y: no such host"
- Timeout: context deadline exceeded or "i/o timeout" in the error string
- SERVFAIL: "server misbehaving" from Go's net package
- DoH-specific: "DoH server returned HTTP N", "no A records for X" → classified as 'other'

### Display in `web/src/routes/scan-results.tsx`

Add a **"Reason" column** to each per-domain result table (after the "Resolved IP" column, before "Evidence"):
- Compliant rows with `r.error !== ''`: show `dnsErrorLabel(classifyDNSError(r.error))` as a small badge using existing CSS class `ip-value` (or a new inline style)
- Compliant rows with no error (e.g., empty string): show `—` (empty-cell)
- Violation rows (DNS resolved — `r.error` is empty): show `—`

---

## Feature 2: Worst ISP in Scan-Results Summary

### Location

`web/src/routes/scan-results.tsx` — new 4th stat box in the existing summary section (already has: Domains scanned, Total violations, ISPs with violations).

### Logic

```typescript
const worstISP = useMemo(() => {
  const counts = new Map<string, number>()
  for (const results of resultsByUrl.values()) {
    for (const r of results) {
      if (!r.compliant) counts.set(r.dns_server.isp, (counts.get(r.dns_server.isp) ?? 0) + 1)
    }
  }
  if (counts.size === 0) return null
  let best = { isp: '', count: 0 }
  for (const [isp, count] of counts) {
    if (count > best.count) best = { isp, count }
  }
  return best
}, [resultsByUrl])
```

### Display

Only shown when `worstISP !== null`. The stat box value is a `<Link to="/isps/$isp" params={{ isp: worstISP.isp }}>` with the ISP name; the label is "Worst ISP ({count} violations)".

---

## Feature 3: Newly Violating Domains

### Baseline Capture (`web/src/routes/__root.tsx`)

In `handleScanSelected`, snapshot results **before** triggering the scan:

```typescript
const handleScanSelected = useCallback(async (urls: string[]) => {
  if (scanning) return
  try {
    const triggeredAt = new Date().toISOString()
    // Snapshot current state before scan overwrites latest results
    const baseline = await fetchResults()
    sessionStorage.setItem(`scan-baseline-${triggeredAt}`, JSON.stringify(baseline))
    await triggerScan(urls)
    setScanning(true)
    startPolling()
    navigate({ to: '/scan-results', search: { urls, triggeredAt } })
  } catch (err) {
    console.error('Targeted scan trigger failed:', err)
  }
}, [scanning, startPolling, navigate])
```

The key includes `triggeredAt` so concurrent scans don't collide. Keys are cleaned up after use (see scan-results page).

### Comparison (`web/src/routes/scan-results.tsx`)

Once `scanning` is false and final results are available, read baseline from sessionStorage and compute `newlyViolatingUrls: Set<string>`:

```typescript
// A domain is "newly violating" if:
// - It has at least one current violation (compliant: false)
// - ALL its baseline results for those same (dns_server_id) pairs were compliant
```

Cleanup: `sessionStorage.removeItem(key)` after reading.

### Display

- Summary stat box: "X newly violating" (only shown when count > 0, styled with `label-violation` color)
- Domain card header: "New" badge (small inline tag, label-violation color) next to the domain name when that domain is in `newlyViolatingUrls`

---

## Feature 4: ISP Violation Trend

### Backend: New Endpoint

`GET /api/isps/{isp}/trend?since=<RFC3339>&until=<RFC3339>`

Default: `since` = 30 days ago, `until` = now.

**New model** (`internal/db/models.go`):
```go
type ISPTrendStat struct {
    Day       string `json:"day"` // YYYY-MM-DD
    Total     int    `json:"total"`
    Compliant int    `json:"compliant"`
}
```

**Store interface** (`internal/db/store.go`):
```go
ISPTrend(ctx context.Context, isp string, since, until time.Time) ([]ISPTrendStat, error)
ISPTrendForDepartment(ctx context.Context, isp string, since, until time.Time, departmentID uint) ([]ISPTrendStat, error)
```

**Implementation** (`internal/db/postgres.go`):

`ISPTrend` — all scans for this ISP in the date range, grouped by calendar day:
```sql
SELECT TO_CHAR(scanned_at, 'YYYY-MM-DD') AS day,
       COUNT(*) AS total,
       SUM(CASE WHEN compliant = true THEN 1 ELSE 0 END) AS compliant
FROM scan_results
JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id
WHERE dns_servers.isp = ?
  AND scan_results.scanned_at >= ?
  AND scan_results.scanned_at <= ?
GROUP BY TO_CHAR(scanned_at, 'YYYY-MM-DD')
ORDER BY day
```

`ISPTrendForDepartment` — adds a JOIN to filter to the department's enabled watchlist:
```sql
... JOIN department_urls ON department_urls.url_id = scan_results.url_id
         AND department_urls.department_id = ?
         AND department_urls.enabled = true
```

**Handler** (`internal/server/handlers.go`): `h.ISPTrend` — parses `since`/`until` query params (RFC3339, defaults to 30 days ago / now), reads ISP from path, branches on `user.IsAdmin` to call `ISPTrend` or `ISPTrendForDepartment`. Safe error messages (no `err.Error()` leakage).

**Router** (`internal/server/router.go`): `r.Get("/isps/{isp}/trend", h.ISPTrend)` inside the `requireAuth` group.

**Test stub** (`internal/server/handlers_test.go`):
```go
func (m *fullMockStore) ISPTrend(_ context.Context, _ string, _, _ time.Time) ([]db.ISPTrendStat, error) {
    return nil, nil
}
func (m *fullMockStore) ISPTrendForDepartment(_ context.Context, _ string, _, _ time.Time, _ uint) ([]db.ISPTrendStat, error) {
    return nil, nil
}
```

### Frontend: Types + API

**`web/src/api/types.ts`** — add:
```typescript
export type ISPTrendStat = {
  day: string   // YYYY-MM-DD
  total: number
  compliant: number
}
```

**`web/src/api/isps.ts`** — add:
```typescript
export async function fetchISPTrend(isp: string, sinceDays = 30): Promise<ISPTrendStat[]> {
  const since = new Date(Date.now() - sinceDays * 24 * 60 * 60 * 1000).toISOString()
  return api.get<ISPTrendStat[]>(`/isps/${encodeURIComponent(isp)}/trend?since=${encodeURIComponent(since)}`)
}
```

### Frontend: ISP Page Sparkline (`web/src/routes/isps.$isp.tsx`)

Fetch trend data in parallel with ISP stats using `Promise.all`. Transform for `LineChart`:

```typescript
const trendChartData = trendStats.map(s => ({
  date: new Date(s.day),
  compliance: s.total > 0 ? Math.round((s.compliant / s.total) * 100) : 0,
}))
```

Render a new "Compliance Trend (last 30 days)" section between "Most Non-Compliant Domain" and "DNS Servers":

```tsx
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'

{trendChartData.length >= 2 && (
  <div className="dash-section">
    <p className="dash-label">Compliance Trend (last 30 days)</p>
    <LineChart
      data={trendChartData}
      xDataKey="date"
      aspectRatio="4 / 1"
      margin={{ top: 8, right: 8, bottom: 24, left: 32 }}
    >
      <Line dataKey="compliance" stroke="var(--color-accent)" />
    </LineChart>
  </div>
)}
```

Hidden when fewer than 2 data points (no meaningful trend to show). No loading skeleton needed — chart renders progressively.

---

## Implementation Tasks

| # | Task | Files | Type |
|---|------|-------|------|
| 1 | Backend ISP trend endpoint | `models.go`, `store.go`, `postgres.go`, `handlers.go`, `router.go`, `handlers_test.go` | Go |
| 2 | Frontend utils + types | `dns-error.ts` (new), `types.ts`, `isps.ts` | TS |
| 3 | scan-results.tsx enhancements | `__root.tsx`, `scan-results.tsx` | TSX |
| 4 | ISP page sparkline | `isps.$isp.tsx` | TSX |

Tasks 2, 3, 4 are frontend-only and depend on Task 1 for the trend API. Task 2 has no backend dependency.

## Verification

- `go test ./...` must pass after Task 1
- `npm run build` from `web/` must compile with no TypeScript errors after each task
