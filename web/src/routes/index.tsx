import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchNationalTrend, fetchResults, fetchResurfacedDomains, groupResults, lastScanTime } from '../api/results'
import { fetchUrlCount, fetchUrlsRequestedThisMonth } from '../api/urls'
import type { ISPTrendStat, ResurfacedDomain, ScanResult } from '../api/types'
import { useScan } from './__root'
import { ThinkingIndicator } from '@/components/ui/thinking-indicator'
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'
import { Grid } from '@/components/charts/grid'
import { XAxis } from '@/components/charts/x-axis'
import { ChartTooltip } from '@/components/charts/tooltip'
import { getISPNames, ISPBentoGrid, ISPBentoSkeleton } from '@/components/isp-bento-grid'

export const Route = createFileRoute('/')({ component: DashboardPage })

/* ─── Dashboard Page ─────────────────────────────────────────────────────── */

function DashboardPage() {
  const { scanning, refreshSignal } = useScan()

  const [results, setResults] = useState<ScanResult[]>([])
  const [urlCount, setUrlCount] = useState<number | null>(null)
  const [requestedThisMonth, setRequestedThisMonth] = useState<number | null>(null)
  const [trend, setTrend] = useState<ISPTrendStat[]>([])
  const [resurfaced, setResurfaced] = useState<ResurfacedDomain[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setError(null)
      const [raw, urls, requested, trendData, resurfacedData] = await Promise.all([
        fetchResults(),
        fetchUrlCount(),
        fetchUrlsRequestedThisMonth(),
        fetchNationalTrend(30),
        fetchResurfacedDomains(),
      ])
      setResults(raw)
      setUrlCount(urls)
      setRequestedThisMonth(requested)
      setTrend(trendData)
      setResurfaced(resurfacedData)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load, refreshSignal])

  const isps = useMemo(() => getISPNames(results), [results])
  const lastScan = useMemo(() => lastScanTime(groupResults(results)), [results])
  const hasResults = results.length > 0

  const nationalTotal = results.length
  const nationalCompliant = results.filter(r => r.compliant).length
  const nationalRate = nationalTotal > 0 ? Math.round((nationalCompliant / nationalTotal) * 100) : 0

  const trendChartData = useMemo(() =>
    trend.map(s => ({
      date: new Date(s.day),
      compliance: s.total > 0 ? Math.round((s.compliant / s.total) * 100) : 0,
    })),
    [trend]
  )

  const subtitleParts: string[] = []
  if (!loading) {
    const u = urlCount ?? 0
    if (u > 0) subtitleParts.push(`${u} ${u === 1 ? 'domain' : 'domains'}`)
    if (isps.length > 0) subtitleParts.push(`${isps.length} ${isps.length === 1 ? 'ISP' : 'ISPs'}`)
    if (lastScan) subtitleParts.push(`Last scan: ${lastScan}`)
  }

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title">Overview</h1>
        {subtitleParts.length > 0 && (
          <p className="page-subtitle">{subtitleParts.join(' · ')}</p>
        )}
      </div>

      {scanning && (
        <div className="scan-banner">
          <ThinkingIndicator className="p-0" />
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
          {!loading && hasResults && (
            <div className="dash-section mt-4">
              <p className="section-title mb-3">National Compliance</p>
              <div style={{ display: 'flex', gap: '2rem', flexWrap: 'wrap' }}>
                <div>
                  <p className="server-count" style={{ color: 'var(--ink)' }}>{nationalRate}%</p>
                  <p className="dash-label">Overall compliance</p>
                </div>
                <div>
                  <p className="server-count" style={{ color: 'var(--ink)' }}>{nationalCompliant} / {nationalTotal}</p>
                  <p className="dash-label">Checks compliant</p>
                </div>
                <div>
                  <p className="server-count" style={{ color: 'var(--ink)' }}>{requestedThisMonth ?? 0}</p>
                  <p className="dash-label">URLs requested this month</p>
                </div>
                <div>
                  <p className={resurfaced.length > 0 ? 'server-count label-violation' : 'server-count'} style={resurfaced.length > 0 ? undefined : { color: 'var(--ink)' }}>{resurfaced.length}</p>
                  <p className="dash-label">Resurfaced</p>
                </div>
              </div>
              {trendChartData.length >= 2 && (
                <div className="mt-4">
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
            </div>
          )}
          <div className="dash-section mt-4">
            <p className="section-title mb-3">ISP Compliance Status</p>
            {loading ? (
              <ISPBentoSkeleton count={4} />
            ) : !hasResults ? (
              <div className="dash-table-wrap dash-empty">
                <p className="dash-empty-heading">No scan data yet</p>
                <p className="dash-empty-body">Run a scan to see ISP compliance status.</p>
              </div>
            ) : (
              <ISPBentoGrid results={results} />
            )}
          </div>
        </div>
      )}
    </div>
  )
}
