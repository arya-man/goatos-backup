# Feed Direction Sheets/App Script Replacement PRD

**Status:** Draft for implementation sequencing
**Date:** 2026-07-19
**Updated:** 2026-07-20 (experiment feed corrected to absolute per-shed kg allocation)
**Owner:** Feed Direction / GoatOS Protocol Engine
**Companions:** [Feed Direction PRD](./PRD.md),
[Feed Direction TRD](./TRD.md),
[Mobile SOP Cutover PRD](./SOP-MOBILE-CUTOVER-PRD.md),
[Mobile SOP Cutover TRD](./SOP-MOBILE-CUTOVER-TRD.md),
[Dependency Closure PRD](./DEPENDENCY-CLOSURE-PRD.md),
[Dependency Closure TRD](./DEPENDENCY-CLOSURE-TRD.md),
[Counts/Shifting Closure PRD](./COUNTS-SHIFTING-CLOSURE-PRD.md),
[Counts/Shifting Closure TRD](./COUNTS-SHIFTING-CLOSURE-TRD.md),
[legacy incident investigation](./legacy-feed-direction-incident-investigation-2026-07-16.md),
and [Protocol Engine state machines](../protocol-engine/state-machines.md).

The reusable discovery inventory is
[Feed Direction Legacy System Reference](../../context/source-findings/feed-direction-legacy-system-reference.md).

## 1. Product story in plain language

Feed Direction is the daily operating system for feeding the farm.

Every day, Mesha needs to answer a simple but unforgiving question: for every
park, shed, session, and feed item, how much feed should the ground team pack,
move, serve, and prove by video? The answer depends on yesterday's real counts,
tomorrow's projected shed composition, breed/stage nutrition rules, feed
availability, late shiftings, and proof that the right quantity was actually
packed and consumed.

The legacy system answers this with Google Sheets, Apps Script, and Slack:

```text
Counting DB / FutureDB
-> Feed Directions Automation DB / Count-DB / Supply Planning / Feed Direction
-> Slack packing and consumption messages
-> Feed Packing Form / Feed Consumption & Wastage
-> Video Verification DB checks videos and remarks
```

That flow works because people understand it, but it is fragile because mutable
sheets, processed flags, hidden formulas, Apps Script retries, and Slack message
state are acting as the source of truth. A missing FutureDB row can skip sheds.
A duplicated Feed Direction append can double quantities. A Slack quota failure
can leave operators unsure which messages were sent. A video may exist in a form
while the verification sheet import is broken.

GoatOS must replace that with a database-owned, replayable, verified process:

```text
canonical count + shifting ledger
-> immutable one-day projection snapshot
-> reviewed feed_direction protocol/config version
-> immutable generation run and instruction rows
-> generic obligation/batch/SOP/inventory kernel
-> notification bridge to Slack/mobile
-> proof/video verification, rework, escalation, and read models
```

Slack can remain a delivery channel during migration. Sheets can remain source
evidence and parity fixtures. Neither can remain runtime truth.

## 2. Source inventory and authority

### Source documents, converted sources, and findings used for this PRD

- `/Users/ravi/mesha/wiki/graphify-out/converted/Feed, Shiftings and Count_27cef16d.md`
- `/Users/ravi/mesha/wiki/Feed transfer KT – 2026_06_24 15_00 IST – Notes by Gemini.docx`
- `/Users/ravi/mesha/wiki/graphify-out/converted/Feed transfer KT – 2026_06_24 15_00 IST – Notes by Gemini_5d7819a8.md`
- `/Users/ravi/mesha/wiki/graphify-out/converted/Experiment Feed Directions Automation DB_8e6ae49a.md`
- `/Users/ravi/mesha/wiki/graphify-out/converted/Feed-Counting-and-Video-Verification-Flow_db8be3ab.md`
- `/Users/ravi/mesha/wiki/graphify-out/converted/Mesha-dept-directors_3e7ab556.md`
- `/Users/ravi/mesha/slack-automation-scripts/experiment_sheds.js`
- `/Users/ravi/mesha/slack-automation-scripts/feed-automation-trigger-inventory.md`
- `/Users/ravi/mesha/slack-automation-scripts/feed-automation-defect-ledger.md`
- `context/source-findings/feed-transfer-kt-2026-06-24.md`
- `context/source-findings/feed-direction-counting-db-reconstruction.md`
- `context/source-findings/feed-direction-workbook-automation-findings.md`
- `docs/feed-direction/PRD.md`
- `docs/feed-direction/TRD.md`
- `docs/feed-direction/legacy-feed-direction-incident-investigation-2026-07-16.md`
- `docs/protocol-engine/state-machines.md`

### Legacy production surfaces to replace

Known production workbook:

```text
Feed Directions Automation DB
spreadsheet id: 1RYY0YNLL0VMZ7Eob7AxwqjaVisX5abHY9U-vE5ky1Mw

Experiment Feed Directions Automation DB
spreadsheet id: 1Hs6PVH0U9nE52YBGxGVWNloiWgS7mZ45A5GpEvqHHUw

Video Verification DB
spreadsheet id: 1qxruWQwfXGLBUBce179ah43IIt8zGWs3j8U8Q-tJ1pk
```

Relevant sheets/tabs and source roles:

| Legacy surface | Role today | GoatOS target |
| --- | --- | --- |
| Counting DB `Template` | Farm/shed structure reference | Canonical `locations`, `shed_profiles`, governed source import/evidence |
| Counting DB `DB` | Official finished-day count | `count_base_anchors` and count/shifting ledger evidence |
| Counting DB `FutureDB` | Tomorrow projected count for feed | `count_projection_snapshots` and `count_projection_snapshot_rows` with horizon `feed_target_date` |
| Feed Directions Automation `Count-DB` | Feed-local copy of projected counts | No runtime copy; Feed consumes the Counts/Shifting projection port and snapshots its consumed input |
| `CPT Supply Planning` / `CBE Supply Planning` | Per-feed quantity staging | Reviewed ration/config preview and immutable generation rows |
| `CPT Validation` / `CBE Validation` / `Validation-BW` / `Feed-Energy-Protein` | Formula/feed-vector validation | Typed feed vector, constraint, alias, and solver/import config with review and publish gates |
| `Template` | Farm sessions and feed sets | Versioned `session_policy` inside published `feed_direction` protocol config |
| `Feed Direction` | Materialized directions, processed flags | Generation run + instruction snapshot rows + obligations/stages + bounded canonical command-lens queries |
| `Feed Packing Form` | Packing submissions and videos from Slack replies | SOP submission/proof records, packing stage completion, verifier state |
| `Feed Transport Form` | Transport proof and status | Transport stage obligation/record with consolidation map |
| `Feed Consumption & Wastage` | Consumed/wasted quantities and videos | Consumption/wastage stage record, variance exception, proof verification |
| Experiment Feed Directions Automation `Experiment Feed Config` | Per-shed kg source that works around normal Sheet/App Script allocation limits | Native absolute per-shed kg allocations inside Feed Direction (hand-entered kg per feed item, split across sessions); optional comparison metadata, never a second backend module |
| Experiment Feed Directions Automation `Feed Direction` | Materialized directions for sheds on the absolute-kg allocation | Normal Feed Direction generation rows flagged as an experiment absolute-kg allocation when a shed uses one instead of the per-head ration |
| Experiment Feed Directions Automation `Feed Packing Experiment Sheds` | Packing proof/checklist rows for experiment absolute-kg sheds | The same packing stage obligations/proof model as every other Feed Direction row |
| Experiment Feed Directions Automation `Wastage Experiment Sheds` | Wastage quantity/media feedback | Observation/wastage records linked to the effective absolute-kg allocation and feed stage |
| Experiment Feed Directions Automation `Feed Distribution Experiment Sheds` | Distribution media and direction-vs-consumption check | Distribution/consumption proof records and variance reconciliation in the normal Feed Direction flow |
| Video Verification DB | Operational media review | Platform proof/video verification queue and read model |

### Why legacy experiment feed exists and the GoatOS product decision

Legacy experiment feed exists because the normal Google Sheet and Apps Script
cannot express all of the allocation flexibility the Feed Director needs. It is
a workaround for the tool, not a separate farming domain that GoatOS should
copy.

The normal workbook is effectively driven by shared breed/shed-tag/category
per-head rules. That works for the default ration, but it means sheds with the
same tag receive the same per-head rate. The missing capability is simple: the
Feed Director must be able to hand-enter an absolute per-shed daily quantity (kg
per feed item) for a selected shed even when it shares a tag with others, have
that shed total split across the day's sessions, compare what happened, and carry
the adjusted kg into the next day. Head count is informational and is never
multiplied in.

The live `Experiment Feed Config` proves the same-tag case directly: three CBE
Castro sheds are all category `Sheep M NEW`, yet each carries a different implied
per-head rate — Castro 1 = 0.969 kg/head concentrate (62 kg / 64 head), Castro 2
= 0.667 (50 / 75), Castro 3 = 0.667 (44 / 66). The config cells are plain typed kg
numbers, not `count x rate` formulas; a genuine per-head composition would give
all same-tag sheds one shared rate, so these differing rates prove the allocation
is a hand-tuned absolute per-shed total, not a per-head composition. GoatOS must
support that ordinary per-shed freedom without first forcing the user to create a
formal experiment. The committed finding is intentionally structural rather than a
dump of private workbook rows.

The legacy workaround is the separate `Experiment Feed Config` plus six CBE/CPT
Slack channels covering packing, distribution, and wastage. They let operations
run permutation/combination trials such as:

- same farm + same shed tag, but different absolute concentrate/roughage kg;
- selected sheds given different absolute per-shed kg for comparison;
- absolute per-shed kg changed after observing wastage, consumption, media proof,
  or operator remarks;
- tomorrow's experiment packing direction regenerated from the hand-entered per-shed
  kg values for that shed rather than from its shared per-head tag default.

The Slack threads prove the operational evidence loop: operators are prompted
for packing, distribution/water, and wastage videos, and the workflow records
completion. The afternoon packing timing is part of that loop. The observed Apps
Script trigger creates `sendExperimentPackingMessages` at about 14:15 IST, even
though an old comment says 07:15. GoatOS therefore needs a review window in
which the Feed Director can inspect same-day distribution/wastage evidence,
revise or retain the absolute per-shed kg for the enrolled experiment sheds, and
issue that for the next-day packing direction. Slack proves
collection and delivery; GoatOS audit history, not a Slack message, must prove
who set the next-day absolute kg and why.

GoatOS should not rebuild this as a duplicate `experiment_feed` backend with
parallel Feed Direction, packing, wastage, Slack, and video-verification tables.
The right product shape is:

```text
default per-head ration for every shed
+ optional absolute per-shed daily kg (kg per feed item) hand-authored for selected experiment sheds, split across the day's sessions
+ optional comparison label/group when the team wants to analyse alternatives
-> one Feed Direction generation/stage/proof/outbox flow
```

In other words, an absolute per-shed kg allocation is a normal Feed Direction
capability. `Experiment` may remain as an optional label for comparing variants,
but a hypothesis, control/treatment wrapper, or separate experiment lifecycle
must not be required just to give two same-tag sheds different rations. The
generated instructions, obligations, proof capture, Slack/mobile delivery,
video verification, inventory, replay, and watchdogs all use the same kernel
path.

### Authority order when sources disagree

1. `Feed, Shiftings and Count.docx` controls the target Feed Direction model:
   base counts, append-only shiftings, one-day projection, Day N 09:00 full
   direction, 13:30 cutoff/Diff, 15:00 staging, Day N+1 09:00/15:00 serving,
   50/50 session split, manual high-priority bridge, as-fed quantities, and
   ration solver constraints.
2. Goat/park/shed semantics come from
   `context/source-findings/goats-and-parks-source-findings.md` and related
   source findings: park/shed scope, shed tags, lifecycle/stage, warm-up,
   pregnancy, lactation, fattening, milking, feed roles, and feed safety.
3. The exact KT notes are directional evidence for configuration and nutrition
   constraints. They do not override the docx clocks or 50/50 default unless
   the Feed Director approves a new effective policy.
4. Sheets and Apps Script are migration/parity evidence, not target schema.
5. Current GoatOS protocol/kernel code determines implementation shape.

## 3. Current legacy timing versus GoatOS target timing

Legacy timings are useful for migration because operators depend on them today.
They are not automatically the final GoatOS schedule.

| Time, India | Source | Current meaning | GoatOS treatment |
| --- | --- | --- | --- |
| ~14:00 Day N | Current live guide | Counting DB daily prep | Replace with count/shifting import/recompute jobs or UI actions; not a Feed-owned sheet prep |
| ~23:30 Day N | Current live guide | `DB` filled with real counts for the finished day | Replace with `count_base_anchors`/shifting events and immutable import run evidence |
| ~00:30 Day N+1 | Current live guide | FutureDB night check updates projection | Replace with bounded `count_projection_recompute_runs` for `feed_target_date` |
| ~01:00 Day N+1 | Current live guide | FutureDB rolled to tomorrow's projected shed counts | Replace with projection snapshot ready check; user specifically flagged ~1:00 afternoon consumption too, which must be verified against live trigger config before hardcoding |
| ~03:00 | Current live guide | Legacy Experiment Feed Direction generation | Retain only as a migration clock for experiment absolute-kg allocations if the owner approves; per-head ration and absolute-kg allocations use the same generation engine |
| ~07:15 | Current live guide and incident evidence | Experiment morning wastage/distribution and main morning consumption family | Migration notification bridge may mimic this, but canonical work must already exist in GoatOS |
| ~07:30 | Current live guide and incident evidence | Main packing Slack from `Feed Direction` | Migration notification bridge only; final product clock should be approved against the docx model |
| ~13:00 | User-supplied migration requirement | Afternoon consumption Slack timing to preserve/check | Mark as uncertain until live trigger inventory confirms exact Apps Script trigger and session semantics |
| ~14:15 | Experiment trigger inventory and `experiment_sheds.js` creator | Legacy experiment packing Slack for next-day absolute-kg allocation after same-day feedback/wastage can be reviewed | Model as a configurable notification/stage due time for experiment absolute-kg work, not a separate sender backend |
| 09:00 Day N | `Feed, Shiftings and Count.docx` | Full direction for Day N+1 | Recommended GoatOS default full-generation product clock |
| 13:30 Day N | `Feed, Shiftings and Count.docx` | Cutoff for changes that affect Day N+1 | Recommended GoatOS default cutoff |
| 13:30-13:45 Day N | `Feed, Shiftings and Count.docx` | Diff for affected sheds | Recommended GoatOS default Diff job window |
| 15:00 Day N | `Feed, Shiftings and Count.docx` | Truck loads/stages packed feed outside sheds | Recommended GoatOS default packing/staging execution window |
| 09:00 and 15:00 Day N+1 | `Feed, Shiftings and Count.docx` | Session 1 and Session 2 serving | Default published session policy, 50/50 split |

Open decision: during migration, GoatOS can run legacy-compatible Slack
notifications at ~07:15/~07:30/~13:00 while the canonical instruction was
generated earlier. That is a notification bridge decision, not a reason to make
Slack/Scripts the system of record.

## 4. Existing GoatOS schema and code surfaces

The current repo already contains the generic kernel pieces Feed Direction
should reuse:

- `protocol_definitions`, `protocol_versions`, `protocol_rules`,
  `protocol_rule_dimensions`, and `protocol_triggers`, including
  `category='feed_direction'`.
- `protocol_versions.rule_dsl` and `proof_policy` for reviewed, immutable,
  effective-dated feed configuration.
- `obligation_instances`, `obligation_batches`, `obligation_status_events`,
  deadline/escalation state, and SOP task/submission tables.
- `inventory_stock` and `inventory_stock_movements`; Feed must reserve/consume
  through the inventory app, not direct balance mutation.
- `feed_direction_completions`, one row per `obligation_id`, with quantity,
  unit, head count, verifier fields, rejection reason, and idempotency key.
- Counts/Shifting tables: `count_base_anchors`, `shifting_events`,
  `shifting_event_impacts`, `count_projection_snapshots`,
  `count_projection_snapshot_rows`, `count_projection_exceptions`,
  readiness gates, and import/recompute run tables.
- Counts app ports:
  `CountAsOf(ctx, CountProjectionRequest)` and
  `ProjectedCountFor(ctx, CountProjectionRequest)`.
- OpenAPI/generated client surfaces for Feed Direction readiness, generation
  preview, and Counts/Shifting projection exceptions.
- Admin Config code paths for `feed_direction` rule DSL authoring.

Schema gaps before full replacement:

| Gap | Why it matters | Recommended target |
| --- | --- | --- |
| Feed generation run header | Need immutable full/Diff attempt, source hash, status, actor/job metadata | Add `feed_direction_generation_runs` |
| Feed count input snapshot | Need to prove exactly which projection rows were consumed | Add `feed_direction_count_input_rows` or equivalent immutable snapshot table |
| Feed instruction rows | Need canonical active/superseded shed/session/feed/cohort rows, not live formulas | Add `feed_direction_generation_rows` |
| Stage discriminator | Packing/transport/consumption/wastage/bridge cannot all fit in one undifferentiated completion row | Prefer separate stage obligations with indexed `stage_kind` context/projection, or add `feed_direction_stage_records` |
| Bridge log | Post-cutoff high-priority additions need 2x-ration evidence and reconciliation | Add `feed_direction_bridge_events` |
| Operational read model | Operators need buckets without scanning raw rows | Add `feed_direction_projection_rows` only if generic joins cannot meet bounded-read requirements |
| Slack notification ledger | Must know requested/sent/failed/reconciled messages durably | Use/extend notification/outbox storage with feed-specific idempotency and Slack delivery references |

Do not add a Sheet-shaped typed stack named after old tabs (`feed_directions`,
`feed_packing`, `feed_consumption`, `feed_transport`) unless a later TRD
explicitly reverses the current kernel decision. The ratified direction is
generic protocol/obligation/SOP/inventory plus feed-specific snapshots and stage
detail.

## 5. Functional requirements

### FR1 — Governed feed configuration

GoatOS must provide a `feed_direction` configuration pack that can be drafted by
the Feed Director and published by COO/CEO authority.

The config pack must include:

- feed item nutrient vectors: metabolizable energy, crude protein, dry matter
  factor, wastage factor, type, cost, availability status, source/version;
- feed type families: concentrate, roughage, grains, dry leaves, green leaves,
  and any approved farm terminology aliases;
- ration dimensions: species, breed, shed tag/stage, kid weight band/ADG,
  pregnancy/lactation/warm-up policy, farm/park scope, feed vector family, and
  effective dates;
- experiment absolute-kg dimensions: absolute per-shed daily quantity (kg per
  feed item), shed/date/session scope, start/end dates, allocation reason,
  owner/reviewer, and optional comparison label/group; head count is
  informational only and is never multiplied in; a formal hypothesis,
  control/treatment model, and stop criteria are optional analysis metadata, not
  prerequisites;
- session policy: slot codes, serving windows, split weights, feed item
  inclusion, ordering, and disabled reasons;
- eligibility/exclusion policy: K0/K1 milk-fed handling, ICU/quarantine,
  warm-up, and other exclusions only after Feed Director approval; a shed on an
  experiment absolute-kg allocation is not excluded from normal Feed Direction
  execution;
- validation tolerances: KT 90-95% shed/pack/breed/tag/energy style checks and
  warm-up allowance only if approved as explicit thresholds;
- proof policy: required media count/type, verifier roles, rejection reasons,
  rework rules, and deadlines;
- bridge policy: high-priority post-cutoff 2x-ration logging and evidence.

KT values such as `80/20`, `400-500g`, `600g`, `F1` 11-15kg, `F2` 15-20kg,
pregnant priority windows, and 90-95% validation are candidate row values. They
must not be hardcoded as global constants.

### FR1a — Native absolute per-shed kg allocation (legacy "experiment feed")

GoatOS must let the Feed Director hand-author an absolute per-shed daily quantity
(kg per feed item) for any enrolled shed/date/session without forking the Feed
Direction workflow or first creating a separate experiment. Head count is
informational and is never multiplied in; the shed total is split across the
day's sessions.

Requirements:

1. An experiment allocation records feed item, absolute per-shed daily quantity,
   unit, session scope, and source evidence. The authored kg is itself the
   instruction; there is no per-head rate and no separate approval lifecycle.
2. An allocation applies to a shed for an effective date/session range. It
   coexists with the normal shed tag, so two sheds with the same tag may
   intentionally receive different absolute kg totals.
3. Generation resolves the default per-head ration and then applies the single
   matching absolute-kg allocation for the shed, splitting that shed total across
   the day's sessions. More than one matching allocation fails closed with an
   owner-visible blocker.
4. Optional comparison metadata may group allocations under a named comparison
   and labels such as `control`, `variant-a`, or `variant-b`. Ordinary per-shed
   tuning must work without that metadata.
5. Packing, distribution, consumption/wastage, media proof, rework, Slack/mobile
   notifications, and Video Verification all use the normal Feed Direction
   kernel path with the effective absolute-kg allocation reference attached.
6. Consumption, wastage, distribution/water, proof, and remarks must be
   queryable by allocation, optional comparison group, shed, date, session, and
   feed item so the Feed Director can set the next day's absolute kg before the
   next packing direction is sent.
7. Ending an allocation returns the shed to the default per-head ration without
   deleting prior allocation, evidence, or observation history.

### FR2 — Counts/Shifting input contract

Feed generation must consume a one-day projection from Counts/Shifting, not
copy rows from `FutureDB`.

The consumed projection row must expose:

- tenant, park, shed, target date, horizon, as-of time;
- breed id/key/label;
- head count;
- structured stage/tag/age/sex where known;
- pregnant, lactating, and warm-up counts;
- `ration_context_resolution_state`;
- ration context reference when resolved;
- blocker reason when unresolved;
- source contract version and source hashes.

Generation must fail closed if a row has unknown breed, unresolved shed/stage,
unreviewed alias, missing structured shifting impact, unresolved pregnancy or
warm-up policy, stale projection, or missing base count anchor.

### FR3 — Full direction generation

At the approved full-generation schedule, GoatOS must:

1. Lock the source inputs by reading the published `feed_direction` protocol
   version and the immutable Counts/Shifting projection snapshot for the target
   date.
2. Validate source hashes, effective dates, aliases, session weights, feed
   vectors, solver/import approvals, and stock-readiness policy.
3. Calculate as-fed daily quantities by shed, breed/cohort, feed item, and
   session.
4. Write a generation run and immutable count/ration/instruction rows.
5. Create shed/session/feed stage obligations and batches through the generic
   kernel.
6. Emit outbox/domain events for notifications and stage tasks; command lenses
   read bounded canonical rows in the current scale envelope.

Field-facing instructions must show as-fed gross quantities only. Dry matter and
wastage factors remain nutrition-accounting inputs, not packing labels.

### FR4 — Diff generation and supersession

At the approved cutoff/Diff schedule, GoatOS must:

1. Collect shiftings eligible for the target date since the full-generation run.
2. Recompute affected sheds using the same protocol/config version unless an
   explicit restatement policy says otherwise.
3. Store canonical affected-shed restatement rows.
4. Optionally emit a source-facing net correction view for operations.
5. Cancel or supersede stale open obligations before creating replacement work.

Idempotency keys alone are not enough. Old live instructions must be explicitly
closed so duplicate or contradictory packing work cannot survive.

### FR5 — Packing, transport, consumption, wastage, and proof

Each execution stage must be durable and queryable.

Required stages:

- `packing`: planned quantity, actual packed quantity, feed lot, video proof,
  verifier status, shortfall/rework.
- `transport`: source-backed direction-shed to transport-shed consolidation,
  transport proof, verifier status, rejection reason, rework.
- `consumption_wastage`: consumed quantity, wasted quantity, variance, proof
  links, verifier status, exception thresholds.
- `bridge`: high-priority post-cutoff 2x-ration manual top-up, proof, and
  reconciliation state.

Feed reserves stock only when packing starts. Accepted packing proof consumes
actual packed quantity and releases any remaining reservation. Rejected proof
does not consume stock.

### FR6 — Slack/mobile notification bridge

Slack messages during migration must be generated from GoatOS work, not from
Sheet rows.

Requirements:

- every outgoing message has a durable notification/outbox record;
- every Slack delivery uses an idempotency key derived from tenant, target date,
  park, shed/transport shed, session, stage, run/instruction row, and channel;
- Slack timestamp, channel, thread timestamp, and file ids are recorded only as
  delivery/proof references;
- retries are bounded and visible;
- reconciliation detects missing, duplicate, failed, or stale messages;
- staff replies/files close GoatOS tasks through authenticated API ingress,
  idempotency, and proof records.

If Slack remains during overlap, Apps Script must call GoatOS APIs and must not
mutate Sheets as canonical state.

### FR7 — Video verification and process integrity

The Feed Director and Video Verification team need to know:

- which packing/transport/consumption videos are missing;
- which proofs are pending, accepted, rejected, or require remarks;
- which operator/team owns rework;
- whether a rejection caused repacking, retransport, or consumption correction;
- whether a missed upload or SOP violation has been communicated and escalated.

This must reuse platform proof/video verification concepts. The `Mesha
dept-directors` source makes daily video double-verification and written
directions non-negotiable Feed Director duties, so proof gaps are product
failures, not optional analytics.

## 6. Domain model recommendation

### Existing tables to use as-is

| Domain need | Existing table/surface |
| --- | --- |
| Published feed policy | `protocol_definitions`, `protocol_versions`, `protocol_rules`, `protocol_rule_dimensions`, `protocol_triggers` |
| Generic work | `obligation_instances`, `obligation_batches`, `obligation_status_events`, `sop_tasks` |
| Stage completion | `feed_direction_completions` per stage obligation, or completion linked to stage records |
| Proof | `sop_submissions`, `sop_submission_items`, proof/media surfaces |
| Counts | `count_base_anchors`, `shifting_events`, `shifting_event_impacts`, `count_projection_snapshots`, `count_projection_snapshot_rows` |
| Inventory | `inventory_stock`, `inventory_stock_movements` |
| Events | `outbox_messages` |
| Parks/sheds | `locations`, `shed_profiles` |

### New feed-specific tables or equivalent projections

The names below are recommendations. Equivalent names are acceptable if they
preserve the same contracts.

```text
feed_direction_generation_runs
  generation_run_id
  tenant_id
  run_kind: full | diff | replay | dry_run
  target_date
  source_projection_snapshot_id
  protocol_version_id
  cutoff_window_start/cutoff_window_end
  source_hash
  idempotency_key
  request_fingerprint
  status: running | generated | blocked | superseded | failed
  row_count, blocked_row_count, obligation_count
  generated_by, trace_id, started_at, completed_at, last_error

feed_direction_count_input_rows
  generation_run_id
  projection_snapshot_row_id
  park_id, shed_id, target_date
  breed_id/key/label
  head_count
  pregnant_count, lactating_count, warmup_count
  ration_context_resolution_state
  ration_context_ref
  blocker_reason
  source_row_hash

feed_direction_generation_rows
  generation_row_id
  generation_run_id
  active_instruction_group_key
  row_kind: full | diff_restatement | bridge_reference
  target_date, park_id, shed_id
  session_code, stage_kind
  breed_id/key, ration_context_ref
  experiment_config_id (null for the normal per-head ration), optional_comparison_set_id
  feed_item_id
  planned_quantity_base_units
  quantity_unit
  source_facing_net_delta_base_units
  obligation_id, batch_id
  status: active | superseded | canceled | blocked

feed_direction_stage_records
  stage_record_id
  generation_row_id
  obligation_id
  stage_kind: packing | transport | consumption_wastage | bridge
  experiment_observation_id
  planned_quantity_base_units
  actual_quantity_base_units
  consumed_quantity_base_units
  wasted_quantity_base_units
  variance_base_units
  proof_ref, verifier_status, rejection_reason
  rework_obligation_id
  idempotency_key

feed_direction_bridge_events
  bridge_event_id
  tenant_id
  shifting_event_id/logical_key
  destination_shed_id
  animal_id or approved aggregate reference
  source_shed_tag_ref
  bridge_quantity_base_units
  proof_ref
  reconciliation_state
  idempotency_key

feed_direction_projection_rows (deferred measured-hotspot option; do not create in the current envelope)
  projection_row_id
  tenant_id, park_id, target_date
  shed_id or transport_shed_id
  session_code, stage_kind
  bucket
  experiment_config_id (null for the normal per-head ration), optional_comparison_set_id
  owner_ref, due_at, deadline_at
  proof_state, verification_state, escalation_state
  active_instruction_group_key
  cursor_key
```

The active 5k-50k design has zero Feed projection commands and zero Feed
projection tables. The deferred row shape above is only a scale-out sketch: it
requires measured query pressure, a replay/rebuild owner and an update to the
accepted operational-kernel scale ADR before implementation.

Quantities should use whole base units, preferably grams/ml as integer values,
with display conversion handled at the edge. Existing `numeric` columns can
remain where already committed, but new hot-path instruction/stage rows should
avoid ambiguous floating math.

### Native absolute-kg allocation extension (legacy "experiment feed")

These are normal Feed Direction policy records, not a second feed backend. They
let the Feed Director hand-author an absolute per-shed daily quantity (kg per feed
item) for selected experiment sheds while all downstream work still uses the
generation/stage/proof/outbox tables above. Head count is informational and is
never multiplied in; the shed total is split across the day's sessions by the
session policy.

```text
feed_experiment_config
  experiment_config_id
  tenant_id
  park_id
  shed_id
  feed_item_id
  absolute_quantity_base_units        -- hand-entered kg per feed item for the whole shed/day
  quantity_unit
  target_date_from, target_date_to, optional_session_code
  head_count_at_authoring             -- informational only, never a multiplier
  allocation_reason
  source_evidence_ref
  optional_comparison_set_id, optional_variant_label
  owner_user_id, reviewer_user_id
  idempotency_key
  no independent approval/effective status

feed_experiment_observations
  experiment_observation_id
  experiment_config_id
  optional_comparison_set_id
  generation_row_id
  stage_record_id
  target_date, session_code
  shed_id
  feed_item_id
  consumed_quantity_base_units
  wasted_quantity_base_units
  variance_base_units
  media_proof_ref
  verifier_status, rejection_reason
  operator_remarks, director_notes
  observed_at, recorded_by

feed_comparison_sets (optional analysis metadata)
  comparison_set_id
  tenant_id
  code, name
  owner_user_id, reviewer_user_id
  question_or_hypothesis nullable
  target_metric nullable
  effective_from, effective_to
  status: draft | active | paused | completed | canceled
  review_or_stop_criteria nullable
  approval_ref, source_ref
  created_at, updated_at
```

The shipped backend implements exactly this: `feed_experiment_config` holds the
absolute kg per shed and feed item, and the `ExperimentPlanner` reads that shed
total, ignores head count, and splits it across the day's sessions — surfaced as
the `Absolute kg (experiment)` workflow. The authored kg is itself the
instruction; there is no per-head rate and no separate allocation approval path.
Generated rows pin the immutable `experiment_config_id`, and observation records
can drive the next day's absolute-kg decision. Comparison metadata is additive and
optional; it must never become a gate for ordinary same-tag/per-shed absolute-kg
tuning.

## 7. Events, Pub/Sub, cron, and kernel responsibilities

### Event families

Existing events to consume:

- `counts.base_count_anchor.recorded`
- `counts.shifting_event.recorded`
- `counts.projection_exception.opened`
- `counts.projection_exception.updated`
- `counts.projection_exception.closed`
- inventory movement events, if/when emitted by inventory app
- SOP/proof verification events, if/when emitted by proof platform

Proposed Feed Direction events:

| Event | Producer | Consumer |
| --- | --- | --- |
| `feed_direction.generation.requested` | Scheduler/API | Feed generation logical stage in the kernel worker |
| `feed_direction.generation.blocked` | Kernel Feed generation stage | Action Center canonical query/alerts |
| `feed_direction.full.generated` | Kernel Feed generation stage | Obligation stage, notification planner |
| `feed_direction.diff.generated` | Kernel Diff stage | Obligation stage, notification planner |
| `feed_direction.instruction.superseded` | Kernel Diff/repair stage | Notification reconciler, inventory/stage guard |
| `feed_direction.stage.due` | Kernel sweeper | Notification planner |
| `feed_direction.packing.started` | API/mobile | Inventory reserve app |
| `feed_direction.packing.accepted` | Verification/API | Inventory consume/release app |
| `feed_direction.packing.rejected` | Verification/API | Rework/escalation |
| `feed_direction.transport.accepted` | Verification/API | Canonical stage/rework close |
| `feed_direction.consumption_wastage.recorded` | API/mobile | Variance checker |
| `feed_direction.bridge.recorded` | API/mobile | Reconciliation/canonical command lens |
| `feed_direction.notification.reconcile_failed` | Reconciler | Watchdog/escalation |

All events use the standard envelope: tenant/scope, aggregate owner,
subject, schema version, idempotency key, occurred_at, recorded_at, actor, trace,
and evidence refs.

### Topics/queues

Exact cloud names should be set by infra conventions at implementation time.
The logical queues are:

- domain event topic for canonical events;
- kernel work topic/queue for generation, Diff, stage due, sweeps, and retries;
- notification delivery topic/queue for Slack/mobile/email delivery;
- proof ingestion topic/queue for media/proof callbacks;
- DLQ/repair queue for poison messages and source row repair.

Postgres remains the source of truth. Pub/Sub only carries work. Every logical
stage in the existing modular kernel worker must be safe to replay from database
state if a message is lost. These stage names do not authorize separate Feed
worker deployments.

### Logical cadence schedule inside the kernel worker

All times are India business time and config-driven. Each row below is a
supervised stage/cadence in the existing worker, not a Cloud Scheduler cron or a
scheduled Cloud Run Job.

| Logical stage | Default target | Responsibility |
| --- | --- | --- |
| Counts source import/recompute | Before generation windows | Ensure `count_projection_snapshots` are ready or blocked with exceptions |
| Full Feed Direction generation | Recommended 09:00 Day N for Day N+1 | Create immutable full instructions and obligations |
| Diff generation | Recommended 13:30-13:45 Day N | Restate affected sheds and supersede stale work |
| Packing/staging due sweep | Recommended 15:00 Day N, or legacy bridge window during migration | Mark packing obligations due, reserve on start, notify operators |
| Absolute-kg feedback review window | Before ~14:15 experiment absolute-kg packing notification, if retained | Review same-day wastage/distribution observations, set the next day's absolute per-shed kg for the enrolled sheds, and issue it for packing |
| Legacy-compatible Slack packing | ~14:15 experiment absolute-kg and ~07:30 default-policy flows during migration only | Deliver notifications from GoatOS work to old operator channels |
| Consumption notification | Morning and afternoon sessions; legacy ~13:00 timing to verify | Remind/collect consumption and wastage proof |
| Watchdog/reconciler | Every few minutes during active windows | Detect missing/duplicate Slack messages, stuck outbox, no proof, failed imports, stale projections, stock-outs |
| Deadline sweeper | Generic kernel cadence | Missed, reminder, escalation, rework |

## 8. API and screen recommendations

### Backend APIs

Existing Feed readiness/preview/exception APIs should remain. Add or extend:

| API | Purpose |
| --- | --- |
| `GET /feed-direction/readiness` | Gate status, blockers, source evidence, config publishability |
| `GET /feed-direction/generation-preview` | Dry-run preview from selected projection/config version |
| `POST /feed-direction/generation-runs` | Trigger full/dry-run generation with idempotency |
| `POST /feed-direction/diff-runs` | Trigger Diff generation/replay with idempotency |
| `GET /feed-direction/generation-runs` | List runs, status, row counts, source hashes |
| `GET /feed-direction/directions` | Cursor-paged active/superseded instruction rows |
| `GET /feed-direction/buckets` | Bounded, indexed canonical command-lens query: blocked, packing due, shortfall, proof missing, transport pending/rejected, consumption incomplete, wastage exception, bridge exception, stock-out, missed/escalated/rework |
| `POST /feed-direction/stages/{stage}/start` | Start packing/transport/consumption stage; reserve stock where applicable |
| `POST /feed-direction/stages/{stage}/submit-proof` | Submit quantities, media refs, and idempotency key |
| `POST /feed-direction/stages/{stage}/verify` | Accept/reject proof with remarks and rework policy |
| `POST /feed-direction/bridge-events` | Record high-priority post-cutoff bridge top-up |
| `POST /feed-direction/slack/reconcile` | Internal bridge endpoint for Slack delivery/reply reconciliation |
| `GET /feed-direction/migration/parity` | Shadow-run parity and unexplained-delta report |

All mutating APIs require idempotency key, semantic request fingerprint,
tenant/scope checks, actor/capability checks, audit/outbox writes, and replay
tests.

### Admin-web screens

- Config → Feed Direction: feed vectors, constraints, ration/session policy,
  aliases, proof policy, validation preview, publish gates.
- Feed Direction readiness: G1-G17/CSG gates, blockers, owner, evidence.
- Generation console: preview, full run, Diff run, row blockers, source hashes,
  replays.
- Feed Direction operations: directions by date/park/shed/session/feed item,
  active/superseded status, proof state, stock state, next action.
- Projection exceptions: unresolved counts/ration context/alias issues.
- Video Verification queue: pending/rejected/accepted feed proofs with rework.
- Migration parity: legacy sheet row counts, GoatOS shadow rows, missing or
  duplicate instructions, Slack delivery reconciliation.

### Mobile/operator screens

- Today/tomorrow feed tasks by park/shed/session.
- Packing start/complete with camera proof and quantity capture.
- Transport proof with consolidated transport-shed work list.
- Consumption/wastage proof with consumed/wasted quantities.
- Rework tasks for rejected media or shortfall.
- Offline-first capture with later sync, idempotency, and no duplicate close.

Ground-operator park scope is not loosened for demos, throwaway databases, or
seeded E2E. A Feed operator phone shows exactly one park: the park that operator
is assigned to execute in. CBE and CPT require separate operator principals.
Multi-park Feed views are director/CEO oversight views for status and follow-up,
not ground execution surfaces.

## 9. Reliability history and non-negotiable architecture lessons

This section is intentionally explicit. Future GoatOS kernel/backend developers
must understand what broke in the Sheet/App Script system and why Feed Direction
cannot be rebuilt as "same script, cleaner code."

### Original incident pattern

The triggering legacy failure was a scheduled `sendPackingFeedDirections` run.
Slack accepted messages at first, including the Mandela 2 Part 3 point in the
operator-visible sequence, but Apps Script died around item/shed 52 before the
sheet tick/checkpoint was durably written for the remaining work. That left an
unsafe mixed state:

- some sheds had Slack messages;
- some sheds were not sent;
- some sent messages had no durable sheet checkpoint/tick;
- a rerun risked duplicate messages or double quantity for already-sent sheds;
- operators could not tell from the Sheet alone which Slack side effects had
  definitely happened.

The manual rerun worked only because the partial sheet ticks that did exist
reduced the remaining work. That was luck and operator intervention, not a
reliable state machine.

### Root causes in legacy architecture

- One long Apps Script execution attempted to process all sheds in one run.
- The script repeatedly scanned large Sheet ranges, including roughly
  43k/43,880-row scans, instead of reading a small immutable work queue.
- Per-shed Sheet writes were used as progress markers.
- Slack was called before the source-of-truth progress checkpoint/tick was
  durably committed.
- The retry wrapper depended on JavaScript `catch`; it could not recover from
  Apps Script platform/internal termination where the process simply stopped.
- Processed flags, row mutation, Slack thread state, and script properties were
  blended together as truth.
- The old flow had no durable external-side-effect reconciliation ledger that
  could prove "Slack accepted this message" versus "the request might have been
  sent but no timestamp was captured."

### Sandbox fixes and defects found over iterations

The legacy sandbox work validated the replacement shape by finding many defects
that a simple Apps Script rewrite would miss:

- durable queue instead of one monolithic send loop;
- batch continuation so a run can resume bounded chunks;
- watchdog for stuck, ambiguous, failed, and unsent work;
- source snapshot locking so a run sends from one frozen source view;
- exact date/session filtering;
- invalid date/session reporting instead of silent skips;
- canonical business idempotency keys independent of transient row numbers;
- Slack marker reconciliation with retry;
- parent-message, prompt, and PDF/file handling;
- ambiguous-versus-definite `429` distinction;
- reconciliation pending-attempt accounting;
- per-stage attempt tracking;
- watchdog preserving ambiguous `sending` state instead of incorrectly freeing
  work for duplicate sends;
- `ok:true` without `ts` handling;
- logging failure isolation so audit/log write errors do not corrupt business
  state;
- run-log finalization for partial and failed runs;
- source-sync initialization guard;
- prepare guard before enqueue/send;
- malformed queue payload guard;
- duplicate-oracle fix and negative duplicate control;
- deep Slack verification, not just optimistic API return checks;
- no production writes while validating fixes.

These lessons must become GoatOS backend/kernel requirements, not comments in a
legacy script.

### Design implications for GoatOS

GoatOS Feed Direction must use:

- a DB-backed outbox/idempotency table for every external side effect;
- state-machine job rows for generation, Diff, notification, proof ingest,
  reconciliation, and watchdog runs;
- transactional claim/lease semantics, so a worker owns a bounded batch and can
  safely expire or resume;
- Pub/Sub retry and dead-letter handling, with the database as replay source;
- external side-effect reconciliation for Slack/mobile delivery, file uploads,
  proof submissions, and inventory actions;
- immutable source snapshot per generation/send run;
- unique business keys across reruns, not row-index or Slack-thread guesses;
- explicit retry classification: retryable, permanent, ambiguous external send,
  quota/rate-limited, malformed payload, auth failure, and operator repair;
- bounded watchdogs that can identify stuck work without creating duplicates;
- observability/audit ledgers for run status, attempts, source hashes,
  notification ids, Slack timestamps, proof refs, DLQ items, and owner-visible
  repair state;
- sandbox/test-channel proof before production promotion.

The proposed `feed_direction_generation_runs`,
`feed_direction_generation_rows`, `feed_direction_stage_records` or equivalent
stage obligations, and notification/outbox ledger are the schema expression of
these lessons. `outbox_messages` and Pub/Sub carry side effects; canonical
instruction/stage rows and idempotency keys decide business truth.

### Legacy trigger inventory context

The migration plan must cover three known sender timings:

- ~07:15 morning/experiment/consumption sender family;
- ~07:30 main packing sender family;
- ~13:00 afternoon consumption sender family, pending exact live trigger
  verification.

Adjacent legacy flows need adapter-specific GoatOS designs before cutover:

- Feed Direction generation;
- FutureDB-to-CountDB refresh;
- Feed Direction changes/Diff paths;
- afternoon packing/change senders;
- daily archive;
- transport messages and transport proof;
- tick/checkpoint clear/reset;
- spreadsheet `onChange`;
- experiment feed generation and packing;
- video verification imports;
- stock and wastage alerts.

Do not collapse these into one generic "Slack bridge." Each adapter must name
its source snapshot, business idempotency key, stage kind, retry class,
reconciliation proof, watchdog rule, and promotion gate.

GoatOS reliability requirements:

- Durable queue: every notification and worker item is stored before delivery.
- Idempotency everywhere: generation, Diff, stage start, proof submit, verify,
  Slack send, Slack reply/file ingest, inventory reserve/consume/release.
- Same key + different payload is a conflict with no side effects.
- Source snapshot locking: generation records projection/config source hashes;
  replay uses the same snapshot or creates a new explicit run.
- No duplicate/missing messages: reconciliation compares expected notification
  ledger rows to Slack timestamps/replies and active obligations.
- Slack is delivery, not truth: deleting a thread or resetting a Sheet flag
  cannot change canonical work.
- Watchdog: alerts on stuck outbox, stale projection, incomplete Feed Direction
  rows before packing window, duplicate active instruction, failed Slack
  delivery, no proof by deadline, or App Script bridge retries exceeding budget.
- Bounded retries: retry attempts charge only on actual delivery attempt, not on
  claim/lease; failed retries enter DLQ/repair with owner and reason.
- Worker boundedness: keyset pagination, leases, `FOR UPDATE SKIP LOCKED`
  where needed, resumable cursors, and no offset-zero infinite rescans.
- Inventory safety: no negative balances; no feed stock reservation at
  generation; rejected proof releases or leaves reservation according to
  explicit policy.
- Promotion gates: isolated test Slack channel, sandbox/test sheet, dry-run
  parity report, no production sheet mutation, explicit Feed Director approval,
  and rollback plan before any live channel or production sheet integration.
- Security: do not copy legacy Slack tokens/secrets; any retained bridge uses
  approved secret storage, scoped service identity, auth/RBAC, and audit.

Promotion gates tied to this history:

1. Run against a sandbox sheet and isolated Slack test channel only.
2. Prove source snapshot locking with a source mutation during a run.
3. Prove duplicate control with rerun, manual retry, Slack retry, and ambiguous
   send fixtures.
4. Prove missing-message detection by intentionally suppressing one delivery.
5. Prove `ok:true` without timestamp and ambiguous `429` classification.
6. Prove watchdog state preservation for `sending`/pending reconciliation.
7. Prove no production sheet write and no production channel send in tests.
8. Require Feed Director/product sign-off before enabling any live bridge.

## 10. Migration plan

### Phase 0 — Read-only discovery

- Inventory all active Apps Script triggers, sheet ids, tab names, channel ids,
  and script properties.
- Export only structure and sanitized samples required for parity; do not commit
  raw rows, media links, names, tokens, or secrets.
- Confirm exact legacy notification times, especially ~07:15, ~07:30, and the
  user-supplied ~13:00 afternoon consumption timing.
- Freeze a source map with checksums for `Count-DB`, `FutureDB`, `Feed
  Direction`, `Feed Packing Form`, `Feed Consumption & Wastage`, Experiment
  tabs, and Video Verification imports.

### Phase 1 — Canonical config and counts

- Load reviewed aliases and count/shifting source evidence through the existing
  Counts/Shifting import path.
- Publish a draft `feed_direction` config pack with feed vectors, session
  policy, eligibility/exclusions, proof policy, and validation tolerances.
- Keep generation disabled until readiness gates are green or fail-closed with
  explicit blockers.

### Phase 2 — Shadow generation

- Run dry-run/full shadow generation for selected parks/dates without sending
  Slack or closing any work.
- Compare GoatOS directions to legacy Feed Direction rows by date, park, shed,
  session, feed item, quantity, and expected exclusions.
- Record unexplained deltas as owner-review items.

### Phase 3 — Sandbox notification and proof loop

- Use an isolated Slack test channel and sandbox/test sheet only.
- Generate notifications from GoatOS obligations.
- Ingest replies/files back into GoatOS proof records.
- Run duplicate/missing/retry/reconciliation tests.
- Prove watchdog catches intentionally skipped, duplicated, and failed sends.

### Phase 4 — Limited live overlap

- Choose one park or non-critical date window.
- GoatOS is canonical; legacy Sheets are either read-only archive or API bridge
  output.
- Apps Script, if retained, calls GoatOS APIs only.
- Compare live operator outcome, proof records, and Video Verification state.
- Stop if any duplicate/missing message or source mutation outside GoatOS is
  detected.

### Phase 5 — Cutover and archive

- Disable legacy write triggers for the cutover scope.
- Archive sheet state with checksums and retention owner.
- Keep read-only migration APIs for audit/parity.
- Remove API bridge after all parks/sessions are cut over and Feed Director
  signs off.

## 11. Testing and acceptance

Minimum acceptance:

- Unit tests for ration config validation, session split weights, KT candidate
  values as data, alias failures, and blocked generation.
- Integration tests for full generation, Diff restatement/supersession,
  high-priority bridge logging, inventory reserve/consume/release, rejected
  proof rework, and missed/deadline escalation.
- Replay tests for every mutating API and worker.
- Parity tests with sanitized workbook/header/source samples.
- Slack bridge tests in isolated channel: send success, quota failure, retry,
  duplicate Slack event, missing file, and reconciliation.
- Watchdog tests: stuck outbox, duplicate active instruction, stale projection,
  missing proof, and stock-out.
- Bounded query-plan tests for command buckets/read models at the current
  5k-to-50k release envelope.
- End-to-end test: count/shifting projection -> generation -> packing -> proof
  verification -> consumption/wastage -> read-model bucket closure.

Before landing implementation, run the relevant repo guardrails and full
`make ci-local` as required by the GoatOS main-landing rule.

## 12. Open questions and owner decisions

1. Should migration notifications keep ~07:15/~07:30/~13:00 timings, or should
   GoatOS move operators to the docx 09:00/15:00 model immediately after
   cutover?
2. Which sheds currently use an experiment absolute-kg allocation instead of the
   default per-head ration, and who is authorized to set the next day's absolute
   per-shed kg before the ~14:15 packing notification? Which allocations, if any,
   also need optional comparison labels?
3. Are K0/K1 exclusions approved as a published `feed_direction` eligibility
   rule?
4. Does `80/20` from KT represent a packing factor, feed-type ratio, or a
   legacy-only value? Which scopes, if any, should publish it?
5. Which Feed Director-approved thresholds replace KT heuristics such as 90-95%
   validation and 5% warm-up allowance?
6. Which stage model should implementation choose: separate stage obligations,
   `feed_direction_stage_records`, or a narrow first slice limited to packing?
7. What is the exact owner-approved transport consolidation map for CBE/CPT?
8. What is the rollback plan if Slack bridge delivery fails during live overlap:
   paper directions, old Slack, GoatOS mobile only, or pause feeding workflow?
9. Which historical sheet rows should close matching GoatOS work, and which
   should remain audit-only?
10. Which service account or bridge identity is allowed to publish
    `feed_direction` config and send Slack notifications during migration?
