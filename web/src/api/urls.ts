import { api } from './client'
import type { URLEntry } from './types'

export async function fetchUrls(): Promise<URLEntry[]> {
  const data = await api.get<URLEntry[]>('/urls')
  return Array.isArray(data) ? data : []
}

export async function fetchUrlCount(): Promise<number> {
  return (await fetchUrls()).length
}

// Watchlist additions since the start of the current calendar month — not a
// rolling 30-day window.
export async function fetchUrlsRequestedThisMonth(): Promise<number> {
  const data = await api.get<{ count: number }>('/urls/requested-count')
  return data.count
}

export async function createUrl(url: string): Promise<URLEntry> {
  return api.post<URLEntry>('/urls', { url })
}

export async function deleteUrl(id: number): Promise<void> {
  await api.delete<void>(`/urls/${id}`)
}

export async function setUrlEnabled(id: number, enabled: boolean): Promise<void> {
  await api.patch<void>(`/urls/${id}`, { enabled })
}

export type URLCaseFields = {
  due_date?: string | null
  agency_id?: number | null
  reference_number?: string
  requesting_dept_id?: number | null
  status?: string
  requested_at?: string | null
}

// Partial update of a URL's case-metadata fields (global per domain, not
// per department — see backend db.URL's doc comment) — only keys present in
// `fields` are sent, mirroring the backend's UpdateURLCaseFields. Pass null
// to clear a field: due_date/requested_at clear via "", agency_id/
// requesting_dept_id clear via 0 (never a real row id).
export async function setUrlFields(id: number, fields: URLCaseFields): Promise<void> {
  const body: Record<string, string | number> = {}
  if (fields.due_date !== undefined) body.due_date = fields.due_date ?? ''
  if (fields.agency_id !== undefined) body.agency_id = fields.agency_id ?? 0
  if (fields.reference_number !== undefined) body.reference_number = fields.reference_number
  if (fields.requesting_dept_id !== undefined) body.requesting_dept_id = fields.requesting_dept_id ?? 0
  if (fields.status !== undefined) body.status = fields.status
  if (fields.requested_at !== undefined) body.requested_at = fields.requested_at ?? ''
  await api.patch<void>(`/urls/${id}`, body)
}
