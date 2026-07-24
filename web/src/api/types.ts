export type DNSServer = {
  id: number
  isp: string
  name: string
  address: string
  protocol: 'udp' | 'dot' | 'doh'
  created_at: string
}

export type ScanResult = {
  id: number
  scan_run_id: number
  url: string
  dns_server_id: number
  dns_server: DNSServer
  compliant: boolean
  resolved_ip: string
  resolved_ipv6: string
  resolved_asn: number
  resolved_org: string
  resolved_netname: string
  resolved_abuse_email: string
  screenshot_url: string
  error: string
  latency_ms: number
  scanned_at: string
}

export type ScanRun = {
  id: number
  triggered_by: string
  status: 'running' | 'completed' | 'failed'
  started_at: string
  completed_at: string | null
}

export type ScanStatus = { status: 'idle' } | ScanRun

export type GroupedResult = {
  url: string
  hostname: string
  results: ScanResult[]
  violationCount: number
  totalCount: number
  latestScannedAt: string
}

export type URLEntry = { id: number; url: string; enabled: boolean; ordered_at?: string; created_at: string }

export type Department = { id: number; name: string; created_at: string }

export type User = {
  id: number
  username: string
  is_admin: boolean
  is_dept_admin: boolean
  department_id?: number
  department?: Department
  created_at: string
}

export type CompliantIP = { id: number; address: string; note: string; created_at: string }

export type DailyComplianceStat = {
  dns_server_id: number
  dns_server_name: string
  day: string // YYYY-MM-DD
  total: number
  compliant: number
  level: number
}

export type ISPServerStat = {
  dns_server: DNSServer
  compliant: number
  total: number
  avg_latency_ms: number
  min_latency_ms: number
  max_latency_ms: number
}

export type ISPStats = {
  isp: string
  servers: ISPServerStat[]
  most_violated_domain: string
}

export type ISPTrendStat = {
  day: string       // YYYY-MM-DD
  total: number
  compliant: number
}

export type DomainTiming = {
  domain: string
  days_to_block: number
  blocked: boolean
}

export type ISPTiming = {
  isp: string
  median_days_to_block: number
  avg_days_to_block: number
  blocked_count: number
  still_open_count: number
  with_order_date_count: number
  total_domains: number
  slowest: DomainTiming[]
}

export type ResurfacedServerEntry = {
  dns_server_id: number
  dns_server_name: string
  isp: string
  last_compliant_at: string  // RFC3339
  resurfaced_at: string      // RFC3339
}

export type ResurfacedDomain = {
  url: string
  resurfaced_at: string      // RFC3339, most recent across affected_servers
  affected_servers: ResurfacedServerEntry[]
}

// One row of GET /api/domains — a lifetime (not just-latest-run) aggregate
// per domain, used by the Domain page's browseable history table.
export type DomainSummary = {
  url: string
  total_scans: number
  compliant_scans: number
  last_scanned_at: string  // RFC3339
}

export type DomainSummariesResponse = {
  domains: DomainSummary[]
  total: number
}

// One row of GET /api/domains/*url — a single DNS server's lifetime
// aggregate for one domain, used by the Domain page's expanded-row breakdown.
export type DomainServerSummary = {
  dns_server_id: number
  dns_server_name: string
  isp: string
  total_scans: number
  compliant_scans: number
  last_scanned_at: string  // RFC3339
}
