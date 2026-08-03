import { api } from './client'
import type { ISPStats, ISPTiming, ISPTrendStat } from './types'

export async function fetchISPStats(isp: string): Promise<ISPStats> {
  return api.get<ISPStats>(`/isps/${encodeURIComponent(isp)}`)
}

export async function fetchISPTrend(isp: string, sinceDays = 30): Promise<ISPTrendStat[]> {
  const since = new Date(Date.now() - sinceDays * 24 * 60 * 60 * 1000).toISOString()
  const data = await api.get<ISPTrendStat[]>(`/isps/${encodeURIComponent(isp)}/trend?since=${encodeURIComponent(since)}`)
  return Array.isArray(data) ? data : []
}

export async function fetchISPTiming(isp: string): Promise<ISPTiming> {
  return api.get<ISPTiming>(`/isps/${encodeURIComponent(isp)}/timing`)
}
