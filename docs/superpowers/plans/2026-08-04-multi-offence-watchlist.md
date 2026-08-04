# Multi-Offence Watchlist Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let analysts attach multiple offences to a domain on the watchlist page, both at Add-Domain time and afterward on already-listed domains, and make `internal/legalcite.Parse`'s `NEEDS_REVIEW` flag fire reliably for multi-provision citations so it correctly routes analysts to that new multi-offence workflow.

**Architecture:** A new `MultiOffencePicker` React component (replacing today's single-selection `OffencePicker`) stages one offence at a time and appends it to a removable-chip list; it's reused both inside `AddUrlDialog` (offences applied to every domain in the batch on submit) and inside a new `EditOffencesDialog` (each add/remove is an immediate API call against one already-existing domain, opened via a new per-row icon in the watchlist table). No new backend endpoints — `AttachOffenceToURL`/`fetchOffencesByUrl`/`detachOffence` already exist and are simply unused today. Separately, `internal/legalcite/parser.go` gets two small, independent regex/logic fixes so multi-provision citations reliably return `NEEDS_REVIEW` instead of silently dropping data at `Confidence: OK`.

**Tech Stack:** Go (backend, `internal/legalcite`), React + TypeScript (frontend, `web/src/routes/urls.tsx`), existing REST API (`internal/server/legal_handlers.go`, unchanged).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-04-multi-offence-watchlist-design.md` — read it for full rationale; this plan implements it task-by-task.
- Scope is the watchlist page (`urls.tsx`) only — no changes to `domain.$url.tsx` (out of scope per spec).
- The same offence list applies to every domain in one Add-Domain batch submission (not per-domain).
- No new backend offence-CRUD — reuse `AttachOffenceToURL`, `GET /api/legal/offences/*url`, `DELETE /api/legal/offences/{id}` exactly as they exist today.
- `go test ./internal/legalcite/...` must pass with zero regressions on pre-existing test cases after the parser fix.
- Follow existing code conventions in the touched files: Tailwind `@apply` in `index.css`, the `screenshot-icon-btn` class for row icon buttons, `Icon`-suffixed lucide-react imports (`ChevronLeftIcon`, not `ChevronLeft`).

---

### Task 1: Fix `legalcite.Parse` to reliably flag multi-provision citations

**Files:**
- Modify: `internal/legalcite/parser.go`
- Test: `internal/legalcite/parser_test.go`

**Interfaces:**
- Consumes: nothing new — this task is self-contained within the existing `legalcite` package.
- Produces: unchanged public API (`Parse(raw string) Result`, `ConfidenceOK`/`ConfidenceNeedsReview`) — only internal behavior changes. Later tasks (frontend) don't touch this package at all.

- [ ] **Step 1: Write the 4 failing tests**

Add these 4 cases to the `cases` slice in `TestParse_RealWorldMalaySamples` (`internal/legalcite/parser_test.go`), right before the closing `}` of the slice (after the existing `"seksyen only — instrument name's own \"dan\" must not be mistaken for a joined list"` case, before line 164's closing brace):

```go
		{
			name: "ampersand-joined provisions — bug: parser didn't recognize '&' as a list joiner",
			raw:  "Seksyen 292 & Seksyen 372 Kanun Keseksaan",
			want: legalcite.Parsed{ProvisionNum: iptr(292)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "ampersand-joined provisions, peraturan label, no instrument suffix words",
			raw:  "Peraturan 62 & 63 Enakmen Kesalahan Syariah Negeri Melaka 1991",
			want: legalcite.Parsed{ProvisionNum: iptr(62)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "list marker after a subsection paren — bug: multiProvisionRe was anchored to the start of tail",
			raw:  "Seksyen 4(1), 7(a), 9 dan 12 Enakmen Kesalahan Jenayah Syariah (Johor) 1997",
			want: legalcite.Parsed{ProvisionNum: iptr(4), SubProvision: iptr(1)},
			conf: legalcite.ConfidenceNeedsReview,
		},
		{
			name: "missing-space typo before dan — bug: greedy suffix letter consumed the 'd' of 'dan'",
			raw:  "Seksyen 14dan 15 Enakmen Kesalahan Jenayah Syariah (Takzir) (Terengganu) 2001",
			want: legalcite.Parsed{ProvisionNum: iptr(14)},
			conf: legalcite.ConfidenceNeedsReview,
		},
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/legalcite/... -run TestParse_RealWorldMalaySamples -v`

Expected: the 4 new subtests FAIL — the first two report `Confidence = "OK", want "NEEDS_REVIEW"`; the third reports `Confidence = "OK", want "NEEDS_REVIEW"` with `Parsed` also containing an unwanted `Paragraph: "a"`; the fourth reports `Confidence = "OK", want "NEEDS_REVIEW"` with `Parsed` containing an unwanted `ProvisionSuffix: "D"`. All other subtests still PASS.

- [ ] **Step 3: Fix 1 — widen `multiProvisionRe` to search the whole tail and recognize `&`**

In `internal/legalcite/parser.go`, replace the `multiProvisionRe` declaration (currently lines 78-83, in the `var (...)` block right after `provisionRe`):

```go
	// A number immediately followed by ", <digit>" or "dan [seksyen] <digit>"
	// is a list of provisions joined into one string (e.g. "Seksyen 211 dan
	// 233 Akta ...", "Seksyen 7, 9, 10 ... dan 16 Enakmen ..."). A Citation
	// only has one provision_num field, so silently keeping just the first
	// number would quietly drop the rest — flag NEEDS_REVIEW instead.
	multiProvisionRe = regexp.MustCompile(`(?i)^\s*(?:,\s*\d|dan\s+(?:seksyen\s+)?\d)`)
```

with:

```go
	// A number followed anywhere later by ", <digit>", "& [seksyen] <digit>",
	// or "dan [seksyen] <digit>" is a list of provisions joined into one
	// string (e.g. "Seksyen 211 dan 233 Akta ...", "Seksyen 7, 9, 10 ... dan
	// 16 Enakmen ...", "Seksyen 292 & Seksyen 372 ...", "Seksyen 4(1), 7(a),
	// 9 dan 12 ..." — the list marker doesn't have to sit immediately after
	// the first provision number; it can follow a subsection/paragraph
	// paren first). A Citation only has one provision_num field, so
	// silently keeping just the first number would quietly drop the rest —
	// flag NEEDS_REVIEW instead. Deliberately not anchored to the start of
	// tail, so it still fires when a paren group comes first; \b before
	// "dan" prevents matching inside an unrelated word ending in "...dan".
	multiProvisionRe = regexp.MustCompile(`(?i)(?:,\s*\d|&\s*(?:seksyen\s+)?\d|\bdan\s+(?:seksyen\s+)?\d)`)
```

- [ ] **Step 4: Fix 2 — stop the suffix letter from swallowing a typo'd "dan"**

In the same file, in `Parse`, replace this block (currently around lines 181-184, right after `m := provisionRe.FindStringSubmatchIndex(remainder)` and its nil check):

```go
	num, _ := strconv.Atoi(remainder[m[2]:m[3]])
	p.ProvisionNum = &num
	p.ProvisionSuffix = strings.ToUpper(remainder[m[4]:m[5]])
	tail := remainder[m[1]:]
```

with:

```go
	num, _ := strconv.Atoi(remainder[m[2]:m[3]])
	p.ProvisionNum = &num
	suffixStart, suffixEnd, tailStart := m[4], m[5], m[1]
	if suffixEnd > suffixStart && suffixEnd < len(remainder) && letterRe.MatchString(remainder[suffixEnd:suffixEnd+1]) {
		// The captured "suffix" letter is immediately followed by another
		// letter — it's actually the start of a word (commonly a
		// missing-space typo like "14dan 15" for "14 dan 15"), not a real
		// amendment suffix. Real suffixes (4A, 372B) are always followed by
		// a non-letter (space/paren/end-of-string). Discard the bogus
		// suffix and re-anchor tail right after the digits so the stray
		// letter survives into tail, where multiProvisionRe now catches it.
		tailStart = m[3]
	} else {
		p.ProvisionSuffix = strings.ToUpper(remainder[suffixStart:suffixEnd])
	}
	tail := remainder[tailStart:]
```

(`letterRe` already exists earlier in the same `var (...)` block — no new regex needed.)

- [ ] **Step 5: Run the full package test suite to verify everything passes**

Run: `go test ./internal/legalcite/... -v`

Expected: PASS for every subtest — the 4 new ones from Step 1, plus every pre-existing case in both `TestParse` and `TestParse_RealWorldMalaySamples`, plus `TestSortKey_OrdersSuffixCorrectly`. If any pre-existing case fails, do not proceed — re-check the regex/logic change against that specific input before continuing (the design spec's "Fix 1"/"Fix 2" sections document why each existing case should be unaffected; find where the trace diverges).

- [ ] **Step 6: Commit**

```bash
git add internal/legalcite/parser.go internal/legalcite/parser_test.go
git commit -m "$(cat <<'EOF'
fix: catch &-joined and mid-tail multi-provision citations in legalcite.Parse

multiProvisionRe was anchored to the start of tail, so a list marker
following a subsection paren (e.g. "4(1), 7(a), 9 dan 12") was never
detected, and it had no concept of "&" as a joiner at all — both cases
silently dropped provisions while reporting Confidence=OK. Separately,
provisionRe's greedy suffix letter could consume the "d" of a
missing-space "14dan 15" typo, fabricating a fake amendment suffix.

Found during a verification pass over 50 real MCMC blocking-list
citations (see legalcite-verify/docs/legalcite-verification-report.md).

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Add `MultiOffencePicker` and wire it into `AddUrlDialog`

**Files:**
- Modify: `web/src/routes/urls.tsx:1-6` (imports)
- Modify: `web/src/routes/urls.tsx:73-174` (replace `OffencePicker` with `StagedOffence` type + `MultiOffencePicker`)
- Modify: `web/src/routes/urls.tsx:176-268` (`AddUrlDialog` — replace offence state/submit/JSX)
- Modify: `web/src/index.css` (new `.offence-chip-list`/`.offence-chip` classes, after `.form-success`)

**Interfaces:**
- Produces: `type StagedOffence = { instrumentId: number; citationId: number; categoryId: number; elementId?: number; label: string }` and `function MultiOffencePicker({ value: StagedOffence[], onChange: (offences: StagedOffence[]) => void, disabled: boolean })` — Task 3's `EditOffencesDialog` imports and reuses both from this file.

- [ ] **Step 1: Add the new CSS classes**

In `web/src/index.css`, right after the `.form-success` rule (currently ends around line 1094, right before `.back-link`), add:

```css
.offence-chip-list {
  @apply flex flex-col gap-1.5 mb-2;
}

.offence-chip {
  @apply flex items-center justify-between gap-2 py-1 px-2.5 text-[13px] text-foreground bg-stone-panel border border-stone-border rounded-lg;
}
```

- [ ] **Step 2: Update imports in `urls.tsx`**

Replace line 3:
```ts
import { ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
```
with:
```ts
import { ChevronLeftIcon, ChevronRightIcon, ScaleIcon } from 'lucide-react'
```
(`ScaleIcon` is used by Task 3, imported here now to keep the import list in one place — harmless if temporarily unused between tasks, `tsc`/eslint will flag it as unused only if Task 3 isn't done in the same session; if running Task 2 standalone, skip adding `ScaleIcon` here and add it in Task 3 instead.)

No other import changes needed for this task — `fetchInstruments`, `fetchCitations`, `fetchCategories`, `fetchElements`, `attachOffence`, `formatParsedCitation` are already imported (line 6), and `Instrument`, `Citation`, `LegalCategory`, `LegalElement` are already imported (line 5).

- [ ] **Step 3: Replace `OffencePicker` with `StagedOffence` + `MultiOffencePicker`**

Replace the entire `OffencePicker` function (currently lines 73-174, from the `// Cascading Instrument -> Citation -> Category -> Element picker...` comment through its closing `}`) with:

```tsx
export type StagedOffence = {
  instrumentId: number
  citationId: number
  categoryId: number
  elementId?: number
  label: string
}

// Cascading Instrument -> Citation -> Category -> Element picker that stages
// one offence at a time and appends it to a removable-chip list on "Add
// offence" — a domain can violate multiple sections/offences at once (real
// MCMC data cites domains under several sections joined by "dan"/"&"), so
// this replaces the old single-selection OffencePicker. Reused by both
// AddUrlDialog (staged offences applied to every domain on submit) and
// EditOffencesDialog (each addition attaches immediately to one existing
// domain) — this component has no knowledge of which caller it's in.
function MultiOffencePicker({
  value, onChange, disabled,
}: {
  value: StagedOffence[]
  onChange: (offences: StagedOffence[]) => void
  disabled: boolean
}) {
  const [instruments, setInstruments] = useState<Instrument[]>([])
  const [citations, setCitations] = useState<Citation[]>([])
  const [categories, setCategories] = useState<LegalCategory[]>([])
  const [elements, setElements] = useState<LegalElement[]>([])

  const [instrumentId, setInstrumentId] = useState<number | ''>('')
  const [citationId, setCitationId] = useState<number | ''>('')
  const [categoryId, setCategoryId] = useState<number | ''>('')
  const [elementId, setElementId] = useState<number | ''>('')

  useEffect(() => { fetchInstruments().then(setInstruments) }, [])
  useEffect(() => {
    if (instrumentId === '') { setCitations([]); return }
    fetchCitations(instrumentId).then(setCitations)
  }, [instrumentId])
  useEffect(() => {
    if (citationId === '') { setCategories([]); return }
    fetchCategories(citationId).then(setCategories)
  }, [citationId])
  useEffect(() => {
    if (categoryId === '') { setElements([]); return }
    fetchElements(categoryId).then(setElements)
  }, [categoryId])

  const resetStaging = () => {
    setInstrumentId(''); setCitationId(''); setCategoryId(''); setElementId('')
  }

  const handleAdd = () => {
    if (instrumentId === '' || citationId === '' || categoryId === '') return
    const citation = citations.find(c => c.id === citationId)
    const category = categories.find(c => c.id === categoryId)
    const element = elementId === '' ? undefined : elements.find(e => e.id === elementId)
    if (!citation || !category) return
    const label = `${formatParsedCitation(citation.parsed)} — ${category.name}${element ? ` (${element.name})` : ''}`
    onChange([...value, { instrumentId, citationId, categoryId, elementId: elementId === '' ? undefined : elementId, label }])
    resetStaging()
  }

  const handleRemove = (index: number) => {
    onChange(value.filter((_, i) => i !== index))
  }

  return (
    <div className="form-field">
      <label className="form-label" id="offence-picker-label">
        Offences <span style={{ color: 'var(--stone-muted)', fontWeight: 400 }}>(optional — attaches to every domain added above)</span>
      </label>
      {value.length > 0 && (
        <ul className="offence-chip-list">
          {value.map((o, i) => (
            <li key={i} className="offence-chip">
              <span>{o.label}</span>
              <button
                type="button"
                className="screenshot-icon-btn"
                onClick={() => handleRemove(i)}
                disabled={disabled}
                aria-label={`Remove offence ${o.label}`}
              >
                <XIcon size={14} />
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="flex flex-col" style={{ gap: 8 }}>
        <Select
          value={String(instrumentId)}
          onValueChange={v => { setInstrumentId(v === '' ? '' : Number(v)); setCitationId(''); setCategoryId(''); setElementId('') }}
          disabled={disabled}
        >
          <SelectTrigger aria-labelledby="offence-picker-label" placeholder="Instrument…" className="w-full" />
          <SelectContent>
            <SelectItem index={0} value="">No instrument</SelectItem>
            {instruments.map((inst, i) => (
              <SelectItem key={inst.id} index={i + 1} value={String(inst.id)}>{inst.short_title}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        {instrumentId !== '' && (
          <Select
            value={String(citationId)}
            onValueChange={v => { setCitationId(v === '' ? '' : Number(v)); setCategoryId(''); setElementId('') }}
            disabled={disabled}
          >
            <SelectTrigger aria-label="Citation" placeholder="Citation…" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">No citation</SelectItem>
              {citations.map((c, i) => (
                <SelectItem key={c.id} index={i + 1} value={String(c.id)}>{formatParsedCitation(c.parsed)} ({c.raw_text})</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {citationId !== '' && (
          <Select
            value={String(categoryId)}
            onValueChange={v => { setCategoryId(v === '' ? '' : Number(v)); setElementId('') }}
            disabled={disabled}
          >
            <SelectTrigger aria-label="Category" placeholder="Category…" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">No category</SelectItem>
              {categories.map((cat, i) => (
                <SelectItem key={cat.id} index={i + 1} value={String(cat.id)}>{cat.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {categoryId !== '' && elements.length > 0 && (
          <Select
            value={String(elementId)}
            onValueChange={v => setElementId(v === '' ? '' : Number(v))}
            disabled={disabled}
          >
            <SelectTrigger aria-label="Element" placeholder="Element (optional)…" className="w-full" />
            <SelectContent>
              <SelectItem index={0} value="">No element</SelectItem>
              {elements.map((el, i) => (
                <SelectItem key={el.id} index={i + 1} value={String(el.id)}>{el.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {categoryId !== '' && (
          <button type="button" className="btn-ghost" onClick={handleAdd} disabled={disabled}>
            + Add offence
          </button>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Wire `MultiOffencePicker` into `AddUrlDialog`**

Replace the entire `AddUrlDialog` function (currently lines 176-268) with:

```tsx
function AddUrlDialog({
  open,
  onClose,
  onAdded,
}: {
  open: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [value, setValue] = useState('')
  const [offences, setOffences] = useState<StagedOffence[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reset = () => {
    setValue(''); setOffences([]); setError(null)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const domains = value.split('\n').map(s => s.trim()).filter(Boolean)
    if (domains.length === 0) { setError('At least one domain is required'); return }
    setLoading(true)
    setError(null)
    try {
      await Promise.all(domains.map(d => createUrl(d)))
      await Promise.all(
        domains.flatMap(d => offences.map(o => attachOffence(d, o.categoryId, o.elementId)))
      )
      reset()
      onAdded()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add domain')
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => { reset(); onClose() }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 440 }}>
        <DialogHeader>
          <DialogTitle>Add Domain</DialogTitle>
          <DialogDescription>
            Enter one or more domains or full URLs to monitor for DNS compliance. Full URLs will have their domain automatically extracted. You can add multiple entries at once, just put each one on a new line.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label className="form-label" htmlFor="add-url-input">Domain</label>
            <textarea
              id="add-url-input"
              className="form-input"
              placeholder={'https://example.com\nhttps://example2.com'}
              value={value}
              onChange={e => setValue(e.target.value)}
              autoFocus
              disabled={loading}
              rows={4}
              style={{ resize: 'vertical', fontFamily: 'inherit' }}
            />
          </div>
          <MultiOffencePicker
            value={offences}
            onChange={setOffences}
            disabled={loading}
          />
          {error && <p className="form-error">{error}</p>}
          <DialogFooter>
            <button type="button" className="btn-ghost" onClick={handleClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={loading}>
              {loading ? 'Adding…' : 'Add Domain'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 5: Type-check and lint**

Run: `cd web && npx tsc --noEmit && npm run lint`

Expected: no errors. If `ScaleIcon` is reported unused (because Task 3 hasn't been done yet in this session), remove it from the Step 2 import for now and re-add it at the start of Task 3 instead.

- [ ] **Step 6: Manual verification**

Run `./dev.sh` from the repo root (or `go run ./cmd/server/ ...` + `cd web && npm run dev` per `CLAUDE.md`, with an admin user logged in who has at least one `Instrument`/`Citation`/`Category` already in the legal catalog — add one via `/legal-citations` first if the catalog is empty). On `/urls`:
1. Click "+ Add Domain", enter a domain, and in the Offences picker select an Instrument → Citation → Category, click "+ Add offence" — confirm a chip appears and the picker resets to "Instrument…".
2. Repeat to stage a second, different offence — confirm both chips are listed.
3. Click the `×` on one chip — confirm it's removed from the list.
4. Submit the form — confirm the domain is created and no error appears.
5. Confirm via `GET /api/legal/offences/<domain>` (e.g. `curl` with the session cookie, or browser devtools Network tab) that both remaining staged offences were attached.

- [ ] **Step 7: Commit**

```bash
git add web/src/routes/urls.tsx web/src/index.css
git commit -m "$(cat <<'EOF'
feat: support attaching multiple offences to a domain in Add Domain

OffencePicker only ever supported one offence per submission. Replace
it with MultiOffencePicker, which stages offences into a removable
chip list before submit — a domain can genuinely violate multiple
sections at once (see docs/superpowers/specs/2026-08-04-multi-offence-watchlist-design.md).

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Add `EditOffencesDialog` and wire it into the watchlist table

**Files:**
- Modify: `web/src/routes/urls.tsx:1-6` (imports — add `ScaleIcon` if not already added in Task 2, plus `fetchOffencesByUrl`/`detachOffence`/`URLOffence`)
- Modify: `web/src/routes/urls.tsx` (add `EditOffencesDialog` function, near `AddUrlDialog`)
- Modify: `web/src/routes/urls.tsx` (`URLsPage` — add state, per-row icon button, dialog render)

**Interfaces:**
- Consumes: `StagedOffence` type and `MultiOffencePicker` component from Task 2 (same file).
- Produces: `function EditOffencesDialog({ url: string | null, open: boolean, onClose: () => void })` — rendered once at the bottom of `URLsPage`, alongside the existing `AddUrlDialog`/`DeleteConfirmDialog`.

- [ ] **Step 1: Update imports**

Ensure line 3 includes `ScaleIcon` (add it now if Task 2's Step 2 skipped it):
```ts
import { ChevronLeftIcon, ChevronRightIcon, ScaleIcon } from 'lucide-react'
```

Update line 4 to add `URLOffence` to the type import:
```ts
import type { URLEntry, Instrument, Citation, LegalCategory, LegalElement, URLOffence } from '../api/types'
```

Update line 6 to add `fetchOffencesByUrl` and `detachOffence`:
```ts
import { fetchInstruments, fetchCitations, fetchCategories, fetchElements, attachOffence, fetchOffencesByUrl, detachOffence, formatParsedCitation } from '../api/legal'
```

- [ ] **Step 2: Add `EditOffencesDialog`**

Add this function right after `AddUrlDialog` (i.e. right before the `/* ─── Skeleton ─── */` comment block):

```tsx
function EditOffencesDialog({
  url,
  open,
  onClose,
}: {
  url: string | null
  open: boolean
  onClose: () => void
}) {
  const [offences, setOffences] = useState<URLOffence[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [staged, setStaged] = useState<StagedOffence[]>([])

  const load = useCallback(async () => {
    if (!url) return
    setLoading(true)
    setError(null)
    try {
      setOffences(await fetchOffencesByUrl(url))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load offences')
    } finally {
      setLoading(false)
    }
  }, [url])

  useEffect(() => {
    if (open) { load(); setStaged([]) }
  }, [open, load])

  const handleRemove = async (id: number) => {
    setError(null)
    try {
      await detachOffence(id)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove offence')
    }
  }

  // MultiOffencePicker's onChange always receives the full next array; the
  // newly-staged item is always the last one, since this dialog has no
  // batch-submit step — every addition attaches immediately, unlike
  // AddUrlDialog which accumulates offences for one later submit.
  const handleAddStaged = async (next: StagedOffence[]) => {
    if (!url || next.length === 0) { setStaged(next); return }
    const added = next[next.length - 1]
    setError(null)
    try {
      await attachOffence(url, added.categoryId, added.elementId)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add offence')
    } finally {
      setStaged([])
    }
  }

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) onClose() }}>
      <DialogContent showCloseButton={false} style={{ maxWidth: 440 }}>
        <DialogHeader>
          <DialogTitle>Offences</DialogTitle>
          <DialogDescription>{url}</DialogDescription>
        </DialogHeader>
        {loading ? (
          <p className="text-sm text-stone-muted">Loading…</p>
        ) : offences.length > 0 ? (
          <ul className="offence-chip-list">
            {offences.map(o => (
              <li key={o.id} className="offence-chip">
                <span>{formatParsedCitation(o.category.citation.parsed)} — {o.category.name}{o.element ? ` (${o.element.name})` : ''}</span>
                <button
                  type="button"
                  className="screenshot-icon-btn"
                  onClick={() => handleRemove(o.id)}
                  aria-label={`Remove offence ${o.category.name}`}
                >
                  <XIcon size={14} />
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-stone-muted mb-2">No offences attached yet.</p>
        )}
        <MultiOffencePicker value={staged} onChange={handleAddStaged} disabled={loading} />
        {error && <p className="form-error">{error}</p>}
        <DialogFooter>
          <button type="button" className="btn-primary" onClick={onClose}>
            Done
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 3: Add state and the per-row icon button in `URLsPage`**

In `URLsPage`, add a new state field right after `deleteTarget` (currently `const [deleteTarget, setDeleteTarget] = useState<URLEntry | null>(null)`):

```tsx
  const [editOffencesTarget, setEditOffencesTarget] = useState<string | null>(null)
```

In the table body, replace the `col-evidence` `TableCell` (currently just the delete button):

```tsx
                    <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                      <button
                        type="button"
                        className="screenshot-icon-btn"
                        onClick={() => setDeleteTarget(u)}
                        aria-label={`Delete ${u.url}`}
                        title="Delete"
                      >
                        <XIcon size={16} />
                      </button>
                    </TableCell>
```

with:

```tsx
                    <TableCell className="col-evidence" style={{ textAlign: 'right' }}>
                      <button
                        type="button"
                        className="screenshot-icon-btn"
                        onClick={() => setEditOffencesTarget(u.url)}
                        aria-label={`Edit offences for ${u.url}`}
                        title="Offences"
                      >
                        <ScaleIcon size={16} />
                      </button>
                      <button
                        type="button"
                        className="screenshot-icon-btn"
                        onClick={() => setDeleteTarget(u)}
                        aria-label={`Delete ${u.url}`}
                        title="Delete"
                      >
                        <XIcon size={16} />
                      </button>
                    </TableCell>
```

- [ ] **Step 4: Render the dialog**

In `URLsPage`'s returned JSX, right after the existing `<DeleteConfirmDialog ... />` block (at the end, before the closing `</div>`), add:

```tsx
      <EditOffencesDialog
        url={editOffencesTarget}
        open={editOffencesTarget !== null}
        onClose={() => setEditOffencesTarget(null)}
      />
```

- [ ] **Step 5: Type-check and lint**

Run: `cd web && npx tsc --noEmit && npm run lint`

Expected: no errors.

- [ ] **Step 6: Manual verification**

With `./dev.sh` running and at least one domain already on the watchlist with an existing offence (from Task 2's manual verification):
1. Click the new scale icon on that domain's row — confirm the dialog opens and shows the existing offence(s) as chips.
2. Remove one via its `×` — confirm it disappears and a `GET /api/legal/offences/<domain>` no longer includes it.
3. Add a new offence via the picker at the bottom — confirm it appears in the list immediately (no page reload needed) and is present via the same `GET` check.
4. Open the dialog for a domain with zero offences — confirm it shows "No offences attached yet." instead of an empty list.
5. Close and reopen the dialog — confirm it always reflects current server state (re-fetches on open).

- [ ] **Step 7: Commit**

```bash
git add web/src/routes/urls.tsx
git commit -m "$(cat <<'EOF'
feat: view/add/remove offences on an existing watchlist domain

fetchOffencesByUrl/detachOffence existed in web/src/api/legal.ts but
had no frontend caller — offences could only ever be set once, at
Add Domain time. Add a per-row action opening EditOffencesDialog,
reusing MultiOffencePicker from the Add Domain flow so both surfaces
share one picker component.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review

**Spec coverage:**
- `MultiOffencePicker` component (spec's Frontend section) → Task 2, Step 3.
- `AddUrlDialog` changes (staged offence list, batch submit via `flatMap`) → Task 2, Step 4.
- `EditOffencesDialog` (spec's new component) → Task 3, Step 2.
- Watchlist table per-row action → Task 3, Step 3.
- Parser Fix 1 (widen `multiProvisionRe`) → Task 1, Step 3.
- Parser Fix 2 (suffix-letter greediness) → Task 1, Step 4.
- Parser test cases from the spec's table → Task 1, Step 1 (all 4 present, exact `want`/`conf` values matched).
- Error handling decisions (`Promise.all` partial failure in Add Domain; re-fetch-on-failure in Edit) → Task 2 Step 4's `handleSubmit`, Task 3 Step 2's `handleRemove`/`handleAddStaged`.
- Out-of-scope items (`domain.$url.tsx`, per-domain batch offences, new backend endpoints) → correctly absent from all three tasks.

**Placeholder scan:** No "TBD"/"TODO"/"add appropriate handling" found — every step has complete, runnable code or an exact shell command.

**Type consistency:** `StagedOffence` (Task 2, Step 3) is used identically in Task 2 Step 4 (`AddUrlDialog`) and Task 3 Step 2 (`EditOffencesDialog`'s `staged`/`handleAddStaged`) — same field names (`instrumentId`, `citationId`, `categoryId`, `elementId`, `label`) throughout. `MultiOffencePicker`'s props (`value`, `onChange`, `disabled`) are called with matching argument shapes in both consumers. `attachOffence(url, categoryId, elementId)`'s signature (from `web/src/api/legal.ts`, unmodified) matches every call site.
