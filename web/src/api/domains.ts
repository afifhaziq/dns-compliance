import { api } from './client'
import type { DomainSummariesResponse, DomainServerSummary } from './types'

export async function fetchDomainSummaries(page: number, pageSize: number): Promise<DomainSummariesResponse> {
  return api.get<DomainSummariesResponse>(`/domains?page=${page}&page_size=${pageSize}`)
}

export async function fetchDomainServerSummaries(url: string): Promise<DomainServerSummary[]> {
  return api.get<DomainServerSummary[]>(`/domains/${encodeURIComponent(url)}`)
}
