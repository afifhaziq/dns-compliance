export interface DnsRecordSet {
  a: string[]
  aaaa: string[]
  cname: string[]
  mx: string[]
  txt: string[]
  ns: string[]
}

export interface DnsRecordsResponse {
  hostname: string
  resolved: boolean
  resolver_ip?: string
  records?: DnsRecordSet
}

export async function fetchDnsRecords(url: string): Promise<DnsRecordsResponse> {
  const res = await fetch(`/api/dns-records/${encodeURIComponent(url)}`)
  if (!res.ok) throw new Error(`Failed to load DNS records: ${res.status}`)
  return res.json()
}
