import { useCallback, useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchDnsServers, createDnsServer, deleteDnsServer } from '../api/dns-servers'
import type { DNSServer } from '../api/types'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'

export const Route = createFileRoute('/dns-servers')({ component: DNSServersPage })

type Protocol = DNSServer['protocol']

/* ─── Add DNS Server Dialog ──────────────────────────────────────────────── */

function AddDnsServerDialog({
  open,
  onClose,
  onAdded,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [protocol, setProtocol] = useState<Protocol>('udp')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setName(''); setAddress(''); setProtocol('udp'); setError(null) }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!address.trim()) { setError('Address is required'); return }
    setLoading(true)
    setError(null)
    try {
      await createDnsServer({ name: name.trim(), address: address.trim(), protocol })
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add server')
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 440 }}>
        <DialogHeader>
          <DialogTitle>Add DNS Server</DialogTitle>
          <DialogDescription>
            Add a resolver to use for compliance checks.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="dns-name-input">
              Name <span style={{ color: 'var(--stone-muted)', fontWeight: 400 }}>(optional)</span>
            </label>
            <input
              id="dns-name-input"
              className="form-input"
              type="text"
              placeholder="e.g. Cloudflare DoT"
              value={name}
              onChange={e => setName(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="dns-address-input">Address</label>
            <input
              id="dns-address-input"
              className="form-input"
              type="text"
              placeholder="1.1.1.1:853 or https://1.1.1.1/dns-query"
              value={address}
              onChange={e => setAddress(e.target.value)}
              disabled={loading}
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="dns-protocol-select">Protocol</label>
            <select
              id="dns-protocol-select"
              className="form-select"
              value={protocol}
              onChange={e => setProtocol(e.target.value as Protocol)}
              disabled={loading}
            >
              <option value="udp">udp — plain DNS</option>
              <option value="dot">dot — DNS-over-TLS</option>
              <option value="doh">doh — DNS-over-HTTPS</option>
            </select>
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add Server'}
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
      {[120, 160, 100].map((w, i) => (
        <tr key={i} className="skeleton-row">
          <td className="col-domain">
            <span className="skeleton" style={{ width: w, height: 14 }} />
          </td>
          <td className="col-ip">
            <span className="skeleton" style={{ width: 130, height: 14 }} />
          </td>
          <td className="col-status">
            <span className="skeleton" style={{ width: 40, height: 20, borderRadius: 99 }} />
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
      <circle cx="24" cy="24" r="16" stroke="currentColor" strokeWidth="1.5" />
      <path d="M24 8C24 8 16 14 16 24C16 34 24 40 24 40" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M24 8C24 8 32 14 32 24C32 34 24 40 24 40" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M8 24H40" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

/* ─── DNS Servers Page ───────────────────────────────────────────────────── */

function serverLabel(s: DNSServer): string {
  return s.name || s.address
}

function DNSServersPage() {
  const [servers, setServers] = useState<DNSServer[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<DNSServer | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      setServers(await fetchDnsServers())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load DNS servers')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleDelete = async () => {
    if (!deleteTarget) return
    await deleteDnsServer(deleteTarget.id)
    setDeleteTarget(null)
    load()
  }

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">DNS Servers</h1>
        <p className="page-subtitle">{!loading && `${servers.length} configured`}</p>
        <button
          className="btn-primary"
          style={{ marginLeft: 'auto' }}
          onClick={() => setAddOpen(true)}
        >
          + Add Server
        </button>
      </div>

      <div className="results-wrap">
        {error ? (
          <div className="error-state">
            <p className="error-message">{error}</p>
            <button className="btn-primary" onClick={load}>Retry</button>
          </div>
        ) : !loading && servers.length === 0 ? (
          <div className="empty-state">
            <EmptyIcon />
            <p className="empty-heading">No DNS servers yet</p>
            <p className="empty-body">Add a DNS server to begin scanning.</p>
            <button className="btn-primary" onClick={() => setAddOpen(true)}>Add Server</button>
          </div>
        ) : (
          <table className="results-table" aria-label="DNS servers">
            <thead>
              <tr>
                <th className="col-domain" scope="col">Name</th>
                <th className="col-ip" scope="col">Address</th>
                <th className="col-status" scope="col">Protocol</th>
                <th className="col-evidence" scope="col" />
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <SkeletonRows />
              ) : (
                servers.map(s => (
                  <tr key={s.id} className="url-row">
                    <td className="col-domain">
                      <span className="hostname">{s.name || <span className="dns-name">(unnamed)</span>}</span>
                    </td>
                    <td className="col-ip">
                      <span className="ip-value">{s.address}</span>
                    </td>
                    <td className="col-status">
                      <span className="protocol-badge">{s.protocol}</span>
                    </td>
                    <td className="col-evidence" style={{ textAlign: 'right' }}>
                      <button
                        className="btn-row-delete"
                        onClick={() => setDeleteTarget(s)}
                        aria-label={`Delete ${serverLabel(s)}`}
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

      <AddDnsServerDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdded={load}
      />

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget ? serverLabel(deleteTarget) : ''}
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </>
  )
}
