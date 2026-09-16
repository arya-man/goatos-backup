# Current source refresh for configuration clarity

2026-09-16. Successfully fetched `origin main` read-only in `/Users/raviteja/mesha/goatos`; inspected Git objects rather than its dirty working tree. **Current origin/main: aa057776567c44cb8941974d723aa6ee1efd62b9**, commit time19:05:42+05:30. Earlier architecture reports are pinned to397114d1d06baddb50dffc7d2c2f9df1d0497b7b. This supplement identifies material changes, preserving those reports as historical evidence. Fetch changes remote references only; no checkout, source edit, merge or deployment. Parent owns new real-staging queries; source migration presence is not live application proof.

## Material new configuration family: Alerts

Current main has a real Alerts page after Work Board and an authorized Configure drawer. The former35-leaf visual inventory is no longer a complete inventory of current source navigation. `backend/internal/adminui/app/service.go:133,267,439`; `apps/admin-web/features/alerts/alerts-configure.tsx:13–20,45–56`.

Two different persisted configuration concepts exist:

| Concept | Current contract | CEO-facing implication |
|---|---|---|
| Known rule settings | `alert_rule_config`, tenant/rule key, enabled and integer threshold. Missing rows mean enabled catalog defaults. Rule meaning/unit/allowed threshold range remain code-defined. | “Change when this alert appears” is accurate; users cannot invent an arbitrary detector by typing a new key. |
| Composed event alerts | `alert_event_rules`, tenant-scoped ID, label, known event kind, severity and enabled. No seed; farm adds desired event alerts. | “Tell me when this event happens” is distinct from “start another workflow.” |

Exact schema: `backend/migrations/postgres/000320_alert_rule_config.sql:1–44`; `000322_alert_event_rules.sql:1–39`. Known rule catalog and defaults: `backend/internal/alerts/domain/rules.go:14–86` — pen feed not following head count, default minimum change1animal; feed stock running out, default5days. Stock reads current balance and is today-only. A previous-day label cannot turn it into an historical stock snapshot.

Known event kinds: birth recorded, death recorded, animal sold, animal added, shifting raised/approved and feed purchase recorded. `backend/internal/alerts/domain/events.go:17–48`. Event rule validation rejects unknown kinds and invalid labels/severity (`:91–105`). This is new persisted event-alert composition, **not evidence of an arbitrary module-to-module task dispatch registry**. Transit-start is not in this inspected alert catalog.

Configure is governed by per-person HRMS capabilities, not a universal CEO-title check. Migration321 follows the existing person access cutover and backfills relevant Alerts capabilities while avoiding partially migrating untouched people. `000321_alerts_person_access.sql:7–24`. The drawer is mounted only when its configure control is enabled; writes use existing APIs/server actions. Local mock role selection remains illustrative and cannot define production authorization.

Relevant new commits:78e8f7eb6 (Alerts page/HRMS config), ca9340c76 (event composition and feed/head-count semantics),9678b2d3d (versioned writes/person capability),59bc0debf (nonce-keyed writes and today-only stock),1dca982ef (bounded concurrent reads),1a914d176 (bounded event preview/live stock checks).

## Material workflow/sync change: per-recording birth review

Birth proof steps now enter review independently and sequencing recognizes recorded steps, including an in-review or rework predecessor, for birth templates. Death retains its bundled ordering. Do not describe all workflows as universally waiting for verifier approval of every previous activity.

`backend/internal/tasks/domain/types.go:618–645` defines ReviewedPerStep, StepRecorded and HoldStepForReview; `:648` onward derives per-recording verification identity. The inspected delta also adds state-derived replay/enqueue healing and guards stale recording verdicts. Android mirrors template-specific sequencing in `apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/WorkflowsRepository.kt:487–512`: rework counts as recorded for birth sequencing, while backend hard blocks remain authoritative.

Relevant commits:0b68d87e6 (per-clip review),252de20ff (recording fencing/proof kinds),43455b185 (recording identity/dependent deadlines),c601ab16e (in-review colostrum semantics). The transit example may still explicitly require its preparation activities complete; these changes caution against presenting its rule as a universal definition of completion.

## Confirmed unchanged mappings

Compared the old snapshot to current main: no diff in `backend/internal/feedconfig`, `backend/internal/inventory`, weighing reporting constants (`backend/internal/weighing/domain/shed_weights.go`), sales valuation repository (`backend/internal/sales/adapters/postgres/overview_repository.go`), `backend/internal/appconfig`, or Android core-data sync directory. Thus the earlier distinctions remain current source evidence:

- Shared inventory/feed identities are not interchangeable with typed policy values.
- Ration/schedules keep dimensional/effective-date semantics; null is not zero.
- Reporting30/35kg and valuation assumptions remain separate hardcoded-source concepts requiring a proposed persistence migration before claiming live editability.
- UI presentation configuration/mobile runtime config is not a general business-policy store.
- Existing published SOP/task pins and durable outbox contracts remain relevant.

The SOP postgres repository delta only adds measured task-sweeper telemetry at this refresh; it does not establish a new generic category/module registry. Weighing calendar remains the previously documented direct-DB configuration rather than an SOP-authoring setting.

## New analytics migrations and deployment boundary

Current source includes migrations320–326: Alerts configuration/access/event rules, analytics rollup event time, crash daily, network daily and rollup dispatch. Latest main specifically renumbers analytics migrations after concurrent main additions. These telemetry changes do not establish a user-authored generic farm analytics product or resolve V05's unspecified metrics. Do not infer that the real staging database or installed Android build has these changes from source alone.

## Last-month review lens and remaining evidence

Re-examined relevant source history since2026-08-16 across admin SOP/feed interfaces, backend SOP/tasks/feed configuration and Android sync, plus changes since the previous audit. Earlier month patterns remain: authored procurement forms and weighing capture slots, direct-DB calendar replacing SOP page settings, published-versus-draft baseline fixes, durable workflow consumers and outbox reconciliation. New alert readers preserve authorization and bounded loading; no runtime latency or failure-screen certification was performed here.

For the clarity redesign: use concrete task names for catalogue items, reusable values, procedures, workflow dependencies and alert rules, while keeping source ownership/lifecycle distinctions visible where they matter. Do not imply that one authoring interface means all data shares one generic table, that a module link grants execution permission, or that an alert creates work. All11 voice-note requirements and the transit screenshot example remain applicable. The final judge must assess the implemented navigation and flows separately; this report is research, not UI approval.

## Explicit clarification: master SOP composed from smaller SOPs

The user's later clarification is authoritative: a master procedure can stitch smaller procedures (example Animal procurement → Transit → Warm-up) with prerequisites at steps, generically for any department. This supersedes the proposed standalone event-automation direction. The screenshot transit example should be represented inside that composition, not treated as a request for a separate event-rule product.

Additional current-source inspection found existing **step dependencies**, not an established arbitrary child-SOP invocation contract:

- `backend/internal/tasks/domain/sop_followup.go:84–117`: follow_up contains tracks and typed steps with task_type, proof, schedule, hard_time_gate, wait_for_all and requires. `:125–144` supports after-step/timing/series. Engine behavior is bound to registered task types/keys (`:29–33,58–68`), not arbitrary text.
- `apps/admin-web/features/sops/followup-model.ts:17–56`: matching authored step/track model, including afterStep/requires. No child SOP/version field in these types.
- Android `core-data/.../forms/FormSpec.kt:19–50,62–88`: fields and conditional form rules; no nested-SOP renderer among known types. Unknown fields fall back safely, which is not child execution support.
- `backend/internal/sop/app/service.go:1487–1499` validates a SOP reference when creating a task; this must not be mistaken for a nested call from one SOP step.
- Bounded Git search for sub-SOP/child-SOP/nested-SOP/subworkflow references across these relevant modules found no applicable composition node. This is not a claim about every external system or uninspected JSON document.

### Bounded mock proposal

Keep composition in the existing SOP builder. A “Follow another SOP” step should select a real saved procedure, carry a stable identity and pinned version/snapshot, show its department, and expose prerequisites in ordinary step language. A master may sequence those steps and gate a later step on prior started/completed milestones as explicitly chosen. Procurement/transit/warm-up are one optional example, not seeded into every new master.

Represent nesting as a proposed local definition extension. Do not relabel an unstructured Instruction with a typed SOP name and claim a connection exists. Validate missing/archived/unpublished targets and cycles, preserve previously published parent/child snapshots after later edits, and distinguish testing acknowledgement from actual execution. The exact choice of supporting only published children or permitting explicitly marked drafts is a design decision; production should resolve a stable published version at publication/open rather than follow a mutable title.

A production implementation needs a validated nested-reference schema, compiler/execution mapping, cross-procedure subject and actor scope, dependency/cancellation semantics, version pinning and Android capability support. Existing requires/after_step primitives can inform it but do not establish those contracts automatically. No new event-automation database or dispatch system should be inferred from the clarification.
