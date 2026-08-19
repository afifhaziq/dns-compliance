import { useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import {
  type ColumnDef,
  type PaginationState,
  type VisibilityState,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { fetchCaseLetters } from '@/api/cases'
import type { CaseLetterEntry } from '@/api/types'
import { DataGrid, DataGridContainer } from '@/components/reui/data-grid/data-grid'
import { DataGridTable } from '@/components/reui/data-grid/data-grid-table'
import { DataGridColumnVisibility } from '@/components/reui/data-grid/data-grid-column-visibility'
import { Button } from '@/components/ui/button'
import { EmptyIcon } from '@/components/results-table-parts'

export const Route = createFileRoute('/docs')({
  component: DocsPage,
})

const PAGE_SIZE = 25

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleDateString()
}

const columns: ColumnDef<CaseLetterEntry>[] = [
  {
    accessorKey: 'letter_date',
    header: 'Letter Date',
    cell: ({ getValue }) => formatDate(getValue<string>()),
  },
  { accessorKey: 'type', header: 'Type' },
  {
    accessorKey: 'reference_number',
    header: 'Reference No.',
    cell: ({ getValue }) => getValue<string>() || '—',
  },
  { accessorKey: 'recipient', header: 'Recipient', cell: ({ getValue }) => getValue<string>() || '—' },
  { accessorKey: 'subject', header: 'Subject', cell: ({ getValue }) => getValue<string>() || '—' },
  { accessorKey: 'requestor', header: 'Requestor', cell: ({ getValue }) => getValue<string>() || '—' },
  { accessorKey: 'workflow_status', header: 'Status', cell: ({ getValue }) => getValue<string>() || '—' },
  { accessorKey: 'received_at', header: 'Received', cell: ({ getValue }) => formatDate(getValue<string>()) },
  { accessorKey: 'submitted_at', header: 'Submission', cell: ({ getValue }) => formatDate(getValue<string>()) },
  { accessorKey: 'department_name', header: 'Dept.' },
  {
    id: 'urls',
    header: 'Link',
    cell: ({ row }) => {
      const urls = row.original.urls ?? []
      if (urls.length === 0) return '—'
      if (urls.length === 1) return urls[0]
      return `${urls[0]} +${urls.length - 1} more`
    },
  },
  { accessorKey: 'remarks', header: 'Remarks', cell: ({ getValue }) => getValue<string>() || '—' },
]

// ponytail: no search/filter bar yet — /api/case-letters only supports
// pagination today. Add a server-side `q` param (mirroring
// fetchDomainSummaries) if the Docs page needs to search across pages.
function DocsPage() {
  const [letters, setLetters] = useState<CaseLetterEntry[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    fetchCaseLetters(pagination.pageIndex + 1, pagination.pageSize)
      .then(res => {
        if (cancelled) return
        setLetters(res.letters)
        setTotal(res.total)
        setError(null)
      })
      .catch(err => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : 'Failed to load documents')
      })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [pagination.pageIndex, pagination.pageSize])

  const pageCount = useMemo(() => Math.max(1, Math.ceil(total / pagination.pageSize)), [total, pagination.pageSize])

  const table = useReactTable({
    data: letters,
    columns,
    state: { pagination, columnVisibility },
    onPaginationChange: setPagination,
    onColumnVisibilityChange: setColumnVisibility,
    manualPagination: true,
    pageCount,
    getCoreRowModel: getCoreRowModel(),
    getRowId: row => String(row.id),
  })

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title">Docs</h1>
      </div>

      <div className="flex flex-col items-stretch w-full gap-4">
        <div style={{ marginLeft: 'auto' }}>
          <DataGridColumnVisibility table={table} trigger={<Button variant="outline">Columns</Button>} />
        </div>

        {error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
          </div>
        ) : !loading && letters.length === 0 ? (
          <div className="empty-state" style={{ padding: '3rem 0' }}>
            <EmptyIcon />
            <p className="empty-heading">No documents yet</p>
            <p className="empty-body">Memos and Notices appear here once a case has letters recorded against it.</p>
          </div>
        ) : (
          <div className="results-wrap w-full">
            <DataGrid table={table} recordCount={total} isLoading={loading} tableClassNames={{ base: 'results-table' }}>
              <DataGridContainer className="overflow-x-auto overflow-y-hidden">
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
    </div>
  )
}
