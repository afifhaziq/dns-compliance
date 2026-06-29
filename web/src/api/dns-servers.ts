import { api } from './client'
import type { DNSServer } from './types'

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

export async function deleteDnsServer(id: number): Promise<void> {
  await api.delete<void>(`/dns-servers/${id}`)
}
