import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { fetchResults, groupResults, lastScanTime } from '../api/results'
import { fetchUrlCount } from '../api/urls'
import type { ScanResult } from '../api/types'
import { useScan } from './__root'
import { Table, TableBody, TableRow, TableCell } from '@/components/ui/table'

export const Route = createFileRoute('/')({ component: DashboardPage })

/* ─── Derived types ──────────────────────────────────────────────────────── */

type ISPStat = { isp: string; compliant: number; total: number; serverCount: number }

/* ─── Computation ────────────────────────────────────────────────────────── */

function computeISPStats(results: ScanResult[]): ISPStat[] {
  const map = new Map<string, ISPStat>()
  const serversSeen = new Map<string, Set<string>>() // isp -> set of server names
  for (const r of results) {
    const isp = r.dns_server.isp
    const s = map.get(isp) ?? { isp, compliant: 0, total: 0, serverCount: 0 }
    s.total++
    if (r.compliant) s.compliant++
    map.set(isp, s)
    const seen = serversSeen.get(isp) ?? new Set()
    seen.add(r.dns_server.name)
    serversSeen.set(isp, seen)
  }
  // Set serverCount from unique server names
  for (const [isp, s] of map) {
    s.serverCount = serversSeen.get(isp)?.size ?? 0
  }
  return Array.from(map.values()).sort((a, b) => a.compliant / a.total - b.compliant / b.total)
}

/* ─── Skeleton ───────────────────────────────────────────────────────────── */

function ISPStatusSkeleton({ count }: { count: number }) {
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

/* ─── ISP Status Table ────────────────────────────────────────────────── */

function ISPStatusTable({ stats }: { stats: ISPStat[] }) {
  return (
    <Table className="server-table" aria-label="ISP compliance status">
      <TableBody>
        {stats.map(s => {
          const pct = s.total > 0 ? Math.round((s.compliant / s.total) * 100) : 0
          const allCompliant = s.compliant === s.total
          const violations = s.total - s.compliant

          return (
            <TableRow key={s.isp} className="server-row">
              <TableCell className="server-name-cell">
                <Link
                  to="/isps/$isp"
                  params={{ isp: s.isp }}
                  className="server-name"
                >
                  {s.isp}
                </Link>
              </TableCell>
              <TableCell className="server-bar-cell">
                <div className="server-bar-wrap">
                  <div className="server-bar" role="presentation">
                    <div className="server-bar-fill" style={{ width: `${pct}%` }} />
                  </div>
                  <span className="server-count">{s.compliant} / {s.total}</span>
                </div>
              </TableCell>
              <TableCell className="server-status-cell">
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
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}

/* ─── Dashboard Page ─────────────────────────────────────────────────────── */

function DashboardPage() {
  const { scanning, refreshSignal } = useScan()

  const [results, setResults] = useState<ScanResult[]>([])
  const [urlCount, setUrlCount] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setError(null)
      const [raw, urls] = await Promise.all([
        fetchResults(),
        fetchUrlCount(),
      ])
      setResults(raw)
      setUrlCount(urls)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load, refreshSignal])

  const ispStats = useMemo(() => computeISPStats(results), [results])
  const lastScan = useMemo(() => lastScanTime(groupResults(results)), [results])
  const hasResults = results.length > 0

  const subtitleParts: string[] = []
  if (!loading) {
    const u = urlCount ?? 0
    if (u > 0) subtitleParts.push(`${u} ${u === 1 ? 'domain' : 'domains'}`)
    if (ispStats.length > 0) subtitleParts.push(`${ispStats.length} ${ispStats.length === 1 ? 'ISP' : 'ISPs'}`)
    if (lastScan) subtitleParts.push(`Last scan: ${lastScan}`)
  }

  return (
    <div className="mx-20">
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
          <div className="dash-section mt-4">
            <p className="dash-label">ISP Compliance Status</p>
            {loading ? (
              <ISPStatusSkeleton count={3} />
            ) : !hasResults ? (
              <div className="dash-table-wrap dash-empty">
                <p className="dash-empty-heading">No scan data yet</p>
                <p className="dash-empty-body">Run a scan to see ISP compliance status.</p>
              </div>
            ) : (
              <ISPStatusTable stats={ispStats} />
            )}
          </div>
        </div>
      )}
    </div>
  )
}
