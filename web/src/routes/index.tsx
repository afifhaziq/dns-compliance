import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchResults, groupResults, lastScanTime } from '../api/results'
import { fetchUrlCount } from '../api/urls'
import { fetchDnsServerCount } from '../api/dns-servers'
import type { GroupedResult, ScanResult } from '../api/types'
import { useScan } from './__root'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardImage,
} from '@/components/animate-ui/components/base/preview-link-card'

export const Route = createFileRoute('/')({ component: DashboardPage })

/* ─── Derived types ──────────────────────────────────────────────────────── */

type ServerStat    = { name: string; compliant: number; total: number }
type StatusFilter  = 'all' | 'violations' | 'compliant'

/* ─── Computation ────────────────────────────────────────────────────────── */

function computeServerStats(results: ScanResult[]): ServerStat[] {
  const map = new Map<string, ServerStat>()
  for (const r of results) {
    const s = map.get(r.dns_server.name) ?? { name: r.dns_server.name, compliant: 0, total: 0 }
    s.total++
    if (r.compliant) s.compliant++
    map.set(r.dns_server.name, s)
  }
  return Array.from(map.values()).sort((a, b) => a.compliant / a.total - b.compliant / b.total)
}

/* ─── Helpers ────────────────────────────────────────────────────────────── */

function relativeTime(isoString: string): string {
  const diff = (Date.now() - new Date(isoString).getTime()) / 1000
  const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
  if (diff < 60) return rtf.format(-Math.round(diff), 'second')
  if (diff < 3600) return rtf.format(-Math.round(diff / 60), 'minute')
  if (diff < 86400) return rtf.format(-Math.round(diff / 3600), 'hour')
  return rtf.format(-Math.round(diff / 86400), 'day')
}

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
          <PreviewLinkCard href={url}>
            <PreviewLinkCardTrigger>
              <span className="hostname" title={url}>{hostname}</span>
            </PreviewLinkCardTrigger>
            <PreviewLinkCardPanel>
              <PreviewLinkCardImage />
            </PreviewLinkCardPanel>
          </PreviewLinkCard>
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
        <td className="col-last-scanned">
          {group.latestScannedAt ? (
            <span title={new Date(group.latestScannedAt).toLocaleString()}>
              {relativeTime(group.latestScannedAt)}
            </span>
          ) : (
            <span className="empty-cell">—</span>
          )}
        </td>
      </tr>
      <SubRows results={group.results} visible={expanded} />
    </>
  )
}

/* ─── Skeletons ──────────────────────────────────────────────────────────── */

function ServerStatusSkeleton({ count }: { count: number }) {
  return (
    <div className="dash-table-wrap">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="dash-skeleton-row">
          <span className="skeleton" style={{ width: 140, height: 13 }} />
          <span className="skeleton" style={{ flex: 1, maxWidth: 200, height: 4 }} />
          <span className="skeleton" style={{ width: 64, height: 13 }} />
        </div>
      ))}
    </div>
  )
}

function ResultsTableSkeleton() {
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

/* ─── Server Status Table ────────────────────────────────────────────────── */

function ServerStatusTable({ stats }: { stats: ServerStat[] }) {
  return (
    <table className="server-table" aria-label="DNS server compliance status">
      <tbody>
        {stats.map(s => {
          const pct         = s.total > 0 ? Math.round((s.compliant / s.total) * 100) : 0
          const allCompliant = s.compliant === s.total
          const violations  = s.total - s.compliant

          return (
            <tr key={s.name} className="server-row">
              <td className="server-name-cell">
                <span className="server-name">{s.name}</span>
              </td>
              <td className="server-bar-cell">
                <div className="server-bar-wrap">
                  <div className="server-bar" role="presentation">
                    <div className="server-bar-fill" style={{ width: `${pct}%` }} />
                  </div>
                  <span className="server-count">{s.compliant} / {s.total}</span>
                </div>
              </td>
              <td className="server-status-cell">
                {allCompliant ? (
                  <span className="status-dot-label">
                    <span className="status-dot dot-compliant" aria-hidden="true" />
                    <span className="label-compliant">All compliant</span>
                  </span>
                ) : (
                  <span className="status-dot-label">
                    <span className="status-dot dot-violation" aria-hidden="true" />
                    <span className="label-violation">
                      {violations} {violations === 1 ? 'violation' : 'violations'}
                    </span>
                  </span>
                )}
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

/* ─── Dashboard Page ─────────────────────────────────────────────────────── */

function DashboardPage() {
  const { scanning, refreshSignal } = useScan()

  const [results,     setResults]     = useState<ScanResult[]>([])
  const [urlCount,    setUrlCount]    = useState<number | null>(null)
  const [serverCount, setServerCount] = useState<number | null>(null)
  const [loading,     setLoading]     = useState(true)
  const [error,       setError]       = useState<string | null>(null)

  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [dnsFilter, setDnsFilter] = useState<string>('all')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const load = useCallback(async () => {
    try {
      setError(null)
      const [raw, urls, servers] = await Promise.all([
        fetchResults(),
        fetchUrlCount(),
        fetchDnsServerCount(),
      ])
      setResults(raw)
      setUrlCount(urls)
      setServerCount(servers)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
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

  const serverStats = useMemo(() => computeServerStats(results), [results])
  const groups       = useMemo(() => groupResults(results), [results])
  const lastScan      = useMemo(() => lastScanTime(groups), [groups])
  const hasResults    = results.length > 0

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

  const subtitleParts: string[] = []
  if (!loading) {
    const u = urlCount ?? 0
    const s = serverCount ?? 0
    if (u > 0) subtitleParts.push(`${u} ${u === 1 ? 'domain' : 'domains'}`)
    if (s > 0) subtitleParts.push(`${s} ${s === 1 ? 'server' : 'servers'}`)
    if (lastScan) subtitleParts.push(`Last scan: ${lastScan}`)
  }

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">Overview</h1>
        {subtitleParts.length > 0 && (
          <p className="page-subtitle">{subtitleParts.join(' · ')}</p>
        )}
      </div>

      {scanning && (
        <div
          className="scan-banner"
          role="status"
          aria-live="polite"
          style={{ marginTop: 'var(--sp-4)' }}
        >
          <span className="scan-banner-dot" aria-hidden="true" />
          Scan in progress — results will update automatically
        </div>
      )}

      {error ? (
        <div className="dash-section">
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        </div>
      ) : (
        <div className="dash-body">

          {/* DNS Server Status */}
          <div className="dash-section">
            <p className="dash-label">DNS Server Status</p>
            {loading ? (
              <ServerStatusSkeleton count={3} />
            ) : !hasResults ? (
              <div className="dash-table-wrap dash-empty">
                <p className="dash-empty-heading">No scan data yet</p>
                <p className="dash-empty-body">Run a scan to see DNS server compliance status.</p>
              </div>
            ) : (
              <ServerStatusTable stats={serverStats} />
            )}
          </div>

          {/* Compliance Results */}
          <div className="dash-section">
            <p className="dash-label">Compliance Results</p>

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
              {!loading && groups.length === 0 ? (
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
                      <ResultsTableSkeleton />
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
          </div>

        </div>
      )}
    </>
  )
}
