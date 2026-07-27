import { useCallback, useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import {
  fetchDepartments,
  createDepartment,
  fetchUsers,
  createUser,
  deleteUser,
  fetchCompliantIPs,
  createCompliantIP,
  deleteCompliantIP,
  fetchScanInterval,
  setScanInterval,
  type ScanSchedule,
} from '../api/admin'
import { fetchISPLogos, upsertISPLogo, deleteISPLogo } from '../api/isp-logos'
import { fetchDnsServers } from '../api/dns-servers'
import type { CompliantIP, Department, DNSServer, ISPLogo, User } from '../api/types'
import { ISPLogoChip } from '@/components/isp-logo-chip'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Select, SelectTrigger, SelectContent, SelectItem } from '@/components/ui/select'
import { Switch } from '@/components/ui/r-switch'
import { XIcon } from '@/components/ui/x'
import { useAuth } from './__root'

export const Route = createFileRoute('/admin/')({ component: AdminPage })

/* ─── Add Department Dialog ─────────────────────────────────────────────── */

function AddDepartmentDialog({
  open,
  onClose,
  onAdded,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setName(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) { setError('Name is required'); return }
    setLoading(true)
    setError(null)
    try {
      await createDepartment(name.trim())
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add department')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 400 }}>
        <DialogHeader>
          <DialogTitle>Add Department</DialogTitle>
          <DialogDescription>
            Departments get their own domain watchlist. Existing roles (CMOD, CRD) can be extended with more.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="dept-name-input">Name</label>
            <input
              id="dept-name-input"
              className="form-input"
              type="text"
              placeholder="e.g. CMOD"
              value={name}
              onChange={e => setName(e.target.value)}
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
              {loading ? 'Adding…' : 'Add Department'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Add User Dialog ────────────────────────────────────────────────────── */

function AddUserDialog({
  open,
  onClose,
  onAdded,
  departments,
  callerIsSuperAdmin,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  departments: Department[]
  callerIsSuperAdmin: boolean
}) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<'member' | 'dept_admin' | 'admin'>('member')
  const [departmentId, setDepartmentId] = useState<number | ''>('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  // A department admin always creates a plain member of their own
  // department — the server forces this regardless of what's sent, so
  // there's nothing for a department-admin caller to pick here.
  const showDepartmentPicker = callerIsSuperAdmin && role !== 'admin'

  const reset = () => {
    setUsername(''); setPassword(''); setRole('member'); setDepartmentId(''); setError(null)
  }
  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username.trim() || !password) { setError('Username and password are required'); return }
    if (showDepartmentPicker && departmentId === '') { setError('Department is required'); return }
    setLoading(true)
    setError(null)
    try {
      await createUser({
        username: username.trim(),
        password,
        is_admin: callerIsSuperAdmin && role === 'admin',
        is_dept_admin: callerIsSuperAdmin && role === 'dept_admin',
        department_id: showDepartmentPicker ? Number(departmentId) : undefined,
      })
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create user')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Create User</DialogTitle>
          <DialogDescription>
            Accounts are admin-provisioned — there's no self-registration. Set a temporary password and share it out of band.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="user-username-input">Username</label>
            <input
              id="user-username-input"
              className="form-input"
              type="text"
              value={username}
              onChange={e => setUsername(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="user-password-input">Temporary Password</label>
            <input
              id="user-password-input"
              className="form-input"
              type="text"
              value={password}
              onChange={e => setPassword(e.target.value)}
              disabled={loading}
            />
          </div>
          {callerIsSuperAdmin && (
            <div className="form-field">
              <label className="form-label" id="user-role-label">Role</label>
              <Select value={role} onValueChange={v => setRole(v as typeof role)} disabled={loading}>
                <SelectTrigger aria-labelledby="user-role-label" className="w-full" />
                <SelectContent>
                  <SelectItem index={0} value="member">Member</SelectItem>
                  <SelectItem index={1} value="dept_admin">Department Admin</SelectItem>
                  <SelectItem index={2} value="admin">Admin (cross-cutting access, no department of its own)</SelectItem>
                </SelectContent>
              </Select>
            </div>
          )}
          {showDepartmentPicker && (
            <div className="form-field">
              <label className="form-label" id="user-department-label">Department</label>
              <Select
                value={String(departmentId)}
                onValueChange={v => setDepartmentId(v === '' ? '' : Number(v))}
                disabled={loading}
              >
                <SelectTrigger aria-labelledby="user-department-label" placeholder="Select a department…" className="w-full" />
                <SelectContent>
                  <SelectItem index={0} value="">Select a department…</SelectItem>
                  {departments.map((d, i) => (
                    <SelectItem key={d.id} index={i + 1} value={String(d.id)}>{d.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Creating…' : 'Create User'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Add Compliant IP Dialog ────────────────────────────────────────────── */

function AddCompliantIPDialog({
  open,
  onClose,
  onAdded,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [address, setAddress] = useState('')
  const [note, setNote] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setAddress(''); setNote(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!address.trim()) { setError('IP address is required'); return }
    setLoading(true)
    setError(null)
    try {
      await createCompliantIP(address.trim(), note.trim())
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add IP')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Add Compliant IP</DialogTitle>
          <DialogDescription>
            DNS resolutions to this IP will be classified as compliant — used for ISP block-page redirects (e.g. MCMC).
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="cip-address-input">IP Address</label>
            <input
              id="cip-address-input"
              className="form-input"
              type="text"
              placeholder="e.g. 175.139.142.25"
              value={address}
              onChange={e => setAddress(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="cip-note-input">
              Note <span style={{ color: 'var(--stone-muted)', fontWeight: 400 }}>(optional)</span>
            </label>
            <input
              id="cip-note-input"
              className="form-input"
              type="text"
              placeholder="e.g. MCMC block page"
              value={note}
              onChange={e => setNote(e.target.value)}
              disabled={loading}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add IP'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Add ISP Logo Dialog ────────────────────────────────────────────────── */

function AddISPLogoDialog({
  open,
  onClose,
  onAdded,
  ispOptions,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  ispOptions: string[]
}) {
  const [isp, setIsp] = useState('')
  const [logoUrl, setLogoUrl] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setIsp(''); setLogoUrl(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!isp) { setError('ISP is required'); return }
    if (!logoUrl.trim()) { setError('Logo URL is required'); return }
    setLoading(true)
    setError(null)
    try {
      await upsertISPLogo(isp, logoUrl.trim())
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add logo')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Add ISP Logo</DialogTitle>
          <DialogDescription>
            Sets the logo shown for this ISP in the Overview page's bento grid. Re-adding an existing ISP overwrites its logo.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" id="isp-logo-isp-label">ISP</label>
            <Select value={isp} onValueChange={setIsp} disabled={loading}>
              <SelectTrigger aria-labelledby="isp-logo-isp-label" className="w-full" />
              <SelectContent>
                {ispOptions.map((name, i) => (
                  <SelectItem key={name} index={i} value={name}>{name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="isp-logo-url-input">Logo URL</label>
            <input
              id="isp-logo-url-input"
              className="form-input"
              type="text"
              placeholder="e.g. https://upload.wikimedia.org/.../cloudflare.svg"
              value={logoUrl}
              onChange={e => setLogoUrl(e.target.value)}
              disabled={loading}
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add Logo'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Scan Interval Settings ─────────────────────────────────────────────── */

const SCAN_INTERVAL_OPTIONS = [
  { minutes: 15, label: '15 minutes' },
  { minutes: 30, label: '30 minutes' },
  { minutes: 60, label: '1 hour' },
  { minutes: 360, label: '6 hours' },
  { minutes: 720, label: '12 hours' },
  { minutes: 1440, label: '1 day' },
]

function ScanIntervalSection({ value, onSaved }: { value: ScanSchedule; onSaved: (schedule: ScanSchedule) => void }) {
  const [minutes, setMinutes] = useState(value.interval_minutes)
  const [enabled, setEnabled] = useState(value.enabled)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => { setMinutes(value.interval_minutes); setEnabled(value.enabled) }, [value])

  const dirty = minutes !== value.interval_minutes || enabled !== value.enabled

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      await setScanInterval(minutes, enabled)
      onSaved({ interval_minutes: minutes, enabled })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className='mb-4'>
      <div className="page-header" style={{ marginBottom: 12 }}>
        <h2 className="section-title">Scan Schedule</h2>
        <p className="page-subtitle" style={{ marginLeft: 8 }}>How often the automated cron sweep runs</p>
      </div>
      <div className="flex flex-row items-center" style={{ gap: 8, maxWidth: 320 }}>
        <Select value={String(minutes)} onValueChange={v => setMinutes(Number(v))} disabled={saving}>
          <SelectTrigger aria-label="Scan interval" className="w-full" />
          <SelectContent>
            {SCAN_INTERVAL_OPTIONS.map((opt, i) => (
              <SelectItem key={opt.minutes} index={i} value={String(opt.minutes)}>{opt.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Switch
          checked={enabled}
          onCheckedChange={setEnabled}
          disabled={saving}
          aria-label={`${enabled ? 'Disable' : 'Enable'} the automated cron sweep`}
        />
        <button className="btn-primary" onClick={handleSave} disabled={saving || !dirty}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
      {error && <p className="form-error">{error}</p>}
    </div>
  )
}

/* ─── Admin Page ─────────────────────────────────────────────────────────── */

const DATE_FMT = new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short', year: 'numeric' })

function AdminPage() {
  const { me } = useAuth()
  const [departments, setDepartments] = useState<Department[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [compliantIPs, setCompliantIPs] = useState<CompliantIP[]>([])
  const [ispLogos, setIspLogos] = useState<ISPLogo[]>([])
  const [dnsServers, setDnsServers] = useState<DNSServer[]>([])
  const [scanSchedule, setScanSchedule] = useState<ScanSchedule | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addDeptOpen, setAddDeptOpen] = useState(false)
  const [addUserOpen, setAddUserOpen] = useState(false)
  const [addIPOpen, setAddIPOpen] = useState(false)
  const [addLogoOpen, setAddLogoOpen] = useState(false)
  const [deleteLogoTarget, setDeleteLogoTarget] = useState<ISPLogo | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null)
  const [deleteIPTarget, setDeleteIPTarget] = useState<CompliantIP | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      // ISP Logos and DNS servers are readable by admin and dept-admin alike
      // (dept-admins already manage the DNS server catalog), unlike the
      // super-admin-only fetches below.
      const [logos, servers] = await Promise.all([fetchISPLogos(), fetchDnsServers()])
      setIspLogos(logos)
      setDnsServers(servers)

      if (me?.is_admin) {
        // Departments/Compliant-IPs/scan interval stay super-admin-only
        // server-side — a department admin would just get a 403 fetching them.
        const [d, u, ips, schedule] = await Promise.all([
          fetchDepartments(), fetchUsers(), fetchCompliantIPs(), fetchScanInterval(),
        ])
        setDepartments(d)
        setUsers(u)
        setCompliantIPs(ips)
        setScanSchedule(schedule)
      } else {
        setUsers(await fetchUsers())
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load admin data')
    } finally {
      setLoading(false)
    }
  }, [me])

  useEffect(() => {
    if (me?.is_admin || me?.is_dept_admin) load()
  }, [me, load])

  if (!me?.is_admin && !me?.is_dept_admin) {
    return (
      <div className="mx-20">
        <div className="error-state">
          <p className="error-message">Admin access required.</p>
        </div>
      </div>
    )
  }

  const handleDeleteUser = async () => {
    if (!deleteTarget) return
    await deleteUser(deleteTarget.id)
    setDeleteTarget(null)
    load()
  }

  const handleDeleteIP = async () => {
    if (!deleteIPTarget) return
    await deleteCompliantIP(deleteIPTarget.id)
    setDeleteIPTarget(null)
    load()
  }

  const handleDeleteLogo = async () => {
    if (!deleteLogoTarget) return
    await deleteISPLogo(deleteLogoTarget.isp)
    setDeleteLogoTarget(null)
    load()
  }

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title">Admin</h1>
        <p className="page-subtitle">
          {!loading && (me?.is_admin ? `${departments.length} departments, ${users.length} users` : `${users.length} users`)}
        </p>
      </div>

      {error && (
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button className="btn-primary" onClick={load}>Retry</button>
        </div>
      )}

      <div className="results-wrap" style={{ marginBottom: 32 }}>
        {me?.is_admin && (
        <>
        <div className='mb-4'>
        <div className="page-header" style={{ marginBottom: 12 }}>
          <h2 className="section-title">Departments</h2>
          <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddDeptOpen(true)}>
            + Add Department
          </button>
        </div>
        <Table className="results-table" aria-label="Departments">
          <TableHeader>
            <TableRow>
              <TableHead className="col-domain th-left" scope="col">Name</TableHead>
              <TableHead className="col-status" scope="col">Created</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {departments.map(d => (
              <TableRow key={d.id} className="admin-row">
                <TableCell className="col-domain">{d.name}</TableCell>
                <TableCell className="col-status text-center">{DATE_FMT.format(new Date(d.created_at))}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        </div>
        {scanSchedule !== null && (
          <ScanIntervalSection value={scanSchedule} onSaved={setScanSchedule} />
        )}
        </>
        )}
        <div className='mb-4'>
        <div className="page-header" style={{ marginBottom: 12 }}>
          <h2 className="section-title">Users</h2>
          <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddUserOpen(true)}>
            + Create User
          </button>
        </div>
        <Table className="results-table" aria-label="Users">
          <TableHeader>
            <TableRow>
              <TableHead className="col-domain th-left" scope="col">Username</TableHead>
              <TableHead className="col-status" scope="col">Department</TableHead>
              <TableHead className="col-status" scope="col">Created</TableHead>
              <TableHead className="col-evidence" scope="col" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.map(u => (
              <TableRow key={u.id} className="admin-row">
                <TableCell className="col-domain">{u.username}</TableCell>
                <TableCell className="col-status">
                  {u.is_admin ? 'Admin' : u.is_dept_admin ? `${u.department?.name ?? '—'} (Admin)` : u.department?.name ?? '—'}
                </TableCell>
                <TableCell className="col-status">{DATE_FMT.format(new Date(u.created_at))}</TableCell>
                <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                  <button
                    type="button"
                    className="screenshot-icon-btn"
                    onClick={() => setDeleteTarget(u)}
                    aria-label={`Delete ${u.username}`}
                    title="Delete"
                  >
                    <XIcon size={16} />
                  </button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        </div>
        <div className='mb-4'>
        <div className="page-header" style={{ marginBottom: 12 }}>
          <h2 className="section-title">ISP Logos</h2>
          <p className="page-subtitle" style={{ marginLeft: 8 }}>Shown next to each ISP's name on the Overview page</p>
          <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddLogoOpen(true)}>
            + Add Logo
          </button>
        </div>
        <Table className="results-table" aria-label="ISP Logos">
          <TableHeader>
            <TableRow>
              <TableHead className="col-status" scope="col">Logo</TableHead>
              <TableHead className="col-domain th-left" scope="col">ISP</TableHead>
              <TableHead className="col-evidence" scope="col" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {ispLogos.map(logo => (
              <TableRow key={logo.isp} className="admin-row">
                <TableCell className="col-status text-center">
                  <ISPLogoChip isp={logo.isp} logoUrl={logo.logo_url} size={24} />
                </TableCell>
                <TableCell className="col-domain">{logo.isp}</TableCell>
                <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                  <button
                    type="button"
                    className="screenshot-icon-btn"
                    onClick={() => setDeleteLogoTarget(logo)}
                    aria-label={`Delete logo for ${logo.isp}`}
                    title="Delete"
                  >
                    <XIcon size={16} />
                  </button>
                </TableCell>
              </TableRow>
            ))}
            {ispLogos.length === 0 && !loading && (
              <TableRow>
                <TableCell colSpan={3} style={{ textAlign: 'center', color: 'var(--stone-muted)', padding: '16px 0' }}>
                  No ISP logos configured
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        </div>
        {me?.is_admin && (
        <div className='mb-4'>
        <div className="page-header" style={{ marginBottom: 12 }}>
          <h2 className="section-title">Compliant IPs</h2>
          <p className="page-subtitle" style={{ marginLeft: 8 }}>DNS resolutions to these IPs are classified as compliant</p>
          <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddIPOpen(true)}>
            + Add IP
          </button>
        </div>
        <Table className="results-table" aria-label="Compliant IPs">
          <TableHeader>
            <TableRow>
              <TableHead className="col-domain th-left" scope="col">IP Address</TableHead>
              <TableHead className="col-status" scope="col">Note</TableHead>
              <TableHead className="col-status" scope="col">Added</TableHead>
              <TableHead className="col-evidence" scope="col" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {compliantIPs.map(ip => (
              <TableRow key={ip.id} className="admin-row">
                <TableCell className="col-domain"><span className="ip-value">{ip.address}</span></TableCell>
                <TableCell className="col-status">{ip.note || '—'}</TableCell>
                <TableCell className="col-status">{DATE_FMT.format(new Date(ip.created_at))}</TableCell>
                <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                  <button
                    type="button"
                    className="screenshot-icon-btn"
                    onClick={() => setDeleteIPTarget(ip)}
                    aria-label={`Delete ${ip.address}`}
                    title="Delete"
                  >
                    <XIcon size={16} />
                  </button>
                </TableCell>
              </TableRow>
            ))}
            {compliantIPs.length === 0 && !loading && (
              <TableRow>
                <TableCell colSpan={4} style={{ textAlign: 'center', color: 'var(--stone-muted)', padding: '16px 0' }}>
                  No compliant IPs configured
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        </div>
        )}
      </div>

      <AddDepartmentDialog open={addDeptOpen} onClose={() => setAddDeptOpen(false)} onAdded={load} />
      <AddUserDialog
        open={addUserOpen}
        onClose={() => setAddUserOpen(false)}
        onAdded={load}
        departments={departments}
        callerIsSuperAdmin={!!me?.is_admin}
      />
      <AddCompliantIPDialog open={addIPOpen} onClose={() => setAddIPOpen(false)} onAdded={load} />
      <AddISPLogoDialog
        open={addLogoOpen}
        onClose={() => setAddLogoOpen(false)}
        onAdded={load}
        ispOptions={Array.from(new Set(dnsServers.map(s => s.isp))).sort()}
      />
      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget?.username ?? ''}
        onConfirm={handleDeleteUser}
        onCancel={() => setDeleteTarget(null)}
      />
      <DeleteConfirmDialog
        open={deleteIPTarget !== null}
        itemLabel={deleteIPTarget?.address ?? ''}
        description="Scans will no longer classify this IP as compliant."
        onConfirm={handleDeleteIP}
        onCancel={() => setDeleteIPTarget(null)}
      />
      <DeleteConfirmDialog
        open={deleteLogoTarget !== null}
        itemLabel={deleteLogoTarget?.isp ?? ''}
        description="The bento grid will fall back to a monogram for this ISP."
        onConfirm={handleDeleteLogo}
        onCancel={() => setDeleteLogoTarget(null)}
      />
    </div>
  )
}
