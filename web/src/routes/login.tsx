import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { login } from '../api/auth'
import { useAuth } from './__root'
import { AuroraBars } from '@/components/unlumen-ui/aurora-bars'

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
      <div className="fixed inset-0 -z-10">
        <AuroraBars />
      </div>
      <div className="relative z-10 bg-card text-card-foreground rounded-xl shadow-2xl p-8" style={{ width: 360 }}>
        <div className="page-header" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: 4 }}>
          <h1 className="page-title">DNS Compliance</h1>
          <p className="page-subtitle">Sign in to continue</p>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="login-username">Username</label>
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
            <label className="form-label" htmlFor="login-password">Password</label>
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
