import { useState } from 'react'
import { exportAllISPUnblocked } from '@/api/isps'
import { downloadBlob } from '@/lib/download'
import { DownloadIcon } from '@/components/animate-ui/icons/download'
import { Button } from '@/components/ui/button'

// "2026-10-06_1720" in GMT+8, matching the server's filename.
function gmt8Stamp(): string {
  const iso = new Date(Date.now() + 8 * 60 * 60 * 1000).toISOString()
  return `${iso.slice(0, 10)}_${iso.slice(11, 13)}${iso.slice(14, 16)}`
}

// Overview: download every ISP's not-blocked domains for the period picked
// beside the compliance trend as one workbook (Summary, DNS Servers, one
// sheet per ISP).
export function AllISPExport({ since, until, disabled }: { since: Date; until: Date; disabled?: boolean }) {
  const [exporting, setExporting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleExport = async () => {
    setExporting(true)
    setError(null)
    try {
      const { blob, filename } = await exportAllISPUnblocked(since, until)
      downloadBlob(blob, filename ?? `isp_weekly_report-${gmt8Stamp()}.xlsx`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Export failed')
    } finally {
      setExporting(false)
    }
  }

  return (
    <>
      {error && <p className="error-message mb-0">{error}</p>}
      <Button
        variant="outline"
        size="icon"
        onClick={handleExport}
        disabled={exporting || disabled}
        aria-label={exporting ? 'Exporting…' : 'Export not-blocked domains for all ISPs'}
        title={exporting ? 'Exporting…' : 'Export not-blocked domains for all ISPs (Summary, DNS Servers, one sheet per ISP)'}
      >
        <DownloadIcon size={16} />
      </Button>
    </>
  )
}
