import { useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { FaviconSearch } from '@/components/unlumen-ui/favicon-search'
import { useAuth } from '@/routes/__root'
import { getRecentSearches, addRecentSearch, clearRecentSearches } from '@/lib/recent-searches'

export const Route = createFileRoute('/domain/')({ component: DomainPickerPage })

function DomainPickerPage() {
  const navigate = useNavigate()
  const { me } = useAuth()
  const [recent, setRecent] = useState<string[]>(() => (me ? getRecentSearches(me.id) : []))

  const goToDomain = (domain: string) =>
    navigate({ to: '/domain/$url', params: { url: domain }, search: { tab: 'overview' } })

  return (
    <div className="mx-60 mt-10 mb-10">
      <div className="page-header px-0">
        <h1 className="page-title mb-2">Domain Lookup</h1>
        <p className="page-subtitle">Search for a domain to view its compliance history.</p>
      </div>

      <div className="dash-section">
        <FaviconSearch
          placeholder="Enter a domain to look up…"
          className="w-96"
          onSearch={(_value, domain) => {
            if (me) setRecent(addRecentSearch(me.id, domain))
            goToDomain(domain)
          }}
        />
      </div>

      {recent.length > 0 && (
        <div className="dash-section mt-6 w-96">
          <div className="flex items-center justify-between mb-2">
            <span className="text-sm text-stone-muted">Recent searches</span>
            <button
              type="button"
              className="text-sm text-stone-muted hover:text-foreground"
              onClick={() => {
                if (me) clearRecentSearches(me.id)
                setRecent([])
              }}
            >
              Clear
            </button>
          </div>
          <ul className="flex flex-col gap-1">
            {recent.map(domain => (
              <li key={domain}>
                <button
                  type="button"
                  className="w-full text-left px-3 py-1.5 rounded hover:bg-muted text-sm"
                  onClick={() => goToDomain(domain)}
                >
                  {domain}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
