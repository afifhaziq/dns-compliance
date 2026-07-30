import { api } from './client'
import type { DNSServer, ServerUptimeStat } from './types'

export async function fetchDnsServers(): Promise<DNSServer[]> {
  const data = await api.get<DNSServer[]>('/dns-servers')
  return Array.isArray(data) ? data : []
}

export async function fetchDnsServerCount(): Promise<number> {
  return (await fetchDnsServers()).length
}

export async function createDnsServer(data: {
  isp: string
  name: string
  address: string
  protocol: DNSServer['protocol']
}): Promise<DNSServer> {
  return api.post<DNSServer>('/dns-servers', data)
}

export async function updateDnsServer(id: number, data: {
  isp: string
  name: string
  address: string
  protocol: DNSServer['protocol']
}): Promise<DNSServer> {
  return api.patch<DNSServer>(`/dns-servers/${id}`, data)
}

export async function deleteDnsServer(id: number): Promise<void> {
  await api.delete<void>(`/dns-servers/${id}`)
}

export async function setDnsServerEnabled(id: number, enabled: boolean): Promise<void> {
  await api.patch<void>(`/dns-servers/${id}/enabled`, { enabled })
}

export async function fetchServerUptime(id: number, sinceDays = 30): Promise<ServerUptimeStat[]> {
  const since = new Date(Date.now() - sinceDays * 24 * 60 * 60 * 1000).toISOString()
  return api.get<ServerUptimeStat[]>(`/dns-servers/${id}/uptime?since=${encodeURIComponent(since)}`)
}

export interface DnsServerTestResult {
  success: boolean
  ip?: string
  latency_ms?: number
  error?: string
}

export async function testDnsServer(address: string, protocol: DNSServer['protocol']): Promise<DnsServerTestResult> {
  return api.post<DnsServerTestResult>('/dns-servers/test', { address, protocol })
}
