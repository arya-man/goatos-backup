# Calendar Vaccination Slice Parallel Handoff

Date: 2026-06-27

Purpose: build the Calendar for the current Preventive Care (PC) Vaccination slice only, with
backend and frontend work running in parallel.

This is not a broad all-domain Calendar rollout. The mock shows a full Calendar
concept, but the current product slice is the vaccination-related projection:

```text
source-backed vaccination rules / due work
  -> dated human action
  -> Calendar event
  -> rich event sidebar
  -> drill-through to Vaccination, Action Center, Workflow, Protocol Adherence,
     proof/rework, and history
```

## Counter-Check

The direction is right: Calendar should be CEO-useful for vaccination and the
clicked event sidebar should expose rich detail.

The only correction is scope:

- Do not duplicate the full Vaccination matrix inside Calendar.
- Do not show all-domain mock Calendar data as live product.
- Do not create Calendar events from label-only vaccine columns, static matrix
  cells, KPI totals, reminder pings, or background system jobs.
- Do enrich the clicked Calendar event sidebar enough that a CEO can understand
  the event without jumping away immediately.

The screen roles stay separate:

```text
Calendar       = what vaccination work is due when
Sidebar        = details and actions for the clicked due event
Vaccination    = full status matrix, cohort detail, drive execution
Action Center  = work queue and operator action state
Workflows      = config -> obligation -> SOP -> proof -> verify -> completion
Adherence      = expected vs actual, gap, severity, owner, next action, evidence
Control Tower  = exception-only leadership rollup
```

## Architecture Non-Negotiables

Calendar must be built like production infrastructure, not as a UI demo.

Calendar vaccination is an operational/process feature. It may not mark kernel
checklist steps `N/A`; it must answer all trigger, audit, outbox, reminder,
notification, escalation, proof/history, scale, and E2E questions in
`context/architecture/operational-kernel.md`.

Use the existing GoatOS architecture:

```text
api -> app -> domain
app -> ports
adapters -> ports
domain imports nothing external
```

Prefer composition and explicit ports over inheritance/override trees. Calendar
will host many event families over time; inheritance makes that brittle. The
scalable shape is:

```text
Calendar app service
  -> CalendarEventSource ports
       vaccination obligation source
       vaccination proof/rework source
       vaccine inventory readiness source
       config/source approval source
  -> ReminderScheduler port
  -> NotificationGateway port
  -> Audit/History port
  -> Postgres read-model adapter
```

Each event source produces the same generic `CalendarEvent` summary contract.
Vaccination can have vaccination-specific detail fields and routes, but the
shared Calendar contract must not be named after vaccination. Adding future
Feed/Breeding/Parks sources should mean adding another source adapter, not
rewriting Calendar or subclassing a base event with overrides.

Frontend has the same rule: generated client + small feature adapter + presentational
components. React components may own open drawer, selected event, hover, loading,
and filter UI state only. They must not own canonical Calendar truth.

## Local Docker And Cloud Parity

Build and prove this locally first, but with the same production seams:

```text
local laptop:
  Docker Postgres
  local API/admin-web
  outbox relay with local/eventbus/inprocess publisher
  obligation sweeper command invoked locally
  local notification adapter / stub
  local proof/storage adapter where needed

Google later:
  Cloud SQL
  Cloud Run API
  Cloud Run Jobs for sweepers
  Cloud Scheduler for cron
  Pub/Sub for outbox delivery
  Cloud Tasks for near-term timers/reminders/retries
  GCS for proof media
  real notification adapters behind NotificationGateway
```

Do not make a local-only Calendar path that has no cloud equivalent. Local can
use adapters/stubs, but the command boundaries, idempotency, outbox, sweepers,
reminder semantics, and API contracts must match the cloud design.

Reference runbooks:

```text
docs/runbooks/local-full-stack-rehearsal.md
docs/runbooks/local-docker-storage.md
docs/runbooks/google-cloud-environments.md
docs/runbooks/deployment.md
docs/runbooks/observability.md
```

Local rehearsal starts with:

```bash
cd /Users/ravi/mesha/goatos
make dev-local
```

Use Docker responsibly:

- permanent local Postgres volume stays `goatos_dev_pg_data`.
- temporary load/benchmark volumes must use `goatos_tmp_`,
  `goatos_test_`, or `goatos_bench_tmp_`.
- do not keep a permanent one-million-goat dataset in local Docker.
- local 1M checks are temporary, summarized, then cleaned up.

## Cron, Sweeper, Reminder, And Notification Model

Calendar is a projection of durable due work. It is not the source of truth and
not a queue.

The runtime chain must follow the protocol engine design:

```text
API write
  -> one transaction: domain mutation + audit + outbox
  -> outbox relay publishes event
  -> idempotent consumer/generator creates obligation_instances
  -> far-future due work stays in Postgres
  -> obligation-sweeper scans due/current window by index
  -> batch/SOP/reminder work is created or dispatched
  -> near-term reminders/retries go through Cloud Tasks in cloud
  -> local adapter simulates the same semantics
  -> Calendar reads Postgres/projection state
```

Required commands/jobs:

```text
outbox-relay
obligation-sweeper
calendar/reminder sweeper or existing notification worker extension
projection refresh/backfill command for Calendar read models
```

Cloud Scheduler should trigger Cloud Run Jobs in cloud. Local Docker should run
the same binaries manually or through a local dev script. Do not hide cron logic
inside frontend timers or Next.js route handlers.

Reminders/nudges:

- A reminder is attached to the same due work item; it is not a second Calendar
  event.
- `Send nudge` is an idempotent backend action. It writes a notification request
  or equivalent durable row plus audit/outbox state keyed to the underlying
  obligation, batch, SOP task, or review task. It does not write to the Calendar
  projection as truth.
- `Snooze` records a durable snooze state/reason keyed to the underlying due-work
  target without mutating vaccination truth. It does not edit
  `obligation_instances.status` and does not hide the source work forever.
- Escalation climbs configured owner/reporting lines through the notification
  gateway.
- Local notification adapter may log/write test rows; cloud adapter can send
  FCM/Slack/webhook/browser notifications later through the same port.
- Current repo scan at handoff time found no dedicated
  `backend/internal` notification, reminder, or snooze module. Backend must add
  or extend durable kernel state and ports/adapters before E2E; the frontend
  must not simulate these actions.
- Reuse the Mesha wiki `goatOS.docx` Event Engine notification shape as prior
  art, not as copy-paste schema: a platform notification log with recipient,
  title/body, channel, triggering entity/task link, notification type, reminder
  number, status, sent/read/failure timestamps, and failure reason. Adapt it to
  the current Goat OS Postgres, tenant, idempotency, audit, and outbox rules.
  FCM, Slack, email, webhook, and alerting stay adapters behind
  `NotificationGateway`.

Idempotency/replay rules for actions:

- Same idempotency key + same semantic payload returns the original action result
  with no duplicate notification, outbox, audit, or reminder rows.
- Same idempotency key + different semantic payload returns an idempotency
  conflict, preferably HTTP `409`, with no new side effects.
- Different key + same target may create a new nudge only if policy allows
  another notification for that actor/window; otherwise return the existing
  throttled/suppressed state.
- Snooze replacement must be explicit. If policy allows replacing a snooze, the
  new row/event must preserve history; never silently overwrite without audit.

## Million-Goat Scale Rules

Calendar must pass the same scale bar as dashboards and obligations:

- tenant scoped, park/date scoped, and owner scoped queries.
- no full-herd scans in API handlers.
- no per-goat SOP task for group vaccination drives.
- far-future due state lives in partitioned Postgres tables, not Cloud Tasks.
- due scans use `(tenant_id, status, due_at, obligation_id)` or equivalent
  bounded indexed access and page through results.
- list APIs are paginated/cursor-based.
- event details load by event id and bounded joins, not by recomputing a week.
- projections/read models are refreshed by workers and read by Calendar.
- hot history/status tables remain partition-aware.
- query-plan validation is required for hot paths.
- all mutating actions are idempotent and replay-safe.
- workers have bounded batch size, bounded goroutines, retry cursors, metrics,
  and DLQ/error visibility.

One-million-goat proof does not mean your laptop permanently stores one million
goats. Local can run a temporary synthetic scale check; staging later keeps the
persistent benchmark dataset.

## Observability And Operations

Add observability for every new API/worker:

```text
calendar_event_list_latency
calendar_event_detail_latency
calendar_event_query_rows
calendar_sweeper_lag
calendar_sweeper_batch_size
calendar_projection_refresh_lag
calendar_nudge_success/error
calendar_snooze_success/error
notification_queue_lag
notification_dlq_count
db_query_latency
```

Logs must include tenant, park/shed/cohort, event id, obligation/batch/SOP ids,
and trace/request ids where available. Do not log secrets. Goat identifiers are
business data and may be logged for diagnostics.

## Audit And Logging For This Slice

Use the operational kernel boundary from
`context/architecture/operational-kernel.md`.

Existing code/schema baseline from the current repo:

- Business audit has a partitioned `audit_log` with tenant, actor, action,
  resource, scope, before/after state, metadata, trace id, and indexed
  domain/module/category/status/result filters.
- `backend/internal/platform/audit` provides a Postgres audit recorder.
- `/operations/audit` is a business product surface over durable audit rows.
- Technical logging is handled through `backend/internal/platform/observability`
  and the Observability ADR; it is not the product audit log.
- `obligation_escalations` exists as a durable escalation table.
- Current repo scan at handoff time found no dedicated backend notification,
  reminder, or snooze module. Calendar notification/reminder/snooze persistence
  and gateway ports are required backend work before E2E.

Calendar-specific requirements:

- `Send nudge` writes durable product audit/history plus notification/outbox
  state keyed to the underlying obligation/batch/SOP/review target.
- `Snooze` writes durable product audit/history plus snooze state keyed to the
  same underlying target.
- Deadline-crossing or escalation creation writes durable product/process state,
  not just a technical log.
- Read-only list/detail polling should not flood `audit_log`. Use technical
  logs/metrics for API latency, errors, authorization failures, and query
  pressure. Add business audit for read access only if an explicit policy later
  requires access review.
- Event history combines domain status events, audit rows, proof/verification
  history, reminder/nudge/snooze records, and escalation records. It is not a
  raw server-log stream.
- Local notification stubs may write test rows/logs, but the same port must map
  to real FCM/Slack/email/webhook/alert adapters later.

## Read First

```text
AGENTS.md
SKILLS.md
context/README.md
context/frontend/current-admin-web-scope.md
docs/decisions/calendar-ownership.md
context/execution/vaccination-process-integrity-backend-handoff.md
context/frontend/vaccination-process-integrity-frontend-handoff.md
docs/protocol-engine/obligation-engine.md
docs/protocol-engine/state-machines.md
docs/preventive-care-vaccination/PRD.md
docs/preventive-care-vaccination/TRD.md
mock/goatos-dashboard-mock.html
```

Relevant mock anchors:

```text
Calendar screen:
  mock/goatos-dashboard-mock.html:918

Calendar event sidebar:
  mock/goatos-dashboard-mock.html:3307

Vaccination screen:
  mock/goatos-dashboard-mock.html:1174

Protocol Adherence vaccination card:
  mock/goatos-dashboard-mock.html:811
```

## Calendar Admission Rule

A Calendar event exists only when all three are true:

```text
due_at or due window exists
owner or executable role exists
human action is required
```

Current active owner pills for the vaccination slice:

| owner_key | Display | Use in this slice |
| --- | --- | --- |
| `all` | All | Filter-only combined view. Do not persist as event owner. |
| `phc` | Preventive Care (PC) | Vaccination dose due, drive, booster, catch-up, defer/waiver review, evidence review, proof verification, rework, Preventive Care (PC) anti-misuse action. |
| `inventory` | Inventory / Stock | Vaccine stock readiness, reservation shortfall, cold-chain, expiry/reorder/GRN when tied to vaccination readiness. |
| `admin_data_ops` | Admin / Data Ops | Source review, config approval, import/replay review, audit follow-up when it is a dated human task for vaccination config/evidence. |

Do not show other owner pills as active in this slice. If the mock keeps the
full pill taxonomy for visual parity, unrelated pills must be disabled,
empty-with-reason, or hidden by the active-slice gate.

## Event Families

Backend must produce these event families when source-backed data exists:

| Family | owner_key | Backend source | Sidebar must show |
| --- | --- | --- | --- |
| Vaccination dose due | `phc` | `obligation_instances` from published source-backed `protocol_versions` and `protocol_rules` under `protocol_definitions(category='vaccination')` | vaccine, dose, due/window, target count, status, owner, source-backed rule |
| Shed/cohort drive | `phc` | `obligation_batches` grouped from due vaccination obligations | park, shed/cohort, target count, assigned executor, SOP task, stock readiness, proof state |
| Manual campaign / catch-up | `phc` | approved campaign rule or Preventive Care approved catch-up batch | campaign reason, target cohort, owner, source/approval |
| Booster due | `phc` | SM-7 from accepted `vaccination_completions.administered_at` | previous dose, administered_at, booster basis, min gap/window |
| Defer / waiver review | `phc` | dated Preventive Care (PC) review task from blocked/deferred obligation | defer reason, impacted goats, reviewer, resume/waiver action |
| HF/historical evidence review | `phc` | procurement/intake evidence plus Preventive Care (PC) review/backfill task | evidence source, accepted-intake link, review due, accept/reject action |
| Proof verification | `phc` | dated verifier task from SOP submission/proof state | shed video, vial video, pending/rejected/accepted, verifier |
| Rework due | `phc` | rejected proof/rework task with due date | rejection reason, required rework, assigned worker/verifier |
| Cold-chain / stock readiness | `inventory` | inventory/cold-chain task or batch readiness task | lot, expiry, FEFO state, reserved/shortfall, cold-chain status |
| Reorder / expiry / GRN | `inventory` | inventory review task tied to vaccination readiness | item, lot, quantity, threshold, due action |
| Preventive Care (PC) stock anti-misuse | `phc` | Preventive Care (PC) discrepancy or spot-audit follow-up with due date | variance, expected use, actual use/movement, Preventive Care (PC) owner |
| Config/source approval | `admin_data_ops` | dated config/source-review workflow | protocol version, source ref, review state, approver |

## Calendar Event Shape

Use one typed read-model response, not ad hoc frontend joining.

The shared summary contract is `CalendarEvent`. The vaccination route can return
`CalendarEventDetail` with vaccination-specific detail blocks. Backend
implementation types can follow repo conventions, but OpenAPI and generated
clients must not make the generic Calendar base vaccination-specific.

Minimum `CalendarEvent` summary fields for hot list/month reads:

```text
event_id
event_type
owner_key
title
subtitle
status
severity
due_at
window_start
window_end
timezone
timezone_source
park_id
park_code
shed_id
shed_name
cohort_id
cohort_name
target_type
target_count
protocol_id
protocol_version_id
rule_id
vaccine_name
dose_code
source_backed
source_label
assignee_label
executor_role
verifier_label
reminder_state
primary_notification_channel
escalation_state
system
cross_cutting
links
```

Timezone source:

- Use the most specific relevant `locations.timezone`: shed/cohort location when
  the event is shed/cohort scoped, otherwise park/current location.
- If a tenant-level default timezone is added later, it can be the next fallback,
  but do not assume it exists in the current schema.
- Final fallback is the current schema default `Asia/Kolkata`; emit/log a
  configuration warning so missing location timezone is visible.

Notification channel source:

- `primary_notification_channel` is the label used by the sidebar metagrid's
  `Channel` cell, for example `push - FCM`, `Slack`, `email`, `webhook`, or
  `local-stub`. It may appear in the summary response because the list and
  drawer preview need one compact channel label.
- `notification_channels` is the ordered list of backend-supported channels for
  that reminder/nudge/escalation policy. It belongs on `CalendarEventDetail`,
  not the hot list/month summary payload.
- The frontend must not invent channel text. If no channel exists yet, render an
  honest `not configured` state.

Sidebar detail fields:

```text
summary:
  park, shed/cohort, vaccine, dose, target count, status, owner, due window

source and rule:
  source-backed flag, source label/ref, protocol version, rule/dose, schedule basis

execution:
  obligation id/count, batch id, SOP task id, assigned executor, work_state

stock:
  item/lot, expiry, FEFO, reserved qty, shortfall, cold-chain state

proof:
  shed video, vial video, dose/lot quantity proof, submission state

verification:
  verifier, accepted/rejected/rework, rejection reason, rework due

links:
  vaccination drive, workflow row, action-center task, adherence row,
  goat/passport or cohort view, history/audit
```

## Calendar Read-Model Status Mapping

Calendar statuses are read-model/UI statuses. They must be derived from
canonical protocol, obligation, batch, SOP, proof, verification, stock, reminder,
and audit state. Do not change `obligation_instances.status` to fit Calendar UI.

Canonical obligation statuses remain:

```text
scheduled
due
in_progress
completed
missed
waived
canceled
superseded
```

Calendar status derivation:

| Calendar status | Derived from canonical state | Notes |
| --- | --- | --- |
| `scheduled` | `obligation_instances.status='scheduled'` and due window is future | Visible only when within requested date range. |
| `due` | `status IN ('scheduled','due')` and due window is current | Projection status; may be computed from `due_at/window_*`. |
| `overdue` | active obligation/batch/task past `window_end` or due threshold | Do not persist as obligation status unless the engine explicitly marks `missed`. |
| `in_progress` | `obligation_instances.status='in_progress'` or `obligation_batches.status='in_progress'` | Drive/SOP has started. |
| `proof_pending` | SOP task/submission/proof requirement is open | Not an obligation status. |
| `verification_pending` | proof/submission is waiting for verifier action | Not an obligation status. |
| `rejected` | verification/review state rejected proof/evidence | Usually leads to a rework due event. |
| `rework_due` | rework task/review due row exists | Not an obligation status. |
| `deferred` | defer/waiver review state or blocker metadata exists | Until a canonical deferred state exists, derive from review/blocker records. |
| `blocked` | stock shortfall, missing owner, missing proof dependency, or other actionable blocker | Owner-missing rows are excluded until owner exists unless a dated owner-fix task exists. |
| `completed` | `obligation_instances.status='completed'` or batch completed | Usually history/detail only unless it creates the next due action. |
| `canceled` | `status IN ('waived','canceled','superseded')` | Excluded from active due list unless a review task is due. |

## API Plan

Backend owns the read-model and actions. Frontend should not reconstruct event
truth from raw tables.

Proposed routes:

```text
GET /calendar/vaccination/events
  query:
    park_id
    shed_id
    owner_key
    status
    date_from (default tenant-today if omitted)
    date_to (default date_from + 30 days if omitted; max inclusive range 45 days)
    cursor
    limit (default 100; max 200)
  returns:
    paginated CalendarEvent summary rows

GET /calendar/vaccination/events/{event_id}
  returns:
    CalendarEventDetail with vaccination detail blocks for sidebar
    notification_channels and notification policy detail

POST /calendar/vaccination/events/{event_id}/nudge
  idempotent action; sends reminder/escalation through existing notification path

POST /calendar/vaccination/events/{event_id}/snooze
  idempotent action; records snooze_until/reason without changing obligation truth

GET /calendar/vaccination/events/{event_id}/history
  returns:
    audit/status/proof/reminder timeline
```

Hard API bounds:

- Reject `date_to - date_from > 45 days` with `400 invalid_date_range`.
- Normalize dates in the tenant timezone before querying.
- Require query-plan coverage for the widest allowed request:
  `owner_key=all`, all parks, 45-day window, max page size.
- Pagination must be cursor/keyset based. Offset pagination is not acceptable for
  the hot list path.

Protected route and permission registration:

- Every Calendar API route must be registered in
  `backend/internal/permissions/routes.go` when implemented.
- Add route-registry tests and a Calendar route smoke test so
  `route_not_registered` cannot ship.
- Prefer generic Calendar permissions plus domain read permissions:
  `calendar.read` for list/detail/history and `calendar.action` for
  nudge/snooze, combined with `vaccination.read` and `obligation.read` for this
  vaccination slice.
- If implementation reuses existing permission constants instead of adding
  `calendar.read` / `calendar.action`, document why the semantics match and add
  role-matrix tests for CEO/admin, Preventive Care (PC), and park-scoped users.
- Actions must also pass tenant/park/shed scope checks from the underlying
  obligation/batch/task target.

Use generated admin-web clients after OpenAPI update.

Do not implement `New event` as arbitrary manual Calendar creation for
vaccination due work. A manual campaign must go through the protocol/config or
Preventive Care approved catch-up path so it creates canonical obligations/batches.

## Postgres Seed And E2E Proof Matrix

Yes: after backend and frontend integration, seed real canonical data in local
Docker Postgres to prove the vaccination Calendar slice. Do not prove this from
frontend-only mock rows.

Add one repeatable local seed command, for example:

```bash
make seed-calendar-vaccination-dev
```

The exact command name can follow repo conventions, but it must be idempotent,
tenant scoped, and safe to rerun against the local dev database. It should reset
only its own fixture namespace/tenant/park labels, not wipe unrelated local data.

Seed canonical tables/projections through the same paths production uses:

```text
protocol_definitions(category='vaccination')
protocol_versions
protocol_rules / rule_dsl source metadata
obligation_instances
obligation_batches
SOP tasks/submissions
vaccination completion/proof/verification/rework state
inventory_items / vaccines / inventory_stock / stock movements
audit/history/reminder/snooze/outbox rows where relevant
Calendar read-model/projection rows if projections are materialized
```

Seeded owner-pill coverage:

| Pill | Must prove |
| --- | --- |
| `all` | Combined vaccination slice only; no unrelated domain events. |
| `phc` | Dose due, drive, booster, campaign/catch-up, defer/waiver review, historical evidence review, proof verification, rework, Preventive Care (PC) stock anti-misuse. |
| `inventory` | Stock readiness, reservation shortfall, cold-chain, expiry/reorder/GRN tied to vaccination readiness. |
| `admin_data_ops` | Config/source approval, import/replay review, audit follow-up tied to vaccination config/evidence. |

Seeded scope coverage:

```text
parks/tabs:
  CBE
  CPT

sheds/cohorts:
  at least one single-shed drive
  at least one multi-shed grouped drive
  at least one adult goat cohort
  at least one kid goat cohort
  at least one sheep/mixed-species row if source rules support it
  at least one quarantine/intake-linked cohort
```

Seeded time/status coverage:

```text
due today
overdue
due soon
future scheduled
in progress
proof pending
verification pending
rejected
rework due
deferred/blocked
snoozed
escalated
completed history only, not a live due event unless it drives the next action
canceled/waived excluded from active due list unless a review task is due
```

Seeded sidebar permutations:

```text
stock card present and ready
stock card present with shortfall
stock card absent because event type does not need stock
proof card absent before SOP submission
proof card pending after submission
proof accepted
proof rejected with rework due
verifier assigned
verifier missing but owner still valid
source-backed published rule
draft/unapproved rule excluded
history timeline with audit + reminder + snooze + proof events
links available to Vaccination, Action Center, Workflow, Protocol Adherence
links honestly disabled/unavailable where no target exists yet
```

Seeded click/action coverage:

```text
Calendar pill filter click
park/tab filter click
date-range filter click
event row/cell click
drawer close
Send nudge
Snooze
Open drive
Open workflow
Open Action Center task
Open Protocol Adherence row
History
retry/error/disabled state for actions that backend rejects or target is absent
```

Seed explicit negative rows and assert they do not appear in Calendar:

```text
label-only vaccine matrix cell
draft protocol version
unsourced/unapproved rule
row without due_at/window
row without owner/executable role
pure reminder ping
background sweeper/system job
already completed historical dose with no next action
non-vaccination feed/milk/sales/MIS event
```

E2E proof flow:

```text
1. start local Docker stack
2. run Calendar vaccination seed command
3. run outbox/projection/sweeper command if required
4. start API/admin-web
5. visit Calendar
6. assert All / Preventive Care (PC) / Inventory/Admin Data Ops counts and rows
7. assert CBE/CPT and date-range filtering
8. click every seeded event type
9. assert drawer sections and bottom actions for each event
10. execute nudge/snooze/history actions and verify backend state
11. assert negative rows are not visible
12. capture desktop and narrow screenshots
```

The seed dataset is not the million-goat benchmark. It is a compact functional
fixture that covers permutations. A separate temporary synthetic load check can
prove 1M-goat query/worker shape without keeping that data in the permanent local
Docker volume.

## Backend Work Plan

1. Find existing vaccination obligation, batch, SOP, proof, verification,
   inventory, process-integrity, audit, outbox, and escalation modules. Extend
   them; do not fork a Calendar engine. Current scan found no dedicated
   notification/reminder/snooze module, so add or extend shared kernel ports and
   durable state instead of faking those actions in Calendar.
2. Add a Calendar vaccination read service that derives events from canonical
   Postgres state and projection tables.
3. Implement the event admission rule and owner-key mapping from
   `docs/decisions/calendar-ownership.md`.
4. Add OpenAPI schemas and handlers for event list, detail, nudge, snooze, and
   history.
5. Add bounded indexed queries for due-window scans:
   `(tenant_id, status, due_at, obligation_id)` and batch/scope indexes already
   expected by the protocol engine. No full-table scans. Enforce the 45-day max
   range and query-plan test the widest allowed All + all-parks request.
6. Add protected route registrations, permission constants or documented reused
   permissions, role-matrix tests, and route smoke tests for list, detail,
   history, nudge, and snooze.
7. Add idempotency to nudge/snooze actions with semantic request fingerprints
   and durable notification/snooze/audit/outbox persistence keyed to the
   underlying obligation/batch/task/review target.
8. Add or extend durable reminder/notification/snooze persistence and
   notification gateway ports if they do not already exist. Do not use technical
   logs as action state. Use the Mesha wiki `goatOS.docx` Event Engine
   notification shape as prior art, but adapt it to current tenant-scoped
   Postgres, idempotency, audit, outbox, and `NotificationGateway` boundaries.
9. Add product audit rows/history tests for nudge, snooze, deadline crossing,
   escalation creation, proof/verification links, and history output. Do not
   audit every read-only polling request unless policy requires it.
10. Add the canonical local Docker Postgres seed command and tests for:
   - ET K1 primary drive
   - ET booster due from accepted completion
   - proof verification due
   - proof rework due
   - vaccine stock readiness/shortfall
   - cold-chain check
   - config/source approval due
   - all seed and exclusion cases in `Postgres Seed And E2E Proof Matrix`
11. Add tests for admission/exclusion:
   - label-only vaccine has no event
   - draft protocol version has no event
   - reminder ping is not a second event
   - system sweeper is excluded
   - owner-missing gap is excluded until owner exists
   - stock readiness maps to `inventory`
   - source/config approval maps to `admin_data_ops`
   - Preventive Care (PC) proof/rework/drive maps to `phc`
12. Add tests for Calendar read-model status mapping so UI statuses never force
    new invalid canonical `obligation_instances.status` values.
13. Wire local Docker rehearsal:
   - API reads the local Docker Postgres state.
   - outbox relay can run locally.
   - obligation sweeper can run locally.
   - Calendar projection/reminder path can run locally with local adapters.
14. Add query-plan validation or equivalent explain checks for hot Calendar list
    and due-window queries.
15. Add worker metrics/logging and bounded batch controls.
16. Add a backend/API E2E or integration proof that runs against the seeded
    Postgres data and validates event list, event detail, nudge, snooze, and
    history behavior.
17. Regenerate clients and run focused backend tests plus contract generation.

## Frontend Work Plan

Claude should own frontend implementation and visual QA.

1. Use the mock Calendar screen and drawer as the visual source of truth:
   layout, density, typography, icon sizes, chips, hover states, drawer
   placement, backdrop blur, bottom actions, spacing, and responsive behavior.
2. Keep the top-level Calendar screen and active-slice pills:
   `All`, `Preventive Care (PC)`, `Inventory / Stock`, `Admin / Data Ops`. Unrelated pills must
   not appear active for this slice.
3. Calendar list/month cells show concise due-work rows:
   time, title, status, owner color, reminder badge, and severe/overdue state.
4. Clicking an event opens the enriched right sidebar. The sidebar must include:
   - event header with type, vaccine/protocol, status/severity
   - When / Reminder / Channel / Escalates grid matching mock treatment, using
     `primary_notification_channel` from the API summary/detail and
     `notification_channels` from the detail API when a full channel list is
     shown
   - park/shed/cohort/target count
   - source-backed rule card
   - execution/SOP/proof/verification card
   - stock readiness card when present
   - links to Vaccination, Workflow, Action Center, Adherence, History
5. Bottom actions:
   - primary `Send nudge`
   - secondary `Snooze`
   - secondary `Open drive` or `Open workflow` depending event type
   - secondary `History`
   - no broad arbitrary `Edit` for generated due events
6. Empty, loading, error, and unauthorized states must match mock style and be
   honest: no fake rows.
7. Preserve top-bar park/date scope. Do not duplicate park/date chips inside
   event sections.
8. Use generated backend client only. No hardcoded legacy rows except clearly
   marked mock/dev fixtures when backend returns fixture data through the API.
9. Make actions call backend endpoints only:
   - `Send nudge`
   - `Snooze`
   - `History`
   - `Open drive`
   - `Open workflow`
   No localStorage, fake browser timers, or UI-only mutation state.
10. Frontend must preserve scalability by requesting list pages and details
    separately; do not fetch a giant all-events payload and filter in React.
11. Run frontend checks, mock fidelity check, and rendered visual QA across
   desktop and narrow widths.
12. After backend integration, run against the seeded Postgres dataset and click
   through every active pill, CBE/CPT/date filter, event type, drawer section,
   and bottom action listed in `Postgres Seed And E2E Proof Matrix`.

## Frontend Sidebar Detail Layout

Keep the mock drawer shell:

```text
CALENDAR EVENT
<title>
close x

metagrid:
  When
  Reminder
  Channel
  Escalates

detail cards:
  Scope
  Source-backed rule
  Execution
  Stock readiness
  Proof and verification

bottom action bar:
  Send nudge
  Snooze
  Open drive / Open workflow
  History
```

Do not render the full matrix here. Instead, show just the selected event's
executive detail and deep links.

## Parallel Contract Between Agents

Backend can proceed first with OpenAPI and generated client shape. Frontend can
start from mock fixtures that match the proposed response shape, then swap to
generated clients once backend regenerates.

Backend must not edit frontend UI files except generated clients/contracts.
Frontend must not invent backend truth or bypass generated clients.

Backend and frontend must both preserve local-to-cloud parity. Local adapters are
allowed; local-only behavior is not.

Shared event statuses:

```text
scheduled
due
overdue
in_progress
proof_pending
verification_pending
rejected
rework_due
deferred
blocked
completed
canceled
```

Shared event types:

```text
vaccination_dose_due
vaccination_drive
vaccination_campaign
vaccination_booster_due
vaccination_defer_review
vaccination_evidence_review
vaccination_proof_verification
vaccination_rework_due
vaccine_stock_readiness
vaccine_cold_chain_check
vaccine_reorder_expiry_grn
phc_stock_anti_misuse
vaccination_config_source_approval
```

## Acceptance Criteria

- CEO can open Calendar, filter to Preventive Care (PC) / Inventory/Admin Data Ops or All, and see
  only vaccination-related due work for the current slice.
- Every visible event has a date/window, owner, and required human action.
- Clicking any vaccination event opens a rich drawer with scope, vaccine/dose,
  target count, status, owner, source/config basis, stock, proof, verification,
  and links.
- The drawer bottom actions match the mock style and perform real backend
  actions or honest disabled states.
- The drawer `Channel` field is backed by API channel fields, not frontend
  hardcoding.
- Calendar statuses are read-model statuses mapped from canonical engine state;
  no new invalid `obligation_instances.status` values are introduced for UI.
- Calendar has no static matrix cells, no label-only vaccine events, no draft
  protocol events, no duplicate reminder events, and no pure system jobs.
- `/vaccination` remains the full matrix/detail screen; Calendar links into it.
- `/action-center`, `/workflows`, `/protocol-adherence`, and Control Tower all
  read the same event/workflow truth; no duplicated frontend state machine.
- Local Docker rehearsal proves API + outbox relay + sweeper + Calendar event
  projection + nudge/snooze path with the same binaries/adapters intended for
  cloud.
- Product audit/history and technical logs/metrics are both present but kept
  separate: `/operations/audit` reads durable business audit rows, while
  observability logs/metrics/traces diagnose runtime behavior.
- A repeatable local Postgres seed proves every active vaccination owner pill,
  CBE/CPT/date filtering, every sidebar permutation, every bottom action, and
  every negative/excluded row in the proof matrix.
- Scale review passes: indexed/paginated list query, bounded detail query,
  idempotent actions, worker batch bounds, observability, and no full-herd scan.
- Desktop and narrow screenshots match the mock's UI/UX standard.

## Backend Prompt

```text
In /Users/ravi/mesha/goatos, implement the backend/API half of
context/execution/calendar-vaccination-slice-parallel-handoff.md.

Read AGENTS.md, SKILLS.md, context/architecture/operational-kernel.md,
docs/decisions/calendar-ownership.md, and the Calendar handoff. Build only the
backend/contracts for the Preventive Care (PC) Vaccination Calendar slice.

Implement the handoff exactly: generic CalendarEvent contract, protected routes,
canonical Postgres-derived list/detail/history, idempotent nudge/snooze with
durable audit/notification/snooze state, local Docker seed/E2E proof, generated
clients, focused tests, query-plan checks, and a concise backend handoff. Do not
edit frontend UI except generated clients/contracts.
```

## Frontend Prompt

```text
In /Users/ravi/mesha/goatos, implement the frontend/UI half of
context/execution/calendar-vaccination-slice-parallel-handoff.md.

Read AGENTS.md, SKILLS.md, context/frontend/current-admin-web-scope.md,
context/architecture/operational-kernel.md, docs/decisions/calendar-ownership.md,
and the Calendar handoff. Build only the /calendar UI for the Preventive Care (PC) Vaccination
slice, matching mock/goatos-dashboard-mock.html precisely.

Use generated clients or exact contract fixtures until backend lands. Implement
active pills, bounded list/detail loading, rich right drawer, metagrid including
Channel from API fields, real/disabled bottom actions, loading/error/empty
states, seeded E2E click-through, mock-fidelity checks, and desktop/narrow visual
QA. Do not invent backend truth, timers, localStorage state, or all-domain rows.
```
