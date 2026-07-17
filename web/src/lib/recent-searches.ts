const MAX_RECENT = 10

function key(userId: number) {
  return `recent-searches-${userId}`
}

export function getRecentSearches(userId: number): string[] {
  try {
    const raw = localStorage.getItem(key(userId))
    return raw ? JSON.parse(raw) : []
  } catch {
    return []
  }
}

export function addRecentSearch(userId: number, domain: string): string[] {
  const existing = getRecentSearches(userId).filter(d => d !== domain)
  const updated = [domain, ...existing].slice(0, MAX_RECENT)
  localStorage.setItem(key(userId), JSON.stringify(updated))
  return updated
}

export function clearRecentSearches(userId: number): void {
  localStorage.removeItem(key(userId))
}
