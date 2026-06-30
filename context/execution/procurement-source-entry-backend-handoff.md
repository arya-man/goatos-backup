# Procurement Source Entry Backend Handoff

Date: 2026-06-24

Purpose: define the backend slice for goats whose journey starts at purchase or
source-side holding, not only when they arrive at CBE/CPT parks.

This is a **separate procurement/source-entry slice**, not a sub-tab of PHC
Vaccination. The current vaccination slice may read accepted-intake outputs
such as `origin_type`, `entry_date`, and trusted intake/vaccination history, but
it must not own the procurement journey.

## Implementation Gate

The procurement/source-entry backend slice has been explicitly opened. Keep the
backend contracts procurement-scoped and do not broaden PHC Vaccination or Parks
execution behavior.

For any further backend work, keep this source-of-truth order:

```text
database/migrations
  -> repository/service/use-case tests
  -> OpenAPI contracts
  -> generated api client
  -> backend route smoke and query-plan validation
```

Do not ask the frontend to build procurement screens from mock rows or guessed
types. Frontend may proceed only from generated contracts and the E2E plan:

```text
context/execution/procurement-vaccination-e2e-plan.md
```

## Read First

```text
AGENTS.md
SKILLS.md
context/README.md
context/product/glossary.md
context/source-findings/goats-and-parks-source-findings.md
context/source-findings/drive-docs-findings.md
context/product/goat-os-feature-phases.md
docs/phc-vaccination/V1-FOUNDATION-SPEC.md
docs/protocol-engine/migration-and-cutover.md
context/execution/vaccination-process-integrity-backend-handoff.md
```

## Source Truth

The business case is documented, but the newest operator clarification makes the
edge case explicit:

```text
goat journey starts at purchase/source
  -> supplier / holding farm warmup is purpose-specific
     breeding: 45-70 days is realistic today
     fattening / non-breeding: can be 0 days or around 2 weeks
  -> source-side tagging/warmup may happen before final accepted intake
  -> goat may be rejected before being loaded onto truck
  -> only accepted goats continue to transit/main parks
```

Existing documented basis:

```text
context/source-findings/goats-and-parks-source-findings.md
  base goat/park semantics for identity, tags/RFID, breeds, shed tags,
  warm-up, pregnancy, fattening, weighing, handling, and trained-role boundary.

context/product/glossary.md
  HF / Holding Farm:
    source-side facility after procurement and before dispatch to main parks.
    holding period originally documented as roughly 2 to 8 weeks, but treat this
    as optimistic/future planning, not a hard cap.
    used for initial selection, tagging, health SOP, and source-side holding.
  Warmup:
    source warm-up is separate from destination/park warmup and must store actual
    start/end/days by load/goat/purpose.

context/source-findings/drive-docs-findings.md
  procurement load -> holding farm/source context -> transit/handoff proof
  -> arrival gate -> discrepancy review -> accepted herd intake.
  Do not skip the arrival gate.

context/product/goat-os-feature-phases.md
  procurement should model purchase/load records, vendor/source history,
  holding farms, advance-paid/shared-pending ownership, transit/handoff proof,
  arrival gate, discrepancy review, intake health SOP, and landing cost.

docs/phc-vaccination/V1-FOUNDATION-SPEC.md
  procurement DB evidence is arrival/intake/history evidence for vaccination,
  including load/vendor/tag/update/selection health/unloading/transit and
  historical vaccination-at-procurement evidence.
```

## Current Implementation State

Backend contracts and the modular procurement/source-entry backend now exist for
the active slice:

```text
procurement backend module
purchase/load records
source/candidate load-goat records
source health outcome
pre-dispatch accept/reject/defer/block decision
dispatch / truck loading proof gate
arrival review
accepted intake handoff
procurement Action Center / Protocol Adherence / Control Tower / Workflow
read models for top-level command screens only
OpenAPI contracts and generated clients
route/permission/query-plan coverage
```

Backend may expose procurement read-model API contracts, but frontend must not
mirror those contracts as nested procurement command-room pages. Control Tower,
Action Center, Protocol Adherence, and Workflows remain top-level UI routes.

## Cross-Slice Architecture Guardrails

Procurement/source-entry is a module/vertical slice, not a new command-room
platform. Its command-lens data must mount through the existing top-level
process-integrity surfaces by domain:

```text
/                         Control Tower, with procurement selected
/action-center?domain=procurement
/protocol-adherence?domain=procurement
/workflows?domain=procurement
/workflows/{row_id}?domain=procurement
```

Do not create a second procurement Action Center, Protocol Adherence, Workflows,
or Control Tower route/service tree under `/procurement/source-entry`. If a
procurement-specific repository already computes rows, treat it as a
module-specific adapter feeding the shared process-integrity contract, not a
parallel command engine.

SOP/proof must remain the platform SOP/proof engine:

```text
procurement source health, dispatch proof, transit handoff proof, arrival proof
  -> existing SOP task/submission/proof APIs and policies
  -> existing verification/rework semantics
```

Any large SOP/proof/contract changes in the working tree must be audited against
the existing SOP/proof modules before use. Extending the platform engine is
correct; forking a procurement-only SOP runtime is not.

The accepted-intake handoff to vaccination is not complete until it is proven
end-to-end at backend level:

```text
accepted intake
  -> procurement_phc_handoffs row/event with idempotent event_status
  -> relay/consumer or synchronous service path runs
  -> vaccination obligation generation sees only accepted-intake goats
  -> rejected/source-only/unresolved goats generate no PHC vaccination work
  -> CT / AC / PA / WF / Vaccination read models update from that same state
```

`event_status` is tracking, not proof of delivery. Tests must assert the
downstream vaccination obligations/read models changed, not only that a handoff
row was inserted.

Still pending before calling the slice complete:

```text
seeded source-entry -> accepted-intake -> vaccination E2E
frontend procurement/source-entry screens and click QA
CRUD/buttons proven by E2E, not broad generic CRUD
production monitoring/reconciliation for long-running failed work
landing-cost and shrinkage/mortality economics, unless separately reopened
```

Use `context/execution/procurement-vaccination-e2e-plan.md` as the next
source of truth for seed shape, assertions, and UI button placement.

## Target Flow

```text
purchase/load created
  or candidate/source-only goat tagged at supplier
  -> source/vendor/holding-farm stay opened
  -> source warmup runs (purpose-specific; breeding supports 45-70 days today,
     fattening/non-breeding may be 0 days or around 2 weeks)
  -> tagging + source health SOP tasks
  -> pre-dispatch decision
       accepted: eligible for truck/transit
       rejected: stays out of canonical accepted herd and vaccination obligations
       deferred/blocked: visible action item
  -> truck load + transit proof
  -> arrival gate at main park
  -> count/identity/health/weight/media reconciliation
  -> accepted herd intake
  -> goat canonical state becomes usable for PHC/post-arrival vaccination rules
```

## Required Case Coverage

The implementation must cover these branches explicitly. Do not hide them as
generic notes or free-text status.

| Case | Backend truth required | Downstream effect |
| --- | --- | --- |
| Purchased goat enters supplier/holding farm | Load, source, holding stay, temporary/source identity, ownership state | Not yet clean park herd truth |
| Candidate/source goat tagged before final acceptance | Source-only identity, source tag/RFID, holding stay, selection/ownership state | Visible in procurement only; not a PHC/Parks goat yet |
| Candidate/source goat rejected before purchase/load/truck | Durable source rejection decision, reason, actor, time, proof | Procurement history only; no active park work, no active PHC vaccination obligation |
| Source warmup duration varies by purpose | Warmup start/end/days, purpose/classification, state, reason if outside purpose-specific window | Breeding 45-70 days is valid today; fattening/non-breeding can be 0 days or around 2 weeks; do not hard-cap from the old 2-8 week note |
| Tagging done at supplier | Identifier evidence and source tag/RFID mapping | Goes through identity module/review, not blind duplicate goat creation |
| Source health SOP passes | SOP task/submission/proof/reviewer state | Goat can move to pre-dispatch decision |
| Source health SOP fails | Failed/rejected/deferred health state, reason, proof | Blocks truck loading; may become rejected/deferred |
| Goat rejected before truck loading | Durable pre-dispatch rejected decision, reason, actor, time, proof | No active park work, no active PHC vaccination obligation |
| Goat deferred before truck loading | Deferred decision, reason, resume condition, owner | Stays in Action Center as source-entry work |
| Missing proof before dispatch | Proof requirement gap, owner, due window | Action Center gap, Control Tower if severe/aged |
| Identity/tag conflict before dispatch | Identity review reference and blocker | Cannot become accepted herd intake until resolved |
| Partial load accepted | Per-goat decisions under one load | Accepted goats proceed; rejected/deferred goats remain separate history |
| Accepted goat not loaded | Loading discrepancy state | Stays open for resolution; no arrival acceptance |
| Truck loading proof submitted | Proof record linked to load/goats/SOP task | Dispatch can progress if verification policy passes |
| Goat dies/sold/lost during holding/transit | Exit/cancel event with reason and proof | Cancels/rescopes active procurement/PHC obligations |
| Arrival count mismatch | Expected, loaded, arrived, missing, extra counts | Arrival gate blocks accepted intake until reconciled |
| Unknown/extra goat arrives | Temporary record and identity review | Cannot silently enter canonical herd |
| Health/weight issue at arrival | Arrival review flags and optional quarantine/defer | Accepted intake may be blocked/deferred; PHC may see defer signal |
| Arrival accepted | Accepted intake event, park/shed assignment, entry date | Goat becomes usable for post-arrival PHC/vaccination rules |
| Historical vaccination at procurement exists | Evidence/completion candidate linked to source proof | PHC backfill/review decides whether it counts; avoid duplicate due work |
| Arrival rejected | Arrival rejection reason/proof | Goat does not become active park/vaccination work |
| Ownership still shared/pending | Ownership/settlement state | Visible in procurement; do not claim clean Mesha ownership |
| Accepted intake -> vaccination handoff | Accepted intake event, canonical goat/location state, vaccination handoff marker/event | Post-arrival PHC obligations may be generated only from accepted-intake truth |

Required invariant:

```text
No pre-accepted, rejected-before-truck, arrival-rejected, dead, sold, lost,
candidate/source-only, rejected-before-purchase/load, unknown/extra-unresolved,
ownership-blocked, or identity-conflict goat may appear as active PHC
vaccination work or vaccination execution work.
```

## Screen/API Read Models

Backend must expose source-of-truth read models that can power every future
screen without frontend guessing.

| Screen | Backend data shape |
| --- | --- |
| Source Entry Board | Load rows grouped by state: source_warmup, health_pending, pre_dispatch_pending, rejected, deferred, dispatch_ready, in_transit, arrival_review |
| Load Detail | Timeline from purchase -> holding stay -> SOP/proof -> pre-dispatch decision -> loading -> transit -> arrival -> intake |
| Pre-Dispatch Decision | Per-goat decision contract with accept/reject/defer/block, reason, proof, owner, audit |
| Arrival Gate | Expected/loaded/arrived/matched/missing/extra counts, identity/health/weight flags, media proof, review status |
| Top-level Action Center lens | Exact procurement work: overdue health SOP, missing proof, identity conflict, deferred, rejected review, arrival mismatch, owner missing |
| Top-level Protocol Adherence lens | Expected vs actual for source health, tagging, dispatch proof, transit proof, arrival review, intake acceptance |
| Top-level Control Tower lens | Exception-only procurement gaps: aged warmup, high rejection, missing proof, unresolved arrival mismatch, owner missing |
| Top-level Workflows lens | Chain nodes from load creation to accepted intake with current/blocked/completed state |
| SOP Library | Source health, pre-dispatch, truck loading, transit handoff, arrival gate SOP definitions/versions |
| PHC Vaccination | Only accepted-intake outputs: origin, entry/intake date, defer signal, trusted vaccination history |

## Backend Design Shape

Keep procurement/source entry modular. Do not overload vaccination or parks.

Recommended module:

```text
backend/internal/procurement
  domain/
  ports/
  app/
  adapters/postgres/
  adapters/http/
```

Possible durable tables, final names subject to implementation review:

```text
procurement_loads
  tenant_id, load_id, source_party_id, source_location_id, expected_count,
  purchase_date, planned_dispatch_at, status

procurement_load_goats
  tenant_id, load_id, goat_id, source_tag/temporary_id, selection_state,
  selection_reason, warmup_started_at, warmup_ended_at

source_holding_stays
  tenant_id, stay_id, goat_id, load_id, holding_location_id, started_at,
  ended_at, warmup_state, health_state, ownership_state

source_entry_decisions
  tenant_id, decision_id, goat_id, load_id, decision_type
  accepted | rejected | deferred | blocked, reason, decided_by, decided_at,
  proof_ref_id, sop_task_id

transit_handoffs
  tenant_id, handoff_id, load_id, from_location_id, to_location_id,
  loaded_count, dispatched_at, arrived_at, proof_ref_id, discrepancy_state

arrival_intake_reviews
  tenant_id, review_id, load_id, park_location_id, counted, matched, rejected,
  missing, health_flags, weight_flags, media_proof_id, status
```

Use existing shared engines where appropriate:

```text
SOP engine:
  source health check, tagging check, pre-dispatch decision, arrival gate.

Proof/media:
  source health proof, truck loading proof, handoff proof, arrival proof.

Obligation/process-integrity:
  source-entry work queue and gap visibility when this slice is approved.

Goat identity:
  source temporary ID / RFID / old tag matching must flow through identity
  review, not direct duplicate canonical goat creation.
```

## State Model

Minimum goat/load states to support:

```text
source_holding
source_warmup
source_candidate
source_health_pending
source_health_passed
source_health_failed
source_rejected
pre_dispatch_pending
pre_dispatch_accepted
pre_dispatch_rejected
pre_dispatch_deferred
in_transit
arrival_review_pending
arrival_accepted
arrival_rejected
accepted_herd_intake
```

Rejection before truck loading is a first-class terminal/branch state for the
procurement slice. It must not silently create PHC vaccination due work.

## Vaccination Boundary

Vaccination may consume procurement/source-entry outputs:

```text
origin_type = procured
entry_date / accepted_herd_intake date
trusted historical vaccination evidence
intake health/defer signal
park_id / shed_id after accepted intake
```

Vaccination must not own:

```text
vendor/source economics
holding-farm stay lifecycle
pre-dispatch rejection
truck/transit proof
arrival discrepancy reconciliation
```

When a procured goat is rejected before dispatch, vaccination should see no
active obligation. If obligations were already created from a provisional row,
the procurement decision must cancel/rescope them through the obligation engine.

Historical vaccination-at-procurement is evidence until reviewed. Do not create
an accepted vaccination completion from source-side history unless the PHC
backfill/review path accepts it under the vaccination contract.

Accepted intake is the only handoff that can make a procured goat eligible for
post-arrival PHC vaccination generation. That handoff should be idempotent and
auditable so retries cannot duplicate obligations.

## APIs

Add only when building this slice:

```text
GET  /procurement/source-entry/loads
POST /procurement/source-entry/loads
GET  /procurement/source-entry/loads/{load_id}
POST /procurement/source-entry/loads/{load_id}/goats
POST /procurement/source-entry/goats/{goat_id}/source-health
POST /procurement/source-entry/goats/{goat_id}/pre-dispatch-decision
POST /procurement/source-entry/loads/{load_id}/dispatch
POST /procurement/source-entry/loads/{load_id}/arrival-review
POST /procurement/source-entry/loads/{load_id}/accept-intake
```

Do not create frontend-only mock endpoints. Contracts must be OpenAPI-backed and
generated for admin-web/mobile.

Minimum backend tests before exposing the contracts:

```text
source warmup:
  purpose-specific warmup remains valid and visible:
    breeding 45-70 days
    fattening/non-breeding 0 days or around 2 weeks

pre-dispatch:
  accept, reject-before-truck, defer, block-missing-proof, and identity-conflict
  branches are durable and audited.

loading/transit:
  accepted-but-not-loaded, partial load, proof-required dispatch, death/sold/lost
  during holding/transit, and retry/idempotency paths are covered.

arrival:
  count mismatch, missing goat, unknown/extra goat, health/weight issue,
  arrival accepted, and arrival rejected branches are covered.

handoff:
  accepted intake creates the PHC/vaccination handoff once.
  rejected-before-truck and unresolved arrival rows create no active PHC/Parks
  vaccination work.
```

## Scale Rules

Procurement imports and load reviews can touch many goats. Every query must be:

```text
tenant scoped
load scoped where possible
indexed by tenant_id + load_id/status/goat_id
paginated or chunked
idempotent for retries
bounded in memory
covered by query-plan validation on hot paths
```

No full-herd scans on dashboard or source-entry page load.

## E2E Seed Needed

The authoritative E2E plan is:

```text
context/execution/procurement-vaccination-e2e-plan.md
```

That plan intentionally starts with a small deterministic fixture before any
broad CRUD. It proves the important business rule: the journey starts at
purchase/source, but PHC vaccination work starts from accepted herd/intake
truth.

## Acceptance

Do not call this backend slice done until:

```text
source warmup supports purpose-specific durations without data loss
pre-dispatch rejection is durable and auditable
rejected-before-truck goats do not appear as active park/vaccination work
accepted-intake goats can trigger post-arrival vaccination obligations
proof records exist for source health, dispatch, and arrival where configured
query-plan validation covers list/read-model paths
OpenAPI contracts and generated clients are updated
backend tests cover accepted, rejected, deferred, and discrepancy branches
```
