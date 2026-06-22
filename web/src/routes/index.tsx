import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchResults, groupResults, lastScanTime } from '../api/results'
import { fetchUrlCount } from '../api/urls'
import { fetchDnsServerCount } from '../api/dns-servers'
import type { ScanResult } from '../api/types'
import { useScan } from './__root'
import {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardImage,
} from '@/components/animate-ui/components/base/preview-link-card'

export const Route = createFileRoute('/')({ component: DashboardPage })

/* ─── Derived types ──────────────────────────────────────────────────────── */

type ServerStat    = { name: string; compliant: number; total: number }
type ViolationEntry = { url: string; hostname: string; servers: string[] }

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

function computeViolations(results: ScanResult[]): ViolationEntry[] {
  const map = new Map<string, ViolationEntry>()
  for (const r of results) {
    if (!r.compliant) {
      const hostname = (() => { try { return new URL(r.url).hostname } catch { return r.url } })()
      const entry = map.get(r.url) ?? { url: r.url, hostname, servers: [] }
      entry.servers.push(r.dns_server.name)
      map.set(r.url, entry)
    }
  }
  return Array.from(map.values())
    .sort((a, b) => b.servers.length - a.servers.length)
    .slice(0, 8)
}

/* ─── Skeleton ───────────────────────────────────────────────────────────── */

function SkeletonRows({ count }: { count: number }) {
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

/* ─── Violations List ────────────────────────────────────────────────────── */

function ViolationsList({ violations }: { violations: ViolationEntry[] }) {
  return (
    <ul className="violations-list" aria-label="Active violations">
      {violations.map(v => (
        <li key={v.url} className="violation-item">
          <span className="status-dot dot-violation" aria-hidden="true" />
          <PreviewLinkCard href={v.url}>
              <PreviewLinkCardTrigger>
                <span className="violation-domain" title={v.url}>{v.hostname}</span>
              </PreviewLinkCardTrigger>
              <PreviewLinkCardPanel>
                <PreviewLinkCardImage />
              </PreviewLinkCardPanel>
            </PreviewLinkCard>
          <span className="violation-meta">
            {v.servers.length === 1 ? v.servers[0] : `${v.servers.length} servers`}
          </span>
        </li>
      ))}
    </ul>
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

  const serverStats = useMemo(() => computeServerStats(results), [results])
  const violations  = useMemo(() => computeViolations(results), [results])
  const lastScan    = useMemo(() => lastScanTime(groupResults(results)), [results])
  const hasResults  = results.length > 0

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
              <SkeletonRows count={3} />
            ) : !hasResults ? (
              <div className="dash-table-wrap dash-empty">
                <p className="dash-empty-heading">No scan data yet</p>
                <p className="dash-empty-body">Run a scan to see DNS server compliance status.</p>
              </div>
            ) : (
              <ServerStatusTable stats={serverStats} />
            )}
          </div>

          {/* Active Violations */}
          {(loading || hasResults) && (
            <div className="dash-section">
              <p className="dash-label">Active Violations</p>
              {loading ? (
                <SkeletonRows count={4} />
              ) : violations.length === 0 ? (
                <div className="dash-table-wrap dash-empty">
                  <p className="dash-empty-heading">No violations detected</p>
                  <p className="dash-empty-body">
                    All monitored domains are failing DNS resolution as expected.
                  </p>
                </div>
              ) : (
                <ViolationsList violations={violations} />
              )}
            </div>
          )}

        </div>
      )}
    </>
  )
}
