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

## Governing CEO operations lens

The CEO does not receive or navigate one card or alert per field operation.
Under `docs/decisions/task-timing-alerting-violations-and-appeals.md`, CEO reads
must aggregate shared task truth into critical incidents, hard breaches,
unowned exceptions, flexible carry-forward, verification aging, Director
follow-up aging, workload saturation, and open appeals, with drill-down to the
exact task/evidence/history when needed. Vaccination and Weighing ordinary
carry-forward remain clearly separated from hard breaches. Raw late rows,
unadjudicated violation candidates, and notification-delivery rows are not
employee guilt, ranking, salary, or payroll inputs. HR receives only final,
appeal-complete findings through its separate authorized process.

## A. Read APIs

Source inventory: the read-API catalog the planner consumes. Leadership-relevant
APIs map to a tier; the rest are documented exclusions with a reason.

| Read API (path) | Coverage path | Notes |
|---|---|---|
| GET /herd-register/summary | api + Cube:active_animals | Primary census; aggregate-first |
| GET /counts/breakdown | api + view:animal_current_scope + external MCP:get_counts_summary | Grouped census drilldown. External MCP clients must use the typed `get_counts_summary` tool for herd/census count questions and keep lifecycle status explicit instead of mixing active, exited, or death states. |
| GET /counts/herd-analytics | api + view:animal_current_scope + view:counts_movement_daily | Counts -> Herd Analytics: the live census rolled up by breed, pen tag, sex, age band and farm, beside one row per IST calendar month of births, deaths, sales, other exits and applied pen movements. It introduces NO new fact. Composition is the same canonical live-goat population `GET /counts/breakdown` reports and stays covered by `ceo_ai.animal_current_scope`; every flow figure is counted off the canonical row that already records the event — an animal's own origin/exit columns and an APPLIED `shifting_events` row — which is exactly the birth/death/movement grain `ceo_ai.counts_movement_daily` governs. No new Cube metric or Toolbox tool: a leadership question about herd composition or monthly movement is already answerable from those two, and this route is the admin-web renderer's single round trip for the page rather than a second source of truth. |
| GET /app/counts/shifting-events/pending-execution | api + view:counts_movement_daily | Raised/authorized/evidence-rework Actions; census moves only after Park Head approval + operator completion. High-priority feed requirement/fingerprint is operator execution detail, not a leadership KPI; leadership movement state remains covered at event/day grain. |
| func:ShiftingActionsDueFrom, func:ShiftingActionsDue | EXCLUDED | Internal operator Actions visibility helpers for pending shifting execution. They only decide whether a movement should open in the operator queue from its priority and effective date; they add no leadership read API, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or KPI. Leadership movement state stays covered by `GET /app/counts/shifting-events/pending-execution` and `view:counts_movement_daily`. |
| GET /app/counts/approvals | api + view:counts_movement_daily | Pending census approvals. The backend-owned display copy on each row (`raised_by_name`, `summary_line`) is composed by `WithApprovalNames` / `ResolveApprovalNames` / `PersonName` / `ApprovalSummaryLine` / `ApprovalSummaryLocationIDs`, which are EXCLUDED as leadership surfaces: they resolve ids to names for the approver's queue copy and derive no new fact. Every underlying fact they render — movement, park/shed, raiser, request type — is already covered at event/day grain by counts_movement_daily, so the assistant reads the fact, never the rendered sentence. |
| GET /app/counts/shifting/destinations | EXCLUDED | Operator write-flow picker; not a leadership metric |
| func:CarryOverUnchangedVaccinationObligations, func:RuleIdentityKey, func:RuleContentFingerprint, func:VaccineCodeForRule | EXCLUDED | Internal publish-time scheduling mechanics. They decide whether a plan rule is the SAME rule as the previous version's (identity + content fingerprint), so an unchanged vaccine's obligations are rebound in place rather than cancelled and re-minted -- see `docs/preventive-care-vaccination/additive-publish.md`. They derive no new leadership fact: what an animal owes, when it is due, and whether it was done are unchanged by carry-over, and stay covered by the existing vaccination obligation surfaces. A leadership question they could newly answer would be about protocol version churn, which is an authoring concern rather than a herd-health one. |
| func:ResolveShiftingDestinationPenStage (counts/domain) | EXCLUDED | The raise-time rule deciding which cohort a movement adopts, now the destination PEN's own tag rather than one derived from the whole shed's residents (maintainer decision 2026-08-14). A pure decision function over values the caller already holds: no read API, Cube metric, `ceo_ai.*` view or MCP tool, and no new fact. The stage it resolves is snapshotted on the shifting event and is already covered at event/day grain by `GET /app/counts/shifting-events/pending-execution` and `view:counts_movement_daily`, exactly as its sibling `func:ShiftingActionsDueFrom` above. |
| func:IsClinicalStage (protocol/domain), func:PartitionAliasExclusionSQL (platform/oploc) | EXCLUDED | Two safety/identity primitives, not leadership surfaces. `IsClinicalStage` answers whether a stage code names one of the canonical `MandatoryClinicalDeferStates`, so a cohort-assigning picker can drop the clinical tags instead of offering ones every such write rejects — it reads no data and reuses the existing set rather than copying it. `PartitionAliasExclusionSQL` is a SQL predicate fragment suppressing the legacy duplicate `locations` rows for pens (`Castro 1` beside `Castro` + pen `1`); it removes phantoms from queries that are themselves covered, and adds no fact. Same class as `func:SplitShedPartitionName` above. |
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
| GET /vaccination/command (func:GetVaccinationCommandBoard, func:VaccinationCommandBoard, func:GetCommandBoardCohortMatrix, func:CommandBoardCohortMatrix, func:GetCommandBoardShedDoseMatrix, func:CommandBoardShedDoseMatrix, func:GetCommandBoardClosedWithoutDose, func:CommandBoardClosedWithoutDoseAnimals, func:GetCommandBoardShedVaccineAnimals, func:CommandBoardShedVaccineAnimals, func:GetCommandBoardCohortExceptions, func:CommandBoardCohortExceptions, func:GetCommandBoardCohortDays, func:CommandBoardCohortDays, func:GetCommandBoardDriveOptions, func:CommandBoardDriveOptions) | api + Cube:vaccination_metrics | Command board: all-history KPI summary (targets, MISSED, verified, awaiting, overdue, scheduled ahead, closed-without-dose), drive-scoped active batch summary, cohort×vaccine pending matrix (ET+TT/PPR/etc pending count per stage/sex), shed×vaccine behind/on-track/not-planned roll-up with its behind-animal drill-down, and verification queue with shed, dose rule, awaiting count, total, and days-in-queue age. Grain: obligation-scoped KPIs + shed×vaccine×cohort matrix + shed-scoped proof backlog age. The `missed` bucket leads the KPI priority chain, so "how many animals have a missed dose" is answerable from the tile rather than inferred from overdue. 2026-09-01: the cohort matrix, the shed x dose grid and the per-cell drilldowns (closed-without-dose animals, shed-vaccine animals, cohort exceptions, cohort days) and the drive-picker catalogue moved to their OWN routes for latency, and each is named above as riding this same coverage row. Nothing about what the assistant can answer changed: they are the same whole-scope facts at the same grain, fetched separately instead of in one payload. `docs/runbooks/vaccination-command-board-latency.md`. |
| GET /vaccination/live-tracker (func:GetVaccinationLiveTracker, func:LiveTracker) | api + Cube:vaccination_metrics + external MCP:get_vaccination_today | Live drive-day tracker at ADMINISTRATION grain (one obligation = one administration) for a single business date in the last seven days. Answers "what is landing right now": scheduled administrations with their park split, completed video proofs received, RFID scan captures, remaining, unassigned scheduled administrations, combo animals (ANIMAL grain, never mixed into the administration totals), and a derived attention count. Carries the per-operator live board (assigned workload, videos, scans, remaining, current shed/partition/vaccine, idle minutes, state), the shed x partition x vaccine x operator proof-progress board with extra-attempt counts and last-proof time, the combo-dose rows with their real scanned identifiers, a keyset-paginated activity feed unioned from proof_artifacts / sop_task_scan_captures / sop_task_scan_attempts / vaccination_completions / obligation_status_events, and the vaccination verification queue counts. Complements rather than duplicates `/vaccination/command`: that surface answers all-history closure at animal and cohort grain, this one answers today's execution at administration grain per operator. Filterable by park (backend-clamped), shed, normalised partition, operator and vaccine family. External MCP clients must route "what vaccination work is scheduled today and what is the progress?" to the typed `get_vaccination_today` tool, not the generic `ask_goatos` fallback, because fallback can mix due-work and overall-dashboard grains. Golden eval question: `vacc-live-today-progress` (`tools/ceo-ai/eval/golden/vaccination.json`). |
| func:NormalizePartitionLabel, func:ShedDisplayLabel, func:VaccineFamilyCode, func:IsLiveTrackerStatus, func:OperatorMatchesStatus, func:ShedMatchesStatus | EXCLUDED | Pure identity and presentation helpers for the row above; they derive no fact and read no data. `NormalizePartitionLabel` collapses the two spellings of one partition (`Part 3` on the assignment side, `3` on the goat side) so both sides of a join agree; `ShedDisplayLabel` composes the shed + partition label already used across the vaccination surface; `VaccineFamilyCode` reduces a dose code to its antigen family because `public.vaccines` is empty; the three `*MatchesStatus` predicates map a row state onto the page's own status filter. Every fact they label or filter is covered by the `/vaccination/live-tracker` row above, so the assistant reads the underlying codes and counts, never the rendered label. |
| func:VaccineAntigenLabel | EXCLUDED | Pure presentation: maps a protocol vaccine CODE to its human label (`ET_TT` → `ET+TT`). Derives no fact and reads no data — it exists so shed×vaccine column headers are server copy instead of a second, drifting label table in the frontend. Every fact it labels is already covered by the `/vaccination/command` row above, at vaccine-code grain, so the assistant reads the code and never the rendered header. |
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
| func:ReplaceDraftVersion, func:DiscardVersion, func:DiscardProtocolVersion, func:DeleteDraftProtocolRules, func:DeleteDraftProtocolTriggers | EXCLUDED | Draft authoring writes in `protocol`, not read surfaces. Saving an edited vaccination plan swaps one DRAFT for another in a single transaction, and discarding deletes one; both operate exclusively on rows with `status='draft'`, which are unpublished and never generate work. Nothing a leader reads -- adherence, the command board, Control Tower -- can see a draft at all, because every read path filters `protocol_versions.status='published'`. They surface no fact and answer no question. |
| func:RepeatCycleRef, func:Valid (obligation/domain) | EXCLUDED | Identity primitives. `RepeatCycleRef` spells the administration that caused a repeat obligation -- vaccine, when it was given, which dose -- as the one string every writer must agree on, and `Valid` refuses half-populated metadata. They derive no fact and read no data; they exist so two writers cannot mint two rows for one vaccination cycle. |
| func:OpenObligationForRepeatCycle, func:GetOpenObligationForRepeatCycle, func:GetObligationRepeatCycle | EXCLUDED | Single-row lookups the generation and reschedule WRITE paths use to find the obligation a suppressed insert collided with, so the surviving row is reconciled rather than left stale. They return one row by cause, never an aggregate, and are not reachable from any read API. |
| func:GoatsWithVaccinationObligationsOutsideVersions | EXCLUDED | A bounded pre-filter inside generation: which of these animals still hold open work under a protocol version that is no longer effective for them, so that work can be superseded after a plan is replaced. It exists to avoid a cancel-per-animal round trip and is never read by a leadership surface. |
| func:Run, func:Modes, func:ValidMode (obligation/repair) | EXCLUDED | The duplicate-obligation repair command. An operational one-shot cleanup, run deliberately per tenant with `--apply`, that retires duplicate open obligations left by the old due-date identity and labels the survivors. Not a query surface: it answers nothing, it repairs rows. |
| func:AuthorizedParkIDsForCapability | EXCLUDED | Authorization primitive in `platform/httpmiddleware`, not a leadership surface. It returns the actor's park scope ids for a single capability, keeping each grant's role bound to its own scope so an unrelated park grant cannot borrow a capability granted on a different park. It narrows what an actor may read; it never widens coverage and exposes no new metric, view, or tool. |
| func:ClientInfoFromContext, func:ClientInfoMetadataFromContext, func:WithClientInfo, func:ClientInfoFromRequest | EXCLUDED | Request provenance plumbing in `platform/httpmiddleware`, not a leadership read surface. These helpers capture Android API headers (app version/code, build type, install id, platform, Android OS/SDK, device model) into request context/logs and audit metadata so support can answer "who did what from which APK/device" for a concrete submitted action. They add no read API, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, KPI, or aggregate. Leadership assistant business answers remain on the existing covered operational reads; the audit `metadata->'client'` block is incident/debug provenance for an already-identified action. |
| func:SplitShedPartitionName (platform/oploc), func:PartitionMatchKey, func:ExperimentLocationKey, func:SplitGrainsByPartition, func:PartitionKey (feeddirection/domain) | EXCLUDED | Partition-resolution primitives for the feed sheet, not leadership surfaces. They parse a shed catalog name into physical shed + partition (`Godel 1 - Part 3`), normalize a partition to its matching key (`whole` when there is none), and bucket one shed's projected grains per operational location so a partly-experimental shed feeds its authored partitions on absolute kg and the rest on the per-head grid (migration 000122). They add NO read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool, and expose no new fact: the quantities and head counts they route were already reported through the existing feed surfaces, which stay covered. `SplitShedPartitionName` is a MOVE, not a new capability -- it was private inside weighing/adapters/postgres and is promoted to `platform/oploc` so one convention has one home. If leadership later asks a per-partition feed question ("which pens are on trial rations"), that becomes a real coverage row against a feed aggregate, not these helpers. |
| GET /feed-analytics/directed, GET /feed-analytics/execution, GET /feed-analytics/experiment, GET /feed-analytics/stock, GET /feed-analytics/shed-feed (func:GetShedFeedAnalytics, func:ShedFeedAnalytics), table:feed_purchases, table:feed_external_consumption, cmd:import-feed-purchases, cmd:import-feed-external-consumption (func:GetStockAnalytics, func:StockAnalytics, func:consumedOrZero, func:GetDirectedAnalytics, func:DirectedAnalytics, func:WithAnalyticsReader, func:ClampAnalyticsWindow, func:GetExecutionAnalytics, func:ExecutionAnalytics, func:GetExperimentAnalytics, func:ExperimentAnalytics) | api | Feed Analytics windowed rollup of the FROZEN feed sheet for the admin-web Feed Analytics page: per-day and per-(day, feed item) DIRECTED kg, head-days (pen-grain counted once per day), and grams per head per day, over a business-date window capped at 92 days, normal workflow only. DIRECTED means the sheet's instruction, never a measured weight — completions carry proofs, not kg, so a consumed/wastage figure does not exist yet and this surface must not be described as consumption. Leadership questions like "how much feed did we direct last month" and "what is the ration per animal trending at" route here; single-day operational drill-down stays on `/feed-direction/preview` (same underlying rows, so the two agree by construction — the parity fixture is `TestDirectedAnalyticsGrainProofs`). `/feed-analytics/shed-feed` serves the same frozen-sheet membership regrouped to the PEN grain — per operational location (shed + partition), the window's directed kg per feed item and the pen total — behind the overview's "Feed by shed — last 7 days" table; summing an item across pens agrees with the per-item series by construction. The sibling reads complete the page: `/feed-analytics/execution` serves per-day proof/verdict status counts (packing, distribution, transport) plus median submit-to-verdict latency — status counts only, since completions carry proofs, never kg; `/feed-analytics/experiment` serves trial-arm authored ABSOLUTE kg per day with pen counts, from which no per-head figure exists or may be derived; `/feed-analytics/stock` serves per-item stock positions (balance, days-left, low-stock) off the one-time-bootstrapped `feed_purchases` ledger (`cmd/import-feed-purchases`, current-catalog feeds only, read-only until Procurement builds purchase entry) with stock depleting at sheet LOCK, plus UHT Milk daily consumption from `feed_effective_external_consumption` (the Milk Preparation workflow's own submitted litres, booked on the feeding date; the `feed_external_consumption` ledger, history-bootstrapped by `cmd/import-feed-external-consumption`, is the fallback for days no preparation covers). Those recorder functions are write-path plumbing into the covered stock read, not standalone assistant tools. The daily expenditure series prices both directed sheet kg and UHT external consumption at each item's most recent load rate — the first rupee figure in GoatOS, answering "what is feed costing us" and "how many days of each feed are left". |
| func:ExceedsPackingVarianceTolerance | EXCLUDED | Pure feed-analytics presentation predicate behind the covered `GET /feed-analytics/execution` read. It compares one already-computed signed bag variance against the shared packing tolerance so the API can emit `exceeds_tolerance` with the same judgement used by the packed-vs-given trend. It reads no data, creates no metric, and is not a leadership assistant tool; leadership answers continue to consume the existing feed analytics execution payload. |
| func:Wants, func:ParseExecutionSection (feed analytics HTTP execution parser) | EXCLUDED | Request-shaping helpers behind the covered `GET /feed-analytics/execution` read. They parse and test section query params so the same endpoint can return the requested execution blocks, but they add no fact, KPI, aggregate, read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. Leadership execution answers remain covered by `GET /feed-analytics/execution` in the row above. |
| feed_external_consumption | api:GET /feed-analytics/stock | Daily UHT Milk consumption ledger for feeds GoatOS does not direct through feed sheets. Covered by the existing Feed Analytics stock read/API: `GetStockAnalytics` unions this table with locked sheet-directed kg for balance, avg kg/day, days-left, and expenditure. History comes from `cmd/import-feed-external-consumption` and is the ONLY writer: since migration 000216 ongoing UHT consumption is read straight from the Milk Preparation workflow through `feed_effective_external_consumption`, and this table is the fallback for days no preparation covers (suppressed on both of a preparation's dates by migration 000241). |
| func:CollapseDirectionRowsByLocation (feeddirection/domain) | EXCLUDED | Presentation fold on an EXISTING feed read, not a leadership surface. The generator and the frozen issue keep the ration grain (shed + partition, tag, breed); this collapses the SERVED direction sheet to one row per (shed, partition, session, workflow) so an operator reads one instruction per pen instead of one per breed (maintainer decision 2026-08-10). It adds NO read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool, and derives NO new fact: it sums quantities the same rows already carried and joins the breed/tag/ration-group labels they already reported. Park and item totals are unchanged to the gram -- `/feed-direction/preview` and `/feed-packing/worklist` stay the covered leadership feed surfaces, and the packing worklist is deliberately not folded. If leadership later asks a per-pen feed question, that becomes a real coverage row against a feed aggregate, not this helper. |
| func:ListPackingCompletionStatuses (feeddirection/adapters/postgres), func:ReopenPackingForFeedChange, func:FeedShiftingRaisedEffectiveBusinessDate (counts/domain) | EXCLUDED | The afternoon feed-correction path (maintainer decision 2026-08-10), not a leadership surface. `ListPackingCompletionStatuses` is an EXISTING per-park-day status overlay for the packing worklist; it gained one column, `rework_reason`, which is operator-facing copy telling a packer why a pen came back to them — a sentence, not a fact anything aggregates. `ReopenPackingForFeedChange` is a WRITE issued by `AmendDirection` that moves a pen's completion back to `rework` when the correction changed how many animals it feeds; a write is not a read surface, and the resulting verification state stays covered by `view:verification_queue_status`. `FeedShiftingRaisedEffectiveBusinessDate` is a pure date rule deciding which feed day a raised-but-unapproved movement counts toward. None adds a read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool, and none exposes a new fact: leadership feed answers stay on `/feed-direction/preview`, `/feed-packing/worklist` and `feed_direction_current`, whose numbers this change makes MORE accurate (a destination pen is now fed for animals a raised movement is bringing) without changing their grain or shape. If leadership later asks "how often does a correction force a repack", that becomes a real coverage row against a feed aggregate, not these functions. |
| func:PartitionAliasExclusionSQL (platform/oploc, used by feed transport postgres reads) | EXCLUDED | Partition alias filtering for feed transport task reads, not a leadership reporting surface. It removes legacy duplicate pen-as-shed location rows when listing transport work so one physical shed/pen does not appear as multiple operator tasks. It adds no new read API, Cube metric, MCP/Toolbox tool, KPI, or `ceo_ai.*` view; leadership feed visibility remains on `/feed-direction/preview`, `/feed-packing/worklist`, `/feed-transport/tasks`, and existing feed aggregates, with cleaner task grain rather than a new fact. |
| POST /feed-config/feed-items (func:CreateFeedItem, func:NormalizeEnergyKcalPerKg, func:NormalizeDryMatterFactor, func:NormalizeWastageFactor, func:ValidateDisplayOrder) | EXCLUDED | Feed Config authoring/validation surface, not a leadership read surface. Adding a catalog item creates a tenant feed vocabulary name and optional nutritional attributes; it authors no ration quantity, no shed factor, and no experiment kg. Leadership feed answers remain covered by `/feed-direction/preview`, `/feed-packing/worklist`, feed issue tables, and the existing feed aggregate rows below. The normalization helpers only validate authored optional attributes and display order before the write; they expose no new metric, `ceo_ai.*` view, MCP Toolbox tool, or leadership KPI. |
| POST /feed-config/feed-items/status (func:SetFeedItemStatus, func:ValidateFeedItemStatus) | EXCLUDED | Feed Config authoring/validation surface, not a leadership read surface. Retiring or restoring a feed item changes which catalog rows future feed generation reads, but it creates no new leadership metric or standalone fact: the effect is visible through the existing feed direction and packing reads (`/feed-direction/preview`, `/feed-packing/worklist`, feed issue tables, and feed aggregates). `ValidateFeedItemStatus` only rejects values outside the closed active/retired enum before the write. |
| PUT /feed-config/session-templates/{template_id}/items/{feed_item_label} (func:SetSessionTemplateItem) | EXCLUDED | Feed Config authoring surface, not a leadership read surface. Declaring or clearing a session template feed item changes the recipe that future feed generation reads for one slot/day, including zero-fill defaults for new catalog items, but it adds no new KPI, aggregate, `ceo_ai.*` view, or MCP Toolbox tool. Leadership feed answers continue to come from `/feed-direction/preview`, `/feed-packing/worklist`, feed issue tables, `feed_direction_current`, and the existing feed aggregates after generation materializes the authored recipe. |
| Process-integrity evidence media resolver (func:WithMediaResolver, func:ActionCenter) | api + view:action_center_current | Coverage clarification: no new assistant tool, Cube metric, or `ceo_ai` reporting view. The existing Action Center / Protocol Adherence / Control Tower / Workflow read surfaces are still covered by `action_center_current`; this change only enriches their shared process-integrity `evidence` payload with proof-media download links resolved through the existing proof/verification signed-URL path. |
| func:ListUploadedProofs, GET /app/proofs/uploads | EXCLUDED | Operator/client proof-preview hydration endpoint, not a leadership reporting surface. It returns already-uploaded proof blobs for one exact `client_task_key` plus optional `field_key` so a phone can reopen a pre-submit proof slot and show the same processed media it already uploaded. It is task/session media recovery plumbing, bounded and authorization-gated through proof execute permissions; it adds no CEO assistant read API, Cube metric, MCP/Toolbox tool, KPI, or `ceo_ai` SQL fallback surface. Leadership continues to see proof/completion state through the covered module aggregates and verification/action-center surfaces, not raw proof blobs. |
| GET /vaccination/capacity-config | api | Capacity behind backlog explanations |
| GET /vaccination/verification-queue | api + view:verification_queue_status | Proof gaps |
| func:GetOversightAnalytics (verification oversight endpoint aggregate) | api + view:verification_queue_status | Oversight Analytics tenant-scoped KPIs: videos waiting, oldest pending age, verdict throughput per active day, per-module median review latency, reject rate over 30 days, pending backlog by module, per-verifier 14-day activity (verdicts/approved/rejected/busiest day), and per-verifier watch integrity (items tracked, watched-to-end, verdict-without-play). Verifier-level activity resolves names from `workforce_members` for display. All aggregates pre-collapse at their grain (module, verifier) before returning, with no per-item or per-actor fan-out. |
| func:OversightAnalytics (domain struct) | EXCLUDED | Container struct for oversight analytics return; not a read surface itself but carries the aggregated results. |
| GET /verification/video-log (func:GetVideoLog, func:VideoLog) | api + view:verification_queue_status | The VIDEO LOG (maintainer decision 2026-08-14): for ONE Asia/Kolkata business day, per operational location (shed + partition), the time each proof was UPLOADED — feed distribution's three captures, feed packing's one, feed transport's one, and the vaccination, weighing, birth, death and shifting proofs beside them. Two grains, both bounded: a per-shed day summary (proof count, item count, awaiting-upload count, first/last arrival, contributing module labels) and, for one selected shed, its work in full with each proof's own arrival time. Answers a DIFFERENT question from the two rows above it: `/vaccination/verification-queue` and `func:GetOversightAnalytics` answer "what is waiting" and "how is the backlog trending", whereas this answers "what arrived from this shed today, and when" — arrival timing, not backlog state, and it includes already-decided items because a rejected proof arrived just as much as a pending one. Time is server-accepted upload time (`proof_artifacts.uploaded_at`); there is no per-proof device capture timestamp in the schema, and `registered_at` is carried alongside so upload lag stays visible rather than inferred. Gated on `permissions.VerificationEvidenceTimeline` (CEO, PC Director AND the verifier — deliberately not `verification.oversee`), park-clamped for a park-scoped caller. No new Cube metric, `ceo_ai.*` view or Toolbox tool: leadership verification facts stay on `view:verification_queue_status`, which this neither widens nor re-grains. |
| GET /verification/sampling (func:GetVerificationSampling, func:SamplingOverview) | api + view:verification_queue_status | RANDOMIZATION (maintainer decision 2026-08-26): per verification category, the SHARE of that category's proof videos the verifier is required to watch, and how one Asia/Kolkata business day is going against it -- captured, drawn for review, reviewed by a person, and settled by the policy without one, plus a backend-owned progress percent (reviewed / drawn, so a fully-worked 40% share reads 100%). Grain: one row per registered category for one business day; the four counts are NOT disjoint (drawn is a subset of captured, reviewed a subset of drawn) and must never be summed. Answers a question none of the rows above it can: they report how much proof EXISTS and how the backlog is trending, this reports how much of it a human is REQUIRED to look at -- so "what proportion of our proof is actually being watched" resolves here rather than being inferred from a queue length. It adds no new Cube metric, `ceo_ai.*` view or Toolbox tool: the underlying facts are verification_items at their existing grain, which `verification_queue_status` already covers and this neither widens nor re-grains. Gated on `permissions.VerificationSampling` (CEO-only, narrower than `verification.oversee`). |
| PUT /verification/sampling/{category} (func:SetVerificationSamplingPolicy, func:SetSamplingPolicy, table:verification_sampling_policies) | EXCLUDED | The CEO's WRITE of that share, and the effective-dated config table behind it. A governance/authoring surface, not a leadership read: it records one number per (tenant, category, business day) with who set it and when. It derives no fact about the herd or the work -- what that number DOES to the day is reported by the covered GET above. Same class as the other authoring/write-flow exclusions in this section. |
| func:ListSamplingPolicies, func:UpsertSamplingPolicy, func:ListSamplingDayStats, func:SettleUnsampledItems, func:SamplingBucket, func:InSample, func:SamplingWaivable, func:ProgressPercent, func:SettledByPolicy, func:DecidedByPerson, func:NewVerificationSamplingCloseoutStage, func:PCCare, func:All | EXCLUDED | The mechanics behind the two rows above, not leadership surfaces. `SamplingBucket` / `InSample` are the pure draw (a stable 0..99 function of the item's own id, mirroring a GENERATED column); `SamplingWaivable` / `SettledByPolicy` / `DecidedByPerson` are predicates deciding whether a category may be sampled and whether a row was decided by a person; `ProgressPercent` is arithmetic over the counts the GET already returns; `ListSamplingPolicies` / `UpsertSamplingPolicy` / `ListSamplingDayStats` are that endpoint's own bounded reads and its write; `SettleUnsampledItems` and `NewVerificationSamplingCloseoutStage` are the WRITE path that approves undrawn items once their day closes -- a write is not a read surface, and the resulting verification state stays covered by `view:verification_queue_status`. `PCCare` / `All` merely enumerate the registered verification category catalog. None adds a read API, Cube metric, `ceo_ai.*` view or MCP Toolbox tool. |
| func:VideoLogShedSummary, func:VideoLogShedRows (verification postgres adapter) | EXCLUDED | The two bounded reads behind `GET /verification/video-log` above, not separate leadership surfaces. `VideoLogShedSummary` aggregates one day to one row per (shed, normalized partition); `VideoLogShedRows` returns one shed's items — or, for the CSV export only (`all_sheds`), the day's items across every shed in scope, capped with an explicit truncation flag. Both are day-bounded, tenant-scoped and park-clamped, and neither adds a read API, Cube metric, `ceo_ai.*` view or MCP Toolbox tool of its own. |
| func:WatchStates (review-event watch aggregate) | EXCLUDED | Internal verifier-queue helper that batches watch-state lookups for a page of items. It returns aggregated watch percentage per item (max position / max duration across all events, all actors for that item); it adds no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. Leadership visibility stays on the existing `GET /vaccination/verification-queue` and `/weighing/campaigns` oversight surfaces. |
| GET /weighing/campaigns, GET /weighing/process-state | api + external MCP:get_weighing_progress + external MCP:get_weighing_process_state | Leadership source planning/monitoring read for manually authored kids weighing campaigns; aggregate/capture surface only. Shared task reads own app-visible owner/clock/contact state after cutover. External MCP clients must use the typed `get_weighing_progress` tool for weighing progress questions, `get_weighing_process_state` for calendar/control-tower weighing gaps, and keep pending verification weight separate from verified/closed weight. |
| GET /app/weighing/campaigns | EXCLUDED | Operator execution list; leadership uses `/weighing/campaigns`. |
| Weighing shed-level operator assignments (`weighing_campaign_sheds.operator_user_id`) | api | Assistant coverage stays on `GET /weighing/campaigns`: leadership sees the campaign, selected shed buckets, per-shed owner/status, and progress rollups there. Operator-scoped mobile filtering and write authorization are execution behavior, not a separate CEO AI tool, Cube metric, MCP/Toolbox tool, or `ceo_ai` SQL fallback surface. |
| Weighing partition operational identity (`weighing_campaign_sheds.location_id`, `partition_label`, indexes from migration 000141; byte-stable after STG application, with follow-up repairs in 000142/000145) | api | Assistant coverage stays on `GET /weighing/campaigns`: the migration canonicalizes legacy partition aliases into physical shed + partition labels and replaces shed-only uniqueness with partition-aware bucket indexes. It creates no new leadership KPI, read API, Cube metric, MCP/Toolbox tool, or `ceo_ai` view; leadership campaign/shed progress already reads the same campaign bucket rows through the existing weighing API coverage. |
| func:ListCampaignsForOperator, func:ListScopeRoster, func:ListScopeRosterForOperator | EXCLUDED | Operator-only execution read helpers for mobile shed buckets. They exist to keep `/app/weighing/campaigns` and `/app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/roster` scoped before pagination/row return. The two roster readers now serve the bucket's SCAN HISTORY only — free-flow has no expected roster to read, so the herd-keyed roster query, its `animal_id` cursor, and the `include_roster` switch are gone (the `items` / `next_cursor` response fields are retained, always empty, purely so an already-installed app keeps its wire shape). Their signatures shrank accordingly; that is why this row's fingerprint moved, not because a new leadership surface appeared. Leadership assistant coverage remains `GET /weighing/campaigns` plus `ceo_ai.weighing_capture_activity` / `ceo_ai.weighing_verification_status` (migration `000080`); no MCP/Toolbox, Cube, or `ceo_ai` SQL fallback surface is added. ISOLATION: these reads touch weighing tables only — no goats, goat_identifiers, herd_animals, protocol, or vaccination join, and no expected-roster denominator, per the free-flow ruling behind `000078`/`000079`. |
| func:ListCampaigns, func:AppListCampaigns, func:ParseCampaignListScope | api | The one weighing list read, split by SURFACE rather than duplicated: `ListCampaigns` serves admin-web and defaults to `scope=all`; `AppListCampaigns` serves the phone and defaults to `scope=mine` (the caller's own work), so an already-installed app that predates the scope parameter keeps its previous meaning. `ParseCampaignListScope` validates that parameter. Leadership coverage is unchanged and remains `GET /weighing/campaigns` — no new tool, metric, view, or KPI. |
| func:PlannerCatalog, func:PlannerParkBuckets | EXCLUDED | The create-wizard's park picker and its per-park shed page. Planning a weighing task is CEO-only (maintainer decision 2026-08-01), so these are authoring surfaces behind `weighing.plan`, not reporting reads: they return selectable capacity for a task that does not exist yet, which no leadership question is asked of. Leadership monitoring of raised tasks stays on `GET /weighing/campaigns`. |
| func:Error, func:Unwrap | EXCLUDED | Go `error` interface methods on weighing's typed errors. Not a read surface of any kind. |
| func:AnimalProofWasRejected | EXCLUDED | Internal weighing write-path guard that refuses reusing a verifier-rejected proof video for another animal while allowing the same-tag recapture correction path. It adds no leadership read API, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or KPI; leadership visibility remains through existing weighing campaign progress and verification status reads. |
| func:New, func:EnqueueWeighingVerification, func:WithVerificationEnqueuer | EXCLUDED | Internal weighing proof-video enqueue bridge. It moves already-captured weighing videos into the existing Verification workstream and adds no new CEO assistant read API, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or leadership KPI. Leadership visibility remains through the existing weighing monitor/video APIs and verification surfaces. |
| POST /weighing/campaigns, /weighing/campaigns/{campaign_id}/publish | EXCLUDED | Leadership write/publish workflow, not a read metric. Result remains visible through `GET /weighing/campaigns`. |
| POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/reopen (func:ReopenScope) | EXCLUDED | Growth Director/CEO write path that reopens a completed weighing shed bucket for more free-flow scans. It adds no new CEO assistant read API, Cube metric, MCP/Toolbox tool, or `ceo_ai` SQL fallback surface. Leadership sees the result through existing `GET /weighing/campaigns` campaign/shed status and progress reads. |
| POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/abandon (func:AbandonScope) | EXCLUDED | Leadership write path that ends a weighing bucket whose work will never finish, WITHOUT the verification gate that the normal close now enforces. Deliberately a separate endpoint/event (`weighing.shed.abandoned`, audit `weighing.scope_abandoned`) so "ended without verification" is never mistaken for "verified and closed". It adds no new CEO assistant read API, Cube metric, MCP/Toolbox tool, or `ceo_ai` SQL fallback surface; leadership sees the outcome through existing `GET /weighing/campaigns` status reads. |
| func:AbandonScope, func:pendingVerificationCount (weighing close gate) | EXCLUDED | Write-path helpers for the maintainer-decided close gate: normal close is blocked while any submitted weighing video is still unverified, and abandon is the explicit reason-bearing way out. Neither adds a leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| POST /app/weighing/campaigns/{campaign_id}/animal-observations, /shed-observations | EXCLUDED | Operator write-flow submissions with mandatory proof; leadership sees progress/review state through `/weighing/campaigns`. |
| app_events | EXCLUDED | Mobile analytics write/audit receipt table (`table:app_events`, `POST /app/analytics/events`, func:NewHandler, func:Register, func:RecordEvent), not a leadership assistant read surface. It mirrors client-side Firebase events into `analytics.app_events` with request/client/device metadata so operators can prove scan/button telemetry was accepted by the backend even when the Firebase console lags. It adds no CEO assistant read API, Cube metric, MCP/Toolbox tool, or `ceo_ai` SQL fallback surface; leadership answers remain on the covered module read APIs and reporting views. |

The four module Alerts rows below describe pre-cutover compatibility feeds only.
Their `notification_requests` rows are delivery evidence, never task/work truth.
After each module's cutover, its route may remain as a module-scoped lens over
shared task/contact state, but the direct module feed must be suppressed before
shared contacts activate and retired only after replay, parity, and zero-use
proof.

| GET /app/weighing/alerts (func:ListAlerts, weighing service + postgres adapter) | EXCLUDED | **Legacy compatibility inbox until shared-task cutover; not future task/contact authority.** The weighing module's own lifecycle ALERTS feed reads work-state transitions already routed to the caller from `notification_requests`. At cutover, suppress this direct lane before shared contacts activate; Today/alerts read shared task truth, and retire the endpoint only after replay and zero-use proof. It remains a per-recipient inbox rather than a CEO metric. Weighing isolation is unchanged: no goat/herd/obligation/protocol/vaccination join or expected-roster denominator is permitted. |
| GET /app/vaccination/alerts (func:ListAlerts, vaccinationexecution service + postgres adapter) | EXCLUDED | The vaccination module's own lifecycle ALERTS feed, and the exact twin of the weighing row above. It reads back the vaccination work-state transitions already ROUTED TO THE CALLER (`vaccination.record.closed`, `vaccination.proof.rework`, `vaccination.proof.approved`, `vaccination.proof.pending.verifier`, `vaccination.proof.pending.leadership`) from `notification_requests`, discriminated by `context->>'message_key' LIKE 'vaccination.%'`. It is a PER-RECIPIENT INBOX, not a reporting surface: every row is filtered to one person's `context->>'member_id'` and bounded to a rolling 30-day window, so it can answer "what happened to me lately" and never "how is vaccination going". Asking it a leadership question would return only the subset of transitions that happened to be routed to that principal, which is strictly worse and non-deterministic next to the real rollups. Leadership vaccination coverage is unchanged and stays on the governed vaccination aggregates, `ceo_ai.*` views and the vaccination read APIs. No new Cube metric, `ceo_ai.*` view, MCP/Toolbox tool, or KPI is added. Notification DELIVERY health keeps its own view (`ceo_ai.notification_delivery_health`) and is untouched. |
| GET /app/feed/alerts (func:ListAlerts, feeddirection service + postgres adapter) | EXCLUDED | The feed module's own lifecycle ALERTS feed, the exact twin of the weighing/vaccination rows above. It reads back the feed work-state transitions already ROUTED TO THE CALLER (`feed.record.closed`, `feed.proof.rework`, `feed.proof.approved`, `feed.proof.pending.verifier`, `feed.proof.pending.leadership`, covering both gated feed completions — packing and distribution — plus feed transport) from `notification_requests`, discriminated by `context->>'message_key' LIKE 'feed.%'`. It is a PER-RECIPIENT INBOX, not a reporting surface: every row is filtered to one person's `context->>'member_id'` and bounded to a rolling 30-day window, so it can answer "what happened to me lately" and never "how is feed going". Leadership feed coverage is unchanged and stays on the governed feed read APIs (`/feed-direction/preview`, `/feed-packing/worklist`, `/feed-transport/tasks`) and any existing `ceo_ai.*` feed views. No new Cube metric, `ceo_ai.*` view, MCP/Toolbox tool, or KPI is added. Notification DELIVERY health keeps its own view (`ceo_ai.notification_delivery_health`) and is untouched. |
| GET /app/counts/alerts (func:ListAlerts, counts service + postgres adapter) | EXCLUDED | The counts module's own lifecycle ALERTS feed, the exact twin of the weighing/vaccination/feed rows above. It reads back the counts (shifting/movement) work-state transitions already ROUTED TO THE CALLER (`counts.record.closed`, `counts.proof.rework`, `counts.proof.approved`, `counts.proof.pending.verifier`, `counts.proof.pending.leadership`) from `notification_requests`, discriminated by `context->>'message_key' LIKE 'counts.%'`. It is a PER-RECIPIENT INBOX, not a reporting surface: every row is filtered to one person's `context->>'member_id'` and bounded to a rolling 30-day window. COUNTS IS AN OFF FEATURE (AGENTS.md) and this route does not change that — it is gated on the dedicated `counts.alerts_read` permission (health_director) plus `counts.write`/`verification.review`, never `counts.read`, so it cannot be mistaken for turning the Counts census surface on. Leadership counts coverage is unchanged; no new Cube metric, `ceo_ai.*` view, MCP/Toolbox tool, or KPI is added. Notification DELIVERY health keeps its own view (`ceo_ai.notification_delivery_health`) and is untouched. |
| func:RegisterVerificationAppliers | EXCLUDED | Phase 1 write-path and verification consumer plumbing; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. |
| func:MarkVerdictApplied, func:AckWeighingVerificationApplied, func:VerdictState, func:WithApplyAcker | EXCLUDED | The verdict APPLY-ACK seam. A verifier's decision is applied asynchronously by a durable-bus consumer, so "decided" and "in effect" used to be indistinguishable from every read surface — the item left the pending queue the instant the verdict was recorded, which read as done even when the applier had not run. These carry the receipt (`applier_ack_expected` / `applied_at`) and derive `awaiting_review | applying | settled` for the VERIFIER's own queue. Per-item workflow state for the person acting, not a reporting surface: leadership weighing coverage stays at `ceo_ai.weighing_capture_activity` + `ceo_ai.weighing_verification_status` (migration 000080) plus `GET /weighing/campaigns`. No new Cube metric, `ceo_ai.*` view, or Toolbox tool. |
| func:RecordOutboxFailed | EXCLUDED | Outbox TERMINAL-visibility instrumentation. An `invalid_event_envelope` or permanent publish failure previously emitted no log and no counter, so a domain event could die with nobody informed — 12 did. This adds a WARN line and a `kernel.outbox.failed` counter. Platform diagnostics consumed by logs/metrics, never by leadership: delivery health already has `ceo_ai.notification_delivery_health`. No new read API, Cube metric, `ceo_ai.*` view, or Toolbox tool. |
| func:Error, func:Unwrap (weighing ports.CaptureIncomplete) | EXCLUDED | Error-type plumbing for the submit-time weight+video pair gate. Enforcement failing silently as `RowsAffected()==0` used to surface as a 404 about a shed the operator is standing in; this carries which animals lack a weight or a finished video so the client can name them. Operator-facing error copy, not a leadership read. |
| func:NewWeighingReworkDigestStage, func:SweepReworkDigests, func:Name, func:Run (weighing rework digest stage) | EXCLUDED | **Legacy compatibility contact stage.** It currently coalesces rework pushes and is not a leadership read. Shared task sign-off/rework/contact policy becomes sole authority at cutover; shadow, dedupe, and suppress this direct sender before activation, then retire it after retained-event and zero-use proof. |
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
| GET /action-center/obligations | api + view:action_center_current + external MCP:get_action_center | Cross-domain queue; API tier executor wired (action_center_obligations tool). External MCP clients must use the typed `get_action_center` tool for "what needs action / blocked / overdue / at-risk" questions so Action Center process-integrity rows are not mixed with operator schedule totals or proof-verdict state. Golden eval question: `action-center` (`tools/ceo-ai/eval/golden/ops-workforce.json`). |
| GET /feed-direction/preview | api + view:feed_direction_current + external MCP:get_feed_today | Feed needed today; blocked≠0. External MCP clients must use the typed `get_feed_today` tool for issued feed-sheet questions and must keep blocked/null quantities as missing configuration rather than zero feed. Golden eval questions: `feed-today`, `feed-blocked` (`tools/ceo-ai/eval/golden/feed-shifting-procurement.json`). |
| GET /feed-direction/generation-preview | api + view:feed_direction_current | Planned generation + gaps |
| GET /feed-direction/counts-projection/exceptions | api + view:ops_exception_queue | Blocked feed cells |
| Low-stock feed alert surfaces (func:LowStockFeeds, func:NewFeedLowStockStage, func:Name, func:Run, func:WithLocationNames, func:NewFeedLowStockNotifier, func:WithClock, func:NotifyLowStock, func:NormalisePackingVariancePage, func:FarmDate, func:FarmDateFromBusinessDate) | api + view:feed_direction_current + view:notification_delivery_health + EXCLUDED helpers | `LowStockFeeds` reads the same farm/feed stock balance and days-left facts surfaced on Feed Analytics Stock and covered by `feed_direction_current`; `NotifyLowStock` / `NewFeedLowStockStage` only queue one actionable notification per low farm/feed through the shared notification pipeline, so delivery health stays covered by `notification_delivery_health`. The notifier constructors, stage `Name`/`Run`, variance page normalizer, and farm-date formatters are plumbing/presentation helpers with no new leadership fact; they are named here so assistant coverage remains explicit. |
| GET /feed-packing/worklist | api | Packing worklist |
| Feed packing reopen push surface (func:NewFeedPackingReopenNotifyConsumer, func:Register, func:HandleEvent) | api + view:verification_queue_status + EXCLUDED notification plumbing | A feed-packing rejection/reopen still lands back in the existing operator packing worklist and verification backlog coverage. The new notification consumer only converts the already-covered `feed.packing.reopened` event into an operator push naming the pen, session, old packed-against quantity, and corrected quantity so the operator knows what to redo. It creates no new leadership KPI, `ceo_ai.*` view, Cube metric, MCP Toolbox tool, or read API; leadership sees the reopened execution state through `GET /feed-packing/worklist` and proof backlog state through `view:verification_queue_status`, never by reading the push-copy helper. |
| GET /feed-transport/tasks | EXCLUDED | Operator-owned, shed-grain today-task execution list; leadership verification backlog remains covered by view:verification_queue_status. |
| POST /feed-transport/tasks/{task_id}/submit | EXCLUDED | Operator evidence mutation, not a leadership read. Resulting verification state is covered by view:verification_queue_status. |
| GET /feed-config/ration-rates (func:ListRationRates, func:ParseGramsOp) | api + view:feed_direction_current | Config behind feed cost. The new filters only narrow the admin Feed Config grid by breed, feed item set, and authored grams comparison; `ParseGramsOp` is wire-token validation for that grid filter and derives no new fact. Leadership feed answers still consume issued directions/cost through `feed_direction_current`, `/feed-direction/preview`, and `/feed-packing/worklist`, not the editable config table or its filter parser. |
| GET /feed-config/ration-groups | EXCLUDED | Config taxonomy reference |
| GET /feed-config/shed-tags | EXCLUDED | Config mapping |
| GET /feed-config/feed-items | EXCLUDED | Reference catalog |
| GET /feed-config/session-templates | EXCLUDED | Config templates |
| GET /feed-config/schedule | EXCLUDED | Config |
| GET /feed-config/shed-factors | EXCLUDED | Config |
| GET /feed-config/experiment (func:ListExperimentConfig) | EXCLUDED | Experiment config; niche. The read now treats `park_id` as OPTIONAL on this one endpoint and adds authoring-grid filters, but an experiment cell carries its own park, so an absent park means "every authored experiment in the tenant" instead of silently rendering one park's pens as the whole company. That widens what the AUTHORING screen can show; it adds no leadership fact. The authored quantities were already excluded config, and the feeding they direct stays covered by `/feed-direction/preview`, `/feed-packing/worklist`, and the feed aggregate rows below. No new Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or KPI. |
| GET /feed-config/pens (func:ListPens) | EXCLUDED | The experiment enroller's candidate picker: a park's active operational locations — each shed, and each pen of a subdivided shed — flagged with whether that pen already carries experiment config. A CONFIG-authoring input at the same grain as `/feed-config/shed-tags` and `/feed-config/shed-factors`, which are excluded above for the same reason. It reports no animal, no quantity, and no execution state; the location catalog it lists is already leadership-visible through the census and feed reads, and per-partition feed answers stay excluded exactly as the partition-primitives row records. Park-scoped by requirement, so it is also not a tenant-wide location dump. No new Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or KPI. |
| POST /feed-config/experiment/batch (func:UpsertExperimentConfigBatch, func:NormalizeFeedItemKey) | EXCLUDED | Feed Config authoring WRITE, the atomic twin of the already-excluded single-cell `POST /feed-config/experiment`: it authors every feed item of ONE pen in one all-or-nothing write so a pen is never left half-enrolled. It is the same authored surface on the same `feed.config.write` permission, split onto its own route only for that guarantee, and it creates no new fact — the resulting quantities are read back through `GET /feed-config/experiment`, excluded above. `NormalizeFeedItemKey` is the Go twin of the Postgres `feed_config_norm` function (trim, casefold, collapse separators) and exists solely to reject two spellings of one feed item inside a single batch before they race onto the unique index; it derives nothing and reads no data. Leadership feed coverage is unchanged and stays on `/feed-direction/preview`, `/feed-packing/worklist`, and the feed aggregate rows below. No new Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or KPI. |
| GET /procurement/source-entry/loads | api + view:procurement_pipeline / Cube:procurement_cost + external MCP:get_procurement_pipeline | Open loads / pipeline; API tier executor wired (procurement_source_entry_loads tool). External MCP clients must use the typed `get_procurement_pipeline` tool for open source-entry load questions and keep expected/received/accepted/rejected/holding states distinct. Golden eval question: `proc-open-loads` (`tools/ceo-ai/eval/golden/feed-shifting-procurement.json`). |
| GET /procurement/source-entry/loads/{load_id} | api + view:source_entry_health_status | Load drilldown |
| GET /procurement/feed-purchases, GET /procurement/feed-purchase-options, POST /procurement/feed-purchases/{purchase_id}/payments, PUT /procurement/feed-purchases/{purchase_id}/payment-status, PUT /procurement/feed-purchases/{purchase_id}, PUT /procurement/feed-purchases/{purchase_id}/delivery, feed_purchase_payments | api + view:feed_direction_current / Cube:procurement_cost | Feed purchase ledger entry, page totals, edit, and instalment/payment-state evidence. Leadership assistant coverage stays on existing feed stock/cost and procurement-cost facts: the ledger records app-entered feed loads into `feed_purchases`, and stock/cost answers already read purchases through `view:feed_direction_current` / procurement cost coverage rather than calling the authoring form. The read endpoint exposes only the bounded admin page plus whole-filter totals for that same ledger; the options endpoint is form vocabulary. `feed_purchase_payments` is row-level payment evidence behind one purchased load, not a new leadership aggregate; paid-so-far and remaining-balance facts stay attached to the bounded feed-purchase API and procurement-cost/feed-stock answer paths until a dedicated payable-aging aggregate is introduced. New write-path and presentation helpers are explicitly not standalone leadership surfaces: `func:NewFeedPurchaseHandler`, `func:RegisterFeedPurchases`, `func:ListFeedPurchases`, `func:FeedPurchaseOptions`, `func:CreateFeedPurchase`, `func:RecordFeedPurchasePayment`, `func:SetFeedPurchasePaymentStatus`, `func:EditFeedPurchase`, `func:UpdateFeedPurchase`, `func:FeedPurchaseHTTPError`, `func:NewFeedPurchaseService`, `func:NewFeedPurchaseServiceWithClock`, `func:ClampFeedPurchasePageSize`, `func:IsFeedFarm`, `func:NormalizeFeedFarmFilter`, `func:PaymentBalance`, `func:Error`, `func:Normalize`, `func:TotalOrSplitSum`, `func:PerKgCost`, `func:Validate`, `func:IsFeedPaymentStatus`, `func:NormalizeFeedPaymentStatus`, `func:DeriveFeedPaymentStatus`, `func:RecordFeedPurchaseDelivery`, `func:NormalizeFeedDeliveryFilter`, `func:DeriveFeedPerKgCost`, `func:StockKg`, `func:IsReached`. The delivery write (maintainer decision 2026-09-03: a load is stock when it REACHES, at the weight received) changes no leadership surface: stock/days-left answers keep reading the same `feed_purchases` ledger through the covered stock read, which now counts only reached loads at `stock_kg`; "what is still on the road" is the bounded admin ledger's `delivery=purchased` filter, not a new aggregate. |
| GET /app/toxin/tasks, GET /toxin/review, GET /app/toxin/tasks/{task_id}, POST /app/toxin/tasks/{task_id}/steps/{step_no}, POST /app/toxin/tasks/{task_id}/reading, POST /toxin/tasks/{task_id}/verdict, table:toxin_test_tasks, table:toxin_test_step_completions | api + view:feed_direction_current / Cube:procurement_cost | Aflatoxin strip-test gate for purchased feed loads. The leadership fact is load safety state for feed already covered by procurement/feed stock answers: pending/in-progress strip tests, negative/positive/invalid outcomes, CEO/CXO verdict state, retest lineage, and proof-backed step completion. No new Cube metric or `ceo_ai.*` view is introduced in this slice; assistant feed-safety answers should route through the bounded Mesha read APIs until a toxin aggregate view exists. Helper/factory and presentation names are explicitly covered or excluded here so the coverage guard sees the new package as intentional: `func:NewHandler`, `func:Register`, `func:ListTasks`, `func:ListReview`, `func:GetTask`, `func:CompleteStep`, `func:SubmitReading`, `func:RecordVerdict`, `func:NewRepository`, `func:CreateTaskFromPurchase`, `func:NewValidator`, `func:ValidateToxinStripPhoto`, `func:ValidateToxinStepVideo`, `func:Error`, `func:BadRequest`, `func:Conflict`, `func:Unprocessable`, `func:NotFound`, `func:Internal`, `func:HTTPError`, `func:NewFeedPurchaseReachedHandler`, `func:HandleEvent`, `func:NewService`, `func:WithClock`, `func:ClampPageSize`, `func:Now`, `func:Is`, `func:Steps`, `func:StepSpecFor`, `func:WorkingSteps`, `func:GateOpensAt`, `func:CheckStepCompletable`, `func:ValidateOutcome`, `func:SubmitDecision`, `func:VerdictDecision`, `func:OutcomeLabel`, `func:OriginLine`, `func:StatusChip`, `func:NextStepNo`, `func:ReadingGuide`, `func:TaskFilters`, `func:StatusesForFilter`, `func:FilterKeyOrDefault`, `func:CountForFilter`, `func:ResolveSexScopeWithAllTime`. Pure helpers derive labels, scopes, timing gates, or HTTP errors from already-covered toxin/feed facts; write handlers mutate task state and are not standalone assistant tools. |
| toxin_test_tasks | api + view:feed_direction_current / Cube:procurement_cost | Explicit parser-visible coverage row for the toxin task table; see the toxin API row above for the read path and helper/function coverage. |
| toxin_test_step_completions | api + view:feed_direction_current / Cube:procurement_cost | Explicit parser-visible coverage row for the toxin proof-step table; see the toxin API row above for the read path and helper/function coverage. |
| GET /app/leadership-tasks, GET /app/leadership-tasks/assignees, GET /app/leadership-tasks/{task_id}, POST /app/leadership-tasks, POST /app/leadership-tasks/{task_id}/edit, POST /app/leadership-tasks/{task_id}/status, POST /app/leadership-tasks/{task_id}/seen, table:leadership_tasks, table:leadership_task_attachments | excluded | Leadership Tasks (maintainer decision 2026-09-04, `docs/decisions/leadership-tasks-module.md`): a director's private ask of one CXO -- a brief plus voice note / gallery media / files -- with an open/in_progress/done status the CXO owns. It is personal leadership correspondence, not a farm KPI, an operational obligation or a herd fact, so it is deliberately NOT exposed to the assistant: no Cube metric, no `ceo_ai.*` view, no MCP tool. Helper/factory and presentation names are excluded here so the coverage guard sees the new package as intentional: `func:NewHandler`, `func:Register`, `func:ListTasks`, `func:ListAssignees`, `func:GetTask`, `func:Raise`, `func:Edit`, `func:ChangeStatus`, `func:MarkSeen`, `func:UnseenCount`, `func:ModuleBadgeCounts`, `func:NewRepository`, `func:WithClock`, `func:NewResolver`, `func:ResolveAttachments`, `func:Error`, `func:BadRequest`, `func:Forbidden`, `func:Conflict`, `func:Unprocessable`, `func:NotFound`, `func:Internal`, `func:HTTPError`, `func:NewService`, `func:ClampPageSize`, `func:Now`, `func:IsOpenForWork`, `func:IsKnownStatus`, `func:IsKnownAttachmentKind`, `func:ValidateBrief`, `func:IsRaiser`, `func:IsAssignee`, `func:CanEdit`, `func:CanCancel`, `func:CanChangeStatus`, `func:StatusOptionsFor`, `func:CheckTransition`, `func:StatusChip`, `func:NumberLabel`, `func:RaisedOnLabel`, `func:MetaLine`, `func:FilterKeyOrDefault`, `func:StatusesForFilter`, `func:FilterLabel`, `func:FilterEmptyMessage`, `func:FilterCount`, `func:NewLeadershipTaskNotifyConsumer`, `func:HandleEvent`, `func:WithModuleBadges`. |
| leadership_tasks | excluded | Explicit parser-visible exclusion row for the leadership task table; see the Leadership Tasks API row above for the reason (private leadership correspondence, not a leadership fact). |
| leadership_task_attachments | excluded | Explicit parser-visible exclusion row for the attachment pointer table; same reason. |
| goat_sale_allocations | api | Sale-allocation operational drilldown for the already-covered Sales page: records which real goats make up one recorded sale and snapshots their sale-time location/tag so leadership can reconcile a sale's stated animal count against the animals that physically left. Covered through the Mesha read API attached to `/sales`: `func:NewSaleAllocationHandler`, `func:RegisterSaleAllocation`, `func:ListSaleCandidates`, `func:PreviewSaleAllocation`, `func:ConfirmSaleAllocation`, `func:GetSaleAllocation`, `func:ListSaleLocations`, `func:ReadSaleCandidateRows`, `func:ListSaleAllocations`, `func:RecordSaleAllocations`, `func:New`, `func:ReadSaleDeal`, `func:NewSaleAllocationService`, `func:GetSaleLocations`, `func:IsClinicalSaleState`, `func:IsExitedLifecycle`, `func:IsMilkDrinkingStage`, `func:Sellable`, `func:ResolveSaleBlocker`, and `func:Remaining`. No new Cube metric or `ceo_ai.*` view is introduced in this slice: aggregate sales value/count questions stay on the sales/procurement coverage, while this table is per-deal evidence and picker/confirm plumbing. |
| GET /admin/roster/positions | api + view:workforce_coverage_status | Who owns which shed |
| GET /admin/roster/positions/{position_id} | EXCLUDED | Single-seat detail. Repo read `GetPositionByID` backs this single-seat drawer only; leadership capacity/coverage answers aggregate through `GET /admin/roster/positions` + `view:workforce_coverage_status`, never a named individual seat. |
| GET /admin/roster/coverage | api + view:workforce_coverage_status + external MCP:get_workforce_coverage | Coverage matrix; API tier executor wired (admin_roster_coverage tool). External MCP clients must use the typed `get_workforce_coverage` tool for uncovered/weakly covered shed, role, and backup-manager questions; do not answer these from Action Center rows. Golden eval question: `workforce-coverage` (`tools/ceo-ai/eval/golden/ops-workforce.json`). |
| GET /admin/roster/leave | api + view:workforce_coverage_status | Absence exposure |
| GET /admin/roster/leave/{absence_id} | EXCLUDED | Single-record detail |
| POST /admin/roster/leave/{absence_id}/resolve-coverage | EXCLUDED | Single-absence coverage mutation (`ResolveLeaveCoverage`), not a leadership read. It is a vaccination-planning-effective transition: it enqueues `vaccination.leave.changed` in the same transaction so the operator-config replan consumer releases/re-plans that park's future drives. Leadership sees the RESULT through `view:workforce_coverage_status` and the vaccination operator/date surfaces, never this write. |
| PUT /vaccination/capacity-config (func:PutCapacityConfig, func:UpdateCapacityConfig, func:UpsertCapacityConfig, func:WithCapacityConfigWriter) | EXCLUDED | Admin config WRITE, not a leadership read. Edits the tenant operator daily-animal cap + nullable per-animal shot-cap override on the People/vaccination-operators screen. It is vaccination-planning-effective: `UpsertCapacityConfig` fans `vaccination.capacity.changed` per active park in the same transaction so the replan consumer re-plans future drives. Leadership sees the RESULT (capacity/throughput) through the vaccination operator/date and drive surfaces, never this write endpoint. |
| vaccination_capacity_config | api + view:vaccination_operator_status | Tenant vaccination capacity config is read by the scheduler as a fallback cap and by `ceo_ai.vaccination_operator_status` for leadership workload/capacity answers. Migration `000234_vaccination_assignment_lane_identity.sql` tightens the operator planning identity/cap constraints without introducing a new leadership read surface; capacity answers continue through the vaccination schedule/operator status coverage. It is not an animal/vaccine obligation fact; row-level edits remain covered as admin writes by `PUT /vaccination/capacity-config`, while aggregate capacity utilization stays on `/vaccination/schedule` and `view:vaccination_operator_status`. |
| func:ApplyCapacityShotCapOverride | EXCLUDED | Obligation-sweeper planner helper that applies the capacity-config per-animal shot-cap override (or falls back to rule_dsl/default). Internal scheduling logic, not a leadership read surface. |
| func:RealignOpenObligationForGeneration | EXCLUDED | Internal obligation-generation repair that atomically moves an already-open, stable adult blank-history campaign row when later same-vaccine history identifies the normal repeat-drive date. It is a scheduler write helper, not a leadership read surface. Leadership sees the resulting operator/day totals through `GET /vaccination/schedule`, `GET /calendar/vaccination/events`, and `view:vaccination_operator_status`. |
| func:ManualVaccineAnchorsForGoat | EXCLUDED | Internal obligation-generation guard for manual vaccination anchor dates. It batch-reads whether one animal already has current/future open manual-campaign anchors for the same vaccine families so DOB/arrival/calendar base rules do not recreate pre-anchor work; old past `missed` anchors are not treated as active baselines. It is not a leadership read API, Cube metric, `ceo_ai.*` view, or MCP tool; leadership sees the resulting planned drive dates and completion state through the existing vaccination schedule, calendar, and command-board surfaces. |
| obligation_instances_unlabelled_identity_lookup_idx, func:CancelOpenVaccinationObligationsForExitedGoats | EXCLUDED | Internal vaccination scheduler repair only. The index and cleanup helper let generation cancel stale open work for exited or procurement-excluded animals and avoid repeated per-goat reconciliation probes; they add no new leadership KPI, read API, `ceo_ai.*` view, Cube metric, MCP Toolbox tool, or read-only SQL fallback. Leadership sees the resulting schedule state through existing vaccination schedule, calendar, and command-board surfaces. |
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
| sop_submissions.partition_label (migration 000147) | EXCLUDED | Runtime submit identity for partition-scoped SOP/vaccination evidence, not a new leadership read surface. It lets existing SOP submission reads disambiguate whole-shed vs partition work; clean-slate seed starts with no submissions, and leadership SOP execution coverage remains on `view:sop_execution_status` plus the existing admin task/submission fanout APIs. |
| SOP submission fanout retry worker (func:NewSopSubmissionFanoutRetryStage, func:SopSubmissionFanoutRetryStage.Run, func:SopSubmissionFanoutRetryStage.Name) | api + view:ops_exception_queue | Operational repair surface for submitted proof fanouts that failed before vaccination completions / verification rows materialized. Leadership does not call the worker directly; failures remain visible through `GET /admin/tasks/submission-fanouts/failed` / ops exception coverage, and the kernel worker retries them durably. |
| GET /app/tasks(+/{id}, /shed-completion-summary), /app/sop-versions/{id} | EXCLUDED | Self-scoped operator worklist / form |
| GET /verification/queue | api + view:verification_queue_status + external MCP:get_verification_backlog | Verification backlog; API tier executor wired (verification_queue tool). External MCP clients must use the typed `get_verification_backlog` tool for proof backlog questions; pending verification is evidence waiting for review, not completed work. Golden eval question: `verification-queue` (`tools/ceo-ai/eval/golden/ops-workforce.json`). |
| POST /verification/review-events | EXCLUDED | Write-only client telemetry ingest (verifier video-review analytics flush); not a leadership read |
| GET /verification/items/{item_id}/review-facts | tool:mesha_verifier_review_integrity (per-item drilldown) | Per-item derived watch/timing facts. The tenant-wide aggregate is `ceo_ai.verifier_review_integrity` / `mesha_verifier_review_integrity` (G14, CLOSED, section D); this per-item endpoint is the drilldown a leadership follow-up ("show me item X") would still need a direct backend call for — not itself the aggregate answer path. |
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
| func:GetWeightHistory, func:GetLeadershipGrowthADG (weighing read-model functions) | api + external MCP:get_weighing_growth_adg | Leadership weighing historical trend and growth metrics. Covered by new read model functions that back leadership reporting. Admin-web Growth Dashboard reads through these functions; leadership assistant coverage through same endpoints + `ceo_ai.weighing_capture_activity` (migration 000080). External MCP clients must use `get_weighing_growth_adg` for growth/ADG trend questions instead of the broader campaign list. No new Cube metric, `ceo_ai` view, or Toolbox tool beyond the function itself. SEX FILTER + ONE GAIN NUMBER (maintainer decisions 2026-08-26): `GetLeadershipGrowthADG` takes a `sex` argument that narrows every figure it returns to that half of the herd, and its headline field is now `average_adg_g_per_day` (was `median_adg_g_per_day`) with `headline_animals` as its denominator -- the animal-weighted mean over kids weighed twice PLUS whole-shed pens, which is the identical statistic the Weights gain charts report per dimension. That is a RESHAPE of an already-covered leadership figure, not a new surface: the same endpoint answers the same question for the same audience, so coverage stays on this row and `get_weighing_growth_adg`. An MCP client reading the old field name must move to the new one; there is no second number to choose between any more, which is the point of the change. |
| func:GetShedWeights, func:GetWeightDemographics (weighing Weights read-model functions) | api + external MCP:get_weighing_shed_weights + external MCP:get_weighing_weight_demographics | Leadership/admin-web Weights page read models. `GetShedWeights` serves the bounded shed-grain table plus whole-filter KPI summary over the caller's authorized park scope and resolved business-date window. `GetWeightDemographics` serves breed/sex/stage weight and daily-gain breakdowns for the same Weights page, using the recorded weighing isolation exception for tag-to-animal demographic resolution. External MCP clients must use `get_weighing_shed_weights` for lagging/latest shed-weight questions and `get_weighing_weight_demographics` for demographic comparisons. Leadership assistant coverage stays on the Mesha read API tier and existing weighing reporting surfaces (`GET /weighing/campaigns`, `ceo_ai.weighing_capture_activity`, `ceo_ai.weighing_verification_status`); no new Cube metric or Toolbox tool is introduced in this slice. SEX + ORIGIN FILTERS (maintainer decisions 2026-08-26 and 2026-09-01): both functions take `sex` and `origin` arguments, where origin is resolved per animal from procurement membership rather than from a shed-level load tag. The filters narrow KPIs, the table, demographic breakdowns, gain charts, load-wise blend, and unattributed counters alike, because a page whose cards disagree about which kids they counted has no true number on it. `GetShedWeights` additionally returns `latest_weighing_date`, the last business day the farm weighed anything of EITHER grain, so the page's landing window can end on a day that carried only scanned kids and no whole-shed weigh. Neither adds a leadership question: they narrow and bound answers the existing coverage already gives, so this row stands unchanged in its artifacts. |
| GET /procurement/loadwise-sales, table:procurement_load_cost_lines, func:OverdueLoadCandidates, func:DaysSincePurchase, func:DaysOnFarmSoFar, func:FinalizeLoadwise, func:CostBucketForKind, func:RollUpCostLines, func:OverdueLoads, func:NewLoadAgeAlertStage, func:Run, func:NewLoadAgeNotifier, func:WithClock, func:NotifyOverdueLoads | api | Purchase and Born load economics and overdue-load alert coverage. The leadership assistant answer path remains the existing Mesha read API for load-wise procurement/sales reconciliation: `GET /procurement/loadwise-sales` now reports landed cost, live purchase weight, landing price per kg, sale weight/sample price per kg, fattening days, days-on-farm-so-far for still-open loads, and cost-line breakdowns, so procurement load profitability and vendor comparison questions continue to route through that API rather than a new Cube metric or `ceo_ai.*` view. `procurement_load_cost_lines` is detail behind the same load-wise row, rolled up by `RollUpCostLines` into the displayed animal/transport/other buckets; it is not a standalone leadership table. `DaysOnFarmSoFar` is the open-load companion to the existing fattening clock and is suppressed once a load is fully sold, so it changes row freshness, not assistant routing. `OverdueLoadCandidates`, `DaysSincePurchase`, `OverdueLoads`, and the load-age stage/notifier functions are alert-selection and delivery plumbing for the same read model: they create one CXO push for old open loads, but add no new assistant query surface or SQL fallback. |
| func:GetGrowthDirectorWeights, func:ResolveSexScope, func:ResolveOriginScope, func:ResolveOriginScopeWithAllTime, func:IntersectScopes, func:Empty (weighing sex/origin/mode scope + Growth Director read) | EXCLUDED | `GetGrowthDirectorWeights` is the analytics block UNDER the Weights page (road-to-sale bands, fair fight, slow growth, feed-vs-growth, capture trust). It is a composition of facts the existing weighing coverage above already reports, re-grained for one screen, and it introduces no leadership question those rows cannot answer; it carries the same `sex`, `origin`, and `weighing_category` narrowing as the Weights page so the director block does not mix individual-animal and whole-pen populations after a UI filter is selected. `ResolveSexScope`, `ResolveOriginScope`, `ResolveOriginScopeWithAllTime`, `IntersectScopes`, and `Empty` are the weighing isolation scope helpers, not reporting surfaces: they are the bounded files allowed to resolve scanned tags or whole-shed pens to an opaque cohort, and they hand the other reads only tag strings plus `(location, partition)` buckets so those reads still name no herd/procurement tables. They answer no question of their own, return no business fact, and an absent filter resolves to an empty scope every caller reads as "no filter". Leadership coverage for everything they narrow stays on the two rows above plus `GET /weighing/campaigns`; no Cube metric, `ceo_ai.*` view or Toolbox tool is added. |
| weighing_shed_load_tags | api | Shed -> procurement-load mapping behind the Weights page's load-wise growth chart (migration 000131). Animals are bought in LOADS from a named supplier and placed into sheds, so "which supplier's animals grow best" is a real leadership question — and it is answered through the EXISTING read: `GetShedWeights` returns the blend as `by_load` on the same response that already serves the shed table and KPI summary, so no new endpoint, Cube metric, `ceo_ai.*` view, or Toolbox tool is introduced. The table is authored reference data (load number, supplier name, shed id) and holds NO animal identity, no clinical state, and no weight — it is a dimension the existing weighing aggregates are grouped by, never a fact source of its own. It is deliberately NOT a foreign key to `procurement_loads`: weighing isolation (AGENTS.md) allows weighing three org tables, and the farm's mapping is shed-level, so weighing owns the tag rather than reading procurement. ATTRIBUTION IS REFUSED WHEN AMBIGUOUS: a shed carrying two loads is dropped from every load and reported in `load_unattributed_sheds`, so no leadership answer is derived by splitting one shed average between two suppliers. If leadership later asks a per-supplier question spanning modules ("which supplier's animals also fall sick most"), that becomes a real coverage row against a procurement aggregate, not this mapping. |
| func:ExportCampaignCSV, func:ExportCSV, GET /weighing/export.csv, func:ListParks (weighing campaign export) | api | Weighing campaign export functionality and park listing for planner/CEO. The broad CSV export is a user-initiated file download over the caller's authorized park scope and bounded weighing date window; it deliberately includes pending verification rows plus video proof reference/link columns so leaders can reconcile today's captured weights. Migration `000148` adds tenant/date access-path indexes for the broad export only; it creates no new fact table or leadership answer surface. Assistant aggregate answers remain covered by `GET /weighing/campaigns`, `GET /app/weighing/leadership/sheds`, and the weighing `ceo_ai` reporting views; no new Cube metric or Toolbox tool is introduced. |
| func:ReopenObligation, func:ReopenTaskForRework (obligation/weighing rework) | EXCLUDED | Write-path rework and obligation state helpers for verification rejection flow. Part of the existing verification verdict application; leadership sees result through `view:verification_queue_status`. No new assistant read API, Cube metric, or `ceo_ai` view. |
| func:ListExhausted, func:RequeueExhausted (outbox/event queue) | EXCLUDED | Operational kernel event delivery retry workers. Infrastructure-layer delivery-health instrumentation; no leadership read API, Cube metric, `ceo_ai` view, or Toolbox tool. Delivery health already covered by `ceo_ai.notification_delivery_health`. |
| func:DeviceIDFromContext, func:WithDeviceID (device context) | EXCLUDED | Device identity context plumbing for FCM and push notification routing. Internal middleware, not a leadership read surface. No new assistant tool, Cube metric, or `ceo_ai` view. |
| func:RetryableConflict, func:WithObligationCompleter, func:Error (error/helper types) | EXCLUDED | Internal error types and completeness helpers for obligation/verification workflows. Type definitions and interface plumbing, not leadership-facing reads. No new assistant tool, Cube metric, or `ceo_ai` view. |
| func:Write (miscellaneous write helper) | EXCLUDED | Internal write-path helper. Not a leadership read surface or API. |
| func:ListColostrumDay, func:GetColostrumDay, func:ColostrumDayCard, func:ColostrumDayWindow, func:ColostrumFilterAllowed, func:IsColostrumAction, func:Complete (colostrum day lens) + `GET /app/workflows?module=colostrum` | EXCLUDED | The Colostrum page in the Milk module (docs/decisions/colostrum-milk-module.md). A per-animal OPERATOR work list, at the same grain as the Birth/Death lists whose row-level detail is already excluded above (`GET /workflows/{row_id}`), and it introduces NO new fact: every row is an existing `workflow_actions` colostrum feed on a birth kid workflow, re-selected by the date each feed is due and re-counted at that day's grain. Nothing is written here that Birth does not already write, and no new table, event, or state exists to report on. Leadership birth/mortality answers stay on the governed Counts aggregates (`ceo_ai.counts_movement_daily`), exactly as the `table:goat_births` exclusion records. No new assistant read API, Cube metric, `ceo_ai` view, or Toolbox tool. If leadership later asks a colostrum-completion question ("how many kids missed a feed yesterday"), that becomes a real coverage row against a new aggregate — not this per-kid operator list. |

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
| feed_wastage_completions | EXCLUDED — operator pen-day wastage task state (experiment pens, one leftover-feed video, verifier-recorded kg); leadership sees pending evidence through verification_queue_status. The recorded wastage_kg becomes a leadership metric only when a governed feed-wastage aggregate is built; until then raw rows are execution/evidence state, not a CEO KPI. |
| feed_packing_verified_quantities (func:RecordPackingVerifiedQuantities, func:PackingVerifiedQuantitiesRecorded, func:NewPackingMeasurementApplier, func:ApplyMeasurement, func:HasRecordedMeasurement) | api:GET /feed-analytics/execution — verifier-entered packed kg rows for blind feed-packing proof review. Leadership coverage is the existing Feed Analytics execution read/API surface, which now compares the frozen planned packing sheet to these verified readings as `packing_variance`; no verifier surface receives planned quantities. The applier/store functions are write-path plumbing for the same table and add no standalone assistant read surface. |
| feed_transport_tasks | EXCLUDED — operator daily shed task state; leadership sees pending evidence through verification_queue_status, not this execution queue |
| feed_transport_attempts | EXCLUDED — immutable row-level proof-attempt history; leadership sees aggregate verification backlog through verification_queue_status |
| shifting_events, shifting_event_impacts, counts_approval_requests, count_projection_* | counts_movement_daily, ops_exception_queue. High-priority feed proof refs, fingerprint, and requirement snapshot are EXCLUDED evidence/config detail; verification backlog remains covered by verification_queue_status. |
| goat_births | EXCLUDED — per-child canonical mother relationship and delivery litter size used by the operator birth workflow; leadership birth/mortality reporting remains on governed Counts aggregates |
| procurement_loads, procurement_load_goats, arrival_intake_reviews, procurement_source_health_checks, procurement_hf_vaccination_evidence | procurement_pipeline, source_entry_health_status |
| sop_tasks, sop_submissions, sop_task_submission_fanouts | sop_execution_status, ops_exception_queue |
| sop_task_scan_captures | EXCLUDED (operator proof capture ledger) — scan-level evidence for mobile SOP task submission. Leadership reads SOP execution/proof state through `sop_execution_status`, `verification_queue_status`, and `ops_exception_queue`; raw scan rows are per-task evidence, not a leadership aggregate. |
| sop_task_scan_attempts | EXCLUDED (operator scan attempt ledger) — accepted/duplicate/not-due/unknown scan attempts used to debug mobile field capture. Failures surface through SOP submission/verification/ops exception read models; raw attempts are operational telemetry, not a CEO KPI or standalone assistant read. |
| verification_sampling_policies | EXCLUDED (CEO-set config: one share per tenant/category/business day; the day's effect is reported by GET /verification/sampling) |
| verification_items | verification_queue_status |
| verification_review_events (migration 000116) | view:verifier_review_integrity (G14, CLOSED) | Append-only client telemetry proving whether a verifier actually watched a proof video (`queue_opened`/`item_opened`/`video_play`/`pause`/`seek_attempt`/`ended`/`proof_switched`/`fullscreen_toggled`/`verdict_recorded`). Per-(item,actor) derived facts served per-item via `GET /verification/items/{item_id}/review-facts`; the tenant-wide aggregate across verifiers/parks/days is `ceo_ai.verifier_review_integrity` (migration 000118) / `mesha_verifier_review_integrity`. |
| inventory_items, inventory_stock, inventory_stock_movements | inventory_stock_position |
| workforce_members, workforce_positions, workforce_absences, workforce_roster_assignments, org_role_catalog | workforce_coverage_status |
| vaccination_drive_assignment_members | EXCLUDED (operational scheduler-written membership) — the exact obligation/goat set behind each `vaccination_drive_assignments` row. It exists so a death/sale/cull decrements the exact assignment arm and so CT/PA/WF/AC can report an animal's own operator-day instead of inferring it from an aggregate. Leadership never reads membership directly; it reads the drive/operator aggregates this table makes correct (`view:vaccination_operator_status`, `GET /vaccination/schedule`). |
| vaccination_prearrival_history_entries (migration 000041) | vaccination_prearrival_history_review — COVERED, not excluded. The accepted/rejected split on supplier-attested pre-arrival vaccination claims for PROCURED animals is a real leadership signal (trusted-history share, rejected-claim rate and reason = supplier data quality + avoided re-injection). Coverage: `ceo_ai.vaccination_prearrival_history_review` (migration 000043) → MCP Toolbox tool `mesha_prearrival_history_review` (in `mesha_ceo_toolset`) → read-only SQL fallback over the same view. No governed Cube metric yet: rejected-rate is not an official tracked KPI today, so this stays tier-3/4 (add a Cube metric if leadership starts trending it). Golden eval question: `prearrival-history-rejected-share` (`tools/ceo-ai/eval/golden/vaccination.json`). |
| weighing_campaigns, weighing_campaign_sheds, weighing_observations, weighing_shed_observations, weighing_work_items | `ceo_ai.weighing_capture_activity` + `ceo_ai.weighing_verification_status` (migration 000080) — COVERED for immutable Weighing campaign/capture/proof/verdict facts. The views remain herd-isolated and never join goat/animal, protocol, vaccination, or clinical state. Partitioned sheds use operational-shed grain: `weighing_campaign_sheds.location_id` is the parent physical shed and `partition_label` distinguishes siblings such as Castro - 1/Castro - 2. After shared-task cutover, owner/clock/Today/delay/contact/sign-off/hierarchy status comes from governed shared task reads; `weighing_work_items` remains only a legacy/source reconciliation input and cannot compete as task truth. `GET /weighing/campaigns` remains the leadership source planning/capture API. |
| weighing_expected_animals | REMOVED — not a coverage gap. Dropped outright by migration `000079` when weighing became fully free-flow: a weighing scan has no expected animal set, so there is nothing left to report. Named here so the surface is explicitly closed rather than silently disappearing. See also `000078` (dropped `weighing_observations.animal_id`) and `tools/agent-hooks/check-weighing-free-flow-guard.mjs`. |
| workforce_clock_events, workforce_clock_entries (migration 000221) + GET/POST /app/clock/* (func:NewClockService, func:Punch, func:Status, func:Presence, func:PersonDay, func:AdminEntries, func:EntryDetail, func:RecordClockPunch, func:ClockDayForMember, func:ListClockPresence, func:ClockPersonDayDetail, func:ClockEntryDetail, func:NewClockHandler, func:RegisterClock, func:ClockIn, func:ClockOut) + GET /admin/workforce/clock-entries | EXCLUDED (scoped: V1 attendance ledger, docs/features/clock-in-out/plan.md) — Clock In/Out punches and the per-person-per-IST-day pairing rows behind them. Leadership already has a governed LIVE read on both surfaces: the phone Team presence board (GET /app/clock/presence, clock.presence.read) and the admin-web People/HRMS clock tab (GET /admin/workforce/clock-entries) — both served from ONE repository page so the numbers cannot diverge. The raw tables are per-punch evidence (location, device, integrity flags), the same class as the excluded scan-capture ledgers above. When leadership starts asking the ASSISTANT attendance questions ("who was late this week", average hours by park), that becomes a real coverage row against a `ceo_ai.attendance_daily` aggregate + Toolbox tool + golden eval — a follow-up named in the plan doc, not a silent gap; until then the bot scope-refuses. |
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
| G14 | Verifier video-review integrity (CEO "did the verifier actually watch it" question) | CLOSED (2026-08-06): `ceo_ai.verifier_review_integrity` (migration 000118) — one row per (verifier, park/category, business_day): videos reviewed, median/p90 time-to-verdict, median watch fraction, `below_watch_threshold_count` (verdicts recorded under the 0.9 `WatchedFullThreshold` watch-fraction gate — the integrity signal), `missing_review_telemetry_count` (reported separately — no telemetry sent, not proof either way), reject rate, and a reject-reason breakdown, aggregated from `verification_review_events` (migration 000116) joined to `verification_items`. Served via MCP Toolbox tool `mesha_verifier_review_integrity` (in `mesha_ceo_toolset`), tier-3 SQL fallback over the same view; no governed Cube metric yet (consistent with the sibling `verification_queue_status`/`mesha_verification_queue` surface, which is also Toolbox-only, not Cube). Supporting index: `verification_items_verified_by_review_idx` (migration 000117, partial on `verified_by IS NOT NULL`). Golden eval question: `verifier-rubber-stamping` (`tools/ceo-ai/eval/golden/ops-workforce.json`). Adversarial projection-review proofs: `backend/internal/ceoai/reporting/verifier_review_integrity_test.go` (whole-filter >page-size, below-threshold count, median/p90 against a known distribution, no park/category fan-out). |

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

## Explicit exclusion: People/HRMS directory + in-app onboarding (2026-08-22)

The People/HRMS rewrite (admin-web `/people`) adds the staff directory read
`GET /admin/workforce/people` (`func:ListPeople`, `func:PeopleCatalog`,
`func:NewPeopleHandler`, `func:RegisterPeople`) and the in-app onboarding write
`POST /admin/workforce/people` (`func:CreatePerson`, `func:NewPeopleService`,
`func:ConventionPassword`), the Firebase Identity Toolkit adapter
(`func:EnsureEmailUser`, `func:New`, `func:WithBaseURL`, `func:WithHTTPClient`,
`func:WithTokenSource`, `func:ProjectIDFromIssuer`), and the DB-backed auth
email allowlist (`table:auth_allowed_emails`, `func:NewAllowedEmailSource`,
`func:EmailAllowed`, `func:AllowsWithDynamic`,
`func:WithDynamicAllowedEmails`).

All of it is ADMIN/AUTH infrastructure, not a leadership business fact:

- `auth_allowed_emails` and the allowlist source/union functions are login
  admission plumbing (who may sign in), the DB twin of the
  `GOATOS_AUTH_ALLOWED_EMAILS` env secret — the same category as the excluded
  auth/session surfaces. Exposing the login allowlist to the assistant adds no
  KPI and would leak account-admin detail.
- The Identity Toolkit adapter creates Firebase accounts on the write path; it
  reads no business data.
- The directory read lists `workforce_members` rows (name, park, department,
  designation, login email) — the same HRMS roster admin surface as the
  already-excluded `/admin/roster/*` config reads, at member grain with login
  emails attached, which is account administration rather than an operational
  KPI. Leadership workforce answers (who executed/verified work, operator
  capacity, coverage) stay on the existing covered vaccination/roster
  execution surfaces. If leadership later asks a headcount-by-park/department
  trend question, that becomes a real coverage row against an aggregate view —
  not this login-bearing admin list.

No new Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or read-only SQL
fallback surface.

| workforce_people_directory | table:auth_allowed_emails, func:NewAllowedEmailSource, func:EmailAllowed, func:AllowsWithDynamic, func:WithDynamicAllowedEmails, func:ProjectIDFromIssuer, func:WithBaseURL, func:WithHTTPClient, func:WithTokenSource, func:New, func:EnsureEmailUser, func:NewPeopleHandler, func:RegisterPeople, func:ListPeople, func:CreatePerson, func:PeopleCatalog, func:NewPeopleService, func:ConventionPassword | Explicit exclusion: admin/auth onboarding infrastructure; leadership workforce answers stay on the existing covered execution/roster surfaces. |

## Explicit exclusion: weighing verification enqueue bridge (2026-07-30)

The weighing verification bridge
(`backend/internal/weighing/adapters/verificationbridge/enqueue.go`) and its
service wiring functions (`func:New`, `func:EnqueueWeighingVerification`,
`func:WithVerificationEnqueuer`) only enqueue already-captured weighing proof
videos into the existing Verification workstream. They add no new CEO assistant
read API, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or leadership KPI.
Leadership visibility remains through the existing weighing monitor/video APIs
and verification surfaces, so this is an explicit documented exclusion.

## Explicit exclusion: feed proof media validator (2026-08-11)

`func:ValidateFeedProofMedia`
(`backend/internal/feeddirection/adapters/proof/validator.go`) is write-path
validation for feed proof intake. It enforces that the operator captures the
expected proof media kinds — feed weight photo, feed distribution video, and
water distribution video — before the existing feed completion and verification
workflow accepts the submission. It adds NO new leadership KPI, table, read API
route, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or read-only SQL fallback
surface. The leadership assistant read surface remains the existing feed
completion/verification reporting coverage. Explicit documented exclusion — no
coverage-matrix mapping required.

| feed_proof_media_validator | func:ValidateFeedProofMedia | Explicit exclusion: write-path proof media validation only; existing feed completion and verification reads remain the leadership assistant coverage source. |

## Explicit exclusion: feed direction frozen-row identity helper (2026-08-17)

`func:RowKey` (`backend/internal/feeddirection/domain/issue.go`) is an internal
reconstruction helper for frozen feed-direction issue rows. It prevents distinct
stored rows from being merged when historical data contains duplicate `row_seq`
values, so the existing mobile feed-direction read shows every already-issued
row. It adds NO new leadership KPI, table, read API route, Cube metric,
`ceo_ai.*` view, MCP Toolbox tool, or read-only SQL fallback surface. Leadership
assistant coverage remains the existing feed completion/verification reporting
coverage. Explicit documented exclusion — no coverage-matrix mapping required.

| feed_direction_frozen_row_identity | func:RowKey | Explicit exclusion: internal feed-direction row reconstruction helper only; existing feed completion and verification reads remain the leadership assistant coverage source. |

## Explicit exclusion: feed experiment basis conversion audit table (2026-09-01)

`table:feed_experiment_basis_conversions` records one-time migration provenance for
experiment feed cells converted from legacy whole-pen kg totals into grams per
animal. It is audit/reversal evidence for migration `000238`, not a live
operational read model, KPI, assistant question surface, admin API route, Cube
metric, `ceo_ai.*` view, MCP Toolbox tool, or read-only SQL fallback. Leadership
assistant coverage remains the existing feed completion, feed direction, and
verification reporting surfaces that answer what pens are fed.

| feed_experiment_basis_conversions | table:feed_experiment_basis_conversions | Explicit exclusion: one-time feed migration provenance table only; existing feed direction/completion/verification reads remain the leadership assistant coverage source. |

## Explicit exclusion: shifting destination tag resolver helpers (2026-08-16)

`func:Resolved`, `func:ResolveShiftingDestinationStageDetailed`,
`func:ResolveShiftingDestinationPenStageDetailed`,
`func:NormalizeClinicalStageKey`, and `func:IsClinicalManagementStage` are
write-path/form-contract helpers for the shifting raise flow. They decide whether
the operator may choose the destination pen's tag and explain unavailable choices
before the existing shifting approval/completion path records the movement. They
add no new leadership KPI, table, read API route, Cube metric, `ceo_ai.*` view,
MCP Toolbox tool, or read-only SQL fallback surface. Leadership assistant
coverage remains the existing counts, herd, and verification reporting reads.
Explicit documented exclusion — no coverage-matrix mapping required.

| shifting_destination_tag_resolver | func:Resolved, func:ResolveShiftingDestinationStageDetailed, func:ResolveShiftingDestinationPenStageDetailed, func:NormalizeClinicalStageKey, func:IsClinicalManagementStage | Explicit exclusion: shifting write-path/form resolver helpers only; existing counts/herd/verification reads remain the leadership assistant coverage source. |

| shifting_type_tag_rules | func:ShiftingGoatFacts, func:KnownShiftType, func:ResolveShiftTypeDecision, func:ConfigureAdoptedShedCohortInTx | Explicit exclusion: typed shifting write-path validation and apply helpers only. They decide whether a raise/apply is allowed and which destination pen tag is adopted; they add no new leadership KPI, read API, `ceo_ai.*` view, Cube metric, MCP Toolbox tool, or read-only SQL fallback. Existing counts, herd, shifting approval/completion, and verification reporting remain the leadership assistant coverage source. |

| vaccination_anchor_stale_obligation_cancel | func:CancelOpenVaccinationObligationsForGoatDose | Explicit exclusion: vaccination write-path repair helper only. It cancels stale generated obligations when accepted history or a manual anchor supersedes the old seed row; it adds no leadership KPI, read API, `ceo_ai.*` view, Cube metric, MCP Toolbox tool, or read-only SQL fallback. Existing vaccination dashboard, execution, completion, and verification reads remain the assistant coverage source. |

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

## Goat passport operational location (2026-08-06)

`func:NewLocationReader` / `func:GoatLocation` / `func:NewService` (passport) resolve a
goat's CURRENT operational location — park, physical shed, and `partition_label` — so the
passport surface can answer with the partition when one exists, per
`docs/decisions/operational-location-convention.md`. This is a READ of facts the assistant
already covers: the same park/shed/partition grain is served to leadership through
`ceo_ai.animal_current_scope` (which carries `partition_label` since migration 000110).
The passport reader adds NO new leadership KPI, Cube metric, `ceo_ai.*` view, or MCP tool —
it is a per-animal detail read behind the existing passport API, and leadership aggregate
questions continue to resolve through `animal_current_scope`.

| goat_passport_location | func:NewLocationReader, func:GoatLocation, func:NewService | Covered by `ceo_ai.animal_current_scope` (same park/shed/partition grain, `partition_label` present); passport reader is a per-animal detail read adding no new leadership aggregate. |

## Explicit exclusion: counts shed-stage reclassification command (2026-08-12)

`func:PreviewReclassifyShedStage`, `func:CommitReclassifyShedStage`, and
`func:ReclassifyShedStage` power an admin-only Counts correction action for
retagging every live animal in one physical shed/partition cohort. The commit
emits the existing `goat.stage_changed` event for changed animals and updates
the existing herd identity facts; it does not introduce a new leadership KPI,
read API route, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or read-only SQL
fallback surface. Leadership questions about current park/shed/partition/stage
composition continue to resolve through the already-covered
`ceo_ai.animal_current_scope` and Counts breakdown/read APIs.

| counts_shed_stage_reclassification | func:PreviewReclassifyShedStage, func:CommitReclassifyShedStage, func:ReclassifyShedStage | Explicit exclusion: admin-only correction write path over existing identity facts; current herd composition remains covered by `ceo_ai.animal_current_scope` and existing Counts reads. |

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
`func:ShedCompletionSummary` is the existing mobile shed-submit summary read
behind `GET /app/tasks/{task_id}/shed-completion-summary`; its partition-label
argument only narrows that existing operator read to the physical partition shed.
These helpers are internal write/readiness reads on the existing vaccination SOP
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

## Explicit exclusion: obligation sweeper failure metric (2026-08-24)

`func:RecordSweeperFailure` records a kernel/internal observability counter
(`kernel.sweeper.failures`) when the obligation sweeper continues to missed
marking after a recoverable planning/batch phase failure. The same failure is
also written to the existing audit log by the sweeper. This adds NO leadership
KPI, read API route, Cube metric intended for CEO answers, `ceo_ai.*` reporting
view, MCP Toolbox tool, or read-only SQL fallback. Leadership assistant coverage
for vaccination status remains the existing vaccination command board, schedule,
Action Center, Protocol Adherence, and Control Tower read surfaces. Explicit
documented exclusion — no coverage-matrix mapping required.

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

**EXCLUDED — `func:ReactivateWorkItemsForBucket`** (legacy Weighing compatibility lane) — a
single indexed UPDATE that returns a weighing work item to `scheduled` when its
bucket leaves a terminal status via rework or reopen (defect B09: reopen left
kernel work terminal, so Calendar and Control Tower kept reporting finished
work). It is a write-path state-transition helper called inside the owning
transaction, not a read surface. At task-kernel cutover, the outward reopen
event drives shared ancestor/task reopening; this local helper is reconciled and
demoted so it cannot remain a competing Calendar/Control Tower authority.

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

**EXCLUDED — `func:ListAlerts`** (verification HTTP adapter) — the current
pre-cutover verifier's per-feature compatibility list,
`GET /verify/alerts?category=<verification category>`. It returns pending
`verification_items` for one module and is not a leadership aggregate or KPI.
At task-kernel cutover each actionable item maps to a separately owned shared
sign-off leaf; module Alerts become lenses over shared task/sign-off truth and
this local work authority is suppressed or retired after parity and zero-use
proof. Leadership task/verification status then comes from governed shared task
reads plus module source facts, not this endpoint.

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

These are legacy write-path state-transition columns and one cadence event consumed by
the notification bridge, inside the owning transaction — not a read surface, no
aggregate, no metric, no new KPI. `closed_reason` is a machine token
(`merged_on_carry_over`); the farm-readable sentence is composed by the
notification consumer. Shared task materialization must preserve the one-open-
work-unit invariant, shadow/reconcile the carry-over result, suppress the direct
contact before shared contact activation, and make shared reads authoritative at
cutover. These local fields then remain source compatibility/evidence only.

**EXCLUDED — `route:/vaccination/alerts`** (Android navigation) — the current
pre-cutover address of Vaccination's compatibility alerts feed, moved off the
generic `/alerts`. Its `notification_requests` rows are transport/delivery
evidence, not work truth. At shared-task cutover the route may remain only as a
module-scoped lens over shared task/contact state; the direct feed must be
suppressed before shared contacts activate and retired after replay, parity, and
zero-use proof. Leadership vaccination health remains on the governed
vaccination reads and Control Tower summary, not this per-recipient route.

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
source weighing progress through `GET /weighing/campaigns`; the current Control
Tower process-state summary is a pre-cutover compatibility read. Shared task
reads own owner/clock/delay/contact/sign-off status after cutover. A refused edit
writes nothing, so source facts are unchanged by definition.

**EXCLUDED — `func:AnimalProofWasRejected`** (weighing postgres adapter) — a
boolean guard read asking whether a proof is already attached to an observation a
verifier sent back, so re-recording an animal cannot reuse the very video that was
rejected. It DOES read weighing_observations -- stating otherwise would hide a real query
surface -- but only to answer one write path's precondition, returning a single
boolean to the caller. It is excluded because nothing in the leadership surface
reads it, not because it touches no data; if a leader ever needs rejected-proof
counts, that is a NEW read to add here, not this one. Leadership continues to see
source weighing progress through `GET /weighing/campaigns` and
`GET /app/weighing/leadership/sheds`. The current Control Tower process-state
summary is pre-cutover compatibility; shared task reads own coordination status
after cutover.

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

## Explicit exclusion: Health Config protocol authoring (2026-08-06)

`/health/config` and its `/health-config/*` API let the CEO tier and the Health
Director AUTHOR treatment protocols: per disease, per age band, the day-by-day
course of medicines, dosages, routes, actions and critical handoffs (maintainer
decision 2026-08-06, `docs/decisions/health-config-authoring.md`).

This is **admin config authoring**, the same class as the already-excluded
`vaccination_operator_assignment_config` surface above. It defines the STANDARD a
treatment is carried out against; it records no clinical event, no animal, and no
outcome. Every read it serves is the authored document itself, addressed by
disease — a rulebook lookup, not a metric. There is no aggregate here a CEO would
ask for: "how many protocols exist" is a count of config rows, and the numbers
that would matter to leadership (how many animals are under treatment, for what,
with what recovery rate) come from the Health module's CASE surfaces, not from
this one.

**EXCLUDED — `table:health_config_write_log`** — the idempotency + audit ledger
for authored edits (one row per accepted write, carrying the key, the fingerprint,
the outcome, and which version each publish retired). It exists so an authored
dosage change is answerable afterwards, and that question is a business AUDIT
question, which `/operations/audit` already owns as the leadership-facing surface
(this ledger's sibling `audit_log` rows are written in the same transaction). It
is not a reporting table: it has no animal, no park, no date grain a metric could
roll up, and one row per click.

**EXCLUDED — the authoring API and its plumbing** — `func:RegisterConfig`,
`func:NewConfigHandler`, `func:NewConfigService`, `func:ListProtocolCatalog`,
`func:GetProtocolDetail`, `func:GetDraftForEdit`, `func:CreateDisease`,
`func:SaveDraft`, `func:PublishDraft`, `func:DiscardDraft`, `func:OpenDraft`.
These are the seven `/health-config/*` routes and their handler/service
constructors. Two reads (catalog, detail) serve the authoring screen a bounded
page of config rows; the rest are writes. No leadership KPI, Cube metric,
`ceo_ai.*` view, or Toolbox tool.

**EXCLUDED — the pure validation/normalisation helpers** —
`func:ValidateAuthoredProtocol`, `func:NormalizeAuthoredProtocol`,
`func:NormalizeDiseaseKey`, `func:ValidDiseaseKey`, `func:ContentHash`,
`func:SortAuthoredSteps`, `func:ToProtocolSteps`, `func:FromProtocolSteps`,
`func:Error` (on the validation error type). Pure functions over an authored
document in `health/domain`. They read nothing and return no data.

**EXCLUDED — `func:ReplacePublishedProtocolsOverwritingAuthored`** — the reviewed
break-glass form of the Google Sheet importer, for a maintainer re-bootstrapping
from a corrected sheet after the app has taken authorship. A deployment/import
seam, never a request path.

| health_protocol_versions, health_protocol_steps, health_config_write_log | EXCLUDED | Authored treatment rulebook + its write ledger. Config authoring, not a reporting surface; the leadership audit question is served by `/operations/audit`. |

### Health clinical read coverage

The Health module's CLINICAL surfaces have never had a coverage row: `health_cases`,
`health_treatment_sessions`, `health_session_steps`,
`health_medicine_administrations`, and the `GET /app/health/work-items` read API
(migration `000098`, shipped before this matrix's Health section existed). Those
are the leadership-relevant ones — how many animals are under treatment, for which
diseases, in which parks, how long courses run, and how much medicine is being
administered.

`GET /app/health/work-items` and `GET /app/counts/milk-feeding/tasks` are now externally reachable through
`external MCP:get_health_today` and `external MCP:get_health_work_items` at
treatment-session grain for CEO/CXO questions about
open/due/in-progress/completed/held/canceled-death work items. Broad health
questions must use `get_health_today`, which combines adult health, kids health,
and kid milk-feeding tasks instead of returning a partial age-band answer.
`external MCP:get_milk_feeding_today` is also available as a typed tool for Milk
Feeding farm-session work. The guardrail is strict: open sick/treatment work is
not a mortality event unless the health workflow explicitly reports an approved
death state.

The richer clinical analytics gap remains for tables such as `health_cases`,
`health_session_steps`, and `health_medicine_administrations`: there is still no
Cube metric, `ceo_ai.*` aggregate view, or Toolbox tool over medicine/disease
duration analytics. When the Health module is switched on beyond work-item
operations, those tables need a real reporting coverage decision, not an
exclusion.

## Verifier Actions subject labels: excluded internal lookup (2026-08-07)

| Surface | Decision | Reason |
| --- | --- | --- |
| `func:CampaignShedLocation` (weighing postgres repository) | EXCLUDED | A display-label lookup, not a reporting surface. It reads `weighing_campaign_sheds.location_id` + `display_name` by primary key so a weighing verification item's `subject_label` can name the shed and partition the clip was shot in (previously the lump-sum literal "Whole shed", and nothing at all on individual captures). It introduces NO new fact, table, event, or state: both columns are already flattened onto the campaign-shed bucket at creation time, and the value is composed into a string a human verifier reads on `/actions`. Nothing here is aggregatable and nothing is a KPI. Leadership weighing answers stay on the governed weighing aggregates and `ceo_ai.*` views, exactly as the existing weighing rows in this matrix record. If leadership later asks a shed-level weighing-coverage question, that becomes a real coverage row against an aggregate — not this per-item label helper. |

## Weighing verifier correction helpers: excluded write path (2026-08-17)

| Surface | Decision | Reason |
| --- | --- | --- |
| `func:RelabelItemBySource`, `func:Category`, `func:HasCountField`, `func:WithWeightCorrector`, `func:CorrectObservationWeight`, `func:RelabelWeighingVerification`, `func:NewWeightCorrectionService`, `func:WithVerificationRelabeler`, `func:Error`, `func:Unwrap`, `func:CorrectionCode`, `func:ValidateWeightCorrection`, `func:RecomputeAverageWeightKg`, `func:CorrectedSubjectLabel`, `func:FormatWeightKg` | EXCLUDED | Verifier write-path, UI copy registry, validation/result formatting, and verification item relabel helpers only. They correct recorded weighing observations and update the existing verification item label/audit trail; they do not introduce a new leadership read fact, aggregate, `ceo_ai.*` view, MCP Toolbox tool, Cube metric, or KPI. Leadership weighing answers continue to come from the governed weighing/growth read surfaces already covered by this matrix, while the correction act remains an operations-audit event. |
| `func:RegisterMeasurementApplier`, `func:ApplyMeasurement`, `func:HasRecordedMeasurement`, `func:NewMeasurementApplier`, `func:NewWastageMeasurementApplier`, `func:WastageMeasurementRecorded` | EXCLUDED | The verifier write-path seam through which an APPROVE carries the number she read off the video (maintainer decision 2026-08-20, replacing the separate save button whose relabel bumped `row_version` and silently fenced out the approve pressed after it). `RegisterMeasurementApplier` is composition-time wiring; the two appliers forward to the SAME `WeightCorrectionService` / `WastageMeasurementService` already excluded on the row above and on `feed_wastage_completions`; `WastageMeasurementRecorded` is a single primary-key existence read that gates an approve on whether a pen-day was already measured. None adds a leadership read API, aggregate, `ceo_ai.*` view, MCP Toolbox tool, Cube metric, or KPI, and none derives a new fact: the corrected weight and the recorded wastage are the same values those excluded services already wrote, and leadership weighing/feed answers stay on the governed surfaces already covered here while the measurement act remains an operations-audit event. |

## Sales module: covered read APIs + excluded internals (2026-08-17)

The Sales module (migration `000173_sales_ledger.sql`, backend
`backend/internal/sales/**`, admin-web `/sales`) records what the
farm actually sold — live animals and manure across CBE and CPT — plus the
demand pipelines and evidence panels behind those sales. The whole leadership
read surface is TWO endpoints: `GET /sales/overview` (whole-filter aggregates:
revenue, animals sold, realized price per kg, monthly series, price bands,
buyer board, pipelines, weight audit, market benchmarks) and `GET /sales/deals`
	(the ledger rows). External MCP clients must use the typed `get_sales_overview`
	and `get_sales_deals` tools for sales KPI and ledger questions. A Cube metric /
	`ceo_ai.*` view / MCP Toolbox tool mapping for official sales KPIs is FUTURE
	work; until it lands, the assistant answers sales questions through these read
	APIs or not at all.

| Surface | Decision | Reason |
| --- | --- | --- |
| table:person_module_access_vendors_mobile_backfill | excluded (migration ledger) | Rows written by migration `000251` so its Down path removes only the mobile Vendors ticks it created (review finding on PR 179). Two-column bookkeeping with no leadership fact. |
| GET /sales/options | api (form vocabulary) | The record-sale vocabularies (farms, product types, breeds per product, statuses with chip tones, default status, date horizon) for the phone's Sales tab and the web drawer (maintainer instruction 2026-09-04). Form vocabulary only, no leadership fact; the assistant keeps answering from the covered `/sales/overview` and `/sales/deals` reads. Helpers `func:GetOptions`, `func:StatusTone` are presentation, not surfaces. |
| table:sales_deals | api (GET /sales/overview, GET /sales/deals) | The sales ledger: one row per deal (Sheep/Goat/Manure), status-bucketed; only `Deal Closed` rows feed the overview aggregates. Cube/`ceo_ai` mapping is future work. |
| sales_deal_payments | api (GET /sales/deals) | Row-level receipt evidence attached to bounded deal rows; paid-so-far and remaining balance stay in that ledger read until a dedicated receivables-aging aggregate is introduced. |
| table:sales_buyer_leads | api (GET /sales/overview → buyer_pipeline) | Buyer demand pipeline, summarised as whole-filter status + top-places rollups. |
| table:sales_fpo_leads | api (GET /sales/overview → fpo_pipeline) | FPO demand pipeline (company-wide; the source carries no farm), summarised as status + district rollups. |
| table:sales_sold_animal_tags | api (GET /sales/overview → tag_roster) | Per-animal tag evidence behind sold deals; sheet-era tag strings, deliberately never joined to goat_identifiers. |
| table:sales_weight_audit | api (GET /sales/overview → weight_audit) | Video-vs-book weight evidence, served as disjoint gap buckets (≤0.3 kg / 0.3–1 kg / >1 kg) + max gap. |
| table:sales_market_benchmarks | api (GET /sales/overview → market_benchmarks) | Comparable market per-kg quotes; `market_price_per_kg` parsed at import time. |
| path:/sales/overview (GET /sales/overview) | api + external MCP:get_sales_overview | The whole sales page in one read; whole-filter aggregates only, per the operational read-model contract. Golden eval question: `sales-overview` (`tools/ceo-ai/eval/golden/feed-shifting-procurement.json`). |
| path:/sales/deals (GET /sales/deals, POST /sales/deals, POST /sales/deals/{deal_id}/payments, POST /sales/deals/{deal_id}/status) | api (read) + external MCP:get_sales_deals / EXCLUDED (writes) | The GET is the ledger read; the POST routes record sales, receipts, and lifecycle status edits (idempotent/audited where money is written) and are WRITES, not leadership read surfaces. Leadership sees the result through the two reads above. Golden eval question: `sales-deals` (`tools/ceo-ai/eval/golden/feed-shifting-procurement.json`). |
| func:NewSalesService, func:NewSalesServiceWithClock, func:GetOverview, func:ListDeals, func:CreateDeal, func:SetDealStatus, func:RecordDealPayment, func:NewSalesHandler, func:NewRepository, func:Register, func:SalesHTTPError, func:BadRequest, func:NotFound, func:Conflict, func:Internal, func:Error | EXCLUDED | Service/handler/repository plumbing behind the two covered read APIs and the writes; no independent read surface. |
| func:BuildDealAggregates, func:BucketWeightGap, func:Animals, func:Month, func:PeriodFromCandidate, func:PaymentBalance, func:ClampDealPageSize, func:NormalizeFarmFilter, func:Normalize, func:TotalOrSplitSum, func:PerKgCost, func:Validate, func:IsFarm, func:IsProductType, func:IsLiveProduct, func:IsStatus | EXCLUDED | Pure domain rollup/validation/helpers over rows the covered reads already serve; they derive no new fact and read no data themselves. |

## Sales load-wise reconciliation (2026-08-31)

| Surface | Decision | Reason |
| --- | --- | --- |
| path:/procurement/loadwise-sales (GET /procurement/loadwise-sales), path:/procurement/loadwise-weights (GET /procurement/loadwise-weights) | api | The Sales page's load-wise reconciliation: per procurement load, purchased / sold / mortality / other exits / remaining / unaccounted counts, recorded landed cost (`purchase_value`, absent = cost not recorded), deal-attributed `sold_value` (deal value split evenly across tagged `goat_sale_allocations`, summed by load; `sold_priced` marks how much of sold is actually priced), and `remaining_value` at `avg_sold_price` with an explicit `price_basis` (load / overall / none). Leadership questions like "how did load X do" and "what is the remaining stock of a load worth" route here. `GET /procurement/loadwise-weights` is the unpriced Growth Director projection of the same row set for weighing comparison; it deliberately carries no cost, sale value, or profit fields and adds no separate leadership fact surface. Whole-read summary aggregates exactly the served rows; grain proof and adversarial fixture: `TestLoadwiseSalesPostgresRead`. Decision: `docs/decisions/sales-loadwise.md`. Cube/`ceo_ai` mapping is future work alongside the sales ledger's. |
| path:/procurement/loads/{load_id}/cost (PUT) | EXCLUDED | Write route: records one load's landed cost (`procurement.load_cost.write`, audited, naturally idempotent). Leadership sees the result through the loadwise read above. |
| migration:000232_procurement_load_costs (procurement_loads.animal_cost / transport_cost / other_cost / cost_recorded_by / cost_recorded_at) | api (GET /procurement/loadwise-sales) | New COLUMNS on the existing loads table, not a new table; served only through the loadwise read's `purchase_value` decomposition. |
| func:NewLoadwiseHandler, func:RegisterLoadwise, func:LoadwiseSales, func:LoadwiseWeights, func:SetLoadCost, func:NewLoadwiseService, func:LoadwiseHTTPError, func:FinalizeLoadwise, func:Validate | EXCLUDED | Service/handler/domain plumbing behind the covered loadwise reads and the write; no independent read surface. |

## Feed direction proof validator: excluded constructor overload (2026-08-15)

| Surface | Decision | Reason |
| --- | --- | --- |
| `func:NewValidatorWithPool` (`backend/internal/feeddirection/adapters/proof/validator.go`) | EXCLUDED | A second constructor for the existing `feeddirection` proof `Validator`, added so the wiring layer can hand it a `*pgxpool.Pool` alongside the existing `proofports.Repository`. It introduces no new table, event, state, or read path — `Validator` still only calls `repo.GetProofsByIDs` and compares fields already on `proof_artifacts`, exactly like `NewValidator`. The pool field exists for a follow-up direct-query capability inside this same struct, not a new reporting surface today; when that follow-up lands and actually queries through the pool, it needs its own coverage decision. |

## Newborn K0 pen placement: excluded rule + write-path helpers (2026-08-20)

`docs/decisions/newborn-k0-pen-placement.md` makes a newborn resolve to its
park's KID PEN — a pen whose authored tag is `K0` — instead of accepting any pen
the operator picked. Everything it adds is a PLACEMENT RULE and its write path.
No new table, event, state, aggregate, or read fact exists: a birth still creates
exactly the `goats` / `goat_births` rows it created before, and the Record shed
fallback step reuses the already-registered `goat.location.changed` producer via
`identity.RelocateGoatsToShedInTx`, which the shifting approval path already
emits and which existing consumers already read.

Leadership birth/mortality answers stay on the governed Counts aggregates
(`ceo_ai.counts_movement_daily`), exactly as the `table:goat_births` exclusion
records, and per-pen location answers stay excluded exactly as the partition
primitives row records. If leadership later asks a placement-quality question
("how many kids were recorded outside a kid pen last month"), that becomes a real
coverage row against a new aggregate — not these helpers.

| Surface | Decision | Reason |
| --- | --- | --- |
| `func:IsNewbornPen`, `func:NewbornPenStage` (`protocol/domain`) | EXCLUDED | A stage-vocabulary predicate, the exact sibling of the already-excluded `func:IsClinicalStage` in this matrix: it answers whether a stage code names the newborn cohort so a birth can refuse a pen tagged for another cohort. It reads no data and derives no fact; it exists in `protocol/domain` only so Counts and Tasks share ONE implementation instead of copying the comparison. |
| `func:ResolveBirthPlacement`, `func:AllowsPen` (`counts/domain`) | EXCLUDED | The placement rule itself, pure over rows the caller already holds. `ResolveBirthPlacement` partitions ONE park's existing destination-catalog rows into "kid pens" plus a mode and a farm-worded notice; `AllowsPen` is the write-side twin that refuses a pen the form would not have offered. Both operate on `GET /app/counts/shifting/destinations`, already EXCLUDED in this matrix as an operator picker, and they add no row, no quantity, and no execution state to it — only which of its existing options a BIRTH may name. |
| `func:ParseRecordedPenAnswer`, `func:FormatRecordedPenAnswer` (`tasks/domain`) | EXCLUDED | The `"<shed_id>\|<partition_label>"` encoding of the Record shed step's stored answer, and its inverse. Pure string handling over a value the operator selected from the same excluded picker; it reports nothing and reads nothing. Same class as the other workflow-action answer helpers already excluded with the Birth/Death row-level detail. |
| `func:TemplateBirthKidAt`, `func:TemplateByKeyAt` (`tasks/domain`) | EXCLUDED | Existing birth-template constructors, unchanged in kind: they gain one parameter that adds the Record shed step to the kid track when the kid is not already in a kid pen. The kid track is the per-animal OPERATOR work list whose rows are already excluded above (`GET /workflows/{row_id}`, and the colostrum day lens for the same reason); one more step on it introduces no new leadership fact. |
| `func:WithIdentityTxWriter` (`tasks/adapters/postgres`) | EXCLUDED | Dependency-injection seam, the exact twin of the identically-named `counts/adapters/postgres` injector this matrix already carries: it hands the tasks repository identity's transaction-scoped relocation writer so the Record shed placement commits with the action row. Wiring only — no table, event, read path, or fact. |

## PC Care module: excluded operator execution surfaces (2026-08-21)

`docs/decisions/pc-care-module.md` adds the PC Care module (module_key `pc_care`):
planner-assigned deworming / ticks removal / hoof trimming / hair trimming tasks
with per-animal live-camera video proof and a verifier gate. Everything it adds
today is OPERATOR EXECUTION AND EVIDENCE STATE, the same class as the excluded
feed/weighing completion tables: leadership sees pending evidence through the
already-covered verification queue surfaces, and pc_director's oversight rides
the module's own monitor endpoints on the phone. A leadership KPI ("how many pens
were dewormed last month", care-cadence adherence) becomes a real coverage row
against a governed aggregate when the maintainer asks for one — not these raw
rows.

| Surface | Decision | Reason |
| --- | --- | --- |
| pc_care_tasks | EXCLUDED — planner-assigned task state (category, pen, planned/due business dates, kernel work_state, verification status). Execution/evidence state, not a CEO KPI; leadership pending-evidence answers stay on verification_queue_status. |
| pc_care_task_assignees | EXCLUDED — per-task operator assignment rows (who may work a task). Pure authorization/execution state. |
| pc_care_task_animals | EXCLUDED — per-scan RFID rows with slot proof refs and capture attribution. Evidence state behind the verification item; the tag is stored verbatim and derives no herd fact. |
| pc_care_task_proofs | EXCLUDED — vaccination-director stock execution proof refs. `func:PutTaskProof`, `func:RegisterTaskProof`, `func:ListTaskProofs`, `func:ValidateLiveCameraMedia`, and `func:ValidateLiveCameraProofKind` create and validate operator/director evidence for the existing PC-care/vaccination mobile workflow; they add no CEO-assistant governed read API, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or SQL fallback. Leadership pending-evidence visibility remains through the existing verification queue/reporting coverage. |
| pc_care_task_inventory_requirements | EXCLUDED — vaccination stock reconciliation write/input rows. `func:NewPcCareInventoryVaccineStage`, `func:Name`, `func:Run`, `func:ReconcileInventoryVaccineTasks`, `func:ListTasks`, and `func:IsKernelOwnedCategory` feed the mobile task execution surface; they add no CEO-assistant governed read API, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or SQL fallback. |
| `GET/POST /app/pc-care/*` (planner catalog/sheds, tasks, worklist, captures, animals, proofs, submit) | EXCLUDED — operator/planner execution surfaces (the mobile module's own screens). No leadership read API or aggregate; the verifier reviews through the existing generic verification routes already covered here. |
| event `pc_care.task.completed` | EXCLUDED — the module's single canonical completion event, consumed today by nothing (registered producer-only in the domain-event registry). Becomes a coverage row when a governed care-adherence aggregate is built over it. |

## Feed distribution completion table: excluded admin analytics detail (2026-08-26)

The feed analytics execution tab now exposes a per-pen-session distribution
completion table for admin-web: every pen-session directed for one feed day,
including untouched pens, status bucket, completion submitter/verifier, and the
three proof references with uploader provenance. This is an operational
leadership UI detail under the existing `/feed-analytics/execution` admin
surface, not a CEO-assistant governed metric, Cube model, `ceo_ai.*` view, or
MCP Toolbox tool. CEO-assistant feed answers remain on the existing governed
feed coverage paths; if leadership later asks for a natural-language KPI such
as "which farms had the most unfed pens yesterday", that should be backed by a
new aggregate/view and a real coverage row rather than this paged admin table.

| Surface | Decision | Reason |
| --- | --- | --- |
| `func:NormaliseCompletionPage`, `func:NormalizeDistributionCompletionStatus`, `func:IsValidDistributionCompletionStatus`, `func:DescribeProofUploads` | EXCLUDED | Helper/read-detail functions for the admin-web feed distribution completion table. They normalize paging/status filters and resolve stored proof ids to uploader provenance for a paged operational table; they introduce no standalone assistant read surface, governed aggregate, Cube metric, `ceo_ai.*` view, or MCP tool. |

## Herd Signals BLE telemetry: excluded backend infrastructure (2026-08-23)

Herd Signals (migration `000192_herd_signals.sql` onwards) ingests and stores BLE
advertisement packets and derived telemetry from HoneyComm smart ear tags on
animals. It is exclusively a **backend telemetry collection and storage system**
— not a behavior classifier, activity recognizer, or health diagnostic system.
The tables, functions, and services exist to capture motion, battery, signal
strength, and tag hardware status at radio-packet grain. The module draws NO
business logic from these low-level signals and makes NO clinical or
operational claims (see `docs/modules/herd-signals.md` Section 3 for the
exhaustive list of what it explicitly cannot measure).

Leadership questions about animal health, movement, location, activity, or
behavior are NOT answered by this telemetry today. They will be answered through
higher-level aggregates and decision models built ON TOP of herd-signals data
when those are implemented — for example, a future Cube metric or `ceo_ai.*` view
that computes health risk scores, activity classification, or behavioral
anomalies from the raw signal history. Until those aggregates exist, herd_signals
remains backend infrastructure only.

| Surface | Decision | Reason |
| --- | --- | --- |
| herd_signal_gateways | EXCLUDED | Backend telemetry storage table: BLE gateway registrations. No leadership read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. Infrastructure table only. |
| herd_signal_packets | EXCLUDED | Backend telemetry storage table: raw BLE advertisement packets. No leadership read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. Infrastructure table only. |
| herd_signal_packets_new | EXCLUDED | Transient partition build artifact created during migration 000200 (herd_signal_packets partitioning). Exists temporarily during DDL transformation and is not a permanent schema surface. No leadership read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. |
| herd_signal_packets_default | EXCLUDED | Partition catch-all table created during migration 000200 (herd_signal_packets partitioning). Temporary partition artifact used during the conversion to daily RANGE partitioning. No leadership read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. |
| herd_signal_tag_latest | EXCLUDED | Backend telemetry storage table: latest tag state (RSSI, battery, motion, pattern). No leadership read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. Infrastructure table only. |
| herd_signal_activity_windows | EXCLUDED | Backend telemetry storage table: time-bucketed motion aggregates (5-min and 1-hour windows). No leadership read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. Infrastructure table only. |
| herd_signal_tag_mappings | EXCLUDED | Backend telemetry storage table: tag-to-animal identity mappings. No leadership read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. Infrastructure table only. |
| herd_signal_motion_delta_1h | EXCLUDED | Backend telemetry storage table: 1-hour motion bucket summary. No leadership read API, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. Infrastructure table only. |
| func:IngestPackets, func:ListLive, func:GetTagActivity, func:ListGateways, func:GetInsights, func:ListActivityWindows, func:GetTimeline, func:BindTagMapping, func:ReplaceTagMapping, func:UnmapTagMapping, func:RecordGatewayHeartbeat, func:GetBaselineDeltas, func:GetBatteryHistory, func:GetGatewayTagStats, func:GetGatewayWindowStats, func:GetInsightsData, func:ExportCSV, func:NewService, func:WithThresholds, func:NewRepository, func:UpsertGateway, func:GetGatewaysByTenant, func:GetTagLatest, func:ListTagsLatest, func:GetGoatIdentifier, func:ResolveTagMapping, func:GetGoatsByIDs, func:ResolveTagsBatch, func:GetShedLocations, func:ListFarmActivity, func:ListTagsLatestPage, func:GetTagActivityScope, func:NewHandler, func:Register, func:Write, func:MotionDelta, func:IsGapDelta, func:MovementStateFromDelta, func:SignalStateFromRSSI, func:BatteryStateFromVoltage, func:BatteryTrendFromHistory, func:BatteryStateWithTrend, func:PatternStateFromHistory, func:Baseline75, func:SelectBucketTier, func:IsSupportedBucketSeconds, func:NormalizeTagIdentifier, func:DefaultThresholds, func:IsHoneyCombAdvertisement, func:DecodeHoneyCombPacket, func:RecordHerdSignalsPartitionMaintenanceRun | EXCLUDED | Backend ingest, storage, and internal telemetry functions (service, handlers, repositories, domain helpers, HTTP wiring). Operate on radio primitives (packet ingestion, motion bucketing, battery trending, signal state calculation) with no business domain or leadership outcome attached. When leadership aggregates are built (e.g. animal activity score, herd movement alerts), those become coverage rows and may delegate to these internals via governance layer. |
| /herd-signals/* (all HTTP routes) | EXCLUDED | Backend operator/admin routes for tag mapping, gateway registration, and insights rendering. No leadership read API or aggregate; operational support only. |
| protocol_rule_dimensions.procurement_purpose | EXCLUDED | Compiled vaccination execution-index column added by migration 000210. It narrows generation prefiltering for purpose-specific procurement rules, defaults to `all` for existing dimensions, and exposes no leadership read API, aggregate, Cube metric, `ceo_ai.*` view, or MCP Toolbox tool. |

## Per-person module and page access: excluded access-control surfaces (2026-08-24, 2026-08-27)

The People/HRMS rewrite replaced role-derived access with per-person assignment
(migrations `000219` and `000220`). Every surface below is ACCESS CONTROL: who
may open which module and which screen. None of it is a business fact about the
farm — it describes the product's own permission model, not animals, work, feed,
proof or money — so none of it belongs in a leadership answer.

The distinction that keeps this an exclusion rather than an oversight: leadership
questions about PEOPLE ("who verified this", "how many operators worked today")
are answered from the roster, verification and task surfaces already covered
elsewhere in this matrix. What a colleague's sidebar contains is an
administrative setting, and an assistant that reported it would be describing
configuration rather than the farm. If a leadership question about access ever
arrives ("who can approve a shifting request?"), it becomes a coverage row over a
read API composed for that question, not a raw read of these tables.

| Surface | Decision | Reason |
| --- | --- | --- |
| person_access | EXCLUDED | Access-control header: a person's scope mode and the designation their access started from. No business fact; no leadership read API, Cube metric, `ceo_ai.*` view or MCP Toolbox tool. |
| person_module_access | EXCLUDED | Access-control assignment: which modules, capabilities and admin-web screens a person holds per surface. Permission model, not farm data. |
| person_park_scope | EXCLUDED | Access-control scope: which parks a `parks`-scoped person covers. The park facts leadership asks about are covered by the locations surfaces already in this matrix. |
| designation_catalog | EXCLUDED | Static catalog of job titles offered by the access editor. HR designation as REPORTED for a person is already carried by the covered workforce roster surfaces; this table only pre-fills ticks. |
| designation_module_defaults | EXCLUDED | What picking a designation pre-fills in the access editor. Configuration for a form, never a record of anyone's access. |
| func:ModuleCapabilities, func:LookupModuleCapability, func:ModuleSupportsSurface, func:LevelOffered, func:HasCapability, func:PermissionsForAssignments, func:PermissionsForAssignmentsWithBaseline, func:SeparationRisks, func:AssignmentsForRole, func:AssignmentsForRoles, func:AuthorizePermissionSet, func:SetPersonAccessSource | EXCLUDED | The capability catalog and the request-path resolver that turns a person's stored rows into a permission set. Authorization internals; they decide whether a leadership read is allowed, and are never its subject. |
| func:ModulePages, func:PagesForModule, func:ModuleOwningRoute, func:Allows, func:PageAccessForAssignments, func:PageKeysForModule, func:NarrowForRetiredLenses, func:FillDefaultPages, func:WithPersonPageAccess | EXCLUDED | Page-grain narrowing of the admin-web sidebar (maintainer decision 2026-08-27, `docs/decisions/per-person-page-access.md`). Composes navigation for one principal; no business aggregate. |
| func:NewAccessService, func:GetPersonAccess, func:NewAccessHandler, func:RegisterAccess, func:GetAccess, func:SaveAccess, func:DesignationDefaults, func:NewAccessRepository, func:LoadPersonAccess, func:SavePersonAccess, func:ListParks, func:ListDesignations, func:ResolvePermissions, func:ResolvePageAccess | EXCLUDED | Read/write path of the access editor itself (service, HTTP handler, repository). Admin configuration screen; not a leadership read. |
| health_diagnosis_runs | EXCLUDED | Per-observation operational record of the health diagnosis engine (2026-08-31): one operator form + engine proposal + Director confirm decision, read only by the phone assessment screens. The leadership-relevant outcome is the health_cases/health_treatment_sessions rows a confirm materializes, already covered by the health work-item surfaces. Engine internals excluded alongside it: func:NewDiagnosisHandler, func:RegisterDiagnosis, func:SubmitObservation, func:ConfirmDiagnosis, func:GetDiagnosisRun, func:ListDiagnosisRuns, func:NewDiagnosisRepository, func:NewDiagnosisService, func:Evaluate, func:RegisterFor, func:AdultRegister, func:RegisterVersion, func:Load, func:Rule, func:IsNonSpecific, func:Validate, func:ResolveAnimal, func:MissingKiddingHistory, func:PlanConfirmation, func:CourseShapeFor, func:ValidExitType, func:ClosesOnDayCount, func:SessionsForHousing, func:ScheduleCourse, func:SOPRefToDiseaseKey, func:SortedProblemKeys, func:ConfirmableFromProposal, func:BoundClass, func:MarshalJSON, func:UnmarshalJSON, func:UnmarshalYAML | EXCLUDED | The health diagnosis engine (2026-08-31): the deterministic observation→proposal→Director-confirm pipeline upstream of a treatment course. `health_diagnosis_runs` is a per-observation operational record (one operator's form + the engine's proposal + the confirm decision) read only by the phone's assessment screens; the leadership-relevant OUTCOME of a confirmed run is the `health_cases`/`health_treatment_sessions` rows it materializes, which stay covered by the existing health work-item surfaces named above. A future governed "diagnosis accuracy" KPI would be a new coverage row, not implicit coverage. |
| func:CloseCase, func:ApplyVerifiedTreatment, func:BounceTreatmentForRework, func:EnqueueTreatmentVerification, func:NewHealthVerificationHandler, func:HandleEvent, func:Register, func:WithVerificationEnqueuer, func:New, func:VerificationCategoryForAgeBand, func:ResolveShedLocations | EXCLUDED | Health case-closure and treatment-evidence verification write path (2026-08-31, `docs/decisions/health-workflows.md`). Closure and verdict-apply mutate case/session state; verification items land in the already-covered verification queue surfaces, and closed-case outcomes are readable through the covered health work-item read API. Write-path internals, not a leadership read. |
| func:EncodeCommandBoardAnimalCursor, func:DecodeCommandBoardAnimalCursor, func:EncodeCommandBoardDueCursor, func:DecodeCommandBoardDueCursor, func:EncodeCommandBoardDriveCursor, func:DecodeCommandBoardDriveCursor, func:Normalized, func:Flatten, func:ExternalAdminDSN | EXCLUDED | Command-board paging and wire plumbing (2026-09-01). The cursor codecs encode a keyset position so a drilldown page can resume; `Normalized` clamps a page size into its published bounds; `Flatten` expands the interned shed x dose matrix back to flat cells for rendering; `ExternalAdminDSN` is a test-harness accessor that reads GOATOS_PGTEST_ADMIN_DSN and is never on a request path. None derives a fact or reads a business row -- they move an already-covered fact across the wire. The facts they carry are covered by the `GET /vaccination/command` row above. |
| POST /app/pc-care/tasks/{task_id}/stock-verdict (func:PostStockVerdict, func:RecordStockVerdict, func:IsDirectorApprovedCategory) | EXCLUDED | PC Director write path for vaccine-stock proof approval (2026-09-02). It mutates one submitted `inventory_vaccine` task to completed or rework; it is not a CEO-assistant read API, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or leadership KPI. Leadership questions about PC Care task state remain covered by the existing PC Care/verification task surfaces; this endpoint only decides one card's workflow state. |
| func:GetShedWeights | EXCLUDED | Handler plumbing for the existing weighing shed-weight read APIs used by the admin/mobile weighing screens, including the Sales page's sale-ready count. It adds an optional request parameter that lowers only the 35 kg sale-ready threshold for that operational view; it creates no new CEO assistant endpoint, Cube metric, `ceo_ai.*` view, MCP Toolbox tool, or standalone leadership KPI. Leadership aggregate answers remain on the governed sales/weighing summary surfaces, not this route handler function. |

## Pen reconciliation: operator work queue (2026-09-02)

Pen reconciliation (`docs/decisions/pen-reconciliation.md`) raises a card when an
individual weighing submit finds a scanned animal in a pen the herd register
disagrees with; the operator returns the animal with a video and the verifier
reviews it. The evidence review lands in the already-covered verification queue
surfaces (category `pen_reconciliation`), which is where a leadership question
about outstanding proof reviews is answered today. The card table itself is an
operator work queue read only by the phone's Reconcile tab.

| Surface | Decision | Reason |
| --- | --- | --- |
| pen_reconciliation_cards | EXCLUDED | Operator work-queue rows (open/pending_verification/rework/completed wrong-pen cards) read only by the phone Reconcile tab; the review workload is covered by the verification queue surfaces. A future governed "pen drift" KPI (cards raised per park/week) would be a new coverage row over a composed read API, not a raw read of this table. |
| GET /app/counts/pen-reconciliation/cards, POST /app/counts/pen-reconciliation/cards/{card_id}/complete | EXCLUDED | Operator phone endpoints (CountsWrite): a keyset work-queue page and the proof-gated completion write. Field execution surface, not a leadership read. |
| func:RaisePenReconciliationCards, func:ListPenReconciliationCards, func:CompletePenReconciliationCard, func:MarkPenReconciliationVerificationEnqueued, func:ListPenReconciliationVerificationEnqueueDebt, func:RecoverVerificationEnqueues, func:NewPenReconciliationEnqueueRecoveryStage, func:ApplyVerifiedPenReconciliation, func:BouncePenReconciliationForRework, func:NewPenReconciliationService, func:NewPenReconciliationRaiser, func:NewPenReconciliationVerificationHandler, func:NewPenReconciliationVerificationEnqueuer, func:EnqueuePenReconciliationVerification, func:RegisterPenReconciliation, func:WithPenReconciliationWorkflow, func:ListPenReconciliationCards, func:CompletePenReconciliationCard, func:EncodePenReconciliationCursor, func:DecodePenReconciliationCursor, func:ValidPenReconciliationBucket | EXCLUDED | Write path, durable enqueue-recovery marker draining/clearing, event consumers, cursor codecs and composition wiring of the same operator flow. They move or mutate the card rows excluded above; none composes a leadership fact. |
