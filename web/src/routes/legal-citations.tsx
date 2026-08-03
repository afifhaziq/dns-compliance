import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { type ColumnDef, type ExpandedState, getCoreRowModel, getExpandedRowModel, useReactTable } from '@tanstack/react-table'
import { DataGrid, DataGridContainer } from '@/components/reui/data-grid/data-grid'
import { DataGridTable, DataGridTableRowExpand } from '@/components/reui/data-grid/data-grid-table'
import {
  fetchInstruments, createInstrument, deleteInstrument,
  fetchCitations, parseCitationPreview, createCitation, deleteCitation,
  fetchCategories, createCategory, deleteCategory,
  fetchElements, createElement, deleteElement,
  formatParsedCitation,
} from '@/api/legal'
import type { Instrument, Citation, LegalCategory, LegalCitationParsed } from '@/api/types'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'
import { BrailleLoader } from '@/components/ui/braille-loader'
import { EmptyIcon } from '@/components/results-table-parts'
import { XIcon } from '@/components/ui/x'
import { useAuth } from './__root'

export const Route = createFileRoute('/legal-citations')({ component: LegalCitationsPage })

/* ─── Tree model ─────────────────────────────────────────────────────────── */

type LegalKind = 'instrument' | 'citation' | 'category' | 'element'

type LegalTreeRow = {
  id: string
  kind: LegalKind
  refId: number
  label: string
  instrument?: Instrument
  citation?: Citation
  category?: LegalCategory
  children?: LegalTreeRow[]
}

// Eagerly walks the whole Instrument -> Citation -> Category -> Element
// hierarchy into one nested tree so it can be rendered as a single indented
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
        const elementRows: LegalTreeRow[] = elements.map(el => ({
          id: `element-${el.id}`, kind: 'element', refId: el.id, label: el.name,
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

const DELETE_DESCRIPTIONS: Record<LegalKind, string> = {
  instrument: 'Cascades to every citation, category, element, and recorded offence under this law.',
  citation: 'Cascades to every category, element, and recorded offence under this citation.',
  category: 'Cascades to every element and recorded offence under this category.',
  element: 'Removes this element from any domain currently tagged with it.',
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

function AddInstrumentDialog({
  open, onClose, onAdded,
}: { open: boolean; onClose: () => void; onAdded: () => void }) {
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
  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!number.trim() || !shortTitle.trim()) { setError('Number and short title are required'); return }
    setLoading(true)
    setError(null)
    try {
      await createInstrument({
        type, jurisdiction, number: number.trim(),
        year: year.trim() ? Number(year) : undefined,
        short_title: shortTitle.trim(),
      })
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add instrument')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 440 }}>
        <DialogHeader>
          <DialogTitle>Add Instrument</DialogTitle>
          <DialogDescription>
            The law itself — created once, reused via lookup across citations (e.g. "Communications and Multimedia Act 1998").
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
              Number <span style={{ color: 'var(--stone-muted)', fontWeight: 400 }}>(always text — e.g. "588", "A1220", "No. 9 of 1995")</span>
            </label>
            <input
              id="instrument-number-input"
              className="form-input"
              type="text"
              placeholder="e.g. 588"
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
              placeholder="e.g. Communications and Multimedia Act 1998"
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
              {loading ? 'Adding…' : 'Add Instrument'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function AddCitationDialog({
  open, onClose, onAdded, instrument,
}: { open: boolean; onClose: () => void; onAdded: () => void; instrument: Instrument | null }) {
  const [rawText, setRawText] = useState('')
  const [preview, setPreview] = useState<{ parsed: LegalCitationParsed; parse_confidence: 'OK' | 'NEEDS_REVIEW' } | null>(null)
  const [parsing, setParsing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setRawText(''); setPreview(null); setError(null) }
  const handleClose = () => { reset(); onClose() }

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
      // Parse first if the user hasn't hit "Parse" yet, so save never sends
      // a stale/empty parsed payload.
      const result = preview ?? await parseCitationPreview(rawText.trim())
      await createCitation({
        instrument_id: instrument.id,
        raw_text: rawText.trim(),
        parsed: result.parsed,
        parse_confidence: result.parse_confidence,
      })
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add citation')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 460 }}>
        <DialogHeader>
          <DialogTitle>Add Citation</DialogTitle>
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
              {loading ? 'Adding…' : 'Add Citation'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function AddCategoryDialog({
  open, onClose, onAdded, citation,
}: { open: boolean; onClose: () => void; onAdded: () => void; citation: Citation | null }) {
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setName(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!citation) return
    if (!name.trim()) { setError('Name is required'); return }
    setLoading(true)
    setError(null)
    try {
      await createCategory(citation.id, name.trim())
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add category')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Add Category</DialogTitle>
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
              {loading ? 'Adding…' : 'Add Category'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function AddElementDialog({
  open, onClose, onAdded, category,
}: { open: boolean; onClose: () => void; onAdded: () => void; category: LegalCategory | null }) {
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setName(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!category) return
    if (!name.trim()) { setError('Name is required'); return }
    setLoading(true)
    setError(null)
    try {
      await createElement(category.id, name.trim())
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add element')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Add Element</DialogTitle>
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
              {loading ? 'Adding…' : 'Add Element'}
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
  const [expanded, setExpanded] = useState<ExpandedState>({})

  const [addInstrumentOpen, setAddInstrumentOpen] = useState(false)
  const [addCitationFor, setAddCitationFor] = useState<Instrument | null>(null)
  const [addCategoryFor, setAddCategoryFor] = useState<Citation | null>(null)
  const [addElementFor, setAddElementFor] = useState<LegalCategory | null>(null)
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
    }
    setDeleteTarget(null)
    load()
  }

  const columns = useMemo<ColumnDef<LegalTreeRow>[]>(() => [
    {
      id: 'name',
      header: 'Name',
      meta: { headerClassName: 'col-domain th-left', cellClassName: 'col-domain' },
      cell: ({ row }) => {
        const r = row.original
        return (
          <div className="flex items-center gap-2">
            <DataGridTableRowExpand row={row} className="-ms-1.5" />
            {r.kind === 'instrument' && r.instrument && (
              <div className="flex flex-col">
                <span className="font-semibold">{r.label}</span>
                <span className="text-xs text-stone-muted">
                  {INSTRUMENT_TYPE_LABELS[r.instrument.type] ?? r.instrument.type} · {jurisdictionLabel(r.instrument.jurisdiction)} · {r.instrument.number}
                  {r.instrument.year ? ` (${r.instrument.year})` : ''}
                </span>
              </div>
            )}
            {r.kind === 'citation' && r.citation && (
              <div className="flex flex-col">
                <span className="dns-name flex items-center gap-2">
                  {r.label}
                  <ConfidenceBadge confidence={r.citation.parse_confidence} />
                </span>
                {/* A clean parse reconstructs to the same string as raw_text —
                    showing it again would just repeat the line above. Only
                    surface it for NEEDS_REVIEW, where it shows exactly how
                    far parsing got before stalling. */}
                {r.citation.parse_confidence === 'NEEDS_REVIEW' && (
                  <span className="text-xs text-stone-muted">
                    Parsed as: {formatParsedCitation(r.citation.parsed)}
                  </span>
                )}
              </div>
            )}
            {(r.kind === 'category' || r.kind === 'element') && (
              <span>{r.label}</span>
            )}
          </div>
        )
      },
      minSize: 340,
    },
    ...(canManage ? [{
      id: 'actions',
      header: '',
      size: 220,
      meta: { headerClassName: 'col-evidence', cellClassName: 'col-evidence text-right' },
      cell: ({ row }: { row: { original: LegalTreeRow } }) => {
        const r = row.original
        return (
          <div className="flex items-center justify-end gap-2">
            {/* .btn-ghost's border-stone-border is tuned for contrast
                against a bg-stone-panel surface (dialogs/cards) — .results-table
                rows have no background of their own, so on the page's near-black
                base the border nearly disappears in dark mode. An explicit
                panel background fixes that regardless of theme, matching how
                the navbar's Sign Out button (also .btn-ghost) gets its own
                explicit border-color override rather than relying on ambient
                contrast. */}
            {r.kind === 'instrument' && r.instrument && (
              <button type="button" className="btn-ghost" style={{ backgroundColor: 'var(--stone-panel)' }} onClick={() => setAddCitationFor(r.instrument!)}>
                + Citation
              </button>
            )}
            {r.kind === 'citation' && r.citation && (
              <button type="button" className="btn-ghost" style={{ backgroundColor: 'var(--stone-panel)' }} onClick={() => setAddCategoryFor(r.citation!)}>
                + Category
              </button>
            )}
            {r.kind === 'category' && r.category && (
              <button type="button" className="btn-ghost" style={{ backgroundColor: 'var(--stone-panel)' }} onClick={() => setAddElementFor(r.category!)}>
                + Element
              </button>
            )}
            <button
              type="button"
              className="screenshot-icon-btn"
              onClick={() => setDeleteTarget({ kind: r.kind, id: r.refId, label: r.label })}
              aria-label={`Delete ${r.label}`}
              title="Delete"
            >
              <XIcon size={16} />
            </button>
          </div>
        )
      },
    } satisfies ColumnDef<LegalTreeRow>] : []),
  ], [canManage])

  const table = useReactTable({
    data: tree,
    columns,
    state: { expanded },
    onExpandedChange: setExpanded,
    getRowId: row => row.id,
    getSubRows: row => row.children,
    getCoreRowModel: getCoreRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
  })

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
        <div className="results-wrap w-full">
          <DataGrid table={table} recordCount={tree.length} tableClassNames={{ base: 'results-table' }}>
            <DataGridContainer>
              <DataGridTable />
            </DataGridContainer>
          </DataGrid>
        </div>
      )}

      <AddInstrumentDialog open={addInstrumentOpen} onClose={() => setAddInstrumentOpen(false)} onAdded={load} />
      <AddCitationDialog open={addCitationFor !== null} onClose={() => setAddCitationFor(null)} onAdded={load} instrument={addCitationFor} />
      <AddCategoryDialog open={addCategoryFor !== null} onClose={() => setAddCategoryFor(null)} onAdded={load} citation={addCategoryFor} />
      <AddElementDialog open={addElementFor !== null} onClose={() => setAddElementFor(null)} onAdded={load} category={addElementFor} />

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
