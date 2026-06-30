import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { ArrowLeftIcon } from 'lucide-react'
import { fetchResults } from '@/api/results'
import type { ScanResult } from '@/api/types'
import { useScan } from '@/routes/__root'
import { Table, TableBody, TableRow, TableCell, TableHead, TableHeader } from '@/components/ui/table'

export const Route = createFileRoute('/scan-results')({
  validateSearch: (search: Record<string, unknown>) => ({
    urls: Array.isArray(search.urls)
      ? (search.urls as string[])
      : search.urls
        ? [search.urls as string]
        : [],
    triggeredAt: typeof search.triggeredAt === 'string'
      ? search.triggeredAt
      : new Date().toISOString(),
  }),
  component: ScanResultsPage,
})

function StatusDot({ compliant }: { compliant: boolean }) {
  return (
    <span className="status-dot-label">
      <span className={`status-dot ${compliant ? 'dot-compliant' : 'dot-violation'}`} aria-hidden="true" />
      <span className={compliant ? 'label-compliant' : 'label-violation'}>
        {compliant ? 'Compliant' : 'Violation'}
      </span>
    </span>
  )
}

function ScanResultsPage() {
  const { urls, triggeredAt } = Route.useSearch()
  const { scanning, refreshSignal } = useScan()

  // 5-second buffer for clock skew between client trigger and server timestamp
  const triggerTime = useMemo(
    () => new Date(triggeredAt).getTime() - 5000,
    [triggeredAt]
  )

  // resultsByUrl: url -> ScanResult[] (only fresh results for this scan)
  const [resultsByUrl, setResultsByUrl] = useState<Map<string, ScanResult[]>>(new Map())
  const [fetchError, setFetchError] = useState<string | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const processFreshResults = useCallback((all: ScanResult[]) => {
    const fresh = all.filter(
      r => urls.includes(r.url) && new Date(r.scanned_at).getTime() >= triggerTime
    )
    if (fresh.length === 0) return
    const grouped = new Map<string, ScanResult[]>()
    for (const r of fresh) {
      const arr = grouped.get(r.url) ?? []
      arr.push(r)
      grouped.set(r.url, arr)
    }
    setResultsByUrl(prev => {
      const next = new Map(prev)
      for (const [url, results] of grouped) next.set(url, results)
      return next
    })
  }, [urls, triggerTime])

  // Poll during scan
  useEffect(() => {
    if (!scanning) return
    const poll = async () => {
      try {
        const all = await fetchResults()
        processFreshResults(all)
      } catch { /* transient errors during scan are expected */ }
    }
    poll() // immediate first poll
    pollRef.current = setInterval(poll, 3000)
    return () => {
      if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null }
    }
  }, [scanning, processFreshResults])

  // Final fetch when scan completes
  useEffect(() => {
    if (scanning) return
    const finalFetch = async () => {
      try {
        setFetchError(null)
        const all = await fetchResults()
        processFreshResults(all)
      } catch (err) {
        setFetchError(err instanceof Error ? err.message : 'Failed to load results')
      }
    }
    finalFetch()
  }, [refreshSignal, scanning, processFreshResults])

  const completedCount = resultsByUrl.size
  const totalViolations = useMemo(() => {
    let count = 0
    for (const results of resultsByUrl.values()) {
      count += results.filter(r => !r.compliant).length
    }
    return count
  }, [resultsByUrl])

  const ispViolations = useMemo(() => {
    const isps = new Set<string>()
    for (const results of resultsByUrl.values()) {
      for (const r of results) {
        if (!r.compliant) isps.add(r.dns_server.isp)
      }
    }
    return isps.size
  }, [resultsByUrl])

  if (urls.length === 0) {
    return (
      <div className="mx-60">
        <Link to="/" className="back-link mt-8">
          <ArrowLeftIcon className="back-link-icon" />
          Overview
        </Link>
        <div className="empty-state mt-8">
          <p className="empty-heading">No domains to display</p>
          <p className="empty-body">Start a scan from the Overview page.</p>
        </div>
      </div>
    )
  }

  return (
    <div className="mx-60">
      <Link to="/" className="back-link mt-8">
        <ArrowLeftIcon className="back-link-icon" />
        Overview
      </Link>

      <div className="page-header px-0">
        <h1 className="page-title mb-2">Scan Results</h1>
        <p className="page-subtitle">{urls.length} {urls.length === 1 ? 'domain' : 'domains'} selected</p>
      </div>

      {/* Progress bar */}
      {scanning && (
        <div className="dash-section">
          <div className="scan-banner" role="status" aria-live="polite">
            <span className="scan-banner-dot" aria-hidden="true" />
            Scanning… {completedCount} / {urls.length} {completedCount === 1 ? 'domain' : 'domains'} complete
          </div>
          <div className="server-bar-wrap mt-3">
            <div className="server-bar" role="progressbar" aria-valuenow={completedCount} aria-valuemax={urls.length}>
              <div
                className="server-bar-fill"
                style={{ width: `${urls.length > 0 ? (completedCount / urls.length) * 100 : 0}%`, transition: 'width 0.4s ease' }}
              />
            </div>
          </div>
        </div>
      )}

      {/* Summary stats (shown once any results arrive) */}
      {completedCount > 0 && (
        <div className="dash-section">
          <p className="dash-label">Summary</p>
          <div style={{ display: 'flex', gap: '2rem' }}>
            <div>
              <p className="server-count">{completedCount} / {urls.length}</p>
              <p className="dash-label">Domains scanned</p>
            </div>
            <div>
              <p className="server-count">{totalViolations}</p>
              <p className="dash-label">Total violations</p>
            </div>
            {ispViolations > 0 && (
              <div>
                <p className="server-count">{ispViolations}</p>
                <p className="dash-label">{ispViolations === 1 ? 'ISP' : 'ISPs'} with violations</p>
              </div>
            )}
          </div>
        </div>
      )}

      {fetchError && (
        <div className="error-state mt-4">
          <p className="error-message">{fetchError}</p>
        </div>
      )}

      {/* Per-domain cards */}
      {urls.map(url => {
        const hostname = (() => { try { return new URL(url).hostname } catch { return url } })()
        const results = resultsByUrl.get(url)
        const hasResults = results && results.length > 0

        return (
          <div key={url} className="dash-section">
            <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.75rem' }}>
              <p className="dash-label">{hostname}</p>
              <Link to="/results/$url" params={{ url }} className="text-xs" style={{ color: 'var(--color-accent)' }}>
                Full history →
              </Link>
            </div>

            {!hasResults ? (
              /* Skeleton while waiting */
              <div className="dash-table-wrap">
                {[1, 2, 3].map(i => (
                  <div key={i} className="dash-skeleton-row">
                    <span className="skeleton" style={{ width: 140, height: 13 }} />
                    <span className="skeleton" style={{ width: 100, height: 13 }} />
                    <span className="skeleton" style={{ width: 120, height: 13 }} />
                  </div>
                ))}
              </div>
            ) : (
              <Table className="results-table" aria-label={`Scan results for ${hostname}`}>
                <TableHeader>
                  <TableRow>
                    <TableHead scope="col">DNS Server</TableHead>
                    <TableHead scope="col">Status</TableHead>
                    <TableHead scope="col">Resolved IP</TableHead>
                    <TableHead scope="col">Evidence</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {results.map(r => (
                    <TableRow key={r.id} className={!r.compliant ? 'violation-row' : ''}>
                      <TableCell><span className="dns-name">{r.dns_server.name}</span></TableCell>
                      <TableCell><StatusDot compliant={r.compliant} /></TableCell>
                      <TableCell>
                        {r.resolved_ip
                          ? <span className="ip-value">{r.resolved_ip}</span>
                          : <span className="empty-cell">—</span>}
                      </TableCell>
                      <TableCell>
                        {r.screenshot_url
                          ? <a href={r.screenshot_url} target="_blank" rel="noopener noreferrer" className="screenshot-link">View screenshot</a>
                          : <span className="empty-cell">—</span>}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>
        )
      })}
    </div>
  )
}
