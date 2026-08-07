import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { BellIcon } from '@/components/ui/bell'
import { XIcon } from '@/components/ui/x'
import { relativeTime } from '@/lib/relative-time'
import { fetchUnreadCount, fetchNotifications, markNotificationRead, deleteNotification } from '@/api/notifications'
import type { Notification } from '@/api/types'

const POLL_MS = 30000

export function NotificationBell() {
  const [unread, setUnread] = useState(0)
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<Notification[]>([])
  const navigate = useNavigate()
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  const refreshCount = useCallback(() => {
    fetchUnreadCount().then(setUnread).catch(() => {})
  }, [])

  useEffect(() => {
    refreshCount()
    pollRef.current = setInterval(refreshCount, POLL_MS)
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [refreshCount])

  useEffect(() => {
    if (!open) return
    const onClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onClickOutside)
    return () => document.removeEventListener('mousedown', onClickOutside)
  }, [open])

  const toggleOpen = () => {
    const next = !open
    setOpen(next)
    if (next) {
      fetchNotifications(1, 10).then(res => setItems(res.notifications)).catch(() => {})
    }
  }

  const handleClick = async (n: Notification) => {
    setOpen(false)
    if (!n.read_at) {
      setUnread(c => Math.max(0, c - 1))
      try {
        await markNotificationRead(n.id)
      } catch {
        // best-effort — the badge will self-correct on the next poll
      }
    }
    // run is only present on due_date_reached (resurfaced has no single
    // scan run — see Notification.scan_run_id's doc comment); the History
    // tab treats a missing run as "just open History", no auto-expand.
    navigate({ to: '/domain/$url', params: { url: n.url }, search: { tab: 'history', run: n.scan_run_id } })
  }

  const handleDelete = async (e: React.MouseEvent, n: Notification) => {
    e.stopPropagation()
    setItems(prev => prev.filter(item => item.id !== n.id))
    if (!n.read_at) setUnread(c => Math.max(0, c - 1))
    try {
      await deleteNotification(n.id)
    } catch {
      // best-effort — a failed dismiss just reappears on the next open
    }
  }

  return (
    <div ref={containerRef} style={{ position: 'relative' }}>
      <button
        type="button"
        className="btn-ghost"
        aria-label={unread > 0 ? `Notifications, ${unread} unread` : 'Notifications'}
        onClick={toggleOpen}
        style={{ position: 'relative', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 36, height: 36 }}
      >
        <BellIcon size={18} />
        {unread > 0 && (
          <span
            style={{
              position: 'absolute', top: 2, right: 2, minWidth: 15, height: 15, borderRadius: 999,
              background: 'var(--accent, #4338ca)', color: 'white', fontSize: 10, lineHeight: '15px',
              textAlign: 'center', padding: '0 3px',
            }}
          >
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>

      {open && (
        <div
          className="absolute right-0 z-50 rounded-lg border border-stone-border bg-background shadow-[0_4px_20px_rgba(0,0,0,0.12)] dark:shadow-[0_4px_20px_rgba(0,0,0,0.4)] overflow-y-auto"
          style={{ top: 'calc(100% + 8px)', width: 340, maxHeight: 400 }}
        >
          {items.length === 0 ? (
            <p style={{ padding: 16, fontSize: '0.85rem', opacity: 0.6 }}>No notifications yet.</p>
          ) : (
            items.map(n => (
              <div
                key={n.id}
                className="flex items-stretch bg-transparent hover:bg-stone-panel transition-colors duration-100"
                style={{ borderBottom: '1px solid var(--stone-border, rgba(0,0,0,0.08))', opacity: n.read_at ? 0.6 : 1 }}
              >
                <button
                  type="button"
                  onClick={() => handleClick(n)}
                  className="flex-1 min-w-0 text-left px-3 py-2 text-sm bg-transparent border-none cursor-pointer font-[inherit]"
                >
                  <div style={{ fontWeight: 600 }}>{n.url}</div>
                  <div style={{ fontSize: '0.75rem', opacity: 0.7 }}>
                    {n.type === 'resurfaced'
                      ? 'Resurfaced — blocked domain is resolving again'
                      : `Due-date scan: ${n.compliant ? 'compliant' : 'still violating'}`}
                  </div>
                  <div style={{ fontSize: '0.7rem', opacity: 0.5, marginTop: 2 }}>
                    {n.scan_run_id != null && `Scan #${n.scan_run_id} · `}
                    {relativeTime(n.scanned_at ?? n.created_at)}
                  </div>
                </button>
                <button
                  type="button"
                  onClick={e => handleDelete(e, n)}
                  aria-label={`Dismiss notification for ${n.url}`}
                  className="screenshot-icon-btn"
                  style={{ alignSelf: 'center', marginRight: 8, flexShrink: 0 }}
                >
                  <XIcon size={14} />
                </button>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}
