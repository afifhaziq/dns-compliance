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
} from '../api/admin'
import type { CompliantIP, Department, User } from '../api/types'
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
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
  departments: Department[]
}) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)
  const [departmentId, setDepartmentId] = useState<number | ''>('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => {
    setUsername(''); setPassword(''); setIsAdmin(false); setDepartmentId(''); setError(null)
  }
  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username.trim() || !password) { setError('Username and password are required'); return }
    if (!isAdmin && departmentId === '') { setError('Department is required for non-admin users'); return }
    setLoading(true)
    setError(null)
    try {
      await createUser({
        username: username.trim(),
        password,
        is_admin: isAdmin,
        department_id: isAdmin ? undefined : Number(departmentId),
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
          <div className="form-field">
            <label className="form-label" htmlFor="user-is-admin-checkbox">
              <input
                id="user-is-admin-checkbox"
                type="checkbox"
                checked={isAdmin}
                onChange={e => setIsAdmin(e.target.checked)}
                disabled={loading}
                style={{ marginRight: 6 }}
              />
              Admin (cross-cutting access, no department of its own)
            </label>
          </div>
          <div className="form-field">
            <label className="form-label" id="user-department-label">Department</label>
            <Select
              value={String(departmentId)}
              onValueChange={v => setDepartmentId(v === '' ? '' : Number(v))}
              disabled={loading || isAdmin}
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

/* ─── Admin Page ─────────────────────────────────────────────────────────── */

const DATE_FMT = new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short', year: 'numeric' })

function AdminPage() {
  const { me } = useAuth()
  const [departments, setDepartments] = useState<Department[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [compliantIPs, setCompliantIPs] = useState<CompliantIP[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addDeptOpen, setAddDeptOpen] = useState(false)
  const [addUserOpen, setAddUserOpen] = useState(false)
  const [addIPOpen, setAddIPOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null)
  const [deleteIPTarget, setDeleteIPTarget] = useState<CompliantIP | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      const [d, u, ips] = await Promise.all([fetchDepartments(), fetchUsers(), fetchCompliantIPs()])
      setDepartments(d)
      setUsers(u)
      setCompliantIPs(ips)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load admin data')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (me?.is_admin) load()
  }, [me, load])

  if (!me?.is_admin) {
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

  return (
    <div className="mx-20 mt-10">
      <div className="page-header">
        <h1 className="page-title">Admin</h1>
        <p className="page-subtitle">{!loading && `${departments.length} departments, ${users.length} users`}</p>
      </div>

      {error && (
        <div className="error-state">
          <p className="error-message">{error}</p>
          <button className="btn-primary" onClick={load}>Retry</button>
        </div>
      )}

      <div className="results-wrap" style={{ marginBottom: 32 }}>
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
                <TableCell className="col-status">{u.is_admin ? 'Admin' : u.department?.name ?? '—'}</TableCell>
                <TableCell className="col-status">{DATE_FMT.format(new Date(u.created_at))}</TableCell>
                <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                  <button
                    className="btn-row-delete"
                    onClick={() => setDeleteTarget(u)}
                    aria-label={`Delete ${u.username}`}
                  >
                    Delete
                  </button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        </div>
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
                <TableCell className="col-domain" style={{ fontFamily: 'monospace' }}>{ip.address}</TableCell>
                <TableCell className="col-status">{ip.note || '—'}</TableCell>
                <TableCell className="col-status">{DATE_FMT.format(new Date(ip.created_at))}</TableCell>
                <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                  <button
                    className="btn-row-delete"
                    onClick={() => setDeleteIPTarget(ip)}
                    aria-label={`Delete ${ip.address}`}
                  >
                    Delete
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
      </div>

      <AddDepartmentDialog open={addDeptOpen} onClose={() => setAddDeptOpen(false)} onAdded={load} />
      <AddUserDialog
        open={addUserOpen}
        onClose={() => setAddUserOpen(false)}
        onAdded={load}
        departments={departments}
      />
      <AddCompliantIPDialog open={addIPOpen} onClose={() => setAddIPOpen(false)} onAdded={load} />
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
    </div>
  )
}
