# Legal Sub-Element Design

**Date:** 2026-08-04
**Branch:** main (design phase — not yet branched)

## Overview

Add a fifth level to the legal citation catalog — `SubElement`, an optional sub-category of `Element` (mirroring how `Element` is itself an optional sub-category of `Category`). Admin/dept-admin manage it on `/legal-citations`; users can optionally select it when attaching an offence to a domain.

## Background

The catalog is currently a four-level hierarchy, each level FK'd to its parent with `OnDelete:CASCADE`: `Instrument` → `Citation` → `Category` → `Element` (optional). `URLOffence` links a `URL` to a `Category` + optional `Element`. This is a direct, one-level-deeper extension of the existing pattern — `Element` is the template to mirror exactly.

## Decision

Mirror `Element` exactly, one level down:

```go
type SubElement struct {
    ID        uint      `gorm:"primaryKey" json:"id"`
    ElementID uint      `gorm:"not null;index" json:"element_id"`
    Element   Element   `gorm:"foreignKey:ElementID;constraint:OnDelete:CASCADE" json:"-"`
    Name      string    `gorm:"not null" json:"name"`
    CreatedAt time.Time `json:"created_at"`
}
```

`URLOffence` gains an optional `SubElementID *uint` / `SubElement *SubElement` (same nullable-FK pattern as `ElementID`).

## Backend

- Add `SubElement` to `AutoMigrate`'s model list and `URLOffence`'s new column — plain additive migration, no rename involved (unlike the due-date field elsewhere), so no special migration handling needed.
- `db.LegalCitationStore`: add `ListSubElements(elementID)`, `CreateSubElement`, `UpdateSubElement`, `DeleteSubElement` — copy the existing `Element` methods in `internal/db/legalcite.go` verbatim, adjusting the parent field name.
- Routes: `GET /api/legal/elements/{id}/subelements` (open read, same as the other catalog read routes), `POST/PATCH/DELETE /api/legal/subelements[/{id}]` (admin-or-dept-admin, same gate as `Category`/`Element`).
- `AttachOffence` (`POST /api/legal/offences/*url`): accept an optional `sub_element_id` alongside the existing `category_id`/`element_id`.

## Frontend

- `legal-citations.tsx`: extend the indented tree one more level — copy the `Element` row/add/edit/delete UI for `SubElement`, nested under each `Element` row.
- `web/src/api/legal.ts`: add `fetchSubElements`, `createSubElement`/`updateSubElement`/`deleteSubElement`.
- `MultiOffencePicker` (`urls.tsx`): add an optional cascading SubElement select, shown only when the currently-selected `Element` has sub-elements — mirror exactly how the existing Element step is only shown/relevant when the selected `Category` has elements (grep the current conditional for that pattern and replicate it one level down). `EditOffencesDialog` gets the same addition since it reuses `MultiOffencePicker`.

## Testing

- Mirror any existing `Element`-level backend tests 1:1 for `SubElement` (CRUD + cascade-delete-on-parent-removal).
- Manual `dev.sh` verification: as admin, add/edit/delete a sub-element under an existing element on `/legal-citations`; as a regular user, confirm the sub-element step appears (only) when attaching an offence whose selected element has sub-elements, and that it's optional (can submit without picking one).

## Out of Scope

- Any nesting beyond sub-element — five levels (`Instrument` → `Citation` → `Category` → `Element` → `SubElement`) is the ceiling.
- Changing existing `Element`/`Category` behavior.
