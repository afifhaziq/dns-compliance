import { api } from './client'
import type { ISPLogo } from './types'

export async function fetchISPLogos(): Promise<ISPLogo[]> {
  const data = await api.get<ISPLogo[]>('/isp-logos')
  return Array.isArray(data) ? data : []
}

export async function upsertISPLogo(isp: string, logoUrl: string): Promise<ISPLogo> {
  return api.post<ISPLogo>('/admin/isp-logos', { isp, logo_url: logoUrl })
}

export async function deleteISPLogo(isp: string): Promise<void> {
  return api.delete<void>(`/admin/isp-logos/${encodeURIComponent(isp)}`)
}
