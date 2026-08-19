import { api } from './client'
import type { Case, CaseLetter, CaseLettersResponse } from './types'

export async function listCases(url: string): Promise<Case[]> {
  const data = await api.get<Case[]>(`/cases/${encodeURIComponent(url)}`)
  // Go nil slice -> JSON null, see web/CLAUDE.md — applies both to the top-level
  // array and to each case's `letters` (a case with no letters yet marshals as null).
  return (Array.isArray(data) ? data : []).map(c => ({ ...c, letters: c.letters ?? [] }))
}

export function createCase(url: string, phase: string): Promise<Case> {
  return api.post<Case>(`/cases/${encodeURIComponent(url)}`, { phase })
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
