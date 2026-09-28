import { api } from './client'

export type BlockingStatRow = { year: number; agency: string; offence: string; count: number }

export async function fetchBlockingStats(): Promise<BlockingStatRow[]> {
  const data = await api.get<BlockingStatRow[]>('/blocking-stats')
  return Array.isArray(data) ? data : []
}

// Overview URL params for the register's filters, one param per field: `offence=Lucah,Palsu`, `year=2022-2024`;
// the `_not` variants are the "is none of" / "is not" operators.
export const REGISTER_FILTER_KEYS = ['agency', 'offence', 'year', 'agency_not', 'offence_not', 'year_not'] as const
export type RegisterFilterKey = typeof REGISTER_FILTER_KEYS[number]
