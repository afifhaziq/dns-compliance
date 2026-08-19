import { useEffect, useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'
import { listCases, createCase, addCaseLetter } from '@/api/cases'
import type { Case } from '@/api/types'

const PHASE_OPTIONS = [
  { value: 'requested', label: 'Requested' },
  { value: 'uplift', label: 'Uplift' },
  { value: 'suspended', label: 'Suspended' },
]

// Mirrors the four letter types CMOD tracks (docs/cmod-blocking-list-migration-clarifications.md);
// CRD-sourced cases default to 'Notice' per docs/db-schema.dbml's resolved design note.
const LETTER_TYPE_OPTIONS = ['Notice', 'Memo', 'Notice (Uplift)', 'Memo (Uplift)']

export function CaseHistoryDialog({ open, onClose, url }: { open: boolean; onClose: () => void; url: string }) {
  const [cases, setCases] = useState<Case[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [newPhase, setNewPhase] = useState('requested')
  const [letterType, setLetterType] = useState('Notice')
  const [referenceNumber, setReferenceNumber] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true)
    setError(null)
    listCases(url)
      .then(c => { if (!cancelled) setCases(c) })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load cases') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [open, url])

  const handleAddCase = async () => {
    setSubmitting(true)
    setError(null)
    try {
      const c = await createCase(url, newPhase)
      // A case has no number of its own — it lives on the first letter
      // (case_letters.reference_number), so open it in the same step.
      await addCaseLetter(c.id, {
        type: letterType,
        reference_number: referenceNumber.trim() || undefined,
      })
      setReferenceNumber('')
      setCases(await listCases(url))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create case')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => !v && onClose()}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 480 }}>
        <DialogHeader>
          <DialogTitle>Case History</DialogTitle>
          <DialogDescription>{url}</DialogDescription>
        </DialogHeader>

        {loading ? (
          <p className="text-sm text-stone-muted">Loading…</p>
        ) : cases.length === 0 ? (
          <p className="text-sm text-stone-muted mb-2">No cases yet.</p>
        ) : (
          <ul className="case-history-list">
            {cases.map(c => (
              <li key={c.id} className="case-history-item">
                <strong>Case #{c.id}</strong> — {PHASE_OPTIONS.find(o => o.value === c.phase)?.label ?? c.phase}
                {c.letters.length > 0 && (
                  <ul className="case-history-letters">
                    {c.letters.map(l => (
                      <li key={l.id}>{l.type}{l.reference_number ? ` — ${l.reference_number}` : ''}</li>
                    ))}
                  </ul>
                )}
              </li>
            ))}
          </ul>
        )}

        {error && <p className="form-error">{error}</p>}

        <div className="form-field">
          <label className="form-label" id="new-case-phase-label">Open New Case</label>
          <Select value={newPhase} onValueChange={setNewPhase} disabled={submitting}>
            <SelectTrigger aria-labelledby="new-case-phase-label" placeholder="—" className="w-full" />
            <SelectContent>
              {PHASE_OPTIONS.map((opt, i) => (
                <SelectItem key={opt.value} index={i} value={opt.value}>{opt.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="form-row">
          <div className="form-field">
            <label className="form-label" id="new-case-letter-type-label">Letter Type</label>
            <Select value={letterType} onValueChange={setLetterType} disabled={submitting}>
              <SelectTrigger aria-labelledby="new-case-letter-type-label" placeholder="—" className="w-full" />
              <SelectContent>
                {LETTER_TYPE_OPTIONS.map((opt, i) => (
                  <SelectItem key={opt} index={i} value={opt}>{opt}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="new-case-reference-number">Case / Reference No.</label>
            <input
              id="new-case-reference-number"
              className="form-input"
              placeholder="e.g. MCMC(S)CMOD/BLK/2026(1-2)"
              value={referenceNumber}
              onChange={e => setReferenceNumber(e.target.value)}
              disabled={submitting}
            />
          </div>
        </div>

        <DialogFooter>
          <button type="button" className="btn-ghost" onClick={onClose} disabled={submitting}>
            Close
          </button>
          <button type="button" className="btn-primary" onClick={handleAddCase} disabled={submitting}>
            {submitting ? 'Opening…' : 'Open New Case'}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
