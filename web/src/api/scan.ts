import type { ScanRun, ScanStatus } from './types'

export async function triggerScan(): Promise<void> {
  const res = await fetch('/api/scan', { method: 'POST' })
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

export async function fetchScanProgress(): Promise<ScanProgressResponse> {
  const res = await fetch('/api/scan/progress')
  if (res.status === 404) throw Object.assign(new Error('no_run'), { code: 'no_run' })
  if (!res.ok) throw new Error(`Failed to load progress: ${res.status}`)
  return res.json()
}
