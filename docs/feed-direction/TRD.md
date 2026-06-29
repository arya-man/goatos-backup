# Feed -> Feed Direction - Technical Requirements / Design (TRD)

**Status:** Draft v2, corrected against committed schema and source review
**Date:** 2026-06-29
**Companion:** [PRD.md](./PRD.md)
**Foundation:** [Generic Protocol & Obligation Engine](../protocol-engine/obligation-engine.md)

> v2 correction: the previous TRD treated a typed Sheet-shaped `feed_*` schema as
> the target. The committed repo moved the other way. Migration
> `000079_feed_direction_module.sql` deliberately reuses the generic kernel and
> adds only `feed_direction_completions` as the feed-specific execution record.
> This TRD ratifies that direction unless a future owner decision explicitly
> reopens the typed-stack design.

## 1. Current committed state

Committed and reusable:

- Counts source-row/projection tables exist, and `goat_location_history` plus
  `movement_commands` exist for per-goat movement history.
- `protocol_definitions`, `protocol_versions`, `protocol_rules`, and
  `protocol_triggers`, including `category='feed_direction'`.
- `protocol.draft.feed_direction` and `protocol.publish.feed_direction`
  capabilities.
- `obligation_instances`, `obligation_batches`, status events, and missed/deadline
  indexes.
- `inventory_items`, `inventory_stock`, and `inventory_stock_movements` ledger.
- `sop_definitions`, `sop_versions`, tasks, submissions, and submission items.
- `feed_direction_completions`, keyed by `tenant_id + obligation_id` and
  `tenant_id + idempotency_key`.
- `backend/internal/feed` service/repository/sqlc code for recording and
  accepting/rejecting feed completion records.

Not operational yet:

- No canonical Counts/Shifting horizon-aware aggregate projection contract for
  shed + breed + stage/tag counts.
- No non-UI RationTable solver or reviewed solver-output import with provenance.
- No feed generation run model or worker.
- No Feed Direction obligation creation.
- No HTTP adapter, route, OpenAPI contract, or generated frontend/mobile client.
- No Pub/Sub consumer or scheduler/sweeper entry point for feed generation.
- No stock reserve/consume call wired from feed execution.
- No stage model for packing, transport, consumption, wastage, rejection, and
  rework against the one-row-per-obligation `feed_direction_completions` table.
- No active admin-web/operator-mobile Feed Direction surface.

## 2. Superseded v1 assumptions

Do not implement these from the previous TRD unless a future owner decision
explicitly reverses the 000079 design:

- `feed_master` as an owning feed catalog separate from generic inventory.
- `feed_pattern`, `feed_pattern_feeds`, `feed_session_templates`, and
  `feed_session_template_items` as the primary config store.
- `feed_directions`, `feed_packing`, `feed_consumption`, and `feed_transport` as a
  parallel execution stack.
- Migration numbers `000076`, `000077`, or `000078`; the repo is already beyond
  those numbers.
- "v1 midnight / v2 2 PM full supersede" as canonical timing.

The only feed-specific tables still allowed by this design are those the generic
kernel cannot represent cleanly: generation run records, source/planning
snapshots, bridge-event records, and read-model/projection rows. Execution,
proof, status, stock, audit, reminders, and escalations stay in the generic
systems with `feed_direction_completions` as the module completion row.

## 3. Source-backed data model

### Count and shifting input

The source-required count model is a replayable aggregate ledger, not a Sheet
row:

```text
physical BaseCount anchor + realized ShiftingEvent ledger
  -> today's count / count_as_of(now)
  -> tomorrow projected count, one-day horizon only,
     including authorized future-effective ShiftingEvents for the target date
```

Counting DB rows are source evidence and migration/fixture material. They must
not become the GoatOS runtime truth. Today, counting is aggregate by shed and
breed; RFID-to-shed association is planned but not implemented.

Repo compatibility is a blocker here. The committed Counts module currently has
`counts_source_rows`, `counts_sync_runs`, and `counts_projection_*` tables for
source sync/projections, while committed movement state is per-goat
`goat_location_history` plus `movement_commands`. It does not yet contain a
`base_count` table or aggregate ShiftingEvent table at the grain Feed Direction
needs. A Feed implementation must therefore first choose and implement one of
these contracts:

1. A Counts/Shifting-owned aggregate ledger/projection at
   tenant + park + shed + breed + stage/tag + effective time.
2. A derivation from per-goat location history, only after RFID-to-shed
   association is reliable enough to produce the same aggregate counts.

Until one contract exists, Feed generation must remain blocked even if protocol
rules, obligations, and completion records exist.

A new physical Base Count becomes canonical immediately. Any discrepancy against
the prior replay creates investigation/accountability work, but that work does
not gate adoption of the physical count as the new ledger anchor for subsequent
feed runs.

The count contract must split consumers by horizon. Realized count
(`count_as_of(now)`) and post-facto reconciliation accept shifting events only
after they reach the configured applied state. The event record must carry
category, independent priority, source/destination, raised/effective time,
authorization state where required, completion/proof references, and verification
state. Events that are pending, rejected, canceled, unauthorized, or missing
required applied-state proof/verification remain process-visible and may drive
reminders/escalations, but they must not enter realized count.

Forward projection (`projected_count_for(target_date)`) may include
authorized/directed future-effective ShiftingEvents with `effective_at` inside
the target date before physical completion/proof. Completion, proof, and
verification still gate realization, reconciliation, exceptions, and rework when
the target date arrives; they do not gate the initial one-day projection input.

The shifting event must also carry structured cohort/stage impact. Legacy K0
handling inferred Mother vs Kid from comments; GoatOS must not use free-text
comments as policy truth for feed counts or ration selection.

### Ration key

Do not key ration rules by raw `(breed, age)`.

- Adult rules are keyed by `breed + shed_tag/stage`.
- Kid rules are keyed by weight band and target ADG.
- Source `Age`, `Shed Tag`, and legacy aliases must normalize through reference
  data before they reach `rule_dsl`.

### Feed eligibility and transforms

Feed eligibility must be explicit in `rule_dsl` or referenced source-backed
config:

- K0/K1 milk-fed cohorts are excluded from normal packed-feed directions.
- Experiment sheds are zero-direction / special-tag exclusions unless a reviewed
  Feed policy explicitly provides a normal feed path.
- F2/Fattening, shed-tag aliases, and other legacy labels normalize before count
  matching and ration lookup.
- Non-baking-soda feed quantities round up to the source-backed packing unit
  rule; baking-soda precision remains a separate source-backed transform.

### Feed protocol rule_dsl

Feed configuration lives in `protocol_versions.rule_dsl` for
`category='feed_direction'`. The DSL should capture:

- Ration scope: tenant, park/shed scope, breed, stage/shed tag, or kid
  weight-band/ADG.
- Feed eligibility rules and zero-direction exclusions.
- Feed item reference to `inventory_items.category='feed'`.
- As-fed quantity and unit.
- Session policy, initially two sessions with 50/50 split unless source-backed
  config changes it. Legacy per-farm session/feed-set templates are evidence to
  review before implementation.
- Proof policy for packing, transport, consumption, wastage, and verification.
- Inventory policy: reserve at packing start, consume/release on accepted
  packing verification.
- Source metadata and review status for imported or manually entered rules.

Typed validators/reference tables are allowed when they protect correctness
(for example stage aliases, feed item nutrient vectors, or optimizer outputs),
but they must not become a second config authority parallel to the protocol
engine.

### RationTable provenance

The full NRC optimizer UI is not required for the first slice, but source-backed
solver provenance is required before Feed Direction can publish quantities. The
implementation must choose one of two acceptable paths:

1. Run a non-UI RationTable solver that enforces the source-required
   feed-type-level constraints: hard floor/ceiling, structural ratio, category
   floor, and quantity floor. Item-level feed ceilings remain deferred until
   explicitly scoped.
2. Import reviewed solver outputs as source-backed `feed_direction` protocol
   rules.

Both paths must persist or attach enough metadata for review and replay:
solver/import version, source document/version, feed item nutrient vectors,
feed costs, available feed-type set, constraint set, output hash, reviewer,
review timestamp, effective date, and re-solve/review trigger. A change to feed
costs or available feed types must create a new reviewed solve/import before the
refreshed lookup replaces the prior RationTable output.

## 4. Minimal new persistence

Use migration numbers after the current tail. At the time of this correction,
the migration list reaches `000117`.

Candidate tables, to finalize during implementation:

| Table | Purpose | Notes |
| --- | --- | --- |
| `feed_direction_generation_runs` | One row per full or Diff generation attempt | `run_kind` full/diff, target date, cutoff window, status, idempotency key, source hash, actor/job metadata |
| `feed_direction_count_input_rows` | Snapshot of the Counts/Shifting projection consumed by a run | Grain: tenant, run, park, shed, target date, breed, stage/shed tag, headcount, source contract/version/hash, realized vs projection horizon |
| `feed_direction_generation_rows` | Source/planning snapshot used to create obligations | Grain: tenant, run, park, shed, target date, session, breed, stage/shed tag, feed item, as-fed quantity, row kind full/diff/restatement |
| `feed_direction_stage_records` | Typed stage outcome rows if separate stage obligations are not enough | Stage kind packing/transport/consumption/wastage/bridge, planned vs actual quantities, consumed/wasted/variance, proof refs, verifier status, rejection reason, rework link |
| `feed_direction_bridge_events` | High-priority post-cutoff 2x-ration bridge log | Destination shed, animal or aggregate count, source shed tag, feed quantities, proof/submission links |
| `feed_direction_projection_rows` | Read model for admin/mobile lists | Bounded by tenant, park, date, shed, session, status, cursor key |

If the implementation can derive a read model directly from generic obligations,
completions, inventory, and run rows without extra projection storage, prefer the
smaller design. Do not add typed tables only to mirror the legacy Google Sheet.

## 5. Generation pipeline

### Full direction

```text
Day N configured publish time, recommended default 09:00
  -> load published feed_direction protocol version
  -> consume the horizon-aware Counts/Shifting projection for tomorrow
  -> apply feed eligibility exclusions and source-backed transforms
  -> calculate shed/session/feed quantities
  -> insert generation run + snapshot rows
  -> create obligation_batches / obligation_instances at shed-session-feed grain
  -> write outbox events for tasks, notifications, projections
```

### Diff

```text
Day N cutoff, recommended default 13:30
  -> collect eligible post-run shiftings before or at cutoff
     under the projection policy
  -> calculate full restatement rows for affected sheds only
  -> insert Diff run + snapshot rows
  -> explicitly cancel/supersede affected stale open obligations
  -> create replacement obligations for the affected shed/session/feed rows
  -> write outbox events
```

Diff is not a full all-shed v2 restatement and it is not a bare numeric delta.
It is a full restatement of the affected shed/session/feed rows only. Idempotency
keys must prevent duplicate rows, but idempotency alone is not enough: stale open
obligations must be canceled or superseded explicitly, otherwise old and new
instructions can both remain live.

For Diff, "eligible" follows the same horizon split as the full direction input:
projection Diffs may include authorized/directed future-effective shiftings for
the target date, while realized-count corrections require the configured applied
state.

<a id="feed-direction-cancel-open-obligations-helper"></a>

Mirror the vaccination cancellation precedent
`CancelOpenVaccinationObligationsForGoatExceptVersions` in
`backend/internal/obligation/adapters/postgres/repository.go` when implementing a
feed-specific cancel/rebuild helper. The trap to avoid: obligation and
completion idempotency keys are replay guards for the new write. A key that
varies by direction/run/version will make `ON CONFLICT DO NOTHING` a no-op for
the new row but will not touch the stale open obligation, which can leave old
instructions live and later double-reserve stock.

### Bridge

High-priority additions after the cutoff use the manual bridge protocol:

```text
priority=High AND raised_at > cutoff AND destination_shed added animals
  -> health/feed team places 2x daily ration at destination shed
  -> proof captured through SOP path
  -> bridge event logged against destination shed/feed record
  -> no source-shed claw-back
  -> normal Day N+2 full direction absorbs the change
```

Do not implement the superseded 07:30 next-morning Diff design.

<a id="feed-direction-inventory-app-anchors"></a>

## 6. Obligation, stage, and inventory behavior

- Target grain is shed/session/feed/cohort, never one task per goat.
- Stock is not reserved at generation.
- Packing task start reserves feed through the generic inventory app
  `ReserveForBatch`, never by mutating balances directly.
- Accepted packing verification consumes actual packed quantity through
  `ConsumeForBatch` and releases the remainder through the same inventory
  movement/release path.
- Stock-out at packing creates blocked/escalation state through obligations and
  read models; do not bare-decrement inventory.
- Missed/overdue state is computed by the sweeper/deadline kernel, not a feed
  flag column.
- All writes must be tenant-scoped and idempotent.

### Stage model

The ratified `feed_direction_completions` table is keyed one row per
`obligation_id`. Therefore the first implementation must not pretend one
completion row can hold every legacy stage for the same shed/session direction.
Choose one model before build:

1. Preferred: create separate child obligations under the shed/session/feed batch
   for `packing`, `transport`, and `consumption_wastage`; each child can have its
   own SOP submission/proof/verification and its own completion row. Each child
   must receive a distinct `obligation_id`, so it satisfies the existing
   `UNIQUE(tenant_id, obligation_id)` completion constraint instead of competing
   for the parent obligation's row.
2. Alternative: keep one direction obligation and add typed
   `feed_direction_stage_records` for each stage.
3. Narrow fallback: use `feed_direction_completions` for packing close only and
   explicitly defer transport/consumption/wastage persistence. This is not a
   shippable Feed Direction parity slice.

Packing shortfall or rejected packing proof must re-open/re-issue the packing
stage, delete or supersede stale notification/task pointers, and create
rework/escalation state. Do not consume inventory on rejected proof.

### Transport execution stage

Transport must be modeled as a first-class Feed execution obligation/stage. The
legacy source has a separate Feed Transport form, transport processed flag,
Slack list item creation, file upload handling, verifier `Pending`/`Verified`/
`Rejected` state, required rejection remarks, Slack notification, and rejected
media tracking. GoatOS maps that to:

- grouped transport shed semantics tied to the generated direction date/session
  or configured transport batch;
- proof upload/submission records with media metadata and uploader/actor;
- verification status with reviewer, timestamp, rejection reason, and audit;
- rework or next-action obligation when proof is rejected;
- replacement of `Transport Processed` with idempotent task/proof/completion
  state, not a Boolean source of truth.

## 7. API and worker wiring

The first implementation slice is generation + wiring:

1. Scheduler/job entry point for full generation and Diff.
2. Consumer path for shifting/count events that invalidate or enqueue Feed
   Direction work.
3. Feed generation service that writes runs/snapshots and creates obligations.
4. HTTP routes for generation status, direction lists, execution/history, and
   verification.
5. OpenAPI update and generated admin/mobile clients.
6. Reserve/consume integration in feed execution.
7. Projection/read APIs with cursor pagination and bounded filters.

All public contracts must keep labels, filter options, status copy, disabled
reasons, and page-size choices backend-owned.

## 8. Legacy parity mapping

Legacy source behavior to port as rules, not as Apps Script:

| Source surface | Source signals | GoatOS mapping |
| --- | --- | --- |
| Count-DB / Projected-DB / FutureDB | Date, farm, shed, shed tag, breed, age, count, counted-by; projected and comparison tabs | Source evidence and fixture/reference shape only. Runtime feed counts come from the physical Base Count anchor, realized ShiftingEvent ledger, and horizon-aware projection contract. See `context/source-findings/feed-direction-counting-db-reconstruction.md`. |
| Shifting reports | Type request/direction, category, priority, source/destination, comments, destination proof, approval/status | Process-visible movement workflow plus count input under horizon-specific gates: authorized/directed future-effective rows may feed the one-day projection, while realized counts wait for the configured applied state. Pending unauthorized, rejected, or canceled movement does not create Feed Direction work. See `context/source-findings/drive-docs-findings.md`. |
| Feed Direction sheet | Date, farm, session, shed, shed tag, breed, age, count, feed item/quantity pairs, session total | Generation snapshot/read-model fields, with quantities derived from reviewed RationTable output and count replay. |
| Packing / Consumption / Transport processed flags | Boolean processed columns in the legacy sheet | Idempotent obligation, SOP task, proof, verification, and completion state; never boolean runtime authority. |
| Feed Transport form and transport scripts | Date, farm, shed, time, video/message links, user, transport list item, uploaded file handling | First-class transport obligation/stage with grouped shed semantics, proof upload, verifier decision, rejection reason, and rework/next action. |
| Feed Consumption & Wastage form | Consumed quantity, wasted quantity, consumption proof, wastage proof, difference, wastage percent | Typed consumption/wastage stage outcome or projection fields with proof references. |
| Video verification / rejection tracker | Media correctness `Pending`/`Verified`/`Rejected`, remarks required on rejection, notification and rejected-media list | Platform proof verification state, audit trail, rejection reason, and follow-up obligation. |
| Packing quantity re-check | Expected vs actual quantities, proof rejection, row reset/re-send loop | Packing shortfall/rejected proof must create rework/re-issue state before inventory consumption. |
| Experiment sheds | Experiment tag and zero count/feed quantities | Explicit feed eligibility exclusion / zero-direction rule. |
| K0/K1 feed exclusions | Milk-fed cohorts filtered out of supply/diff paths | Explicit eligibility rule with source-backed rationale, not an incidental transform. |
| Per-farm Template sheet | Session labels and feed items per farm; quantities split by session count | Open parity decision: retain as source-backed session/feed-set config or supersede with the June 2026 two-session 50/50 default. |
| Diff regeneration | Delete affected shed rows and regenerate affected shed directions | Diff means full affected-shed restatement, not numeric delta only. |

Other parity rules:

- Session split is currently 50/50 by source priority, but legacy per-farm
  templates must be reviewed before implementation.
- Legacy feed rounding rules, K0/K1 exclusions, and F2/Fattening alias handling
  must be captured as explicit source-backed transform rules before use.
- Slack delivery is notification/cutover bridge only. It must call GoatOS APIs if
  retained; it must not mutate Sheets as canonical state.

Security note: the legacy Slack/App Script repo contains committed Slack token
and shared-secret material. Do not copy it into GoatOS docs, code, tests, or
fixtures. Rotation in the source system is required before any bridge reuse.

## 9. Admin-web and mobile constraints

- Feed Direction operational UI remains gated by the admin-web scope lock.
- Current Config may keep internal Feed DSL code paths only if the backend page
  contract does not expose `feed_direction` as a visible category.
- The current Feed mock is static and stale on timing/nav labels; it does not
  show pagination, filters, row drawers, or column controls for Feed Direction.
- When scope reopens, update the mock and `check-mock-fidelity` scan paths in
  the same UI slice.
- Operator-mobile should execute SOP/proof/offline queue through generated APIs;
  it must not access Sheets, Slack, GCS, Firestore, BigQuery, or Postgres
  directly.

## 10. Scale requirements

- Use tenant, park, date, shed, session, status, owner, and cursor keys for every
  hot read.
- No unbounded raw scans from admin-web/mobile.
- Workers lease bounded chunks by tenant/park/date/run.
- Generation rows/projections must support replay and rebuild from canonical
  events plus source evidence hashes.
- Validate SQL plans for the list and generation queries.
- Load tests must include skew: many sheds, repeated Diff runs, and large
  historical count ledgers.

## 11. Acceptance gates

- Unit tests for ration key normalization and Diff/bridge decision tables.
- Unit tests for feed eligibility exclusions, rounding transforms, and
  structured shifting cohort impact.
- Integration tests for Counts/Shifting input snapshots, generation idempotency,
  affected-shed restatement, explicit stale-obligation cancel,
  reserve/consume/release, stage proof verification, and packing rework.
- API route tests and OpenAPI generated-client checks.
- SQL plan validation for hot list/filter queries.
- Local end-to-end proof through Postgres, API, outbox/consumer or documented
  local equivalent, sweeper/job, SOP submission, verification, and read model.
- UI visual smoke only after scope reopens and the mock is updated or explicitly
  superseded for stale Feed labels.

## 12. Blockers before build

1. Owner confirms Feed Direction scope is reopened beyond the current
   PHC/Vaccination review slice.
2. Counts/Shifting owner provides the aggregate horizon-aware projection contract
   or approved per-goat derivation path.
3. Feed Director confirms clock values per park.
4. Source-backed ration values, aliases, eligibility exclusions, and rounding
   rules are available.
5. Stage model for packing, transport, consumption, wastage, proof rejection,
   and rework is chosen.
6. Legacy Slack token/shared-secret rotation ownership is assigned and rotation
   happens before any bridge reuse.
7. Next migration number is chosen after checking the live repo tail.
