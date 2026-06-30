# Feed Direction Dependency Closure - PRD

**Status:** Draft v2, counter-reviewed source/kernel readiness phase before Feed Direction build
**Date:** 2026-06-30
**Companions:** [Feed Direction Feature Closure Plan](./FEATURE-CLOSURE-PLAN.md),
[Feed Direction PRD](./PRD.md), [Feed Direction TRD](./TRD.md),
[Dependency Closure TRD](./DEPENDENCY-CLOSURE-TRD.md),
[Counts/Shifting Closure PRD](./COUNTS-SHIFTING-CLOSURE-PRD.md),
[Counts/Shifting Closure TRD](./COUNTS-SHIFTING-CLOSURE-TRD.md), and
[Feed Direction Build-To-Done Goal](./BUILD-TO-DONE-GOAL.md)

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

Use [FEATURE-CLOSURE-PLAN.md](./FEATURE-CLOSURE-PLAN.md) to execute the gates in
feature language. The gate IDs below are traceability labels; the implementation
run must close safe feed input projection first, then ration/template/session
config, generation, stage/proof work, feed-safety rework, APIs/frontend, and
real local E2E. Do not add more source-proof work once the milestone checker can
either calculate a safe row or block it with an explicit reason.

## 2. Scope Gate

`G1` is reopened for Feed Direction build as of 2026-06-30. The older pause tied
to PHC/Vaccination UI and foundation review is satisfied by the local Goal 1
vaccination/kernel closure documented in
`context/execution/operational-kernel-stability-closure-handoff.md`. Do not keep
using the older pause wording as a live blocker.

Google dev rollout, clean-slate seeding, and Google E2E are a separate Goal 2
gate. They must not be claimed as done from local evidence, but they do not block
starting Feed Direction.

The reopened `G1` does not remove Feed Direction's own gates. Active Feed UI,
Feed Config categories, SOP cards, and command-lens surfaces must still be
backed by source-backed or clearly marked local-dev data, backend-owned
contracts, mock-fidelity, rendered visual proof, the product milestones in
`FEATURE-CLOSURE-PLAN.md`, and the `G2`-`G17` evidence map.

## 3. Source Base

Use these source families before asking the business to answer from memory:

| Source family | Use in this phase |
| --- | --- |
| `Feed, Shiftings and Count.docx` v1.1 | Primary source for count/shifting ledger, one-day projection, timing, Diff, manual bridge SOP/logging, as-fed quantities, and ration model |
| Other feed-relevant wiki/source findings | `context/source-findings/goats-and-parks-source-findings.md` for base goat/park/stage/shed-tag semantics; `context/source-findings/sheds-db-source-findings.md` for manual shed profile tags/capacity/potential tags; Counting DB reconstruction, Feed Transfer KT findings, Shifting reports, Feed Director ops, transport consolidation, Warmup/K0/K1/Experiment evidence, and feed-stock/procurement boundaries |
| `Counting DB - values only.xlsx` and `Feed Directions Automation DB.xlsx` | Workbook/tab/formula/processed-flag evidence for import design, parity fixtures, known data-quality gaps, ration/source tables, session templates, and execution/proof stage inventory |
| Legacy Slack/App Script feed automation | Feature inventory for packing, consumption, transport, verification, reset/re-send, retries, dedupe, notifications, and security/cutover evidence |
| GoatOS protocol/kernel docs and committed code | Obligations, batches, SOP proof, inventory ledger, outbox, scheduler, notifications, read models, scale gates, RBAC, OpenAPI, and generated clients |
| Admin-web mock | UI anatomy source only after `G1`; it does not override the primary Feed business source |

Slack, Sheets, and Apps Script remain legacy evidence and cutover surfaces only.
They must not be runtime truth in GoatOS. Legacy scripts are a feature inventory,
not an implementation blueprint: exact code line references can drift, and
legacy weak validation must be replaced with typed GoatOS policy, not preserved.
Workbook tabs and formulas are also not target schema. `Count-DB`,
`CPT Validation`, `CBE Validation`, `Feed-Energy-Protein`, `Supply Planning`,
`Template`, `Feed Packing Form`, `Feed Transport Form`, and
`Feed Consumption & Wastage` are import/parity labels only.
For the Feed Direction slice, `Feed, Shiftings and Count.docx` v1.1 is the
controlling business source. If older mocks, prior GoatOS notes, or legacy
Slack/App Script trigger times disagree with it, follow the docx unless the Feed
Director explicitly reopens the rule. Legacy trigger times are audit/cutover
evidence only, not GoatOS schedules by default.

The next implementation session must use
[FEATURE-CLOSURE-PLAN.md](./FEATURE-CLOSURE-PLAN.md) as the product milestone
map and [BUILD-TO-DONE-GOAL.md](./BUILD-TO-DONE-GOAL.md) as the stop-rule
charter for source cross-check, backend/frontend integration, seeds, E2E,
high-effort review agents, and Mesha/VGoats push verification.

Do not treat Feed dependency closure as Feed-only vocabulary. Gate decisions for
Counts/Shifting, ration context, eligibility, proof, and UI must respect
`context/source-findings/goats-and-parks-source-findings.md` for identity,
park/shed scope, shed tags, lifecycle/stage, pregnancy, lactation, warm-up,
fattening, milking, handling, weighing, and feed-safety semantics.

## 4. Canonical Gate Table

Feed Direction is feasible and fits the GoatOS kernel, but the build cannot
honestly start as "full Feed Direction" until these gates are closed.

Use this table as the single readiness source. The TRD blockers and PRD open
questions must reference these IDs rather than maintaining independent lists.

| ID | Gate | Required outcome | Type |
| --- | --- | --- | --- |
| G1 | Scope launch gate | Reopened as of 2026-06-30 because local PHC/Vaccination UI/foundation closure is accepted for Feed Direction sequencing. Google dev vaccination rollout remains separate Goal 2 and is not required before Feed starts. Active Feed UI/Config/SOP exposure still requires Feed-owned backend contracts, mock fidelity, rendered proof, product-milestone closure from `FEATURE-CLOSURE-PLAN.md`, and `G2`-`G17` evidence closure. | Owner |
| G2 | Counts/Shifting long pole | Counts/Shifting closure PRD/TRD is accepted and implemented enough to expose aggregate Base Count anchors, realized ShiftingEvent ledger, one-day projection at shed + breed grain, reviewed ration-context resolution state, idempotency, stale/imported mismatch scanning through the bounded `counts-mismatch-scan` worker path with durable scan-run evidence, recompute worker evidence through `count_projection_recompute_runs`, fail-closed exceptions with reviewed resolve/dismiss audit, and `CSG1`-`CSG10` readiness breakdown under `G2`. Breed/tag constraint tables without shed placement must produce a blocker until reviewed context resolves the physical count row to a nutrition cohort. RFID-to-shed per-goat derivation is out of initial Feed scope. | Backend/source |
| G3 | Clock and legacy trigger inventory | Feed Director signs off the default Feed Direction clocks from `Feed, Shiftings and Count.docx`: Day N `09:00` full direction, Day N `13:30` cutoff, Day N `13:30-13:45` Diff, Day N `15:00` staging, and Day N+1 `09:00`/`15:00` serving slots. `G5` owns approved session-slot changes beyond that default. Legacy Slack/App Script trigger installers from `unified_automation.js`, `counting_db_automation.js`, `feed_automation.js`, and `video_verification_system.js` are a separate audit-only cutover subgate for retain/retire/replace decisions; their timings must not be treated as GoatOS schedules unless explicitly retained or replaced against the docx. | Owner |
| G4 | Ration source/provenance | Solver/import path, ration values, aliases, feed vectors, uploaded breed/tag/energy constraint tables, quantity/weight thresholds, constraint hashes, approval metadata, publish authority checks, and typed CRUD/import/review/publish flow are defined. KT examples such as `80/20`, `400-500g`, `600g`, `F1` `11-15kg`, and `F2` `15-20kg` are reviewed as candidate row values, not hardcoded constants. `G4` must define the runtime template pipeline: source tables -> typed parameter rows -> dimension/alias/source-hash validation -> calculation preview -> row repair/DLQ -> approved `feed_direction_config_pack`. Workbook formulas and tabs are evidence only. | Source/owner |
| G5 | Eligibility and stage-tag/session policy | Warmup 14-day transition tags, ICU, Quarantine, Flushing, Breeding, K0/K1, Experiment sheds, F2/Fattening, SIROHI->Beetal or other breed aliases, pregnant-animal policy, lactation/warm-up safety policy, and versioned session-slot/feed-set policy are explicitly approved. The source default is two serving slots with 50/50 split, but admins may add, disable, reorder, or reweight slots only through approved effective-dated Feed Direction protocol config with validation and supersession rules. KT pregnant windows such as `12:30-15:00`/`14:00-15:00` are policy candidates only; they do not override docx clocks unless approved. Shifted pregnant/lactating/warm-up cohorts must re-resolve destination shed ration context and block on shortage or missing policy before generation/Diff. | Source/owner |
| G6 | Quantity and precision boundary | Feed units are whole grams/ml into the current inventory app port, or inventory app ports are widened before decimal/sub-gram feed use; baking-soda precision is resolved before build | Architecture |
| G7 | Stage model | Packing, transport, consumption/wastage, bridge proof/rework model chosen with a durable queryable `stage_kind` discriminator | Architecture |
| G8 | Transport map and checklist entity | Direction-shed to transport-shed consolidation owner/storage is confirmed, and any transport list/checklist entity from legacy overlap has a GoatOS equivalent | Source/owner |
| G9 | Exception thresholds and rework policy | Packing discrepancy, wastage variance, and KT-style `90-95%` shed/pack/breed/tag/energy match thresholds plus warm-up allowance are either approved, rejected, or marked draft-only; legacy alert plus reset/re-send evidence is inventoried; destination-shed shortage, overpack, moist/unsafe leftover feed, refusal-to-eat, and sickness-risk reasons are typed; and GoatOS typed rework/re-issue is explicitly formalized | Owner |
| G10 | Slack security | Affected legacy scripts inventoried, credentials revoked/rotated, any bridge credential moved to secret storage, and GoatOS API-only ingress proven before overlap. If `G10` is deferred, Slack bridge/overlap stays disabled and cannot count as done. | Security |
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

- Physical Base Count anchors by tenant, park, shed, and breed.
- Ration-context resolution state for joining aggregate counts to ration lookup
  or blocking when reviewed context is missing.
- Append-only ShiftingEvents with source, destination, priority, category,
  raised/effective time, authorization, completion/proof/verification state, and
  structured cohort/stage impact.
- `count_as_of(time)` for realized count.
- `projected_count_for(target_date)` for the source-required one-day horizon.
- Idempotent application by `shifting_event_id` or equivalent logical key.
- Fail-closed handling when cohort/stage impact is missing or unresolved.
- Count-mismatch/unreported-shifting detection as process exception work.
- Breed/stage alias normalization before aggregate counts feed ration lookup.
- Reviewed ration-context resolution before aggregate shed + breed counts feed
  ration lookup. Breed/tag/energy constraint tables are not shed-placement truth.

This is aggregate-first. RFID-to-shed per-animal association is a future
replacement path only after separately proven identity/location confidence; it is
not hidden work inside initial Feed Direction.

A new physical Base Count becomes the new anchor immediately. Discrepancy
investigation creates accountability/process work, but it does not block the
physical count from becoming the next ledger anchor.
Base Count cadence has source history moving from roughly weekly to roughly
monthly. Treat it as reviewed policy or schedule config, not a hardcoded
implementation interval.

Close `G2` through the sibling Counts/Shifting closure docs, not by burying this
module inside Feed implementation.

`G2` readiness rolls up from `CSG1`-`CSG10`. The Feed readiness API must expose
each Counts/Shifting subgate with status, owner, evidence pointer, and blocker
reason so a green `G2` is traceable instead of a single opaque checkbox.

### 5.2 Clock And Legacy Trigger Inventory

`G3` closes in two separate parts.

First, confirm the default Feed Direction clocks from
`Feed, Shiftings and Count.docx`:

| Clock | Product meaning |
| --- | --- |
| Day N `09:00` | Full Feed Direction for Day N+1 |
| Day N `13:30` | Shifting cutoff for Day N+1 Diff inclusion |
| Day N `13:30-13:45` | Diff window for changes raised between `09:00` and cutoff |
| Day N `15:00` | Packed and diff-corrected feed staged outside sheds |
| Day N+1 `09:00` | Session 1 served from staged stock |
| Day N+1 `15:00` | Session 2 served from staged stock |

Second, audit legacy Slack/App Script trigger installers only as cutover
evidence. The audit must cover current/older feed packing, transport, count
projection/update, watchdog/recovery, feed consumption list, archive/retry,
proof/quantity, stock update, wastage summary, and stock alert paths in
`unified_automation.js`, `counting_db_automation.js`, `feed_automation.js`, and
`video_verification_system.js`.

Those legacy installer times are not product clocks. Each legacy trigger family
must receive an explicit retain, retire, or replace decision with owner, reason,
and GoatOS target mechanism if anything is retained or replaced. The default is
to retire Apps Script timing and replace needed side effects with typed GoatOS
kernel schedules, reminders, rework, or proof/stock policies. Trigger comments
and logger text in the legacy scripts are inconsistent in places, so the audit
records the actual `ScriptApp.newTrigger(...).timeBased()` installer shape as
evidence, then separately checks that any retained behavior is approved against
`Feed, Shiftings and Count.docx`.

### 5.3 Ration Source And Provenance

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

Initial ration scope is feed-type-level only: hard floor/ceiling, structural
ratio, category floor, and quantity floor. Item-level feed ceilings and
palatability modeling are deferred. The `60:40` structural ratio applies only to
Milking/Fattening tags, roughage/category floor values such as 30 percent must be
confirmed per tag before hardcoding, cost minimization means no paired overshoot
ceiling is needed, and a reviewed re-solve replaces the RationTable output
wholesale with no versioned blend.

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

The legacy workbooks show why `G4` cannot close with a spreadsheet clone. The
closure must specify the admin/data-ops surfaces or import APIs that create,
edit, validate, review, approve, publish, retire, and replay Feed Direction
configuration. At minimum this includes feed item nutrient vectors, feed costs,
feed-type constraints, breed/species aliases, stage/tag aliases, kid
weight-band/ADG rules, quantity/weight thresholds, warm-up/pregnancy policy,
eligibility/exclusions, session slots, feed-set templates, transport maps, proof
thresholds, validation tolerances, and reviewed ration solver/import outputs.
Each import needs row-level validation, source hash, dry-run parity preview where
a workbook source exists, dead-letter/repair workflow, reviewer/approval
metadata, and audit.

The Feed Transfer KT examples strengthen this gate but do not close it by
themselves. `80/20`, `400-500g`, `600g`, `F1` `11-15kg`, `F2` `15-20kg`,
pregnant scheduling windows, and `90-95%` matching/warm-up allowance must be
approved, rejected, or left draft-only in the protocol config before generation
can rely on them.

### 5.4 Default Engineering Decisions To Unblock Build

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
- Feed Direction, Diff, packing, and field outputs carry as-fed gross quantities
  only. `wastage_factor` and `DM_factor` are internal nutrient-accounting inputs
  and must not surface to packing/field teams as instruction quantities.
- Inventory stock and movement rows remain `numeric` with `quantity_unit`, but
  the current inventory app port accepts whole `int64` quantities. Feed may call
  that port only with deterministic whole base units.
- Do not rely on the current inventory repository's dose-style numeric floor for
  feed quantities. If Feed requires fractional base units, widen the inventory
  app port and add decimal reserve/consume/release tests before using them.
- Baking-soda precision must be resolved inside `G6`; if source-approved
  baking-soda quantities need sub-gram precision, integer gram base units are not
  sufficient for that item.

### 5.5 Stage And Proof Requirements

The first operational Feed Direction slice must preserve the useful legacy stage
signals:

- Packing due, packed, discrepancy, shortfall, rejected proof, and rework.
- Transport pending, uploaded proof, verified, rejected, and rework.
- Consumption recorded, wastage recorded, variance exception, and proof state.
- Manual bridge exception for high-priority post-cutoff additions: log the
  video-confirmed 2x destination-shed top-up with `shed_id`, `animal_id` or
  approved aggregate reference, `timestamp`, `quantity`, proof reference,
  source event/logical shifting reference where known, and reconciliation state.
  Do not build the superseded `07:30` next-morning Diff or source-shed claw-back.

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

### 5.6 Inventory Binding

Stock must be bound through the inventory kernel:

- No stock reservation during generation.
- Reserve at packing start.
- Consume actual accepted quantity after verification.
- Release the remainder from the reserved batch.
- Stock-out creates blocked/escalation work, not a direct negative balance.
- All reserve/consume/release calls use whole base units for the first build and
  persist inventory movements with the matching `quantity_unit`. Decimal feed
  quantities require an explicit inventory app-port widening before build.

### 5.7 Command And UI Readiness

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

Any Feed Direction UI that is built after `G1` reopens must follow the admin-web
mock-fidelity law: update or explicitly supersede stale Feed mock labels first,
port the mock anatomy for layout, typography, colors/tokens, button/icon sizing,
pagination, filters, drawers, tables, hover/active/focus/disabled states, empty
states, and proof/status surfaces, and run `npm --prefix apps/admin-web run
check:mock-fidelity` with rendered visual proof before handoff or push.

### 5.8 Kernel Closure

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
3. Ration values and KT-derived candidate parameters are source-backed through
   solver or reviewed import, and publishability requires
   `review_status='approved'`, approval metadata, and publish authority checks.
4. Warmup, pregnancy, K0/K1, Experiment, breed aliases, and versioned
   session-slot/feed-set policy are explicitly approved. The June 2026 `50/50`
   two-session split is a deliberate source simplification, not derived
   nutrition logic; GoatOS must support admin-approved effective-dated slot
   changes without silently mutating already-generated FeedDirection/Diff rows.
   Shifted pregnant/lactating/warm-up cohorts must have destination-shed ration
   recompute, shortage blocking, and wastage/moist-feed exception behavior before
   `G5`/`G9` can be green.
5. The wiki do-not-resolve items are represented in implementation gates:
   aggregate-first counts, manual bridge logging only, as-fed gross field output,
   ration solver limits, no hardcoded Base Count cadence, and no generated
   `07:30` bridge Diff.
6. Stage execution model is chosen, reflected in the TRD, and includes a durable
   queryable `stage_kind` discriminator before bucket APIs.
7. Slack security closeout inventories affected legacy scripts, revokes or
   rotates tokens/shared secrets, stores any bridge credential in a secret
   manager, and proves API-only ingress with auth/RBAC/idempotency before any
   bridge reuse. If `G10` is owner-deferred, Slack bridge/overlap is explicitly
   disabled and cannot be used as Feed Direction completion evidence.
8. Feed-specific generation/read queries have plan-test coverage requirements.
9. Feed Direction PRD/TRD and this dependency PRD/TRD agree on what is blocked,
   what is buildable under the scope gate, and what must remain hidden.
10. The build-to-done charter is acknowledged for the next session, including
   source priority, seeded E2E proof, frontend mock-fidelity proof, high-effort
   review-agent gate, and `git mesha-push main` verification.

## 8. Readiness Output

After this phase, the next build starts under reopened `G1` by following the
product milestones in `FEATURE-CLOSURE-PLAN.md`, with this gate order as the
evidence/readiness map:

1. `G2` Counts/Shifting projection closure.
2. `G3` clock and legacy trigger inventory sign-off.
3. `G4` Ration provenance.
4. `G5` Eligibility and stage-tag policy.
5. `G6` Quantity and precision boundary.
6. `G7` Generation, Diff, stage obligations, and durable stage discriminator.
7. `G8` Transport map and checklist/list equivalent.
8. `G9` Exception thresholds and rework policy.
9. `G10` Slack security closeout or Slack bridge disabled.
10. `G11` Reminder/escalation SLA.
11. `G12` NotificationGateway routing.
12. `G13` Missed/recovery events.
13. `G14` Audit and observability.
14. `G15` Command-lens field mapping.
15. `G16` Query-plan coverage.
16. `G17` Read models/API/OpenAPI with cursor semantics.
17. UI mock update, frontend implementation, visual proof, and mock-fidelity
   checks under reopened `G1`, after Feed-owned backend contracts and source data
   gates make the surface truthful.
18. High-effort cross-source/code/UI/security/E2E review, fix confirmed findings,
   and push through the Mesha/VGoats PAT path only after final verification.
