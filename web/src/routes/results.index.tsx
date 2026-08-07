import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import {
  type ColumnDef,
  type ExpandedState,
  type SortingState,
  type PaginationState,
  type VisibilityState,
  getCoreRowModel,
  getSortedRowModel,
  getExpandedRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { Camera, Image as ImageIcon, ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { GripIcon } from '@/components/ui/grip'
import { ChevronRight } from '@/components/ui/chevron-right'
import { fetchResults, groupResults, lastScanTime } from '../api/results'
import { fetchScanStatus, isScanning, triggerScreenshot } from '../api/scan'
import { fetchDomainSummaries, fetchDomainServerSummaries } from '../api/domains'
import { fetchDnsServers } from '../api/dns-servers'
import type { GroupedResult, ScanResult, DomainSummary, DomainServerSummary, DNSServer } from '../api/types'
import { useScan } from './__root'
import {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardImage,
} from '@/components/animate-ui/components/base/preview-link-card'
import { Dialog, DialogContent, DialogTitle } from '@/components/animate-ui/components/radix/dialog'
import { Progress, ProgressTrack } from '@/components/animate-ui/components/base/progress'
import { AnimatedNumber } from '@/components/ui/animated-number'
import { DataGrid, DataGridContainer } from '@/components/reui/data-grid/data-grid'
import { DataGridTable, DataGridTableRowExpand } from '@/components/reui/data-grid/data-grid-table'
import { DataGridColumnVisibility } from '@/components/reui/data-grid/data-grid-column-visibility'
import { BrailleLoader } from '@/components/ui/braille-loader'
import { ThinkingIndicator } from '@/components/ui/thinking-indicator'
import { Button } from '@/components/ui/button'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/motion/tabs'
import { SortableHeader, StatusDot, EmptyIcon } from '@/components/results-table-parts'
import { ScanFilterBar } from '@/components/scan-filter-bar'
import type { Filter } from '@/components/reui/filters'
import { relativeTime } from '@/lib/relative-time'
import { useGridPreference } from '@/hooks/use-grid-preference'

const RESULTS_TABS = ['latest', 'all-time'] as const
type ResultsTab = typeof RESULTS_TABS[number]

export const Route = createFileRoute('/results/')({
  component: ResultsPage,
  validateSearch: (search: Record<string, unknown>): { tab: ResultsTab } => ({
    tab: RESULTS_TABS.includes(search.tab as ResultsTab) ? (search.tab as ResultsTab) : 'latest',
  }),
})

const PAGE_SIZE = 25

/* ─── Latest Scan tab: tree rows (domain parent + per-DNS-server children) ─
   Real tree rows via getExpandedRowModel/subRows, so per-server rows share
   the same column set (and DataGridTableRowExpand's depth indent) as the
   domain row instead of a bespoke, headerless inner table. */

type DomainRow = { kind: 'domain'; group: GroupedResult; subRows: ServerRow[] }
type ServerRow = { kind: 'server'; result: ScanResult }
type ResultRow = DomainRow | ServerRow

function ServerIpCell({ result }: { result: ScanResult }) {
  return (
    <div className="ip-meta">
      {result.resolved_ip ? (
        <span className="ip-value">{result.resolved_ip}</span>
      ) : (
        <span className="empty-cell" aria-label="Not resolved">—</span>
      )}
      {result.resolved_ipv6 && <span className="ip-meta-secondary">{result.resolved_ipv6}</span>}
      {result.resolved_asn > 0 && (
        <span className="ip-meta-secondary">
          AS{result.resolved_asn}{result.resolved_org && ` — ${result.resolved_org}`}
        </span>
      )}
      {result.resolved_netname && <span className="ip-meta-secondary">{result.resolved_netname}</span>}
    </div>
  )
}

function ServerEvidenceCell({
  result,
  pendingScreenshotIds,
  screenshotErrors,
  screenshotsBlocked,
  onRequestScreenshot,
  onViewScreenshot,
}: {
  result: ScanResult
  pendingScreenshotIds: Set<number>
  screenshotErrors: Record<number, string>
  screenshotsBlocked: boolean
  onRequestScreenshot: (results: ScanResult[]) => void
  onViewScreenshot: (result: ScanResult) => void
}) {
  if (result.screenshot_url) {
    return (
      <button
        type="button"
        className="screenshot-icon-btn"
        onClick={() => onViewScreenshot(result)}
        aria-label={`View screenshot for ${result.dns_server.name}`}
        title="View screenshot"
      >
        <ImageIcon className="screenshot-icon" aria-hidden="true" />
      </button>
    )
  }
  if (pendingScreenshotIds.has(result.id)) {
    return (
      <span className="screenshot-pending" aria-live="polite" aria-label="Requesting screenshot">
        <BrailleLoader variant="typing" fontSize={13} />
      </span>
    )
  }
  if (!result.compliant) {
    return (
      <button
        type="button"
        className="screenshot-icon-btn"
        onClick={() => onRequestScreenshot([result])}
        disabled={screenshotsBlocked}
        title={screenshotErrors[result.id] ?? 'Take screenshot'}
        aria-label={`Request screenshot for ${result.dns_server.name}`}
      >
        <Camera className="screenshot-icon" aria-hidden="true" />
      </button>
    )
  }
  return <span className="empty-cell" aria-label="No screenshot">—</span>
}

/* ─── Latest Scan tab ─────────────────────────────────────────────────────── */

function LatestScanTab() {
  const { scanning, refreshSignal, progress } = useScan()

  const [results, setResults] = useState<ScanResult[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [filters, setFilters] = useState<Filter<string>[]>([])
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<ExpandedState>({})
  const [sorting, setSorting] = useState<SortingState>([{ id: 'status', desc: true }])
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })
  // Session-only, not persisted (unlike sorting below) — mirrors urls.tsx's
  // column-visibility toggle.
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})

  // Only sorting is persisted here — unlike urls.tsx, this grid doesn't
  // persist column visibility or page size.
  const { ready: gridPrefReady } = useGridPreference('results', { sorting }, { setSorting })

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

  const dnsServerOptions = useMemo(() => dnsServers.map(name => ({ value: name, label: name })), [dnsServers])

  const statusFilter = filters.find(f => f.field === 'status')?.values[0] as 'violations' | 'compliant' | undefined
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

  const treeData = useMemo<DomainRow[]>(() => filtered.map(group => ({
    kind: 'domain',
    group,
    subRows: group.results.map(result => ({ kind: 'server', result })),
  })), [filtered])

  const columns = useMemo<ColumnDef<ResultRow>[]>(() => [
    {
      id: 'expand',
      header: () => null,
      size: 30,
      enableSorting: false,
      enableHiding: false,
      meta: {
        headerClassName: 'col-expand',
        cellClassName: 'col-expand',
        skeleton: <span className="skeleton" style={{ width: 16, height: 16, borderRadius: 3 }} />,
      },
      cell: ({ row }) => (
        <DataGridTableRowExpand row={row}>
          <ChevronRight className={`expand-icon${row.getIsExpanded() ? ' expanded' : ''}`} />
        </DataGridTableRowExpand>
      ),
    },
    {
      id: 'domain',
      accessorFn: r => r.kind === 'domain' ? r.group.hostname : r.result.dns_server.name,
      header: ({ column }) => <SortableHeader column={column} title="Domain" />,
      enableHiding: false,
      meta: {
        headerTitle: 'Domain',
        headerClassName: 'col-domain th-left',
        cellClassName: 'col-domain',
        skeleton: <span className="skeleton" style={{ width: 180, height: 14 }} />,
      },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'server') {
          return <span className="dns-name">{original.result.dns_server.name}</span>
        }
        const { group } = original
        return (
          <PreviewLinkCard href={`https://${group.hostname}`}>
            <PreviewLinkCardTrigger>
              <span className="hostname" title={group.url}>{group.hostname}</span>
            </PreviewLinkCardTrigger>
            <PreviewLinkCardPanel>
              <PreviewLinkCardImage />
            </PreviewLinkCardPanel>
          </PreviewLinkCard>
        )
      },
    },
    {
      id: 'status',
      accessorFn: r => r.kind === 'domain' ? r.group.violationCount : (r.result.compliant ? 0 : 1),
      header: ({ column }) => <SortableHeader column={column} title="Status" />,
      meta: {
        headerTitle: 'Status',
        headerClassName: 'col-status th-left',
        cellClassName: 'col-status',
        skeleton: <span className="skeleton" style={{ width: 100, height: 20, borderRadius: 4 }} />,
      },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'server') return <StatusDot compliant={original.result.compliant} />
        const { violationCount, totalCount } = original.group
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
      cell: ({ row }) => row.original.kind === 'server' ? <ServerIpCell result={row.original.result} /> : null,
    },
    {
      id: 'error',
      header: 'Error',
      enableSorting: false,
      meta: { headerClassName: 'col-error th-left', cellClassName: 'col-error' },
      cell: ({ row }) => {
        if (row.original.kind !== 'server') return null
        const { error } = row.original.result
        return error ? <span className="col-error-text" title={error}>{error}</span> : <span className="empty-cell">—</span>
      },
    },
    {
      id: 'lastScanned',
      accessorFn: r => r.kind === 'domain' ? r.group.latestScannedAt : r.result.scanned_at,
      header: ({ column }) => <SortableHeader column={column} title="Last scanned" />,
      meta: { headerTitle: 'Last scanned', headerClassName: 'col-last-scanned th-left', cellClassName: 'col-last-scanned' },
      cell: ({ row }) => {
        const at = row.original.kind === 'domain' ? row.original.group.latestScannedAt : row.original.result.scanned_at
        return at ? (
          <span title={new Date(at).toLocaleString()}>{relativeTime(at)}</span>
        ) : (
          <span className="empty-cell">—</span>
        )
      },
    },
    {
      id: 'actions',
      header: 'Actions',
      enableSorting: false,
      enableHiding: false,
      meta: { headerClassName: 'col-evidence th-center', cellClassName: 'col-evidence text-center' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'server') {
          return (
            <ServerEvidenceCell
              result={original.result}
              pendingScreenshotIds={pendingScreenshotIds}
              screenshotErrors={screenshotErrors}
              screenshotsBlocked={screenshotsBlocked}
              onRequestScreenshot={requestScreenshot}
              onViewScreenshot={setPreviewScreenshot}
            />
          )
        }
        const { group } = original
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
    data: treeData,
    columns,
    state: { expanded, sorting, pagination, columnVisibility },
    onExpandedChange: setExpanded,
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    onColumnVisibilityChange: setColumnVisibility,
    getRowId: r => r.kind === 'domain' ? r.group.url : `sr:${r.result.id}`,
    getSubRows: r => r.kind === 'domain' ? r.subRows : undefined,
    getRowCanExpand: row => row.original.kind === 'domain',
    paginateExpandedRows: false,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  })

  const pageCount = table.getPageCount()

  // Holds the grid in its loading state until the saved sort order has been
  // applied, so it renders once already sorted instead of flashing the
  // default order first.
  const gridLoading = loading || !gridPrefReady

  return (
    <>
      {!loading && lastScan && (
        <p className="page-subtitle mb-2">Last scan: {lastScan}</p>
      )}

      {scanning && (
        <div className="scan-banner mb-2 flex items-center gap-4">
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
        <div className="flex flex-col items-stretch w-full gap-4">
          <ScanFilterBar
            search={search}
            onSearchChange={setSearch}
            filters={filters}
            onFiltersChange={setFilters}
            dnsServerOptions={dnsServerOptions}
          >
            <div style={{ marginLeft: 'auto' }}>
              <DataGridColumnVisibility table={table} trigger={<Button variant="outline">Columns</Button>} />
            </div>
          </ScanFilterBar>

          <div className="results-wrap w-full">
            {!gridLoading && groups.length === 0 ? (
              <div className="empty-state">
                <EmptyIcon />
                <p className="empty-heading">No results yet</p>
                <p className="empty-body">
                  No scan has been run. Use Run Scan to begin compliance monitoring.
                </p>
              </div>
            ) : !gridLoading && filtered.length === 0 ? (
              <div className="empty-state" style={{ padding: '3rem 0' }}>
                <p className="empty-heading">No results match the current filters</p>
              </div>
            ) : (
              <DataGrid
                table={table}
                recordCount={filtered.length}
                isLoading={gridLoading}
                onRowClick={row => { if (row.kind === 'domain') table.getRow(row.group.url).toggleExpanded() }}
                rowClassName={row => row.kind === 'server' && !row.result.compliant ? 'violation-row' : undefined}
                tableClassNames={{ base: 'results-table' }}
              >
                <DataGridContainer className="overflow-visible">
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
    </>
  )
}

/* ─── All Time tab: server breakdown (expanded nested table) ────────────── */

function DomainServerBreakdown({ domain }: { domain: string }) {
  const navigate = useNavigate()
  const [servers, setServers] = useState<DomainServerSummary[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    fetchDomainServerSummaries(domain)
      .then(res => { if (!cancelled) setServers(res) })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load server breakdown') })
    return () => { cancelled = true }
  }, [domain])

  if (error) {
    return <p className="error-message p-4">{error}</p>
  }
  if (!servers) {
    return (
      <div className="flex items-center justify-center p-4">
        <BrailleLoader variant="typing" fontSize={13} />
      </div>
    )
  }
  if (servers.length === 0) {
    return <p className="empty-cell p-4">No per-server history</p>
  }

  return (
    <Table className="results-table" aria-label={`Per-DNS-server history for ${domain}`}>
      <TableHeader>
        <TableRow>
          <TableHead className="th-left" scope="col">DNS Server</TableHead>
          <TableHead className="th-left" scope="col">ISP</TableHead>
          <TableHead className="th-left" scope="col">Address</TableHead>
          <TableHead className="col-status th-left" scope="col">Compliance</TableHead>
          <TableHead className="col-scan-id th-left" scope="col">Total Scans</TableHead>
          <TableHead className="col-last-scanned th-left" scope="col">Last Scanned</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {servers.map(s => {
          const pct = s.total_scans > 0 ? Math.round((s.compliant_scans / s.total_scans) * 100) : 0
          return (
            <TableRow
              key={s.dns_server_id}
              className="sub-row cursor-pointer hover:bg-stone-panel transition-colors duration-150 ease-snappy"
              onClick={() => navigate({
                to: '/domain/$url',
                params: { url: domain },
                search: { tab: 'history', server: s.dns_server_name },
              })}
            >
              <TableCell><span className="dns-name">{s.dns_server_name}</span></TableCell>
              <TableCell className="text-stone-muted text-[0.8rem]">{s.isp}</TableCell>
              <TableCell><span className="dns-server-addr">{s.address}</span></TableCell>
              <TableCell className="col-status">
                <div className="server-bar-wrap">
                  <div className="server-bar" role="presentation">
                    <div className="server-bar-fill" style={{ width: `${pct}%` }} />
                  </div>
                  <span className="server-count">{pct}%</span>
                </div>
              </TableCell>
              <TableCell className="col-scan-id">{s.total_scans}</TableCell>
              <TableCell className="col-last-scanned">
                <span title={new Date(s.last_scanned_at).toLocaleString()}>{relativeTime(s.last_scanned_at)}</span>
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}

const allTimeSkeletonWidths = [180, 90, 60, 100]

const allTimeColumns: ColumnDef<DomainSummary>[] = [
  {
    id: 'expand',
    header: () => null,
    size: 30,
    enableHiding: false,
    meta: {
      headerClassName: 'col-expand',
      cellClassName: 'col-expand',
      expandedContent: (row: DomainSummary) => <DomainServerBreakdown domain={row.url} />,
    },
    cell: ({ row }) => (
      <button
        type="button"
        className="expand-btn"
        onClick={e => { e.stopPropagation(); row.getToggleExpandedHandler()() }}
        aria-expanded={row.getIsExpanded()}
        aria-label={`${row.getIsExpanded() ? 'Collapse' : 'Expand'} per-server breakdown for ${row.original.url}`}
      >
        <ChevronRight className={`expand-icon${row.getIsExpanded() ? ' expanded' : ''}`} />
      </button>
    ),
  },
  {
    accessorKey: 'url',
    header: 'Domain',
    enableHiding: false,
    meta: {
      headerClassName: 'col-domain th-left',
      cellClassName: 'col-domain pl-4',
      skeleton: <span className="skeleton" style={{ width: allTimeSkeletonWidths[0], height: 14 }} />,
    },
    cell: ({ getValue }) => <span className="hostname">{getValue<string>()}</span>,
  },
  {
    id: 'compliance',
    header: 'Compliance',
    meta: {
      headerClassName: 'col-status th-left',
      cellClassName: 'col-status pl-4',
      skeleton: <span className="skeleton" style={{ width: allTimeSkeletonWidths[1], height: 20, borderRadius: 4 }} />,
    },
    cell: ({ row }) => {
      const { total_scans, compliant_scans } = row.original
      const pct = total_scans > 0 ? Math.round((compliant_scans / total_scans) * 100) : 0
      return (
        <div className="server-bar-wrap">
          <div className="server-bar" role="presentation">
            <div className="server-bar-fill" style={{ width: `${pct}%` }} />
          </div>
          <span className="server-count">{pct}%</span>
        </div>
      )
    },
  },
  {
    accessorKey: 'total_scans',
    header: 'Total Scans',
    meta: {
      headerClassName: 'col-scan-id th-left',
      cellClassName: 'col-scan-id pl-4',
      skeleton: <span className="skeleton" style={{ width: allTimeSkeletonWidths[2], height: 14 }} />,
    },
  },
  {
    accessorKey: 'last_scanned_at',
    header: 'Last Scanned',
    meta: {
      headerClassName: 'col-last-scanned th-left',
      cellClassName: 'col-last-scanned pl-4',
      skeleton: <span className="skeleton" style={{ width: allTimeSkeletonWidths[3], height: 14 }} />,
    },
    cell: ({ getValue }) => {
      const value = getValue<string>()
      return <span title={new Date(value).toLocaleString()}>{relativeTime(value)}</span>
    },
  },
  {
    id: 'actions',
    header: 'Actions',
    enableHiding: false,
    meta: {
      headerClassName: 'col-evidence th-center',
      cellClassName: 'col-evidence text-center',
    },
    cell: ({ row }) => (
      <Link
        to="/domain/$url"
        params={{ url: row.original.url }}
        search={{ tab: 'overview' }}
        className="btn-row-history"
        aria-label={`View overview for ${row.original.url}`}
        onClick={e => e.stopPropagation()}
      >
        <GripIcon className="btn-row-history-icon" size={16} />
      </Link>
    ),
  },
]

/* ─── All Time tab ────────────────────────────────────────────────────────── */

function AllTimeTab() {
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [dnsServers, setDnsServers] = useState<DNSServer[]>([])
  const [filters, setFilters] = useState<Filter<string>[]>([])
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})

  const [domains, setDomains] = useState<DomainSummary[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })
  const [expanded, setExpanded] = useState<ExpandedState>({})

  useEffect(() => { fetchDnsServers().then(setDnsServers).catch(() => {}) }, [])

  // Debounced so every keystroke doesn't fire a request — the search hits
  // fetchDomainSummaries's server-side `q` param, unlike Latest Scan's
  // client-side filter.
  useEffect(() => {
    const handle = setTimeout(() => setDebouncedSearch(search.trim()), 300)
    return () => clearTimeout(handle)
  }, [search])

  const dnsServerOptions = useMemo(() => dnsServers.map(s => ({ value: String(s.id), label: s.name })), [dnsServers])

  const statusFilter = filters.find(f => f.field === 'status')?.values[0] as 'compliant' | 'violations' | undefined
  const dnsServerFilterValue = filters.find(f => f.field === 'dns_server')?.values[0]
  const dnsServerId = dnsServerFilterValue ? Number(dnsServerFilterValue) : undefined

  useEffect(() => { setPagination(p => ({ ...p, pageIndex: 0 })) }, [statusFilter, dnsServerId, debouncedSearch])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    fetchDomainSummaries(pagination.pageIndex + 1, pagination.pageSize, {
      status: statusFilter,
      dnsServerId,
      search: debouncedSearch || undefined,
    })
      .then(res => {
        if (cancelled) return
        setDomains(res.domains)
        setTotal(res.total)
        setError(null)
      })
      .catch(err => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : 'Failed to load domains')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [pagination.pageIndex, pagination.pageSize, statusFilter, dnsServerId, debouncedSearch])

  const pageCount = useMemo(() => Math.max(1, Math.ceil(total / pagination.pageSize)), [total, pagination.pageSize])

  const table = useReactTable({
    data: domains,
    columns: allTimeColumns,
    state: { pagination, expanded, columnVisibility },
    onPaginationChange: setPagination,
    onExpandedChange: setExpanded,
    onColumnVisibilityChange: setColumnVisibility,
    manualPagination: true,
    pageCount,
    getRowCanExpand: () => true,
    getCoreRowModel: getCoreRowModel(),
    getRowId: row => row.url,
  })

  const goToDomain = (domain: string) =>
    navigate({ to: '/domain/$url', params: { url: domain }, search: { tab: 'overview' } })

  const hasActiveFilter = Boolean(statusFilter || dnsServerId || debouncedSearch)

  return (
    <div className="flex flex-col items-stretch w-full gap-4">
      <ScanFilterBar
        search={search}
        onSearchChange={setSearch}
        filters={filters}
        onFiltersChange={setFilters}
        dnsServerOptions={dnsServerOptions}
      >
        <div style={{ marginLeft: 'auto' }}>
          <DataGridColumnVisibility table={table} trigger={<Button variant="outline">Columns</Button>} />
        </div>
      </ScanFilterBar>

      {error ? (
        <div className="error-state">
          <p className="error-message">{error}</p>
        </div>
      ) : !loading && domains.length === 0 ? (
        <div className="empty-state" style={{ padding: '3rem 0' }}>
          <EmptyIcon />
          <p className="empty-heading">{hasActiveFilter ? 'No domains match the current filters' : 'No scan history yet'}</p>
          <p className="empty-body">
            {hasActiveFilter
              ? 'Try clearing a filter to widen the search.'
              : "Domains will appear here once they've been scanned at least once."}
          </p>
        </div>
      ) : (
        <div className="results-wrap w-full">
          <DataGrid
            table={table}
            recordCount={total}
            isLoading={loading}
            onRowClick={row => goToDomain(row.url)}
            tableClassNames={{ base: 'results-table' }}
          >
            <DataGridContainer>
              <DataGridTable />
            </DataGridContainer>
          </DataGrid>
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
      )}
    </div>
  )
}

/* ─── Results Page (tab shell) ───────────────────────────────────────────── */

function ResultsPage() {
  const { tab } = Route.useSearch()
  const navigate = useNavigate()

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title">Compliance Results</h1>
      </div>

      <Tabs
        value={tab}
        onValueChange={next => navigate({ to: '/results', search: { tab: next as ResultsTab } })}
        variant="underline"
      >
        <TabsList>
          <TabsTrigger value="latest">Latest Scan</TabsTrigger>
          <TabsTrigger value="all-time">All Time</TabsTrigger>
        </TabsList>

        <TabsContent value="latest">
          <LatestScanTab />
        </TabsContent>
        <TabsContent value="all-time">
          <AllTimeTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}
