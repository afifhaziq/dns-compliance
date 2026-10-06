import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { fetchISPStats, fetchISPTiming, fetchISPTrend, fetchISPUnblocked } from '@/api/isps'
import { fetchISPLogos } from '@/api/isp-logos'
import { fetchResurfacedDomains } from '@/api/results'
import type { ISPStats, ISPTiming, ISPTrendStat, ResurfacedDomain } from '@/api/types'
import { Table, TableBody, TableRow, TableCell, TableHead, TableHeader } from '@/components/ui/table'
import { ISPLogoChip } from '@/components/isp-logo-chip'
import { ISPUnblockedTable } from '@/components/isp-unblocked-table'
import { periodRange, previousRange, type Period } from '@/lib/period'
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'
import { Grid } from '@/components/charts/grid'
import { XAxis } from '@/components/charts/x-axis'
import { ChartTooltip } from '@/components/charts/tooltip'

type ISPSearch = { period?: Period; from?: string; to?: string }

export const Route = createFileRoute('/isps/$isp')({
  validateSearch: (search: Record<string, unknown>): ISPSearch => ({
    period: search.period === 'last-week' || search.period === 'custom' ? search.period : undefined,
    from: typeof search.from === 'string' ? search.from : undefined,
    to: typeof search.to === 'string' ? search.to : undefined,
  }),
  component: ISPDetailPage,
})

const PERIOD_LABEL: Record<Period, string> = { week: 'this week', 'last-week': 'last week', custom: 'in range' }

function ISPDetailPage() {
  const { isp } = Route.useParams()
  const { period = 'week', from, to } = Route.useSearch()
  const navigate = Route.useNavigate()
  const [stats, setStats] = useState<ISPStats | null>(null)
  const [timing, setTiming] = useState<ISPTiming | null>(null)
  const [trend, setTrend] = useState<ISPTrendStat[]>([])
  const [resurfaced, setResurfaced] = useState<ResurfacedDomain[]>([])
  const [logoUrl, setLogoUrl] = useState<string | undefined>()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [unblockedTotal, setUnblockedTotal] = useState<number | null>(null)
  const [previous, setPrevious] = useState<{ key: string; total: number } | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      const [statsData, trendData, timingData, resurfacedData] = await Promise.all([
        fetchISPStats(isp),
        fetchISPTrend(isp, 30),
        fetchISPTiming(isp),
        fetchResurfacedDomains(),
      ])
      setStats(statsData)
      setTrend(trendData)
      setTiming(timingData)
      setResurfaced(resurfacedData)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [isp])

  useEffect(() => { load() }, [load])

  useEffect(() => {
    fetchISPLogos()
      .then(logos => setLogoUrl(logos.find(l => l.isp === isp)?.logo_url))
      .catch(() => {})
  }, [isp])

  // Same-length window before the selected one, for the delta in the summary.
  const periodKey = `${isp}|${period}|${from}|${to}`
  useEffect(() => {
    let cancelled = false
    fetchISPUnblocked(isp, { ...previousRange(periodRange(period, from, to)), pageSize: 1 })
      .then(res => { if (!cancelled) setPrevious({ key: periodKey, total: res.total }) })
      .catch(() => {})
    return () => { cancelled = true }
  }, [isp, period, from, to, periodKey])
  const previousTotal = previous?.key === periodKey ? previous.total : null

  const setPeriod = useCallback((p: Period, f?: string, t?: string) => {
    navigate({
      search: p === 'custom' ? { period: p, from: f, to: t } : p === 'week' ? {} : { period: p },
      replace: true,
    })
  }, [navigate])

  // A domain rolls up multiple servers; only the ones matching this ISP matter here.
  const resurfacedForThisISP = useMemo(() =>
    resurfaced
      .map(d => ({ ...d, affected_servers: d.affected_servers.filter(s => s.isp === isp) }))
      .filter(d => d.affected_servers.length > 0),
    [resurfaced, isp]
  )
  const resurfacedSet = useMemo(() => new Set(resurfacedForThisISP.map(d => d.url)), [resurfacedForThisISP])

  const trendChartData = useMemo(() =>
    trend.map(s => ({
      date: new Date(s.day),
      compliance: s.total > 0 ? Math.round((s.compliant / s.total) * 100) : 0,
    })),
    [trend]
  )

  const servers = stats?.servers ?? []
  const compliant = servers.reduce((sum, s) => sum + s.compliant, 0)
  const checks = servers.reduce((sum, s) => sum + s.total, 0)
  const pct = checks > 0 ? Math.round((compliant / checks) * 100) : null
  const delta = unblockedTotal != null && previousTotal != null ? unblockedTotal - previousTotal : null

  return (
    <div className="mx-20 mb-20">
      <Breadcrumbs items={[{ label: 'Overview', to: '/' }, { label: isp }]} />

      <div className="page-header px-0">
        <div className="inline-flex items-center gap-3 mb-2">
          <ISPLogoChip isp={isp} logoUrl={logoUrl} size={28} background="white" />
          <h1 className="page-title mb-0">{isp}</h1>
        </div>
        {stats && (
          <p className="page-subtitle">{servers.length} {servers.length === 1 ? 'DNS server' : 'DNS servers'}</p>
        )}
      </div>

      {error ? (
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button className="btn-primary" onClick={load}>Retry</button>
        </div>
      ) : (
        <>
          {/* Summary: facts in one line, the unblocked count first since it's what this page is for */}
          <div className="dash-section mt-2">
            <dl className="flex flex-wrap gap-x-10 gap-y-3">
              <div>
                <dt className="dash-label">Not blocked {PERIOD_LABEL[period]}</dt>
                <dd className="server-count mt-1" style={{ color: 'var(--ink)', fontSize: '1.125rem' }}>
                  {unblockedTotal == null ? '—' : unblockedTotal.toLocaleString()}
                  {delta != null && delta !== 0 && (
                    <span className="dash-label mb-0 ml-2">
                      {delta > 0 ? '+' : '−'}{Math.abs(delta).toLocaleString()} vs previous
                    </span>
                  )}
                </dd>
              </div>
              <div>
                <dt className="dash-label">Compliance (latest scan)</dt>
                <dd className="server-count mt-1" style={{ color: 'var(--ink)', fontSize: '1.125rem' }}>
                  {loading || pct == null ? '—' : `${pct}%`}
                  {!loading && checks > 0 && <span className="dash-label mb-0 ml-2">{compliant.toLocaleString()} / {checks.toLocaleString()} checks</span>}
                </dd>
              </div>
              {timing && timing.with_due_date_count > 0 && (
                <>
                  <div>
                    <dt className="dash-label">Median time to block</dt>
                    <dd className="server-count mt-1" style={{ color: 'var(--ink)', fontSize: '1.125rem' }}>{timing.median_days_to_block.toFixed(1)} days</dd>
                  </div>
                  <div>
                    <dt className="dash-label">Past due, still open</dt>
                    <dd className="server-count mt-1" style={{ color: 'var(--ink)', fontSize: '1.125rem' }}>
                      {timing.still_open_count.toLocaleString()}
                      <span className="dash-label mb-0 ml-2">of {timing.with_due_date_count.toLocaleString()} with a due date</span>
                    </dd>
                  </div>
                </>
              )}
              <div>
                <dt className="dash-label">Resurfaced</dt>
                <dd className="server-count mt-1" style={{ color: 'var(--ink)', fontSize: '1.125rem' }}>{loading ? '—' : resurfacedForThisISP.length}</dd>
              </div>
            </dl>
          </div>

          {!loading && trendChartData.length >= 2 && (
            <div className="dash-section mt-10">
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

          <div className="dash-section mt-10">
            <p className="section-title mb-1">Domains Not Blocked</p>
            <p className="text-sm text-stone-muted mb-4" style={{ maxWidth: '70ch' }}>
              Requested domains that still resolved on at least one {isp} DNS server in its latest scan of the period. Uplifted and suspended cases are excluded.
            </p>
            <ISPUnblockedTable
              isp={isp}
              period={period}
              from={from}
              to={to}
              serverCount={servers.length}
              resurfaced={resurfacedSet}
              onPeriodChange={setPeriod}
              onTotal={setUnblockedTotal}
            />
          </div>

          <div className="dash-section mt-10">
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
            ) : (
              <Table className="server-table" aria-label={`DNS servers for ${isp}`}>
                <TableHeader>
                  <TableRow>
                    <TableHead scope="col">Server</TableHead>
                    <TableHead scope="col">Address</TableHead>
                    <TableHead scope="col">Compliance</TableHead>
                    <TableHead scope="col">Violations</TableHead>
                    <TableHead scope="col">Avg Latency</TableHead>
                    <TableHead scope="col">Min / Max</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {servers.map(s => {
                    const serverPct = s.total > 0 ? Math.round((s.compliant / s.total) * 100) : 0
                    const violations = s.total - s.compliant
                    const hasLatency = s.avg_latency_ms > 0

                    return (
                      <TableRow key={s.dns_server.id}>
                        <TableCell>
                          <span className="server-name">{s.dns_server.name}</span>
                          <span className="text-xs text-muted ml-2">{(s.dns_server.protocol || 'UDP').toUpperCase()}</span>
                        </TableCell>
                        <TableCell><span className="ip-value">{s.dns_server.address}</span></TableCell>
                        <TableCell>
                          <div className="server-bar-wrap">
                            <div className="server-bar" role="presentation">
                              <div className="server-bar-fill" style={{ width: `${serverPct}%` }} />
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
                          {hasLatency ? <span className="ip-value">{s.avg_latency_ms.toFixed(1)} ms</span> : <span className="empty-cell">—</span>}
                        </TableCell>
                        <TableCell>
                          {hasLatency ? <span className="ip-value">{s.min_latency_ms} / {s.max_latency_ms} ms</span> : <span className="empty-cell">—</span>}
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            )}
          </div>

          {!loading && resurfacedForThisISP.length > 0 && (
            <div className="dash-section mt-10">
              <p className="section-title mb-1">Resurfaced Domains</p>
              <p className="text-sm text-stone-muted mb-4">Blocked in an earlier scan, resolving again in the latest one.</p>
              <Table className="server-table" aria-label={`Resurfaced domains on ${isp}`}>
                <TableHeader>
                  <TableRow>
                    <TableHead scope="col">Domain</TableHead>
                    <TableHead scope="col">Servers</TableHead>
                    <TableHead scope="col">Resurfaced</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {resurfacedForThisISP.map(d => {
                    const latest = d.affected_servers.reduce((max, s) => s.resurfaced_at > max ? s.resurfaced_at : max, '')
                    return (
                      <TableRow key={d.url}>
                        <TableCell>
                          <Link to="/domain/$url" params={{ url: d.url }} search={{ tab: 'overview' }} className="ip-value">{d.url}</Link>
                        </TableCell>
                        <TableCell><span className="server-count">{d.affected_servers.map(s => s.dns_server_name).join(', ')}</span></TableCell>
                        <TableCell><span className="server-count">{latest ? new Date(latest).toLocaleString() : '—'}</span></TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </>
      )}
    </div>
  )
}
