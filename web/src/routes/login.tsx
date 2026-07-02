import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { login } from '../api/auth'
import { useAuth } from './__root'
import { AuroraBars } from '@/components/unlumen-ui/primitives/effects/aurora-bars'

export const Route = createFileRoute('/login')({ component: LoginPage })

function LoginPage() {
  const { refresh } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    setError(null)
    try {
      await login(username, password)
      await refresh()
    } catch {
      setError('Invalid username or password')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden">
      <AuroraBars className="fixed inset-0 -z-10" gap={0} />

      <div
        className="rounded-2xl shadow-2xl backdrop-blur-md"
        style={{ width: 360, padding: 32, background: 'rgba(20, 20, 22, 0.45)' }}
      >
        <div className="page-header mb-4" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: 4, padding: 0 }}>
          <h1 className="page-title" style={{ color: '#fff' }}>DNS Compliance</h1>
          <p className="page-subtitle" style={{ color: 'rgba(255,255,255,0.6)' }}>Sign in to continue</p>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="login-username" style={{ color: 'rgba(255,255,255,0.85)' }}>Username</label>
            <input
              id="login-username"
              className="form-input"
              type="text"
              value={username}
              onChange={e => setUsername(e.target.value)}
              autoFocus
              disabled={loading}
              autoComplete="username"
            />
          </div>
          <div className="form-field">
            <label className="form-label" htmlFor="login-password" style={{ color: 'rgba(255,255,255,0.85)' }}>Password</label>
            <input
              id="login-password"
              className="form-input"
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              disabled={loading}
              autoComplete="current-password"
            />
          </div>
          {error && <p className="form-error">{error}</p>}
          <button type="submit" className="btn-primary" style={{ width: '100%' }} disabled={loading}>
            {loading ? 'Signing in…' : 'Sign In'}
          </button>
        </form>
      </div>
    </div>
  )
}
