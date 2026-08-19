import { api } from './client'
import type { Requestor } from './types'

// Read open to any authenticated role — the Requestor dropdown on the Docs
// page needs it for every user, not just admins. Mutations are
// admin-or-dept-admin (see internal/server/router.go).
export async function fetchRequestors(): Promise<Requestor[]> {
  const data = await api.get<Requestor[]>('/requestors')
  return Array.isArray(data) ? data : []
}

export async function createRequestor(name: string): Promise<Requestor> {
  return api.post<Requestor>('/admin/requestors', { name })
}

export async function updateRequestor(id: number, name: string): Promise<Requestor> {
  return api.patch<Requestor>(`/admin/requestors/${id}`, { name })
}

export async function deleteRequestor(id: number): Promise<void> {
  await api.delete<void>(`/admin/requestors/${id}`)
}
