import { api } from './client'
import type { ISPStats, ISPTrendStat } from './types'

export async function fetchISPStats(isp: string): Promise<ISPStats> {
  return api.get<ISPStats>(`/isps/${encodeURIComponent(isp)}`)
}

export async function fetchISPTrend(isp: string, sinceDays = 30): Promise<ISPTrendStat[]> {
  const since = new Date(Date.now() - sinceDays * 24 * 60 * 60 * 1000).toISOString()
  return api.get<ISPTrendStat[]>(`/isps/${encodeURIComponent(isp)}/trend?since=${encodeURIComponent(since)}`)
}
