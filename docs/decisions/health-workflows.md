# Health workflows

Status: accepted implementation contract (2026-07-30)

## Product contract

The mobile display name is **Health**. Its internal module key is `aas_health`; this key is never shown to users. The module has two peer work-list pages, Adults and Kids, using the Birth/Death workflow shell: selected-day calendar navigation, backend-owned filters, compact status pills, sectioned Slack-style action cards, sync state, and a prominent top-right `+`. The `+` opens a hosted report-sick-goat form for that page's fixed age band. Goat lookup is bounded and live, disease choices come from published protocols, and submit queues the canonical case write through Android's durable outbox.

The source SOP is the Google Sheet `Health DB`, tabs `Adults SOP` and `Kids SOP`, plus the supplied Apps Script behavior. The checked-in normalized snapshot is `context/source-findings/health-sop-v1.json`. A published protocol version is immutable. A new course snapshots its published steps, so later edits never rewrite administered-medicine history.

## Three-day rule

Treatment duration is configured per disease and age band. When a source protocol has dated rows, its duration is the greatest Day value. When a disease has no configured duration, the default is **3 days**. A course creates only the day/session work items present in the protocol; a protocol with no detailed steps receives one unscheduled follow-up action for each configured day.

## Grains and state

- Canonical write owner: Health.
- Case grain: one goat × diagnosed disease episode × start date.
- Work-item grain: one case × business date × session.
- Step grain: one immutable protocol-step snapshot inside a work item.
- Administration grain: one completed medication step; unique by work item and step.
- Time grain: Asia/Kolkata business date.
- Shared-summary grain: whole filtered set of work items. Status buckets are disjoint and must never be added as overlapping totals.
- Stable work selector: `health_session_id`, never disease key alone.
- Paging: keyset `(due_at, health_session_id)`, maximum 20 rows.

Case states are `active`, `recovered`, `continued`, `referred`, `held_death_review`, `closed_dead`, and `canceled`. Session states are `scheduled`, `due`, `in_progress`, `completed`, `rework`, `held_death_review`, and `canceled_death`.

## Critical-action boundary

Slack rows that say cull, quarantine, isolate, or move are preserved verbatim as source evidence, but they are classified as `critical_action`. Health renders a fail-closed “Raise critical action” handoff and never mutates lifecycle, location, or quarantine state itself. The relevant policy-pack workflow remains the only writer. This follows `docs/features/critical-animal-action-guardrails.md`.

## Death semantics

- `counts.death.reported`: atomically hold every incomplete Health session for the goat and remember its prior state.
- `counts.death.rejected`: restore held work. Past work becomes due; future work returns to scheduled.
- `goat.exited` with `exit_reason=died`: close every open case as `closed_dead` and cancel every incomplete session as `canceled_death`.
- Completed sessions and medicine-administration history remain immutable.
- Default mobile work lists exclude `canceled_death`, so an approved-dead goat never appears on tomorrow's action list. The canceled bucket remains an audit/read-model diagnostic and is excluded from the actionable `total`.

Handlers are idempotent and are registered on every durable event bus.

## Commands and reads

- `POST /app/health/cases`: open a diagnosed course from one published disease protocol.
- `GET /app/health/work-items`: bounded day work list, marker counts, disjoint summary, and filter options. Disease options are the published protocol catalog for the requested age band, including protocols with no existing case; park/shed options remain case-derived.
- `GET /app/health/work-items/{health_session_id}`: one work item with its immutable ordered steps.
- `POST /app/health/work-items/{health_session_id}/complete`: idempotently complete the work item and record medicine administrations.

Every mutation writes canonical state, audit, and transactional outbox in one transaction. Android stores reads in Room, renders Room as the single source of truth, pages at 20 rows, and drains writes with stable idempotency keys.

## Source-to-execution rule

The normalized source file is data, not an executable bypass. Publishing imports protocols only after validation: unique disease/age keys, positive day numbers, known sessions/routes, at least one action, and no direct critical mutation. Unknown fields fail import. The source snapshot intentionally contains no Slack tokens, channel IDs, webhooks, or user IDs.

## Shared surfaces and leadership

Health owns its canonical operational read contract. Calendar and leadership integrations may consume its bounded read model; they may not recompute status from mobile rows. The first mobile release exposes the Health work-item summary to leadership navigation but does not add a CEO assistant query class until a governed Health KPI is approved; this is an explicit temporary exclusion, not implicit coverage.
