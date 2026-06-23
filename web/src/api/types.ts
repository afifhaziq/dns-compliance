export type DNSServer = {
  id: number
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
  screenshot_url: string
  error: string
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

export type URLEntry = { id: number; url: string; created_at: string }

export type DailyComplianceStat = {
  dns_server_id: number
  dns_server_name: string
  day: string // YYYY-MM-DD
  total: number
  compliant: number
  level: number
}
