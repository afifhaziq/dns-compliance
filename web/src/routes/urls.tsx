import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
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
import { GripIcon } from '@/components/ui/grip'
import { fetchUrls, createUrl, deleteUrl, setUrlEnabled, setUrlFields } from '../api/urls'
import { fetchAgencies } from '../api/agencies'
import { fetchDepartmentsOpen } from '../api/departments'
import type { URLEntry, Agency, Department, Instrument, Citation, LegalCategory, LegalElement, URLOffence } from '../api/types'
import { fetchInstruments, fetchCitations, fetchCategories, fetchElements, attachOffence, fetchOffencesByUrl, detachOffence, formatParsedCitation } from '../api/legal'
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
import { Filters, type Filter, type FilterFieldConfig } from '@/components/reui/filters'
import { SortableHeader, EmptyIcon } from '@/components/results-table-parts'
import { XIcon } from '@/components/ui/x'
import { FaviconSearch } from '@/components/unlumen-ui/favicon-search'
import { faviconApiUrl } from '../api/domain'
import {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardImage,
} from '@/components/animate-ui/components/base/preview-link-card'
import { useAuth } from './__root'

/* ─── Quick Add (single domain, favicon preview) ─────────────────────────── */

function QuickAddFavicon({ onAdded }: { onAdded: () => void }) {
  const [key, setKey] = useState(0) // bumped to reset FaviconSearch's internal input after a successful add
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const handleSearch = async (value: string) => {
    setLoading(true)
    setError(null)
    try {
      await createUrl(value)
      onAdded()
      setKey(k => k + 1)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add domain')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex items-center gap-2">
      <FaviconSearch
        key={key}
        placeholder="Quick add a domain…"
        className="w-72"
        onSearch={value => handleSearch(value)}
      />
      {loading && <span className="text-xs text-stone-muted">Adding…</span>}
      {error && <span className="form-error">{error}</span>}
    </div>
  )
}

export const Route = createFileRoute('/urls')({ component: URLsPage })

/* ─── Case metadata (shared by the Add Domain form + inline table cells) ─── */

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '—' },
  { value: 'requested', label: 'Requested' },
  { value: 'uplift', label: 'Uplift' },
  { value: 'suspended', label: 'Suspended' },
]

// due_date is the ISP's block deadline (some takedown orders require
// blocking within 6h/24h), but rather than picking a calendar date+time by
// hand, the case owner picks how long from now the ISP has — the computed
// deadline (now + duration) is what actually gets stored in due_date, same
// field ISPTiming already measures against.
const DUE_DATE_DURATION_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '—' },
  { value: '6', label: '6 hours' },
  { value: '24', label: '24 hours' },
  { value: '48', label: '48 hours' },
  { value: '72', label: '72 hours' },
  { value: '168', label: '7 days' },
]

function dueDateFromDurationHours(hours: number): string {
  return new Date(Date.now() + hours * 60 * 60 * 1000).toISOString()
}

/* ─── Add Domain Dialog ──────────────────────────────────────────────────── */

export type StagedOffence = {
  instrumentId: number
  citationId: number
  categoryId: number
  elementId?: number
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

  const [instrumentId, setInstrumentId] = useState<number | ''>('')
  const [citationId, setCitationId] = useState<number | ''>('')
  const [categoryId, setCategoryId] = useState<number | ''>('')
  const [elementId, setElementId] = useState<number | ''>('')

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

  const resetStaging = () => {
    setInstrumentId(''); setCitationId(''); setCategoryId(''); setElementId('')
  }

  const computePending = (): StagedOffence | null => {
    if (instrumentId === '' || citationId === '' || categoryId === '') return null
    const citation = citations.find(c => c.id === citationId)
    const category = categories.find(c => c.id === categoryId)
    const element = elementId === '' ? undefined : elements.find(e => e.id === elementId)
    if (!citation || !category) return null
    const label = `${formatParsedCitation(citation.parsed)} — ${category.name}${element ? ` (${element.name})` : ''}`
    return { instrumentId, citationId, categoryId, elementId: elementId === '' ? undefined : elementId, label }
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
          onValueChange={v => { setInstrumentId(v === '' ? '' : Number(v)); setCitationId(''); setCategoryId(''); setElementId('') }}
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
            onValueChange={v => { setCitationId(v === '' ? '' : Number(v)); setCategoryId(''); setElementId('') }}
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
            onValueChange={v => { setCategoryId(v === '' ? '' : Number(v)); setElementId('') }}
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
            onValueChange={v => setElementId(v === '' ? '' : Number(v))}
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
        {categoryId !== '' && (
          <button type="button" className="btn-ghost" onClick={handleAdd} disabled={disabled}>
            + Add offence
          </button>
        )}
      </div>
    </div>
  )
})

function AddUrlDialog({
  open,
  onClose,
  onAdded,
  agencies,
  departments,
  defaultDepartmentId,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  agencies: Agency[]
  departments: Department[]
  defaultDepartmentId: number | null
}) {
  const [value, setValue] = useState('')
  const [offences, setOffences] = useState<StagedOffence[]>([])
  const [agencyId, setAgencyId] = useState<number | ''>('')
  const [referenceNumber, setReferenceNumber] = useState('')
  const [requestingDeptId, setRequestingDeptId] = useState<number | ''>('')
  const [status, setStatus] = useState('')
  const [dueDurationHours, setDueDurationHours] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const pickerRef = useRef<MultiOffencePickerHandle>(null)

  // Requesting Dept defaults to the current user's own department, but stays
  // changeable — reset it whenever the dialog reopens (a stale value from a
  // previous open shouldn't linger) or once the department list arrives.
  useEffect(() => {
    if (open) setRequestingDeptId(defaultDepartmentId ?? '')
  }, [open, defaultDepartmentId])

  const reset = () => {
    setValue(''); setOffences([]); setError(null)
    setAgencyId(''); setReferenceNumber(''); setRequestingDeptId(defaultDepartmentId ?? '')
    setStatus(''); setDueDurationHours('')
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const domains = value.split('\n').map(s => s.trim()).filter(Boolean)
    if (domains.length === 0) { setError('At least one domain is required'); return }
    // Catch a filled-in-but-not-yet-"+ Add offence"-clicked selection sitting
    // in the picker — otherwise it's silently dropped rather than attached.
    const pending = pickerRef.current?.flush()
    const allOffences = pending ? [...offences, pending] : offences

    const caseFields: Parameters<typeof setUrlFields>[1] = {}
    if (agencyId !== '') caseFields.agency_id = agencyId
    if (referenceNumber.trim()) caseFields.reference_number = referenceNumber.trim()
    if (requestingDeptId !== '') caseFields.requesting_dept_id = requestingDeptId
    if (status) caseFields.status = status
    if (dueDurationHours) caseFields.due_date = dueDateFromDurationHours(Number(dueDurationHours))
    const hasCaseFields = Object.keys(caseFields).length > 0

    setLoading(true)
    setError(null)
    try {
      const created = await Promise.all(domains.map(d => createUrl(d)))
      await Promise.all([
        ...created.flatMap(u => allOffences.map(o => attachOffence(u.url, o.categoryId, o.elementId))),
        ...(hasCaseFields ? created.map(u => setUrlFields(u.id, caseFields)) : []),
      ])
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add domain')
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 560 }}>
        <DialogHeader>
          <DialogTitle>Add Domain</DialogTitle>
          <DialogDescription>
            Enter one or more domains or full URLs to monitor for DNS compliance. Full URLs will have their domain automatically extracted. You can add multiple entries at once, just put each one on a new line. Case details below (if any) apply to every domain added.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
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
              <label className="form-label" id="add-requesting-dept-label">Requesting Dept.</label>
              <Select value={String(requestingDeptId)} onValueChange={v => setRequestingDeptId(v === '' ? '' : Number(v))} disabled={loading}>
                <SelectTrigger aria-labelledby="add-requesting-dept-label" placeholder="—" className="w-full" />
                <SelectContent>
                  <SelectItem index={0} value="">—</SelectItem>
                  {departments.map((d, i) => (
                    <SelectItem key={d.id} index={i + 1} value={String(d.id)}>{d.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="form-row">
            <div className="form-field" style={{ flex: '0 1 35%' }}>
              <label className="form-label" id="add-status-label">Status</label>
              <Select value={status} onValueChange={setStatus} disabled={loading}>
                <SelectTrigger aria-labelledby="add-status-label" placeholder="—" className="w-full" />
                <SelectContent>
                  {STATUS_OPTIONS.map((opt, i) => (
                    <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="form-field">
              <label className="form-label" id="add-due-date-label">Time to Block</label>
              <Select value={dueDurationHours} onValueChange={setDueDurationHours} disabled={loading}>
                <SelectTrigger aria-labelledby="add-due-date-label" placeholder="—" className="w-full" />
                <SelectContent>
                  {DUE_DATE_DURATION_OPTIONS.map((opt, i) => (
                    <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="form-field">
            <label className="form-label" htmlFor="add-reference-number">Reference No.</label>
            <input
              id="add-reference-number"
              type="text"
              className="form-input form-input-strong"
              maxLength={255}
              value={referenceNumber}
              onChange={e => setReferenceNumber(e.target.value)}
              disabled={loading}
            />
          </div>

          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add Domain'}
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
  agencies,
  departments,
  onAgencyChange,
  onRequestingDeptChange,
  onStatusChange,
  onDueDurationChange,
  onRefFocus,
  onRefChange,
  onRefBlur,
}: {
  entry: URLEntry | null
  open: boolean
  onClose: () => void
  agencies: Agency[]
  departments: Department[]
  onAgencyChange: (id: number, agencyId: number | null) => void
  onRequestingDeptChange: (id: number, deptId: number | null) => void
  onStatusChange: (id: number, status: string) => void
  onDueDurationChange: (id: number, durationHours: string) => void
  onRefFocus: (id: number, value: string) => void
  onRefChange: (id: number, value: string) => void
  onRefBlur: (id: number) => void
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
      await attachOffence(url, added.categoryId, added.elementId)
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
        await attachOffence(url, pending.categoryId, pending.elementId)
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
          <DialogTitle>Edit Domain</DialogTitle>
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
                  <span>{formatParsedCitation(o.category.citation.parsed)} — {o.category.name}{o.element ? ` (${o.element.name})` : ''}</span>
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

        {entry && (
          <>
            <div className="form-row">
              <div className="form-field">
                <label className="form-label" id="edit-agency-label">Agency</label>
                <Select
                  value={String(entry.agency_id ?? '')}
                  onValueChange={v => onAgencyChange(entry.id, v === '' ? null : Number(v))}
                >
                  <SelectTrigger aria-labelledby="edit-agency-label" placeholder="—" className="w-full" />
                  <SelectContent>
                    <SelectItem index={0} value="">—</SelectItem>
                    {agencies.map((a, i) => (
                      <SelectItem key={a.id} index={i + 1} value={String(a.id)}>{a.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="form-field">
                <label className="form-label" id="edit-requesting-dept-label">Requesting Dept.</label>
                <Select
                  value={String(entry.requesting_dept_id ?? '')}
                  onValueChange={v => onRequestingDeptChange(entry.id, v === '' ? null : Number(v))}
                >
                  <SelectTrigger aria-labelledby="edit-requesting-dept-label" placeholder="—" className="w-full" />
                  <SelectContent>
                    <SelectItem index={0} value="">—</SelectItem>
                    {departments.map((d, i) => (
                      <SelectItem key={d.id} index={i + 1} value={String(d.id)}>{d.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="form-row">
              <div className="form-field" style={{ flex: '0 1 35%' }}>
                <label className="form-label" id="edit-status-label">Status</label>
                <Select value={entry.status ?? ''} onValueChange={v => onStatusChange(entry.id, v)}>
                  <SelectTrigger aria-labelledby="edit-status-label" placeholder="—" className="w-full" />
                  <SelectContent>
                    {STATUS_OPTIONS.map((opt, i) => (
                      <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="form-field">
                <label className="form-label" id="edit-due-date-label">Time to Block</label>
                <Select value="" onValueChange={v => onDueDurationChange(entry.id, v)}>
                  <SelectTrigger aria-labelledby="edit-due-date-label" placeholder="—" className="w-full" />
                  <SelectContent>
                    {DUE_DATE_DURATION_OPTIONS.map((opt, i) => (
                      <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {entry.due_date && (
                  <p className="text-xs text-stone-muted">Deadline: {DUE_DATE_FMT.format(new Date(entry.due_date))}</p>
                )}
              </div>
            </div>

            <div className="form-field">
              <label className="form-label" htmlFor="edit-reference-number">Reference No.</label>
              <input
                id="edit-reference-number"
                type="text"
                className="form-input form-input-strong"
                maxLength={255}
                value={entry.reference_number ?? ''}
                onFocus={e => onRefFocus(entry.id, e.target.value)}
                onChange={e => onRefChange(entry.id, e.target.value)}
                onBlur={() => onRefBlur(entry.id)}
              />
            </div>
          </>
        )}

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

const DUE_DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit',
})

const PAGE_SIZE = 25

const IS_ONLY = [{ value: 'is', label: 'is' }]

function URLsPage() {
  const { me } = useAuth()
  const [urls, setUrls] = useState<URLEntry[]>([])
  const [agencies, setAgencies] = useState<Agency[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<URLEntry | null>(null)
  // id, not a URLEntry snapshot, so the dialog re-reads the live row out of
  // `urls` below and reflects its own edits (agency/status/etc.) immediately.
  const [editTargetId, setEditTargetId] = useState<number | null>(null)
  const editTarget = urls.find(u => u.id === editTargetId) ?? null

  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<Filter<string>[]>([])
  const [sorting, setSorting] = useState<SortingState>([])
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: PAGE_SIZE })
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})

  // Snapshots the reference-number field's pre-edit value on focus so a
  // failed blur-commit can roll back to it.
  const refOriginalRef = useRef<Record<number, string>>({})

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      const [u, a, d] = await Promise.all([fetchUrls(), fetchAgencies(), fetchDepartmentsOpen()])
      setUrls(u)
      setAgencies(a)
      setDepartments(d)
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

  // Generic case-field commit: optimistic local update, roll back to the
  // previous URLEntry snapshot on failure. Shared by every select-style
  // field (agency, requesting dept, status) and the date-picker/time pair.
  const commitField = useCallback(async (id: number, patch: Partial<URLEntry>, body: Parameters<typeof setUrlFields>[1]) => {
    const previous = urls.find(u => u.id === id)
    setUrls(prev => prev.map(u => u.id === id ? { ...u, ...patch } : u))
    try {
      await setUrlFields(id, body)
    } catch {
      if (previous) setUrls(prev => prev.map(u => u.id === id ? previous : u))
    }
  }, [urls])

  const handleAgencyChange = useCallback((id: number, agencyId: number | null) => {
    const agency = agencies.find(a => a.id === agencyId)
    commitField(id, { agency_id: agencyId ?? undefined, agency_name: agency?.name }, { agency_id: agencyId })
  }, [agencies, commitField])

  const handleRequestingDeptChange = useCallback((id: number, deptId: number | null) => {
    const dept = departments.find(d => d.id === deptId)
    commitField(id, { requesting_dept_id: deptId ?? undefined, requesting_dept_name: dept?.name }, { requesting_dept_id: deptId })
  }, [departments, commitField])

  const handleStatusChange = useCallback((id: number, status: string) => {
    commitField(id, { status }, { status })
  }, [commitField])

  const handleDueDurationChange = useCallback((id: number, durationHours: string) => {
    if (!durationHours) {
      commitField(id, { due_date: undefined }, { due_date: null })
      return
    }
    const combined = dueDateFromDurationHours(Number(durationHours))
    commitField(id, { due_date: combined }, { due_date: combined })
  }, [commitField])

  // Reference number commits on blur (not per keystroke) to avoid a PATCH
  // per character — refOriginalRef snapshots the pre-edit value on focus so
  // a failed commit can roll back to it.
  const handleRefFocus = useCallback((id: number, value: string) => {
    refOriginalRef.current[id] = value
  }, [])

  const handleRefChange = useCallback((id: number, value: string) => {
    setUrls(prev => prev.map(u => u.id === id ? { ...u, reference_number: value } : u))
  }, [])

  const handleRefBlur = useCallback(async (id: number) => {
    const original = refOriginalRef.current[id] ?? ''
    const current = urls.find(u => u.id === id)?.reference_number ?? ''
    if (current === original) return
    try {
      await setUrlFields(id, { reference_number: current })
    } catch {
      setUrls(prev => prev.map(u => u.id === id ? { ...u, reference_number: original } : u))
    }
  }, [urls])

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
  ], [agencies, departments])

  const statusFilter = filters.find(f => f.field === 'status')?.values[0]
  const deptFilter = filters.find(f => f.field === 'requesting_dept')?.values[0]
  const agencyFilter = filters.find(f => f.field === 'agency')?.values[0]

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    return urls.filter(u =>
      (!query || u.url.toLowerCase().includes(query)) &&
      (!statusFilter || u.status === statusFilter) &&
      (!deptFilter || String(u.requesting_dept_id ?? '') === deptFilter) &&
      (!agencyFilter || String(u.agency_id ?? '') === agencyFilter)
    )
  }, [urls, search, statusFilter, deptFilter, agencyFilter])

  useEffect(() => { setPagination(p => ({ ...p, pageIndex: 0 })) }, [search, statusFilter, deptFilter, agencyFilter])

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
      id: 'reference_number',
      accessorFn: u => u.reference_number ?? '',
      size: 130,
      header: 'Reference No.',
      meta: { headerTitle: 'Reference No.', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{row.original.reference_number || '—'}</span>,
    },
    {
      id: 'requesting_dept',
      accessorFn: u => u.requesting_dept_id ?? '',
      size: 140,
      header: 'Requesting Dept.',
      meta: { headerTitle: 'Requesting Dept.', headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{row.original.requesting_dept_name ?? '—'}</span>,
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
      cell: ({ row }) => <span className="dns-name">{STATUS_OPTIONS.find(o => o.value === row.original.status)?.label ?? '—'}</span>,
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
      id: 'enabled',
      header: 'Scan',
      enableHiding: false,
      size: 70,
      meta: { headerClassName: 'th-center', cellClassName: 'text-center' },
      cell: ({ row }) => {
        const u = row.original
        return (
          <Switch
            checked={u.enabled}
            onCheckedChange={checked => handleToggle(u.id, checked)}
            aria-label={`${u.enabled ? 'Disable' : 'Enable'} ${u.url} in scan`}
          />
        )
      },
    },
    {
      id: 'actions',
      header: '',
      enableHiding: false,
      size: 90,
      meta: { headerClassName: 'col-evidence', cellClassName: 'col-evidence text-right' },
      cell: ({ row }) => {
        const u = row.original
        return (
          <div className="flex items-center justify-end gap-1">
            <button
              type="button"
              className="screenshot-icon-btn"
              onClick={() => setEditTargetId(u.id)}
              aria-label={`Edit ${u.url}`}
              title="Edit"
            >
              <GripIcon size={16} />
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
        )
      },
    },
  ], [handleToggle])

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
  })

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title mb-4">Domains</h1>
        <p className="page-subtitle">{!loading && `${urls.length} monitored`}</p>
        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          <QuickAddFavicon onAdded={load} />
          <Button onClick={() => setAddOpen(true)}>
            + Add Domain
          </Button>
        </div>
      </div>

      {error ? (
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button className="btn-primary" onClick={load}>Retry</button>
        </div>
      ) : !loading && urls.length === 0 ? (
        <div className="empty-state">
          <EmptyIcon />
          <p className="empty-heading">No domains yet</p>
          <p className="empty-body">Add a domain to start monitoring DNS compliance.</p>
          <button className="btn-primary" onClick={() => setAddOpen(true)}>Add Domain</button>
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
            {!loading && filtered.length === 0 ? (
              <div className="empty-state" style={{ padding: '3rem 0' }}>
                <p className="empty-heading">No domains match the current filters</p>
              </div>
            ) : (
              <DataGrid table={table} recordCount={filtered.length} isLoading={loading} tableClassNames={{ base: 'results-table' }}>
                <DataGridContainer className="overflow-visible">
                  <DataGridTable />
                </DataGridContainer>
                <DataGridPagination sizes={[10, 25, 50, 100]} />
              </DataGrid>
            )}
          </div>
        </div>
      )}

      <AddUrlDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdded={load}
        agencies={agencies}
        departments={departments}
        defaultDepartmentId={me?.department_id ?? null}
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
        agencies={agencies}
        departments={departments}
        onAgencyChange={handleAgencyChange}
        onRequestingDeptChange={handleRequestingDeptChange}
        onStatusChange={handleStatusChange}
        onDueDurationChange={handleDueDurationChange}
        onRefFocus={handleRefFocus}
        onRefChange={handleRefChange}
        onRefBlur={handleRefBlur}
      />
    </div>
  )
}
