# DB perf audit: Calendar vaccination, Process Integrity, Vaccination queue/summary

READ-ONLY. goatos-stg data, tenant `...0001`, as_of 2026-09-24 12:00 IST. Code at `~/mesha/goatos-pr-burst/backend`.
"DB ms" = planning + execution summed over every SQL statement in one request (cold per-instance cache). Planning is included because
calendar queries are custom-planned (literal and prepared-custom plans both take about 110 ms to plan), and process-integrity uses `pgx.QueryExecModeExec`, so it never reuses a plan.
Plans are saved under `scratchpad/audit/vc/plan_*.txt` and reproduced with `vc/ex2.py` (it resolves Go const concatenation; `PREP=1` uses PREPARE/EXECUTE).

Background that affects every row: `obligation_instances` holds 86.6k live and 347k dead tuples. Its last ANALYZE ran on 2026-09-21, before the drive-member churn. Any random PK probe into it is 3–5x more expensive than it should be, and row estimates are wrong. For example, members for one assignment are estimated at 1 row but the actual count is 42, and that error causes the 5.7 s targets plan.

## Summary table

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

## In-process caches (do they hit?)

- **Calendar** `Repository.listCache` in repository.go:27. It is per Cloud Run instance, uses a 60 s TTL and a 256-entry map, and applies only to `ListEvents`. The key holds all 20 query dims, including cursor/limit and all `include_*` flags. Month view sends a `markers_only` call and a list call, and these never share an entry. The cache is cleared only by writes from the same process (nudge/snooze/escalation). Sweepers run in other binaries and never clear it, so it can also serve stale data for up to 60 s after a vaccination write. Detail, targets and history have **no cache**, and they are the slowest endpoints. Hit rate at stg traffic is effectively a user re-clicking within 60 s on the same instance.
- **Process integrity** `readCache`/`countCache` in repository.go:24. The key uses as_of bucketed to 30 s, so it does hit on repeat within the same instance. `ListRows` also seeds `countCache`, so `/action-center` followed by `/counts` hits on the second call. `GetRow` (drilldown) goes through ListRows with RowID in the key, so each row is its own miss.

## Detailed notes (non-PASS)

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
