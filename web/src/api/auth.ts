import { api } from './client'
import type { User } from './types'

export async function login(username: string, password: string): Promise<User> {
  return api.post<User>('/auth/login', { username, password })
}

export async function logout(): Promise<void> {
  await api.post<void>('/auth/logout', {}, { skipAuthRedirect: true }).catch(() => {
    // best-effort — the session cookie is cleared server-side regardless
  })
}

// fetchMe resolves to null on 401 instead of throwing, so the root layout's
// own "am I logged in" check never triggers client.ts's redirect-to-login.
export async function fetchMe(): Promise<User | null> {
  try {
    return await api.get<User>('/auth/me', { skipAuthRedirect: true })
  } catch {
    return null
  }
}
