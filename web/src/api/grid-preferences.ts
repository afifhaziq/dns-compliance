import { api } from './client'
import type { GridPreference } from './types'

export async function fetchGridPreference(key: string): Promise<GridPreference> {
  return api.get<GridPreference>(`/grid-preferences/${key}`)
}

export async function saveGridPreference(key: string, pref: GridPreference): Promise<GridPreference> {
  return api.put<GridPreference>(`/grid-preferences/${key}`, pref)
}
