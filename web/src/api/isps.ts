import { api, type BlobDownload } from './client'
import type { ISPStats, ISPTiming, ISPTrendStat, ResurfacedPage, UnblockedPage } from './types'

export async function fetchISPStats(isp: string): Promise<ISPStats> {
  return api.get<ISPStats>(`/isps/${encodeURIComponent(isp)}`)
}

export async function fetchISPTrend(isp: string, sinceDays = 30): Promise<ISPTrendStat[]> {
  const since = new Date(Date.now() - sinceDays * 24 * 60 * 60 * 1000).toISOString()
  const data = await api.get<ISPTrendStat[]>(`/isps/${encodeURIComponent(isp)}/trend?since=${encodeURIComponent(since)}`)
  return Array.isArray(data) ? data : []
}

export async function fetchISPTiming(isp: string): Promise<ISPTiming> {
  return api.get<ISPTiming>(`/isps/${encodeURIComponent(isp)}/timing`)
}

export type UnblockedQuery = {
  since: Date
  until: Date
  q?: string
  sort?: 'days_open' | 'url' | 'notice_date'
  dir?: 'asc' | 'desc'
  page?: number
  pageSize?: number
}

function unblockedParams({ since, until, q, sort, dir, page, pageSize }: UnblockedQuery): string {
  const p = new URLSearchParams({ since: since.toISOString(), until: until.toISOString() })
  if (q) p.set('q', q)
  if (sort) p.set('sort', sort)
  if (dir) p.set('dir', dir)
  if (page) p.set('page', String(page))
  if (pageSize) p.set('page_size', String(pageSize))
  return p.toString()
}

export async function fetchISPUnblocked(isp: string, query: UnblockedQuery): Promise<UnblockedPage> {
  const data = await api.get<UnblockedPage>(`/isps/${encodeURIComponent(isp)}/unblocked?${unblockedParams(query)}`)
  return { items: data.items ?? [], total: data.total }
}

// Every row in scope, one per (domain, DNS server) — ignores paging.
export function exportISPUnblocked(isp: string, since: Date, until: Date): Promise<BlobDownload> {
  return api.getBlob(`/isps/${encodeURIComponent(isp)}/unblocked/export?${unblockedParams({ since, until })}`)
}

// One ISP's resurfaced domains, newest flip first, paged server-side.
export async function fetchISPResurfaced(isp: string, page: number, pageSize: number): Promise<ResurfacedPage> {
  const data = await api.get<ResurfacedPage>(`/isps/${encodeURIComponent(isp)}/resurfaced?page=${page}&page_size=${pageSize}`)
  return { items: data.items ?? [], total: data.total }
}

// Every ISP in one workbook: Summary, Matrix, then one sheet per ISP.
export function exportAllISPUnblocked(since: Date, until: Date): Promise<BlobDownload> {
  return api.getBlob(`/unblocked/export?${unblockedParams({ since, until })}`)
}
