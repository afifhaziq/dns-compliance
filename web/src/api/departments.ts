import { api } from './client'
import type { Department } from './types'

// Open read of the department list (GET /api/departments) — distinct from
// admin.ts's fetchDepartments, which hits the super-admin-only
// /api/admin/departments and backs the admin Departments tab. This one is
// for the Requesting Dept dropdown any authenticated user needs when
// adding/editing a domain's case metadata.
export async function fetchDepartmentsOpen(): Promise<Department[]> {
  const data = await api.get<Department[]>('/departments')
  return Array.isArray(data) ? data : []
}
