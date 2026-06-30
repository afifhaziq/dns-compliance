# Additional Stats Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four stats to the DNS compliance tool: DNS error type badges in scan-results, worst-ISP summary stat, newly-violating domain highlights, and a 30-day compliance trend sparkline on the ISP detail page.

**Architecture:** Task 1 adds a backend `/api/isps/{isp}/trend` endpoint following the existing ISPStats RBAC pattern. Tasks 2–4 are pure frontend: a utility module for error classification, enhancements to `scan-results.tsx` (uses a sessionStorage baseline snapshot taken in `__root.tsx`), and a `LineChart` trend on `isps.$isp.tsx` that calls the new endpoint.

**Tech Stack:** Go 1.26 / GORM / PostgreSQL (backend); React 19 / TypeScript / TanStack Router / `@bklit/line-chart` (frontend)

## Global Constraints

- Module name in `go.mod`: `github.com/afif/dns-tracking`
- `go test ./...` must pass after Task 1 (and stay passing)
- `npm run build` (from `web/`) must compile with no TypeScript errors after Tasks 2, 3, 4
- Never edit `web/src/routeTree.gen.ts` (auto-generated)
- `@` alias resolves to `web/src/` in all TypeScript imports
- HTTP handler errors must NOT leak raw `err.Error()` strings — use safe static messages like `"failed to load trend data"`
- RBAC: admin → global scope; non-admin → department-scoped (same pattern as ISPStats/ISPStatsForDepartment)
- DNS compliance inversion: `compliant: true` means DNS did NOT resolve (block working); `compliant: false` means DNS resolved (violation). `ScanResult.error` is only set when DNS fails (compliant rows).

---

### Task 1: Backend ISP Trend Endpoint

**Files:**
- Modify: `internal/db/models.go` — add `ISPTrendStat` type
- Modify: `internal/db/store.go` — add `ISPTrend` and `ISPTrendForDepartment` to Store interface
- Modify: `internal/db/postgres.go` — implement both methods
- Modify: `internal/server/handlers.go` — add `ISPTrend` handler method
- Modify: `internal/server/router.go` — register GET `/isps/{isp}/trend`
- Modify: `internal/server/handlers_test.go` — add stubs to `fullMockStore`

**Interfaces:**
- Produces: `db.ISPTrendStat` type; `store.ISPTrend(ctx, isp, since, until)` and `store.ISPTrendForDepartment(ctx, isp, since, until, departmentID)`; `GET /api/isps/{isp}/trend?since=<RFC3339>&until=<RFC3339>` returning `[]ISPTrendStat` JSON

- [ ] **Step 1: Add `ISPTrendStat` to `internal/db/models.go`**

Append after the closing brace of `ISPStatsResult` (around line 139):

```go
// ISPTrendStat is one calendar day of aggregated compliance for an ISP,
// used by GET /api/isps/{isp}/trend.
type ISPTrendStat struct {
	Day       string `json:"day"` // YYYY-MM-DD
	Total     int    `json:"total"`
	Compliant int    `json:"compliant"`
}
```

- [ ] **Step 2: Add methods to Store interface in `internal/db/store.go`**

After the `ISPStatsForDepartment` line (around line 34), add:

```go
ISPTrend(ctx context.Context, isp string, since, until time.Time) ([]ISPTrendStat, error)
ISPTrendForDepartment(ctx context.Context, isp string, since, until time.Time, departmentID uint) ([]ISPTrendStat, error)
```

- [ ] **Step 3: Implement `ISPTrend` in `internal/db/postgres.go`**

Append after the closing brace of `ISPStatsForDepartment` (at the end of the file):

```go
func (s *postgresStore) ISPTrend(ctx context.Context, isp string, since, until time.Time) ([]ISPTrendStat, error) {
	var rows []ISPTrendStat
	err := s.db.WithContext(ctx).
		Table("scan_results").
		Select(`TO_CHAR(scan_results.scanned_at, 'YYYY-MM-DD') AS day,
            COUNT(*) AS total,
            SUM(CASE WHEN scan_results.compliant = true THEN 1 ELSE 0 END) AS compliant`).
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Where("dns_servers.isp = ? AND scan_results.scanned_at >= ? AND scan_results.scanned_at <= ?", isp, since, until).
		Group("TO_CHAR(scan_results.scanned_at, 'YYYY-MM-DD')").
		Order("day").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *postgresStore) ISPTrendForDepartment(ctx context.Context, isp string, since, until time.Time, departmentID uint) ([]ISPTrendStat, error) {
	var rows []ISPTrendStat
	err := s.db.WithContext(ctx).
		Table("scan_results").
		Select(`TO_CHAR(scan_results.scanned_at, 'YYYY-MM-DD') AS day,
            COUNT(*) AS total,
            SUM(CASE WHEN scan_results.compliant = true THEN 1 ELSE 0 END) AS compliant`).
		Joins("JOIN dns_servers ON dns_servers.id = scan_results.dns_server_id").
		Joins("JOIN department_urls ON department_urls.url_id = scan_results.url_id AND department_urls.department_id = ? AND department_urls.enabled = true", departmentID).
		Where("dns_servers.isp = ? AND scan_results.scanned_at >= ? AND scan_results.scanned_at <= ?", isp, since, until).
		Group("TO_CHAR(scan_results.scanned_at, 'YYYY-MM-DD')").
		Order("day").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
```

- [ ] **Step 4: Add `ISPTrend` handler to `internal/server/handlers.go`**

Append after the closing brace of `ISPStats` handler (around line 648):

```go
// ISP Trend

func (h *Handlers) ISPTrend(w http.ResponseWriter, r *http.Request) {
	isp, err := url.PathUnescape(chi.URLParam(r, "isp"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ISP")
		return
	}
	now := time.Now()
	since := now.AddDate(0, 0, -30)
	until := now
	if s := r.URL.Query().Get("since"); s != "" {
		if t, err2 := time.Parse(time.RFC3339, s); err2 == nil {
			since = t
		}
	}
	if u := r.URL.Query().Get("until"); u != "" {
		if t, err2 := time.Parse(time.RFC3339, u); err2 == nil {
			until = t
		}
	}
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var stats []db.ISPTrendStat
	if user.IsAdmin {
		stats, err = h.store.ISPTrend(r.Context(), isp, since, until)
	} else {
		if user.DepartmentID == nil {
			writeError(w, http.StatusForbidden, "user has no department")
			return
		}
		stats, err = h.store.ISPTrendForDepartment(r.Context(), isp, since, until, *user.DepartmentID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load trend data")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
```

Note: `err` is declared at top of function via the `url.PathUnescape` call; the subsequent assignments `stats, err = ...` reuse it without `:=`.

- [ ] **Step 5: Register route in `internal/server/router.go`**

Find the `r.Get("/isps/{isp}", h.ISPStats)` line and add immediately below it:

```go
r.Get("/isps/{isp}/trend", h.ISPTrend)
```

- [ ] **Step 6: Add stubs to `internal/server/handlers_test.go`**

Find the two ISPStats stubs (around lines 352–357) and add after them:

```go
func (m *fullMockStore) ISPTrend(_ context.Context, _ string, _, _ time.Time) ([]db.ISPTrendStat, error) {
	return nil, nil
}
func (m *fullMockStore) ISPTrendForDepartment(_ context.Context, _ string, _, _ time.Time, _ uint) ([]db.ISPTrendStat, error) {
	return nil, nil
}
```

- [ ] **Step 7: Verify compilation and tests**

```bash
go build -o /tmp/server-check ./cmd/server/ && go test ./...
```

Expected: all packages pass. The new store methods are satisfied by both the postgres implementation and the mock. Clean up the binary: `rm /tmp/server-check`.

- [ ] **Step 8: Commit**

```bash
git add internal/db/models.go internal/db/store.go internal/db/postgres.go \
        internal/server/handlers.go internal/server/router.go internal/server/handlers_test.go
git commit -m "feat(api,db): ISPTrend endpoint — daily compliance aggregation per ISP"
```

---

### Task 2: Frontend Utilities — DNS Error Classification, ISPTrendStat Type, fetchISPTrend

**Files:**
- Create: `web/src/lib/dns-error.ts`
- Modify: `web/src/api/types.ts` — append `ISPTrendStat`
- Modify: `web/src/api/isps.ts` — add `fetchISPTrend`

**Interfaces:**
- Produces: `classifyDNSError(error: string): DnsErrorType`, `dnsErrorLabel(type: DnsErrorType): string`, exported from `@/lib/dns-error`; `ISPTrendStat` type from `@/api/types`; `fetchISPTrend(isp: string, sinceDays?: number): Promise<ISPTrendStat[]>` from `@/api/isps`

- [ ] **Step 1: Create `web/src/lib/dns-error.ts`**

```typescript
export type DnsErrorType = 'nxdomain' | 'timeout' | 'servfail' | 'other' | 'none'

// classifyDNSError maps raw Go error strings (stored in ScanResult.error) to a
// human-readable category. Compliant rows (DNS failed) have non-empty errors;
// violation rows (DNS resolved) have empty errors → 'none'.
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

- [ ] **Step 2: Append `ISPTrendStat` to `web/src/api/types.ts`**

After the closing brace of `ISPStats` (the last type in the file), append:

```typescript
export type ISPTrendStat = {
  day: string       // YYYY-MM-DD
  total: number
  compliant: number
}
```

- [ ] **Step 3: Add `fetchISPTrend` to `web/src/api/isps.ts`**

The file currently imports `ISPStats` from `./types`. Update the import to include `ISPTrendStat`, then append the function:

```typescript
import { api } from './client'
import type { ISPStats, ISPTrendStat } from './types'

export async function fetchISPStats(isp: string): Promise<ISPStats> {
  return api.get<ISPStats>(`/isps/${encodeURIComponent(isp)}`)
}

export async function fetchISPTrend(isp: string, sinceDays = 30): Promise<ISPTrendStat[]> {
  const since = new Date(Date.now() - sinceDays * 24 * 60 * 60 * 1000).toISOString()
  return api.get<ISPTrendStat[]>(`/isps/${encodeURIComponent(isp)}/trend?since=${encodeURIComponent(since)}`)
}
```

- [ ] **Step 4: Verify TypeScript**

```bash
cd /home/afif/dns-compliance/web && npm run build
```

Expected: builds with no errors.

- [ ] **Step 5: Commit**

```bash
cd /home/afif/dns-compliance
git add web/src/lib/dns-error.ts web/src/api/types.ts web/src/api/isps.ts
git commit -m "feat(web): dns-error classifier util, ISPTrendStat type, fetchISPTrend"
```

---

### Task 3: scan-results.tsx Enhancements (Worst ISP, Newly Violating, Error Type Column)

**Files:**
- Modify: `web/src/routes/__root.tsx` — snapshot baseline before triggering scan
- Modify: `web/src/routes/scan-results.tsx` — worst ISP stat, newly violating highlight, Reason column

**Interfaces:**
- Consumes: `classifyDNSError`, `dnsErrorLabel` from `@/lib/dns-error` (Task 2); `fetchResults` from `@/api/results` (already in codebase)
- `sessionStorage` key format: `scan-baseline-${triggeredAt}` (string, value is `JSON.stringify(ScanResult[])`)

- [ ] **Step 1: Update `handleScanSelected` in `web/src/routes/__root.tsx`**

Add `fetchResults` to the import from `../api/results` (currently only `triggerScan` etc. are imported from scan; `fetchResults` is in `../api/results`). Add the import at the top of the file alongside the existing results imports — or add a new import line:

```typescript
import { fetchResults } from '../api/results'
```

Then replace the `handleScanSelected` callback (currently lines 329–339):

```typescript
const handleScanSelected = useCallback(async (urls: string[]) => {
  if (scanning) return
  try {
    const triggeredAt = new Date().toISOString()
    // Snapshot current results before this scan overwrites latest-per-(url,server).
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

- [ ] **Step 2: Add state + imports to `web/src/routes/scan-results.tsx`**

Add one new import at the top (alongside existing ones). `Link` is already imported from `@tanstack/react-router` — do not add it again:

```typescript
import { classifyDNSError, dnsErrorLabel } from '@/lib/dns-error'
```

Inside `ScanResultsPage`, add the new state variable after the existing state declarations:

```typescript
const [newlyViolatingUrls, setNewlyViolatingUrls] = useState<Set<string>>(new Set())
```

- [ ] **Step 3: Add `worstISP` useMemo to `ScanResultsPage`**

After the existing `ispViolations` useMemo, add:

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

- [ ] **Step 4: Extend the final-fetch effect to compute newly violating**

Replace the existing final-fetch `useEffect` (the one that triggers on `[refreshSignal, scanning, processFreshResults]`):

```typescript
useEffect(() => {
  if (scanning) return
  const finalFetch = async () => {
    try {
      setFetchError(null)
      const all = await fetchResults()
      processFreshResults(all)

      // Compute newly violating: was compliant in baseline, is now a violation.
      const key = `scan-baseline-${triggeredAt}`
      const baselineJson = sessionStorage.getItem(key)
      if (baselineJson) {
        const baseline: ScanResult[] = JSON.parse(baselineJson)
        const baselineMap = new Map<string, boolean>()
        for (const r of baseline) {
          baselineMap.set(`${r.url}:${r.dns_server_id}`, r.compliant)
        }
        const freshForScan = all.filter(
          r => urls.includes(r.url) && new Date(r.scanned_at).getTime() >= triggerTime
        )
        const newViolating = new Set<string>()
        for (const r of freshForScan) {
          if (!r.compliant) {
            const wasCompliant = baselineMap.get(`${r.url}:${r.dns_server_id}`)
            // undefined means no prior result for that (url, server) pair → treat as newly seen
            if (wasCompliant === true || wasCompliant === undefined) {
              newViolating.add(r.url)
            }
          }
        }
        setNewlyViolatingUrls(newViolating)
        sessionStorage.removeItem(key)
      }
    } catch (err) {
      setFetchError(err instanceof Error ? err.message : 'Failed to load results')
    }
  }
  finalFetch()
}, [refreshSignal, scanning, processFreshResults, triggeredAt, urls, triggerTime])
```

- [ ] **Step 5: Update summary section JSX to show worst ISP + newly violating**

The existing summary section in the return JSX (around the `{completedCount > 0 && (...)}` block) currently has three stat boxes. Replace the entire summary section with a version that adds a 4th and 5th stat box:

```tsx
{completedCount > 0 && (
  <div className="dash-section">
    <p className="dash-label">Summary</p>
    <div style={{ display: 'flex', gap: '2rem', flexWrap: 'wrap' }}>
      <div>
        <p className="server-count">{completedCount} / {urls.length}</p>
        <p className="dash-label">Domains scanned</p>
      </div>
      <div>
        <p className="server-count">{totalViolations}</p>
        <p className="dash-label">Total violations</p>
      </div>
      {ispViolations > 0 && (
        <div>
          <p className="server-count">{ispViolations}</p>
          <p className="dash-label">{ispViolations === 1 ? 'ISP' : 'ISPs'} with violations</p>
        </div>
      )}
      {worstISP && (
        <div>
          <Link to="/isps/$isp" params={{ isp: worstISP.isp }} className="server-count" style={{ color: 'var(--color-accent)' }}>
            {worstISP.isp}
          </Link>
          <p className="dash-label">Worst ISP ({worstISP.count} violations)</p>
        </div>
      )}
      {newlyViolatingUrls.size > 0 && (
        <div>
          <p className="server-count label-violation">{newlyViolatingUrls.size}</p>
          <p className="dash-label">Newly violating</p>
        </div>
      )}
    </div>
  </div>
)}
```

- [ ] **Step 6: Update per-domain cards to show "New" badge and Reason column**

Replace the per-domain card section (`{urls.map(url => { ... })}`) with:

```tsx
{urls.map(url => {
  const results = resultsByUrl.get(url)
  const hasResults = results && results.length > 0
  const isNewlyViolating = newlyViolatingUrls.has(url)

  return (
    <div key={url} className="dash-section">
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.75rem' }}>
        <p className="dash-label">{url}</p>
        {isNewlyViolating && (
          <span className="label-violation" style={{ fontSize: '0.7rem', fontWeight: 600 }}>NEW</span>
        )}
        <Link to="/results/$url" params={{ url }} className="text-xs" style={{ color: 'var(--color-accent)' }}>
          Full history →
        </Link>
      </div>

      {!hasResults ? (
        <div className="dash-table-wrap">
          {[1, 2, 3].map(i => (
            <div key={i} className="dash-skeleton-row">
              <span className="skeleton" style={{ width: 140, height: 13 }} />
              <span className="skeleton" style={{ width: 100, height: 13 }} />
              <span className="skeleton" style={{ width: 120, height: 13 }} />
            </div>
          ))}
        </div>
      ) : (
        <Table className="results-table" aria-label={`Scan results for ${url}`}>
          <TableHeader>
            <TableRow>
              <TableHead scope="col">DNS Server</TableHead>
              <TableHead scope="col">Status</TableHead>
              <TableHead scope="col">Resolved IP</TableHead>
              <TableHead scope="col">Reason</TableHead>
              <TableHead scope="col">Evidence</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {results.map(r => {
              const errType = classifyDNSError(r.error)
              const errLabel = dnsErrorLabel(errType)
              return (
                <TableRow key={r.id} className={!r.compliant ? 'violation-row' : ''}>
                  <TableCell><span className="dns-name">{r.dns_server.name}</span></TableCell>
                  <TableCell><StatusDot compliant={r.compliant} /></TableCell>
                  <TableCell>
                    {r.resolved_ip
                      ? <span className="ip-value">{r.resolved_ip}</span>
                      : <span className="empty-cell">—</span>}
                  </TableCell>
                  <TableCell>
                    {errLabel
                      ? <span className="ip-value">{errLabel}</span>
                      : <span className="empty-cell">—</span>}
                  </TableCell>
                  <TableCell>
                    {r.screenshot_url
                      ? <a href={r.screenshot_url} target="_blank" rel="noopener noreferrer" className="screenshot-link">View screenshot</a>
                      : <span className="empty-cell">—</span>}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}
    </div>
  )
})}
```

Note: the old code used `hostname` derived from `new URL(url).hostname` (which always threw because URLs stored are bare hostnames). Removing that variable and using `url` directly is correct.

- [ ] **Step 7: Verify TypeScript**

```bash
cd /home/afif/dns-compliance/web && npm run build
```

Expected: builds with no errors.

- [ ] **Step 8: Commit**

```bash
cd /home/afif/dns-compliance
git add web/src/routes/__root.tsx web/src/routes/scan-results.tsx
git commit -m "feat(web): worst ISP stat, newly violating domains, DNS error type column in scan-results"
```

---

### Task 4: ISP Page Violation Trend Sparkline

**Files:**
- Modify: `web/src/routes/isps.$isp.tsx` — add trend state, parallel fetch, chart section

**Interfaces:**
- Consumes: `fetchISPTrend` from `@/api/isps` (Task 2); `ISPTrendStat` from `@/api/types` (Task 2); `LineChart` from `@/components/charts/line-chart`; `Line` from `@/components/charts/line`

- [ ] **Step 1: Update imports in `web/src/routes/isps.$isp.tsx`**

Replace the existing import block at the top of the file with:

```typescript
import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { ArrowLeftIcon } from 'lucide-react'
import { fetchISPStats, fetchISPTrend } from '@/api/isps'
import type { ISPStats, ISPTrendStat } from '@/api/types'
import { Table, TableBody, TableRow, TableCell, TableHead, TableHeader } from '@/components/ui/table'
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'
```

- [ ] **Step 2: Add `trend` state and update `load` to fetch both in parallel**

Inside `ISPDetailPage`, after the existing state declarations, add:

```typescript
const [trend, setTrend] = useState<ISPTrendStat[]>([])
```

Replace the existing `load` callback:

```typescript
const load = useCallback(async () => {
  setLoading(true)
  try {
    setError(null)
    const [statsData, trendData] = await Promise.all([
      fetchISPStats(isp),
      fetchISPTrend(isp, 30),
    ])
    setStats(statsData)
    setTrend(trendData)
  } catch (err) {
    setError(err instanceof Error ? err.message : 'Failed to load')
  } finally {
    setLoading(false)
  }
}, [isp])
```

- [ ] **Step 3: Add `trendChartData` useMemo**

After the state declarations and before the `useEffect`, add:

```typescript
const trendChartData = useMemo(() =>
  trend.map(s => ({
    date: new Date(s.day),
    compliance: s.total > 0 ? Math.round((s.compliant / s.total) * 100) : 0,
  })),
  [trend]
)
```

- [ ] **Step 4: Add "Compliance Trend" section to the JSX**

In the return block, after the "Most Non-Compliant Domain" section and before the "DNS Servers" section, insert:

```tsx
{/* Compliance Trend */}
{!loading && trendChartData.length >= 2 && (
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

- [ ] **Step 5: Verify TypeScript**

```bash
cd /home/afif/dns-compliance/web && npm run build
```

Expected: builds with no errors.

- [ ] **Step 6: Commit**

```bash
cd /home/afif/dns-compliance
git add web/src/routes/isps.\$isp.tsx
git commit -m "feat(web): 30-day compliance trend sparkline on ISP detail page"
```
