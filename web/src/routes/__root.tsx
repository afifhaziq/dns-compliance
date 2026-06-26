import '@fontsource/dm-sans/400.css'
import '@fontsource/dm-sans/500.css'
import '@fontsource/dm-sans/600.css'
import '@fontsource/geist-mono/400.css'

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react'
import {
  createRootRoute,
  Navigate,
  Outlet,
  useLocation,
} from '@tanstack/react-router'
import { TanStackRouterDevtools } from '@tanstack/router-devtools'
import { fetchScanStatus, isScanning, triggerScan } from '../api/scan'
import { fetchMe, logout as apiLogout } from '../api/auth'
import { fetchUrls } from '../api/urls'
import type { User, URLEntry } from '../api/types'
import { ThemeSwitch } from '../components/theme-switch'
import { ShimmerButton } from '../components/shimmer-button'
import { GlassNavbar, LogoutButton } from '../components/aicanvas/glass-navbar'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/animate-ui/components/radix/dialog'

/* ─── Scan Selected Dialog ─────────────────────────────────────────────────── */

function ScanSelectedDialog({
  open,
  onClose,
  onStart,
}: {
  open: boolean
  onClose: () => void
  onStart: (urls: string[]) => void
}) {
  const [watchlistUrls, setWatchlistUrls] = useState<URLEntry[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [adhoc, setAdhoc] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    fetchUrls().then(urls => {
      setWatchlistUrls(urls)
      setSelected(new Set(urls.filter(u => u.enabled).map(u => u.url)))
    }).catch(() => {})
  }, [open])

  const toggle = (url: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      next.has(url) ? next.delete(url) : next.add(url)
      return next
    })
  }

  const handleStart = () => {
    const adhocList = adhoc.split('\n').map(s => s.trim()).filter(Boolean)
    const all = [...Array.from(selected), ...adhocList]
    if (all.length === 0) { setError('Select at least one domain'); return }
    onStart(all)
    setAdhoc('')
    setError(null)
    onClose()
  }

  const handleClose = () => { setAdhoc(''); setError(null); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 480 }}>
        <DialogHeader>
          <DialogTitle>Scan Selected Domains</DialogTitle>
          <DialogDescription>
            Choose domains from your watchlist or enter additional ones. Each domain is scanned once regardless of how many departments watch it.
          </DialogDescription>
        </DialogHeader>

        {watchlistUrls.length > 0 && (
          <div style={{ maxHeight: 220, overflowY: 'auto', marginBottom: 12 }}>
            {watchlistUrls.map(u => (
              <label
                key={u.id}
                style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 0', cursor: 'pointer', fontSize: '0.875rem' }}
              >
                <input
                  type="checkbox"
                  checked={selected.has(u.url)}
                  onChange={() => toggle(u.url)}
                />
                <span>{u.url}</span>
              </label>
            ))}
          </div>
        )}

        <div className="form-field">
          <label className="form-label" htmlFor="scan-adhoc-input">
            Additional domains{' '}
            <span style={{ fontWeight: 400 }}>(one per line, not saved to watchlist)</span>
          </label>
          <textarea
            id="scan-adhoc-input"
            className="form-input"
            placeholder={'adhoc-domain.com\nanother.com'}
            value={adhoc}
            onChange={e => setAdhoc(e.target.value)}
            rows={3}
            style={{ resize: 'vertical', fontFamily: 'inherit' }}
          />
        </div>

        {error && <p className="form-error">{error}</p>}

        <DialogFooter>
          <button className="btn-ghost" onClick={handleClose}>Cancel</button>
          <button className="btn-primary" onClick={handleStart}>Start Scan</button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/* ─── Auth Context ─────────────────────────────────────────────────────────── */

type AuthContextValue = {
  me: User | null
  loading: boolean
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue>({
  me: null,
  loading: true,
  logout: async () => {},
  refresh: async () => {},
})

export const useAuth = () => useContext(AuthContext)

/* ─── Scan Context ─────────────────────────────────────────────────────────── */

type ScanContextValue = {
  scanning: boolean
  refreshSignal: number
  handleScanClick: () => void
}

const ScanContext = createContext<ScanContextValue>({
  scanning: false,
  refreshSignal: 0,
  handleScanClick: () => {},
})

export const useScan = () => useContext(ScanContext)

/* ─── Root Component ───────────────────────────────────────────────────────── */

function RootLayout() {
  const location = useLocation()
  const [me, setMe] = useState<User | null>(null)
  const [authLoading, setAuthLoading] = useState(true)

  const [scanning, setScanning] = useState(false)
  const [refreshSignal, setRefreshSignal] = useState(0)
  const [scanSelectedOpen, setScanSelectedOpen] = useState(false)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const refreshAuth = useCallback(async () => {
    const u = await fetchMe()
    setMe(u)
    setAuthLoading(false)
  }, [])

  useEffect(() => { refreshAuth() }, [refreshAuth])

  const logout = useCallback(async () => {
    await apiLogout()
    setMe(null)
  }, [])

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current)
      pollRef.current = null
    }
  }, [])

  const startPolling = useCallback(() => {
    stopPolling()
    pollRef.current = setInterval(async () => {
      try {
        const status = await fetchScanStatus()
        if (!isScanning(status)) {
          setScanning(false)
          setRefreshSignal(s => s + 1)
          stopPolling()
        }
      } catch {
        // keep polling; transient errors during scan are expected
      }
    }, 3000)
  }, [stopPolling])

  // on mount (once authenticated): check if a scan is already running
  useEffect(() => {
    if (!me) return
    fetchScanStatus()
      .then(status => {
        if (isScanning(status)) {
          setScanning(true)
          startPolling()
        }
      })
      .catch(() => {})

    return stopPolling
  }, [me, startPolling, stopPolling])

  const handleScanClick = useCallback(async () => {
    if (scanning) return
    try {
      await triggerScan()
      setScanning(true)
      startPolling()
    } catch (err) {
      console.error('Scan trigger failed:', err)
    }
  }, [scanning, startPolling])

  const handleScanSelected = useCallback(async (urls: string[]) => {
    if (scanning) return
    try {
      await triggerScan(urls)
      setScanning(true)
      startPolling()
    } catch (err) {
      console.error('Targeted scan trigger failed:', err)
    }
  }, [scanning, startPolling])

  const authValue: AuthContextValue = { me, loading: authLoading, logout, refresh: refreshAuth }
  const isLoginRoute = location.pathname === '/login'

  if (authLoading) return null

  if (!me && !isLoginRoute) {
    return (
      <AuthContext value={authValue}>
        <Navigate to="/login" />
      </AuthContext>
    )
  }

  if (me && isLoginRoute) {
    return (
      <AuthContext value={authValue}>
        <Navigate to="/" />
      </AuthContext>
    )
  }

  if (isLoginRoute) {
    return (
      <AuthContext value={authValue}>
        <Outlet />
      </AuthContext>
    )
  }

  return (
    <AuthContext value={authValue}>
      <ScanContext value={{ scanning, refreshSignal, handleScanClick }}>
        <a href="#main" className="skip-link">Skip to main content</a>

        <GlassNavbar
          actions={
            <>
              <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
                <ShimmerButton
                  text="Scan All"
                  scanning={scanning}
                  disabled={scanning}
                  onClick={handleScanClick}
                  ariaLabel={scanning ? 'Scan in progress' : 'Scan all watched domains'}
                />
                <button
                  className="btn-ghost"
                  disabled={scanning}
                  onClick={() => setScanSelectedOpen(true)}
                  aria-label="Scan selected domains"
                  style={{ fontSize: '0.8125rem' }}
                >
                  Scan Selected
                </button>
              </div>
              <ThemeSwitch />
              <LogoutButton />
            </>
          }
        />

        <ScanSelectedDialog
          open={scanSelectedOpen}
          onClose={() => setScanSelectedOpen(false)}
          onStart={handleScanSelected}
        />

        <main id="main" className="max-w-10xl mx-auto">
          <Outlet />
        </main>

        {import.meta.env.DEV && <TanStackRouterDevtools />}
      </ScanContext>
    </AuthContext>
  )
}

/* ─── Route ─────────────────────────────────────────────────────────────────── */

export const Route = createRootRoute({ component: RootLayout })
