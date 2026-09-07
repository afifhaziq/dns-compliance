const BASE = '/api'

type RequestOptions = {
  skipAuthRedirect?: boolean
}

async function request<T>(path: string, init?: RequestInit, opts?: RequestOptions): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'fetch', ...init?.headers },
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

export type BlobDownload = { blob: Blob; filename: string | null }

// Content-Disposition looks like `attachment; filename="blocking-list-export-2026-09-04.xlsx"`
// (see internal/server's export handlers) — quotes are optional per RFC 6266,
// so the regex tolerates both `filename=foo.xlsx` and `filename="foo.xlsx"`.
function filenameFromContentDisposition(header: string | null): string | null {
  if (!header) return null
  const match = /filename="?([^";]+)"?/i.exec(header)
  return match?.[1]?.trim() ?? null
}

async function requestBlob(path: string, opts?: RequestOptions): Promise<BlobDownload> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'X-Requested-With': 'fetch' },
    credentials: 'same-origin',
  })
  if (res.status === 401 && !opts?.skipAuthRedirect && window.location.pathname !== '/login') {
    window.location.assign('/login')
    throw new Error('401 Unauthorized')
  }
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  const blob = await res.blob()
  const filename = filenameFromContentDisposition(res.headers.get('Content-Disposition'))
  return { blob, filename }
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>(path, undefined, opts),
  post: <T>(path: string, body: unknown, opts?: RequestOptions) =>
    request<T>(path, { method: 'POST', body: JSON.stringify(body) }, opts),
  patch: <T>(path: string, body: unknown, opts?: RequestOptions) =>
    request<T>(path, { method: 'PATCH', body: JSON.stringify(body) }, opts),
  put: <T>(path: string, body: unknown, opts?: RequestOptions) =>
    request<T>(path, { method: 'PUT', body: JSON.stringify(body) }, opts),
  delete: <T>(path: string, opts?: RequestOptions) => request<T>(path, { method: 'DELETE' }, opts),
  getBlob: (path: string, opts?: RequestOptions) => requestBlob(path, opts),
}
