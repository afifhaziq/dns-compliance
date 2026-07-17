import { api } from './client'

// Mirrors internal/server/handlers.go's domainInfoResponse — {fetched:false}
// when the domain is owned but has no cached WHOIS row yet, otherwise the
// flattened db.DomainWhois fields alongside fetched:true.
export type DomainInfo = {
  fetched: boolean
  url_id?: number
  registrar?: string
  registrar_url?: string
  registrar_abuse_email?: string
  registrar_abuse_phone?: string
  domain_created?: string | null
  domain_expires?: string | null
  last_fetched_at?: string
  fetch_error?: string
}

export function fetchDomainInfo(url: string): Promise<DomainInfo> {
  return api.get<DomainInfo>(`/domain/${encodeURIComponent(url)}`)
}

export function refreshDomainInfo(url: string): Promise<DomainInfo> {
  return api.post<DomainInfo>(`/domain/${encodeURIComponent(url)}`, undefined)
}

// Mirrors internal/server/handlers.go's RefreshHostingInfo response — the
// flattened db.IPInfo row it just (re)fetched and cached for this IP,
// bypassing IPInfo's normal fetch-once-ever cache.
export type HostingRefreshResult = {
  ip: string
  asn: number
  org: string
  netname: string
  abuse_email: string
  fetched_at: string
  fetch_error?: string
}

export function refreshHostingInfo(ip: string): Promise<HostingRefreshResult> {
  return api.post<HostingRefreshResult>(`/hosting/${encodeURIComponent(ip)}`, undefined)
}

// Server-cached favicon endpoint (GET /api/favicon/*url) — for use directly
// as an <img src>, not via api.get, since the response is raw image bytes.
// Fetches and caches the domain's favicon server-side on first request, so
// the browser never has to contact the domain (or Google's favicon proxy)
// directly — see db.Favicon's comment for why that matters for this app.
export function faviconApiUrl(domain: string): string {
  return `/api/favicon/${encodeURIComponent(domain)}`
}
