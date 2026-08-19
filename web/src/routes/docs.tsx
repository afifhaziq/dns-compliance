import { useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import {
  type ColumnDef,
  type SortingState,
  type PaginationState,
  type VisibilityState,
  getCoreRowModel,
  getSortedRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { fetchAllCaseLetters } from '@/api/cases'
import { fetchDepartmentsOpen } from '@/api/departments'
import type { CaseLetterEntry, Department } from '@/api/types'
import { LETTER_TYPE_OPTIONS as CASE_LETTER_TYPE_OPTIONS } from '@/lib/case-options'
import { DataGrid, DataGridContainer } from '@/components/reui/data-grid/data-grid'
import { DataGridTable } from '@/components/reui/data-grid/data-grid-table'
import { DataGridColumnVisibility } from '@/components/reui/data-grid/data-grid-column-visibility'
import { DataGridPagination } from '@/components/reui/data-grid/data-grid-pagination'
import { Filters, type Filter, type FilterFieldConfig } from '@/components/reui/filters'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { SortableHeader, EmptyIcon } from '@/components/results-table-parts'
import { useGridPreference } from '@/hooks/use-grid-preference'

export const Route = createFileRoute('/docs')({
  component: DocsPage,
})

const PAGE_SIZE = 25
const IS_ONLY = [{ value: 'is', label: 'is' }]
const WORKFLOW_STATUS_OPTIONS = ['Draft', 'Pending Legal', 'Pending TSC', 'Submitted']

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleDateString()
}

function DocsPage() {
  const [letters, setLetters] = useState<CaseLetterEntry[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<Filter<string>[]>([])
  const [sorting, setSorting] = useState<SortingState>([])
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})

  const { ready: gridPrefReady } = useGridPreference(
    'docs',
    { sorting, columnVisibility, pageSize: pagination.pageSize },
    {
      setSorting,
      setColumnVisibility,
      setPageSize: pageSize => setPagination(p => ({ ...p, pageSize })),
    }
  )

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    Promise.all([fetchAllCaseLetters(), fetchDepartmentsOpen()])
      .then(([l, d]) => {
        if (cancelled) return
        setLetters(l)
        setDepartments(d)
        setError(null)
      })
      .catch(err => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : 'Failed to load documents')
      })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [])

  const filterFields = useMemo<FilterFieldConfig<string>[]>(() => [
    { key: 'type', label: 'Type', type: 'select', operators: IS_ONLY, options: CASE_LETTER_TYPE_OPTIONS.map(t => ({ value: t, label: t })) },
    { key: 'department', label: 'Department', type: 'select', operators: IS_ONLY, options: departments.map(d => ({ value: d.name, label: d.name })) },
    { key: 'workflow_status', label: 'Workflow Status', type: 'select', operators: IS_ONLY, options: WORKFLOW_STATUS_OPTIONS.map(s => ({ value: s, label: s })) },
  ], [departments])

  const typeFilter = filters.find(f => f.field === 'type')?.values[0]
  const deptFilter = filters.find(f => f.field === 'department')?.values[0]
  const workflowFilter = filters.find(f => f.field === 'workflow_status')?.values[0]

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    const matchesQuery = (l: CaseLetterEntry) =>
      !query ||
      (l.reference_number ?? '').toLowerCase().includes(query) ||
      (l.recipient ?? '').toLowerCase().includes(query) ||
      (l.subject ?? '').toLowerCase().includes(query) ||
      (l.requestor ?? '').toLowerCase().includes(query) ||
      (l.remarks ?? '').toLowerCase().includes(query) ||
      (l.urls ?? []).some(u => u.toLowerCase().includes(query))
    return letters.filter(l =>
      matchesQuery(l) &&
      (!typeFilter || l.type === typeFilter) &&
      (!deptFilter || l.department_name === deptFilter) &&
      (!workflowFilter || l.workflow_status === workflowFilter)
    )
  }, [letters, search, typeFilter, deptFilter, workflowFilter])

  useEffect(() => { setPagination(p => ({ ...p, pageIndex: 0 })) }, [search, typeFilter, deptFilter, workflowFilter])

  const columns = useMemo<ColumnDef<CaseLetterEntry>[]>(() => [
    {
      id: 'letter_date',
      accessorFn: l => l.letter_date ?? '',
      header: ({ column }) => <SortableHeader column={column} title="Letter Date" />,
      meta: { headerTitle: 'Letter Date' },
      cell: ({ row }) => formatDate(row.original.letter_date),
    },
    { id: 'type', accessorFn: l => l.type, header: 'Type', meta: { headerTitle: 'Type' } },
    {
      id: 'reference_number',
      accessorFn: l => l.reference_number ?? '',
      header: 'Reference No.',
      meta: { headerTitle: 'Reference No.' },
      cell: ({ row }) => row.original.reference_number || '—',
    },
    {
      id: 'recipient',
      accessorFn: l => l.recipient ?? '',
      header: 'Recipient',
      meta: { headerTitle: 'Recipient' },
      cell: ({ row }) => row.original.recipient || '—',
    },
    {
      id: 'subject',
      accessorFn: l => l.subject ?? '',
      header: 'Subject',
      meta: { headerTitle: 'Subject' },
      cell: ({ row }) => row.original.subject || '—',
    },
    {
      id: 'requestor',
      accessorFn: l => l.requestor ?? '',
      header: 'Requestor',
      meta: { headerTitle: 'Requestor' },
      cell: ({ row }) => row.original.requestor || '—',
    },
    {
      id: 'workflow_status',
      accessorFn: l => l.workflow_status ?? '',
      header: 'Status',
      meta: { headerTitle: 'Status' },
      cell: ({ row }) => row.original.workflow_status || '—',
    },
    {
      id: 'received_at',
      accessorFn: l => l.received_at ?? '',
      header: ({ column }) => <SortableHeader column={column} title="Received" />,
      meta: { headerTitle: 'Received' },
      cell: ({ row }) => formatDate(row.original.received_at),
    },
    {
      id: 'submitted_at',
      accessorFn: l => l.submitted_at ?? '',
      header: ({ column }) => <SortableHeader column={column} title="Submission" />,
      meta: { headerTitle: 'Submission' },
      cell: ({ row }) => formatDate(row.original.submitted_at),
    },
    { id: 'department_name', accessorFn: l => l.department_name, header: 'Dept.', meta: { headerTitle: 'Dept.' } },
    {
      id: 'urls',
      accessorFn: l => (l.urls ?? []).join(', '),
      header: 'Link',
      meta: { headerTitle: 'Link' },
      cell: ({ row }) => {
        const urls = row.original.urls ?? []
        if (urls.length === 0) return '—'
        if (urls.length === 1) return urls[0]
        return `${urls[0]} +${urls.length - 1} more`
      },
    },
    {
      id: 'remarks',
      accessorFn: l => l.remarks ?? '',
      header: 'Remarks',
      meta: { headerTitle: 'Remarks' },
      cell: ({ row }) => row.original.remarks || '—',
    },
  ], [])

  const table = useReactTable({
    data: filtered,
    columns,
    state: { sorting, pagination, columnVisibility },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    onColumnVisibilityChange: setColumnVisibility,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getRowId: row => String(row.id),
  })

  const gridLoading = loading || !gridPrefReady

  return (
    <div className="mx-20 mt-10 mb-10">
      <div className="page-header">
        <h1 className="page-title mb-4">Docs</h1>
        <p className="page-subtitle">{!loading && `${letters.length} documents`}</p>
      </div>

      {error ? (
        <div className="error-state">
          <p className="error-message">{error}</p>
        </div>
      ) : !gridLoading && letters.length === 0 ? (
        <div className="empty-state">
          <EmptyIcon />
          <p className="empty-heading">No documents yet</p>
          <p className="empty-body">Memos and Notices appear here once a case has letters recorded against it.</p>
        </div>
      ) : (
        <div className="flex flex-col items-stretch w-full gap-4 mt-4">
          <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full">
            <Input
              type="search"
              placeholder="Search documents..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="max-w-64"
              aria-label="Search documents"
            />
            <Filters filters={filters} fields={filterFields} onChange={setFilters} />
            <div style={{ marginLeft: 'auto' }}>
              <DataGridColumnVisibility table={table} trigger={<Button variant="outline">Columns</Button>} />
            </div>
          </div>

          <div className="results-wrap w-full">
            {!gridLoading && filtered.length === 0 ? (
              <div className="empty-state" style={{ padding: '3rem 0' }}>
                <p className="empty-heading">No documents match the current filters</p>
              </div>
            ) : (
              <DataGrid
                table={table}
                recordCount={filtered.length}
                isLoading={gridLoading}
                tableClassNames={{ base: 'results-table' }}
              >
                <DataGridContainer className="overflow-x-auto overflow-y-visible mb-5">
                  <DataGridTable />
                </DataGridContainer>
                <DataGridPagination sizes={[10, 25, 50, 100]} />
              </DataGrid>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
