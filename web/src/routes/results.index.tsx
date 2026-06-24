import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchResults, groupResults, lastScanTime } from '../api/results'
import { useScan } from './__root'

export const Route = createFileRoute('/results/')({ component: ResultsPage })

/* ─── Results Page ───────────────────────────────────────────────────────── */

function ResultsPage() {
  const { scanning, refreshSignal } = useScan()

  const [lastScan, setLastScan] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setError(null)
      const raw = await fetchResults()
      setLastScan(lastScanTime(groupResults(raw)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load results')
    }
  }, [])

  useEffect(() => { load() }, [load, refreshSignal])

  const subtitle = useMemo(() => (lastScan ? `Last scan: ${lastScan}` : null), [lastScan])

  return (
    <div className="mx-20">
      {/* Page header */}
      <div className="page-header">
        <h1 className="page-title">Compliance Results</h1>
        {subtitle && (
          <p className="page-subtitle">{subtitle}</p>
        )}
      </div>

      {/* Scan in progress banner */}
      {scanning && (
        <div className="scan-banner" role="status" aria-live="polite">
          <span className="scan-banner-dot" aria-hidden="true" />
          Scan in progress — results will update automatically
        </div>
      )}

      {error && (
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button className="btn-primary" onClick={load}>Retry</button>
        </div>
      )}
    </div>
  )
}
