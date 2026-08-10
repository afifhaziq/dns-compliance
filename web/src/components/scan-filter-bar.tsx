import type { ReactNode } from 'react'
import { Input } from '@/components/ui/input'
import { Filters, type Filter, type FilterFieldConfig } from '@/components/reui/filters'

// Multi-select operators only. The underlying Filters primitive
// (@reui/c-filters-2, web/src/components/reui/filters.tsx) also offers
// "includes all of"/"excludes all of", but those only make sense for a field
// where one row can hold several values at once (e.g. tags) — a single scan
// result has exactly one status and one DNS server, so there's nothing for
// those two operators to mean here. "is empty"/"is not empty" are dropped
// for the same reason: both fields are always present on every row.
const MULTI_OPERATORS = [
  { value: 'is_any_of', label: 'is any of' },
  { value: 'is_not_any_of', label: 'is not any of' },
]

const STATUS_FIELD: FilterFieldConfig<string> = {
  key: 'status',
  label: 'Status',
  type: 'multiselect',
  operators: MULTI_OPERATORS,
  options: [
    { value: 'violations', label: 'Violations' },
    { value: 'compliant', label: 'Compliant' },
  ],
}

export type DnsServerFilterOption = { value: string; label: string }

// Shared Status + DNS-Server filter fields for the results/domain-history
// tables. Each caller applies the resulting filter values to its own query
// (client-side array filtering vs. a server-side fetch param) — this only
// builds the field config, it doesn't know or care how filtering happens.
export function buildScanFilterFields(dnsServerOptions: DnsServerFilterOption[]): FilterFieldConfig<string>[] {
  const fields: FilterFieldConfig<string>[] = [STATUS_FIELD]
  if (dnsServerOptions.length > 1) {
    fields.push({
      key: 'dns_server',
      label: 'DNS Server',
      type: 'multiselect',
      operators: MULTI_OPERATORS,
      options: dnsServerOptions,
    })
  }
  return fields
}

export function ScanFilterBar({
  search,
  onSearchChange,
  filters,
  onFiltersChange,
  dnsServerOptions,
  children,
}: {
  search: string
  onSearchChange: (value: string) => void
  filters: Filter<string>[]
  onFiltersChange: (filters: Filter<string>[]) => void
  dnsServerOptions: DnsServerFilterOption[]
  children?: ReactNode
}) {
  return (
    <div className="filter-bar flex flex-row items-center justify-start gap-4 w-full">
      <Input
        type="search"
        placeholder="Search domain..."
        value={search}
        onChange={e => onSearchChange(e.target.value)}
        className="max-w-64"
        aria-label="Search domain"
      />
      <Filters filters={filters} fields={buildScanFilterFields(dnsServerOptions)} onChange={onFiltersChange} />
      {children}
    </div>
  )
}
