import { useCallback, useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchUrls, createUrl, deleteUrl } from '../api/urls'
import type { URLEntry } from '../api/types'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'

export const Route = createFileRoute('/urls')({ component: URLsPage })

/* ─── Add URL Dialog ─────────────────────────────────────────────────────── */

function AddUrlDialog({
  open,
  onClose,
  onAdded,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [url, setUrl] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setUrl(''); setError(null) }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!url.trim()) { setError('URL is required'); return }
    setLoading(true)
    setError(null)
    try {
      await createUrl(url.trim())
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add URL')
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Add URL</DialogTitle>
          <DialogDescription>
            Add a URL to monitor for DNS compliance.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="add-url-input">URL</label>
            <input
              id="add-url-input"
              className="form-input"
              type="url"
              placeholder="https://example.com"
              value={url}
              onChange={e => setUrl(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add URL'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Skeleton ───────────────────────────────────────────────────────────── */

function SkeletonRows() {
  return (
    <>
      {[200, 160, 240].map((w, i) => (
        <tr key={i} className="skeleton-row">
          <td className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </td>
          <td className="col-status">
            <span className="skeleton" style={{ width: 90, height: 14 }} />
          </td>
          <td className="col-evidence" />
        </tr>
      ))}
    </>
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

/* ─── URLs Page ──────────────────────────────────────────────────────────── */

const DATE_FMT = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric', month: 'short', year: 'numeric',
})

function URLsPage() {
  const [urls, setUrls] = useState<URLEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<URLEntry | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      setUrls(await fetchUrls())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load URLs')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleDelete = async () => {
    if (!deleteTarget) return
    await deleteUrl(deleteTarget.id)
    setDeleteTarget(null)
    load()
  }

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">URLs</h1>
        <p className="page-subtitle">{!loading && `${urls.length} monitored`}</p>
        <button
          className="btn-primary"
          style={{ marginLeft: 'auto' }}
          onClick={() => setAddOpen(true)}
        >
          + Add URL
        </button>
      </div>

      <div className="results-wrap">
        {error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : !loading && urls.length === 0 ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No URLs yet</p>
            <p className="empty-body">Add a URL to start monitoring DNS compliance.</p>
            <button className="btn-primary" onClick={() => setAddOpen(true)}>Add URL</button>
          </div>
        ) : (
          <table className="results-table" aria-label="Monitored URLs">
            <thead>
              <tr>
                <th className="col-domain" scope="col">URL</th>
                <th className="col-status" scope="col">Added</th>
                <th className="col-evidence" scope="col" />
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <SkeletonRows />
              ) : (
                urls.map(u => (
                  <tr key={u.id} className="url-row">
                    <td className="col-domain">
                      <span className="hostname">{u.url}</span>
                    </td>
                    <td className="col-status">
                      <span className="dns-name">
                        {DATE_FMT.format(new Date(u.created_at))}
                      </span>
                    </td>
                    <td className="col-evidence" style={{ textAlign: 'right' }}>
                      <button
                        className="btn-row-delete"
                        onClick={() => setDeleteTarget(u)}
                        aria-label={`Delete ${u.url}`}
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </div>

      <AddUrlDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdded={load}
      />

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget?.url ?? ''}
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </>
  )
}
