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
  useNavigate,
} from '@tanstack/react-router'
import { TanStackRouterDevtools } from '@tanstack/router-devtools'
import { fetchScanStatus, isScanning, triggerScan } from '../api/scan'
import { fetchMe, logout as apiLogout } from '../api/auth'
import { fetchResults } from '../api/results'
import { fetchUrls } from '../api/urls'
import type { User, URLEntry } from '../api/types'
import { ThemeSwitch } from '../components/theme-switch'
import { GlassNavbar, LogoutButton } from '../components/aicanvas/glass-navbar'
import { IconBar, IconBarItem } from '@/components/ui/icon-bar'
import { Zap, Crosshair, X, Plus } from 'lucide-react'
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
  const [chips, setChips] = useState<string[]>([])
  const [inputText, setInputText] = useState('')
  const [dropdownOpen, setDropdownOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    fetchUrls().then(urls => {
      setWatchlistUrls(urls)
    }).catch(() => {})
  }, [open])

  const available = watchlistUrls.map(u => u.url).filter(url => !chips.includes(url))
  const filtered = inputText
    ? available.filter(url => url.toLowerCase().includes(inputText.toLowerCase()))
    : []

  const addChip = (value: string) => {
    const t = value.trim()
    if (!t || chips.includes(t)) return
    setChips(prev => [...prev, t])
    setInputText('')
    setDropdownOpen(false)
    setTimeout(() => inputRef.current?.focus(), 0)
  }

  const removeChip = (url: string) => setChips(prev => prev.filter(c => c !== url))

  const handleStart = () => {
    if (chips.length === 0) { setError('Select at least one domain'); return }
    onStart(chips)
    setError(null)
    onClose()
  }

  const handleClose = () => {
    setChips([])
    setInputText('')
    setDropdownOpen(false)
    setError(null)
    onClose()
  }

  const showAddNew = Boolean(inputText.trim()) && !chips.includes(inputText.trim())
  const showDropdown = dropdownOpen && (filtered.length > 0 || showAddNew)

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 500 }}>
        <DialogHeader>
          <DialogTitle>Scan Selected Domains</DialogTitle>
          <DialogDescription>
            Search your watchlist or type a domain to add it.
          </DialogDescription>
        </DialogHeader>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {/* Input + dropdown */}
          <div style={{ position: 'relative' }}>
            <input
              ref={inputRef}
              className="form-input"
              value={inputText}
              placeholder="Search watchlist or type a domain..."
              onChange={e => {
                setInputText(e.target.value)
                setDropdownOpen(true)
              }}
              onBlur={() => setTimeout(() => setDropdownOpen(false), 150)}
              onKeyDown={e => {
                if (e.key === 'Escape') { setDropdownOpen(false); return }
                if (e.key === 'Enter' && showAddNew && filtered.length === 0) {
                  e.preventDefault()
                  addChip(inputText)
                }
              }}
              autoComplete="off"
              spellCheck={false}
            />

            {showDropdown && (
              <div
                className="absolute left-0 right-0 z-50 rounded-lg border border-stone-border bg-background shadow-[0_4px_20px_rgba(0,0,0,0.12)] dark:shadow-[0_4px_20px_rgba(0,0,0,0.4)] overflow-y-auto py-1"
                style={{ top: 'calc(100% + 4px)', maxHeight: 220 }}
              >
                {filtered.map(url => (
                  <button
                    key={url}
                    type="button"
                    onMouseDown={e => { e.preventDefault(); addChip(url) }}
                    className="block w-full text-left px-3 py-[7px] text-sm text-foreground bg-transparent border-none cursor-pointer font-[inherit] hover:bg-stone-panel transition-colors duration-100"
                  >
                    {url}
                  </button>
                ))}
                {showAddNew && (
                  <button
                    type="button"
                    onMouseDown={e => { e.preventDefault(); addChip(inputText) }}
                    className={`flex items-center gap-1.5 w-full text-left px-3 py-[7px] text-sm text-ink bg-transparent border-none cursor-pointer font-[inherit] hover:bg-stone-panel transition-colors duration-100${filtered.length > 0 ? ' border-t border-stone-border mt-1' : ''}`}
                  >
                    <Plus size={13} />
                    Add "{inputText.trim()}"
                  </button>
                )}
              </div>
            )}
          </div>

          {/* Chips */}
          {chips.length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {chips.map(url => (
                <span
                  key={url}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 4,
                    padding: '3px 6px 3px 10px',
                    borderRadius: 5,
                    fontSize: '0.8125rem',
                    lineHeight: 1.4,
                    background: 'color-mix(in oklch, currentColor 6%, transparent)',
                    border: '1px solid color-mix(in oklch, currentColor 14%, transparent)',
                    maxWidth: 260,
                  }}
                >
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: 0 }}>
                    {url}
                  </span>
                  <button
                    type="button"
                    onClick={() => removeChip(url)}
                    aria-label={`Remove ${url}`}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      padding: 2,
                      background: 'none',
                      border: 'none',
                      cursor: 'pointer',
                      opacity: 0.5,
                      flexShrink: 0,
                      borderRadius: 3,
                    }}
                  >
                    <X size={11} />
                  </button>
                </span>
              ))}
            </div>
          )}
        </div>

        {error && <p className="form-error">{error}</p>}

        <DialogFooter>
          <button className="btn-ghost" onClick={handleClose}>Cancel</button>
          <button className="btn-primary" onClick={handleStart}>
            {chips.length > 0 ? `Start Scan (${chips.length})` : 'Start Scan'}
          </button>
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
  const navigate = useNavigate()
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
      const triggeredAt = new Date().toISOString()
      // Snapshot current results before this scan overwrites latest-per-(url,server).
      const baseline = await fetchResults()
      sessionStorage.setItem(`scan-baseline-${triggeredAt}`, JSON.stringify(baseline))
      await triggerScan(urls)
      setScanning(true)
      startPolling()
      navigate({ to: '/scan-results', search: { urls, triggeredAt } })
    } catch (err) {
      console.error('Targeted scan trigger failed:', err)
    }
  }, [scanning, startPolling, navigate])

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
              <IconBar value={null} onValueChange={() => {}}>
                <IconBarItem
                  icon={Zap}
                  label="Scan All"
                  disabled={scanning}
                  onClick={handleScanClick}
                />
                <IconBarItem
                  icon={Crosshair}
                  label="Scan Selected"
                  disabled={scanning}
                  onClick={() => setScanSelectedOpen(true)}
                />
              </IconBar>
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
