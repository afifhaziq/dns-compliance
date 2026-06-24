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
import type { User } from '../api/types'
import { ThemeSwitch } from '../components/theme-switch'
import { ShimmerButton } from '../components/shimmer-button'
import { GlassNavbar, LogoutButton } from '../components/aicanvas/glass-navbar'

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
              <ShimmerButton
                text="Run Scan"
                scanning={scanning}
                disabled={scanning}
                onClick={handleScanClick}
                ariaLabel={scanning ? 'Scan in progress' : 'Run compliance scan'}
              />
              <ThemeSwitch />
              <LogoutButton />
            </>
          }
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
