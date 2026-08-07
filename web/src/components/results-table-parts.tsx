// Shared building blocks for the Results page table (results.index.tsx),
// the domain History tab table (domain.$url.tsx), and the watchlist data
// grid (urls.tsx) — kept in one place so they stay visually consistent
// instead of drifting via copy-paste.

import type { Column } from '@tanstack/react-table'
import { ArrowUpIcon, ArrowDownIcon, ChevronsUpDownIcon } from 'lucide-react'

// Minimal stand-in for reui's DataGridColumnHeader: that component pulls in
// a full dropdown-menu (pin/move/visibility) tied to Next.js-specific paths
// that this app doesn't use — this is just click-to-cycle-sort with an
// indicator icon.
export function SortableHeader<TData, TValue>({ column, title }: { column: Column<TData, TValue>; title: string }) {
  const sorted = column.getIsSorted()
  const cycleSort = () => {
    if (sorted === 'asc') column.toggleSorting(true)
    else if (sorted === 'desc') column.clearSorting()
    else column.toggleSorting(false)
  }
  return (
    <button
      type="button"
      className="inline-flex items-center gap-1 text-[11px] font-semibold tracking-[0.06em] uppercase text-stone-muted hover:text-foreground transition-colors duration-150 ease-snappy"
      onClick={cycleSort}
    >
      {title}
      {sorted === 'asc' ? (
        <ArrowUpIcon className="w-3 h-3" />
      ) : sorted === 'desc' ? (
        <ArrowDownIcon className="w-3 h-3" />
      ) : (
        <ChevronsUpDownIcon className="w-3 h-3 opacity-40" />
      )}
    </button>
  )
}

export function StatusDot({ compliant }: { compliant: boolean }) {
  return (
    <span className="status-dot-label">
      <span className={`status-dot ${compliant ? 'dot-compliant' : 'dot-violation'}`} aria-hidden="true" />
      <span className={compliant ? 'label-compliant' : 'label-violation'}>
        {compliant ? 'Compliant' : 'Violation'}
      </span>
    </span>
  )
}

export function EmptyIcon() {
  return (
    <svg className="empty-icon" width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
      <rect x="8" y="4" width="24" height="32" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M32 4L40 12V36C40 37.1 39.1 38 38 38H32" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M40 12H32V4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 18H26M14 24H22" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}
