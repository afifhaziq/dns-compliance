# Per-URL Scan History — Design

## Problem

The Overview page's "Compliance Results" table only shows the latest result per (URL, DNS server) pair. There's no way to see how a URL's compliance status has changed across past scan runs over time. Auditors need this to spot trends (e.g. a server that recently started/stopped enforcing a takedown) and to back up a report with historical evidence, not just a snapshot.

## Goals

- View a single URL's compliance history across all DNS servers, over a recent time window.
- Both a visual trend (chart) and exact per-scan detail (table), since auditors need both "what's the trend" and "what exactly happened, with evidence links."
- Don't block on scan-frequency assumptions — today's default is hourly scans, but that may change, and the design shouldn't need rework if it does.

## Non-goals (out of scope for this spec)

- User-adjustable lookback window. Fixed at 7 days for v1. The backend accepts an optional `?since=` param, so adding a frontend selector later is a frontend-only change.
- Per-DNS chart legend/line-toggle UI beyond whatever `@bklit/line-chart` provides by default.
- Any change to scan scheduling/frequency itself.

## Backend change

`GET /api/results/*url` (existing endpoint, `internal/server/handlers.go` `ResultsByURL`) gains an optional `?since=<RFC3339>` query param.

- `db.Store.ResultsByURL(ctx, urlValue string, since time.Time) ([]ScanResult, error)` — add `since` parameter; implementation (`internal/db/postgres.go`) adds `.Where("scanned_at >= ?", since)` to the existing query (which already does `Where("url_value = ?")`, `Preload("DNSServer")`, `Order("scanned_at desc")`).
- Handler parses `?since=`; if absent or unparseable, defaults to `time.Now().AddDate(0, 0, -7)`. Existing callers (none currently pass `since`) get the same 7-day-capped behavior — this is additive, not breaking.
- No new endpoint, no migration, no new table.

## Frontend changes

### New route: `web/src/routes/results.$url.tsx`

- Dynamic segment is `encodeURIComponent(url)` — a single path segment, avoiding slash-in-path issues since raw URLs contain `https://...`. Mirrors the backend's `*url` wildcard, which accepts the raw (decoded) value.
- Fetches `GET /api/results/${encodeURIComponent(url)}` (7-day default window, no explicit `since` sent by the frontend for v1).
- **Header:** hostname (parsed from the URL) as the page title, raw URL as subtitle, with a way back to Overview (standard page-header pattern already used elsewhere).
- **Chart:** new `@bklit/line-chart` dependency (npm; sits alongside the existing `@visx/*` packages already in `web/package.json`, which it's built on). One line per DNS server; x-axis = `scanned_at`, y-axis = compliant(1)/violation(0). Plot raw per-scan points (no daily bucketing — see "Why raw + decimation" below). When point count exceeds `maxRenderPointsForWidth(innerWidth)` (`web/src/components/charts/decimate-time-series.ts`), run `decimateTimeSeries` (existing LTTB implementation, already used by the live scan chart) before rendering.
- **Filter bar:** reuse the existing Status (`ToggleGroup`: All/Violations/Compliant) + DNS Server (`<select>`) pattern from the Overview table, scoped to this URL's results. Filters both the chart series and the table.
- **Table:** flat list of `ScanResult` rows for this URL within the window, newest first (matches backend ordering). Columns: DNS Server, Status, Resolved IP, Evidence (screenshot link), Scanned At — **absolute** timestamp (not relative "X minutes ago," which loses meaning over a multi-day view).
- Loading/empty/error states follow existing patterns (skeleton rows, `EmptyIcon`, retry button on error).

### Why raw per-scan data + decimation, not daily bucketing

Bucketing by day bakes in an assumption about scan frequency. If the scan interval changes (more or less frequent than hourly), a fixed daily bucket either discards almost everything (high frequency) or is needlessly lossy relative to actual data density (low frequency). Raw-status-plus-decimation adapts automatically: sparse data passes through unchanged; dense data gets downsampled to a fixed visual budget via LTTB, which preserves brief status flips (sharp value changes survive LTTB's "largest triangle" selection) better than naive uniform sampling would. Full per-scan detail remains available in the table regardless of what the chart decimates.

### Entry point: Overview table (`web/src/routes/index.tsx`)

`URLGroupRow` gains a small icon button (e.g. a `lucide-react` icon, consistent with existing icon usage elsewhere) linking via TanStack `Link` to `/results/${encodeURIComponent(group.url)}`. Uses `onClick={e => e.stopPropagation()}` so it doesn't also trigger the row's existing expand/collapse toggle.

Unchanged: clicking the row/chevron still toggles inline per-DNS sub-rows (today's quick-peek behavior); the hostname's hover preview-card (external link, opens in a new tab) is untouched.

## Testing

- Backend: extend `internal/server/handlers_test.go` (`fullMockStore` pattern) to cover `ResultsByURL` with and without `?since=`, and a unit test in `internal/db/` confirming the date filter is applied.
- Frontend: no existing test suite for routes (this codebase currently has no frontend test runner configured); verify manually via dev server — chart renders, filter bar narrows both chart and table, navigation from Overview works, empty/error states render correctly for a URL with no history in the window.

## Open implementation detail

`@bklit/line-chart`'s exact install method (npm package vs. shadcn-registry copy-paste) and prop shape weren't fully confirmed during design (its docs site didn't expose a full code example). Resolve this at implementation time by checking `https://bklit.com/charts/line-chart` and its docs directly before wiring up the chart.
