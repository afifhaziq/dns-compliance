import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { ArrowLeftIcon } from 'lucide-react'
import { fetchResultsByUrl } from '../api/results'
import type { ScanResult } from '../api/types'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import { curveStepAfter } from '@visx/curve'
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'
import { Grid } from '@/components/charts/grid'
import { XAxis } from '@/components/charts/x-axis'
import { ChartTooltip } from '@/components/charts/tooltip'
import { decimateTimeSeries } from '@/components/charts/decimate-time-series'

export const Route = createFileRoute('/results/$url')({ component: URLHistoryPage })

type StatusFilter = 'all' | 'violations' | 'compliant'

const DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

function EmptyIcon() {
  return (
    <svg className="empty-icon" width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
      <rect x="8" y="4" width="24" height="32" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M32 4L40 12V36C40 37.1 39.1 38 38 38H32" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M40 12H32V4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 18H26M14 24H22" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

function StatusDot({ compliant }: { compliant: boolean }) {
  return (
    <span className="status-dot-label">
      <span className={`status-dot ${compliant ? 'dot-compliant' : 'dot-violation'}`} aria-hidden="true" />
      <span className={compliant ? 'label-compliant' : 'label-violation'}>
        {compliant ? 'Compliant' : 'Violation'}
      </span>
    </span>
  )
}

function HistorySkeletonRows() {
  return (
    <>
      {[180, 140, 220, 160].map((w, i) => (
        <tr key={i} className="skeleton-row">
          <td className="col-domain"><span className="skeleton" style={{ width: w, height: 14 }} /></td>
          <td className="col-status"><span className="skeleton" style={{ width: 100, height: 20, borderRadius: 4 }} /></td>
          <td className="col-ip" />
          <td className="col-evidence" />
          <td className="col-last-scanned" />
        </tr>
      ))}
    </>
  )
}

// Caps rendered chart points regardless of scan frequency — decimateTimeSeries
// (LTTB) below picks the most visually significant points rather than every
// Nth one, so brief compliance flips are more likely to survive than with
// naive uniform sampling or a frequency-coupled bucket size (e.g. "daily").
const MAX_CHART_POINTS = 240

type ChartPoint = { date: Date; [dnsServerName: string]: number | Date }

function pivotByRun(results: ScanResult[]): ChartPoint[] {
  const runs = new Map<number, { date: Date; values: Record<string, number> }>()
  for (const r of results) {
    const existing = runs.get(r.scan_run_id)
    const values = existing?.values ?? {}
    values[r.dns_server.name] = r.compliant ? 1 : 0
    const date = existing?.date ?? new Date(r.scanned_at)
    runs.set(r.scan_run_id, { date, values })
  }
  return Array.from(runs.values())
    .sort((a, b) => a.date.getTime() - b.date.getTime())
    .map(({ date, values }) => ({ date, ...values }))
}

function URLHistoryPage() {
  const { url } = Route.useParams()
  const hostname = useMemo(() => { try { return new URL(url).hostname } catch { return url } }, [url])

  const [results, setResults] = useState<ScanResult[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [dnsFilter, setDnsFilter] = useState<string>('all')

  const load = useCallback(async () => {
    try {
      setError(null)
      const raw = await fetchResultsByUrl(url)
      setResults(raw)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load results')
    } finally {
      setLoading(false)
    }
  }, [url])

  useEffect(() => { load() }, [load])

  const dnsServers = useMemo(() => {
    const seen = new Map<string, string>()
    for (const r of results) seen.set(r.dns_server.name, r.dns_server.name)
    return Array.from(seen.values()).sort()
  }, [results])

  const filtered = useMemo(() => {
    return results.filter(r => {
      if (dnsFilter !== 'all' && r.dns_server.name !== dnsFilter) return false
      if (statusFilter === 'violations' && r.compliant) return false
      if (statusFilter === 'compliant' && !r.compliant) return false
      return true
    })
  }, [results, statusFilter, dnsFilter])

  const chartData = useMemo(
    () => decimateTimeSeries(pivotByRun(filtered), MAX_CHART_POINTS, dnsServers),
    [filtered, dnsServers],
  )

  return (
    <>
      <Link to="/" className="back-link">
        <ArrowLeftIcon className="back-link-icon" />
        Overview
      </Link>

      <div className="page-header">
        <h1 className="page-title">{hostname}</h1>
        <p className="page-subtitle">{url} · Last 7 days</p>
      </div>

      {!loading && !error && results.length > 0 && (
        <div className="dash-section">
          <LineChart data={chartData} xDataKey="date" aspectRatio="16 / 6">
            <Grid horizontal />
            <XAxis />
            {dnsServers.map((name, i) => (
              <Line
                key={name}
                dataKey={name}
                curve={curveStepAfter}
                stroke={`var(--chart-${5 - (i % 5)})`}
              />
            ))}
            <ChartTooltip />
          </LineChart>
        </div>
      )}

      <div className="filter-bar">
        <span className="filter-label" id="status-label">Status</span>
        <ToggleGroup
          type="single"
          value={statusFilter}
          onValueChange={v => { if (v) setStatusFilter(v as StatusFilter) }}
          variant="outline"
          aria-labelledby="status-label"
        >
          <ToggleGroupItem value="all">All</ToggleGroupItem>
          <ToggleGroupItem value="violations">Violations</ToggleGroupItem>
          <ToggleGroupItem value="compliant">Compliant</ToggleGroupItem>
        </ToggleGroup>

        {dnsServers.length > 1 && (
          <>
            <span className="filter-label" id="dns-label">DNS Server</span>
            <select
              className="filter-select"
              value={dnsFilter}
              onChange={e => setDnsFilter(e.target.value)}
              aria-labelledby="dns-label"
            >
              <option value="all">All servers</option>
              {dnsServers.map(name => (
                <option key={name} value={name}>{name}</option>
              ))}
            </select>
          </>
        )}
      </div>

      <div className="results-wrap">
        {error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : !loading && results.length === 0 ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No results yet</p>
            <p className="empty-body">No scans for this domain in the last 7 days.</p>
          </div>
        ) : (
          <table className="results-table" aria-label={`Scan history for ${hostname}`}>
            <thead>
              <tr>
                <th className="col-domain" scope="col">DNS Server</th>
                <th className="col-status" scope="col">Status</th>
                <th className="col-ip" scope="col">Resolved IP</th>
                <th className="col-evidence" scope="col">Evidence</th>
                <th className="col-last-scanned" scope="col">Scanned At</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <HistorySkeletonRows />
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={5}>
                    <div className="empty-state" style={{ padding: '3rem 0' }}>
                      <p className="empty-heading">No results match the current filters</p>
                    </div>
                  </td>
                </tr>
              ) : (
                filtered.map(r => (
                  <tr key={r.id} className={!r.compliant ? 'violation-row' : ''}>
                    <td className="col-domain"><span className="dns-name">{r.dns_server.name}</span></td>
                    <td className="col-status"><StatusDot compliant={r.compliant} /></td>
                    <td className="col-ip">
                      {r.resolved_ip ? <span className="ip-value">{r.resolved_ip}</span> : <span className="empty-cell" aria-label="Not resolved">—</span>}
                    </td>
                    <td className="col-evidence">
                      {r.screenshot_url ? (
                        <a href={r.screenshot_url} target="_blank" rel="noopener noreferrer" className="screenshot-link" aria-label={`View screenshot for ${r.dns_server.name}`}>
                          View screenshot
                        </a>
                      ) : <span className="empty-cell" aria-label="No screenshot">—</span>}
                    </td>
                    <td className="col-last-scanned">
                      {r.scanned_at ? <span>{DATE_FMT.format(new Date(r.scanned_at))}</span> : <span className="empty-cell">—</span>}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </div>
    </>
  )
}
