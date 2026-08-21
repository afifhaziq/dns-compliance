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
import { listCases, createCase, addCaseLetter, updateCase, updateCaseURLPhase } from '@/api/cases'
import type { Case, Agency, DueDatePreset } from '@/api/types'
import { PHASE_OPTIONS, LETTER_TYPE_OPTIONS } from '@/lib/case-options'

// Case-level status vocabulary — same requested/uplift/suspended values as
// PHASE_OPTIONS, but this is db.Case.status (shared by every URL the case
// covers), unrelated to CaseURL.Phase (this url's own override, edited via
// the Phase select below each case). Kept local rather than imported from
// urls.tsx since it's the one field urls.tsx's own STATUS_OPTIONS list adds
// beyond PHASE_OPTIONS: a blank "—" option.
const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '—' },
  { value: 'requested', label: 'Requested' },
  { value: 'uplift', label: 'Uplift' },
  { value: 'suspended', label: 'Suspended' },
  { value: 'internal', label: 'Internal' },
]

// Same duration-picker convention as urls.tsx's AddUrlDialog/EditUrlDialog —
// case owners pick "the ISP has 24h", not a calendar date; duplicated here
// (rather than importing from the route file) to keep this component
// independent of urls.tsx.
function dueDateOptionsFrom(presets: DueDatePreset[]): { value: string; label: string }[] {
  return [{ value: '', label: '—' }, ...presets.map(p => ({ value: String(p.minutes), label: p.label }))]
}

function dueDateFromDurationMinutes(minutes: number): string {
  return new Date(Date.now() + minutes * 60 * 1000).toISOString()
}

const DUE_DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit',
  timeZone: 'Asia/Kuala_Lumpur',
})

export function CaseHistoryDialog({
  open,
  onClose,
  url,
  urlId,
  agencies,
  duePresets,
}: {
  open: boolean
  onClose: () => void
  url: string
  // The domain's numeric id — needed to target CaseURL.Phase edits
  // (updateCaseURLPhase addresses `/cases/{id}/urls/{url_id}`, not the raw
  // url string). Undefined only while the dialog is closed/target unset;
  // phase edits are hidden until it's available.
  urlId: number | undefined
  agencies: Agency[]
  duePresets: DueDatePreset[]
}) {
  const [cases, setCases] = useState<Case[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [savingCaseId, setSavingCaseId] = useState<number | null>(null)
  const [newPhase, setNewPhase] = useState('requested')
  const [newAgencyId, setNewAgencyId] = useState<number | ''>('')
  const [newDueDurationMinutes, setNewDueDurationMinutes] = useState('1440')
  const [letterType, setLetterType] = useState('Notice')
  const [referenceNumberExternal, setReferenceNumberExternal] = useState('')
  const [referenceNumberInternal, setReferenceNumberInternal] = useState('')
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

  const refresh = () => listCases(url).then(setCases)

  const handleCaseFieldChange = async (caseId: number, fields: Parameters<typeof updateCase>[1]) => {
    setSavingCaseId(caseId)
    setError(null)
    try {
      await updateCase(caseId, fields)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update case')
    } finally {
      setSavingCaseId(null)
    }
  }

  const handleCaseDueDateChange = (caseId: number, durationMinutes: string) => {
    if (!durationMinutes) return
    handleCaseFieldChange(caseId, { dueDate: dueDateFromDurationMinutes(Number(durationMinutes)) })
  }

  const handlePhaseChange = async (caseId: number, phase: string) => {
    if (urlId === undefined) return
    setSavingCaseId(caseId)
    setError(null)
    try {
      await updateCaseURLPhase(caseId, urlId, phase)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update phase')
    } finally {
      setSavingCaseId(null)
    }
  }

  const handleAddCase = async () => {
    setSubmitting(true)
    setError(null)
    try {
      const opts: { agencyId?: number; dueDate?: string } = {}
      if (newAgencyId !== '') opts.agencyId = newAgencyId
      if (newDueDurationMinutes) opts.dueDate = dueDateFromDurationMinutes(Number(newDueDurationMinutes))
      // A case has no number of its own — it lives on the first letter
      // (case_letters.reference_number_external/_internal), so open it in
      // the same step.
      const c = await createCase(url, newPhase, opts)
      await addCaseLetter(c.id, {
        type: letterType,
        reference_number_external: referenceNumberExternal.trim() || undefined,
        reference_number_internal: referenceNumberInternal.trim() || undefined,
      })
      setReferenceNumberExternal('')
      setReferenceNumberInternal('')
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
            {cases.map(c => {
              const saving = savingCaseId === c.id
              return (
                <li key={c.id} className="case-history-item">
                  <div className="flex items-center justify-between">
                    <strong>Case #{c.id}</strong>
                    {saving && <span className="text-xs text-stone-muted">Saving…</span>}
                  </div>

                  <div className="form-row" style={{ marginTop: 8 }}>
                    <div className="form-field">
                      <label className="form-label" id={`case-${c.id}-agency-label`}>Agency</label>
                      <Select
                        value={String(c.agency_id ?? '')}
                        onValueChange={v => handleCaseFieldChange(c.id, { agencyId: v === '' ? null : Number(v) })}
                        disabled={saving}
                      >
                        <SelectTrigger aria-labelledby={`case-${c.id}-agency-label`} placeholder="—" className="w-full" />
                        <SelectContent>
                          <SelectItem index={0} value="">—</SelectItem>
                          {agencies.map((a, i) => (
                            <SelectItem key={a.id} index={i + 1} value={String(a.id)}>{a.name}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>

                    <div className="form-field">
                      <label className="form-label" id={`case-${c.id}-status-label`}>Status</label>
                      <Select
                        value={c.status ?? ''}
                        onValueChange={v => handleCaseFieldChange(c.id, { status: v })}
                        disabled={saving}
                      >
                        <SelectTrigger aria-labelledby={`case-${c.id}-status-label`} placeholder="—" className="w-full" />
                        <SelectContent>
                          {STATUS_OPTIONS.map((opt, i) => (
                            <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>

                    <div className="form-field">
                      <label className="form-label" id={`case-${c.id}-due-label`}>Time to Block</label>
                      <Select
                        value=""
                        onValueChange={v => handleCaseDueDateChange(c.id, v)}
                        disabled={saving}
                      >
                        <SelectTrigger aria-labelledby={`case-${c.id}-due-label`} placeholder="—" className="w-full" />
                        <SelectContent>
                          {dueDateOptionsFrom(duePresets).map((opt, i) => (
                            <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      {c.due_date && (
                        <p className="text-xs text-stone-muted">Deadline: {DUE_DATE_FMT.format(new Date(c.due_date))}</p>
                      )}
                    </div>
                  </div>

                  {urlId !== undefined && (
                    <div className="form-field" style={{ marginTop: 4 }}>
                      <label className="form-label" id={`case-${c.id}-phase-label`}>
                        Phase <span style={{ color: 'var(--stone-muted)', fontWeight: 400 }}>(this domain within this case)</span>
                      </label>
                      <Select value={c.phase} onValueChange={v => handlePhaseChange(c.id, v)} disabled={saving}>
                        <SelectTrigger aria-labelledby={`case-${c.id}-phase-label`} placeholder="—" className="w-full" />
                        <SelectContent>
                          {PHASE_OPTIONS.map((opt, i) => (
                            <SelectItem key={opt.value} index={i} value={opt.value}>{opt.label}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                  )}

                  {c.letters.length > 0 && (
                    <ul className="case-history-letters">
                      {c.letters.map(l => (
                        <li key={l.id}>
                          {l.type}
                          {l.reference_number_external && ` — Ext: ${l.reference_number_external}`}
                          {l.reference_number_internal && ` · Int: ${l.reference_number_internal}`}
                        </li>
                      ))}
                    </ul>
                  )}
                </li>
              )
            })}
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
            <label className="form-label" id="new-case-agency-label">Agency</label>
            <Select value={String(newAgencyId)} onValueChange={v => setNewAgencyId(v === '' ? '' : Number(v))} disabled={submitting}>
              <SelectTrigger aria-labelledby="new-case-agency-label" placeholder="—" className="w-full" />
              <SelectContent>
                <SelectItem index={0} value="">—</SelectItem>
                {agencies.map((a, i) => (
                  <SelectItem key={a.id} index={i + 1} value={String(a.id)}>{a.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="form-field">
            <label className="form-label" id="new-case-due-date-label">Time to Block</label>
            <Select value={newDueDurationMinutes} onValueChange={setNewDueDurationMinutes} disabled={submitting}>
              <SelectTrigger aria-labelledby="new-case-due-date-label" placeholder="—" className="w-full" />
              <SelectContent>
                {dueDateOptionsFrom(duePresets).map((opt, i) => (
                  <SelectItem key={opt.value || 'none'} index={i} value={opt.value}>{opt.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
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
            <label className="form-label" htmlFor="new-case-reference-number-external">External Ref. (No. Rujukan NMD)</label>
            <input
              id="new-case-reference-number-external"
              className="form-input"
              placeholder="e.g. MCMC(S)CMOD/BLK/2026(1-2)"
              value={referenceNumberExternal}
              onChange={e => setReferenceNumberExternal(e.target.value)}
              disabled={submitting}
            />
          </div>
        </div>
        <div className="form-field">
          <label className="form-label" htmlFor="new-case-reference-number-internal">Internal Ref. (No. Rujukan NMSMD)</label>
          <input
            id="new-case-reference-number-internal"
            className="form-input"
            placeholder="MCMC-internal only, never sent externally"
            value={referenceNumberInternal}
            onChange={e => setReferenceNumberInternal(e.target.value)}
            disabled={submitting}
          />
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
