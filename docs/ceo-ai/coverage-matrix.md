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
| GET /app/counts/shifting-events/pending-execution | api + view:counts_movement_daily | Movement backlog |
| GET /app/counts/approvals | api + view:counts_movement_daily | Pending census approvals |
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
| HRMS operator vaccination animal cap | api + view:workforce_coverage_status + view:vaccination_operator_status | Coverage clarification: the scheduler and leadership assistant read per-operator animal capacity from `workforce_positions.vaccination_daily_animal_cap`; tenant `vaccination_capacity_config.max_per_day` is fallback only. Capacity/utilization answers must use the HRMS position cap that admin-web HRMS edits persist. `null` clears a custom HRMS cap and means default capacity, never zero; DB coverage must include real Postgres proof of custom/default/week-off operator rows. |
| vaccination_operator_capacity_overrides | api + view:vaccination_operator_status | Date-scoped operational exception table for explicit operator/date animal-cap overrides. Leadership capacity answers stay on the operator schedule/status surface and must show normal HRMS cap semantics unless a matching override row exists for that exact operator/date. CPT uses this only for the one-time 2026-07-25 ET+TT seed catch-up allowance; it is not a general cap increase. |
| GET /vaccination/sheds | api + view:vaccination_shed_status | Shed status list |
| GET /vaccination/sheds/{shed_id} | api + view:vaccination_shed_status | Shed drilldown |
| GET /vaccination/sheds/{shed_id}/animals | EXCLUDED | Animal-level detail; not aggregate |
| GET /vaccination/action-center | api + view:action_center_current | Exception queue |
| GET /vaccination/action-center/counts | api + view:action_center_current | Summary tiles |
| GET /vaccination/adherence | api + Cube:vaccination_compliance | Governed compliance KPI |
| Process-integrity vaccination labels and same-business-day status | api + view:action_center_current | Coverage clarification: no new assistant tool or KPI. Existing Action Center, Protocol Adherence, Control Tower, and Workflow read paths must report human vaccine labels when available and must classify assignment-planned vaccination work as overdue only after its India business date has passed, so assistant and admin-web process-integrity answers do not leak raw rule codes or mark today's drive late at morning check-in. Grouped process-integrity rows must carry the computed execution date forward under the alias consumed by final rows, so fresh API binaries do not fall back to stale obligation dates or fail Control Tower reads. |
| GET /vaccination/capacity-config | api | Capacity behind backlog explanations |
| GET /vaccination/verification-queue | api + view:verification_queue_status | Proof gaps |
| GET /vaccination/workflows/{row_id} | EXCLUDED | Row-level process-integrity detail |
| GET /control-tower/vaccination | api + view:action_center_current | Leadership control tower |
| GET /app/vaccination/execution(+/sheds/…, roster, coverage, gaps, tasks/…) | EXCLUDED | Operator-scoped app views; leadership uses /vaccination/*. Runtime contract: operator execution and scan rosters must filter split-shed work by `vaccination_drive_assignments` plus `goat_shed_partitions`, so one operator cannot see another operator's partition animals inside the same batch/shed. |
| GET /calendar/vaccination/events | api | Calendar timeline (dots) |
| Calendar vaccination date markers | api | Leadership assistant read API coverage: month/week marker dots use the same assignment-effective schedule date as the calendar event list and vaccination operator schedule, so leadership answers and client overview counts do not report stale batch/obligation dates after a drive move. |
| GET /calendar/vaccination/events/{event_id}(+/history,/targets) | EXCLUDED | Single-event / target detail; admin-web Calendar drive target rosters must still open the shared Goat Passport local drawer with per-goat vaccination history |
| GET /action-center/obligations | api + view:action_center_current | Cross-domain queue; API tier executor wired (action_center_obligations tool) |
| GET /feed-direction/preview | api + view:feed_direction_current | Feed needed today; blocked≠0 |
| GET /feed-direction/generation-preview | api + view:feed_direction_current | Planned generation + gaps |
| GET /feed-direction/counts-projection/exceptions | api + view:ops_exception_queue | Blocked feed cells |
| GET /feed-packing/worklist | api | Packing worklist |
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
| GET /app/tasks(+/{id}, /shed-completion-summary), /app/sop-versions/{id} | EXCLUDED | Self-scoped operator worklist / form |
| GET /verification/queue | api + view:verification_queue_status | Verification backlog; API tier executor wired (verification_queue tool) |
| GET /verification/action-queue | api + view:verification_queue_status | Actionable proof exceptions |
| GET /operations/audit | api + view:audit_activity_summary | Audit stream |
| GET /operations/audit/summary | api + view:audit_activity_summary | "What changed" summary; API tier executor wired (operations_audit_summary tool, via operationsaudit.Service.Summary) |
| GET /operations/dlq | api + view:ops_exception_queue | Failed-event queue (see gap G5) |
| GET /operations/kernel-health | api + view:ops_exception_queue | System integrity (see gap G5); API tier executor wired (operations_kernel_health tool, via processintegrity.Service.ControlTower with OnlyBrokenOrAtRisk) |
| func:SuppressInvalidRecipient | EXCLUDED | Notification delivery hygiene only: clears FCM tokens that Firebase reports as invalid and suppresses queued rows for that exact token. Leadership delivery health remains covered by `ceo_ai.notification_delivery_health` / `mesha_notification_delivery_health`; this function is not a leadership read surface. |
| GET /workflows/{row_id} | EXCLUDED | Row-level workflow detail |
| GET /protocols | api | Protocol/schedule definitions |
| GET /protocols/versions/{id}, /protocols/animal-stages | EXCLUDED | Protocol version / reference taxonomy |
| GET /app/config, /app/bootstrap, /app/me, /admin-web/bootstrap | EXCLUDED | Client/session bootstrap; RBAC/chrome, not data |
| GET /app/proofs/{id}/download(+/signed) | EXCLUDED | Binary proof retrieval |
| GET /healthz, /livez, /readyz, /version | EXCLUDED | Infra probes; no tenant scope, no business data |

## B. `ceo_ai.*` reporting views

Every view maps its grain + columns to real sources. Columns with no backing
source are typed `NULL::type -- TODO` placeholders (never silently dropped) and
tracked as gaps below.

| View | Status | Column gaps (typed NULL + TODO) |
|---|---|---|
| animal_current_scope | draft | — (all mapped) |
| shed_capacity_current | draft | — |
| vaccination_shed_status | draft | planned_sessions (park-level batches → shed derivation approx) |
| vaccination_dose_pickup | draft | vaccine_label (needs display-label mapping — gap G2) |
| vaccination_operator_status | draft | — (operator-grain drive load/capacity/overdue/utilization over `vaccination_drive_assignments`; migration 000026) |
| vaccination_prearrival_history_review | draft | vaccine_label (raw protocol `vaccine_code` only at this grain — typed NULL + TODO; joining the published rule label would fan the trust buckets out per vaccine). Supplier pre-arrival vaccination-claim trust for PROCURED animals over `vaccination_prearrival_history_entries` (migration 000043); Toolbox tool `mesha_prearrival_history_review`; golden eval `prearrival-history-rejected-share` |
| feed_direction_current | draft | — (directive only; actuals → gap G7 feed_adherence) |
| counts_movement_daily | draft | transfers_out (derived from terminal exits — partial) |
| procurement_pipeline | draft | batch_label (no stored load label — gap) |
| source_entry_health_status | draft | load_label (no stored load label) |
| ops_exception_queue | draft | — (UNION across modules) |
| sop_execution_status | draft | — |
| verification_queue_status | draft | — |
| inventory_stock_position | draft | reorder_flag (no threshold config — gap G1), last_reconciled_at (partial) |
| workforce_coverage_status | draft | — |
| action_center_current | draft | — (UNION) |
| audit_activity_summary | draft | result (no outcome column — derive from jsonb, partial) |

## C. Leadership-relevant tables → covering view

| Table | Covered by |
|---|---|
| goats, herd_register_summary_projection | animal_current_scope, counts_movement_daily |
| locations, park_profiles, shed_profiles, location_capacity_records | shed_capacity_current |
| vaccination_eligibility_rollups, obligation_instances, obligation_batches, vaccination_completions | vaccination_shed_status, vaccination_dose_pickup |
| vaccination_drive_assignments (operator-based drive model) | vaccination_operator_status (operator load / capacity / overdue / utilization) |
| obligation_escalations | ops_exception_queue, action_center_current |
| feed_direction_issues, feed_direction_issue_rows | feed_direction_current, ops_exception_queue |
| feed_direction_completions | gap G7 (feed_adherence) |
| shifting_events, shifting_event_impacts, counts_approval_requests, count_projection_* | counts_movement_daily, ops_exception_queue |
| procurement_loads, procurement_load_goats, arrival_intake_reviews, procurement_source_health_checks, procurement_hf_vaccination_evidence | procurement_pipeline, source_entry_health_status |
| sop_tasks, sop_submissions | sop_execution_status |
| verification_items | verification_queue_status |
| inventory_items, inventory_stock, inventory_stock_movements | inventory_stock_position |
| workforce_members, workforce_positions, workforce_absences, workforce_roster_assignments, org_role_catalog | workforce_coverage_status |
| vaccination_drive_assignment_members | EXCLUDED (operational scheduler-written membership) — the exact obligation/goat set behind each `vaccination_drive_assignments` row. It exists so a death/sale/cull decrements the exact assignment arm and so CT/PA/WF/AC can report an animal's own operator-day instead of inferring it from an aggregate. Leadership never reads membership directly; it reads the drive/operator aggregates this table makes correct (`view:vaccination_operator_status`, `GET /vaccination/schedule`). |
| vaccination_prearrival_history_entries (migration 000041) | vaccination_prearrival_history_review — COVERED, not excluded. The accepted/rejected split on supplier-attested pre-arrival vaccination claims for PROCURED animals is a real leadership signal (trusted-history share, rejected-claim rate and reason = supplier data quality + avoided re-injection). Coverage: `ceo_ai.vaccination_prearrival_history_review` (migration 000043) → MCP Toolbox tool `mesha_prearrival_history_review` (in `mesha_ceo_toolset`) → read-only SQL fallback over the same view. No governed Cube metric yet: rejected-rate is not an official tracked KPI today, so this stays tier-3/4 (add a Cube metric if leadership starts trending it). Golden eval question: `prearrival-history-rejected-share` (`tools/ceo-ai/eval/golden/vaccination.json`). |
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
outbox, and `func:ShedCompletionReadiness` gates whether a shed-level submission
has the expected scans/proofs before it can enter verification. Both are internal
write/readiness helpers on the existing vaccination SOP execution path. They add
NO new leadership KPI, table, read API route, Cube metric, `ceo_ai.*` view, or
MCP Toolbox tool; the leadership assistant read surface remains the existing
vaccination execution/process-integrity coverage. Explicit documented exclusion —
no coverage-matrix mapping required.

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
