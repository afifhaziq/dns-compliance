import type { DailyComplianceStat, GroupedResult, ISPTrendStat, ResurfacedDomain, ScanResult } from './types'

export async function fetchResults(): Promise<ScanResult[]> {
  const res = await fetch('/api/results')
  if (!res.ok) throw new Error(`Failed to load results: ${res.status}`)
  return res.json()
}

export async function fetchNationalTrend(sinceDays = 30): Promise<ISPTrendStat[]> {
  const since = new Date(Date.now() - sinceDays * 24 * 60 * 60 * 1000).toISOString()
  const res = await fetch(`/api/trend?since=${encodeURIComponent(since)}`)
  if (!res.ok) throw new Error(`Failed to load trend: ${res.status}`)
  return res.json()
}

export async function fetchResurfacedDomains(): Promise<ResurfacedDomain[]> {
  const res = await fetch('/api/resurfaced')
  if (!res.ok) throw new Error(`Failed to load resurfaced domains: ${res.status}`)
  return res.json()
}

export async function fetchResultsByUrl(url: string, sinceDays = 7): Promise<ScanResult[]> {
  const since = new Date(Date.now() - sinceDays * 24 * 60 * 60 * 1000).toISOString()
  const res = await fetch(`/api/results/${encodeURIComponent(url)}?since=${encodeURIComponent(since)}`)
  if (!res.ok) throw new Error(`Failed to load results: ${res.status}`)
  return res.json()
}

export async function fetchHeatmapByUrlAndYear(url: string, year: number): Promise<DailyComplianceStat[]> {
  const since = new Date(year, 0, 1).toISOString()
  const until = new Date(year, 11, 31, 23, 59, 59, 999).toISOString()
  const res = await fetch(`/api/heatmap/${encodeURIComponent(url)}?since=${encodeURIComponent(since)}&until=${encodeURIComponent(until)}`)
  if (!res.ok) throw new Error(`Failed to load heatmap: ${res.status}`)
  return res.json()
}

export function groupResults(results: ScanResult[]): GroupedResult[] {
  const map = new Map<string, ScanResult[]>()
  for (const r of results) {
    const existing = map.get(r.url) ?? []
    map.set(r.url, [...existing, r])
  }

  return Array.from(map.entries())
    .map(([url, items]) => {
      let hostname = url
      try { hostname = new URL(url).hostname } catch { /* bare hostname */ }

      const latestScannedAt = items.reduce(
        (max, r) => (r.scanned_at > max ? r.scanned_at : max),
        items[0]?.scanned_at ?? '',
      )

      return {
        url,
        hostname,
        results: items,
        violationCount: items.filter(r => !r.compliant).length,
        totalCount: items.length,
        latestScannedAt,
      }
    })
    .sort((a, b) => b.violationCount - a.violationCount) // violations first
}

export function lastScanTime(groups: GroupedResult[]): string | null {
  if (groups.length === 0) return null
  const ts = groups.reduce(
    (max, g) => (g.latestScannedAt > max ? g.latestScannedAt : max),
    groups[0].latestScannedAt,
  )
  if (!ts) return null
  return new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(ts))
}
