import { api } from './client'
import type { Case, CaseLetter } from './types'

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
