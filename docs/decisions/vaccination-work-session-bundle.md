# Vaccination Work-Session Bundles

Status: Proposed, requires maintainer sign-off before runtime code

Date: 2026-07-11

## Required Sign-Off

Maintainer decision: Pending

Runtime implementation is blocked until this section records the accepted
outcome, signer, and date.

## Decision Reconciliation

This ADR exists because the vaccination bundle model conflicts with a committed
engine decision:

- `docs/protocol-engine/obligation-engine.md` says the batch is the drive/work
  unit and says not to create a parallel `vaccination_drives` table.
- `context/execution/sop-vaccination-backend-handoff.md` repeats that the batch
  is the drive/work unit and says not to create a parallel
  `vaccination_drives` table.

Those statements were correct for a single-protocol batch. They are incomplete
for real vaccination execution, where one shed visit can administer multiple
vaccines across multiple per-vaccine protocol versions, with one operator task,
one submission, and one video proof.

No code should be written for this model until the maintainer explicitly accepts
one of these outcomes:

1. Retire the older "one batch is the work unit" wording for vaccination combo
   visits and adopt the work-session grouping below.
2. Reject the work-session grouping and keep one task/submission per batch.
3. Define a narrower scope where both rules apply.

This ADR intentionally does not authorize a vaccine-specific drive table. It
keeps the prior "no parallel `vaccination_drives` table" rule.

## Context

The repository already has a partial combo model:

- `obligation_batches.session` carries deterministic session labels such as
  `combo:FMD+HS`.
- `AlignComboDrives` groups planned combo batches by
  `scope_type + scope_id + session` and aligns planned dates.
- Per-vaccine obligations are already collision-safe because
  `obligation_instances` keeps `rule_id` in its duplicate guard.

The missing primitive is not a new vaccine grain. The missing primitive is a
first-class operator execution group above the existing per-vaccine batches.

The same primitive is also needed for daily capacity. A planner that correctly
groups FMD+HS into one shed visit can still create an impossible visit if it
packs 300 or 500 administrations into one day. The work-session layer is where
combo grouping and capacity splitting meet.

## Combined Problem Statement

Two current assumptions fail together:

1. One vaccine batch is treated as one operator drive.
2. The planner optimizes for maximum eligible output without an operator/day
   capacity ceiling.

Real execution needs this shape:

```text
one due population
  -> compatible vaccine bundles
  -> daily capacity buckets
  -> one or more work sessions
  -> per-vaccine batches and obligations
  -> matrix submission cells
```

Example: 300 FMD+HS administrations are due and the daily capacity is 100
administrations. The planner should create three work sessions across safe days,
not one impossible session:

```text
day 1 -> 100 administration cells
day 2 -> 100 administration cells
day 3 -> 100 administration cells
```

## Decision

Promote the existing batch `session` concept into a first-class vaccination
work-session grouping.

The model is:

```text
work session
  -> many obligation_batches
  -> many per-vaccine obligation_instances
  -> one SOP task
  -> one SOP submission and proof video
  -> many animal x obligation decision cells
```

The work-session grouping is generic. It must not be implemented as a
`vaccination_drives` table.

The minimal grouping key is:

```text
tenant_id
scope_type
scope_id
session_id
planned_date or window_start/window_end
```

`session_id` is the promoted version of the existing `session` label. The date
or window is required so recurring sessions such as `combo:FMD+HS` do not merge
across different drive cycles. If a persistent row is required for APIs,
assignment, proof, or offline sync, it should be a generic work-session head for
this key, not a vaccination-specific drive table.

Capacity-managed work sessions must resolve to one concrete `business_date`
before they are finalized. `window_start/window_end` is allowed only for
pre-assignment candidates or non-capacity-managed planning; once the daily cap is
applied, the work session belongs to a dated capacity bucket.

Capacity splitting may create multiple work sessions for the same
`scope_type + scope_id + session_id` across different planned dates. Those
sessions are siblings for the same medical/bundle cohort, not one merged visit.
Each dated sibling owns its own SOP task, submission, and proof video. A medical
cohort split across three days therefore has three execution proof packages, not
one video for the whole multi-day cohort.

## Grain Rules

Keep these grains separate:

- Per-vaccine tracking: one `obligation_instance` per animal, vaccine/rule, dose,
  due date, and sequence. This remains the source for coverage, Passport,
  Protocol Adherence, Control Tower rollups, and vaccine-specific analytics.
- Per-batch stock/protocol grain: `obligation_batches` remain tied to one
  protocol version and the existing stock/reservation lifecycle.
- Per-work-session execution: one shed or park visit can group many batches
  under one task/submission/video.
- Per-cell decision: the submitted matrix records one cell per
  `animal_id + obligation_id` decision. Only terminal accepted cells materialize
  active vaccination completion truth.

## Daily Capacity Contract

The daily cap counts vaccination administration cells, not just animals,
batches, or sessions.

```text
one animal + FMD = 1 administration cell
one animal + FMD + HS = 2 administration cells
```

The cap is an operational capacity rule: "do not plan more than N vaccination
administrations in a day for the configured capacity scope." The default scope
should support the user's real constraint: across sheds and vaccine types for a
team/day. Implementations may later narrow or widen the scope by tenant, park,
team, or operator, but the algorithm must not assume capacity is per shed or per
vaccine. Route scope is reserved for future route planning.

Minimum policy fields:

```text
max_vaccination_administrations_per_day
capacity_scope: tenant | park | team | operator
capacity_scope_id
max_capacity_buffer_days
overflow_policy: split_until_buffer_then_breach
```

`max_capacity_buffer_days` is the capacity-facing name for the existing
one-time drive planner hold budget, currently expressed as
`max_batching_hold_days` / `max_batching_hold_count`. It must reuse that budget,
not create a second postponement path. A cell that has already consumed its
allowed batching hold cannot get another independent capacity hold.

Default capacity scope is `team` when a team/doctor assignment exists, otherwise
`park`. Do not default to tenant-wide capacity unless the rule explicitly says
one team covers the whole tenant on that business date.

The resolved capacity bucket key is:

```text
tenant_id + business_date + capacity_scope + capacity_scope_id
```

`business_date` is the Goat OS business calendar date in `Asia/Kolkata`, not a
UTC date.

`capacity_scope_id` resolution:

```text
tenant -> tenant_id
park -> park_id resolved from the work-session scope; shed/cohort scopes must
        walk the locations ancestor tree to the owning park before bucketing
team -> assigned vaccination team / doctor team id
operator -> assigned primary operator / doctor id
```

`route` is reserved for a future route-planning primitive. V1 publish/preview
must not accept `route` as a capacity scope until a real planned-route identity
source exists.

If the selected scope cannot resolve an id, publish/preview must fail closed or
fall back only to a configured explicit default. Silent fallback to tenant scope
is not allowed.

Before a packet is assigned to a business date, the planner can use only the
capacity scope key:

```text
tenant_id + capacity_scope + capacity_scope_id
```

This undated key is for deterministic ordering only. The dated
`capacity_bucket_key` is created after packing assigns a `business_date`.

Capacity is strong but not medically absolute. Medical safety windows,
cross-vaccine spacing, per-animal shot caps, and the maximum buffer win over the
daily cap.

Effective deadline per administration cell is:

```text
if batching_hold_count >= max_batching_hold_count:
  planning_start_date
else:
  min(cell_latest_safe_date, planning_start_date + max_capacity_buffer_days)
```

When the hold-count gate is already exhausted, the cell can no longer be delayed
for capacity smoothing even if the day-based buffer has calendar time left. It
must be placed in the current planning run if medically eligible, or marked with
a capacity/medical-window exception.

For a same-session animal packet, the packet's effective deadline is the
earliest effective deadline across its cells. Its eligible dates are the
intersection of the eligible dates for all cells inside the packet. If that
intersection is empty, the planner must decompose the packet into smaller
medically compatible packets or single-vaccine sessions. If no safe plan exists,
it must emit a medical-window/capacity exception instead of dead-ending or
silently pushing a cell outside its window.

Planning rules:

1. If all due administration cells fit under the cap within the allowed buffer,
   split sessions so each day stays at or below the cap.
2. If the cap cannot fit all due work before the latest safe date or max buffer,
   do not push animals farther away just to satisfy capacity.
3. In that breach case, distribute the remaining work as evenly as possible only
   across dates each packet is eligible for, allow the cap to be exceeded when no
   safe under-cap allocation exists, and mark a capacity exception on the
   affected work sessions.

The capacity packing atom is an animal's compatible same-session vaccine packet:

```text
animal_id + compatible obligation set for the same work session
weight = number of administration cells in that packet
```

Example: animal A, such as a goat, with FMD+HS due in one compatible session is
one packet with weight 2. The planner must not satisfy a daily cap by putting
animal A's FMD on day 1 and HS on day 2. It may split only when medical windows,
compatibility, or the per-animal shot cap mean the vaccines are not actually
eligible for the same session.

Examples:

```text
300 cells, cap 100/day, safe days >= 3
  -> day 1: 100, day 2: 100, day 3: 100

250 cells, cap 100/day, safe days >= 3
  -> day 1: 100, day 2: 100, day 3: 50

500 cells, cap 100/day, only 4 safe days remain
  -> day 1: 125, day 2: 125, day 3: 125, day 4: 125
  -> capacity exception: cap exceeded because medical window wins
```

The planner should allocate by urgency first:

```text
build same-session animal packets
sort packets by effective_deadline, disease priority, batching_hold_count,
  capacity_scope_key, animal_id, obligation_set_hash
pack packets into daily capacity buckets inside each packet's eligible dates
split oversized work-session candidates into dated work-session siblings
mark capacity_breach when safe windows force over-cap allocation
```

The packet sort is a total deterministic order. `batching_hold_count` is the
existing one-time hold counter behind `max_batching_hold_count`; when a packet
contains multiple cells, use the maximum hold count across those cells.
`obligation_set_hash` is a stable hash of the packet's sorted obligation/rule
identifiers. Do not rely on database row order or map iteration order for
capacity allocation.

`disease priority` comes from the governed vaccination rule metadata. The source
rules already carry a priority for each vaccine row; rule authoring must preserve
that value into the published rule metadata used by the planner. If priority is
missing, publish/preview must fail closed instead of using an implicit hardcoded
disease order.

This rule is separate from `max_shots_per_animal_per_drive`. The per-animal cap
prevents too many vaccines on one animal in one visit. The daily capacity cap
prevents too much total human work in one day.

Capacity state definitions:

```text
within_cap = planned count is at or below cap for the bucket
capacity_breach = planned count exceeds cap because no safe under-cap allocation
                  exists inside the binding medical/buffer window
```

`capacity_breach` is a bucket/day property. When a dated capacity bucket exceeds
the cap, every work-session row sharing that over-cap bucket surfaces
`capacity_breach`; do not mark only the marginal row that happened to push the
bucket over.

Do not use a separate `over_cap` state unless a future ADR defines a non-breach
reason for exceeding the cap. Until then, over-cap work is `capacity_breach`.

Planner re-runs must be stable and idempotent:

- Re-running with the same due set and policy produces the same dated
  work-session assignments.
- New due packets are packed into remaining capacity first.
- Published, in-progress, submitted, or completed work sessions are not silently
  moved by a re-plan.
- If a new packet cannot fit without moving a locked session, create a later
  eligible sibling session or mark a capacity/medical-window exception.
- A re-plan that proposes moving unlocked planned sessions must record the
  previous assignment and reason, and must not change already linked task or
  submission evidence without an explicit supersede/replan path.

The supersede/replan API and verification overwrite rules are out of scope for
this ADR. Until a later verification/replan decision names that path, an
implementation must not silently overwrite materialized terminal completion rows,
linked tasks, submissions, or proof evidence.

## Batch And Stock Cardinality

Capacity split subdivides batches per dated work-session sibling.

One `obligation_batch` must not span multiple planned business dates after
capacity splitting. If FMD cells from one medical cohort are split over three
dates, the finalizer creates three dated FMD batches, one per dated sibling
work-session. The corresponding HS cells follow the same rule when HS is part of
the same compatible packet.

Stock reservation and finalization happen per dated sibling batch, close to that
day's execution package. Do not hold one multi-day reservation by keeping a
single per-vaccine batch attached to multiple dated work sessions.

## Matrix Submission Contract

The vaccination SOP submission body must be a matrix, not a flat per-animal form.

Each cell represents one `animal_id + obligation_id` and carries the execution
facts for that obligation:

```text
animal_id
obligation_id
batch_id
rule_id / vaccine code
state: administered | skipped | deferred | needs_review
vaccine_inventory_lot_id
doses
dose_ml_given
route_site
administered_at
cold_chain_verified
adverse_reaction
skip_or_defer_reason
```

The cell state is not automatically a completion row. `administered` and a
review-accepted final `skipped` close the obligation as terminal outcomes.
`deferred`, `needs_review`, and retryable/non-final skips record the submitted
decision and leave the obligation open or deferred for a later work session.
They must not consume the active completion uniqueness slot.

Terminal cells materialize into the existing `vaccination_completions` table.
Do not create a new vaccination completion table for matrix fanout.

A goat can therefore receive FMD and HS in the same work session while another
animal in the same shed receives only FMD because HS is skipped, deferred, or
not due. Runtime APIs and persisted contracts use `animal_id`; goat wording is
example language only. Legacy tables or adapters may still map that value to
`goat_id` while the physical schema is being migrated.

The form DSL and mobile runner should expand from repeat-per-animal to
repeat-per-animal-by-obligation for vaccination work sessions. The existing
repeat-per-goat scaffold may be reused as a legacy implementation detail, but
the vaccination session form adds a cell axis for the session's vaccine
obligations.

## Partial And Incremental Submission

A work session may receive multiple accepted submissions when offline sync,
multiple devices, or staged execution closes different parts of the same matrix.

Each submission has its own whole-submission idempotency key and closes a
declared set of `animal_id + obligation_id` cells. A second submission may append
new disjoint cells to the same work session. If it includes a cell that was
already accepted and materialized with identical semantics, that cell is a
no-op. If it includes the same cell with different execution facts, it is a
conflict unless an explicit future supersede/rework path authorizes the change.

Materialized completion uniqueness applies only to active terminal completion
truth, not to every submitted matrix cell. Preserve the existing rework pattern:
rejected/reversed attempts remain as history, while only one active
recorded/accepted completion exists for an obligation and animal. In the
species-neutral target contract, that hard guard is:

```text
UNIQUE(tenant_id, obligation_id, animal_id)
WHERE status IN ('recorded', 'accepted')
```

While the physical schema still uses `goat_id`, adapters may implement the same
guard as `(tenant_id, obligation_id, goat_id)` and map it from `animal_id`. This
ADR does not supersede the rejected/reversed rework history model from migration
`000082_vaccination_rework_and_sop_review_fanout.sql`.

Work-session completeness is therefore derived from all required cells reaching a
terminal state across one or more accepted submissions, not from the existence of
exactly one submission row.

## Idempotency Contract

Two levels of idempotency are required.

Whole submission:

- One client idempotency key per work-session submission.
- Exact replay returns the original submission and fanout result.
- Same key with a different task, proof set, or canonical matrix fingerprint is
  rejected as an idempotency conflict.
- Fanout runs in one transaction for the submitted matrix.
- The canonical matrix fingerprint is a semantic JSON hash: sort cells by
  `animal_id, obligation_id`; sort proof references by proof type and id;
  normalize decimals such as `dose_ml_given`; use stable boolean, timestamp, and
  null/default handling; and omit client-only presentation fields.

Per-cell fanout:

- Per-cell fanout idempotency key must be derived from:

```text
submission_id + animal_id + obligation_id
```

- It must not be derived only from `sop_submission_item_id`.
- The collision fix is adding `obligation_id`: animal A's FMD and animal A's HS
  are different obligations even when they come from one submission row.
  `animal_id` is retained in the key for audit readability and defensive
  validation.
- Replay of a cell is a no-op when the same cell was already materialized.
- A different submission containing an already active terminal completion for the
  same `obligation_id + animal_id` must hit the same active-completion uniqueness
  check described above.
- A deferred, needs-review, or retryable skipped cell does not block a later
  administered/final-skipped terminal cell.
- A different payload for the same cell key is a conflict unless the existing
  verification/rework path explicitly supersedes it.

## Projection Contract

Operator-facing projections are group-first:

- Calendar emits one vaccination work-session row for the grouping key.
- Action Center emits one operator row for the grouping key.
- The row contains vaccine/batch children so the operator can inspect expected,
  completed, skipped, deferred, and blocked counts per vaccine.
- Rows split by daily capacity must appear on their assigned dates, with an
  explicit capacity state: `within_cap` or `capacity_breach`.

Analytical and accountability projections remain per-vaccine:

- Coverage percent by vaccine is unchanged.
- Passport history is unchanged.
- Protocol Adherence and Control Tower keep per-vaccine truth, but may add a
  work-session completeness view.

## Config Contract

Rule authoring may expose milestone bundles such as "week 12 -> FMD + HS".
Publishing still compiles to per-vaccine protocol rules and per-vaccine
obligations.

Bundle metadata must be carried forward so batching can assign related
per-vaccine batches to the same `session_id` and same planned work-session
window. Do not collapse vaccine rules into a single coverage rule.

Capacity policy must be authored with the drive planner settings, not hidden in
Calendar or Action Center. The planner needs the cap before batches and sessions
are finalized.

The daily capacity fields belong in the same vaccination matrix / rule
configuration flow as the existing drive planner settings. Operators should not
need a separate hidden settings page to understand why a 300-cell drive split
into three days.

Required config UI fields:

```text
max administrations per day
capacity scope
max buffer days (the existing batching hold budget)
overflow policy
```

The config UI must include an info popover next to the daily cap field. The
popover must explain, in plain language:

```text
This cap counts vaccine administrations, not animals.
One animal, for example a goat receiving FMD + HS, counts as 2 administrations.
The planner splits large drives across days when it can do so safely.
Medical due windows, cross-vaccine spacing, and max buffer days win over the cap.
If the cap cannot fit all due work inside the safe window, Goat OS spreads the
work across safe days, allows an over-cap day, and marks a capacity exception.
```

The preview/impact panel for a draft rule must show the planned split before
publish:

```text
total administration cells
capacity per day
number of planned work sessions
business date and capacity bucket key
per-day cell counts
capacity state: within_cap | capacity_breach
binding deadline per session/day: latest safe date or buffer end
breach reason when medical windows force over-cap allocation
```

## E2E Proof And Published Report Contract

This model is not closed until the E2E report names and proves the new cases
explicitly. The existing GitHub Pages report path may be reused; the Pages site
must clearly show these vaccination work-session stories under the E2E report.

Minimum report requirements:

```text
story id
story name
business case
input setup
business date
capacity bucket key and scope resolution
expected sessions
expected per-day counts
binding deadline / breach reason
expected matrix cells
expected terminal completion rows
expected projection rows
idempotency expectation
pass/fail status
evidence links or artifact paths
```

Minimum E2E stories:

1. Single vaccine baseline: one vaccine, many animals, one work session, one
   task, one submission.
2. FMD + HS combo: same shed/session/date, two per-vaccine batches, one work
   session, one task, one submission/video.
3. Mixed matrix cells: one animal receives both vaccines, another receives only
   one, another is skipped/deferred with reason.
4. Per-cell idempotency: replay of the same matrix does not duplicate
   completions; same submission key with a changed matrix conflicts.
5. Capacity split exact: 300 administration cells with cap 100/day becomes three
   dated work sessions of 100 each.
6. Capacity split remainder: 250 cells with cap 100/day becomes 100, 100, 50.
7. Capacity breach: 500 cells with cap 100/day and only four safe days becomes
   an even over-cap split with `capacity_breach` recorded.
8. Cap scope across sheds and vaccines: when the configured scope is team/day,
   work from multiple sheds and vaccine types shares the same daily capacity
   bucket.
9. Medical window wins: no animal is pushed beyond the latest safe date just to
   satisfy the cap.
10. Re-plan stability: re-running the planner keeps locked/published sessions in
    place, packs new due packets into remaining capacity first, and records any
    required new sibling or exception.
11. Calendar and Action Center: operator sees one row per dated work session,
    with vaccine children and capacity state.
12. Config UI: daily capacity fields appear in the vaccination matrix/rule
    config flow, the info popover explains cell counting and safety override,
    and the impact preview shows the planned split.
13. Incremental submission: two devices submit disjoint cell sets for the same
    work session; accepted cells append without duplicate completions, and a
    conflicting repeat cell is rejected.
14. Batch and stock split: one medical cohort split across three dates creates
    dated per-vaccine sibling batches and per-date stock reservations, not one
    multi-day batch/reservation.
15. Heterogeneous-window breach: some packets are eligible only on days 1-2 and
    others on days 1-4; under breach, day-1-2 packets stay inside days 1-2 and
    are never shoved to days 3-4 by an even splitter.
16. Defer then administer: one cell is deferred in an earlier work session, the
    obligation remains re-attemptable, and a later administered terminal cell
    records the active completion without a uniqueness conflict.

When development is complete, the GitHub Pages E2E report must be treated as the
owner-readable closure artifact. It must not bury these cases in raw logs only.
If any story is not implemented, the report must mark it missing or skipped with
the exact blocker.

## Implementation Sequence

1. Get maintainer sign-off on this ADR and the two retired/amended doc lines.
2. Define the exact work-session and capacity persistence shape:
   - preferred minimal path: normalized `session_id` plus grouped fields on
     `obligation_batches`;
   - optional path: generic work-session head row only if APIs/offline sync need
     a stable addressable object;
   - capacity fields for daily administration cap, scope, the existing
     one-time hold budget / capacity buffer, and overflow policy.
3. Backfill and preserve disease priority on every governed vaccination rule
   before enabling fail-closed publish/preview validation for missing priority.
4. Update the sweeper/planner so combo-compatible obligations first form
   work-session candidates, then split into daily capacity buckets.
5. Update the finalizer so each dated work-session sibling owns one SOP task and
   its own dated per-vaccine batches for stock reservation/finalization.
6. Update the vaccination matrix/rule config UI so capacity is authored in the
   same flow, with info popover and impact preview.
7. Update SOP/form DSL to model vaccination matrix submissions.
8. Rewrite vaccination submission fanout to process all submitted
   `animal_id + obligation_id` cells idempotently.
   - preserve the existing active-completion/rework-history pattern from
     migration `000082`: rejected/reversed attempts stay as history, while only
     active terminal recorded/accepted completions are unique;
   - ensure deferred, needs-review, and retryable skipped cells do not consume
     the active completion slot.
9. Update Calendar and Action Center to emit work-session rows with vaccine
   children and capacity state.
10. Extend the E2E harness and GitHub Pages report so the new stories are named,
   counted, and owner-readable.
11. Add tests for:
   - FMD + HS same session, one task, one submission, two completions for one
     animal;
   - one animal receiving only one vaccine while another receives both;
   - 300 administration cells with cap 100/day split into three dated sessions;
   - cap breach when 500 cells cannot fit into the remaining safe window;
   - heterogeneous safe-window breach keeps short-window packets inside their
     own eligible dates;
   - planner re-run does not silently move published/in-flight sessions;
   - mixed safe windows force packet decomposition or a medical-window exception;
   - per-dated-sibling batch and stock reservation cardinality;
   - partial/incremental submissions append disjoint cells and reject conflicting
     repeated cells;
   - deferred and needs-review cells do not block later terminal administration;
   - config UI cap fields, info popover, and impact preview;
   - GitHub Pages E2E report lists the bundle/capacity stories with pass/fail;
   - exact replay returns original result;
   - same submission key with different matrix conflicts;
   - same cell replay is a no-op;
   - Calendar and Action Center show one operator row.

## Non-Goals

- Do not add `vaccination_drives`.
- Do not collapse per-vaccine obligations.
- Do not make coverage bundle-based.
- Do not implement runtime behavior from this ADR until sign-off is recorded.
