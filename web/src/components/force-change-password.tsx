import { useState } from 'react'
import { changePassword } from '../api/auth'
import { AuroraBars } from '@/components/unlumen-ui/primitives/effects/aurora-bars'

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
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden">
      <AuroraBars className="fixed inset-0 -z-10" gap={0} blur={3.142} />

      <div
        className="rounded-2xl shadow-2xl backdrop-blur-md"
        style={{ width: 380, padding: 32, background: 'var(--auth-card-bg)' }}
      >
        <div className="page-header mb-4" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: 4, padding: 0 }}>
          <h1 className="page-title" style={{ color: 'var(--auth-card-fg)' }}>Set a New Password</h1>
          <p className="page-subtitle" style={{ color: 'var(--auth-card-fg-muted)' }}>
            Your password was reset by an admin. Set your own before continuing.
          </p>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="fcp-current" style={{ color: 'var(--auth-card-label)' }}>Temporary Password</label>
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
            <label className="form-label" htmlFor="fcp-new" style={{ color: 'var(--auth-card-label)' }}>New Password</label>
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
            <label className="form-label" htmlFor="fcp-confirm" style={{ color: 'var(--auth-card-label)' }}>Confirm New Password</label>
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
          {error && <p className="form-error">{error}</p>}
          <button type="submit" className="btn-primary" style={{ width: '100%' }} disabled={loading}>
            {loading ? 'Saving…' : 'Set Password'}
          </button>
        </form>
      </div>
    </div>
  )
}
