import { useCallback, useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchUrls, createUrl, deleteUrl, setUrlEnabled } from '../api/urls'
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
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/r-switch'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardImage,
} from '@/components/animate-ui/components/base/preview-link-card'

export const Route = createFileRoute('/urls')({ component: URLsPage })

/* ─── Add Domain Dialog ──────────────────────────────────────────────────── */

function AddUrlDialog({
  open,
  onClose,
  onAdded,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [value, setValue] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setValue(''); setError(null) }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const domains = value.split('\n').map(s => s.trim()).filter(Boolean)
    if (domains.length === 0) { setError('At least one domain is required'); return }
    setLoading(true)
    setError(null)
    try {
      await Promise.all(domains.map(d => createUrl(d)))
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add domain')
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Add Domain</DialogTitle>
          <DialogDescription>
            Enter one or more domains or full URLs to monitor for DNS compliance. Full URLs will have their domain automatically extracted. You can add multiple entries at once, just put each one on a new line.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="add-url-input">Domain</label>
            <textarea
              id="add-url-input"
              className="form-input"
              placeholder={'https://example.com\nhttps://example2.com'}
              value={value}
              onChange={e => setValue(e.target.value)}
              autoFocus
              disabled={loading}
              rows={4}
              style={{ resize: 'vertical', fontFamily: 'inherit' }}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add Domain'}
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
        <TableRow key={i} className="skeleton-row">
          <TableCell className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </TableCell>
          <TableCell className="col-status">
            <span className="skeleton" style={{ width: 90, height: 14 }} />
          </TableCell>
          <TableCell style={{ width: 52 }} />
          <TableCell className="col-evidence" />
        </TableRow>
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
      setError(err instanceof Error ? err.message : 'Failed to load domains')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleToggle = useCallback(async (id: number, enabled: boolean) => {
    setUrls(prev => prev.map(u => u.id === id ? { ...u, enabled } : u))
    try {
      await setUrlEnabled(id, enabled)
    } catch {
      setUrls(prev => prev.map(u => u.id === id ? { ...u, enabled: !enabled } : u))
    }
  }, [])

  const handleDelete = async () => {
    if (!deleteTarget) return
    await deleteUrl(deleteTarget.id)
    setDeleteTarget(null)
    load()
  }

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title mb-4">Domains</h1>
        <p className="page-subtitle">{!loading && `${urls.length} monitored`}</p>
        <Button
          style={{ marginLeft: 'auto' }}
          onClick={() => setAddOpen(true)}
        >
          + Add Domain
        </Button>
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
            <p className="empty-heading">No domains yet</p>
            <p className="empty-body">Add a domain to start monitoring DNS compliance.</p>
            <button className="btn-primary" onClick={() => setAddOpen(true)}>Add Domain</button>
          </div>
        ) : (
          <Table className="results-table" aria-label="Monitored domains">
            <TableHeader>
              <TableRow>
                <TableHead className="col-domain th-left" scope="col">Domain</TableHead>
                <TableHead className="col-status" scope="col">Added</TableHead>
                <TableHead scope="col" style={{ width: 52, textAlign: 'center' }}>Scan</TableHead>
                <TableHead className="col-evidence" scope="col" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <SkeletonRows />
              ) : (
                urls.map(u => (
                  <TableRow key={u.id} className="url-row">
                    <TableCell className="col-domain">
                      <PreviewLinkCard href={u.url}>
                        <PreviewLinkCardTrigger>
                          <span className="hostname">{u.url}</span>
                        </PreviewLinkCardTrigger>
                        <PreviewLinkCardPanel>
                          <PreviewLinkCardImage />
                        </PreviewLinkCardPanel>
                      </PreviewLinkCard>
                    </TableCell>
                    <TableCell className="col-status text-center">
                      <span className="dns-name">
                        {DATE_FMT.format(new Date(u.created_at))}
                      </span>
                    </TableCell>
                    <TableCell style={{ textAlign: 'center' }}>
                      <Switch
                        checked={u.enabled}
                        onCheckedChange={checked => handleToggle(u.id, checked)}
                        aria-label={`${u.enabled ? 'Disable' : 'Enable'} ${u.url} in scan`}
                      />
                    </TableCell>
                    <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                      <button
                        className="btn-row-delete"
                        onClick={() => setDeleteTarget(u)}
                        aria-label={`Delete ${u.url}`}
                      >
                        Delete
                      </button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
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
        description="This will remove it from your department's watchlist. The domain and its scan history are kept if any other department still watches it."
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  )
}
