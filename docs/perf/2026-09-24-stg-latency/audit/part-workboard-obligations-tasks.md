# Part: Work Board, Action Center obligations, SOP tasks, leadership tasks, pen routines — DB audit

This is a read-only audit. Every statement ran as `BEGIN READ ONLY; SET LOCAL jit=off; EXPLAIN (ANALYZE, BUFFERS) …; ROLLBACK`. **`jit=off` is required.** The API pool sets it in `platform/postgres/postgres.go:configureOLTPRuntime`. Without it the canonical vaccination SQL (cost about 140k) shows about 936 ms of JIT emission, which the app never pays.

Setup: tenant `…0001`, park `…3001` (995 goats). Dates: 2026-09-24 (today, no vaccination work), 2026-08-01 (727 completed shots), 2027-01-30 (738 scheduled), and 2026-09-23 / 2026-09-10 for the other sources. SQL was taken verbatim from the Go constants. The Python const evaluator and a `go test -overlay` dump rendered the SQL without editing the tree. Artifacts are in `scratchpad/wb/`, `audit-tasks/` and `audit-pen/`.

## Summary table

| endpoint | handler file:func | #SQL/req | DB ms (clone, plan+exec) | seq scans / spills | grade | fix |
|---|---|---|---|---|---|---|
| GET /work-board/page | workboard/adapters/http/handler.go:Page → app.Service.Summary + 4× Service.List | 20–45. Summary: 8 source counts + 4 feed cards + vaccination precheck + 1 canonical rows (memo) + lane prefetch lists. Lanes: up to 8 sources × 4 lanes, memoized | Critical path about 400 ms: precheck 245 on an empty day (33 on a busy day), then canonical rows about 150, then about 15 per lane list. Total DB work about 700–800 ms, mostly planning (5–15 ms per non-vaccination statement, 56 ms for the canonical SQL) | No seq scans on tables over 1k rows. The precheck does a **full-tenant index walk of obligation_instances** (86k tuples via batch_idx, 133k buffers). No spills | **FAIL** (stg p50 7.2 s; clone about 0.4–0.5 s) | (1) Rewrite the precheck as a UNION ALL of indexable arms plus a new partial index (note W1). (2) Pool: a page uses up to 9–10 connections at once against MaxConns=10 (note W2). (3) Get the real stg breakdown with `?debug_timing=1` before further changes |
| GET /work-board/rows | handler.go:Rows → Service.List → listFirstPage | 10 source lists + 3 more feed cards + vaccination precheck + canonical rows (up to 20 walk pages) + fillOwnerUserIDs ≈ 16 | about 250–400 critical path (precheck plus canonical); others about 10–25 each at 4-way concurrency | same precheck full-tenant walk | **MAX** (stg 114 / 852) | W1. p95 matches precheck-on-empty-day + canonical |
| GET /work-board/summary | handler.go:Summary → Service.Summary | 8 counts + 4 feed + precheck + canonical **counts** SQL (no memo, so `CountByWorkStateLive`) ≈ 14 | about 250 (precheck 245 on an empty day) or about 150–170 on a busy day (precheck 33 + counts 58 plan + 94–105 exec); other counts under 15 each | precheck walk | **OK** | W1 |
| GET /work-board/rows/{key}/subtasks | handler.go:Subtasks → Service.FindRow (a whole ListRows walk of one source) + Source.ListSubtasks | vaccination: precheck + canonical walk + fillOwner + subtask SQL = 4; other sources: 1 list + 1 subtask SQL | vaccination about 250 busy / 430 empty day (FindRow ≈ rows read; subtask SQL 33 plan + 5 exec); others under 30 | none | **OK / MAX** (vaccination) | W1, and W3: resolve the row with one `row_id`-keyed canonical read instead of walking |
| POST /work-board/flags | handler.go:Flag → FlagService.Raise | FindRow (as above) + parkHeadSQL (user_scope_grants ⋈ workforce_members, tables under 100 rows) + leadership Raise tx (about 5) | about 200–450 vaccination / under 60 other | none | OK (graded by FindRow) | W1 / W3 |
| GET /action-center/obligations | **processintegrity** handler.go:ActionCenter → Service.ActionCenter → Repository.ListRows (the obligation handler is dead code, see note A) | 2 in parallel: canonical rows + canonical counts (same 1.3k-line CTE); in-memory cache for about 2 s | rows 60 plan + 192 exec, counts 60 + 207 → **about 265 wall** (default: no park, DueAfter NULL, horizon +30 d, vaccination) | `obligation_instances` BitmapOr over due_window_idx (31k index tuples for `status IN` across all history) + batch_idx + pkey, heap 119 rows. No seq scan, no spill | **OK** | A1: pass a DueAfter floor (open-work horizon) so due_window_idx does not scan all history; JIT is already off |
| GET /admin/tasks, /admin/tasks/{id}, /app/tasks, /app/tasks/{id} | sop/adapters/http/handler.go:ListAdminTasks/GetTask/ListAppTasks/GetAppTask | 1 / 3+S (N+1 per submission, S up to 16) / 2 / 3+S | 7 / 5–27 / 1 / 5–27 | sop_tasks seq (571 rows); top-N heapsort 109 kB | PASS | Detail: load all `sop_submission_items` in one query (N+1 kill; 19 round trips at about 40 ms RTT ≈ 760 ms wall). Growth: `CREATE INDEX CONCURRENTLY sop_tasks_admin_list_idx ON sop_tasks (tenant_id, due_at ASC NULLS LAST, updated_at DESC, task_id DESC);` |
| POST /admin/tasks*, /app/tasks/{id}/scan-*, /submissions | sop handler Create/Assign/Verify/Rework/RecordScan*/SubmitTask | 1–6 in tx; submit also runs `listSubmissions` twice after commit | under 75 (submit worst case; obligation_instances via batch_idx/PK only) | none | PASS | Drop the duplicate post-commit listSubmissions |
| GET /app/leadership-tasks (+/assignees, /{id}, /activity, /mentionable-users) | leadershiptasks/adapters/http/handler.go:ListTasks/ListAssignees/GetTask/GetTaskActivity/ListMentionableUsers | 5 (2 pgx batches) / 1 / 6 (2 RT) / 4 / 1 | 21 / 7 / 23 / 18 / 9 | tiny tables only | PASS | Optional: fold getRow into the enrich batch |
| POST /app/leadership-tasks* | Raise/Edit/Status/Comment/Seen | 2–7 in tx | under 30 | none | PASS | – |
| GET /admin/pen-routines/catalog | penroutines/adapters/http/admin_handler.go:Catalog → CatalogPens + RoleHoldersForPark | 2 sequential | about 38 (pens 8 + 26; role holders 4) | a correlated EXISTS **seq-scans goat_shed_partitions (1.7k rows) once per pen**: 67 loops ≈ 50k row visits plus regexp | PASS on DB (**stg 984/1218 is not DB time**) | Pre-aggregate occupancy in one CTE (note P1). Trace the rest (pool connection acquire / auth) |
| GET /admin/pen-routines, /{id}, /tasks | admin_handler.go:List/Get/ListTasks | 3 / 2 / 2 | about 15 / 10 / 12 (pen_routine_* tables are empty on the clone, so only plan shape was checked) | – | PASS (volume unverified) | – |
| GET /app/pen-routines, /{task_id} | handler.go:ListMine/GetTask | 2 / 1 | about 12 / 4 | – | PASS | – |
| POST/PUT pen-routines, presence, submit | admin_handler Create/Update/SetStatus; handler RecordPresence/Submit | 6–8 in tx | under 40 est. | – | PASS | – |

Per-source Work Board numbers (clone, park 3001, plan + exec in ms, measured on 2026-09-10/23/24): weighing count 9 + 0.2, list 13 + 0.5 (plus listClosed/listVP 11–19 plan); health 5 + 0.2 / 9 + 0.4; pccare 4 + 0.2 / 8 + 0.4; penvisits 2 + 0.2 / 6 + 0.9; penroutines 2 + 0.1 / 10 + 0.4; milk 1.5 + 0.2 / 5 + 0.6; verification 4 + 1.9 / 7–11 + 2; approvals 9–10 + 0.5 / 13 + 1; feed cards 5–7 + 0.3–7 each (×4). **Every non-vaccination source is planning-bound (70–95% planning), and none exceeds 25 ms.**

## Notes per non-PASS

### W1 — vaccination due-work precheck walks the whole tenant (work-board page/rows/summary/subtasks/flags)
`processintegrity/adapters/boardsource/source.go:vaccinationDueWorkPrecheckSQL` is `EXISTS(... WHERE oi.tenant_id=$1 AND target_type='goat' AND status<>'canceled' AND (due_at in day OR ob.planned_date in day OR EXISTS(member-assignment in day) OR EXISTS(batch-assignment in day)))`. Because the day window is OR'ed across three joined tables, no index predicate is left except `tenant_id`. The plan is an `Index Scan using obligation_instances_batch_idx` with only `Index Cond: tenant_id`. It reads **all 86,616 rows (76,041 removed by filter), 133k buffers, and 228 ms** whenever the day has no match. That covers every "today"/future board with no drive, which is the common case. When a match exists, EXISTS stops early (14 ms). This one query also accounts for most of `obligation_instances_batch_idx` idx_tup_read (471M on the clone). On stg the tuple count is larger, so it overruns its 300 ms budget and **fails open**: the canonical 400 ms read then always runs, and the source is degraded or slow on every page.

Fix, a rewrite with the same semantics (verified: **5 ms exec, 24 ms plan** on the empty day vs 228 ms):
```sql
WITH cand AS (
  SELECT oi.target_id, oi.protocol_version_id, oi.status FROM obligation_instances oi
   WHERE oi.tenant_id=$1 AND oi.target_type='goat' AND oi.status<>'canceled' AND oi.due_at>=$3 AND oi.due_at<$4
  UNION ALL
  SELECT oi.target_id, oi.protocol_version_id, oi.status FROM obligation_batches ob
   JOIN obligation_instances oi ON oi.tenant_id=ob.tenant_id AND oi.batch_id=ob.batch_id
   WHERE ob.tenant_id=$1 AND ob.planned_date >= ($3 AT TIME ZONE 'Asia/Kolkata')::date AND ob.planned_date < ($4 AT TIME ZONE 'Asia/Kolkata')::date
  UNION ALL
  SELECT oi.target_id, oi.protocol_version_id, oi.status FROM vaccination_drive_assignments vda
   JOIN vaccination_drive_assignment_members vdam ON vdam.tenant_id=vda.tenant_id AND vdam.assignment_id=vda.assignment_id
   JOIN obligation_instances oi ON oi.tenant_id=vdam.tenant_id AND oi.obligation_id=vdam.obligation_id
   WHERE vda.tenant_id=$1 AND vda.planned_date >= ($3 AT TIME ZONE 'Asia/Kolkata')::date AND vda.planned_date < ($4 AT TIME ZONE 'Asia/Kolkata')::date
  UNION ALL
  SELECT oi.target_id, oi.protocol_version_id, oi.status FROM vaccination_drive_assignments vda
   JOIN obligation_instances oi ON oi.tenant_id=vda.tenant_id AND oi.batch_id=vda.batch_id
   WHERE vda.tenant_id=$1 AND vda.park_id=$2 AND vda.planned_date >= ($3 AT TIME ZONE 'Asia/Kolkata')::date AND vda.planned_date < ($4 AT TIME ZONE 'Asia/Kolkata')::date)
SELECT EXISTS (SELECT 1 FROM cand c
  JOIN goats g ON g.tenant_id=$1 AND g.goat_id=c.target_id AND g.park_id=$2
  JOIN protocol_versions pv ON pv.tenant_id=$1 AND pv.protocol_version_id=c.protocol_version_id
  JOIN protocol_definitions pd ON pd.tenant_id=$1 AND pd.protocol_id=pv.protocol_id AND pd.category='vaccination'
  WHERE c.status<>'canceled' AND ($5 OR c.status<>'completed'));
```
Supporting indexes (proposed only). Arm 1 currently has no exact partial match: `vaccination_drive_day_idx` also excludes superseded and waived.
```sql
CREATE INDEX CONCURRENTLY obligation_instances_goat_live_due_idx
  ON obligation_instances (tenant_id, due_at) INCLUDE (target_id, protocol_version_id, status)
  WHERE target_type = 'goat' AND status <> 'canceled';
CREATE INDEX CONCURRENTLY obligation_batches_tenant_planned_idx ON obligation_batches (tenant_id, planned_date);
CREATE INDEX CONCURRENTLY vaccination_drive_assignments_tenant_planned_idx ON vaccination_drive_assignments (tenant_id, planned_date, park_id) INCLUDE (batch_id);
```
The batch and assignment tables are small today (1.3k / 734 rows, seq-scanned in about 0.3 ms). Their indexes are for growth.

### W2 — /work-board/page fan-out vs pool (the 7.2 s p50 is not explained by clone SQL)
On the clone the worst critical path is about 0.4–0.5 s, and no single statement except the precheck exceeds 110 ms exec. A page issues 20–45 statements: summary concurrency 9, then 4 lanes × 2 sources. Each non-vaccination statement spends 5–15 ms in **planning** and under 1 ms executing. With the default `MaxConns=10` (`platform/postgres/postgres.go:normalizedConfig`), one page nearly saturates the pool. Two concurrent board loads plus background traffic queue on `pool.Acquire`, and each extra round trip adds Cloud Run→DB RTT. Fixes, in order:
1. W1. It removes the only heavy statement, and on stg it also removes the fail-open double read.
2. Measure: the handler already supports `?debug_timing=1` / `X-GoatOS-Debug-Timing: 1` (phases `summary_service`, `lane_service_*`, `source_count_*`, `source_read_*`). Capture one stg page to separate pool wait from SQL.
3. Cut round trips: collapse the 8 cheap sources' `CountByState` into one `pgx.Batch`, or into one UNION ALL count statement per page. Use the lane counts already known from the summary to skip zero lanes (partly done). The feed source's 4 card queries can be one statement with 4 UNION ALL arms.
4. Planning cost: the canonical PI SQL is forced to `QueryExecModeExec` (a 56–60 ms re-plan every call). Keep that (custom plans matter here), but it should be the only statement per page. Other sources run on the pool's default statement cache, so generic plans are fine for them.
5. Raise `GOATOS_PG_MAX_CONNS` (for example 20) or give Work Board its own bounded semaphore, so one page cannot starve the pool.

### W3 — subtasks/flags resolve the row by walking the whole source
`Service.FindRow` calls `src.ListRows(..., Limit: MaxLimit)` to check that the row is on the caller's board. For vaccination that is the precheck plus the full canonical read (about 150–250 ms) before the 5 ms subtask SQL. Fix: add a `FindRow(ctx, q, sourceID)` method on the vaccination source that sets `pidomain.Query.RowID` (the canonical SQL already supports `$12` row_id, and `GetRow` exists) and skips the precheck. This gives about 60 ms of planning plus under 20 ms exec.

### A — /action-center/obligations: which handler wins
Only `processintegrity/adapters/http/handler.go:Register` (`GET /action-center/obligations` → `ActionCenter`) is wired, in `bootstrap/api.go:1492`. `obligation/adapters/http` (`ListDue`) is **not imported anywhere**, so it is dead code (Go 1.22 mux would panic on a duplicate pattern anyway). It is graded as the PI canonical read. Default request: two 1.3k-line CTE statements run in parallel, each about 60 ms planning + about 200 ms exec. `raw` fetches obligation_instances with a BitmapOr of `due_window_idx` (31k index entries, because `status IN (...)` with no lower due bound walks all history), `batch_idx` and `pkey`, and keeps 119 rows. **A1:** default `DueAfter` to `AsOf - closedHistoryAge (14 d)` for open work, and let overdue come from `obligation_instances_missed_deadline_idx`. That cuts the bitmap to the live window, for an expected under 100 ms exec. JIT must stay off: with JIT on this endpoint is about 1.2 s.

### P1 — pen-routines catalog (stg 984 ms; clone DB only about 38 ms)
The correlated `occupied` EXISTS runs once per pen: 67 × seq scan of goat_shed_partitions plus regexp. Pre-aggregate it once:
`WITH occ AS (SELECT DISTINCT g.shed_id, regexp_replace(lower(btrim(COALESCE(gsp.partition_label,'whole'))),'^part[[:space:]]+','') norm FROM goats g LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id=g.tenant_id AND gsp.goat_id=g.goat_id WHERE g.tenant_id=$1 AND g.park_id=$2 AND g.lifecycle_status='alive' AND g.exited_at IS NULL)`, then `EXISTS (SELECT 1 FROM occ WHERE occ.shed_id=shed.location_id AND (sp.normalized_label IS NULL OR occ.norm=sp.normalized_label))`. Optional: `CREATE INDEX CONCURRENTLY goats_tenant_park_alive_shed_idx ON goats (tenant_id, park_id, shed_id) WHERE lifecycle_status='alive' AND exited_at IS NULL;`. The remaining about 900 ms on stg is outside this SQL: probably cold pool connections (the endpoint is rarely hit) or auth. Add a span around `service.Catalog`, and set pool `MinConns>=2`.

### obligation_instances seq scans (26k on the clone, 1.36M on stg)
**None of the endpoints in this part seq-scan obligation_instances** in any measured plan (PI canonical rows/counts, precheck, subtasks, SOP scan/submit). The heaviest reader in scope is the W1 precheck. It is a full *index* walk (`batch_idx` with only `tenant_id` bound, 86k tuples per call), which shows up as idx_tup_read (471M on batch_idx), not seq_tup_read. Why the existing indexes do not help it: every useful partial index is keyed on `status = ANY(...)`, `batch_id IS NULL`, or `due_at` as the second column. The precheck's `status <> 'canceled'` plus the OR across `ob.planned_date` / assignment dates leaves only `tenant_id` sargable. Checked and ruled out: the FK on `(tenant_id, generated_by_trigger_id)` has no index (Parallel Seq Scan on a protocol_triggers delete), but protocol_triggers has had 0 updates or deletes, so it is not the source. The seq scans come from outside this scope, most likely calendar/vaccination-generation queries that filter on `target_type`/`status` without `tenant_id` + due leading. Attribute them with `pg_stat_statements`, which is **not installed** on the clone: `CREATE EXTENSION pg_stat_statements` plus `shared_preload_libraries`. Index hygiene: `obligation_instances_tenant_due_target_protocol_idx`, `unbatched_due_version_idx`, `idempotency_unique` (16 MB, a constraint, so keep it) and `open_rule_identity/repeat_cycle` show 0 scans on the clone. `tenant_due_target_protocol_idx` (0 scans) is a candidate to drop once W1's index lands.

Merged sub-reports: `audit/part-tasks-sub.md` (SOP + leadership, full table) and `audit/part-penroutines-sub.md` (pen routines, full table).
