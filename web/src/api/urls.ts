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

export type DepartmentURLFields = {
  due_date?: string | null
  agency?: string
  reference_number?: string
  requesting_dept?: string
  status?: string
  requested_at?: string | null
}

// Partial update of the case-metadata fields on one watchlist entry — only
// keys present in `fields` are sent, mirroring the backend's
// UpdateDepartmentURLFields. Pass null on due_date/requested_at to clear them.
export async function setUrlFields(id: number, fields: DepartmentURLFields): Promise<void> {
  const body: Record<string, string> = {}
  if ('due_date' in fields) body.due_date = fields.due_date ?? ''
  if (fields.agency !== undefined) body.agency = fields.agency
  if (fields.reference_number !== undefined) body.reference_number = fields.reference_number
  if (fields.requesting_dept !== undefined) body.requesting_dept = fields.requesting_dept
  if (fields.status !== undefined) body.status = fields.status
  if ('requested_at' in fields) body.requested_at = fields.requested_at ?? ''
  await api.patch<void>(`/urls/${id}`, body)
}
