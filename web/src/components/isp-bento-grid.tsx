import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Minus, TrendingDown, TrendingUp } from 'lucide-react'
import { fetchISPStats, fetchISPTiming, fetchISPTrend } from '@/api/isps'
import type { ISPStats, ISPTiming, ScanResult } from '@/api/types'
import { AnimateIcon } from '@/components/animate-ui/icons/icon'
import { ChevronRightIcon } from '@/components/animate-ui/icons/chevron-right'
import { useChart } from '@/components/charts/chart-context'
import { Gauge } from '@/components/charts/gauge'
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'

type TrendPoint = { date: Date; compliance: number }

type ISPCardData = {
  isp: string
  stats: ISPStats
  timing: ISPTiming
  trend: TrendPoint[]
  pct: number
  avgLatency: number | null
}

export function getISPNames(results: ScanResult[]): string[] {
  return Array.from(new Set(results.map(r => r.dns_server.isp))).sort()
}

async function loadISPCard(isp: string): Promise<ISPCardData> {
  const [stats, timing, trendData] = await Promise.all([
    fetchISPStats(isp),
    fetchISPTiming(isp),
    fetchISPTrend(isp, 30),
  ])

  const totalCompliant = stats.servers.reduce((sum, s) => sum + s.compliant, 0)
  const totalChecks = stats.servers.reduce((sum, s) => sum + s.total, 0)
  const pct = totalChecks > 0 ? Math.round((totalCompliant / totalChecks) * 100) : 0

  const latencies = stats.servers.map(s => s.avg_latency_ms).filter(ms => ms > 0)
  const avgLatency = latencies.length > 0 ? latencies.reduce((a, b) => a + b, 0) / latencies.length : null

  const trend = trendData.map(t => ({
    date: new Date(t.day),
    compliance: t.total > 0 ? Math.round((t.compliant / t.total) * 100) : 0,
  }))

  return { isp, stats, timing, trend, pct, avgLatency }
}

export function ISPBentoSkeleton({ count }: { count: number }) {
  return (
    <div className="bento-grid">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="bento-card">
          <span className="skeleton" style={{ width: 120, height: 14 }} />
          <span className="skeleton" style={{ width: '100%', height: 64 }} />
          <span className="skeleton" style={{ width: 160, height: 12 }} />
        </div>
      ))}
    </div>
  )
}

export function ISPBentoGrid({ results }: { results: ScanResult[] }) {
  const isps = useMemo(() => getISPNames(results), [results])
  const [cards, setCards] = useState<ISPCardData[]>([])
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    const loaded = await Promise.all(isps.map(loadISPCard))
    setCards(loaded)
    setLoading(false)
  }, [isps])

  useEffect(() => { load() }, [load])

  if (loading) return <ISPBentoSkeleton count={isps.length || 4} />

  return (
    <div className="bento-grid">
      {cards.map(card => <ISPCard key={card.isp} data={card} />)}
    </div>
  )
}

function ISPCard({ data }: { data: ISPCardData }) {
  const { isp, stats, timing, trend, pct, avgLatency } = data
  const serverCount = stats.servers.length

  return (
    <div className="bento-card">
      <div className="bento-card-header">
        <AnimateIcon animateOnHover asChild>
          <Link to="/isps/$isp" params={{ isp }} className="server-name inline-flex items-center gap-1">
            {isp}
            <ChevronRightIcon size={14} />
          </Link>
        </AnimateIcon>
        <span className="dash-label mb-0">{serverCount} {serverCount === 1 ? 'server' : 'servers'}</span>
      </div>

      <div>
        <p className="server-count" style={{ color: 'var(--ink)' }}>{pct}%</p>
        <p className="dash-label mb-0">Compliant</p>
      </div>

      <Gauge
        orientation="linear"
        value={pct}
        totalNotches={100}
        linearHeight={20}
        activeFill="var(--ink)"
        inactiveFill="var(--stone-border)"
        enterStaggerScale={0.25}
      />

      <div className="bento-stat-row">
        <div>
          <p className="server-count">{avgLatency != null ? `${avgLatency.toFixed(1)} ms` : '—'}</p>
          <p className="dash-label mb-0">Avg latency</p>
        </div>
        {stats.most_violated_domain && (
          <div className="text-right">
            <Link
              to="/domain/$url"
              params={{ url: stats.most_violated_domain }}
              search={{ tab: 'overview' }}
              className="server-count"
              style={{ overflowWrap: 'anywhere' }}
            >
              {stats.most_violated_domain}
            </Link>
            <p className="dash-label mb-0">Most violated</p>
          </div>
        )}
      </div>

      {timing.with_order_date_count > 0 && (
        <div className="bento-stat-row">
          <div>
            <p className="server-count">{timing.median_days_to_block.toFixed(1)} days</p>
            <p className="dash-label mb-0">Median time to block</p>
          </div>
          <div className="text-right">
            <p className="server-count label-violation">{timing.still_open_count}</p>
            <p className="dash-label mb-0">Still open</p>
          </div>
        </div>
      )}

      {trend.length >= 2 && <TrendSparkline trend={trend} />}
    </div>
  )
}

type HoverPoint = { date: Date; compliance: number }

function TrendSparkline({ trend }: { trend: TrendPoint[] }) {
  const delta = trend[trend.length - 1].compliance - trend[0].compliance
  const deltaClass = delta > 0 ? 'label-compliant' : delta < 0 ? 'label-violation' : ''
  const DeltaIcon = delta > 0 ? TrendingUp : delta < 0 ? TrendingDown : Minus

  const [hover, setHover] = useState<HoverPoint | null>(null)

  return (
    <div className="bento-trend">
      <div className="bento-trend-header">
        {hover ? (
          <>
            <span className="dash-label mb-0">
              {hover.date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}
            </span>
            <span className="server-count">{hover.compliance}%</span>
          </>
        ) : (
          <>
            <span className="dash-label mb-0">30-day trend</span>
            <span className={`server-count flex items-center gap-1 ${deltaClass}`}>
              <DeltaIcon size={12} />
              {Math.abs(delta)}%
            </span>
          </>
        )}
      </div>
      <LineChart
        data={trend}
        xDataKey="date"
        aspectRatio="4 / 1"
        margin={{ top: 4, right: 4, bottom: 4, left: 4 }}
      >
        <TrendHoverBridge onHoverChange={setHover} />
        <Line dataKey="compliance" stroke="var(--ink)" strokeWidth={1.5} fadeEdges={false} showHighlight />
      </LineChart>
    </div>
  )
}

/** Headless: syncs the chart's hovered point into the card header above it. */
function TrendHoverBridge({ onHoverChange }: { onHoverChange: (point: HoverPoint | null) => void }) {
  const { tooltipData } = useChart()

  useEffect(() => {
    const point = tooltipData?.point
    const date = point?.date
    const compliance = point?.compliance
    if (date instanceof Date && typeof compliance === 'number') {
      onHoverChange({ date, compliance })
    } else {
      onHoverChange(null)
    }
  }, [tooltipData, onHoverChange])

  return null
}
