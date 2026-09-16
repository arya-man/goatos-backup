# Android production configuration and SOP architecture

## Evidence boundary

Read-only inspection on 2026-09-16 of Git object `origin/main` = **397114d1d06baddb50dffc7d2c2f9df1d0497b7b** in `/Users/raviteja/mesha/goatos`. The working checkout is dirty and was not changed. All code citations below refer to that commit, not potentially modified checkout files. No staging data, installed APK, or runtime configuration values were read; presence in source does not establish deployment. Reviewed relevant Android commit history since 2026-08-16 and applicable repository instructions. No nested Android AGENTS file was found.

Path abbreviations for precise citations:

- `DTO/` = `apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/`
- `DATA/` = `apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/`
- `VM/` = `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/`

## Main finding

Production is substantially more developed than a shared item dropdown and an isolated workflow simulator. Android already consumes backend-owned, versioned SOP definitions, conditional form rules, dynamically resolved stock choices, task-specific proof policy, business capability flags, backend-computed read models and durable offline writes. It does **not** define a single universal “Common → Category → Subcategory → Item” schema in the inspected contracts. The mock's internal `Common` owner and consumer checkbox rules are a proposed UI mechanism, not a verified production model.

The correct discovery unit is **a typed configuration/rule, its scope and version, its consumers, and its execution effect**. A medicine, a diagnosis threshold, a clock cutoff, a video requirement and an animal eligibility rule are not interchangeable inventory records.

## Existing generic execution foundation

| Concern | Existing contract / behavior | Evidence |
|---|---|---|
| Versioned SOP | Task detail carries `sop_version_id`, SOP ID, version, status, `form_dsl`, `proof_policy`; submissions carry SOP version, answers, proof references, idempotency key, validation report and row version. | `DTO/TaskDto.kt:127–169` |
| Field types | Boolean, number, text, select, datetime, RFID/goat scan, vaccine batch, location and media renderers; repeat fields, inline options, dynamic option source, disabled reason and proof subject. | `DATA/forms/FormSpec.kt:20–78` |
| Conditional rules | `visible_if`, `required_if`, `enabled_if`, `proof_required_if`, `block_submission_if`, `requires_supervisor_if`; condition field/operator/value/message. Enum/parser support is evidence of representation, not proof that every renderer enforces every rule. | `DATA/forms/FormSpec.kt:39–88` |
| Real option sources | Choices can be disabled with a reason, available quantity, unit, expiry date, FEFO rank. These are context-sensitive backend choices, not simply all active shared catalogue records. | `DTO/TaskDto.kt:174–194` |
| Proof configuration | Media types, required flag, proof mode, subject scope, expected subjects, total/per-subject minima and maxima, capture source and allowed sources; per-field cap is a separate client model property. | `DATA/forms/ProofPolicy.kt:24–56,86–114` |
| Proof fallback | Missing policy defaults field-by-field. Shared caps are deliberately uncapped (`maximumCount`/per-subject max null); business limits must come from explicit backend policy or feature caller. Parser shown does not populate `maximumCountPerField`, although the data class supports it. | `DATA/forms/ProofPolicy.kt:12–17,32–36,52,98–114` |

Do not equate a field's supported wire type with a new configurable engine feature. Several modules have dedicated DTOs and execution paths outside this generic form DSL.

## Business rule/configuration inventory

**Classification:** “server-served” means the phone receives it; persistence/config-authoring must be confirmed in backend code. “Computed” means backend output, not a editable master setting. “Fallback” is the DTO/client default when omitted, not the current farm value.

| Domain | Meaningful inputs, rules and effects beyond sale eligibility/valuation | Classification and evidence |
|---|---|---|
| Weighing planning | Allowed individual/per-pen modes; daily planned cap; feed/water removal required/optional/off; effective evening cutoff; instructions; arbitrary authored proof slots and questions; lump-sum video min/max. Planner uses published version; existing task uses pinned version. | Server-served versioned rules: `DATA/weighing/WeighingSopRules.kt:6–36,64–75`; `DTO/WeighingDto.kt:277–361,432–446`. |
| Weighing defaults and residual caps | Older-server fallback cap 100, both modes, required removal, 1–5 videos. Actual phone clamps configured lump-sum maximum to 5 and uses 100 maximum proofs for free-flow scope. These are real residual client limits, not proof of global configurable values. | Fallback: `DATA/weighing/WeighingSopRules.kt:43–58`. Client constraints: `VM/WeighingViewModel.kt:2540,3447–3448,3910,3922`; planner fallback `VM/WeighingPlanWizardViewModel.kt:649,869`. |
| Procurement purchase inspection | Versioned load form and per-animal questionnaire; section, choice, multi-choice, text, number, media; requiredness, “other”, slot/max files/accepted kinds, numeric min/max/unit, conditional visibility; species/sex/condition/farm choices and breed suggestions. | Server-served authored forms: `DTO/AnimalPurchaseDto.kt:26–75`. Animal decision and totals are read models; `:78–100`. |
| Generic herd follow-up workflows | Ordered question/select/action/approval steps; status pending/in-review/completed/rework/canceled; previous-sibling gate; due date, answer type, proof minima per media kind, task type, rejection reason. Backend controls labels, progress and next action. | Server-served execution + computed cards: `DTO/WorkflowDto.kt:43–75,99–134`. Birth/death endpoint scope and outbox writes `:6–19`. |
| Health diagnosis | Complete observation schema, multiselect findings, sex/age-specific hidden fields, adult/kid distinction; server reads species/sex/age/status and existing courses. Ranked proposals, emergency immediate actions, covered problems, rechecks, field actions, unexplained findings, close/extend proposals, tiers, SOP/course mapping, contraindication flags. Treatment opens only after director confirmation, except immediate actions. | Typed submission + computed deterministic proposal: `DTO/HealthDiagnosisDtos.kt:6–19,32–104,130–159`. Null findings are draft-capable, not permission for incomplete submission. |
| Health treatment | Disease + age-band course, day/session/duration, ordered medication/instruction/critical steps, medicine name, dosage text/denominator, administration route; completion/close capabilities; exact diagnosis register identity is distinct from treatment card identity. | Server-served treatment: `DTO/HealthDtos.kt:8–28,66–95,99–136`. Duration 3 is DTO fallback (`:17,78,108`), not universal clinical policy. |
| Health housing relationship | Acuity/containment/low competition/walks/no-due-overnight are directives. Health does not write location or move animals; the location-owning workflow does. | Computed directive boundary: `DTO/HealthDiagnosisDtos.kt:113–127`. |
| Feed direction / packing | Item quantity, grams/head, shed factor, blocked reason; park/shed/session vocabulary; issued/amended/locked lifecycle; dispatch-time gate; whole-scope totals and blocked-cell counts. Null blocked ration is never zero. | Computed from backend configuration: `DTO/FeedDto.kt:11–30,47–89,107–127`. Dispatch times 07:00 normal / 14:00 experiment occur in contract commentary, not confirmed live configuration. |
| Feed verification | Packing readings more than 500g from plan trigger a server warning requiring explicit confirmation. Phone records confirmation, not an independent tolerance calculator. | `VM/VerifyDetailViewModel.kt:76–83,466`; relevant history `b88b20e5e`. Numeric limit declaration 10,000kg exists at `:101`, but usage not established in this pass—do not present declaration alone as active enforcement. |
| Milk preparation | Management-stage head count, per-head ml, session count, required ml, farm total, acid grams/litre and computed acid grams; verification/rework attempt. | Mixed served parameters/computed outputs: `DTO/MilkPreparationDto.kt:6–28`. 5.5 grams/litre is DTO fallback at `:23`, not verified persisted setting. |
| Preventive care | Deworming/ticks/hoof/hair categories; category-specific expected proof slots, backend capture mode (scan versus roster), verifier gate, assignees, dates, inventory dose requirements and batch identities. Duration hint is guidance, not enforced video duration. | `DTO/PcCareDto.kt:6–25,29–71`. |
| Preventive care → removal | Feed-administered deworming can create linked evening-before feed/water-removal task in the same write; removal operators required; unsupported categories refused with 422. | Explicit cross-module transaction semantics: `DTO/PcCareDto.kt:360–377`. This relationship already exists; it should not be reimplemented as a browser event approximation. |
| PC care date choices | Planner UI 14-day future window; worklist seven-day future window; planner checks today and configured constants. | Hardcoded client windows: `VM/PcCarePlanViewModel.kt:379,507,895–896`; `VM/PcCareWorklistViewModel.kt:153,162–163`. Distinguish selection/navigation windows from clinical schedules. |
| Toxin → purchased feed | One guided test per purchased feed load; ordered video/wait/photo+reading steps; server-clock settling gate; later test rounds and review outcomes; execution capabilities. | Server execution: `DTO/ToxinDto.kt:6–35,40–77`. Wait minutes are served; local countdown does not authorize a write. |
| Sales | Farm/product-type choices, product-type→breed mapping, status/tone/default status, maximum sale date horizon; selectable buyers reuse procurement vendor identity. Multiple deal lines capped at 20 in phone. | `DTO/SalesDto.kt:121–149`; horizon 60 is DTO fallback `:138`; actual client cap `VM/SalesViewModels.kt:642,687,886`. |
| Procurement vendors | Active/retired vocabularies for record type, breeds, states, cities, statuses, feeds, capacity unit, supply frequency. Retired value remains visible on existing record but is unavailable for new records. | Server-managed catalogue: `DTO/VendorsDto.kt:73–91`. |
| Vaccination / task completion | Per-shed expected/handled/proof-ready counts, proof mode, vaccine breakdown, submit enabled/blocking reason, current round submission/fingerprint; stock sources carry expiry/FEFO. | Computed execution gates: `DTO/TaskDto.kt:107–124,174–194`. Detailed vaccine dose/age/safe-gap scheduling values are backend-owned and not enumerated in these Android DTOs. |
| Counts / reconciliation | Actual scan identifier is separate from display ID; registered versus found shed/partition; pending/completed/verified state and proof; birth registration can create child records before approval controls count eligibility. | `DTO/CountsDto.kt:644,742–760,806–823`. These are event/approval semantics, not generic item hierarchy. |
| App runtime | Feature flags, revision/policy revision, page-size default, sync base/max backoff, refresh cadence, cache TTL, jank sampling, cache policy/ETag. | Server contract `DTO/AppConfigDto.kt:6–36`; values 50/1000/60000/300/3600/0.1 are decode defaults. This pass did not prove every field is wired into runtime consumers. |
| People/task UX limits | Leadership title 80, body 4000, attachment 50MiB, voice recording 10min; user content and transport/UI limits must not be mixed with animal/business policy. | Client constants `VM/LeadershipTaskComposeViewModel.kt:480–486`; call-site enforcement not exhaustively traced. Leave approvals expose park-head/HR-required flags (`DTO/LeaveDto.kt:51–52`). |

## Category, subcategory, item type and inheritance implications

1. Production uses **multiple typed taxonomies**, not a verified universal category tree: PC-care operation category; procurement species/condition and form type; sales product type→breed; health disease/age band/course type; feed item and management stage; vendor record type. Their identifiers, units and behavior differ.
2. Inventory-like records are only one kind of reusable source. Existing task choices also depend on stock availability, expiry, FEFO, disable reasons and task scope (`DTO/TaskDto.kt:174–194`). A generic item registry must not bypass those conditions.
3. Verified inheritance is **definition-to-execution**: task-pinned SOP versions and per-feature DTO fallbacks. Field media subject can fall back to SOP proof policy (`DATA/forms/FormSpec.kt:76–78`); absent proof fields use explicit policy defaults. These mechanisms are not the same as category-level grants.
4. No evidence from the inspected Android contracts proves the mock's whole-category consumer grant inheritance is a production requirement. Likewise, “Common” is not established here as a production business owner.
5. Configurable numeric/boolean/time/enum thresholds should remain typed rules with units, scope, validation, revision and consumers. Representing a ₹/kg rate or a kg threshold as an untyped item name would lose executable semantics.

## Sync and authorization boundaries the mock must represent honestly

- Backend response → Room cache → observed UI; edits enter durable Room outbox, not only in-memory state. Enqueue, reconnect and WorkManager all trigger the same drain. In-flight rows are reclaimed after process death (`DATA/sync/SyncEngine.kt:102–119`).
- Ordering is per group key (for example shed), chronological within group, bounded concurrency across groups (`DATA/sync/SyncEngine.kt:121–125`). Do not replace this with unconstrained cross-module callbacks.
- Terminal validation/non-retryable 4xx is surfaced as conflict rather than blindly retried; stored successful responses reconcile back into feature cache before local overlays retract (`DATA/sync/SyncEngine.kt:63–94,199–210,414–438`).
- Bootstrap 401/403 never silently falls back to cached shell; connectivity failures may (`DATA/DefaultBootstrapRepository.kt:38–58`). Catalog consumer visibility is not role authorization.
- Backend capabilities govern feature actions: health `can_complete/can_close_case`, market `can_record`, toxin `can_execute` fail safe (`DTO/HealthDtos.kt:121–125`; `DTO/MarketDto.kt:44–49`; `DTO/ToxinDto.kt:71–77`).
- Existing task versions/published captures must remain readable after configuration changes. New publication and editing existing executions are distinct operations.

## Last-month evolution relevant to avoiding regressions

Relevant commits inspected by history, with final implementation verified at the revision above:

- `664a307ef`, `512b9901d`, `4b92325dc`, `2b8595b56`, `f126c16e3`: weighing migrated from client assumptions to published/pinned SOP rules; arbitrary removal proof slots; conditional answers and missing-number handling. A new mock must not present the old fixed two-video/card model as current.
- `e637634c9`, `606578151`: procurement inspection and load form became SOP authored; media kind enforced server-side.
- `d7f7ae214`, `3e4f1baba`, `0e380de7a`: herd workflows gained authored text, multi-choice, multi-proof, rework reason and shared durable workflow consumers.
- `08dd1bf77`: configurable market call opening time; `f50d51ea5`: backend-authored city questions rendered on phone.
- `2a7e97779`, `0b2380b9b`, `7c1e547a8`: pen-visit dependency and separate task routing; completing one stage need not mean downstream work is done.
- `f53c8c714`: multi-line sales; `b88b20e5e`: packing variance acknowledgement.

## What remains unproven

This is an Android-focused contract and consumer inventory, not an exhaustive backend business-rule audit. Backend persistence schemas, authoring controls, tenant/park override precedence, migration defaults versus active values, dose/safe-gap clinical scheduling, transport/warm-up generation rules, all option-source implementations, and exact sale valuation/eligibility ownership require the accompanying backend discovery. Every business family above needs that binding before claiming universal configuration coverage. No phone E2E, deployed config verification or new tests were run because this task was read-only architecture discovery.
