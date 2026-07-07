import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { fetchISPStats, fetchISPTiming, fetchISPTrend } from '@/api/isps'
import type { ISPStats, ISPTiming, ISPTrendStat } from '@/api/types'
import { Table, TableBody, TableRow, TableCell, TableHead, TableHeader } from '@/components/ui/table'
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'
import { Grid } from '@/components/charts/grid'
import { XAxis } from '@/components/charts/x-axis'
import { ChartTooltip } from '@/components/charts/tooltip'

export const Route = createFileRoute('/isps/$isp')({ component: ISPDetailPage })

function ISPDetailPage() {
  const { isp } = Route.useParams()
  const [stats, setStats] = useState<ISPStats | null>(null)
  const [timing, setTiming] = useState<ISPTiming | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [trend, setTrend] = useState<ISPTrendStat[]>([])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      const [statsData, trendData, timingData] = await Promise.all([
        fetchISPStats(isp),
        fetchISPTrend(isp, 30),
        fetchISPTiming(isp),
      ])
      setStats(statsData)
      setTrend(trendData)
      setTiming(timingData)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [isp])

  const trendChartData = useMemo(() =>
    trend.map(s => ({
      date: new Date(s.day),
      compliance: s.total > 0 ? Math.round((s.compliant / s.total) * 100) : 0,
    })),
    [trend]
  )

  useEffect(() => { load() }, [load])

  return (
    <div className="mx-20 mb-20">
      <Breadcrumbs items={[{ label: 'Overview', to: '/' }, { label: isp }]} />

      <div className="page-header px-0">
        <h1 className="page-title mb-2">{isp}</h1>
        {stats && (
          <p className="page-subtitle">{stats.servers.length} {stats.servers.length === 1 ? 'server' : 'servers'}</p>
        )}
      </div>

      {/* Most violated domain */}
      {!loading && stats?.most_violated_domain && (
        <div className="dash-section">
          <p className="section-title">Most Non-Compliant Domain</p>
          <div className="mb-4">
          <Link
            to="/domain/$url"
            params={{ url: stats.most_violated_domain }}
            search={{ tab: 'overview' }}
            className="server-name"
          >
            {stats.most_violated_domain}
          </Link>
          </div>
        </div>
      )}
      {/* Time to compliance */}
      {!loading && timing && timing.with_order_date_count > 0 && (
        <div className="dash-section">
          <p className="section-title mt-3 mb-3">Time to Compliance</p>
          <div style={{ display: 'flex', gap: '2rem', flexWrap: 'wrap' }}>
            <div>
              <p className="server-count" style={{ color: 'var(--ink)' }}>{timing.median_days_to_block.toFixed(1)} days</p>
              <p className="dash-label">Median time to block</p>
            </div>
            <div>
              <p className="server-count label-violation">{timing.still_open_count}</p>
              <p className="dash-label">Still open</p>
            </div>
            <div>
              <p className="server-count" style={{ color: 'var(--ink)' }}>{timing.with_order_date_count} / {timing.total_domains}</p>
              <p className="dash-label">Domains with order date</p>
            </div>
          </div>
        </div>
      )}

      {/* Compliance Trend */}
      {!loading && trendChartData.length >= 2 && (
        <div className="">
          <p className="section-title mb-3">Compliance Trend (last 30 days)</p>
          <LineChart
            data={trendChartData}
            xDataKey="date"
            aspectRatio="6 / 1"
            margin={{ top: 16, right: 40, bottom: 36, left: 40 }}
          >
            <Grid horizontal numTicksRows={4} />
            <XAxis numTicks={5} />
            <Line
              dataKey="compliance"
              stroke="var(--ink)"
              strokeWidth={2}
              showMarkers
              markers={{ radius: 3, fill: 'var(--ink)', stroke: 'var(--chart-background)', strokeWidth: 2 }}
              fadeEdges={false}
            />
            <ChartTooltip
              rows={(point) => [{
                color: 'var(--ink)',
                label: 'Compliance',
                value: `${point.compliance as number}%`,
              }]}
            />
          </LineChart>
        </div>
      )}

      {/* Per-server table */}
      <div className="dash-section">
        <p className="section-title mb-3">DNS Servers</p>
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
          <Table className="server-table" aria-label={`DNS servers for ${isp}`}>
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
                      <span className="text-xs text-muted ml-2">{(s.dns_server.protocol || 'UDP').toUpperCase()}</span>
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

      {/* Slowest domains to block */}
      {!loading && timing && timing.slowest.length > 0 && (
        <div className="dash-section">
          <p className="section-title mb-3">Slowest Domains to Block</p>
          <Table className="server-table" aria-label="Slowest domains to block">
            <TableHeader>
              <TableRow>
                <TableHead scope="col">Domain</TableHead>
                <TableHead scope="col">Days</TableHead>
                <TableHead scope="col">Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {timing.slowest.map(t => (
                <TableRow key={t.domain}>
                  <TableCell>
                    <Link to="/domain/$url" params={{ url: t.domain }} search={{ tab: 'overview' }} className="server-name">
                      {t.domain}
                    </Link>
                  </TableCell>
                  <TableCell><span className="ip-value">{t.days_to_block}</span></TableCell>
                  <TableCell>
                    {t.blocked ? (
                      <span className="label-compliant">Blocked</span>
                    ) : (
                      <span className="label-violation">Still open</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
