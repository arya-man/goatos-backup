# Feed Direction Dependency Closure - PRD

**Status:** Draft v2, counter-reviewed source/kernel readiness phase before Feed Direction build
**Date:** 2026-06-30
**Companions:** [Feed Direction PRD](./PRD.md), [Feed Direction TRD](./TRD.md),
[Dependency Closure TRD](./DEPENDENCY-CLOSURE-TRD.md),
[Counts/Shifting Closure PRD](./COUNTS-SHIFTING-CLOSURE-PRD.md), and
[Counts/Shifting Closure TRD](./COUNTS-SHIFTING-CLOSURE-TRD.md)

## 1. Purpose

This PRD defines the work that must be closed before Feed Direction can be built
as an operational product surface.

The Feed Direction PRD/TRD already cover the main business model: physical Base
Count, append-only Shifting ledger, one-day projected count, Day N full
direction, cutoff Diff, bridge protocol, ration provenance, stage execution,
inventory binding, and million-goat scale shape. The remaining problem is not
whether Feed Direction is implementable. It is implementable. The problem is
that several source-backed inputs and kernel integrations are still missing,
unowned, or too large to hide behind one Feed gate.

This phase converts those open items into explicit gates, owner decisions, and
backend contracts so the next Feed Direction build can start without pretending
mock UI, legacy Slack state, or unscoped Counts work is product truth.

## 2. Scope Gate

Feed Direction operational UI remains gated by the current admin-web scope lock.
This dependency phase may prepare backend contracts, tables, workers, tests, and
source-backed config. It must not expose active Feed Direction UI, Feed Config
categories, SOP cards, or command-room routes until the owner explicitly reopens
Feed Direction after the current PHC/Vaccination scope is reviewed and approved.

Gate `G1` blocks operational launch, visible UI, and active Config/SOP exposure.
It does not block backend contract preparation, source analysis, migrations, or
testable local worker/read-model work that remains hidden behind the scope gate.

The result of this phase is "Feed Direction ready for backend build", not "Feed
Direction shipped". UI build remains behind `G1`.

## 3. Source Base

Use these source families before asking the business to answer from memory:

| Source family | Use in this phase |
| --- | --- |
| `Feed, Shiftings and Count.docx` v1.1 | Canonical count/shifting ledger, one-day projection, timing, Diff, bridge, ration model |
| Goats & Parks / stage-tag source findings | Warmup tags, stage aliases, breed aliases, and ration-scope gaps not covered by the feed docx |
| Feed Director handbook | Written directions before shift, proof discipline, EOD/accountability, stock and procurement boundaries |
| Counting DB reconstruction | Source evidence and fixture shape for aggregate counts, not runtime truth |
| Legacy Slack/App Script feed automation | Feature inventory for packing, consumption, transport, verification, retries, dedupe, and notifications |
| GoatOS protocol/kernel docs | Obligations, batches, SOP proof, inventory ledger, outbox, scheduler, notifications, read models, scale gates |

Slack, Sheets, and Apps Script remain legacy evidence and cutover surfaces only.
They must not be runtime truth in GoatOS. Legacy scripts are a feature inventory,
not an implementation blueprint: exact code line references can drift, and
legacy weak validation must be replaced with typed GoatOS policy, not preserved.

## 4. Canonical Gate Table

Feed Direction is feasible and fits the GoatOS kernel, but the build cannot
honestly start as "full Feed Direction" until these gates are closed.

Use this table as the single readiness source. The TRD blockers and PRD open
questions must reference these IDs rather than maintaining independent lists.

| ID | Gate | Required outcome | Type |
| --- | --- | --- | --- |
| G1 | Scope launch gate | Owner explicitly reopens Feed Direction beyond the PHC/Vaccination review slice before any active Feed UI, Config category, SOP card, or command-room route is exposed | Owner |
| G2 | Counts/Shifting long pole | Counts/Shifting closure PRD/TRD is accepted and implemented enough to expose Base Count anchors, realized ShiftingEvent ledger, one-day projection, idempotency, fail-closed exceptions, and `CSG1`-`CSG10` readiness breakdown under `G2` | Backend/source |
| G3 | Clock and legacy trigger inventory | Feed Director signs off park-level publish, cutoff, staging, serving, retry, archive, and parity/cutover treatment for canonical `09:00`/`13:30`/`15:00` clocks plus legacy `07:30`, `14:45`, `06:30`, `07:15`, `14:15`, `00:15`, `23:45`, and `07:00` trigger windows | Owner |
| G4 | Ration source/provenance | Solver/import path, ration values, aliases, feed vectors, constraints, approval metadata, and publish authority checks are defined | Source/owner |
| G5 | Eligibility and stage-tag policy | Warmup 14-day transition tags, ICU, Quarantine, Flushing, Breeding, K0/K1, Experiment sheds, F2/Fattening, SIROHI->Beetal or other breed aliases, and per-farm session/feed-set retention are explicitly approved or excluded | Source/owner |
| G6 | Quantity and precision boundary | Feed units are whole grams/ml into the current inventory app port, or inventory app ports are widened before decimal/sub-gram feed use; baking-soda precision is resolved before build | Architecture |
| G7 | Stage model | Packing, transport, consumption/wastage, bridge proof/rework model chosen with a durable queryable `stage_kind` discriminator | Architecture |
| G8 | Transport map and checklist entity | Direction-shed to transport-shed consolidation owner/storage is confirmed, and any transport list/checklist entity from legacy overlap has a GoatOS equivalent | Source/owner |
| G9 | Exception thresholds and rework policy | Packing discrepancy and wastage variance thresholds are confirmed; legacy alert plus reset/re-send evidence is inventoried, and GoatOS typed rework/re-issue is explicitly formalized | Owner |
| G10 | Slack security | Affected legacy scripts inventoried, credentials revoked/rotated, any bridge credential moved to secret storage, and GoatOS API-only ingress proven before overlap | Security |
| G11 | Reminder/escalation SLA | Per-stage deadlines, reminder cadence, escalation owner, retry policy, and admin-alert fallback are defined | Kernel |
| G12 | NotificationGateway routing | Feed alert events and channel mappings are defined behind replaceable notification ports; Slack is only one adapter/cutover channel | Kernel/security |
| G13 | Missed/recovery events | Feed explicitly closes or acknowledges the kernel missed/overdue gap: deadline crossing creates durable missed/recovery events and visible process exceptions | Kernel |
| G14 | Audit and observability | Business audit rows, worker metrics, queue lag, retry counts, DLQ/error counters, and alert thresholds are specified | Kernel/ops |
| G15 | Command-lens field mapping | Calendar, Action Center, Protocol Adherence, Workflows, and Control Tower can subscribe to stage, due, owner, evidence, resolution, and escalation fields from one source of truth; the vaccination-locked calendar projection blocker is widened, replaced, or acknowledged before Feed uses Calendar/Protocol Adherence | Backend/product |
| G16 | Query-plan coverage | Feed hot reads, generation, projections, and count snapshots have bounded filters and plan checks | Build-time |
| G17 | Cursor read models | Backend read models expose bounded filters, stable cursor pagination, and no unbounded list scans for command/UI/API use | Build-time |

`G2` is the long pole. It is not a small Feed subtask; it is a sibling
Counts/Shifting module closure with its own source inputs, persistence,
idempotency, anti-fraud reconciliation, projection API, and tests.

## 5. Product Requirements

### 5.1 Counts/Shifting Contract

Feed Direction must consume a Counts/Shifting-owned contract, not raw Counting
DB rows.

The contract must expose:

- Physical Base Count anchors by tenant, park, shed, breed, and stage/tag.
- Append-only ShiftingEvents with source, destination, priority, category,
  raised/effective time, authorization, completion/proof/verification state, and
  structured cohort/stage impact.
- `count_as_of(time)` for realized count.
- `projected_count_for(target_date)` for the source-required one-day horizon.
- Idempotent application by `shifting_event_id` or equivalent logical key.
- Fail-closed handling when cohort/stage impact is missing or unresolved.
- Count-mismatch/unreported-shifting detection as process exception work.
- Breed/stage alias normalization before aggregate counts feed ration lookup.

A new physical Base Count becomes the new anchor immediately. Discrepancy
investigation creates accountability/process work, but it does not block the
physical count from becoming the next ledger anchor.

Close `G2` through the sibling Counts/Shifting closure docs, not by burying this
module inside Feed implementation.

`G2` readiness rolls up from `CSG1`-`CSG10`. The Feed readiness API must expose
each Counts/Shifting subgate with status, owner, evidence pointer, and blocker
reason so a green `G2` is traceable instead of a single opaque checkbox.

### 5.2 Ration Source And Provenance

Feed Direction must not publish quantities from hand-entered guesses or UI-only
values.

The first build must choose one source-backed path:

1. A non-UI RationTable solver that enforces the source-required feed-type
   constraints.
2. A reviewed import of solver outputs into `feed_direction` protocol rules.

Both paths must persist provenance: source version, solver/import version, feed
vectors, costs, available feed types, constraints, output hash, reviewer,
reviewed_at, review_status, approved_by, approved_at, effective date, and
re-solve/review trigger.

Reviewed import is not the same thing as publishable truth. A ration protocol
version can publish only when `review_status='approved'`, `approved_by` and
`approved_at` are present, the source hashes match the reviewed solve/import,
and the actor/job performing publication has `protocol.publish.feed_direction`
or its approved equivalent.

Adult ration keys are `breed + shed_tag/stage`. Kid ration keys are
`weight_band + target_adg`. Raw `Age` is source evidence only; it is not the
runtime ration key.

Warmup tags require explicit Feed Director sign-off before build. Source findings
list a Warmup 14-day transition and tags such as Fattening M/F Warmup, Warmup
Non-Pregnant, Warmup Buck, Warmup Pregnant, and Milking Warmup, but the feed docx
ration table does not settle their quantities. If Warmup animals receive packed
feed, they must have a reviewed ration path.

ICU, Quarantine, Flushing, and Breeding shed tags also need explicit handling.
They may become reviewed ration paths, explicit exclusions, or process-exception
states, but they must not inherit packed-feed behavior through catch-all alias
cleanup.

K0/K1 and Experiment exclusions are not settled by the feed docx alone. Treat
legacy zero rows/automation filters as candidate policy evidence and require
Feed Director approval before suppressing normal packed-feed obligations.

### 5.3 Default Engineering Decisions To Unblock Build

These are the recommended defaults unless the owner explicitly reverses them:

| Decision | Recommended default | Reason |
| --- | --- | --- |
| Stage execution | Separate `obligation_instances` grouped by `obligation_batches` for packing, transport, and consumption/wastage, plus a durable `stage_kind` discriminator | Fits one `feed_direction_completions` row per obligation without inventing a parent-child obligation FK |
| Feed quantity unit | Deterministic integer base units: grams for solids, milliliters for liquids | Matches the current inventory app whole-unit port while preserving the SQL `numeric + quantity_unit` ledger |
| Diff runtime model | Affected-shed restatement rows with explicit stale-obligation cancel/supersede | Prevents old and new instructions staying live together |
| Source-facing Diff | Net correction may be displayed/exported | Matches source language while preserving canonical restatement truth |
| Transport map | Source-backed effective-dated config, owned by Locations + Feed operations | Avoids string inference inside feed generation |
| Threshold config | Store in Feed proof policy/rule DSL; legacy values are candidates only | Keeps tolerances reviewable and park/feed-item scoped |

Quantity boundary for the first build is explicit:

- Feed generation stores `quantity_base_units` as whole grams or milliliters.
- API/UI display quantities are derived from those base units; display
  conversion never changes the stored instruction.
- Inventory stock and movement rows remain `numeric` with `quantity_unit`, but
  the current inventory app port accepts whole `int64` quantities. Feed may call
  that port only with deterministic whole base units.
- Do not rely on the current inventory repository's dose-style numeric floor for
  feed quantities. If Feed requires fractional base units, widen the inventory
  app port and add decimal reserve/consume/release tests before using them.
- Baking-soda precision must be resolved inside `G6`; if source-approved
  baking-soda quantities need sub-gram precision, integer gram base units are not
  sufficient for that item.

### 5.4 Stage And Proof Requirements

The first operational Feed Direction slice must preserve the useful legacy stage
signals:

- Packing due, packed, discrepancy, shortfall, rejected proof, and rework.
- Transport pending, uploaded proof, verified, rejected, and rework.
- Consumption recorded, wastage recorded, variance exception, and proof state.
- Bridge exception for high-priority post-cutoff additions.

Legacy processed flags become idempotent GoatOS obligations, SOP submissions,
proof, verification, and read-model state. They must not be copied as Boolean
runtime authority.

Every stage-bearing obligation, completion, or read-model projection must carry
a stable `stage_kind` from a controlled set such as `packing`, `transport`,
`consumption_wastage`, and `bridge_exception`. The discriminator may live in a
typed Feed stage table, obligation context with an indexed projection, or a
first-class completion/stage record. It must exist before execution-bucket APIs
are built; stage buckets must not infer stage from free-text status, SOP labels,
or legacy processed flags.

Short-packed or mismatched quantity behavior must be labeled honestly. Legacy
automation included both discrepancy/admin alert paths and a separate packing
quantity check that reset packing state for re-send. GoatOS must not copy that
Sheet/runtime mechanism; it formalizes the useful parity as typed rework,
re-issue, audit, and idempotent obligation state.

### 5.5 Inventory Binding

Stock must be bound through the inventory kernel:

- No stock reservation during generation.
- Reserve at packing start.
- Consume actual accepted quantity after verification.
- Release the remainder from the reserved batch.
- Stock-out creates blocked/escalation work, not a direct negative balance.
- All reserve/consume/release calls use whole base units for the first build and
  persist inventory movements with the matching `quantity_unit`. Decimal feed
  quantities require an explicit inventory app-port widening before build.

### 5.6 Command And UI Readiness

Before UI implementation starts, backend contracts must expose operational
buckets with cursor pagination and bounded filters:

- `generation_blocked`
- `packing_due`
- `packing_shortfall`
- `proof_missing`
- `transport_pending`
- `transport_rejected`
- `consumption_incomplete`
- `wastage_exception`
- `bridge_exception`
- `stock_out`
- `missed_or_overdue`
- `escalated`
- `rework`

These buckets feed top-level command lenses through domain/category filters.
They do not create nested Feed-owned Action Center, Control Tower, Protocol
Adherence, Config, or SOP Library screens.

Bucket membership must be derived from durable `stage_kind` plus obligation,
proof, verification, inventory, deadline, escalation, and threshold state. API
buckets must not depend on brittle label parsing.

### 5.7 Kernel Closure

Feed Direction is not ready for backend build until it answers the operational
kernel questions:

- What reminder and escalation schedule applies to every stage?
- Which notification events are emitted, and which adapters may deliver them?
- What durable state is written when a deadline crosses?
- What recovery event closes missed/overdue or rejected proof?
- Which business audit rows and technical metrics are required?
- Which fields drive Calendar, Action Center, Protocol Adherence, Workflows, and
  Control Tower from the same source of truth?

Current Calendar projection storage is a shared blocker for `G15`: the committed
`calendar_event_projections` and identity constraints are vaccination-slice
locked. Feed can feed top-level Action Center-style buckets from its own
projection, but Calendar/Protocol Adherence integration needs a widened
multi-slice projection, a generic projection, or a Feed-specific projector with
compatible event vocabulary before it can be called closed.

## 6. Out Of Scope For This Closure Phase

- Active Feed Direction admin-web UI.
- Operator-mobile Feed Direction execution screens.
- Full NRC optimizer UI.
- Feed procurement, stock-floor/reorder, supplier network, fodder production,
  and monthly Feed Director reporting dashboards.
- Production deployment without the shared GoatOS production infra gates.
- Any Slack/Sheets mutation as canonical execution truth.

Feed Director handbook items such as one-month stock floor, stockist/factory
network, procurement transport, EOD stock accountability, and anti-misuse belong
to Feed Stock, Procurement, Inventory, and Director Ops scopes unless explicitly
pulled into this slice.

## 7. Acceptance Criteria

This phase is complete when:

1. Gates `G1`-`G17` have a status, owner, evidence pointer, and no unowned
   blocker.
2. Counts/Shifting closure docs are accepted, and `G2` is no longer hiding a
   whole module behind one Feed bullet. `GET /feed-direction/readiness` exposes
   `CSG1`-`CSG10` statuses beneath `G2`.
3. Ration values are source-backed through solver or reviewed import, and
   publishability requires `review_status='approved'`, approval metadata, and
   publish authority checks.
4. Warmup, K0/K1, Experiment, breed aliases, and per-farm session/feed-set
   decisions are explicitly approved or excluded.
5. Stage execution model is chosen, reflected in the TRD, and includes a durable
   queryable `stage_kind` discriminator before bucket APIs.
6. Slack security closeout inventories affected legacy scripts, revokes or
   rotates tokens/shared secrets, stores any bridge credential in a secret
   manager, and proves API-only ingress with auth/RBAC/idempotency before any
   bridge reuse.
7. Feed-specific generation/read queries have plan-test coverage requirements.
8. Feed Direction PRD/TRD and this dependency PRD/TRD agree on what is blocked,
   what is buildable under the scope gate, and what must remain hidden.

## 8. Readiness Output

After this phase, the next build may start hidden Feed Direction backend
implementation in this order:

1. Counts/Shifting projection closure.
2. Ration provenance and eligibility policy.
3. Generation and Diff.
4. Stage obligations, durable stage discriminator, and inventory unit-boundary
   wiring.
5. Kernel reminders, notifications, missed/recovery events, audit, observability,
   and command-lens field mapping.
6. Read models/API/OpenAPI.
7. UI mock update and frontend implementation only after `G1` reopens scope.
