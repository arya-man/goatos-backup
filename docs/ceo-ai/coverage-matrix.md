# Leadership Assistant — Coverage Matrix (backfill baseline)

## Implementation Status

**2026-07-22**: Fixed critical readtools findings:
1. Deleted the broken projected-count call (wrong method signature; missing park + target date). Species now sourced from Cube.
2. Routed animal species counts (counts_breakdown) to Cube tier instead of API tier (Cube has correct active_animal_count metric).
3. Fixed error swallowing: toolexecutors now propagate reader errors instead of silently returning empty facts.
4. Vaccination/feed executors return errors when not wired, preventing silent empty results.
5. Added comprehensive toolexecutors_test.go with error-propagation + species-split assertions.
API tier routing now returns real data from the Mesha read APIs listed in section A below,
with errors properly surfaced for fallback orchestration.

One-time full sweep of every leadership-relevant table, read API, and feature in
the repo against the assistant's read path. Each row resolves to exactly one
coverage path — a **Cube** governed metric, a **`ceo_ai.*`** reporting view, an
**MCP Toolbox** tool, a mapped **Mesha read API**, or a **documented exclusion**.
This is the fully-covered baseline the `leadership-assistant-coverage-guard`
enforces forward from. When you add a feature, add its row here (or an exclusion)
in the same change. Draft = view/tool/metric designed here, implementation
tracked in `docs/ceo-ai/mcp-toolbox-plan.md`.

Legend: **Cube** governed metric · **view** = `ceo_ai.*` reporting view · **tool**
= MCP Toolbox curated tool · **api** = Mesha read API · **EXCLUDED** = documented
non-leadership surface.

## A. Read APIs

Source inventory: the read-API catalog the planner consumes. Leadership-relevant
APIs map to a tier; the rest are documented exclusions with a reason.

| Read API (path) | Coverage path | Notes |
|---|---|---|
| GET /herd-register/summary | api + Cube:active_animals | Primary census; aggregate-first |
| GET /counts/breakdown | api + view:animal_current_scope | Grouped census drilldown |
| GET /app/counts/shifting-events/pending-execution | api + view:counts_movement_daily | Raised/authorized/evidence-rework Actions; census moves only after Park Head approval + operator completion. High-priority feed requirement/fingerprint is operator execution detail, not a leadership KPI; leadership movement state remains covered at event/day grain. |
| GET /app/counts/approvals | api + view:counts_movement_daily | Pending census approvals. The backend-owned display copy on each row (`raised_by_name`, `summary_line`) is composed by `WithApprovalNames` / `ResolveApprovalNames` / `PersonName` / `ApprovalSummaryLine` / `ApprovalSummaryLocationIDs`, which are EXCLUDED as leadership surfaces: they resolve ids to names for the approver's queue copy and derive no new fact. Every underlying fact they render — movement, park/shed, raiser, request type — is already covered at event/day grain by counts_movement_daily, so the assistant reads the fact, never the rendered sentence. |
| GET /app/counts/shifting/destinations | EXCLUDED | Operator write-flow picker; not a leadership metric |
| GET /goats/search | EXCLUDED | Record-level lookup; leadership stays aggregate |
| GET /goats/{goat_id} | EXCLUDED | Single-animal detail |
| GET /goats/{goat_id}/passport | EXCLUDED | Per-animal history detail; admin-web goat rosters may open this as an operator/local-drawer detail, but CEO assistant answers stay aggregate unless a leader explicitly asks for a named animal record |
| GET /goats/{goat_id}/vaccination-passport | EXCLUDED | Per-animal vaccination history/open-obligation detail for Goat Passport drawers; not a leadership aggregate tool. Assistant coverage/read API note: when this detail is opened from Calendar/Herd/Shed rosters, open obligation dates must use the canonical vaccination effective-date chain: `vaccination_drive_assignments.planned_date`, then `obligation_batches.planned_date`, then raw `obligation_instances.due_at` only as the final legacy fallback. |
| GET /goats/{goat_id}/timeline | EXCLUDED | Per-animal audit trail |
| GET /identifiers/{type}/{value}/resolve | EXCLUDED | Scan-time resolution utility |
| GET /app/vaccination/tasks/{task_id}/option-values (func:TaskOptionValues) | EXCLUDED | Operator scan/execute form option-values (dropdown vocabulary for a task); an operator write-flow input, not a leadership aggregate metric |
| GET /vaccination/execution | api + Cube:vaccination_due/overdue | Due/overdue by shed |
| GET /vaccination/execution/sheds/{shed_id} | api + view:vaccination_shed_status | Cause drilldown |
| GET /vaccination/operations | api + view:vaccination_shed_status | Cohort rollups |
| GET /vaccination/schedule | api + view:vaccination_operator_status | Operator-date drive workload from `vaccination_drive_assignments`; leadership assistant and admin-web summarize one operator/day row with animal cap, animal progress buckets, physical shed total chips, vaccines, total doses, and partition metadata only as local drawer drilldown context; drawer shed rows deep-link to the shed execution/goat roster view |
| POST /vaccination/schedule/drive-date-overrides | api + view:vaccination_operator_status | Admin vaccine-date move/revert. The write path must split or restore raw `vaccination_drive_assignments` membership for the moved vaccine; leadership assistant, MCP Toolbox/read API answers, and SQL fallback must report the regenerated operator-date rows, not stale mixed rows from the original date. |
| Vaccination schedule-source sync across Calendar/AC/PA/WF/execution/Android | api + view:vaccination_operator_status | Coverage clarification: no new assistant tool or KPI. Existing vaccination read API, MCP Toolbox, and `ceo_ai` coverage must prefer the effective `vaccination_drive_assignments` operator-day date before legacy batch/obligation/task due dates, so leadership answers match the same current schedule shown in admin-web and Android. |
| GET /vaccination/command (func:GetVaccinationCommandBoard, func:VaccinationCommandBoard) | api + Cube:vaccination_metrics | Command board: all-history KPI summary (targets, verified, awaiting, overdue, scheduled ahead), drive-scoped active batch summary, cohort×vaccine pending matrix (ET+TT/PPR/etc pending count per stage/sex), verification queue with shed, dose rule, awaiting count, total, and days-in-queue age. Grain: obligation-scoped KPIs + shed×vaccine×cohort matrix + shed-scoped proof backlog age. |
| HRMS operator vaccination animal cap | api + view:workforce_coverage_status + view:vaccination_operator_status | Coverage clarification: the scheduler and leadership assistant read per-operator animal capacity from `workforce_positions.vaccination_daily_animal_cap`; tenant `vaccination_capacity_config.max_per_day` is fallback only. Capacity/utilization answers must use the HRMS position cap that admin-web HRMS edits persist. `null` clears a custom HRMS cap and means default capacity, never zero; DB coverage must include real Postgres proof of custom/default/week-off operator rows. |
| vaccination_operator_capacity_overrides | api + view:vaccination_operator_status | Date-scoped operational exception table for explicit operator/date animal-cap overrides. Leadership capacity answers stay on the operator schedule/status surface and must show normal HRMS cap semantics unless a matching override row exists for that exact operator/date. CPT uses this only for the one-time 2026-07-25 ET+TT seed catch-up allowance; it is not a general cap increase. |
| GET /vaccination/sheds | api + view:vaccination_shed_status | Shed status list |
| GET /vaccination/sheds/{shed_id} | api + view:vaccination_shed_status | Shed drilldown |
| GET /vaccination/sheds/{shed_id}/animals | EXCLUDED | Animal-level detail; not aggregate |
| GET /vaccination/action-center | api + view:action_center_current | Exception queue |
| GET /vaccination/action-center/counts | api + view:action_center_current | Summary tiles |
| GET /vaccination/adherence | api + Cube:vaccination_compliance | Governed compliance KPI |
| Process-integrity vaccination labels and same-business-day status | api + view:action_center_current | Coverage clarification: no new assistant tool or KPI. Existing Action Center, Protocol Adherence, Control Tower, and Workflow read paths must report human vaccine labels when available and must classify assignment-planned vaccination work as overdue only after its India business date has passed, so assistant and admin-web process-integrity answers do not leak raw rule codes or mark today's drive late at morning check-in. Grouped process-integrity rows must carry the computed execution date forward under the alias consumed by final rows, so fresh API binaries do not fall back to stale obligation dates or fail Control Tower reads. |
| func:NewVaccineLabelResolver, func:ResolveVaccineLabels (notification bridge) | EXCLUDED | Notification-copy helper, not a read surface. It batch-resolves `protocol_rules.rule_id` → human vaccine label in ONE query so a push/notification says "PPR" instead of a raw rule uuid. It adds no read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool; leadership vaccination answers stay on the existing `view:action_center_current` / `view:vaccination_shed_status` surfaces, which already carry their own labels. |
| func:AuthorizedParkIDsForCapability | EXCLUDED | Authorization primitive in `platform/httpmiddleware`, not a leadership surface. It returns the actor's park scope ids for a single capability, keeping each grant's role bound to its own scope so an unrelated park grant cannot borrow a capability granted on a different park. It narrows what an actor may read; it never widens coverage and exposes no new metric, view, or tool. |
| Process-integrity evidence media resolver (func:WithMediaResolver, func:ActionCenter) | api + view:action_center_current | Coverage clarification: no new assistant tool, Cube metric, or `ceo_ai` reporting view. The existing Action Center / Protocol Adherence / Control Tower / Workflow read surfaces are still covered by `action_center_current`; this change only enriches their shared process-integrity `evidence` payload with proof-media download links resolved through the existing proof/verification signed-URL path. |
| GET /vaccination/capacity-config | api | Capacity behind backlog explanations |
| GET /vaccination/verification-queue | api + view:verification_queue_status | Proof gaps |
| GET /weighing/campaigns | api | Leadership planning/monitoring read for weekly kids weighing campaigns; aggregate/status surface only. |
| GET /app/weighing/campaigns | EXCLUDED | Operator execution list; leadership uses `/weighing/campaigns`. |
| Weighing shed-level operator assignments (`weighing_campaign_sheds.operator_user_id`) | api | Assistant coverage stays on `GET /weighing/campaigns`: leadership sees the campaign, selected shed buckets, per-shed owner/status, and progress rollups there. Operator-scoped mobile filtering and write authorization are execution behavior, not a separate CEO AI tool, Cube metric, MCP/Toolbox tool, or `ceo_ai` SQL fallback surface. |
| func:ListCampaignsForOperator, func:ListScopeRoster, func:ListScopeRosterForOperator | EXCLUDED | Operator-only execution read helpers for mobile shed buckets. They exist to keep `/app/weighing/campaigns` and `/app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/roster` scoped before pagination/row return. The two roster readers now serve the bucket's SCAN HISTORY only — free-flow has no expected roster to read, so the herd-keyed roster query, its `animal_id` cursor, and the `include_roster` switch are gone (the `items` / `next_cursor` response fields are retained, always empty, purely so an already-installed app keeps its wire shape). Their signatures shrank accordingly; that is why this row's fingerprint moved, not because a new leadership surface appeared. Leadership assistant coverage remains `GET /weighing/campaigns` plus `ceo_ai.weighing_capture_activity` / `ceo_ai.weighing_verification_status` (migration `000080`); no MCP/Toolbox, Cube, or `ceo_ai` SQL fallback surface is added. ISOLATION: these reads touch weighing tables only — no goats, goat_identifiers, herd_animals, protocol, or vaccination join, and no expected-roster denominator, per the free-flow ruling behind `000078`/`000079`. |
| func:ListCampaigns, func:AppListCampaigns, func:ParseCampaignListScope | api | The one weighing list read, split by SURFACE rather than duplicated: `ListCampaigns` serves admin-web and defaults to `scope=all`; `AppListCampaigns` serves the phone and defaults to `scope=mine` (the caller's own work), so an already-installed app that predates the scope parameter keeps its previous meaning. `ParseCampaignListScope` validates that parameter. Leadership coverage is unchanged and remains `GET /weighing/campaigns` — no new tool, metric, view, or KPI. |
| func:PlannerCatalog, func:PlannerParkBuckets | EXCLUDED | The create-wizard's park picker and its per-park shed page. Planning a weighing task is CEO-only (maintainer decision 2026-08-01), so these are authoring surfaces behind `weighing.plan`, not reporting reads: they return selectable capacity for a task that does not exist yet, which no leadership question is asked of. Leadership monitoring of raised tasks stays on `GET /weighing/campaigns`. |
| func:Error, func:Unwrap | EXCLUDED | Go `error` interface methods on weighing's typed errors. Not a read surface of any kind. |
| func:New, func:EnqueueWeighingVerification, func:WithVerificationEnqueuer | EXCLUDED | Internal weighing proof-video enqueue bridge. It moves already-captured weighing videos into the existing Verification workstream and adds no new CEO assistant read API, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or leadership KPI. Leadership visibility remains through the existing weighing monitor/video APIs and verification surfaces. |
| POST /weighing/campaigns, /weighing/campaigns/{campaign_id}/publish | EXCLUDED | Leadership write/publish workflow, not a read metric. Result remains visible through `GET /weighing/campaigns`. |
| POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/reopen (func:ReopenScope) | EXCLUDED | Growth Director/CEO write path that reopens a completed weighing shed bucket for more free-flow scans. It adds no new CEO assistant read API, Cube metric, MCP/Toolbox tool, or `ceo_ai` SQL fallback surface. Leadership sees the result through existing `GET /weighing/campaigns` campaign/shed status and progress reads. |
| POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/abandon (func:AbandonScope) | EXCLUDED | Leadership write path that ends a weighing bucket whose work will never finish, WITHOUT the verification gate that the normal close now enforces. Deliberately a separate endpoint/event (`weighing.shed.abandoned`, audit `weighing.scope_abandoned`) so "ended without verification" is never mistaken for "verified and closed". It adds no new CEO assistant read API, Cube metric, MCP/Toolbox tool, or `ceo_ai` SQL fallback surface; leadership sees the outcome through existing `GET /weighing/campaigns` status reads. |
| func:AbandonScope, func:pendingVerificationCount (weighing close gate) | EXCLUDED | Write-path helpers for the maintainer-decided close gate: normal close is blocked while any submitted weighing video is still unverified, and abandon is the explicit reason-bearing way out. Neither adds a leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| POST /app/weighing/campaigns/{campaign_id}/animal-observations, /shed-observations | EXCLUDED | Operator write-flow submissions with mandatory proof; leadership sees progress/review state through `/weighing/campaigns`. |
| GET /app/weighing/alerts (func:ListAlerts, weighing service + postgres adapter) | EXCLUDED | The weighing module's own lifecycle ALERTS feed: the weighing work-state transitions (assigned / submitted / reopened / rework / closed) that were already routed to the CALLER, read back from `notification_requests`. It is a PER-RECIPIENT INBOX, not a reporting surface — every row is scoped to one person's `context->>'member_id'` and bounded to a rolling 30-day window, so it can answer "what happened to me lately", never "how is weighing going". Asking it a leadership question would give the CEO only the subset of transitions that happened to be pushed to the CEO's own devices, which is a strictly worse and non-deterministic answer than the real rollups. Leadership weighing coverage therefore stays exactly where migration `000080` put it: `ceo_ai.weighing_capture_activity` + `ceo_ai.weighing_verification_status`, plus `GET /weighing/campaigns` as the planning/monitoring read API. No new Cube metric, `ceo_ai.*` view, MCP/Toolbox tool, or KPI is added. Notification DELIVERY health already has its own view (`ceo_ai.notification_delivery_health`) and is unchanged. ISOLATION: this read touches `notification_requests` and `workforce_members` only — it joins no goats, goat_identifiers, herd_animals, obligation, protocol, or vaccination object, and reports no expected-roster denominator, per the free-flow ruling behind `000078`/`000079`. |
| func:RegisterVerificationAppliers | EXCLUDED | Phase 1 write-path and verification consumer plumbing; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| func:MarkVerdictApplied, func:AckWeighingVerificationApplied, func:VerdictState, func:WithApplyAcker | EXCLUDED | The verdict APPLY-ACK seam. A verifier's decision is applied asynchronously by a durable-bus consumer, so "decided" and "in effect" used to be indistinguishable from every read surface — the item left the pending queue the instant the verdict was recorded, which read as done even when the applier had not run. These carry the receipt (`applier_ack_expected` / `applied_at`) and derive `awaiting_review | applying | settled` for the VERIFIER's own queue. Per-item workflow state for the person acting, not a reporting surface: leadership weighing coverage stays at `ceo_ai.weighing_capture_activity` + `ceo_ai.weighing_verification_status` (migration 000080) plus `GET /weighing/campaigns`. No new Cube metric, `ceo_ai.*` view, or Toolbox tool. |
| func:RecordOutboxFailed | EXCLUDED | Outbox TERMINAL-visibility instrumentation. An `invalid_event_envelope` or permanent publish failure previously emitted no log and no counter, so a domain event could die with nobody informed — 12 did. This adds a WARN line and a `kernel.outbox.failed` counter. Platform diagnostics consumed by logs/metrics, never by leadership: delivery health already has `ceo_ai.notification_delivery_health`. No new read API, Cube metric, `ceo_ai.*` view, or Toolbox tool. |
| func:Error, func:Unwrap (weighing ports.CaptureIncomplete) | EXCLUDED | Error-type plumbing for the submit-time weight+video pair gate. Enforcement failing silently as `RowsAffected()==0` used to surface as a 404 about a shed the operator is standing in; this carries which animals lack a weight or a finished video so the client can name them. Operator-facing error copy, not a leadership read. |
| func:NewWeighingReworkDigestStage, func:SweepReworkDigests, func:Name, func:Run (weighing rework digest stage) | EXCLUDED | The per-shed rework DIGEST sweeper. A verifier rejecting 5 animals in one shed used to send the operator 5 separate pushes for one trip back to the shed; this coalesces them into one push naming the animals. A bounded operational-kernel stage plus its claim query — operator notification plumbing, not a leadership read. No new Cube metric, `ceo_ai.*` view, or Toolbox tool. |
| func:DeleteUnattachedProof, func:DeleteUpload, func:StatObject, func:EnsureObjectAvailable, func:EnsureEvidenceAvailable | EXCLUDED | Proof-evidence integrity plumbing, shared by every module that captures proof. `DeleteUnattachedProof`/`DeleteUpload` scope the re-record cleanup to the proof's own uploader (it previously checked tenant but not owner). `StatObject`/`EnsureObjectAvailable`/`EnsureEvidenceAvailable` let a verdict verify the proof object still EXISTS before an APPROVE, rather than trusting that a signed link can be issued from the DB row — deliberately at decision time for ONE item, never on the queue read. Write-path/authorization plumbing with no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| func:NewWeighingLifecycleEventConsumer | EXCLUDED | Phase 1 write-path consumer factory; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| func:Register (weighing lifecycle event consumer) | EXCLUDED | Phase 1 write-path consumer registration into kernel; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| func:HandleEvent (weighing lifecycle event consumer) | EXCLUDED | Phase 1 write-path event consumption; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. Leadership sees weighing state through `GET /weighing/campaigns`. |
| func:CloseScope (weighing verification verdict handler) | EXCLUDED | Phase 1 verification consumer write-path helper; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| func:CloseCampaign (weighing verification verdict handler) | EXCLUDED | Phase 1 verification consumer write-path helper; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| func:ApplyVerificationVerdict (weighing verification verdict handler) | EXCLUDED | Phase 1 verification consumer write-path helper; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. Leadership sees weighing verification state through `GET /weighing/campaigns`. |
| func:NewVerificationVerdictHandler (weighing verification verdict handler) | EXCLUDED | Phase 1 verification consumer factory; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| GET /vaccination/workflows/{row_id} | EXCLUDED | Row-level process-integrity detail |
| GET /control-tower/vaccination (func:ControlTower) | api + view:action_center_current | Leadership control tower |
| GET /app/vaccination/execution(+/sheds/…, roster, coverage, gaps, tasks/…) | EXCLUDED | Operator-scoped app views; leadership uses /vaccination/*. Runtime contract: operator execution and scan rosters must filter split-shed work by `vaccination_drive_assignments` plus `goat_shed_partitions`, so one operator cannot see another operator's partition animals inside the same batch/shed. |
| GET /calendar/vaccination/events | api | Calendar timeline (dots) |
| Calendar vaccination date markers | api | Leadership assistant read API coverage: month/week marker dots use the same assignment-effective schedule date as the calendar event list and vaccination operator schedule, so leadership answers and client overview counts do not report stale batch/obligation dates after a drive move. |
| GET /calendar/vaccination/events/{event_id}(+/history,/targets) | EXCLUDED | Single-event / target detail; admin-web Calendar drive target rosters must still open the shared Goat Passport local drawer with per-goat vaccination history |
| GET /action-center/obligations | api + view:action_center_current | Cross-domain queue; API tier executor wired (action_center_obligations tool) |
| GET /feed-direction/preview | api + view:feed_direction_current | Feed needed today; blocked≠0 |
| GET /feed-direction/generation-preview | api + view:feed_direction_current | Planned generation + gaps |
| GET /feed-direction/counts-projection/exceptions | api + view:ops_exception_queue | Blocked feed cells |
| GET /feed-packing/worklist | api | Packing worklist |
| GET /feed-transport/tasks | EXCLUDED | Operator-owned, shed-grain today-task execution list; leadership verification backlog remains covered by view:verification_queue_status. |
| POST /feed-transport/tasks/{task_id}/submit | EXCLUDED | Operator evidence mutation, not a leadership read. Resulting verification state is covered by view:verification_queue_status. |
| GET /feed-config/ration-rates | api + view:feed_direction_current | Config behind feed cost |
| GET /feed-config/ration-groups | EXCLUDED | Config taxonomy reference |
| GET /feed-config/shed-tags | EXCLUDED | Config mapping |
| GET /feed-config/feed-items | EXCLUDED | Reference catalog |
| GET /feed-config/session-templates | EXCLUDED | Config templates |
| GET /feed-config/schedule | EXCLUDED | Config |
| GET /feed-config/shed-factors | EXCLUDED | Config |
| GET /feed-config/experiment | EXCLUDED | Experiment config; niche |
| GET /procurement/source-entry/loads | api + view:procurement_pipeline / Cube:procurement_cost | Open loads / pipeline; API tier executor wired (procurement_source_entry_loads tool) |
| GET /procurement/source-entry/loads/{load_id} | api + view:source_entry_health_status | Load drilldown |
| GET /admin/roster/positions | api + view:workforce_coverage_status | Who owns which shed |
| GET /admin/roster/positions/{position_id} | EXCLUDED | Single-seat detail. Repo read `GetPositionByID` backs this single-seat drawer only; leadership capacity/coverage answers aggregate through `GET /admin/roster/positions` + `view:workforce_coverage_status`, never a named individual seat. |
| GET /admin/roster/coverage | api + view:workforce_coverage_status | Coverage matrix; API tier executor wired (admin_roster_coverage tool) |
| GET /admin/roster/leave | api + view:workforce_coverage_status | Absence exposure |
| GET /admin/roster/leave/{absence_id} | EXCLUDED | Single-record detail |
| POST /admin/roster/leave/{absence_id}/resolve-coverage | EXCLUDED | Single-absence coverage mutation (`ResolveLeaveCoverage`), not a leadership read. It is a vaccination-planning-effective transition: it enqueues `vaccination.leave.changed` in the same transaction so the operator-config replan consumer releases/re-plans that park's future drives. Leadership sees the RESULT through `view:workforce_coverage_status` and the vaccination operator/date surfaces, never this write. |
| PUT /vaccination/capacity-config (func:PutCapacityConfig, func:UpdateCapacityConfig, func:UpsertCapacityConfig, func:WithCapacityConfigWriter) | EXCLUDED | Admin config WRITE, not a leadership read. Edits the tenant operator daily-animal cap + nullable per-animal shot-cap override on the People/vaccination-operators screen. It is vaccination-planning-effective: `UpsertCapacityConfig` fans `vaccination.capacity.changed` per active park in the same transaction so the replan consumer re-plans future drives. Leadership sees the RESULT (capacity/throughput) through the vaccination operator/date and drive surfaces, never this write endpoint. |
| func:ApplyCapacityShotCapOverride | EXCLUDED | Obligation-sweeper planner helper that applies the capacity-config per-animal shot-cap override (or falls back to rule_dsl/default). Internal scheduling logic, not a leadership read surface. |
| func:RealignOpenObligationForGeneration | EXCLUDED | Internal obligation-generation repair that atomically moves an already-open, stable adult blank-history campaign row when later same-vaccine history identifies the normal repeat-drive date. It is a scheduler write helper, not a leadership read surface. Leadership sees the resulting operator/day totals through `GET /vaccination/schedule`, `GET /calendar/vaccination/events`, and `view:vaccination_operator_status`. |
| func:PlannedDriveSessionsForShed | EXCLUDED | Operator/execution read helper for a shed's planned drive sessions; operational drive-execution detail, not a leadership aggregate metric. Leadership drive/coverage answers aggregate through the vaccination operator/date + drive surfaces. |
| func:ParkIDsForVaccinationOperator, func:ListVaccinationOperatorsForPark | EXCLUDED | Internal workforce roster reads that back the min-operator leave-coverage GUARD (duty-based operator membership per park + the leaving member's park set), mirroring the scheduler's `position_module_duties` predicate. They enforce a write-path invariant (a leave can't drop a park below one available operator); they are not leadership-facing reads. Leadership coverage answers aggregate through `view:workforce_coverage_status` + the vaccination operator/date surfaces. |
| GET /admin/roster/backup-config | EXCLUDED | Config |
| GET /admin/roster/vaccination-owner | api + view:workforce_coverage_status | Accountability mapping |
| GET /app/roster/timetable, /app/roster/my-coverage | EXCLUDED | Self-scoped operator schedule |
| GET /admin/operators | api + view:workforce_coverage_status | Operator roster |
| GET /admin/operators/{id}(+/devices,/grants) | EXCLUDED | Single-record / RBAC / device detail |
| GET /admin/locations | api | Facility inventory |
| GET /admin/locations/{id}(+/children,/aliases) | EXCLUDED | Single-record / hierarchy / naming |
| GET /admin/locations/{id}/capacity | api + view:shed_capacity_current | Capacity |
| GET /admin/locations/{id}/usage | api + view:shed_capacity_current | Occupancy vs capacity; admin_location_usage tool left on Toolbox/fallback -- locations.Service only exposes single-location Usage/ListCapacity reads (one location_id argument), not a bounded across-parks/sheds listing a capacity-variance question needs; wiring it would require a new read model, out of scope of this pass |
| GET /admin/location-review-items | api + view:ops_exception_queue | Facility data-integrity queue |
| GET /admin/sops | api + view:sop_execution_status | SOP definitions |
| GET /admin/sops/{id}(+/versions/…) | EXCLUDED | SOP version detail |
| GET /admin/tasks | api + view:sop_execution_status | SOP execution backlog |
| GET /admin/tasks/{id} | EXCLUDED | Task detail |
| GET /admin/tasks/submission-fanouts/failed | api + view:ops_exception_queue | Proof fan-out failures |
| SOP submission fanout retry worker (func:NewSopSubmissionFanoutRetryStage, func:SopSubmissionFanoutRetryStage.Run, func:SopSubmissionFanoutRetryStage.Name) | api + view:ops_exception_queue | Operational repair surface for submitted proof fanouts that failed before vaccination completions / verification rows materialized. Leadership does not call the worker directly; failures remain visible through `GET /admin/tasks/submission-fanouts/failed` / ops exception coverage, and the kernel worker retries them durably. |
| GET /app/tasks(+/{id}, /shed-completion-summary), /app/sop-versions/{id} | EXCLUDED | Self-scoped operator worklist / form |
| GET /verification/queue | api + view:verification_queue_status | Verification backlog; API tier executor wired (verification_queue tool) |
| Admin-web `/actions` | api + view:verification_queue_status | Cross-module evidence viewer over the already-covered verification queue. Action-type/status filters, details, and signed proof-video links add no new KPI or assistant tool. |
| GET /verification/action-queue | api + view:verification_queue_status | Actionable proof exceptions |
| GET /operations/audit | api + view:audit_activity_summary | Audit stream |
| GET /operations/audit/summary | api + view:audit_activity_summary | "What changed" summary; API tier executor wired (operations_audit_summary tool, via operationsaudit.Service.Summary) |
| GET /operations/dlq | api + view:ops_exception_queue | Failed-event queue (see gap G5) |
| GET /operations/kernel-health | api + view:ops_exception_queue | System integrity (see gap G5); API tier executor wired (operations_kernel_health tool, via processintegrity.Service.ControlTower with OnlyBrokenOrAtRisk) |
| func:SuppressInvalidRecipient | EXCLUDED | Notification delivery hygiene only: clears FCM tokens that Firebase reports as invalid and suppresses queued rows for that exact token. Leadership delivery health remains covered by `ceo_ai.notification_delivery_health` / `mesha_notification_delivery_health`; this function is not a leadership read surface. |
| DELETE /app/proofs/{proof_id} (func:DeleteUpload, func:DeleteUnattachedProof, func:Delete, func:FinalizeUpload, func:ResolveProofRefs) | EXCLUDED | Operator pre-submit proof cleanup and binary object lifecycle. It can remove only unattached draft proof artifacts; submitted proof and verification visibility remain covered through `/vaccination/verification-queue`, `view:verification_queue_status`, and process-integrity proof-media resolver coverage. |
| GET /workflows/{row_id} | EXCLUDED | Row-level workflow detail |
| GET /protocols | api | Protocol/schedule definitions |
| GET /protocols/versions/{id}, /protocols/animal-stages | EXCLUDED | Protocol version / reference taxonomy |
| GET /app/config, /app/bootstrap, /app/me, /admin-web/bootstrap | EXCLUDED | Client/session bootstrap; RBAC/chrome, not data |
| GET /app/proofs/{id}/download(+/signed) | EXCLUDED | Binary proof retrieval |
| GET /healthz, /livez, /readyz, /version | EXCLUDED | Infra probes; no tenant scope, no business data |
| vaccination_completion_rejections | EXCLUDED | Per-completion rejection audit rows written when a verifier sends a vaccination proof back. Leadership reads the CONSEQUENCE, not the row: a rejected completion reopens its obligation and reappears through `view:verification_queue_status`. No new assistant read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| func:ResolveProofDownloadURL, func:WithProofURLResolver (weighing proof URL resolver) | EXCLUDED | Proof-media retrieval plumbing. Signed URL resolution at download time, part of the existing verification proof-media surfaces covered by `view:verification_queue_status` and `action_center_current`. No new assistant read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| func:GetWeightHistory, func:GetLeadershipGrowthADG (weighing read-model functions) | api | Leadership weighing historical trend and growth metrics. Covered by new read model functions that back leadership reporting. Admin-web Growth Dashboard reads through these functions; leadership assistant coverage through same endpoints + `ceo_ai.weighing_capture_activity` (migration 000080). No new Cube metric, `ceo_ai` view, or Toolbox tool beyond the function itself. |
| func:ExportCampaignCSV, func:ListParks (weighing campaign export) | api | Weighing campaign export functionality and park listing for planner. Planner/admin-web surfaces export campaign results to CSV for analysis. No new assistant tool, Cube metric, or `ceo_ai` view. |
| func:ReopenObligation, func:ReopenTaskForRework (obligation/weighing rework) | EXCLUDED | Write-path rework and obligation state helpers for verification rejection flow. Part of the existing verification verdict application; leadership sees result through `view:verification_queue_status`. No new assistant read API, Cube metric, or `ceo_ai` view. |
| func:ListExhausted, func:RequeueExhausted (outbox/event queue) | EXCLUDED | Operational kernel event delivery retry workers. Infrastructure-layer delivery-health instrumentation; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. Delivery health already covered by `ceo_ai.notification_delivery_health`. |
| func:DeviceIDFromContext, func:WithDeviceID (device context) | EXCLUDED | Device identity context plumbing for FCM and push notification routing. Internal middleware, not a leadership read surface. No new assistant tool, Cube metric, or `ceo_ai` view. |
| func:RetryableConflict, func:WithObligationCompleter, func:Error (error/helper types) | EXCLUDED | Internal error types and completeness helpers for obligation/verification workflows. Type definitions and interface plumbing, not leadership-facing reads. No new assistant tool, Cube metric, or `ceo_ai` view. |
| func:Write (miscellaneous write helper) | EXCLUDED | Internal write-path helper. Not a leadership read surface or API. |

## B. `ceo_ai.*` reporting views

Every view maps its grain + columns to real sources. Columns with no backing
source are typed `NULL::type -- TODO` placeholders (never silently dropped) and
tracked as gaps below.

| View | Status | Column gaps (typed NULL + TODO) |
|---|---|---|
| animal_current_scope | draft | — (all mapped; `partition_label` added migration 000110 — see "Partition-scoped reporting rule" below) |
| shed_capacity_current | draft | — (`partition_label` + per-partition occupancy rows added migration 000110; capacity/variance/status stay shed-grain, no per-partition capacity column exists anywhere in schema) |
| vaccination_shed_status | draft | planned_sessions (park-level batches → shed derivation approx). `partition_label` added migration 000114: `vaccination_eligibility_rollups` gained a partition column (recompute now groups by it), and obligation due/done/overdue counts are resolved per-partition via a join from `obligation_instances.target_id` (the goat_id for every vaccination obligation) to `goat_shed_partitions` — see "Partition-scoped reporting rule" below. |
| vaccination_dose_pickup | draft | vaccine_label (needs display-label mapping — gap G2). `partition_label` added migration 000114 by the same `obligation_instances.target_id` → `goat_shed_partitions` join; `doses_to_pick` stays batch-grain (no per-partition dose-reservation column exists anywhere in schema) and is repeated verbatim on every partition row of that batch/shed, never divided or guessed — see "Partition-scoped reporting rule" below. |
| vaccination_operator_status | draft | — (operator-grain drive load/capacity/overdue/utilization over `vaccination_drive_assignments`; migration 000026). `partition_label` added migration 000110 by grouping on the partition column already stored on `vaccination_drive_assignments` — see "Partition-scoped reporting rule" below. |
| vaccination_prearrival_history_review | draft | vaccine_label (raw protocol `vaccine_code` only at this grain — typed NULL + TODO; joining the published rule label would fan the trust buckets out per vaccine). Supplier pre-arrival vaccination-claim trust for PROCURED animals over `vaccination_prearrival_history_entries` (migration 000043); Toolbox tool `mesha_prearrival_history_review`; golden eval `prearrival-history-rejected-share` |
| feed_direction_current | draft | — (directive only; actuals → gap G7 feed_adherence) |
| counts_movement_daily | draft | transfers_out (derived from terminal exits — partial) |
| procurement_pipeline | draft | batch_label (no stored load label — gap) |
| source_entry_health_status | draft | load_label (no stored load label) |
| ops_exception_queue | draft | — (UNION across modules) |
| sop_execution_status | draft | — |
| verification_queue_status | draft | owner_label (always NULL — operator_id is sensitive, shed position holder not joined). `accepted` returned 0 unconditionally from the 000001 baseline until migration 000089: it filtered on status values the verification_items CHECK does not permit. Any accepted figure read before 000085 was broken, not empty. 000085 also appends `total` / `withdrawn` / `total_including_withdrawn` on 000080's rule — withdrawn rows are kept so an all-superseded scope still appears, and are excluded from `total` so pending + rejected + accepted = total. |
| inventory_stock_position | draft | reorder_flag (no threshold config — gap G1), last_reconciled_at (partial) |
| workforce_coverage_status | draft | — |
| action_center_current | draft | — (UNION) |
| audit_activity_summary | draft | result (no outcome column — derive from jsonb, partial) |

## C. Leadership-relevant tables → covering view

| Table | Covered by |
|---|---|
| goats, herd_register_summary_projection | animal_current_scope, counts_movement_daily |
| locations, park_profiles, shed_profiles, location_capacity_records | shed_capacity_current |
| vaccination_eligibility_rollups, obligation_instances, obligation_batches, vaccination_completions, vaccination_completion_rejections | vaccination_shed_status, vaccination_dose_pickup |
| vaccination_drive_assignments (operator-based drive model) | vaccination_operator_status (operator load / capacity / overdue / utilization) |
| obligation_escalations | ops_exception_queue, action_center_current |
| feed_direction_issues, feed_direction_issue_rows | feed_direction_current, ops_exception_queue |
| feed_direction_completions | gap G7 (feed_adherence) |
| feed_transport_tasks | EXCLUDED — operator daily shed task state; leadership sees pending evidence through verification_queue_status, not this execution queue |
| feed_transport_attempts | EXCLUDED — immutable row-level proof-attempt history; leadership sees aggregate verification backlog through verification_queue_status |
| shifting_events, shifting_event_impacts, counts_approval_requests, count_projection_* | counts_movement_daily, ops_exception_queue. High-priority feed proof refs, fingerprint, and requirement snapshot are EXCLUDED evidence/config detail; verification backlog remains covered by verification_queue_status. |
| goat_births | EXCLUDED — per-child canonical mother relationship and delivery litter size used by the operator birth workflow; leadership birth/mortality reporting remains on governed Counts aggregates |
| procurement_loads, procurement_load_goats, arrival_intake_reviews, procurement_source_health_checks, procurement_hf_vaccination_evidence | procurement_pipeline, source_entry_health_status |
| sop_tasks, sop_submissions, sop_task_submission_fanouts | sop_execution_status, ops_exception_queue |
| sop_task_scan_captures | EXCLUDED (operator proof capture ledger) — scan-level evidence for mobile SOP task submission. Leadership reads SOP execution/proof state through `sop_execution_status`, `verification_queue_status`, and `ops_exception_queue`; raw scan rows are per-task evidence, not a leadership aggregate. |
| sop_task_scan_attempts | EXCLUDED (operator scan attempt ledger) — accepted/duplicate/not-due/unknown scan attempts used to debug mobile field capture. Failures surface through SOP submission/verification/ops exception read models; raw attempts are operational telemetry, not a CEO KPI or standalone assistant read. |
| verification_items | verification_queue_status |
| inventory_items, inventory_stock, inventory_stock_movements | inventory_stock_position |
| workforce_members, workforce_positions, workforce_absences, workforce_roster_assignments, org_role_catalog | workforce_coverage_status |
| vaccination_drive_assignment_members | EXCLUDED (operational scheduler-written membership) — the exact obligation/goat set behind each `vaccination_drive_assignments` row. It exists so a death/sale/cull decrements the exact assignment arm and so CT/PA/WF/AC can report an animal's own operator-day instead of inferring it from an aggregate. Leadership never reads membership directly; it reads the drive/operator aggregates this table makes correct (`view:vaccination_operator_status`, `GET /vaccination/schedule`). |
| vaccination_prearrival_history_entries (migration 000041) | vaccination_prearrival_history_review — COVERED, not excluded. The accepted/rejected split on supplier-attested pre-arrival vaccination claims for PROCURED animals is a real leadership signal (trusted-history share, rejected-claim rate and reason = supplier data quality + avoided re-injection). Coverage: `ceo_ai.vaccination_prearrival_history_review` (migration 000043) → MCP Toolbox tool `mesha_prearrival_history_review` (in `mesha_ceo_toolset`) → read-only SQL fallback over the same view. No governed Cube metric yet: rejected-rate is not an official tracked KPI today, so this stays tier-3/4 (add a Cube metric if leadership starts trending it). Golden eval question: `prearrival-history-rejected-share` (`tools/ceo-ai/eval/golden/vaccination.json`). |
| weighing_campaigns, weighing_campaign_sheds, weighing_observations, weighing_shed_observations, weighing_work_items | `ceo_ai.weighing_capture_activity` + `ceo_ai.weighing_verification_status` (migration 000080) — COVERED; this no longer "graduates after the first reporting requirement". One row per weighing bucket (capture rollups + work-item state) and a park/shed verification rollup scoped to `module = 'weighing'`. Both views read weighing's own tables only: weighing is free-flow and fully herd-isolated, so they never join goats, goat_identifiers, herd_animals, protocol_rules, or any vaccination/clinical table. `GET /weighing/campaigns` remains the leadership planning/monitoring read API. |
| weighing_expected_animals | REMOVED — not a coverage gap. Dropped outright by migration `000079` when weighing became fully free-flow: a weighing scan has no expected animal set, so there is nothing left to report. Named here so the surface is explicitly closed rather than silently disappearing. See also `000078` (dropped `weighing_observations.animal_id`) and `tools/agent-hooks/check-weighing-free-flow-guard.mjs`. |
| audit_log | audit_activity_summary |
| breeds, animal_stage_lookup, vaccines, parties, vaccination_capacity_config | reference/config — EXCLUDED (support tables, surfaced via joins, not standalone leadership reads) |

## D. Gaps to close (decisions)

Each gap has an explicit decision: draft a view/metric, or a documented
scoped-refusal exclusion so the bot says "not covered yet" rather than inventing.

| Id | Gap | Decision |
|---|---|---|
| G1 | Inventory has no tier-2 read API; reorder threshold has no source | Draft `ceo_ai.inventory_stock_position` + MCP tool (tier-3); `reorder_flag` = typed NULL until a reorder-point config column exists. Document inventory as SQL/Toolbox-tier (no tier-2 API yet) in the routing contract |
| G2 | vaccine_label leaks raw codes | Add a shared vaccine display-label mapping (single source consumed by view + `ui-vaccine-labels` guard); guard test asserts no raw token (et_tt/ppr_booster/…) reaches an answer |
| G3 | No answer-grounding validator | Composer grounding validator: every number/label must map to a citation-backed tool result or be stripped/downgraded; first-class eval assertion |
| G4 | Bounded agent step loop not explicit | `backend/internal/ceoai/app/loop.go` honoring `MESHA_AI_MAX_STEPS` + per-step deadline + deterministic stop; eval asserts termination |
| G5 | mortality_rate KPI has no base view; DLQ/kernel-health surfaced only via ops queue | Draft `ceo_ai.mortality_base` + Cube `mortality_rate`; map DLQ/kernel-health into `ops_exception_queue` (done above) |
| G6 | Breeding/reproduction: columns exist, no read path | Scoped-refusal exclusion until a breeding workflow ships: bot answers "breeding pipeline not covered yet" (add refusal copy). Not a silent gap |
| G7 | Feed adherence (actuals vs directive) uncovered | Draft `ceo_ai.feed_adherence` joining directive rows to `feed_direction_completions` + a GenAI intent |
| G8 | Notification/escalation delivery health uncovered | CLOSED: `ceo_ai.notification_delivery_health` view built (migration 000020) + `mesha_notification_delivery_health` MCP Toolbox tool (tier-3) in the leadership toolset. No governed Cube metric — this is an operational delivery-reliability read served via Toolbox, not Cube |
| G9 | Scale certification of `ceo_ai.*` views | EXPLAIN-based query-plan proofs at ~500k-row seed wired into a `validate-ceo-views-plans` gate (planned; tracked in mcp-toolbox-plan) |
| G10 | Conversation thread titles unpopulated | Title-derivation on conversation create (planner summary or truncated first message) + rename path |
| G11 | Identity-resolution / data-quality backlog uncovered | Surface via `ops_exception_queue` (location-review + escalations); dedicated identity-conflict view deferred, documented here |
| G12 | Transit/holding, proof artifacts, device fleet | EXCLUDED for now: transit/holding deferred (draft when volume matters); proof artifacts surfaced via per-module "evidence present" derivation; device fleet is ops-admin telemetry, not a leadership KPI |
| G13 | API-tier tool executors (in-process adapters) | CLOSED: tier-2 in-process `ToolExecutor` adapters registered in `ceoai/adapters/readtools/` for planner-routed API tools (vaccination_shed_summary, vaccination_execution, feed_direction_today). Species counts routed to Cube tier (active_animal_count metric) instead of API. Executors propagate errors via `ToolResult.Err` instead of swallowing them; toolexecutors_test.go covers error-propagation + species-split assertions. Resolves `backend/internal/ceoai/app/registry.go:106` routing error + P0-critical error-swallowing bug. |

## Partition-scoped reporting rule (migration 000110, 2026-08-05)

OperationalLocation = park + physical shed + OPTIONAL partition
(`backend/internal/platform/oploc`). `goats.shed_id` is always the PARENT
physical shed; `goat_shed_partitions` (PK `(tenant_id, goat_id)`) carries the
actual sub-location for a goat that lives in a partitioned shed. Not every shed
has partitions — a non-partitioned shed renders as the bare shed name, never a
synthetic "whole" location; shed names repeat across parks, so grouping/
filtering must always key off `shed_id`, never the shed name alone.

**Leadership reporting rule: when a partition exists, a leadership answer must
never resolve at the parent-shed grain only.** "How many kids in Castro 1" must
answer Castro 1's count, not the whole-Castro total, whenever `goat_shed_partitions`
has rows for that shed. A view/tool that silently collapses every partition of a
shed into one shed-total row is a coverage defect, not an acceptable
approximation — that was exactly the bug migration 000110 fixed in four views
(`animal_current_scope`, `shed_capacity_current`, `counts_movement_daily`,
`vaccination_operator_status`): "at Castro 1" and "at Castro 2" both answered
with the Castro TOTAL before this migration.

**Fixed (migration 000110):**

- `ceo_ai.animal_current_scope` — `partition_label` added to the per-goat row
  (LEFT JOIN `goat_shed_partitions`, 1:{0,1} per goat, no fan-out).
- `ceo_ai.shed_capacity_current` — one additional row per (shed, partition)
  reporting occupancy scoped to that partition; the pre-existing bare-shed row
  is unchanged (still the whole-shed total). Capacity/variance/status stay
  shed-grain on every row (partition or bare) because no per-partition capacity
  column exists anywhere in the schema — `shed_profiles.capacity` is a
  whole-shed figure only.
- `ceo_ai.counts_movement_daily` — the birth/death branches (read `goats`
  per-animal) now carry `partition_label`. The transfer/shift/approval branches
  (read `shifting_event_impacts`, which is aggregated per `(event, breed,
  stage)`, NOT per goat) cannot honestly attribute a partition and continue to
  emit `partition_label = NULL`, rolling up at shed grain only — documented,
  not silently dropped.
- `ceo_ai.vaccination_operator_status` — GROUP BY now includes the
  `partition_label` column `vaccination_drive_assignments` already stores (it
  existed pre-migration but was ignored by the view's GROUP BY), so two
  partitions of one shed assigned to the same operator on the same day report
  as distinct rows instead of merging. The per-operator-per-day capacity/
  utilization window stays `PARTITION BY (tenant_id, operator_id, planned_date)`
  — unchanged — because capacity is a whole-day, cross-shed, cross-partition
  cap, never a per-partition one.
- `backend/internal/bootstrap/ceoai_readers.go` (`buildCountsReader`, backing
  the `counts_breakdown` tool) now accepts `partition_label` as a scoped param:
  rows are filtered to the named partition (`oploc.SamePartition`, tolerating
  both the numeric and "Part N" label conventions) and every scope string
  renders through `oploc.OperationalLocation.Display()`.

**Fixed (migration 000114 — closes the vaccination_shed_status /
vaccination_dose_pickup gap left open by migration 000111):**

- `vaccination_eligibility_rollups` (owned by this migration) gained a
  `partition_label` column; `Repository.RecomputeEligibilityRollup`
  (`backend/internal/vaccination/adapters/postgres/repository.go`) now groups
  by the normalized partition key (`NULLIF(gsp.partition_label,'whole')`
  sourced 1:{0,1} from `goat_shed_partitions`, PK `(tenant_id, goat_id)`) in the
  same delete+reinsert transaction it already ran. The grain-uniqueness index
  (`vaccination_eligibility_rollups_grain_uidx`) now includes the partition so
  two partitions of one shed can coexist as distinct rows.
- `ceo_ai.vaccination_shed_status` — one additional row per (shed, partition)
  attested by `goat_shed_partitions`; the pre-existing bare-shed row is
  unchanged (still the whole-shed total). `animals` comes from the now
  partition-aware rollup; `due`/`done`/`overdue`/`planned_sessions` are
  resolved per-partition by joining `obligation_instances.target_id` (the
  goat_id — a vaccination obligation's `target_type` is always `'goat'`) to
  `goat_shed_partitions`, 1:{0,1} per goat, so the added join cannot fan out
  the `COUNT/FILTER` aggregates.
- `ceo_ai.vaccination_dose_pickup` — same additive per-partition rows via the
  same `target_id` → `goat_shed_partitions` join, scoping `animals_due` and
  `animals_overdue`. `doses_to_pick` is a whole-batch dose reservation
  (`obligation_batches.reserved_quantity`) with no per-partition column
  anywhere in the schema, so a partition row repeats the SAME
  `doses_to_pick` as its parent batch/shed row — mirrors
  `ceo_ai.shed_capacity_current`'s shed-grain-only `capacity` column; never
  divided or guessed per partition.
- `docs/ceo-ai/mcp-toolbox-tools.yaml` — `mesha_vaccination_due_summary` and
  `mesha_vaccination_dose_pickup` now select `partition_label` and accept it as
  an optional filter param, matched with the same normalized comparison as
  `oploc.SamePartition` (`'Part 3'` == `'3'`; NULL/''/'whole' all mean
  "not partitioned").

**Leadership surfaces that MUST carry partition labels (when one exists):**

Every leadership-visible answer about animal/shed location, counts, vaccination,
shifting, or work status must carry `partition_label` in the response payload,
in scope params, in tool names, and in rendered text when a partition exists.
Affected surfaces:

- Counts Breakdown (Cube metric + tool) — filters/groups by partition
- Vaccination Shed Status (tool) — per-partition animal/obligation counts
- Vaccination Dose Pickup (tool) — per-partition animals due/overdue; doses_to_pick stays batch-grain
- Vaccination Operator Status (view) — operator-date rows per partition
- Animal Current Scope (view) — per-goat partition label
- Shed Capacity (view) — per-partition rows alongside shed total
- Counts Movement Daily (view) — birth/death partition labels
- Any leadership drilldown or detail drawer showing "animals at Shed X"

A CEO tool/query/report that answers "X animals/doses/actions at [ShedName]"
without checking `goat_shed_partitions` for that shed is incomplete.

There is no remaining "known gap" in this section: migration 000114 closed the
`vaccination_shed_status` / `vaccination_dose_pickup` partition-grain gap that
migration 000111 had left open (see "Fixed (migration 000114)" above). Any
future leadership-relevant table/view that is shed-grain-only must be treated
as a new gap and recorded here, not silently assumed covered by this
migration's fix.

## E. Rule

Nothing may be leadership-relevant AND uncovered AND undocumented. A new
table/API/feature is incomplete until it appears in this matrix as a coverage
path or an explicit exclusion, and the `leadership-assistant-coverage-guard`
passes. Use `node tools/ceo-ai/scaffold-coverage.mjs <module>` to generate the
stubs and this row.

## Cube governed-metric source views (migration 000030)

The Cube semantic layer connects to Postgres as the `mesha_cube_readonly` role,
which has SELECT on `ceo_ai.*` only (`public` is revoked). Cube models therefore
MUST read `ceo_ai.*` views, never raw `public` tables — otherwise every governed
metric fails with `permission denied for table ...` and the assistant returns
`cube: could not be retrieved.` (see `docs/runbooks/cube-local.md`).

Migration `000030_ceo_ai_cube_source_views.sql` adds the thin per-cube source
views below (grain/columns identical to the cube models they back), completing
the Cube tier-1 read path for these KPIs:

| Cube (tier-1 metrics) | ceo_ai source view (tier-2) | Canonical source |
|---|---|---|
| `kpi_vaccination.*` (due/overdue/due_today/completed/compliance) | `ceo_ai.vaccination_obligations_base` | `obligation_instances` |
| `kpi_animals.*` (active/total/mortality) | `ceo_ai.animals_base` | `goats` |
| `kpi_feed.*` (feed quantity/cost draft) | `ceo_ai.feed_completions_base` | `feed_direction_completions` |
| `kpi_procurement.*` (procurement animals/cost draft) | `ceo_ai.procurement_loads_base` | `procurement_loads` |
| `kpi_workforce.*` (task count/completion draft) | `ceo_ai.workforce_tasks_base` | `sop_tasks` |

`kpi_vaccination_operator.*` was already repointed to
`ceo_ai.vaccination_operator_status` in migration 000027. No new leadership KPI,
table, or exclusion is introduced by 000030 — it is a read-path plumbing fix so
the existing governed metrics resolve under the read-only Cube role.

## Assistant DB roles: full read on public (maintainer decision 2026-07-23)

The leadership assistant is internal, CEO/CXO-only, and READ-ONLY. Its two DB
roles (`mesha_cube_readonly` for Cube, `mesha_ceo_readonly` for MCP Toolbox /
SQL fallback) are granted SELECT on ALL current tables in `public` plus
`ceo_ai.*`, and — for every current object owner the grant admin can cover —
future objects those owners create (`ALTER DEFAULT PRIVILEGES ... ON TABLES`
also covers future views). Applied via migration
`000031_assistant_roles_public_read.sql` and
`tools/dev/setup-ceo-ai-local-role.sh`. The migration is guarded (`IF EXISTS`)
so it only grants roles that already exist; in stg/prod the roles are created as
a separate provisioning step, so after creating them run
`make grant-assistant-public-read` (idempotent) to guarantee the grant lands.
`make grant-assistant-public-read` sets `ALTER DEFAULT PRIVILEGES FOR ROLE
<owner>` for every current owner discovered across tables, views, and
materialized views, so future objects created by those owners are covered too.
Future coverage is therefore per-covered-owner, not blanket: a brand-new object
owner (or an owner the grant admin is not a member of) is not covered until the
grant is re-run with sufficient privileges, and the script now FAILS non-zero on
any missing role or skipped owner unless `ALLOW_PARTIAL=1` is set.
This removes the prior "ceo_ai.* only / public revoked" restriction so current
Cube models and read queries — and future ones over covered owners' tables — do
not fail with `permission denied`. Access stays read-only (SELECT only +
`default_transaction_read_only=on`; no write/DDL). This supersedes the "Cube
never reads raw Postgres" boundary for these two read-only roles.

## Explicit exclusion: vaccination proof/label/withdrawal internal fixes (2026-07-23)

The shed-proof scope-recovery fix (`CaptureRepository.kt`), the withdrawal-until
IST date fix and the dose-label cleanup
(`backend/internal/vaccination/adapters/postgres/repository.go`,
`backend/internal/vaccinationexecution/domain/labels.go`) are internal
correctness fixes to existing vaccination paths. They add NO new leadership KPI,
table, read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool — the
leadership assistant read surface is unchanged. No coverage-matrix mapping is
required; this is an explicit documented exclusion.

## Explicit exclusion: weighing verification enqueue bridge (2026-07-30)

The weighing verification bridge
(`backend/internal/weighing/adapters/verificationbridge/enqueue.go`) and its
service wiring functions (`func:New`, `func:EnqueueWeighingVerification`,
`func:WithVerificationEnqueuer`) only enqueue already-captured weighing proof
videos into the existing Verification workstream. They add no new CEO assistant
read API, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or leadership KPI.
Leadership visibility remains through the existing weighing monitor/video APIs
and verification surfaces, so this is an explicit documented exclusion.

## Explicit exclusion: counts census lifecycle facet + Android UI modernization (2026-07-23)

`GET /counts/breakdown` (row 35 above, already `api + view:animal_current_scope`)
gained one additional facet dimension — `CountsBreakdownFacets.lifecycle`, the
distinct `lifecycle_status` vocabulary present in the whole tenant herd
(`backend/internal/counts/adapters/postgres/repository.go`,
`backend/internal/counts/domain/types.go`,
`contracts/openapi/app-api.yaml`, `packages/api-client/src/generated/app-api.ts`).
This is an additive facet on an ALREADY-covered read API/contract: it lets the
mobile census filter sheet offer Live/Sold/Culled/Dead/Transferred instead of
only ever showing the live herd, but it exposes no new table, no new read API
route, no new Cube metric, and no new `ceo_ai.*` view — the underlying
`lifecycle_status` counts were already readable through
`GET /herd-register/summary` (row 34, `api + Cube:active_animals`), which this
change does not touch.

The accompanying Android changes (`apps/goatos-android/feature/feature-counts/**`,
`apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/CountsViewModel.kt`,
`apps/goatos-android/core/core-network/**/CountsDto.kt`) are a mobile UI
restyle of the four existing Counts screens (census hero, filter bottom sheet,
shed subtotals, animal hero, M3 date picker, softened approval cards) — layout
and interaction only, rendering the same backend-owned contract. No new
leadership-relevant surface, KPI, or workflow was introduced. No
coverage-matrix mapping is required beyond this note; this is an explicit
documented exclusion.

## Explicit exclusion: operator remaining-cap + partial-attach ledger fixes (2026-07-23)

The operator remaining-cap fix (`backend/internal/obligation/adapters/postgres/visit_shot_lock.go`,
`backend/internal/obligation/app/sweeper.go`) and the partial-attach drive-assignment
scoping fix are internal vaccination sweeper/planner correctness fixes. The partial-attach
fix adds one new internal repository write, `func:ReplaceVaccinationDriveAssignmentsForBatch`
(`backend/internal/obligation/adapters/postgres/visit_shot_lock.go`), which replaces the
scoped `(tenant, batch)` drive-assignment set so no stale row survives a partial attach.
It is an internal write on the obligation sweeper path, not a leadership read surface. They add NO
new leadership KPI, table, read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox
tool — the leadership assistant read surface is unchanged. Explicit documented
exclusion; no coverage-matrix mapping required.

## Explicit exclusion: counts lifecycle facet display-label + shed-subtotal follow-up (2026-07-23)

Follow-up fixing two P2 review findings on the already-covered
`GET /counts/breakdown` read API. (1) The lifecycle facet now maps raw
`lifecycle_status` tokens to human display labels (alive→"Live", sold→"Sold",
dead→"Dead", culled→"Culled", etc.) in `series_label` while `series_key` stays
the raw token — a presentation-only change to the existing facet contract.
(2) The Android census carries the full (park-unnarrowed) shed facet in a
`shedSubtotals` display field so the per-shed subtotal renders on the default
all-parks view. Neither adds a new table, read API route, Cube metric,
`ceo_ai.*` view, or MCP Toolbox tool; the leadership assistant read surface,
read-only SQL fallback, and tool catalog are unchanged. Explicit documented
exclusion — no coverage-matrix mapping required.

## Explicit exclusion: vaccine display-label helpers (ceo_ai boundary fix, 2026-07-23)

The ceo_ai reporting-boundary fix (`docs/decisions/ceo-ai-reporting-boundary.md`)
moves vaccine display-label composition out of the SQL `ceo_ai.vaccine_label_for()`
reporting function into core Go, so the Control Tower / process-integrity read
path no longer depends on the leadership-assistant reporting schema. It adds two
new internal display-label helpers:

- `func:DoseDisplayLabel` (`backend/internal/vaccination/domain/vaccinelabels.go`)
  — the canonical antigen display label, delegated to by
  `vaccinationexecution/domain.VaccinationDoseDisplayLabel`.
- `func:DoseQualifiedDisplayLabel`
  (`backend/internal/vaccination/domain/vaccinelabels.go`) — the same canonical
  antigen label plus the dose position within its course ("ET+TT · Dose 2"),
  used by per-dose-grain surfaces (the vaccination command board shed × dose
  matrix and verification queue). Display-mapper helper only — no new data
  surface; assistant coverage rides the existing `GET /vaccination/command`
  coverage row.
- `func:ControlTowerDoseLabel`
  (`backend/internal/processintegrity/domain/vaccinelabels.go`) — composes that
  base with Control Tower course/dose/booster qualifiers.

Both are pure presentation helpers turning an internal dose code into UI copy.
They add NO new leadership KPI, table, read API, Cube metric, `ceo_ai.*` view, or
MCP Toolbox tool; the `dose_code` on-the-wire contract is unchanged (it already
carried the label). This change REMOVES a `ceo_ai.*` dependency from a core read
path rather than adding a leadership surface. Explicit documented exclusion — no
coverage-matrix mapping required.

## Explicit exclusion: vaccination operator shift + assignment config (admin config, 2026-07-23)

Adds two new tables (`vaccination_operator_assignment_config`,
`vaccination_operator_shift_config`, migration 000035) plus
`GET/PUT /vaccination/operator-assignment/config`. This is admin-only
authoring config (CPT/Channapatna operator shift + N-active-operators-per-day
default), analogous to the already-excluded `vaccination_capacity_config` admin
Config screen surface — it is not a leadership KPI, business outcome metric, or
reporting dimension. The drive/obligation scheduler consumes it for daily
operator assignment, but no new leadership-relevant table, official KPI, or
reporting view is introduced. Explicit documented exclusion — no coverage-matrix
mapping required.

2026-07-25 follow-up: the same `table:vaccination_operator_assignment_config`
admin config gained `selected_operator_ids` plus
`func:ReassignPlannedDrives` so saving the People / HRMS operator setting can
immediately reassign current/future open planned drive assignment rows to the
selected operators. This is still admin-only authoring plumbing on the existing
vaccination execution/schedule APIs. It adds NO leadership KPI, read API route,
Cube metric, `ceo_ai.*` view, or MCP Toolbox tool; completed scans/proofs remain
untouched. Explicit documented exclusion — no coverage-matrix mapping required.

| vaccination_operator_assignment_config | func:ReassignPlannedDrives | Explicit exclusion: admin-only operator assignment config write/reassignment; existing vaccination execution/schedule reads remain the covered user-visible source. |

## Explicit exclusion: one-time vaccination drive recompute (ops tool, 2026-07-23)

`func:RecomputeFutureVaccinationDrives`
(`backend/internal/obligation/adapters/postgres/operator_recompute.go`) and the
`recompute-vaccination-drives` CLI are an internal one-time maintenance operation
that releases future `planned` vaccination drive batches so the sweeper re-plans
them under the current operator-assignment config. They add NO leadership KPI,
read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool — the leadership
assistant read surface is unchanged. Explicit documented exclusion.

## Explicit exclusion: operator-config auto-cascade consumer (2026-07-23)

The vaccination operator-config auto-cascade — new table
`obligation_operator_config_replan_watermarks` (migration 000036, idempotency
watermark only) and `func:ClaimOperatorConfigReplanWatermark`,
`func:ParkIDForShed`, `func:NewOperatorConfigReplanHandler`, `func:Register`,
`func:HandleEvent`, `func:WithBus` (`backend/internal/obligation/app/operator_config_replan.go`)
— is an internal durable-consumer that, on vaccination.capacity/roster/leave.changed,
re-plans future vaccination drive batches. It adds NO leadership KPI, read API, Cube
metric, `ceo_ai.*` view, or MCP Toolbox tool; the leadership assistant read surface is
unchanged. Explicit documented exclusion.

## Explicit exclusion: operator-config cascade watermark helpers (2026-07-23)

The operator-config auto-cascade durability fix
(`backend/internal/obligation/adapters/postgres/operator_recompute.go`) adds three
internal two-phase idempotency-watermark helpers on the cascade consumer path:

- `func:ClaimOperatorConfigReplanWatermarkPending` — claims the per-event watermark
  in the `pending` state before recompute runs.
- `func:MarkOperatorConfigReplanWatermarkSucceeded` — promotes the watermark to
  `succeeded` after the recompute + batch supersede commit.
- `func:GetOperatorConfigReplanWatermarkStatus` — reads the watermark state so a
  redelivered event retries a `pending` (failed) attempt and no-ops a `succeeded` one.

All three are internal outbox-consumer idempotency plumbing for the
`vaccination.capacity.changed` / `vaccination.leave.changed` cascade. They add NO
new leadership KPI, table, read API route, Cube metric, `ceo_ai.*` view, or MCP
Toolbox tool; the leadership assistant read surface, read-only SQL fallback, and
tool catalog are unchanged. Explicit documented exclusion — no coverage-matrix
mapping required.

## Explicit exclusion: vaccination scan draft + shed-readiness helpers (2026-07-25)

`func:RecordScanCapture` persists per-tap draft scan rows for the mobile operator
outbox, `func:ShedCompletionReadiness` gates whether a shed-level submission has
the expected scans/proofs before it can enter verification, and
`func:CompletedTaskProofRefs` recovers completed server proof refs for that same
operator submit path when the mobile cache no longer carries the local proof row.
All three are internal write/readiness helpers on the existing vaccination SOP
execution path. They add NO new leadership KPI, table, read API route, Cube
metric, `ceo_ai.*` view, or MCP Toolbox tool; the leadership assistant read
surface remains the existing vaccination execution/process-integrity coverage.
Explicit documented exclusion — no coverage-matrix mapping required.

## Explicit exclusion: vaccination drive safe-date override internals (2026-08-04)

`table:vaccination_drive_date_overrides` now stores requested/applied move
metadata (`requested_override_date`, `shift_reason`,
`clinical_shift_metadata`) for the existing vaccination drive move command.
`func:ApprovedVaccineComboSessions` and `func:VaccinesShareApprovedCombo` are
internal clinical scheduling helpers used to decide same-day combo exceptions
for the write path. These surfaces add NO new leadership KPI, read API route,
Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. Leadership assistant
coverage remains the existing vaccination schedule, drive-assignment, Action
Center, Protocol Adherence, and Control Tower read surfaces, which already read
the effective active override state. Explicit documented exclusion — no
coverage-matrix mapping required.

## Explicit exclusion: proof artifact retention lifecycle plumbing (2026-07-25)

`func:NewProofRetentionSweeperStage`, `func:Name`, and `func:Run` add hourly
housekeeping for expired proof-artifact metadata, missed submitted-SOP retention
stamps, and abandoned pending/uploading proof rows. `func:ApplyRetention`,
`func:BackfillSubmissionRetention`, `func:PurgeExpired`,
`func:PurgeAbandonedUploads`, and `func:ApplyRetentionPolicy` are internal proof
repository/application helpers that attach or repair SOP proof-policy retention
windows and delete expired runtime proof rows. Migration
`000046_proof_artifact_retention` adds only retention/upload-expiry metadata and
indexes on `proof_artifacts`; those columns are lifecycle plumbing, not a CEO
chat answer source. They add NO leadership KPI, table, read API route, Cube
metric, `ceo_ai.*` view, or MCP Toolbox tool; the leadership assistant read
surface remains unchanged. Physical media deletion remains owned by object-store
lifecycle configuration, not a leadership assistant read path. Explicit
documented exclusion — no coverage-matrix mapping required.

## Pre-arrival vaccination history: covered table + excluded write/repair surfaces (2026-07-24)

The pre-arrival supplier vaccination-history change (migration 000041) and the
`goat.created` recovery change land together. They split cleanly into ONE covered
leadership surface and a set of write-path / internal-repair / vocabulary
surfaces that are excluded for stated, checkable reasons.

**COVERED — `table:vaccination_prearrival_history_entries`.** See section C: the
accepted-vs-rejected split on supplier-attested pre-arrival vaccination claims is
a genuine leadership question ("how many procured animals arrived with trusted
vaccination history", "what share of supplier claims did we reject and why").
Coverage artifact: `ceo_ai.vaccination_prearrival_history_review` view
(migration 000043, tenant-scoped, IST review day, accepted and rejected buckets
never collapsed, rejection_reason preserved) → MCP Toolbox tool
`mesha_prearrival_history_review` registered in `mesha_ceo_toolset` → read-only
SQL fallback over the same view. Golden eval question:
`prearrival-history-rejected-share`.

**EXCLUDED — the writer for that table (WRITE path, not a leadership read).**

- `func:IngestPreArrivalHistory`
  (`backend/internal/vaccination/adapters/postgres/prearrival_history.go`) —
  the idempotent INSERT that persists an accepted or rejected claim. It is a
  mutation entry point; the leadership assistant is read-only and refuses every
  write. Leadership reads its OUTPUT through the covered view above, never this
  function.
- `func:WithPreArrivalHistoryWriter`
  (`backend/internal/vaccination/app/generation.go`) — a constructor option that
  injects that writer into `GenerationService`. Dependency wiring on the same
  write path; it exposes no data and no read route.

**EXCLUDED — `goat.created` recovery (internal repair machinery, not a KPI).**

The `goatcreatedrecovery` package (`backend/internal/identity/goatcreatedrecovery/recovery.go`)
and its kernel-worker stage (`backend/internal/kernelstages/goat_created_recovery.go`)
detect goats whose `goat.created` domain event was lost and re-emit it so the
downstream projections/obligations converge:

- `func:ScanCandidates` — finds goats missing the event.
- `func:Recover` — re-emits the missing events.
- `func:BackfillOne` — repairs a single goat transactionally.
- `func:NewGoatCreatedRecoveryStage`, `func:Name`, `func:Run` — the worker-stage
  constructor and the `Stage` interface methods that schedule the sweep.

Reason: this is self-healing plumbing for an internal event-delivery defect. It
adds NO business fact — it restores facts that already exist — so a leadership
answer computed before and after a repair differs only by the underlying data
being correct, which the already-covered census/vaccination views report. A
repair COUNT is at most an ops-health signal, and Goat OS already routes
ops-health/kernel-health (DLQ, escalations, stuck work) through
`ceo_ai.ops_exception_queue` / `action_center_current`; adding a per-repair
leadership metric would put internal defect telemetry into the leadership KPI
matrix, which section D/G12 explicitly rejects (device fleet / ops-admin
telemetry is not a leadership KPI). No new table, read API, Cube metric,
`ceo_ai.*` view, or Toolbox tool.

**EXCLUDED — `func:AuthorizedParkOptions`**
(`backend/internal/vaccinationexecution/app/service.go`) — returns the list of
parks the calling user is permitted to choose from, for an admin-web selector.
It is a scope/vocabulary helper derived from the caller's grants: no counts, no
business measure, no time dimension, nothing to trend or compare. It is the same
category as the already-excluded `GET /app/counts/shifting/destinations`
operator picker. The leadership assistant derives park scope from the
server-side session, never from a UI picker endpoint.

**EXCLUDED — BUG-041 obligation-engine drive-assignment rebuild internals**
(`backend/internal/obligation/...`) — `UpdateBatchPlannedDate`,
`mergeUnfinalizedBatchIntoPlannedDate`'s target return, `DriveRebuildInputsForBatch`,
`AvailableVaccinationOperatorsForDriveExcludingBatch`,
`RebuildMergedBatchDriveAssignments`, and the `SweepSession` rebuild-outcome
accessors `DriveRebuilds` / `Rebuilt` are internal sweeper/scheduler mechanics for
rebuilding a merged vaccination drive batch's operator assignments. They produce no
counts, no KPI, no time-series, and expose no read API, table, `ceo_ai.*` view,
Cube metric, or Toolbox tool. Leadership drive/coverage answers stay on the
existing aggregate `/vaccination/*` surfaces and `ceo_ai.*` views; these functions
only keep the operator drive sheets internally consistent after a combo-align
merge. Same category as the already-excluded operator execution/scan internals.

**EXCLUDED — `func:SyncPartitionMoveForGoat`**
(`backend/internal/obligation/adapters/postgres/partition_move.go`) — a repository
transactional primitive that, on a same-shed partition move, updates
`goat_shed_partitions.partition_label` and re-syncs the goat's vaccination
drive-assignment membership to the new partition arm. It is operator/scheduler
plumbing: no counts, no KPI, no time-series, no read API, table, `ceo_ai.*` view,
Cube metric, or Toolbox tool. Leadership drive/coverage answers stay on the
existing aggregate `/vaccination/*` surfaces; this only keeps operator drive
sheets internally consistent after a within-shed partition move.

**EXCLUDED — `func:VaccinationExecutionCarrySummary`**
(`backend/internal/vaccinationexecution/adapters/postgres/repository.go`) — computes
the per-operator, per-business-day "vaccines to carry" total (remaining doses by
vaccine over that operator's own sheds for the day) served ONLY on the operator
mobile execution response (`OperatorScopeActorID` set); admin/leadership reads get
`nil`. It is operator field-work chrome — how many doses one operator packs for one
day — not a leadership KPI, time-series, or tenant/park rollup. Same category as the
already-excluded operator execution/scan internals; leadership drive/coverage answers
stay on the existing aggregate `/vaccination/*` surfaces and `ceo_ai.*` views. No
`ceo_ai.*` view, Cube metric, or Toolbox tool is warranted.

**EXCLUDED — `table:goat_births` / `POST /app/counts/birth-events` birth-form metadata**
— `goat_births` stores one child's canonical mother relationship and delivery
litter size; the app endpoint captures required breed, mother RFID, and litter
choice. These are operator-entry and per-animal detail facts, not an official
leadership KPI or aggregate read surface. Leadership birth/mortality answers
remain on the existing governed Counts aggregates; no `ceo_ai.*` view, Cube
metric, or Toolbox row-level tool is warranted.

**EXCLUDED — `GET /counts/milk-preparation` current preparation instruction (2026-07-29).**

This endpoint and `/counts/milk-preparation` admin-web page are a current-day operator preparation
direction at physical shed x K1/K2/K3 cohort grain. Park-day preparation and immutable step-video
attempts are now durable, but remain operational evidence rather than an approved leadership KPI,
cost, trend, adherence metric, or feeding outcome. Leadership sees the aggregate verifier backlog
through existing `verification_queue_status`; raw `milk_preparation_completions` and
`milk_preparation_proof_attempts` are excluded. Leadership animal counts remain on existing
Counts/Cube coverage until a separately approved prepared/fed/refusal KPI contract exists.

## Explicit exclusion: weighing verification bridge and lifecycle consumer (2026-07-31)

The weighing verification bridge writes newly-captured animals' observations into
the existing Verification workstream. The following are Phase 1 implementation
internals for the weighing-to-verification integration and operator lifecycle
event handling. Leadership visibility on weighing state remains through the
existing `GET /weighing/campaigns` read API and its shed/campaign status surfaces.

**EXCLUDED — `func:RegisterVerificationAppliers`** — internal registry function
that wires weighing verification appliers into the verification consumer. It is
plumbing on the write path with no leadership read API, KPI, Cube metric, `ceo_ai`
view, or Toolbox tool.

**EXCLUDED — `func:NewWeighingLifecycleEventConsumer`** — internal constructor
for the weighing lifecycle event consumer. It is write-path consumer factory code
with no leadership read surface.

**EXCLUDED — `func:Register`** (on the weighing lifecycle event consumer) —
internal Stage interface implementation to register the consumer into the kernel
worker. It is scheduler/kernel plumbing, not a leadership read.

**EXCLUDED — `func:HandleEvent`** (on the weighing lifecycle event consumer) —
internal consumer event handler that processes weighing state transitions. It is
write-path event consumption with no leadership-facing read API, Cube metric,
`ceo_ai` view, or Toolbox tool.

**EXCLUDED — `func:CloseScope`** (on the weighing verification verdict handler) —
internal helper that closes a weighing campaign/shed scope after a verifier
approval/reject. It is part of the verification write path with no leadership
aggregate read surface.

**EXCLUDED — `func:CloseCampaign`** (on the weighing verification verdict
handler) — internal helper that marks a weighing campaign as complete when all
sheds are done. It is verification consumer plumbing with no leadership KPI.

**EXCLUDED — `func:ApplyVerificationVerdict`** (on the weighing verification
verdict handler) — internal function that applies a verifier approval or reject
to a weighing observation. It is verification write-path application logic, not a
leadership read. Leadership sees the result (campaign/shed status, progress) through
the existing `GET /weighing/campaigns` API.

**EXCLUDED — `func:NewVerificationVerdictHandler`** (on the weighing verification
verdict handler) — internal constructor for the verification verdict consumer
listening to `verification.verdict.approved`/`.rework` events scoped to weighing.
It is consumer factory code with no leadership read surface.

A future Phase 2 Calendar/Control Tower binding may add leadership views; that
will graduate some surfaces from excluded to covered. For now, the weighing write
path and verification consumer are Phase 1 operational internals covered only by
their existing operational surface read APIs (`GET /weighing/campaigns`).

**EXCLUDED — `func:NewLocationNameResolver`, `func:ResolveNames`,
`func:WithLocationNames`** (notificationbridge) — a batched park/shed
name lookup used only to render human place names into push notification copy
(2026-08-02 maintainer rule: a push must name the park, shed, vaccine and date,
never a bare count — see
`docs/decisions/2026-08-02-meaningful-notification-copy.md`). It reads
`locations.name` and returns display strings to the notification builder; it
exposes no aggregate, no metric, and no leadership-facing read. Leadership sees
the same places through the existing operational read APIs.

**EXCLUDED — `func:ReactivateWorkItemsForBucket`** (weighing kernel) — a
single indexed UPDATE that returns a weighing work item to `scheduled` when its
bucket leaves a terminal status via rework or reopen (defect B09: reopen left
kernel work terminal, so Calendar and Control Tower kept reporting finished
work). It is a write-path state-transition helper called inside the owning
transaction, not a read surface. Leadership continues to read weighing progress
through `GET /weighing/campaigns` and the Control Tower process-state summary.

**EXCLUDED — `func:NewVaccineLabelResolver`, `func:ResolveVaccineLabels`,
`func:WithVaccineLabels`, `func:WithLocationNames`** (notificationbridge) —
batched display-label lookups used only to render human vaccine names
("ET+TT", "PPR · Booster") and park/shed names into push notification copy,
per the 2026-08-02 meaningful-notification rule
(`docs/decisions/2026-08-02-meaningful-notification-copy.md`) and the
`ui-vaccine-labels-guard` ban on raw config tokens in user-facing text. They
read existing label/location rows and return display strings to the
notification builder — one batched query per event, never per recipient. No
aggregate, no metric, no leadership-facing read: leadership sees the same
vaccines and places through the existing vaccination and weighing read APIs.

**EXCLUDED — `func:ListAlerts`** (verification HTTP adapter) — the verifier's
per-feature alerts list, `GET /verify/alerts?category=<verification category>`.
It returns the pending verification items of ONE module (vaccination, weighing,
feed, counts) for the verifier who must action them, so the drawer's per-feature
Alerts tab shows that feature's own work (maintainer decision 2026-08-02:
"alerts per feature wise", role-specific). It is an operator-facing worklist
scoped to a single role, not an aggregate or KPI: same rows, same grain, and the
same `verification_items` source the existing verification queue already serves.
Leadership continues to see verification health through the module read APIs and
the Control Tower process-state summary, not through this endpoint.

**EXCLUDED — `table:weighing_repair_batch_progress`** (migration 000090) —
bookkeeping for the batched weighing data repairs. It records how far a one-time
repair procedure has drained so an interrupted run can resume; it holds no
business fact, no herd or weighing measurement, and nothing reads it for
correctness. A leader has no question this table answers. The weighing facts it
protects are already covered by `ceo_ai.weighing_capture_activity` and
`ceo_ai.weighing_verification_status` (000080).

**EXCLUDED — `func:ResolveAuthorizedParkScopeForCapabilities`,
`func:HasTenantWideCapability`** (platform HTTP middleware),
**`func:HasTenantWideAuthority`, `func:AuthorizedParks`**
(vaccination-execution domain) — authorization primitives. They answer "which
parks may this actor exercise this capability in", which is a question about the
CALLER, not about the herd. They carry no metric and expose no new data: every
one of them can only ever NARROW what an already-covered read API returns. They
exist because the capability-blind versions let an actor combine an unrelated
park grant with a capability-carrying grant scoped to a different park. Making
them assistant-visible would be a category error.

**EXCLUDED — `func:ResolveVaccineLabelsForTask`** (notification bridge) — a copy
resolver. It turns the sop task a push payload actually carries into that dose's
display label ("ET+TT") so a vaccination notification stops degrading to generic
text. It is presentation for a push notification, reading protocol rules the
assistant already reaches through the vaccination read APIs. It adds no fact.

**EXCLUDED — `func:ListLeadershipSheds`** (weighing app service) — the
leadership weighing gallery, one page of buckets with their proof videos for a
Growth Director reviewing operator capture. It is a paged, evidence-level
worklist at bucket grain, scoped to the actor's monitor parks — the same shape
as the verifier alerts list excluded above, and the same reason: leadership
aggregates for weighing are served by `ceo_ai.weighing_capture_activity` and
`ceo_ai.weighing_verification_status`, which answer "how many animals were
weighed, where, and how much verification is outstanding" without paging
per-bucket video evidence.

**EXCLUDED — `weighing_work_items.closed_reason`,
`weighing_work_items.merged_into_work_item_id`,
`event:weighing.work_item.merged_on_carry_over`,
`func:SweepWorkItems` carry-over merge pass** (weighing kernel, migration
000086) — the carry-over collision rule (maintainer decision 2026-08-03): when
unfinished weighing rolls onto a day where another task already covers that
(park, shed), the carried-over work item is CLOSED and linked to the surviving
one, so two people can never owe the same shed on the same real day. Nothing is
re-assigned and no capture moves.

These are write-path state-transition columns and one cadence event consumed by
the notification bridge, inside the owning transaction — not a read surface, no
aggregate, no metric, no new KPI. `closed_reason` is a machine token
(`merged_on_carry_over`); the farm-readable sentence is composed by the
notification consumer. Leadership continues to read weighing progress through
`GET /weighing/campaigns` and the Control Tower process-state summary, whose
numbers are unchanged: a merged carry-over leaves exactly one open work item for
that shed-day, which is what those surfaces already count.

**EXCLUDED — `route:/vaccination/alerts`** (Android navigation) — the
vaccination alerts feed's address, moved off the generic `/alerts` so every
feature's alerts are addressed by the feature that owns them
(`docs/decisions/module-alerts-tab.md`). Same feed, same rows, same
`notification_requests` source and the same per-recipient scoping as before;
only the route string changed, and the old generic route was deleted rather than
aliased. It is an operator/leadership worklist for ONE feature, not an aggregate:
leadership sees vaccination health through the existing vaccination read APIs and
the Control Tower summary.

**EXCLUDED — `func:Error`, `func:Unwrap` on `FinishedShedConflict`** (weighing
ports) — the two error-interface methods of the typed refusal that blocks MOVING
a weighing task which already holds weighed sheds. A weighed bucket records the
day the work actually happened and its proof hangs off that day, so the task
cannot take that day with it; the refusal names the sheds so the planner can act
(maintainer priority 2026-08-04).

They render an error string and unwrap to `ErrFinishedShedBlocksReschedule` —
no query, no aggregate, no metric, no read surface, and nothing a leader can ask
a question about. It is the same shape as the existing `ShedScheduleConflict`,
which is already excluded above for the same reason. Leadership continues to see
weighing progress through `GET /weighing/campaigns` and the Control Tower
process-state summary; a refused edit writes nothing, so those numbers are
unchanged by definition.

**EXCLUDED — `func:AnimalProofWasRejected`** (weighing postgres adapter) — a
boolean guard read asking whether a proof is already attached to an observation a
verifier sent back, so re-recording an animal cannot reuse the very video that was
rejected. It DOES read weighing_observations -- stating otherwise would hide a real query
surface -- but only to answer one write path's precondition, returning a single
boolean to the caller. It is excluded because nothing in the leadership surface
reads it, not because it touches no data; if a leader ever needs rejected-proof
counts, that is a NEW read to add here, not this one. Leadership continues to see weighing progress
through `GET /weighing/campaigns`, `GET /app/weighing/leadership/sheds` and the
Control Tower process-state summary.

**EXCLUDED — `func:Error`, `func:Is`, `func:ReworkNotRecapturedFor`** (weighing
ports) — the error-interface methods and constructor of the typed refusal that
blocks SUBMITTING a shed still holding an animal a verifier sent back. The type
carries the scanned identifiers so the operator is told WHICH animals to redo
instead of "that animal", which is unactionable in a shed of forty.

They render an error string, compare errors.Is-equal to
`ErrReworkNotRecaptured`, and wrap a tag list — no query, no aggregate, no
metric, no read surface, and nothing a leader can ask a question about. Same
shape as `FinishedShedConflict` and `ShedScheduleConflict`, both already excluded
above for the same reason.

**EXCLUDED — `func:AlreadyDecided`** (verification ports) — the constructor of the
typed refusal returned when a verdict write targets an item that ALREADY carries a
verdict, as distinct from losing a row_version race. Both refuse; only one is worth
retrying, and reporting the terminal case as "modified by someone else" sent
verifiers hunting for a colleague who never touched the item.

It wraps a status string and compares errors.Is-equal to `ErrConflict` — no query,
no aggregate, no metric, no read surface. A refused verdict writes nothing, so
leadership's verification numbers are unchanged by definition; they continue to
come from `GET /verification/queue` and the Control Tower summary.

**EXCLUDED — `func:navigationModuleForDutyCode`** (verification http adapter) — a
pure string translation from a position_module_duties module_code
("pc.vaccination") to the NavigationModule key the category registry uses
("vaccination"). The two vocabularies had drifted, which refused verifiers the
vaccination queue outright. It reads nothing and returns no data.
