// Reporting periods for the ISP page's Unblocked table (isps.$isp.tsx).
export type Period = 'week' | 'last-week' | 'custom'

const DAY = 24 * 60 * 60 * 1000

function startOfWeek(d: Date): Date {
  const s = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  s.setDate(s.getDate() - ((s.getDay() + 6) % 7)) // Monday
  return s
}

// The [since, until] window for a period. Custom dates are inclusive whole
// days in the viewer's local time; an incomplete custom range falls back to
// this week.
export function periodRange(period: Period, from?: string, to?: string): { since: Date; until: Date } {
  const now = new Date()
  const monday = startOfWeek(now)
  if (period === 'last-week') return { since: new Date(monday.getTime() - 7 * DAY), until: new Date(monday.getTime() - 1) }
  if (period === 'custom' && from && to) {
    return { since: new Date(`${from}T00:00:00`), until: new Date(`${to}T23:59:59.999`) }
  }
  return { since: monday, until: now }
}

// The same-length window immediately before [since, until], for the
// "vs previous period" delta.
export function previousRange({ since, until }: { since: Date; until: Date }): { since: Date; until: Date } {
  const len = until.getTime() - since.getTime()
  return { since: new Date(since.getTime() - len - 1), until: new Date(since.getTime() - 1) }
}
