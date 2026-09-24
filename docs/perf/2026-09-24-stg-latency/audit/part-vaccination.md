## Vaccination endpoints — DB performance audit (read-only)

Measured on the OCI stg-like clone (tenant `00000000-0000-4000-8000-000000000001`, as_of 2026-09-24 12:00 IST, `jit=off`, default `work_mem=4MB`). Each statement ran as `BEGIN READ ONLY; EXPLAIN (ANALYZE, BUFFERS) …; ROLLBACK`. Statements that build SQL from Go helpers were dumped verbatim with a `go test -overlay` shim, so no repo files changed. "DB ms" is the sum of planning and execution for every statement in a request. Sections that run concurrently are summed too, and the wall-clock figure is noted next to them. Raw plans: `scratchpad/audit/vx/plan_*.txt`.

### Cross-cutting root causes (read first)

1. **`obligation_instances` is badly bloated and mostly dead weight.** It has 86.6k live rows, 347k dead tuples and 135 MB of heap. 76k of the live rows (88%) are `canceled`. `n_mod_since_analyze` is 693k and the last manual ANALYZE was 09-19, so planner estimates are 2.5x high (216k estimated vs 86k actual). Every "open work" bitmap heap scan touches **16.4k heap blocks to return about 10k rows**, roughly one page per row. The same 16.6k-block read shows up in sheds, execution, schedule, operations and live views, and it is uncached on each run. **Fix:** `VACUUM (ANALYZE) obligation_instances`, followed by `pg_repack` or `VACUUM FULL` in a maintenance window. Also set per-table autovacuum: `ALTER TABLE obligation_instances SET (autovacuum_vacuum_scale_factor=0.02, autovacuum_analyze_scale_factor=0.01)`. Then find the writer that churns about 700k modifications in 3 days, which looks like the reconciler cancelling and re-inserting.
2. **All read caches are dead on the admin routes.** Cache keys contain `vaccinationCacheExactTime(asOf)` (RFC3339Nano), and the handler sets `asOf = h.now()` with nanosecond precision. The command board, cohort-matrix, shed-dose-matrix, sheds, execution and operations caches therefore never hit (`adapters/postgres/commandboard.go:137`, `:952`, `commandboard_drilldown.go:365`, `repository.go:207/384/3513`). The only exception is `/app/vaccination/execution`, which truncates to 30 s. **Fix:** key on the IST business date plus a floor of about 30 s, as the live tracker already does with `businessDate`. On Cloud Run the cache is also per-instance, so a shared cache is needed to get real hit rates.
3. **Command-board first paint is gated by a shared 6-slot semaphore** (`commandBoardConcurrencyBudget=6`, `commandboard.go:37`). First paint issues 6 board sections, 2 cohort-matrix statements, 1 shed-dose statement and a lazy drives call, so at least 9 statements queue on 6 slots per instance. Several 200–400 ms statements serialise behind each other. That explains why stg p50 (1.8 s) is about 3x the clone DB time.
4. **Planning costs 20–90 ms per statement.** The execution family uses `pgx.QueryExecModeExec`, which forces a custom plan every time; the classified CTE takes about 45 ms to plan and `scanRosterSQL` about 93 ms. Custom plans on the live tracker cost 35–46 ms each, 6 per request. **Fix:** use prepared or cached statement mode, or `plan_cache_mode=force_generic_plan` for these specific statements after checking the generic plans. The other option is to shrink the CTEs.

### Summary table

| endpoint | handler file:func | #SQL/req | DB ms (sum; wall) | seq scans / spills | grade | fix |
|---|---|---|---|---|---|---|
| GET /vaccination/command | vaccinationexecution/adapters/http/handler.go:GetVaccinationCommandBoard → postgres/commandboard.go:VaccinationCommandBoard | 6 (concurrent, shared 6-slot sem) | **~625** (KPI 113, drives 213, shedVaccine 219, weekly 58, verifyQ 16, codes 6); wall ~220 | Seq scan obligation_instances (86.6k→all) in KPI, shedVaccine, weekly, drives; goats, vaccination_completions | **FAIL** | Exclude dead obligations early and add a partial index (N1). Fix the cache key (root 2). Add a rollup (N2). Stop sharing slots with cohort/shed-dose (root 3) |
| GET /vaccination/command/cohort-matrix | handler.go:GetCommandBoardCohortMatrix → commandboard.go:CommandBoardCohortMatrix | 2 (concurrent) | **~412** (cohort 404, head 8) | Seq scan obligation_instances 86.6k rows→395 cells; **external merge sort 7.3 MB** | MAX (stg 1379/3009) | N1 filter: 404→176 ms. `SET LOCAL work_mem='32MB'` removes the spill (384→279). Rollup N2 |
| GET /vaccination/command/shed-dose-matrix | handler.go:GetCommandBoardShedDoseMatrix → commandboard_drilldown.go:CommandBoardShedDoseMatrix | 1 | 144 | Parallel seq scan obligation_instances 86.6k | OK (stg 962/2300 = queueing on sem + bloat) | N1 filter and partial index; dead cache key; passes `q.AsOf` (zero) instead of the defaulted asOf |
| GET /vaccination/command/drives | handler.go:GetCommandBoardDriveOptions → commandboard.go:CommandBoardDriveOptions | 1 | 213 | Seq scan obligation_instances 86.6k, obligation_batches | OK (cache key has no asOf, so it can hit) | N3: drive the query from `obligation_batches` (529 rows) and join obligations by `(tenant_id,batch_id)` (index exists) |
| GET /vaccination/command/closed-without-dose | handler.go:GetCommandBoardClosedWithoutDose → commandboard_drilldown.go:CommandBoardClosedWithoutDoseAnimals | 1 | 297 | Seq scan obligation_instances 86.6k→51 rows | OK | N1 (same fold as KPI) |
| GET /vaccination/command/shed-vaccine-animals | handler.go:GetCommandBoardShedVaccineAnimals → commandboard_drilldown.go:CommandBoardShedVaccineAnimals | 1–2 (+videos) | ~55 (40 + videos 14) | none (scope_idx) | PASS | – |
| GET /vaccination/command/cohort-exceptions | handler.go:GetCommandBoardCohortExceptions → commandboard_drilldown.go:CommandBoardCohortExceptions | **0** (returns empty page; stub) | 0 | – | PASS | Functional gap: the drawer is always empty |
| GET /vaccination/command/cohort-days | handler.go:GetCommandBoardCohortDays → commandboard_drilldown.go:CommandBoardCohortDays | 1 | 62 | Parallel seq scan obligation_instances | PASS | (N1 would help) |
| GET /vaccination/live-tracker | live_tracker_handler.go:GetVaccinationLiveTracker → live_tracker_repository.go:LiveTracker | 6 (errgroup limit 4) | busy day (07-01, 768 doses) **~390** (exec 195 + **planning ~200**); quiet day ~210; wall ~150 | Seq scan verification_items 21.6k→1070 | MAX | Planning dominates (root 4). Partial index for the verification tile (N6) |
| GET /vaccination/execution | handler.go:ListVaccinationExecution → app/service.go:VaccinationExecutionPage → execution_combined.go (combined) + CarrySummary + AuthorizedParkOptions | 3 (combined page+cards, carry, parks) | **~425** (combined 359, carry 63, parks 3) | bitmap heap obligation_instances 16.4k blocks for 5.5k rows | MAX | Root 1 (bloat) plus index N4. Planning 45 ms per statement (root 4). Dead cache key |
| GET /app/vaccination/execution | same (operator-scoped) | 3 | ~375 (combined 312, carry 63) | same | MAX (stg 440/3595: p95 = cold heap reads of 16k blocks) | Same as above. The p95 is almost entirely heap I/O on the bloated table |
| GET /vaccination/execution/sheds/{id} (+/app) | handler.go:GetShedDrilldown → service.ShedDrilldown → VaccinationExecution | 2–3 | ~350 (exec 284 + carry 63) | Shed filter applied **after** tenant-wide classification | MAX | N5: push shed/park filter into `executionClassifiedCTE` base scan (`oi.scope_id = $3`) |
| GET /vaccination/operations (+ /app/vaccination/coverage) | handler.go:VaccinationOperations / VaccinationCoverage → repository.go:VaccinationOperations | 1 | 259 | bitmap heap 14.4k blocks | OK | Root 1, N4 |
| GET /vaccination/schedule | handler.go:VaccinationSchedule → repository.go:VaccinationSchedule | 1 | **483** | bitmap heap 16.4k blocks; correlated `vda_guess` LIMIT subquery ×10,564 loops | MAX | N7: replace per-obligation lateral with pre-aggregated `DISTINCT ON (batch_id, shed_id)` CTE + hash join |
| GET /vaccination/drive-assignments | handler.go:DriveAssignments → repository.go:DriveAssignments | 1 | 210 | member_rules subquery ×734 loops (all assignments, then month filter) | OK | N8: filter `planned_date` window before the member_rules lateral |
| GET /vaccination/sheds | handler.go:ListShedSummary → service.ShedSummary → repository.go:listShedCanonical + CapacityConfig + roster.ShedOwnerships | 3 | **~905–1065** (summary 870–1029 exec + 34 plan, capacity 1, ownership small) | bitmap heap 16.4k blocks; **nested loop re-aggregates a 5.8k-row sort 102× (loops=102)** after a rows=1 misestimate | **FAIL** (stg 1537/2250) | N9: MATERIALIZED per-shed aggregate + hash join (removes about 580 ms). Root 1 (removes about 200 ms). Fix the cache key |
| GET /vaccination/sheds/{id} | handler.go:GetShedDetail → service.ShedDetail (ShedSummary shed-filtered + ShedOwnership + VaccinationOperations) | 4 | ~460 (summary 234, ops 223, cfg 1, owner ~2) | Shed filter applied late; both statements do tenant-wide work | MAX | N5/N9: push `shed_id` into base CTEs. Both then drop to under 50 ms |
| GET /vaccination/sheds/{id}/animals | handler.go:GetShedAnimals → repository.go:ShedAnimals | 1 | ~23 (plan 22; returned 0 rows at sample params, so the plan could not be exercised) | – | PASS (unverified) | – |
| GET /vaccination/capacity-config | handler.go:GetCapacityConfig → repository.go:CapacityConfig | 1 (PK) | 1 | – | PASS | – |
| GET /vaccination/operator-assignment/config | handler.go:GetOperatorAssignmentConfig → repository.go:OperatorAssignmentConfig + OperatorShifts | 2 (PK/small) | <5 (not measured, PK lookups) | – | PASS | – |
| GET /app/vaccination/alerts | handler.go:ListAlerts → alerts.go:ListAlerts | 1 | 29 | workforce_members (40 rows) only | PASS | – |
| GET /app/vaccination/execution/sheds/{id}/roster | handler.go:ScanRoster → repository.go:ScanRoster (+taskExecutionIdentity) | 2 | ~95 (**planning 93 ms**, exec <1 at sample, which had no task) | – | PASS/borderline | Planning cost (root 4) |
| GET /app/vaccination/tasks/{id}/option-values | handler.go:TaskOptionValues → repository.go:TaskOptionValues | 1 | small (single-row, not measured) | – | PASS | – |
| GET /app/vaccination/gaps | handler.go:VaccinationGaps → repository.go:VaccinationGaps | 1 | 10 | – | PASS | – |

POST/PUT (graded by the main statement only):
- `POST /vaccination/schedule/drive-date-overrides`: one idempotent upsert on the override table plus an outbox insert. PASS.
- `PUT /vaccination/capacity-config`: a row_version-checked upsert on a 1-row table plus one outbox row per active park in the same transaction. PASS; the re-plan runs async.
- `PUT /vaccination/operator-assignment/config`: an upsert plus outbox. The `ReassignPlannedDrives` re-plan runs off the request path. PASS.
- `POST /app/vaccination/obligations/{id}/reschedule`: a PK update and an event insert. PASS.

### Detailed notes (non-PASS)

**N1 — Dead obligations in every board fold (command, cohort-matrix, shed-dose, closed-without-dose, cohort-days).**
Each statement seq-scans all 86.6k obligations, 76k of them `canceled`. In these folds a canceled obligation with no completion contributes nothing to any numerator. It only (a) adds the animal to `animal_count`/`targets` and (b) lands the animal in the `closed_without_dose` residual. Adding
`AND (oi.status NOT IN ('canceled','superseded','waived') OR comp.obligation_id IS NOT NULL)` to the `narrowed` WHERE clause of the cohort statement cut it from **384 to 156 ms**, even with the seq scan still in place.
Product decision needed: whether an animal whose only obligation for a dose was canceled should count in that cohort cell or tile. The current residual contract says yes, so this filter needs sign-off, or needs to be moved into a separate count. Then add a partial index so the scan becomes an index scan:
```sql
CREATE INDEX CONCURRENTLY obligation_instances_board_live_idx
  ON obligation_instances (tenant_id, batch_id, scope_id)
  INCLUDE (obligation_id, target_id, rule_id, status, due_at)
  WHERE target_type = 'goat' AND status NOT IN ('canceled','superseded','waived');
```
(about 10k rows, so it fits in cache and supports index-only scans once the table is vacuumed). With `drive_batch_id` set, the same statements already take 15–35 ms, so the unscoped default view is the only slow case.

**N2 — Rollup for the whole-tenant board.** The unscoped board recomputes the same per-(animal, dose_code) fold 4–5 times, once per section. Materialise `vaccination_board_animal_dose(tenant_id, target_id, scope_id, dose_code, batch_id, open_due_date, has_accepted, has_recorded_unverified, has_rework, is_missed, min/max_administered_at)`. Maintain it in the completion, verification and obligation-status write transactions, or refresh it every 30–60 s. KPI, cohort, shed-vaccine and weekly then read about 24k narrow rows (under 30 ms each). As a stopgap, one CTE could feed several sections, but they run on separate connections by design.

**Cohort-matrix sort spill.** `Sort Method: external merge Disk: 7272kB` on the 86k-row `narrowed` group sort. With `SET LOCAL work_mem='32MB'` inside the read transaction, the statement goes from 384 to 279 ms. N1 shrinks the input so the sort fits in 4 MB anyway.

**N3 — /command/drives (213 ms, and on the board's critical path as a required section).** The statement seq-scans all obligations to compute per-drive dose codes and target counts, then pages to 21 rows. Rewrite it so the page is chosen first from `obligation_batches` (529 rows, keyset on status_rank/planned_date/…) with `LIMIT $3`. Then compute counts only for those 21 batches with `JOIN obligation_instances oi ON oi.tenant_id=$1 AND oi.batch_id = b.batch_id`, which is served by the existing `obligation_instances_batch_idx (tenant_id,batch_id,status)`. Expected time is under 20 ms.

**N4 — Open-window bitmap heap scans (execution, operations, schedule, sheds).** All of these use `obligation_instances_due_window_idx` with `status IN (scheduled,due,in_progress,deferred,completed,missed,waived)` and then read 14–16k heap blocks. Root 1 (vacuum/repack) is the main fix. Adding a covering partial index avoids heap visits for the classifier:
```sql
CREATE INDEX CONCURRENTLY obligation_instances_exec_window_idx
  ON obligation_instances (tenant_id, due_at)
  INCLUDE (obligation_id, target_id, scope_id, batch_id, rule_id, protocol_version_id, status, completed_at, sop_task_id)
  WHERE target_type = 'goat' AND status NOT IN ('canceled','superseded');
```

**N5 — Shed-scoped reads do tenant-wide work** (`/vaccination/execution/sheds/{id}`, `/vaccination/sheds/{id}`). The `shed_id`/`park_id` predicates are applied on `located`/`scoped` at the end of `executionClassifiedCTE` and `shedSummaryCanonicalReadSQL`. With a single shed filter the execution query still took 240 ms against 250 ms unfiltered. Push `($3::text='' OR g.shed_id = NULLIF($3,'')::uuid)` into the base `oi ⋈ goats` join, or `oi.scope_id` if the shed is the scope. With `obligation_instances_scope_idx (tenant_id, scope_type, scope_id, status, due_at)` this should drop to under 30 ms.

**N6 — Live tracker verification tile.** Seq scan on verification_items (21.6k rows scanned, 1,070 kept) with `module='vaccination' OR source_module='vaccination'`:
```sql
CREATE INDEX CONCURRENTLY verification_items_vacc_day_idx
  ON verification_items (tenant_id, status, verified_at)
  INCLUDE (shed_id, park_id, captured_at)
  WHERE module = 'vaccination' OR source_module = 'vaccination';
```
The bigger cost on this endpoint is about 200 ms of custom-plan planning across 6 statements (root 4).

**N7 — /vaccination/schedule (483 ms).** The correlated `vda_guess` subquery (`ORDER BY … LIMIT 1` on vaccination_drive_assignments) runs 10,564 times, about 95 ms. Replace it with a CTE `SELECT DISTINCT ON (tenant_id,batch_id,shed_id) … FROM vaccination_drive_assignments WHERE tenant_id=$1 AND planned_date BETWEEN $3 AND $4 ORDER BY …` joined by hash. The remaining time is the 16k-block heap read (root 1 / N4).

**N8 — /vaccination/drive-assignments.** The `member_rules` lateral runs for all 734 assignments, then the month window keeps 14. Apply `planned_date >= $2 AND planned_date < $3` (and the park filter) in the base assignment CTE before the lateral. Supporting index: `CREATE INDEX CONCURRENTLY vaccination_drive_assignments_month_idx ON vaccination_drive_assignments (tenant_id, planned_date, park_id);`.

**N9 — /vaccination/sheds (FAIL, 870–1029 ms exec).** The plan does `Hash Join (rows=1 estimate, actual 102 sheds)` and then a **Nested Loop Left Join whose inner side is a GroupAggregate over a 5.8k-row sort, re-executed 102 times** (loops=102, about 580 ms). A second GroupAggregate also runs 102 times. Both come from stale stats and misestimates. Fixes, in order:
(a) Make the per-(shed, partition) obligation aggregate a `WITH shed_obligation_agg AS MATERIALIZED (… GROUP BY shed_id, partition_key)` and join it by equality, so the planner hash-joins one pre-grouped set. Do the same for the vda/operator-names aggregate.
(b) `ANALYZE obligation_instances, obligation_batches, goat_shed_partitions` (root 1).
(c) Fix the dead cache key.
Expected after (a)+(b): about 250 ms. With N4 or a vacuumed heap: under 150 ms.

### Why stg latency ≫ clone DB ms
- `/vaccination/command`: 1843 ms on stg against about 220 ms wall / 625 ms sum on the clone. This is semaphore queueing (root 3) plus zero cache hits (root 2), multiplied by concurrent CEO/leadership refreshes.
- `shed-dose-matrix`: 962 ms on stg against 144 ms on the clone. It waits for a slot behind the board's 6 sections.
- `/app/vaccination/execution` p95 of 3.6 s: cold reads of about 16.6k heap pages of the bloated table (130 MB) on Cloud SQL, per request.
- `/command/drives` p95 of 2 s: the same shared semaphore on first paint.

---

# DB perf audit: Calendar vaccination, Process Integrity, Vaccination queue/summary

READ-ONLY. goatos-stg data, tenant `...0001`, as_of 2026-09-24 12:00 IST. Code at `~/mesha/goatos-pr-burst/backend`.
"DB ms" = planning + execution summed over every SQL statement in one request (cold per-instance cache). Planning is included because
calendar queries are custom-planned (literal and prepared-custom plans both take about 110 ms to plan), and process-integrity uses `pgx.QueryExecModeExec`, so it never reuses a plan.
Plans are saved under `scratchpad/audit/vc/plan_*.txt` and reproduced with `vc/ex2.py` (it resolves Go const concatenation; `PREP=1` uses PREPARE/EXECUTE).

Background that affects every row: `obligation_instances` holds 86.6k live and 347k dead tuples. Its last ANALYZE ran on 2026-09-21, before the drive-member churn. Any random PK probe into it is 3–5x more expensive than it should be, and row estimates are wrong. For example, members for one assignment are estimated at 1 row but the actual count is 42, and that error causes the 5.7 s targets plan.

### Summary table

| endpoint | handler file:func | #SQL/req | DB ms | seq scans / spills | grade | fix |
|---|---|---|---|---|---|---|
| GET /calendar/vaccination/events (week: list+rail+date markers+filter opts+drive summary) | calendar/adapters/http/handler.go:ListEvents | 4 (+BEGIN/SET LOCAL/COMMIT for drive summary) | list 227 + markers 251 + rail 257 + filters 14 = **~750** | seq: protocol_rule_dimensions(1971), goats(1741), obligation_batches×4, vda×3, vaccination_completions; no spills. 4.5k PK probes into bloated obligation_instances (23k buffers) in drive-member branch | **FAIL** | Collapse list+markers+rail into one CTE pass (or run in parallel); stop 3× 110 ms planning; vacuum/repack obligation_instances; partial index below; longer-term restore a projection (see note C1) |
| GET /calendar/vaccination/events?markers_only (month, 6 wks) | same | 1 | 107 + 366 = **473** | same seq scans; drive grouping 1023 rows sorted | **MAX** | same as above; markers from a per-day rollup |
| GET /calendar/vaccination/events (month list page) | same | 1 | 109 + 234 = **343** | same | **MAX** | same |
| GET /calendar/vaccination/events/{id} | handler.go:GetEventDetail → repo.GetEventDetail + History | 4 (detail CTE, **eventExists CTE**, history, +BEGIN/SET) | 126+1346 + 109+1204 + 11 = **~2,800** | 20 seq scans; ±2-year window over canonical CTE | **FAIL** (explains stg p95 3.2 s) | Derive a narrow window from event_id (C2); drop the redundant eventExists inside detail→History |
| GET /calendar/vaccination/events/{id}/targets | handler.go:ListDriveTargets | 2 (eventExists + targets) | 1313 + 38+5689 = **~7,000** | Bitmap heap on obligation_instances rescanned **42×** (692k heap blocks read, 213k join-filter rejects) | **FAIL** | Drive the assignment path from members → oi PK (C3); cheap existence check |
| GET /calendar/vaccination/events/{id}/history | handler.go:History | 2 | 1313 + 11 = **~1,320** | eventExists = full canonical CTE ±2y | **FAIL** | replace eventExists with id-parsed indexed lookup (C2) |
| GET /vaccination/action-center | processintegrity/adapters/http/handler.go:ActionCenter | 2 (rows ‖ counts, parallel goroutines) | 61+188 + 64+195 = **~508** (wall ≈ 260) | seq vaccination_completions(6.1k), vdam(6.2k)×3, goats, obligation_batches×2, vda×4, sop_tasks | **FAIL** (sum) | One query for rows + counts (C4); plan caching; grain rollup (C5) |
| GET /vaccination/action-center/counts | handler.go:ActionCenterCounts | 1 (count cache, 30 s bucket) | 64+195 = **~259** | same | **OK** | C5 rollup; reuse list cache key hits |
| GET /vaccination/adherence | handler.go:ProtocolAdherence | 2 (rows ‖ adherence summary) | 65+170 + 59+164 = **~458** | same | **MAX** | C4/C5 |
| GET /control-tower/vaccination | handler.go:ControlTower | 2 (rows ‖ counts; 2nd CountByWorkState skipped when no filters, else +1) | 57+184 + 62+190 = **~493** (3 SQL / ~750 when filters set) | same | **MAX** | C4/C5; filters: compute summary counts in same pass |
| GET /vaccination/workflows/{row_id} | handler.go:WorkflowDrilldown → GetRow → ListRows(RowID) | 1 | 65+631 = **~696** | builds all 6,243 grains, then filters by row_id; **external merge sort 4.9 MB** (still spills at work_mem 32MB: 585 ms) | **FAIL** | Push row_id components (batch/rule/pv/shed/date) into the `raw` CTE (C6) |
| GET /vaccination/verification-queue | vaccination/adapters/http/handler.go:VerificationQueue | 2 (count + page, sqlc) | 7.4 + 3.9 = **~12** | none (vaccination_completions_review_idx) | **PASS** | — |
| GET /app/tasks/{id}/shed-completion-summary | handler.go:ShedCompletionSummary | 1 | 28 + 5 = **~33** (200-member task) | locations (179 rows) only | **PASS** | — |

### In-process caches (do they hit?)

- **Calendar** `Repository.listCache` in repository.go:27. It is per Cloud Run instance, uses a 60 s TTL and a 256-entry map, and applies only to `ListEvents`. The key holds all 20 query dims, including cursor/limit and all `include_*` flags. Month view sends a `markers_only` call and a list call, and these never share an entry. The cache is cleared only by writes from the same process (nudge/snooze/escalation). Sweepers run in other binaries and never clear it, so it can also serve stale data for up to 60 s after a vaccination write. Detail, targets and history have **no cache**, and they are the slowest endpoints. Hit rate at stg traffic is effectively a user re-clicking within 60 s on the same instance.
- **Process integrity** `readCache`/`countCache` in repository.go:24. The key uses as_of bucketed to 30 s, so it does hit on repeat within the same instance. `ListRows` also seeds `countCache`, so `/action-center` followed by `/counts` hits on the second call. `GetRow` (drilldown) goes through ListRows with RowID in the key, so each row is its own miss.

### Detailed notes (non-PASS)

### C1: Calendar list/markers/rail (events, week and month)
- All three queries rebuild `calendarCanonicalEventsCTE` (≈2,000 lines) from scratch. Each one costs **~110 ms of planning**, so the week view pays 330 ms just to plan. Execution is 116–366 ms.
- Hot spot in execution is the drive-member branch of `source_events`. It hash-joins 6,212 `vaccination_drive_assignment_members` rows to 624 assignments, then does **4,506 PK probes into `obligation_instances`** (`obligation_instances_tenant_id_unique`, 23k buffers). Only 692 survive the `status NOT IN (superseded,canceled,waived) AND batch_id IS NOT NULL` filter. Next come the `drive_sources` GroupAggregate and the `obligation_drive_membership` re-aggregations, which cost about 40–120 ms.
- Fixes, in order:
  1. **Ops**: `VACUUM (ANALYZE) obligation_instances;` and plan a `pg_repack` or VACUUM FULL window, because 347k dead tuples of 135 MB is roughly 4x the live data. Tune `ALTER TABLE obligation_instances SET (autovacuum_vacuum_scale_factor=0.02, autovacuum_analyze_scale_factor=0.02);`.
  2. **Partial covering index**, so member probes skip canceled and dead heap rows and can run index-only after vacuum:
     `CREATE INDEX CONCURRENTLY idx_oi_live_batched_by_id ON obligation_instances (tenant_id, obligation_id) INCLUDE (batch_id, rule_id, protocol_version_id, target_id, due_at, status) WHERE batch_id IS NOT NULL AND status NOT IN ('superseded','canceled','waived');`
     This covers about 10k of the 86.6k rows, because 76k are canceled.
  3. **One pass per request.** When `include_date_markers`/`include_reminder_rail` are set, compute `canonical_selected`, markers (GROUP BY business date) and the rail (`count(*) OVER()` + LIMIT 20) in **one** statement over the same CTE, returned as JSON aggregates. This removes 2×(110 ms plan + ~120 ms exec). A cheaper minimum is to run the three statements in parallel goroutines, as processintegrity already does.
  4. Structural fix: a canonical compute-on-read over the full drive graph cannot fit under 300 ms with planning included. Add a trigger- or outbox-maintained `calendar_drive_day_rollup(tenant_id, event_id, due_at, status, park_id, shed_id, owner_key, vaccine_name, summary jsonb)`, or a 60 s shared cache (Redis/pg `UNLOGGED` table) keyed by (tenant, window, filters) instead of the per-instance map.

### C2: Calendar detail and history (`GetEventDetail`, `History`, `eventExists`)
- `GetEventDetail` and `eventExists` both use `canonicalUnboundedWindow(now)` = **now−2y … now+2y**. That rebuilds every drive over 4 years to find one event: 1.35 s + 1.20 s.
- `GetEventDetail` calls `r.History(...)`, and History calls `eventExists` again. That is a second full canonical CTE for an event already resolved.
- Fixes:
  1. The window can be derived from the event_id, which is parsed by `domain.ParseDriveEventID` / `event_id.go`:
     - `parkdrive:...:date:YYYY-MM-DD`: the date is ±1 day.
     - `vaccinationdrive:assignment:<id>`: look up `planned_date` with `SELECT planned_date FROM vaccination_drive_assignments WHERE tenant_id=$1 AND assignment_id=$2`, which is a PK lookup.
     - `obligation:<id>`: `due_at` by PK. Pass `[d−1d, d+1d)`, extended by the same 45-day catch-up used by the list for in-progress/missed drives.
     Measured with a ±1-day window: detail drops from 1,472 ms to **578 ms** (118 plan + 460 exec). Combine with C1 (1)+(2) to go lower.
  2. Add an internal `historyRows` without `eventExists` and call it from GetEventDetail. Saves ~1.3 s immediately.
  3. For standalone `/history` and `/targets`, replace `eventExists` with a scope check driven by the id: assignment/batch/obligation PK plus park/shed scope comparison. That is a few ms instead of 1.3 s.

### C3: Drive targets (`calendarDriveTargetsSQL`, targets.go:55)
- The plan starts from `obligation_instances oi` (bitmap on `obligation_instances_vaccination_drive_day_idx`, tenant-only: 53k index entries and 10.5k heap rows), joins protocols and goats, and only then LEFT JOINs `vaccination_drive_assignment_members m`. The assignment filter (`$15`) is applied late. Members are misestimated at 1 row but the actual count is 42, so the planner put the whole oi bitmap scan on the inner side of a nested loop and **ran it 42 times**. The result was 692k heap blocks read (about 5.4 GB of I/O) and 196k goat PK probes for 42 output rows.
- Fix (query rewrite): when `assignmentID` is set, use a dedicated SQL constant whose row source is
  `FROM vaccination_drive_assignment_members m JOIN obligation_instances oi ON oi.tenant_id=m.tenant_id AND oi.obligation_id=m.obligation_id WHERE m.tenant_id=$1 AND m.assignment_id=$15 AND m.canceled_at IS NULL`,
  with the rest of the joins unchanged. Do the same for `batchID`, using `oi.tenant_id=$1 AND oi.batch_id=$2` (`obligation_instances_batch_idx` exists). A generic OR-heavy predicate across 4 identity shapes will keep producing bad plans, so each shape needs its own SQL. The expected result is 42 PK probes, under 20 ms.
- Also: fresh stats on obligation_instances (C1-1). Drop the duplicate `vaccination_drive_assignment_members_tenant_assignment_idx`, which is identical in columns to `..._active_assignment_idx` (check the partial predicate first).

### C4: Process integrity list and counts (action-center, adherence, control-tower)
- Each request runs `processIntegrityAllRowsSQL` twice in parallel: once for rows and once for counts or the adherence summary. Each run builds the full `raw → located → grouped → all_rows` chain: about 6.2k obligation grains, 6.1k vaccination_completions seq-scanned, and vdam seq-scanned 3 times.
- Each run costs ~60 ms planning plus ~165–195 ms execution. `QueryExecModeExec` rules out plan reuse.
- Fixes:
  1. Emit rows and counts from **one** statement. `all_rows` is already materialized as a CTE, so add `counts AS (SELECT work_state, count(*) FROM all_rows GROUP BY 1)` and return it as a JSON column on the first row, or with a `UNION ALL` tagged row type. This halves DB time to about 250 ms (OK).
  2. Switch to `QueryExecModeCacheDescribe` or the default statement cache, and test `plan_cache_mode=force_generic_plan` for these statements. The `$N::text=''` OR-guards are already generic-friendly, and this saves about 60 ms per statement.
  3. The seq scans over `vaccination_completions` (6.1k) and `vaccination_drive_assignment_members` (6.2k) are cheap at 1–3 ms. Stale stats (estimated 2,033 rows, actual 6,098) push the planner toward nested loops elsewhere, so ANALYZE both tables.
- Control tower with a work_state/severity/owner filter issues a 3rd full query (`CountByWorkState(summaryQuery)`), about 250 ms more. Compute the unfiltered summary counts in the same pass: `count(*) FILTER (...)` over `all_rows` before the filter.

### C5: Structural option for PI
All four PI endpoints reaggregate the same (park, shed, batch, rule, protocol_version, business_date) grain. A projector-maintained `process_integrity_grains` table would make every read an indexed range scan with total DB time well under 50 ms. The table would be refreshed on obligation, completion, and verification events, the same outbox the calendar reconciler uses. Suggested index: `(tenant_id, work_state, sort_priority, due_at, row_id)`.

### C6: Workflow drilldown (`/vaccination/workflows/{row_id}`)
- `GetRow` sets RowID and IncludeCompleted, then runs the **full** rows query. The `row_id = $12` filter applies only at `all_rows` (repository.go:1626), after all 6,243 grains are grouped. The GroupAggregate sort spills to disk (external merge, 4.9 MB) and still spills at work_mem 32MB (585 ms).
- Fix: the row id is structured as `batch:<uuid>:rule:<uuid>:protocol_version:<uuid>:shed:<uuid>:partition:<n>:date:<YYYY-MM-DD>` (feed rows use `feed_projection_exception:<uuid>`). Parse it in Go and pass `$batch_id`, `$rule_id`, `$pv_id` and the business date as extra params pushed into the `raw` CTE: `AND ($21::uuid IS NULL OR oi.batch_id=$21) AND ($22::uuid IS NULL OR oi.rule_id=$22)`, and set due window = that date. `obligation_instances_batch_idx` already exists. Expected result is about 1.5k rows scanned instead of 15k, with no spill and roughly 70 ms including planning.

### Index DDL proposed (not executed)
```sql
-- C1/C3: live, batched obligations by id (≈10k of 86.6k rows), covering the drive-member probe columns
CREATE INDEX CONCURRENTLY idx_oi_live_batched_by_id
  ON obligation_instances (tenant_id, obligation_id)
  INCLUDE (batch_id, rule_id, protocol_version_id, target_id, due_at, status)
  WHERE batch_id IS NOT NULL AND status NOT IN ('superseded','canceled','waived');
-- Ops (not DDL on schema): VACUUM (ANALYZE) obligation_instances, vaccination_drive_assignment_members,
-- vaccination_completions; schedule pg_repack for obligation_instances; lower its autovacuum scale factors.
```
The larger gains come from the query rewrites in C2, C3, C4 and C6, not from indexes. Most scans are already index-driven. The cost comes from rebuilding very large CTEs and from probing a bloated table.
