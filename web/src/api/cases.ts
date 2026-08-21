import { api } from './client'
import type { Case, CaseLetter, CaseLettersResponse, CaseSummary } from './types'

export async function listCases(url: string): Promise<Case[]> {
  const data = await api.get<Case[]>(`/cases/${encodeURIComponent(url)}`)
  // Go nil slice -> JSON null, see web/CLAUDE.md — applies both to the top-level
  // array and to each case's `letters` (a case with no letters yet marshals as null).
  return (Array.isArray(data) ? data : []).map(c => ({ ...c, letters: c.letters ?? [] }))
}

// agencyId/dueDate optionally seed Case.AgencyID/DueDate at creation time —
// omitted keys are left unset server-side (see db.CaseCreateOptions).
export function createCase(
  url: string,
  phase: string,
  opts?: { agencyId?: number; dueDate?: string },
): Promise<Case> {
  const body: Record<string, string | number> = { phase }
  if (opts?.agencyId !== undefined) body.agency_id = opts.agencyId
  if (opts?.dueDate !== undefined) body.due_date = opts.dueDate
  return api.post<Case>(`/cases/${encodeURIComponent(url)}`, body)
}

export type CaseFields = {
  agencyId?: number | null
  status?: string
  dueDate?: string | null
  requestedAt?: string | null
}

// Partial update of a case's shared fields (PATCH /api/cases/{id}) — only
// keys present in `fields` are sent. Pass null to clear a field: due_date/
// requested_at clear via "", agency_id clears via 0 (never a real row id) —
// same clear-sentinel convention the old PATCH /api/urls/{id} used (see
// setUrlFields's prior implementation in urls.ts).
export async function updateCase(caseId: number, fields: CaseFields): Promise<void> {
  const body: Record<string, string | number> = {}
  if (fields.agencyId !== undefined) body.agency_id = fields.agencyId ?? 0
  if (fields.status !== undefined) body.status = fields.status
  if (fields.dueDate !== undefined) body.due_date = fields.dueDate ?? ''
  if (fields.requestedAt !== undefined) body.requested_at = fields.requestedAt ?? ''
  await api.patch<void>(`/cases/${caseId}`, body)
}

// Sets one url's own CaseURL.Phase within a case — the per-domain override
// of Case.status. Takes a numeric urlId (not a raw url string like
// addUrlToCase above) since the route addresses it by path segment
// (`/cases/{id}/urls/{url_id}`), not a body the server resolves — callers
// need the url's id already (e.g. URLEntry.id).
export async function updateCaseURLPhase(caseId: number, urlId: number, phase: string): Promise<void> {
  await api.patch<void>(`/cases/${caseId}/urls/${urlId}`, { phase })
}

export function addCaseLetter(
  caseId: number,
  fields: Partial<Omit<CaseLetter, 'id' | 'case_id' | 'created_at'>>,
): Promise<CaseLetter> {
  return api.post<CaseLetter>(`/cases/${caseId}/letters`, fields)
}

// AddURLToCase — links an additional URL to an already-created case, the
// "N URLs in one Notice" shape createCase alone can't build (it only ever
// links the one URL a case is opened for).
export function addUrlToCase(
  caseId: number,
  url: string,
  phase: string,
): Promise<{ case_id: number; url_id: number; phase: string }> {
  return api.post<{ case_id: number; url_id: number; phase: string }>(`/cases/${caseId}/urls`, { url, phase })
}

export async function fetchCaseLetters(page: number, pageSize: number): Promise<CaseLettersResponse> {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  const data = await api.get<CaseLettersResponse>(`/case-letters?${params.toString()}`)
  // See web/CLAUDE.md — a nil Go slice marshals to JSON null, not [].
  return { letters: Array.isArray(data.letters) ? data.letters : [], total: data.total ?? 0 }
}

// Loads every case_letters row by paging through the (deliberately capped,
// see internal/server/CLAUDE.md) /api/case-letters endpoint, so the Docs
// page can filter/sort/paginate entirely client-side — same shape as
// fetchUrls(), which already does this for the (much larger) urls table.
const CASE_LETTERS_FETCH_PAGE_SIZE = 100

export async function fetchAllCaseLetters(): Promise<CaseLettersResponse['letters']> {
  let page = 1
  let all: CaseLettersResponse['letters'] = []
  for (;;) {
    const res = await fetchCaseLetters(page, CASE_LETTERS_FETCH_PAGE_SIZE)
    all = all.concat(res.letters)
    if (res.letters.length === 0 || all.length >= res.total) break
    page++
  }
  return all
}

export async function fetchCaseSummaries(): Promise<CaseSummary[]> {
  const data = await api.get<CaseSummary[]>('/case-summaries')
  return Array.isArray(data) ? data : []
}

export type CaseLetterFieldsUpdate = Partial<{
  subject: string
  workflowStatus: string
  referenceNumberExternal: string
  referenceNumberInternal: string
  recipient: string
  requestor: string
  remarks: string
  letterDate: string | null
  receivedAt: string | null
  submittedAt: string | null
}>

// Partial update of one CaseLetter's fields (PATCH
// /api/cases/{caseId}/letters/{letterId}) — only keys present in `fields`
// are sent. Date fields clear via null -> "" (same sentinel convention as
// updateCase's dueDate/requestedAt above); string fields clear via "".
export async function updateCaseLetter(caseId: number, letterId: number, fields: CaseLetterFieldsUpdate): Promise<void> {
  const body: Record<string, string> = {}
  if (fields.subject !== undefined) body.subject = fields.subject
  if (fields.workflowStatus !== undefined) body.workflow_status = fields.workflowStatus
  if (fields.referenceNumberExternal !== undefined) body.reference_number_external = fields.referenceNumberExternal
  if (fields.referenceNumberInternal !== undefined) body.reference_number_internal = fields.referenceNumberInternal
  if (fields.recipient !== undefined) body.recipient = fields.recipient
  if (fields.requestor !== undefined) body.requestor = fields.requestor
  if (fields.remarks !== undefined) body.remarks = fields.remarks
  if (fields.letterDate !== undefined) body.letter_date = fields.letterDate ?? ''
  if (fields.receivedAt !== undefined) body.received_at = fields.receivedAt ?? ''
  if (fields.submittedAt !== undefined) body.submitted_at = fields.submittedAt ?? ''
  await api.patch<void>(`/cases/${caseId}/letters/${letterId}`, body)
}
