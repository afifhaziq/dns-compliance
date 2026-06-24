import { useCallback, useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import {
  fetchDepartments,
  createDepartment,
  fetchUsers,
  createUser,
  deleteUser,
} from '../api/admin'
import type { Department, User } from '../api/types'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'
import { DeleteConfirmDialog } from '@/components/delete-confirm-dialog'
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
            <label className="form-label" htmlFor="user-department-select">Department</label>
            <select
              id="user-department-select"
              className="form-select"
              value={departmentId}
              onChange={e => setDepartmentId(e.target.value === '' ? '' : Number(e.target.value))}
              disabled={loading || isAdmin}
            >
              <option value="">Select a department…</option>
              {departments.map(d => (
                <option key={d.id} value={d.id}>{d.name}</option>
              ))}
            </select>
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

/* ─── Admin Page ─────────────────────────────────────────────────────────── */

const DATE_FMT = new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short', year: 'numeric' })

function AdminPage() {
  const { me } = useAuth()
  const [departments, setDepartments] = useState<Department[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addDeptOpen, setAddDeptOpen] = useState(false)
  const [addUserOpen, setAddUserOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      const [d, u] = await Promise.all([fetchDepartments(), fetchUsers()])
      setDepartments(d)
      setUsers(u)
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

  return (
    <div className="mx-20">
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

      <section className="results-wrap" style={{ marginBottom: 32 }}>
        <div className="page-header" style={{ marginBottom: 12 }}>
          <h2 className="page-title" style={{ fontSize: '1.1rem' }}>Departments</h2>
          <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddDeptOpen(true)}>
            + Add Department
          </button>
        </div>
        <table className="results-table" aria-label="Departments">
          <thead>
            <tr>
              <th className="col-domain" scope="col">Name</th>
              <th className="col-status" scope="col">Created</th>
            </tr>
          </thead>
          <tbody>
            {departments.map(d => (
              <tr key={d.id}>
                <td className="col-domain">{d.name}</td>
                <td className="col-status">{DATE_FMT.format(new Date(d.created_at))}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <section className="results-wrap">
        <div className="page-header" style={{ marginBottom: 12 }}>
          <h2 className="page-title" style={{ fontSize: '1.1rem' }}>Users</h2>
          <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddUserOpen(true)}>
            + Create User
          </button>
        </div>
        <table className="results-table" aria-label="Users">
          <thead>
            <tr>
              <th className="col-domain" scope="col">Username</th>
              <th className="col-status" scope="col">Department</th>
              <th className="col-status" scope="col">Created</th>
              <th className="col-evidence" scope="col" />
            </tr>
          </thead>
          <tbody>
            {users.map(u => (
              <tr key={u.id}>
                <td className="col-domain">{u.username}</td>
                <td className="col-status">{u.is_admin ? 'Admin' : u.department?.name ?? '—'}</td>
                <td className="col-status">{DATE_FMT.format(new Date(u.created_at))}</td>
                <td className="col-evidence" style={{ textAlign: 'right' }}>
                  <button
                    className="btn-row-delete"
                    onClick={() => setDeleteTarget(u)}
                    aria-label={`Delete ${u.username}`}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <AddDepartmentDialog open={addDeptOpen} onClose={() => setAddDeptOpen(false)} onAdded={load} />
      <AddUserDialog
        open={addUserOpen}
        onClose={() => setAddUserOpen(false)}
        onAdded={load}
        departments={departments}
      />
      <DeleteConfirmDialog
        open={deleteTarget !== null}
        itemLabel={deleteTarget?.username ?? ''}
        onConfirm={handleDeleteUser}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  )
}
