// Triggers the browser's save dialog for an in-memory Blob — used by the
// Cases-view (urls.tsx) and Docs (docs.tsx) export buttons to save the
// .xlsx returned by exportCaseSummaries/exportCaseLetters.
export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
