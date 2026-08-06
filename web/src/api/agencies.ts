import { api } from './client'
import type { Agency } from './types'

// Read open to any authenticated role — the Agency dropdown on the
// watchlist page needs it for every user, not just admins. Mutations are
// admin-or-dept-admin (see internal/server/router.go).
export async function fetchAgencies(): Promise<Agency[]> {
  const data = await api.get<Agency[]>('/agencies')
  return Array.isArray(data) ? data : []
}

export async function createAgency(name: string): Promise<Agency> {
  return api.post<Agency>('/admin/agencies', { name })
}

export async function deleteAgency(id: number): Promise<void> {
  await api.delete<void>(`/admin/agencies/${id}`)
}
