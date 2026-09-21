import { api } from './client'
import type { Instrument, Citation, LegalCategory, LegalElement, LegalSubElement, URLOffence, LegalCitationParsed } from './types'

export async function fetchInstruments(): Promise<Instrument[]> {
  const data = await api.get<Instrument[]>('/legal/instruments')
  return Array.isArray(data) ? data : []
}

export function createInstrument(input: {
  type: string
  jurisdiction: string
  number: string
  year?: number
  short_title: string
}): Promise<Instrument> {
  return api.post<Instrument>('/legal/instruments', input)
}

export function updateInstrument(id: number, input: {
  type: string
  jurisdiction: string
  number: string
  year?: number
  short_title: string
}): Promise<Instrument> {
  return api.patch<Instrument>(`/legal/instruments/${id}`, input)
}

export function deleteInstrument(id: number): Promise<void> {
  return api.delete<void>(`/legal/instruments/${id}`)
}

export async function fetchCitations(instrumentId: number): Promise<Citation[]> {
  const data = await api.get<Citation[]>(`/legal/instruments/${instrumentId}/citations`)
  return Array.isArray(data) ? data : []
}

// Flat/unscoped counterparts of fetchCitations/fetchCategories/fetchElements/
// fetchSubElements below — used by legal-citations.tsx's up-front tree load
// to fetch each level in one request instead of one per parent node (see
// loadTree's doc comment there).
export async function fetchAllCitations(): Promise<Citation[]> {
  const data = await api.get<Citation[]>('/legal/citations')
  return Array.isArray(data) ? data : []
}

export type CitationParsePreview = { parsed: LegalCitationParsed; parse_confidence: 'OK' | 'NEEDS_REVIEW' }

// Runs internal/legalcite.Parse against free text without persisting
// anything — backs the live preview shown while typing a citation.
export function parseCitationPreview(rawText: string): Promise<CitationParsePreview> {
  return api.post<CitationParsePreview>('/legal/citations/parse-preview', { raw_text: rawText })
}

export function createCitation(input: {
  instrument_id: number
  raw_text: string
  parsed: LegalCitationParsed
  parse_confidence: string
}): Promise<Citation> {
  return api.post<Citation>('/legal/citations', input)
}

export function updateCitation(id: number, input: {
  instrument_id: number
  raw_text: string
  parsed: LegalCitationParsed
  parse_confidence: string
}): Promise<Citation> {
  return api.patch<Citation>(`/legal/citations/${id}`, input)
}

export function deleteCitation(id: number): Promise<void> {
  return api.delete<void>(`/legal/citations/${id}`)
}

export async function fetchCategories(citationId: number): Promise<LegalCategory[]> {
  const data = await api.get<LegalCategory[]>(`/legal/citations/${citationId}/categories`)
  return Array.isArray(data) ? data : []
}

export async function fetchAllCategories(): Promise<LegalCategory[]> {
  const data = await api.get<LegalCategory[]>('/legal/categories')
  return Array.isArray(data) ? data : []
}

export function createCategory(citationId: number, name: string): Promise<LegalCategory> {
  return api.post<LegalCategory>('/legal/categories', { citation_id: citationId, name })
}

export function updateCategory(id: number, name: string): Promise<LegalCategory> {
  return api.patch<LegalCategory>(`/legal/categories/${id}`, { name })
}

export function deleteCategory(id: number): Promise<void> {
  return api.delete<void>(`/legal/categories/${id}`)
}

export async function fetchElements(categoryId: number): Promise<LegalElement[]> {
  const data = await api.get<LegalElement[]>(`/legal/categories/${categoryId}/elements`)
  return Array.isArray(data) ? data : []
}

export async function fetchAllElements(): Promise<LegalElement[]> {
  const data = await api.get<LegalElement[]>('/legal/elements')
  return Array.isArray(data) ? data : []
}

export function createElement(categoryId: number, name: string): Promise<LegalElement> {
  return api.post<LegalElement>('/legal/elements', { category_id: categoryId, name })
}

export function updateElement(id: number, name: string): Promise<LegalElement> {
  return api.patch<LegalElement>(`/legal/elements/${id}`, { name })
}

export function deleteElement(id: number): Promise<void> {
  return api.delete<void>(`/legal/elements/${id}`)
}

export async function fetchSubElements(elementId: number): Promise<LegalSubElement[]> {
  const data = await api.get<LegalSubElement[]>(`/legal/elements/${elementId}/subelements`)
  return Array.isArray(data) ? data : []
}

export async function fetchAllSubElements(): Promise<LegalSubElement[]> {
  const data = await api.get<LegalSubElement[]>('/legal/subelements')
  return Array.isArray(data) ? data : []
}

export function createSubElement(elementId: number, name: string): Promise<LegalSubElement> {
  return api.post<LegalSubElement>('/legal/subelements', { element_id: elementId, name })
}

export function updateSubElement(id: number, name: string): Promise<LegalSubElement> {
  return api.patch<LegalSubElement>(`/legal/subelements/${id}`, { name })
}

export function deleteSubElement(id: number): Promise<void> {
  return api.delete<void>(`/legal/subelements/${id}`)
}

// URL <-> offence linking (department-ownership-scoped server-side).

export async function fetchOffencesByUrl(url: string): Promise<URLOffence[]> {
  const data = await api.get<URLOffence[]>(`/legal/offences/${encodeURIComponent(url)}`)
  return Array.isArray(data) ? data : []
}

export function attachOffence(url: string, categoryId: number, elementId?: number, subElementId?: number): Promise<URLOffence> {
  return api.post<URLOffence>(`/legal/offences/${encodeURIComponent(url)}`, {
    category_id: categoryId,
    element_id: elementId,
    sub_element_id: subElementId,
  })
}

export function detachOffence(id: number): Promise<void> {
  return api.delete<void>(`/legal/offences/${id}`)
}

// Renders a parsed citation as a short human-readable string, e.g.
// "Bahagian 9 → Seksyen 233(1)(a)" — used for live preview and list
// display. Labels are Malay to match what analysts actually type
// (internal/legalcite is Malay-only — see its package doc).
export function formatParsedCitation(parsed: LegalCitationParsed): string {
  const parts: string[] = []
  if (parsed.part != null) parts.push(`Bahagian ${parsed.part}`)
  if (parsed.chapter != null) parts.push(`Bab ${parsed.chapter}`)
  if (parsed.schedule != null) {
    let s = `Jadual ${parsed.schedule}`
    if (parsed.schedule_list != null) s += `, Senarai ${parsed.schedule_list}`
    parts.push(s)
  }
  if (parsed.provision_num != null) {
    let s = `Seksyen ${parsed.provision_num}${parsed.provision_suffix ?? ''}`
    if (parsed.sub_provision != null) s += `(${parsed.sub_provision}${parsed.sub_provision_suffix ?? ''})`
    if (parsed.paragraph) s += `(${parsed.paragraph})`
    if (parsed.subparagraph) s += `(${parsed.subparagraph})`
    if (parsed.sub_subparagraph) s += `(${parsed.sub_subparagraph})`
    parts.push(s)
  }
  return parts.length > 0 ? parts.join(' → ') : '(unparsed)'
}
