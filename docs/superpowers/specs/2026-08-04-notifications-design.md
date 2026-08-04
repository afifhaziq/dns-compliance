# Notifications Design

**Date:** 2026-08-04
**Branch:** main (design phase — not yet branched)

## Overview

A notification center (bell icon in the navbar) covering two event types: a domain resurfacing (compliant → violating regression, reusing the existing `ResurfacedDomains` logic), and a domain's due date (`DepartmentURL.DueDate`, see the URL-compliance-case-fields spec) being reached — which triggers a targeted scan of that domain and notifies the owning department with the outcome (still violating, or now compliant).

## Background

This codebase currently has no message-broker dependency — background work is periodic goroutines (`StartScheduler`, `StartWhoisRefresher`, `StartSessionCleanup`) plus an in-memory SSE `Broadcaster` for scan-progress push. Given the monitored-domain scale (~40k) and the need for *precise* per-domain due-date timing (some SLAs are 6h, not "whenever the next periodic sweep happens to run"), the product decision was to add a real queue rather than a plain DB-polling sweep: **Redis + [`hibiken/asynq`](https://github.com/hibiken/asynq)**, the standard Go task-queue library for Redis (retries, scheduling, and a worker processor built in — matches "find a suitable component" better than hand-rolling scheduling/retry logic).

## Decisions

### Infrastructure
- Add a `redis` service to `docker-compose.yml` and `docker-compose.dev.yml`.
- New server flag `--redis-addr` (env `REDIS_ADDR`, default `localhost:6379`).
- The asynq worker runs **embedded inside `cmd/server`** as additional goroutines (an `asynq.Server`+`Mux`, started alongside the existing `StartScheduler`/`StartWhoisRefresher` goroutines in `main.go`) — no third binary. `internal/notify/` (new package) is a reasonable home for the asynq wiring, task handlers, and the `Notification` store methods' calling code.

### Producer 1 — resurfaced domains (periodic)
An asynq **periodic task** (registered via asynq's scheduler, e.g. every 15 minutes — fixed constant, no new admin-configurable setting was requested) that reuses `store.ResurfacedDomains`/`ResurfacedDomainsForDepartment` and, for each affected `(department, url)` not already notified for that specific resurface event (dedup: skip if a `Notification{Type: "resurfaced"}` row already exists for this URL+department created after the domain's last compliant streak — e.g. compare against the existing row's `created_at` vs the resurface's `resurfaced_at`), inserts a new `Notification` row and enqueues nothing further (this producer *is* the notification, not a scan trigger).

### Producer 2 — due-date reached (scheduled, one-shot)
Whenever `PATCH /api/urls/{id}` sets or changes `DepartmentURL.DueDate`:
- Delete any previously-scheduled task for this `(department, url)` pair (deterministic `asynq.TaskID` = `due:<department_id>:<url_id>` — delete is a no-op if none exists or it already ran).
- If the new `DueDate` is non-nil and in the future, enqueue a one-shot task with `asynq.ProcessAt(dueDate)` and that same deterministic `TaskID`.
- Clearing `DueDate` (or removing the URL from the watchlist) just deletes the scheduled task without enqueueing a replacement.

When the task fires:
1. Call the existing `Scanner.Trigger(ctx, "due-date-check", []string{url})` — a blocking, targeted single-URL scan (same mechanism `TriggerScreenshot`/"Scan Selected" already use).
2. Read the resulting latest `ScanResult` rows for that URL (per DNS server) to determine compliance outcome.
3. Insert one `Notification{Type: "due_date_reached", Compliant: <overall bool>, Details: <per-server breakdown>}` row, scoped to the department that owns the watchlist entry.

### Notification table
See the reviewed DBML (`notifications` table: `department_id`, `url_id`, `type` [`resurfaced`|`due_date_reached`], `compliant` [nullable — always meaningful-false for `resurfaced`, the scan outcome for `due_date_reached`], `details` jsonb, `read_at`, `created_at`, indexed on `(department_id, read_at)` and `created_at`).

### Delivery
Keep this simple rather than building a second real-time transport: the navbar bell polls `GET /api/notifications/unread-count` (e.g. every 30s, same lightweight-polling pattern `scan-results.tsx` already uses for scan status) rather than multiplexing onto the existing scan-progress-specific SSE `Broadcaster`, which has its own message shape and consumers. A dropdown/panel lists recent notifications via a paginated `GET /api/notifications`.

## Backend

- `db.Notification` model + `NotificationStore` sub-interface (`ListNotifications`/`ListNotificationsForDepartment`, `UnreadCount`/`UnreadCountForDepartment`, `MarkNotificationRead`) — same admin-global/non-admin-department-scoped pattern as `/api/results`.
- Routes: `GET /api/notifications` (paginated), `GET /api/notifications/unread-count`, `PATCH /api/notifications/{id}/read`.
- `internal/notify/` package: asynq client/server setup, the two task handlers above, and the enqueue/cancel-on-due-date-change hook called from the `PATCH /api/urls/{id}` handler.

## Frontend

- Bell icon + unread badge in `glass-navbar.tsx`, polling `GET /api/notifications/unread-count`.
- Dropdown/panel listing recent notifications (domain, type, timestamp; a compliant/violating badge for `due_date_reached`, a regression badge for `resurfaced`); clicking marks it read and navigates to `/domain/$url`.

## Error Handling

asynq's built-in retry (bounded, exponential backoff by default) covers transient failures in either task handler — no custom retry logic needed. `// ponytail: asynq default retry policy (25 attempts, exponential backoff); tighten if due-date-check floods on a persistently-failing scan target.`

## Testing

- Unit test the resurfaced-dedup logic and the due-date task enqueue/cancel logic (delete-then-maybe-enqueue) against a fake/injected asynq client interface, not a live Redis, where practical.
- If an integration-level test genuinely needs Redis, guard it the same way `internal/dns` guards its real-network tests (skip when unavailable) rather than making it a hard dependency of `go test ./...`.
- Manual `dev.sh` verification: set a near-future due date (a minute or two out) on a watchlist domain, confirm a targeted scan fires at that time and a notification appears with the correct compliant/violating outcome; force a resurfaced-domain scenario (compliant scan, then a violating scan) and confirm a resurfaced notification appears; confirm the unread badge count and mark-as-read both work.

## Out of Scope

- Per-user notification preferences or email/Slack delivery — in-app only for now.
- Admin-configurable sweep interval for the resurfaced-domain producer — fixed constant.
- Real-time push (SSE) for notifications — polling is sufficient for a bell-badge use case.
