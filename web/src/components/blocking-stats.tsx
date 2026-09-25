import { useEffect, useMemo, useRef, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { curveCatmullRom } from '@visx/curve'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { BarChart } from '@/components/charts/bar-chart'
import { Bar } from '@/components/charts/bar'
import { BarXAxis } from '@/components/charts/bar-x-axis'
import { BarYAxis } from '@/components/charts/bar-y-axis'
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'
import { Background } from '@/components/charts/background'
import { PieChart } from '@/components/charts/pie-chart'
import { PieSlice } from '@/components/charts/pie-slice'
import { PieCenter } from '@/components/charts/pie-center'
import { SunburstChart } from '@/components/charts/sunburst-chart'
import { SunburstSegment } from '@/components/charts/sunburst-segment'
import { SunburstCenter } from '@/components/charts/sunburst-center'
import { SunburstLabels } from '@/components/charts/sunburst-labels'
import { buildArcs, type ArcDatum } from '@/components/charts/sunburst'
import type { SunburstNode } from '@/components/charts/sunburst-data'
import { Legend, LegendItem, LegendMarker, LegendLabel, LegendValue, type LegendItemData } from '@/components/charts/legend'
import { Grid } from '@/components/charts/grid'
import { ChartTooltip, TooltipBox, TooltipContent } from '@/components/charts/tooltip'
import { fetchBlockingStats, type BlockingStatRow } from '../api/blocking-stats'

// Mirrors the source workbook's scope (2022 onward, MCMC vs everyone else).
const FIRST_YEAR = 2022
const MCMC = 'MCMC'
// The MCMC workbook only ever classifies offences under these five categories —
// anything else on an MCMC-owned case is stray/legacy data, not a sixth category.
const MCMC_CATEGORIES = ['Lucah', 'Sumbang', 'Palsu', 'Jelik', 'Mengancam']
const isMcmcCategory = (offence: string) => MCMC_CATEGORIES.some(c => c.toLowerCase() === offence.toLowerCase())
const fmt = (n: number) => n.toLocaleString()
const pct = (n: number, total: number) => (total ? `${((n / total) * 100).toFixed(2)}%` : '—')

type Line = { agency: string; offence: string; byYear: Record<number, number>; total: number }

// Sum rows into one line per (agency, offence), each with per-year counts.
function toLines(rows: BlockingStatRow[]): Line[] {
  const m = new Map<string, Line>()
  for (const r of rows) {
    const k = `${r.agency}\u0000${r.offence}`
    const l = m.get(k) ?? { agency: r.agency, offence: r.offence, byYear: {}, total: 0 }
    l.byYear[r.year] = (l.byYear[r.year] ?? 0) + r.count
    l.total += r.count
    m.set(k, l)
  }
  return [...m.values()].sort((a, b) => a.agency.localeCompare(b.agency) || b.total - a.total)
}

// Count + inline proportional bar, as two fixed-width cells so every table lines up.
function BarCells({ value, max }: { value: number; max: number }) {
  return (
    <>
      <TableCell className="text-right tabular-nums">{fmt(value)}</TableCell>
      <TableCell>
        <div className="h-2 rounded-sm" style={{ background: 'color-mix(in srgb, var(--ink) 12%, transparent)' }}>
          <div className="h-full rounded-sm" style={{ background: SHADES[1], width: `${max ? (value / max) * 100 : 0}%` }} />
        </div>
      </TableCell>
    </>
  )
}

// Single-hue --ink-scale-N ramp (index.css) — follows light/dark via the variables.
const SHADES = [1, 2, 3, 4, 5, 6].map(n => `var(--ink-scale-${n})`)
const shade = (i: number) => SHADES[Math.min(i, SHADES.length - 1)]
// Colour for item i of n, interpolated along the 6-step ramp so every item gets
// a distinct shade (the ramp alone runs out at 6).
const ramp = (i: number, n: number) => {
  const t = n > 1 ? (i / (n - 1)) * (SHADES.length - 1) : 0
  const lo = Math.floor(t)
  const hi = Math.min(lo + 1, SHADES.length - 1)
  return `color-mix(in srgb, ${SHADES[lo]}, ${SHADES[hi]} ${Math.round((t - lo) * 100)}%)`
}

// Keep the top n by total, fold the rest into "Other" — but only when that
// actually collapses 2+ items; folding a single leftover just relabels it.
function topN<T extends { label: string; value: number }>(items: T[], n: number): { label: string; value: number }[] {
  const sorted = [...items].sort((a, b) => b.value - a.value)
  if (sorted.length <= n + 1) return sorted
  const head = sorted.slice(0, n)
  const rest = sorted.slice(n).reduce((s, x) => s + x.value, 0)
  return [...head, { label: 'Other', value: rest }]
}

function Card({ title, children, className = '' }: { title: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={`bento-card ${className}`}>
      <h3 className="text-sm font-semibold mb-3">{title}</h3>
      {children}
    </div>
  )
}

function Kpi({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="server-count" style={{ color: 'var(--ink)' }}>{value}</p>
      <p className="dash-label">{label}</p>
    </div>
  )
}

// Charts keep a minimum width and scroll inside their card on narrow screens.
function ChartScroll({ children }: { children: React.ReactNode }) {
  return <div className="overflow-x-auto"><div className="min-w-[520px]">{children}</div></div>
}

// Horizontal bars, one per label, sorted by value.
function HBar({ items, left = 160, max = 8, wide }: { items: { label: string; value: number }[]; left?: number; max?: number; wide?: boolean }) {
  const data = topN(items, max - 1).map(i => ({ name: i.label, value: i.value }))
  return (
    <ChartScroll><BarChart
      data={data}
      orientation="horizontal"
      aspectRatio={`${wide ? 1300 : 640} / ${data.length * 34 + 30}`}
      margin={{ top: 10, right: 30, bottom: 20, left }}
    >
      <Grid horizontal={false} vertical />
      <Bar dataKey="value" fill={SHADES[0]} />
      <BarYAxis maxLabels={data.length} />
      <ChartTooltip />
    </BarChart></ChartScroll>
  )
}

// Donut with a swatch legend beside it.
function Donut({ items, label }: { items: { label: string; value: number }[]; label: string }) {
  const data = topN(items, 5).map((d, i) => ({ ...d, color: shade(i) }))
  const total = data.reduce((s, d) => s + d.value, 0)
  return (
    <div className="flex flex-1 flex-col items-center gap-4">
      {/* size/hoverOffset grown together (+14 each way) vs the plain 176/10 pair so the
          ring's own radius (176/2 - 10 = 78) is unchanged — outerRadius = size/2 - hoverOffset
          stays 78 — while the box gets extra margin for the hover glow's 12px blur, which
          the default 10px hoverOffset margin alone was too tight for and got clipped by the
          svg's own edge. */}
      <PieChart data={data} size={204} innerRadius={56} padAngle={0.02} hoverOffset={24}>
        {/* hoverEffect="none": the default "translate" pop-out shifts a hovered slice's
            whole path by one fixed vector — fine for a modest wedge, but with one
            category near 100% share it shears the near-full-circle arc and shows up
            as a seam/detached fragment where the minor slices are squeezed together. */}
        {data.map((_, i) => <PieSlice key={i} index={i} hoverEffect="none" />)}
        <PieCenter defaultLabel={label} />
      </PieChart>
      <ul className="text-sm space-y-1">
        {data.map(d => (
          <li key={d.label} className="flex items-center gap-2">
            <span className="inline-block size-3 rounded-sm" style={{ background: d.color }} />
            <span>{d.label}</span>
            <span className="tabular-nums opacity-70">{fmt(d.value)} · {pct(d.value, total)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

// Returns the direct children of the focused node as legend items (value +
// share of that node's total) plus their arcIndex list, for wiring the
// Legend's hover state to the chart's. Not exported by @bklit/sunburst-chart
// itself (its docs demo defines the equivalent inline) — buildArcs() already
// gives us everything it needs: filter arcs whose parent is the current focus.
function legendForFocus(arcs: ArcDatum[], focusId: string): { items: LegendItemData[]; arcIndices: number[] } {
  const children = arcs.filter(a => a.parentId === focusId)
  // LegendValue's percentage is value/maxValue — "relative to the largest item
  // here" (a mini progress bar), not "share of the whole" — that's the
  // library's own semantics (see its demo: 198/145/95 → 100%/73%/48%, which
  // only makes sense against the top item, not summing to 100).
  const maxValue = Math.max(0, ...children.map(a => a.value))
  return {
    items: children.map(a => ({ label: a.name, value: a.value, maxValue, color: a.color ?? shade(a.categoryIndex) })),
    arcIndices: children.map(a => a.arcIndex),
  }
}

// Agency -> offence drill-down sunburst (real @bklit/sunburst-chart, already
// vendored alongside the other chart primitives) — inner ring is agency,
// outer ring is that agency's offence breakdown, arc size is total blocked.
// Legend tracks whatever ring is currently focused (root = agencies; click a
// segment to drill into that agency's offences, click center to zoom out).
// Library default is a 1.1s tween per segment plus 0.08s/segment stagger; this is ~2.5x quicker.
const SUNBURST_ENTER = { type: "tween", duration: 0.45, ease: [0.22, 1, 0.36, 1] } as const

function AgencySunburst({ rows, label }: { rows: { agency: string; offence: string; total: number }[]; label: string }) {
  const data: SunburstNode = useMemo(() => {
    const byAgency = new Map<string, Map<string, number>>()
    for (const r of rows) {
      const m = byAgency.get(r.agency) ?? new Map<string, number>()
      m.set(r.offence, (m.get(r.offence) ?? 0) + r.total)
      byAgency.set(r.agency, m)
    }
    const agencies = [...byAgency]
      .map(([agency, offences]) => ({ agency, offences, total: [...offences.values()].reduce((s, v) => s + v, 0) }))
      .sort((a, b) => b.total - a.total)
    // Arc angles are log1p-scaled (weight) so tiny agencies stay visible and
    // clickable; value stays the real count for the tooltip/legend/centre.
    return {
      name: label,
      children: agencies.map(({ agency, offences, total }, i) => {
        const color = ramp(i, agencies.length)
        return {
          name: agency,
          color,
          weight: Math.log1p(total),
          children: [...offences].map(([name, value]) => ({ name, value, weight: Math.log1p(value), color })),
        }
      }),
    }
  }, [rows, label])
  const { arcs, total } = useMemo(() => buildArcs(data), [data])
  const [focusId, setFocusId] = useState(data.name)
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const [pointer, setPointer] = useState<{ x: number; y: number; width: number; height: number } | null>(null)

  const { items, arcIndices } = legendForFocus(arcs, focusId)
  const legendHoveredIndex = hoveredIndex != null ? arcIndices.indexOf(hoveredIndex) : null
  // Legend only makes sense once the user has drilled into an agency — at the
  // root it would just repeat what the chart's own agency ring already shows.
  const showLegend = focusId !== data.name

  const hoveredArc = hoveredIndex != null ? arcs[hoveredIndex] : null
  const hoveredParent = hoveredArc?.parentId ? arcs.find(a => a.id === hoveredArc.parentId) : undefined
  // Ring 1 = agency (share of the grand total); ring 2 = offence (share of its agency).
  const hoveredShareOf = hoveredArc && hoveredArc.depth === 1 ? total : (hoveredParent?.value ?? hoveredArc?.value ?? 0)

  return (
    <div className="flex flex-1 flex-col items-center gap-4">
      <div
        className="relative"
        ref={boxRef}
        onPointerMove={e => {
          const rect = boxRef.current?.getBoundingClientRect()
          if (rect) setPointer({ x: e.clientX - rect.left, y: e.clientY - rect.top, width: rect.width, height: rect.height })
        }}
        onPointerLeave={() => setPointer(null)}
      >
        <SunburstChart data={data} size={260} enterTransition={SUNBURST_ENTER} enterStaggerScale={0.4} focusId={focusId} onFocusChange={setFocusId} hoveredIndex={hoveredIndex} onHoverChange={setHoveredIndex}>
          {arcs.map(arc => <SunburstSegment index={arc.arcIndex} key={arc.id} />)}
          <SunburstLabels onlyDepth={1} minArcLength={12} fontSize={10} fill="light-dark(#000, #fff)" strokeWidth={0} />
          <SunburstCenter />
        </SunburstChart>
        {hoveredArc && pointer && (
          <TooltipBox
            containerHeight={pointer.height}
            containerRef={boxRef}
            containerWidth={pointer.width}
            visible
            x={pointer.x}
            y={pointer.y}
          >
            <TooltipContent
              rows={[{
                color: hoveredArc.color ?? shade(hoveredArc.categoryIndex),
                label: hoveredArc.depth === 1 ? 'Agency' : 'Offence',
                value: `${fmt(hoveredArc.value)} · ${pct(hoveredArc.value, hoveredShareOf)}`,
              }]}
              title={hoveredArc.depth === 1 ? hoveredArc.name : `${hoveredParent?.name ?? ''} · ${hoveredArc.name}`}
            />
          </TooltipBox>
        )}
      </div>
      <AnimatePresence>
        {showLegend && (
          <motion.div
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -4 }}
            initial={{ opacity: 0, y: -4 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
          >
            <Legend
              items={items}
              hoveredIndex={legendHoveredIndex}
              onHoverChange={i => setHoveredIndex(i == null ? null : (arcIndices[i] ?? null))}
              className="flex-row flex-wrap justify-center gap-x-4 gap-y-1"
            >
              <LegendItem className="flex items-center gap-1.5">
                <LegendMarker />
                <LegendLabel />
                <LegendValue showPercentage />
              </LegendItem>
            </Legend>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

// Vertical bars per year, one series per key (stacked or grouped).
// showShare appends each series' percentage of that bar's total to its tooltip row.
function YearBars({ data, series, stacked, wide, showShare }: { data: Record<string, unknown>[]; series: { key: string; label: string }[]; stacked?: boolean; wide?: boolean; showShare?: boolean }) {
  const rows = showShare
    ? (point: Record<string, unknown>) => {
        const total = series.reduce((s, x) => s + (Number(point[x.key]) || 0), 0)
        return series.map((s, i) => {
          const value = Number(point[s.key]) || 0
          return { color: shade(i), label: s.label, value: `${fmt(value)} · ${pct(value, total)}` }
        })
      }
    : undefined
  return (
    <>
      <ChartScroll><BarChart data={data} stacked={stacked} stackGap={1} aspectRatio={wide ? '4.5 / 1' : '2.2 / 1'} margin={{ top: 8, right: 16, bottom: 30, left: 16 }}>
        <Grid horizontal vertical={false} />
        {series.map((s, i) => <Bar key={s.key} dataKey={s.key} fill={shade(i)} />)}
        <BarXAxis />
        <ChartTooltip rows={rows} />
      </BarChart></ChartScroll>
      <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs mt-2">
        {series.map((s, i) => (
          <li key={s.key} className="flex items-center gap-1.5">
            <span className="inline-block size-2.5 rounded-sm" style={{ background: shade(i) }} />{s.label}
          </li>
        ))}
      </ul>
    </>
  )
}

// Multi-line take on YearBars: one curved line per series, tooltip rows show
// each series' share of that year's total.
function YearLines({ data, series }: { data: Record<string, unknown>[]; series: { key: string; label: string }[] }) {
  const chartMargin = { top: 8, right: 16, bottom: 8, left: 16 }
  // The shared LineChart's y-scale is hardcoded to scaleLinear (grid, tween, tooltip
  // positions all assume it) — no log-scale switch, and a real one would break on any
  // zero-count year anyway. log1p the plotted values instead so a small series isn't
  // flattened by a much bigger one; tooltip rows below read the untransformed fields.
  const logData = data.map(d => {
    const row = { ...d }
    for (const s of series) row[`${s.key}__log`] = Math.log1p(Number(d[s.key]) || 0)
    return row
  })
  return (
    <>
      <LineChart data={logData} xDataKey="date" aspectRatio="4.5 / 1.5" margin={chartMargin}>
        <Background pattern="dots" opacity={0.85} />
        {series.map((s, i) => (
          <Line key={s.key} dataKey={`${s.key}__log`} stroke={shade(i)} curve={curveCatmullRom} strokeWidth={2} fadeEdges />
        ))}
        <ChartTooltip
          showDatePill={false}
          content={({ point }) => (
            <TooltipContent
              title={String((point.date as Date).getFullYear())}
              rows={series.map((s, i) => {
                const total = series.reduce((sum, x) => sum + (Number(point[x.key]) || 0), 0)
                const value = Number(point[s.key]) || 0
                return { color: shade(i), label: s.label, value: `${fmt(value)} · ${pct(value, total)}` }
              })}
            />
          )}
        />
      </LineChart>
      {/* One point per year, no day-level granularity — the vendored XAxis/date-pill format
          to month/day and can't be overridden, so ticks are a plain static row here instead. */}
      <div className="flex justify-between text-xs text-chart-label">
        {data.map((d, i) => <span key={i}>{d.name as string}</span>)}
      </div>
      <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs mt-2">
        {series.map((s, i) => (
          <li key={s.key} className="flex items-center gap-1.5">
            <span className="inline-block size-2.5 rounded-sm" style={{ background: shade(i) }} />{s.label}
          </li>
        ))}
      </ul>
    </>
  )
}

function yearRows(years: number[], pick: (y: number) => Record<string, number>) {
  return years.map(y => ({ name: String(y), date: new Date(y, 0, 1), ...pick(y) }))
}

const PREVIEW_ROWS = 10

// "Show all N" toggle for long tables; returns the visible slice + the button.
function useRowLimit<T>(rows: T[], limit = PREVIEW_ROWS) {
  const [all, setAll] = useState(false)
  const shown = all ? rows : rows.slice(0, limit)
  const toggle = rows.length > limit && (
    <button className="btn-ghost mt-3" onClick={() => setAll(v => !v)}>
      {all ? 'Show fewer' : `Show all ${rows.length} rows`}
    </button>
  )
  return { shown, toggle }
}

function OffenceTable({ title, lines, showAgency, hideOffence }: { title: string; lines: { agency: string; offence: string; total: number }[]; showAgency?: boolean; hideOffence?: boolean }) {
  const total = lines.reduce((s, l) => s + l.total, 0)
  const max = Math.max(0, ...lines.map(l => l.total))
  const sorted = [...lines].sort((a, b) => b.total - a.total)
  const { shown, toggle } = useRowLimit(sorted)
  const lead = (showAgency ? 1 : 0) + (hideOffence ? 0 : 1)
  return (
    <section className="mb-10">
      <h2 className="text-base font-semibold mb-3">{title}</h2>
      <Table className="table-fixed min-w-[680px]">
        <TableHeader>
          <TableRow>
            {showAgency && <TableHead className="w-28">Agency</TableHead>}
            {!hideOffence && <TableHead>Offence</TableHead>}
            <TableHead className="w-24 text-right">Blocked</TableHead>
            <TableHead className="w-[30%]"><span className="sr-only">Proportion</span></TableHead>
            <TableHead className="w-24 text-right">Share</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {shown.map(l => (
            <TableRow key={l.agency + l.offence}>
              {showAgency && <TableCell>{l.agency}</TableCell>}
              {!hideOffence && <TableCell className="truncate">{l.offence}</TableCell>}
              <BarCells value={l.total} max={max} />
              <TableCell className="text-right tabular-nums">{pct(l.total, total)}</TableCell>
            </TableRow>
          ))}
          <TableRow className="font-semibold">
            <TableCell colSpan={lead}>Total</TableCell>
            <TableCell className="text-right tabular-nums">{fmt(total)}</TableCell>
            <TableCell />
            <TableCell className="text-right">100%</TableCell>
          </TableRow>
        </TableBody>
      </Table>
      {toggle}
    </section>
  )
}

function TabA({ lines, years }: { lines: Line[]; years: number[] }) {
  const mcmc = lines.filter(l => l.agency === MCMC && isMcmcCategory(l.offence))
  const latest = years[years.length - 1]
  const overall = mcmc.map(l => ({ ...l, total: sumYears(l, years) })).filter(l => l.total)
  const thisYear = mcmc.map(l => ({ ...l, total: l.byYear[latest] ?? 0 })).filter(l => l.total)
  const total = overall.reduce((s, l) => s + l.total, 0)
  const top = topN(overall.map(l => ({ label: l.offence, value: l.total })), 4).map(t => t.label).filter(l => l !== 'Other')
  const stackedSeries = top.length < overall.length
    ? [...top.map(t => ({ key: t, label: t })), { key: 'Other', label: 'Other' }]
    : top.map(t => ({ key: t, label: t }))
  const perYear = yearRows(years, y => {
    const r: Record<string, number> = { Other: 0 }
    for (const l of mcmc) r[top.includes(l.offence) ? l.offence : 'Other'] = (r[top.includes(l.offence) ? l.offence : 'Other'] ?? 0) + (l.byYear[y] ?? 0)
    return r
  })
  return (
    <>
      <div className="flex flex-wrap gap-8 mb-6">
        <Kpi label={`MCMC blocks ${years[0]}–${latest}`} value={fmt(total)} />
        <Kpi label={`MCMC blocks ${latest}`} value={fmt(thisYear.reduce((s, l) => s + l.total, 0))} />
        <Kpi label="Offence types" value={String(overall.length)} />
      </div>
      <div className="grid gap-4 grid-cols-1 lg:grid-cols-[3fr_7fr] mb-10">
        <Card title="Share of blocks"><Donut items={overall.map(l => ({ label: l.offence, value: l.total }))} label="Blocks" /></Card>
        <Card title="By year and offence"><YearLines data={perYear} series={stackedSeries} /></Card>
      </div>
    </>
  )
}

// Vertical stacked bars: one bar per agency, one stack segment per offence,
// filtered to a single year or a from–to range.
function AgencyOffenceStack({ lines, years }: { lines: Line[]; years: number[] }) {
  const first = years[0]
  const last = years[years.length - 1]
  const [mode, setMode] = useState<'single' | 'range'>('range')
  const [from, setFrom] = useState(first)
  const [to, setTo] = useState(last)
  const selected = mode === 'single' ? [from] : years.filter(y => y >= Math.min(from, to) && y <= Math.max(from, to))

  const { data, series } = useMemo(() => {
    const byAgency = new Map<string, Map<string, number>>()
    const byOffence = new Map<string, number>()
    for (const l of lines) {
      const t = sumYears(l, selected)
      if (!t) continue
      const m = byAgency.get(l.agency) ?? new Map<string, number>()
      m.set(l.offence, (m.get(l.offence) ?? 0) + t)
      byAgency.set(l.agency, m)
      byOffence.set(l.offence, (byOffence.get(l.offence) ?? 0) + t)
    }
    const offences = [...byOffence].sort((a, b) => b[1] - a[1]).map(([o]) => o)
    const agencies = [...byAgency]
      .map(([name, m]) => ({ name, m, total: [...m.values()].reduce((a, b) => a + b, 0) }))
      .sort((a, b) => b.total - a.total)
    return {
      series: offences.map(o => ({ key: o, label: o })),
      // The shared BarChart's value scale is hardcoded linear, so log the plotted
      // height instead: each bar's total becomes log1p(total), split into segments
      // in their real proportions. Real counts ride along in `raw` for the tooltip.
      data: agencies.map(a => {
        const raw = Object.fromEntries(offences.map(o => [o, a.m.get(o) ?? 0]))
        const k = Math.log1p(a.total) / a.total
        return { name: a.name, raw, ...Object.fromEntries(offences.map(o => [o, raw[o] * k])) }
      }),
    }
  }, [lines, selected.join(",")])

  const tooltipRows = (point: Record<string, unknown>) => {
    const raw = point.raw as Record<string, number>
    const total = series.reduce((s, x) => s + (raw[x.key] || 0), 0)
    return series
      .map((x, i) => ({ color: ramp(i, series.length), label: x.label, v: raw[x.key] || 0 }))
      .filter(r => r.v)
      .map(r => ({ color: r.color, label: r.label, value: `${fmt(r.v)} · ${pct(r.v, total)}` }))
  }
  const yearSelect = (value: number, onChange: (y: number) => void, label: string) => (
    <select aria-label={label} className="filter-select" value={value} onChange={e => onChange(Number(e.target.value))}>
      {years.map(y => <option key={y} value={y}>{y}</option>)}
    </select>
  )

  return (
    <>
      <div className="filter-bar mb-3 flex-wrap">
        <span className="filter-label">Period</span>
        <select aria-label="Filter mode" className="filter-select" value={mode} onChange={e => setMode(e.target.value as 'single' | 'range')}>
          <option value="range">Year range</option>
          <option value="single">Single year</option>
        </select>
        {mode === 'single'
          ? yearSelect(from, setFrom, 'Year')
          : <>{yearSelect(from, setFrom, 'From year')}<span className="filter-label">to</span>{yearSelect(to, setTo, 'To year')}</>}
      </div>
      {data.length === 0 ? <p className="text-sm opacity-60">No blocks in this period.</p> : (
        <>
          <div className="overflow-x-auto">
            <div style={{ minWidth: Math.max(520, data.length * 72) }}>
              <BarChart data={data} stacked stackGap={1} aspectRatio="2.4 / 1" margin={{ top: 8, right: 16, bottom: 30, left: 16 }}>
                <Grid horizontal vertical={false} />
                {series.map((s, i) => <Bar key={s.key} dataKey={s.key} fill={ramp(i, series.length)} />)}
                <BarXAxis />
                <ChartTooltip rows={tooltipRows} />
              </BarChart>
            </div>
          </div>
          <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs mt-2">
            {series.map((s, i) => (
              <li key={s.key} className="flex items-center gap-1.5">
                <span className="inline-block size-2.5 rounded-sm" style={{ background: ramp(i, series.length) }} />{s.label}
              </li>
            ))}
          </ul>
        </>
      )}
    </>
  )
}

function TabB({ lines, years }: { lines: Line[]; years: number[] }) {
  const other = lines
    .filter(l => l.agency !== MCMC)
    .map(l => ({ ...l, total: sumYears(l, years) }))
    .filter(l => l.total)
  const byAgency = new Map<string, number>()
  for (const l of other) byAgency.set(l.agency, (byAgency.get(l.agency) ?? 0) + l.total)
  const agencyItems = [...byAgency].map(([label, value]) => ({ label, value }))
  return (
    <>
      <div className="flex flex-wrap gap-8 mb-6">
        <Kpi label="Blocks by other agencies" value={fmt(agencyItems.reduce((s, a) => s + a.value, 0))} />
        <Kpi label="Agencies" value={String(agencyItems.length)} />
      </div>
      <div className="grid gap-4 grid-cols-1 lg:grid-cols-2 mb-10">
        <Card title="Share of blocks" className="col-span-full"><AgencySunburst rows={other} label="Blocks" /></Card>
        <Card title="By agency" className="col-span-full"><HBar items={agencyItems} wide /></Card>
        <Card title="Agency and offence" className="col-span-full">
          <AgencyOffenceStack lines={lines.filter(l => l.agency !== MCMC)} years={years} />
        </Card>
      </div>
    </>
  )
}

const sumYears = (l: Line, years: number[]) => years.reduce((s, y) => s + (l.byYear[y] ?? 0), 0)

const zero = (n: number) => (n ? fmt(n) : <span className="opacity-30">–</span>)

// Year/total/share column widths shared by both TabC tables so they align.
function NumHeads({ years }: { years: number[] }) {
  return (
    <>
      {years.map(y => <TableHead key={y} className="w-20 text-right">{y}</TableHead>)}
      <TableHead className="w-24 text-right">Total</TableHead>
      <TableHead className="w-24 text-right">Share</TableHead>
    </>
  )
}

function TabC({ lines, years }: { lines: Line[]; years: number[] }) {
  const yearTotals = (pred: (l: Line) => boolean) => years.map(y => lines.filter(pred).reduce((s, l) => s + (l.byYear[y] ?? 0), 0))
  const mcmc = yearTotals(l => l.agency === MCMC && isMcmcCategory(l.offence))
  const other = yearTotals(l => l.agency !== MCMC || !isMcmcCategory(l.offence))
  const all = years.map((_, i) => mcmc[i] + other[i])
  const grand = all.reduce((s, n) => s + n, 0)
  const maxYear = Math.max(0, ...all)
  const rows = [
    { label: MCMC, vals: mcmc },
    { label: 'Other agencies', vals: other },
  ]
  const byOffenceYear = new Map<string, Record<number, number>>()
  for (const l of lines) {
    const label = `${l.agency} · ${l.offence}`
    const rec = byOffenceYear.get(label) ?? {}
    for (const y of years) rec[y] = (rec[y] ?? 0) + (l.byYear[y] ?? 0)
    byOffenceYear.set(label, rec)
  }
  const topOffences = [...byOffenceYear.entries()]
    .map(([k, r]) => [k, Object.values(r).reduce((s, n) => s + n, 0)] as const)
    .sort((a, b) => b[1] - a[1]).slice(0, 5).map(([k]) => k)
  const detail = lines.filter(l => sumYears(l, years)).sort((a, b) => sumYears(b, years) - sumYears(a, years))
  const { shown, toggle } = useRowLimit(detail, 12)
  return (
    <>
      <section className="mb-10">
        <h2 className="text-base font-semibold mb-3">Blocks by jurisdiction and year</h2>
        <div className="grid gap-4 grid-cols-1 lg:grid-cols-2 mb-6">
          <Card title="Blocks per year: MCMC vs other agencies">
            <YearBars
              data={yearRows(years, y => ({ mcmc: mcmc[years.indexOf(y)], other: other[years.indexOf(y)] }))}
              series={[{ key: 'mcmc', label: MCMC }, { key: 'other', label: 'Other agencies' }]}
              stacked
            />
          </Card>
          <Card title="Jurisdiction share"><Donut items={rows.map(r => ({ label: r.label, value: r.vals.reduce((s, n) => s + n, 0) }))} label="Blocks" /></Card>
          <Card title="Top offences per year" className="col-span-full">
            <YearBars
              data={yearRows(years, y => Object.fromEntries(topOffences.map(t => [t, byOffenceYear.get(t)?.[y] ?? 0])))}
              series={topOffences.map(t => ({ key: t, label: t }))}
              wide
            />
          </Card>
        </div>
        <Table className="table-fixed min-w-[900px]">
          <TableHeader>
            <TableRow>
              <TableHead colSpan={2}>Jurisdiction</TableHead>
              <NumHeads years={years} />
            </TableRow>
</TableHeader>
          <TableBody>
            {rows.map(r => {
              const t = r.vals.reduce((s, n) => s + n, 0)
              return (
                <TableRow key={r.label}>
                  <TableCell colSpan={2}>{r.label}</TableCell>
                  {r.vals.map((v, i) => <TableCell key={years[i]} className="text-right tabular-nums">{fmt(v)}</TableCell>)}
                  <TableCell className="text-right tabular-nums">{fmt(t)}</TableCell>
                  <TableCell className="text-right tabular-nums">{pct(t, grand)}</TableCell>
                </TableRow>
              )
            })}
            <TableRow className="font-semibold">
              <TableCell colSpan={2}>Total</TableCell>
              {all.map((v, i) => <TableCell key={years[i]} className="text-right tabular-nums">{fmt(v)}</TableCell>)}
              <TableCell className="text-right tabular-nums">{fmt(grand)}</TableCell>
              <TableCell className="text-right">100%</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </section>

      <section className="mb-10">
        <h2 className="text-base font-semibold mb-3">Blocks by agency, offence and year</h2>
        <Table className="table-fixed min-w-[900px]">
          <TableHeader>
            <TableRow>
              <TableHead className="w-28">Agency</TableHead>
              <TableHead>Offence</TableHead>
              <NumHeads years={years} />
            </TableRow>
</TableHeader>
          <TableBody>
            {shown.map(l => (
              <TableRow key={l.agency + l.offence}>
                <TableCell>{l.agency}</TableCell>
                <TableCell className="truncate">{l.offence}</TableCell>
                {years.map(y => <TableCell key={y} className="text-right tabular-nums">{zero(l.byYear[y] ?? 0)}</TableCell>)}
                <TableCell className="text-right tabular-nums">{fmt(sumYears(l, years))}</TableCell>
                <TableCell className="text-right tabular-nums">{pct(sumYears(l, years), grand)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {toggle}
      </section>
    </>
  )
}

export type StatsPart = 'a' | 'b' | 'c'

// One Statistics view (A: MCMC, B: other agencies, C: comparison); fetches on mount.
export function BlockingStatsTab({ part }: { part: StatsPart }) {
  const [rows, setRows] = useState<BlockingStatRow[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    fetchBlockingStats().then(setRows, e => setError(e instanceof Error ? e.message : 'Failed to load'))
  }, [])

  const lines = useMemo(() => toLines((rows ?? []).filter(r => r.year >= FIRST_YEAR)), [rows])
  const years = useMemo(() => [...new Set(lines.flatMap(l => Object.keys(l.byYear).map(Number)))].sort(), [lines])

  if (error) return <p className="text-sm mt-4">{error}</p>
  if (!rows) return null
  if (years.length === 0) return <p className="text-sm mt-4">No blocked domains with a dated Notice letter yet.</p>
  return (
    <div className="mt-4">
      <p className="page-subtitle mb-4">Domains blocked since {FIRST_YEAR}, by Notice-letter year. A domain with several offences counts under each.</p>
      {part === 'a' && <TabA lines={lines} years={years} />}
      {part === 'b' && <TabB lines={lines} years={years} />}
      {part === 'c' && <TabC lines={lines} years={years} />}
    </div>
  )
}
