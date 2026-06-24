import type { DnsRecordsResponse } from '@/api/dns-records'

const KEY_PREFIX = 'dns-records:'

export function getCachedDnsRecords(hostname: string): DnsRecordsResponse | null {
  try {
    const raw = sessionStorage.getItem(KEY_PREFIX + hostname)
    return raw ? JSON.parse(raw) as DnsRecordsResponse : null
  } catch {
    return null
  }
}

export function setCachedDnsRecords(hostname: string, data: DnsRecordsResponse): void {
  try {
    sessionStorage.setItem(KEY_PREFIX + hostname, JSON.stringify(data))
  } catch {
    // sessionStorage unavailable (e.g. browser privacy mode) — caching is a
    // nice-to-have, never required for the feature to function.
  }
}
