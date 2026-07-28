import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { Camera, Image as ImageIcon, ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { GripIcon } from '@/components/ui/grip'
import { ChevronRight } from '@/components/ui/chevron-right'
import { fetchResults, groupResults, lastScanTime } from '../api/results'
import { fetchScanStatus, isScanning, triggerScreenshot } from '../api/scan'
import type { GroupedResult, ScanResult } from '../api/types'
import { useScan } from './__root'
import {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardImage,
} from '@/components/animate-ui/components/base/preview-link-card'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Input } from '@/components/ui/input'
import { Dialog, DialogContent, DialogTitle } from '@/components/animate-ui/components/radix/dialog'
import { Progress, ProgressTrack } from '@/components/animate-ui/components/base/progress'
import { AnimatedNumber } from '@/components/ui/animated-number'
import { Filters, type Filter, type FilterFieldConfig } from '@/components/reui/filters'
import { BrailleLoader } from '@/components/ui/braille-loader'
import { ThinkingIndicator } from '@/components/ui/thinking-indicator'
import { StatusDot, EmptyIcon } from '@/components/results-table-parts'
import { relativeTime } from '@/lib/relative-time'

export const Route = createFileRoute('/results/')({ component: ResultsPage })

/* ─── Types ──────────────────────────────────────────────────────────────── */

type StatusFilter = 'violations' | 'compliant'

const PAGE_SIZE = 25

// Single "is" operator only — these fields are single-value pickers, not
// full is/is-not/empty builders, so there's nothing else to implement.
const IS_ONLY = [{ value: 'is', label: 'is' }]

const STATUS_FIELD: FilterFieldConfig<string> = {
  key: 'status',
  label: 'Status',
  type: 'select',
  operators: IS_ONLY,
  options: [
    { value: 'violations', label: 'Violations' },
    { value: 'compliant', label: 'Compliant' },
  ],
}

/* ─── Sub-rows (expanded DNS results) ───────────────────────────────────── */

function SubRows({
  results,
  visible,
  pendingScreenshotIds,
  screenshotErrors,
  screenshotsBlocked,
  onRequestScreenshot,
  onViewScreenshot,
}: {
  results: ScanResult[]
  visible: boolean
  pendingScreenshotIds: Set<number>
  screenshotErrors: Record<number, string>
  screenshotsBlocked: boolean
  onRequestScreenshot: (results: ScanResult[]) => void
  onViewScreenshot: (result: ScanResult) => void
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
              {r.resolved_netname && <span className="ip-meta-secondary">{r.resolved_netname}</span>}
            </div>
          </TableCell>
          <TableCell className="col-error">
            {r.error ? (
              <span className="col-error-text" title={r.error}>{r.error}</span>
            ) : (
              <span className="empty-cell">—</span>
            )}
          </TableCell>
          <TableCell className="col-last-scanned">
            {r.scanned_at ? (
              <span title={new Date(r.scanned_at).toLocaleString()}>{relativeTime(r.scanned_at)}</span>
            ) : (
              <span className="empty-cell">—</span>
            )}
          </TableCell>
          <TableCell className="col-evidence text-center">
            {r.screenshot_url ? (
              <button
                type="button"
                className="screenshot-icon-btn"
                onClick={() => onViewScreenshot(r)}
                aria-label={`View screenshot for ${r.dns_server.name}`}
                title="View screenshot"
              >
                <ImageIcon className="screenshot-icon" aria-hidden="true" />
              </button>
            ) : pendingScreenshotIds.has(r.id) ? (
              <span className="screenshot-pending" aria-live="polite" aria-label="Requesting screenshot">
                <BrailleLoader variant="typing" fontSize={13} />
              </span>
            ) : !r.compliant ? (
              <button
                type="button"
                className="screenshot-icon-btn"
                onClick={() => onRequestScreenshot([r])}
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
  pendingScreenshotIds,
  screenshotErrors,
  screenshotsBlocked,
  onRequestScreenshot,
  onViewScreenshot,
}: {
  group: GroupedResult
  expanded: boolean
  onToggle: () => void
  pendingScreenshotIds: Set<number>
  screenshotErrors: Record<number, string>
  screenshotsBlocked: boolean
  onRequestScreenshot: (results: ScanResult[]) => void
  onViewScreenshot: (result: ScanResult) => void
}) {
  const { violationCount, totalCount, hostname, url } = group
  const compliantCount = totalCount - violationCount
  const pct = totalCount > 0 ? Math.round((compliantCount / totalCount) * 100) : 0
  const needsScreenshot = group.results.filter(r => !r.compliant && !r.screenshot_url)
  const groupPending = group.results.some(r => pendingScreenshotIds.has(r.id))
  const bulkError = needsScreenshot.map(r => screenshotErrors[r.id]).find(Boolean)

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
        <TableCell className="col-last-scanned">
          {group.latestScannedAt ? (
            <span title={new Date(group.latestScannedAt).toLocaleString()}>
              {relativeTime(group.latestScannedAt)}
            </span>
          ) : (
            <span className="empty-cell">—</span>
          )}
        </TableCell>
        <TableCell className="col-evidence text-center">
          <div className="flex items-center justify-center gap-1">
            {needsScreenshot.length > 0 && (
              groupPending ? (
                <span className="screenshot-pending" aria-live="polite" aria-label="Requesting screenshots">
                  <BrailleLoader variant="typing" fontSize={13} />
                </span>
              ) : (
                <button
                  type="button"
                  className="screenshot-icon-btn"
                  onClick={e => { e.stopPropagation(); onRequestScreenshot(needsScreenshot) }}
                  disabled={screenshotsBlocked}
                  title={bulkError ?? `Take screenshots for ${needsScreenshot.length} violating server${needsScreenshot.length > 1 ? 's' : ''}`}
                  aria-label={`Request screenshots for all violating DNS servers for ${hostname}`}
                >
                  <Camera className="screenshot-icon" aria-hidden="true" />
                </button>
              )
            )}
            <Link
              to="/domain/$url"
              params={{ url }}
              search={{ tab: 'overview' }}
              className="btn-row-history"
              aria-label={`View overview for ${hostname}`}
              onClick={e => e.stopPropagation()}
            >
              <GripIcon className="btn-row-history-icon" size={16} />
            </Link>
          </div>
        </TableCell>
      </TableRow>
      <SubRows
        results={group.results}
        visible={expanded}
        pendingScreenshotIds={pendingScreenshotIds}
        screenshotErrors={screenshotErrors}
        screenshotsBlocked={screenshotsBlocked}
        onRequestScreenshot={onRequestScreenshot}
        onViewScreenshot={onViewScreenshot}
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
          <TableCell className="col-last-scanned" />
          <TableCell className="col-evidence" />
        </TableRow>
      ))}
    </>
  )
}

/* ─── Results Page ───────────────────────────────────────────────────────── */

function ResultsPage() {
  const { scanning, refreshSignal, progress } = useScan()

  const [results, setResults] = useState<ScanResult[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [filters, setFilters] = useState<Filter<string>[]>([])
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [page, setPage] = useState(1)

  const [pendingScreenshotIds, setPendingScreenshotIds] = useState<Set<number>>(new Set())
  const [screenshotErrors, setScreenshotErrors] = useState<Record<number, string>>({})
  const screenshotPollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const [previewScreenshot, setPreviewScreenshot] = useState<ScanResult | null>(null)

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

  const requestScreenshot = useCallback(async (results: ScanResult[]) => {
    if (results.length === 0) return
    const ids = results.map(r => r.id)
    setScreenshotErrors(prev => {
      const next = { ...prev }
      for (const id of ids) delete next[id]
      return next
    })
    setPendingScreenshotIds(prev => new Set([...prev, ...ids]))
    try {
      const dnsServerIds = [...new Set(results.map(r => r.dns_server_id))]
      await triggerScreenshot(results[0].url, dnsServerIds)
    } catch (err) {
      setPendingScreenshotIds(prev => {
        const next = new Set(prev)
        for (const id of ids) next.delete(id)
        return next
      })
      const message = err instanceof Error ? err.message : 'Failed to request screenshot'
      setScreenshotErrors(prev => {
        const next = { ...prev }
        for (const id of ids) next[id] = message
        return next
      })
      return
    }
    screenshotPollRef.current = setInterval(async () => {
      try {
        const status = await fetchScanStatus()
        if (!isScanning(status)) {
          if (screenshotPollRef.current) clearInterval(screenshotPollRef.current)
          screenshotPollRef.current = null
          setPendingScreenshotIds(new Set())
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

  const filterFields = useMemo(() => {
    const fields: FilterFieldConfig<string>[] = [STATUS_FIELD]
    if (dnsServers.length > 1) {
      fields.push({
        key: 'dns_server',
        label: 'DNS Server',
        type: 'select',
        operators: IS_ONLY,
        options: dnsServers.map(name => ({ value: name, label: name })),
      })
    }
    return fields
  }, [dnsServers])

  const statusFilter = filters.find(f => f.field === 'status')?.values[0] as StatusFilter | undefined
  const dnsFilter = filters.find(f => f.field === 'dns_server')?.values[0] as string | undefined

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    return groups
      .filter(g => !query || g.url.toLowerCase().includes(query))
      .map(g => {
        let res = g.results
        if (dnsFilter) res = res.filter(r => r.dns_server.name === dnsFilter)
        if (statusFilter === 'violations') res = res.filter(r => !r.compliant)
        else if (statusFilter === 'compliant') res = res.filter(r => r.compliant)
        if (res.length === 0) return null
        const violationCount = res.filter(r => !r.compliant).length
        return { ...g, results: res, violationCount, totalCount: res.length }
      })
      .filter(Boolean) as GroupedResult[]
  }, [groups, statusFilter, dnsFilter, search])

  useEffect(() => { setPage(1) }, [statusFilter, dnsFilter, search])

  const scanProgress = useMemo(() => {
    if (!progress) return undefined
    const servers = progress.per_dns.length
    // Average completed count across servers, not the max — a domain isn't
    // "scanned" until every DNS server has checked it, and results now stream
    // in per-server rather than all at once.
    const completed = servers === 0
      ? 0
      : Math.floor(progress.per_dns.reduce((total, p) => total + p.completed, 0) / servers)
    return { completed, total: progress.total_urls }
  }, [progress])

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
        <div className="scan-banner mt-2 flex items-center gap-4">
          <ThinkingIndicator className="p-0" />
          {scanProgress && (
            <Progress
              value={scanProgress.total > 0 ? (scanProgress.completed / scanProgress.total) * 100 : 0}
              className="flex items-center gap-2 w-36"
            >
              <ProgressTrack className="flex-1" />
              <span className="flex items-baseline gap-1 text-[13px] text-stone-muted [font-variant-numeric:tabular-nums] whitespace-nowrap">
                <AnimatedNumber value={scanProgress.completed} />
                <span>/ {scanProgress.total}</span>
              </span>
            </Progress>
          )}
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
            <Input
              type="search"
              placeholder="Search domain..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="max-w-64"
              aria-label="Search domain"
            />
            <Filters filters={filters} fields={filterFields} onChange={setFilters} />
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
                    <TableHead className="col-last-scanned th-left" scope="col">Last scanned</TableHead>
                    <TableHead className="col-evidence th-center" scope="col">Actions</TableHead>
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
                        pendingScreenshotIds={pendingScreenshotIds}
                        screenshotErrors={screenshotErrors}
                        screenshotsBlocked={scanning || pendingScreenshotIds.size > 0}
                        onRequestScreenshot={requestScreenshot}
                        onViewScreenshot={setPreviewScreenshot}
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

      <Dialog open={!!previewScreenshot} onOpenChange={v => { if (!v) setPreviewScreenshot(null) }}>
        <DialogContent className="sm:max-w-3xl">
          <DialogTitle className="sr-only">
            Screenshot evidence{previewScreenshot ? ` for ${previewScreenshot.dns_server.name}` : ''}
          </DialogTitle>
          {previewScreenshot?.screenshot_url && (
            <img
              src={previewScreenshot.screenshot_url}
              alt={`Screenshot evidence for ${previewScreenshot.url} via ${previewScreenshot.dns_server.name}`}
              className="w-full h-auto rounded"
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
