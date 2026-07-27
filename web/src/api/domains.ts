import { api } from './client'
import type { DomainSummariesResponse, DomainServerSummary } from './types'

export type DomainSummaryFilter = {
  search?: string
  dnsServerId?: number
  status?: 'compliant' | 'violations'
}

export async function fetchDomainSummaries(
  page: number,
  pageSize: number,
  filter?: DomainSummaryFilter,
): Promise<DomainSummariesResponse> {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  if (filter?.search) params.set('q', filter.search)
  if (filter?.dnsServerId) params.set('dns_server_id', String(filter.dnsServerId))
  if (filter?.status) params.set('status', filter.status)
  return api.get<DomainSummariesResponse>(`/domains?${params.toString()}`)
}

export async function fetchDomainServerSummaries(url: string): Promise<DomainServerSummary[]> {
  return api.get<DomainServerSummary[]>(`/domains/${encodeURIComponent(url)}`)
}
