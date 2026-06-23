import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { ArrowLeftIcon, ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { fetchHeatmapByUrlAndYear, fetchResultsByUrl } from '../api/results'
import type { DailyComplianceStat, ScanResult } from '../api/types'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import { HeatmapChart } from '@/components/charts/heatmap'
import { HeatmapChartLoading } from '@/components/charts/heatmap/heatmap-chart-loading'
import { HeatmapCells } from '@/components/charts/heatmap/heatmap-cells'
import { HeatmapXAxis } from '@/components/charts/heatmap/heatmap-x-axis'
import { HeatmapYAxis } from '@/components/charts/heatmap/heatmap-y-axis'
import { HeatmapTooltip } from '@/components/charts/heatmap/heatmap-tooltip'
import { HeatmapLegend } from '@/components/charts/heatmap/heatmap-legend'
import {
  buildYearHeatmapColumns,
  compliancePercentFromStats,
  dateKey,
  HEATMAP_LEVEL_COLORS,
  heatmapYearColorScale,
} from '@/lib/heatmap-year'

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

const currentYear = new Date().getFullYear()

const heatmapTooltipDateFmt = new Intl.DateTimeFormat('en-US', {
  month: 'long',
  day: 'numeric',
  year: 'numeric',
})

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

  const [selectedYear, setSelectedYear] = useState(currentYear)
  const [heatmapStats, setHeatmapStats] = useState<DailyComplianceStat[]>([])
  const [yearLoading, setYearLoading] = useState(true)

  const loadYear = useCallback(async (year: number) => {
    try {
      setYearLoading(true)
      const raw = await fetchHeatmapByUrlAndYear(url, year)
      setHeatmapStats(raw)
    } catch {
      setHeatmapStats([])
    } finally {
      setYearLoading(false)
    }
  }, [url])

  useEffect(() => { loadYear(selectedYear) }, [loadYear, selectedYear])

  const heatmapDnsServers = useMemo(() => {
    const seen = new Map<string, string>()
    for (const s of heatmapStats) seen.set(s.dns_server_name, s.dns_server_name)
    for (const r of results) seen.set(r.dns_server.name, r.dns_server.name)
    return Array.from(seen.values()).sort()
  }, [heatmapStats, results])

  const filtered = useMemo(() => {
    return results.filter(r => {
      if (dnsFilter !== 'all' && r.dns_server.name !== dnsFilter) return false
      if (statusFilter === 'violations' && r.compliant) return false
      if (statusFilter === 'compliant' && !r.compliant) return false
      return true
    })
  }, [results, statusFilter, dnsFilter])

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

      {heatmapDnsServers.length > 0 && (
        <div className="dash-section">
          <div className="heatmap-year-nav">
            <button
              type="button"
              className="heatmap-year-nav-btn"
              onClick={() => setSelectedYear(y => y - 1)}
              aria-label="Previous year"
            >
              <ChevronLeftIcon className="w-4 h-4" />
            </button>
            <span className="heatmap-year-label">{selectedYear}</span>
            <button
              type="button"
              className="heatmap-year-nav-btn"
              onClick={() => setSelectedYear(y => y + 1)}
              disabled={selectedYear >= currentYear}
              aria-label="Next year"
            >
              <ChevronRightIcon className="w-4 h-4" />
            </button>
          </div>

          {heatmapDnsServers.map(name => {
            const serverStats = heatmapStats.filter(s => s.dns_server_name === name)
            const statsByDay = new Map(serverStats.map(s => [s.day, s]))
            const columns = buildYearHeatmapColumns(selectedYear, statsByDay)
            const pct = compliancePercentFromStats(serverStats)

            return (
              <div key={name} className="heatmap-server-block">
                <div className="heatmap-server-header">
                  <p className="dash-label">{name}</p>
                  {pct !== null && !yearLoading && (
                    <span className="heatmap-compliance-badge">{pct}% compliant</span>
                  )}
                </div>

                {yearLoading ? (
                  <HeatmapChartLoading data={columns} gap={3} cornerRadius={999} />
                ) : (
                  <>
                    <HeatmapChart data={columns} gap={3} layout="fluid" levelColors={HEATMAP_LEVEL_COLORS}>
                      <HeatmapCells cornerRadius={999} />
                      <HeatmapXAxis />
                      <HeatmapYAxis />
                      <HeatmapTooltip
                        formatLabel={(_count, date) => {
                          const stat = statsByDay.get(dateKey(date))
                          if (!stat) return `No scans · ${heatmapTooltipDateFmt.format(date)}`
                          return `${stat.compliant} of ${stat.total} compliant · ${heatmapTooltipDateFmt.format(date)}`
                        }}
                      />
                    </HeatmapChart>
                    {/* Rendered as a sibling, not a HeatmapChart child: HeatmapLegend
                        renders a plain <div>, and the chart places its children inside
                        an <svg><g> — nesting it there puts the div in the SVG namespace,
                        where browsers don't lay it out (it silently doesn't render). */}
                    <HeatmapLegend
                      align="center"
                      cornerRadius={999}
                      gap={3}
                      lessLabel="Compliant"
                      moreLabel="More violations"
                      colorScale={heatmapYearColorScale}
                    />
                  </>
                )}
              </div>
            )
          })}
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
