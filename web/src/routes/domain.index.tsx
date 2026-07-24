import { useEffect, useMemo, useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { type ColumnDef, type ExpandedState, type PaginationState, getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { FaviconSearch } from '@/components/unlumen-ui/favicon-search'
import { DataGrid, DataGridContainer } from '@/components/reui/data-grid/data-grid'
import { DataGridTable } from '@/components/reui/data-grid/data-grid-table'
import { fetchDomainSummaries, fetchDomainServerSummaries } from '@/api/domains'
import type { DomainSummary, DomainServerSummary } from '@/api/types'
import { EmptyIcon } from '@/components/results-table-parts'
import { relativeTime } from '@/lib/relative-time'
import { ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { ChevronRight } from '@/components/ui/chevron-right'
import { BrailleLoader } from '@/components/ui/braille-loader'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'

export const Route = createFileRoute('/domain/')({ component: DomainPickerPage })

const PAGE_SIZE = 25

const skeletonWidths = [180, 90, 60, 100]

/* ─── Server breakdown (expanded nested table) ──────────────────────────── */

function DomainServerBreakdown({ domain }: { domain: string }) {
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
          <TableHead className="col-last-scanned th-left w-2" scope="col">Last Scanned</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {servers.map(s => {
          const pct = s.total_scans > 0 ? Math.round((s.compliant_scans / s.total_scans) * 100) : 0
          return (
            <TableRow key={s.dns_server_id} className="sub-row">
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

/* ─── Domain history table ───────────────────────────────────────────────── */

const columns: ColumnDef<DomainSummary>[] = [
  {
    id: 'expand',
    header: () => null,
    size: 30,
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
    meta: {
      headerClassName: 'col-domain th-left',
      cellClassName: 'col-domain',
      skeleton: <span className="skeleton" style={{ width: skeletonWidths[0], height: 14 }} />,
    },
    cell: ({ getValue }) => <span className="hostname">{getValue<string>()}</span>,
  },
  {
    id: 'compliance',
    header: 'Compliance',
    meta: {
      headerClassName: 'col-status th-left',
      cellClassName: 'col-status',
      skeleton: <span className="skeleton" style={{ width: skeletonWidths[1], height: 20, borderRadius: 4 }} />,
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
      cellClassName: 'col-scan-id',
      skeleton: <span className="skeleton" style={{ width: skeletonWidths[2], height: 14 }} />,
    },
  },
  {
    accessorKey: 'last_scanned_at',
    header: 'Last Scanned',
    meta: {
      headerClassName: 'col-last-scanned th-left',
      cellClassName: 'col-last-scanned',
      skeleton: <span className="skeleton" style={{ width: skeletonWidths[3], height: 14 }} />,
    },
    cell: ({ getValue }) => {
      const value = getValue<string>()
      return <span title={new Date(value).toLocaleString()}>{relativeTime(value)}</span>
    },
  },
]

function DomainHistoryTable() {
  const navigate = useNavigate()
  const [domains, setDomains] = useState<DomainSummary[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })
  const [expanded, setExpanded] = useState<ExpandedState>({})

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    fetchDomainSummaries(pagination.pageIndex + 1, pagination.pageSize)
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
  }, [pagination.pageIndex, pagination.pageSize])

  const pageCount = useMemo(() => Math.max(1, Math.ceil(total / pagination.pageSize)), [total, pagination.pageSize])

  const table = useReactTable({
    data: domains,
    columns,
    state: { pagination, expanded },
    onPaginationChange: setPagination,
    onExpandedChange: setExpanded,
    manualPagination: true,
    pageCount,
    getRowCanExpand: () => true,
    getCoreRowModel: getCoreRowModel(),
    getRowId: row => row.url,
  })

  const goToDomain = (domain: string) =>
    navigate({ to: '/domain/$url', params: { url: domain }, search: { tab: 'overview' } })

  return (
    <div className="dash-section mt-6">
      <div className="flex items-center justify-between mb-2">
        <span className="text-sm text-stone-muted">Domain history</span>
      </div>

      {error ? (
        <div className="error-state">
          <p className="error-message">{error}</p>
        </div>
      ) : !loading && domains.length === 0 ? (
        <div className="empty-state" style={{ padding: '3rem 0' }}>
          <EmptyIcon />
          <p className="empty-heading">No scan history yet</p>
          <p className="empty-body">Domains will appear here once they've been scanned at least once.</p>
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

function DomainPickerPage() {
  const navigate = useNavigate()

  const goToDomain = (domain: string) =>
    navigate({ to: '/domain/$url', params: { url: domain }, search: { tab: 'overview' } })

  return (
    <div className="mx-20 mt-10 mb-10">
      <div className="page-header px-0">
        <h1 className="page-title mb-2">Domain Lookup</h1>
        <p className="page-subtitle">Search for a domain to view its compliance history.</p>
      </div>

      <div className="dash-section">
        <FaviconSearch
          placeholder="Enter a domain to look up…"
          className="w-96"
          onSearch={(_value, domain) => goToDomain(domain)}
        />
      </div>

      <DomainHistoryTable />
    </div>
  )
}
