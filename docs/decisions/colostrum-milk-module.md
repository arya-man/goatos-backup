# Colostrum page in the Milk module (proposed 2026-08-06)

Status: **proposed — awaiting maintainer sign-off** · Owner: tasks + milk (counts) ·
Branch: `colostrum-development`
Parent contract: `docs/decisions/birth-death-workflows.md` (the colostrum tasks this
page renders already exist there; nothing in that decision changes).

## What this is

A third page in the **Milk** module — `Colostrum` — that shows the colostrum feeds
due on ONE business date, so the person doing milk work sees newborn feeds without
opening Birth.

It is a **LENS over work that already exists**, not a new workflow. Every row it
renders is a `workflow_actions` row created by `TemplateBirthKidAt`
(`backend/internal/tasks/domain/templates.go`): the immediate `1st Colostrum` plus
the birth-time-derived series at 07:00 · 11:00 · 15:00 · 18:30 · 22:00 IST.

**One completion, two entry points.** Completing a feed from Colostrum writes the
same `workflow_actions` row, through the same
`POST /app/workflows/{workflow_id}/actions/{action_id}/complete`, as completing it
from Birth. There is no second table, no mirrored state, and no second completion
record. Doing it in Birth marks it done in Colostrum and vice versa.

## Maintainer decisions (2026-08-06)

1. **Card grain = one card per kid**, same visual shape as the Birth card
   (`displayId`, role chip, meta line, `done/total`, progress bar, next-action line,
   due chip).
2. **The card shows THAT DAY's colostrum only.** A kid's `done/total` is counted over
   the feeds due **on the selected date**, never the whole 6–11 feed series. Tomorrow's
   progress is seen tomorrow. A kid born on 5 Aug therefore appears on 5 Aug with its
   birth-day feeds and again on 6 Aug with that day's five feeds, each date carrying
   its own independent counter.
3. **Detail = colostrum actions only.** Tapping a card opens only that kid's colostrum
   feeds for that date. Iodine dipping, weight, kid-standing and Tag the kid stay in
   Birth.
4. **Date range = past + today**, identical to Birth. The picker is capped at today;
   ‹ › step one day; a "Today" chip returns from a past day.
5. **No ＋ button.** Colostrum is never the place a birth is recorded.
6. **Nav = third leaf of the Milk module**, `counts.write`, alongside Milk Prep and
   Milk Feeding. Milk Prep remains the module landing page.

## Screen contract (Android)

Reuses `WorkflowListScreen` verbatim with a third module value — same header, same
bell, same date bar, same chips, same card renderer.

```
[‹ Colostrum]                    [🔔 3] [⟳]        ← bell + sync, NO ＋
 Synced 2 min ago
 [‹]  [📅 Today · 6 Aug]  [›]
 (All 14) (Overdue 2) (Due 9) (Completed 3)        ← backend-owned counts
 ── Overdue ──────────────────────────────
 │ CPT-10234  kid            2/5            ← 2 of TODAY's 5 feeds
 │ ████░░░░░░  Next: 4th Colostrum   2h late
 │ Castro 2 · CPT
 ── Due today ────────────────────────────
 │ CPT-10235  kid            0/5
 │ ░░░░░░░░░░  Next: 6th Colostrum     15:00
```

- **Bell** — the same bounded previous-day attention summary Birth uses: at most the
  five most recent past business dates that still hold an incomplete colostrum feed,
  with a count each. Tapping a date selects it and the Overdue filter.
- **Chips** — `All · Overdue · Due · Completed`, mutually exclusive, counted over the
  whole selected date (not the loaded page). `Awaiting video` is deliberately absent:
  verification is enqueued at whole-workflow grain when every kid task is done, so a
  single day's feeds can never sit in that bucket.
- **Group headers** — Overdue / Due today / Completed, derived client-side from each
  card's own backend fields (same rule as Birth: presentation only, never re-derived
  business truth).

## Backend read

`GET /app/workflows?module=colostrum&date=YYYY-MM-DD&filter=&cursor=&page_size≤20`

Same route, same response DTO, same keyset shape as birth/death, so the Android card
model, Room entity, Paging source and renderer are reused unchanged. `module=colostrum`
is a **LENS keyword**, not a `workflow_instances.module` value — the handler branches to
the colostrum query. This is stated in the handler comment so a later author does not
look for a `'colostrum'` row in `workflow_instances`.

Why the existing birth query cannot serve it, in one line each:

| Birth list column | Why it is wrong here |
| --- | --- |
| `event_date` filter | A kid born 5 Aug has feeds on 6 Aug; filtering on `event_date` would hide it on the 6th |
| `actions_total` / `actions_done` | Counts ALL operator actions (iodine, weight, standing, tag) — wrong denominator for a colostrum-only, single-day card |
| `next_action_title` / `next_due_at` | The next action of any section; can be "Tag the kid" |

**Query shape** — canonical indexed read, aggregated at the card grain:

```sql
SELECT wa.workflow_id,
       count(*)                                                       AS total,
       count(*) FILTER (WHERE wa.status = 'completed')                 AS done,
       min(wa.due_at) FILTER (WHERE wa.status IN ('pending','rework')) AS next_due_at
FROM workflow_actions wa
JOIN workflow_instances wi USING (workflow_id)
WHERE wi.tenant_id = $1::uuid
  AND wi.template_key = 'birth_kid'
  AND (wa.section = 'colostrum_session' OR wa.action_key = 'first_colostrum')
  AND wa.due_at >= $2::timestamptz AND wa.due_at < $3::timestamptz   -- IST business day
GROUP BY wa.workflow_id
```

- The date filter is a **half-open range on `due_at`**, with both bounds computed in Go
  from `biztime.BusinessDayStart` — never `(due_at AT TIME ZONE 'Asia/Kolkata')::date`,
  which is STABLE, non-indexable, and non-SARGable.
- Every action carries a `due_at`: unscheduled ones resolve to the birth moment
  (`Schedule.DueAt`, zero offset), so `1st Colostrum` lands on the birth date and needs
  no special case.
- New partial index (forward migration):
  `workflow_actions (due_at, workflow_id) WHERE section = 'colostrum_session' OR action_key = 'first_colostrum'`.
- `scale-guard`: this is an aggregate on a read path. It is bounded by construction —
  one IST day of colostrum actions is `(kids born in a 2-day window) × ≤11`, i.e. low
  thousands at the 50k-animal envelope — and carries an explicit
  `// scale-guard:ignore:` naming that bound, per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. If a measurement at the
  envelope's upper bound says otherwise, the fallback is a
  `(tenant_id, workflow_id, colostrum_date)` projection maintained in the same
  transaction as every action write — not a bigger timeout.

**projection-review** (required grain proof):
- producer unique key: `workflow_actions (workflow_id, action_key)`
- consumer card key: `(workflow_id, colostrum_business_date)` — one row per kid per date
- join multiplicity: `workflow_instances` 1:1 on `workflow_id` (PK); goat tag / park /
  shed enrichment each 1:at-most-1
- chips numerator and denominator range over the identical key set (same tenant, same
  day range, same colostrum action predicate, grouped to `workflow_id`); no fan-out
- pagination: chips aggregate the whole filter; cards keyset on
  `(next_due_at ASC NULLS LAST, workflow_id ASC)`

`GET /app/workflows/{workflow_id}?lens=colostrum&date=YYYY-MM-DD` returns the same
detail DTO with the action list filtered to that date's colostrum feeds. The **backend**
filters, so the client never decides which tasks belong to colostrum (backend-owns-the-
contract rule).

## Write path — unchanged

`POST .../actions/{action_id}/answer` and `.../complete`, with `Idempotency-Key`, exactly
as Birth uses them. Every existing gate is inherited, not re-implemented:

- `409 action_not_yet_due` — a session cannot be fed before its scheduled time.
- `409 action_out_of_sequence` — feeds unlock one at a time within their section.
- `422 proof_required` — every colostrum feed requires one live-camera video.

**One boundary to render honestly:** `1st Colostrum` is `section=main`, seq 5, so it sits
behind four earlier birth steps (kid clean, iodine, front teeth, suck reflex). A milk
operator who opens Colostrum before those are done must see a **backend-owned disabled
reason** on the row ("Earlier birth steps are pending"), not a raw 409 after tapping. The
detail response carries the reason string; the client renders it verbatim.

## Files

Backend
- `backend/internal/tasks/adapters/http/handler.go` — accept `module=colostrum`, branch
- `backend/internal/tasks/adapters/postgres/repository.go` — `ListColostrumDay` + chips + previous-date summary
- `backend/internal/tasks/domain/read.go` — colostrum lens query/DTO types
- `backend/internal/tasks/app/service.go` — detail filtering + disabled reason
- `backend/migrations/postgres/0000NN_colostrum_day_index.sql` — partial index (forward-only)
- `backend/internal/workforce/app/bootstrap_copy.go` — Milk module third leaf + en/hi/kn/te labels
- `contracts/openapi/app-api.yaml` + regenerated `packages/api-client`

Android
- `app/.../ui/AppNavHost.kt` — `/counts/colostrum` (L0) and `/counts/colostrum/workflows/{id}` (L1 hosted, Up/Back, no root chrome)
- `feature/feature-counts/.../WorkflowListScreen.kt` — `WorkflowModuleUi.COLOSTRUM` + `showAdd` flag
- `feature/feature-counts/.../WorkflowDetailScreen.kt` — reused; renders the filtered list
- `app/.../viewmodel/` — colostrum list/detail ViewModels (`RefreshOnResume`, `SyncIconButton`, analytics constants, Crashlytics non-fatals)
- `core/core-data/.../WorkflowsRepository.kt` — reuse the existing card/chips/detail Room caches keyed `colostrum|<date>`; **no new entity, so no Room schema bump**
- strings in `values/`, `values-hi/`, `values-kn/`, `values-te/`

## Proof

- Domain: day-membership from the birth moment (born 22:50 → zero birth-day slots; born 06:00 → all five; the exact 15-minute cutoff, equality excluded).
- Postgres integration: two kids born on different days; date D returns only D's feeds; per-day counters independent; keyset paging across a page boundary.
- **Parity (the headline test):** complete a feed through the Birth lens → the Colostrum list for that date shows it done, and the reverse. One row, two entry points.
- Gate tests: not-yet-due, out-of-sequence, and the `1st Colostrum`-blocked disabled reason.
- Android: ViewModel tests, Paparazzi screenshots (light/dark), `TopLevelChromeTest` asserting the Milk bar carries Colostrum, the bell renders, and **no ＋**.
- Cross-surface: the same fixture makes Birth and Colostrum agree on that day's counts.

Guards to run: `mobile-guard`, `nav-composition-guard`, `aggregate-projection-guard`,
`scale-guard`, `operational-location-guard` (cards carry `partition_label` — "Castro 2",
never "Castro"), `telemetry-guard`, `ui-vaccine-labels-guard`,
`leadership-assistant-coverage-guard` (a new read surface needs a coverage row or a
recorded exclusion), then `make ci-local` on the exact SHA.

## Explicitly out of scope

- No new domain event. Colostrum produces and consumes nothing new; the existing birth
  workflow events already carry every state change.
- No change to the colostrum schedule, its 15-minute pre-notification cutoff, the
  sequence rule, or the verification grain.
- No colostrum config/ownership screen (still out of scope from the birth decision).
- No admin-web surface in this slice.
