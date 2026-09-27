import { useState } from 'react'
import { changePassword } from '../api/auth'
import { AuthShell } from '@/components/auth-shell'

// Blocks the entire app until a temporary password (set by an admin's
// reset) is replaced — see the must_change_password gate in __root.tsx.
// Visually mirrors login.tsx since it's the same "nothing else is usable
// yet" moment, just post-authentication instead of pre-.
export function ForceChangePasswordScreen({ onChanged }: { onChanged: () => Promise<void> }) {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!currentPassword || !newPassword) { setError('Both fields are required'); return }
    if (newPassword !== confirmPassword) { setError('New passwords do not match'); return }
    setLoading(true)
    setError(null)
    try {
      await changePassword(currentPassword, newPassword)
      await onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to change password')
    } finally {
      setLoading(false)
    }
  }

  return (
    <AuthShell title="Set a new password" subtitle="Your password was reset by an admin. Set your own before continuing.">
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="fcp-current">Temporary password</label>
            <input
              id="fcp-current"
              className="form-input"
              type="password"
              value={currentPassword}
              onChange={e => setCurrentPassword(e.target.value)}
              autoFocus
              disabled={loading}
              autoComplete="current-password"
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="fcp-new">New password</label>
            <input
              id="fcp-new"
              className="form-input"
              type="password"
              value={newPassword}
              onChange={e => setNewPassword(e.target.value)}
              disabled={loading}
              autoComplete="new-password"
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="fcp-confirm">Confirm new password</label>
            <input
              id="fcp-confirm"
              className="form-input"
              type="password"
              value={confirmPassword}
              onChange={e => setConfirmPassword(e.target.value)}
              disabled={loading}
              autoComplete="new-password"
            />
          </div>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button type="submit" className="btn-primary" style={{ width: '100%', justifyContent: 'center', padding: '10px 14px' }} disabled={loading}>
            {loading ? 'Saving…' : 'Set password'}
          </button>
        </form>
    </AuthShell>
  )
}
