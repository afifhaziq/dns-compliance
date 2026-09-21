import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import {
  type ColumnDef,
  type SortingState,
  type PaginationState,
  type VisibilityState,
  type ExpandedState,
  getCoreRowModel,
  getSortedRowModel,
  getPaginationRowModel,
  getExpandedRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { GripIcon } from '@/components/ui/grip'
import { FileText } from 'lucide-react'
import { CASE_STATUS_OPTIONS } from '@/lib/case-options'
import { ChevronRight } from '@/components/ui/chevron-right'
import { SquarePenIcon } from '@/components/ui/square-pen'
import { DownloadIcon } from '@/components/animate-ui/icons/download'
import { DataGridTableRowExpand } from '@/components/reui/data-grid/data-grid-table'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import { fetchUrls, createUrl, deleteUrl, setUrlEnabled } from '../api/urls'
import { normalizeForClient } from './__root'
import { createCase, addCaseLetter, addUrlToCase, updateCase, updateCaseURLStatus, updateCaseURLAgency, removeUrlFromCase, updateCaseLetter, fetchCaseSummariesPage, exportCaseSummaries } from '../api/cases'
import { downloadBlob } from '@/lib/download'
import { fetchAgencies } from '../api/agencies'
import { fetchDepartmentsOpen } from '../api/departments'
import { fetchDueDatePresets } from '../api/due-date-presets'
import { fetchRecipients } from '../api/recipients'
import { fetchRequestors } from '../api/requestors'
import { useGridPreference } from '@/hooks/use-grid-preference'
import type { URLEntry, Agency, Department, DueDatePreset, Instrument, OffenceEntry, Citation, LegalCategory, LegalElement, LegalSubElement, URLOffence, Recipient, Requestor, CaseSummary, CaseSummaryDomain } from '../api/types'
import { fetchInstruments, fetchCitations, fetchCategories, fetchElements, fetchSubElements, attachOffence, fetchOffencesByUrl, detachOffence, formatParsedCitation } from '../api/legal'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/r-switch'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'
import { Input } from '@/components/ui/input'
import { DataGrid, DataGridContainer } from '@/components/reui/data-grid/data-grid'
import { DataGridTable } from '@/components/reui/data-grid/data-grid-table'
import { DataGridColumnVisibility } from '@/components/reui/data-grid/data-grid-column-visibility'
import { DataGridPagination } from '@/components/reui/data-grid/data-grid-pagination'
import { Filters, type Filter, type FilterFieldConfig, type CustomRendererProps } from '@/components/reui/filters'
import { DatePicker } from '@/components/ui/date-picker'
import { format, parseISO } from 'date-fns'
import { SortableHeader, EmptyIcon } from '@/components/results-table-parts'
import { XIcon } from '@/components/ui/x'
import { faviconApiUrl } from '../api/domain'
import {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardImage,
} from '@/components/animate-ui/components/base/preview-link-card'

export const Route = createFileRoute('/urls')({
  validateSearch: (search: Record<string, unknown>): { view: 'domains' | 'cases' } => ({
    view: search.view === 'domains' ? 'domains' : 'cases',
  }),
  component: URLsPage,
})

/* ─── Case metadata (shared by the Add Domain form + inline table cells) ─── */

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '—' },
  { value: 'requested', label: 'Requested' },
  { value: 'blocked', label: 'Blocked' },
  { value: 'uplift', label: 'Uplift' },
  { value: 'suspended', label: 'Suspended' },
  { value: 'not_blocked', label: 'Not Blocked' },
  { value: 'internal', label: 'Internal' },
]

// due_date is the ISP's block deadline (some takedown orders require
// blocking within 6h/24h), but rather than picking a calendar date+time by
// hand, the case owner picks how long from now the ISP has — the computed
// deadline (now + duration) is what actually gets stored in due_date, same
// field ISPTiming already measures against. The duration list itself is
// admin/dept-admin-configurable (DueDatePreset, managed from the Admin page's
// "Time to Block" tab) rather than hardcoded, seeded on first boot with the
// original 6h/24h/48h/72h/7-day options.
//
// Keyed by minutes (not preset id) since that's what dueDateFromDurationMinutes
// actually needs — two presets sharing the same minutes with different labels
// would be indistinguishable once selected, an accepted edge case for a
// simple duration list.
function dueDateOptionsFrom(presets: DueDatePreset[]): { value: string; label: string }[] {
  return [{ value: '', label: '—' }, ...presets.map(p => ({ value: String(p.minutes), label: p.label }))]
}

function dueDateFromDurationMinutes(minutes: number): string {
  return new Date(Date.now() + minutes * 60 * 1000).toISOString()
}

// Mirrors docs.tsx's own WORKFLOW_STATUS_OPTIONS/isoFromDateInput (not
// imported — parallel-owned files, see dueDateOptionsFrom above).
const WORKFLOW_STATUS_OPTIONS = ['Draft', 'Pending Legal', 'Pending TSC', 'Submitted']

function isoFromDateInput(value: string): string | undefined {
  return value ? new Date(value).toISOString() : undefined
}

// A raw domain-field line is worth keeping as CaseURL.OriginalURL only when
// it's more than just what the server already normalized it down to (e.g.
// "https://example.com/path?x=1" vs the stored bare hostname
// "example.com") — a plain bare-hostname line would just duplicate `url`.
function originalUrlFor(raw: string | undefined, normalizedUrl: string): string | undefined {
  return raw && raw !== normalizedUrl ? raw : undefined
}

/* ─── Add Domain Dialog ──────────────────────────────────────────────────── */

export type StagedOffence = {
  instrumentId: number
  citationId: number
  categoryId: number
  elementId?: number
  subElementId?: number
  label: string
}

// Imperative escape hatch for a picker that has Instrument/Citation/Category
// filled in but hasn't had "+ Add offence" clicked yet — without this, that
// selection lives only in the picker's own local state, invisible to the
// parent, so closing/submitting silently drops it (the exact bug reported:
// fill in the picker, close the dialog, reopen — nothing saved). Callers
// flush() right before they close/submit and fold the result into what they
// were about to save, rather than relying on the user to remember the extra
// click.
export type MultiOffencePickerHandle = {
  flush: () => StagedOffence | null
}

// Cascading Instrument -> Citation -> Category -> Element picker that stages
// one offence at a time and appends it to a removable-chip list on "Add
// offence" — a domain can violate multiple sections/offences at once (real
// MCMC data cites domains under several sections joined by "dan"/"&"), so
// this replaces the old single-selection OffencePicker. Reused by both
// AddUrlDialog (staged offences applied to every domain on submit) and
// EditUrlDialog (each addition attaches immediately to one existing
// domain) — this component has no knowledge of which caller it's in.
const MultiOffencePicker = forwardRef<MultiOffencePickerHandle, {
  value: StagedOffence[]
  onChange: (offences: StagedOffence[]) => void
  disabled: boolean
}>(function MultiOffencePicker({ value, onChange, disabled }, ref) {
  const [instruments, setInstruments] = useState<Instrument[]>([])
  const [citations, setCitations] = useState<Citation[]>([])
  const [categories, setCategories] = useState<LegalCategory[]>([])
  const [elements, setElements] = useState<LegalElement[]>([])
  const [subElements, setSubElements] = useState<LegalSubElement[]>([])

  const [instrumentId, setInstrumentId] = useState<number | ''>('')
  const [citationId, setCitationId] = useState<number | ''>('')
  const [categoryId, setCategoryId] = useState<number | ''>('')
  const [elementId, setElementId] = useState<number | ''>('')
  const [subElementId, setSubElementId] = useState<number | ''>('')

  useEffect(() => { fetchInstruments().then(setInstruments) }, [])
  useEffect(() => {
    if (instrumentId === '') { setCitations([]); return }
    fetchCitations(instrumentId).then(setCitations)
  }, [instrumentId])
  useEffect(() => {
    if (citationId === '') { setCategories([]); return }
    fetchCategories(citationId).then(setCategories)
  }, [citationId])
  useEffect(() => {
    if (categoryId === '') { setElements([]); return }
    fetchElements(categoryId).then(setElements)
  }, [categoryId])
  useEffect(() => {
    if (elementId === '') { setSubElements([]); return }
    fetchSubElements(elementId).then(setSubElements)
  }, [elementId])

  const resetStaging = () => {
    setInstrumentId(''); setCitationId(''); setCategoryId(''); setElementId(''); setSubElementId('')
  }

  const computePending = (): StagedOffence | null => {
    if (instrumentId === '' || citationId === '' || categoryId === '') return null
    const citation = citations.find(c => c.id === citationId)
    const category = categories.find(c => c.id === categoryId)
    const element = elementId === '' ? undefined : elements.find(e => e.id === elementId)
    const subElement = subElementId === '' ? undefined : subElements.find(se => se.id === subElementId)
    if (!citation || !category) return null
    const label = `${formatParsedCitation(citation.parsed)} — ${category.name}${element ? ` (${element.name})` : ''}${subElement ? ` › ${subElement.name}` : ''}`
    return {
      instrumentId, citationId, categoryId,
      elementId: elementId === '' ? undefined : elementId,
      subElementId: subElementId === '' ? undefined : subElementId,
      label,
    }
  }

  const handleAdd = () => {
    const pending = computePending()
    if (!pending) return
    onChange([...value, pending])
    resetStaging()
  }

  const handleRemove = (index: number) => {
    onChange(value.filter((_, i) => i !== index))
  }

  useImperativeHandle(ref, () => ({ flush: computePending }))

  return (
    <div className="form-field">
      <label className="form-label" id="offence-picker-label">
        Offences <span style={{ color: 'var(--stone-muted)', fontWeight: 400 }}>(optional — attaches to every domain added above)</span>
      </label>
      {value.length > 0 && (
        <ul className="offence-chip-list">
          {value.map((o, i) => (
            <li key={i} className="offence-chip">
              <span>{o.label}</span>
              <button
                type="button"
                className="screenshot-icon-btn"
                onClick={() => handleRemove(i)}
                disabled={disabled}
                aria-label={`Remove offence ${o.label}`}
              >
                <XIcon size={14} />
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="flex flex-col" style={{ gap: 8 }}>
        <Select
          value={String(instrumentId)}
          onValueChange={v => { setInstrumentId(v === '' ? '' : Number(v)); setCitationId(''); setCategoryId(''); setElementId(''); setSubElementId('') }}
          disabled={disabled}
        >
          <SelectTrigger aria-labelledby="offence-picker-label" placeholder="Instrument…" className="w-full" />
          <SelectContent>
            <SelectItem index={0} value="">No instrument</SelectItem>
            {instruments.map((inst, i) => (
              <SelectItem key={inst.id} index={i + 1} value={String(inst.id)}>{inst.short_title}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        {instrumentId !== '' && (
          <Select
            value={String(citationId)}
            onValueChange={v => { setCitationId(v === '' ? '' : Number(v)); setCategoryId(''); setElementId(''); setSubElementId('') }}
            disabled={disabled}
          >
            <SelectTrigger aria-label="Citation" placeholder="Citation…" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">No citation</SelectItem>
              {citations.map((c, i) => (
                <SelectItem key={c.id} index={i + 1} value={String(c.id)}>
                  {/* A clean parse reconstructs to the same string as raw_text —
                      showing both would just repeat it. Only surface the parsed
                      form for NEEDS_REVIEW, where it shows how far parsing got.
                      Must stay a single string child, not a JSX fragment — Select's
                      label registration (select.tsx) only stores a label when
                      typeof children === 'string'. */}
                  {c.parse_confidence === 'NEEDS_REVIEW' ? `${c.raw_text} (parsed: ${formatParsedCitation(c.parsed)})` : c.raw_text}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {citationId !== '' && (
          <Select
            value={String(categoryId)}
            onValueChange={v => { setCategoryId(v === '' ? '' : Number(v)); setElementId(''); setSubElementId('') }}
            disabled={disabled}
          >
            <SelectTrigger aria-label="Category" placeholder="Category…" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">No category</SelectItem>
              {categories.map((cat, i) => (
                <SelectItem key={cat.id} index={i + 1} value={String(cat.id)}>{cat.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {categoryId !== '' && elements.length > 0 && (
          <Select
            value={String(elementId)}
            onValueChange={v => { setElementId(v === '' ? '' : Number(v)); setSubElementId('') }}
            disabled={disabled}
          >
            <SelectTrigger aria-label="Element" placeholder="Element (optional)…" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">No element</SelectItem>
              {elements.map((el, i) => (
                <SelectItem key={el.id} index={i + 1} value={String(el.id)}>{el.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {elementId !== '' && subElements.length > 0 && (
          <Select
            value={String(subElementId)}
            onValueChange={v => setSubElementId(v === '' ? '' : Number(v))}
            disabled={disabled}
          >
            <SelectTrigger aria-label="Sub-Element" placeholder="Sub-Element (optional)…" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">No sub-element</SelectItem>
              {subElements.map((se, i) => (
                <SelectItem key={se.id} index={i + 1} value={String(se.id)}>{se.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {categoryId !== '' && (
          <button type="button" className="btn-ghost" onClick={handleAdd} disabled={disabled}>
            + Add offence
          </button>
        )}
      </div>
    </div>
  )
})

// Case creation is the mandatory entry point for adding a domain — there is
// no standalone "add to watchlist" path on this page any more. Every domain
// entered here is attached to one shared case (case_urls is many-to-many —
// matches CMOD's real "N URLs in one Notice" pattern): the case is opened on
// the first URL, the rest are attached via addUrlToCase. Status is per-url
// (CaseURL.Status) — this dialog's Status field sets the initial status for
// every url created in this submission (create mode: all of them; edit
// mode: only newly-added domains, since an existing domain's own status is
// edited inline in the grid, not here). Due Date remains a case-level
// default; Agency (like Status) is per-domain — this dialog's Agency field
// seeds every url created in this submission the same way Status does.
const DOMAIN_ROW_STYLE: React.CSSProperties = {
  display: 'flex', gap: '0.75rem', alignItems: 'flex-start',
  border: '1px solid var(--border)', borderRadius: 8, padding: '0.5rem 0.75rem', marginBottom: '0.375rem',
}

// Per-domain Agency + Status selects, laid out as two fixed columns so the
// triggers can't overlap. Used by both existing-domain and new-domain rows.
function DomainSettings({ agencies, agencyId, status, onChange, disabled, idPrefix }: {
  agencies: Agency[]
  agencyId: number | ''
  status: string
  onChange: (patch: { agencyId?: number | ''; status?: string }) => void
  disabled: boolean
  idPrefix: string
}) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '170px 170px', gap: '0.5rem', flexShrink: 0 }}>
      <div style={{ minWidth: 0 }}>
        <label className="form-label" id={`${idPrefix}-agency`} style={{ marginBottom: 2 }}>Agency</label>
        <Select value={String(agencyId)} onValueChange={v => onChange({ agencyId: v === '' ? '' : Number(v) })} disabled={disabled}>
          <SelectTrigger aria-labelledby={`${idPrefix}-agency`} placeholder="—" className="w-full" />
          <SelectContent>
            <SelectItem index={0} value="">—</SelectItem>
            {agencies.map((a, i) => (
              <SelectItem key={a.id} index={i + 1} value={String(a.id)}>{a.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div style={{ minWidth: 0 }}>
        <label className="form-label" id={`${idPrefix}-status`} style={{ marginBottom: 2 }}>Status</label>
        <Select value={status} onValueChange={v => onChange({ status: v })} disabled={disabled}>
          <SelectTrigger aria-labelledby={`${idPrefix}-status`} placeholder="—" className="w-full" />
          <SelectContent>
            {CASE_STATUS_OPTIONS.map((opt, i) => (
              <SelectItem key={opt.value} index={i} value={opt.value}>{opt.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  )
}

function AddUrlDialog({
  open,
  onClose,
  onAdded,
  agencies,
  duePresets,
  recipients,
  requestors,
  editing,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  agencies: Agency[]
  duePresets: DueDatePreset[]
  recipients: Recipient[]
  requestors: Requestor[]
  editing: CaseSummary | null
}) {
  const [value, setValue] = useState('')
  const [offences, setOffences] = useState<StagedOffence[]>([])
  const [agencyId, setAgencyId] = useState<number | ''>('')
  // Edit mode: domains staged for removal (applied on Save) and new domain rows, each with its own agency/status.
  const [removedIds, setRemovedIds] = useState<Set<number>>(new Set())
  const [newRows, setNewRows] = useState<{ url: string; agencyId: number | ''; status: string }[]>([])
  // Per-domain agency/status edits when editing a case, keyed by url_id — seeded from `editing`, diffed on save.
  const [domainEdits, setDomainEdits] = useState<Record<number, { agencyId: number | ''; status: string }>>({})
  const [dueDurationMinutes, setDueDurationMinutes] = useState('1440')
  const [status, setStatus] = useState('requested')
  const [createLetter, setCreateLetter] = useState(false)
  // CRD doesn't need the full letter form to track a case — External/
  // Internal ref alone (always visible, outside the Create Letter switch)
  // are enough, and always get recorded as a Notice-type CaseLetter
  // (current_reference_number, ListDepartmentURLs, derives from exactly
  // that). Internal ref is case-level here (unlike docs.tsx's CMOD form,
  // where Notice/Memo genuinely carry different internal refs), so the
  // Notice section below just mirrors it read-only; Memo gets its own.
  const [referenceNumberExternal, setReferenceNumberExternal] = useState('')
  const [referenceNumberInternal, setReferenceNumberInternal] = useState('')
  const [recipient, setRecipient] = useState('')
  const [noticeSubject, setNoticeSubject] = useState('')
  const [memoSubject, setMemoSubject] = useState('')
  const [memoReferenceNumberInternal, setMemoReferenceNumberInternal] = useState('')
  const [requestor, setRequestor] = useState('')
  const [workflowStatus, setWorkflowStatus] = useState('')
  const [letterDate, setLetterDate] = useState('')
  const [receivedAt, setReceivedAt] = useState('')
  const [submittedAt, setSubmittedAt] = useState('')
  const [remarks, setRemarks] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const pickerRef = useRef<MultiOffencePickerHandle>(null)

  const reset = () => {
    setValue(''); setOffences([]); setError(null)
    setAgencyId(''); setDueDurationMinutes('1440')
    setStatus('requested'); setCreateLetter(false)
    setReferenceNumberExternal(''); setReferenceNumberInternal('')
    setRecipient(''); setNoticeSubject(''); setMemoSubject(''); setMemoReferenceNumberInternal('')
    setRequestor(''); setWorkflowStatus('')
    setLetterDate(''); setReceivedAt(''); setSubmittedAt(''); setRemarks('')
  }

  useEffect(() => {
    if (!open) return
    if (editing) {
      setValue(editing.domains.map(d => d.url).join('\n'))
      setOffences([])
      setAgencyId('') // agency is per-domain now; this only seeds newly-added domains, not the case's existing ones
      setDueDurationMinutes('') // existing due date shown read-only; picking a duration replaces it (see the read-only line in the form below)
      setStatus('requested') // status is per-domain now; this only seeds newly-added domains, not the case's existing ones
      setRemovedIds(new Set())
      setNewRows([])
      setDomainEdits(Object.fromEntries(editing.domains.map(d => [d.url_id, { agencyId: d.agency_id ?? '', status: d.status }])))
      setCreateLetter(true)
      setReferenceNumberExternal(editing.notice_reference_number_external ?? '')
      setReferenceNumberInternal(editing.notice_reference_number_internal ?? '')
      setRecipient(editing.notice_recipient ?? '')
      setNoticeSubject(editing.notice_subject ?? '')
      setMemoSubject(editing.memo_subject ?? '')
      setMemoReferenceNumberInternal(editing.memo_reference_number_internal ?? '')
      setRequestor(editing.notice_requestor ?? '')
      setWorkflowStatus(editing.notice_workflow_status ?? '')
      setLetterDate(editing.notice_letter_date ? editing.notice_letter_date.slice(0, 10) : '')
      setReceivedAt(editing.notice_received_at ? editing.notice_received_at.slice(0, 10) : '')
      setSubmittedAt(editing.notice_submitted_at ? editing.notice_submitted_at.slice(0, 10) : '')
      setRemarks(editing.notice_remarks ?? '')
      setError(null)
    } else {
      reset()
    }
  }, [open, editing])

  const copySubjectFromMemo = () => setNoticeSubject(memoSubject)
  const copySubjectFromNotice = () => setMemoSubject(noticeSubject)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const domains = editing
      ? editing.domains.filter(d => !removedIds.has(d.url_id)).map(d => d.url).concat(newRows.map(r => r.url.trim()).filter(Boolean))
      : value.split('\n').map(s => s.trim()).filter(Boolean)
    if (domains.length === 0) { setError('At least one domain is required'); return }
    // Catch a filled-in-but-not-yet-"+ Add offence"-clicked selection sitting
    // in the picker — otherwise it's silently dropped rather than attached.
    const pending = pickerRef.current?.flush()
    const allOffences = pending ? [...offences, pending] : offences

    const caseOpts: { agencyId?: number; dueDate?: string; originalUrl?: string } = {}
    if (agencyId !== '') caseOpts.agencyId = agencyId
    if (dueDurationMinutes) caseOpts.dueDate = dueDateFromDurationMinutes(Number(dueDurationMinutes))

    setLoading(true)
    setError(null)
    try {
      if (editing) {
        await updateCase(editing.id, {
          ...(caseOpts.dueDate ? { dueDate: caseOpts.dueDate } : {}),
        })

        // editing.domains is already server-normalized; typed rows aren't, so
        // normalize before diffing or an already-attached domain typed in a
        // different raw form (e.g. "https://example.com/") looks new. The
        // as-typed text survives as CaseURL.OriginalURL. New domains must be
        // get-or-created first — addUrlToCase 404s on a url with no db.URL row.
        const existingURLs = new Set(editing.domains.map(d => d.url))
        const rowsToAdd = newRows
          .map(r => ({ ...r, raw: r.url.trim() }))
          .filter(r => r.raw)
          .map(r => ({ ...r, norm: normalizeForClient(r.raw) }))
          .filter(r => !existingURLs.has(r.norm))
        const createdDomains = await Promise.all(rowsToAdd.map(r => createUrl(r.norm)))
        await Promise.all(createdDomains.map((u, i) =>
          addUrlToCase(editing.id, u.url, rowsToAdd[i].status, originalUrlFor(rowsToAdd[i].raw, u.url), rowsToAdd[i].agencyId === '' ? undefined : rowsToAdd[i].agencyId)
        ))

        // Unconditional (not `|| undefined`) so a field the user blanked out
        // actually reaches updateCaseLetter's PATCH body instead of being
        // silently omitted — updateCaseLetter only sends keys that are
        // `!== undefined`. Date fields use `?? null` for the same reason:
        // isoFromDateInput('') is undefined, which would otherwise vanish.
        const richFields = {
          recipient: recipient.trim(),
          requestor: requestor.trim(),
          workflowStatus: workflowStatus,
          letterDate: isoFromDateInput(letterDate) ?? null,
          receivedAt: isoFromDateInput(receivedAt) ?? null,
          submittedAt: isoFromDateInput(submittedAt) ?? null,
          remarks: remarks.trim(),
        }
        const letterWork: Promise<unknown>[] = []
        if (editing.notice_letter_id) {
          letterWork.push(updateCaseLetter(editing.id, editing.notice_letter_id, {
            ...richFields,
            subject: noticeSubject.trim(),
            referenceNumberExternal: referenceNumberExternal.trim(),
            referenceNumberInternal: referenceNumberInternal.trim(),
          }))
        } else {
          letterWork.push(addCaseLetter(editing.id, {
            type: 'Notice',
            recipient: richFields.recipient, requestor: richFields.requestor,
            workflow_status: richFields.workflowStatus, letter_date: richFields.letterDate ?? undefined,
            received_at: richFields.receivedAt ?? undefined, submitted_at: richFields.submittedAt ?? undefined, remarks: richFields.remarks,
            subject: noticeSubject.trim() || undefined,
            reference_number_external: referenceNumberExternal.trim() || undefined,
            reference_number_internal: referenceNumberInternal.trim() || undefined,
          }))
        }
        if (editing.memo_letter_id) {
          letterWork.push(updateCaseLetter(editing.id, editing.memo_letter_id, {
            ...richFields,
            subject: memoSubject.trim(),
            referenceNumberInternal: memoReferenceNumberInternal.trim(),
          }))
        } else if (memoSubject.trim() || memoReferenceNumberInternal.trim()) {
          letterWork.push(addCaseLetter(editing.id, {
            type: 'Memo',
            recipient: richFields.recipient, requestor: richFields.requestor,
            workflow_status: richFields.workflowStatus, letter_date: richFields.letterDate ?? undefined,
            received_at: richFields.receivedAt ?? undefined, submitted_at: richFields.submittedAt ?? undefined, remarks: richFields.remarks,
            subject: memoSubject.trim() || undefined,
            reference_number_external: referenceNumberExternal.trim() || undefined,
            reference_number_internal: memoReferenceNumberInternal.trim() || undefined,
          }))
        }
        // Per-domain agency/status edits — only the ones that actually changed.
        const domainWork = editing.domains.flatMap(d => {
          if (removedIds.has(d.url_id)) return [removeUrlFromCase(editing.id, d.url_id)]
          const e = domainEdits[d.url_id]
          if (!e) return []
          const work: Promise<unknown>[] = []
          if (e.agencyId !== (d.agency_id ?? '')) work.push(updateCaseURLAgency(editing.id, d.url_id, e.agencyId === '' ? undefined : e.agencyId))
          if (e.status !== d.status) work.push(updateCaseURLStatus(editing.id, d.url_id, e.status))
          return work
        })
        await Promise.all([
          ...letterWork,
          ...domainWork,
          ...createdDomains.flatMap(u => allOffences.map(o => attachOffence(u.url, o.categoryId, o.elementId, o.subElementId))),
        ])
        reset()
        onAdded()
        onClose()
        return
      }

      // domains and created stay index-aligned (Promise.all preserves order),
      // so each raw as-typed line pairs with the URL it normalized to.
      const created = await Promise.all(domains.map(d => createUrl(d)))
      const caseWork = (async () => {
        const c = await createCase(created[0].url, status, { ...caseOpts, originalUrl: originalUrlFor(domains[0], created[0].url) })
        await Promise.all(created.slice(1).map((u, i) =>
          addUrlToCase(c.id, u.url, status, originalUrlFor(domains[i + 1], u.url), caseOpts.agencyId)
        ))
        // External/Internal ref are recorded regardless of the "Create
        // Letter" switch — CRD needs current_reference_number tracked even
        // when nobody fills in the fuller letter detail below.
        const richFields = createLetter ? {
          recipient: recipient.trim() || undefined,
          requestor: requestor.trim() || undefined,
          workflow_status: workflowStatus || undefined,
          letter_date: isoFromDateInput(letterDate),
          received_at: isoFromDateInput(receivedAt),
          submitted_at: isoFromDateInput(submittedAt),
          remarks: remarks.trim() || undefined,
        } : {}
        const letters = [{
          ...richFields,
          type: 'Notice',
          subject: createLetter ? (noticeSubject.trim() || undefined) : undefined,
          reference_number_external: referenceNumberExternal.trim() || undefined,
          reference_number_internal: referenceNumberInternal.trim() || undefined,
        }]
        // Memo is fully optional even with the switch on — only add it if
        // the user actually put something in its section.
        if (createLetter && (memoSubject.trim() || memoReferenceNumberInternal.trim())) {
          letters.push({
            ...richFields,
            type: 'Memo',
            subject: memoSubject.trim() || undefined,
            reference_number_external: referenceNumberExternal.trim() || undefined,
            reference_number_internal: memoReferenceNumberInternal.trim() || undefined,
          })
        }
        await Promise.all(letters.map(l => addCaseLetter(c.id, l)))
      })()
      await Promise.all([
        ...created.flatMap(u => allOffences.map(o => attachOffence(u.url, o.categoryId, o.elementId, o.subElementId))),
        caseWork,
      ])
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${editing ? 'save' : 'create'} case`)
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }
  const domainCount = value.split('\n').map(s => s.trim()).filter(Boolean).length
  const newDomainCount = newRows.filter(r => r.url.trim()).length
  const visibleExisting = editing ? editing.domains.filter(d => !removedIds.has(d.url_id)) : []
  const canRemoveDomain = visibleExisting.length + newDomainCount > 1

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: editing ? 960 : 560 }}>
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit Case' : 'Create Case'}</DialogTitle>
          <DialogDescription>
            {editing
              ? 'Case details apply to the whole case; domain details (agency, status, offences) belong to each domain. Linking a new domain adds it to this case; removing a domain line does not unlink it.'
              : <>Enter one or more domains or full URLs to monitor for DNS compliance — full URLs will have their domain automatically extracted, and multiple entries (one per line) share the case opened below. A case is required to add {domainCount > 1 ? 'these domains' : 'a domain'}; if a later batch covers a different offence, open a new case for it instead of reusing this one.</>}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          {!editing && (
            <>
              <div className="form-field">
                <label className="form-label" htmlFor="add-url-input">Domain</label>
                <textarea
                  id="add-url-input"
                  className="form-input form-input-strong"
                  placeholder={'https://example.com\nhttps://example2.com'}
                  value={value}
                  onChange={e => setValue(e.target.value)}
                  autoFocus
                  disabled={loading}
                  rows={4}
                  style={{ resize: 'vertical', fontFamily: 'inherit' }}
                />
              </div>

              <MultiOffencePicker
                ref={pickerRef}
                value={offences}
                onChange={setOffences}
                disabled={loading}
              />

              <div className="form-row">
                <div className="form-field">
                  <label className="form-label" id="add-agency-label">Agency</label>
                  <Select value={String(agencyId)} onValueChange={v => setAgencyId(v === '' ? '' : Number(v))} disabled={loading}>
                    <SelectTrigger aria-labelledby="add-agency-label" placeholder="—" className="w-full" />
                    <SelectContent>
                      <SelectItem index={0} value="">—</SelectItem>
                      {agencies.map((a, i) => (
                        <SelectItem key={a.id} index={i + 1} value={String(a.id)}>{a.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="form-field">
                  <label className="form-label" id="add-case-status-label">Status</label>
                  <Select value={status} onValueChange={setStatus} disabled={loading}>
                    <SelectTrigger aria-labelledby="add-case-status-label" placeholder="—" className="w-full" />
                    <SelectContent>
                      {CASE_STATUS_OPTIONS.map((opt, i) => (
                        <SelectItem key={opt.value} index={i} value={opt.value}>{opt.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="form-field">
                  <label className="form-label" id="add-due-date-label">Time to Block</label>
                  {editing && (
                    <p className="text-xs text-stone-muted" style={{ marginTop: 0, marginBottom: 4 }}>
                      Current deadline: {editing.due_date ? DUE_DATE_FMT.format(new Date(editing.due_date)) : '—'} — pick a duration below to replace it
                    </p>
                  )}
                  <Select value={dueDurationMinutes} onValueChange={setDueDurationMinutes} disabled={loading}>
                    <SelectTrigger aria-labelledby="add-due-date-label" placeholder="—" className="w-full" />
                    <SelectContent>
                      {dueDateOptionsFrom(duePresets).map((opt, i) => (
                        <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </>
          )}
          {editing && (
            <>
              <p className="form-label" style={{ fontSize: 13, fontWeight: 600, margin: '0.75rem 0 0.5rem' }}>Case details</p>
              <div className="form-row">
                <div className="form-field">
                  <label className="form-label" id="add-due-date-label">Time to Block</label>
                  {editing && (
                    <p className="text-xs text-stone-muted" style={{ marginTop: 0, marginBottom: 4 }}>
                      Current deadline: {editing.due_date ? DUE_DATE_FMT.format(new Date(editing.due_date)) : '—'} — pick a duration below to replace it
                    </p>
                  )}
                  <Select value={dueDurationMinutes} onValueChange={setDueDurationMinutes} disabled={loading}>
                    <SelectTrigger aria-labelledby="add-due-date-label" placeholder="—" className="w-full" />
                    <SelectContent>
                      {dueDateOptionsFrom(duePresets).map((opt, i) => (
                        <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </>
          )}

          <div className="form-row">
            <div className="form-field">
              <label className="form-label" htmlFor="add-case-reference-number-external">External Ref. (No. Rujukan NMD)</label>
              <input
                id="add-case-reference-number-external"
                className="form-input"
                placeholder="e.g. MCMC(S)CMOD/BLK/2026(1-2)"
                value={referenceNumberExternal}
                onChange={e => setReferenceNumberExternal(e.target.value)}
                disabled={loading}
              />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="add-case-reference-number-internal">Internal Ref. (No. Rujukan NMSMD)</label>
              <input
                id="add-case-reference-number-internal"
                className="form-input"
                placeholder="MCMC-internal only, never sent externally"
                value={referenceNumberInternal}
                onChange={e => setReferenceNumberInternal(e.target.value)}
                disabled={loading}
              />
            </div>
          </div>
          <p className="text-xs text-stone-muted" style={{ marginTop: '-0.5rem', marginBottom: '0.75rem' }}>
            Recorded as a Notice against this case regardless of "Create Letter" below.
          </p>

          {editing && (
            <>
              <p className="form-label" style={{ fontSize: 13, fontWeight: 600, margin: '0.5rem 0 0.25rem' }}>Domain details</p>
              {visibleExisting.map(d => {
                const e = domainEdits[d.url_id] ?? { agencyId: d.agency_id ?? '', status: d.status }
                const setE = (patch: Partial<typeof e>) => setDomainEdits(prev => ({ ...prev, [d.url_id]: { ...e, ...patch } }))
                return (
                  <div key={d.url_id} style={DOMAIN_ROW_STYLE}>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div className="form-label" style={{ margin: 0, wordBreak: 'break-all' }}>{d.url}</div>
                      {d.original_url && d.original_url !== d.url && (
                        <p className="text-xs text-stone-muted" style={{ margin: '2px 0 0', wordBreak: 'break-all' }}>Cited as {d.original_url}</p>
                      )}
                      {(d.offences ?? []).length > 0 ? (
                        <ul className="offence-chip-list" style={{ marginTop: 4, marginBottom: 0 }}>
                          {(d.offences ?? []).map((o, i) => (
                            <li key={i} className="offence-chip">
                              <span>{citationLabel(o)} — {o.category}{o.element ? ` (${o.element})` : ''}{o.sub_element ? ` › ${o.sub_element}` : ''}</span>
                            </li>
                          ))}
                        </ul>
                      ) : <p className="text-xs text-stone-muted" style={{ margin: '4px 0 0' }}>No offences recorded.</p>}
                    </div>
                    <DomainSettings agencies={agencies} agencyId={e.agencyId} status={e.status} onChange={setE} disabled={loading} idPrefix={`edit-${d.url_id}`} />
                    <button
                      type="button"
                      className="screenshot-icon-btn"
                      style={{ visibility: canRemoveDomain ? 'visible' : 'hidden' }}
                      onClick={() => setRemovedIds(prev => new Set(prev).add(d.url_id))}
                      disabled={loading || !canRemoveDomain}
                      aria-label={`Remove ${d.url} from this case`}
                      title="Remove from case (applied on Save)"
                    >
                      <XIcon size={14} />
                    </button>
                  </div>
                )
              })}

              {newRows.map((row, i) => {
                const setRow = (patch: Partial<typeof row>) => setNewRows(prev => prev.map((r, j) => j === i ? { ...r, ...patch } : r))
                return (
                  <div key={i} style={DOMAIN_ROW_STYLE}>
                    <input
                      className="form-input form-input-strong"
                      style={{ flex: 1, minWidth: 0 }}
                      placeholder="https://example.com"
                      aria-label="New domain"
                      value={row.url}
                      autoFocus={!row.url}
                      onChange={ev => setRow({ url: ev.target.value })}
                      disabled={loading}
                    />
                    <DomainSettings agencies={agencies} agencyId={row.agencyId} status={row.status} onChange={setRow} disabled={loading} idPrefix={`new-${i}`} />
                    <button
                      type="button"
                      className="screenshot-icon-btn"
                      onClick={() => setNewRows(prev => prev.filter((_, j) => j !== i))}
                      disabled={loading}
                      aria-label="Remove new domain row"
                    >
                      <XIcon size={14} />
                    </button>
                  </div>
                )
              })}
              <div style={{ margin: '0.25rem 0 0.5rem' }}>
                <button type="button" className="btn-ghost" onClick={() => setNewRows(prev => [...prev, { url: '', agencyId: '', status: 'requested' }])} disabled={loading}>
                  + Add domain
                </button>
              </div>
              {newDomainCount > 0 && (
                <MultiOffencePicker ref={pickerRef} value={offences} onChange={setOffences} disabled={loading} />
              )}
            </>
          )}

          <div className="form-field">
            <label className="form-label flex items-center justify-between" htmlFor="add-case-create-letter">
              Create Letter
              <Switch
                id="add-case-create-letter"
                checked={createLetter}
                onCheckedChange={setCreateLetter}
                disabled={loading}
              />
            </label>
          </div>

          {createLetter && (
            <>
              <div style={{ border: '1px solid var(--border)', borderRadius: 8, padding: '0.75rem', marginBottom: '0.75rem' }}>
                <div className="flex items-center justify-between" style={{ marginBottom: '0.5rem' }}>
                  <span className="form-label" style={{ margin: 0 }}>Notice</span>
                  <button
                    type="button"
                    className="btn-ghost"
                    style={{ fontSize: 12, padding: '2px 8px' }}
                    onClick={copySubjectFromMemo}
                    disabled={loading || !memoSubject.trim() || !!noticeSubject.trim()}
                  >
                    Copy subject from Memo
                  </button>
                </div>
                <div className="form-field">
                  <label className="form-label" htmlFor="add-case-notice-subject">Subject</label>
                  <input
                    id="add-case-notice-subject"
                    className="form-input"
                    value={noticeSubject}
                    onChange={e => setNoticeSubject(e.target.value)}
                    disabled={loading}
                  />
                </div>
                <div className="form-field">
                  <label className="form-label" htmlFor="add-case-notice-reference-internal">Internal Ref. (No. Rujukan NMSMD)</label>
                  <input
                    id="add-case-notice-reference-internal"
                    className="form-input"
                    value={referenceNumberInternal}
                    disabled
                    style={{ background: 'var(--muted)', color: 'var(--stone-muted)' }}
                  />
                </div>
              </div>

              <div style={{ border: '1px solid var(--border)', borderRadius: 8, padding: '0.75rem', marginBottom: '0.75rem' }}>
                <div className="flex items-center justify-between" style={{ marginBottom: '0.5rem' }}>
                  <span className="form-label" style={{ margin: 0 }}>Memo</span>
                  <button
                    type="button"
                    className="btn-ghost"
                    style={{ fontSize: 12, padding: '2px 8px' }}
                    onClick={copySubjectFromNotice}
                    disabled={loading || !noticeSubject.trim() || !!memoSubject.trim()}
                  >
                    Copy subject from Notice
                  </button>
                </div>
                <div className="form-field">
                  <label className="form-label" htmlFor="add-case-memo-subject">Subject</label>
                  <input
                    id="add-case-memo-subject"
                    className="form-input"
                    value={memoSubject}
                    onChange={e => setMemoSubject(e.target.value)}
                    disabled={loading}
                  />
                </div>
                <div className="form-field">
                  <label className="form-label" htmlFor="add-case-memo-reference-internal">Internal Ref. (No. Rujukan NMSMD)</label>
                  <input
                    id="add-case-memo-reference-internal"
                    className="form-input"
                    value={memoReferenceNumberInternal}
                    onChange={e => setMemoReferenceNumberInternal(e.target.value)}
                    disabled={loading}
                  />
                </div>
              </div>

              <div className="form-field">
                <label className="form-label" id="add-case-letter-recipient-label">Recipient</label>
                <Select value={recipient} onValueChange={setRecipient} disabled={loading}>
                  <SelectTrigger aria-labelledby="add-case-letter-recipient-label" placeholder="—" className="w-full" />
                  <SelectContent>
                    <SelectItem index={0} value="">—</SelectItem>
                    {recipients.map((r, i) => (
                      <SelectItem key={r.id} index={i + 1} value={r.name}>{r.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="form-row">
                <div className="form-field">
                  <label className="form-label" id="add-case-letter-requestor-label">Requestor</label>
                  <Select value={requestor} onValueChange={setRequestor} disabled={loading}>
                    <SelectTrigger aria-labelledby="add-case-letter-requestor-label" placeholder="—" className="w-full" />
                    <SelectContent>
                      <SelectItem index={0} value="">—</SelectItem>
                      {requestors.map((r, i) => (
                        <SelectItem key={r.id} index={i + 1} value={r.name}>{r.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="form-field">
                  <label className="form-label" id="add-case-letter-workflow-label">Workflow Status</label>
                  <Select value={workflowStatus} onValueChange={setWorkflowStatus} disabled={loading}>
                    <SelectTrigger aria-labelledby="add-case-letter-workflow-label" placeholder="—" className="w-full" />
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
                  <label className="form-label" htmlFor="add-case-letter-date">Letter Date</label>
                  <input id="add-case-letter-date" type="date" className="form-input" value={letterDate} onChange={e => setLetterDate(e.target.value)} disabled={loading} />
                </div>
                <div className="form-field">
                  <label className="form-label" htmlFor="add-case-letter-received">Received</label>
                  <input id="add-case-letter-received" type="date" className="form-input" value={receivedAt} onChange={e => setReceivedAt(e.target.value)} disabled={loading} />
                </div>
                <div className="form-field">
                  <label className="form-label" htmlFor="add-case-letter-submission">Submission</label>
                  <input id="add-case-letter-submission" type="date" className="form-input" value={submittedAt} onChange={e => setSubmittedAt(e.target.value)} disabled={loading} />
                </div>
              </div>

              <div className="form-field">
                <label className="form-label" htmlFor="add-case-letter-remarks">Remarks</label>
                <textarea
                  id="add-case-letter-remarks"
                  className="form-input"
                  rows={2}
                  value={remarks}
                  onChange={e => setRemarks(e.target.value)}
                  disabled={loading}
                  style={{ resize: 'vertical', fontFamily: 'inherit' }}
                />
              </div>
            </>
          )}

          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {editing ? (loading ? 'Saving…' : 'Save Changes') : (loading ? 'Creating…' : 'Create Case')}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function EditUrlDialog({
  entry,
  open,
  onClose,
}: {
  entry: URLEntry | null
  open: boolean
  onClose: () => void
}) {
  const url = entry?.url ?? null
  const [offences, setOffences] = useState<URLOffence[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [staged, setStaged] = useState<StagedOffence[]>([])
  const pickerRef = useRef<MultiOffencePickerHandle>(null)

  const load = useCallback(async () => {
    if (!url) return
    setLoading(true)
    setError(null)
    try {
      setOffences(await fetchOffencesByUrl(url))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load offences')
    } finally {
      setLoading(false)
    }
  }, [url])

  useEffect(() => {
    if (open) { load(); setStaged([]) }
  }, [open, load])

  const handleRemove = async (id: number) => {
    setError(null)
    try {
      await detachOffence(id)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove offence')
    }
  }

  // MultiOffencePicker's onChange always receives the full next array; the
  // newly-staged item is always the last one, since this dialog has no
  // batch-submit step — every addition attaches immediately, unlike
  // AddUrlDialog which accumulates offences for one later submit.
  const handleAddStaged = async (next: StagedOffence[]) => {
    if (!url || next.length === 0) { setStaged(next); return }
    const added = next[next.length - 1]
    setError(null)
    try {
      await attachOffence(url, added.categoryId, added.elementId, added.subElementId)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add offence')
    } finally {
      setStaged([])
    }
  }

  // "Done" used to just close — a filled-in-but-not-yet-"+ Add offence"-
  // clicked selection sitting in the picker was silently discarded rather
  // than attached. Flush it first, and keep the dialog open on failure so
  // the error is visible instead of losing the offence a second way.
  const handleDone = async () => {
    const pending = pickerRef.current?.flush()
    if (pending && url) {
      setError(null)
      try {
        await attachOffence(url, pending.categoryId, pending.elementId, pending.subElementId)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to add offence')
        return
      }
    }
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleDone() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 560 }}>
        <DialogHeader>
          <DialogTitle>Edit Offences</DialogTitle>
          <DialogDescription>{url}</DialogDescription>
        </DialogHeader>

        <div className="form-field">
          <label className="form-label" id="edit-offences-label">Offences</label>
          {loading ? (
            <p className="text-sm text-stone-muted">Loading…</p>
          ) : offences.length > 0 ? (
            <ul className="offence-chip-list">
              {offences.map(o => (
                <li key={o.id} className="offence-chip">
                  <span>{formatParsedCitation(o.category.citation.parsed)} — {o.category.name}{o.element ? ` (${o.element.name})` : ''}{o.sub_element ? ` › ${o.sub_element.name}` : ''}</span>
                  <button
                    type="button"
                    className="screenshot-icon-btn"
                    onClick={() => handleRemove(o.id)}
                    aria-label={`Remove offence ${o.category.name}`}
                  >
                    <XIcon size={14} />
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-stone-muted mb-2">No offences attached yet.</p>
          )}
        </div>
        <MultiOffencePicker ref={pickerRef} value={staged} onChange={handleAddStaged} disabled={loading} />

        {error && <p className="form-error">{error}</p>}
        <DialogFooter>
          <button type="button" className="btn-primary" onClick={handleDone}>
            Done
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/* ─── URLs Page ──────────────────────────────────────────────────────────── */

const DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric', month: 'short', year: 'numeric',
})

const LETTER_DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC',
})

// Explicit timeZone so the deadline reads the same regardless of the
// viewing browser's OS timezone — matches the backend's own hardcoded
// Asia/Kuala_Lumpur reporting convention (see postgresStore's
// reportingLocation) rather than leaving it to an implicit local default.
const DUE_DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit',
  timeZone: 'Asia/Kuala_Lumpur',
})

// Renders a table cell for a multi-valued column (e.g. one of the offence
// sub-fields, which a domain can have several of) as one line per instance,
// wrapping the row's height rather than truncating — shared by the
// Domain-view and Cases-view offence columns below.
// Act + section, so a bare "Seksyen 233" never appears without its instrument.
const citationLabel = (o: OffenceEntry) => o.instrument ? `${o.instrument}, ${o.citation}` : o.citation

function joinedCell(values: (string | undefined)[]) {
  const list = values.filter((v): v is string => !!v)
  if (list.length === 0) return <span className="dns-name">—</span>
  return (
    <div className="flex flex-col gap-1">
      {list.map((v, i) => <span key={i} className="dns-name">{v}</span>)}
    </div>
  )
}

const PAGE_SIZE = 25

const IS_ONLY = [{ value: 'is', label: 'is' }]

const DATE_OPERATORS = [
  { value: 'on', label: 'on' },
  { value: 'before', label: 'before' },
  { value: 'after', label: 'after' },
  { value: 'between', label: 'between' },
]

// `bare` skips DatePicker's own bordered/rounded shell (built for standalone
// form fields) — the customRenderer slot's ButtonGroupText wrapper already
// supplies that chrome, so without `bare` the two nest into a double box.
// values hold plain 'yyyy-MM-dd' strings (see matchesDateFilter) — parseISO
// (not `new Date()`) so a date-only string parses as local midnight, not UTC.
function DateFilterRenderer({ values, onChange, operator }: CustomRendererProps<string>) {
  const [from, to] = values
  const set = (index: 0 | 1) => (date: Date | null) => {
    const next = [from ?? '', to ?? '']
    next[index] = date ? format(date, 'yyyy-MM-dd') : ''
    onChange(operator === 'between' ? next : [next[0]])
  }

  if (operator === 'between') {
    return (
      <div className="flex items-center gap-1">
        <DatePicker value={from ? parseISO(from) : null} onChange={set(0)} placeholder="From" clearable bare calendarProps={{ size: 'sm' }} />
        <span className="text-xs text-stone-muted">–</span>
        <DatePicker value={to ? parseISO(to) : null} onChange={set(1)} placeholder="To" clearable bare calendarProps={{ size: 'sm' }} />
      </div>
    )
  }
  return <DatePicker value={from ? parseISO(from) : null} onChange={set(0)} clearable bare calendarProps={{ size: 'sm' }} />
}

// due_date/created_at are full ISO timestamps; filter values are date-only —
// compare on the date portion so "on 10 Aug" matches any time that day.
function matchesDateFilter(value: string | null | undefined, filter: Filter<string> | undefined): boolean {
  if (!filter) return true
  const [from, to] = filter.values
  // Chip added but no date picked yet — pass through, same as an unset
  // select filter, rather than hiding every row until a date is chosen.
  if (!from && !to) return true
  if (!value) return false
  const day = value.slice(0, 10)
  switch (filter.operator) {
    case 'on': return day === from
    case 'before': return day < from
    case 'after': return day > from
    case 'between': return (!from || day >= from) && (!to || day <= to)
    default: return true
  }
}

type DomainSubRow = { kind: 'domain'; caseId: number; status: string; domain: CaseSummaryDomain }
type CaseRow = { kind: 'case'; summary: CaseSummary; subRows: DomainSubRow[] }
type CaseTreeRow = CaseRow | DomainSubRow

function URLsPage() {
  const { view } = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  const [caseSummaries, setCaseSummaries] = useState<CaseSummary[]>([])
  const [addOpen, setAddOpen] = useState(false)
  const [editingCase, setEditingCase] = useState<CaseSummary | null>(null)
  // Cases are paged/filtered/sorted on the server; `casesNonce` re-runs the fetch after an edit.
  const [casesTotal, setCasesTotal] = useState(0)
  const [casesLoading, setCasesLoading] = useState(true)
  const [casesNonce, setCasesNonce] = useState(0)
  const [exporting, setExporting] = useState(false)
  const [exportError, setExportError] = useState<string | null>(null)

  const [casesSorting, setCasesSorting] = useState<SortingState>([])
  const [casesPagination, setCasesPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })
  const [casesColumnVisibility, setCasesColumnVisibility] = useState<VisibilityState>({})
  const [casesExpanded, setCasesExpanded] = useState<ExpandedState>({})

  const { ready: casesGridPrefReady } = useGridPreference(
    'urls-cases',
    { sorting: casesSorting, columnVisibility: casesColumnVisibility, pageSize: casesPagination.pageSize },
    {
      setSorting: setCasesSorting,
      setColumnVisibility: setCasesColumnVisibility,
      setPageSize: pageSize => setCasesPagination(p => ({ ...p, pageSize })),
    }
  )

  const [urls, setUrls] = useState<URLEntry[]>([])
  const [agencies, setAgencies] = useState<Agency[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [duePresets, setDuePresets] = useState<DueDatePreset[]>([])
  const [recipients, setRecipients] = useState<Recipient[]>([])
  const [requestors, setRequestors] = useState<Requestor[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{ id: number; url: string } | null>(null)
  // id, not a URLEntry snapshot, so the dialog re-reads the live row out of
  // `urls` below and reflects its own edits (agency/status/etc.) immediately.
  const [editTargetId, setEditTargetId] = useState<number | null>(null)
  const editTarget = urls.find(u => u.id === editTargetId) ?? null

  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<Filter<string>[]>([])
  const [sorting, setSorting] = useState<SortingState>([])
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})

  const { ready: gridPrefReady } = useGridPreference(
    'urls',
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
      setError(null)
      const [u, a, d, p, rc, rq] = await Promise.all([
        fetchUrls(), fetchAgencies(), fetchDepartmentsOpen(), fetchDueDatePresets(), fetchRecipients(), fetchRequestors(),
      ])
      setUrls(u)
      setAgencies(a)
      setDepartments(d)
      setDuePresets(p)
      setRecipients(rc)
      setRequestors(rq)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load domains')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleToggle = useCallback(async (id: number, enabled: boolean) => {
    setUrls(prev => prev.map(u => u.id === id ? { ...u, enabled } : u))
    try {
      await setUrlEnabled(id, enabled)
    } catch {
      setUrls(prev => prev.map(u => u.id === id ? { ...u, enabled: !enabled } : u))
    }
  }, [])

  // `status` is derived from the url's own CaseURL row within its latest
  // Case (see internal/db/CLAUDE.md), so editing it patches that (case, url)
  // pair via case_id, not a PATCH /api/urls/{id}.
  const handleStatusChange = useCallback(async (entry: URLEntry, status: string) => {
    if (!entry.case_id) return
    const prevStatus = entry.status
    setUrls(prev => prev.map(u => u.id === entry.id ? { ...u, status } : u))
    try {
      await updateCaseURLStatus(entry.case_id, entry.id, status)
    } catch {
      setUrls(prev => prev.map(u => u.id === entry.id ? { ...u, status: prevStatus } : u))
    }
  }, [])

  const handleDelete = async () => {
    if (!deleteTarget) return
    await deleteUrl(deleteTarget.id)
    setDeleteTarget(null)
    load()
  }

  const filterFields = useMemo<FilterFieldConfig<string>[]>(() => [
    { key: 'status', label: 'Status', type: 'select', operators: IS_ONLY, options: STATUS_OPTIONS.filter(o => o.value).map(o => ({ value: o.value, label: o.label })) },
    { key: 'requesting_dept', label: 'Requesting Dept.', type: 'select', operators: IS_ONLY, options: departments.map(d => ({ value: String(d.id), label: d.name })) },
    { key: 'agency', label: 'Agency', type: 'select', operators: IS_ONLY, options: agencies.map(a => ({ value: String(a.id), label: a.name })) },
    { key: 'created_at', label: 'Date Added', type: 'custom', operators: DATE_OPERATORS, defaultOperator: 'on', customRenderer: DateFilterRenderer },
    { key: 'due_date', label: 'Due Date', type: 'custom', operators: DATE_OPERATORS, defaultOperator: 'on', customRenderer: DateFilterRenderer },
  ], [agencies, departments])

  const statusFilter = filters.find(f => f.field === 'status')?.values[0]
  const deptFilter = filters.find(f => f.field === 'requesting_dept')?.values[0]
  const deptFilterName = deptFilter ? departments.find(d => String(d.id) === deptFilter)?.name : undefined
  const agencyFilter = filters.find(f => f.field === 'agency')?.values[0]
  const createdAtFilter = filters.find(f => f.field === 'created_at')
  const dueDateFilter = filters.find(f => f.field === 'due_date')

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    return urls.filter(u =>
      (!query || u.url.toLowerCase().includes(query) || (u.current_reference_number ?? '').toLowerCase().includes(query)) &&
      (!statusFilter || u.status === statusFilter) &&
      (!deptFilterName || (u.requesting_departments ?? []).includes(deptFilterName)) &&
      (!agencyFilter || String(u.agency_id ?? '') === agencyFilter) &&
      matchesDateFilter(u.created_at, createdAtFilter) &&
      matchesDateFilter(u.due_date, dueDateFilter)
    )
  }, [urls, search, statusFilter, deptFilterName, agencyFilter, createdAtFilter, dueDateFilter])

  // Debounced so typing in the search box doesn't fire a request per keystroke.
  const [debouncedSearch, setDebouncedSearch] = useState('')
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search), 300)
    return () => clearTimeout(t)
  }, [search])

  // Column id -> the server's sort key; columns not listed aren't sortable.
  const CASE_SORT_KEYS = { case: 'id', due_date: 'due_date', notice_letter_date: 'notice_letter_date', uplift_letter_date: 'uplift_letter_date' } as const
  const casesSortKey = CASE_SORT_KEYS[casesSorting[0]?.id as keyof typeof CASE_SORT_KEYS]
  const casesSortDesc = casesSorting[0]?.desc
  const createdKey = JSON.stringify(createdAtFilter)
  const dueKey = JSON.stringify(dueDateFilter)
  useEffect(() => {
    if (!casesGridPrefReady) return // wait for the saved page size so the first fetch is the only one
    let stale = false
    setCasesLoading(true)
    const dateQuery = (f: Filter<string> | undefined) => (f ? { op: f.operator, from: f.values[0], to: f.values[1] } : undefined)
    fetchCaseSummariesPage({
      page: casesPagination.pageIndex + 1,
      pageSize: casesPagination.pageSize,
      q: debouncedSearch,
      status: statusFilter,
      agencyId: agencyFilter,
      deptId: deptFilter,
      created: dateQuery(createdAtFilter),
      due: dateQuery(dueDateFilter),
      sort: casesSortKey,
      desc: casesSortDesc,
    })
      .then(r => { if (!stale) { setCaseSummaries(r.cases); setCasesTotal(r.total); setError(null) } })
      .catch(err => { if (!stale) setError(err instanceof Error ? err.message : 'Failed to load cases') })
      .finally(() => { if (!stale) setCasesLoading(false) })
    return () => { stale = true }
    // createdAtFilter/dueDateFilter are re-created every render; their JSON keys are the stable deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [casesPagination.pageIndex, casesPagination.pageSize, debouncedSearch, statusFilter, agencyFilter, deptFilter, createdKey, dueKey, casesSortKey, casesSortDesc, casesNonce, casesGridPrefReady])

  useEffect(() => {
    setPagination(p => ({ ...p, pageIndex: 0 }))
    setCasesPagination(p => ({ ...p, pageIndex: 0 }))
  }, [search, statusFilter, deptFilter, agencyFilter, createdAtFilter, dueDateFilter, casesSortKey, casesSortDesc])

  const columns = useMemo<ColumnDef<URLEntry>[]>(() => [
    {
      id: 'domain',
      accessorFn: u => u.url,
      header: ({ column }) => <SortableHeader column={column} title="Domain" />,
      enableHiding: false,
      size: 280,
      meta: {
        headerTitle: 'Domain',
        headerClassName: 'col-domain th-left',
        cellClassName: 'col-domain',
        skeleton: <span className="skeleton" style={{ width: 200, height: 14 }} />,
      },
      cell: ({ row }) => {
        const u = row.original
        return (
          <PreviewLinkCard href={u.url}>
            <PreviewLinkCardTrigger>
              <span className="hostname flex items-center gap-2 min-w-0">
                <img src={faviconApiUrl(u.url)} alt="" width={16} height={16} className="shrink-0" onError={e => { e.currentTarget.style.visibility = 'hidden' }} />
                {u.url}
              </span>
            </PreviewLinkCardTrigger>
            <PreviewLinkCardPanel>
              <PreviewLinkCardImage />
            </PreviewLinkCardPanel>
          </PreviewLinkCard>
        )
      },
    },
    {
      id: 'created_at',
      accessorFn: u => u.created_at,
      size: 110,
      header: ({ column }) => <SortableHeader column={column} title="Added" />,
      meta: { headerTitle: 'Added', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{DATE_FMT.format(new Date(row.original.created_at))}</span>,
    },
    {
      id: 'agency',
      accessorFn: u => u.agency_id ?? '',
      size: 130,
      header: 'Agency',
      meta: { headerTitle: 'Agency', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{row.original.agency_name ?? '—'}</span>,
    },
    {
      id: 'current_reference_number',
      accessorFn: u => u.current_reference_number ?? '',
      size: 130,
      header: 'Reference No.',
      meta: { headerTitle: 'Reference No.', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{row.original.current_reference_number || '—'}</span>,
    },
    {
      id: 'requesting_departments',
      accessorFn: u => (u.requesting_departments ?? []).join(', '),
      size: 160,
      header: 'Requesting Dept.',
      meta: { headerTitle: 'Requesting Dept.', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{(row.original.requesting_departments ?? []).join(', ') || '—'}</span>,
    },
    {
      id: 'offence_citation',
      accessorFn: u => (u.offences ?? []).map(citationLabel).join('; '),
      size: 220,
      header: 'Offence Details',
      meta: { headerTitle: 'Offence Details', headerClassName: 'col-status', cellClassName: 'col-status' },
      cell: ({ row }) => joinedCell((row.original.offences ?? []).map(citationLabel)),
    },
    {
      id: 'offence_category',
      accessorFn: u => (u.offences ?? []).map(o => o.category).join('; '),
      size: 140,
      header: 'Category',
      meta: { headerTitle: 'Category', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => joinedCell((row.original.offences ?? []).map(o => o.category)),
    },
    {
      id: 'offence_element',
      accessorFn: u => (u.offences ?? []).map(o => o.element ?? '').join('; '),
      size: 140,
      header: 'Element',
      meta: { headerTitle: 'Element', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => joinedCell((row.original.offences ?? []).map(o => o.element)),
    },
    {
      id: 'offence_sub_element',
      accessorFn: u => (u.offences ?? []).map(o => o.sub_element ?? '').join('; '),
      size: 140,
      header: 'Sub-Element',
      meta: { headerTitle: 'Sub-Element', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => joinedCell((row.original.offences ?? []).map(o => o.sub_element)),
    },
    {
      id: 'status',
      accessorFn: u => u.status ?? '',
      size: 110,
      header: 'Status',
      meta: {
        headerTitle: 'Status',
        headerClassName: 'col-status',
        cellClassName: 'col-status text-center',
        skeleton: <span className="skeleton" style={{ width: 90, height: 20, borderRadius: 4 }} />,
      },
      cell: ({ row }) => {
        const u = row.original
        return (
          <Select value={u.status ?? ''} onValueChange={v => handleStatusChange(u, v)} disabled={!u.case_id}>
            <SelectTrigger aria-label={`Status for ${u.url}`} placeholder="—" className="w-full" />
            <SelectContent>
              {STATUS_OPTIONS.map((opt, i) => (
                <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )
      },
    },
    {
      id: 'due_date',
      accessorFn: u => u.due_date ?? '',
      size: 170,
      header: ({ column }) => <SortableHeader column={column} title="Due Date" />,
      meta: {
        headerTitle: 'Due Date',
        headerClassName: 'col-status',
        cellClassName: 'col-status text-center',
        skeleton: <span className="skeleton" style={{ width: 160, height: 20, borderRadius: 4 }} />,
      },
      cell: ({ row }) => {
        const { due_date } = row.original
        return <span className="dns-name">{due_date ? DUE_DATE_FMT.format(new Date(due_date)) : '—'}</span>
      },
    },
    {
      id: 'action',
      header: 'Action',
      enableHiding: false,
      size: 140,
      meta: { headerClassName: 'th-center', cellClassName: 'text-center' },
      cell: ({ row }) => {
        const u = row.original
        return (
          <div className="flex items-center justify-center gap-3">
            <Switch
              checked={u.enabled}
              onCheckedChange={checked => handleToggle(u.id, checked)}
              aria-label={`${u.enabled ? 'Disable' : 'Enable'} ${u.url} in scan`}
            />
            <div className="flex items-center gap-1">
              <Link
                to="/domain/$url"
                params={{ url: u.url }}
                search={{ tab: 'overview' }}
                className="screenshot-icon-btn"
                aria-label={`View details for ${u.url}`}
                title="View details"
              >
                <GripIcon size={16} />
              </Link>
              <button
                type="button"
                className="screenshot-icon-btn"
                onClick={() => setEditTargetId(u.id)}
                aria-label={`Edit ${u.url}`}
                title="Edit"
              >
                <FileText size={16} />
              </button>
              <button
                type="button"
                className="screenshot-icon-btn"
                onClick={() => setDeleteTarget(u)}
                aria-label={`Delete ${u.url}`}
                title="Delete"
              >
                <XIcon size={16} />
              </button>
            </div>
          </div>
        )
      },
    },
  ], [handleToggle, handleStatusChange])

  const table = useReactTable({
    data: filtered,
    columns,
    initialState: { columnPinning: { left: ['domain'], right: ['action'] } },
    state: { sorting, pagination, columnVisibility },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    onColumnVisibilityChange: setColumnVisibility,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  })

  // Holds the grid in its loading state until the saved column
  // visibility/sort/page-size layout has been applied, so it renders once
  // already in its final shape instead of flashing plain defaults first.
  const gridLoading = loading || !gridPrefReady

  const caseTreeData = useMemo<CaseRow[]>(() => caseSummaries.map(summary => ({
    kind: 'case',
    summary,
    subRows: summary.domains.map(domain => ({ kind: 'domain', caseId: summary.id, status: domain.status, domain })),
  })), [caseSummaries])

  const handleExportCases = useCallback(async () => {
    setExporting(true)
    setExportError(null)
    try {
      // Always the full list — the table only holds one page, and the server
      // already scopes the export to the caller's own cases.
      const { blob, filename } = await exportCaseSummaries()
      downloadBlob(blob, filename ?? `blocking-list-export-${new Date().toISOString().slice(0, 10)}.xlsx`)
    } catch (err) {
      setExportError(err instanceof Error ? err.message : 'Failed to export cases')
    } finally {
      setExporting(false)
    }
  }, [])

  const caseColumns = useMemo<ColumnDef<CaseTreeRow>[]>(() => [
    {
      id: 'case',
      accessorFn: r => r.kind === 'case' ? r.summary.id : r.domain.url,
      header: ({ column }) => <SortableHeader column={column} title="Case #" />,
      enableHiding: false,
      size: 340,
      meta: { headerTitle: 'Case #', headerClassName: 'col-domain th-left', cellClassName: 'col-domain' },
      cell: ({ row }) => {
        const original = row.original
        const expandControl = (
          <DataGridTableRowExpand row={row}>
            <ChevronRight className={`expand-icon${row.getIsExpanded() ? ' expanded' : ''}`} />
          </DataGridTableRowExpand>
        )
        if (original.kind === 'domain') {
          return <span className="flex items-center gap-[2px]">{expandControl}<span className="dns-name">{original.domain.url}</span></span>
        }
        return (
          <span className="flex items-center gap-[2px]">
            {expandControl}
            <span className="flex flex-col">
              <span className="hostname">#{original.summary.id}</span>
              {original.summary.notice_reference_number_internal && (
                <span className="text-muted-foreground text-[0.8rem]">{original.summary.notice_reference_number_internal}</span>
              )}
            </span>
          </span>
        )
      },
    },
    {
      id: 'cited_url',
      header: 'Original URL',
      // Per-domain, same reasoning as status/offences below — CaseURL.OriginalURL
      // is only set when the as-cited text (e.g. a full URL with a path) differs
      // from the normalized `url` DNS scanning uses, so most rows show '—'.
      accessorFn: r => r.kind === 'domain' ? (r.domain.original_url ?? '') : '',
      meta: { headerTitle: 'Original URL', headerClassName: 'col-status', cellClassName: 'col-status' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'case') return <span className="dns-name">—</span>
        const { original_url, url } = original.domain
        return <span className="dns-name">{original_url && original_url !== url ? original_url : '—'}</span>
      },
    },
    {
      id: 'agency',
      header: 'Agency',
      // Agency is per-domain (CaseURL.AgencyID, moved off Case 2026-09-15) —
      // a case row has no single agency of its own (its domains can each be
      // requested by a different one), so it's left blank here and only
      // shown/edited on domain subrows, same convention as Status above.
      accessorFn: r => r.kind === 'domain' ? (r.domain.agency_name ?? '') : '',
      meta: { headerTitle: 'Agency', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'case') {
          return <span className="dns-name">—</span>
        }
        return <span className="dns-name">{original.domain.agency_name ?? '—'}</span>
      },
    },
    {
      id: 'status',
      header: 'Status',
      // Status is per-domain — a case row has no single status of its own
      // (its domains can each carry a different one), so it's left blank
      // here and only shown/edited on domain subrows.
      accessorFn: r => r.kind === 'domain' ? r.status : '',
      meta: { headerTitle: 'Status', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'case') {
          return <span className="dns-name">—</span>
        }
        return <span className="dns-name">{STATUS_OPTIONS.find(o => o.value === original.status)?.label ?? original.status}</span>
      },
    },
    {
      id: 'scan_status',
      header: 'Latest Scan',
      // Per-domain, like Status above: the latest scan run's outcome across every DNS server checked.
      // Not the case's workflow Status — this is what the scan actually observed.
      accessorFn: r => r.kind === 'domain' ? (r.domain.scan_total ?? 0) : 0,
      meta: { headerTitle: 'Latest Scan', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'case') return <span className="dns-name">—</span>
        const { scan_total: total = 0, scan_compliant: compliant = 0, scanned_at } = original.domain
        if (total === 0) return <span className="dns-name">Not scanned</span>
        const all = compliant === total
        return (
          <span className="status-dot-label" title={scanned_at ? `Scan run started ${DUE_DATE_FMT.format(new Date(scanned_at))}` : undefined}>
            <span className={`status-dot ${all ? 'dot-compliant' : 'dot-violation'}`} aria-hidden="true" />
            <span className={all ? 'label-compliant' : 'label-violation'}>{compliant} of {total} compliant</span>
          </span>
        )
      },
    },
    {
      id: 'offence_citation',
      header: 'Offence Details',
      // Same reasoning as status above — offences are per-domain, so a case
      // row (whose domains can each carry different ones) is left blank.
      accessorFn: r => r.kind === 'domain' ? (r.domain.offences ?? []).map(citationLabel).join('; ') : '',
      meta: { headerTitle: 'Offence Details', headerClassName: 'col-status', cellClassName: 'col-status' },
      cell: ({ row }) => {
        const original = row.original
        return original.kind === 'case' ? <span className="dns-name">—</span> : joinedCell((original.domain.offences ?? []).map(citationLabel))
      },
    },
    {
      id: 'offence_category',
      header: 'Category',
      accessorFn: r => r.kind === 'domain' ? (r.domain.offences ?? []).map(o => o.category).join('; ') : '',
      meta: { headerTitle: 'Category', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => {
        const original = row.original
        return original.kind === 'case' ? <span className="dns-name">—</span> : joinedCell((original.domain.offences ?? []).map(o => o.category))
      },
    },
    {
      id: 'offence_element',
      header: 'Element',
      accessorFn: r => r.kind === 'domain' ? (r.domain.offences ?? []).map(o => o.element ?? '').join('; ') : '',
      meta: { headerTitle: 'Element', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => {
        const original = row.original
        return original.kind === 'case' ? <span className="dns-name">—</span> : joinedCell((original.domain.offences ?? []).map(o => o.element))
      },
    },
    {
      id: 'offence_sub_element',
      header: 'Sub-Element',
      accessorFn: r => r.kind === 'domain' ? (r.domain.offences ?? []).map(o => o.sub_element ?? '').join('; ') : '',
      meta: { headerTitle: 'Sub-Element', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => {
        const original = row.original
        return original.kind === 'case' ? <span className="dns-name">—</span> : joinedCell((original.domain.offences ?? []).map(o => o.sub_element))
      },
    },
    {
      id: 'due_date',
      accessorFn: r => r.kind === 'case' ? (r.summary.due_date ?? '') : '',
      header: ({ column }) => <SortableHeader column={column} title="Due Date" />,
      meta: { headerTitle: 'Due Date', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case'
        ? <span className="dns-name">{row.original.summary.due_date ? DUE_DATE_FMT.format(new Date(row.original.summary.due_date)) : '—'}</span>
        : null,
    },
    {
      id: 'reference_number_internal',
      header: 'Internal Ref',
      accessorFn: r => r.kind === 'case' ? (r.summary.notice_reference_number_internal ?? '') : '',
      meta: { headerTitle: 'Internal Ref', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? <span className="dns-name">{row.original.summary.notice_reference_number_internal || '—'}</span> : null,
    },
    {
      id: 'reference_number_external',
      header: 'External Ref',
      accessorFn: r => r.kind === 'case' ? (r.summary.notice_reference_number_external ?? '') : '',
      meta: { headerTitle: 'External Ref', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? <span className="dns-name">{row.original.summary.notice_reference_number_external || '—'}</span> : null,
    },
    {
      id: 'notice_letter_date',
      header: ({ column }) => <SortableHeader column={column} title="Notice Letter Date" />,
      accessorFn: r => r.kind === 'case' ? (r.summary.notice_letter_date ?? '') : '',
      meta: { headerTitle: 'Notice Letter Date', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      // Letter dates are stored as UTC midnight (a calendar day, not an instant), so format in UTC.
      cell: ({ row }) => row.original.kind === 'case'
        ? <span className="dns-name">{row.original.summary.notice_letter_date ? LETTER_DATE_FMT.format(new Date(row.original.summary.notice_letter_date)) : '—'}</span>
        : null,
    },
    {
      id: 'uplift_letter_date',
      header: ({ column }) => <SortableHeader column={column} title="Uplift Letter Date" />,
      accessorFn: r => r.kind === 'case' ? (r.summary.uplift_letter_date ?? '') : '',
      meta: { headerTitle: 'Uplift Letter Date', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case'
        ? <span className="dns-name">{row.original.summary.uplift_letter_date ? LETTER_DATE_FMT.format(new Date(row.original.summary.uplift_letter_date)) : '—'}</span>
        : null,
    },
    {
      id: 'domain_count',
      header: 'Domains',
      accessorFn: r => r.kind === 'case' ? r.summary.domains.length : '',
      meta: { headerTitle: 'Domains', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? <span className="dns-name">{row.original.summary.domains.length}</span> : null,
    },
    {
      id: 'subject',
      header: 'Subject',
      accessorFn: r => r.kind === 'case' ? (r.summary.notice_subject ?? '') : '',
      meta: { headerTitle: 'Subject', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? <span className="dns-name">{row.original.summary.notice_subject || '—'}</span> : null,
    },
    {
      id: 'workflow_status',
      header: 'Workflow Status',
      accessorFn: r => r.kind === 'case' ? (r.summary.notice_workflow_status ?? '') : '',
      meta: { headerTitle: 'Workflow Status', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? <span className="dns-name">{row.original.summary.notice_workflow_status || '—'}</span> : null,
    },
    {
      id: 'action',
      header: 'Action',
      enableHiding: false,
      size: 140,
      meta: { headerClassName: 'th-center', cellClassName: 'text-center' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'case') {
          return (
            <button type="button" className="screenshot-icon-btn" onClick={e => { e.stopPropagation(); setEditingCase(original.summary) }} aria-label={`Edit case #${original.summary.id}`} title="Edit">
              <SquarePenIcon size={16} />
            </button>
          )
        }
        return (
          <div className="flex items-center justify-center gap-1">
            <Link to="/domain/$url" params={{ url: original.domain.url }} search={{ tab: 'overview' }} className="screenshot-icon-btn" aria-label={`View details for ${original.domain.url}`} title="View details">
              <GripIcon size={16} />
            </Link>
          </div>
        )
      },
    },
  ], [])

  const casesTable = useReactTable({
    data: caseTreeData,
    columns: caseColumns,
    initialState: { columnPinning: { left: ['case'], right: ['action'] } },
    state: { sorting: casesSorting, pagination: casesPagination, columnVisibility: casesColumnVisibility, expanded: casesExpanded },
    manualPagination: true,
    manualSorting: true,
    rowCount: casesTotal,
    onSortingChange: setCasesSorting,
    onPaginationChange: setCasesPagination,
    onColumnVisibilityChange: setCasesColumnVisibility,
    onExpandedChange: setCasesExpanded,
    getRowId: r => r.kind === 'case' ? `case:${r.summary.id}` : `case-domain:${r.caseId}:${r.domain.url_id}`,
    getSubRows: r => r.kind === 'case' ? r.subRows : undefined,
    getRowCanExpand: row => row.original.kind === 'case' && row.original.subRows.length > 0,
    // Paging is server-side (manualPagination), so sub-rows must join the expanded model directly —
    // with false they'd only be spliced in by getPaginationRowModel, which manualPagination bypasses.
    paginateExpandedRows: true,
    getCoreRowModel: getCoreRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
  })

  const casesGridLoading = loading || casesLoading || !casesGridPrefReady
  const noCasesAtAll = !casesGridLoading && casesTotal === 0 && !debouncedSearch && filters.length === 0

  return (
    <div className="mx-20 mt-10 mb-10">
      <div className="page-header">
        <h1 className="page-title mb-4">Domains</h1>
        <p className="page-subtitle">{!loading && `${urls.length} monitored`}</p>
        <ToggleGroup
          type="single"
          value={view}
          onValueChange={v => { if (v) navigate({ to: '/urls', search: { view: v as 'domains' | 'cases' }, replace: true }) }}
          variant="outline"
          aria-label="View"
        >
          <ToggleGroupItem value="cases">Cases</ToggleGroupItem>
          <ToggleGroupItem value="domains">Domain</ToggleGroupItem>
        </ToggleGroup>
        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          <Button onClick={() => setAddOpen(true)}>
            + Create Case
          </Button>
        </div>
      </div>

      {view === 'domains' ? (
        error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : !gridLoading && urls.length === 0 ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No domains yet</p>
            <p className="empty-body">Create a case to start monitoring a domain for DNS compliance.</p>
            <button className="btn-primary" onClick={() => setAddOpen(true)}>Create Case</button>
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
              <div style={{ marginLeft: 'auto' }}>
                <DataGridColumnVisibility table={table} trigger={<Button variant="outline">Columns</Button>} />
              </div>
            </div>

            <div className="results-wrap w-full">
              {!gridLoading && filtered.length === 0 ? (
                <div className="empty-state" style={{ padding: '3rem 0' }}>
                  <p className="empty-heading">No domains match the current filters</p>
                </div>
              ) : (
                <DataGrid
                  table={table}
                  recordCount={filtered.length}
                  isLoading={gridLoading}
                  tableClassNames={{ base: 'results-table results-table--pinned' }}
                  tableLayout={{ columnsPinnable: true }}
                >
                  <DataGridContainer className="overflow-x-auto overflow-y-visible mb-5">
                    <DataGridTable />
                  </DataGridContainer>
                  <DataGridPagination sizes={[10, 25, 50, 100]} />
                </DataGrid>
              )}
            </div>
          </div>
        )
      ) : (
        error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : noCasesAtAll ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No cases yet</p>
            <p className="empty-body">Create a case to start monitoring a domain for DNS compliance.</p>
            <button className="btn-primary" onClick={() => setAddOpen(true)}>Create Case</button>
          </div>
        ) : (
          <div className="flex flex-col items-stretch w-full gap-4 mt-4">
            <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full">
              <Input
                type="search"
                placeholder="Search case #, ref. or domain..."
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="max-w-64"
                aria-label="Search cases"
              />
              <Filters filters={filters} fields={filterFields} onChange={setFilters} />
              {exportError && <p className="error-message">{exportError}</p>}
              <div style={{ marginLeft: 'auto' }} className="flex items-center gap-2">
                <Button variant="outline" size="icon" onClick={handleExportCases} disabled={exporting} aria-label={exporting ? 'Exporting…' : 'Export'} title={exporting ? 'Exporting…' : 'Export'}>
                  <DownloadIcon size={16} />
                </Button>
                <DataGridColumnVisibility table={casesTable} trigger={<Button variant="outline">Columns</Button>} />
              </div>
            </div>
            <div className="results-wrap w-full">
              {!casesGridLoading && casesTotal === 0 ? (
                <div className="empty-state" style={{ padding: '3rem 0' }}>
                  <p className="empty-heading">No cases match the current filters</p>
                </div>
              ) : (
                <DataGrid
                  table={casesTable}
                  recordCount={casesTotal}
                  isLoading={casesGridLoading}
                  tableClassNames={{ base: 'results-table results-table--pinned' }}
                  tableLayout={{ columnsPinnable: true }}
                  onRowClick={row => { if (row.kind === 'case') casesTable.getRow(`case:${row.summary.id}`).toggleExpanded() }}
                >
                  <DataGridContainer className="overflow-x-auto overflow-y-visible mb-5">
                    <DataGridTable />
                  </DataGridContainer>
                  <DataGridPagination sizes={[10, 25, 50, 100]} />
                </DataGrid>
              )}
            </div>
          </div>
        )
      )}

      <AddUrlDialog
        open={addOpen || editingCase !== null}
        onClose={() => { setAddOpen(false); setEditingCase(null) }}
        onAdded={() => { load(); setCasesNonce(n => n + 1) }}
        agencies={agencies}
        duePresets={duePresets}
        recipients={recipients}
        requestors={requestors}
        editing={editingCase}
      />

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget?.url ?? ''}
        description="This will remove it from your department's watchlist. The domain and its scan history are kept if any other department still watches it."
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />

      <EditUrlDialog
        entry={editTarget}
        open={editTargetId !== null}
        onClose={() => setEditTargetId(null)}
      />
    </div>
  )
}
