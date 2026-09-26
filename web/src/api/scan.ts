import { api } from './client'
import type { ScanRun, ScanStatus } from './types'

export async function triggerScan(urls?: string[]): Promise<void> {
  const body = urls && urls.length > 0 ? JSON.stringify({ urls }) : undefined
  const res = await fetch('/api/scan', {
    method: 'POST',
    credentials: 'same-origin',
    headers: body ? { 'Content-Type': 'application/json', 'X-Requested-With': 'fetch' } : { 'X-Requested-With': 'fetch' },
    body,
  })
  if (!res.ok && res.status !== 409) throw new Error(`Failed to start scan: ${res.status}`)
}

export async function cancelScan(): Promise<void> {
  const res = await fetch('/api/scan/cancel', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'X-Requested-With': 'fetch' },
  })
  if (!res.ok && res.status !== 409) throw new Error(`Failed to cancel scan: ${res.status}`)
}

export async function fetchScanStatus(): Promise<ScanStatus> {
  const res = await fetch('/api/scan/status')
  if (!res.ok) throw new Error(`Failed to get scan status: ${res.status}`)
  return res.json()
}

export function isScanning(status: ScanStatus): boolean {
  return 'id' in status && status.status === 'running'
}

export type ProgressEntry = {
  dns_server_id: number
  name: string
  completed: number
}

export type ScanProgressResponse = {
  scan_run: ScanRun
  total_urls: number
  per_dns: ProgressEntry[]
}

// fetchScanProgress backs the "Scan All" confirmation dialog's domain count.
// total_urls is computed fresh from ListWatchedURLs on every call, so it's
// accurate for the *current* watchlist state even though it's served
// alongside the last scan run's per-DNS tally. Returns null (not an error)
// when no scan has ever run — GET /api/scan/progress 404s in that case since
// there's no ScanRun row to attach the payload to.
export async function fetchScanProgress(): Promise<ScanProgressResponse | null> {
  const res = await fetch('/api/scan/progress', { credentials: 'same-origin' })
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`Failed to get scan progress: ${res.status}`)
  return res.json()
}

export async function triggerScreenshot(url: string, dnsServerIds: number[]): Promise<void> {
  await api.post<void>('/screenshot', { url, dns_server_ids: dnsServerIds })
}
