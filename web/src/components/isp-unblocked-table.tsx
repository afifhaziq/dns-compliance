import { Fragment, useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { exportISPUnblocked, fetchISPUnblocked, type UnblockedQuery } from '@/api/isps'
import type { UnblockedDomain } from '@/api/types'
import { downloadBlob } from '@/lib/download'
import { Table, TableBody, TableRow, TableCell, TableHead, TableHeader } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/animate-ui/components/radix/toggle-group'
import { DownloadIcon } from '@/components/animate-ui/icons/download'
import { ChevronRightIcon } from '@/components/animate-ui/icons/chevron-right'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { periodRange, type Period } from '@/lib/period'

const PAGE_SIZE = 50

const fmtDate = (iso?: string) => iso ? new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' }) : '—'
const fmtDateTime = (iso: string) => new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })

type SortKey = NonNullable<UnblockedQuery['sort']>

export function ISPUnblockedTable({
  isp, period, from, to, serverCount, resurfaced, onPeriodChange, onTotal,
}: {
  isp: string
  period: Period
  from?: string
  to?: string
  serverCount: number
  resurfaced: Set<string>
  onPeriodChange: (period: Period, from?: string, to?: string) => void
  onTotal: (total: number | null) => void
}) {
  const [search, setSearch] = useState('')
  const [debounced, setDebounced] = useState('')
  const [sort, setSort] = useState<SortKey>('days_open')
  const [dir, setDir] = useState<'asc' | 'desc'>('desc')
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const [exporting, setExporting] = useState(false)
  const [exportError, setExportError] = useState<string | null>(null)
  const [reloadNonce, setReloadNonce] = useState(0)

  useEffect(() => {
    const t = setTimeout(() => setDebounced(search.trim()), 300)
    return () => clearTimeout(t)
  }, [search])

  // Paging resets whenever the filter changes: a page number only counts
  // while it was set under the current filter key.
  const filterKey = JSON.stringify([period, from, to, debounced, sort, dir])
  const [pageState, setPageState] = useState({ key: filterKey, page: 1 })
  const page = pageState.key === filterKey ? pageState.page : 1
  const setPage = (p: number) => setPageState({ key: filterKey, page: p })

  // loading is derived: the last settled response is for a different request.
  const requestKey = `${isp}|${filterKey}|${page}|${reloadNonce}`
  const [result, setResult] = useState<{ key: string; items: UnblockedDomain[]; total: number; error: string | null }>({ key: '', items: [], total: 0, error: null })
  const loading = result.key !== requestKey
  const { items, total } = result
  const error = result.error ?? exportError

  useEffect(() => {
    let cancelled = false
    const { since, until } = periodRange(period, from, to)
    fetchISPUnblocked(isp, { since, until, q: debounced || undefined, sort, dir, page, pageSize: PAGE_SIZE })
      .then(res => {
        if (cancelled) return
        setResult({ key: requestKey, items: res.items, total: res.total, error: null })
        if (!debounced) onTotal(res.total)
      })
      .catch(err => {
        if (cancelled) return
        setResult({ key: requestKey, items: [], total: 0, error: err instanceof Error ? err.message : 'Failed to load' })
        onTotal(null)
      })
    return () => { cancelled = true }
  }, [isp, period, from, to, debounced, sort, dir, page, requestKey, onTotal])

  const handleExport = async () => {
    setExporting(true)
    setExportError(null)
    try {
      const { since, until } = periodRange(period, from, to)
      const { blob, filename } = await exportISPUnblocked(isp, since, until)
      downloadBlob(blob, filename ?? `unblocked-${isp}-${new Date().toISOString().slice(0, 10)}.xlsx`)
    } catch (err) {
      setExportError(err instanceof Error ? err.message : 'Export failed')
    } finally {
      setExporting(false)
    }
  }

  const toggleSort = (key: SortKey) => {
    if (sort === key) setDir(d => d === 'asc' ? 'desc' : 'asc')
    else { setSort(key); setDir(key === 'url' ? 'asc' : 'desc') }
  }
  const sortHead = (key: SortKey, label: string) => (
    <TableHead scope="col" aria-sort={sort === key ? (dir === 'asc' ? 'ascending' : 'descending') : undefined}>
      <button type="button" className="inline-flex items-center gap-1" onClick={() => toggleSort(key)}>
        {label}{sort === key && <span aria-hidden>{dir === 'asc' ? '↑' : '↓'}</span>}
      </button>
    </TableHead>
  )

  const toggleRow = (id: number) => setExpanded(s => {
    const next = new Set(s)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })

  const pageStart = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1
  const pageEnd = Math.min(page * PAGE_SIZE, total)

  return (
    <div>
      <div className="flex flex-wrap items-center gap-3 mb-3">
        <ToggleGroup
          type="single"
          value={period}
          onValueChange={v => { if (v) onPeriodChange(v as Period, from, to) }}
          variant="outline"
          aria-label="Period"
        >
          <ToggleGroupItem value="week">This week</ToggleGroupItem>
          <ToggleGroupItem value="last-week">Last week</ToggleGroupItem>
          <ToggleGroupItem value="custom">Custom</ToggleGroupItem>
        </ToggleGroup>
        {period === 'custom' && (
          <div className="flex items-center gap-2">
            <Input type="date" aria-label="From" value={from ?? ''} max={to} onChange={e => onPeriodChange('custom', e.target.value, to)} className="w-auto" />
            <span className="dash-label mb-0">to</span>
            <Input type="date" aria-label="To" value={to ?? ''} min={from} onChange={e => onPeriodChange('custom', from, e.target.value)} className="w-auto" />
          </div>
        )}
        <Input
          type="search"
          placeholder="Search domain or reference no."
          aria-label="Search unblocked domains"
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="w-64"
        />
        <Button
          variant="outline"
          className="ml-auto"
          onClick={handleExport}
          disabled={exporting || total === 0}
          title="One row per domain and DNS server, for the whole period"
        >
          <DownloadIcon size={16} />
          {exporting ? 'Exporting…' : 'Export .xlsx'}
        </Button>
      </div>

      {error ? (
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button className="btn-primary" onClick={() => { setExportError(null); setReloadNonce(n => n + 1) }}>Retry</button>
        </div>
      ) : loading && items.length === 0 ? (
        <div className="dash-table-wrap">
          {[1, 2, 3, 4, 5].map(i => (
            <div key={i} className="dash-skeleton-row">
              <span className="skeleton" style={{ width: 200, height: 13 }} />
              <span className="skeleton" style={{ width: 80, height: 13 }} />
              <span className="skeleton" style={{ width: 120, height: 13 }} />
            </div>
          ))}
        </div>
      ) : items.length === 0 ? (
        <div className="dash-table-wrap dash-empty">
          <p className="dash-empty-heading">{debounced ? 'No matching domains' : 'No unblocked domains'}</p>
          <p className="dash-empty-body">
            {debounced
              ? `Nothing in this period matches "${debounced}".`
              : `Every requested domain scanned in this period was blocked by ${isp}, or no scan ran in it.`}
          </p>
        </div>
      ) : (
        <>
          <Table className="server-table" aria-label={`Domains ${isp} has not blocked`} aria-busy={loading}>
            <TableHeader>
              <TableRow>
                {sortHead('url', 'Domain')}
                <TableHead scope="col">Resolving on</TableHead>
                <TableHead scope="col">Resolved IP</TableHead>
                {sortHead('notice_date', 'Notice date')}
                <TableHead scope="col">Due</TableHead>
                {sortHead('days_open', 'Days open')}
                <TableHead scope="col">Scanned</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map(d => {
                const open = expanded.has(d.url_id)
                const ips = Array.from(new Set(d.servers.map(s => s.resolved_ip).filter(Boolean)))
                return (
                  <Fragment key={d.url_id}>
                    <TableRow>
                      <TableCell>
                        <div className="inline-flex items-center gap-2">
                          <button
                            type="button"
                            onClick={() => toggleRow(d.url_id)}
                            aria-expanded={open}
                            aria-label={`${open ? 'Hide' : 'Show'} DNS servers for ${d.url}`}
                            className="text-stone-muted"
                            style={{ transform: open ? 'rotate(90deg)' : undefined, transition: 'transform 150ms cubic-bezier(0.25, 1, 0.5, 1)' }}
                          >
                            <ChevronRightIcon size={14} />
                          </button>
                          <Link to="/domain/$url" params={{ url: d.url }} search={{ tab: 'overview' }} className="ip-value">
                            {d.url}
                          </Link>
                          {resurfaced.has(d.url) && <span className="dash-label mb-0 label-violation">Resurfaced</span>}
                        </div>
                      </TableCell>
                      <TableCell><span className="server-count">{d.servers.length} / {serverCount}</span></TableCell>
                      <TableCell>
                        <span className="ip-value">{ips[0] ?? '—'}</span>
                        {ips.length > 1 && <span className="dash-label mb-0 ml-1">+{ips.length - 1}</span>}
                      </TableCell>
                      <TableCell><span className="server-count">{fmtDate(d.notice_date)}</span></TableCell>
                      <TableCell><span className="server-count">{fmtDate(d.due_date)}</span></TableCell>
                      <TableCell>
                        {d.days_open != null
                          ? <span className="server-count label-violation">{d.days_open}</span>
                          : <span className="empty-cell">—</span>}
                      </TableCell>
                      <TableCell><span className="server-count">{fmtDateTime(d.last_scanned_at)}</span></TableCell>
                    </TableRow>
                    {open && d.servers.map(s => (
                      <TableRow key={s.dns_server_id} className="bg-stone-panel">
                        <TableCell className="pl-10">
                          <span className="server-name">{s.dns_server_name}</span>
                          <span className="ip-value ml-2">{s.dns_server_address}</span>
                          <span className="text-xs text-muted ml-2">{(s.dns_server_protocol || 'udp').toUpperCase()}</span>
                        </TableCell>
                        <TableCell colSpan={2}>
                          <span className="ip-value">{s.resolved_ip || '—'}</span>
                          {s.resolved_org && <span className="dash-label mb-0 ml-2">{s.resolved_org}</span>}
                        </TableCell>
                        <TableCell colSpan={3}>
                          {s.screenshot_url && (
                            <a href={s.screenshot_url} target="_blank" rel="noreferrer" className="server-count">Screenshot</a>
                          )}
                        </TableCell>
                        <TableCell><span className="server-count">{fmtDateTime(s.scanned_at)}</span></TableCell>
                      </TableRow>
                    ))}
                  </Fragment>
                )
              })}
            </TableBody>
          </Table>
          <div className="flex items-center justify-end gap-3 mt-3">
            <span className="dash-label mb-0">{pageStart}–{pageEnd} of {total.toLocaleString()}</span>
            <Button variant="outline" size="sm" disabled={page <= 1 || loading} onClick={() => setPage(page - 1)}>Previous</Button>
            <Button variant="outline" size="sm" disabled={pageEnd >= total || loading} onClick={() => setPage(page + 1)}>Next</Button>
          </div>
        </>
      )}
    </div>
  )
}
