import { useCallback, useEffect, useMemo, useState } from 'react'
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
import { fetchAllCaseLetters, createCase, addCaseLetter, addUrlToCase } from '@/api/cases'
import { createUrl, fetchUrls } from '@/api/urls'
import { fetchDepartmentsOpen } from '@/api/departments'
import { fetchRecipients } from '@/api/recipients'
import { fetchRequestors } from '@/api/requestors'
import type { CaseLetterEntry, Department, Recipient, Requestor } from '@/api/types'
import { PHASE_OPTIONS as CASE_PHASE_OPTIONS, LETTER_TYPE_OPTIONS as CASE_LETTER_TYPE_OPTIONS } from '@/lib/case-options'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'
import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
} from '@/components/ui/b-combobox'
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

// yyyy-MM-dd from a native <input type="date"> -> RFC3339, or undefined for
// an empty field (omitted from the PATCH body rather than sent as "").
function isoFromDateInput(value: string): string | undefined {
  return value ? new Date(value).toISOString() : undefined
}

/* ─── Add Document Dialog ────────────────────────────────────────────────── */

// Opens a brand-new case (+ its first letter) for a domain — get-or-creates
// the domain the same way AddUrlDialog does, so pointing this at an
// already-watchlisted domain is idempotent. To add a *second* letter to an
// already-open case, use that domain's Cases dialog on the Watchlist page
// instead (this dialog has no case picker, only "open a new one").
function AddDocumentDialog({
  open,
  onClose,
  onAdded,
  domainOptions,
  recipients,
  requestors,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  domainOptions: string[]
  recipients: Recipient[]
  requestors: Requestor[]
}) {
  const [domains, setDomains] = useState<string[]>([])
  const [domainQuery, setDomainQuery] = useState('')
  const [phase, setPhase] = useState('requested')
  const [type, setType] = useState('Notice')
  const [referenceNumber, setReferenceNumber] = useState('')
  const [recipient, setRecipient] = useState('')
  const [subject, setSubject] = useState('')
  const [requestor, setRequestor] = useState('')
  const [workflowStatus, setWorkflowStatus] = useState('')
  const [letterDate, setLetterDate] = useState('')
  const [receivedAt, setReceivedAt] = useState('')
  const [submittedAt, setSubmittedAt] = useState('')
  const [remarks, setRemarks] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => {
    setDomains([]); setDomainQuery(''); setPhase('requested'); setType('Notice'); setReferenceNumber('')
    setRecipient(''); setSubject(''); setRequestor(''); setWorkflowStatus('')
    setLetterDate(''); setReceivedAt(''); setSubmittedAt(''); setRemarks(''); setError(null)
  }

  // The typed-but-not-yet-selected query is offered back as a pickable item
  // itself (labeled "Add …") so this stays create-or-pick like the old
  // textarea — a domain doesn't have to already be on a watchlist.
  const trimmedQuery = domainQuery.trim()
  const domainItems = useMemo(() => {
    if (!trimmedQuery || domainOptions.includes(trimmedQuery) || domains.includes(trimmedQuery)) return domainOptions
    return [...domainOptions, trimmedQuery]
  }, [domainOptions, trimmedQuery, domains])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (domains.length === 0) { setError('At least one domain is required'); return }
    setLoading(true)
    setError(null)
    try {
      // Get-or-create every domain (idempotent for one already on a
      // watchlist), then link them all to one shared case — matches
      // AddUrlDialog's "N URLs in one Notice" batch shape rather than
      // opening a separate case per domain.
      const created = await Promise.all(domains.map(d => createUrl(d)))
      const c = await createCase(created[0].url, phase)
      await Promise.all(created.slice(1).map(u => addUrlToCase(c.id, u.url, phase)))
      await addCaseLetter(c.id, {
        type,
        reference_number: referenceNumber.trim() || undefined,
        recipient: recipient.trim() || undefined,
        subject: subject.trim() || undefined,
        requestor: requestor.trim() || undefined,
        workflow_status: workflowStatus || undefined,
        letter_date: isoFromDateInput(letterDate),
        received_at: isoFromDateInput(receivedAt),
        submitted_at: isoFromDateInput(submittedAt),
        remarks: remarks.trim() || undefined,
      })
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add document')
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 560 }}>
        <DialogHeader>
          <DialogTitle>Add Document</DialogTitle>
          <DialogDescription>
            Opens a new case linking one or more domains and records its first letter (Memo or Notice). To add a second letter to a case that's already open, use that domain's Cases dialog on the Watchlist page instead.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="add-doc-url">Domain(s)</label>
            <Combobox
              items={domainItems}
              value={domains}
              onValueChange={setDomains}
              onInputValueChange={setDomainQuery}
              multiple
            >
              <ComboboxChips>
                {domains.map(d => (
                  <ComboboxChip key={d} aria-label={d}>{d}</ComboboxChip>
                ))}
                <ComboboxChipsInput
                  id="add-doc-url"
                  placeholder={domains.length === 0 ? 'Search or type a domain…' : undefined}
                  autoFocus
                  disabled={loading}
                />
              </ComboboxChips>
              <ComboboxContent>
                <ComboboxEmpty>No domains found.</ComboboxEmpty>
                <ComboboxList>
                  {(item: string) => (
                    <ComboboxItem key={item} value={item}>
                      {domainOptions.includes(item) ? item : `Add "${item}"`}
                    </ComboboxItem>
                  )}
                </ComboboxList>
              </ComboboxContent>
            </Combobox>
            <p className="text-xs text-stone-muted">Search existing watchlist domains or type a new one. All selected domains will share this one case.</p>
          </div>

          <div className="form-row">
            <div className="form-field">
              <label className="form-label" id="add-doc-phase-label">Case Phase</label>
              <Select value={phase} onValueChange={setPhase} disabled={loading}>
                <SelectTrigger aria-labelledby="add-doc-phase-label" placeholder="—" className="w-full" />
                <SelectContent>
                  {CASE_PHASE_OPTIONS.map((opt, i) => (
                    <SelectItem key={opt.value} index={i} value={opt.value}>{opt.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="form-field">
              <label className="form-label" id="add-doc-type-label">Type</label>
              <Select value={type} onValueChange={setType} disabled={loading}>
                <SelectTrigger aria-labelledby="add-doc-type-label" placeholder="—" className="w-full" />
                <SelectContent>
                  {CASE_LETTER_TYPE_OPTIONS.map((opt, i) => (
                    <SelectItem key={opt} index={i} value={opt}>{opt}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="form-row">
            <div className="form-field">
              <label className="form-label" htmlFor="add-doc-reference">Reference No.</label>
              <input
                id="add-doc-reference"
                className="form-input"
                placeholder="e.g. MCMC(S)CMOD/BLK/2026(1-2)"
                value={referenceNumber}
                onChange={e => setReferenceNumber(e.target.value)}
                disabled={loading}
              />
            </div>
            <div className="form-field">
              <label className="form-label" id="add-doc-recipient-label">Recipient</label>
              <Select value={recipient} onValueChange={setRecipient} disabled={loading}>
                <SelectTrigger aria-labelledby="add-doc-recipient-label" placeholder="—" className="w-full" />
                <SelectContent>
                  <SelectItem index={0} value="">—</SelectItem>
                  {recipients.map((r, i) => (
                    <SelectItem key={r.id} index={i + 1} value={r.name}>{r.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="form-field">
            <label className="form-label" htmlFor="add-doc-subject">Subject</label>
            <input id="add-doc-subject" className="form-input" value={subject} onChange={e => setSubject(e.target.value)} disabled={loading} />
          </div>

          <div className="form-row">
            <div className="form-field">
              <label className="form-label" id="add-doc-requestor-label">Requestor</label>
              <Select value={requestor} onValueChange={setRequestor} disabled={loading}>
                <SelectTrigger aria-labelledby="add-doc-requestor-label" placeholder="—" className="w-full" />
                <SelectContent>
                  <SelectItem index={0} value="">—</SelectItem>
                  {requestors.map((r, i) => (
                    <SelectItem key={r.id} index={i + 1} value={r.name}>{r.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="form-field">
              <label className="form-label" id="add-doc-workflow-label">Workflow Status</label>
              <Select value={workflowStatus} onValueChange={setWorkflowStatus} disabled={loading}>
                <SelectTrigger aria-labelledby="add-doc-workflow-label" placeholder="—" className="w-full" />
                <SelectContent>
                  <SelectItem index={0} value="">—</SelectItem>
                  {WORKFLOW_STATUS_OPTIONS.map((opt, i) => (
                    <SelectItem key={opt} index={i + 1} value={opt}>{opt}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="form-row">
            <div className="form-field">
              <label className="form-label" htmlFor="add-doc-letter-date">Letter Date</label>
              <input id="add-doc-letter-date" type="date" className="form-input" value={letterDate} onChange={e => setLetterDate(e.target.value)} disabled={loading} />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="add-doc-received">Received</label>
              <input id="add-doc-received" type="date" className="form-input" value={receivedAt} onChange={e => setReceivedAt(e.target.value)} disabled={loading} />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="add-doc-submission">Submission</label>
              <input id="add-doc-submission" type="date" className="form-input" value={submittedAt} onChange={e => setSubmittedAt(e.target.value)} disabled={loading} />
            </div>
          </div>

          <div className="form-field">
            <label className="form-label" htmlFor="add-doc-remarks">Remarks</label>
            <textarea
              id="add-doc-remarks"
              className="form-input"
              rows={2}
              value={remarks}
              onChange={e => setRemarks(e.target.value)}
              disabled={loading}
              style={{ resize: 'vertical', fontFamily: 'inherit' }}
            />
          </div>

          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add Document'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function DocsPage() {
  const [letters, setLetters] = useState<CaseLetterEntry[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [domainOptions, setDomainOptions] = useState<string[]>([])
  const [recipients, setRecipients] = useState<Recipient[]>([])
  const [requestors, setRequestors] = useState<Requestor[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)

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

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [l, d, u, rc, rq] = await Promise.all([
        fetchAllCaseLetters(), fetchDepartmentsOpen(), fetchUrls(), fetchRecipients(), fetchRequestors(),
      ])
      setLetters(l)
      setDepartments(d)
      setDomainOptions(u.map(entry => entry.url))
      setRecipients(rc)
      setRequestors(rq)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load documents')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

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
      meta: { headerTitle: 'Letter Date', skeleton: <span className="skeleton" style={{ width: 90, height: 14 }} /> },
      cell: ({ row }) => formatDate(row.original.letter_date),
    },
    {
      id: 'type',
      accessorFn: l => l.type,
      header: 'Type',
      meta: { headerTitle: 'Type', skeleton: <span className="skeleton" style={{ width: 70, height: 20, borderRadius: 4 }} /> },
    },
    {
      id: 'reference_number',
      accessorFn: l => l.reference_number ?? '',
      header: 'Reference No.',
      meta: { headerTitle: 'Reference No.', skeleton: <span className="skeleton" style={{ width: 160, height: 14 }} /> },
      cell: ({ row }) => row.original.reference_number || '—',
    },
    {
      id: 'recipient',
      accessorFn: l => l.recipient ?? '',
      header: 'Recipient',
      meta: { headerTitle: 'Recipient', skeleton: <span className="skeleton" style={{ width: 110, height: 14 }} /> },
      cell: ({ row }) => row.original.recipient || '—',
    },
    {
      id: 'subject',
      accessorFn: l => l.subject ?? '',
      header: 'Subject',
      meta: { headerTitle: 'Subject', skeleton: <span className="skeleton" style={{ width: 140, height: 14 }} /> },
      cell: ({ row }) => row.original.subject || '—',
    },
    {
      id: 'requestor',
      accessorFn: l => l.requestor ?? '',
      header: 'Requestor',
      meta: { headerTitle: 'Requestor', skeleton: <span className="skeleton" style={{ width: 110, height: 14 }} /> },
      cell: ({ row }) => row.original.requestor || '—',
    },
    {
      id: 'workflow_status',
      accessorFn: l => l.workflow_status ?? '',
      header: 'Status',
      meta: { headerTitle: 'Status', skeleton: <span className="skeleton" style={{ width: 90, height: 20, borderRadius: 4 }} /> },
      cell: ({ row }) => row.original.workflow_status || '—',
    },
    {
      id: 'received_at',
      accessorFn: l => l.received_at ?? '',
      header: ({ column }) => <SortableHeader column={column} title="Received" />,
      meta: { headerTitle: 'Received', skeleton: <span className="skeleton" style={{ width: 90, height: 14 }} /> },
      cell: ({ row }) => formatDate(row.original.received_at),
    },
    {
      id: 'submitted_at',
      accessorFn: l => l.submitted_at ?? '',
      header: ({ column }) => <SortableHeader column={column} title="Submission" />,
      meta: { headerTitle: 'Submission', skeleton: <span className="skeleton" style={{ width: 90, height: 14 }} /> },
      cell: ({ row }) => formatDate(row.original.submitted_at),
    },
    {
      id: 'department_name',
      accessorFn: l => l.department_name,
      header: 'Dept.',
      meta: { headerTitle: 'Dept.', skeleton: <span className="skeleton" style={{ width: 60, height: 14 }} /> },
    },
    {
      id: 'urls',
      accessorFn: l => (l.urls ?? []).join(', '),
      header: 'Link',
      meta: { headerTitle: 'Link', skeleton: <span className="skeleton" style={{ width: 140, height: 14 }} /> },
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
      meta: { headerTitle: 'Remarks', skeleton: <span className="skeleton" style={{ width: 120, height: 14 }} /> },
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
        <div style={{ marginLeft: 'auto' }}>
          <Button onClick={() => setAddOpen(true)}>+ Add Document</Button>
        </div>
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
          <button className="btn-primary" onClick={() => setAddOpen(true)}>+ Add Document</button>
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

      <AddDocumentDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdded={load}
        domainOptions={domainOptions}
        recipients={recipients}
        requestors={requestors}
      />
    </div>
  )
}
