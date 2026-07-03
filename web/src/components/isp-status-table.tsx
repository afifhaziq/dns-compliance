import { Link } from '@tanstack/react-router'
import type { ScanResult } from '@/api/types'
import { Table, TableBody, TableRow, TableCell } from '@/components/ui/table'

export type ISPStat = { isp: string; compliant: number; total: number; serverCount: number }

export function computeISPStats(results: ScanResult[]): ISPStat[] {
  const map = new Map<string, ISPStat>()
  const serversSeen = new Map<string, Set<string>>() // isp -> set of server names
  for (const r of results) {
    const isp = r.dns_server.isp
    const s = map.get(isp) ?? { isp, compliant: 0, total: 0, serverCount: 0 }
    s.total++
    if (r.compliant) s.compliant++
    map.set(isp, s)
    const seen = serversSeen.get(isp) ?? new Set()
    seen.add(r.dns_server.name)
    serversSeen.set(isp, seen)
  }
  // Set serverCount from unique server names
  for (const [isp, s] of map) {
    s.serverCount = serversSeen.get(isp)?.size ?? 0
  }
  return Array.from(map.values()).sort((a, b) => a.compliant / a.total - b.compliant / b.total)
}

export function ISPStatusSkeleton({ count }: { count: number }) {
  return (
    <div className="dash-table-wrap">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="dash-skeleton-row">
          <span className="skeleton" style={{ width: 140, height: 13 }} />
          <span className="skeleton" style={{ flex: 1, maxWidth: 200, height: 4 }} />
          <span className="skeleton" style={{ width: 64, height: 13 }} />
        </div>
      ))}
    </div>
  )
}

export function ISPStatusTable({ stats }: { stats: ISPStat[] }) {
  return (
    <Table className="server-table" aria-label="ISP compliance status">
      <TableBody>
        {stats.map(s => {
          const pct = s.total > 0 ? Math.round((s.compliant / s.total) * 100) : 0
          const allCompliant = s.compliant === s.total
          const violations = s.total - s.compliant

          return (
            <TableRow key={s.isp} className="server-row">
              <TableCell className="server-name-cell">
                <Link
                  to="/isps/$isp"
                  params={{ isp: s.isp }}
                  className="server-name"
                >
                  {s.isp}
                </Link>
              </TableCell>
              <TableCell className="server-bar-cell">
                <div className="server-bar-wrap">
                  <div className="server-bar" role="presentation">
                    <div className="server-bar-fill" style={{ width: `${pct}%` }} />
                  </div>
                  <span className="server-count">{s.compliant} / {s.total}</span>
                </div>
              </TableCell>
              <TableCell className="server-status-cell">
                {allCompliant ? (
                  <span className="status-dot-label">
                    <span className="status-dot dot-compliant" aria-hidden="true" />
                    <span className="label-compliant">All compliant</span>
                  </span>
                ) : (
                  <span className="status-dot-label">
                    <span className="status-dot dot-violation" aria-hidden="true" />
                    <span className="label-violation">
                      {violations} {violations === 1 ? 'violation' : 'violations'}
                    </span>
                  </span>
                )}
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}
