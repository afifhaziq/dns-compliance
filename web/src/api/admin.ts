import { api } from './client'
import type { CompliantIP, Department, URLEntry, User } from './types'

export async function fetchDepartments(): Promise<Department[]> {
  const data = await api.get<Department[]>('/admin/departments')
  return Array.isArray(data) ? data : []
}

export async function createDepartment(name: string): Promise<Department> {
  return api.post<Department>('/admin/departments', { name })
}

export async function fetchUsers(): Promise<User[]> {
  const data = await api.get<User[]>('/admin/users')
  return Array.isArray(data) ? data : []
}

export async function createUser(input: {
  username: string
  password: string
  is_admin: boolean
  department_id?: number
}): Promise<User> {
  return api.post<User>('/admin/users', input)
}

export async function deleteUser(id: number): Promise<void> {
  await api.delete<void>(`/admin/users/${id}`)
}

export async function fetchUnassignedUrls(): Promise<URLEntry[]> {
  const data = await api.get<URLEntry[]>('/admin/urls/unassigned')
  return Array.isArray(data) ? data : []
}

// purgeUrl hard-deletes a URL row (and its scan history) — distinct from
// removeUrl in urls.ts, which only unlinks a department's watchlist entry.
export async function purgeUrl(id: number): Promise<void> {
  await api.delete<void>(`/admin/urls/${id}`)
}

export async function fetchCompliantIPs(): Promise<CompliantIP[]> {
  const data = await api.get<CompliantIP[]>('/admin/compliant-ips')
  return Array.isArray(data) ? data : []
}

export async function createCompliantIP(address: string, note: string): Promise<CompliantIP> {
  return api.post<CompliantIP>('/admin/compliant-ips', { address, note })
}

export async function deleteCompliantIP(id: number): Promise<void> {
  await api.delete<void>(`/admin/compliant-ips/${id}`)
}
