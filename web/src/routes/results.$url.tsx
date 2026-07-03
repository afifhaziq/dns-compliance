import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { Breadcrumbs } from '@/components/breadcrumbs'
import { fetchDnsServers } from '../api/dns-servers'
import { fetchDnsRecords } from '../api/dns-records'
import type { DnsRecordSet, DnsRecordsResponse } from '../api/dns-records'
import { fetchDomainInfo } from '../api/domain'
import type { DomainInfo } from '../api/domain'
import { fetchHeatmapByUrlAndYear, fetchResultsByUrl } from '../api/results'
import type { DailyComplianceStat, DNSServer, ScanResult } from '../api/types'
import { getCachedDnsRecords, setCachedDnsRecords } from '@/lib/dns-records-cache'
import { cn } from '@/lib/utils'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
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

export const Route = createFileRoute('/results/$url')({ component: URLHistoryPage })

type StatusFilter = 'all' | 'violations' | 'compliant'

type ScanGroup = {
  scanRunId: number
  scannedAt: string
  results: ScanResult[]
}

const PAGE_SIZE = 25

const DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

function EmptyIcon() {
  return (
    <svg className="empty-icon" width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
      <rect x="8" y="4" width="24" height="32" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M32 4L40 12V36C40 37.1 39.1 38 38 38H32" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M40 12H32V4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 18H26M14 24H22" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

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
  ['domain_created', 'Created'],
  ['domain_expires', 'Expires'],
  ['last_fetched_at', 'Last refreshed'],
]

function DomainInfoPanel({
  data,
  loading,
}: {
  data: DomainInfo | null
  loading: boolean
}) {
  const hasInfo = data?.fetched

  return (
    <div className="dash-section dns-records-panel">
      {loading ? (
        <div className="dns-records-grid">
          {DOMAIN_INFO_LABELS.map(([key]) => (
            <div key={key} className="dns-record-block">
              <span className="skeleton" style={{ width: 60, height: 11 }} />
              <span
                className="skeleton"
                style={{ width: 120, height: 14, marginTop: 6 }}
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
    </div>
  )
}

function StatusDot({ compliant }: { compliant: boolean }) {
  return (
    <span className="status-dot-label">
      <span className={`status-dot ${compliant ? 'dot-compliant' : 'dot-violation'}`} aria-hidden="true" />
      <span className={compliant ? 'label-compliant' : 'label-violation'}>
        {compliant ? 'Compliant' : 'Violation'}
      </span>
    </span>
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
  const hostname = useMemo(() => { try { return new URL(url).hostname } catch { return url } }, [url])

  const [results, setResults] = useState<ScanResult[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [dnsFilter, setDnsFilter] = useState<string>('all')
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

  const [ispFilter, setIspFilter] = useState<string>('overall')

  // Servers to render heatmaps for: every server when viewing "Overall",
  // or just the servers belonging to the selected ISP.
  const ispHeatmapServers = useMemo(
    () => (ispFilter === 'overall'
      ? heatmapDnsServers
      : dnsServerList.filter(s => s.isp === ispFilter).map(s => s.name).sort()),
    [dnsServerList, heatmapDnsServers, ispFilter],
  )

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

  const [expandedRuns, setExpandedRuns] = useState<Set<number>>(new Set())
  const toggleRun = useCallback((scanRunId: number) => {
    setExpandedRuns(prev => {
      const next = new Set(prev)
      if (next.has(scanRunId)) next.delete(scanRunId)
      else next.add(scanRunId)
      return next
    })
  }, [])

  return (
    <div className="mx-60 mb-10">
      <Breadcrumbs items={[{ label: 'Overview', to: '/' }, { label: 'Results', to: '/results' }, { label: hostname }]} />

      <div className="page-header px-0">
        <h1 className="page-title mb-2">{hostname}</h1>
        <p className="page-subtitle">{url} · Last 7 days</p>
        {!dnsRecordsLoading && dnsRecords?.resolver_ip && (
          <p className="dns-records-resolver ml-auto">
            Looked up via host DNS resolver {dnsRecords.resolver_ip}
          </p>
        )}
      </div>

      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="history">History</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
      {(dnsServerListLoading || heatmapDnsServers.length > 0) && (
        <div className="dash-section">
          <div className="heatmap-year-nav">
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

          {dnsServerListLoading ? (
            <div className="heatmap-server-block">
              <div className="heatmap-server-header">
                <p className="dash-label">&nbsp;</p>
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
              <div className="heatmap-server-header">
                {isps.length > 0 ? (
                  <Select value={ispFilter} onValueChange={setIspFilter}>
                    <SelectTrigger aria-label="Filter heatmap by ISP" />
                    <SelectContent>
                      <SelectItem index={0} value="overall">Overall</SelectItem>
                      {isps.map((isp, i) => (
                        <SelectItem key={isp} index={i + 1} value={isp}>{isp}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : (
                  <p className="dash-label">Overall</p>
                )}
                {ispFilter === 'overall' && overallPct !== null && !yearLoading && (
                  <span className="heatmap-compliance-badge">{overallPct}% compliant</span>
                )}
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
                      <HeatmapLegend
                        align="start"
                        cornerRadius={999}
                        gap={3}
                        lessLabel="Compliant"
                        moreLabel="More violations"
                        colorScale={heatmapYearColorScale}
                        className='my-2 px-10'
                      />
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
                      {pct !== null && !yearLoading && (
                        <span className="heatmap-compliance-badge">{pct}% compliant</span>
                      )}
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
                        <HeatmapLegend
                          align="start"
                          cornerRadius={999}
                          gap={3}
                          lessLabel="Compliant"
                          moreLabel="More violations"
                          colorScale={heatmapYearColorScale}
                          className='my-2 px-10'
                        />
                      </>
                    )}
                  </div>
                )
              })}
            </>
          )}
        </div>
      )}

      <DnsRecordsPanel data={dnsRecords} loading={dnsRecordsLoading} />

      <div className="dash-section">
        <p className="section-title mb-3">Domain info</p>
        <DomainInfoPanel data={domainInfo} loading={domainInfoLoading} />
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

      <div className="results-wrap">
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
              <TableHeader>
                <TableRow>
                  <TableHead className="col-expand" scope="col" />
                  <TableHead className="col-scan-id th-left" scope="col">Scan ID</TableHead>
                  <TableHead className="col-domain th-left" scope="col">DNS Server</TableHead>
                  <TableHead className="col-status th-left" scope="col">Status</TableHead>
                  <TableHead className="col-ip th-left" scope="col">Resolved IP</TableHead>
                  <TableHead className="col-evidence th-left" scope="col">Evidence</TableHead>
                  <TableHead className="col-last-scanned th-left" scope="col">Scanned At</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading ? (
                  <HistorySkeletonRows />
                ) : filtered.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7}>
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
                          <TableCell colSpan={5}>
                            <div className="scan-group-header">
                              <span className="scan-group-time">
                                {g.scannedAt ? DATE_FMT.format(new Date(g.scannedAt)) : '—'}
                              </span>
                              <span className="scan-group-summary">
                                {violations > 0 && <span className="label-violation">{violations} {violations === 1 ? 'violation' : 'violations'}</span>}
                                {violations > 0 && compliantCount > 0 && ', '}
                                {compliantCount > 0 && <span className="label-compliant">{compliantCount} compliant</span>}
                              </span>
                            </div>
                          </TableCell>
                        </TableRow>
                        {expanded && g.results.map(r => (
                          <TableRow key={r.id} className={!r.compliant ? 'violation-row' : ''}>
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
                              </div>
                            </TableCell>
                            <TableCell className="col-evidence">
                              {r.screenshot_url ? (
                                <a href={r.screenshot_url} target="_blank" rel="noopener noreferrer" className="screenshot-link" aria-label={`View screenshot for ${r.dns_server.name}`}>
                                  View screenshot
                                </a>
                              ) : <span className="empty-cell" aria-label="No screenshot">—</span>}
                            </TableCell>
                            <TableCell className="col-last-scanned">
                              {r.scanned_at ? <span>{DATE_FMT.format(new Date(r.scanned_at))}</span> : <span className="empty-cell">—</span>}
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
