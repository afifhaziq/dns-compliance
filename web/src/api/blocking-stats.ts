import { api } from './client'

export type BlockingStatRow = { year: number; agency: string; offence: string; count: number }

export async function fetchBlockingStats(): Promise<BlockingStatRow[]> {
  const data = await api.get<BlockingStatRow[]>('/blocking-stats')
  return Array.isArray(data) ? data : []
}
