// Shared between urls.tsx's AddUrlDialog and docs.tsx's AddDocumentDialog —
// both let a user open a case with a status/letter-type/reference-number.

export const CASE_STATUS_OPTIONS = [
  { value: 'requested', label: 'Requested' },
  { value: 'blocked', label: 'Blocked' },
  { value: 'uplift', label: 'Uplift' },
  { value: 'suspended', label: 'Suspended' },
  { value: 'not_blocked', label: 'Not Blocked' },
  { value: 'internal', label: 'Internal' },
]

// Mirrors the four letter types CMOD tracks (docs/cmod-blocking-list-migration-clarifications.md);
// CRD-sourced cases default to 'Notice' per docs/db-schema.dbml's resolved design note.
export const LETTER_TYPE_OPTIONS = ['Notice', 'Memo', 'Notice (Uplift)', 'Memo (Uplift)']

// CMOD tracks a case by its letters' workflow status and has no per-domain
// status; every other department the reverse (server enforces the same
// split, internal/server/case_handlers.go).
export const usesWorkflowStatus = (departmentName?: string) => departmentName === 'CMOD'
