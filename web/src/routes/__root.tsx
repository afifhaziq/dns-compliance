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
  Outlet,
} from '@tanstack/react-router'
import { TanStackRouterDevtools } from '@tanstack/router-devtools'
import { fetchScanStatus, isScanning, triggerScan } from '../api/scan'
import { ThemeSwitch } from '../components/theme-switch'
import { ShimmerButton } from '../components/shimmer-button'
import { GlassNavbar } from '../components/aicanvas/glass-navbar'

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
  const [scanning, setScanning] = useState(false)
  const [refreshSignal, setRefreshSignal] = useState(0)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

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

  // on mount: check if a scan is already running
  useEffect(() => {
    fetchScanStatus()
      .then(status => {
        if (isScanning(status)) {
          setScanning(true)
          startPolling()
        }
      })
      .catch(() => {})

    return stopPolling
  }, [startPolling, stopPolling])

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

  return (
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
          </>
        }
      />

      <main id="main" className="page">
        <Outlet />
      </main>

      {import.meta.env.DEV && <TanStackRouterDevtools />}
    </ScanContext>
  )
}

/* ─── Route ─────────────────────────────────────────────────────────────────── */

export const Route = createRootRoute({ component: RootLayout })
