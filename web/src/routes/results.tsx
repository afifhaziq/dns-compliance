import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchResults, groupResults, lastScanTime } from '../api/results'
import { fetchScanProgress } from '../api/scan'
import type { ProgressEntry, ScanProgressResponse } from '../api/scan'
import type { GroupedResult, ScanResult } from '../api/types'
import { useScan } from './__root'
import { LiveLineChart } from '@/components/charts/live-line-chart'
import { LiveLine } from '@/components/charts/live-line'
import { LiveXAxis } from '@/components/charts/live-x-axis'
import { LiveYAxis } from '@/components/charts/live-y-axis'

export const Route = createFileRoute('/results')({ component: ResultsPage })

/* ─── Helpers ────────────────────────────────────────────────────────────── */

function relativeTime(isoString: string): string {
  const diff = (Date.now() - new Date(isoString).getTime()) / 1000
  const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
  if (diff < 60) return rtf.format(-Math.round(diff), 'second')
  if (diff < 3600) return rtf.format(-Math.round(diff / 60), 'minute')
  if (diff < 86400) return rtf.format(-Math.round(diff / 3600), 'hour')
  return rtf.format(-Math.round(diff / 86400), 'day')
}

/* ─── Types ─────────────────────────────────────────────────────────────── */

type StatusFilter = 'all' | 'violations' | 'compliant'

/* ─── Chevron Icon ───────────────────────────────────────────────────────── */

function ChevronRight({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      width="12"
      height="12"
      viewBox="0 0 12 12"
      fill="none"
      aria-hidden="true"
    >
      <path d="M4.5 2.5L7.5 6L4.5 9.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

/* ─── Empty document icon ────────────────────────────────────────────────── */

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

/* ─── Status Dot ─────────────────────────────────────────────────────────── */

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

/* ─── Sub-rows (expanded DNS results) ───────────────────────────────────── */

function SubRows({
  results,
  visible,
}: {
  results: ScanResult[]
  visible: boolean
}) {
  if (!visible) return null

  return (
    <>
      {results.map(r => (
        <tr
          key={r.id}
          className={`sub-row${!r.compliant ? ' violation-row' : ''}`}
        >
          <td className="col-expand" />
          <td className="col-domain">
            <span className="dns-name">{r.dns_server.name}</span>
            {r.error && (
              <span
                style={{
                  marginLeft: 8,
                  fontSize: '0.75rem',
                  color: 'var(--violation-text)',
                }}
                title={r.error}
              >
                (error)
              </span>
            )}
          </td>
          <td className="col-status">
            <StatusDot compliant={r.compliant} />
          </td>
          <td className="col-ip">
            {r.resolved_ip ? (
              <span className="ip-value">{r.resolved_ip}</span>
            ) : (
              <span className="empty-cell" aria-label="Not resolved">—</span>
            )}
          </td>
          <td className="col-evidence">
            {r.screenshot_url ? (
              <a
                href={r.screenshot_url}
                target="_blank"
                rel="noopener noreferrer"
                className="screenshot-link"
                aria-label={`View screenshot for ${r.dns_server.name}`}
              >
                View screenshot
              </a>
            ) : (
              <span className="empty-cell" aria-label="No screenshot">—</span>
            )}
          </td>
          <td className="col-last-scanned">
            {r.scanned_at ? (
              <span title={new Date(r.scanned_at).toLocaleString()}>{relativeTime(r.scanned_at)}</span>
            ) : (
              <span className="empty-cell">—</span>
            )}
          </td>
        </tr>
      ))}
    </>
  )
}

/* ─── URL Group Row ──────────────────────────────────────────────────────── */

function URLGroupRow({
  group,
  expanded,
  onToggle,
}: {
  group: GroupedResult
  expanded: boolean
  onToggle: () => void
}) {
  const { violationCount, totalCount, hostname, url } = group
  const allCompliant = violationCount === 0

  const summaryLabel = allCompliant
    ? `All ${totalCount} compliant`
    : `${violationCount} of ${totalCount} non-compliant`

  return (
    <>
      <tr
        className="url-row"
        onClick={onToggle}
        aria-expanded={expanded}
      >
        <td className="col-expand">
          <button
            className="expand-btn"
            onClick={e => { e.stopPropagation(); onToggle() }}
            aria-expanded={expanded}
            aria-label={`${expanded ? 'Collapse' : 'Expand'} results for ${hostname}`}
            tabIndex={0}
          >
            <ChevronRight className={`expand-icon${expanded ? ' expanded' : ''}`} />
          </button>
        </td>
        <td className="col-domain">
          <span className="hostname" title={url}>{hostname}</span>
        </td>
        <td className="col-status">
          <span
            className={`summary-chip ${allCompliant ? 'all-compliant' : 'has-violations'}`}
          >
            {summaryLabel}
          </span>
        </td>
        <td className="col-ip" />
        <td className="col-evidence" />
        <td className="col-last-scanned" />
      </tr>
      <SubRows results={group.results} visible={expanded} />
    </>
  )
}

/* ─── Skeleton Loading ───────────────────────────────────────────────────── */

function SkeletonRows() {
  return (
    <>
      {[180, 140, 220, 160, 200].map((w, i) => (
        <tr key={i} className="skeleton-row">
          <td className="col-expand">
            <span className="skeleton" style={{ width: 16, height: 16, borderRadius: 3 }} />
          </td>
          <td className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </td>
          <td className="col-status">
            <span className="skeleton" style={{ width: 100, height: 20, borderRadius: 4 }} />
          </td>
          <td className="col-ip" />
          <td className="col-evidence" />
          <td className="col-last-scanned" />
        </tr>
      ))}
    </>
  )
}

/* ─── Results Page ───────────────────────────────────────────────────────── */

/* ─── Scan Progress ──────────────────────────────────────────────────────── */

type ChartPoint = { time: number; value: number }

function useScanProgress() {
  const { scanning } = useScan()
  const [progress, setProgress] = useState<ScanProgressResponse | null>(null)
  const [chartData, setChartData] = useState<ChartPoint[]>([])
  const [latestValue, setLatestValue] = useState(0)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const prevScanningRef = useRef(scanning)

  const appendPoint = useCallback((total: number) => {
    setChartData(prev => [...prev.slice(-500), { time: Date.now() / 1000, value: total }])
    setLatestValue(total)
  }, [])

  const fetchAndUpdate = useCallback(async () => {
    try {
      const data = await fetchScanProgress()
      setProgress(data)
      const total = data.per_dns.reduce((sum, d) => sum + d.completed, 0)
      appendPoint(total)
    } catch (err) {
      if (err instanceof Error && err.message === 'no_run') return
    }
  }, [appendPoint])

  useEffect(() => { fetchAndUpdate() }, [fetchAndUpdate])

  useEffect(() => {
    const wasScanning = prevScanningRef.current
    prevScanningRef.current = scanning

    if (scanning) {
      pollRef.current = setInterval(fetchAndUpdate, 2000)
      return () => {
        if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null }
      }
    } else if (wasScanning) {
      fetchAndUpdate()
    }
  }, [scanning, fetchAndUpdate])

  return { progress, chartData, latestValue }
}

function DNSProgressTable({ perDns, totalUrls }: { perDns: ProgressEntry[]; totalUrls: number }) {
  const sorted = [...perDns].sort((a, b) => {
    const aDone = totalUrls > 0 && a.completed === totalUrls
    const bDone = totalUrls > 0 && b.completed === totalUrls
    if (aDone && !bDone) return -1
    if (!aDone && bDone) return 1
    return a.name.localeCompare(b.name)
  })

  return (
    <div className="progress-dns-wrap">
      <table className="progress-dns-table" aria-label="Per-DNS scan progress">
        <thead>
          <tr>
            <th scope="col">DNS Server</th>
            <th scope="col">Completed</th>
            <th scope="col">Progress</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map(d => {
            const pct = totalUrls > 0 ? Math.round((d.completed / totalUrls) * 100) : 0
            const done = totalUrls > 0 && d.completed === totalUrls
            return (
              <tr key={d.dns_server_id} className={done ? 'progress-row-done' : ''}>
                <td>{d.name}</td>
                <td className="progress-count">{d.completed} / {totalUrls}</td>
                <td className="progress-bar-cell">
                  <div className="progress-bar-wrap" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
                    <div className="progress-bar-fill" style={{ width: `${pct}%` }} />
                  </div>
                  {done && <span className="progress-check" aria-label="Complete">✓</span>}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function ScanProgressSection() {
  const { scanning } = useScan()
  const { progress, chartData, latestValue } = useScanProgress()

  if (!progress || progress.total_urls === 0) return null

  const { scan_run, total_urls, per_dns } = progress
  const isRunning = scan_run.status === 'running'

  return (
    <section className="progress-section" aria-label="Scan progress">
      <div className="progress-section-header">
        <h2 className="progress-section-title">Scan Progress</h2>
        {isRunning ? (
          <span className="progress-status-badge running" role="status">
            <span className="scan-banner-dot" aria-hidden="true" />
            Running…
          </span>
        ) : (
          <span className="progress-status-badge completed">Completed ✓</span>
        )}
      </div>

      <div className="progress-chart-wrap">
        <LiveLineChart data={chartData} value={latestValue} window={60} paused={!scanning}>
          <LiveLine dataKey="value" pulse={scanning} formatValue={(v: number) => `${Math.round(v)} URLs`} />
          <LiveXAxis />
          <LiveYAxis position="left" formatValue={(v: number) => String(Math.round(v))} />
        </LiveLineChart>
      </div>

      <DNSProgressTable perDns={per_dns} totalUrls={total_urls} />
    </section>
  )
}

/* ─── Results Page ───────────────────────────────────────────────────────── */

function ResultsPage() {
  const { scanning, refreshSignal } = useScan()

  const [groups, setGroups] = useState<GroupedResult[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [dnsFilter, setDnsFilter] = useState<string>('all')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const load = useCallback(async () => {
    try {
      setError(null)
      const raw = await fetchResults()
      setGroups(groupResults(raw))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load results')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load, refreshSignal])

  const toggleExpanded = useCallback((url: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(url)) next.delete(url)
      else next.add(url)
      return next
    })
  }, [])

  // All unique DNS server names in the results
  const dnsServers = useMemo(() => {
    const seen = new Map<string, string>()
    for (const g of groups) {
      for (const r of g.results) {
        seen.set(r.dns_server.name, r.dns_server.name)
      }
    }
    return Array.from(seen.values()).sort()
  }, [groups])

  // Client-side filtering
  const filtered = useMemo(() => {
    return groups
      .map(g => {
        let results = g.results

        if (dnsFilter !== 'all') {
          results = results.filter(r => r.dns_server.name === dnsFilter)
        }

        if (statusFilter === 'violations') {
          results = results.filter(r => !r.compliant)
        } else if (statusFilter === 'compliant') {
          results = results.filter(r => r.compliant)
        }

        if (results.length === 0) return null

        const violationCount = results.filter(r => !r.compliant).length
        return { ...g, results, violationCount, totalCount: results.length }
      })
      .filter(Boolean) as GroupedResult[]
  }, [groups, statusFilter, dnsFilter])

  const lastScan = useMemo(() => lastScanTime(groups), [groups])

  return (
    <>
      {/* Page header */}
      <div className="page-header">
        <h1 className="page-title">Compliance Results</h1>
        {lastScan && (
          <p className="page-subtitle">Last scan: {lastScan}</p>
        )}
      </div>

      <ScanProgressSection />

      {/* Filter bar */}
      <div className="filter-bar">
        <span className="filter-label" id="status-label">Status</span>
        <div className="segmented" role="group" aria-labelledby="status-label">
          {(['all', 'violations', 'compliant'] as StatusFilter[]).map(s => (
            <button
              key={s}
              className="segmented-btn"
              aria-pressed={statusFilter === s}
              onClick={() => setStatusFilter(s)}
            >
              {s === 'all' ? 'All' : s === 'violations' ? 'Violations' : 'Compliant'}
            </button>
          ))}
        </div>

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

      {/* Scan in progress banner */}
      {scanning && (
        <div className="scan-banner" role="status" aria-live="polite">
          <span className="scan-banner-dot" aria-hidden="true" />
          Scan in progress — results will update automatically
        </div>
      )}

      {/* Table area */}
      <div className="results-wrap">
        {error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : !loading && groups.length === 0 ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No results yet</p>
            <p className="empty-body">
              No scan has been run. Use Run Scan to begin compliance monitoring.
            </p>
          </div>
        ) : (
          <table className="results-table" aria-label="DNS compliance results">
            <thead>
              <tr>
                <th className="col-expand" scope="col" />
                <th className="col-domain" scope="col">Domain</th>
                <th className="col-status" scope="col">Status</th>
                <th className="col-ip" scope="col">Resolved IP</th>
                <th className="col-evidence" scope="col">Evidence</th>
                <th className="col-last-scanned" scope="col">Last scanned</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <SkeletonRows />
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={5}>
                    <div className="empty-state" style={{ padding: '3rem 0' }}>
                      <p className="empty-heading">No results match the current filters</p>
                    </div>
                  </td>
                </tr>
              ) : (
                filtered.map(group => (
                  <URLGroupRow
                    key={group.url}
                    group={group}
                    expanded={expanded.has(group.url)}
                    onToggle={() => toggleExpanded(group.url)}
                  />
                ))
              )}
            </tbody>
          </table>
        )}
      </div>
    </>
  )
}
