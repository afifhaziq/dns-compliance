const BASE = '/api'

type RequestOptions = {
  skipAuthRedirect?: boolean
}

async function request<T>(path: string, init?: RequestInit, opts?: RequestOptions): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    credentials: 'same-origin',
    ...init,
  })
  if (res.status === 401 && !opts?.skipAuthRedirect && window.location.pathname !== '/login') {
    window.location.assign('/login')
    throw new Error('401 Unauthorized')
  }
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>(path, undefined, opts),
  post: <T>(path: string, body: unknown, opts?: RequestOptions) =>
    request<T>(path, { method: 'POST', body: JSON.stringify(body) }, opts),
  delete: <T>(path: string, opts?: RequestOptions) => request<T>(path, { method: 'DELETE' }, opts),
}
