import { api } from './client'
import type { DueDatePreset } from './types'

// Read open to any authenticated role — the "Time to Block" dropdown on the
// watchlist page needs it for every user, not just admins. Mutations are
// admin-or-dept-admin (see internal/server/router.go).
export async function fetchDueDatePresets(): Promise<DueDatePreset[]> {
  const data = await api.get<DueDatePreset[]>('/due-date-presets')
  return Array.isArray(data) ? data : []
}

export async function createDueDatePreset(label: string, minutes: number): Promise<DueDatePreset> {
  return api.post<DueDatePreset>('/due-date-presets', { label, minutes })
}

export async function deleteDueDatePreset(id: number): Promise<void> {
  await api.delete<void>(`/due-date-presets/${id}`)
}
