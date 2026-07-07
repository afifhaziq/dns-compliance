import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { FaviconSearch } from '@/components/unlumen-ui/favicon-search'

export const Route = createFileRoute('/domain/')({ component: DomainPickerPage })

function DomainPickerPage() {
  const navigate = useNavigate()

  return (
    <div className="mx-60 mb-10">
      <div className="page-header px-0">
        <h1 className="page-title mb-2">Domain Lookup</h1>
        <p className="page-subtitle">Search for a domain to view its compliance history.</p>
      </div>

      <div className="dash-section">
        <FaviconSearch
          placeholder="Enter a domain to look up…"
          className="w-96"
          onSearch={(_value, domain) => navigate({ to: '/domain/$url', params: { url: domain }, search: { tab: 'overview' } })}
        />
      </div>
    </div>
  )
}
