# Per-DNS Compliance Heatmap — Design

## Problem

`results.$url.tsx` currently renders a single `HeatmapChart` wired to `generateSyntheticHeatmapData()` — 12 weeks of random numbers, not real compliance data. There's no calendar-year view of a domain's compliance history, and no way to see DNS-server-specific patterns (a server that's mostly clean but had a bad week, vs. one that's been failing all year).

## Goals

- One heatmap per configured DNS server, each showing a full calendar year (Jan 1 – Dec 31) of this domain's results against that server.
- Year navigation (`< 2026 >`) since scanning history will eventually span multiple years.
- Each day-cell's color encodes that day's outcome for that (domain, DNS server) pair: any violation that day puts the cell on the red scale, graded by what share of that day's scans failed; zero violations is a solid "compliant" cell; no scans that day is neutral/empty.
- A compliance-rate summary (e.g. "94% compliant") next to each heatmap's legend, for the year currently shown.
- Stay inside the existing design system: gray = compliant, red = violation, no new hues (DESIGN.md's one-accent rule).

## Non-goals (out of scope for this spec)

- Changing the existing 7-day results table/filters/chart-toggle on this page — untouched, keeps its own 7-day fetch.
- Smooth/continuous color gradient. The underlying `HeatmapCells`/`heatmap-colors.ts` primitive is hard-wired to 5 discrete levels (`getHeatmapContributionLevel`); this spec buckets into those 5 levels rather than reworking the chart primitive.
- Per-cell click-through to the underlying scan rows. Hover tooltip only.
- Caching/persisting the selected year across navigation away from the page.

## Backend change

`GET /api/results/*url` (`internal/server/handlers.go` `ResultsByURL`) gains an optional `?until=<RFC3339>` query param, mirroring the existing `?since=`.

- `db.Store.ResultsByURL(ctx, urlValue string, since, until time.Time) ([]ScanResult, error)` — add `until` parameter; `internal/db/postgres.go` adds `.Where("scanned_at <= ?", until)` alongside the existing `since` filter.
- Handler parses `?until=`; if absent or unparseable, defaults to `time.Now()` (today), preserving current behavior for the existing 7-day caller, which never sends it.
- Existing tests in `internal/server/handlers_test.go` (`fullMockStore`) and `internal/db/` get a case covering `until`.

## Frontend changes

### New API function: `web/src/api/results.ts`

```ts
export async function fetchResultsByUrlAndYear(url: string, year: number): Promise<ScanResult[]> {
  const since = new Date(year, 0, 1).toISOString()
  const until = new Date(year, 11, 31, 23, 59, 59, 999).toISOString()
  const res = await fetch(`/api/results/${encodeURIComponent(url)}?since=${encodeURIComponent(since)}&until=${encodeURIComponent(until)}`)
  if (!res.ok) throw new Error(`Failed to load results: ${res.status}`)
  return res.json()
}
```

This is a **second, independent fetch** from the page's existing 7-day `fetchResultsByUrl` call. The existing table/filter/7-day state in `results.$url.tsx` is untouched; a new `yearResults`/`selectedYear`/`yearLoading` state trio drives only the heatmaps, refetching when `selectedYear` changes.

### Per-day bucketing (`web/src/routes/results.$url.tsx`, or a small new helper module alongside it)

For a given DNS server's results within the selected year, group by local calendar day, then bucket each day into the level the chart primitive expects:

```ts
function dayLevel(dayResults: ScanResult[]): number {
  if (dayResults.length === 0) return 0          // no scans — no-data
  const violations = dayResults.filter(r => !r.compliant).length
  if (violations === 0) return 1                  // 100% compliant
  const rate = violations / dayResults.length
  if (rate <= 1 / 3) return 2                     // light red
  if (rate <= 2 / 3) return 3                     // medium red
  return 4                                        // deep red
}
```

This integer becomes `bin.count` for that day's `HeatmapBin`. Because `getHeatmapContributionLevel` maps `0→0, 1→1, 2→2, 3→3, ≥4→4`, passing these pre-bucketed integers lands exactly on the level we intend — no change needed to the chart library itself.

A `buildYearHeatmapColumns(year, dayLevelByDate)` helper builds the full Jan 1–Dec 31 `HeatmapColumn[]` grid (53 week-columns × 7 day-rows, Sunday-first to match `HeatmapYAxis`'s `Mon/Wed/Fri` row assumptions), filling in every calendar day of the year — including days with no scan data (level 0) — so the grid shape is always complete regardless of how much history exists.

### Color mapping (`web/src/routes/results.$url.tsx` — local `levelColors` constant)

```ts
const HEATMAP_LEVEL_COLORS = [
  'var(--stone-panel)',                                              // 0: no data
  'var(--compliant)',                                                // 1: compliant
  'color-mix(in oklch, var(--violation) 33%, var(--violation-subtle))', // 2: light red
  'color-mix(in oklch, var(--violation) 66%, var(--violation-subtle))', // 3: medium red
  'var(--violation)',                                                // 4: deep red
] as const
```

Passed as `levelColors` to each `HeatmapChart`. No new CSS tokens — reuses the existing `--compliant`/`--violation`/`--violation-subtle`/`--stone-panel` tokens already defined in `index.css`, so light/dark mode are handled automatically.

### Legend & tooltip

- `HeatmapLegend` gets `lessLabel="Compliant"`, `moreLabel="More violations"`, and the same `levelColors` (via `colorScale` built from them) so swatches match the cells.
- Next to each legend, a small text badge: `{overallRate}% compliant` — computed as `(scans with 0 violations that day, weighted by... )`. Simpler and matches "compliance rate for the respective domain": `compliantScans / totalScans` across that DNS server's results in the selected year (per-scan rate, not per-day — avoids double meaning vs. the per-day cell logic).
- `HeatmapTooltip` gets a custom `formatLabel={(count, date) => ...}` that looks up that day's actual `{compliant, total}` (not the bucketed level) and renders e.g. `"3 of 10 compliant · 12 Mar 2026"`, or `"No scans · 5 Jan 2026"` when `total === 0`.

### Page layout (`web/src/routes/results.$url.tsx`)

Replace the single synthetic-data `HeatmapChart` block with:

1. A year-nav row (`< 2026 >`) shared above all heatmaps, since every heatmap on the page shows the same year. Disable the `>` button when `selectedYear === currentYear` (no future years).
2. For each DNS server in `dnsServers` (already computed on this page from the 7-day results — needs to instead come from the year fetch, or from a lightweight separate source, since a DNS server might have results in the year window but not the trailing 7 days, or vice versa): a `.dash-section` block with a `.dash-label`-style heading (the server name) + compliant-% badge, then that server's `HeatmapChart`.
3. Loading state: existing `HeatmapChartLoading`/`status="loading"` chart prop (already supported by `HeatmapChart`) while `yearLoading` is true. Empty state (server has zero results in the selected year): render the empty grid (all level-0) rather than hiding the chart, so the calendar shape and month labels stay visible.

Everything below (filter bar, 7-day table) is unchanged.

## Testing

- Backend: extend `internal/server/handlers_test.go` and the `internal/db/` date-filter test to cover `?until=`.
- Frontend: no test runner configured for routes (consistent with the existing per-URL history page). Verify manually: heatmap renders real per-day colors for a domain/DNS pair with mixed compliant/violation history, year nav switches data, hover tooltip shows correct day counts, legend badge percentage matches manual calculation, light and dark mode both read correctly (red/gray contrast, no color outside the existing palette).

## Open implementation detail

`dnsServers` is currently derived from the 7-day `results` state (`results.$url.tsx:134-138`). Once heatmaps need a year's worth of DNS servers, decide at implementation time whether to derive the heatmap section list from `yearResults` instead (a server could appear in one window but not the other) — likely: union of both, so a server with only old history still gets a heatmap when its year is selected.