# Feed -> Feed Direction - Product Requirements (PRD)

**Status:** Draft v5, refined against source-first counter-review, read-model review, and dependency-closure audit
**Date:** 2026-06-30
**Vertical:** Feed. Feed Direction is the first Feed module. Parks are a scope
dimension, not the owning vertical.

**Scope gate:** `G1` is reopened for Feed Direction build as of 2026-06-30
because local PHC/Vaccination UI and foundation closure is accepted for
sequencing. Google dev rollout remains a separate Goal 2 gate and must not be
claimed as done, but it does not block Feed Direction. Active Feed UI, SOP
cards, and Config categories still require the Feed-owned gates, backend
contracts, mock-fidelity, rendered proof, and no fake production data.

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

**Build-to-done charter:** The next full Feed Direction build must follow
[BUILD-TO-DONE-GOAL.md](./BUILD-TO-DONE-GOAL.md). That charter owns the session
stop rules for source priority, backend/frontend integration, seeds, E2E proof,
high-effort review agents, and Mesha/VGoats push verification.

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
| 1 | `Feed, Shiftings and Count.docx` v1.1, June 2026 | Primary Feed Direction business source: Base Count, append-only Shifting ledger, one-day projection, timing, Diff, manual bridge SOP/logging, as-fed quantities, and ration solver constraints |
| 2 | Feed-relevant wiki/source docs and findings | `context/source-findings/goats-and-parks-source-findings.md` for base goat/park/stage/shed-tag semantics; Counting DB reconstruction, Feed Transfer KT findings, Shifting reports, Feed Director ops, transport consolidation, Warmup/K0/K1/Experiment evidence, and feed-stock/procurement boundaries |
| 3 | `Counting DB - values only.xlsx` and `Feed Directions Automation DB.xlsx` | Workbook/tab/column/formula evidence for imports, parity fixtures, validation gaps, ration tables, session templates, processed flags, and execution/proof stages; not runtime truth or target schema |
| 4 | Legacy Slack/App Script feed workflows | Feature inventory for sheet fields, form stages, proof/rejection, reset/re-send, retry/dedupe, transport, notifications, and security/cutover evidence; not a code blueprint |
| 5 | GoatOS protocol/kernel docs and committed code | Implementation shape for protocol, obligations, SOP proof, inventory, audit, outbox, reminders, read models, RBAC, OpenAPI, and generated clients |
| 6 | Older GoatOS feed docs and mock feed panels | Historical UI/direction references only where not contradicted above; the mock controls UI anatomy, not Feed business timing |

Source authority rule: for the Feed Direction slice, always follow
`Feed, Shiftings and Count.docx` v1.1 when timing, Diff, bridge, projection,
quantity, or ration behavior conflicts with legacy scripts, older mocks, or
prior GoatOS notes. Legacy Slack/App Script trigger times are audit/cutover
evidence only and must not become GoatOS schedules unless the Feed Director
explicitly approves a retained/replaced schedule against that docx.

Goats and Parks base rule: Feed Direction must also obey
`context/source-findings/goats-and-parks-source-findings.md` for goat identity,
park/shed scope, shed tags, lifecycle/stage, warm-up, pregnancy, lactation,
fattening, milking, handling, weighing, feed-session, and feed-safety semantics.
The Feed source owns Direction timing and ration behavior; it does not erase the
base shed-tag/cohort meanings.

Sheds DB source rule: Feed Direction must treat
`context/source-findings/sheds-db-source-findings.md` as legacy evidence for
manual ground-reality shed tags, capacity-like values, and potential tags. These
values must flow through governed Location/Park profile CRUD, review, effective
dates, and workflow/event updates before Feed uses them. A raw Sheds DB value or
manual spreadsheet edit cannot silently authorize a ration, shed capacity, or
high-risk cohort placement.

Known conflict: older docs and the current mock mention "v1 midnight / v2 2 PM".
The newer June 2026 source says Day N 09:00 full direction for Day N+1,
13:30 cutoff, 13:30-13:45 Diff, 15:00 stage, and Day N+1 09:00/15:00 serving.
Treat the newer model as the recommended default, keep the clock values
configurable, and get Feed Director sign-off before hardcoding schedules.

Bridge conflict rule: the June 2026 source keeps high-priority post-cutoff
additions as a manual SOP. GoatOS may log the top-up and proof, but must not
build the superseded `07:30` next-morning system Diff unless the owner explicitly
reopens that design.

Workbook boundary rule: `Counting DB - values only.xlsx` and
`Feed Directions Automation DB.xlsx` are source evidence for migration, typed
imports, parity checks, and known gaps. Their tabs (`Count-DB`,
`CPT Validation`, `CBE Validation`, `Feed-Energy-Protein`, `Supply Planning`,
`Template`, `Feed Packing Form`, `Feed Transport Form`, and
`Feed Consumption & Wastage`) must not become GoatOS product modules, runtime
tables, or hardcoded business rules. GoatOS owns the kernel model: typed
Postgres state, governed CRUD/import/review/publish config, immutable generation
snapshots, stage obligations, proof/rework, audit/outbox, and bounded read
models.

## 3. Scope

| In scope for the first reopened Feed Direction slice | Fast follow | Out of scope |
| --- | --- | --- |
| Feed protocol config in generic `rule_dsl` | FCR/cost dashboards | Parallel typed `feed_*` execution stack |
| Source-backed ration table: non-UI solver output or reviewed imported solve | Full NRC optimizer UI | Slack forms as canonical execution |
| Define and consume the horizon-aware Counts/Shifting input contract | RFID-to-shed individual association | Old 07:30 next-morning Diff design |
| Day N full direction, cutoff Diff, and versioned session-slot policy | FCR/cost dashboards | Feed procurement/fodder modules |
| Packing, reserve/consume, proof, verification | Item-level palatability caps | Full feed cost accounting |
| Consumption, transport, variance/wastage proof | Advanced transport optimization | Milk Preparation verification workflow |
| Post-cutoff high-priority manual bridge logging | | |

Milk Preparation is feed-adjacent legacy verification, but it is not part of the
packed-feed Feed Direction slice. If it is reopened, scope it as a sibling
verification workflow with its own Pending/Verified/Rejected proof contract
instead of silently folding it into packing, transport, or wastage.

Initial ration keys are not raw age buckets. Adult rations are keyed by
`breed + shed_tag/stage` such as Early Gestation, Late Gestation,
Non-Pregnant/Maintenance, Early/Mid/Late Milking, Fattening, and Mother. Kid
rations are keyed by weight band and target ADG. Source fields such as `Age`
must be normalized through reference data and aliases before they affect ration
math. Warmup tags require explicit Feed Director sign-off because source findings
identify a 14-day Warmup transition and Warmup operating states, but the feed
docx ration table does not settle their packed-feed quantities. ICU, Quarantine,
Flushing, and Breeding shed tags also require explicit ration path or exclusion
decisions instead of inheriting default packed-feed behavior.

Feed Transfer KT adds an important gap: ration/constraint sheets may be keyed by
breed, tag/stage, energy/vector policy, weight band, warm-up, pregnancy, and
other nutrition dimensions without carrying authoritative shed placement. Do not
treat those constraint tables as proof that a shed has that tag or cohort. GoatOS
must keep the physical count/projection grain (`park + shed + breed + horizon`)
separate from the nutrition/ration cohort key, then resolve between them through
reviewed source-backed context. If the resolver cannot prove the shed tag/cohort
for a projected count row, generation blocks with visible exception work instead
of guessing a ration.

KT-specific examples are directional source evidence and must be validated before
publication: feed unit vectors/energy capacity, an `80/20` packing or
distribution factor, grain vs dry/green leaf feed-type grouping, `400-500g` and
`600g` quantity/weight examples, `F1` around `11-15kg`, `F2` around `15-20kg`,
pregnant-animal priority windows around `12:30-15:00`, and `90-95%`
shed/pack/breed/tag/energy validation with warm-up allowance. These examples do
not override `Feed, Shiftings and Count.docx`: `80/20` is not the default
session split, and KT scheduling windows are not GoatOS product clocks unless
Feed Director approval turns them into effective-dated policy.

No KT or workbook number is a global default. Ratios, quantities, thresholds,
and tolerances vary by approved runtime template dimensions: breed, shed
tag/stage, normalized age/stage alias, kid weight band/ADG,
pregnancy/lactation/warm-up policy, feed vector family, farm/source context, and
effective-dated version. GoatOS must ingest these as typed parameter rows,
validate aliases and dimensions, run calculation preview, surface bad rows for
repair/DLQ, and publish only a reviewed `feed_direction_config_pack`.

Pregnant, lactating, and warm-up animals are safety-critical cohorts. If a
pregnant goat or pregnant aggregate is shifted into a new shed, the destination
shed must not keep the old shed's normal average feed by accident. GoatOS must
re-resolve the projected shed + breed count into the approved nutrition cohort,
recalculate ration/session quantities for the destination shed, and block or
escalate when the resolver, source parameters, stock, or serving slot cannot
cover that cohort. Underfeeding creates abortion/kid-loss and herd-growth risk;
overfeeding creates feed waste, moist/unsafe leftover feed, refusal-to-eat, and
sickness risk. Both sides are first-class Feed exceptions, not later analytics.

The workbook and legacy automation review confirms why this cannot be a
Sheet-clone build. Legacy `Count-DB`, validation, supply-planning, and template
tabs use formulas, hidden copies, string transforms, processed flags, and script
glue to bridge count rows to ration rows. GoatOS must expose governed admin/data
ops CRUD and import flows for the business-managed pieces those tabs attempted
to hold: feed item nutrient vectors and costs, feed-type constraints, breed and
stage/tag aliases, kid weight-band/ADG rules, quantity/weight thresholds,
warm-up/pregnancy policy, eligibility/exclusions, ration solver/import outputs,
transport maps, proof thresholds, validation tolerances, and
session-slot/feed-set policy.

The admin Config surface for `feed_direction` must expose this shape, not a
single hardcoded ration field: source tables, parameter families, dimension
keys, ratio/quantity policy, session slots and weights, validation checks,
calculation outputs, source/review metadata, and disabled-with-reason calculation
preview until the backend import/solver endpoint exists.

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

Initial solver scope is deliberately narrower than the eventual nutrition
optimizer. It is feed-type-level only: hard floor/ceiling, structural ratio,
category floor, and quantity floor. Item-level feed ceilings and palatability
modeling are deferred. Apply the `60:40` structural ratio only to Milking and
Fattening tags, treat roughage/category floor numbers such as 30 percent as
examples until confirmed per tag, rely on cost minimization rather than adding a
paired overshoot ceiling, and replace the RationTable output wholesale after a
reviewed re-solve instead of blending old and new versions.

Feed eligibility is reviewed config, not presentation cleanup. K0/K1
milk-fed exclusions and Experiment zero-direction handling are candidate policy
from legacy zero rows/automation behavior, not settled by the feed docx alone.
They must receive Feed Director approval before suppressing normal packed-feed
obligations. The June 2026 source default is two sessions with a 50/50 split, and
the same source calls that split a deliberate simplification to revisit if a
breed + tag combo needs an uneven split. GoatOS therefore must model session
slots as versioned admin config, not hardcoded code. A Feed Director can draft
more slots, disable/reorder slots, change serving times, change split weights, or
scope feed-item inclusion by reviewed park/farm/shed-tag/breed policy. COO/CEO
publish makes the policy immutable for an effective date. The first published
policy should default to the docx two slots unless the Feed Director approves a
different source-backed session policy.

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
closed through the Counts/Shifting closure docs with aggregate shed + breed
output. Ration context must resolve from reviewed source-backed evidence before
ration lookup; unresolved context is a fail-closed blocker, not a default. It is
not a separate physical count grain. RFID-to-shed per-animal association is
planned but not implemented in the primary source and must not be introduced as
hidden initial Feed scope. A future per-goat derivation can only replace the
aggregate ledger after RFID/location confidence is separately proven and
owner-approved.

Packing, transport, consumption, and wastage are first-class execution stages,
not generic proof footnotes. Legacy feed execution has separate processed flags,
forms, videos, verifier decisions, rejection remarks, and discrepancy alerts.
GoatOS should model those as obligation/proof/verification state instead of
preserving processed flags as Boolean source-of-truth columns. Short-packed or
rejected packing proof must re-open/re-issue work rather than silently accepting
a lower quantity. Legacy evidence includes alert/admin-ping behavior and a
separate packing quantity reset/re-send loop; GoatOS formalizes the useful
behavior as typed rework/reissue instead of copying Sheet flag resets.

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
must log that manual action as a first-class exception with at least `shed_id`,
`animal_id` or approved aggregate reference, `timestamp`, `quantity`, and proof
link, plus source event/logical shifting reference where known and
reconciliation state so consumption/wastage reconciliation can see it. It must
not generate a bridge Diff row that changes source-shed quantities.

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
which is a deliberate source simplification, not inferred nutrition logic. Do not
mix this canonical docx timeline with old Apps Script trigger times. Gate `G3`
audits legacy trigger functions, retries, proof checks, stock checks, and archive
timers only so the owner can retire, retain, or replace legacy side effects
during cutover. The default is not to carry any old Apps Script timing into
GoatOS unless the Feed Director explicitly approves it against
`Feed, Shiftings and Count.docx`. Base Count cadence is an operational policy
that has already moved from roughly weekly to roughly monthly in source history;
store it as reviewed policy or schedule, not a code constant.

The two serving rows above are the source-backed default session policy, not a
permanent slot limit. Admins with Feed Direction draft/publish authority can
change or add serving slots through a new effective-dated protocol version. The
policy must validate that active slot weights sum to the full daily as-fed
quantity for each applicable feed item/scope, that generated FeedDirection and
Diff rows are per configured slot, and that already-generated target dates are
not silently changed; any after-generation change requires explicit supersession
or starts with the next ungenerated target date.

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
- No typed importer/admin CRUD/review/publish surface for feed vectors,
  constraint tables, aliases, eligibility, session slots, transport maps, and
  proof thresholds.
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

Under reopened `G1`, when building Feed Direction UI:

1. Update the mock first, or explicitly document which stale mock labels or
   missing Feed details are superseded by the source docs.
2. Keep Feed Direction under the Feed vertical; park/date remain top-bar scope.
3. Preserve the mock's anatomy: layout, density, typography scale, spacing,
   colors/tokens, cards, status tags, chart/table rhythm, execution lock, SOP
   buttons, proof/status surfaces, icon-button sizing, hover/active/focus
   states, disabled states, empty/error states, drawers, filters, and pagination.
4. Add filters, server pagination, row drawers, and column controls only after
   the mock/contract are updated to include them.
5. Add Feed Direction paths to frontend mock-fidelity scan coverage when the UI
   becomes in scope.
6. Keep backend-owned labels, columns, options, filters, buckets, action
   availability, pagination semantics, and disabled reasons. The frontend owns
   layout and interaction state, not product truth.
7. Before any frontend push or handoff, run
   `npm --prefix apps/admin-web run check:mock-fidelity` and capture rendered
   visual proof at relevant desktop and mobile widths.

## 9. Success metrics

- Full direction generated on the configured morning SLA.
- Diff generated before staging for all eligible pre-cutoff shiftings: operator
  output can be a net correction, while canonical rows are affected-shed
  restatements with explicit cancel/supersede of stale work.
- Count gates are horizon-specific: realized counts and reconciliation require
  the configured applied state, while one-day projection may include only
  authorized/directed future-effective shiftings for the target date.
- Ration-context resolution is explicit: a projected shed + breed count row must
  resolve to reviewed nutrition cohort context before ration lookup. Breed/tag
  constraint tables without shed placement are not enough to publish quantities.
- Shifting ledger application is idempotent by event id, and unresolved
  cohort/stage impact fails closed before counts or ration selection change.
- Warmup, K0/K1, Experiment-shed, and breed/stage alias policies cannot publish
  without reviewed source/provenance and Feed Director sign-off.
- K0/K1 and Experiment-shed exclusions do not generate normal packed feed
  obligations only after the approved policy says so.
- Field, packing, Feed Direction, and Diff quantities are as-fed gross values
  only. `wastage_factor` and `DM_factor` remain internal nutrient-accounting
  inputs and must not surface to packing/field teams as instruction quantities.
- Zero stale open obligations after an affected-row Diff or cancel/rebuild.
- Stock never goes negative and every reserve/consume/release is ledger-backed.
- Transport obligations use reviewed direction-shed to transport-shed
  consolidation config where source operations require grouped staging.
- Packing discrepancy and wastage variance thresholds create visible exception or
  rework state, not only stored percentages.
- Shifted pregnant/lactating/warm-up cohorts are recalculated against the
  destination shed before generation/Diff, with fail-closed shortage,
  overpack/wastage, and moist/unsafe-leftover exceptions.
- Packing, transport, consumption, wastage, and bridge proof each have visible
  verification state.
- Rejected proof or packing shortfall records a reason and creates rework/next
  action.
- High-priority post-cutoff manual bridge actions are logged and reconciled, with
  no generated `07:30` next-morning bridge Diff.
- Reminder, notification, missed/recovery, audit, and observability paths exist
  for every execution stage before production readiness.
- Read models remain bounded by tenant, park, date, shed, session, status, and
  cursor pagination at million-goat scale.

## 10. Open questions

1. `G1`: Reopened for Feed Direction build as of 2026-06-30. Keep Google dev
   rollout separate from this claim, and do not expose Feed UI/Config/SOP
   surfaces without Feed-owned contracts, source-backed or clearly marked
   local-dev data, mock-fidelity, and rendered proof.
2. `G2`: Close the Counts/Shifting input contract in the sibling closure docs at
   aggregate shed + breed grain, with ration context resolved from reviewed
   source-backed evidence or surfaced as a fail-closed blocker. Breed/tag-only KT
   constraint tables do not provide shed placement by themselves. RFID-to-shed
   per-goat derivation is out of initial Feed Direction scope and can only replace
   the aggregate ledger after separate proof and owner approval.
3. `G3`: Confirm Feed Director clock values per park from
   `Feed, Shiftings and Count.docx`: Day N `09:00` full direction, Day N
   `13:30` cutoff, Day N `13:30-13:45` Diff, Day N `15:00` staging, and Day N+1
   `09:00`/`15:00` serving. Separately audit legacy Apps Script triggers,
   retries, proof/stock checks, and archive timers as retain/retire/replace
   cutover evidence, not product clocks.
4. `G4`: Confirm whether the first slice runs the non-UI ration solver or imports
   reviewed solver outputs as source-backed config.
5. `G4`: Confirm initial feed vectors, costs, ration aliases, source-backed
   ration values, kid weight-band/ADG inputs, roughage/category floor values, and
   how uploaded breed/tag/energy constraint tables map into reviewed ration
   cohort keys through typed import/CRUD/review/publish, not workbook formulas.
   Explicitly review KT examples such as `80/20`, `400-500g`, `600g`, `F1`
   `11-15kg`, and `F2` `15-20kg` before treating them as protocol values.
6. `G5`: Confirm Warmup 14-day transition handling, ICU, Quarantine, Flushing,
   Breeding, K0/K1, Experiment-shed, F2/Fattening, SIROHI->Beetal, and other
   alias/exclusion policies, plus which admin roles may draft/publish
   effective-dated session-slot changes.
7. `G9`: Confirm whether KT `90-95%` shed/pack/breed/tag/energy matching and
   warm-up allowance become reviewed validation thresholds, draft-only warnings,
   or rejected legacy heuristics.
8. `G6`: Confirm feed unit and baking-soda precision policy before implementing
   inventory wiring.
9. `G7`: Confirm the stage model and durable `stage_kind` home: typed stage
   records, indexed obligation context, or first-class completion/stage rows.
10. `G8`: Confirm transport consolidation map ownership and whether it is
   maintained in Locations config, Feed protocol config, or a dedicated
   source-backed mapping; decide whether a transport checklist entity is needed.
11. `G9`: Confirm packing discrepancy tolerance, wastage variance thresholds, and
   how legacy alert/reset evidence maps into GoatOS typed rework/reissue.
12. `G10`: Assign security remediation ownership, inventory affected legacy
   scripts, rotate or revoke Slack tokens/webhook shared secrets, move any
   retained bridge credential to secret storage, and prove GoatOS API-only
   ingress before any Slack bridge is reused. If `G10` is owner-deferred, the
   Slack bridge remains disabled and no Slack overlap/reuse counts as done.
13. `G11`-`G15`: Confirm reminder/escalation SLA, NotificationGateway routing,
   missed/recovery events, audit/observability, and command-lens field mapping.
