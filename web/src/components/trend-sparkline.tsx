import { useEffect, useState } from 'react'
import { Minus, TrendingDown, TrendingUp } from 'lucide-react'
import { curveStepAfter } from '@visx/curve'
import { useChart } from '@/components/charts/chart-context'
import { LineChart } from '@/components/charts/line-chart'
import { Line } from '@/components/charts/line'

export type TrendPoint = { date: Date; compliance: number }

type TrendSparklineProps = {
  trend: TrendPoint[]
  label?: string
  /** 'percentage' (default) shows a %/delta trend. 'status' treats compliance
   *  as binary (0 or 100) and reads it as Up/Down instead. */
  mode?: 'percentage' | 'status'
}

export function TrendSparkline({ trend, label = '30-day trend', mode = 'percentage' }: TrendSparklineProps) {
  const isStatus = mode === 'status'
  const downDays = trend.filter(t => t.compliance === 0).length

  const delta = trend[trend.length - 1].compliance - trend[0].compliance
  const deltaClass = delta > 0 ? 'label-compliant' : delta < 0 ? 'label-violation' : ''
  const DeltaIcon = delta > 0 ? TrendingUp : delta < 0 ? TrendingDown : Minus

  const [hover, setHover] = useState<TrendPoint | null>(null)

  return (
    <div className="bento-trend">
      <div className="bento-trend-header">
        {hover ? (
          <>
            <span className="dash-label mb-0">
              {hover.date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}
            </span>
            {isStatus ? (
              <span className={`server-count ${hover.compliance === 100 ? 'label-compliant' : 'label-violation'}`}>
                {hover.compliance === 100 ? 'Up' : 'Down'}
              </span>
            ) : (
              <span className="server-count">{hover.compliance}%</span>
            )}
          </>
        ) : isStatus ? (
          <>
            <span className="dash-label mb-0">{label}</span>
            <span className={`server-count ${downDays > 0 ? 'label-violation' : 'label-compliant'}`}>
              {downDays > 0 ? `${downDays} ${downDays === 1 ? 'day' : 'days'} down` : 'All up'}
            </span>
          </>
        ) : (
          <>
            <span className="dash-label mb-0">{label}</span>
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
        margin={{ top: 4, right: 4, left: 4 }}
      >
        <TrendHoverBridge onHoverChange={setHover} />
        <Line
          dataKey="compliance"
          stroke="var(--ink)"
          strokeWidth={1.5}
          fadeEdges={false}
          showHighlight
          curve={isStatus ? curveStepAfter : undefined}
        />
      </LineChart>
    </div>
  )
}

/** Headless: syncs the chart's hovered point into the card header above it. */
function TrendHoverBridge({ onHoverChange }: { onHoverChange: (point: TrendPoint | null) => void }) {
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
