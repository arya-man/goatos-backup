## Pen routines (internal/penroutines) — DB audit

Clone data: goats 1741, goat_shed_partitions 1740, locations 179, shed_partitions 130, user_scope_grants 71, workforce_members 42. **pen_routine_* tables are EMPTY on the clone**, so the task and routine reads were checked for plan shape and planning time only. Their exec time at stg volume is not measured. All numbers use jit=off, EXPLAIN (ANALYZE, BUFFERS), park 3001.

| endpoint | handler file:func | #SQL/req | DB ms (plan+exec) | seq scans/spills | grade | fix |
|---|---|---|---|---|---|---|
| GET /admin/pen-routines/catalog | admin_handler.go:Catalog → AuthoringService.Catalog → CatalogPens (sqlAuthoring9) + RoleHoldersForPark | 2 (sequential, no N+1) | pens 8.0+25.9 (generic plan 8.8+28.1; psql wall 40–57); role holders 2.9+1.0 → **~38 ms, ~45–60 wall** | For each of the 67 pens, a correlated EXISTS runs a **Seq Scan of goat_shed_partitions (746 rows × 67 loops ≈ 50k rows, 5.4k buffers)** plus a bitmap scan of goats; the alias-exclusion anti join discards 6,674 rows with regexp. No spills. | PASS (DB) — **stg 984/1218 ms is not DB time** | see note A |
| GET /admin/pen-routines | admin_handler.go:List → ListRoutines(readRoutines) + listRoutineAssignees + ListParks | 3 | parks 2.3+0.2; routines/assignees ~5–10 plan, ~0 exec (empty) → **~15 ms** | locations seq (179 rows, fine) | PASS | none; the 2 correlated counts per routine use pen_routine_tasks_routine_idx |
| GET /admin/pen-routines/{id} | admin_handler.go:Get → GetRoutine(readRoutines) | 2 | ~10 | – | PASS | – |
| GET /admin/pen-routines/tasks | admin_handler.go:ListTasks → ListForPark + sqlRepository11 | 2 | ~8–12 (empty tables; park_day_idx covers both) | the assigneesJSONSQL subquery runs once per row (≤26 rows) | PASS (volume unverified) | recheck when stg has tasks |
| POST/PUT /admin/pen-routines (brief) | admin_handler.go:Create/Update | ~6–8 in 1 tx (idem reserve, insert def/version, delete+insert pens, re-read via readRoutines+assignees, outbox/audit, idem complete) | est. 20–40 (round-trip bound) | – | PASS | – |
| GET /app/pen-routines | handler.go:ListMine → ListMine + sqlRepository3 counts | 2 | plan 3.2 + 6.6, exec <1 (empty) → **~12 ms** | The RoutinesHeldBy IN-semijoin is resolved once. workforce_members seq (42 rows). | PASS | – |
| GET /app/pen-routines/{task_id} | handler.go:GetTask → GetTask | 1 | ~4 | PK lookup | PASS | – |
| POST /app/pen-routines/{id}/presence, /submit (brief) | handler.go:RecordPresence/Submit | ~6–7 in 1 tx (idem, getRow FOR UPDATE, update, insert presence, outbox, re-read); Submit also validates proofs first (+1–2) | est. 15–35 | – | PASS | – |

### Note A — why catalog is ~1 s on stg while DB is ~40–60 ms
1. **No N+1.** The service issues exactly 2 queries and the handler loops only in memory. Pool round trips add ~2–4 ms. Planning is ~11 ms, and pgx statement caching removes most of it after the first call.
2. **The largest DB cost** is the per-pen `occupied` EXISTS. It joins all of goat_shed_partitions for each pen and applies `regexp_replace` to every row (~26 ms here). If stg Cloud SQL is shared-core or has a cold cache, this could run 2–4× slower (~60–100 ms). That still does not reach ~1 s.
3. **The remaining ~900 ms is outside this module's SQL.** Candidates to check in a trace span: the auth middleware (`buildAuthMiddleware` with grantSource / allowedEmailSource / App Check, loaded per request unless cached), pool connection acquire when idle conns = 0 (TLS plus a Cloud SQL connector handshake of ~100–300 ms), and Cloud Run CPU throttling. Catalog is a rarely called editor-open endpoint, so it probably hits a cold connection almost every time. **Action:** add an otel span around `h.service.Catalog` and compare it with the request span. If the DB span is <100 ms, set pgxpool `MinConns>=2` / `MaxConnIdleTime` and cache grants.
4. **Fix for the DB part (N+1-in-SQL kill).** Pre-aggregate occupancy once instead of once per pen:
```sql
WITH occ AS (
  SELECT DISTINCT g.shed_id,
         regexp_replace(lower(btrim(COALESCE(gsp.partition_label,'whole'))),'^part[[:space:]]+','') AS norm
  FROM goats g
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id=g.tenant_id AND gsp.goat_id=g.goat_id
  WHERE g.tenant_id=$1 AND ($2::uuid IS NULL OR g.park_id=$2)
    AND g.lifecycle_status='alive' AND g.exited_at IS NULL)
... EXISTS (SELECT 1 FROM occ WHERE occ.shed_id=shed.location_id
            AND (sp.normalized_label IS NULL OR occ.norm=sp.normalized_label)) AS occupied
```
This makes one pass over alive goats (~1.7k rows) instead of ~50k row visits. Expected exec is ~5 ms. Optional supporting index (propose only):
`CREATE INDEX CONCURRENTLY goats_tenant_park_alive_shed_idx ON goats (tenant_id, park_id, shed_id) WHERE lifecycle_status='alive' AND exited_at IS NULL;`
Cache alternative: the catalog changes only when sheds or partitions change, and when occupancy changes. A 60 s in-process cache keyed by (tenant, park) removes the whole cost.
