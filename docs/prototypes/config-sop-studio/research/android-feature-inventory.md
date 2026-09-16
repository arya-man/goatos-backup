# Android feature inventory — current main

Read-only source inventory at `origin/main` **e06d27bf600b4d5d7c46d178d9341a5f6c942a6a**, inspected 2026-09-16. The dirty working checkout was preserved; reads used a git archive of that revision under `/tmp/android-feature-audit`. This revision is newer than the earlier architecture report's `397114d1…`. No APK, device, staging data, or backend behavior was tested in this pass.

**Implemented** below means a concrete production source route plus screen/viewmodel/data contract exists, not that deployment or every edge case is certified. **Partial** means a related implemented capability is evidenced but the requested end-to-end integration is not. **Proposed/unverified** means no matching Android integration was located in the inspected paths; it is not a claim that the backend/admin product lacks it.

Path prefixes (all relative to repository root, at the revision above):

- `N` = `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt`
- `V/` = `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/`
- `D/` = `apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/`
- `DTO/` = `apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/`
- `API` = `apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/AppApi.kt`

## Feature coverage

| Area | Source evidence | Classification / contract boundary |
|---|---|---|
| Authentication, bootstrap, navigation | `app/.../auth/AuthRepository.kt`; `API:483` bootstrap; `N:1511` onward installs feature routes | Implemented. Server bootstrap/nav grants and session identity precede business screens; module directory names alone do not establish access. |
| Calendar, timetable, vaccination execution | `N:1511`, `1594`, `2299`, `2606`; `API:497` execution list, `510` shed detail, `519` calendar events; `D/CalendarRepository.kt`, `D/ExecutionRepository.kt` | Implemented. Task/drive/shed execution, scanner/submit/record paths already exist. Reuse these task identities; do not replace them with a standalone mock job list. |
| Shared scan/submit/record and RFID | `N:2381`, `2454`, `2499`, `2567`; `V/ScanViewModel.kt`, `SubmitViewModel.kt`, `RecordViewModel.kt`, `RfidViewModel.kt` | Implemented. Scan context carries task/campaign/shed/partition information. User RFID means actual identifiers, not internal display_id. |
| Weighing planning and execution | `N:1653`, `1900`, `1937`, `2054`, `2140`, `2176`, `2200`; `API:629` planner catalog, `647` campaign create, `669` roster; `D/weighing/WeighingSopRules.kt:14`, `64` | Implemented. Authored modes, removal/proof slots, planner, operator allocation, task/shed execution and export exist. Current main has SOP-driven capture rules and version/cutoff handling; not merely hardcoded questions. |
| Weighing leadership / alerts | `N:2003`, `2113`, `2598`; `API:680`; `V/WeighingAlertsViewModel.kt`, `WeighingShedDetailViewModel.kt` | Implemented surfaces. Actual alert thresholds and live delivery require backend/data mapping. |
| Counts: births, deaths, milk, colostrum | `N:2618–2858` individual route anchors; `V/AddBirthViewModel.kt`, `AddDeathViewModel.kt`, `MilkPreparationViewModel.kt`, `MilkFeedingViewModel.kt`; `DTO/WorkflowDto.kt:7–18` | Implemented. Birth/death follow-up workflows and authored action questions already exist; progress is backend-owned rather than recomputed by phone. |
| Shifting, reconciliation, promotion, approvals | `N:2858`, `2948`, `2981`, `3040`, `3073`, `4611`; `API:1066`, `1099`, `1141`, `1970`, `1981`; `V/ApprovalViewModel.kt` | Implemented. Pending execution, pen reconciliation, temporary-tag promotion and approve/reject are separate existing paths. Approval is not equivalent to an operator acknowledging a mock action. |
| Feed direction, packing, distribution, transport, wastage | `N:3099`, `3150`, `3202`, `3206`, `3316`, `3363`, `3402`, `3470`; `API:1179`, `1257`, `1267`, `1286`, `1918`; `D/FeedRepository.kt`, `FeedTransportRepository.kt` | Implemented. Existing feed task/capture/completion flow. Animal transport feed-carry calculation across travel and warm-up is a different requested integration and is not established by Feed Transport naming. |
| Health cases, observations, diagnosis review | `N:1734`, `1749`, `1765`, `1797`, `1824`, `1849`, `1865`; `D/HealthRepository.kt:89–110`, `223`, `251`, `286`; `V/DiagnosisProposalViewModel.kt` | Implemented. Age-band forms, case work items, observation/diagnosis proposals and treatment completion. Clinical source rules must retain their owners; generic configuration does not authorize inventing medical protocols. |
| Preventive care | `N:4451`, `4492`, `4568`; `API:1340–1468` worklists, tasks, roster, captures, planner, rounds and removal pens; `V/PcCarePlanViewModel.kt`, `PcCareTaskViewModel.kt` | Implemented. Planning/task/animal flows are existing primitives. Seller-site vaccination linkage and candidate-to-herd identity handoff remain integration questions. |
| Toxin | `N:3537`, `3581`; `API:1510`, `1521`; `D/ToxinRepository.kt` | Implemented task detail/steps/readings. Not a placeholder under “others.” |
| Vendors, feed purchases, market | `N:3618`, `3651`, `3665`, `3684`, `3702`; `API:1556`, `1574`, `1591`; `D/MarketRepository.kt`, `V/MarketViewModels.kt` | Implemented. Market city/day forms and configurable call timing are distinct from animal selection and transport stages. |
| Procurement animal purchase | `N:3721`, `3750`, `3773`, `3812`, `3834`; `API:1646–1661`; `D/AnimalPurchaseRepository.kt:62–115`; `DTO/AnimalPurchaseDto.kt:39–77`, `163–209` | Implemented load + per-candidate questionnaire, media slots, queued local animals, field recommendation, separate CEO decision/readback. This is the starting production surface to reuse, not rebuild as generic blank questions. |
| Sales | `N:3857`, `3908`, `3931`, `3958`, `3979`, `3993`, `4014`; `API:1691`, `1725`, `1771–1807`; `D/SalesRepository.kt`, `V/SalesPipelineViewModels.kt` | Implemented deals/payments/tagging plus buyer/FPO leads, benchmarks, sold tags and weight checks. Recorded transaction prices must remain separate from adjustable valuation assumptions. |
| Leadership tasks and Work Board | `N:4034`, `4092`, `4144`, `4241`, `4276`; `D/LeadershipTasksRepository.kt`, `WorkBoardRepository.kt`, `TasksRepository.kt` | Implemented assignment/detail/compose/board surfaces. Cross-stage orchestration must reconcile these existing tasks instead of pretending a separate local task model is production. |
| Pen visits | `N:4177`, `4217`; `D/PenVisitsRepository.kt`, `V/PenVisitDetailViewModel.kt` | Implemented visit worklist/detail with permission-aligned navigation. |
| Clock, attendance, leave, team | `N:4302`, `4319`, `4339`, `4361`, `4367`, `4396`; `D/ClockRepository.kt:92–127` status, location-aware punch, presence/person-day, leave request/withdraw | Implemented Android workforce subset. Does not establish a complete payroll/HRMS product. |
| Verification | `API:943`, `955`, `967`; `feature/feature-verify/.../VerifyQueueScreen.kt`, `VerifyDetailScreen.kt`; `D/VerificationRepository.kt:31` | Implemented queues and verdict writes. Real role/grant/evidence constraints belong to server contracts, not generic “approve” checkboxes. |
| Profile, alerts, push and device session | `N:2532`, `2573`, `2586`, `2598`; `API:486–494`; `app/.../push/PushNavigationViewModel.kt`; `D/push/DefaultNotificationsPort.kt` | Implemented surfaces/adapters. Delivery and notification policies not live-verified. |

## Existing authored SOP and form contract

Procurement already serves both `questionnaire` and `load_form` with `questionnaire_version` (`DTO/AnimalPurchaseDto.kt:62–77`). Questions carry stable IDs, kinds, exact option values, required flags, `allow_other`, conditional `only_if`, numeric min/max/unit, proof slot/max_files/accepts (`:39–59`). The load and animal write DTOs send the version and answers (`:128`, `:236`). `V/AnimalPurchaseViewModels.kt:234–318` retains draft scalar/multi answers and reads the served form; this is not evidence of an arbitrary graph runner.

Generic follow-up workflows already have list/detail and idempotent answer/complete actions (`DTO/WorkflowDto.kt:7–18`; `API:2003`, `2032`, `2044`). `WorkflowDto.kt:41–64` explicitly says progress and next action are backend-maintained. `D/WorkflowsRepository.kt:77`, `:247`, `:363`, `:486` documents durable writes, detail refresh and optimistic projection. These are concrete reuse candidates for stage tasks; their existence does not prove nested master-SOP execution is implemented.

## Sync, proof media and safety boundaries

`D/sync/SyncEngine.kt:116–117` documents durable Room writes and reclamation of in-flight rows after process death. `D/AnimalPurchaseRepository.kt:62–115` separates cached reads from outbox load/candidate creates and exposes queued animals. `D/WorkflowsRepository.kt:102–132` handles durable evidence drafts, submission locks and terminal-failure recovery. A master SOP must preserve idempotency, queued proof dependencies, local/server identities, row versions and authoritative refresh; it cannot treat local button clicks as durable approval or completed animal movement.

Shared media surface: `apps/goatos-android/core/core-ui/src/main/kotlin/sg/mesha/goatos/core/ui/ProofMediaPreview.kt` and its contract test. Photo previews require stable identity/cache/bounded display; video reading must remain explicit-action driven. The latest month contains fixes for proof recovery, media identity, remote egress and authored capture kind support. This inventory did not execute egress guards or device media tests.

No current E2E proof was gathered for `Admin-web contract unavailable` / `backend_down`, Work Board load failure, or Weights load failure. Code availability must not be reported as availability certification.

## Latest procurement scenario: reuse versus unresolved integration

| Requested scenario | What Android already supports | Remaining question / classification |
|---|---|---|
| Seller offers 100; inspect each; CEO accepts 70 | Loads/candidate IDs, per-animal authored Q&A/proofs, field verdict distinct from accepted/rejected CEO decision (`AnimalPurchaseDto:163–209`) | **Implemented primitives.** Confirm server selection bulk workflow and rejection audit; phone decision display alone does not prove CEO write UI. |
| Tag accepted subset and schedule vaccination | Candidate temp_tag; shared RFID, promotion, vaccination/PC task flows | **Partial.** What creates herd identity, when, and how candidate acceptance binds vaccination obligation? |
| Hold at seller 15 days | Date/time/task infrastructure exists elsewhere | **Unverified integration.** Need persisted stage clock, start condition, extensions and approval role; do not hardcode 15 as universal rule. |
| Reinspect before transport; reduce subset | Authored candidate questions and decision fields | **Unverified repeated-stage semantics.** Is a new inspection attempt/version stored per candidate? How is shipped subset distinguished from initially accepted set? |
| Truck sanitation; carry feed for travel plus warm-up | Proof-capable tasks, feed configuration/workflows | **Partial primitives.** Transport-load identity, sanitation approval, travel/warm-up duration, ration scope/units and rounding are not established by these Android files. |
| Every three hours photo/video en route | Feature-owned proof capture/upload/task rendering | **Unverified integration.** Need scheduled obligations, clock/timezone, retries/offline overdue semantics and exact proof slot policy. No global media rule. |
| Destination readiness only after transit starts | Work Board/tasks and source-owned execution workflows | **Unverified dependency integration.** Need source event/idempotency, destination scope, responsible team and started-versus-completed prerequisite. |
| Arrival then gradual old-feed/farm experiment-feed warm-up | Feed task/verification primitives, animal scopes | **Unverified stage/experiment integration.** Need ration plan ownership, per-group experiment identity, daily adjustment and clinical approval boundaries. |
| Generic parent SOP composed from child SOPs | Authored questionnaires and backend workflow actions | **Proposed until mapped.** No nested master/child SOP DTO or execution stack was established in this bounded Android pass. Reuse existing contracts first; do not assert absence across backend/admin. |

## Recent history and documentation drift

Reviewed the last month of Android-affecting history. Relevant changes include `e637634c9` procurement animal SOP; `606578151` authored load form; `d7f7ae214` authored workflow steps/proof choices; `f50d51ea5` Procurement Market tab; `664a307ef` weighing SOP execution; `512b9901d` configurable removal capture slots; `f126c16e3` optional answer/missing number fixes; `0b68d87e6` individual birth clip review. These materially expand the real product compared with older prototype assumptions.

`apps/goatos-android/MODULE-MAP.md:3` still describes 22 modules and largely placeholder features. The current tree has 21 feature directories and substantial routes/repositories. Treat that map as historical, not authoritative feature status.

## Questions before further implementation

1. Which existing backend workflow/template/task IDs should represent parent stage, child SOP version and candidate cohort? Who owns approval transitions?
2. Which requested procurement stages already have staging records/contracts versus proposed additions? Parent's DB/backend inventories must answer this.
3. Does published SOP pinning currently govern all affected task families or only authored procurement/weighing/workflow features? Map exact publication consumers.
4. How do accepted/held/rejected/reinspected/shipped/arrived candidate sets evolve without losing original inspection history?
5. Which stage dependencies are start, completion, approval, elapsed time, or periodic obligations; how are offline actions reconciled?

No additional prototype implementation should claim these answers until the backend, admin-web and staging inventories are synthesized.

## Cross-team staging evidence received during inventory

Parent independently reports the current staging publication exported as `research/procurement-published-v7.json`: **7 load questions and 40 animal questions**, matching the inspected live admin UI. This supersedes the earlier prototype's 38-animal-question import. This Android pass confirms the phone contract consumes server-provided form lists/version rather than fixing that count. The export/live-UI verification is attributed to the parent; existing user drafts must not be overwritten when a refreshed reference is eventually imported.
