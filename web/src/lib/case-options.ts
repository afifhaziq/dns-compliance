// Shared between case-history-dialog.tsx and urls.tsx's AddUrlDialog — both
// let a user open a case with a phase/letter-type/reference-number.

export const PHASE_OPTIONS = [
  { value: 'requested', label: 'Requested' },
  { value: 'uplift', label: 'Uplift' },
  { value: 'suspended', label: 'Suspended' },
]

// Mirrors the four letter types CMOD tracks (docs/cmod-blocking-list-migration-clarifications.md);
// CRD-sourced cases default to 'Notice' per docs/db-schema.dbml's resolved design note.
export const LETTER_TYPE_OPTIONS = ['Notice', 'Memo', 'Notice (Uplift)', 'Memo (Uplift)']
