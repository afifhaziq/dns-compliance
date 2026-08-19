# SLA Scan Settings — Frontend Handoff

> **For agentic workers:** This is a bounded, single-file-ish frontend task — no brainstorming/spec cycle needed, the backend is already built and the UI pattern to follow already exists in the file you're editing. Read this whole doc, then implement directly; no separate plan doc needed beyond this one.

**Goal:** Add two new fields — `sla_interval_minutes` and `sla_streak_threshold` — to the existing "Scan Settings" panel on `/admin`, so an admin can view and edit them from the UI instead of only via `--sla-interval`/`--sla-streak-threshold` CLI flags or raw API calls.

## Context you need

The backend for this already landed (currently **uncommitted on the working tree** — check `git status`/`git diff` before you start so you're not surprised by pre-existing changes to `internal/`, `cmd/server/main.go`). It added a second, independent scan scheduler:

- **`sla_interval_minutes`** — how often URLs still under active SLA tracking (a `DueDate` that's passed, not yet racked up enough consecutive compliant scans) get scanned. Typically shorter than the normal `interval_minutes`, so time-to-compliance measurement (`GET /api/isps/{isp}/timing`) has finer granularity for domains that actually matter for SLA reporting.
- **`sla_streak_threshold`** — how many consecutive compliant scans, per `(URL, DNS server)`, are required before a URL under SLA tracking reverts to the normal scan interval.

Both live on the same single-row `db.ScanSettings` table as the existing `interval_minutes`/`enabled`/`dns_workers`, and are exposed through the **same** endpoint you're already calling — no new route.

Full mechanism (background reading, not required to do the frontend work): `internal/server/CLAUDE.md`'s "Scheduler" section, `internal/server/scheduler.go`.

## API contract (already live on the backend)

`GET /api/admin/scan-interval` now returns:
```json
{
  "interval_minutes": 60,
  "enabled": true,
  "dns_workers": 100,
  "sla_interval_minutes": 15,
  "sla_streak_threshold": 3
}
```

`PATCH /api/admin/scan-interval` now **requires** all five fields in the body, not just the original three:
```json
{
  "interval_minutes": 60,
  "enabled": true,
  "dns_workers": 100,
  "sla_interval_minutes": 15,
  "sla_streak_threshold": 3
}
```
`sla_interval_minutes` and `sla_streak_threshold` must both be positive integers (`>= 1`) — same validation shape as the existing fields (400 if not). **This is the one sharp edge**: the frontend must send both new fields on every save, or the request 400s. There's no partial-update support on this endpoint (never was, even for the original three fields).

## Files to touch

Both pieces of this live in one route file plus its API client — no new files needed.

### 1. `web/src/api/admin.ts` (around line 77)

Current shape:
```ts
export interface ScanSchedule {
  interval_minutes: number
  enabled: boolean
  dns_workers: number
}

export async function fetchScanInterval(): Promise<ScanSchedule> {
  return api.get<ScanSchedule>('/admin/scan-interval')
}

export async function setScanInterval(minutes: number, enabled: boolean, dnsWorkers: number): Promise<void> {
  await api.patch<void>('/admin/scan-interval', { interval_minutes: minutes, enabled, dns_workers: dnsWorkers })
}
```
Extend `ScanSchedule` with `sla_interval_minutes: number` and `sla_streak_threshold: number`. Extend `setScanInterval`'s parameters (two more positional args, matching its existing style — it doesn't take an object today) and include the two new keys in the PATCH body.

### 2. `web/src/routes/admin.index.tsx`

- `ScanSettingsSection` component, starting around **line 940**. It's a plain-`useState` fetched-then-edited form (not react-hook-form): local state initialized from a `value: ScanSchedule` prop, resynced via `useEffect` when `value` changes, a manual field-by-field `dirty` check gating the Save button, and inline `<p className="form-success">`/`<p className="form-error">` messages on save — no toast library, no client-side schema validation. Follow this exact pattern for the two new fields: add `slaMinutes`/`slaStreak` state, add them to the `useEffect` resync, add them to the `dirty` boolean, pass them through to `setScanInterval` in `handleSave`.
- Two existing field widgets to use as reference, both in this same file:
  - `interval_minutes` uses a `Select` (`web/src/components/ui/select.tsx`) over a hardcoded `SCAN_INTERVAL_OPTIONS` list (line ~920: 15/30/60/360/720/1440 minutes).
  - `dns_workers` uses a `Slider` (`web/src/components/ui/slider.tsx`) indexed into a power-of-2 `DNS_WORKER_STEPS` array, with a `nearestDNSWorkerStep` snapping helper and a row of tick labels underneath.
- `enabled` uses `Switch` from `web/src/components/ui/r-switch.tsx` — not relevant here (no new boolean field), noted only so you don't confuse it with the two number fields you're adding.

## Suggested widget choice (not a hard requirement — match this file's visual style, use your judgment)

- **`sla_interval_minutes`**: same "cadence in minutes" concept as `interval_minutes`, so a `Select` with a hardcoded options list is the natural fit — but the option set must start lower than `SCAN_INTERVAL_OPTIONS`' 15-minute floor, since the entire point of this field is that it's usually *shorter* than the normal interval. Something like 1/5/10/15/30/60 minutes. Don't reuse `SCAN_INTERVAL_OPTIONS` itself for this field.
- **`sla_streak_threshold`**: a small integer, realistically 1–10. A stepped `Slider` with tick labels (like `dns_workers`) is overkill for a handful of values — a compact `Select` with options 1 through 10, or a plain bounded number input, both fit better. Pick whichever matches the file's existing visual density more closely; don't invent a third widget pattern without a reason.

Whatever you choose, put both new controls inside the same `ScanSettingsSection` card as the existing three — this isn't a new admin section, it's two more fields on the existing one. No changes to admin-page navigation, tabs, or the `scan-settings` deep-link anchor (`ADMIN_TABS`/`ADMIN_ONLY_TABS` in this file are unaffected).

## Testing / verification

There's no existing frontend test suite covering this component (or its siblings on `/admin`) — matching that, don't introduce a new test framework or file for two fields. Verify per this repo's standard UI-change process (`CLAUDE.md`'s top-level "Doing tasks" section): run `./dev.sh`, log in as admin (`admin`/`aaAA1234` on the local dev DB), open `/admin`, and confirm:
- The two new fields load with the backend's current values on page open.
- Editing either one enables the Save button (dirty check works) and editing back to the original values re-disables it.
- Save round-trips correctly (reload the page and confirm the new values persisted).
- Save with the two new fields at their existing values still works (regression-checks that the PATCH body now always includes all five fields, not just the three it used to).
- A department admin still can't see this section at all (it's gated `is_admin`-only already — no change needed there, just confirm you haven't broken that gate).

Also run `tsc`/the frontend lint/build step this repo normally runs before calling a frontend change done (check `web/package.json` scripts or `web/CLAUDE.md` if unsure which command).
