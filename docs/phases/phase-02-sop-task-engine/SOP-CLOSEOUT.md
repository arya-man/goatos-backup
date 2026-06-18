# Phase 2 SOP Closeout Cross-Check

Status: active cross-check for review; not an implementation-complete status.

This file answers one question:

```text
What is still missing before Goat OS can say SOP replacement is complete enough
to retire Slack execution and, later, retire BigQuery/Sheets as dashboard inputs?
```

Phase 2 does not implement every domain SOP. Phase 2 must build the common
builder, Android runner, task/proof/verification loop, and canonical submission
path so each legacy SOP can migrate without a new one-off system.

This file deliberately separates three gates:

```text
Phase 2 platform acceptance
  Shifting proves the reusable builder/runner/backend loop.

Legacy SOP execution retirement
  Every active legacy execution path is inventoried, migrated, retired, or
  bridged through Goat OS without direct canonical writes.

BQ/Sheets dashboard input retirement
  Each dashboard section/grain has canonical coverage, dedup, and shadow parity.
```

Passing one gate does not imply the others. In particular, Android becoming the
canonical intake path for a migrated SOP family does not automatically make
Counts, Locations, or Mortality canonical-only.

## End-State Data Path

Final operating path:

```text
Admin SOP Builder
  -> published SOP version
  -> Android operator task/runner
  -> Goat OS app API
  -> backend validation, idempotency, proof, audit
  -> module-owned canonical command/event
  -> operational Postgres projections
  -> operational product dashboards and notifications
```

Governed/leadership analytics KPIs and AI read the Cube metric layer, not raw
Postgres. Operational product dashboards read Postgres projections only when the
KPI is dual-served and covered by the parity gate in
`docs/decisions/high-scale-dashboard-projections.md`, per
`context/analytics/final-analytics-infra.md`.

Temporary migration path:

```text
Legacy Slack/App Script/Sheets/BQ
  -> backend-owned sync or bridge
  -> source evidence/review/parity
  -> same canonical or projection contracts where coverage permits
```

Never:

```text
frontend/mobile -> BigQuery, Sheets, Slack, Drive, Firestore, or raw DB
frontend/mobile -> storage without a backend-issued upload intent
Slack/App Script -> canonical Goat OS tables directly
```

## Source Evidence Reviewed

Repo-owned source summaries already cover:

- Shifting
- Death report
- Birth / abortion
- Health diagnosis / follow-up
- Not Eating as a health-related form seed
- Feed timing and proof implications
- Procurement / arrival implications
- Cross-cutting video/proof verification behavior

Local source-material SOP visualization was reviewed, but the raw playground
HTML is not committed as product truth. The useful sanitized intent is:
catalog-driven SOP selection, custom SOP draft creation, field palette, field
editor, rule builder, workflow pattern/canvas, Android preview, scenario
simulator, proof state, validate, and publish.

## Review Counterpoints

Keep these constraints visible during implementation and closeout:

- Shifting is the walking skeleton, not proof that the full Slack/App Script SOP
  universe has moved.
- Android is canonical intake only for SOP families whose app package, generated
  client, offline queue, media upload, backend validation, and module command
  path are implemented and accepted.
- Android SOP intake also requires Operator Management: active operator profile,
  verified login, role/scope grant, capability, device/session state, app
  bootstrap, and dynamic task/SOP visibility gates.
- Generic form submissions are never canonical by themselves; the owning domain
  module must apply the accepted command/event.
- BQ/Sheets removal is per feature section/grain. A tenant-wide
  `canonical_only` flag is too coarse for Counts, Locations, or Mortality.
- Legacy and Android overlap windows require source-independent dedup keys and
  reconciliation review, especially for death and count verification facts.
- Source inventory is a closure requirement. Unknown Slack/App Script/Sheet
  flows should be classified before any "SOP replacement complete" claim.

## Current Cross-Feature Dependencies

| Feature | Already captured | Missing before BQ/Sheets removal |
| --- | --- | --- |
| Locations | Canonical location tree, aliases, capacity, review, usage checks, projection invalidation. | Android location option sources/offline cache must use Locations APIs; Shifting must write canonical movement/current-location events; source-label review must cover SOP-submitted labels, not only legacy BQ labels. |
| Counts | Projection contract, blend mode, coverage registry, dashboard parity, current active goats, farm/shed/status/breed/age/gender sections. | Daily count verification SOP, Shifting/location events, lifecycle changes, status/stage transitions, weight capture or weight policy, valuation facts, sale/inactive events, and per-section shadow parity from canonical facts. |
| Mortality | Event-based mortality semantics, `mortality_events`, Death SOP cutover model, proof/correction/idempotency, formulas pending. | Death SOP implementation, post-mortem checklist policy, abortion/birth/litter event ownership, denominator projections, event-source coverage gate, source-independent death keys, and canonical-vs-legacy shadow parity. |
| SOP/task engine | Shifting walking skeleton, DSL rules, proof policy, task states, Android runner shape, backend revalidation. | Complete form DSL/schema/evaluator, admin builder APIs/UI, Android app package/client/offline queue, media upload intents, workflow state machine, source inventory, reusable template seeds, and domain commands for each canonical effect. |
| Operator Management | Phase 1 auth/RBAC grants and final architecture workforce rules exist. | Active roster, legacy submitter mapping, capabilities, scopes, shifts/absence/backfill, devices, app bootstrap manifest, dynamic Android feature visibility, and source-candidate review. |
| Analytics/cutover | Shared cutover contract with source composition and coverage registry. | Per-SOP coverage rows, shadow parity artifact generation, cross-source dedup tests, and projection invalidation hooks from accepted SOP submissions. |

## SOP Families Needed For Full Closure

These are the minimum SOP families implied by current docs before the old
Slack/Scripts/Sheets/BQ operating loop can fully go away. They are not all Phase
2 implementation blockers unless a phase PRD/TRD explicitly promotes them into
scope.

| SOP family | Purpose | Current status | Missing product decisions |
| --- | --- | --- | --- |
| Shifting / movement | Move goats across park/shed/partition and update current location truth. | Phase 2 seed SOP. | Exact approval scope, proof scope, blocking goat states, destination-count tolerance. |
| Count verification | Replace daily aggregate count inputs and prove shed/farm/headcount snapshots. | Required by Counts cutover; not yet a Phase 2 seed. | Cutoff time, who counts, variance tolerance, proof requirement, recount/rework path. |
| Weight capture | Replace or justify weight/value KPI inputs. | Not yet in SOP catalog. | Which goats are weighed when, trusted weight policy, proof requirement, unit/value policy owner. |
| Status/stage transition | Replace legacy `shed_tag`/stage changes used by Counts. | Partly inferred from movement and health docs. | Owner for K/F/adult stage changes, adult/kid cutoff policy, review gates. |
| Death report | Feed mortality events and lifecycle terminal state. | Captured as legacy schema/template. | Exact proof media type, post-mortem checklist, verifier roles, void/reversal policy. |
| Birth / abortion | Create kid/mother/litter facts and future mortality denominator inputs. | Captured as legacy schema/template. | Kid identity creation timing, colostrum schedule ownership, litter denominator formulas. |
| Health diagnosis/follow-up | Diagnosis, problem, treatment, ICU/quarantine follow-up, movement blocks. | Captured as Phase 3 scope. | Disease taxonomy, treatment template ownership, close/extend policy, withdrawal rules. |
| Vaccination | Scheduled vaccination history, batch/medicine proof, adverse reaction flow. | Named as next SOP after Shifting. | Vaccine catalog, schedule rules, dose/batch validation, missed-dose escalation. |
| Feed report | Feed packing/distribution proof, stock use, variance review. | Source findings captured. | Inventory owner, feed catalog, variance thresholds, stock decrement policy. |
| Video/proof verification | Shared proof review, rejection, rectification, verifier evidence. | Cross-cutting proof engine captured. | Retention period, AI pre-check scope, verifier SLAs, rectified-proof requirements. |
| Procurement / arrival | Load intake, source/vendor evidence, arrival count/health/proof gate. | Later phase implication captured. | Load identity, arrival discrepancy policy, ownership/cost handoff, intake health SOP. |
| Sale/exit/inactive | Remove goats from active counts and prevent promise/dashboard drift. | Needed by Counts/promise safety, not yet SOP-seeded. | Sale/dispatch proof, inactive reasons, replacement/substitution and allocation handoff. |

## Missing Technical Building Blocks

To close the reusable SOP platform:

- JSON Schema for SOP version, workflow nodes/edges, rule expressions, proof
  policy, option sources, submission, per-goat item result, and dry-run output.
- Shared deterministic evaluator used by admin preview, Android, and backend.
- Backend dry-run endpoint for draft validation and preview scenarios.
- Admin builder route with catalog, custom draft, field editor, rule builder,
  workflow pattern/canvas, Android preview, validation errors, publish/retire.
- Android app package path, generated app-api client, task list, runner,
  offline cache, draft storage, sync queue, and per-goat retry.
- Operator Management v1: active operator profile, login, role/scope grants,
  capabilities, source-submitter mapping, device registration/revocation, app
  bootstrap manifest, and Android feature/SOP compatibility gates.
- Media upload intent flow for photo, video, generic attachments, original
  proof, rectified proof, verifier proof, hash, metadata, retention, and DLQ.
- Task state machine with assignment, approval, verification, rework, missed,
  voided, and carry-forward states.
- Workforce hooks for operator capability, park/shed scope, absence/backfill,
  current load, and escalation.
- Domain command adapters for movement, lifecycle, health, vaccination, feed,
  weight, count verification, procurement, sale/exit, and proof verification.
- Coverage registry writes and shadow parity artifact generation for each
  dashboard section that wants to stop using legacy sources.
- Projection invalidation/rebuild hooks from accepted canonical SOP events.
- Query-plan/load validation for task queues, assigned tasks, submissions,
  media/proof queues, review queues, and projection invalidation at one million
  goats.

## Missing Source Inventory Work

Before declaring the legacy SOP universe closed:

- Inventory every Slack workflow, App Script, Val.town script, Sheet form, and
  proof verification script.
- For each legacy flow, record fields, options, validation, due rules,
  assignment logic, proof requirement, notification rules, correction behavior,
  canonical domain effect, and downstream dashboard dependency.
- Classify each legacy flow as:
  `seed in Phase 2`, `Phase 3 health/lifecycle`, `later module`,
  `notification-only`, `retire`, or `external integration`.
- Produce sanitized fixtures or schema summaries for flows not already captured.
- Keep raw private rows, media URLs, contacts, and PII out of the repo.

## Closeout Acceptance

Phase 2 platform acceptance is limited to proving the reusable engine with
Shifting:

1. Admin can build, preview, validate, publish, retire, and version SOPs from
   Goat OS admin web.
2. Android can execute assigned Shifting tasks online/offline with proof upload
   and idempotent retry.
3. Android bootstrap is gated by active Operator Management profile, grant,
   capability, device/session, compatible app version, and pinned SOP version.
4. Backend revalidates every submission against pinned SOP version, RBAC, live
   goat/location state, proof policy, idempotency, and domain gates.
5. Accepted Shifting submissions write module-owned canonical movement/location
   events, not generic form-table mutations.
6. Proof, verification, rejection, rework, void/reversal, audit, and outbox are
   implemented as shared platform behavior for the walking skeleton.
7. `SOP-CLOSEOUT.md` classifies the non-Phase-2 SOP families and cutover gates
   so Phase 2 acceptance cannot be mistaken for full legacy retirement.

Legacy SOP execution can be called closed only when:

1. Every active Slack workflow, App Script, Val.town script, Sheet form, and
   proof verification script is inventoried and classified.
2. Shifting, Death Report, Count Verification, one scheduled SOP, and every
   active flow that writes count/location/mortality/promise facts either run
   through the shared Goat OS engine or have an approved temporary backend-owned
   bridge with an expiry/rollback policy.
3. Accepted SOP submissions write module-owned canonical events, not generic
   form-table mutations.
4. Proof, verification, rejection, rework, void/reversal, audit, and outbox are
   shared platform behavior.
5. Slack/App Script are frozen as reference/notification or temporary bridge
   only; they no longer write canonical operational truth.
6. One-million-goat scale gates are green: bounded API reads, indexed queues,
   chunked rebuilds, media jobs off request path, idempotent retries, and
   observability for API latency, DB pressure, queue lag, DLQ, and media
   failures.

BQ/Sheets dashboard inputs can be removed only when:

1. Counts, Locations, and Mortality have explicit canonical coverage/dependency
   rows for every section/grain they need before removal.
2. Shadow parity artifacts show no unexplained deltas for any section promoted
   to canonical.
3. Cross-source dedup tests cover overlap windows between legacy sources and
   Android/backend canonical facts.
4. The feature coverage registry records source versions, parity artifact path,
   approving actor/job, rollback policy, and expiry policy.
