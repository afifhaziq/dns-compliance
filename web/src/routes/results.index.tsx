import { useCallback, useEffect, useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { HistoryIcon } from 'lucide-react'
import { fetchResults, groupResults, lastScanTime } from '../api/results'
import type { GroupedResult, ScanResult } from '../api/types'
import { useScan } from './__root'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardImage,
} from '@/components/animate-ui/components/base/preview-link-card'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'

export const Route = createFileRoute('/results/')({ component: ResultsPage })

/* ─── Types ──────────────────────────────────────────────────────────────── */

type StatusFilter = 'all' | 'violations' | 'compliant'

/* ─── Helpers ────────────────────────────────────────────────────────────── */

function relativeTime(isoString: string): string {
  const diff = (Date.now() - new Date(isoString).getTime()) / 1000
  const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
  if (diff < 60) return rtf.format(-Math.round(diff), 'second')
  if (diff < 3600) return rtf.format(-Math.round(diff / 60), 'minute')
  if (diff < 86400) return rtf.format(-Math.round(diff / 3600), 'hour')
  return rtf.format(-Math.round(diff / 86400), 'day')
}

/* ─── Chevron Icon ───────────────────────────────────────────────────────── */

function ChevronRight({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      width="12"
      height="12"
      viewBox="0 0 12 12"
      fill="none"
      aria-hidden="true"
    >
      <path d="M4.5 2.5L7.5 6L4.5 9.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

/* ─── Empty Icon ─────────────────────────────────────────────────────────── */

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

/* ─── Status Dot ─────────────────────────────────────────────────────────── */

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

/* ─── Sub-rows (expanded DNS results) ───────────────────────────────────── */

function SubRows({ results, visible }: { results: ScanResult[]; visible: boolean }) {
  if (!visible) return null
  return (
    <>
      {results.map(r => (
        <TableRow key={r.id} className={`sub-row${!r.compliant ? ' violation-row' : ''}`}>
          <TableCell className="col-expand" />
          <TableCell className="col-domain">
            <span className="dns-name">{r.dns_server.name}</span>
            {r.error && (
              <span
                style={{ marginLeft: 8, fontSize: '0.75rem', color: 'var(--violation-text)' }}
                title={r.error}
              >
                (error)
              </span>
            )}
          </TableCell>
          <TableCell className="col-status">
            <StatusDot compliant={r.compliant} />
          </TableCell>
          <TableCell className="col-ip">
            {r.resolved_ip ? (
              <span className="ip-value">{r.resolved_ip}</span>
            ) : (
              <span className="empty-cell" aria-label="Not resolved">—</span>
            )}
          </TableCell>
          <TableCell className="col-evidence">
            {r.screenshot_url ? (
              <a
                href={r.screenshot_url}
                target="_blank"
                rel="noopener noreferrer"
                className="screenshot-link"
                aria-label={`View screenshot for ${r.dns_server.name}`}
              >
                View screenshot
              </a>
            ) : (
              <span className="empty-cell" aria-label="No screenshot">—</span>
            )}
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
    </>
  )
}

/* ─── URL Group Row ──────────────────────────────────────────────────────── */

function URLGroupRow({
  group,
  expanded,
  onToggle,
}: {
  group: GroupedResult
  expanded: boolean
  onToggle: () => void
}) {
  const { violationCount, totalCount, hostname, url } = group
  const allCompliant = violationCount === 0
  const summaryLabel = allCompliant
    ? `All ${totalCount} compliant`
    : `${violationCount} of ${totalCount} non-compliant`

  return (
    <>
      <TableRow className="url-row" onClick={onToggle} aria-expanded={expanded}>
        <TableCell className="col-expand">
          <button
            className="expand-btn"
            onClick={e => { e.stopPropagation(); onToggle() }}
            aria-expanded={expanded}
            aria-label={`${expanded ? 'Collapse' : 'Expand'} results for ${hostname}`}
            tabIndex={0}
          >
            <ChevronRight className={`expand-icon${expanded ? ' expanded' : ''}`} />
          </button>
        </TableCell>
        <TableCell className="col-domain">
          <PreviewLinkCard href={url}>
            <PreviewLinkCardTrigger>
              <span className="hostname" title={url}>{hostname}</span>
            </PreviewLinkCardTrigger>
            <PreviewLinkCardPanel>
              <PreviewLinkCardImage />
            </PreviewLinkCardPanel>
          </PreviewLinkCard>
        </TableCell>
        <TableCell className="col-status">
          <span className={`summary-chip ${allCompliant ? 'all-compliant' : 'has-violations'}`}>
            {summaryLabel}
          </span>
        </TableCell>
        <TableCell className="col-ip" />
        <TableCell className="col-evidence">
          <Link
            to="/results/$url"
            params={{ url }}
            className="btn-row-history"
            aria-label={`View history for ${hostname}`}
            onClick={e => e.stopPropagation()}
          >
            <HistoryIcon className="btn-row-history-icon" />
          </Link>
        </TableCell>
        <TableCell className="col-last-scanned">
          {group.latestScannedAt ? (
            <span title={new Date(group.latestScannedAt).toLocaleString()}>
              {relativeTime(group.latestScannedAt)}
            </span>
          ) : (
            <span className="empty-cell">—</span>
          )}
        </TableCell>
      </TableRow>
      <SubRows results={group.results} visible={expanded} />
    </>
  )
}

/* ─── Skeleton ───────────────────────────────────────────────────────────── */

function ResultsTableSkeleton() {
  return (
    <>
      {[180, 140, 220, 160, 200].map((w, i) => (
        <TableRow key={i} className="skeleton-row">
          <TableCell className="col-expand">
            <span className="skeleton" style={{ width: 16, height: 16, borderRadius: 3 }} />
          </TableCell>
          <TableCell className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </TableCell>
          <TableCell className="col-status">
            <span className="skeleton" style={{ width: 100, height: 20, borderRadius: 4 }} />
          </TableCell>
          <TableCell className="col-ip" />
          <TableCell className="col-evidence" />
          <TableCell className="col-last-scanned" />
        </TableRow>
      ))}
    </>
  )
}

/* ─── Results Page ───────────────────────────────────────────────────────── */

function ResultsPage() {
  const { scanning, refreshSignal } = useScan()

  const [results, setResults] = useState<ScanResult[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [dnsFilter, setDnsFilter] = useState<string>('all')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const load = useCallback(async () => {
    try {
      setError(null)
      setResults(await fetchResults())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load results')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load, refreshSignal])

  const toggleExpanded = useCallback((url: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(url)) next.delete(url)
      else next.add(url)
      return next
    })
  }, [])

  const groups = useMemo(() => groupResults(results), [results])
  const lastScan = useMemo(() => lastScanTime(groups), [groups])

  const dnsServers = useMemo(() => {
    const seen = new Map<string, string>()
    for (const g of groups) {
      for (const r of g.results) seen.set(r.dns_server.name, r.dns_server.name)
    }
    return Array.from(seen.values()).sort()
  }, [groups])

  const filtered = useMemo(() => {
    return groups
      .map(g => {
        let res = g.results
        if (dnsFilter !== 'all') res = res.filter(r => r.dns_server.name === dnsFilter)
        if (statusFilter === 'violations') res = res.filter(r => !r.compliant)
        else if (statusFilter === 'compliant') res = res.filter(r => r.compliant)
        if (res.length === 0) return null
        const violationCount = res.filter(r => !r.compliant).length
        return { ...g, results: res, violationCount, totalCount: res.length }
      })
      .filter(Boolean) as GroupedResult[]
  }, [groups, statusFilter, dnsFilter])

  return (
    <div className="mx-20">
      <div className="page-header">
        <h1 className="page-title">Compliance Results</h1>
        {!loading && lastScan && (
          <p className="page-subtitle">Last scan: {lastScan}</p>
        )}
      </div>

      {scanning && (
        <div className="scan-banner" role="status" aria-live="polite">
          <span className="scan-banner-dot" aria-hidden="true" />
          Scan in progress — results will update automatically
        </div>
      )}

      {error ? (
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button className="btn-primary" onClick={load}>Retry</button>
        </div>
      ) : (
        <div className="flex flex-col items-stretch w-full gap-4 mt-4">
          <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full">
            <span className="filter-label" id="status-label">Status</span>
            <ToggleGroup
              type="single"
              value={statusFilter}
              size="sm"
              onValueChange={v => { if (v) setStatusFilter(v as StatusFilter) }}
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
                <select
                  className="filter-select"
                  value={dnsFilter}
                  onChange={e => setDnsFilter(e.target.value)}
                  aria-labelledby="dns-label"
                >
                  <option value="all">All servers</option>
                  {dnsServers.map(name => (
                    <option key={name} value={name}>{name}</option>
                  ))}
                </select>
              </>
            )}
          </div>

          <div className="results-wrap w-full">
            {!loading && groups.length === 0 ? (
              <div className="empty-state">
                <EmptyIcon />
                <p className="empty-heading">No results yet</p>
                <p className="empty-body">
                  No scan has been run. Use Run Scan to begin compliance monitoring.
                </p>
              </div>
            ) : (
              <Table className="results-table" aria-label="DNS compliance results">
                <TableHeader>
                  <TableRow>
                    <TableHead className="col-expand" scope="col" />
                    <TableHead className="col-domain th-left" scope="col">Domain</TableHead>
                    <TableHead className="col-status" scope="col">Status</TableHead>
                    <TableHead className="col-ip" scope="col">Resolved IP</TableHead>
                    <TableHead className="col-evidence" scope="col">Evidence</TableHead>
                    <TableHead className="col-last-scanned" scope="col">Last scanned</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {loading ? (
                    <ResultsTableSkeleton />
                  ) : filtered.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={5}>
                        <div className="empty-state" style={{ padding: '3rem 0' }}>
                          <p className="empty-heading">No results match the current filters</p>
                        </div>
                      </TableCell>
                    </TableRow>
                  ) : (
                    filtered.map(group => (
                      <URLGroupRow
                        key={group.url}
                        group={group}
                        expanded={expanded.has(group.url)}
                        onToggle={() => toggleExpanded(group.url)}
                      />
                    ))
                  )}
                </TableBody>
              </Table>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
