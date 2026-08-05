import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { GripIcon } from '@/components/ui/grip'
import { fetchUrls, createUrl, deleteUrl, setUrlEnabled, setUrlFields } from '../api/urls'
import type { URLEntry, Instrument, Citation, LegalCategory, LegalElement, URLOffence } from '../api/types'
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
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { XIcon } from '@/components/ui/x'
import { FaviconSearch } from '@/components/unlumen-ui/favicon-search'
import { faviconApiUrl } from '../api/domain'
import {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardImage,
} from '@/components/animate-ui/components/base/preview-link-card'

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
// EditOffencesDialog (each addition attaches immediately to one existing
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
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [value, setValue] = useState('')
  const [offences, setOffences] = useState<StagedOffence[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const pickerRef = useRef<MultiOffencePickerHandle>(null)

  const reset = () => {
    setValue(''); setOffences([]); setError(null)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const domains = value.split('\n').map(s => s.trim()).filter(Boolean)
    if (domains.length === 0) { setError('At least one domain is required'); return }
    // Catch a filled-in-but-not-yet-"+ Add offence"-clicked selection sitting
    // in the picker — otherwise it's silently dropped rather than attached.
    const pending = pickerRef.current?.flush()
    const allOffences = pending ? [...offences, pending] : offences
    setLoading(true)
    setError(null)
    try {
      await Promise.all(domains.map(d => createUrl(d)))
      await Promise.all(
        domains.flatMap(d => allOffences.map(o => attachOffence(d, o.categoryId, o.elementId)))
      )
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
      <DialogContent showCloseButton={false} style={{ maxWidth: 440 }}>
        <DialogHeader>
          <DialogTitle>Add Domain</DialogTitle>
          <DialogDescription>
            Enter one or more domains or full URLs to monitor for DNS compliance. Full URLs will have their domain automatically extracted. You can add multiple entries at once, just put each one on a new line.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="add-url-input">Domain</label>
            <textarea
              id="add-url-input"
              className="form-input"
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

function EditOffencesDialog({
  url,
  open,
  onClose,
}: {
  url: string | null
  open: boolean
  onClose: () => void
}) {
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
      <DialogContent showCloseButton={false} style={{ maxWidth: 440 }}>
        <DialogHeader>
          <DialogTitle>Offences</DialogTitle>
          <DialogDescription>{url}</DialogDescription>
        </DialogHeader>
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

/* ─── Skeleton ───────────────────────────────────────────────────────────── */

function SkeletonRows() {
  return (
    <>
      {[200, 160, 240].map((w, i) => (
        <TableRow key={i} className="skeleton-row">
          <TableCell className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </TableCell>
          {Array.from({ length: 6 }).map((_, j) => (
            <TableCell key={j} className="col-status">
              <span className="skeleton" style={{ width: 90, height: 14 }} />
            </TableCell>
          ))}
          <TableCell style={{ width: 52 }} />
          <TableCell className="col-evidence" />
        </TableRow>
      ))}
    </>
  )
}

/* ─── Empty Icon ─────────────────────────────────────────────────────────── */

function EmptyIcon() {
  return (
    <svg className="empty-icon" width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
      <rect x="8" y="4" width="24" height="32" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M32 4L40 12V36C40 37.1 39.1 38 38 38H32" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M40 12H32V4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 18H26M14 24H22" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

/* ─── URLs Page ──────────────────────────────────────────────────────────── */

const DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric', month: 'short', year: 'numeric',
})

const PAGE_SIZE = 25

// `<input type="datetime-local">` interprets its value in the browser's
// local timezone, but `due_date` is stored as a UTC ISO string. Slicing the
// UTC digits directly would feed local-timezone-formatted digits into a
// local-timezone-interpreting input, silently shifting the displayed time by
// the local UTC offset. Shift the Date by that offset first so the sliced
// digits are local wall-clock time.
function toLocalDatetimeInputValue(iso: string): string {
  const d = new Date(iso)
  const local = new Date(d.getTime() - d.getTimezoneOffset() * 60000)
  return local.toISOString().slice(0, 16)
}

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '—' },
  { value: 'requested', label: 'Requested' },
  { value: 'uplift', label: 'Uplift' },
  { value: 'suspended', label: 'Suspended' },
]

type CaseTextField = 'agency' | 'reference_number' | 'requesting_dept'

function URLsPage() {
  const [urls, setUrls] = useState<URLEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<URLEntry | null>(null)
  const [editOffencesTarget, setEditOffencesTarget] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  // Snapshots a text field's pre-edit value on focus so handleTextBlur can
  // roll back to it if the commit fails.
  const fieldOriginalRef = useRef<Record<string, string>>({})

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      setUrls(await fetchUrls())
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

  const handleDueDateChange = useCallback(async (id: number, dateStr: string) => {
    const previous = urls.find(u => u.id === id)?.due_date
    const dueDate = dateStr ? new Date(dateStr).toISOString() : null
    setUrls(prev => prev.map(u => u.id === id ? { ...u, due_date: dueDate ?? undefined } : u))
    try {
      await setUrlFields(id, { due_date: dueDate })
    } catch {
      setUrls(prev => prev.map(u => u.id === id ? { ...u, due_date: previous } : u))
    }
  }, [urls])

  const handleStatusChange = useCallback(async (id: number, status: string) => {
    const previous = urls.find(u => u.id === id)?.status
    setUrls(prev => prev.map(u => u.id === id ? { ...u, status } : u))
    try {
      await setUrlFields(id, { status })
    } catch {
      setUrls(prev => prev.map(u => u.id === id ? { ...u, status: previous } : u))
    }
  }, [urls])

  // Text fields commit on blur (not per keystroke) to avoid a PATCH per
  // character — fieldOriginalRef snapshots the pre-edit value on focus so a
  // failed commit can roll back to it.
  const handleTextFocus = useCallback((id: number, field: CaseTextField, value: string) => {
    fieldOriginalRef.current[`${id}:${field}`] = value
  }, [])

  const handleTextChange = useCallback((id: number, field: CaseTextField, value: string) => {
    setUrls(prev => prev.map(u => u.id === id ? { ...u, [field]: value } : u))
  }, [])

  const handleTextBlur = useCallback(async (id: number, field: CaseTextField) => {
    const key = `${id}:${field}`
    const original = fieldOriginalRef.current[key] ?? ''
    const current = urls.find(u => u.id === id)?.[field] ?? ''
    if (current === original) return
    try {
      await setUrlFields(id, { [field]: current })
    } catch {
      setUrls(prev => prev.map(u => u.id === id ? { ...u, [field]: original } : u))
    }
  }, [urls])

  const handleDelete = async () => {
    if (!deleteTarget) return
    await deleteUrl(deleteTarget.id)
    setDeleteTarget(null)
    load()
  }

  const totalPages = Math.max(1, Math.ceil(urls.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const paginated = useMemo(
    () => urls.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE),
    [urls, currentPage],
  )

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

      <div className="results-wrap">
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
          <Table className="results-table" aria-label="Monitored domains">
            <TableHeader>
              <TableRow>
                <TableHead className="col-domain th-left" scope="col">Domain</TableHead>
                <TableHead className="col-status" scope="col">Added</TableHead>
                <TableHead className="col-status" scope="col">Agency</TableHead>
                <TableHead className="col-status" scope="col">Reference No.</TableHead>
                <TableHead className="col-status" scope="col">Requesting Dept.</TableHead>
                <TableHead className="col-status" scope="col">Status</TableHead>
                <TableHead className="col-status" scope="col">Due Date</TableHead>
                <TableHead scope="col" style={{ width: 52, textAlign: 'center' }}>Scan</TableHead>
                <TableHead className="col-evidence" scope="col" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <SkeletonRows />
              ) : (
                paginated.map(u => (
                  <TableRow key={u.id} className="url-row">
                    <TableCell className="col-domain">
                      <PreviewLinkCard href={u.url}>
                        <PreviewLinkCardTrigger>
                          <span className="hostname flex items-center gap-2">
                            <img src={faviconApiUrl(u.url)} alt="" width={16} height={16} className="shrink-0" onError={e => { e.currentTarget.style.visibility = 'hidden' }} />
                            {u.url}
                          </span>
                        </PreviewLinkCardTrigger>
                        <PreviewLinkCardPanel>
                          <PreviewLinkCardImage />
                        </PreviewLinkCardPanel>
                      </PreviewLinkCard>
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <span className="dns-name">
                        {DATE_FMT.format(new Date(u.created_at))}
                      </span>
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <input
                        type="text"
                        className="form-input"
                        style={{ width: 120 }}
                        value={u.agency ?? ''}
                        maxLength={255}
                        onFocus={e => handleTextFocus(u.id, 'agency', e.target.value)}
                        onChange={e => handleTextChange(u.id, 'agency', e.target.value)}
                        onBlur={() => handleTextBlur(u.id, 'agency')}
                        aria-label={`Agency for ${u.url}`}
                      />
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <input
                        type="text"
                        className="form-input"
                        style={{ width: 120 }}
                        value={u.reference_number ?? ''}
                        maxLength={255}
                        onFocus={e => handleTextFocus(u.id, 'reference_number', e.target.value)}
                        onChange={e => handleTextChange(u.id, 'reference_number', e.target.value)}
                        onBlur={() => handleTextBlur(u.id, 'reference_number')}
                        aria-label={`Reference number for ${u.url}`}
                      />
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <input
                        type="text"
                        className="form-input"
                        style={{ width: 140 }}
                        value={u.requesting_dept ?? ''}
                        maxLength={255}
                        onFocus={e => handleTextFocus(u.id, 'requesting_dept', e.target.value)}
                        onChange={e => handleTextChange(u.id, 'requesting_dept', e.target.value)}
                        onBlur={() => handleTextBlur(u.id, 'requesting_dept')}
                        aria-label={`Requesting department for ${u.url}`}
                      />
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <Select
                        value={u.status ?? ''}
                        onValueChange={v => handleStatusChange(u.id, v)}
                      >
                        <SelectTrigger aria-label={`Status for ${u.url}`} placeholder="—" className="w-full" />
                        <SelectContent>
                          {STATUS_OPTIONS.map((opt, i) => (
                            <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <input
                        type="datetime-local"
                        className="form-input"
                        style={{ width: 180 }}
                        value={u.due_date ? toLocalDatetimeInputValue(u.due_date) : ''}
                        onChange={e => handleDueDateChange(u.id, e.target.value)}
                        aria-label={`Due date for ${u.url}`}
                      />
                    </TableCell>
                    <TableCell style={{ textAlign: 'center' }}>
                      <Switch
                        checked={u.enabled}
                        onCheckedChange={checked => handleToggle(u.id, checked)}
                        aria-label={`${u.enabled ? 'Disable' : 'Enable'} ${u.url} in scan`}
                      />
                    </TableCell>
                    <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                      <div className="flex items-center justify-end gap-1">
                        <button
                          type="button"
                          className="screenshot-icon-btn"
                          onClick={() => setEditOffencesTarget(u.url)}
                          aria-label={`Edit offences for ${u.url}`}
                          title="Offences"
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
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
        {!loading && totalPages > 1 && (
          <div className="pagination">
            <span className="pagination-label">Page {currentPage} of {totalPages}</span>
            <button
              type="button"
              className="pagination-btn"
              onClick={() => setPage(p => p - 1)}
              disabled={currentPage <= 1}
              aria-label="Previous page"
            >
              <ChevronLeftIcon className="w-4 h-4" />
            </button>
            <button
              type="button"
              className="pagination-btn"
              onClick={() => setPage(p => p + 1)}
              disabled={currentPage >= totalPages}
              aria-label="Next page"
            >
              <ChevronRightIcon className="w-4 h-4" />
            </button>
          </div>
        )}
      </div>

      <AddUrlDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdded={load}
      />

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget?.url ?? ''}
        description="This will remove it from your department's watchlist. The domain and its scan history are kept if any other department still watches it."
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />

      <EditOffencesDialog
        url={editOffencesTarget}
        open={editOffencesTarget !== null}
        onClose={() => setEditOffencesTarget(null)}
      />
    </div>
  )
}
