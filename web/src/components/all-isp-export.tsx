import { useState } from 'react'
import { exportAllISPUnblocked } from '@/api/isps'
import { downloadBlob } from '@/lib/download'
import { periodRange, type Period } from '@/lib/period'
import { PeriodPicker } from '@/components/period-picker'
import { DownloadIcon } from '@/components/animate-ui/icons/download'
import { Button } from '@/components/ui/button'

// Overview toolbar: download every ISP's not-blocked domains for a period
// as one workbook (Summary, Matrix, one sheet per ISP).
export function AllISPExport() {
  const [period, setPeriod] = useState<Period>('week')
  const [from, setFrom] = useState<string | undefined>()
  const [to, setTo] = useState<string | undefined>()
  const [exporting, setExporting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const incompleteCustom = period === 'custom' && !(from && to)

  const handleExport = async () => {
    setExporting(true)
    setError(null)
    try {
      const { since, until } = periodRange(period, from, to)
      const { blob, filename } = await exportAllISPUnblocked(since, until)
      downloadBlob(blob, filename ?? `unblocked-all-isps-${new Date().toISOString().slice(0, 10)}.xlsx`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Export failed')
    } finally {
      setExporting(false)
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-3">
      {error && <p className="error-message mb-0">{error}</p>}
      <PeriodPicker
        period={period}
        from={from}
        to={to}
        onChange={(p, f, t) => { setPeriod(p); setFrom(f); setTo(t) }}
      />
      <Button
        variant="outline"
        onClick={handleExport}
        disabled={exporting || incompleteCustom}
        title="Summary, a domain × ISP matrix, and one sheet per ISP"
      >
        <DownloadIcon size={16} />
        {exporting ? 'Exporting…' : 'Export not blocked (all ISPs)'}
      </Button>
    </div>
  )
}
