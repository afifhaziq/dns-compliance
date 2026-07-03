<!-- SEED: re-run /impeccable document once there's code to capture the actual tokens and components. -->

---
name: DNS Compliance Checker
description: ISP DNS takedown compliance monitoring for regulatory auditors
---

# Design System: DNS Compliance Checker

## 1. Overview

**Creative North Star: "The Registry"**

The interface is a formal record of compliance facts, not a monitoring dashboard. Like a land registry or court docket, it presents findings with the authority of documentation. Data is the content. Chrome exists to serve it. The design does not celebrate violations or dramatize failures — it records them, the way an auditor would, for a report that may be presented in a hearing.

The palette is built on two roles. Pure gray carries the paper and the printed record — backgrounds, structure, body text. A single deep, low-chroma indigo carries the auditor's own mark: action, identity, selection — the ink of a signature or a registry stamp's date-block, not the document itself. References: Linear's operational precision, Bloomberg Terminal's information density and trust through restraint, Notion's commitment to legibility over hierarchy drama.

This system explicitly rejects: generic SaaS dashboard layouts (cream backgrounds, cheerful sky-blue accents, identical metric-card grids), security product aesthetics (dark mode, neon, animated threat maps), and the AdminLTE vocabulary (blue sidebar, card-grid overviews, excessive widget density). The ledger indigo accent below is not that "blue accent" anti-reference — it is a single deep, desaturated ink tone, used narrowly against an otherwise achromatic page, not a decorative or cheerful hue. If the interface could have been generated from a "compliance dashboard" prompt, it has failed.

**Key Characteristics:**
- Light mode, day-use: formal office context, data for printed reports and screen-shared hearings
- Achromatic base — black, white, and pure grays with zero chroma — plus exactly one reserved hue: ledger indigo (action/identity)
- Dense, scannable tables as the primary surface — not cards
- Responsive feedback transitions on actions; no choreography or entrance sequences
- Structure through tonal surface shifts and spacing, not decorative borders

## 2. Colors: Restrained Neutral Palette with One Accent

Three named semantic roles. Every color in the interface belongs to one of them.

### Accent (Action / Identity)
- **Ledger Indigo** `oklch(0.32 0.09 265)`: Primary action buttons, the active navigation pill, focus rings, links, selected table rows, and the filled portion of quantity bars (e.g. the compliance progress bar). This is the only role permitted outside the achromatic ramp. Text on accent buttons is pure white `oklch(0.985 0 0)`.
- Hover: `oklch(0.38 0.10 265)` · Active/pressed: `oklch(0.26 0.08 265)` · Subtle bg (selected row, link hover): `oklch(0.95 0.02 265)`
- **Quantity vs. status, kept distinct:** the progress-bar fill uses indigo because it visualizes a proportion (how much of this server is compliant), not because indigo means "compliant." The actual compliance verdict is still conveyed only through the status dot and its text label, never through the bar's color alone.

### Compliant (Status only)
- **Medium Gray** `oklch(0.45 0 0)`: Domains that fail DNS resolution (blocks working). Expressed in gray — compliance is the expected, unremarkable state; no celebratory green. Subtle bg: `oklch(0.95 0 0)` · Text: `oklch(0.40 0 0)`

### Neutral — Pure Gray Ramp
All backgrounds, surfaces, borders, muted text, and structural chrome. Zero chroma throughout.

| Token | Value | Usage |
|---|---|---|
| Background | `oklch(1 0 0)` | Page background |
| Nav surface | `oklch(0.985 0 0)` | Top nav bar |
| Panel | `oklch(0.95 0 0)` | Table headers, segmented controls |
| Border | `oklch(0.905 0 0)` | All dividers and outlines |
| Muted | `oklch(0.55 0 0)` | Secondary text, column headers |
| Text | `oklch(0.145 0 0)` | All primary text |

### Named Rules
**The One-Accent Rule.** Exactly one non-gray hue exists in the system: ledger indigo (action/identity). Every other color — surfaces, text, structural chrome — is a pure oklch step with `0` chroma. Adding a second hue breaks the palette contract.

**The Indigo-Is-Not-Status Rule.** Indigo means "you can act here" or "this is selected/yours" — never "this is the result." It never appears on a compliance status dot, chip, or badge. Near-black is no longer a separate action color — indigo fully owns the action/identity role.

**The Status-Colors-Stay-Semantic Rule.** Compliant gray is prohibited outside status contexts. It does not appear on buttons, navigation, headings, decoration, or destructive actions like delete. Its rarity preserves its signal.

## 3. Typography

**Body/UI Font:** DM Sans — neutral, highly legible at 12–14px, authoritative without personality. One family carries all levels: headings, labels, table cells, buttons, body. Stack: `'DM Sans', ui-sans-serif, system-ui, sans-serif`.

**Mono:** Geist Mono — for IP addresses, domain names, DNS server addresses, and other technical strings. Inline only, not as a UI theme. Stack: `'Geist Mono', ui-monospace, monospace`.

**Character:** Maximum legibility at small sizes. A flat enough scale to support dense table interfaces without creating a noise of contrasting sizes. Authority through weight, not scale drama.

### Hierarchy
- **Title** (600 weight, 24px/1.5rem, tracked -0.02em): Page headings only — one per page (`.page-title`). Rare.
- **Section** (600 weight, 18px/1.125rem, tracked -0.02em, sentence case, full foreground color): Major section titles within a page — "National Compliance", "DNS Servers", "Time to Compliance" (`.section-title`). Reads as a smaller sibling of Title, not a bigger Label — case and color stay full-strength so it's unambiguously a heading, not a caption.
- **Body** (400 weight, 0.9375rem, 1.5 line-height, max 70ch for prose): Table cells, labels, supporting text, prose descriptions.
- **Label** (500 weight, 0.75rem, tracked +0.02em, uppercase or title-case): Column headers, status chip text, navigation items, secondary metadata, stat captions nested under a Section title (`.dash-label`).
- **Mono** (system monospace, 0.875rem): IP addresses, domain strings, DNS server identifiers. Inline only.

### Named Rules
**The One Family Rule.** No display/body pairing. No serif accent for headings. One technical sans, tuned at every size. Hierarchy comes from weight and case contrast, not family switching.

**The Fixed Scale Rule.** Fixed rem values, not fluid clamp type. Compliance officers view this tool at consistent screen sizes; a fluid h1 that shrinks in a side panel is wrong. Scale steps as shipped: 24 / 18 / 15 / 11px (Title / Section / Body / Label). Not a single clean ratio (24/18=1.33, 18/15=1.2, 15/11=1.36) — Title, Body, and Label were fixed values already in use before Section was introduced; a fully rebalanced geometric scale would mean resizing those too, which is out of scope for now.

## 4. Elevation

Flat by default. Structure comes from tonal surface shifts — a slightly darker stone step for the sidebar, a slightly lighter step for table headers — rather than shadows or lifted cards. The audit context demands a printed-document quality: no depth illusions, no floating surfaces.

### Radius
- **Base radius**: `1rem` (16px) — the default corner radius for panels, inputs, and modals.
- **Small radius**: `6px` — compact elements: table row chips, inline tags, tight buttons.
- **Medium radius**: `10px` — standard interactive elements: buttons, filter controls, banners.
- **Full radius**: `9999px` — pill shapes: status chips, progress bars.

### Shadow Vocabulary
- **Overlay ambient** — a single subtle ambient shadow (`0 1px 3px oklch(0 0 0 / 0.1)`): Reserved for interactive pop-overs, dropdowns, and tooltips only. Signals that an element is above the content layer. Nothing else uses shadows.

### Named Rules
**The Flat-By-Default Rule.** Surfaces are flat at rest. The only shadow in the system is the overlay ambient, used exclusively for floating UI elements (dropdowns, tooltips, date pickers). Cards, panels, table rows, and list items are flat. Depth is expressed tonally, not with shadows.

**The No-Nesting Rule.** Panels contain content, not other panels. Tables live on content surfaces. If a design requires a card inside a card, the structure is wrong.

## 5. Components

[Omitted in seed. Re-run `/impeccable document` once component code exists to extract button, table row, status chip, input, and navigation patterns.]

## 6. Do's and Don'ts

### Do:
- **Do** use tonal surface shifts (a darker gray step) to separate the nav or panel from the main content area — not decorative borders.
- **Do** make compliance status legible through both color and text label. Color alone is not the signal.
- **Do** use tables as the primary data surface. Rows are the canonical affordance; cards are not.
- **Do** treat ledger indigo as the action/identity color only: primary buttons, active nav, focus rings, links, selected rows, quantity-bar fills. Nowhere else.
- **Do** use inline monospace (Geist Mono) for IP addresses, domain strings, and DNS server identifiers.
- **Do** include all interactive states: hover, focus-visible, loading skeleton, empty state. Especially on the scan results table.
- **Do** keep motion in the 150–250 ms range for state transitions. Feedback on action, not choreography on load.

### Don't:
- **Don't** use a generic metric-card grid (4 identical horizontal cards at the top of the overview). From PRODUCT.md: this is explicitly banned as a layout pattern.
- **Don't** use gradient backgrounds or gradient text. Prohibited.
- **Don't** use `border-left` or `border-right` greater than 1px as a colored accent stripe on any element. Rewrite with background tints or nothing.
- **Don't** use any hue with non-zero oklch chroma except ledger indigo. A second hue, or indigo used on a status indicator, breaks the contract.
- **Don't** use Security product aesthetics: no dark mode, no neon, no animated threat maps, no globe visualizations.
- **Don't** use the AdminLTE / Bootstrap admin vocabulary: no blue sidebar, no card-grid overview page, no widget-heavy panels.
- **Don't** use generic SaaS dashboard styling: no cream backgrounds with cheerful sky-blue accents, no soft rounded stat cards, no gradient accents on data numbers. (Ledger indigo is deliberately darker and more desaturated than this anti-reference.)
- **Don't** use display fonts, serif accents, or decorative type treatments in UI labels, table cells, or buttons. DM Sans only, consistently.
- **Don't** animate page-load sequences or choreograph content entry. The interface loads into the task.
