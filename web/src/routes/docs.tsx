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
import { fetchAgencies } from '@/api/agencies'
import { fetchDueDatePresets } from '@/api/due-date-presets'
import type { CaseLetterEntry, Department, Recipient, Requestor, Agency, DueDatePreset } from '@/api/types'
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
  ComboboxInput,
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

// Mirrors urls.tsx's own dueDateOptionsFrom/dueDateFromDurationMinutes (not
// imported from there — that file is owned by a parallel task) — same
// "Time to Block" duration-picker convention: the case owner picks how long
// from now the ISP has, not a calendar date.
function dueDateOptionsFrom(presets: DueDatePreset[]): { value: string; label: string }[] {
  return [{ value: '', label: '—' }, ...presets.map(p => ({ value: String(p.minutes), label: p.label }))]
}

function dueDateFromDurationMinutes(minutes: number): string {
  return new Date(Date.now() + minutes * 60 * 1000).toISOString()
}

// One selectable "existing case" option for AddDocumentDialog's case picker —
// derived from the already-loaded case-letters list (see DocsPage's
// `caseOptions`), not a separate fetch.
type CaseOption = { id: number; label: string }

/* ─── Add Document Dialog ────────────────────────────────────────────────── */

// Records one letter, either against a brand-new case (pick/type a domain —
// get-or-creates it the same way AddUrlDialog does, so an already-watchlisted
// domain is idempotent) or against a case that's already open (pick it from
// "Existing Case" instead) — mutually exclusive paths, see the two fields'
// own disabled states below.
function AddDocumentDialog({
  open,
  onClose,
  onAdded,
  domainOptions,
  recipients,
  requestors,
  agencies,
  duePresets,
  caseOptions,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  domainOptions: string[]
  recipients: Recipient[]
  requestors: Requestor[]
  agencies: Agency[]
  duePresets: DueDatePreset[]
  caseOptions: CaseOption[]
}) {
  const [domains, setDomains] = useState<string[]>([])
  const [domainQuery, setDomainQuery] = useState('')
  const [existingCaseId, setExistingCaseId] = useState<number | ''>('')
  const [phase, setPhase] = useState('requested')
  const [agencyId, setAgencyId] = useState<number | ''>('')
  const [dueDurationMinutes, setDueDurationMinutes] = useState('')
  const [type, setType] = useState('Notice')
  const [referenceNumberExternal, setReferenceNumberExternal] = useState('')
  const [referenceNumberInternal, setReferenceNumberInternal] = useState('')
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
    setDomains([]); setDomainQuery(''); setExistingCaseId('')
    setPhase('requested'); setAgencyId(''); setDueDurationMinutes('')
    setType('Notice'); setReferenceNumberExternal(''); setReferenceNumberInternal('')
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
    if (existingCaseId === '' && domains.length === 0) {
      setError('Pick a domain (to open a new case) or an existing case')
      return
    }
    setLoading(true)
    setError(null)
    try {
      const letterFields = {
        type,
        reference_number_external: referenceNumberExternal.trim() || undefined,
        reference_number_internal: referenceNumberInternal.trim() || undefined,
        recipient: recipient.trim() || undefined,
        subject: subject.trim() || undefined,
        requestor: requestor.trim() || undefined,
        workflow_status: workflowStatus || undefined,
        letter_date: isoFromDateInput(letterDate),
        received_at: isoFromDateInput(receivedAt),
        submitted_at: isoFromDateInput(submittedAt),
        remarks: remarks.trim() || undefined,
      }
      if (existingCaseId !== '') {
        // Linking to a case that's already open — no domain/case creation,
        // just record this letter against it.
        await addCaseLetter(existingCaseId, letterFields)
      } else {
        // Get-or-create every domain (idempotent for one already on a
        // watchlist, and auto-linked to the caller's own department
        // watchlist by AddToWatchlist), then link them all to one shared
        // case — matches AddUrlDialog's "N URLs in one Notice" batch shape
        // rather than opening a separate case per domain.
        const created = await Promise.all(domains.map(d => createUrl(d)))
        const caseOpts: { agencyId?: number; dueDate?: string } = {}
        if (agencyId !== '') caseOpts.agencyId = agencyId
        if (dueDurationMinutes) caseOpts.dueDate = dueDateFromDurationMinutes(Number(dueDurationMinutes))
        const c = await createCase(created[0].url, phase, caseOpts)
        await Promise.all(created.slice(1).map(u => addUrlToCase(c.id, u.url, phase)))
        await addCaseLetter(c.id, letterFields)
      }
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
            Record a letter (Memo or Notice) either by opening a new case for one or more domains, or by picking a case that's already open.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="add-doc-url">Domain(s) — new case</label>
            <Combobox
              items={domainItems}
              value={domains}
              onValueChange={setDomains}
              onInputValueChange={setDomainQuery}
              multiple
              disabled={loading || existingCaseId !== ''}
            >
              <ComboboxChips>
                {domains.map(d => (
                  <ComboboxChip key={d} aria-label={d}>{d}</ComboboxChip>
                ))}
                <ComboboxChipsInput
                  id="add-doc-url"
                  placeholder={domains.length === 0 ? 'Search or type a domain…' : undefined}
                  autoFocus
                  disabled={loading || existingCaseId !== ''}
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
            <p className="text-xs text-stone-muted">Pick an already-watchlisted domain, or type a new one — either way it opens a new case, and gets added to your department's watchlist automatically. Mutually exclusive with linking to an existing case below.</p>
          </div>

          <div className="form-field">
            <label className="form-label" htmlFor="add-doc-existing-case">Existing Case</label>
            <Combobox
              items={caseOptions}
              value={caseOptions.find(c => c.id === existingCaseId) ?? null}
              onValueChange={item => setExistingCaseId(item ? item.id : '')}
              disabled={loading || domains.length > 0}
            >
              <ComboboxInput
                id="add-doc-existing-case"
                placeholder="Search cases by domain…"
                disabled={loading || domains.length > 0}
              />
              <ComboboxContent>
                <ComboboxEmpty>No cases found.</ComboboxEmpty>
                <ComboboxList>
                  {(item: CaseOption) => <ComboboxItem key={item.id} value={item}>{item.label}</ComboboxItem>}
                </ComboboxList>
              </ComboboxContent>
            </Combobox>
            <p className="text-xs text-stone-muted">Link this letter to a case that's already open, instead of opening a new one. Mutually exclusive with the domain picker above.</p>
          </div>

          {existingCaseId === '' && (
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
                <label className="form-label" id="add-doc-agency-label">Agency</label>
                <Select value={String(agencyId)} onValueChange={v => setAgencyId(v === '' ? '' : Number(v))} disabled={loading}>
                  <SelectTrigger aria-labelledby="add-doc-agency-label" placeholder="—" className="w-full" />
                  <SelectContent>
                    <SelectItem index={0} value="">—</SelectItem>
                    {agencies.map((a, i) => (
                      <SelectItem key={a.id} index={i + 1} value={String(a.id)}>{a.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="form-field">
                <label className="form-label" id="add-doc-due-date-label">Time to Block</label>
                <Select value={dueDurationMinutes} onValueChange={setDueDurationMinutes} disabled={loading}>
                  <SelectTrigger aria-labelledby="add-doc-due-date-label" placeholder="—" className="w-full" />
                  <SelectContent>
                    {dueDateOptionsFrom(duePresets).map((opt, i) => (
                      <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
          )}

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

          <div className="form-row">
            <div className="form-field">
              <label className="form-label" htmlFor="add-doc-reference-external">External Ref. (No. Rujukan NMD)</label>
              <input
                id="add-doc-reference-external"
                className="form-input"
                placeholder="e.g. MCMC(S)CMOD/BLK/2026(1-2)"
                value={referenceNumberExternal}
                onChange={e => setReferenceNumberExternal(e.target.value)}
                disabled={loading}
              />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="add-doc-reference-internal">Internal Ref. (No. Rujukan NMSMD)</label>
              <input
                id="add-doc-reference-internal"
                className="form-input"
                value={referenceNumberInternal}
                onChange={e => setReferenceNumberInternal(e.target.value)}
                disabled={loading}
              />
            </div>
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
  const [agencies, setAgencies] = useState<Agency[]>([])
  const [duePresets, setDuePresets] = useState<DueDatePreset[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)

  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<Filter<string>[]>([])
  const [sorting, setSorting] = useState<SortingState>([])
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })
  // Internal ref. starts hidden — External is the operationally common one
  // (matches urls.tsx's single-reference-number column); Internal is still
  // reachable via the Columns toggle below.
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({ reference_number_internal: false })

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
      const [l, d, u, rc, rq, ag, dp] = await Promise.all([
        fetchAllCaseLetters(), fetchDepartmentsOpen(), fetchUrls(), fetchRecipients(), fetchRequestors(),
        fetchAgencies(), fetchDueDatePresets(),
      ])
      setLetters(l)
      setDepartments(d)
      setDomainOptions(u.map(entry => entry.url))
      setRecipients(rc)
      setRequestors(rq)
      setAgencies(ag)
      setDuePresets(dp)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load documents')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  // One option per distinct case_id already seen among loaded letters —
  // backs AddDocumentDialog's "Existing Case" picker without a separate
  // fetch. A case with no letters yet never appears here, but it also can't
  // exist without one (createCase always opens with a first letter).
  const caseOptions = useMemo<CaseOption[]>(() => {
    const byId = new Map<number, string[]>()
    for (const l of letters) {
      if (!byId.has(l.case_id)) byId.set(l.case_id, l.urls ?? [])
    }
    return Array.from(byId.entries()).map(([id, urls]) => ({
      id,
      label: `Case #${id} — ${urls.length > 0 ? urls.join(', ') : 'no domains'}`,
    }))
  }, [letters])

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
      (l.reference_number_external ?? '').toLowerCase().includes(query) ||
      (l.reference_number_internal ?? '').toLowerCase().includes(query) ||
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
      id: 'reference_number_external',
      accessorFn: l => l.reference_number_external ?? '',
      header: 'External Ref.',
      meta: { headerTitle: 'External Ref. (No. Rujukan NMD)', skeleton: <span className="skeleton" style={{ width: 160, height: 14 }} /> },
      cell: ({ row }) => row.original.reference_number_external || '—',
    },
    {
      id: 'reference_number_internal',
      accessorFn: l => l.reference_number_internal ?? '',
      header: 'Internal Ref.',
      meta: { headerTitle: 'Internal Ref. (No. Rujukan NMSMD)', skeleton: <span className="skeleton" style={{ width: 160, height: 14 }} /> },
      cell: ({ row }) => row.original.reference_number_internal || '—',
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
        agencies={agencies}
        duePresets={duePresets}
        caseOptions={caseOptions}
      />
    </div>
  )
}
