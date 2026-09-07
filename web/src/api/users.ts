import { api } from './client'
import type { User } from './types'

// Open read of the user list (GET /api/users/open), department-scoped
// server-side for a non-admin caller (their own department only; admin
// sees everyone) — distinct from admin.ts's user-management fetchers,
// which hit the requireAnyAdmin-gated /api/admin/users. This one backs the
// OIC picker any authenticated user sees when recording a case's letters
// (docs.tsx).
export async function fetchUsersOpen(): Promise<User[]> {
  const data = await api.get<User[]>('/users/open')
  return Array.isArray(data) ? data : []
}
