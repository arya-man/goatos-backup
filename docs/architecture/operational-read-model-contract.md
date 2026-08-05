# Operational Read Model Contract

Status: accepted architecture guardrail; implementation in progress  
Audience: backend, admin web, Android, QA, product owners  
Scope: vaccination first, then shifting, counts, breeding, weighing, feed, procurement, and future verticals

Implementation handoff for the current PA / CT / admin-web / Android gaps:
`docs/architecture/operational-read-model-contract-implementation-handoff.md`.

## Problem

Goat OS is growing from one large vaccination workflow into an operating system
with many verticals. The current backend already has separate domain modules, but
recent vaccination fixes showed a deeper architecture risk: different surfaces
can still interpret the same operational facts differently.

The failure mode is not simply a frontend bug or a mobile bug. It is a contract
and read-model problem.

Example from vaccination:

- Calendar shows park/day drive progress.
- Protocol Adherence shows selected-drive rows and summary.
- Control Tower shows exception/alert rows.
- Admin web shows cards, drawers, and detail views.
- Android shows execution state and proof capture.
- Backend stores obligations, completions, SOP tasks, verification, assignment
  membership, shed partitions, and proof media.

When mobile proof capture entered the flow, terms like `completed`,
`submitted`, `verification_pending`, `total_animals`, `completed_animals`,
`submitted_animals`, `total_count`, `row_count`, `drive`, `shed`, and
`partition` were not enforced as one shared contract across all surfaces. The
result was number drift, mixed grains, and patches that had to reconcile one
screen at a time.

Before adding more verticals, Goat OS needs a strict operational read model
contract: every surface may render differently, but it must consume the same
facts, at the same grain, with the same definitions.

Critical animal actions such as quarantine, ICU, death, high-risk movement, and
clinical schedule defers also load
`docs/features/critical-animal-action-guardrails.md`; their read models must
show guardrail-required, expected-return/checkpoint, extension, defer, and reopen
state without clients inventing it locally.

## Architecture Verdict

The foundation is good but not yet strong enough to scale safely by adding
vertical after vertical.

Good:

- Backend modules are already separated by domain: vaccination, execution,
  process integrity, calendar, counts, weighing, feed, procurement, etc.
- There is a canonical backend serving path and OpenAPI contract.
- Admin web and Android have dedicated client layers rather than direct SQL.
- The project already has tests, generated clients, screenshot tests, and scale
  guards.

Weak:

- Read models are not yet governed as first-class contracts.
- Cross-surface numbers can be recomputed independently.
- Grain semantics are not consistently encoded in names and tests.
- OpenAPI, generated clients, Android DTOs, and backend structs can drift.
- A new field can be added to one response without all consumers and schemas
  moving together.
- Vertical-specific logic can leak into shared dashboards like Calendar,
  Control Tower, and Protocol Adherence.

Conclusion:

Goat OS is scalable in domain ambition, but it is not yet plug-and-play enough.
The next architectural step is to make read models pluggable, contract-first,
and grain-explicit.

This document defines the response/DTO/read contract for operational surfaces.
It does not require every vertical to introduce a new materialized table. For
5k-50k row envelopes, indexed canonical reads are allowed and often preferred.
New projection tables or denormalized read stores need the normal ADR and scale
review path, including freshness, replay, stale-read behavior, and
`scale-guard` coverage.

## Target Principle

One operational fact should have one owner, one grain definition, and one
contracted read shape.

Surfaces may choose what to display, but they must not invent facts.

```text
Canonical domain writes
  -> vertical-owned facts
  -> operational read model
  -> surface adapters
       Calendar
       Control Tower
       Action Center
       Protocol Adherence
       Workflows
       Admin Web detail pages
       Android execution/proof screens
       CEO/AI reporting (read-only consumer)
```

## Core Vocabulary

Every read model must declare its grain.

Common grains:

- `animal`: one goat.
- `obligation`: one due work item, for example one vaccine dose obligation.
- `completion`: one submitted or accepted execution fact.
- `proof`: one proof media/submission fact.
- `verification`: one verifier decision fact.
- `shed`: physical shed.
- `partition`: shed sub-scope such as `whole`, `1`, `2`, etc.
- `drive`: an operational batch/drive selected for execution.
- `park_day`: one park and business date.
- `task`: one SOP task or mobile execution task.
- `alert`: one Control Tower exception row.
- `calendar_event`: one calendar presentation event.

Naming rule:

- Counts must name their grain.
- Avoid generic names like `total`, `completed`, `submitted` in shared contracts.
- Prefer `total_animals`, `completed_obligations`,
  `submitted_completion_animals`, `verification_pending_obligations`, etc.

If a compact API keeps legacy names, its schema description must state the grain
and whether buckets are disjoint or overlapping.

## Required Invariants

These invariants should become testable for every vertical.

1. Contract Invariant

   Backend response structs, OpenAPI schema, generated TypeScript client, and
   Android DTOs must agree on field names, optionality, enum values, and
   semantics.

2. Grain Invariant

   A field must not change grain across surfaces. If Calendar displays animals,
   Android and Admin Web cannot compare it against obligation counts without
   explicit conversion.

3. Bucket Invariant

   Any status bucket must state whether it is disjoint or overlapping.

   Example:

   - `completed_obligations`, `due_obligations`, `overdue_obligations`, and
     `deferred_obligations` should usually be disjoint and sum to
     `total_obligations`.
   - `submitted_animals` may overlap with `completed_animals` unless explicitly
     defined otherwise. If overlapping, the UI must not add them.

4. Pagination Invariant

   Summary numbers must be whole-result aggregates, not current-page aggregates,
   unless the field name says `page_*`.

5. Scope Invariant

   A selected operational unit must be scoped by enough identity to be stable.
   Rule ID alone is not enough when the same rule recurs across dates or drives.
   Drive scoping should include the selected drive identity or an equivalent
   stable tuple such as tenant, park/shed/partition, business date, batch/drive,
   protocol version, and rule.

6. Surface Consistency Invariant

   For the same seed fixture, Calendar, Action Center, Protocol Adherence,
   Control Tower, Workflows, Admin Web, and Android must agree on the facts they
   share.

7. Backward Compatibility Invariant

   Mixed-version API responses must degrade honestly. Fallbacks are allowed only
   when the UI labels the fallback grain correctly.

8. Shared-Parent Scope Invariant

   Shed- or partition-grain state must not be inferred from a parent task,
   batch, park, or protocol row unless the contract explicitly declares that the
   parent state is authoritative for every child. Shared parent rows are allowed
   as lookup dimensions, but child execution/proof/submission state must come
   from child-grain facts.

9. Location Grain Invariant

   An animal's ground location is `OperationalLocation = park + physical_shed +
   optional partition_label`. When a partition exists, every surface must
   render/answer it:
   
   - No partition → `Yashoda` (bare shed name)
   - Numeric partition → `Castro 2`
   - Prefixed partition → `Godel 1 - Part 3`
   
   Never render a non-partitioned shed as `Yashoda whole` — `whole` is an
   internal matching sentinel only, never user-facing copy. `NULL`, `''`, and
   `'whole'` all mean non-partitioned.
   
   Counts, destination catalogs, animal search results, vaccination obligation
   detail, shifting form, action center rows, CEO/leadership reporting, and any
   location-bearing surface must carry `partition_label` alongside `shed_id` and
   park. Collapsing partitions into the parent shed for display makes the
   dropdown lie: an operator cannot express "Castro 1 → Castro 2" from a
   parent-only `Castro` list, and a CEO report showing `Castro: 200 animals` when
   the animals actually live in `Castro 1/2/3: 50+75+75` obscures operational
   reality.
   
   Partitions must group by `shed_id` (uuid) + park, never by shed name: names
   repeat across parks, so name-keyed aggregation silently merges parks.
   
   A partition catalog must enumerate all existing partitions, including empty
   ones. A partition with zero live animals still exists (e.g., CBE `Yashoda 5`)
   and must remain a valid shifting destination. Derive partition existence from
   `shed_partitions` (migration 000111), not from `goat_shed_partitions` (a
   per-goat relation that hides empty partitions).

## Proposed Pluggable Model

Each vertical should expose a small set of operational providers. These do not
need to be literal Go interfaces on day one, but the architecture should move in
this direction.

```go
type WorkItemProvider interface {
    ListWorkItems(ctx context.Context, q WorkItemQuery) (WorkItemResult, error)
}

type SummaryProvider interface {
    Summary(ctx context.Context, q SummaryQuery) (OperationalSummary, error)
}

type EvidenceProvider interface {
    Evidence(ctx context.Context, q EvidenceQuery) (EvidenceResult, error)
}

type CalendarProvider interface {
    CalendarEvents(ctx context.Context, q CalendarQuery) (CalendarEventResult, error)
}

type ControlTowerProvider interface {
    Alerts(ctx context.Context, q AlertQuery) (AlertResult, error)
}

type MobileExecutionProvider interface {
    ExecutionRows(ctx context.Context, q ExecutionQuery) (ExecutionResult, error)
}
```

The important part is not the exact names. The important part is that every
vertical plugs into the same shared surface concepts instead of every surface
learning vertical-specific SQL and semantics.

```text
Vertical modules:
  vaccination
  shifting
  counts
  breeding
  weighing
  feed
  procurement

Shared surfaces:
  calendar
  control_tower
  action_center
  protocol_adherence
  workflows
  mobile_execution
  admin_detail
  ceo_ai_reporting
```

`ceo_ai_reporting` is a one-way reporting consumer. It may read contracted
operational facts through approved reporting views/tools, but core operator read
paths must not depend on `ceo_ai.*` tables, views, or assistant-specific
fallbacks for runtime truth.

## Recommended Read Shape

Every shared operational row should carry:

```yaml
identity:
  tenant_id
  vertical
  work_item_id
  source_fact_id
  source_fact_type

scope:
  park_id
  park_name
  shed_id
  shed_name
  partition_label
  animal_id
  cohort_id

time:
  business_date
  due_at
  window_start
  window_end
  timezone

freshness:
  projection_version
  projected_at
  as_of
  freshness_status
  serving_state
  stale

state:
  work_state
  proof_state
  verification_state
  severity
  process_intact

grain:
  row_grain
  summary_grain

counts:
  total_animals
  total_obligations
  completed_animals
  completed_obligations
  submitted_completion_animals
  submitted_completion_obligations
  verification_pending_obligations

evidence:
  proof_ids
  latest_evidence_at
  latest_rejection_reason

owner:
  operator
  verifier
  escalation_owner
```

Individual APIs may expose only the fields they need, but the canonical internal
read model should be rich enough that surfaces do not recompute these concepts
locally.

## Vaccination Lessons To Generalize

### Lesson 1: Calendar drive summaries must be authoritative

Calendar cards should not reconstruct drive totals from paginated target rows.
The drive summary must be computed as a whole-result aggregate at the backend.

Apply this to:

- weighing campaign progress
- shifting movement batches
- breeding check schedules
- counts reconciliation drives

### Lesson 2: Protocol Adherence must not page its summary

Protocol Adherence summaries must come from a full filtered aggregate. Row
pagination is separate from summary computation.

Apply this to every vertical:

- The table page can show 50 rows.
- The summary must still describe all matching rows.
- If the summary describes only the current page, call it `page_summary`.

### Lesson 3: Control Tower is a surface, not a source

Control Tower should display operational alerts generated from canonical facts.
It should not invent state labels, parse workflow IDs as evidence, or hold its
own hidden interpretation of proof/verification state.

### Lesson 4: Mobile proof changes backend truth

When Android submits proof, it changes the operational state machine. Calendar,
Protocol Adherence, Control Tower, Admin Web, and Android cache all need to
observe the same transition.

Mobile is not "just a client" for these flows. It is a write surface for
canonical operational facts.

### Lesson 5: Partitions are first-class scope

Shed partition labels cannot be UI decoration. If operators execute by
partition, then partition must be part of the operational scope contract.

### Lesson 6: Shed state cannot be borrowed from shared parent rows

Vaccination often has a park/batch/task parent with many sheds underneath it.
Per-shed execution state, proof state, submit state, and verification state must
come from shed-grain facts. A parent `sop_tasks.state` value can explain the
overall workflow, but it must not be rendered as the state of every shed unless
the backend contract explicitly marks it as a whole-parent state.

## Contract Governance

Every change that lands a shared API must satisfy these checks:

1. Backend domain type changed.
2. OpenAPI schema changed.
3. Generated TypeScript client changed.
4. Android DTO changed when Android consumes the endpoint.
5. Admin Web usage changed when Admin Web consumes the endpoint.
6. Contract test or fixture changed.
7. Mixed-version fallback reviewed if the field is optional.

Current and required gates:

```text
make operational-read-model-contract-guard   # discoverability only
make api-client-check
make mobile-contract-ownership-guard
make aggregate-projection-guard
make guardrail-registration-guard
```

`operational-read-model-contract-guard` only proves this contract remains
discoverable from local agent/CI entry points. It is not a semantic drift guard.
The semantic gate set must fail if:

- a JSON field exists in Go response structs but not OpenAPI
- OpenAPI changed but generated clients are stale
- Android DTOs omit required consumed fields
- enum values differ between backend, TS, and Kotlin
- `additionalProperties: false` schemas reject backend-emitted fields
- frontend or mobile derives backend-owned state by parsing display strings
- a paged endpoint returns whole-result labels from page-local rows

Known missing semantic guard halves:

- Go response structs vs OpenAPI field drift.
- Kotlin DTOs vs consumed OpenAPI schema drift.
- Frontend command-surface fallbacks that mask missing backend contracts.

## Cross-Surface Golden Fixtures

For each vertical, create at least one golden fixture that exercises the whole
path.

Example vaccination fixture:

```text
Drive: PPR booster, park CBE, 2 sheds, one partitioned shed
Animals: 10
Obligations: 15
Submitted proof: 4 obligations across 3 animals
Accepted completions: 6 obligations across 5 animals
Rejected proof: 1 obligation
Deferred: 1 animal
Overdue: 2 obligations
```

The fixture should assert:

- Calendar drive summary counts.
- Protocol Adherence summary counts.
- Protocol Adherence rows for selected drive.
- Action Center rows, counts, and drawer state.
- Control Tower alert rows and details.
- Workflow list/drilldown chain state.
- Android execution rows and proof states.
- Admin Web card/detail text where relevant.
- Generated API client accepts the response.

This catches the exact class of bugs where one screen says 3, another says 5,
and a third says 8 because they used different grains.

## Vertical Onboarding Checklist

Before a new vertical is allowed into shared surfaces:

- Define canonical write owner.
- Define work item identity.
- Define scope grain.
- Define time grain.
- Define status state machine.
- Define evidence model.
- Define summary counts and bucket disjointness.
- Define Calendar representation.
- Define Action Center representation.
- Define Control Tower alert representation.
- Define Workflow representation and drilldown chain.
- Define mobile execution representation if mobile participates.
- Add OpenAPI schema.
- Generate clients.
- Add cross-surface golden fixture.
- Add scale test for target row count.
- Add pagination test proving summary is not page-local.

## Vaccination Read Models

### GET /vaccination/command

Status: Implemented; grain and bucket definitions below.

**Purpose**: CEO/leadership command board: all-history vaccination KPIs, active-batch drive status, cohort pending matrix, and verification queue backlog.

**Grain and Buckets** (disjoint unless noted):

- **KPIs (all-history scope, park-scoped)** — ANIMAL grain (`COUNT(DISTINCT target_id)`), and a
  **DISJOINT and EXHAUSTIVE** partition of `targets`: the four buckets are evaluated as a priority
  chain over the SAME obligation row, so
  `doses_verified + awaiting_verification + overdue_not_given + scheduled_ahead = targets`.
  The UI may therefore render them side by side and as a share of `targets`.
  - `targets`: `COUNT(DISTINCT target_id)` over all open + completed obligations in scope
  - `doses_verified`: animals with an `accepted` completion
  - `awaiting_verification`: animals with a `recorded`-unverified completion AND no `accepted` one
  - `overdue_not_given`: animals with NO completion, an OPEN obligation, and IST business due date
    **earlier than** the as-of IST business date
  - `scheduled_ahead`: animals with NO completion, an OPEN obligation, and IST business due date
    **on or after** the as-of IST business date
  - **OPEN** is the repo's canonical open-obligation status set
    `('scheduled','due','in_progress','deferred','missed')` — the same set behind
    `obligation_instances_open_logical_due_idx`. It is **not** `'scheduled'` alone: the sweeper
    flips `scheduled -> due` on the due business day, so a `'scheduled'`-only predicate drops every
    currently-actionable obligation out of both the overdue and the scheduled bucket.
  - Known residual: because the grain is ANIMAL, an animal carrying two obligations in different
    buckets can appear in two buckets. The partition is exact at one-obligation-per-animal, the
    grain every live vaccination drive uses.

- **Active Batch Status (current drive scope, per park)**:
  - `targets`: `COUNT(DISTINCT animal_id)` assigned in current active `vaccination_drive_assignments` batch
  - `verified`: `COUNT(DISTINCT completion_id WHERE status='accepted')` from active batch animals
  - `awaiting`: `COUNT(DISTINCT completion_id WHERE status='verification_pending')` from active batch animals
  - `overdue`: `COUNT(DISTINCT obligation_id WHERE status='overdue')` from active batch animals

- **Cohort Submission Matrix (obligation grain, per farm×stage×sex×dose-qualified-vaccine, disjoint)**:
  - Key: `(park_id, management_stage, sex, vaccine_label)` from obligation eligibility. Read
    FARMWISE — the same cohort on two farms stays two cells.
  - `animal_count`: `COUNT(DISTINCT animal_id)` in cohort across open + completed obligations
  - **Three DISJOINT buckets**, partitioned by *who owes the next move*. Each obligation is counted
    in at most one, and `pending + submitted + verified <=` the cell's obligation total:
    - `pending_count` — the **OPERATOR** owes field work:
      `COUNT(DISTINCT obligation_id WHERE NOT has_accepted AND NOT has_recorded_unverified AND
      status IN ('scheduled','due','in_progress','deferred','missed') AND due business date <=
      as-of business date)`
    - `submitted_count` — the **VERIFIER** owes review:
      `COUNT(DISTINCT obligation_id WHERE NOT has_accepted AND has_recorded_unverified)`
    - `verified_count` — nobody owes anything:
      `COUNT(DISTINCT obligation_id WHERE has_accepted)`
  - **Why `pending_count` changed.** It used to be
    `COUNT(DISTINCT obligation_id WHERE status IN ('scheduled', 'due'))` — a definition the query
    implemented faithfully. But obligation status advances only on **VERIFICATION**, never on
    submission, so a park whose every animal had been vaccinated and whose proof was submitted
    produced a byte-identical matrix to a park nobody had touched. The page then showed
    "40 awaiting verification" in the KPI row and "40 pending" in the matrix directly below it,
    with no column reconciling the two, and a CEO could not tell that all 40 animals had in fact
    been vaccinated. The query was conformant; the **definition** was the defect. `pending` now
    means field work NOT DONE, and `submitted_count` is the reconciling column.
  - **Cross-surface reconciliation with the KPI row on the same page** (same tenant/batch/park
    filter): `SUM(submitted_count) == kpis.awaiting_verification`,
    `SUM(verified_count) == kpis.doses_verified`,
    `SUM(pending_count) == kpis.overdue_not_given + kpis.scheduled_ahead`.
  - **Grain**: these buckets are **obligation** grain; the KPI row is **animal** grain. They agree
    exactly at one-obligation-per-animal-per-vaccine, the grain every live drive uses. The matrix
    stays obligation grain by design so a multi-vaccine animal is visible once per vaccine.
  - Business-day grain, Asia/Kolkata, on both sides of the due-date comparison.
  - Each obligation counted once per its (farm, stage, sex, vaccine) tuple

- **Verification Queue (shed-scoped backlog, per shed×dose-rule, ordered by age)**:
  - Key: `(shed_id, dose_rule)` from SOP task + dose rule origin
  - `awaiting_count`: `COUNT(DISTINCT completion_id WHERE status='verification_pending')` for shed×rule
  - `total_count`: `COUNT(DISTINCT completion_id WHERE status IN ('verification_pending', 'accepted', 'rejected'))` for shed×rule
  - `days_in_queue`: `DATEDIFF(current_business_date, MIN(created_date WHERE status='verification_pending'))` for oldest pending in shed×rule
  - Ordered: `days_in_queue DESC` (oldest first, most urgent)

### GET /calendar/vaccination/events — park drive card summary

Status: Implemented; buckets below.

The drive card's `summary` counts are OBLIGATION grain and are read from the **same**
`(park_id, due_date)` membership rows that decide the card's headline `status`, so counts and
status can never disagree:

- `scheduled_count`: open, **not submitted**, not deferred (the "still to do" bucket)
- `review_count`: **submitted for verification** and not yet completed — an obligation COUNT, not a
  boolean flag. It must never be derived from `obligation_batches.status`: a batch sitting in
  `in_progress` while all its obligations are submitted made this render `0` under a
  `verification_pending` headline, and simultaneously left the same animals inside
  `scheduled_count`.
- `deferred_count`: deferred and not submitted
- `target_count`: roster size for the park-day; `scheduled_count`, `review_count` and
  `deferred_count` are disjoint and must not be summed against `target_count` when completed work
  exists (completed obligations are reported only in the optional `drive_summary` block).

#### `drive_summary` — progress and remaining (one answer per question)

Drive progress is **FIELD WORK DONE = completed + submitted** (maintainer decision 2026-08-03). An
operator who vaccinated every animal and submitted proof sees **100%** and **"4 of 4 sheds done"**;
the outstanding video review is carried by the `verification_pending` status/chip and
`submitted_count`, never by holding the ring below 100%. The backend owns the single number;
admin-web and Android render `progress_completed` / `progress_total` / `progress_pct` verbatim on
the `progress_basis` grain and MUST NOT derive their own numerator.

- Buckets (obligation grain, five disjoint, total):
  `total_count = completed_count + submitted_count + due_count + overdue_count + deferred_count`
- `remaining_count` = **work still owed by the OPERATOR** =
  `due_count + overdue_count + deferred_count` = `total_count - completed_count - submitted_count`.
  It **excludes** submitted work, exactly like the progress numerator, so a fully submitted drive
  reports `progress_pct: 100` **and** `remaining_count: 0`.
- **Why `remaining_count` changed.** It was `total_count - completed_count`. Under the OLD
  "progress = verified only" rule that agreed with the ring; after the progress-semantics decision
  it did not, and one object shipped `progress_pct: 100` next to `remaining_count: 20`. A client
  picking `remaining_count` for an "N left" label disagreed with the ring beside it — the same
  cross-surface failure mode as the 200-vs-400 incident. A payload must never carry two
  contradictory answers to "how much is left".

**Contract**: See backend `VaccinationCommandBoardResponse` OpenAPI schema and generated TypeScript client in `lib/api/vaccination-command-board.ts`.

## Rollout Plan

### Phase 1: Stop Contract Drift

- Add a contract drift check for Go response structs vs OpenAPI vs generated TS.
- Add Android DTO coverage for consumed shared endpoints.
- Fix known drift such as `ControlTowerAlert.partition_label`.
- Make PR review require generated client diffs for shared API changes.

### Phase 2: Lock Vaccination Semantics

- Document vaccination grains and bucket definitions.
- Split overlapping vs disjoint counts explicitly.
- Fix Protocol Adherence summary pagination/scoping.
- Add a vaccination cross-surface golden fixture.
- Add tests for repeated rule across multiple drives.
- Remove Android calendar/execution phantom date fields unless backend/OpenAPI
  owns them.
- Add missing consumed Android DTO fields for Control Tower and Protocol
  Adherence.
- Stop mobile shed/day summaries from computing whole-day or adherence totals
  from only the currently fetched page.
- Lock mobile Vaccination Overview to current-drive animal counts only:
  total animals in the visible drive, animals vaccinated/submitted, and animals
  left. This card must not display obligation history, CEO Protocol Adherence,
  shift carry-forward, dose count, or rule-dimension fan-out. Multi-dimension
  vaccine rules such as ET+TT must still count distinct drive animals once.
- Stop frontend drawers from parsing `detail` strings for scope/proof meaning.
- Ensure shed-grain proof/submit state is read from shed-grain facts, not a
  shared parent task state.

### Phase 3: Extract Shared Operational Surface Interfaces

- Define common internal read shapes for work items, summaries, alerts, calendar
  events, and mobile execution rows.
- Move vertical-specific mapping into provider/adapters.
- Keep SQL optimized per vertical, but return common contracted shapes.

### Phase 4: Onboard Weighing As The First Clean Vertical

Weighing is a good next candidate because it is active but not as historically
complex as vaccination.

Implement weighing through the new checklist:

- campaign identity
- animal/session/measurement grains
- proof/evidence expectations
- mobile execution rows
- admin web summary
- Control Tower alerts
- Calendar events if scheduled
- golden fixture

### Phase 5: Onboard Counts, Shifting, Breeding

Apply the same pattern to each vertical. Do not allow custom surface logic until
the vertical has declared its contract and fixture.

## Anti-Patterns To Ban

- Computing whole-drive summaries from paginated rows.
- Adding backend JSON fields without OpenAPI and generated-client changes.
- Using generic `completed` fields without a grain.
- Treating mobile DTOs as hand-maintained secondary contracts.
- Inferring selected drive from rule ID alone.
- Parsing user-visible strings to recover scope or state.
- Letting Calendar, Control Tower, and Protocol Adherence each define their own
  status buckets.
- Allowing optional fallback fields without labeling the fallback grain.

## Definition Of Done

This architecture is working when:

- A new vertical can plug into Calendar, Control Tower, Admin Web, and Android
  by implementing defined read providers.
- Every shared count has an explicit grain.
- Every shared response is generated from OpenAPI into TypeScript and validated
  against Kotlin DTOs.
- One golden fixture can prove all surfaces agree.
- Pagination never changes summary totals.
- Adding mobile writes does not require screen-by-screen number patches.

## Immediate Action Items

### Landed

- `ControlTowerAlert.partition_label` contract drift is fixed across the
  shared contract surfaces.
- Protocol Adherence preserves row pagination while returning whole-result
  summary truth from repository-owned scoped rows.
- Protocol Adherence selected-drive scoping is repository-owned and no longer
  relies on service-side rule/date filtering.
- Control Tower/admin-web render typed backend fields directly instead of
  parsing detail strings or relying on global shared fallback tables.
- Android Calendar, Control Tower, Adherence, and execution DTOs have been
  brought back to the current OpenAPI-owned field shape.

### Pending

1. Add a vaccination golden fixture covering Calendar, Action Center, Protocol
   Adherence, Control Tower, Workflows, Admin Web, Android, reporting, and
   generated clients.
2. Add semantic drift guards for Go/OpenAPI/TypeScript/Kotlin/frontend contract
   agreement. `make operational-read-model-contract-guard` is currently a
   discoverability/static-text guard only.
3. Write the vertical onboarding checklist into the engineering gate.
4. Add a backend-owned whole-result Sheds day/adherence summary so Android does
   not need a "load all rows" fallback.
5. Use weighing as the first module to implement the clean pluggable pattern.
