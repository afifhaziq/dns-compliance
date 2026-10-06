import { useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { fetchISPResurfaced } from '@/api/isps'
import type { ResurfacedDomain } from '@/api/types'
import { Table, TableBody, TableRow, TableCell, TableHead, TableHeader } from '@/components/ui/table'
import { Button } from '@/components/ui/button'

const PAGE_SIZE = 25

// The ISP page's Resurfaced Domains table, paged server-side
// (GET /api/isps/{isp}/resurfaced). Reports the total for the summary line.
export function ISPResurfacedTable({ isp, onTotal }: { isp: string; onTotal: (total: number | null) => void }) {
  const [pageState, setPageState] = useState({ isp, page: 1 })
  const page = pageState.isp === isp ? pageState.page : 1
  const [reloadNonce, setReloadNonce] = useState(0)
  const requestKey = `${isp}|${page}|${reloadNonce}`
  const [result, setResult] = useState<{ key: string; items: ResurfacedDomain[]; total: number; error: string | null }>({ key: '', items: [], total: 0, error: null })
  const loading = result.key !== requestKey
  const { items, total, error } = result

  useEffect(() => {
    let cancelled = false
    fetchISPResurfaced(isp, page, PAGE_SIZE)
      .then(res => {
        if (cancelled) return
        setResult({ key: requestKey, items: res.items, total: res.total, error: null })
        onTotal(res.total)
      })
      .catch(err => {
        if (cancelled) return
        setResult({ key: requestKey, items: [], total: 0, error: err instanceof Error ? err.message : 'Failed to load' })
        onTotal(null)
      })
    return () => { cancelled = true }
  }, [isp, page, requestKey, onTotal])

  if (error) {
    return (
      <div className="error-state">
        <p className="error-message">{error}</p>
        <button className="btn-primary" onClick={() => setReloadNonce(n => n + 1)}>Retry</button>
      </div>
    )
  }
  if (loading && items.length === 0) {
    return (
      <div className="dash-table-wrap">
        {[1, 2, 3].map(i => (
          <div key={i} className="dash-skeleton-row">
            <span className="skeleton" style={{ width: 200, height: 13 }} />
            <span className="skeleton" style={{ width: 120, height: 13 }} />
            <span className="skeleton" style={{ width: 140, height: 13 }} />
          </div>
        ))}
      </div>
    )
  }

  const pageStart = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1
  const pageEnd = Math.min(page * PAGE_SIZE, total)
  return (
    <>
      <Table className="server-table" aria-label={`Resurfaced domains on ${isp}`} aria-busy={loading}>
        <TableHeader>
          <TableRow>
            <TableHead scope="col">Domain</TableHead>
            <TableHead scope="col">Servers</TableHead>
            <TableHead scope="col">Last blocked</TableHead>
            <TableHead scope="col">Resurfaced</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map(d => {
            const lastBlocked = d.affected_servers.reduce((max, s) => s.last_compliant_at > max ? s.last_compliant_at : max, '')
            return (
              <TableRow key={d.url}>
                <TableCell>
                  <Link to="/domain/$url" params={{ url: d.url }} search={{ tab: 'overview' }} className="ip-value">{d.url}</Link>
                </TableCell>
                <TableCell><span className="server-count">{d.affected_servers.map(s => s.dns_server_name).join(', ')}</span></TableCell>
                <TableCell><span className="server-count">{lastBlocked ? new Date(lastBlocked).toLocaleString() : '—'}</span></TableCell>
                <TableCell><span className="server-count">{new Date(d.resurfaced_at).toLocaleString()}</span></TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
      {total > PAGE_SIZE && (
        <div className="flex items-center justify-end gap-3 mt-3">
          <span className="dash-label mb-0">{pageStart}–{pageEnd} of {total.toLocaleString()}</span>
          <Button variant="outline" size="sm" disabled={page <= 1 || loading} onClick={() => setPageState({ isp, page: page - 1 })}>Previous</Button>
          <Button variant="outline" size="sm" disabled={pageEnd >= total || loading} onClick={() => setPageState({ isp, page: page + 1 })}>Next</Button>
        </div>
      )}
    </>
  )
}
