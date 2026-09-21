import { useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/motion/tabs'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { BarChart } from '@/components/charts/bar-chart'
import { Bar } from '@/components/charts/bar'
import { BarXAxis } from '@/components/charts/bar-x-axis'
import { BarYAxis } from '@/components/charts/bar-y-axis'
import { PieChart } from '@/components/charts/pie-chart'
import { PieSlice } from '@/components/charts/pie-slice'
import { PieCenter } from '@/components/charts/pie-center'
import { Grid } from '@/components/charts/grid'
import { ChartTooltip } from '@/components/charts/tooltip'
import { fetchBlockingStats, type BlockingStatRow } from '../api/blocking-stats'

export const Route = createFileRoute('/blocking-stats')({
  component: BlockingStatsPage,
})

// Mirrors the source workbook's scope (2022 onward, MCMC vs everyone else).
const FIRST_YEAR = 2022
const MCMC = 'MCMC'
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

// Count cell with an inline proportional bar in the theme accent.
function BarCell({ value, max }: { value: number; max: number }) {
  return (
    <TableCell className="tabular-nums">
      <div className="flex items-center gap-3">
        <span className="w-16 text-right">{fmt(value)}</span>
        <div className="h-2 flex-1 rounded-sm" style={{ maxWidth: 240, background: 'color-mix(in srgb, var(--ink) 12%, transparent)' }}>
          <div className="h-full rounded-sm" style={{ background: SHADES[1], width: `${max ? (value / max) * 100 : 0}%` }} />
        </div>
      </div>
    </TableCell>
  )
}


// Monochromatic ramp of the theme's --ink accent (follows light/dark via the variable).
const SHADES = [100, 68, 46, 30, 19, 11].map(p => `color-mix(in srgb, var(--ink) ${p}%, transparent)`)
const shade = (i: number) => SHADES[Math.min(i, SHADES.length - 1)]

// Keep the top n by total, fold the rest into "Other".
function topN<T extends { label: string; value: number }>(items: T[], n: number): { label: string; value: number }[] {
  const sorted = [...items].sort((a, b) => b.value - a.value)
  const head = sorted.slice(0, n)
  const rest = sorted.slice(n).reduce((s, x) => s + x.value, 0)
  return rest ? [...head, { label: 'Other', value: rest }] : head
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
    <div className="bento-card">
      <div className="text-xs opacity-70">{label}</div>
      <div className="text-3xl font-semibold tabular-nums mt-1">{value}</div>
    </div>
  )
}

// Horizontal bars, one per label, sorted by value.
function HBar({ items }: { items: { label: string; value: number }[] }) {
  const data = [...items].sort((a, b) => b.value - a.value).map(i => ({ name: i.label, value: i.value }))
  return (
    <BarChart
      data={data}
      orientation="horizontal"
      aspectRatio={`640 / ${data.length * 34 + 30}`}
      margin={{ top: 10, right: 30, bottom: 20, left: 160 }}
    >
      <Grid horizontal={false} vertical />
      <Bar dataKey="value" fill={SHADES[0]} />
      <BarYAxis maxLabels={data.length} />
      <ChartTooltip />
    </BarChart>
  )
}

// Donut with a swatch legend beside it.
function Donut({ items, label }: { items: { label: string; value: number }[]; label: string }) {
  const data = topN(items, 5).map((d, i) => ({ ...d, color: shade(i) }))
  const total = data.reduce((s, d) => s + d.value, 0)
  return (
    <div className="flex items-center gap-6 flex-wrap">
      <div style={{ width: 220, height: 220 }}>
        <PieChart data={data} size={220} innerRadius={70} padAngle={0.02}>
          {data.map((_, i) => <PieSlice key={i} index={i} />)}
          <PieCenter defaultLabel={label} />
        </PieChart>
      </div>
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

// Vertical bars per year, one series per key (stacked or grouped).
function YearBars({ data, series, stacked, wide }: { data: Record<string, unknown>[]; series: { key: string; label: string }[]; stacked?: boolean; wide?: boolean }) {
  return (
    <>
      <BarChart data={data} stacked={stacked} stackGap={1} aspectRatio={wide ? '4.5 / 1' : '2.2 / 1'} margin={{ top: 20, right: 20, bottom: 30, left: 50 }}>
        <Grid horizontal vertical={false} />
        {series.map((s, i) => <Bar key={s.key} dataKey={s.key} fill={shade(i)} />)}
        <BarXAxis />
        <ChartTooltip />
      </BarChart>
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
  return years.map(y => ({ name: String(y), ...pick(y) }))
}

function OffenceTable({ title, lines, showAgency }: { title: string; lines: { agency: string; offence: string; total: number }[]; showAgency?: boolean }) {
  const total = lines.reduce((s, l) => s + l.total, 0)
  const max = Math.max(0, ...lines.map(l => l.total))
  const sorted = [...lines].sort((a, b) => b.total - a.total)
  return (
    <section className="mb-10">
      <h2 className="text-base font-semibold mb-3">{title}</h2>
      <Table>
        <TableHeader>
          <TableRow>
            {showAgency && <TableHead>Agency</TableHead>}
            <TableHead>Offence</TableHead>
            <TableHead>Blocked</TableHead>
            <TableHead className="text-right">Share</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sorted.map(l => (
            <TableRow key={l.agency + l.offence}>
              {showAgency && <TableCell>{l.agency}</TableCell>}
              <TableCell>{l.offence}</TableCell>
              <BarCell value={l.total} max={max} />
              <TableCell className="text-right tabular-nums">{pct(l.total, total)}</TableCell>
            </TableRow>
          ))}
          <TableRow className="font-semibold">
            <TableCell colSpan={showAgency ? 2 : 1}>Total</TableCell>
            <TableCell className="tabular-nums">{fmt(total)}</TableCell>
            <TableCell className="text-right">100%</TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </section>
  )
}

function TabA({ lines, years }: { lines: Line[]; years: number[] }) {
  const mcmc = lines.filter(l => l.agency === MCMC)
  const latest = years[years.length - 1]
  const overall = mcmc.map(l => ({ ...l, total: sumYears(l, years) })).filter(l => l.total)
  const thisYear = mcmc.map(l => ({ ...l, total: l.byYear[latest] ?? 0 })).filter(l => l.total)
  const total = overall.reduce((s, l) => s + l.total, 0)
  const top = topN(overall.map(l => ({ label: l.offence, value: l.total })), 4).map(t => t.label).filter(l => l !== 'Other')
  const stackedSeries = [...top.map(t => ({ key: t, label: t })), { key: 'Other', label: 'Other' }]
  const perYear = yearRows(years, y => {
    const r: Record<string, number> = { Other: 0 }
    for (const l of mcmc) r[top.includes(l.offence) ? l.offence : 'Other'] = (r[top.includes(l.offence) ? l.offence : 'Other'] ?? 0) + (l.byYear[y] ?? 0)
    return r
  })
  return (
    <>
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-3 mb-6">
        <Kpi label={`MCMC blocks ${years[0]}–${latest}`} value={fmt(total)} />
        <Kpi label={`MCMC blocks ${latest}`} value={fmt(thisYear.reduce((s, l) => s + l.total, 0))} />
        <Kpi label="Offence types" value={String(overall.length)} />
      </div>
      <div className="grid gap-4 grid-cols-1 lg:grid-cols-2 mb-10">
        <Card title="By offence"><HBar items={overall.map(l => ({ label: l.offence, value: l.total }))} /></Card>
        <Card title="Share of blocks"><Donut items={overall.map(l => ({ label: l.offence, value: l.total }))} label="Blocks" /></Card>
        <Card title="By year and offence" className="col-span-full"><YearBars data={perYear} series={stackedSeries} stacked wide /></Card>
      </div>
      <OffenceTable title={`MCMC blocks by offence, ${years[0]}–${latest}`} lines={overall} />
      <OffenceTable title={`MCMC blocks by offence, ${latest}`} lines={thisYear} />
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
  const agencyLines = agencyItems.map(a => ({ agency: a.label, offence: '—', total: a.value }))
  return (
    <>
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-2 mb-6">
        <Kpi label="Blocks by other agencies" value={fmt(agencyItems.reduce((s, a) => s + a.value, 0))} />
        <Kpi label="Agencies" value={String(agencyItems.length)} />
      </div>
      <div className="grid gap-4 grid-cols-1 lg:grid-cols-2 mb-10">
        <Card title="By agency"><HBar items={agencyItems} /></Card>
        <Card title="Share of blocks"><Donut items={agencyItems} label="Blocks" /></Card>
        <Card title="Top agency–offence pairs" className="col-span-full">
          <HBar items={other.map(l => ({ label: `${l.agency} · ${l.offence}`, value: l.total })).sort((a, b) => b.value - a.value).slice(0, 12)} />
        </Card>
      </div>
      <OffenceTable title="Blocks by agency (excluding MCMC)" lines={agencyLines} showAgency />
      <OffenceTable title="Blocks by agency and offence" lines={other} showAgency />
    </>
  )
}

const sumYears = (l: Line, years: number[]) => years.reduce((s, y) => s + (l.byYear[y] ?? 0), 0)

function TabC({ lines, years }: { lines: Line[]; years: number[] }) {
  const yearTotals = (pred: (l: Line) => boolean) => years.map(y => lines.filter(pred).reduce((s, l) => s + (l.byYear[y] ?? 0), 0))
  const mcmc = yearTotals(l => l.agency === MCMC)
  const other = yearTotals(l => l.agency !== MCMC)
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
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Jurisdiction</TableHead>
              {years.map(y => <TableHead key={y} className="text-right">{y}</TableHead>)}
              <TableHead className="text-right">Total</TableHead>
              <TableHead className="text-right">Share</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map(r => {
              const t = r.vals.reduce((s, n) => s + n, 0)
              return (
                <TableRow key={r.label}>
                  <TableCell>{r.label}</TableCell>
                  {r.vals.map((v, i) => <TableCell key={years[i]} className="text-right tabular-nums">{fmt(v)}</TableCell>)}
                  <TableCell className="text-right tabular-nums">{fmt(t)}</TableCell>
                  <TableCell className="text-right tabular-nums">{pct(t, grand)}</TableCell>
                </TableRow>
              )
            })}
            <TableRow className="font-semibold">
              <TableCell>Total</TableCell>
              {all.map((v, i) => <TableCell key={years[i]} className="text-right tabular-nums">{fmt(v)}</TableCell>)}
              <TableCell className="text-right tabular-nums">{fmt(grand)}</TableCell>
              <TableCell className="text-right">100%</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </section>

      <section className="mb-10">
        <h2 className="text-base font-semibold mb-3">Blocks by agency, offence and year</h2>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Agency</TableHead>
              <TableHead>Offence</TableHead>
              {years.map(y => <TableHead key={y} className="text-right">{y}</TableHead>)}
              <TableHead className="text-right">Total</TableHead>
              <TableHead className="text-right">Share</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {detail.map(l => (
              <TableRow key={l.agency + l.offence}>
                <TableCell>{l.agency}</TableCell>
                <TableCell>{l.offence}</TableCell>
                {years.map(y => <TableCell key={y} className="text-right tabular-nums">{fmt(l.byYear[y] ?? 0)}</TableCell>)}
                <TableCell className="text-right tabular-nums">{fmt(sumYears(l, years))}</TableCell>
                <TableCell className="text-right tabular-nums">{pct(sumYears(l, years), grand)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </section>
    </>
  )
}

function BlockingStatsPage() {
  const [rows, setRows] = useState<BlockingStatRow[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    fetchBlockingStats().then(setRows, e => setError(e instanceof Error ? e.message : 'Failed to load'))
  }, [])

  const lines = useMemo(() => toLines((rows ?? []).filter(r => r.year >= FIRST_YEAR)), [rows])
  const years = useMemo(() => [...new Set(lines.flatMap(l => Object.keys(l.byYear).map(Number)))].sort(), [lines])

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title mb-4">Blocking Statistics</h1>
        <p className="page-subtitle">Domains blocked since {FIRST_YEAR}, by Notice-letter year. A domain with several offences counts under each.</p>
      </div>
      {error && <p className="text-sm mb-4">{error}</p>}
      {rows && years.length > 0 && (
        <Tabs defaultValue="a" variant="underline">
          <TabsList>
            <TabsTrigger value="a">A. MCMC</TabsTrigger>
            <TabsTrigger value="b">B. Other agencies</TabsTrigger>
            <TabsTrigger value="c">C. Comparison</TabsTrigger>
          </TabsList>
          <TabsContent value="a"><TabA lines={lines} years={years} /></TabsContent>
          <TabsContent value="b"><TabB lines={lines} years={years} /></TabsContent>
          <TabsContent value="c"><TabC lines={lines} years={years} /></TabsContent>
        </Tabs>
      )}
      {rows && years.length === 0 && <p className="text-sm">No blocked domains with a dated Notice letter yet.</p>}
    </div>
  )
}
