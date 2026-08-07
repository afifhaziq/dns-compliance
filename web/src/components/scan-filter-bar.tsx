import type { ReactNode } from 'react'
import { Input } from '@/components/ui/input'
import { Filters, type Filter, type FilterFieldConfig } from '@/components/reui/filters'

// Single "is" operator only — these fields are single-value pickers, not
// full is/is-not/empty builders, so there's nothing else to implement.
const IS_ONLY = [{ value: 'is', label: 'is' }]

const STATUS_FIELD: FilterFieldConfig<string> = {
  key: 'status',
  label: 'Status',
  type: 'select',
  operators: IS_ONLY,
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
      type: 'select',
      operators: IS_ONLY,
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
