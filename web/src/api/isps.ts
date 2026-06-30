import { api } from './client'
import type { ISPStats } from './types'

export async function fetchISPStats(isp: string): Promise<ISPStats> {
  return api.get<ISPStats>(`/isps/${encodeURIComponent(isp)}`)
}
