import { api } from './client'

// Mirrors internal/server/handlers.go's domainInfoResponse — {fetched:false}
// when the domain is owned but has no cached WHOIS row yet, otherwise the
// flattened db.DomainWhois fields alongside fetched:true.
export type DomainInfo = {
  fetched: boolean
  url_id?: number
  registrar?: string
  domain_created?: string | null
  domain_expires?: string | null
  last_fetched_at?: string
  fetch_error?: string
}

export function fetchDomainInfo(url: string): Promise<DomainInfo> {
  return api.get<DomainInfo>(`/domain/${encodeURIComponent(url)}`)
}
