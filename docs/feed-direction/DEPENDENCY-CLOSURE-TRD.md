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

Source authority for this slice is `Feed, Shiftings and Count.docx` v1.1. It
owns the Feed Direction business clocks, Diff behavior, bridge rule, projection
semantics, as-fed quantities, and ration constraints. Legacy Slack/App Script
trigger installers are audit/cutover evidence only; their timings do not become
GoatOS schedules unless the Feed Director explicitly retains or replaces the
behavior against that docx.

`Counting DB - values only.xlsx` and `Feed Directions Automation DB.xlsx` are
workbook implementation evidence only. Their tabs, formulas, hidden copies,
processed flags, script properties, and Slack media links must be replaced with
typed imports, CRUD/review/publish config, generation snapshots, stage
obligations, proof/rework state, audit/outbox, and bounded read models.

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
grain: shed + breed
reviewed_ration_context_resolution required: resolved | blocked
horizon: realized | one_day_projection
protocol_version_id or source_hash consumer tag
```

Required response row shape:

```text
tenant_id
park_id
shed_id
breed_id
ration_context_resolution_state
resolved_ration_context_ids nullable
ration_context_blocker_reason nullable
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

Feed Transfer KT evidence means ration/constraint config may be keyed by
breed/tag/stage, energy/vector policy, weight band, warm-up, pregnancy, and other
nutrition dimensions without authoritative shed placement. Counts/Shifting owns
the physical shed + breed projection. Feed owns the reviewed resolver from that
projection to nutrition/ration cohort keys. If the resolver cannot prove the
cohort context for a projected row, `ration_context_resolution_state` is blocked
and generation must create visible exception work instead of choosing a default
ration.

### 3.2 Counts/Shifting Persistence Requirement

The Counts module must provide one of these implementation paths before Feed
generation can run:

#### Preferred: aggregate ledger

Candidate tables owned by Counts/Shifting:

| Table | Purpose |
| --- | --- |
| `count_base_anchors` | Physical count anchor by tenant, park, shed, breed, counted_at |
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
bounded tenant + park + shed + breed aggregates plus reviewed shed-tag/ration
context resolution state. It still must expose the same
`CountProjectionProvider` interface and snapshot rows, and it must fail closed if
RFID, identifier, location, or ration-context confidence cannot support the
aggregate.

This alternative is out of initial Feed Direction scope. The first build must
consume aggregate shed + breed projections, with ration context resolved from
reviewed source-backed evidence or blocked with a reason; RFID-to-shed per-goat
derivation can only replace that after separate implementation, scale proof, and
owner approval.

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

Base Count cadence is not a hardcoded interval. Source history moved from
roughly weekly to roughly monthly physical counts, so cadence must live in
reviewed policy or ops schedule config.

## 4. Ration Provenance Contract

Feed Direction consumes ration values from `protocol_versions.rule_dsl` with
typed validation.

Required DSL families:

```text
ration_scope
nutrition_cohort_key
ration_context_resolver_policy
stage_aliases
breed_aliases
kid_weight_band_adg_rules
warmup_stage_policy
feed_item_vectors
feed_type_constraints
quantity_weight_thresholds
validation_tolerances
session_policy
eligibility_exclusions
rounding_policy
proof_policy
inventory_policy
source_metadata
```

Workbook-derived config enters through typed import or admin CRUD, never by
reading live formulas. Import/review support must cover feed item nutrient
vectors, feed costs, feed-type constraints, breed/species aliases, stage/tag
aliases, kid weight-band/ADG rules, quantity/weight thresholds,
warm-up/pregnancy policy, eligibility/exclusions, session slots,
feed-set templates, transport maps, proof thresholds, validation tolerances, and
reviewed ration solver/import outputs. Each import family must carry source
checksum, row-level validation, dry-run parity preview when a workbook source
exists, dead-letter/repair state, reviewer/approval metadata, and audit.
Pregnancy, lactation, warm-up, and similar safety-critical states must be
dimension keys that the resolver can join from a projected destination shed row
to reviewed nutrition cohort context; they cannot remain free-text comments or
spreadsheet-only labels.

KT examples are candidate rows for those families, not code constants. The
import/review layer must be able to represent feed vectors/energy capacity,
`80/20` packing or distribution factors, grain vs dry/green leaf feed groups,
`400-500g` and `600g` thresholds, `F1` `11-15kg`, `F2` `15-20kg`, pregnancy
policy, and future tag-reader/device evidence as reviewed, rejected, or
draft-only source values.

The authored Feed Config payload must preserve the template pipeline explicitly:
`parameter_template.source_tables`, `parameter_template.parameter_families`,
`parameter_template.dimension_keys`, `parameter_template.ratio_policy`,
`validation_policy.checks`, and `validation_policy.calculation_outputs`.
Validation must block unknown aliases, missing source hashes, invalid or global
ratio promotion, slot weights that do not cover the full as-fed daily quantity,
non-numeric quantities, broken formula/reference evidence, missing resolver
coverage, and calculation-preview mismatches before any generation run can use
the rows. For shifted pregnant/lactating/warm-up cohorts, validation must also
block missing destination-shed ration context, missing stock/slot capacity, or a
stale source-shed ration snapshot.

`session_policy` is a versioned admin config family, not a fixed enum. It must
store active session slots for the target date with slot code, label, serving
time, sort order, effective range, split weight, and optional feed-item/scope
inclusion. The default policy is the docx Session 1 `09:00` / Session 2 `15:00`
with `0.5` + `0.5` weights. Feed Director draft plus COO/CEO publish can add,
disable, reorder, or reweight slots in a new effective-dated protocol version.
Validators must require at least one active slot, unique codes/order per scope,
weights that sum to the full daily as-fed quantity for each applicable feed
item/scope, and explicit supersession if a policy change touches an already
generated target date.

Pregnant-animal timing notes from the KT, such as `12:30-15:00` or
`14:00-15:00`, are not product clocks. They can become scoped slot/session
policy only through the same effective-dated approval path, and must not rewrite
the docx `09:00`/`15:00` default by accident.

`nutrition_cohort_key` and `ration_context_resolver_policy` keep source
constraint tables separate from shed truth. Uploaded breed/tag/energy constraint
tables define reviewed ration cohorts; they do not prove which shed has that
cohort. The resolver policy must define accepted source evidence, split behavior
when a shed + breed row maps to multiple cohorts, and the fail-closed blocker for
missing context.

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

Workbook tab names are not product module or table names. `CPT Validation`,
`CBE Validation`, `Feed-Energy-Protein`, `Supply Planning`, `Template`,
`Feed Packing Form`, `Feed Transport Form`, and `Feed Consumption & Wastage`
are import/parity labels only.

Initial solver scope is feed-type-level only: hard floor/ceiling, structural
ratio, category floor, and quantity floor. Item-level feed ceilings and
palatability modeling are deferred. The `60:40` structural ratio applies only to
Milking/Fattening tags, roughage/category floor values such as 30 percent must be
confirmed per tag before hardcoding, cost minimization means no paired overshoot
ceiling is needed, and a reviewed re-solve replaces the RationTable output
wholesale with no versioned blend.

## 5. Feed Generation Persistence

Add only the feed-specific tables the generic kernel cannot naturally represent.

Candidate tables after the current migration tail:

| Table | Required | Purpose |
| --- | --- | --- |
| `feed_direction_generation_runs` | yes | Full/Diff run header and manual bridge-log header where needed, idempotency, source hash, status |
| `feed_direction_count_input_rows` | yes | Immutable projection snapshot consumed by the run |
| `feed_direction_generation_rows` | yes | Canonical shed/session/feed/cohort instruction rows |
| `feed_direction_import_batches` | maybe | Typed import/review batches for workbook or solver-output migration if protocol source metadata alone is not enough for repair/audit |
| `feed_direction_bridge_events` | yes | Manual post-cutoff high-priority addition bridge log; not a generated bridge Diff |
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
row_kind: full | diff_restatement | bridge_log_reference
source_facing_net_delta_base_units nullable
active_instruction_key
superseded_by_run_id nullable
```

Here `stage_tag_id` is the ration/shed-tag context resolved from reviewed shed
reference data for the generation row. It is not part of the physical Base Count
anchor, which remains aggregate shed + breed.

Quantity convention for first build:

- solids: grams as integer base units;
- liquids: milliliters as integer base units;
- generation rows store immutable `quantity_base_units` plus `quantity_unit`;
- display conversion belongs to API/UI contracts;
- Feed Direction, Diff, packing, and field contracts display as-fed gross
  quantities only; `wastage_factor` and `DM_factor` stay internal to nutrient
  accounting and cannot surface as field-facing instruction quantities;
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
resolve ration cohort context for every shed + breed count row
lookup ration rows
apply as-fed rounding policy at approved grain
load target-date session_policy and split by active slot weights
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
Do not implement the superseded `07:30` next-morning system Diff for
high-priority bridge additions. Bridge handling is manual SOP proof and
exception logging only.

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
| Bridge | `stage_kind='bridge_exception'`, destination shed, source event/logical shifting reference where known, animal id or approved aggregate reference, timestamp, 2x ration quantity, proof reference, reconciliation state |

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
feed_context_match_percent_threshold
warmup_allowance_percent
feed_item_overrides
park_overrides
effective_from
review_status
source_reference
```

Legacy and KT evidence gives candidate values, including expected-vs-actual feed
comparison, a 20 percent wastage flag, `90-95%` shed/pack/breed/tag/energy
matching, and about `5%` warm-up allowance. These may be seeded only as draft or
candidate config until Feed Director review approves them.

The exception contract must separate shortage from unsafe surplus. Destination
shed shortage after shifting, under-consumption by pregnant/lactating/warm-up
cohorts, overpacked feed, moist or stale leftover feed, refusal-to-eat signals,
and sickness-risk remarks must each map to typed exception reasons with owner,
proof/rework link, audit row, and outbox/notification eligibility. None of these
states may be collapsed into a generic wastage percentage or silently carried
forward as reusable feed.

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
| `GET /feed-direction/bridge-events` | Manual bridge exception list with destination shed, animal id or approved aggregate reference, timestamp, quantity, proof reference, source event/logical shifting reference where known, and reconciliation state |

OpenAPI and generated clients must be updated in the same build slice. Labels,
filters, status copy, disabled reasons, and pagination semantics are backend
owned.

Frontend implementation must consume these contracts through generated clients.
`G1` is reopened for Feed Direction build as of 2026-06-30, but mock anatomy
still governs visual execution: pagination, buttons, icons, typography, spacing,
colors/tokens, hover/active/focus/disabled states, drawers, filters, tables,
empty/error states, and proof/status surfaces must match
`mock/goatos-dashboard-mock.html` or a deliberately updated Feed mock. Do not
ship a Feed UI that is only functionally wired but visually plainer than the
Mesha mock system.

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

Clock inventory for `G3` has two lanes.

The product-clock lane is controlled by `Feed, Shiftings and Count.docx`, with
two serving slots as the default published `session_policy`. Because the same
docx flags 50/50 as a deliberate simplification to revisit, GoatOS must keep
serving slots configurable through approved protocol versions rather than code:

| Clock | Product meaning |
| --- | --- |
| Day N `09:00` | Full Feed Direction for Day N+1 |
| Day N `13:30` | Cutoff for Day N+1 Diff inclusion |
| Day N `13:30-13:45` | Diff for shiftings raised between full direction and cutoff |
| Day N `15:00` | Packed and diff-corrected feed staged outside sheds |
| Day N+1 `09:00` | Session 1 served from staged stock |
| Day N+1 `15:00` | Session 2 served from staged stock |

If an admin-approved policy adds or changes serving slots, FeedDirection, Diff,
packing, transport, consumption, wastage, proof, and read-model rows must be
generated per configured slot for the new effective date. Existing generated
rows remain immutable unless an explicit supersede/reissue path runs.

The legacy-audit lane inventories installed Apps Script triggers so cutover does
not miss old side effects. It is not a schedule proposal.

| Legacy audit family | Evidence path/functions | Required `G3` decision |
| --- | --- | --- |
| Current feed packing and transport | `slack-automation-scripts/unified_automation.js`: `UE_installFeedPackingTrigger`, `UE_installFeedPackingAfternoonTrigger`, `UE_installFeedTransportTrigger` | retain, retire, or replace only after checking against the docx clocks |
| Counts/Shifting updates and recovery | `slack-automation-scripts/counting_db_automation.js`: `setupTwelveAMTrigger`, `setupThreeAMTrigger`, `setupFourAMTrigger`, `setupTwoPMTrigger`, `setupWatchdogTrigger` | retain, retire, or replace under the Counts/Shifting projection contract |
| Older feed packing, Diff/change, consumption list, archive, retry, and transport paths | `slack-automation-scripts/feed_automation.js`: `createFeedDirectionTrigger`, `createFeedDirectionDifferenceTrigger`, `createFeedDirectionChangesTrigger`, `createFeedConsumptionTrigger`, `creat2PMTrigger`, `create3AMTrigger`, `createDailyArchiveTrigger`, `createChangeFDTrigger`, `wrapWithRetry_`, `sendTransportMessages` | usually retire or replace; retain only with explicit Feed Director approval against the docx |
| Video/proof, quantity, stock, wastage, and alert paths | `slack-automation-scripts/video_verification_system.js`: `createFeedPackingCheckTrigger`, `setupDailyTrigger`, `setupStockTrigger`, `setupWastageSummaryTrigger`, `setupStockAlertTrigger` | replace with typed GoatOS proof, stock, wastage, rework, alert, or retry policy where still needed |

Some legacy comments/logger text disagree with actual `ScriptApp.newTrigger`
hour/minute values. The `G3` audit records the installer shape as cutover
evidence, then separately decides whether GoatOS retires the path or replaces it
with kernel scheduler/sweeper/reminder/proof policy. The legacy time value alone
is never enough to create a GoatOS schedule.

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
- Legacy trigger windows and installed trigger code -> `G3` audit evidence only.
  Use the audit to retain, retire, or replace feed/count/watchdog/archive/retry,
  proof, stock, wastage, quantity-check, and alert side effects. The docx clocks
  remain the only default Feed Direction product schedule.
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

If `G10` is owner-deferred, the consequence is not "security later." It means
Slack/App Script bridge execution remains disabled: no Slack overlap, no
Slack-delivered proof/transport/packing bridge, and no Slack bridge reuse can
count as Feed Direction done until the closeout above is complete.

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

1. Close `G2` Counts/Shifting projection in its sibling PRD/TRD, or add an adapter
   stub that fails closed with explicit `G2` readiness state.
2. Close `G3` by first confirming the docx default clocks, then auditing
   legacy installed trigger functions, archive/retry/watchdog behavior, and
   proof/stock side effects only for retain/retire/replace cutover decisions.
3. Close `G4`-`G6`: add ration DSL validators, provenance requirements, KT
   candidate parameter review, Warmup/pregnancy/K0/K1/Experiment sign-off, and
   quantity/precision decisions.
4. Add generation run/count snapshot/generation row/manual bridge-log
   migrations.
5. Build full generation service and idempotency tests.
6. Build Diff service and stale-obligation cancel/supersede helper.
7. Build `G7` stage obligations, durable `stage_kind`, and proof/rework wiring.
8. Wire inventory reserve/consume/release with exact base-unit to
   `numeric + quantity_unit` persistence.
9. Close `G8` transport map provider and tests.
10. Close `G9` exception threshold policy and variance tests.
11. Close `G10`; if bounded instead of closed, prove Slack bridge disabled.
12. Close `G11`-`G15`: reminder/escalation, notification, missed/recovery,
    audit, observability, and command-lens field mapping paths.
13. Close `G16`-`G17`: APIs, OpenAPI, generated clients, read models, cursor
    semantics, and plan checks.
14. Under reopened `G1`, update mock anatomy, build frontend, run
    `npm --prefix apps/admin-web run check:mock-fidelity`, and capture rendered
    desktop/mobile visual proof after Feed-owned backend contracts and source
    data gates make the surface truthful.
15. Run the build-to-done review gate from
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
- Bridge decision table proving manual log/proof/reconciliation only, with no
  generated `07:30` next-morning Diff and no source-shed claw-back.
- Durable `stage_kind` supports packing, transport, consumption/wastage, bridge,
  and execution-bucket queries without label parsing.
- Packing discrepancy rework tests cover both legacy alert/reset evidence and
  GoatOS typed rework/re-issue policy without relying on Sheet flag resets.
- Shifted pregnant/lactating/warm-up tests prove destination-shed recompute,
  shortage fail-closed behavior, unsafe surplus/wastage exception creation, and
  bounded query plans suitable for million-goat scale.
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
  ingress, auth/RBAC, idempotency, and audit before overlap; if `G10` is
  deferred, tests/proof must show the Slack bridge is disabled rather than
  partially accepted.
- Cursor lists and SQL plan gates.
- High-effort review-agent findings are resolved or explicitly owner-deferred
  before GitHub push.
