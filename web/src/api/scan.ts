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

export async function triggerScreenshot(url: string, dnsServerIds: number[]): Promise<void> {
  await api.post<void>('/screenshot', { url, dns_server_ids: dnsServerIds })
}
