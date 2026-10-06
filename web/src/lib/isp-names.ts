import type { ScanResult } from '@/api/types'

export function getISPNames(results: ScanResult[]): string[] {
  return Array.from(new Set(results.map(r => r.dns_server.isp))).sort()
}
