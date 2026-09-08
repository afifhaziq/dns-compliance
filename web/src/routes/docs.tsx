import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
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
import { fetchAllCaseLetters, createCase, addCaseLetter, addUrlToCase, updateCaseLetter, deleteCaseLetter, exportCaseLetters } from '@/api/cases'
import { downloadBlob } from '@/lib/download'
import { createUrl, fetchUrls } from '@/api/urls'
import { fetchDepartmentsOpen } from '@/api/departments'
import { fetchRecipients } from '@/api/recipients'
import { fetchRequestors } from '@/api/requestors'
import { fetchAgencies } from '@/api/agencies'
import { fetchDueDatePresets } from '@/api/due-date-presets'
import { fetchUsersOpen } from '@/api/users'
import { useAuth } from './__root'
import type { CaseLetterEntry, Department, Recipient, Requestor, Agency, DueDatePreset, User } from '@/api/types'
import { CASE_STATUS_OPTIONS, LETTER_TYPE_OPTIONS as CASE_LETTER_TYPE_OPTIONS } from '@/lib/case-options'
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
import { DataGridTable, DataGridTableRowExpand } from '@/components/reui/data-grid/data-grid-table'
import { DataGridColumnVisibility } from '@/components/reui/data-grid/data-grid-column-visibility'
import { DataGridPagination } from '@/components/reui/data-grid/data-grid-pagination'
import { Filters, type Filter, type FilterFieldConfig } from '@/components/reui/filters'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { SortableHeader, EmptyIcon } from '@/components/results-table-parts'
import { useGridPreference } from '@/hooks/use-grid-preference'
import { ChevronRight } from '@/components/ui/chevron-right'
import { SquarePenIcon } from '@/components/ui/square-pen'
import { SquareXIcon } from '@/components/animate-ui/icons/square-x'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'

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
  users,
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
  users: User[]
}) {
  const [domains, setDomains] = useState<string[]>([])
  const [domainQuery, setDomainQuery] = useState('')
  const [existingCaseId, setExistingCaseId] = useState<number | ''>('')
  const [status, setStatus] = useState('requested')
  const [agencyId, setAgencyId] = useState<number | ''>('')
  const [dueDurationMinutes, setDueDurationMinutes] = useState('')
  // External ref is shared — one citable reference covers both letters of
  // a pair. Internal ref is per-letter (Notice and Memo get their own "No.
  // Rujukan NMSMD"), same as Subject below.
  const [referenceNumberExternal, setReferenceNumberExternal] = useState('')
  const [recipient, setRecipient] = useState('')
  const { me } = useAuth()
  const [oicUserId, setOicUserId] = useState<number | ''>('')
  // Two type-segmented subject/internal-ref fields, one per letter this
  // dialog always records (Notice + Memo, matching CaseLetter's real grain
  // — see db.CaseLetter's comment on a block getting up to 4 rows, 2 per
  // action). The "Copy" buttons below let the user autofill an empty
  // subject from the other section instead of retyping it.
  const [noticeSubject, setNoticeSubject] = useState('')
  const [noticeReferenceNumberInternal, setNoticeReferenceNumberInternal] = useState('')
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

  const reset = () => {
    setDomains([]); setDomainQuery(''); setExistingCaseId('')
    setStatus('requested'); setAgencyId(''); setDueDurationMinutes('')
    setReferenceNumberExternal('')
    setRecipient(''); setNoticeSubject(''); setNoticeReferenceNumberInternal('')
    setMemoSubject(''); setMemoReferenceNumberInternal('')
    setRequestor(''); setWorkflowStatus('')
    setLetterDate(''); setReceivedAt(''); setSubmittedAt(''); setRemarks(''); setError(null)
  }

  const copySubjectFromMemo = () => setNoticeSubject(memoSubject)
  const copySubjectFromNotice = () => setMemoSubject(noticeSubject)

  useEffect(() => {
    if (open) setOicUserId(me?.id ?? '')
  }, [open, me])

  // The typed-but-not-yet-selected query is offered back as a pickable item
  // itself (labeled "Add …") so this stays create-or-pick like the old
  // textarea — a domain doesn't have to already be on a watchlist.
  const trimmedQuery = domainQuery.trim()
  const domainItems = useMemo(() => {
    if (!trimmedQuery || domainOptions.includes(trimmedQuery) || domains.includes(trimmedQuery)) return domainOptions
    return [...domainOptions, trimmedQuery]
  }, [domainOptions, trimmedQuery, domains])

  const oicOptions = useMemo(
    () => users.filter(u => u.department_id === me?.department_id),
    [users, me],
  )

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (existingCaseId === '' && domains.length === 0) {
      setError('Pick a domain (to open a new case) or an existing case')
      return
    }
    const trimmedNotice = noticeSubject.trim()
    const trimmedMemo = memoSubject.trim()
    if (!trimmedNotice && !trimmedMemo) {
      setError('Enter a subject for the Notice or the Memo')
      return
    }
    setLoading(true)
    setError(null)
    try {
      const commonFields = {
        reference_number_external: referenceNumberExternal.trim() || undefined,
        recipient: recipient.trim() || undefined,
        requestor: requestor.trim() || undefined,
        workflow_status: workflowStatus || undefined,
        letter_date: isoFromDateInput(letterDate),
        received_at: isoFromDateInput(receivedAt),
        submitted_at: isoFromDateInput(submittedAt),
        remarks: remarks.trim() || undefined,
        oic_user_id: oicUserId === '' ? undefined : oicUserId,
      }
      // One row each, per CaseLetter's real grain. Subject/internal ref are
      // each section's own — use the "Copy" button beforehand to autofill
      // one from the other instead of relying on an implicit fallback here.
      const letters = [
        { ...commonFields, type: 'Notice', subject: trimmedNotice || undefined, reference_number_internal: noticeReferenceNumberInternal.trim() || undefined },
        { ...commonFields, type: 'Memo', subject: trimmedMemo || undefined, reference_number_internal: memoReferenceNumberInternal.trim() || undefined },
      ]
      if (existingCaseId !== '') {
        // Linking to a case that's already open — no domain/case creation,
        // just record these letters against it.
        await Promise.all(letters.map(l => addCaseLetter(existingCaseId, l)))
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
        const c = await createCase(created[0].url, status, caseOpts)
        await Promise.all(created.slice(1).map(u => addUrlToCase(c.id, u.url, status)))
        await Promise.all(letters.map(l => addCaseLetter(c.id, l)))
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

          <div className="flex items-center gap-2" style={{ margin: '0.5rem 0' }}>
            <span style={{ flex: 1, height: 1, background: 'var(--border)' }} />
            <span className="text-xs text-stone-muted">or</span>
            <span style={{ flex: 1, height: 1, background: 'var(--border)' }} />
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
                <label className="form-label" id="add-doc-status-label">Case Status</label>
                <Select value={status} onValueChange={setStatus} disabled={loading}>
                  <SelectTrigger aria-labelledby="add-doc-status-label" placeholder="—" className="w-full" />
                  <SelectContent>
                    {CASE_STATUS_OPTIONS.map((opt, i) => (
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
            <label className="form-label" htmlFor="add-doc-reference-external">External Ref. (No. Rujukan NMD) — shared</label>
            <input
              id="add-doc-reference-external"
              className="form-input"
              placeholder="e.g. MCMC(S)CMOD/BLK/2026(1-2)"
              value={referenceNumberExternal}
              onChange={e => setReferenceNumberExternal(e.target.value)}
              disabled={loading}
            />
          </div>

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
              <label className="form-label" htmlFor="add-doc-notice-subject">Subject</label>
              <input
                id="add-doc-notice-subject"
                className="form-input"
                value={noticeSubject}
                onChange={e => setNoticeSubject(e.target.value)}
                disabled={loading}
              />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="add-doc-notice-reference-internal">Internal Ref. (No. Rujukan NMSMD)</label>
              <input
                id="add-doc-notice-reference-internal"
                className="form-input"
                value={noticeReferenceNumberInternal}
                onChange={e => setNoticeReferenceNumberInternal(e.target.value)}
                disabled={loading}
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
              <label className="form-label" htmlFor="add-doc-memo-subject">Subject</label>
              <input
                id="add-doc-memo-subject"
                className="form-input"
                value={memoSubject}
                onChange={e => setMemoSubject(e.target.value)}
                disabled={loading}
              />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="add-doc-memo-reference-internal">Internal Ref. (No. Rujukan NMSMD)</label>
              <input
                id="add-doc-memo-reference-internal"
                className="form-input"
                value={memoReferenceNumberInternal}
                onChange={e => setMemoReferenceNumberInternal(e.target.value)}
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
            <label className="form-label" id="add-doc-oic-label">OIC</label>
            <Select
              value={oicUserId === '' ? '' : String(oicUserId)}
              onValueChange={v => setOicUserId(v === '' ? '' : Number(v))}
              disabled={loading}
            >
              <SelectTrigger aria-labelledby="add-doc-oic-label" placeholder="—" className="w-full" />
              <SelectContent>
                <SelectItem index={0} value="">—</SelectItem>
                {oicOptions.map((u, i) => (
                  <SelectItem key={u.id} index={i + 1} value={String(u.id)}>{u.username}</SelectItem>
                ))}
              </SelectContent>
            </Select>
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

// Edits both of a case's letters at once — a case always gets exactly a
// Notice + Memo pair (see AddDocumentDialog's comment on CaseLetter's
// grain), so editing happens at the case level, not per-letter. Mirrors
// AddDocumentDialog's shared/per-type split: reference external, recipient,
// requestor, workflow status, dates and remarks are common fields applied
// to both letters on save; subject and internal ref stay in their own
// Notice/Memo section since those are each letter's own field. Domains/
// agency/status still live on the parent Case, edited from urls.tsx's Cases
// view instead.
function EditDocumentDialog({
  open, onClose, onSaved, editing, recipients, requestors, users,
}: { open: boolean; onClose: () => void; onSaved: () => void; editing: CaseGroupRow | null; recipients: Recipient[]; requestors: Requestor[]; users: User[] }) {
  const notice = editing?.subRows.find(s => s.letter.type === 'Notice')?.letter
  const memo = editing?.subRows.find(s => s.letter.type === 'Memo')?.letter

  const [referenceNumberExternal, setReferenceNumberExternal] = useState('')
  const [recipient, setRecipient] = useState('')
  const [oicUserId, setOicUserId] = useState<number | ''>('')
  const [requestor, setRequestor] = useState('')
  const [workflowStatus, setWorkflowStatus] = useState('')
  const [letterDate, setLetterDate] = useState('')
  const [receivedAt, setReceivedAt] = useState('')
  const [submittedAt, setSubmittedAt] = useState('')
  const [remarks, setRemarks] = useState('')
  const [noticeSubject, setNoticeSubject] = useState('')
  const [noticeReferenceNumberInternal, setNoticeReferenceNumberInternal] = useState('')
  const [memoSubject, setMemoSubject] = useState('')
  const [memoReferenceNumberInternal, setMemoReferenceNumberInternal] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!open || !editing) return
    // Common fields seed from whichever letter has them (Notice preferred)
    // — if the pair had diverged (e.g. via the grid's per-letter status
    // dropdown), saving here re-syncs both to one shared value.
    const source = notice ?? memo
    setReferenceNumberExternal(source?.reference_number_external ?? '')
    setRecipient(source?.recipient ?? '')
    setOicUserId(source?.oic_user_id ?? '')
    setRequestor(source?.requestor ?? '')
    setWorkflowStatus(source?.workflow_status ?? '')
    setLetterDate(source?.letter_date ? source.letter_date.slice(0, 10) : '')
    setReceivedAt(source?.received_at ? source.received_at.slice(0, 10) : '')
    setSubmittedAt(source?.submitted_at ? source.submitted_at.slice(0, 10) : '')
    setRemarks(source?.remarks ?? '')
    setNoticeSubject(notice?.subject ?? '')
    setNoticeReferenceNumberInternal(notice?.reference_number_internal ?? '')
    setMemoSubject(memo?.subject ?? '')
    setMemoReferenceNumberInternal(memo?.reference_number_internal ?? '')
    setError(null)
  }, [open, editing, notice, memo])

  const oicOptions = useMemo(
    () => users.filter(u => u.department_id === editing?.departmentId),
    [users, editing],
  )

  const handleClose = () => { setError(null); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing) return
    setLoading(true)
    setError(null)
    try {
      // Trimmed values are sent unconditionally (not `|| undefined`), and
      // dates via `?? null`, so blanking a field out actually reaches
      // updateCaseLetter's clear semantics instead of being silently
      // omitted — same fix as urls.tsx's case-edit dialog.
      const commonFields = {
        referenceNumberExternal: referenceNumberExternal.trim(),
        recipient: recipient.trim(),
        requestor: requestor.trim(),
        workflowStatus,
        letterDate: isoFromDateInput(letterDate) ?? null,
        receivedAt: isoFromDateInput(receivedAt) ?? null,
        submittedAt: isoFromDateInput(submittedAt) ?? null,
        remarks: remarks.trim(),
        oicUserId: oicUserId === '' ? null : oicUserId,
      }
      const updates: Promise<void>[] = []
      if (notice) {
        updates.push(updateCaseLetter(notice.case_id, notice.id, {
          ...commonFields,
          subject: noticeSubject.trim(),
          referenceNumberInternal: noticeReferenceNumberInternal.trim(),
        }))
      }
      if (memo) {
        updates.push(updateCaseLetter(memo.case_id, memo.id, {
          ...commonFields,
          subject: memoSubject.trim(),
          referenceNumberInternal: memoReferenceNumberInternal.trim(),
        }))
      }
      await Promise.all(updates)
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save document')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 560 }}>
        <DialogHeader>
          <DialogTitle>Edit Documents</DialogTitle>
          <DialogDescription>
            {editing ? `Case #${editing.caseId}${editing.urls.length > 0 ? ` — ${editing.urls.join(', ')}` : ''}` : ''}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="edit-doc-reference-external">External Ref. (No. Rujukan NMD) — shared</label>
            <input id="edit-doc-reference-external" className="form-input" value={referenceNumberExternal} onChange={e => setReferenceNumberExternal(e.target.value)} disabled={loading} autoFocus />
          </div>
          <div className="form-field">
            <label className="form-label" id="edit-doc-recipient-label">Recipient</label>
            <Select value={recipient} onValueChange={setRecipient} disabled={loading}>
              <SelectTrigger aria-labelledby="edit-doc-recipient-label" placeholder="—" className="w-full" />
              <SelectContent>
                <SelectItem index={0} value="">—</SelectItem>
                {recipients.map((r, i) => (
                  <SelectItem key={r.id} index={i + 1} value={r.name}>{r.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="form-field">
            <label className="form-label" id="edit-doc-oic-label">OIC</label>
            <Select
              value={oicUserId === '' ? '' : String(oicUserId)}
              onValueChange={v => setOicUserId(v === '' ? '' : Number(v))}
              disabled={loading}
            >
              <SelectTrigger aria-labelledby="edit-doc-oic-label" placeholder="—" className="w-full" />
              <SelectContent>
                <SelectItem index={0} value="">—</SelectItem>
                {oicOptions.map((u, i) => (
                  <SelectItem key={u.id} index={i + 1} value={String(u.id)}>{u.username}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="form-row">
            <div className="form-field">
              <label className="form-label" id="edit-doc-requestor-label">Requestor</label>
              <Select value={requestor} onValueChange={setRequestor} disabled={loading}>
                <SelectTrigger aria-labelledby="edit-doc-requestor-label" placeholder="—" className="w-full" />
                <SelectContent>
                  <SelectItem index={0} value="">—</SelectItem>
                  {requestors.map((r, i) => (
                    <SelectItem key={r.id} index={i + 1} value={r.name}>{r.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="form-field">
              <label className="form-label" id="edit-doc-workflow-label">Workflow Status — shared</label>
              <Select value={workflowStatus} onValueChange={setWorkflowStatus} disabled={loading}>
                <SelectTrigger aria-labelledby="edit-doc-workflow-label" placeholder="—" className="w-full" />
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
              <label className="form-label" htmlFor="edit-doc-letter-date">Letter Date</label>
              <input id="edit-doc-letter-date" type="date" className="form-input" value={letterDate} onChange={e => setLetterDate(e.target.value)} disabled={loading} />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="edit-doc-received">Received</label>
              <input id="edit-doc-received" type="date" className="form-input" value={receivedAt} onChange={e => setReceivedAt(e.target.value)} disabled={loading} />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="edit-doc-submission">Submission</label>
              <input id="edit-doc-submission" type="date" className="form-input" value={submittedAt} onChange={e => setSubmittedAt(e.target.value)} disabled={loading} />
            </div>
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="edit-doc-remarks">Remarks</label>
            <textarea
              id="edit-doc-remarks"
              className="form-input"
              rows={2}
              value={remarks}
              onChange={e => setRemarks(e.target.value)}
              disabled={loading}
              style={{ resize: 'vertical', fontFamily: 'inherit' }}
            />
          </div>

          <div style={{ border: '1px solid var(--border)', borderRadius: 8, padding: '0.75rem', marginBottom: '0.75rem' }}>
            <span className="form-label" style={{ margin: 0, display: 'block', marginBottom: '0.5rem' }}>Notice</span>
            <div className="form-field">
              <label className="form-label" htmlFor="edit-doc-notice-subject">Subject</label>
              <input id="edit-doc-notice-subject" className="form-input" value={noticeSubject} onChange={e => setNoticeSubject(e.target.value)} disabled={loading || !notice} />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="edit-doc-notice-reference-internal">Internal Ref. (No. Rujukan NMSMD)</label>
              <input id="edit-doc-notice-reference-internal" className="form-input" value={noticeReferenceNumberInternal} onChange={e => setNoticeReferenceNumberInternal(e.target.value)} disabled={loading || !notice} />
            </div>
          </div>

          <div style={{ border: '1px solid var(--border)', borderRadius: 8, padding: '0.75rem', marginBottom: '0.75rem' }}>
            <span className="form-label" style={{ margin: 0, display: 'block', marginBottom: '0.5rem' }}>Memo</span>
            <div className="form-field">
              <label className="form-label" htmlFor="edit-doc-memo-subject">Subject</label>
              <input id="edit-doc-memo-subject" className="form-input" value={memoSubject} onChange={e => setMemoSubject(e.target.value)} disabled={loading || !memo} />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="edit-doc-memo-reference-internal">Internal Ref. (No. Rujukan NMSMD)</label>
              <input id="edit-doc-memo-reference-internal" className="form-input" value={memoReferenceNumberInternal} onChange={e => setMemoReferenceNumberInternal(e.target.value)} disabled={loading || !memo} />
            </div>
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Saving…' : 'Save Changes'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

type LetterSubRow = { kind: 'letter'; letter: CaseLetterEntry }
type CaseGroupRow = { kind: 'case'; caseId: number; departmentId: number; departmentName: string; urls: string[]; subRows: LetterSubRow[] }
type DocTreeRow = CaseGroupRow | LetterSubRow

function DocsPage() {
  const [letters, setLetters] = useState<CaseLetterEntry[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [domainOptions, setDomainOptions] = useState<string[]>([])
  const [recipients, setRecipients] = useState<Recipient[]>([])
  const [requestors, setRequestors] = useState<Requestor[]>([])
  const [agencies, setAgencies] = useState<Agency[]>([])
  const [duePresets, setDuePresets] = useState<DueDatePreset[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<CaseGroupRow | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<CaseLetterEntry | null>(null)
  const [exportScope, setExportScope] = useState<'current' | 'all'>('current')
  const [exporting, setExporting] = useState(false)

  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<Filter<string>[]>([])
  const [sorting, setSorting] = useState<SortingState>([])
  const [expanded, setExpanded] = useState<ExpandedState>({})
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
      const [l, d, u, rc, rq, ag, dp, us] = await Promise.all([
        fetchAllCaseLetters(), fetchDepartmentsOpen(), fetchUrls(), fetchRecipients(), fetchRequestors(),
        fetchAgencies(), fetchDueDatePresets(), fetchUsersOpen(),
      ])
      setLetters(l)
      setDepartments(d)
      setDomainOptions(u.map(entry => entry.url))
      setRecipients(rc)
      setRequestors(rq)
      setAgencies(ag)
      setDuePresets(dp)
      setUsers(us)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load documents')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleDeleteDocument = async () => {
    if (!deleteTarget) return
    await deleteCaseLetter(deleteTarget.case_id, deleteTarget.id)
    setDeleteTarget(null)
    load()
  }

  // Inline status edit straight from the grid, same optimistic-update/
  // rollback-on-failure shape as urls.tsx's handleCaseStatusChange —
  // avoids a full reload for a single-field change.
  const handleWorkflowStatusChange = useCallback(async (letter: CaseLetterEntry, status: string) => {
    const prev = letter.workflow_status
    setLetters(prevList => prevList.map(l => l.id === letter.id ? { ...l, workflow_status: status } : l))
    try {
      await updateCaseLetter(letter.case_id, letter.id, { workflowStatus: status })
    } catch {
      setLetters(prevList => prevList.map(l => l.id === letter.id ? { ...l, workflow_status: prev } : l))
    }
  }, [])

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

  const handleExportLetters = useCallback(async () => {
    setExporting(true)
    setError(null)
    try {
      const ids = exportScope === 'current' ? filtered.map(l => l.id) : undefined
      const { blob, filename } = await exportCaseLetters(ids)
      downloadBlob(blob, filename ?? `cmod-blocking-export-${new Date().toISOString().slice(0, 10)}.xlsx`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to export documents')
    } finally {
      setExporting(false)
    }
  }, [exportScope, filtered])

  // Groups the flat, already-filtered letter list into one row per case
  // (department/domains, shared across a case's letters) with its Notice/
  // Memo letters as expandable subrows — same tree-grid shape as urls.tsx's
  // Cases view. Preserves `filtered`'s order (backend returns newest
  // letter_date first) rather than re-sorting by case.
  const caseTreeData = useMemo<CaseGroupRow[]>(() => {
    const byId = new Map<number, CaseGroupRow>()
    const order: number[] = []
    for (const l of filtered) {
      let group = byId.get(l.case_id)
      if (!group) {
        group = { kind: 'case', caseId: l.case_id, departmentId: l.department_id, departmentName: l.department_name, urls: l.urls ?? [], subRows: [] }
        byId.set(l.case_id, group)
        order.push(l.case_id)
      }
      group.subRows.push({ kind: 'letter', letter: l })
    }
    return order.map(id => byId.get(id)!)
  }, [filtered])

  const columns = useMemo<ColumnDef<DocTreeRow>[]>(() => [
    {
      id: 'case',
      accessorFn: r => r.kind === 'case' ? r.caseId : r.letter.type,
      header: ({ column }) => <SortableHeader column={column} title="Case # / Type" />,
      enableHiding: false,
      size: 160,
      meta: { headerTitle: 'Case # / Type', headerClassName: 'col-domain th-left', cellClassName: 'col-domain', skeleton: <span className="skeleton" style={{ width: 90, height: 14 }} /> },
      cell: ({ row }) => {
        const original = row.original
        const expandControl = (
          <DataGridTableRowExpand row={row}>
            <ChevronRight className={`expand-icon${row.getIsExpanded() ? ' expanded' : ''}`} />
          </DataGridTableRowExpand>
        )
        if (original.kind === 'letter') {
          return <span className="flex items-center gap-[2px]">{expandControl}<span className="dns-name">{original.letter.type}</span></span>
        }
        return <span className="flex items-center gap-[2px]">{expandControl}<span className="hostname">#{original.caseId}</span></span>
      },
    },
    {
      id: 'letter_date',
      accessorFn: r => r.kind === 'letter' ? (r.letter.letter_date ?? '') : '',
      header: ({ column }) => <SortableHeader column={column} title="Letter Date" />,
      meta: { headerTitle: 'Letter Date', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'letter' ? formatDate(row.original.letter.letter_date) : null,
    },
    {
      id: 'reference_number_external',
      accessorFn: r => r.kind === 'letter' ? (r.letter.reference_number_external ?? '') : '',
      header: 'External Ref.',
      meta: { headerTitle: 'External Ref. (No. Rujukan NMD)', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'letter' ? (row.original.letter.reference_number_external || '—') : null,
    },
    {
      id: 'reference_number_internal',
      accessorFn: r => r.kind === 'letter' ? (r.letter.reference_number_internal ?? '') : '',
      header: 'Internal Ref.',
      meta: { headerTitle: 'Internal Ref. (No. Rujukan NMSMD)', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'letter' ? (row.original.letter.reference_number_internal || '—') : null,
    },
    {
      id: 'recipient',
      accessorFn: r => r.kind === 'letter' ? (r.letter.recipient ?? '') : '',
      header: 'Recipient',
      meta: { headerTitle: 'Recipient', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'letter' ? (row.original.letter.recipient || '—') : null,
    },
    {
      id: 'subject',
      accessorFn: r => r.kind === 'letter' ? (r.letter.subject ?? '') : '',
      header: 'Subject',
      meta: { headerTitle: 'Subject', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'letter' ? (row.original.letter.subject || '—') : null,
    },
    {
      id: 'requestor',
      accessorFn: r => r.kind === 'letter' ? (r.letter.requestor ?? '') : '',
      header: 'Requestor',
      meta: { headerTitle: 'Requestor', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'letter' ? (row.original.letter.requestor || '—') : null,
    },
    {
      id: 'workflow_status',
      accessorFn: r => r.kind === 'letter' ? (r.letter.workflow_status ?? '') : '',
      header: 'Status',
      meta: { headerTitle: 'Status', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind !== 'letter') return null
        const l = original.letter
        return (
          <Select value={l.workflow_status ?? ''} onValueChange={v => handleWorkflowStatusChange(l, v)}>
            <SelectTrigger aria-label={`Status for ${l.type} on case #${l.case_id}`} placeholder="—" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">—</SelectItem>
              {WORKFLOW_STATUS_OPTIONS.map((opt, i) => (
                <SelectItem key={opt} index={i + 1} value={opt}>{opt}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )
      },
    },
    {
      id: 'received_at',
      accessorFn: r => r.kind === 'letter' ? (r.letter.received_at ?? '') : '',
      header: ({ column }) => <SortableHeader column={column} title="Received" />,
      meta: { headerTitle: 'Received', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'letter' ? formatDate(row.original.letter.received_at) : null,
    },
    {
      id: 'submitted_at',
      accessorFn: r => r.kind === 'letter' ? (r.letter.submitted_at ?? '') : '',
      header: ({ column }) => <SortableHeader column={column} title="Submission" />,
      meta: { headerTitle: 'Submission', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'letter' ? formatDate(row.original.letter.submitted_at) : null,
    },
    {
      id: 'department_name',
      accessorFn: r => r.kind === 'case' ? r.departmentName : '',
      header: 'Dept.',
      meta: { headerTitle: 'Dept.', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? row.original.departmentName : null,
    },
    {
      id: 'urls',
      accessorFn: r => r.kind === 'case' ? r.urls.join(', ') : '',
      header: 'Link',
      meta: { headerTitle: 'Link', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => {
        if (row.original.kind !== 'case') return null
        const urls = row.original.urls
        if (urls.length === 0) return '—'
        if (urls.length === 1) return urls[0]
        return `${urls[0]} +${urls.length - 1} more`
      },
    },
    {
      id: 'documents',
      accessorFn: r => r.kind === 'case' ? r.subRows.length : '',
      header: 'Documents',
      meta: { headerTitle: 'Documents', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'case' ? row.original.subRows.length : null,
    },
    {
      id: 'remarks',
      accessorFn: r => r.kind === 'letter' ? (r.letter.remarks ?? '') : '',
      header: 'Remarks',
      meta: { headerTitle: 'Remarks', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => row.original.kind === 'letter' ? (row.original.letter.remarks || '—') : null,
    },
    {
      id: 'action',
      header: 'Action',
      enableHiding: false,
      size: 90,
      meta: { headerClassName: 'th-center', cellClassName: 'text-center' },
      cell: ({ row }) => {
        const original = row.original
        if (original.kind === 'case') {
          return (
            <div className="flex items-center justify-center gap-1">
              <button type="button" className="screenshot-icon-btn" onClick={() => setEditTarget(original)} aria-label={`Edit documents for case #${original.caseId}`} title="Edit">
                <SquarePenIcon size={16} />
              </button>
            </div>
          )
        }
        const l = original.letter
        return (
          <div className="flex items-center justify-center gap-1">
            <button type="button" className="screenshot-icon-btn" onClick={() => setDeleteTarget(l)} aria-label={`Delete ${l.type} for case #${l.case_id}`} title="Delete">
              <SquareXIcon size={16} animateOnHover animation="path-loop" />
            </button>
          </div>
        )
      },
    },
  ], [handleWorkflowStatusChange])

  const table = useReactTable({
    data: caseTreeData,
    columns,
    initialState: { columnPinning: { left: ['case'], right: ['action'] } },
    state: { sorting, pagination, columnVisibility, expanded },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    onColumnVisibilityChange: setColumnVisibility,
    onExpandedChange: setExpanded,
    getRowId: r => r.kind === 'case' ? `case:${r.caseId}` : `letter:${r.letter.id}`,
    getSubRows: r => r.kind === 'case' ? r.subRows : undefined,
    getRowCanExpand: row => row.original.kind === 'case' && row.original.subRows.length > 0,
    paginateExpandedRows: false,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
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
            <Select value={exportScope} onValueChange={v => setExportScope(v as 'current' | 'all')}>
              <SelectTrigger aria-label="Export scope" className="w-40" />
              <SelectContent>
                <SelectItem index={0} value="current">Current view</SelectItem>
                <SelectItem index={1} value="all">All cases</SelectItem>
              </SelectContent>
            </Select>
            <Button variant="outline" onClick={handleExportLetters} disabled={exporting}>
              {exporting ? 'Exporting…' : 'Export'}
            </Button>
            <div style={{ marginLeft: 'auto' }}>
              <DataGridColumnVisibility table={table} trigger={<Button variant="outline">Columns</Button>} />
            </div>
          </div>

          <div className="results-wrap w-full">
            {!gridLoading && caseTreeData.length === 0 ? (
              <div className="empty-state" style={{ padding: '3rem 0' }}>
                <p className="empty-heading">No documents match the current filters</p>
              </div>
            ) : (
              <DataGrid
                table={table}
                recordCount={caseTreeData.length}
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
        users={users}
      />

      <EditDocumentDialog
        open={editTarget !== null}
        onClose={() => setEditTarget(null)}
        onSaved={load}
        editing={editTarget}
        recipients={recipients}
        requestors={requestors}
        users={users}
      />

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget ? `${deleteTarget.type} — Case #${deleteTarget.case_id}` : ''}
        onConfirm={handleDeleteDocument}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  )
}
