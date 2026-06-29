# Feed Direction Dependency Closure - TRD

**Status:** Draft v2, technical readiness design before Feed Direction build
**Date:** 2026-06-30
**Companions:** [Dependency Closure PRD](./DEPENDENCY-CLOSURE-PRD.md),
[Feed Direction PRD](./PRD.md), [Feed Direction TRD](./TRD.md),
[Counts/Shifting Closure PRD](./COUNTS-SHIFTING-CLOSURE-PRD.md), and
[Counts/Shifting Closure TRD](./COUNTS-SHIFTING-CLOSURE-TRD.md), and
[Feed Direction Build-To-Done Goal](./BUILD-TO-DONE-GOAL.md)

## 1. Technical Goal

Implement or specify the missing contracts that Feed Direction depends on:

```text
Counts/Shifting projection
  -> source-backed RationTable lookup
  -> full generation / Diff generation
  -> stage obligations / stage_kind
  -> inventory reserve/consume/release
  -> proof/verification/rework
  -> reminders / notification / missed-recovery events
  -> audit / observability
  -> cursor read models and command-lens buckets
```

The existing GoatOS kernel remains the target. Do not build a parallel Feed
execution engine.

## 2. Existing Kernel Anchors

These pieces are reusable:

| Piece | Current state |
| --- | --- |
| Protocol rules | `protocol_versions.rule_dsl` supports `category='feed_direction'` |
| Obligations/batches | `obligation_instances` and `obligation_batches` are the work source |
| Feed completion row | `feed_direction_completions` exists and is unique by tenant + obligation |
| Inventory | `ReserveForBatch`, `ConsumeForBatch`, and release path exist |
| SOP proof | SOP submissions/items and verification are reusable |
| Diff precedent | Vaccination cancellation helpers prove explicit stale-work cancel patterns |
| Scale rules | No per-goat feed tasks; bounded reads; query-plan validation |

The gaps are module contracts and wiring, not a missing platform. Feed must still
close the feed-specific kernel wiring named by gates `G11`-`G15`:
reminders/escalations, notifications, missed/recovery state, audit/observability,
and command-lens field mapping. One shared blocker remains for `G15`: the
committed Calendar projection is currently vaccination-slice locked and must be
widened or bypassed with an approved generic/feed projection before Feed can
appear in Calendar or Protocol Adherence.

## 3. Dependency Architecture

### 3.1 Counts/Shifting Port

Feed generation must depend on a Counts/Shifting application port. This is gate
`G2` and is specified in the sibling Counts/Shifting closure docs.

```go
type CountProjectionProvider interface {
    CountAsOf(ctx context.Context, req CountAsOfRequest) (CountProjection, error)
    ProjectedCountFor(ctx context.Context, req ProjectedCountRequest) (CountProjection, error)
}
```

Required request shape:

```text
tenant_id
park_id
target_date or as_of
grain: shed + breed + stage/tag
horizon: realized | one_day_projection
protocol_version_id or source_hash consumer tag
```

Required response row shape:

```text
tenant_id
park_id
shed_id
breed_id
stage_tag_id
head_count
projection_horizon
source_contract_version
source_hash
base_count_anchor_id
included_shifting_event_ids_hash
exception_count
```

Feed must snapshot these rows into `feed_direction_count_input_rows` for every
generation run. A later count replay must not silently rewrite the input used by
an already-issued direction.

### 3.2 Counts/Shifting Persistence Requirement

The Counts module must provide one of these implementation paths before Feed
generation can run:

#### Preferred: aggregate ledger

Candidate tables owned by Counts/Shifting:

| Table | Purpose |
| --- | --- |
| `count_base_anchors` | Physical count anchor by tenant, park, shed, breed, stage/tag, counted_at |
| `shifting_events` | Append-only movement events with priority, category, effective time, authorization, source/destination, proof/verification state |
| `shifting_event_impacts` | Structured cohort/stage deltas per event; no free-text policy decisions |
| `count_projection_snapshots` | Optional cached output for tenant/park/date/grain with source hash |
| `count_projection_exceptions` | Durable fail-closed work for unresolved cohort/stage, unreported shifting, or insufficient source data |

Idempotency:

- `shifting_events` must be unique by tenant + logical shifting event key.
- Applying a shifting event to a projection must be idempotent by event key.
- Legacy applied-event dedupe windows and file-id dedupe are parity evidence;
  GoatOS must replace them with durable idempotency keys and replay tests.
- Replays may refresh source metadata, but cannot double-apply deltas.

#### Alternative: per-goat derivation

Allowed only after RFID-to-shed association and committed GoatOS identity/location
state can prove the same aggregate grain without full-herd scans. This path must
join active identifiers from `goat_identifiers`, authoritative movement/location
history from `goat_location_history`, and reviewed shed/stage reference data into
bounded tenant + park + shed + breed + stage/tag aggregates. It still must expose
the same `CountProjectionProvider` interface and snapshot rows, and it must fail
closed if RFID, identifier, or location confidence cannot support the aggregate.

### 3.3 Horizon Rules

`CountAsOf` uses only the configured applied state.

`ProjectedCountFor(target_date)` may include authorized/directed
future-effective shiftings whose `effective_at` falls inside the target date.
Pending unauthorized, rejected, canceled, or unresolved events are visible
process work but excluded from quantities.

Missing structured cohort/stage impact is fail-closed:

```text
exclude from realized count
exclude from one-day projection
raise count_projection_exception
preserve raw comments as audit only
```

## 4. Ration Provenance Contract

Feed Direction consumes ration values from `protocol_versions.rule_dsl` with
typed validation.

Required DSL families:

```text
ration_scope
stage_aliases
breed_aliases
kid_weight_band_adg_rules
warmup_stage_policy
feed_item_vectors
feed_type_constraints
session_policy
eligibility_exclusions
rounding_policy
proof_policy
inventory_policy
source_metadata
```

Required provenance:

```text
source_document_version
solver_or_import_version
feed_item_vector_hash
feed_cost_hash
available_feed_type_hash
constraint_hash
output_hash
reviewer
reviewed_at
review_status
approved_by
approved_at
effective_from
re_solve_trigger
```

Implementation options:

1. Non-UI solver service writes reviewed output into a new protocol version.
2. Importer validates reviewed solver output and writes it into a new protocol
   version.

Unsourced values stay `draft` and cannot generate production obligations.
Reviewed values are not publishable until `review_status='approved'`,
`approved_by`, `approved_at`, and the reviewed source hashes are present. The
publish transaction must also enforce the actor/job capability check for
`protocol.publish.feed_direction` or its approved service equivalent.

Warmup, K0/K1, Experiment, F2/Fattening, SIROHI->Beetal, and any other legacy
stage/breed transforms must be captured as reviewed config or explicit
Feed-Director exclusions. Do not treat automation zero rows as automatic policy
authority.

## 5. Feed Generation Persistence

Add only the feed-specific tables the generic kernel cannot naturally represent.

Candidate tables after the current migration tail:

| Table | Required | Purpose |
| --- | --- | --- |
| `feed_direction_generation_runs` | yes | Full/Diff/bridge-log run header, idempotency, source hash, status |
| `feed_direction_count_input_rows` | yes | Immutable projection snapshot consumed by the run |
| `feed_direction_generation_rows` | yes | Canonical shed/session/feed/cohort instruction rows |
| `feed_direction_bridge_events` | yes | Post-cutoff high-priority addition bridge log |
| `feed_direction_projection_rows` | maybe | Materialized cursor read model if generic joins are too expensive |
| `feed_direction_stage_records` | yes/equivalent | Required typed `stage_kind` and detail projection unless an indexed obligation-context or completion-stage record supplies the same discriminator |

Generation row grain:

```text
tenant_id
run_id
target_date
park_id
shed_id
session_code
breed_id
stage_tag_id
feed_item_id
quantity_base_units
display_unit
quantity_unit
row_kind: full | diff_restatement | bridge
source_facing_net_delta_base_units nullable
active_instruction_key
superseded_by_run_id nullable
```

Quantity convention for first build:

- solids: grams as integer base units;
- liquids: milliliters as integer base units;
- generation rows store immutable `quantity_base_units` plus `quantity_unit`;
- display conversion belongs to API/UI contracts;
- inventory stock and movement rows remain the committed SQL shape:
  `numeric` quantity columns plus `quantity_unit`;
- the current inventory app port still accepts whole `int64` quantities, so Feed
  adapters may call it only with deterministic whole base units;
- do not rely on the current repository's whole-dose numeric floor for Feed; if
  Feed requires fractional base units, widen Reserve/Consume/Release app ports
  and add decimal reserve/consume/release tests before use;
- baking-soda precision is a gate `G6` decision, not a later implementation
  surprise.

## 6. Full Generation Algorithm

```text
lease tenant/park/date generation key
load published feed_direction protocol version
verify ration source approval and publish capability metadata
load ProjectedCountFor(target_date)
fail if projection contract or source-backed ration is missing
snapshot count input rows
apply eligibility exclusions
normalize breed/stage aliases
lookup ration rows
apply as-fed rounding policy at approved grain
split by session policy
insert generation run and generation rows
create obligation batch
create obligation_instances for packing, transport, consumption_wastage as needed
attach durable stage_kind to every stage obligation/projection
write outbox events for projection/notifications
mark run published
```

Generation must be idempotent by tenant + park + target date + run kind +
protocol version + projection source hash.

## 7. Diff Algorithm

Canonical runtime truth is affected-shed restatement, not delta-only storage.

```text
lease tenant/park/date Diff key
load prior active full run
load eligible projection changes up to cutoff
identify affected shed/session/feed rows
calculate restatement rows
calculate optional source-facing net delta
insert Diff run and restatement rows
cancel/supersede stale open obligations for affected instruction keys
create replacement obligation_instances
write outbox events
```

The feed cancel helper must mirror the vaccination stale-obligation pattern:
find open obligations by tenant, target date, shed/session/feed instruction key,
and prior run/version; cancel or supersede them inside the same transaction that
creates replacement work. Also inspect the per-version eligibility-drop
precedent when designing Feed rule/version cancellation.

Idempotency keys do not replace explicit stale-work cancellation.

Legacy two-cycle Diff behavior is cutover evidence, not the target runtime. The
June source model keeps the Day N 09:00 full direction, Day N 13:30 cutoff, Day
N 13:30-13:45 Diff, and post-cutoff bridge/no-claw-back protocol unless Feed
Director explicitly reverses it.

## 8. Stage Model

Use separate obligations grouped by a batch as the default model.

```text
obligation_batch: Feed Direction run / target date / park / shed-session-feed group
  obligation_instance: packing
  obligation_instance: transport
  obligation_instance: consumption_wastage
  obligation_instance: bridge_exception where applicable
```

This does not imply a parent-child obligation foreign key. The grouping primitive
is `obligation_batches` plus multiple `obligation_instances`, each with a
distinct `obligation_id` that can satisfy the existing tenant + obligation
completion uniqueness constraint.

`stage_kind` is a hard discriminator, not a UI label. Before the execution-bucket
APIs are built, Feed must implement one of these durable indexed options:

1. `feed_direction_stage_records` keyed by tenant, obligation, generation row,
   and `stage_kind`.
2. Obligation context containing `stage_kind`, with an indexed projection or
   generated column usable by read models.
3. A first-class completion/stage table linked to `feed_direction_completions`
   and carrying `stage_kind`.

The existing `feed_direction_completions` row alone is insufficient because it
has no stage discriminator. Buckets such as `packing_due`, `transport_pending`,
`consumption_incomplete`, `wastage_exception`, and `bridge_exception` must query
durable stage identity plus state; they must not infer stage from SOP labels,
free-text statuses, or legacy processed flags.

Stage detail requirements:

| Stage | Required detail |
| --- | --- |
| Packing | `stage_kind='packing'`, planned quantity, actual packed, proof, verifier, discrepancy, shortfall, rejection, rework |
| Transport | `stage_kind='transport'`, transport shed, mapped direction sheds, checklist/list entity where needed, proof media, verifier, rejection reason, rework |
| Consumption/wastage | `stage_kind='consumption_wastage'`, planned packed, consumed, wasted, difference, wastage percent, proof, verifier |
| Bridge | `stage_kind='bridge_exception'`, destination shed, source event, 2x ration quantity, proof, reconciliation link |

If these details do not fit cleanly in SOP submission items and completion
metadata, add `feed_direction_stage_records` as typed detail rows linked to the
obligation and generation row. Do not force all stages into one completion row,
and do not build read-model buckets until the durable discriminator exists.

Packing shortfall/rework is parity plus formalization. Legacy evidence includes
red-flag/admin alert paths and a separate video-verification packing quantity
check that resets packing processed state and causes re-send behavior. GoatOS
must not reproduce the Sheet/thread reset mechanism; it maps the useful behavior
to typed rework/re-issue obligations, audit, idempotency, and inventory-safe
state transitions.

## 9. Inventory Wiring

Feed inventory integration uses the generic inventory app.

```text
packing starts
  -> ReserveForBatch(tenant, batch, location, feed_item, planned_qty_base_units)

packing proof rejected
  -> no consume
  -> keep/release reservation according to proof policy
  -> create rework obligation

packing accepted
  -> ConsumeForBatch(actual_qty_base_units)
  -> ReleaseForBatch(planned_qty - actual_qty)
```

The first Feed build treats these `*_qty_base_units` values as whole `int64`
grams or milliliters at the inventory app boundary. The inventory adapter must
persist the corresponding movement quantity as SQL `numeric` with the matching
`quantity_unit`. No Feed path may depend on implicit decimal flooring. A future
decimal Feed requirement must widen the inventory app port before implementation.

Reserve/consume/release must share stable idempotency keys:

```text
feed:<tenant>:<batch_id>:<obligation_id>:<feed_item_id>:reserve
feed:<tenant>:<batch_id>:<obligation_id>:<feed_item_id>:consume:<verification_id>
feed:<tenant>:<batch_id>:<obligation_id>:<feed_item_id>:release:<verification_id>
```

Stock-out or reservation mismatch becomes blocked obligation state plus command
bucket projection. It must not mutate balances directly.

## 10. Kernel Reminders, Notifications, Missed State, Audit

Feed must implement the operational kernel, not just generation rows.

Required stage timing contract:

```text
stage_kind
due_at
reminder_schedule
deadline_at
escalation_after
owner_role / assignee
recovery_action
```

Required notification contract:

```text
event_type: feed.stage_due | feed.stage_overdue | feed.proof_rejected |
            feed.stock_out | feed.bridge_exception | feed.generation_blocked
tenant_id
park_id
shed_id / transport_shed_id
target_date
stage_kind
obligation_id
severity
idempotency_key
channel_policy
```

Notifications must go through a `NotificationGateway`-style port. Slack may be a
bridge adapter only after `G10`; it is never the canonical execution source.

Deadline crossing must write durable business state, not only logs. Feed must
wire into the shared missed/deadline materializer where available and add any
feed-specific recovery subscriber or blocker needed for stage recovery. Required
events include:

- stage became due;
- reminder sent or suppressed with reason;
- stage became missed/overdue;
- escalation opened;
- recovery/rework created;
- proof accepted/rejected;
- escalation resolved.

Business audit rows must include actor/job, tenant, scope, stage, obligation,
source/input hash, idempotency key, before/after or payload, and trace id where
available. Technical observability must include generation latency, worker
errors, retry counts, queue/outbox lag, notification result counts, projection
refresh lag, DLQ counts, and DB/query-plan failures.

Counts/Shifting workers must meet the same observability bar before `G2` can turn
green: base-count import latency, shifting ingest latency, projection recompute
latency, stale-projection age, queue/outbox lag, retry and DLQ counts,
exception counts by type, and query-plan failures must be emitted or made
available to the same monitoring slice.

## 11. Transport Consolidation Contract

Feed must not infer transport sheds by string manipulation at runtime.

Provide a source-backed transport map:

```text
tenant_id
park_id
map_version
direction_shed_id
transport_shed_id
consolidation_kind: none | full | partial
effective_from
effective_to nullable
source_reference
review_status
reviewer
```

Preferred ownership: Locations owns canonical shed identities and valid shed
relationships; Feed operations owns the effective transport consolidation
policy. If a generic location-relationship table exists when implemented, use
it. Otherwise add a small Feed-owned transport map table behind a
`TransportMapProvider` port.

If legacy overlap preserves a transport Slack/list item concept, GoatOS must
model it as a checklist/work entity linked to the transport obligation/stage,
not as Slack state.

## 12. Exception Threshold Contract

Packing discrepancy and wastage thresholds are source-backed policy, not UI
coloring.

Store thresholds in the Feed proof policy DSL:

```text
packing_shortfall_tolerance_base_units
packing_overage_tolerance_base_units
wastage_percent_warning_threshold
wastage_percent_blocking_threshold
feed_item_overrides
park_overrides
effective_from
review_status
source_reference
```

Legacy evidence gives candidate values, including expected-vs-actual feed
comparison and a 20 percent wastage flag. These may be seeded only as draft or
candidate config until Feed Director review approves them.

## 13. API Contracts

Add backend-owned contracts before UI work:

| API | Purpose |
| --- | --- |
| `GET /feed-direction/readiness` | Shows canonical gates `G1`-`G17`, missing source inputs, blocker reasons, owner, evidence pointer, and `CSG1`-`CSG10` subgate breakdown under `G2` |
| `POST /feed-direction/generation-runs` | Manually enqueue/generate full run or Diff with idempotency |
| `GET /feed-direction/generation-runs` | Cursor list by tenant/park/date/kind/status |
| `GET /feed-direction/directions` | Cursor instruction rows with active/superseded state |
| `GET /feed-direction/execution-buckets` | Command buckets for due, blocked, missed, escalated, rejected, rework, derived from durable `stage_kind` plus state |
| `POST /feed-direction/stages/{obligation_id}/proof` | Stage proof submission through SOP/proof path |
| `POST /feed-direction/stages/{obligation_id}/verify` | Accept/reject/rework stage proof |
| `POST /feed-direction/stages/{obligation_id}/recover` | Create or resolve recovery/rework after missed/rejected state |
| `GET /feed-direction/bridge-events` | Bridge exception list and reconciliation state |

OpenAPI and generated clients must be updated in the same build slice. Labels,
filters, status copy, disabled reasons, and pagination semantics are backend
owned.

Frontend implementation must consume these contracts through generated clients.
When `G1` reopens UI, mock anatomy still governs visual execution: pagination,
buttons, icons, typography, spacing, colors/tokens, hover/active/focus/disabled
states, drawers, filters, tables, empty/error states, and proof/status surfaces
must match `mock/goatos-dashboard-mock.html` or a deliberately updated Feed
mock. Do not ship a Feed UI that is only functionally wired but visually plainer
than the Mesha mock system.

Command-lens rows must expose at least:

```text
tenant_id
domain='feed'
module='feed_direction'
target_date
park_id
shed_id nullable
transport_shed_id nullable
session_code nullable
stage_kind
bucket
status
due_at
deadline_at
owner_role
assignee_id nullable
active_instruction_key
obligation_id
batch_id nullable
proof_state
verification_state
evidence_refs
blocked_reason nullable
escalation_state nullable
resolution_state nullable
cursor_key
```

These fields are what Calendar, Action Center, Protocol Adherence, Workflows,
and Control Tower subscribe to. Do not let each lens derive a different truth.

`G15` implementation note: the existing `calendar_event_projections` table and
calendar identity constraints are currently limited to `slice_key='vaccination'`
and vaccination event types. Feed command buckets can be built in Feed read
models first, but Calendar/Protocol Adherence exposure requires either widening
that projection vocabulary to multi-slice use or introducing an approved generic
projection/feed projector with equivalent indexed fields.

## 14. Worker And Outbox Wiring

Required worker paths:

| Worker path | Trigger | Behavior |
| --- | --- | --- |
| Full generation | Cloud Scheduler / local job | Generate Day N+1 full direction |
| Diff generation | Cloud Scheduler / local job | Generate pre-cutoff restatement Diff |
| Count event consumer | Pub/Sub / local outbox relay | Invalidate or enqueue affected Feed generation |
| Sweeper/deadline worker | Cloud Scheduler / local job | Mark due/missed, reminders, escalation buckets, recovery work |
| Notification dispatcher | Outbox / notification queue | Send or stub feed alerts through replaceable adapters |
| Projection refresh | Outbox consumer/local job | Refresh Feed read model and command buckets |

Local development may use the existing local outbox/eventbus equivalent, but the
acceptance proof must distinguish local-dev fixture closure from production
Pub/Sub/Scheduler/Cloud Tasks deployment.

Clock inventory for `G3` is explicit evidence, not schedule law. The canonical
source clocks are Day N `09:00` full direction, Day N `13:30` cutoff/Diff window,
Day N `15:00` staging, and Day N+1 `09:00`/`15:00` serving. Legacy automation
also contains windows at `07:30`, `14:45`, `06:30`, `07:15`, `14:15`, `00:15`,
`23:45`, and `07:00`; the `23:45` path is the packing quantity check/reset
loop, and the midnight archive/retry family is `00:15`, not an assumed 03:00
clock. Feed Director sign-off must mark each retained, retired, or replaced.

## 15. Legacy Cutover Rules

Legacy import/replay must be idempotent by source row/checksum and logical stage
key.

Mappings:

- Feed Direction rows -> generation run/rows or audit-only archive.
- Packing proof -> packing stage obligation proof/completion or audit-only archive.
- Feed Transport rows/list/checklist items -> transport stage with map version
  and checklist entity if overlap preserves that work shape.
- Consumption/wastage rows -> consumption/wastage stage detail or audit-only
  archive.
- Counting DB rows -> source archive and sanitized fixture only.
- Applied shifting rows -> Counts/Shifting event ledger or source archive.
- Retry scheduler behavior -> durable retry/reminder policy, not Apps Script
  timers.
- Legacy trigger windows -> clock-signoff evidence for `G3`, including
  `07:30`, `14:45`, `06:30`, `07:15`, `14:15`, `00:15`, `23:45`, and `07:00`,
  not automatic GoatOS schedules.
- Packing quantity check/reset loop -> typed packing discrepancy/rework policy;
  legacy reset/re-send behavior is inventoried, but Sheet flag clearing and
  thread deletion are not copied as runtime authority.
- 3-day applied-event/file dedupe -> durable idempotency and replay tests.
- Count-mismatch unreported-shifting detection -> Counts/Shifting exception
  work, not Feed-side hidden correction.
- Breed remaps and aliases -> reviewed breed/stage alias config.

Slack/App Script may only call GoatOS APIs during overlap. It must not mutate
Sheets as canonical state. Do not copy legacy tokens or shared secrets into
GoatOS code, docs, tests, or fixtures.

Bridge overlap is blocked until a security closeout records evidence, without
reproducing secret values:

- inventory of affected legacy scripts, webhook URLs, tokens, shared secrets,
  and call sites;
- revocation or rotation evidence for every affected credential;
- any retained bridge credential stored in Secret Manager or the approved
  environment secret store, never in source or fixtures;
- GoatOS API-only ingress proof using authenticated requests, RBAC scope checks,
  idempotency keys, and audit/outbox records;
- explicit overlap window, owner, rollback/disable switch, and post-overlap
  decommission check.

## 16. Scale And Query Gates

Every hot query must be bounded by tenant and at least one operational scope:
park, target date, shed/transport shed, session, status/bucket, owner, or cursor.

Required tests/checks:

- EXPLAIN/plan validation for generation input queries.
- EXPLAIN/plan validation for direction list and bucket list APIs.
- Cursor pagination tests with stable ordering.
- Repeated Diff runs with no duplicate active obligations.
- Large ledger fixture or synthetic scale fixture for many sheds and repeated
  shiftings.
- No per-goat feed task generation.

## 17. Implementation Sequence

1. Close Counts/Shifting projection in its sibling PRD/TRD, or add an adapter
   stub that fails closed with explicit `G2` readiness state.
2. Add ration DSL validators, provenance requirements, Warmup/K0/K1/Experiment
   sign-off, and quantity/precision decisions.
3. Add generation run/count snapshot/generation row/bridge migrations.
4. Build full generation service and idempotency tests.
5. Build Diff service and stale-obligation cancel/supersede helper.
6. Build stage obligations, durable `stage_kind`, and proof/rework wiring.
7. Wire inventory reserve/consume/release with exact base-unit to
   `numeric + quantity_unit` persistence.
8. Add reminder/escalation, notification, missed/recovery, audit, and
   observability paths.
9. Add transport map provider and tests.
10. Add exception threshold policy and variance tests.
11. Add APIs, OpenAPI, generated clients, read models, command-lens field map,
   and plan checks.
12. Only after `G1` reopens scope, update mock anatomy, build frontend, run
    `npm --prefix apps/admin-web run check:mock-fidelity`, and capture rendered
    desktop/mobile visual proof.
13. Run the build-to-done review gate from
    [BUILD-TO-DONE-GOAL.md](./BUILD-TO-DONE-GOAL.md): source/wiki parity,
    legacy/cutover/security, backend architecture, frontend fidelity, seeded E2E,
    and docs sync. Fix confirmed findings before push.
14. Push through `git mesha-push main` only after final tests, visual proof,
    clean-tree check, and `HEAD == origin/main` verification target are ready.

## 18. Non-Negotiable Acceptance Tests

- Counts horizon split: realized vs one-day projection.
- Base Count adoption not blocked by discrepancy investigation.
- Shifting event idempotency by event key.
- Missing cohort/stage impact fails closed.
- Count-mismatch/unreported-shifting detection creates exception work.
- Ration key normalization and eligibility exclusions.
- Warmup/K0/K1/Experiment policy cannot publish without sign-off/provenance.
- Ration publish gate rejects missing approval metadata or missing publish
  capability.
- Baking-soda precision and feed-unit boundary are tested.
- Rounding and as-fed output; no DM/wastage field-facing quantities.
- Generation idempotency.
- Diff restatement plus zero stale open obligations.
- Bridge decision table.
- Durable `stage_kind` supports packing, transport, consumption/wastage, bridge,
  and execution-bucket queries without label parsing.
- Packing discrepancy rework tests cover both legacy alert/reset evidence and
  GoatOS typed rework/re-issue policy without relying on Sheet flag resets.
- Packing reject creates rework and does not consume inventory.
- Accepted packing consumes actual and releases remainder.
- Inventory calls use exact whole base units for the current app port; no Feed
  path depends on numeric flooring.
- Transport map consolidation and checklist entity where overlap requires it.
- Wastage threshold exception.
- Reminder/escalation SLA and missed/recovery events.
- NotificationGateway routing and adapter-level idempotency.
- Business audit rows and worker observability metrics/alerts.
- Command-lens field mapping for Calendar, Action Center, Protocol Adherence,
  Workflows, and Control Tower.
- Frontend mock-fidelity check and rendered visual smoke for any reopened Feed
  UI, including pagination, buttons, typography, hover/focus states, and
  responsive layout.
- Legacy replay dedupe.
- Slack bridge closeout proves credential rotation, secret storage, API-only
  ingress, auth/RBAC, idempotency, and audit before overlap.
- Cursor lists and SQL plan gates.
- High-effort review-agent findings are resolved or explicitly owner-deferred
  before GitHub push.
