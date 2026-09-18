# Android hardcoded business-rule audit

2026-09-16. Read-only origin/main object inspection at **e06d27bf600b4d5d7c46d178d9341a5f6c942a6a**. No checkout edits, fetch, build, phone test, or deployment. Git object paths/lines below deliberately do not use dirty working-tree line numbers.

## Findings and ownership

| Area | Value | Source at revision | Classification |
|---|---|---|---|
| Weighing planning fallback | 100 animals/day | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/WeighingPlanWizardViewModel.kt:649,869` | Server-owned rules.defaultCapPerDay wins when positive;100fallback. Do not present as Sales eligibility. |
| Weighing capture clamp | 5 shed-group videos | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/WeighingViewModel.kt:2540,3922` | Client coerceIn(1,5) constrains server lumpSumVideoMax; a genuine feature cap, not global proof cap. |
| Weighing free-flow scope | 100 proofs/scope | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/WeighingViewModel.kt:3910` | Client scope constraint explicitly independent of sync pagination; review configuration ownership. |
| Workflow evidence compatibility | requiresVideo with0minimum becomes1 | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/WorkflowDetailViewModel.kt:449,965` | Server proofMinPhotos/Videos generally consumed; fallback preserves legacy required-video behavior. |
| Feed wastage validation | 0..10000kg | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/VerifyDetailViewModel.kt:95-101` | Client mirror of backend range, not a ration rule. |
| Feed packing verification | 500g variance | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/VerifyDetailViewModel.kt:75-85` | Comment describes backend measurement_confirmation_required; phone displays server refusal, not locally calculating threshold. |
| Sales deal shape | maximum20lines | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/SalesViewModels.kt:884-886` | Explicit mirror of backend MaxDealLines; not price policy. |
| Sales breed catalogue | o.breeds[product] | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/SalesViewModels.kt:618` | Consumed options; not hardcoded breed catalogue here. |
| Sales future date wording | within60days | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/SalesViewModels.kt:897` | Literal error wording found; backend enforcement must be crosschecked, do not infer from string alone. |
| PC care planning window | past30days/future14days | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/PcCarePlanViewModel.kt:895-896` | Client query/work planning window; separate from clinical dose intervals. |
| PC care worklist window | today through7days | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/PcCareWorklistViewModel.kt:162-163` | Client worklist horizon, not vaccine policy. |
| Feed historical window | 30days | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedPackingViewModel.kt:374` | Client history visibility; FeedWastageViewModel.kt:321 same. |
| RFID heuristic | numeric length12 | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/PcCareTaskViewModel.kt:2486` | Identifier input classification constraint; not animal display_id mapping. |
| Voice note capture | 3minutes | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/capture/AudioCaptureLauncher.kt:51` | Feature capture duration business UX limit, not global video cap. |
| Leadership voice attachment | 10minutes | `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/LeadershipTaskComposeViewModel.kt:484` | Different explicit feature duration; preserve independent ownership. |

## Shared policy defaults versus business enforcement

`apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/forms/ProofPolicy.kt:11-17,24-55,86-120`: backend-owned proof-policy structural parser; missing fields default to video, required=true, per_goat_video/goat, in_app_camera; minimum counts0 and maximumsnull. **Deliberately uncapped** shared layer. Do not replace this with a global policy cap. Feature-specific constraints above remain separate.

`apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/WeighingDto.kt:286-301`: planning defaults modes individual_animal/per_shed_partition and cap100; removal mode defaults required, cutoff/instruction blank and proof/question arrays empty. These are deserialization fallbacks, not evidence the server currently publishes those values.

`apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/WorkflowDto.kt:130-131`: proof counts default0. Workflow compatibility code supplies1video when legacy requiresVideo=true.

## Sales ownership requested by maintainer

₹450/₹600/₹500 rates and30/35kg thresholds must be Sales-owned shared policy consumed by Weighing as instructed. Broad main-source search did **not** establish those literal monetary/eligibility rules in Android production business code. Numeric search hits include design tokens, compression bitrates, tests and samples; these must not be relabelled Sales configuration. Android Sales currently consumes breed options and captures ledger amounts; this does not prove shared eligibility enforcement. Backend/admin-web audit must locate the actual price/weight computations; then phone/API contract can consume versioned policy without duplicating values.

## Last-month context

Relevant origin/main history inspected:4b92325dc loads pinned weighing capture rules;2b8595b56 guards SOP windows and pinned cutoffs;11e16783d keeps task removal definitions stable across edits;dad1d38a0 closes replay/removal/authored capture gaps;f126c16e3 validates optional answers and missing numbers;0b68d87e6 verifies each birth clip separately. These argue for version-pinned server rules and feature-owned evidence rather than a new generic constant service overriding active tasks.

## Coverage and exclusions

Searched all Android production Kotlin paths across app/core/features for price/weight literals, business min/max/default/duration/proof values and catalogue constructors; inspected full shared ProofPolicy parser and relevant weighing/Sales/verification sections. This is a broad triage inventory, **not exhaustive certification of every expression or resource string**. Health duration displayed from durationDays in HealthViewModels.kt:620-629; not evidence of local treatment-duration invention. Procurement numeric range comment0.5–300kg describes renderer examples; source line1391 is not itself a hardcoded validator.

Excluded technical retries, network timeouts, analytics caps, pixel/color/font constants, queue sizes, pagination and test fixtures. No claim that absence from this report means absent feature configuration. Device adoption, live values, and backend parity remain separately unverified. Do not promote based solely on this report.
