import { api } from './client'
import type { Recipient } from './types'

// Read open to any authenticated role — the Recipient dropdown on the Docs
// page needs it for every user, not just admins. Mutations are
// admin-or-dept-admin (see internal/server/router.go).
export async function fetchRecipients(): Promise<Recipient[]> {
  const data = await api.get<Recipient[]>('/recipients')
  return Array.isArray(data) ? data : []
}

export async function createRecipient(name: string): Promise<Recipient> {
  return api.post<Recipient>('/admin/recipients', { name })
}

export async function updateRecipient(id: number, name: string): Promise<Recipient> {
  return api.patch<Recipient>(`/admin/recipients/${id}`, { name })
}

export async function deleteRecipient(id: number): Promise<void> {
  await api.delete<void>(`/admin/recipients/${id}`)
}
