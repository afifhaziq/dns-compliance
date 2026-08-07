export type DNSServer = {
  id: number
  isp: string
  name: string
  address: string
  protocol: 'udp' | 'dot' | 'doh'
  enabled: boolean
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

export type URLEntry = {
  id: number
  url: string
  enabled: boolean
  due_date?: string
  agency_id?: number
  agency_name?: string
  reference_number?: string
  requesting_dept_id?: number
  requesting_dept_name?: string
  status?: string
  requested_at?: string
  created_at: string
}

export type Agency = { id: number; name: string; created_at: string }

// A "Time to Block" duration option on the watchlist page — admin/dept-admin
// managed, see CLAUDE.md's Notifications section.
export type DueDatePreset = { id: number; label: string; hours: number; created_at: string }

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

export type ISPLogo = { isp: string; logo_url: string; created_at: string }

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

export type ServerUptimeStat = {
  day: string       // YYYY-MM-DD
  up: boolean
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
  with_due_date_count: number
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
  address: string  // IP:port for udp/dot, full URL for doh
  total_scans: number
  compliant_scans: number
  last_scanned_at: string  // RFC3339
}

// Mirrors db.LegalCitationParsed — structured breakdown of a Citation's
// raw_text, either from internal/legalcite.Parse or hand-corrected.
export type LegalCitationParsed = {
  part?: number
  chapter?: number
  provision_num?: number
  provision_suffix?: string
  sub_provision?: number
  sub_provision_suffix?: string
  paragraph?: string
  subparagraph?: string
  sub_subparagraph?: string
  schedule?: number
  schedule_list?: number
}

export type Instrument = {
  id: number
  type: 'ACT' | 'ORDINANCE' | 'ENACTMENT' | 'SUBSIDIARY' | 'CONSTITUTION'
  jurisdiction: string
  number: string
  year?: number
  short_title: string
  created_at: string
}

export type Citation = {
  id: number
  instrument_id: number
  instrument: Instrument
  raw_text: string
  parsed: LegalCitationParsed
  parse_confidence: 'OK' | 'NEEDS_REVIEW'
  created_at: string
}

// Scoped to one Citation, not a shared lookup — see internal/db/models.go's
// Category doc comment.
export type LegalCategory = {
  id: number
  citation_id: number
  citation: Citation
  name: string
  created_at: string
}

// Named LegalElement (not Element) to avoid shadowing the DOM Element type.
export type LegalElement = {
  id: number
  category_id: number
  name: string
  created_at: string
}

// Named LegalSubElement (not SubElement) for the same DOM-shadowing reason
// LegalElement avoids Element.
export type LegalSubElement = {
  id: number
  element_id: number
  name: string
  created_at: string
}

// One row of GET /api/legal/offences/*url — a domain tagged with a specific
// (Category, optional Element, optional SubElement) offence.
export type URLOffence = {
  id: number
  url_id: number
  category_id: number
  category: LegalCategory
  element_id?: number
  element?: LegalElement
  sub_element_id?: number
  sub_element?: LegalSubElement
  recorded_at: string
}

export type Notification = {
  id: number
  department_id: number
  url_id: number
  url: string
  type: 'resurfaced' | 'due_date_reached'
  compliant?: boolean
  details?: Record<string, unknown>
  // scan_run_id is only set for "due_date_reached" (one targeted Trigger
  // call is always exactly one ScanRun); "resurfaced" can span servers
  // checked in different runs, so it only carries scanned_at (the flip time).
  scan_run_id?: number
  scanned_at?: string
  read_at?: string
  created_at: string
}

export type NotificationsResponse = { notifications: Notification[]; total: number }

// GET/PUT /api/grid-preferences/{key} — a user's saved data-grid layout.
// GET returns {} (all fields absent) when nothing has been saved yet.
export type GridPreference = {
  column_visibility?: Record<string, boolean>
  sort_field?: string
  sort_desc?: boolean
  page_size?: number
  updated_at?: string
}
