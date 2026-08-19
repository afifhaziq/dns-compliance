import { api } from './client'
import type { Case, CaseLetter } from './types'

export async function listCases(url: string): Promise<Case[]> {
  const data = await api.get<Case[]>(`/cases/${encodeURIComponent(url)}`)
  return Array.isArray(data) ? data : [] // Go nil slice -> JSON null, see web/CLAUDE.md
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
