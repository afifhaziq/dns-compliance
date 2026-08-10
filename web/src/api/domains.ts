import { api } from './client'
import type { DomainSummariesResponse, DomainServerSummary } from './types'

export type DomainSummaryFilter = {
  search?: string
  // dnsServerIds/statuses match ANY of the listed values, or NONE of them
  // when the paired *Exclude flag is set — the "is any of"/"is not any of"
  // filter operators (web/src/components/scan-filter-bar.tsx).
  dnsServerIds?: number[]
  dnsServerExclude?: boolean
  statuses?: ('compliant' | 'violations')[]
  statusExclude?: boolean
}

export async function fetchDomainSummaries(
  page: number,
  pageSize: number,
  filter?: DomainSummaryFilter,
): Promise<DomainSummariesResponse> {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  if (filter?.search) params.set('q', filter.search)
  for (const id of filter?.dnsServerIds ?? []) params.append('dns_server_id', String(id))
  if (filter?.dnsServerIds?.length) params.set('dns_server_op', filter.dnsServerExclude ? 'not_any' : 'any')
  for (const status of filter?.statuses ?? []) params.append('status', status)
  if (filter?.statuses?.length) params.set('status_op', filter.statusExclude ? 'not_any' : 'any')
  return api.get<DomainSummariesResponse>(`/domains?${params.toString()}`)
}

export async function fetchDomainServerSummaries(url: string): Promise<DomainServerSummary[]> {
  const data = await api.get<DomainServerSummary[]>(`/domains/${encodeURIComponent(url)}`)
  return Array.isArray(data) ? data : []
}
