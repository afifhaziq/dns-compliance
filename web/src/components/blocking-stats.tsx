import { useEffect, useMemo, useRef, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { curveCatmullRom } from '@visx/curve'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
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
import { Filters } from '@/components/reui/filters/filters'
import { createFilterQuery, createFilterRule } from '@/components/reui/filters/filters-query'
import type { FilterField, FilterQuery, FilterRule } from '@/components/reui/filters/filters-types'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Tabs, TabsList, TabsTrigger } from '@/components/motion/tabs'
import { Button } from '@/components/ui/button'
import { ButtonGroup, ButtonGroupText } from '@/components/ui/button-group'
import { DropdownMenu, DropdownMenuContent, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { ChevronDownIcon } from 'lucide-react'
import { ChartTooltip, TooltipBox, TooltipContent } from '@/components/charts/tooltip'
import { fetchBlockingStats, type BlockingStatRow } from '../api/blocking-stats'

// Mirrors the source workbook's scope (2022 onward, MCMC vs everyone else).
const FIRST_YEAR = 2022
const MCMC = 'MCMC'
// The MCMC workbook only ever classifies offences under these five categories —
// anything else on an MCMC-owned case is stray/legacy data, not a sixth category.
const MCMC_CATEGORIES = ['Lucah', 'Sumbang', 'Palsu', 'Jelik', 'Mengancam']
const isMcmcCategory = (offence: string) => MCMC_CATEGORIES.some(c => c.toLowerCase() === offence.toLowerCase())
// The workbook credits a block to the agency whose law the offence falls under,
// not the one that handled it: MCMC's five categories are MCMC's whatever the
// Agensi, and gambling MCMC handled is listed under PDRM. Applied once on load
// so every preset and filter sees the same attribution.
const OFFENCE_OWNER: Record<string, string> = { judi: 'PDRM' }
const attribute = (r: BlockingStatRow): BlockingStatRow => {
  if (isMcmcCategory(r.offence)) return { ...r, agency: MCMC }
  const owner = r.agency === MCMC ? OFFENCE_OWNER[r.offence.toLowerCase()] : undefined
  return owner ? { ...r, agency: owner } : r
}
const fmt = (n: number) => n.toLocaleString()
const pct = (n: number, total: number) => (total ? `${((n / total) * 100).toFixed(2)}%` : '—')

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

function Card({ title, children, className = '', action }: { title: string; children: React.ReactNode; className?: string; action?: React.ReactNode }) {
  return (
    <div className={`bento-card ${className}`}>
      <div className="flex items-start justify-between gap-3 mb-3">
        <h3 className="text-sm font-semibold">{title}</h3>
        {action}
      </div>
      {children}
    </div>
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
          <SunburstLabels onlyDepth={1} minArcLength={12} fontSize={10} fill="var(--background)" strokeWidth={0} />
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

type Rule = FilterRule<unknown>
type GroupBy = 'offence' | 'agency' | 'agencyOffence' | 'jurisdiction'

// Blank/partial values match everything, so a half-built chip doesn't empty the view.
function passes(rule: Rule, v: string | number): boolean {
  const val = rule.value as unknown
  const blank = (x: unknown) => x == null || x === ''
  let ok = true
  switch (rule.operator) {
    case 'in': ok = !Array.isArray(val) || !val.length || val.includes(v); break
    case 'nin': ok = !Array.isArray(val) || !val.includes(v); break
    case 'eq': ok = blank(val) || Number(val) === v; break
    case 'neq': ok = blank(val) || Number(val) !== v; break
    case 'between':
    case 'not_between': {
      const [a, b] = Array.isArray(val) ? val.map(x => (blank(x) ? NaN : Number(x))) : [NaN, NaN]
      const lo = isNaN(a) ? -Infinity : isNaN(b) ? a : Math.min(a, b)
      const hi = isNaN(b) ? Infinity : isNaN(a) ? b : Math.max(a, b)
      const inside = Number(v) >= lo && Number(v) <= hi
      ok = rule.operator === 'between' ? inside : !inside
    }
  }
  return rule.negated ? !ok : ok
}

// Workbook C1's split: "Agensi Lain" is every agency except MCMC (after attribute()).
const jurisdiction = (r: BlockingStatRow) => (r.agency === MCMC ? MCMC : 'Other agencies')
// Agency + offence keys join with a separator no name contains; the table splits it back into two columns.
const SEP = '\u0000'
const groupKey: Record<GroupBy, (r: BlockingStatRow) => string> = {
  offence: r => r.offence,
  agency: r => r.agency,
  agencyOffence: r => `${r.agency}${SEP}${r.offence}`,
  jurisdiction,
}
const GROUP_LABEL: Record<GroupBy, string> = { offence: 'Offence', agency: 'Agency', agencyOffence: 'Agency + offence', jurisdiction: 'Jurisdiction' }
const display = (label: string) => label.replace(SEP, ' · ')

type GroupLine = { label: string; byYear: Record<number, number>; total: number }

// One line per group with per-year counts, largest first.
function aggregate(rows: BlockingStatRow[], years: number[], groupBy: GroupBy): GroupLine[] {
  const key = groupKey[groupBy]
  const groups = new Map<string, Record<number, number>>()
  for (const r of rows) {
    const g = groups.get(key(r)) ?? {}
    g[r.year] = (g[r.year] ?? 0) + r.count
    groups.set(key(r), g)
  }
  return [...groups]
    .map(([label, byYear]) => ({ label, byYear, total: years.reduce((s, y) => s + (byYear[y] ?? 0), 0) }))
    .filter(l => l.total)
    .sort((a, b) => b.total - a.total)
}

type Preset = { id: string; label: string; groupBy: GroupBy; rules: (offences: string[]) => Rule[] }
// Each preset is the filter + grouping that reproduces one sheet of the source workbook.
const PRESETS: Preset[] = [
  {
    id: 'a', label: 'A · MCMC', groupBy: 'offence',
    rules: offences => [
      createFilterRule<unknown>({ id: 'agency', path: ['agency'], operator: 'in', value: [MCMC] }),
      createFilterRule<unknown>({ id: 'offence', path: ['offence'], operator: 'in', value: offences.filter(isMcmcCategory) }),
    ],
  },
  {
    id: 'b', label: 'B · Other agencies', groupBy: 'agency',
    rules: () => [createFilterRule<unknown>({ id: 'agency', path: ['agency'], operator: 'nin', value: [MCMC] })],
  },
  // C1 is the jurisdiction split; grouping by jurisdiction also renders C2 (agency + offence) below it.
  { id: 'c', label: 'C · Comparison', groupBy: 'jurisdiction', rules: () => [] },
]

const multiselect = (id: string, label: string, values: string[]): FilterField => ({
  id,
  label,
  type: 'multiselect',
  operators: [
    { value: 'in', label: 'is any of', arity: 'many', inverse: 'nin' },
    { value: 'nin', label: 'is none of', arity: 'many', inverse: 'in' },
  ],
  defaultOperator: 'in',
  options: values.map(v => ({ value: v, label: v })),
})

function Explorer({ rows, allYears }: { rows: BlockingStatRow[]; allYears: number[] }) {
  const agencies = useMemo(() => [...new Set(rows.map(r => r.agency))].sort((a, b) => a.localeCompare(b)), [rows])
  const offences = useMemo(() => [...new Set(rows.map(r => r.offence))].sort((a, b) => a.localeCompare(b)), [rows])
  const fields = useMemo<FilterField[]>(() => [
    multiselect('agency', 'Agency', agencies),
    multiselect('offence', 'Offence', offences),
    {
      id: 'year',
      label: 'Year',
      type: 'number',
      operators: [
        { value: 'eq', label: 'is', inverse: 'neq' },
        { value: 'between', label: 'is between', arity: 'range', inverse: 'not_between' },
      ],
      defaultOperator: 'eq',
      placeholder: String(allYears[allYears.length - 1]),
    },
  ], [agencies, offences, allYears])

  const [preset, setPreset] = useState<string | null>('a')
  const [groupBy, setGroupBy] = useState<GroupBy>(PRESETS[0].groupBy)
  const [query, setQuery] = useState<FilterQuery>(() => createFilterQuery<unknown>(PRESETS[0].rules(offences)))
  const applyPreset = (id: string) => {
    const p = PRESETS.find(x => x.id === id)
    if (!p) return
    setPreset(id)
    setGroupBy(p.groupBy)
    setQuery(createFilterQuery<unknown>(p.rules(offences)))
  }

  const rules = query.rules.filter((r): r is Rule => r.type === 'rule')
  const keep = (r: BlockingStatRow) => rules.every(rule => {
    const f = rule.path[0] as 'agency' | 'offence' | 'year'
    return f in r ? passes(rule, r[f]) : true
  })
  const filtered = rows.filter(keep)
  const yearRule = rules.filter(r => r.path[0] === 'year')
  const years = allYears.filter(y => yearRule.every(r => passes(r, y)))

  const lines = aggregate(filtered, years, groupBy)
  const total = lines.reduce((s, l) => s + l.total, 0)

  const top = topN(lines.map(l => ({ label: l.label, value: l.total })), 4).map(t => t.label).filter(l => l !== 'Other')
  const series = [...top.map(t => ({ key: t, label: display(t) })), ...(top.length < lines.length ? [{ key: 'Other', label: 'Other' }] : [])]
  const perYear = yearRows(years, y => {
    const r: Record<string, number> = { Other: 0 }
    for (const l of lines) {
      const k = top.includes(l.label) ? l.label : 'Other'
      r[k] = (r[k] ?? 0) + (l.byYear[y] ?? 0)
    }
    return r
  })
  const noun = GROUP_LABEL[groupBy].toLowerCase()

  return (
    <>
      <Tabs value={preset ?? ''} onValueChange={applyPreset} variant="segment" className="mb-3">
        <TabsList>
          {PRESETS.map(p => <TabsTrigger key={p.id} value={p.id} indicatorClassName="">{p.label}</TabsTrigger>)}
        </TabsList>
      </Tabs>
      {/* Filters decide what is counted, Group by how it's split: one sentence-like row, Clear last. */}
      <div className="flex flex-wrap items-center gap-1.5 mb-8">
        <TooltipProvider>
          <Filters fields={fields} query={query} onQueryChange={q => { setQuery(q); setPreset(null) }} size="sm" className="w-auto" />
        </TooltipProvider>
        <ButtonGroup>
          <ButtonGroupText className="bg-background dark:bg-input/30 text-muted-foreground">Group by</ButtonGroupText>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" className="bg-background dark:bg-input/30">
                {GROUP_LABEL[groupBy]}<ChevronDownIcon className="opacity-60" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuRadioGroup value={groupBy} onValueChange={v => { setGroupBy(v as GroupBy); setPreset(null) }}>
                {(Object.keys(GROUP_LABEL) as GroupBy[]).map(g => <DropdownMenuRadioItem key={g} value={g}>{GROUP_LABEL[g]}</DropdownMenuRadioItem>)}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </ButtonGroup>
        {query.rules.length > 0 && (
          <Button variant="outline" size="sm" onClick={() => { setQuery(createFilterQuery<unknown>([])); setPreset(null) }}>Clear</Button>
        )}
      </div>

      {total === 0 ? <p className="text-sm opacity-60 mb-10">No blocks match these filters.</p> : (
        <>
          <div className="grid gap-4 grid-cols-1 lg:grid-cols-[3fr_7fr] mb-8">
            <Card title="Share of blocks" className={years.length < 2 ? 'lg:col-span-2' : ''}>
              {groupBy === 'agency' || groupBy === 'agencyOffence'
                ? <AgencySunburst rows={filtered.filter(r => years.includes(r.year)).map(r => ({ agency: r.agency, offence: r.offence, total: r.count }))} label="Blocks" />
                : <Donut items={lines.map(l => ({ label: display(l.label), value: l.total }))} label="Blocks" />}
            </Card>
            {years.length > 1 && <Card title={`By year and ${noun}`}><YearLines data={perYear} series={series} /></Card>}
            <BreakdownTable lines={lines} years={years} groupBy={groupBy} />
            {groupBy === 'jurisdiction' && (
              <BreakdownTable lines={aggregate(filtered, years, 'agencyOffence')} years={years} groupBy="agencyOffence" />
            )}
          </div>
        </>
      )}
    </>
  )
}

// Header/cell classes for the breakdown table; matches .results-table's header type.
const TH = 'h-10 px-4 text-[11px] font-semibold tracking-[0.06em] uppercase text-stone-muted'
const TD = 'px-4 py-2.5'
const CELL_EDGE = 'border-l border-stone-border'

// Agency + offence rows as contiguous agency blocks: MCMC first, then agencies
// by their total, and each agency's offences by count.
function byAgencyBlocks(lines: GroupLine[]): GroupLine[] {
  const agencyTotal = new Map<string, number>()
  for (const l of lines) {
    const a = l.label.split(SEP)[0]
    agencyTotal.set(a, (agencyTotal.get(a) ?? 0) + l.total)
  }
  const rank = (a: string) => (a === MCMC ? Infinity : agencyTotal.get(a) ?? 0)
  return [...lines].sort((x, y) => {
    const ax = x.label.split(SEP)[0], ay = y.label.split(SEP)[0]
    return rank(ay) - rank(ax) || ax.localeCompare(ay) || y.total - x.total
  })
}

// Group × year table with Total/Share; agency + offence gets two label columns.
// Full grid: dash-table-wrap draws the rounded outer frame, cells draw the inner lines.
function BreakdownTable({ lines, years, groupBy }: { lines: GroupLine[]; years: number[]; groupBy: GroupBy }) {
  const pair = groupBy === 'agencyOffence'
  const rows = pair ? byAgencyBlocks(lines) : lines
  // Agency blocks are only readable whole, and MCMC alone outruns a 12-row preview.
  const { shown, toggle } = useRowLimit(rows, pair ? Infinity : 12)
  const total = lines.reduce((s, l) => s + l.total, 0)
  const yearTotals = years.map(y => lines.reduce((s, l) => s + (l.byYear[y] ?? 0), 0))
  // Agency cell spans its run of rows within the visible slice.
  const agencyOf = (l: GroupLine) => l.label.split(SEP)[0]
  const span = (i: number) => {
    let n = 1
    while (i + n < shown.length && agencyOf(shown[i + n]) === agencyOf(shown[i])) n++
    return n
  }
  return (
    <div className="lg:col-span-2">
      <div className="dash-table-wrap">
        <Table className="table-fixed min-w-[760px]" aria-label={`Blocks by ${GROUP_LABEL[groupBy].toLowerCase()} and year`}>
          <TableHeader>
            <TableRow className="bg-stone-panel">
              {pair ? (
                <>
                  <TableHead scope="col" className={`w-28 ${TH}`}>Agency</TableHead>
                  <TableHead scope="col" className={`w-72 ${TH} ${CELL_EDGE}`}>Offence</TableHead>
                </>
              ) : <TableHead scope="col" className={`w-56 ${TH}`}>{GROUP_LABEL[groupBy]}</TableHead>}
              {years.map(y => <TableHead scope="col" key={y} className={`${TH} ${CELL_EDGE} text-right`}>{y}</TableHead>)}
              <TableHead scope="col" className={`${TH} ${CELL_EDGE} text-right`}>Total</TableHead>
              <TableHead scope="col" className={`${TH} ${CELL_EDGE} text-right`}>Share</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {shown.map((l, i) => {
              const [agency, offence] = l.label.split(SEP)
              const first = i === 0 || agencyOf(shown[i - 1]) !== agency
              return (
                <TableRow key={l.label} className="border-t border-stone-border transition-colors duration-150 hover:bg-stone-panel">
                  {pair ? (
                    <>
                      {first && <TableCell rowSpan={span(i)} className={`${TD} align-top font-medium bg-background`}>{agency}</TableCell>}
                      <TableCell className={`${TD} ${CELL_EDGE} truncate`} title={offence}>{offence}</TableCell>
                    </>
                  ) : <TableCell className={`${TD} truncate font-medium`} title={l.label}>{l.label}</TableCell>}
                  {years.map(y => <TableCell key={y} className={`${TD} ${CELL_EDGE} text-right tabular-nums`}>{zero(l.byYear[y] ?? 0)}</TableCell>)}
                  <TableCell className={`${TD} ${CELL_EDGE} text-right tabular-nums font-medium`}>{fmt(l.total)}</TableCell>
                  <TableCell className={`${TD} ${CELL_EDGE} text-right tabular-nums text-stone-muted`}>{pct(l.total, total)}</TableCell>
                </TableRow>
              )
            })}
            <TableRow className="border-t border-stone-border bg-stone-panel font-semibold">
              <TableCell className={TD} colSpan={pair ? 2 : 1}>Total</TableCell>
              {yearTotals.map((v, i) => <TableCell key={years[i]} className={`${TD} ${CELL_EDGE} text-right tabular-nums`}>{fmt(v)}</TableCell>)}
              <TableCell className={`${TD} ${CELL_EDGE} text-right tabular-nums`}>{fmt(total)}</TableCell>
              <TableCell className={`${TD} ${CELL_EDGE} text-right tabular-nums`}>100%</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
      {toggle}
    </div>
  )
}

const zero = (n: number) => (n ? fmt(n) : <span className="opacity-30">–</span>)

// The blocking register: one explorer over every (year, agency, offence) count,
// with presets that reproduce each sheet of the source workbook.
export function BlockingRegister() {
  const [rows, setRows] = useState<BlockingStatRow[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    fetchBlockingStats().then(setRows, e => setError(e instanceof Error ? e.message : 'Failed to load'))
  }, [])

  const scoped = useMemo(() => (rows ?? []).filter(r => r.year >= FIRST_YEAR).map(attribute), [rows])
  const years = useMemo(() => [...new Set(scoped.map(r => r.year))].sort(), [scoped])

  if (error) return <p className="text-sm mt-4">{error}</p>
  if (!rows) return null
  if (years.length === 0) return <p className="text-sm mt-4">No blocked domains with a dated Notice letter yet.</p>
  return (
    <div className="mt-6 mb-10">
      <Explorer rows={scoped} allYears={years} />
    </div>
  )
}
