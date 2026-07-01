import { useCallback, useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchDnsServers, createDnsServer, deleteDnsServer } from '../api/dns-servers'
import type { DNSServer } from '../api/types'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'

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
  const [isp, setIsp] = useState('')
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [protocol, setProtocol] = useState<Protocol>('udp')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setIsp(''); setName(''); setAddress(''); setProtocol('udp'); setError(null) }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!isp.trim()) { setError('ISP is required'); return }
    if (!address.trim()) { setError('Address is required'); return }
    setLoading(true)
    setError(null)
    try {
      await createDnsServer({ isp: isp.trim(), name: name.trim(), address: address.trim(), protocol })
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
            <label className="form-label" htmlFor="dns-isp-input">ISP</label>
            <input
              id="dns-isp-input"
              className="form-input"
              type="text"
              placeholder="e.g. Cloudflare"
              value={isp}
              onChange={e => setIsp(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
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
            <label className="form-label" id="dns-protocol-label">Protocol</label>
            <Select value={protocol} onValueChange={v => setProtocol(v as Protocol)} disabled={loading}>
              <SelectTrigger aria-labelledby="dns-protocol-label" className="w-full" />
              <SelectContent>
                <SelectItem index={0} value="udp">udp — plain DNS</SelectItem>
                <SelectItem index={1} value="dot">dot — DNS-over-TLS</SelectItem>
                <SelectItem index={2} value="doh">doh — DNS-over-HTTPS</SelectItem>
              </SelectContent>
            </Select>
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

/* ─── Grouping ───────────────────────────────────────────────────────────── */

type DNSGroup = { name: string; servers: DNSServer[] }

function groupByISP(servers: DNSServer[]): DNSGroup[] {
  const groups = new Map<string, DNSServer[]>()
  for (const s of servers) {
    if (!groups.has(s.isp)) groups.set(s.isp, [])
    groups.get(s.isp)!.push(s)
  }
  return Array.from(groups.entries()).map(([name, svrs]) => ({ name, servers: svrs }))
}

/* ─── Skeleton ───────────────────────────────────────────────────────────── */

function SkeletonSection() {
  return (
    <>
      {[2, 1].map((count, gi) => (
        <div key={gi} className="dns-isp-group">
          <span className="skeleton" style={{ width: 100, height: 15, marginBottom: 12 }} />
          {Array.from({ length: count }).map((_, i) => (
            <div key={i} className="dns-server-entry">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                <span className="skeleton" style={{ width: 140, height: 14 }} />
                <span className="skeleton" style={{ width: 110, height: 12 }} />
              </div>
              <span className="skeleton" style={{ width: 38, height: 20, borderRadius: 99 }} />
            </div>
          ))}
        </div>
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
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title mb-4">DNS Servers</h1>
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
          <div>
            {loading ? (
              <SkeletonSection />
            ) : (
              groupByISP(servers).map(group => (
                <div key={group.name} className="dns-isp-group">
                  <div className="dns-isp-header">
                    <h2 className="dns-isp-name">{group.name}</h2>
                    <span className="dns-isp-count">{group.servers.length} {group.servers.length === 1 ? 'server' : 'servers'}</span>
                  </div>
                  {group.servers.map(s => (
                    <div key={s.id} className="dns-server-entry">
                      <div className="dns-server-info">
                        <div className="flex items-center gap-2">
                          <span className="dns-server-name">{s.name || s.address}</span>
                          <Badge
                            size="sm"
                            animate={false}
                            color={s.protocol === 'dot' ? 'teal' : s.protocol === 'doh' ? 'blue' : 'gray'}
                          >
                            {s.protocol === 'dot' ? 'DoT' : s.protocol === 'doh' ? 'DoH' : 'UDP'}
                          </Badge>
                        </div>
                        {s.name && <span className="dns-server-addr">{s.address}</span>}
                      </div>
                      <div className="dns-server-meta">
                        <button
                          className="btn-row-delete"
                          onClick={() => setDeleteTarget(s)}
                          aria-label={`Delete ${serverLabel(s)}`}
                        >
                          Delete
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              ))
            )}
          </div>
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
    </div>
  )
}
