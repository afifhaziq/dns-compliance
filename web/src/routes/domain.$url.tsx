import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { ChevronLeftIcon, ChevronRightIcon, RefreshCwIcon, CopyIcon, CheckIcon, Image as ImageIcon } from 'lucide-react'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { StatusDot, EmptyIcon } from '@/components/results-table-parts'
import { relativeTime } from '@/lib/relative-time'
import { fetchDnsServers } from '../api/dns-servers'
import { fetchDnsRecords } from '../api/dns-records'
import type { DnsRecordSet, DnsRecordsResponse } from '../api/dns-records'
import { fetchDomainInfo, refreshDomainInfo, refreshHostingInfo, faviconApiUrl } from '../api/domain'
import type { DomainInfo } from '../api/domain'
import { fetchSubdomains, refreshSubdomains } from '../api/subdomains'
import type { SubdomainScan } from '../api/subdomains'
import { fetchHeatmapByUrlAndYear, fetchResultsByUrl } from '../api/results'
import type { DailyComplianceStat, DNSServer, ScanResult } from '../api/types'
import { getCachedDnsRecords, setCachedDnsRecords } from '@/lib/dns-records-cache'
import { cn } from '@/lib/utils'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'
import { Combobox, ComboboxInput, ComboboxContent, ComboboxEmpty, ComboboxList, ComboboxItem } from '@/components/ui/b-combobox'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/motion/tabs'
import { HeatmapChart } from '@/components/charts/heatmap'
import { HeatmapChartLoading } from '@/components/charts/heatmap/heatmap-chart-loading'
import { HeatmapCells } from '@/components/charts/heatmap/heatmap-cells'
import { HeatmapXAxis } from '@/components/charts/heatmap/heatmap-x-axis'
import { HeatmapYAxis } from '@/components/charts/heatmap/heatmap-y-axis'
import { HeatmapTooltip } from '@/components/charts/heatmap/heatmap-tooltip'
import { HeatmapLegend } from '@/components/charts/heatmap/heatmap-legend'
import {
  aggregateStatsByDay,
  buildYearHeatmapColumns,
  compliancePercentFromStats,
  dateKey,
  HEATMAP_LEVEL_COLORS,
  heatmapYearColorScale,
} from '@/lib/heatmap-year'

export const Route = createFileRoute('/domain/$url')({
  validateSearch: (search: Record<string, unknown>): { tab: 'history' | 'overview'; server?: string } => ({
    tab: search.tab === 'history' ? 'history' : 'overview',
    server: typeof search.server === 'string' ? search.server : undefined,
  }),
  component: URLHistoryPage,
})

type StatusFilter = 'all' | 'violations' | 'compliant'

type ScanGroup = {
  scanRunId: number
  scannedAt: string
  results: ScanResult[]
}

const PAGE_SIZE = 25
const SUBDOMAIN_PAGE_SIZE = 20

const DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

const DNS_RECORD_LABELS: ReadonlyArray<readonly [keyof DnsRecordSet, string]> = [
  ['a', 'A'],
  ['aaaa', 'AAAA'],
  ['cname', 'CNAME'],
  ['mx', 'MX'],
  ['txt', 'TXT'],
  ['ns', 'NS'],
]

function DnsRecordsPanel({
  data,
  loading,
}: {
  data: DnsRecordsResponse | null
  loading: boolean
}) {
  const hasRecords = data?.resolved && data.records

  return (
    <div className="dash-section dns-records-panel">
      {loading ? (
        <div className="dns-records-grid">
          {DNS_RECORD_LABELS.map(([key]) => (
            <div key={key} className="dns-record-block">
              <span className="skeleton" style={{ width: 60, height: 11 }} />
              <span
                className="skeleton"
                style={{ width: 120, height: 14, marginTop: 6 }}
              />
            </div>
          ))}
        </div>
      ) : !hasRecords ? (
        <p className="dns-records-error">
          Unable to resolve DNS records for this host.
        </p>
      ) : (
        <div className="dns-records-grid">
          {DNS_RECORD_LABELS.map(([key, label]) => {
            const values = data.records?.[key] ?? []

            return (
              <div key={key} className="dns-record-block">
                <span className="dns-record-type">{label}</span>

                {values.length ? (
                  <span className="dns-record-values">
                    {values.map((value, index) => (
                      <span key={index} className="ip-value">
                        {value}
                      </span>
                    ))}
                  </span>
                ) : (
                  <span className="empty-cell">—</span>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

const DOMAIN_INFO_LABELS: ReadonlyArray<readonly [keyof DomainInfo, string]> = [
  ['registrar', 'Registrar'],
  ['registrar_url', 'Registrar URL'],
  ['registrar_abuse_email', 'Registrar Abuse Email'],
  ['registrar_abuse_phone', 'Registrar Abuse Phone'],
  ['domain_created', 'Created'],
  ['domain_expires', 'Expires'],
  ['last_fetched_at', 'Last refreshed'],
]

type HostingInfo = { ip: string; asn: number; org: string; netname: string; abuseEmail: string }

function DomainInfoPanel({
  data,
  loading,
  hosting,
}: {
  data: DomainInfo | null
  loading: boolean
  hosting: HostingInfo | null
}) {
  const hasInfo = data?.fetched

  return (
    <div className="dash-section dns-records-panel domain-info-panel">
      {loading ? (
        <div className="dns-records-grid">
          {DOMAIN_INFO_LABELS.map(([key]) => (
            <div key={key} className="dns-record-block">
              <span className="skeleton" style={{ width: 60, height: 11, marginLeft: 'auto' }} />
              <span
                className="skeleton"
                style={{ width: 120, height: 14, marginTop: 6, marginLeft: 'auto' }}
              />
            </div>
          ))}
        </div>
      ) : !hasInfo ? (
        <p className="dns-records-error">
          No WHOIS data fetched for this domain yet.
        </p>
      ) : (
        <>
          <p className="dash-label-subheading text-right">Domain Registrar</p>
          <div className="dns-records-grid">
            {DOMAIN_INFO_LABELS.map(([key, label]) => {
              const value = data[key]
              const display = key === 'domain_created' || key === 'domain_expires' || key === 'last_fetched_at'
                ? (value ? DATE_FMT.format(new Date(value as string)) : null)
                : (value as string | undefined)

              return (
                <div key={key} className="dns-record-block">
                  <span className="dns-record-type">{label}</span>
                  {display ? (
                    <span className="ip-value">{display}</span>
                  ) : (
                    <span className="empty-cell">—</span>
                  )}
                </div>
              )
            })}
          </div>
          {data.fetch_error && (
            <p className="dns-records-error">{data.fetch_error}</p>
          )}
        </>
      )}

      {hosting && (
        <div className="mt-4">
          <p className="dash-label-subheading text-right">Domain Hosting</p>
          <div className="dns-records-grid">
            <div className="dns-record-block">
              <span className="dns-record-type">Hosting Provider</span>
              {hosting.org ? <span className="ip-value">{hosting.org}</span> : <span className="empty-cell">—</span>}
            </div>
            <div className="dns-record-block">
              <span className="dns-record-type">ASN</span>
              {hosting.asn > 0 ? <span className="ip-value">AS{hosting.asn}</span> : <span className="empty-cell">—</span>}
            </div>
            <div className="dns-record-block">
              <span className="dns-record-type">NetName</span>
              {hosting.netname ? <span className="ip-value">{hosting.netname}</span> : <span className="empty-cell">—</span>}
            </div>
            <div className="dns-record-block">
              <span className="dns-record-type">Hosting Email</span>
              {hosting.abuseEmail ? <span className="ip-value">{hosting.abuseEmail}</span> : <span className="empty-cell">—</span>}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}


function HistorySkeletonRows() {
  return (
    <>
      {[180, 140, 220, 160].map((w, i) => (
        <TableRow key={i} className="skeleton-row">
          <TableCell className="col-expand" />
          <TableCell className="col-scan-id" />
          <TableCell className="col-domain"><span className="skeleton" style={{ width: w, height: 14 }} /></TableCell>
          <TableCell className="col-status"><span className="skeleton" style={{ width: 100, height: 20, borderRadius: 4 }} /></TableCell>
          <TableCell className="col-ip" />
          <TableCell className="col-error" />
          <TableCell className="col-evidence" />
          <TableCell className="col-last-scanned" />
        </TableRow>
      ))}
    </>
  )
}

const currentYear = new Date().getFullYear()

const heatmapTooltipDateFmt = new Intl.DateTimeFormat('en-US', {
  month: 'long',
  day: 'numeric',
  year: 'numeric',
})

function URLHistoryPage() {
  const { url } = Route.useParams()
  const { tab, server } = Route.useSearch()
  const hostname = useMemo(() => { try { return new URL(url).hostname } catch { return url } }, [url])

  const [results, setResults] = useState<ScanResult[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  // Deep-linked from e.g. the Domain page's per-server breakdown
  // (?tab=history&server=<name>), which pre-selects this dropdown instead of
  // defaulting to "all servers".
  const [dnsFilter, setDnsFilter] = useState<string>(() => server ?? 'all')
  const [page, setPage] = useState(1)

  const load = useCallback(async () => {
    try {
      setError(null)
      const raw = await fetchResultsByUrl(url)
      setResults(raw)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load results')
    } finally {
      setLoading(false)
    }
  }, [url])

  useEffect(() => { load() }, [load])

  const dnsServers = useMemo(() => {
    const seen = new Map<string, string>()
    for (const r of results) seen.set(r.dns_server.name, r.dns_server.name)
    return Array.from(seen.values()).sort()
  }, [results])

  const [selectedYear, setSelectedYear] = useState(currentYear)
  const [heatmapStats, setHeatmapStats] = useState<DailyComplianceStat[]>([])
  const [yearLoading, setYearLoading] = useState(true)
  const [dnsServerList, setDnsServerList] = useState<DNSServer[]>([])
  const [dnsServerListLoading, setDnsServerListLoading] = useState(true)

  const [dnsRecords, setDnsRecords] = useState<DnsRecordsResponse | null>(null)
  const [dnsRecordsLoading, setDnsRecordsLoading] = useState(true)

  const [domainInfo, setDomainInfo] = useState<DomainInfo | null>(null)
  const [domainInfoLoading, setDomainInfoLoading] = useState(true)
  const [domainInfoRefreshing, setDomainInfoRefreshing] = useState(false)

  const handleRefreshDomainInfo = useCallback(async () => {
    setDomainInfoRefreshing(true)
    try {
      setDomainInfo(await refreshDomainInfo(url))
    } catch {
      // ponytail: keep the stale cached info on a failed refresh rather than
      // clearing it — the fetch_error field already surfaces server-side
      // failures when the refresh does succeed.
    } finally {
      setDomainInfoRefreshing(false)
    }
  }, [url])

  const [subdomains, setSubdomains] = useState<SubdomainScan | null>(null)
  const [subdomainsLoading, setSubdomainsLoading] = useState(true)
  const [subdomainsRefreshing, setSubdomainsRefreshing] = useState(false)
  const [subdomainPage, setSubdomainPage] = useState(1)
  const [subdomainsCopied, setSubdomainsCopied] = useState(false)

  const handleCopySubdomains = useCallback(async () => {
    const list = subdomains?.subdomains ?? []
    if (list.length === 0) return
    await navigator.clipboard.writeText(list.join('\n'))
    setSubdomainsCopied(true)
    setTimeout(() => setSubdomainsCopied(false), 1500)
  }, [subdomains])

  const handleRefreshSubdomains = useCallback(async () => {
    setSubdomainsRefreshing(true)
    try {
      setSubdomains(await refreshSubdomains(url))
      setSubdomainPage(1)
    } catch {
      // ponytail: same as domain-info refresh — keep the stale cached list
      // rather than clearing it on a failed refresh.
    } finally {
      setSubdomainsRefreshing(false)
    }
  }, [url])

  const loadYear = useCallback(async (year: number) => {
    try {
      setYearLoading(true)
      const raw = await fetchHeatmapByUrlAndYear(url, year)
      setHeatmapStats(raw)
    } catch {
      setHeatmapStats([])
    } finally {
      setYearLoading(false)
    }
  }, [url])

  useEffect(() => { loadYear(selectedYear) }, [loadYear, selectedYear])

  // Drives which heatmap sections render — the configured DNS server list
  // (not the scan results) so the count is known before any scan data for
  // this URL/year has loaded. dnsServerListLoading keeps the section (and
  // its skeleton) visible while this fetch is still in flight, instead of
  // hiding everything until it resolves.
  useEffect(() => {
    fetchDnsServers()
      .then(setDnsServerList)
      .catch(() => setDnsServerList([]))
      .finally(() => setDnsServerListLoading(false))
  }, [])

  const heatmapDnsServers = useMemo(
    () => dnsServerList.map(s => s.name).sort(),
    [dnsServerList],
  )

  const isps = useMemo(
    () => Array.from(new Set(dnsServerList.map(s => s.isp))).sort(),
    [dnsServerList],
  )

  const ispItems = useMemo(
    () => [{ value: 'overall', label: 'Overall' }, ...isps.map(isp => ({ value: isp, label: isp }))],
    [isps],
  )

  const [ispFilter, setIspFilter] = useState<string>('overall')

  // Servers to render heatmaps for: every server when viewing "Overall",
  // or just the servers belonging to the selected ISP.
  const ispHeatmapServers = useMemo(
    () => (ispFilter === 'overall'
      ? heatmapDnsServers
      : dnsServerList.filter(s => s.isp === ispFilter).map(s => s.name).sort()),
    [dnsServerList, heatmapDnsServers, ispFilter],
  )

  // Most recently resolved scan result across all DNS servers — the domain's
  // current hosting/ASN info, independent of the separate domain-WHOIS fetch.
  const hosting = useMemo(() => {
    const resolved = results
      .filter(r => r.resolved_ip)
      .sort((a, b) => b.scanned_at.localeCompare(a.scanned_at))[0]
    if (!resolved) return null
    return {
      ip: resolved.resolved_ip,
      asn: resolved.resolved_asn,
      org: resolved.resolved_org,
      netname: resolved.resolved_netname,
      abuseEmail: resolved.resolved_abuse_email,
    }
  }, [results])

  // Overrides `hosting` with a freshly re-fetched IPInfo row once the user
  // clicks refresh — `hosting` itself is derived from ScanResult rows, which
  // freeze ASN/org/netname/abuse-email at insert time and don't reflect a
  // manual IPInfo cache refresh until a new scan happens.
  const [hostingOverride, setHostingOverride] = useState<HostingInfo | null>(null)
  const [hostingRefreshing, setHostingRefreshing] = useState(false)
  const displayHosting = hostingOverride ?? hosting

  const hostingIP = hosting?.ip
  const handleRefreshHosting = useCallback(async () => {
    if (!hostingIP) return
    setHostingRefreshing(true)
    try {
      const res = await refreshHostingInfo(hostingIP)
      setHostingOverride({ ip: res.ip, asn: res.asn, org: res.org, netname: res.netname, abuseEmail: res.abuse_email })
    } catch {
      // ponytail: same as domain-info refresh — keep the stale data rather
      // than clearing it on a failed refresh.
    } finally {
      setHostingRefreshing(false)
    }
  }, [hostingIP])

  const overallStatsByDay = useMemo(() => aggregateStatsByDay(heatmapStats), [heatmapStats])
  const overallPct = useMemo(() => compliancePercentFromStats(heatmapStats), [heatmapStats])

  // Fetches once per page visit; a sessionStorage cache (keyed by hostname,
  // not full URL) means revisiting this page later in the same tab skips
  // the network call entirely — these records don't change fast enough to
  // need re-fetching on every visit.
  useEffect(() => {
    const cached = getCachedDnsRecords(hostname)
    if (cached) {
      setDnsRecords(cached)
      setDnsRecordsLoading(false)
      return
    }
    setDnsRecordsLoading(true)
    fetchDnsRecords(url)
      .then(data => {
        setDnsRecords(data)
        setCachedDnsRecords(hostname, data)
      })
      .catch(() => setDnsRecords({ hostname, resolved: false }))
      .finally(() => setDnsRecordsLoading(false))
  }, [url, hostname])

  useEffect(() => {
    setDomainInfoLoading(true)
    fetchDomainInfo(url)
      .then(setDomainInfo)
      .catch(() => setDomainInfo({ fetched: false }))
      .finally(() => setDomainInfoLoading(false))
  }, [url])

  // Reads whatever subfinder result is already cached (populated on
  // watchlist-add) — never triggers a fresh run itself; only the refresh
  // button does that.
  useEffect(() => {
    setSubdomainsLoading(true)
    fetchSubdomains(url)
      .then(setSubdomains)
      .catch(() => setSubdomains({ fetched: false }))
      .finally(() => setSubdomainsLoading(false))
  }, [url])

  const filtered = useMemo(() => {
    return results.filter(r => {
      if (dnsFilter !== 'all' && r.dns_server.name !== dnsFilter) return false
      if (statusFilter === 'violations' && r.compliant) return false
      if (statusFilter === 'compliant' && !r.compliant) return false
      return true
    })
  }, [results, statusFilter, dnsFilter])

  const groups = useMemo(() => {
    const byRun = new Map<number, ScanGroup>()
    for (const r of filtered) {
      let g = byRun.get(r.scan_run_id)
      if (!g) {
        g = { scanRunId: r.scan_run_id, scannedAt: r.scanned_at, results: [] }
        byRun.set(r.scan_run_id, g)
      }
      g.results.push(r)
    }
    return Array.from(byRun.values())
  }, [filtered])

  const totalPages = Math.max(1, Math.ceil(groups.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const paginatedGroups = useMemo(
    () => groups.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE),
    [groups, currentPage],
  )

  const subdomainList = useMemo(() => subdomains?.subdomains ?? [], [subdomains])
  const subdomainTotalPages = Math.max(1, Math.ceil(subdomainList.length / SUBDOMAIN_PAGE_SIZE))
  const subdomainCurrentPage = Math.min(subdomainPage, subdomainTotalPages)
  const paginatedSubdomains = useMemo(
    () => subdomainList.slice((subdomainCurrentPage - 1) * SUBDOMAIN_PAGE_SIZE, subdomainCurrentPage * SUBDOMAIN_PAGE_SIZE),
    [subdomainList, subdomainCurrentPage],
  )

  const [expandedRuns, setExpandedRuns] = useState<Set<number>>(new Set())
  const toggleRun = useCallback((scanRunId: number) => {
    setExpandedRuns(prev => {
      const next = new Set(prev)
      if (next.has(scanRunId)) next.delete(scanRunId)
      else next.add(scanRunId)
      return next
    })
  }, [])

  const yearNav = (
    <div className="heatmap-year-nav pr-5">
      <button
        type="button"
        className="heatmap-year-nav-btn"
        onClick={() => setSelectedYear(y => y - 1)}
        aria-label="Previous year"
      >
        <ChevronLeftIcon className="w-4 h-4" />
      </button>
      <span className="heatmap-year-label">{selectedYear}</span>
      <button
        type="button"
        className="heatmap-year-nav-btn"
        onClick={() => setSelectedYear(y => y + 1)}
        disabled={selectedYear >= currentYear}
        aria-label="Next year"
      >
        <ChevronRightIcon className="w-4 h-4" />
      </button>
    </div>
  )

  return (
    <div className="mx-60 mb-10">
      <Breadcrumbs items={[{ label: 'Overview', to: '/' }, { label: 'Results', to: '/results' }, { label: hostname }]} />

      <div className="page-header">
        <div>
        <h1 className="page-title flex-end items-end gap-3">
          <img src={faviconApiUrl(hostname)} alt="" width={16} height={16} className="shrink-0" onError={e => { e.currentTarget.style.visibility = 'hidden' }} />
          <div>
          {hostname}
          </div>
        </h1>
        </div>
        <div className="page-subtitle">{url} · Last 7 days</div>
        {!dnsRecordsLoading && dnsRecords?.resolver_ip && (
          <p className="dns-records-resolver ml-auto">
            Looked up via host DNS resolver {dnsRecords.resolver_ip}
          </p>
        )}
      </div>

      <Tabs defaultValue={tab} variant="underline">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="history">History</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
      {(dnsServerListLoading || heatmapDnsServers.length > 0) && (
        <div className="dash-section">
          {dnsServerListLoading ? (
            <div className="heatmap-server-block">
              <div className="heatmap-server-header">
                <p className="dash-label">&nbsp;</p>
                {yearNav}
              </div>
              <HeatmapChartLoading
                data={buildYearHeatmapColumns(selectedYear, new Map())}
                gap={3}
                cornerRadius={999}
                label="Loading compliance history"
              />
            </div>
          ) : (
            <>
              <div className="heatmap-server-header mb-4">
                {isps.length > 0 ? (
                  <Combobox
                    items={ispItems}
                    value={ispItems.find(i => i.value === ispFilter) ?? null}
                    onValueChange={item => setIspFilter(item ? item.value : 'overall')}
                  >
                    <ComboboxInput aria-label="Filter heatmap by ISP" showClear={false} size="sm" className="w-48" />
                    <ComboboxContent>
                      <ComboboxEmpty>No ISPs found.</ComboboxEmpty>
                      <ComboboxList>
                        {(item: { value: string; label: string }) => (
                          <ComboboxItem key={item.value} value={item}>{item.label}</ComboboxItem>
                        )}
                      </ComboboxList>
                    </ComboboxContent>
                  </Combobox>
                ) : (
                  <p className="dash-label">Overall</p>
                )}
                {yearNav}
              </div>

              {ispFilter === 'overall' ? (
                (() => {
                  const columns = buildYearHeatmapColumns(selectedYear, overallStatsByDay)

                  return (
                    <div className="heatmap-server-block">
                      {yearLoading ? (
                    <HeatmapChartLoading data={columns} gap={3} cornerRadius={999} label="Loading compliance history" />
                  ) : (
                    <>
                      <HeatmapChart data={columns} gap={3} layout="fluid" levelColors={HEATMAP_LEVEL_COLORS}>
                        <HeatmapCells cornerRadius={999} />
                        <HeatmapXAxis />
                        <HeatmapYAxis />
                        <HeatmapTooltip
                          formatLabel={(_count, date) => {
                            const stat = overallStatsByDay.get(dateKey(date))
                            if (!stat) return `No scans · ${heatmapTooltipDateFmt.format(date)}`
                            return `${stat.compliant} of ${stat.total} compliant · ${heatmapTooltipDateFmt.format(date)}`
                          }}
                        />
                      </HeatmapChart>
                      <div className="heatmap-legend-row pl-10 pr-5 my-2">
                        <HeatmapLegend
                          align="start"
                          cornerRadius={999}
                          gap={3}
                          lessLabel="Compliant"
                          moreLabel="More violations"
                          colorScale={heatmapYearColorScale}
                        />
                        {overallPct !== null && (
                          <span className="heatmap-compliance-badge">{overallPct}% compliant</span>
                        )}
                      </div>
                    </>
                  )}
                    </div>
                  )
                })()
              ) : ispHeatmapServers.map(name => {
                const serverStats = heatmapStats.filter(s => s.dns_server_name === name)
                const statsByDay = new Map(serverStats.map(s => [s.day, s]))
                const columns = buildYearHeatmapColumns(selectedYear, statsByDay)
                const pct = compliancePercentFromStats(serverStats)

                return (
                  <div key={name} className="heatmap-server-block">
                    <div className="heatmap-server-header">
                      <p className="dash-label">{name}</p>
                    </div>

                    {yearLoading ? (
                      <HeatmapChartLoading data={columns} gap={3} cornerRadius={999} label="Loading compliance history" />
                    ) : (
                      <>
                        <HeatmapChart data={columns} gap={3} layout="fluid" levelColors={HEATMAP_LEVEL_COLORS}>
                          <HeatmapCells cornerRadius={999} />
                          <HeatmapXAxis />
                          <HeatmapYAxis />
                          <HeatmapTooltip
                            formatLabel={(_count, date) => {
                              const stat = statsByDay.get(dateKey(date))
                              if (!stat) return `No scans · ${heatmapTooltipDateFmt.format(date)}`
                              return `${stat.compliant} of ${stat.total} compliant · ${heatmapTooltipDateFmt.format(date)}`
                            }}
                          />
                        </HeatmapChart>
                        {/* Rendered as a sibling, not a HeatmapChart child: HeatmapLegend
                            renders a plain <div>, and the chart places its children inside
                            an <svg><g> — nesting it there puts the div in the SVG namespace,
                            where browsers don't lay it out (it silently doesn't render). */}
                        <div className="heatmap-legend-row pl-10 pr-6 my-2">
                          <HeatmapLegend
                            align="start"
                            cornerRadius={999}
                            gap={3}
                            lessLabel="Compliant"
                            moreLabel="More violations"
                            colorScale={heatmapYearColorScale}
                          />
                          {pct !== null && (
                            <span className="heatmap-compliance-badge">{pct}% compliant</span>
                          )}
                        </div>
                      </>
                    )}
                  </div>
                )
              })}
            </>
          )}
        </div>
      )}

      <div className="grid grid-cols-2 gap-x-10">
        <div className="dash-section">
          <p className="section-title mb-3">DNS info</p>
          <DnsRecordsPanel data={dnsRecords} loading={dnsRecordsLoading} />
        </div>

        <div className="dash-section">
          <div className="flex items-center justify-end gap-2 mb-3">
            <button
              type="button"
              className="heatmap-year-nav-btn"
              onClick={() => { handleRefreshDomainInfo(); handleRefreshHosting() }}
              disabled={domainInfoLoading || domainInfoRefreshing || hostingRefreshing}
              aria-label="Refresh domain info"
              title="Refresh WHOIS and hosting data"
            >
              <RefreshCwIcon className={cn('w-3.5 h-3.5', (domainInfoRefreshing || hostingRefreshing) && 'animate-spin')} />
            </button>
            <p className="section-title">Domain info</p>
          </div>
          <DomainInfoPanel data={domainInfo} loading={domainInfoLoading} hosting={displayHosting} />
        </div>
      </div>

      <div className="dash-section mt-6">
        <div className="flex items-start justify-between mb-3">
          <div className="items-start gap-2">
            <div className="flex flex-row">
              <p className="section-title">Subdomains</p>
              <div className="flex ml-2">
              <button
                type="button"
                className="heatmap-year-nav-btn"
                onClick={handleCopySubdomains}
                disabled={!subdomains?.subdomains?.length}
                aria-label="Copy all subdomains"
                title="Copy all subdomains (newline-separated)"
              >
                {subdomainsCopied ? <CheckIcon className="w-3.5 h-3.5" /> : <CopyIcon className="w-3.5 h-3.5" />}
              </button>
              
              <button
              type="button"
              className="heatmap-year-nav-btn"
              onClick={handleRefreshSubdomains}
              disabled={subdomainsLoading || subdomainsRefreshing}
              aria-label="Refresh subdomains"
              title="Re-run subdomain enumeration"
              >
              <RefreshCwIcon className={cn('w-3.5 h-3.5', subdomainsRefreshing && 'animate-spin')} />
              </button>
              </div>
              
            </div>
            {subdomains?.fetched && subdomains.subdomains && (
                <span className="dash-label">{subdomains.subdomains.length} found</span>
              )}
            
            
          </div>
          
        </div>

        <div className="results-wrap">
          {!subdomainsLoading && !subdomainsRefreshing && subdomainList.length === 0 ? (
            <div className="empty-state">
              <EmptyIcon />
              <p className="empty-heading">
                {subdomains?.fetch_error ? 'Enumeration failed' : 'No subdomains found yet'}
              </p>
              <p className="empty-body">
                {subdomains?.fetch_error ?? 'Click refresh to run an enumeration.'}
              </p>
            </div>
          ) : (
            <>
              <Table className="results-table" aria-label={`Subdomains for ${hostname}`}>
                <TableHeader>
                  <TableRow>
                    <TableHead className="col-domain th-left" scope="col">Subdomain</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {subdomainsLoading || subdomainsRefreshing ? (
                    [180, 140, 220].map((w, i) => (
                      <TableRow key={i} className="skeleton-row">
                        <TableCell className="col-domain"><span className="skeleton" style={{ width: w, height: 14 }} /></TableCell>
                      </TableRow>
                    ))
                  ) : (
                    paginatedSubdomains.map(sub => (
                      <TableRow key={sub}>
                        <TableCell className="col-domain">
                          <a href={`https://${sub}`} target="_blank" rel="noopener noreferrer" className="hostname">
                            {sub}
                          </a>
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
              {!subdomainsLoading && subdomainTotalPages > 1 && (
                <div className="pagination">
                  <span className="pagination-label">Page {subdomainCurrentPage} of {subdomainTotalPages}</span>
                  <button
                    type="button"
                    className="pagination-btn"
                    onClick={() => setSubdomainPage(p => p - 1)}
                    disabled={subdomainCurrentPage <= 1}
                    aria-label="Previous page"
                  >
                    <ChevronLeftIcon className="w-4 h-4" />
                  </button>
                  <button
                    type="button"
                    className="pagination-btn"
                    onClick={() => setSubdomainPage(p => p + 1)}
                    disabled={subdomainCurrentPage >= subdomainTotalPages}
                    aria-label="Next page"
                  >
                    <ChevronRightIcon className="w-4 h-4" />
                  </button>
                </div>
              )}
            </>
          )}
        </div>
      </div>
        </TabsContent>

        <TabsContent value="history">
      <div className="filter-bar">
        <span className="filter-label" id="status-label">Status</span>
        <ToggleGroup
          type="single"
          value={statusFilter}
          onValueChange={v => { if (v) { setStatusFilter(v as StatusFilter); setPage(1) } }}
          variant="outline"
          aria-labelledby="status-label"
        >
          <ToggleGroupItem value="all">All</ToggleGroupItem>
          <ToggleGroupItem value="violations">Violations</ToggleGroupItem>
          <ToggleGroupItem value="compliant">Compliant</ToggleGroupItem>
        </ToggleGroup>

        {dnsServers.length > 1 && (
          <>
            <span className="filter-label" id="dns-label">DNS Server</span>
            <Select value={dnsFilter} onValueChange={v => { setDnsFilter(v); setPage(1) }}>
              <SelectTrigger aria-labelledby="dns-label" />
              <SelectContent>
                <SelectItem index={0} value="all">All servers</SelectItem>
                {dnsServers.map((name, i) => (
                  <SelectItem key={name} index={i + 1} value={name}>{name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </>
        )}
      </div>

      <div className="results-wrap -mx-30 mt-5">
        {error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : !loading && results.length === 0 ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No results yet</p>
            <p className="empty-body">No scans for this domain in the last 7 days.</p>
          </div>
        ) : (
          <>
            <Table className="results-table" aria-label={`Scan history for ${hostname}`}>
              <TableBody>
                {loading ? (
                  <HistorySkeletonRows />
                ) : filtered.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8}>
                      <div className="empty-state" style={{ padding: '3rem 0' }}>
                        <p className="empty-heading">No results match the current filters</p>
                      </div>
                    </TableCell>
                  </TableRow>
                ) : (
                  paginatedGroups.map(g => {
                    const expanded = expandedRuns.has(g.scanRunId)
                    const violations = g.results.filter(r => !r.compliant).length
                    const compliantCount = g.results.length - violations

                    return (
                      <Fragment key={g.scanRunId}>
                        <TableRow
                          className="scan-group-row"
                          onClick={() => toggleRun(g.scanRunId)}
                          role="button"
                          tabIndex={0}
                          aria-expanded={expanded}
                          onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleRun(g.scanRunId) } }}
                        >
                          <TableCell className="col-expand">
                            <ChevronRightIcon className={cn('scan-group-chevron', expanded && 'expanded')} />
                          </TableCell>
                          <TableCell className="col-scan-id">#{g.scanRunId}</TableCell>
                          <TableCell colSpan={6}>
                            <div className="scan-group-header">
                              <span className="scan-group-time" title={g.scannedAt ? new Date(g.scannedAt).toLocaleString() : undefined}>
                                {g.scannedAt ? relativeTime(g.scannedAt) : '—'}
                              </span>
                              <span className="scan-group-summary">
                                {violations > 0 && <span className="label-violation">{violations} {violations === 1 ? 'violation' : 'violations'}</span>}
                                {violations > 0 && compliantCount > 0 && ', '}
                                {compliantCount > 0 && <span className="label-compliant">{compliantCount} compliant</span>}
                              </span>
                            </div>
                          </TableCell>
                        </TableRow>
                        {expanded && (
                          <TableRow className="scan-group-subheader">
                            <TableHead className="col-expand" scope="col" />
                            <TableHead className="col-scan-id th-left" scope="col" />
                            <TableHead className="col-domain th-left" scope="col">DNS Server</TableHead>
                            <TableHead className="col-status th-left" scope="col">Status</TableHead>
                            <TableHead className="col-ip th-left" scope="col">Resolved IP</TableHead>
                            <TableHead className="col-error th-left" scope="col">Error</TableHead>
                            <TableHead className="col-evidence th-left" scope="col">Evidence</TableHead>
                            <TableHead className="col-last-scanned th-left" scope="col">Scanned At</TableHead>
                          </TableRow>
                        )}
                        {expanded && g.results.map(r => (
                          <TableRow key={r.id} className={`sub-row${!r.compliant ? ' violation-row' : ''}`}>
                            <TableCell className="col-expand" />
                            <TableCell className="col-scan-id" />
                            <TableCell className="col-domain"><span className="dns-name">{r.dns_server.name}</span></TableCell>
                            <TableCell className="col-status"><StatusDot compliant={r.compliant} /></TableCell>
                            <TableCell className="col-ip">
                              <div className="ip-meta">
                                {r.resolved_ip ? <span className="ip-value">{r.resolved_ip}</span> : <span className="empty-cell" aria-label="Not resolved">—</span>}
                                {r.resolved_ipv6 && <span className="ip-meta-secondary">{r.resolved_ipv6}</span>}
                                {r.resolved_asn > 0 && (
                                  <span className="ip-meta-secondary">
                                    AS{r.resolved_asn}{r.resolved_org && ` — ${r.resolved_org}`}
                                  </span>
                                )}
                                {r.resolved_netname && <span className="ip-meta-secondary">{r.resolved_netname}</span>}
                              </div>
                            </TableCell>
                            <TableCell className="col-error">
                              {r.error ? (
                                <span className="col-error-text" title={r.error}>{r.error}</span>
                              ) : (
                                <span className="empty-cell">—</span>
                              )}
                            </TableCell>
                            <TableCell className="col-evidence text-center">
                              {r.screenshot_url ? (
                                <a
                                  href={r.screenshot_url}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="screenshot-icon-btn"
                                  aria-label={`View screenshot for ${r.dns_server.name}`}
                                  title="View screenshot"
                                >
                                  <ImageIcon className="screenshot-icon" aria-hidden="true" />
                                </a>
                              ) : <span className="empty-cell" aria-label="No screenshot">—</span>}
                            </TableCell>
                            <TableCell className="col-last-scanned">
                              {r.scanned_at ? (
                                <span title={new Date(r.scanned_at).toLocaleString()}>{relativeTime(r.scanned_at)}</span>
                              ) : (
                                <span className="empty-cell">—</span>
                              )}
                            </TableCell>
                          </TableRow>
                        ))}
                      </Fragment>
                    )
                  })
                )}
              </TableBody>
            </Table>
            {!loading && totalPages > 1 && (
              <div className="pagination">
                <span className="pagination-label">Page {currentPage} of {totalPages}</span>
                <button
                  type="button"
                  className="pagination-btn"
                  onClick={() => setPage(p => p - 1)}
                  disabled={currentPage <= 1}
                  aria-label="Previous page"
                >
                  <ChevronLeftIcon className="w-4 h-4" />
                </button>
                <button
                  type="button"
                  className="pagination-btn"
                  onClick={() => setPage(p => p + 1)}
                  disabled={currentPage >= totalPages}
                  aria-label="Next page"
                >
                  <ChevronRightIcon className="w-4 h-4" />
                </button>
              </div>
            )}
          </>
        )}
      </div>
        </TabsContent>
      </Tabs>
    </div>
  )
}
