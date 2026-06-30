import { useCallback, useEffect, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { ArrowLeftIcon } from 'lucide-react'
import { fetchISPStats } from '@/api/isps'
import type { ISPStats } from '@/api/types'
import { Table, TableBody, TableRow, TableCell, TableHead, TableHeader } from '@/components/ui/table'

export const Route = createFileRoute('/isps/$isp')({ component: ISPDetailPage })

function ISPDetailPage() {
  const { isp } = Route.useParams()
  const [stats, setStats] = useState<ISPStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setError(null)
      setStats(await fetchISPStats(isp))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [isp])

  useEffect(() => { load() }, [load])

  return (
    <div className="mx-60">
      <Link to="/" className="back-link mt-8">
        <ArrowLeftIcon className="back-link-icon" />
        Overview
      </Link>

      <div className="page-header px-0">
        <h1 className="page-title mb-2">{decodeURIComponent(isp)}</h1>
        {stats && (
          <p className="page-subtitle">{stats.servers.length} {stats.servers.length === 1 ? 'server' : 'servers'}</p>
        )}
      </div>

      {/* Most violated domain */}
      {!loading && stats?.most_violated_domain && (
        <div className="dash-section">
          <p className="dash-label">Most Non-Compliant Domain</p>
          <Link
            to="/results/$url"
            params={{ url: stats.most_violated_domain }}
            className="server-name"
          >
            {stats.most_violated_domain}
          </Link>
        </div>
      )}

      {/* Per-server table */}
      <div className="dash-section">
        <p className="dash-label">DNS Servers</p>
        {loading ? (
          <div className="dash-table-wrap">
            {[1, 2, 3].map(i => (
              <div key={i} className="dash-skeleton-row">
                <span className="skeleton" style={{ width: 160, height: 13 }} />
                <span className="skeleton" style={{ flex: 1, maxWidth: 200, height: 4 }} />
                <span className="skeleton" style={{ width: 80, height: 13 }} />
              </div>
            ))}
          </div>
        ) : error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : (
          <Table className="server-table" aria-label={`DNS servers for ${decodeURIComponent(isp)}`}>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">Server</TableHead>
                <TableHead scope="col">Compliance</TableHead>
                <TableHead scope="col">Violations</TableHead>
                <TableHead scope="col">Avg Latency</TableHead>
                <TableHead scope="col">Min / Max</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats?.servers.map(s => {
                const pct = s.total > 0 ? Math.round((s.compliant / s.total) * 100) : 0
                const violations = s.total - s.compliant
                const hasLatency = s.avg_latency_ms > 0

                return (
                  <TableRow key={s.dns_server.id}>
                    <TableCell>
                      <span className="server-name">{s.dns_server.name}</span>
                      <span className="text-xs text-muted ml-2">{s.dns_server.protocol.toUpperCase()}</span>
                    </TableCell>
                    <TableCell>
                      <div className="server-bar-wrap">
                        <div className="server-bar" role="presentation">
                          <div className="server-bar-fill" style={{ width: `${pct}%` }} />
                        </div>
                        <span className="server-count">{s.compliant} / {s.total}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      {violations > 0 ? (
                        <span className="label-violation">{violations} {violations === 1 ? 'violation' : 'violations'}</span>
                      ) : (
                        <span className="label-compliant">All compliant</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {hasLatency ? (
                        <span className="ip-value">{s.avg_latency_ms.toFixed(1)} ms</span>
                      ) : (
                        <span className="empty-cell">—</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {hasLatency ? (
                        <span className="ip-value">{s.min_latency_ms} / {s.max_latency_ms} ms</span>
                      ) : (
                        <span className="empty-cell">—</span>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </div>
    </div>
  )
}
