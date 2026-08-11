import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import {
  type ColumnDef,
  type SortingState,
  type PaginationState,
  getCoreRowModel,
  getSortedRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/motion/tabs'
import { DataGrid, DataGridContainer } from '@/components/reui/data-grid/data-grid'
import { DataGridTable } from '@/components/reui/data-grid/data-grid-table'
import { DataGridPagination } from '@/components/reui/data-grid/data-grid-pagination'
import { Filters, type Filter, type FilterFieldConfig } from '@/components/reui/filters'
import { SortableHeader, EmptyIcon } from '@/components/results-table-parts'
import {
  fetchDepartments,
  createDepartment,
  updateDepartment,
  fetchUsers,
  createUser,
  updateUser,
  deleteUser,
  resetUserPassword,
  fetchCompliantIPs,
  createCompliantIP,
  deleteCompliantIP,
  fetchScanInterval,
  setScanInterval,
  type ScanSchedule,
} from '../api/admin'
import { fetchAgencies, createAgency, updateAgency, deleteAgency } from '../api/agencies'
import { fetchDueDatePresets, createDueDatePreset, deleteDueDatePreset } from '../api/due-date-presets'
import type { Agency, CompliantIP, Department, DueDatePreset, User } from '../api/types'
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
import { Switch } from '@/components/ui/r-switch'
import { Slider } from '@/components/ui/slider'
import { Input } from '@/components/ui/input'
import { SquarePenIcon } from '@/components/ui/square-pen'
import { XIcon } from '@/components/ui/x'
import { KeyRoundIcon } from 'lucide-react'
import { useAuth } from './__root'

const ADMIN_TABS = ['departments', 'users', 'ip', 'agencies', 'due-dates', 'scan-settings'] as const
type AdminTab = typeof ADMIN_TABS[number]

// Departments/Agencies/Users are the tabbed CRUD section at the top of the
// page; Compliant IPs/Time to Block/Scan Settings stay as plain stacked
// sections below (see admin.tsx's route bullet in web/CLAUDE.md).
const CRUD_TABS = ['departments', 'agencies', 'users'] as const
type CrudTab = typeof CRUD_TABS[number]

// Sections gated to is_admin server-side (see `load` below) — a department
// admin never sees these anywhere on the page, not even as a disabled
// placeholder. Covers both the CRUD tabs and the sections below them.
const ADMIN_ONLY_TABS = new Set<AdminTab>(['departments', 'ip', 'scan-settings'])

const IS_ONLY = [{ value: 'is', label: 'is' }]

const ROLE_OPTIONS: { value: 'member' | 'dept_admin' | 'admin'; label: string }[] = [
  { value: 'member', label: 'Member' },
  { value: 'dept_admin', label: 'Department Admin' },
  { value: 'admin', label: 'Admin' },
]

function roleOf(u: User): 'member' | 'dept_admin' | 'admin' {
  return u.is_admin ? 'admin' : u.is_dept_admin ? 'dept_admin' : 'member'
}

export const Route = createFileRoute('/admin/')({
  component: AdminPage,
  // No default — an absent/invalid `tab` means "just landed on the page",
  // distinct from an explicit deep link, so the page opens at the top
  // instead of auto-scrolling to whichever section used to be the default.
  validateSearch: (search: Record<string, unknown>): { tab?: AdminTab } => ({
    tab: ADMIN_TABS.includes(search.tab as AdminTab) ? (search.tab as AdminTab) : undefined,
  }),
})

/* ─── Add / Edit Department Dialogs ─────────────────────────────────────── */

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

function EditDepartmentDialog({
  department,
  onClose,
  onSaved,
}: {
  department: Department | null
  onClose: () => void
  onSaved: () => void
}) {
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => { if (department) setName(department.name) }, [department])

  const handleClose = () => { setError(null); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!department) return
    if (!name.trim()) { setError('Name is required'); return }
    setLoading(true)
    setError(null)
    try {
      await updateDepartment(department.id, name.trim())
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update department')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={department !== null} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 400 }}>
        <DialogHeader>
          <DialogTitle>Edit Department</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="edit-dept-name-input">Name</label>
            <input
              id="edit-dept-name-input"
              className="form-input"
              type="text"
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
              {loading ? 'Saving…' : 'Save'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Add / Edit Agency Dialogs ──────────────────────────────────────────── */

function AddAgencyDialog({
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
      await createAgency(name.trim())
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add agency')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 400 }}>
        <DialogHeader>
          <DialogTitle>Add Agency</DialogTitle>
          <DialogDescription>
            Agencies appear in the Agency dropdown when adding or editing a domain's case details.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="agency-name-input">Name</label>
            <input
              id="agency-name-input"
              className="form-input"
              type="text"
              placeholder="e.g. MCMC"
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
              {loading ? 'Adding…' : 'Add Agency'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function EditAgencyDialog({
  agency,
  onClose,
  onSaved,
}: {
  agency: Agency | null
  onClose: () => void
  onSaved: () => void
}) {
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => { if (agency) setName(agency.name) }, [agency])

  const handleClose = () => { setError(null); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!agency) return
    if (!name.trim()) { setError('Name is required'); return }
    setLoading(true)
    setError(null)
    try {
      await updateAgency(agency.id, name.trim())
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update agency')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={agency !== null} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 400 }}>
        <DialogHeader>
          <DialogTitle>Edit Agency</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="edit-agency-name-input">Name</label>
            <input
              id="edit-agency-name-input"
              className="form-input"
              type="text"
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
              {loading ? 'Saving…' : 'Save'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Add Due-Date Preset Dialog ─────────────────────────────────────────── */

function AddDueDatePresetDialog({
  open,
  onClose,
  onAdded,
}: {
  open: boolean
  onClose: () => void
  onAdded: (preset: DueDatePreset) => void
}) {
  const [label, setLabel] = useState('')
  const [days, setDays] = useState('')
  const [hours, setHours] = useState('')
  const [minutes, setMinutes] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => { setLabel(''); setDays(''); setHours(''); setMinutes(''); setError(null) }
  const handleClose = () => { reset(); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const totalMinutes = (Number(days) || 0) * 1440 + (Number(hours) || 0) * 60 + (Number(minutes) || 0)
    if (!label.trim() || totalMinutes <= 0) { setError('Label and a positive duration are required'); return }
    setLoading(true)
    setError(null)
    try {
      const created = await createDueDatePreset(label.trim(), totalMinutes)
      onAdded(created)
      reset()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add duration')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 400 }}>
        <DialogHeader>
          <DialogTitle>Add Time-to-Block Duration</DialogTitle>
          <DialogDescription>
            Options shown in the "Time to Block" picker when setting a domain's due date on the Watchlist. Minute-level durations are handy for testing due-date notifications.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="preset-label-input">Label</label>
            <input
              id="preset-label-input"
              className="form-input"
              type="text"
              placeholder="e.g. 12 hours"
              value={label}
              onChange={e => setLabel(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          <div className="form-row">
            <div className="form-field">
              <label className="form-label" htmlFor="preset-days-input">Days</label>
              <input
                id="preset-days-input"
                className="form-input"
                type="number"
                min={0}
                value={days}
                onChange={e => setDays(e.target.value)}
                disabled={loading}
              />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="preset-hours-input">Hours</label>
              <input
                id="preset-hours-input"
                className="form-input"
                type="number"
                min={0}
                value={hours}
                onChange={e => setHours(e.target.value)}
                disabled={loading}
              />
            </div>
            <div className="form-field">
              <label className="form-label" htmlFor="preset-minutes-input">Minutes</label>
              <input
                id="preset-minutes-input"
                className="form-input"
                type="number"
                min={0}
                value={minutes}
                onChange={e => setMinutes(e.target.value)}
                disabled={loading}
              />
            </div>
          </div>
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add Duration'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Add / Edit User Dialogs ────────────────────────────────────────────── */

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

// EditUserDialog is only ever opened by a super admin (see UsersTab's
// action-column gating) — username/role/department, no password field
// (that's ResetPasswordDialog's job).
function EditUserDialog({
  user,
  onClose,
  onSaved,
  departments,
}: {
  user: User | null
  onClose: () => void
  onSaved: () => void
  departments: Department[]
}) {
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<'member' | 'dept_admin' | 'admin'>('member')
  const [departmentId, setDepartmentId] = useState<number | ''>('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!user) return
    setUsername(user.username)
    setRole(roleOf(user))
    setDepartmentId(user.department_id ?? '')
  }, [user])

  const showDepartmentPicker = role !== 'admin'
  const handleClose = () => { setError(null); onClose() }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!user) return
    if (!username.trim()) { setError('Username is required'); return }
    if (showDepartmentPicker && departmentId === '') { setError('Department is required'); return }
    setLoading(true)
    setError(null)
    try {
      await updateUser(user.id, {
        username: username.trim(),
        is_admin: role === 'admin',
        is_dept_admin: role === 'dept_admin',
        department_id: showDepartmentPicker ? Number(departmentId) : undefined,
      })
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update user')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={user !== null} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
        <DialogHeader>
          <DialogTitle>Edit User</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="edit-user-username-input">Username</label>
            <input
              id="edit-user-username-input"
              className="form-input"
              type="text"
              value={username}
              onChange={e => setUsername(e.target.value)}
              autoFocus
              disabled={loading}
            />
          </div>
          <div className="form-field">
            <label className="form-label" id="edit-user-role-label">Role</label>
            <Select value={role} onValueChange={v => setRole(v as typeof role)} disabled={loading}>
              <SelectTrigger aria-labelledby="edit-user-role-label" className="w-full" />
              <SelectContent>
                <SelectItem index={0} value="member">Member</SelectItem>
                <SelectItem index={1} value="dept_admin">Department Admin</SelectItem>
                <SelectItem index={2} value="admin">Admin (cross-cutting access, no department of its own)</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {showDepartmentPicker && (
            <div className="form-field">
              <label className="form-label" id="edit-user-department-label">Department</label>
              <Select
                value={String(departmentId)}
                onValueChange={v => setDepartmentId(v === '' ? '' : Number(v))}
                disabled={loading}
              >
                <SelectTrigger aria-labelledby="edit-user-department-label" placeholder="Select a department…" className="w-full" />
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
              {loading ? 'Saving…' : 'Save'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Reset Password Dialog ──────────────────────────────────────────────── */

// Two steps: confirm, then show the generated temp password exactly once —
// the server never returns it again after this response.
function ResetPasswordDialog({
  user,
  onClose,
}: {
  user: User | null
  onClose: () => void
}) {
  const [step, setStep] = useState<'confirm' | 'result'>('confirm')
  const [tempPassword, setTempPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const reset = () => { setStep('confirm'); setTempPassword(''); setError(null); setCopied(false) }
  const handleClose = () => { reset(); onClose() }

  const handleConfirm = async () => {
    if (!user) return
    setLoading(true)
    setError(null)
    try {
      const { temp_password } = await resetUserPassword(user.id)
      setTempPassword(temp_password)
      setStep('result')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to reset password')
    } finally {
      setLoading(false)
    }
  }

  const handleCopy = async () => {
    await navigator.clipboard.writeText(tempPassword)
    setCopied(true)
  }

  return (
    <Dialog open={user !== null} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 400 }}>
        {step === 'confirm' ? (
          <>
            <DialogHeader>
              <DialogTitle>Reset password for "{user?.username}"?</DialogTitle>
              <DialogDescription>
                A new temporary password will be generated. {user?.username} will be required to set their own password before using the app again.
              </DialogDescription>
            </DialogHeader>
            {error && <p className="form-error">{error}</p>}
            <DialogFooter>
              <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
                Cancel
              </button>
              <button type="button" className="btn-primary" onClick={handleConfirm} disabled={loading}>
                {loading ? 'Resetting…' : 'Reset Password'}
              </button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Temporary Password</DialogTitle>
              <DialogDescription>
                Share this with {user?.username} out of band now — it will not be shown again.
              </DialogDescription>
            </DialogHeader>
            <div className="form-field">
              <div className="flex items-center gap-2">
                <input className="form-input" type="text" value={tempPassword} readOnly />
                <button type="button" className="btn-ghost" onClick={handleCopy}>
                  {copied ? 'Copied' : 'Copy'}
                </button>
              </div>
            </div>
            <DialogFooter>
              <button type="button" className="btn-primary" onClick={handleClose}>Done</button>
            </DialogFooter>
          </>
        )}
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

/* ─── Scan Interval Settings ─────────────────────────────────────────────── */

const SCAN_INTERVAL_OPTIONS = [
  { minutes: 15, label: '15 minutes' },
  { minutes: 30, label: '30 minutes' },
  { minutes: 60, label: '1 hour' },
  { minutes: 360, label: '6 hours' },
  { minutes: 720, label: '12 hours' },
  { minutes: 1440, label: '1 day' },
]

// 10 steps, each double the last: 1, 2, 4, ..., 512.
const DNS_WORKER_STEPS = Array.from({ length: 10 }, (_, i) => 2 ** i)

// Snaps an arbitrary stored value (e.g. a legacy non-power-of-2 setting) to
// the closest slider step, so the slider always has a valid position.
function nearestDNSWorkerStep(value: number) {
  return DNS_WORKER_STEPS.reduce((closest, step) =>
    Math.abs(step - value) < Math.abs(closest - value) ? step : closest,
  )
}

function ScanSettingsSection({ value, onSaved }: { value: ScanSchedule; onSaved: (schedule: ScanSchedule) => void }) {
  const [minutes, setMinutes] = useState(value.interval_minutes)
  const [enabled, setEnabled] = useState(value.enabled)
  const [dnsWorkers, setDnsWorkers] = useState(value.dns_workers)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [savedMessage, setSavedMessage] = useState<string | null>(null)

  useEffect(() => {
    setMinutes(value.interval_minutes)
    setEnabled(value.enabled)
    setDnsWorkers(value.dns_workers)
  }, [value])

  const dirty = minutes !== value.interval_minutes || enabled !== value.enabled || dnsWorkers !== value.dns_workers

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    setSavedMessage(null)
    try {
      await setScanInterval(minutes, enabled, dnsWorkers)
      onSaved({ interval_minutes: minutes, enabled, dns_workers: dnsWorkers })
      setSavedMessage(
        enabled
          ? 'Scan is active. It will start the cron job from the moment you saved this setting.'
          : 'Scan schedule saved. The cron job is disabled and will not run.',
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className='mb-4'>
      <div className="page-header" style={{ marginBottom: 12 }}>
        <h2 className="section-title">Scan Settings</h2>
        <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={handleSave} disabled={saving || !dirty}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>

      <div style={{ marginBottom: 16 }}>
        <p className="dash-label-subheading">Scan Frequency</p>
        <p className="page-subtitle" style={{ marginBottom: 8 }}>How often the automated cron sweep runs</p>
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
        </div>
      </div>

      <div style={{ marginBottom: 16 }}>
        <p className="dash-label-subheading">DNS Workers</p>
        <p className="page-subtitle" style={{ marginBottom: 8 }}>Concurrent lookups per DNS server per sweep (default 100) — currently {dnsWorkers}</p>
        <div className="grid w-full max-w-sm gap-4">
          <Slider
            value={[DNS_WORKER_STEPS.indexOf(nearestDNSWorkerStep(dnsWorkers))]}
            onValueChange={([index]) => setDnsWorkers(DNS_WORKER_STEPS[index])}
            min={0}
            max={DNS_WORKER_STEPS.length - 1}
            step={1}
            disabled={saving}
            aria-label="Concurrent DNS workers"
          />
          <span
            aria-hidden="true"
            className="text-muted-foreground flex w-full items-center justify-between gap-1 px-2.5 text-xs font-medium"
          >
            {DNS_WORKER_STEPS.map(step => (
              <span key={step} className="flex w-0 flex-col items-center justify-center gap-2">
                <span className="bg-muted-foreground/70 h-1 w-px" />
                <span>{step}</span>
              </span>
            ))}
          </span>
        </div>
      </div>

      {savedMessage && <p className="form-success" style={{ marginTop: 12 }}>{savedMessage}</p>}
      {error && <p className="form-error" style={{ marginTop: 12 }}>{error}</p>}
    </div>
  )
}

/* ─── Departments Tab ────────────────────────────────────────────────────── */

const DATE_FMT = new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short', year: 'numeric' })

function DepartmentsTab({
  departments,
  loading,
  onAdd,
  onEdit,
}: {
  departments: Department[]
  loading: boolean
  onAdd: () => void
  onEdit: (d: Department) => void
}) {
  const [search, setSearch] = useState('')
  const [sorting, setSorting] = useState<SortingState>([])
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 })

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    return departments.filter(d => !query || d.name.toLowerCase().includes(query))
  }, [departments, search])

  useEffect(() => { setPagination(p => ({ ...p, pageIndex: 0 })) }, [search])

  const columns = useMemo<ColumnDef<Department>[]>(() => [
    {
      id: 'name',
      accessorFn: d => d.name,
      header: ({ column }) => <SortableHeader column={column} title="Name" />,
      enableHiding: false,
      cell: ({ row }) => <span className="font-medium">{row.original.name}</span>,
    },
    {
      id: 'created_at',
      accessorFn: d => d.created_at,
      size: 140,
      header: ({ column }) => <SortableHeader column={column} title="Created" />,
      meta: { headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{DATE_FMT.format(new Date(row.original.created_at))}</span>,
    },
    {
      id: 'action',
      header: 'Action',
      enableHiding: false,
      size: 90,
      meta: { headerClassName: 'th-center', cellClassName: 'text-center' },
      cell: ({ row }) => (
        <button
          type="button"
          className="screenshot-icon-btn"
          onClick={() => onEdit(row.original)}
          aria-label={`Edit ${row.original.name}`}
          title="Edit"
        >
          <SquarePenIcon size={16} />
        </button>
      ),
    },
  ], [onEdit])

  const table = useReactTable({
    data: filtered,
    columns,
    state: { sorting, pagination },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  })

  return (
    <div>
      <div className="page-header" style={{ marginBottom: 12 }}>
        <h2 className="section-title">Departments</h2>
        <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={onAdd}>
          + Add Department
        </button>
      </div>
      <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full" style={{ marginBottom: 16 }}>
        <Input
          type="search"
          placeholder="Search departments..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="max-w-64"
          aria-label="Search departments"
        />
      </div>
      {!loading && filtered.length === 0 ? (
        <div className="empty-state" style={{ padding: '3rem 0' }}>
          <EmptyIcon />
          <p className="empty-heading">{departments.length === 0 ? 'No departments yet' : 'No departments match your search'}</p>
        </div>
      ) : (
        <DataGrid table={table} recordCount={filtered.length} isLoading={loading} loadingMode="spinner" tableClassNames={{ base: 'results-table' }}>
          <DataGridContainer className="overflow-x-auto overflow-y-visible mb-5">
            <DataGridTable />
          </DataGridContainer>
          <DataGridPagination sizes={[10, 25, 50]} />
        </DataGrid>
      )}
    </div>
  )
}

/* ─── Agencies Tab ───────────────────────────────────────────────────────── */

function AgenciesTab({
  agencies,
  loading,
  onAdd,
  onEdit,
  onDeleteRequest,
}: {
  agencies: Agency[]
  loading: boolean
  onAdd: () => void
  onEdit: (a: Agency) => void
  onDeleteRequest: (a: Agency) => void
}) {
  const [search, setSearch] = useState('')
  const [sorting, setSorting] = useState<SortingState>([])
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 })

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    return agencies.filter(a => !query || a.name.toLowerCase().includes(query))
  }, [agencies, search])

  useEffect(() => { setPagination(p => ({ ...p, pageIndex: 0 })) }, [search])

  const columns = useMemo<ColumnDef<Agency>[]>(() => [
    {
      id: 'name',
      accessorFn: a => a.name,
      header: ({ column }) => <SortableHeader column={column} title="Name" />,
      enableHiding: false,
      cell: ({ row }) => <span className="font-medium">{row.original.name}</span>,
    },
    {
      id: 'created_at',
      accessorFn: a => a.created_at,
      size: 140,
      header: ({ column }) => <SortableHeader column={column} title="Created" />,
      meta: { headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{DATE_FMT.format(new Date(row.original.created_at))}</span>,
    },
    {
      id: 'action',
      header: 'Action',
      enableHiding: false,
      size: 100,
      meta: { headerClassName: 'th-center', cellClassName: 'text-center' },
      cell: ({ row }) => (
        <div className="flex items-center justify-center gap-1">
          <button
            type="button"
            className="screenshot-icon-btn"
            onClick={() => onEdit(row.original)}
            aria-label={`Edit ${row.original.name}`}
            title="Edit"
          >
            <SquarePenIcon size={16} />
          </button>
          <button
            type="button"
            className="screenshot-icon-btn"
            onClick={() => onDeleteRequest(row.original)}
            aria-label={`Delete ${row.original.name}`}
            title="Delete"
          >
            <XIcon size={16} />
          </button>
        </div>
      ),
    },
  ], [onEdit, onDeleteRequest])

  const table = useReactTable({
    data: filtered,
    columns,
    state: { sorting, pagination },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  })

  return (
    <div>
      <div className="page-header" style={{ marginBottom: 12 }}>
        <h2 className="section-title">Agencies</h2>
        <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={onAdd}>
          + Add Agency
        </button>
      </div>
      <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full" style={{ marginBottom: 16 }}>
        <Input
          type="search"
          placeholder="Search agencies..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="max-w-64"
          aria-label="Search agencies"
        />
      </div>
      {!loading && filtered.length === 0 ? (
        <div className="empty-state" style={{ padding: '3rem 0' }}>
          <EmptyIcon />
          <p className="empty-heading">{agencies.length === 0 ? 'No agencies yet' : 'No agencies match your search'}</p>
        </div>
      ) : (
        <DataGrid table={table} recordCount={filtered.length} isLoading={loading} loadingMode="spinner" tableClassNames={{ base: 'results-table' }}>
          <DataGridContainer className="overflow-x-auto overflow-y-visible mb-5">
            <DataGridTable />
          </DataGridContainer>
          <DataGridPagination sizes={[10, 25, 50]} />
        </DataGrid>
      )}
    </div>
  )
}

/* ─── Users Tab ──────────────────────────────────────────────────────────── */

function UsersTab({
  users,
  departments,
  me,
  loading,
  onAdd,
  onEdit,
  onDeleteRequest,
  onResetRequest,
}: {
  users: User[]
  departments: Department[]
  me: User | null | undefined
  loading: boolean
  onAdd: () => void
  onEdit: (u: User) => void
  onDeleteRequest: (u: User) => void
  onResetRequest: (u: User) => void
}) {
  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<Filter<string>[]>([])
  const [sorting, setSorting] = useState<SortingState>([])
  const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 })

  // Department filter only makes sense for a super admin's global view — a
  // department admin only ever sees their own department's users anyway
  // (server-scoped), and `departments` itself is only admin-fetched.
  const filterFields = useMemo<FilterFieldConfig<string>[]>(() => {
    const fields: FilterFieldConfig<string>[] = [
      { key: 'role', label: 'Role', type: 'select', operators: IS_ONLY, options: ROLE_OPTIONS },
    ]
    if (me?.is_admin) {
      fields.push({ key: 'department', label: 'Department', type: 'select', operators: IS_ONLY, options: departments.map(d => ({ value: String(d.id), label: d.name })) })
    }
    return fields
  }, [departments, me?.is_admin])

  const roleFilter = filters.find(f => f.field === 'role')?.values[0]
  const deptFilter = filters.find(f => f.field === 'department')?.values[0]

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    return users.filter(u =>
      (!query || u.username.toLowerCase().includes(query)) &&
      (!roleFilter || roleOf(u) === roleFilter) &&
      (!deptFilter || String(u.department_id ?? '') === deptFilter),
    )
  }, [users, search, roleFilter, deptFilter])

  useEffect(() => { setPagination(p => ({ ...p, pageIndex: 0 })) }, [search, roleFilter, deptFilter])

  const columns = useMemo<ColumnDef<User>[]>(() => [
    {
      id: 'username',
      accessorFn: u => u.username,
      header: ({ column }) => <SortableHeader column={column} title="Username" />,
      enableHiding: false,
      cell: ({ row }) => <span className="font-medium">{row.original.username}</span>,
    },
    {
      id: 'role',
      accessorFn: u => roleOf(u),
      size: 170,
      header: 'Role',
      meta: { headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{ROLE_OPTIONS.find(o => o.value === roleOf(row.original))?.label}</span>,
    },
    {
      id: 'department',
      accessorFn: u => u.department?.name ?? '',
      size: 140,
      header: 'Department',
      meta: { headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{row.original.is_admin ? '—' : row.original.department?.name ?? '—'}</span>,
    },
    {
      id: 'created_at',
      accessorFn: u => u.created_at,
      size: 120,
      header: ({ column }) => <SortableHeader column={column} title="Created" />,
      meta: { headerClassName: 'col-status', cellClassName: 'col-status text-center' },
      cell: ({ row }) => <span className="dns-name">{DATE_FMT.format(new Date(row.original.created_at))}</span>,
    },
    {
      id: 'action',
      header: 'Action',
      enableHiding: false,
      size: 140,
      meta: { headerClassName: 'th-center', cellClassName: 'text-center' },
      cell: ({ row }) => {
        const u = row.original
        // Mirrors the server's own DeleteUser/ResetUserPassword scoping —
        // a department admin only manages a plain member of their own
        // department (already guaranteed here since ListUsers already
        // scopes their rows), never another admin/dept-admin.
        const canResetOrEdit = me?.is_admin || (me?.is_dept_admin && !u.is_admin && !u.is_dept_admin)
        return (
          <div className="flex items-center justify-center gap-1">
            {canResetOrEdit && (
              <button
                type="button"
                className="screenshot-icon-btn"
                onClick={() => onResetRequest(u)}
                aria-label={`Reset password for ${u.username}`}
                title="Reset Password"
              >
                <KeyRoundIcon size={16} />
              </button>
            )}
            {me?.is_admin && (
              <button
                type="button"
                className="screenshot-icon-btn"
                onClick={() => onEdit(u)}
                aria-label={`Edit ${u.username}`}
                title="Edit"
              >
                <SquarePenIcon size={16} />
              </button>
            )}
            <button
              type="button"
              className="screenshot-icon-btn"
              onClick={() => onDeleteRequest(u)}
              aria-label={`Delete ${u.username}`}
              title="Delete"
            >
              <XIcon size={16} />
            </button>
          </div>
        )
      },
    },
  ], [me, onEdit, onDeleteRequest, onResetRequest])

  const table = useReactTable({
    data: filtered,
    columns,
    state: { sorting, pagination },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  })

  return (
    <div>
      <div className="page-header" style={{ marginBottom: 12 }}>
        <h2 className="section-title">Users</h2>
        <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={onAdd}>
          + Create User
        </button>
      </div>
      <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full" style={{ marginBottom: 16 }}>
        <Input
          type="search"
          placeholder="Search username..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="max-w-64"
          aria-label="Search username"
        />
        <Filters filters={filters} fields={filterFields} onChange={setFilters} />
      </div>
      {!loading && filtered.length === 0 ? (
        <div className="empty-state" style={{ padding: '3rem 0' }}>
          <EmptyIcon />
          <p className="empty-heading">{users.length === 0 ? 'No users yet' : 'No users match the current filters'}</p>
        </div>
      ) : (
        <DataGrid table={table} recordCount={filtered.length} isLoading={loading} loadingMode="spinner" tableClassNames={{ base: 'results-table' }}>
          <DataGridContainer className="overflow-x-auto overflow-y-visible mb-5">
            <DataGridTable />
          </DataGridContainer>
          <DataGridPagination sizes={[10, 25, 50]} />
        </DataGrid>
      )}
    </div>
  )
}

/* ─── Time to Block ──────────────────────────────────────────────────────── */

function formatDuration(totalMinutes: number): string {
  const d = Math.floor(totalMinutes / 1440)
  const h = Math.floor((totalMinutes % 1440) / 60)
  const m = totalMinutes % 60
  const parts = [d && `${d}d`, h && `${h}h`, m && `${m}m`].filter(Boolean)
  return parts.length ? parts.join(' ') : '0m'
}

// Bar length is log-scaled (not linear) because presets can span minutes to
// weeks — a linear scale would make a 2-minute test preset invisible next to
// a 7-day one. Floored so even the shortest preset stays visibly present.
function presetBarPct(minutes: number, maxMinutes: number): number {
  if (maxMinutes <= 0) return 0
  return Math.max((Math.log1p(minutes) / Math.log1p(maxMinutes)) * 100, 4)
}

/* ─── Admin Page ─────────────────────────────────────────────────────────── */

function AdminPage() {
  const { me } = useAuth()
  const { tab: deepLinkTab } = Route.useSearch()
  const navigate = useNavigate()

  // Compliant IPs / Time to Block / Scan Settings — plain stacked sections
  // below the CRUD tabs; sectionRefs only backs the deep-link scroll below.
  const sectionRefs = useRef<Partial<Record<AdminTab, HTMLDivElement | null>>>({})
  const didDeepLinkScroll = useRef(false)

  // Departments/Agencies/Users — the tabbed CRUD section above the fold.
  const visibleCrudTabs = useMemo(
    () => CRUD_TABS.filter(id => !ADMIN_ONLY_TABS.has(id) || me?.is_admin),
    [me],
  )
  const [crudTab, setCrudTab] = useState<CrudTab>(
    deepLinkTab && (CRUD_TABS as readonly string[]).includes(deepLinkTab) ? (deepLinkTab as CrudTab) : (visibleCrudTabs[0] ?? 'users'),
  )

  const [departments, setDepartments] = useState<Department[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [compliantIPs, setCompliantIPs] = useState<CompliantIP[]>([])
  const [agencies, setAgencies] = useState<Agency[]>([])
  const [duePresets, setDuePresets] = useState<DueDatePreset[]>([])
  const [scanSchedule, setScanSchedule] = useState<ScanSchedule | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addDeptOpen, setAddDeptOpen] = useState(false)
  const [editDeptTarget, setEditDeptTarget] = useState<Department | null>(null)
  const [addUserOpen, setAddUserOpen] = useState(false)
  const [editUserTarget, setEditUserTarget] = useState<User | null>(null)
  const [resetPasswordTarget, setResetPasswordTarget] = useState<User | null>(null)
  const [addIPOpen, setAddIPOpen] = useState(false)
  const [addAgencyOpen, setAddAgencyOpen] = useState(false)
  const [editAgencyTarget, setEditAgencyTarget] = useState<Agency | null>(null)
  const [addPresetOpen, setAddPresetOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null)
  const [deleteIPTarget, setDeleteIPTarget] = useState<CompliantIP | null>(null)
  const [deleteAgencyTarget, setDeleteAgencyTarget] = useState<Agency | null>(null)
  const [deletePresetTarget, setDeletePresetTarget] = useState<DueDatePreset | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      // Agencies and due-date presets are readable by both roles
      // (admin-or-dept-admin manageable, unlike Departments/Compliant-IPs/
      // scan interval, which stay super-admin-only server-side — a
      // department admin would just get a 403 fetching those).
      if (me?.is_admin) {
        const [d, u, ips, schedule, a, p] = await Promise.all([
          fetchDepartments(), fetchUsers(), fetchCompliantIPs(), fetchScanInterval(), fetchAgencies(), fetchDueDatePresets(),
        ])
        setDepartments(d)
        setUsers(u)
        setCompliantIPs(ips)
        setScanSchedule(schedule)
        setAgencies(a)
        setDuePresets(p)
      } else {
        const [u, a, p] = await Promise.all([fetchUsers(), fetchAgencies(), fetchDueDatePresets()])
        setUsers(u)
        setAgencies(a)
        setDuePresets(p)
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

  // Deep link (?tab=) jumps to a section once, on landing — the CRUD tabs
  // (Departments/Agencies/Users) don't need this, since Tabs already opens
  // on the right value from crudTab's initial state above.
  useEffect(() => {
    if (!deepLinkTab || (CRUD_TABS as readonly string[]).includes(deepLinkTab) || loading || didDeepLinkScroll.current) return
    didDeepLinkScroll.current = true
    requestAnimationFrame(() => {
      sectionRefs.current[deepLinkTab]?.scrollIntoView({ block: 'start' })
    })
  }, [deepLinkTab, loading])

  const handleCrudTabChange = useCallback((id: string) => {
    setCrudTab(id as CrudTab)
    navigate({ to: '/admin', search: { tab: id as AdminTab }, replace: true })
  }, [navigate])

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

  const handleDeleteAgency = async () => {
    if (!deleteAgencyTarget) return
    await deleteAgency(deleteAgencyTarget.id)
    setDeleteAgencyTarget(null)
    load()
  }

  const handleDeletePreset = async () => {
    if (!deletePresetTarget) return
    await deleteDueDatePreset(deletePresetTarget.id)
    setDeletePresetTarget(null)
    load()
  }

  const maxPresetMinutes = Math.max(1, ...duePresets.map(p => p.minutes))

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

      <Tabs value={crudTab} onValueChange={handleCrudTabChange} variant="underline">
        <TabsList>
          {visibleCrudTabs.includes('departments') && <TabsTrigger value="departments">Departments</TabsTrigger>}
          <TabsTrigger value="agencies">Agencies</TabsTrigger>
          <TabsTrigger value="users">Users</TabsTrigger>
        </TabsList>

        {visibleCrudTabs.includes('departments') && (
          <TabsContent value="departments">
            <DepartmentsTab
              departments={departments}
              loading={loading}
              onAdd={() => setAddDeptOpen(true)}
              onEdit={setEditDeptTarget}
            />
          </TabsContent>
        )}
        <TabsContent value="agencies">
          <AgenciesTab
            agencies={agencies}
            loading={loading}
            onAdd={() => setAddAgencyOpen(true)}
            onEdit={setEditAgencyTarget}
            onDeleteRequest={setDeleteAgencyTarget}
          />
        </TabsContent>
        <TabsContent value="users">
          <UsersTab
            users={users}
            departments={departments}
            me={me}
            loading={loading}
            onAdd={() => setAddUserOpen(true)}
            onEdit={setEditUserTarget}
            onDeleteRequest={setDeleteTarget}
            onResetRequest={setResetPasswordTarget}
          />
        </TabsContent>
      </Tabs>

      <div className="flex flex-col gap-10 my-10">
        {me?.is_admin && (
          <div id="ip" ref={el => { sectionRefs.current.ip = el }} data-section="ip" className="scroll-mt-24">
            <div className="page-header" style={{ marginBottom: 12 }}>
              <h2 className="section-title">Compliant IPs</h2>
              <p className="page-subtitle" style={{ marginLeft: 8 }}>DNS resolutions to these IPs are classified as compliant</p>
              <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddIPOpen(true)}>
                + Add IP
              </button>
            </div>
            <div className="border border-stone-border rounded-lg overflow-hidden">
              {compliantIPs.map(ip => (
                <div key={ip.id} className="admin-row py-[9px] px-4 flex items-center gap-4">
                  <span className="ip-value flex-1">{ip.address}</span>
                  <span className="text-stone-muted text-sm">{ip.note || '—'}</span>
                  <span className="dns-name">{DATE_FMT.format(new Date(ip.created_at))}</span>
                  <button
                    type="button"
                    className="screenshot-icon-btn"
                    onClick={() => setDeleteIPTarget(ip)}
                    aria-label={`Delete ${ip.address}`}
                    title="Delete"
                  >
                    <XIcon size={16} />
                  </button>
                </div>
              ))}
              {compliantIPs.length === 0 && !loading && (
                <p className="text-center text-stone-muted py-4">No compliant IPs configured</p>
              )}
            </div>
          </div>
        )}

        <div className="flex flex-row gap-10">
          {me?.is_admin && scanSchedule !== null && (
            <div id="scan-settings" ref={el => { sectionRefs.current['scan-settings'] = el }} data-section="scan-settings" className="scroll-mt-24 flex-1 min-w-0">
              <ScanSettingsSection value={scanSchedule} onSaved={setScanSchedule} />
            </div>
          )}

          <div id="due-dates" ref={el => { sectionRefs.current['due-dates'] = el }} data-section="due-dates" className="scroll-mt-24 flex-1 min-w-0">
            <div className="page-header" style={{ marginBottom: 4 }}>
              <h2 className="section-title">Time to Block</h2>
              <button className="btn-primary" style={{ marginLeft: 'auto' }} onClick={() => setAddPresetOpen(true)}>
                + Add Duration
              </button>
            </div>
            <p className="page-subtitle" style={{ marginBottom: 12 }}>Duration options offered in the "Time to Block" picker when setting a domain's due date on the Watchlist</p>
            <div className="border border-stone-border rounded-lg overflow-hidden">
              {duePresets.map(p => (
                <div key={p.id} className="admin-row py-[9px] px-4 flex items-center gap-4">
                  <span className="flex-1 font-medium truncate">{p.label}</span>
                  <div className="server-bar-wrap" style={{ width: 240 }}>
                    <div className="server-bar" role="presentation">
                      <div className="server-bar-fill" style={{ width: `${presetBarPct(p.minutes, maxPresetMinutes)}%` }} />
                    </div>
                    <span className="server-count">{formatDuration(p.minutes)}</span>
                  </div>
                  <button
                    type="button"
                    className="screenshot-icon-btn"
                    onClick={() => setDeletePresetTarget(p)}
                    aria-label={`Delete duration ${p.label}`}
                    title="Delete"
                  >
                    <XIcon size={16} />
                  </button>
                </div>
              ))}
              {duePresets.length === 0 && !loading && (
                <p className="text-center text-stone-muted py-4">No durations configured</p>
              )}
            </div>
          </div>
        </div>
      </div>

      <AddDepartmentDialog open={addDeptOpen} onClose={() => setAddDeptOpen(false)} onAdded={load} />
      <EditDepartmentDialog department={editDeptTarget} onClose={() => setEditDeptTarget(null)} onSaved={load} />
      <AddUserDialog
        open={addUserOpen}
        onClose={() => setAddUserOpen(false)}
        onAdded={load}
        departments={departments}
        callerIsSuperAdmin={!!me?.is_admin}
      />
      <EditUserDialog user={editUserTarget} onClose={() => setEditUserTarget(null)} onSaved={load} departments={departments} />
      <ResetPasswordDialog user={resetPasswordTarget} onClose={() => setResetPasswordTarget(null)} />
      <AddCompliantIPDialog open={addIPOpen} onClose={() => setAddIPOpen(false)} onAdded={load} />
      <AddAgencyDialog open={addAgencyOpen} onClose={() => setAddAgencyOpen(false)} onAdded={load} />
      <EditAgencyDialog agency={editAgencyTarget} onClose={() => setEditAgencyTarget(null)} onSaved={load} />
      <AddDueDatePresetDialog
        open={addPresetOpen}
        onClose={() => setAddPresetOpen(false)}
        onAdded={preset => setDuePresets(prev => [...prev, preset].sort((a, b) => a.minutes - b.minutes))}
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
        open={deleteAgencyTarget !== null}
        itemLabel={deleteAgencyTarget?.name ?? ''}
        description="Domains currently assigned to this agency will show a blank Agency instead."
        onConfirm={handleDeleteAgency}
        onCancel={() => setDeleteAgencyTarget(null)}
      />
      <DeleteConfirmDialog
        open={deletePresetTarget !== null}
        itemLabel={deletePresetTarget?.label ?? ''}
        description="It will no longer appear as an option in the Watchlist's Time to Block picker."
        onConfirm={handleDeletePreset}
        onCancel={() => setDeletePresetTarget(null)}
      />
    </div>
  )
}
