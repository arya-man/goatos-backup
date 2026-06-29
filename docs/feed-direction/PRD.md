# Feed -> Feed Direction - Product Requirements (PRD)

**Status:** Draft v5, refined against source-first counter-review, read-model review, and dependency-closure audit
**Date:** 2026-06-30
**Vertical:** Feed. Feed Direction is the first Feed module. Parks are a scope
dimension, not the owning vertical.

**Scope gate:** Full Feed Direction operational screens are not in the current
admin-web review scope. Do not build or expose Feed Direction UI, SOP cards, or
Config categories as active product until the owner explicitly reopens scope
after PHC/Vaccination and the generic Config foundation are reviewed and
approved. Backend/generic-engine work can be prepared underneath that gate.

**Dependency closure:** Before Feed Direction implementation starts, close the
readiness phase in [DEPENDENCY-CLOSURE-PRD.md](./DEPENDENCY-CLOSURE-PRD.md) and
[DEPENDENCY-CLOSURE-TRD.md](./DEPENDENCY-CLOSURE-TRD.md). Those docs turn the
latest source audit into canonical gates `G1`-`G17` for Counts/Shifting, ration
provenance, eligibility, quantity precision, stage execution, transport
consolidation, thresholds, Slack security, kernel reminders/notifications,
missed/recovery state, audit/observability, and million-goat query/read-model
proof. Counts/Shifting gate `G2` has sibling docs:
[COUNTS-SHIFTING-CLOSURE-PRD.md](./COUNTS-SHIFTING-CLOSURE-PRD.md) and
[COUNTS-SHIFTING-CLOSURE-TRD.md](./COUNTS-SHIFTING-CLOSURE-TRD.md).

**Design decision:** Ratify the committed `000079_feed_direction_module.sql`
direction unless the owner explicitly reverses it. Feed Direction reuses the
generic protocol, obligation, SOP, inventory, audit, outbox, and projection
kernel. Ration config lives in `protocol_versions.rule_dsl` under
`category='feed_direction'`. Shed/session work is represented by
`obligation_instances` and `obligation_batches`. The current feed-specific
execution table is `feed_direction_completions`. Do not resurrect the older
typed `feed_*` table stack as the default target.

## 1. Why this exists

Where vaccination answers "which goat needs which dose when", Feed Direction
answers "how much of which feed each shed gets, each session, each day", and how
that instruction changes when the count/shifting ledger changes.

The source-backed operating promise is:

1. Feed leadership publishes ration rules by breed and shed tag/stage.
2. GoatOS derives tomorrow's projected count from a Counts/Shifting-owned input
   contract that implements the source-required physical base-count anchor,
   realized shifting ledger, and authorized future-effective shiftings for the
   one-day projection.
3. GoatOS issues a full next-day Feed Direction at the configured morning publish
   time.
4. Eligible shiftings before the cutoff produce a Diff. Source-facing Diff may
   be presented as a net correction, but GoatOS stores and reconciles it as an
   affected-shed restatement so stale open work cannot survive.
5. Packing/staging reserves and consumes stock through the generic inventory
   ledger.
6. Packing, transport, consumption, wastage, and bridge exceptions are recorded
   with proof and verification.

## 2. Source priority and conflicts

Use this priority when sources disagree:

| Priority | Source | Use |
| --- | --- | --- |
| 1 | `Feed, Shiftings and Count.docx` v1.1, June 2026 | Canonical timing, count/shifting model, source-facing Diff semantics, bridge protocol, ration-key model |
| 2 | Feed Director operations and Feed Directions Automation docs/sheets | Feature inventory for sheet fields, form stages, retry/dedupe, transport proof/rejection, Slack notification behavior; not a code blueprint |
| 3 | Counting DB reconstruction | Count fixture/reference shape, not runtime truth |
| 4 | Older GoatOS feed docs and mock feed panels | Historical UI/direction references only where not contradicted above |

Known conflict: older docs and the current mock mention "v1 midnight / v2 2 PM".
The newer June 2026 source says Day N 09:00 full direction for Day N+1,
13:30 cutoff, 13:30-13:45 Diff, 15:00 stage, and Day N+1 09:00/15:00 serving.
Treat the newer model as the recommended default, keep the clock values
configurable, and get Feed Director sign-off before hardcoding schedules.

## 3. Scope

| In scope for the first reopened Feed Direction slice | Fast follow | Out of scope |
| --- | --- | --- |
| Feed protocol config in generic `rule_dsl` | FCR/cost dashboards | Parallel typed `feed_*` execution stack |
| Source-backed ration table: non-UI solver output or reviewed imported solve | Full NRC optimizer UI | Slack forms as canonical execution |
| Define and consume the horizon-aware Counts/Shifting input contract | RFID-to-shed individual association | Old 07:30 next-morning Diff design |
| Day N full direction and cutoff Diff | Uneven session split tuning | Feed procurement/fodder modules |
| Packing, reserve/consume, proof, verification | Item-level palatability caps | Full feed cost accounting |
| Consumption, transport, variance/wastage proof | Advanced transport optimization | |
| Post-cutoff high-priority bridge logging | | |

Initial ration keys are not raw age buckets. Adult rations are keyed by
`breed + shed_tag/stage` such as Early Gestation, Late Gestation,
Non-Pregnant/Maintenance, Early/Mid/Late Milking, Fattening, and Mother. Kid
rations are keyed by weight band and target ADG. Source fields such as `Age`
must be normalized through reference data and aliases before they affect ration
math. Warmup tags require explicit Feed Director sign-off because source findings
identify Warmup operating states but the feed docx ration table does not settle
their packed-feed quantities.

The full NRC optimizer UI can wait, but ration quantity provenance cannot. The
first slice must either run a non-UI RationTable solver for the source-required
feed-type constraints, or import reviewed solver outputs as source-backed
protocol rules. Either path must preserve provenance for cost/feed-vector
inputs, constraints, solver/import version, reviewer, reviewed_at,
review_status, approved_by, approved_at, and the re-solve/review trigger when
feed costs or available feed types change. A reviewed solve/import is not
publishable until `review_status='approved'`, approval metadata is present, and
the publish actor/job has `protocol.publish.feed_direction` or its approved
service equivalent.

Feed eligibility is reviewed config, not presentation cleanup. K0/K1
milk-fed exclusions and Experiment zero-direction handling are candidate policy
from legacy zero rows/automation behavior, not settled by the feed docx alone.
They must receive Feed Director approval before suppressing normal packed-feed
obligations. The June 2026 source default is two sessions with a 50/50 split.
Legacy per-farm templates still carry session labels and per-session feed sets;
the first implementation must either import them as reviewed config or record
Feed Director sign-off that the two-session simplification supersedes them.

## 4. Actors and authority

| Actor | Does | Authority |
| --- | --- | --- |
| Feed Director | Drafts ration rules, session policy, proof policy, and bridge policy | `protocol.draft.feed_direction` |
| COO / CEO | Publishes immutable effective-dated feed protocol versions | `protocol.publish.feed_direction` |
| Park Head / Manager | Monitors packing, transport, consumption, and variance for scoped park/sheds | Scoped operational read/approve |
| Feed worker | Packs, stages, transports, serves, and captures proof | Feed SOP execution |
| Verifier | Accepts/rejects quantities, videos, bridge entries, and variance explanations | `proof.verify` |
| System | Computes projections, directions, Diffs, obligations, reminders, escalations, and read models | Kernel jobs/services |

## 5. Core flow

```text
published feed protocol rule_dsl
  -> horizon-aware Counts/Shifting projection from latest physical base-count anchor
  -> tomorrow projected count, one-day horizon only
  -> Day N full direction run for Day N+1
  -> shed/session/feed obligations and packing tasks
  -> eligible pre-cutoff shiftings produce Diff runs from affected-shed
     restatement snapshots
  -> stale open obligations for affected sheds are canceled/superseded explicitly
  -> 15:00 packing/staging starts, stock reserve happens here
  -> accepted packing proof consumes actual quantity and releases remainder
  -> transport, consumption, and wastage stages record proof, rejection, and rework
  -> verification, missed/flagged escalation, projections, dashboards
```

Count/shifting dependency is required, not optional. A new physical Base Count
becomes the immediate ledger anchor even when discrepancy investigation remains
open. Feed must consume a horizon-aware Counts/Shifting contract. For realized
today's count (`count_as_of(now)`) and post-facto reconciliation, shifting rows
alter current truth only after the configured applied state is satisfied:
category, independent priority, source/destination, effective time,
authorization where required, completion/proof, and verification state. For the
Day N 09:00 one-day-ahead projection, authorized/directed future-effective
shiftings with deterministic source, destination, cohort/stage impact, and
`effective_at` inside the Day N+1 target date may be included before physical
proof; completion/proof/verification then gate Day N+1 realization,
reconciliation, exceptions, and rework. Pending requests without authorization,
and rejected or canceled movements, remain visible process work and must not
alter Feed Direction quantities. Every shifting event that contributes to an
aggregate count must be applied idempotently by `shifting_event_id`, so replay or
redelivery cannot double-apply the same movement into the realized ledger.

The count contract must fail closed on cohort/stage ambiguity. A movement whose
cohort/stage impact is missing, unresolved, or still free-text-only is excluded
from realized count and one-day projection, creates durable process-exception
work, and cannot silently change ration selection. Raw comments may be preserved
for audit; they are not policy truth.

Repo compatibility note: the committed Counts tables are source-row sync and
projection tables, and committed movement state is per-goat. They are not yet
the aggregate base-count, realized-shifting ledger, and horizon-aware projection
Feed Direction requires. Before Feed Direction is operational, gate `G2` must be
closed through the Counts/Shifting closure docs: the Counts/Shifting owner must
either provide that realized ledger plus horizon-aware projection at shed + breed
and stage/tag grain, or prove an equivalent derivation from per-goat location
history after RFID-to-shed association exists.

Packing, transport, consumption, and wastage are first-class execution stages,
not generic proof footnotes. Legacy feed execution has separate processed flags,
forms, videos, verifier decisions, rejection remarks, and discrepancy alerts.
GoatOS should model those as obligation/proof/verification state instead of
preserving processed flags as Boolean source-of-truth columns. Short-packed or
rejected packing proof must re-open/re-issue work rather than silently accepting
a lower quantity; this is a stronger GoatOS process requirement where legacy only
alerted or pinged admins.

Every stage-bearing Feed obligation, completion, or projection must carry a
durable `stage_kind` discriminator such as `packing`, `transport`,
`consumption_wastage`, or `bridge_exception`. It can live in a typed stage table,
an indexed obligation-context projection, or a first-class completion/stage
record, but bucket APIs must not infer stage from labels or legacy processed
flags.

Transport also has its own physical grain: multiple direction sheds can map to a
smaller transport-shed list through fully or partially consolidated shed names.
That mapping must be source-backed config owned with Locations/Feed operations,
not inferred from string labels in feed code.

Consumption/wastage proof must not stop at storing numbers. The first slice must
carry source-backed exception thresholds for packing expected-vs-actual mismatch
and wastage variance, with durable escalation/rework when the threshold is crossed.

Feed command/read models must preserve the operator buckets legacy Slack already
made visible, while keeping Feed UI gated until scope reopens: generation blocked,
packing due, packing shortfall, proof/video missing, transport pending, transport
rejected, consumption incomplete, wastage/discrepancy exception, bridge exception,
stock-out, missed/overdue, escalated, and rework. These buckets feed top-level
command lenses such as Calendar, Action Center, Control Tower, Protocol
Adherence, and Workflows through filters; do not create nested Feed copies of
those command/authority screens. The feed read model must expose stage, due,
deadline, owner, evidence, blocked reason, escalation, and resolution fields from
one backend source of truth.

High-priority additions after the cutoff do not get a next-morning system Diff.
They use the source-defined bridge protocol: the health/feed team places a 2x
daily ration at the destination shed with video proof, no source-shed claw-back,
and the formal Feed Direction catches up in the normal Day N+2 cycle. GoatOS
must log that bridge action as a first-class exception so consumption/wastage
reconciliation can see it.

## 6. Daily timeline

| Time | Step | Notes |
| --- | --- | --- |
| Day N 09:00 | Full Feed Direction for Day N+1 | Computed from tomorrow projected count; no stock locked yet |
| Day N 13:30 | Cutoff | Changes after this do not enter Day N+1 formal Diff |
| Day N 13:30-13:45 | Diff | Source-facing net correction may be emitted; canonical GoatOS run stores affected-shed/session/feed restatement rows and supersedes stale work |
| Day N 15:00 | Packing/staging | Stock reserve starts here; feed staged outside sheds |
| Day N+1 09:00 | Session 1 served | Proof/consumption recorded |
| Day N+1 15:00 | Session 2 served | Proof/consumption recorded |

Times are config values, not literals in code. Session split is currently 50/50,
which is a deliberate source simplification, not inferred nutrition logic. Legacy
automation also contained additional trigger windows, retries, and archive
timers; gate `G3` decides which are parity/cutover evidence and which become
GoatOS schedules.

## 7. Current GoatOS implementation state

Already present:

- Generic protocol/obligation/inventory/SOP primitives.
- `protocol.draft.feed_direction` and `protocol.publish.feed_direction`
  capability vocabulary.
- `feed_direction_completions` plus a draft `feed.direction` SOP skeleton.
- `backend/internal/feed` domain/app/repository/sqlc code for record,
  accept/reject, history, and verification queue.

Missing before Feed Direction is operational:

- No canonical Counts/Shifting horizon-aware aggregate projection contract.
- No non-UI RationTable solver or reviewed solver-output import with provenance.
- No generation run or Diff pipeline.
- No feed obligation generation path.
- No HTTP adapter/route/OpenAPI contract for Feed Direction.
- No production entry point through scheduler, consumer, or sweeper.
- No reserve/consume integration wired from feed completion.
- No stage model for packing, transport, consumption, wastage, rejection, and
  rework against the one-row-per-obligation `feed_direction_completions` table.
- No active admin-web or operator-mobile Feed Direction screens.

## 8. UI/UX requirements

Do not claim the current mock already contains pagination/filter anatomy for Feed
Direction. The current feed mock is a static chart band plus plain Feed
Directions table and Feed Execution panels. It also carries stale timing labels
and Parks breadcrumbs.

When Feed Direction scope is reopened:

1. Update the mock first, or explicitly document which stale mock labels are
   superseded by the source docs.
2. Keep Feed Direction under the Feed vertical; park/date remain top-bar scope.
3. Preserve the mock's density, cards, status tags, chart/table rhythm, execution
   lock, SOP buttons, and proof/status surfaces.
4. Add filters, server pagination, row drawers, and column controls only after
   the mock/contract are updated to include them.
5. Add Feed Direction paths to frontend mock-fidelity scan coverage when the UI
   becomes in scope.
6. Keep backend-owned labels, options, filters, and disabled reasons.

## 9. Success metrics

- Full direction generated on the configured morning SLA.
- Diff generated before staging for all eligible pre-cutoff shiftings: operator
  output can be a net correction, while canonical rows are affected-shed
  restatements with explicit cancel/supersede of stale work.
- Count gates are horizon-specific: realized counts and reconciliation require
  the configured applied state, while one-day projection may include only
  authorized/directed future-effective shiftings for the target date.
- Shifting ledger application is idempotent by event id, and unresolved
  cohort/stage impact fails closed before counts or ration selection change.
- Warmup, K0/K1, Experiment-shed, and breed/stage alias policies cannot publish
  without reviewed source/provenance and Feed Director sign-off.
- K0/K1 and Experiment-shed exclusions do not generate normal packed feed
  obligations only after the approved policy says so.
- Zero stale open obligations after an affected-row Diff or cancel/rebuild.
- Stock never goes negative and every reserve/consume/release is ledger-backed.
- Transport obligations use reviewed direction-shed to transport-shed
  consolidation config where source operations require grouped staging.
- Packing discrepancy and wastage variance thresholds create visible exception or
  rework state, not only stored percentages.
- Packing, transport, consumption, wastage, and bridge proof each have visible
  verification state.
- Rejected proof or packing shortfall records a reason and creates rework/next
  action.
- High-priority post-cutoff bridge actions are logged and reconciled.
- Reminder, notification, missed/recovery, audit, and observability paths exist
  for every execution stage before production readiness.
- Read models remain bounded by tenant, park, date, shed, session, status, and
  cursor pagination at million-goat scale.

## 10. Open questions

1. `G1`: Confirm when Feed Direction scope is reopened for UI/Config/SOP
   exposure; hidden backend prep may continue before that.
2. `G2`: Close the Counts/Shifting input contract in the sibling closure docs:
   aggregate base-count ledger vs derivation from per-goat history after
   RFID-to-shed association exists.
3. `G3`: Confirm Feed Director clock values per park, including how legacy extra
   trigger windows, retry scheduler, and archive timers are treated.
4. `G4`: Confirm whether the first slice runs the non-UI ration solver or imports
   reviewed solver outputs as source-backed config.
5. `G4`: Confirm initial feed vectors, costs, ration aliases, source-backed
   ration values, kid weight-band/ADG inputs, and roughage/category floor values.
6. `G5`: Confirm Warmup, K0/K1, Experiment-shed, F2/Fattening, SIROHI->Beetal,
   and other alias/exclusion policies.
7. `G6`: Confirm feed unit and baking-soda precision policy before implementing
   inventory wiring.
8. `G7`: Confirm the stage model and durable `stage_kind` home: typed stage
   records, indexed obligation context, or first-class completion/stage rows.
9. `G8`: Confirm transport consolidation map ownership and whether it is
   maintained in Locations config, Feed protocol config, or a dedicated
   source-backed mapping; decide whether a transport checklist entity is needed.
10. `G9`: Confirm packing discrepancy tolerance, wastage variance thresholds, and
   where GoatOS intentionally strengthens legacy alert-only behavior into rework.
11. `G10`: Assign security remediation ownership, inventory affected legacy
   scripts, rotate or revoke Slack tokens/webhook shared secrets, move any
   retained bridge credential to secret storage, and prove GoatOS API-only
   ingress before any Slack bridge is reused.
12. `G11`-`G15`: Confirm reminder/escalation SLA, NotificationGateway routing,
   missed/recovery events, audit/observability, and command-lens field mapping.
