import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { ArrowLeftIcon } from 'lucide-react'
import { fetchResults, fetchResurfacedDomains } from '@/api/results'
import type { ScanResult } from '@/api/types'
import { useScan } from '@/routes/__root'
import { Table, TableBody, TableRow, TableCell, TableHead, TableHeader } from '@/components/ui/table'
import { classifyDNSError, dnsErrorLabel } from '@/lib/dns-error'

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
  const [resurfacedUrls, setResurfacedUrls] = useState<Set<string>>(new Set())
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
        const [all, resurfaced] = await Promise.all([fetchResults(), fetchResurfacedDomains()])
        processFreshResults(all)
        setResurfacedUrls(new Set(resurfaced.filter(d => urls.includes(d.url)).map(d => d.url)))
      } catch (err) {
        setFetchError(err instanceof Error ? err.message : 'Failed to load results')
      }
    }
    finalFetch()
  }, [refreshSignal, scanning, processFreshResults, urls])

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

  const worstISP = useMemo(() => {
    const counts = new Map<string, number>()
    for (const results of resultsByUrl.values()) {
      for (const r of results) {
        if (!r.compliant) counts.set(r.dns_server.isp, (counts.get(r.dns_server.isp) ?? 0) + 1)
      }
    }
    if (counts.size === 0) return null
    let best = { isp: '', count: 0 }
    for (const [isp, count] of counts) {
      if (count > best.count) best = { isp, count }
    }
    return best
  }, [resultsByUrl])

  if (urls.length === 0) {
    return (
      <div className="mx-20">
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
    <div className="mx-20">
      <Link to="/" className="back-link mt-8">
        <ArrowLeftIcon className="back-link-icon" />
        Overview
      </Link>

      <div className="page-header px-0">
        <h1 className="page-title mb-2">Scan Results</h1>
        <p className="page-subtitle">
          {urls.length === 1 ? urls[0] : `${urls.length} domains selected`}
        </p>
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
          <p className="section-title mb-3">Summary</p>
          <div style={{ display: 'flex', gap: '2rem', flexWrap: 'wrap' }}>
            <div>
              <p className="server-count" style={{ color: 'var(--ink)' }}>{completedCount} / {urls.length}</p>
              <p className="dash-label">Domains scanned</p>
            </div>
            <div>
              <p className="server-count" style={{ color: 'var(--ink)' }}>{totalViolations}</p>
              <p className="dash-label">Total violations</p>
            </div>
            {ispViolations > 0 && (
              <div>
                <p className="server-count" style={{ color: 'var(--ink)' }}>{ispViolations}</p>
                <p className="dash-label">{ispViolations === 1 ? 'ISP' : 'ISPs'} with violations</p>
              </div>
            )}
            {worstISP && (
              <div>
                <Link to="/isps/$isp" params={{ isp: worstISP.isp }} className="server-count" style={{ color: 'var(--ink)' }}>
                  {worstISP.isp}
                </Link>
                <p className="dash-label">Worst ISP ({worstISP.count} violations)</p>
              </div>
            )}
            <div>
              <p className={resurfacedUrls.size > 0 ? 'server-count label-violation' : 'server-count'} style={resurfacedUrls.size > 0 ? undefined : { color: 'var(--ink)' }}>{resurfacedUrls.size}</p>
              <p className="dash-label">Resurfaced</p>
            </div>
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
        const results = resultsByUrl.get(url)
        const hasResults = results && results.length > 0
        const isResurfaced = resurfacedUrls.has(url)

        return (
          <div key={url} className="dash-section mb-4">
            <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.75rem' }}>
              <div className="section-title mb-2">{url}</div>
              {isResurfaced && (
                <span className="label-violation" style={{ fontSize: '0.7rem', fontWeight: 600 }}>RESURFACED</span>
              )}
              <Link to="/domain/$url" params={{ url }} search={{ tab: 'overview' }} className="text-xs" style={{ color: 'var(--ink)' }}>
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
              <Table className="results-table" aria-label={`Scan results for ${url}`}>
                <TableHeader>
                  <TableRow>
                    <TableHead scope="col">DNS Server</TableHead>
                    <TableHead scope="col">Status</TableHead>
                    <TableHead scope="col">Resolved IP</TableHead>
                    <TableHead scope="col">Reason</TableHead>
                    <TableHead scope="col">Evidence</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {results.map(r => {
                    const errType = classifyDNSError(r.error)
                    const errLabel = dnsErrorLabel(errType)
                    return (
                      <TableRow key={r.id} className={!r.compliant ? 'violation-row' : ''}>
                        <TableCell><span className="dns-name">{r.dns_server.name}</span></TableCell>
                        <TableCell><StatusDot compliant={r.compliant} /></TableCell>
                        <TableCell>
                          {r.resolved_ip
                            ? <span className="ip-value">{r.resolved_ip}</span>
                            : <span className="empty-cell">—</span>}
                        </TableCell>
                        <TableCell>
                          {errLabel
                            ? <span className="ip-value">{errLabel}</span>
                            : <span className="empty-cell">—</span>}
                        </TableCell>
                        <TableCell>
                          {r.screenshot_url
                            ? <a href={r.screenshot_url} target="_blank" rel="noopener noreferrer" className="screenshot-link">View screenshot</a>
                            : <span className="empty-cell">—</span>}
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            )}
          </div>
        )
      })}
    </div>
  )
}
