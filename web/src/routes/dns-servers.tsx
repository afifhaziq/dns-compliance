import { useCallback, useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { fetchDnsServers, createDnsServer, updateDnsServer, deleteDnsServer, fetchServerUptime, setDnsServerEnabled } from '../api/dns-servers'
import { fetchISPLogos, upsertISPLogo, deleteISPLogo } from '../api/isp-logos'
import type { DNSServer, ISPLogo } from '../api/types'
import { Radio, ShieldCheck, Lock } from 'lucide-react'
import { SquarePenIcon } from '@/components/ui/square-pen'
import { XIcon } from '@/components/ui/x'
import { ISPLogoChip } from '@/components/isp-logo-chip'
import { TrendSparkline, type TrendPoint } from '@/components/trend-sparkline'
import { Switch } from '@/components/ui/r-switch'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'
import { IPPortInput } from '@/components/ip-port-input'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { useAuth } from './__root'

export const Route = createFileRoute('/dns-servers')({ component: DNSServersPage })

type Protocol = DNSServer['protocol']

const DEFAULT_PORTS: Partial<Record<Protocol, string>> = { udp: '53', dot: '853' }

// Fills in the default port for udp/dot when the address has none, or still has
// the other protocol's default — leaves a deliberately custom port untouched.
// doh addresses are full URLs (e.g. https://1.1.1.1/dns-query), not host:port.
function syncPort(address: string, protocol: Protocol): string {
  const trimmed = address.trim()
  const defaultPort = DEFAULT_PORTS[protocol]
  if (!trimmed || !defaultPort) return address
  const idx = trimmed.lastIndexOf(':')
  const host = idx === -1 ? trimmed : trimmed.slice(0, idx)
  const port = idx === -1 ? '' : trimmed.slice(idx + 1)
  if (port && port !== '53' && port !== '853') return address
  return `${host}:${defaultPort}`
}

/* ─── Add / Edit DNS Server Dialog ───────────────────────────────────────── */

function DnsServerFormDialog({
  open,
  onClose,
  onSaved,
  editing,
}: {
  open: boolean
  onClose: () => void
  onSaved: () => void
  editing: DNSServer | null
}) {
  const [isp, setIsp] = useState('')
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [protocol, setProtocol] = useState<Protocol>('udp')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setIsp(''); setName(''); setAddress(''); setProtocol('udp'); setError(null) }

  useEffect(() => {
    if (!open) return
    if (editing) {
      setIsp(editing.isp)
      setName(editing.name)
      setAddress(editing.address)
      setProtocol(editing.protocol)
      setError(null)
    } else {
      reset()
    }
  }, [open, editing])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!isp.trim()) { setError('ISP is required'); return }
    if (!address.trim()) { setError('Address is required'); return }
    const normalizedAddress = syncPort(address, protocol)
    setLoading(true)
    setError(null)
    try {
      const payload = { isp: isp.trim(), name: name.trim(), address: normalizedAddress, protocol }
      if (editing) {
        await updateDnsServer(editing.id, payload)
      } else {
        await createDnsServer(payload)
      }
      reset()
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${editing ? 'save' : 'add'} server`)
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 440 }}>
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit DNS Server' : 'Add DNS Server'}</DialogTitle>
          <DialogDescription>
            {editing ? 'Update this resolver\'s details.' : 'Add a resolver to use for compliance checks.'}
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
            <label className="form-label" id="dns-address-label">Address</label>
            <IPPortInput
              value={address}
              onChange={setAddress}
              onBlur={() => setAddress(a => syncPort(a, protocol))}
              disabled={loading}
              portPlaceholder={DEFAULT_PORTS[protocol]}
            />
            <div className="relative my-1 mt-5 mb-5">
              <Separator />
              <span className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 bg-background px-2 text-xs text-stone-muted">
                or
              </span>
            </div>
            <input
              id="dns-address-doh-input"
              className="form-input"
              type="text"
              placeholder="https://1.1.1.1/dns-query (DoH URL)"
              aria-label="DoH URL"
              value={address}
              onChange={e => setAddress(e.target.value)}
              disabled={loading}
            />
          </div>
          <div className="form-field">
            <label className="form-label" id="dns-protocol-label">Protocol</label>
            <Select
              value={protocol}
              onValueChange={v => {
                const next = v as Protocol
                setProtocol(next)
                setAddress(a => syncPort(a, next))
              }}
              disabled={loading}
            >
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
              {editing ? (loading ? 'Saving…' : 'Save Changes') : (loading ? 'Adding…' : 'Add Server')}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Edit ISP Logo Dialog ───────────────────────────────────────────────── */

function EditISPLogoDialog({
  open,
  onClose,
  onSaved,
  isp,
  currentLogoUrl,
}: {
  open: boolean
  onClose: () => void
  onSaved: () => void
  isp: string
  currentLogoUrl?: string
}) {
  const [logoUrl, setLogoUrl] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [removing, setRemoving] = useState(false)
  const busy = loading || removing

  useEffect(() => {
    if (open) { setLogoUrl(currentLogoUrl ?? ''); setError(null) }
  }, [open, currentLogoUrl])

  const handleClose = () => { setError(null); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!logoUrl.trim()) { setError('Logo URL is required'); return }
    setLoading(true)
    setError(null)
    try {
      await upsertISPLogo(isp, logoUrl.trim())
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save logo')
    } finally {
      setLoading(false)
    }
  }

  const handleRemove = async () => {
    setRemoving(true)
    setError(null)
    try {
      await deleteISPLogo(isp)
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove logo')
    } finally {
      setRemoving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Edit {isp} Logo</DialogTitle>
          <DialogDescription>
            Sets the logo shown for this ISP in the Overview page's bento grid.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <div className="flex items-center gap-3">
              <ISPLogoChip isp={isp} logoUrl={logoUrl} size={32} />
              <div style={{ flex: 1 }}>
                <label className="form-label" htmlFor="isp-logo-edit-url-input">Logo URL</label>
                <input
                  id="isp-logo-edit-url-input"
                  className="form-input"
                  type="text"
                  placeholder="e.g. https://upload.wikimedia.org/.../cloudflare.svg"
                  value={logoUrl}
                  onChange={e => setLogoUrl(e.target.value)}
                  autoFocus
                  disabled={busy}
                />
              </div>
            </div>
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            {currentLogoUrl && (
              <button
                type="button"
                className="btn-danger"
                style={{ marginRight: 'auto' }}
                onClick={handleRemove}
                disabled={busy}
              >
                {removing ? 'Removing…' : 'Remove Logo'}
              </button>
            )}
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={busy}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={busy}>
              {loading ? 'Saving…' : 'Save Logo'}
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
    <div className="bento-grid">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="bento-card">
          <div className="bento-card-header">
            <span className="skeleton" style={{ width: 90, height: 14 }} />
          </div>
          <span className="skeleton" style={{ width: 140, height: 14 }} />
          <span className="skeleton" style={{ width: 110, height: 12 }} />
          <span className="skeleton" style={{ width: '100%', height: 20, borderRadius: 6 }} />
        </div>
      ))}
    </div>
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

const PROTOCOL_META: Record<Protocol, { Icon: typeof Radio; bg: string; label: string }> = {
  udp: { Icon: Radio, bg: '#a3a3a3', label: 'UDP — plain DNS' },
  dot: { Icon: ShieldCheck, bg: '#14b8a6', label: 'DoT — DNS-over-TLS' },
  doh: { Icon: Lock, bg: '#3b82f6', label: 'DoH — DNS-over-HTTPS' },
}

function ProtocolIcon({ protocol }: { protocol: Protocol }) {
  const { Icon, bg, label } = PROTOCOL_META[protocol]
  return (
    <div
      className="flex items-center justify-center w-8 h-8 rounded-lg text-white shrink-0"
      style={{ backgroundColor: bg }}
      title={label}
      aria-label={label}
    >
      <Icon size={16} strokeWidth={2} />
    </div>
  )
}

function DNSServersPage() {
  const { me } = useAuth()
  const canManage = me?.is_admin || me?.is_dept_admin
  const [servers, setServers] = useState<DNSServer[]>([])
  const [logos, setLogos] = useState<ISPLogo[]>([])
  const [serverTrends, setServerTrends] = useState<Map<number, TrendPoint[]>>(new Map())
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<DNSServer | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<DNSServer | null>(null)
  const [editLogoTarget, setEditLogoTarget] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      const [srvs, lgs] = await Promise.all([fetchDnsServers(), fetchISPLogos()])
      setServers(srvs)
      setLogos(lgs)

      const uptimes = await Promise.all(srvs.map(s => fetchServerUptime(s.id, 30).catch(() => [])))
      const byServerTrend = new Map<number, TrendPoint[]>()
      srvs.forEach((s, i) => {
        byServerTrend.set(s.id, uptimes[i].map(t => ({
          date: new Date(t.day),
          compliance: t.up ? 100 : 0,
        })))
      })
      setServerTrends(byServerTrend)
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

  const handleToggleEnabled = useCallback(async (id: number, enabled: boolean) => {
    setServers(prev => prev.map(s => s.id === id ? { ...s, enabled } : s))
    try {
      await setDnsServerEnabled(id, enabled)
    } catch {
      setServers(prev => prev.map(s => s.id === id ? { ...s, enabled: !enabled } : s))
    }
  }, [])

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title mb-4">DNS Servers</h1>
        <p className="page-subtitle">{!loading && `${servers.length} configured`}</p>
        {canManage && (
          <button
            className="btn-primary"
            style={{ marginLeft: 'auto' }}
            onClick={() => setAddOpen(true)}
          >
            + Add Server
          </button>
        )}
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
            {canManage && <button className="btn-primary" onClick={() => setAddOpen(true)}>Add Server</button>}
          </div>
        ) : (
          <div>
            {loading ? (
              <SkeletonSection />
            ) : (
              <div className="bento-grid">
                {groupByISP(servers).flatMap(group => group.servers).map(s => {
                  const ispLogo = logos.find(l => l.isp === s.isp)
                  return (
                    <div key={s.id} className={`bento-card ${s.enabled ? '' : 'opacity-60'}`}>
                      <div className="bento-card-header">
                        <button
                          type="button"
                          className="flex gap-2 bg-transparent border-none cursor-pointer"
                          onClick={() => canManage && setEditLogoTarget(s.isp)}
                          disabled={!canManage}
                          aria-label={canManage ? `Edit logo for ${s.isp}` : undefined}
                          title={canManage ? 'Edit logo' : undefined}
                        >
                          <ISPLogoChip isp={s.isp} logoUrl={ispLogo?.logo_url} size={40} matchLogoBackground />
                          <span className="dash-label">{s.isp}</span>
                        </button>
                        <ProtocolIcon protocol={s.protocol} />
                      </div>
                      <div className="dns-server-info">
                        <span className="dns-server-name">{s.name || s.address}</span>
                        {s.name && <span className="dns-server-addr">{s.address}</span>}
                      </div>
                      {(serverTrends.get(s.id)?.length ?? 0) >= 2 && (
                        <TrendSparkline trend={serverTrends.get(s.id)!} label="30-day uptime" mode="status" />
                      )}
                      {canManage && (
                        <div className="dns-server-meta mt-auto pt-2 border-t border-stone-border justify-between">
                          <div className="flex items-center gap-2">
                            <Switch
                              checked={s.enabled}
                              onCheckedChange={checked => handleToggleEnabled(s.id, checked)}
                              aria-label={`${s.enabled ? 'Stop' : 'Resume'} monitoring ${serverLabel(s)}`}
                            />
                            <span className="dash-label">{s.enabled ? 'Monitoring' : 'Off'}</span>
                          </div>
                          <div className="flex items-center gap-3">
                            <button
                              type="button"
                              className="screenshot-icon-btn"
                              onClick={() => setEditTarget(s)}
                              aria-label={`Edit ${serverLabel(s)}`}
                              title="Edit"
                            >
                              <SquarePenIcon size={16} />
                            </button>
                            <button
                              type="button"
                              className="screenshot-icon-btn"
                              onClick={() => setDeleteTarget(s)}
                              aria-label={`Delete ${serverLabel(s)}`}
                              title="Delete"
                            >
                              <XIcon size={16} />
                            </button>
                          </div>
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}
      </div>

      <DnsServerFormDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onSaved={load}
        editing={null}
      />

      <DnsServerFormDialog
        open={editTarget !== null}
        onClose={() => setEditTarget(null)}
        onSaved={load}
        editing={editTarget}
      />

      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget ? serverLabel(deleteTarget) : ''}
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />

      <EditISPLogoDialog
        open={editLogoTarget !== null}
        onClose={() => setEditLogoTarget(null)}
        onSaved={load}
        isp={editLogoTarget ?? ''}
        currentLogoUrl={logos.find(l => l.isp === editLogoTarget)?.logo_url}
      />
    </div>
  )
}
