import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { HistoryIcon, Camera, Image as ImageIcon, ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { fetchResults, groupResults, lastScanTime } from '../api/results'
import { fetchScanStatus, isScanning, triggerScreenshot } from '../api/scan'
import type { GroupedResult, ScanResult } from '../api/types'
import { useScan } from './__root'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardImage,
} from '@/components/animate-ui/components/base/preview-link-card'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'
import { BrailleLoader } from '@/components/ui/braille-loader'
import { ThinkingIndicator } from '@/components/ui/thinking-indicator'

export const Route = createFileRoute('/results/')({ component: ResultsPage })

/* ─── Types ──────────────────────────────────────────────────────────────── */

type StatusFilter = 'all' | 'violations' | 'compliant'

const PAGE_SIZE = 25

/* ─── Helpers ────────────────────────────────────────────────────────────── */

function relativeTime(isoString: string): string {
  const diff = (Date.now() - new Date(isoString).getTime()) / 1000
  const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
  if (diff < 60) return rtf.format(-Math.round(diff), 'second')
  if (diff < 3600) return rtf.format(-Math.round(diff / 60), 'minute')
  if (diff < 86400) return rtf.format(-Math.round(diff / 3600), 'hour')
  return rtf.format(-Math.round(diff / 86400), 'day')
}

/* ─── Chevron Icon ───────────────────────────────────────────────────────── */

function ChevronRight({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      width="12"
      height="12"
      viewBox="0 0 12 12"
      fill="none"
      aria-hidden="true"
    >
      <path d="M4.5 2.5L7.5 6L4.5 9.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

/* ─── Empty Icon ─────────────────────────────────────────────────────────── */

function EmptyIcon() {
  return (
    <svg className="empty-icon" width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
      <rect x="8" y="4" width="24" height="32" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M32 4L40 12V36C40 37.1 39.1 38 38 38H32" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M40 12H32V4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 18H26M14 24H22" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

/* ─── Status Dot ─────────────────────────────────────────────────────────── */

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

/* ─── Sub-rows (expanded DNS results) ───────────────────────────────────── */

function SubRows({
  results,
  visible,
  pendingScreenshotId,
  screenshotErrors,
  screenshotsBlocked,
  onRequestScreenshot,
}: {
  results: ScanResult[]
  visible: boolean
  pendingScreenshotId: number | null
  screenshotErrors: Record<number, string>
  screenshotsBlocked: boolean
  onRequestScreenshot: (result: ScanResult) => void
}) {
  if (!visible) return null
  return (
    <>
      {results.map(r => (
        <TableRow key={r.id} className={`sub-row${!r.compliant ? ' violation-row' : ''}`}>
          <TableCell className="col-expand" />
          <TableCell className="col-domain">
            <span className="dns-name">{r.dns_server.name}</span>
          </TableCell>
          <TableCell className="col-status">
            <StatusDot compliant={r.compliant} />
          </TableCell>
          <TableCell className="col-ip">
            <div className="ip-meta">
              {r.resolved_ip ? (
                <span className="ip-value">{r.resolved_ip}</span>
              ) : (
                <span className="empty-cell" aria-label="Not resolved">—</span>
              )}
              {r.resolved_ipv6 && <span className="ip-meta-secondary">{r.resolved_ipv6}</span>}
              {r.resolved_asn > 0 && (
                <span className="ip-meta-secondary">
                  AS{r.resolved_asn}{r.resolved_org && ` — ${r.resolved_org}`}
                </span>
              )}
            </div>
          </TableCell>
          <TableCell className="col-error">
            {r.error ? (
              <span className="col-error-text" title={r.error}>{r.error}</span>
            ) : (
              <span className="empty-cell">—</span>
            )}
          </TableCell>
          <TableCell className="col-evidence text-center">
            {r.screenshot_url ? (
              <a
                href={r.screenshot_url}
                target="_blank"
                rel="noopener noreferrer"
                className="screenshot-icon-btn"
                aria-label={`View screenshot for ${r.dns_server.name}`}
                title="View screenshot"
              >
                <ImageIcon className="screenshot-icon" aria-hidden="true" />
              </a>
            ) : pendingScreenshotId === r.id ? (
              <span className="screenshot-pending" aria-live="polite" aria-label="Requesting screenshot">
                <BrailleLoader variant="typing" fontSize={13} />
              </span>
            ) : !r.compliant ? (
              <button
                type="button"
                className="screenshot-icon-btn"
                onClick={() => onRequestScreenshot(r)}
                disabled={screenshotsBlocked}
                title={screenshotErrors[r.id] ?? 'Take screenshot'}
                aria-label={`Request screenshot for ${r.dns_server.name}`}
              >
                <Camera className="screenshot-icon" aria-hidden="true" />
              </button>
            ) : (
              <span className="empty-cell" aria-label="No screenshot">—</span>
            )}
          </TableCell>
          <TableCell className="col-last-scanned">
            {r.scanned_at ? (
              <span title={new Date(r.scanned_at).toLocaleString()}>{relativeTime(r.scanned_at)}</span>
            ) : (
              <span className="empty-cell">—</span>
            )}
          </TableCell>
        </TableRow>
      ))}
    </>
  )
}

/* ─── URL Group Row ──────────────────────────────────────────────────────── */

function URLGroupRow({
  group,
  expanded,
  onToggle,
  pendingScreenshotId,
  screenshotErrors,
  screenshotsBlocked,
  onRequestScreenshot,
}: {
  group: GroupedResult
  expanded: boolean
  onToggle: () => void
  pendingScreenshotId: number | null
  screenshotErrors: Record<number, string>
  screenshotsBlocked: boolean
  onRequestScreenshot: (result: ScanResult) => void
}) {
  const { violationCount, totalCount, hostname, url } = group
  const compliantCount = totalCount - violationCount
  const pct = totalCount > 0 ? Math.round((compliantCount / totalCount) * 100) : 0

  return (
    <>
      <TableRow className="url-row" onClick={onToggle} aria-expanded={expanded}>
        <TableCell className="col-expand">
          <button
            className="expand-btn"
            onClick={e => { e.stopPropagation(); onToggle() }}
            aria-expanded={expanded}
            aria-label={`${expanded ? 'Collapse' : 'Expand'} results for ${hostname}`}
            tabIndex={0}
          >
            <ChevronRight className={`expand-icon${expanded ? ' expanded' : ''}`} />
          </button>
        </TableCell>
        <TableCell className="col-domain">
          <PreviewLinkCard href={url}>
            <PreviewLinkCardTrigger>
              <span className="hostname" title={url}>{hostname}</span>
            </PreviewLinkCardTrigger>
            <PreviewLinkCardPanel>
              <PreviewLinkCardImage />
            </PreviewLinkCardPanel>
          </PreviewLinkCard>
        </TableCell>
        <TableCell className="col-status">
          <div className="server-bar-wrap">
            <div className="server-bar" role="presentation">
              <div className="server-bar-fill" style={{ width: `${pct}%` }} />
            </div>
            <span className="server-count">{compliantCount} / {totalCount}</span>
          </div>
        </TableCell>
        <TableCell className="col-ip" />
        <TableCell className="col-error" />
        <TableCell className="col-evidence text-center">
          <Link
            to="/results/$url"
            params={{ url }}
            className="btn-row-history"
            aria-label={`View history for ${hostname}`}
            onClick={e => e.stopPropagation()}
          >
            <HistoryIcon className="btn-row-history-icon" />
          </Link>
        </TableCell>
        <TableCell className="col-last-scanned">
          {group.latestScannedAt ? (
            <span title={new Date(group.latestScannedAt).toLocaleString()}>
              {relativeTime(group.latestScannedAt)}
            </span>
          ) : (
            <span className="empty-cell">—</span>
          )}
        </TableCell>
      </TableRow>
      <SubRows
        results={group.results}
        visible={expanded}
        pendingScreenshotId={pendingScreenshotId}
        screenshotErrors={screenshotErrors}
        screenshotsBlocked={screenshotsBlocked}
        onRequestScreenshot={onRequestScreenshot}
      />
    </>
  )
}

/* ─── Skeleton ───────────────────────────────────────────────────────────── */

function ResultsTableSkeleton() {
  return (
    <>
      {[180, 140, 220, 160, 200].map((w, i) => (
        <TableRow key={i} className="skeleton-row">
          <TableCell className="col-expand">
            <span className="skeleton" style={{ width: 16, height: 16, borderRadius: 3 }} />
          </TableCell>
          <TableCell className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </TableCell>
          <TableCell className="col-status">
            <span className="skeleton" style={{ width: 100, height: 20, borderRadius: 4 }} />
          </TableCell>
          <TableCell className="col-ip" />
          <TableCell className="col-error" />
          <TableCell className="col-evidence" />
          <TableCell className="col-last-scanned" />
        </TableRow>
      ))}
    </>
  )
}

/* ─── Results Page ───────────────────────────────────────────────────────── */

function ResultsPage() {
  const { scanning, refreshSignal } = useScan()

  const [results, setResults] = useState<ScanResult[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [dnsFilter, setDnsFilter] = useState<string>('all')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [page, setPage] = useState(1)

  const [pendingScreenshotId, setPendingScreenshotId] = useState<number | null>(null)
  const [screenshotErrors, setScreenshotErrors] = useState<Record<number, string>>({})
  const screenshotPollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const load = useCallback(async () => {
    try {
      setError(null)
      setResults(await fetchResults())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load results')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load, refreshSignal])

  useEffect(() => {
    return () => {
      if (screenshotPollRef.current) clearInterval(screenshotPollRef.current)
    }
  }, [])

  const requestScreenshot = useCallback(async (result: ScanResult) => {
    setScreenshotErrors(prev => {
      if (!(result.id in prev)) return prev
      const next = { ...prev }
      delete next[result.id]
      return next
    })
    setPendingScreenshotId(result.id)
    try {
      await triggerScreenshot(result.url, result.dns_server_id)
    } catch (err) {
      setPendingScreenshotId(null)
      setScreenshotErrors(prev => ({
        ...prev,
        [result.id]: err instanceof Error ? err.message : 'Failed to request screenshot',
      }))
      return
    }
    screenshotPollRef.current = setInterval(async () => {
      try {
        const status = await fetchScanStatus()
        if (!isScanning(status)) {
          if (screenshotPollRef.current) clearInterval(screenshotPollRef.current)
          screenshotPollRef.current = null
          setPendingScreenshotId(null)
          load()
        }
      } catch {
        // transient error while polling; keep trying
      }
    }, 3000)
  }, [load])

  const toggleExpanded = useCallback((url: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(url)) next.delete(url)
      else next.add(url)
      return next
    })
  }, [])

  const groups = useMemo(() => groupResults(results), [results])
  const lastScan = useMemo(() => lastScanTime(groups), [groups])

  const dnsServers = useMemo(() => {
    const seen = new Map<string, string>()
    for (const g of groups) {
      for (const r of g.results) seen.set(r.dns_server.name, r.dns_server.name)
    }
    return Array.from(seen.values()).sort()
  }, [groups])

  const filtered = useMemo(() => {
    return groups
      .map(g => {
        let res = g.results
        if (dnsFilter !== 'all') res = res.filter(r => r.dns_server.name === dnsFilter)
        if (statusFilter === 'violations') res = res.filter(r => !r.compliant)
        else if (statusFilter === 'compliant') res = res.filter(r => r.compliant)
        if (res.length === 0) return null
        const violationCount = res.filter(r => !r.compliant).length
        return { ...g, results: res, violationCount, totalCount: res.length }
      })
      .filter(Boolean) as GroupedResult[]
  }, [groups, statusFilter, dnsFilter])

  useEffect(() => { setPage(1) }, [statusFilter, dnsFilter])

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const paginated = useMemo(
    () => filtered.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE),
    [filtered, currentPage],
  )

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title">Compliance Results</h1>
        {!loading && lastScan && (
          <p className="page-subtitle">Last scan: {lastScan}</p>
        )}
      </div>

      {scanning && (
        <div className="scan-banner">
          <ThinkingIndicator className="p-0" />
        </div>
      )}

      {error ? (
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button className="btn-primary" onClick={load}>Retry</button>
        </div>
      ) : (
        <div className="flex flex-col items-stretch w-full gap-4 mt-4">
          <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full">
            <span className="filter-label" id="status-label">Status</span>
            <ToggleGroup
              type="single"
              value={statusFilter}
              size="sm"
              onValueChange={v => { if (v) setStatusFilter(v as StatusFilter) }}
              variant="outline"
              aria-labelledby="status-label"
            >
              <ToggleGroupItem value="all">All</ToggleGroupItem>
              <ToggleGroupItem value="violations">Violations</ToggleGroupItem>
              <ToggleGroupItem value="compliant">Compliant</ToggleGroupItem>
            </ToggleGroup>

            {dnsServers.length > 1 && (
              <>
                <span className="filter-label" id="dns-label">DNS Server</span>
                <Select value={dnsFilter} onValueChange={setDnsFilter}>
                  <SelectTrigger aria-labelledby="dns-label" />
                  <SelectContent>
                    <SelectItem index={0} value="all">All servers</SelectItem>
                    {dnsServers.map((name, i) => (
                      <SelectItem key={name} index={i + 1} value={name}>{name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </>
            )}
          </div>

          <div className="results-wrap w-full">
            {!loading && groups.length === 0 ? (
              <div className="empty-state">
                <EmptyIcon />
                <p className="empty-heading">No results yet</p>
                <p className="empty-body">
                  No scan has been run. Use Run Scan to begin compliance monitoring.
                </p>
              </div>
            ) : (
              <Table className="results-table" aria-label="DNS compliance results">
                <TableHeader>
                  <TableRow>
                    <TableHead className="col-expand" scope="col" />
                    <TableHead className="col-domain th-left" scope="col">Domain</TableHead>
                    <TableHead className="col-status th-left" scope= "col">Status</TableHead>
                    <TableHead className="col-ip th-left" scope="col">Resolved IP</TableHead>
                    <TableHead className="col-error th-left" scope="col">Error</TableHead>
                    <TableHead className="col-evidence th-center" scope="col">Evidence</TableHead>
                    <TableHead className="col-last-scanned th-left" scope="col">Last scanned</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {loading ? (
                    <ResultsTableSkeleton />
                  ) : filtered.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={7}>
                        <div className="empty-state" style={{ padding: '3rem 0' }}>
                          <p className="empty-heading">No results match the current filters</p>
                        </div>
                      </TableCell>
                    </TableRow>
                  ) : (
                    paginated.map(group => (
                      <URLGroupRow
                        key={group.url}
                        group={group}
                        expanded={expanded.has(group.url)}
                        onToggle={() => toggleExpanded(group.url)}
                        pendingScreenshotId={pendingScreenshotId}
                        screenshotErrors={screenshotErrors}
                        screenshotsBlocked={scanning || pendingScreenshotId !== null}
                        onRequestScreenshot={requestScreenshot}
                      />
                    ))
                  )}
                </TableBody>
              </Table>
            )}
            {!loading && totalPages > 1 && (
              <div className="pagination">
                <span className="pagination-label">Page {currentPage} of {totalPages}</span>
                <button
                  type="button"
                  className="pagination-btn"
                  onClick={() => setPage(p => p - 1)}
                  disabled={currentPage <= 1}
                  aria-label="Previous page"
                >
                  <ChevronLeftIcon className="w-4 h-4" />
                </button>
                <button
                  type="button"
                  className="pagination-btn"
                  onClick={() => setPage(p => p + 1)}
                  disabled={currentPage >= totalPages}
                  aria-label="Next page"
                >
                  <ChevronRightIcon className="w-4 h-4" />
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
