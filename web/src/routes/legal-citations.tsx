import { useCallback, useEffect, useMemo, useState, type ElementType, type ReactNode } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { Landmark, BookText, Tag, ListTree, FileText } from 'lucide-react'
import { Files, FolderItem, FolderTrigger, FolderContent, FileItem, SubFiles } from '@/components/animate-ui/components/radix/files'
import {
  fetchInstruments, createInstrument, updateInstrument, deleteInstrument,
  fetchCitations, parseCitationPreview, createCitation, updateCitation, deleteCitation,
  fetchCategories, createCategory, updateCategory, deleteCategory,
  fetchElements, createElement, updateElement, deleteElement,
  fetchSubElements, createSubElement, updateSubElement, deleteSubElement,
  formatParsedCitation,
} from '@/api/legal'
import type { Instrument, Citation, LegalCategory, LegalElement, LegalSubElement, LegalCitationParsed } from '@/api/types'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'
import { Input } from '@/components/ui/input'
import { Filters, type Filter, type FilterFieldConfig } from '@/components/reui/filters'
import { BrailleLoader } from '@/components/ui/braille-loader'
import { EmptyIcon } from '@/components/results-table-parts'
import { SquarePenIcon } from '@/components/ui/square-pen'
import { SquarePlusIcon } from '@/components/animate-ui/icons/square-plus'
import { SquareXIcon } from '@/components/animate-ui/icons/square-x'
import { useAuth } from './__root'

export const Route = createFileRoute('/legal-citations')({ component: LegalCitationsPage })

/* ─── Tree model ─────────────────────────────────────────────────────────── */

type LegalKind = 'instrument' | 'citation' | 'category' | 'element' | 'subelement'

type LegalTreeRow = {
  id: string
  kind: LegalKind
  refId: number
  label: string
  instrument?: Instrument
  citation?: Citation
  category?: LegalCategory
  element?: LegalElement
  subElement?: LegalSubElement
  children?: LegalTreeRow[]
}

// Eagerly walks the whole Instrument -> Citation -> Category -> Element ->
// SubElement hierarchy into one nested tree so it can be rendered as a single indented
// table via getSubRows — a reference catalog like this is small enough that
// loading it all up front (rather than lazy-fetching per expand) is simpler
// and keeps expand/collapse instant.
async function loadTree(): Promise<LegalTreeRow[]> {
  const instruments = await fetchInstruments()
  return Promise.all(instruments.map(async (inst): Promise<LegalTreeRow> => {
    const citations = await fetchCitations(inst.id)
    const citationRows = await Promise.all(citations.map(async (cit): Promise<LegalTreeRow> => {
      const categories = await fetchCategories(cit.id)
      const categoryRows = await Promise.all(categories.map(async (cat): Promise<LegalTreeRow> => {
        const elements = await fetchElements(cat.id)
        const elementRows: LegalTreeRow[] = await Promise.all(elements.map(async (el): Promise<LegalTreeRow> => {
          const subElements = await fetchSubElements(el.id)
          const subElementRows: LegalTreeRow[] = subElements.map(se => ({
            id: `subelement-${se.id}`, kind: 'subelement', refId: se.id, label: se.name, subElement: se,
          }))
          return {
            id: `element-${el.id}`, kind: 'element', refId: el.id, label: el.name, element: el,
            children: subElementRows.length > 0 ? subElementRows : undefined,
          }
        }))
        return {
          id: `category-${cat.id}`, kind: 'category', refId: cat.id, label: cat.name, category: cat,
          children: elementRows.length > 0 ? elementRows : undefined,
        }
      }))
      return {
        id: `citation-${cit.id}`, kind: 'citation', refId: cit.id, label: cit.raw_text, citation: cit,
        children: categoryRows.length > 0 ? categoryRows : undefined,
      }
    }))
    return {
      id: `instrument-${inst.id}`, kind: 'instrument', refId: inst.id, label: inst.short_title, instrument: inst,
      children: citationRows.length > 0 ? citationRows : undefined,
    }
  }))
}

const IS_ONLY = [{ value: 'is', label: 'is' }]

// Keeps a row if its own label matches, or if any descendant's does — when
// kept only because of a descendant match, its children are pruned down to
// the matching-or-ancestor-of-match ones too (so an unrelated sibling
// category/element doesn't tag along just because its parent instrument
// matched something elsewhere). A row that matches directly keeps its whole
// subtree untouched, same as today with no search applied.
function filterTreeBySearch(rows: LegalTreeRow[], query: string): LegalTreeRow[] {
  if (!query) return rows
  const walk = (row: LegalTreeRow): LegalTreeRow | null => {
    if (row.label.toLowerCase().includes(query)) return row
    const children = row.children?.map(walk).filter((r): r is LegalTreeRow => r !== null)
    return children && children.length > 0 ? { ...row, children } : null
  }
  return rows.map(walk).filter((r): r is LegalTreeRow => r !== null)
}

const DELETE_DESCRIPTIONS: Record<LegalKind, string> = {
  instrument: 'Cascades to every citation, category, element, sub-element, and recorded offence under this law.',
  citation: 'Cascades to every category, element, sub-element, and recorded offence under this citation.',
  category: 'Cascades to every element, sub-element, and recorded offence under this category.',
  element: 'Cascades to every sub-element and recorded offence under this element.',
  subelement: 'Deletes the recorded offence of any domain tagged with this sub-element.',
}

/* ─── Dialogs ─────────────────────────────────────────────────────────────── */

// Stored/API values stay these English constants (matches every other enum
// in this codebase — DNSServer.Protocol, ScanRun.Status, etc.) — only the
// label shown to the user is Malay, via INSTRUMENT_TYPE_LABELS below.
const INSTRUMENT_TYPES = ['ACT', 'ORDINANCE', 'ENACTMENT', 'SUBSIDIARY', 'CONSTITUTION'] as const
const INSTRUMENT_TYPE_LABELS: Record<string, string> = {
  ACT: 'Akta', ORDINANCE: 'Ordinan', ENACTMENT: 'Enakmen', SUBSIDIARY: 'Subsidiari', CONSTITUTION: 'Perlembagaan',
}
// The 13 states are already their Malay/official names (proper nouns, no
// translation needed) — only "FEDERAL" needs a display label.
const JURISDICTIONS = [
  'FEDERAL', 'Johor', 'Kedah', 'Kelantan', 'Melaka', 'Negeri Sembilan', 'Pahang',
  'Perak', 'Perlis', 'Pulau Pinang', 'Sabah', 'Sarawak', 'Selangor', 'Terengganu',
]
const jurisdictionLabel = (j: string) => j === 'FEDERAL' ? 'Persekutuan' : j

const KIND_ICON: Record<LegalKind, ElementType> = {
  instrument: Landmark,
  citation: BookText,
  category: Tag,
  element: ListTree,
  subelement: FileText,
}

function RowLabel({ r }: { r: LegalTreeRow }) {
  if (r.kind === 'instrument' && r.instrument) {
    return (
      <div className="flex flex-col min-w-0">
        <span className="font-semibold truncate">{r.label}</span>
        <span className="text-xs text-stone-muted truncate">
          {[
            INSTRUMENT_TYPE_LABELS[r.instrument.type] ?? r.instrument.type,
            jurisdictionLabel(r.instrument.jurisdiction),
            r.instrument.number || null,
          ].filter(Boolean).join(' · ')}
          {r.instrument.year ? ` (${r.instrument.year})` : ''}
        </span>
      </div>
    )
  }
  if (r.kind === 'citation' && r.citation) {
    return (
      <div className="flex flex-col min-w-0">
        <span className="dns-name flex items-center gap-2 truncate">
          {r.label}
          <ConfidenceBadge confidence={r.citation.parse_confidence} />
        </span>
        {/* A clean parse reconstructs to the same string as raw_text —
            showing it again would just repeat the line above. Only surface
            it for NEEDS_REVIEW, where it shows how far parsing got before
            stalling. */}
        {r.citation.parse_confidence === 'NEEDS_REVIEW' && (
          <span className="text-xs text-stone-muted truncate">Parsed as: {formatParsedCitation(r.citation.parsed)}</span>
        )}
      </div>
    )
  }
  return <span className="truncate">{r.label}</span>
}

function ConfidenceBadge({ confidence }: { confidence: 'OK' | 'NEEDS_REVIEW' }) {
  return (
    <span
      style={{
        fontSize: 11, fontWeight: 600, padding: '1px 6px', borderRadius: 4,
        color: confidence === 'OK' ? 'var(--stone-muted)' : 'var(--ledger-indigo, #4338ca)',
        border: '1px solid currentColor',
      }}
    >
      {confidence === 'OK' ? 'OK' : 'Needs review'}
    </span>
  )
}

function InstrumentFormDialog({
  open, onClose, onSaved, editing,
}: { open: boolean; onClose: () => void; onSaved: () => void; editing: Instrument | null }) {
  const [type, setType] = useState<string>('ACT')
  const [jurisdiction, setJurisdiction] = useState('FEDERAL')
  const [number, setNumber] = useState('')
  const [year, setYear] = useState('')
  const [shortTitle, setShortTitle] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => {
    setType('ACT'); setJurisdiction('FEDERAL'); setNumber(''); setYear(''); setShortTitle(''); setError(null)
  }

  useEffect(() => {
    if (!open) return
    if (editing) {
      setType(editing.type); setJurisdiction(editing.jurisdiction)
      setNumber(editing.number); setYear(editing.year != null ? String(editing.year) : '')
      setShortTitle(editing.short_title); setError(null)
    } else {
      reset()
    }
  }, [open, editing])

  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!shortTitle.trim()) { setError('Short title is required'); return }
    setLoading(true)
    setError(null)
    try {
      const payload = {
        type, jurisdiction, number: number.trim(),
        year: year.trim() ? Number(year) : undefined,
        short_title: shortTitle.trim(),
      }
      if (editing) {
        await updateInstrument(editing.id, payload)
      } else {
        await createInstrument(payload)
      }
      reset()
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${editing ? 'save' : 'add'} instrument`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 440 }}>
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit Instrument' : 'Add Instrument'}</DialogTitle>
          <DialogDescription>
            The law itself — created once, reused via lookup across citations (e.g. "Akta Komunikasi dan Multimedia 1998").
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" id="instrument-type-label">Type</label>
            <Select value={type} onValueChange={setType} disabled={loading}>
              <SelectTrigger aria-labelledby="instrument-type-label" className="w-full" />
              <SelectContent>
                {INSTRUMENT_TYPES.map((t, i) => (
                  <SelectItem key={t} index={i} value={t}>{INSTRUMENT_TYPE_LABELS[t]}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="form-field">
            <label className="form-label" id="instrument-jurisdiction-label">Jurisdiction</label>
            <Select value={jurisdiction} onValueChange={setJurisdiction} disabled={loading}>
              <SelectTrigger aria-labelledby="instrument-jurisdiction-label" className="w-full" />
              <SelectContent>
                {JURISDICTIONS.map((j, i) => (
                  <SelectItem key={j} index={i} value={j}>{jurisdictionLabel(j)}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="instrument-number-input">
              Number <span style={{ color: 'var(--stone-muted)', fontWeight: 400 }}>
                (optional — the Act's own official number, e.g. "588" for Akta 588; not a Seksyen number, which belongs to a Citation instead. Leave blank if this instrument doesn't have one — common for older Acts and most Enactments, e.g. Akta Rumah Judi Terbuka 1953)
              </span>
            </label>
            <input
              id="instrument-number-input"
              className="form-input"
              type="text"
              placeholder="e.g. 588 (leave blank if unknown/none)"
              value={number}
              onChange={e => setNumber(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="instrument-year-input">
              Year <span style={{ color: 'var(--stone-muted)', fontWeight: 400 }}>(optional)</span>
            </label>
            <input
              id="instrument-year-input"
              className="form-input"
              type="number"
              placeholder="e.g. 1998"
              value={year}
              onChange={e => setYear(e.target.value)}
              disabled={loading}
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="instrument-title-input">Short Title</label>
            <input
              id="instrument-title-input"
              className="form-input"
              type="text"
              placeholder="e.g. Akta Komunikasi dan Multimedia 1998"
              value={shortTitle}
              onChange={e => setShortTitle(e.target.value)}
              disabled={loading}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {editing ? (loading ? 'Saving…' : 'Save Changes') : (loading ? 'Adding…' : 'Add Instrument')}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function CitationFormDialog({
  open, onClose, onSaved, instrument, editing,
}: { open: boolean; onClose: () => void; onSaved: () => void; instrument: Instrument | null; editing: Citation | null }) {
  const [rawText, setRawText] = useState('')
  const [preview, setPreview] = useState<{ parsed: LegalCitationParsed; parse_confidence: 'OK' | 'NEEDS_REVIEW' } | null>(null)
  const [parsing, setParsing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setRawText(''); setPreview(null); setError(null) }
  const handleClose = () => { reset(); onClose() }

  useEffect(() => {
    if (!open) return
    if (editing) {
      setRawText(editing.raw_text)
      setPreview({ parsed: editing.parsed, parse_confidence: editing.parse_confidence })
      setError(null)
    } else {
      reset()
    }
  }, [open, editing])

  const handleParse = async () => {
    if (!rawText.trim()) return
    setParsing(true)
    setError(null)
    try {
      setPreview(await parseCitationPreview(rawText.trim()))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to parse')
    } finally {
      setParsing(false)
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!instrument) return
    if (!rawText.trim()) { setError('Citation text is required'); return }
    setLoading(true)
    setError(null)
    try {
      // Parse first if the user hasn't hit "Parse" yet (or hasn't touched
      // a pre-filled edit), so save never sends a stale/empty parsed payload.
      const result = preview ?? await parseCitationPreview(rawText.trim())
      const payload = {
        instrument_id: instrument.id,
        raw_text: rawText.trim(),
        parsed: result.parsed,
        parse_confidence: result.parse_confidence,
      }
      if (editing) {
        await updateCitation(editing.id, payload)
      } else {
        await createCitation(payload)
      }
      reset()
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${editing ? 'save' : 'add'} citation`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 460 }}>
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit Citation' : 'Add Citation'}</DialogTitle>
          <DialogDescription>
            {instrument ? `Under ${instrument.short_title}.` : ''} Type the citation as free text in Malay (e.g. "Seksyen 233(1)(a)") — it's parsed automatically into structured fields.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="citation-rawtext-input">Citation</label>
            <div className="flex flex-row items-center" style={{ gap: 8 }}>
              <input
                id="citation-rawtext-input"
                className="form-input"
                type="text"
                placeholder='e.g. Seksyen 233(1)(a)'
                value={rawText}
                onChange={e => { setRawText(e.target.value); setPreview(null) }}
                autoFocus
                disabled={loading}
              />
              <button type="button" className="btn-ghost" onClick={handleParse} disabled={loading || parsing || !rawText.trim()}>
                {parsing ? 'Parsing…' : 'Parse'}
              </button>
            </div>
          </div>
          {preview && (
            <div className="form-field" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span className="dns-name">{formatParsedCitation(preview.parsed)}</span>
              <ConfidenceBadge confidence={preview.parse_confidence} />
            </div>
          )}
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {editing ? (loading ? 'Saving…' : 'Save Changes') : (loading ? 'Adding…' : 'Add Citation')}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function CategoryFormDialog({
  open, onClose, onSaved, citation, editing,
}: { open: boolean; onClose: () => void; onSaved: () => void; citation: Citation | null; editing: LegalCategory | null }) {
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setName(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  useEffect(() => {
    if (!open) return
    if (editing) { setName(editing.name); setError(null) } else { reset() }
  }, [open, editing])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!citation) return
    if (!name.trim()) { setError('Name is required'); return }
    setLoading(true)
    setError(null)
    try {
      if (editing) {
        await updateCategory(editing.id, name.trim())
      } else {
        await createCategory(citation.id, name.trim())
      }
      reset()
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${editing ? 'save' : 'add'} category`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit Category' : 'Add Category'}</DialogTitle>
          <DialogDescription>
            {citation ? `Under "${citation.raw_text}".` : ''} Scoped to this citation only — e.g. "Harassment", "Hate Speech".
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="category-name-input">Name</label>
            <input
              id="category-name-input"
              className="form-input"
              type="text"
              placeholder="e.g. Harassment"
              value={name}
              onChange={e => setName(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {editing ? (loading ? 'Saving…' : 'Save Changes') : (loading ? 'Adding…' : 'Add Category')}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ElementFormDialog({
  open, onClose, onSaved, category, editing,
}: { open: boolean; onClose: () => void; onSaved: () => void; category: LegalCategory | null; editing: LegalElement | null }) {
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setName(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  useEffect(() => {
    if (!open) return
    if (editing) { setName(editing.name); setError(null) } else { reset() }
  }, [open, editing])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing && !category) return
    if (!name.trim()) { setError('Name is required'); return }
    setLoading(true)
    setError(null)
    try {
      if (editing) {
        await updateElement(editing.id, name.trim())
      } else if (category) {
        await createElement(category.id, name.trim())
      }
      reset()
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${editing ? 'save' : 'add'} element`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit Element' : 'Add Element'}</DialogTitle>
          <DialogDescription>
            {category ? `Sub-category of "${category.name}".` : ''} Optional finer-grained qualifier — e.g. "Menacing", "Obscene".
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="element-name-input">Name</label>
            <input
              id="element-name-input"
              className="form-input"
              type="text"
              placeholder="e.g. Menacing"
              value={name}
              onChange={e => setName(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {editing ? (loading ? 'Saving…' : 'Save Changes') : (loading ? 'Adding…' : 'Add Element')}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function SubElementFormDialog({
  open, onClose, onSaved, element, editing,
}: { open: boolean; onClose: () => void; onSaved: () => void; element: LegalElement | null; editing: LegalSubElement | null }) {
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setName(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  useEffect(() => {
    if (!open) return
    if (editing) { setName(editing.name); setError(null) } else { reset() }
  }, [open, editing])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing && !element) return
    if (!name.trim()) { setError('Name is required'); return }
    setLoading(true)
    setError(null)
    try {
      if (editing) {
        await updateSubElement(editing.id, name.trim())
      } else if (element) {
        await createSubElement(element.id, name.trim())
      }
      reset()
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${editing ? 'save' : 'add'} sub-element`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit Sub-Element' : 'Add Sub-Element'}</DialogTitle>
          <DialogDescription>
            {element ? `Sub-category of "${element.name}".` : ''} Optional finer-grained qualifier one level below Element.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="subelement-name-input">Name</label>
            <input
              id="subelement-name-input"
              className="form-input"
              type="text"
              placeholder="e.g. Direct Threat"
              value={name}
              onChange={e => setName(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {editing ? (loading ? 'Saving…' : 'Save Changes') : (loading ? 'Adding…' : 'Add Sub-Element')}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Page ────────────────────────────────────────────────────────────────── */

function LegalCitationsPage() {
  const { me } = useAuth()
  const canManage = me?.is_admin || me?.is_dept_admin
  const [tree, setTree] = useState<LegalTreeRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  // Lifted out of the Files tree itself (rather than left as its own
  // uncontrolled state) because every add/edit/delete re-triggers load(),
  // which flips `loading` and swaps the tree out for the spinner — that
  // unmounts the tree subtree, and uncontrolled state living inside it
  // would reset to closed on every save. One flat set of open node ids
  // shared by every nesting level (passed to both Files and each nested
  // SubFiles below) mirrors how the old react-table `expanded` state was
  // keyed by row id regardless of depth.
  const [openFolders, setOpenFolders] = useState<string[]>([])
  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<Filter<string>[]>([])

  const [addInstrumentOpen, setAddInstrumentOpen] = useState(false)
  const [addCitationFor, setAddCitationFor] = useState<Instrument | null>(null)
  const [addCategoryFor, setAddCategoryFor] = useState<Citation | null>(null)
  const [addElementFor, setAddElementFor] = useState<LegalCategory | null>(null)
  const [addSubElementFor, setAddSubElementFor] = useState<LegalElement | null>(null)
  const [editInstrumentTarget, setEditInstrumentTarget] = useState<Instrument | null>(null)
  const [editCitationTarget, setEditCitationTarget] = useState<Citation | null>(null)
  const [editCategoryTarget, setEditCategoryTarget] = useState<LegalCategory | null>(null)
  const [editElementTarget, setEditElementTarget] = useState<LegalElement | null>(null)
  const [editSubElementTarget, setEditSubElementTarget] = useState<LegalSubElement | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{ kind: LegalKind; id: number; label: string } | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      setTree(await loadTree())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load legal citations')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleDelete = async () => {
    if (!deleteTarget) return
    switch (deleteTarget.kind) {
      case 'instrument': await deleteInstrument(deleteTarget.id); break
      case 'citation': await deleteCitation(deleteTarget.id); break
      case 'category': await deleteCategory(deleteTarget.id); break
      case 'element': await deleteElement(deleteTarget.id); break
      case 'subelement': await deleteSubElement(deleteTarget.id); break
    }
    setDeleteTarget(null)
    load()
  }

  const filterFields = useMemo<FilterFieldConfig<string>[]>(() => [
    { key: 'type', label: 'Type', type: 'select', operators: IS_ONLY, options: INSTRUMENT_TYPES.map(t => ({ value: t, label: INSTRUMENT_TYPE_LABELS[t] })) },
    { key: 'jurisdiction', label: 'Jurisdiction', type: 'select', operators: IS_ONLY, options: JURISDICTIONS.map(j => ({ value: j, label: jurisdictionLabel(j) })) },
  ], [])

  const typeFilter = filters.find(f => f.field === 'type')?.values[0]
  const jurisdictionFilter = filters.find(f => f.field === 'jurisdiction')?.values[0]

  // Type/Jurisdiction only make sense against top-level instrument rows, so
  // they filter the instrument array first; search then walks whatever
  // subtree survives that, at any depth.
  const filteredTree = useMemo(() => {
    let rows = tree
    if (typeFilter) rows = rows.filter(r => r.instrument?.type === typeFilter)
    if (jurisdictionFilter) rows = rows.filter(r => r.instrument?.jurisdiction === jurisdictionFilter)
    return filterTreeBySearch(rows, search.trim().toLowerCase())
  }, [tree, search, typeFilter, jurisdictionFilter])

  // Which "+" a row's add-child button creates, and where it goes — keyed
  // off the row's own kind since each level only ever adds the next one down.
  const addChildFor = (r: LegalTreeRow): { label: string; onClick: () => void } | null => {
    switch (r.kind) {
      case 'instrument': return r.instrument ? { label: 'Citation', onClick: () => setAddCitationFor(r.instrument!) } : null
      case 'citation': return r.citation ? { label: 'Category', onClick: () => setAddCategoryFor(r.citation!) } : null
      case 'category': return r.category ? { label: 'Element', onClick: () => setAddElementFor(r.category!) } : null
      case 'element': return r.element ? { label: 'Sub-Element', onClick: () => setAddSubElementFor(r.element!) } : null
      case 'subelement': return null
    }
  }

  const editRow = (r: LegalTreeRow) => {
    if (r.kind === 'instrument' && r.instrument) setEditInstrumentTarget(r.instrument)
    else if (r.kind === 'citation' && r.citation) setEditCitationTarget(r.citation)
    else if (r.kind === 'category' && r.category) setEditCategoryTarget(r.category)
    else if (r.kind === 'element' && r.element) setEditElementTarget(r.element)
    else if (r.kind === 'subelement' && r.subElement) setEditSubElementTarget(r.subElement)
  }

  // Plain recursive function, not useCallback — it's only ever called
  // during render (never passed to a memoized child needing referential
  // stability), and self-referencing recursion inside a useCallback trips
  // the React Compiler's "accessed before declared" check.
  function renderNode(r: LegalTreeRow): ReactNode {
    const addChild = addChildFor(r)
    const actions = canManage && (
      <>
        {addChild && (
          <button type="button" className="screenshot-icon-btn" onClick={addChild.onClick} aria-label={`Add ${addChild.label} to ${r.label}`} title={`Add ${addChild.label}`}>
            <SquarePlusIcon size={16} animateOnHover animation="path-loop" />
          </button>
        )}
        <button type="button" className="screenshot-icon-btn" onClick={() => editRow(r)} aria-label={`Edit ${r.label}`} title="Edit">
          <SquarePenIcon size={16} />
        </button>
        <button type="button" className="screenshot-icon-btn" onClick={() => setDeleteTarget({ kind: r.kind, id: r.refId, label: r.label })} aria-label={`Delete ${r.label}`} title="Delete">
          <SquareXIcon size={16} animateOnHover animation="path-loop" />
        </button>
      </>
    )

    if (r.children && r.children.length > 0) {
      return (
        <FolderItem key={r.id} value={r.id}>
          <FolderTrigger icon={KIND_ICON[r.kind]} actions={actions}>
            <RowLabel r={r} />
          </FolderTrigger>
          <FolderContent>
            <SubFiles open={openFolders} onOpenChange={setOpenFolders}>{r.children.map(renderNode)}</SubFiles>
          </FolderContent>
        </FolderItem>
      )
    }

    return (
      <FileItem key={r.id} icon={KIND_ICON[r.kind]} actions={actions}>
        <RowLabel r={r} />
      </FileItem>
    )
  }

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title">Legal Citations</h1>
        <p className="page-subtitle">{!loading && `${tree.length} instrument${tree.length === 1 ? '' : 's'}`}</p>
        {canManage && (
          <button type="button" className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddInstrumentOpen(true)}>
            + Add Instrument
          </button>
        )}
      </div>

      {error ? (
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button className="btn-primary" onClick={load}>Retry</button>
        </div>
      ) : loading ? (
        <div className="flex items-center justify-center" style={{ padding: '4rem 0' }}>
          <BrailleLoader variant="typing" fontSize={13} />
        </div>
      ) : tree.length === 0 ? (
        <div className="empty-state">
          <EmptyIcon />
          <p className="empty-heading">No instruments yet</p>
          <p className="empty-body">
            {canManage
              ? 'Add a law (Instrument) to start building out its citations, categories, and elements.'
              : 'Nothing has been added to the legal citation catalog yet.'}
          </p>
          {canManage && (
            <button className="btn-primary" onClick={() => setAddInstrumentOpen(true)}>Add Instrument</button>
          )}
        </div>
      ) : (
        <div className="flex flex-col items-stretch w-full gap-4 mt-4">
          <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full">
            <Input
              type="search"
              placeholder="Search instruments, citations, categories…"
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="max-w-64"
              aria-label="Search legal citations"
            />
            <Filters filters={filters} fields={filterFields} onChange={setFilters} />
          </div>

          <div className="results-wrap w-full mt-4">
            {filteredTree.length === 0 ? (
              <div className="empty-state" style={{ padding: '3rem 0' }}>
                <p className="empty-heading">No legal citations match the current filters</p>
              </div>
            ) : (
              <Files open={openFolders} onOpenChange={setOpenFolders}>{filteredTree.map(renderNode)}</Files>
            )}
          </div>
        </div>
      )}

      <InstrumentFormDialog open={addInstrumentOpen} onClose={() => setAddInstrumentOpen(false)} onSaved={load} editing={null} />
      <InstrumentFormDialog open={editInstrumentTarget !== null} onClose={() => setEditInstrumentTarget(null)} onSaved={load} editing={editInstrumentTarget} />

      <CitationFormDialog open={addCitationFor !== null} onClose={() => setAddCitationFor(null)} onSaved={load} instrument={addCitationFor} editing={null} />
      <CitationFormDialog open={editCitationTarget !== null} onClose={() => setEditCitationTarget(null)} onSaved={load} instrument={editCitationTarget?.instrument ?? null} editing={editCitationTarget} />

      <CategoryFormDialog open={addCategoryFor !== null} onClose={() => setAddCategoryFor(null)} onSaved={load} citation={addCategoryFor} editing={null} />
      <CategoryFormDialog open={editCategoryTarget !== null} onClose={() => setEditCategoryTarget(null)} onSaved={load} citation={editCategoryTarget?.citation ?? null} editing={editCategoryTarget} />

      <ElementFormDialog open={addElementFor !== null} onClose={() => setAddElementFor(null)} onSaved={load} category={addElementFor} editing={null} />
      {/* category is null here (not editCategoryTarget — unrelated state for
          the edit-category dialog above): LegalElement doesn't carry its
          parent Category, and the update payload doesn't need it either
          (updateElement only takes id+name). handleSubmit only requires
          category when creating (editing is falsy), so this only affects
          the dialog's "Sub-category of ..." description line, which is
          simply omitted for the edit case. */}
      <ElementFormDialog open={editElementTarget !== null} onClose={() => setEditElementTarget(null)} onSaved={load} category={null} editing={editElementTarget} />

      <SubElementFormDialog open={addSubElementFor !== null} onClose={() => setAddSubElementFor(null)} onSaved={load} element={addSubElementFor} editing={null} />
      <SubElementFormDialog open={editSubElementTarget !== null} onClose={() => setEditSubElementTarget(null)} onSaved={load} element={null} editing={editSubElementTarget} />

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget?.label ?? ''}
        description={deleteTarget ? DELETE_DESCRIPTIONS[deleteTarget.kind] : undefined}
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  )
}
