import { api } from './client'
import type { URLEntry } from './types'

export async function fetchUrls(): Promise<URLEntry[]> {
  const data = await api.get<URLEntry[]>('/urls')
  return Array.isArray(data) ? data : []
}

export async function fetchUrlCount(): Promise<number> {
  return (await fetchUrls()).length
}

export async function createUrl(url: string, departmentId?: number): Promise<URLEntry> {
  return api.post<URLEntry>('/urls', { url, department_id: departmentId })
}

export async function deleteUrl(id: number): Promise<void> {
  await api.delete<void>(`/urls/${id}`)
}
