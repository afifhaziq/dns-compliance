import { useEffect, useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { fetchResults, groupResults } from '@/api/results'
import { Combobox, ComboboxInput, ComboboxContent, ComboboxEmpty, ComboboxList, ComboboxItem } from '@/components/ui/b-combobox'

export const Route = createFileRoute('/domain/')({ component: DomainPickerPage })

type DomainItem = { value: string; label: string }

function DomainPickerPage() {
  const navigate = useNavigate()
  const [domains, setDomains] = useState<DomainItem[]>([])
  const [loading, setLoading] = useState(true)
  const [selected, setSelected] = useState<DomainItem | null>(null)

  useEffect(() => {
    fetchResults()
      .then(results => setDomains(groupResults(results).map(g => ({ value: g.url, label: g.hostname }))))
      .catch(() => setDomains([]))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="mx-60 mb-10">
      <div className="page-header px-0">
        <h1 className="page-title mb-2">Domain Lookup</h1>
        <p className="page-subtitle">Search for a domain to view its compliance history.</p>
      </div>

      <div className="dash-section">
        <Combobox
          items={domains}
          value={selected}
          onValueChange={item => {
            setSelected(item)
            if (item) navigate({ to: '/domain/$url', params: { url: item.value }, search: { tab: 'overview' } })
          }}
        >
          <ComboboxInput aria-label="Search domains" placeholder="Search domains..." className="w-96" />
          <ComboboxContent>
            <ComboboxEmpty>{loading ? 'Loading domains…' : 'No domains found.'}</ComboboxEmpty>
            <ComboboxList>
              {(item: DomainItem) => <ComboboxItem key={item.value} value={item}>{item.label}</ComboboxItem>}
            </ComboboxList>
          </ComboboxContent>
        </Combobox>
      </div>
    </div>
  )
}
