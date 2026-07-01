import type { HeatmapBin, HeatmapColumn } from '@/components/charts/heatmap/heatmap-context'
import type { HeatmapLevelColors } from '@/components/charts/heatmap/heatmap-colors'
import type { DailyComplianceStat } from '@/api/types'

// Reuses the app's existing compliant/violation design tokens — no new hues.
// Index 0 = no scans that day, 1 = fully compliant, 2-4 = increasing violation severity.
// Mirrors db.DailyComplianceLevel (internal/db/models.go) — the level itself
// is computed server-side; this is only the color mapping for that level.
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

// Builds a full Jan 1-Dec 31 grid (Sunday-first weeks, padded at both ends
// to whole weeks) regardless of how much scan history actually exists.
export function buildYearHeatmapColumns(year: number, statsByDay: Map<string, DailyComplianceStat>): HeatmapColumn[] {
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
      const stat = inYear ? statsByDay.get(dateKey(cellDate)) : undefined
      bins.push({ bin: d, date: displayDate, count: stat?.level ?? 0 })
    }
    columns.push({ bin: columnIndex, bins })
    columnIndex++
  }
  return columns
}

export function compliancePercentFromStats(stats: DailyComplianceStat[]): number | null {
  let total = 0
  let compliant = 0
  for (const s of stats) {
    total += s.total
    compliant += s.compliant
  }
  if (total === 0) return null
  return Math.round((compliant / total) * 100)
}

// Mirrors db.DailyComplianceLevel (internal/db/models.go) — buckets a day's
// results onto the heatmap's 5-level scale from raw total/compliant counts.
function dailyComplianceLevel(total: number, compliant: number): number {
  if (total === 0) return 0
  const violations = total - compliant
  if (violations === 0) return 1
  const rate = violations / total
  if (rate <= 1 / 3) return 2
  if (rate <= 2 / 3) return 3
  return 4
}

// Combines per-DNS-server DailyComplianceStat rows into one row per day —
// used for the "Overall" heatmap view, which represents compliance across
// every configured server rather than one server at a time.
export function aggregateStatsByDay(stats: DailyComplianceStat[]): Map<string, DailyComplianceStat> {
  const totals = new Map<string, { total: number; compliant: number }>()
  for (const s of stats) {
    const cur = totals.get(s.day) ?? { total: 0, compliant: 0 }
    cur.total += s.total
    cur.compliant += s.compliant
    totals.set(s.day, cur)
  }

  const result = new Map<string, DailyComplianceStat>()
  for (const [day, { total, compliant }] of totals) {
    result.set(day, {
      dns_server_id: 0,
      dns_server_name: 'Overall',
      day,
      total,
      compliant,
      level: dailyComplianceLevel(total, compliant),
    })
  }
  return result
}
