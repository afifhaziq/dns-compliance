import { useEffect, useRef, useState } from 'react'
import type { SortingState, VisibilityState } from '@tanstack/react-table'
import { fetchGridPreference, saveGridPreference } from '@/api/grid-preferences'

type GridPreferenceState = {
  sorting?: SortingState
  columnVisibility?: VisibilityState
  pageSize?: number
}

type GridPreferenceSetters = {
  setSorting?: (s: SortingState) => void
  setColumnVisibility?: (v: VisibilityState) => void
  setPageSize?: (n: number) => void
}

const SAVE_DEBOUNCE_MS = 600

// Loads a saved data-grid layout (column visibility/sort/page size) once on
// mount and applies whichever setters were passed, then persists further
// changes back (debounced) once loading has settled — so it works whether a
// caller wants all three axes (urls.tsx) or just one (results.index.tsx's
// sorting, which has no column-visibility or page-size control to persist).
// Returns `ready`, which flips true once the initial load has settled
// (found a preference, found none, or failed) — callers should hold the
// grid in its existing loading/skeleton state until then, otherwise it
// renders once with the plain defaults and immediately snaps to the saved
// layout a moment later.
export function useGridPreference(
  key: string,
  state: GridPreferenceState,
  setters: GridPreferenceSetters
): { ready: boolean } {
  const [ready, setReady] = useState(false)
  const settersRef = useRef(setters)
  useEffect(() => {
    settersRef.current = setters
  })

  useEffect(() => {
    let cancelled = false
    fetchGridPreference(key)
      .then(pref => {
        if (cancelled) return
        if (pref.sort_field && settersRef.current.setSorting) {
          settersRef.current.setSorting([{ id: pref.sort_field, desc: !!pref.sort_desc }])
        }
        if (pref.column_visibility && settersRef.current.setColumnVisibility) {
          settersRef.current.setColumnVisibility(pref.column_visibility)
        }
        if (pref.page_size && settersRef.current.setPageSize) {
          settersRef.current.setPageSize(pref.page_size)
        }
      })
      .catch(() => {})
      .finally(() => { if (!cancelled) setReady(true) })
    return () => { cancelled = true }
  }, [key])

  useEffect(() => {
    if (!ready) return
    const timeout = setTimeout(() => {
      saveGridPreference(key, {
        column_visibility: state.columnVisibility,
        sort_field: state.sorting?.[0]?.id,
        sort_desc: state.sorting?.[0]?.desc,
        page_size: state.pageSize,
      }).catch(() => {})
    }, SAVE_DEBOUNCE_MS)
    return () => clearTimeout(timeout)
  }, [key, ready, state.sorting, state.columnVisibility, state.pageSize])

  return { ready }
}
