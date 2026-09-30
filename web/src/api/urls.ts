import { api } from './client'
import type { URLEntry } from './types'

export async function fetchUrls(): Promise<URLEntry[]> {
  const data = await api.get<URLEntry[]>('/urls')
  return Array.isArray(data) ? data : []
}

export type UrlListQuery = {
  page: number
  pageSize: number
  q?: string
  status?: string
  workflowStatus?: string
  agencyId?: string
  deptId?: string
  created?: { op: string; from?: string; to?: string }
  due?: { op: string; from?: string; to?: string }
  sort?: 'url' | 'created_at' | 'due_date'
  desc?: boolean
}

// One server-side page of the Domain view (GET /api/urls/page) — same query
// shape as fetchCaseSummariesPage. Admin gets every department's watchlist.
export async function fetchUrlsPage(query: UrlListQuery): Promise<{ urls: URLEntry[]; total: number }> {
  const p = new URLSearchParams({ page: String(query.page), page_size: String(query.pageSize) })
  const set = (k: string, v?: string) => { if (v) p.set(k, v) }
  set('q', query.q?.trim()); set('status', query.status); set('workflow_status', query.workflowStatus); set('agency_id', query.agencyId); set('dept_id', query.deptId)
  for (const [key, f] of [['created', query.created], ['due', query.due]] as const) {
    if (!f) continue
    set(`${key}_op`, f.op); set(`${key}_from`, f.from); set(`${key}_to`, f.to)
  }
  if (query.sort) { p.set('sort', query.sort); p.set('dir', query.desc ? 'desc' : 'asc') }
  const data = await api.get<{ urls: URLEntry[]; total: number }>(`/urls/page?${p}`)
  return { urls: data?.urls ?? [], total: data?.total ?? 0 }
}

// Only needs the total, so ask for a one-row page instead of the full list.
export async function fetchUrlCount(): Promise<number> {
  return (await fetchUrlsPage({ page: 1, pageSize: 1 })).total
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
