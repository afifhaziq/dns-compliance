import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import { Input } from '@/components/ui/input'
import type { Period } from '@/lib/period'

// This week / Last week / Custom (two inclusive dates). Shared by the ISP
// page's unblocked table and the Overview's all-ISP export.
export function PeriodPicker({ period, from, to, onChange }: {
  period: Period
  from?: string
  to?: string
  onChange: (period: Period, from?: string, to?: string) => void
}) {
  return (
    <>
      <ToggleGroup
        type="single"
        value={period}
        onValueChange={v => { if (v) onChange(v as Period, from, to) }}
        variant="outline"
        aria-label="Period"
      >
        <ToggleGroupItem value="week">This week</ToggleGroupItem>
        <ToggleGroupItem value="last-week">Last week</ToggleGroupItem>
        <ToggleGroupItem value="custom">Custom</ToggleGroupItem>
      </ToggleGroup>
      {period === 'custom' && (
        <div className="flex items-center gap-2">
          <Input type="date" aria-label="From" value={from ?? ''} max={to} onChange={e => onChange('custom', e.target.value, to)} className="w-auto" />
          <span className="dash-label mb-0">to</span>
          <Input type="date" aria-label="To" value={to ?? ''} min={from} onChange={e => onChange('custom', from, e.target.value)} className="w-auto" />
        </div>
      )}
    </>
  )
}
