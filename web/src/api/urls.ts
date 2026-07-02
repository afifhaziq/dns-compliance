import { api } from './client'
import type { URLEntry } from './types'

export async function fetchUrls(): Promise<URLEntry[]> {
  const data = await api.get<URLEntry[]>('/urls')
  return Array.isArray(data) ? data : []
}

export async function fetchUrlCount(): Promise<number> {
  return (await fetchUrls()).length
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

// orderedAt is an RFC3339 string; pass null to clear a previously set order date.
export async function setUrlOrderedAt(id: number, orderedAt: string | null): Promise<void> {
  await api.patch<void>(`/urls/${id}`, { ordered_at: orderedAt ?? '' })
}
