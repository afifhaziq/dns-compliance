import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import {
  type ColumnDef,
  type Column,
  type ExpandedState,
  type SortingState,
  type PaginationState,
  getCoreRowModel,
  getSortedRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { Camera, Image as ImageIcon, ChevronLeftIcon, ChevronRightIcon, ArrowUpIcon, ArrowDownIcon, ChevronsUpDownIcon } from 'lucide-react'
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
import { Table, TableBody, TableRow, TableCell } from '@/components/ui/table'
import { Input } from '@/components/ui/input'
import { Dialog, DialogContent, DialogTitle } from '@/components/animate-ui/components/radix/dialog'
import { Progress, ProgressTrack } from '@/components/animate-ui/components/base/progress'
import { AnimatedNumber } from '@/components/ui/animated-number'
import { Filters, type Filter, type FilterFieldConfig } from '@/components/reui/filters'
import { DataGrid, DataGridContainer } from '@/components/reui/data-grid/data-grid'
import { DataGridTable } from '@/components/reui/data-grid/data-grid-table'
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

/* ─── Sortable column header ────────────────────────────────────────────── */
// Minimal stand-in for reui's DataGridColumnHeader: that component pulls in a
// column-visibility/pin/move dropdown menu and an IconPlaceholder shim tied
// to a Next.js app path this repo doesn't have. All we need is click-to-cycle
// sort with an indicator icon, so it's written directly instead of vendored.

function SortableHeader<TData, TValue>({ column, title }: { column: Column<TData, TValue>; title: string }) {
  const sorted = column.getIsSorted()
  const cycleSort = () => {
    if (sorted === 'asc') column.toggleSorting(true)
    else if (sorted === 'desc') column.clearSorting()
    else column.toggleSorting(false)
  }
  return (
    <button
      type="button"
      className="inline-flex items-center gap-1 text-[11px] font-semibold tracking-[0.06em] uppercase text-stone-muted hover:text-foreground transition-colors duration-150 ease-snappy"
      onClick={cycleSort}
    >
      {title}
      {sorted === 'asc' ? (
        <ArrowUpIcon className="w-3 h-3" />
      ) : sorted === 'desc' ? (
        <ArrowDownIcon className="w-3 h-3" />
      ) : (
        <ChevronsUpDownIcon className="w-3 h-3 opacity-40" />
      )}
    </button>
  )
}

/* ─── Sub-rows (expanded per-DNS-server results) ────────────────────────── */

function SubRowsTable({
  results,
  pendingScreenshotIds,
  screenshotErrors,
  screenshotsBlocked,
  onRequestScreenshot,
  onViewScreenshot,
}: {
  results: ScanResult[]
  pendingScreenshotIds: Set<number>
  screenshotErrors: Record<number, string>
  screenshotsBlocked: boolean
  onRequestScreenshot: (results: ScanResult[]) => void
  onViewScreenshot: (result: ScanResult) => void
}) {
  return (
    <Table aria-label="Per-DNS-server results">
      <TableBody>
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
      </TableBody>
    </Table>
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
  const [expanded, setExpanded] = useState<ExpandedState>({})
  const [sorting, setSorting] = useState<SortingState>([{ id: 'status', desc: true }])
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })

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

  useEffect(() => { setPagination(p => ({ ...p, pageIndex: 0 })) }, [statusFilter, dnsFilter, search])

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

  const screenshotsBlocked = scanning || pendingScreenshotIds.size > 0

  const columns = useMemo<ColumnDef<GroupedResult>[]>(() => [
    {
      id: 'expand',
      header: () => null,
      size: 30,
      enableSorting: false,
      meta: {
        headerClassName: 'col-expand',
        cellClassName: 'col-expand',
        expandedContent: (group: GroupedResult) => (
          <SubRowsTable
            results={group.results}
            pendingScreenshotIds={pendingScreenshotIds}
            screenshotErrors={screenshotErrors}
            screenshotsBlocked={screenshotsBlocked}
            onRequestScreenshot={requestScreenshot}
            onViewScreenshot={setPreviewScreenshot}
          />
        ),
        skeleton: <span className="skeleton" style={{ width: 16, height: 16, borderRadius: 3 }} />,
      },
      cell: ({ row }) => (
        <button
          type="button"
          className="expand-btn"
          onClick={e => { e.stopPropagation(); row.getToggleExpandedHandler()() }}
          aria-expanded={row.getIsExpanded()}
          aria-label={`${row.getIsExpanded() ? 'Collapse' : 'Expand'} results for ${row.original.hostname}`}
        >
          <ChevronRight className={`expand-icon${row.getIsExpanded() ? ' expanded' : ''}`} />
        </button>
      ),
    },
    {
      accessorKey: 'hostname',
      id: 'domain',
      header: ({ column }) => <SortableHeader column={column} title="Domain" />,
      meta: {
        headerClassName: 'col-domain th-left',
        cellClassName: 'col-domain',
        skeleton: <span className="skeleton" style={{ width: 180, height: 14 }} />,
      },
      cell: ({ row }) => (
        <PreviewLinkCard href={`https://${row.original.hostname}`}>
          <PreviewLinkCardTrigger>
            <span className="hostname" title={row.original.url}>{row.original.hostname}</span>
          </PreviewLinkCardTrigger>
          <PreviewLinkCardPanel>
            <PreviewLinkCardImage />
          </PreviewLinkCardPanel>
        </PreviewLinkCard>
      ),
    },
    {
      id: 'status',
      accessorFn: g => g.violationCount,
      header: ({ column }) => <SortableHeader column={column} title="Status" />,
      meta: {
        headerClassName: 'col-status th-left',
        cellClassName: 'col-status',
        skeleton: <span className="skeleton" style={{ width: 100, height: 20, borderRadius: 4 }} />,
      },
      cell: ({ row }) => {
        const { violationCount, totalCount } = row.original
        const compliantCount = totalCount - violationCount
        const pct = totalCount > 0 ? Math.round((compliantCount / totalCount) * 100) : 0
        return (
          <div className="server-bar-wrap">
            <div className="server-bar" role="presentation">
              <div className="server-bar-fill" style={{ width: `${pct}%` }} />
            </div>
            <span className="server-count">{compliantCount} / {totalCount}</span>
          </div>
        )
      },
    },
    {
      id: 'ip',
      header: 'Resolved IP',
      enableSorting: false,
      meta: { headerClassName: 'col-ip th-left', cellClassName: 'col-ip' },
      cell: () => null,
    },
    {
      id: 'error',
      header: 'Error',
      enableSorting: false,
      meta: { headerClassName: 'col-error th-left', cellClassName: 'col-error' },
      cell: () => null,
    },
    {
      id: 'lastScanned',
      accessorFn: g => g.latestScannedAt,
      header: ({ column }) => <SortableHeader column={column} title="Last scanned" />,
      meta: { headerClassName: 'col-last-scanned th-left', cellClassName: 'col-last-scanned' },
      cell: ({ row }) => {
        const { latestScannedAt } = row.original
        return latestScannedAt ? (
          <span title={new Date(latestScannedAt).toLocaleString()}>{relativeTime(latestScannedAt)}</span>
        ) : (
          <span className="empty-cell">—</span>
        )
      },
    },
    {
      id: 'actions',
      header: 'Actions',
      enableSorting: false,
      meta: { headerClassName: 'col-evidence th-center', cellClassName: 'col-evidence text-center' },
      cell: ({ row }) => {
        const group = row.original
        const needsScreenshot = group.results.filter(r => !r.compliant && !r.screenshot_url)
        const groupPending = group.results.some(r => pendingScreenshotIds.has(r.id))
        const bulkError = needsScreenshot.map(r => screenshotErrors[r.id]).find(Boolean)
        return (
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
                  onClick={e => { e.stopPropagation(); requestScreenshot(needsScreenshot) }}
                  disabled={screenshotsBlocked}
                  title={bulkError ?? `Take screenshots for ${needsScreenshot.length} violating server${needsScreenshot.length > 1 ? 's' : ''}`}
                  aria-label={`Request screenshots for all violating DNS servers for ${group.hostname}`}
                >
                  <Camera className="screenshot-icon" aria-hidden="true" />
                </button>
              )
            )}
            <Link
              to="/domain/$url"
              params={{ url: group.url }}
              search={{ tab: 'overview' }}
              className="btn-row-history"
              aria-label={`View overview for ${group.hostname}`}
              onClick={e => e.stopPropagation()}
            >
              <GripIcon className="btn-row-history-icon" size={16} />
            </Link>
          </div>
        )
      },
    },
  ], [pendingScreenshotIds, screenshotErrors, screenshotsBlocked, requestScreenshot])

  const table = useReactTable({
    data: filtered,
    columns,
    state: { expanded, sorting, pagination },
    onExpandedChange: setExpanded,
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    getRowId: g => g.url,
    getRowCanExpand: () => true,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  })

  const pageCount = table.getPageCount()

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
            ) : !loading && filtered.length === 0 ? (
              <div className="empty-state" style={{ padding: '3rem 0' }}>
                <p className="empty-heading">No results match the current filters</p>
              </div>
            ) : (
              <DataGrid
                table={table}
                recordCount={filtered.length}
                isLoading={loading}
                onRowClick={group => table.getRow(group.url).toggleExpanded()}
                tableClassNames={{ base: 'results-table' }}
              >
                <DataGridContainer>
                  <DataGridTable />
                </DataGridContainer>
              </DataGrid>
            )}
            {!loading && pageCount > 1 && (
              <div className="pagination">
                <span className="pagination-label">Page {pagination.pageIndex + 1} of {pageCount}</span>
                <button
                  type="button"
                  className="pagination-btn"
                  onClick={() => table.previousPage()}
                  disabled={!table.getCanPreviousPage()}
                  aria-label="Previous page"
                >
                  <ChevronLeftIcon className="w-4 h-4" />
                </button>
                <button
                  type="button"
                  className="pagination-btn"
                  onClick={() => table.nextPage()}
                  disabled={!table.getCanNextPage()}
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
