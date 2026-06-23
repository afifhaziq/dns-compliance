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
