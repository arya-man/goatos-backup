# Feed -> Feed Direction - Technical Requirements / Design (TRD)

**Status:** Draft v5, refined against source-first counter-review, read-model review, and dependency-closure audit
**Date:** 2026-06-30
**Companion:** [PRD.md](./PRD.md)
**Foundation:** [Generic Protocol & Obligation Engine](../protocol-engine/obligation-engine.md)
**Readiness phase:** [Dependency Closure PRD](./DEPENDENCY-CLOSURE-PRD.md) and
[Dependency Closure TRD](./DEPENDENCY-CLOSURE-TRD.md); Counts/Shifting long pole:
[Counts/Shifting Closure PRD](./COUNTS-SHIFTING-CLOSURE-PRD.md) and
[Counts/Shifting Closure TRD](./COUNTS-SHIFTING-CLOSURE-TRD.md)
**Build-to-done charter:** [Feed Direction Build-To-Done Goal](./BUILD-TO-DONE-GOAL.md)

> v2 correction: the previous TRD treated a typed Sheet-shaped `feed_*` schema as
> the target. The committed repo moved the other way. Migration
> `000079_feed_direction_module.sql` deliberately reuses the generic kernel and
> adds only `feed_direction_completions` as the feed-specific execution record.
> This TRD ratifies that direction unless a future owner decision explicitly
> reopens the typed-stack design.

Source authority rule: `Feed, Shiftings and Count.docx` v1.1 is the controlling
Feed Direction business source for timing, Diff, bridge, one-day projection,
as-fed quantities, and ration constraints. Legacy Slack/App Script trigger
timings are audit/cutover evidence only. They must not become GoatOS schedules
unless the Feed Director explicitly approves a retained or replaced behavior
against that docx.

Workbook boundary rule: `Counting DB - values only.xlsx` and
`Feed Directions Automation DB.xlsx` are source evidence for import mapping,
parity fixtures, formula-gap review, and legacy execution-stage inventory. Their
tabs, formulas, hidden copies, processed flags, Apps Script properties, and
Slack media links are not target schema. GoatOS must land the same operational
intent in typed Postgres state, governed protocol/config CRUD, typed imports,
review/publish gates, immutable generation snapshots, stage obligations,
proof/rework, audit/outbox, and bounded read models.

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
  shed + breed counts plus reviewed ration-context resolution state.
- No non-UI RationTable solver or reviewed solver-output import with provenance.
- No feed generation run model or worker.
- No Feed Direction obligation creation.
- No HTTP adapter, route, OpenAPI contract, or generated frontend/mobile client.
- No Pub/Sub consumer or scheduler/sweeper entry point for feed generation.
- No stock reserve/consume call wired from feed execution.
- No stage model for packing, transport, consumption, wastage, rejection, and
  rework against the one-row-per-obligation `feed_direction_completions` table.
- No feed-specific reminder/escalation SLA, NotificationGateway routing,
  missed/recovery events, audit/observability contract, or command-lens field
  mapping.
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

1. A Counts/Shifting-owned aggregate realized ledger plus horizon-aware
   projection at tenant + park + shed + breed + effective time, with reviewed
   ration context resolved from reviewed source-backed context or returned as a
   fail-closed blocker.
2. A derivation from per-goat location history, only after RFID-to-shed
   association through `goat_identifiers` and `goat_location_history` is reliable
   enough to produce the same aggregate counts without full-herd scans.

The initial Feed Direction build must take the aggregate-ledger path. The
per-goat derivation remains a future replacement option only after RFID-to-shed
association is separately implemented, confidence-gated, scale-tested, and
owner-approved.

Until one contract exists, Feed generation must remain blocked even if protocol
rules, obligations, and completion records exist.

Keep physical count grain and nutrition key separate. The Feed Transfer KT
supports uploaded constraint tables keyed by breed, tag/stage, energy/feed
vectors, weight bands, warm-up, pregnancy, and related nutrition dimensions, but
it does not provide authoritative shed placement. Therefore:

- `CountProjectionProvider` owns physical count rows at shed + breed + horizon.
- Feed protocol/ration config owns the reviewed nutrition cohort key.
- A resolver must map each projected count row to one or more approved ration
  cohort contexts before generation.
- Missing shed tag, cohort split, or ration-context evidence is a generation
  blocker and process exception, not a default-ration fallback.

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
handling inferred Mother vs Kid from comments and halted on unresolved K0
outflows. GoatOS must not use free-text comments as policy truth for feed counts
or ration selection. If cohort/stage impact is absent, unresolved, or
free-text-only, the event is excluded from realized count and
`projected_count_for(target_date)`, and the Counts/Shifting module raises a
durable process exception for owner resolution.

The realized ShiftingEvent ledger must apply each event idempotently by
`shifting_event_id` or an equivalent source-independent logical event key.
Redelivery/replay must not double-apply a movement into aggregate counts.

### Ration key

Do not key ration rules by raw `(breed, age)`.

- Adult rules are keyed by `breed + shed_tag/stage`.
- Kid rules are keyed by weight band and target ADG.
- Source `Age`, `Shed Tag`, and legacy aliases must normalize through reference
  data before they reach `rule_dsl`.
- Uploaded constraint tables may not carry shed placement. Treat them as ration
  cohort config only; the shed/breed count row still needs reviewed resolver
  evidence before lookup.

### Feed eligibility and transforms

Feed eligibility must be explicit in `rule_dsl` or referenced source-backed
config:

- Warmup stage tags require an explicit reviewed ration path or an explicit
  exclusion before Feed Direction publishes quantities, including the 14-day
  Warmup transition.
- ICU, Quarantine, Flushing, and Breeding shed tags require explicit ration path,
  exclusion, or process-exception policy before they affect packed-feed output.
- K0/K1 milk-fed cohort exclusions and Experiment zero-direction behavior are
  candidate policy from legacy automation/zero rows until Feed Director approval.
- F2/Fattening, SIROHI->Beetal where approved, shed-tag aliases, and other legacy
  labels normalize before count matching and ration lookup.
- Non-baking-soda feed quantities round up to the source-backed packing unit at
  the per-shed daily total per feed item before session split. Baking-soda
  precision remains a separate source-backed transform and zero baking-soda
  quantity emits no direction row/cell. If baking soda requires sub-gram
  precision, integer gram base units cannot be used for that item without
  widening the inventory app port or defining a smaller base unit.

### Feed protocol rule_dsl

Feed configuration lives in `protocol_versions.rule_dsl` for
`category='feed_direction'`. The DSL should capture:

- Ration scope: tenant, park/shed scope, breed, stage/shed tag, or kid
  weight-band/ADG.
- Ration context resolver policy: how a shed + breed projection row resolves to
  one or more approved nutrition/ration cohort keys, and what blocker is emitted
  when context is missing.
- Warmup stage policy and approved alias/exclusion handling.
- Feed eligibility rules and zero-direction exclusions.
- Feed item reference to `inventory_items.category='feed'`.
- As-fed quantity and unit.
- Session policy. The June 2026 source default is two sessions with a 50/50 split,
  and the source itself flags that split as a deliberate simplification to revisit
  if a breed + tag combo needs an uneven split. Model this as versioned admin
  config, not code constants: session slot code/label, serving time, sort order,
  active/effective range, split weights by scope/feed item where needed, and
  optional per-slot feed-item inclusion. The default published policy is Session 1
  `09:00` weight `0.5` and Session 2 `15:00` weight `0.5`; Feed Director draft
  plus COO/CEO publish can add, disable, reorder, or reweight slots in a new
  effective-dated protocol version.
- Proof policy for packing, transport, consumption, wastage, and verification.
- Inventory policy: reserve at packing start, consume/release on accepted
  packing verification.
- Source metadata and review status for imported or manually entered rules.

Workbook-derived config must enter the DSL through typed import or admin CRUD,
not through live spreadsheet formulas. The importer/config surface must cover at
least feed item nutrient vectors, feed costs, feed-type constraints,
breed/species aliases, stage/tag aliases, kid weight-band/ADG rules,
warm-up/pregnancy policy, eligibility/exclusions, session slots, feed-set
templates, transport maps, proof thresholds, and reviewed ration solver/import
outputs. Each family needs source checksum, row-level validation, dry-run parity
preview where a workbook source exists, dead-letter/repair state, reviewer,
approval metadata, and business audit.

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
review timestamp, review_status, approved_by, approved_at, effective date, and
re-solve/review trigger. A change to feed costs or available feed types must
create a new reviewed solve/import before the refreshed lookup replaces the
prior RationTable output.

The protocol publish gate must reject Feed Direction ration rules unless
`review_status='approved'`, approval metadata and source hashes are present, and
the publisher has `protocol.publish.feed_direction` or the approved service
equivalent. "Reviewed import" alone is not enough for live obligations.

Ration solver limits are hard scope, not implementation blanks. The first build
supports feed-type-level constraints only. Item-level feed ceilings and
palatability modeling are deferred. The `60:40` structural ratio applies only to
Milking and Fattening tags; roughage/category floor numbers are examples until
confirmed per tag. Do not add a paired overshoot ceiling; source logic relies on
cost minimization. When feed costs or feed-type availability changes, the
reviewed re-solve replaces the RationTable output wholesale, with no versioned
blend of old and new rows.

Do not encode workbook tab names as product modules or SQL table names. Names
such as `CPT Validation`, `CBE Validation`, `Feed-Energy-Protein`,
`Supply Planning`, `Template`, `Feed Packing Form`, `Feed Transport Form`, and
`Feed Consumption & Wastage` are import/parity labels only. The canonical model
is protocol config plus generation, obligation, proof, inventory, audit, and
read-model state.

## 4. Minimal new persistence

Use migration numbers after the current tail. At the time of this correction,
the migration list reaches `000117`.

Candidate tables, to finalize during implementation:

| Table | Purpose | Notes |
| --- | --- | --- |
| `feed_direction_generation_runs` | One row per full or Diff generation attempt | `run_kind` full/diff, target date, cutoff window, status, idempotency key, source hash, actor/job metadata |
| `feed_direction_count_input_rows` | Snapshot of the Counts/Shifting projection consumed by a run | Grain: tenant, run, park, shed, target date, breed, headcount, ration_context_resolution_state, reviewed ration context ids where resolved, blocker reason where unresolved, source contract/version/hash, realized vs projection horizon |
| `feed_direction_generation_rows` | Source/planning snapshot used to create obligations | Grain: tenant, run, park, shed, target date, session, breed, ration cohort key/stage/shed tag, feed item, as-fed quantity, row kind full/diff/restatement, optional source-facing net correction quantity |
| `feed_direction_import_batches` | Optional typed import/review batch storage for workbook or solver-output migration | Add only if protocol source metadata is not enough for row repair, DLQ, parity preview, and audit |
| `feed_direction_stage_records` | Typed stage outcome rows, or replaced only by an indexed obligation-context/completion-stage projection with the same durable discriminator | Stage kind packing/transport/consumption/wastage/bridge, planned vs actual quantities, consumed/wasted/variance, proof refs, verifier status, rejection reason, rework link |
| `feed_direction_bridge_events` | Manual high-priority post-cutoff 2x-ration bridge log | Destination shed, animal id or approved aggregate reference, timestamp, quantity, source event/logical shifting reference where known, source shed tag where known, proof/submission links, reconciliation state |
| `feed_direction_projection_rows` | Read model for admin/mobile lists and command buckets | Bounded by tenant, park, date, shed, session, status, bucket, owner, cursor key |

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
  -> load and validate target-date session slots and split weights
  -> calculate shed/session/feed quantities for every active slot
  -> insert generation run + snapshot rows
  -> create obligation_batches / obligation_instances at shed-session-feed grain
  -> write outbox events for tasks, notifications, projections
```

### Diff

```text
Day N cutoff, recommended default 13:30
  -> collect eligible post-run shiftings before or at cutoff
     under the projection policy
  -> calculate affected-shed/session/feed restatement rows using the same
     target-date session policy and optional net correction output
  -> insert Diff run + snapshot rows
  -> explicitly cancel/supersede affected stale open obligations
  -> create replacement obligations for the affected shed/session/feed rows
  -> write outbox events
```

Diff has two layers. The June source and operator language describe Diff as the
net correction needed for affected shed/session rows. GoatOS may emit that
source-facing delta for field adjustment, but its canonical generation run must
store affected-shed/session/feed restatement rows and supersede stale work.
Persisting only a bare numeric delta as runtime authority is not enough because
the system must know the one active instruction after correction. Idempotency
keys must prevent duplicate rows, but idempotency alone is not enough: stale open
obligations must be canceled or superseded explicitly, otherwise old and new
instructions can both remain live.

For Diff, "eligible" follows the same horizon split as the full direction input:
projection Diffs may include authorized/directed future-effective shiftings for
the target date, while realized-count corrections require the configured applied
state.

<a id="feed-direction-cancel-open-obligations-helper"></a>

Mirror the vaccination cancellation precedents
`CancelOpenVaccinationObligationsForGoatExceptVersions` in
`backend/internal/obligation/adapters/postgres/repository.go` when implementing a
feed-specific cancel/rebuild helper. Also review the per-version eligibility-drop
helper in the same repository area when Feed rule/version changes cancel already
materialized work. The trap to avoid: obligation and completion idempotency keys
are replay guards for the new write. A key that varies by direction/run/version
will make `ON CONFLICT DO NOTHING` a no-op for the new row but will not touch the
stale open obligation, which can leave old instructions live and later
double-reserve stock.

### Bridge

High-priority additions after the cutoff use the manual bridge protocol:

```text
priority=High AND raised_at > cutoff AND destination_shed added animals
  -> health/feed team places 2x daily ration at destination shed
  -> proof captured through SOP path
  -> manual bridge log recorded against destination shed/feed record
  -> no source-shed claw-back
  -> normal Day N+2 full direction absorbs the change
```

Do not implement the superseded 07:30 next-morning Diff design. Bridge rows are
logs of a manual video-confirmed SOP only. They must not generate source-shed
claw-back, a bridge Diff, or any extra system-mediated instruction outside the
normal Day N+2 catch-up direction.

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
Choose one model before build. In every model, `stage_kind` is a durable
queryable discriminator, not a display label:

1. Preferred: create separate `obligation_instances` grouped by the
   shed/session/feed `obligation_batch` for `packing`, `transport`, and
   `consumption_wastage`; each stage obligation can have its own SOP
   submission/proof/verification and its own completion row. Each stage must
   receive a distinct `obligation_id`, so it satisfies the existing
   `UNIQUE(tenant_id, obligation_id)` completion constraint instead of competing
   for another stage's row. The obligation context or indexed projection must
   carry `stage_kind`. Do not imply a parent-child obligation FK unless a future
   migration adds one.
2. Alternative: keep one direction obligation and add typed
   `feed_direction_stage_records` for each stage.
3. Narrow fallback: use `feed_direction_completions` for packing close only and
   explicitly defer transport/consumption/wastage persistence. This is not a
   shippable Feed Direction parity slice.

The current `feed_direction_completions` table has no `stage_kind`, so it cannot
serve execution-bucket queries by itself. Before API buckets are built, stage
identity must live in a typed Feed stage table, obligation context with an
indexed projection, or a first-class completion/stage record.

Packing shortfall or rejected packing proof must re-open/re-issue the packing
stage, delete or supersede stale notification/task pointers, and create
rework/escalation state. Do not consume inventory on rejected proof. This is a
GoatOS process-integrity requirement that formalizes useful legacy evidence:
legacy automation has discrepancy red-flag/admin-alert paths and a separate
video-verification packing quantity reset/re-send loop. GoatOS maps that to typed
stage rework/reissue and idempotent obligations, not Sheet flag resets or thread
deletion.

### Transport execution stage

Transport must be modeled as a first-class Feed execution obligation/stage. The
legacy source has a separate Feed Transport form, transport processed flag,
Slack list item creation, file upload handling, verifier `Pending`/`Verified`/
`Rejected` state, required rejection remarks, Slack notification, and rejected
media tracking. GoatOS maps that to:

- source-backed transport consolidation config that maps direction sheds to the
  smaller transport-shed work list, including fully consolidated and
  part-consolidated shed-name cases;
- proof upload/submission records with media metadata and uploader/actor;
- verification status with reviewer, timestamp, rejection reason, and audit;
- rework or next-action obligation when proof is rejected;
- replacement of `Transport Processed` with idempotent task/proof/completion
  state, not a Boolean source of truth.

### Consumption, wastage, and discrepancy exceptions

Consumption/wastage stage records must preserve planned packed quantity, actual
consumed quantity, actual wasted quantity, computed difference, wastage percent,
proof links, verifier status, and rejection/rework state. Legacy evidence
highlighted a packing expected-vs-actual discrepancy and flagged wastage above
20%. Treat those as source-backed default exception rules pending Feed Director
confirmation, not as UI coloring only.

Cross-stage comparisons must use the active/superseded-aware Feed Direction row
or obligation snapshot for the same tenant, park, shed, session, feed item, and
target date. A superseded direction must not be used as the expected quantity for
new proof unless the proof was already captured against that older instruction.

## 7. Legacy import, replay, and cutover contract

Feed Direction cutover follows the shared legacy-to-canonical contract in
`../features/cutover-contract.md` and the PHC/Feed migration policy in
`../protocol-engine/migration-and-cutover.md`: legacy Sheets/App Script rows are
archived source evidence and fixture/parity material, not runtime truth.

The implementation plan must declare a Feed-specific import/replay map before
Slack/Sheets is replaced:

| Legacy surface | Import target | Idempotency / dedupe key | Replay behavior |
| --- | --- | --- | --- |
| Feed Direction rows | `feed_direction_generation_runs` + `feed_direction_generation_rows` or audit-only source archive | tenant + source sheet row id/checksum, plus tenant + target date + park + shed + session + feed item + run kind | Replays update the archived source/checksum or no-op; canonical obligations are generated only from approved GoatOS runs |
| Packing processed state and media | SOP submission/proof/completion records or audit-only history | tenant + source row id/checksum + logical stage key | Imported accepted history may close matching canonical work only when coverage is approved; otherwise stays audit-only |
| Feed Transport rows/list items | transport stage obligation/record with consolidation map version | tenant + target date + transport shed + session/batch + source row id/checksum | Replays do not create duplicate transport tasks; rejected media remains rejection/rework evidence |
| Consumption/wastage rows | consumption/wastage stage records or audit-only history | tenant + target date + park + shed + session + feed item + source row id/checksum | Recompute variance from the active instruction snapshot; mark conflicts for review |
| Counting DB / projected count rows | source archive and sanitized fixture/parity rows | tenant + source tab + row id/checksum + snapshot date/grain | Never serve as runtime count authority; compare to canonical base-count + ShiftingEvent projection during shadow parity |
| Applied shifting ledger | Counts/Shifting canonical event ledger or source archive | tenant + shifting_event_id/logical event key | Replays are idempotent and must not double-apply counts |

During overlap, Slack/App Script may notify or bridge only by calling GoatOS APIs.
It must not mutate Sheets as the canonical execution path. Removing legacy source
requires coverage-grain approval, cross-source dedupe tests, and a shadow parity
artifact with no unexplained deltas for required Feed sections.

## 8. API and worker wiring

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

### Command/read-model contract

Do not reduce Feed Direction reads to "all rows plus status." The backend contract
must expose source-backed operational buckets that match the daily work shape
without creating active Feed UI before scope reopens:

| Bucket | Source/legacy signal | GoatOS source of truth |
| --- | --- | --- |
| `generation_blocked` | missing Count/Shifting projection or reviewed ration config | generation run / blocker records |
| `packing_due` | unprocessed packing work | due packing stage obligation or stage record |
| `packing_shortfall` | expected-vs-actual mismatch or rejected packing proof | packing verification + rework state |
| `proof_missing` | no media/proof for required stage | SOP task/submission/proof state |
| `transport_pending` | Feed Transport work list/form row pending | transport obligation/stage and consolidation map version |
| `transport_rejected` | Feed Transport media correctness `Rejected` with remarks | proof verification rejection and rework obligation |
| `consumption_incomplete` | missing consumption/wastage report | consumption/wastage obligation or stage record |
| `wastage_exception` | difference/wastage percent over configured threshold | active instruction snapshot + stage variance rule |
| `bridge_exception` | post-cutoff high-priority 2x ration bridge | `feed_direction_bridge_events` |
| `stock_out` | reserve failure at packing start | inventory reserve result + blocked obligation state |
| `missed_or_overdue` | due/deadline crossed for any stage | obligation deadline/missed event + escalation state |
| `escalated` | reminder/escalation policy fired | notification/escalation event + owner |
| `rework` | rejected media or GoatOS-required packing/transport/consumption rework | follow-up obligation/workflow link |

Each bucket must carry `tenant_id`, park/location scope, target date, shed or
transport shed, session/batch where applicable, owner/assignee where known,
`stage_kind`, active/superseded instruction reference, due_at, deadline_at,
proof state, verification state, evidence refs, due/blocked reason, escalation
state, resolution state, and a cursor key. These buckets may feed top-level
command lenses through filters, but Feed Direction must not create nested
Action Center, Control Tower, Protocol Adherence, Config, or SOP Library routes.
Calendar, Action Center, Protocol Adherence, Workflows, and Control Tower must
read the same Feed projection fields rather than deriving separate truth.

Shared Calendar blocker: the committed `calendar_event_projections` storage is
currently constrained to the vaccination slice and vaccination event vocabulary.
Feed can still build its own command buckets behind the scope gate, but Calendar
and Protocol Adherence integration must wait for a widened multi-slice calendar
projection, an approved generic projection, or a Feed-specific projector with
equivalent indexed fields.

## 9. Legacy parity mapping

Legacy source behavior to port as rules, not as Apps Script. Treat this as a
feature inventory, not an exact line-number blueprint; exact legacy code
references must be re-verified before import/cutover work.

| Source surface | Source signals | GoatOS mapping |
| --- | --- | --- |
| Count-DB / Projected-DB / FutureDB | Date, farm, shed, shed tag, breed, age, count, counted-by; projected and comparison tabs (`feed-direction-counting-db-reconstruction.md`) | Source evidence and fixture/reference shape only. Runtime feed counts come from the physical Base Count anchor, realized ShiftingEvent ledger, and horizon-aware projection contract. |
| Shifting reports | Type request/direction, category, priority, source/destination, comments, destination proof, approval/status | Process-visible movement workflow plus count input under horizon-specific gates: authorized/directed future-effective rows may feed the one-day projection, while realized counts wait for the configured applied state. Pending unauthorized, rejected, or canceled movement does not create Feed Direction work. Ledger application is idempotent by event id. |
| K0 cohort parsing/validation | K0 in/out Mother-vs-Kid parsing, unresolved/insufficient-count halt | Structured cohort/stage impact is mandatory. Missing or unresolved impact fails closed and raises process-exception work instead of changing counts. K0/K1 feed exclusion still needs Feed Director approval before publish. |
| Feed Direction sheet | Date, farm, session, shed, shed tag, breed, age, count, feed item/quantity pairs, session total | Generation snapshot/read-model fields, with quantities derived from reviewed RationTable output and count replay. |
| Feed Transfer KT / constraint sheets | Configuration screen and uploaded tables for feed vectors, breed/tag/energy requirements, weight bands, warm-up/pregnancy-style policy, and downstream values; transcript is noisy and does not establish shed placement | Source evidence for ration/constraint config only. Generation must explicitly resolve physical shed + breed counts to approved nutrition cohort keys; missing resolver context blocks instead of guessing from breed/tag tables. |
| Rounding and zero transforms | Round non-baking-soda up at daily shed total before session split; baking soda precision/blank-zero special case | Source-backed transform rule with unit tests; do not round independently per session unless approved as a behavior change. Resolve baking-soda precision before locking the unit boundary. |
| Packing / Consumption / Transport processed flags | Boolean processed columns in the legacy sheet | Idempotent obligation, SOP task, proof, verification, and completion state; never boolean runtime authority. |
| Feed Transport form and transport scripts | Date, farm, shed, time, video/message links, user, transport list/checklist item, uploaded file handling, consolidated transport sheds | First-class transport obligation/stage with source-backed direction-shed to transport-shed consolidation, proof upload, verifier decision, rejection reason, and rework/next action. |
| Feed Consumption & Wastage form | Consumed quantity, wasted quantity, consumption proof, wastage proof, difference, wastage percent; legacy 20% wastage flag | Typed consumption/wastage stage outcome or projection fields with proof references and source-backed variance exception thresholds. |
| Milk Preparation verification | Separate feed-adjacent workflow with Pending/Verified/Rejected media correctness and Slack notification behavior | Out of the packed-feed Feed Direction slice. Treat as a sibling verification workflow if reopened, not as implicit packing/transport/consumption scope. |
| Video verification / rejection tracker | Media correctness `Pending`/`Verified`/`Rejected`, remarks required on rejection, notification and rejected-media list | Platform proof verification state, audit trail, rejection reason, and follow-up obligation. |
| Packing quantity discrepancy | Expected vs actual quantity discrepancy, red-flag/admin notification behavior, and video-verification packing quantity reset/re-send behavior | GoatOS formalizes this as typed packing discrepancy/rework/reissue with audit and idempotency; do not copy Sheet flag resets or thread deletion as runtime authority. |
| Experiment sheds | Experiment tag and zero count/feed quantities in source evidence | Candidate feed eligibility exclusion / zero-direction rule requiring Feed Director sign-off. |
| Warmup stage tags | Warmup operating states from source findings, not fully covered by feed docx ration table | Reviewed ration path or explicit exclusion before Feed Direction publish. |
| K0/K1 feed exclusions | Milk-fed cohorts filtered out of legacy supply/diff paths | Candidate eligibility rule from legacy evidence; requires Feed Director approval before publish, not an incidental transform. |
| Per-farm Template sheet / session policy | Session labels, serving windows, split defaults, and which feed items appear in each session; June source default is two slots with 50/50 split and an explicit note to revisit if breed + tag needs uneven split | Versioned `session_policy` in `feed_direction` protocol config. Default publish uses the docx two slots; Feed Director/COO can add, disable, reorder, or reweight slots through an effective-dated protocol version. |
| Diff regeneration | Legacy automation had additional cycles/intermediate storage, while the June source model defines the 09:00 full direction, 13:30 cutoff, 13:30-13:45 Diff, and post-cutoff bridge | Canonical GoatOS Diff run stores affected-shed restatement rows and cancels/supersedes stale work; source-facing output may present net correction. Old two-cycle behavior is cutover evidence unless explicitly reinstated. |
| Clock/trigger inventory | `Feed, Shiftings and Count.docx` owns the default product clocks: Day N `09:00` full direction, Day N `13:30` cutoff, Day N `13:30-13:45` Diff, Day N `15:00` staging, and Day N+1 default `09:00`/`15:00` serving slots. Legacy Apps Script installers in `unified_automation.js`, `counting_db_automation.js`, `feed_automation.js`, and `video_verification_system.js` remain audit evidence for old packing, count, transport, archive/retry, proof, quantity, stock, wastage, and alert side effects. | Gate `G3` first confirms the docx default clocks, then marks each legacy trigger family retained, retired, or replaced. Installed trigger code and old trigger times are evidence only, not automatic GoatOS schedule law. |
| Retry scheduler | Reschedule/retry and admin alert behavior | Durable reminder/retry/escalation policy under gates `G11`-`G12`, not Apps Script timers. |
| Applied-event and file dedupe | Short-window dedupe and file-id dedupe in legacy automation | Durable idempotency keys, replay tests, and source archive checksums. |
| Count-mismatch detection | Unreported-shifting/count-reconciliation signal | Counts/Shifting exception work under gate `G2`, not silent Feed-side correction. |
| Breed remap | Legacy breed normalization such as SIROHI->Beetal | Reviewed breed alias config before projection/ration lookup. |

Other parity rules:

- Session split is currently 50/50 by source default, but it must be represented
  as versioned admin session-slot config. Validators require at least one active
  slot, unique slot codes/order per effective scope, split weights that sum to the
  full daily as-fed quantity for each applicable feed item/scope, and no silent
  mutation of already-generated target dates.
- Base Count cadence has source history moving from roughly weekly to roughly
  monthly. Treat cadence as reviewed policy or schedule config, never as a code
  constant.
- Feed Direction, Diff, packing, and field surfaces show as-fed gross
  quantities only. `wastage_factor` and `DM_factor` remain internal nutrition
  accounting inputs and must not appear as field-facing instruction quantities.
- Legacy feed rounding grain, Warmup handling, K0/K1 exclusions, F2/Fattening
  alias handling, breed remaps, transport consolidation, discrepancy thresholds,
  retry/dedupe behavior, and processed-flag behavior must be captured as
  explicit reviewed transform/config rules before use.
- Slack delivery is notification/cutover bridge only. It must call GoatOS APIs if
  retained; it must not mutate Sheets as canonical state.

Security note: the legacy Slack/App Script repo contains committed Slack token
and shared-secret material. Do not copy it into GoatOS docs, code, tests, or
fixtures. Rotation in the source system is required before any bridge reuse.
Overlap is allowed only after a security closeout inventories affected scripts,
revokes or rotates credentials, moves any retained bridge credential to the
approved secret store, and proves GoatOS API-only ingress with auth/RBAC,
idempotency keys, and audit/outbox evidence.
If `G10` is owner-deferred, overlap is not partially accepted: Slack bridge
execution remains disabled until that closeout is complete.

## 10. Admin-web and mobile constraints

- `G1` is reopened for Feed Direction build as of 2026-06-30. The old
  admin-web scope lock tied to PHC/Vaccination review is satisfied for
  sequencing; Google dev vaccination rollout remains separate Goal 2 and does
  not block Feed Direction.
- Current Config may keep internal Feed DSL code paths only if the backend page
  contract does not expose `feed_direction` as a visible category.
- The current Feed mock is static and stale on timing/nav labels; it does not
  show pagination, filters, row drawers, or column controls for Feed Direction.
- When scope reopens, update the mock and `check-mock-fidelity` scan paths in
  the same UI slice before implementing unmocked Feed Direction surfaces.
- UI implementation must port the mock anatomy: component structure, typography,
  spacing, colors/tokens, button/icon sizing, pagination, drawers, tables,
  filters, empty/error states, and `:hover`/active/focus/disabled states. Do not
  ship a plainer local UI just because the backend is ready.
- Backend contracts own page titles, table columns, filters, buckets, disabled
  reasons, pagination semantics, and action availability. Admin-web and operator
  clients render generated API contracts and may own only layout, responsive
  density, icon mapping, and local interaction state.
- Frontend push/handoff requires `npm --prefix apps/admin-web run
  check:mock-fidelity` plus rendered visual proof at relevant desktop and mobile
  widths.
- Operator-mobile should execute SOP/proof/offline queue through generated APIs;
  it must not access Sheets, Slack, GCS, Firestore, BigQuery, or Postgres
  directly.

## 11. Scale requirements

- Use tenant, park, date, shed, session, status, owner, and cursor keys for every
  hot read.
- No unbounded raw scans from admin-web/mobile.
- Workers lease bounded chunks by tenant/park/date/run.
- Generation rows/projections must support replay and rebuild from canonical
  events plus source evidence hashes.
- Validate SQL plans for the list and generation queries.
- Load tests must include skew: many sheds, repeated Diff runs, and large
  historical count ledgers.

## 12. Acceptance gates

- Unit tests for ration key normalization and Diff/bridge decision tables.
- Unit tests for ration-context resolver behavior: one projection row to one
  cohort, one projection row split across multiple approved cohorts, and missing
  shed/tag/cohort context producing a fail-closed blocker.
- Unit tests for the ration do-not-resolve rules: feed-type-level constraints
  only, Milking/Fattening-only `60:40`, no paired overshoot ceiling, deferred
  item-level ceilings, and wholesale RationTable replacement after re-solve.
- Unit tests for feed eligibility exclusions, rounding grain/zero transforms,
  transport consolidation mapping, variance thresholds, and structured shifting
  cohort impact fail-closed behavior.
- Integration tests that keep initial count projection at aggregate shed + breed
  grain, resolve ration context from reviewed source-backed evidence, fail when
  the ration-context resolver is missing, and fail if RFID-to-shed per-goat
  derivation becomes an implicit dependency.
- Integration tests for Counts/Shifting input snapshots, generation idempotency,
  affected-shed restatement, explicit stale-obligation cancel,
  reserve/consume/release, stage proof verification, and packing rework.
- Integration tests for as-fed gross field outputs, proving `wastage_factor` and
  `DM_factor` are internal only and never alter packing/field instruction copy.
- Bridge tests prove manual log/proof/reconciliation behavior and no generated
  `07:30` next-morning Diff or source-shed claw-back.
- Integration tests for reminder/escalation SLA, NotificationGateway routing,
  missed/recovery events, audit rows, observability counters, and command-lens
  field mapping.
- Seeded local E2E proof through Postgres, API/generated client, generation
  worker or local scheduler, obligations, durable `stage_kind`, inventory,
  SOP proof/verification, read models, and frontend visual proof after `G1`
  reopens.
- Import/replay tests for Feed legacy source rows, applied shifting events, proof
  rows, and duplicate/cross-source overlap cases from the cutover contract.
- API route tests and OpenAPI generated-client checks.
- Projection/read-model tests for the command buckets, including no duplicate
  active work after Diff supersession and no unbounded list scans.
- SQL plan validation for hot list/filter queries.
- Local end-to-end proof through Postgres, API, outbox/consumer or documented
  local equivalent, sweeper/job, SOP submission, verification, and read model.
- UI visual smoke under reopened `G1` after the mock is updated or explicitly
  superseded for stale Feed labels; the smoke must inspect mock anatomy, hover,
  focus, pagination, buttons, typography, and responsive layout.

## 13. Blockers before build

Use the canonical gate table in
[DEPENDENCY-CLOSURE-PRD.md](./DEPENDENCY-CLOSURE-PRD.md) rather than maintaining
a separate blocker list. Technical implementation may start under reopened `G1`
by closing `G2`-`G17` in order. A gate may only unblock later work through a
narrow owner decision that defines a fail-closed adapter or disabled capability;
broad owner-deferral language does not make hidden runtime scope ready. `G15`
closure specifically includes resolving or explicitly bounding the
vaccination-locked Calendar projection blocker. If `G10` is bounded instead of
closed, the bound is Slack bridge disabled. The next migration number is a
build-time check after the live repo tail, currently `000117`, is reverified.
The full build stop rule lives in [BUILD-TO-DONE-GOAL.md](./BUILD-TO-DONE-GOAL.md)
and includes source cross-check, E2E seed proof, high-effort review-agent pass,
clean-tree verification, and `git mesha-push main` through the Mesha/VGoats PAT
path before the goal can be called done.
