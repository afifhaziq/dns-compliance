import { api } from './client'

// Mirrors internal/server/handlers.go's subdomainScanResponse — {fetched:false}
// when the domain is owned but has no cached SubdomainScan row yet (never
// fetched, or subfinder is disabled server-side), otherwise the flattened
// db.SubdomainScan fields alongside fetched:true.
export type SubdomainScan = {
  fetched: boolean
  url_id?: number
  subdomains?: string[]
  fetched_at?: string
  fetch_error?: string
}

export function fetchSubdomains(url: string): Promise<SubdomainScan> {
  return api.get<SubdomainScan>(`/subdomains/${encodeURIComponent(url)}`)
}

export function refreshSubdomains(url: string): Promise<SubdomainScan> {
  return api.post<SubdomainScan>(`/subdomains/${encodeURIComponent(url)}`, undefined)
}
