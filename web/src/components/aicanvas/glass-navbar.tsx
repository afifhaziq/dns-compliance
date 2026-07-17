import { motion } from 'framer-motion'
import { Link, useLocation } from '@tanstack/react-router'
import { Highlight, HighlightItem } from '@/components/unlumen-ui/primitives/effects/highlight'
import type { ReactNode } from 'react'
import { useAuth } from '@/routes/__root'

const BASE_NAV_ITEMS = [
  { to: '/' as const, label: 'Overview' },
  { to: '/results' as const, label: 'Results' },
  { to: '/domain' as const, label: 'Domain' },
  { to: '/urls' as const, label: 'Watchlist' },
  { to: '/dns-servers' as const, label: 'DNS Servers' },
]

const ADMIN_NAV_ITEM = { to: '/admin' as const, label: 'Admin' }

interface GlassNavbarProps {
  actions?: ReactNode
}

export function LogoutButton() {
  const { logout } = useAuth()
  return (
    <button
      className="btn-ghost"
      style={{ color: 'var(--ink)', borderColor: 'color-mix(in srgb, var(--ink) 40%, transparent)' }}
      onClick={() => { logout() }}
    >
      Sign Out
    </button>
  )
}

function BrandMark() {
  return (
    <svg viewBox="0 0 24 24" width="22" height="22" fill="none" aria-hidden="true" className="brand-mark">
      <line x1="7.5" y1="16" x2="7.5" y2="11" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <line className="brand-mark-watch" x1="12" y1="16" x2="12" y2="7.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <line x1="16.5" y1="16" x2="16.5" y2="11" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  )
}

export function GlassNavbar({ actions }: GlassNavbarProps) {
  const location = useLocation()
  const { me } = useAuth()
  const isActive = (path: string) => location.pathname === path
  const navItems = (me?.is_admin || me?.is_dept_admin) ? [...BASE_NAV_ITEMS, ADMIN_NAV_ITEM] : BASE_NAV_ITEMS

  return (
    <div className="sticky top-0 z-50 px-4 pt-3 pb-1" >
      <motion.nav
        initial={{ y: -48, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        transition={{ type: 'spring', stiffness: 220, damping: 26 }}
        className="relative isolate mx-auto flex max-w-5xl items-center gap-1 rounded-full px-2 py-1.5
                   bg-white/25 border border-black/[0.10] shadow-[0_4px_28px_rgba(0,0,0,0.10),inset_0_1px_0_rgba(255,255,255,1)]
                   dark:bg-white/[0.02] dark:border-white/[0.01] dark:shadow-[0_8px_32px_rgba(0,0,0,0.45),inset_0_1px_0_rgba(255,255,255,0.10)]"
        style={{
          backdropFilter: 'blur(28px) saturate(2)',
          WebkitBackdropFilter: 'blur(28px) saturate(2)',
        }}
        aria-label="Primary"
      >
        {/* Brand */}
        <Link to="/" className="nav-brand">
          <BrandMark />
          Citadel
        </Link>

        {/* Nav links */}
        <Highlight
          mode="parent"
          hover={true}
          controlledItems={true}
          className="nav-hl-pill"
          containerClassName="flex flex-1 items-center gap-0.5"
          transition={{ type: 'spring', stiffness: 500, damping: 50 }}
        >
          {navItems.map(({ to, label }) => (
            <HighlightItem key={to} value={to} asChild>
              <Link
                to={to}
                className="nav-link"
                data-active={isActive(to) ? 'true' : 'false'}
              >
                {label}
              </Link>
            </HighlightItem>
          ))}
        </Highlight>

        {/* Actions */}
        {actions && (
          <div className="flex items-center gap-2 pr-1">
            {actions}
          </div>
        )}
      </motion.nav>
    </div>
  )
}
