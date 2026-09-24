# Per-endpoint DB audit: notifications / auth / admin / app / verification / ops

Code: goatos-pr-burst (PR #389 head, read-only). DB: OCI clone <ip>:15432 (PG16), all reads inside read-only sessions (`BEGIN READ ONLY … ROLLBACK`, or a pgx pool with `default_transaction_read_only=on`). Nothing was written and no indexes were created.
Method: real repository methods were run through a pgx tracer harness (`scratchpad/harness`, injected with `go test -overlay`). Every captured statement was re-run under `EXPLAIN (ANALYZE, BUFFERS)` with its real args.
**Grades use server-side plan+exec time.** The tunnel adds about 40 ms RTT per statement; on stg, Cloud Run to Cloud SQL is about 1 ms.
Grades: <=100 PASS, <=300 OK, <=500 MAX, >500 FAIL.
Route inventory: 257 in-scope routes from `internal/permissions/routes.go` (`scratchpad/routes2.txt`), plus `POST /auth/session-events` and the two `/app/counts/shifting*` endpoints. Groups:
- this file: notifications, auth, bootstrap, proofs, goats/search, leadership-tasks, shifting, devices
- `part-admin.md`: every `/admin/*` and `/admin-web/*` route
- `part-verif-ops.md`: verification, ceo-ai, operations, app/health|clock|leave|roster|pc-care|pen-visits|market|workflows, protocols, health-config, health/analytics, goats/{id}, identifiers

## Cross-cutting
1. **JIT is on** (`jit_above_cost=100000`) and bad row estimates trigger it. It adds 0.8–1.0 s to large-cost statements (action-center, workflows) and 20–66 ms to small admin lists. Fix: `ALTER DATABASE goatos SET jit = off;` or set `RuntimeParams["jit"]="off"` in the pool config. **Verify with `SHOW jit` on goatos-stg.**
2. **Tails on cheap endpoints are not caused by their own DB work.** Server-side DB time is small:

| endpoint | stg p95 | server DB |
|---|---|---|
| admin/locations | 658 ms | 7 ms |
| shifting/destinations | 658 ms | 14 ms |
| pending-execution | 858 ms | 6–17 ms |
| auth/session-events | 512 ms | ~1 ms |
| leadership-tasks/assignees | 1320 ms | 2 ms |
| proofs download | 6119 ms | 0.3 ms |

   That points to pool waits, CPU, or IO contention on the shared instance. The main IO load is the notifications unread aggregate: about 78k buffers (≈610 MB) per call for the top member, and cold reads of up to 4 s. Fix notifications first, then re-measure these tails.

## This slice

| Endpoint | Handler → repo | Stmts/req | Tables / indexes | Server DB ms (plan+exec) | Rows scanned→returned; seq/sort | Grade | Fix |
|---|---|---|---|---|---|---|---|
| GET /app/notifications (page) | notificationcentre http.List → postgres.ListNotifications `sqlListNotifications[First]` | 2 (page + unread) | notification_requests (228k rows, 1.35 GB) via `member_feed_idx` (000354) + anti-join probe on `member_dedupe_idx` (000396); workforce_members | page: 2.6–4 ms (161 idx rows→21) | 3 tiny workforce_members seq scans (42 rows) | page PASS | — |
| ↳ unread count `sqlUnreadCount` | same | (1 of 2) | `member_dedupe_idx` range scan + heap Filter read_at/status | **282–322 ms warm; 368 ms part-cold; 4.1 s fully cold (stg note)** | top member 04b544bc: **96,171 heap rows → 19,241 keys → 1 number**, 78k buffers (53k read) | **FAIL (stg p50 1910 / p95 15175; OCI warm = MAX)** | stored counter (below); interim partial index |
| POST /app/notifications/read | MarkRead (tx) | 4: idem reserve, mark CTE, idem complete, commit | pkey + `member_dedupe_idx` | <1 ms (20 ids → 3 keys → 24 rows) | — | PASS | keep counter decrement in this tx |
| AUTH middleware (every request) | httpmiddleware → AccessRepository.ResolveAccessSnapshot | 1 | workforce_members, person_access, person_module_access, person_park_scope (<100 rows) | 0.9 | seq scans on <100-row tables | PASS | optional 30 s in-proc cache |
| POST /auth/session-events | authaudit.RecordSessionEvent | 0–1 allowlist read (30 s TTL cache) + pending-grant claim tx (SELECT … FOR UPDATE on auth_pending_email_grants, `lookup_idx`) + audit_log INSERT | 26-row / 3-row tables | ~1 | — | PASS (p95 512 = Firebase verify + auth pool) | check auth-pool size and JWKS caching |
| GET /app/bootstrap | workforce Service.Bootstrap | 6 **sequential** (member, capabilities, module keys, person assignments, device, grants) | small tables | 5.3 | — | PASS (≈6 RTT) | one pgx.Batch → 1 RTT |
| GET /admin-web/bootstrap | adminui Service.Bootstrap | 2–4 (page access, perms, verify module keys, contract families) | small | <10 (see part-admin) | — | PASS | — |
| POST /app/devices/register, /{id}/heartbeat, /deregister | workforce RegisterDevice etc. | 2–4 writes by PK/unique | workforce_member_devices | ~1 | — | PASS | — |
| GET /app/proofs/{id}/download (+/signed) | proof Download → GetProof (PK) + local RSA V4 signing (no network) | 1 | proof_artifacts (44k) pkey | 0.3 | 1→1 | PASS | p95 6119 is not the DB. The metric likely mixes in PUT upload (streams the body) and complete (GCS HEAD). Split the route metric by method. |
| GET /app/proofs/uploads | ListUploadedProofs | 1–2 (scope auth check + list) | `proof_artifacts_scope_idx` then Filter on metadata->>'client_task_key' | 7.7 | **2,407 rows scanned → 3 returned**, 2k buffers | PASS (grows with each shed's proof history) | `CREATE INDEX CONCURRENTLY proof_artifacts_scope_task_key_idx ON proof_artifacts (tenant_id, scope_type, scope_id, (metadata->>'client_task_key'), created_at DESC, proof_id DESC) WHERE upload_state='completed';` |
| POST/PUT/DELETE /app/proofs/* | Create/UploadLocal/Complete/Delete | 1–3 | proof_artifacts | <2 | — | PASS (DB) | — |
| GET /goats/search | identity SearchGoats | 1 | goats (1.7k on clone) `tenant_display_idx`, 4× locations, gsp, LATERAL goat_identity_events, 2× goat_identifiers | 9.4–10.8 (**8–9 ms is planning**: 10-way join) | q=identifier: `display_id = q OR EXISTS(...)` walks **every live goat** in display order (1741 → 0/1) | PASS on clone; stg p95 1034 (bigger herd plus cold) | rewrite below |
| GET /app/leadership-tasks, /{id}, /activity, /mentionable-users, /assignees; POST raise/edit/status/comment/seen | leadershiptasks repo | 1 (batch of rows+counts); 1–2 | 16 tasks; trgm indexes (000351) | 0.2–2.5 | — | PASS | assignees p95 1320 is not DB (2 ms) |
| GET /app/counts/shifting/destinations | counts ShiftingDestinationCatalog + stage lookup | 2 | locations, partitions, feed tag tables | 14 | small | PASS | — |
| GET /app/counts/shifting-events/pending-execution | ShiftingExecutionService.ListPendingExecution | 2 (page + batched SOP rules; no N+1) | shifting_events, counts_approval_requests (seq, tiny), goats/goat_identifiers per preview | 2–9 | — | PASS | — |

## Notifications deep-dive
- **Access path:** both reads filter `tenant_id = $1 AND context->>'member_id' = <resolved member>::text`.
  - Page read: `notification_requests_member_feed_idx (tenant_id, (context->>'member_id'), requested_at DESC, notification_request_id DESC)` (000354), walked in order and stopped after LIMIT+1.
  - Dedupe anti-join: probes `notification_requests_member_dedupe_idx (tenant_id, (context->>'member_id'), COALESCE(NULLIF(context->>'event_key',''), notification_request_id::text), requested_at DESC, notification_request_id DESC)` (000396). It gets 161 probes at about 8 µs each, so **000396 fully covers the page and mark-read paths.**
- **Where 000396 does not help:** the unread count. The planner uses it as a range scan, but `read_at`/`status` are not in the index, so it needs a heap fetch per row: 96k rows, ~610 MB of buffers. Nothing ever marks these rows read (100% unread; 85% are `feed.proof.pending.verifier`, about 5 delivery rows per key), so the count grows with lifetime history.
- **Interim index** (lets the count run as an index-only scan with no heap reads; estimated 20–40 ms at 96k entries, still linear):
  `CREATE INDEX CONCURRENTLY notification_requests_member_unread_idx ON notification_requests (tenant_id, (context->>'member_id'), (COALESCE(NULLIF(context->>'event_key',''), notification_request_id::text))) WHERE read_at IS NULL AND status <> 'read';`
  The expression must match `dedupeKeyExpr` character for character.
- **Stored unread counter** (the real fix; O(1) read):
  ```sql
  CREATE TABLE notification_member_unread_keys (
    tenant_id uuid NOT NULL, member_id uuid NOT NULL, dedupe_key text NOT NULL,
    PRIMARY KEY (tenant_id, member_id, dedupe_key));
  CREATE TABLE notification_member_unread_counts (
    tenant_id uuid NOT NULL, member_id uuid NOT NULL, unread_count int NOT NULL DEFAULT 0 CHECK (unread_count >= 0),
    PRIMARY KEY (tenant_id, member_id));
  ```
  - **Insert side:** a statement-level `AFTER INSERT` trigger with `REFERENCING NEW TABLE` on notification_requests, so all 5 calendar insert sites (`calendar/adapters/postgres/repository.go:517,1167,1529,1782`, `reminder_cadence.go:738`) and any future producer are covered in the **same transaction**. It inserts the distinct (tenant, member_id, dedupe_key) of new unread rows with `ON CONFLICT DO NOTHING RETURNING`, then upserts `unread_count = unread_count + <inserted keys>`.
    - A second delivery row for an already-unread key adds nothing, which keeps dedupe grain parity with `sqlUnreadCount`.
    - It skips rows with a NULL member_id or with read_at set.
  - **Mark-read side:** in the MarkRead tx, after `marked`, `DELETE FROM notification_member_unread_keys WHERE (tenant, member, dedupe_key) IN (SELECT DISTINCT dedupe_key FROM marked) RETURNING 1`, then `UPDATE … SET unread_count = unread_count - <deleted>`. The per-key table makes this idempotent under replay and races, and never double-decrements.
    - An `AFTER UPDATE OF read_at, status` trigger is also acceptable and covers any future writer.
  - **Read side:** replace `sqlUnreadCount` with a PK lookup on the counts table (<1 ms). Keep the old query as a reconciler: a nightly job, or run on a mismatch sample.
  - **Backfill:** a one-off `INSERT … SELECT DISTINCT` from the current unread rows, run in member-sized batches.
  - **Contention:** the counter row is per-member, and producers insert at most about 1 row per device per transition, so the hot-row risk is low. If it bites, shard with `(tenant, member, bucket smallint)` and sum.
  - **Also:** an auto-read or retention policy for `verification_pending` rows once the item is decided. Without it, both history and the key table grow forever.

## goats/search rewrite
Replace the OR with an id set that can use an index:
```sql
AND g.goat_id IN (
  SELECT gi.goat_id FROM goat_identifiers gi
   WHERE gi.tenant_id=$1 AND gi.normalized_value=$q AND gi.status='active' [AND type/scope]
  UNION
  SELECT g2.goat_id FROM goats g2 WHERE g2.tenant_id=$1 AND g2.display_id=$q)
```
It uses `goat_identifiers_lifetime_value_unique (tenant_id, normalized_value)` and `goats_tenant_display_idx`, so it is O(1) instead of O(herd). A miss currently walks the whole herd.

---

## [admin slice] DB latency audit: /admin/* and /admin-web/* GETs (excluding /admin/locations list and /admin-web/bootstrap)

This audit was read-only. It ran against the OCI clone, tenant `00000000-0000-4000-8000-000000000001`.
- **How it was measured:** real repository methods ran in a scratch copy of the backend, through the go-test tracer harness (`default_transaction_read_only=on`). Every captured statement was re-run with `EXPLAIN (ANALYZE, BUFFERS)` using its real arguments. Where the harness was not used, the SQL was rebuilt from the Go constants and run through `q.sh`.
- **What "DB ms" means:** the server-side sum of Planning Time and Execution Time across all statements in one request. It leaves out the OCI tunnel, which adds about 40 ms per statement. On stg, add about 1 ms of round trip per statement.
- **Parameters:** the largest real park (003001, 995 goats), the largest sale deal (41 allocations), the largest register (animals, 1,741 goats), the busiest clock day (2026-09-12), and the SOP with the most versions.
- **Grades:** 100 ms or less is PASS, 300 ms or less is OK, 500 ms or less is MAX, and anything over 500 ms is FAIL.

**The main context:** every admin/config/workforce table except `verification_items` (21.6k rows) and `feed_direction_issue_rows` (83k rows) has fewer than 2k rows. In this slice, DB time is driven by (a) whole-set projections that are sorted before the LIMIT, (b) **JIT compilation**, and (c) serial round trips.

## Cross-cutting finding: JIT is costing 20–66 ms
The clone has `jit=on` and `jit_above_cost=100000`. Some queries estimate costs above that threshold, even though their real work is tiny:
- `sex_lookup` and `species_lookup` hold 2 rows but are estimated at 920–940.
- The people-directory lateral over `verification_items` is estimated at a cost of 242k.

LLVM emission then takes **20 ms** (sexes and species lists, status=all) and **66 ms** (`/admin/workforce/people`, where 24 ms is real work and 66 ms is JIT).
- **Fix:** `ALTER DATABASE goatos SET jit = off;` (or `ALTER ROLE <app_role> SET jit = off;`), or at least `SET jit_above_cost = 5000000`. Check the stg Cloud SQL flag `jit` first. If it is already off, these numbers drop by the JIT share.

## Per-endpoint table

| Endpoint | Handler → repo | Stmts/req (N+1?) | Tables / indexes | Dominant plan notes | DB ms (server) | Grade | Fix |
|---|---|---|---|---|---|---|---|
| GET /admin-web/leave/approvals | workforce/adapters/http/leave_handler.go:Queue → LeaveService.Queue → GetMemberForActor + ListLeaveQueue | 2 (compose loop is Go only) | workforce_members, leave_requests | seq scan workforce_members (42 rows) | 2.2 | PASS | – |
| GET /admin/leave/requests | leave_handler.go:AdminList → ListLeaveRequestsAdmin | 1 | leave_requests, workforce_members | – | 1.1 | PASS | – |
| GET /admin/leave/approval-config | leave_handler.go:Config → GetLeaveApprovalConfig | 1 | leave_approval_config | – | 0.3 | PASS | – |
| GET /admin/configuration/registers | configuration/adapters/http/handler.go:Registers → Repository.Counts + ReferenceLists (called **twice**: once in Counts, once in Service.Registers) | **21 serial** (one count per register plus one per reference list) | every register table | each count is 0.1–5 ms | 13.1 (stg ≈ 35 with RTT) | PASS | Fold the counts into a single `SELECT (SELECT count(*) …) AS parks, (…) AS pens, …` or a `UNION ALL` statement, and reuse the ReferenceLists result. That takes it from 21 statements to 2. |
| GET /admin/configuration/{register} (tested animals, the largest at 1,741 goats; breeds, partitions, sexes, pens) | handler.go:List → projection.list (common.go) | 2 (page and countWrap) | goats seq (1,569 alive), goat_identifiers_goat_status_idx ×2 laterals per goat, goat_shed_partitions_pkey, locations | **animals:** builds all 1,569 rows (2 identifier laterals each), then sorts and applies LIMIT 51; the count re-runs the whole projection | animals 74.8; animals?q=12 98.1; breeds 17.2; partitions 13.1; sexes 1.6; others <10 | PASS (animals is borderline and grows linearly with the herd) | animals: push the keyset and LIMIT into goats before the laterals when no search is given (`FROM (SELECT … FROM goats WHERE tenant_id=$1 AND merged_into_goat_id IS NULL AND (display_id,goat_id)>($cursor) ORDER BY display_id, goat_id LIMIT $n) g LEFT JOIN LATERAL …`). This is served by the existing `goats_tenant_display_idx`, which should be extended to `(tenant_id, display_id, goat_id) WHERE merged_into_goat_id IS NULL`. Replace countWrap for animals with `SELECT count(*) FROM goats WHERE tenant_id=$1 AND merged_into_goat_id IS NULL AND ($2='all' OR lifecycle_status='alive')`. |
| GET /admin/configuration/{register}/options | handler.go:Options → projection.options | 1 | – | pens is the largest | ≤9.6 | PASS | – |
| GET /admin/configuration/{register}/{row_id} | handler.go:Get → projection.get (uses getSQL, uuid-pushed) | 1 | pk lookups | animals get: 2.8 exec (12 plan) | ≤15 | PASS | – |
| GET /admin/configuration/{register}/{row_id}/usage | handler.go:Usage → usageOf | 1–6 (fixed check list, not data-driven) | **feed_items:** `feed_direction_issue_rows` (83k) **seq scan** 20.8 ms, feed_ration_rates seq 4.1 ms | seq scan on an 83k-row table | feed_items ≈ 27; others <3 | PASS | `CREATE INDEX CONCURRENTLY feed_direction_issue_rows_item_idx ON feed_direction_issue_rows (tenant_id, feed_item_key);` and `CREATE INDEX CONCURRENTLY feed_ration_rates_item_idx ON feed_ration_rates (tenant_id, feed_item_key);` If the UI only needs "blocked", use `SELECT EXISTS(…)` instead. |
| GET /admin/configuration/{register}/export (animals, all) | handler_bulk.go:Export → Service.exportRows → repo.List(limit 2000) per page | 2 per 2,000-row page | as for List | 1,741 rows built and sorted | 84.5 (tunnel wall 311 ms is payload transfer) | PASS | Skip countWrap when exporting (add `ListParams.NoTotal`), which saves about 20 ms per register. |
| GET /admin/configuration/workbook/export | handler_bulk.go:WorkbookExport → per register List(2000) | **32** (16 registers × page + count) | all registers | animals 60 ms, species and sexes lists each hit **JIT (~19 ms)** | **207** | **OK** | (1) NoTotal on export: −16 statements and −~60 ms. (2) JIT off: −~40 ms. (3) The animals rewrite above. Expected result is 16 statements and under 100 ms. |
| GET /admin/configuration/{register}/template, /workbook/template | handler_bulk.go:Template/WorkbookTemplate | 1–2 (ReferenceLists only) | reference_lists | – | <1 | PASS | – |
| GET /admin/configuration/{register}/imports, /workbook/imports, /configuration-imports/{job_id}, …/rows, …/errors, /configuration-import-bundles/{id}, …/errors | handler_bulk.go → import_store.go (GetImportJob, ListImportJobs, ImportRows, ImportRowsAfter, GetImportBundle + attachBundleJobs) | 1–2; errors endpoints page by keyset (row_no) | configuration_import_jobs_register_idx, configuration_import_rows PK + `_state_idx (tenant_id, job_id, state, row_no)`, bundles_recent_idx | all 3 tables are **empty** on the clone. Judged statically: every predicate is a leading prefix of an index and keyset-paged | <1 (empty) | PASS (static) | Re-test after a real 2-lakh workbook import. |
| GET /admin/goats/sale-locations | identity/adapters/http/sale_allocation_handler.go:ListSaleLocations → sale_locations.go | 1 | locations, shed_partitions, goats | `EXISTS goats g2` is a **seq scan of goats run 104 times** (0.28 ms each) | 33.4 | PASS | `CREATE INDEX CONCURRENTLY goats_tenant_shed_live_idx ON goats (tenant_id, shed_id) WHERE lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive');` (drops to about 2 ms) |
| GET /admin/goats/sale-candidates | sale_allocation.go:ListSaleCandidates | 1 | goats_tenant_goat_unique, goat_identifiers_goat_status_idx, trgm idx for q, vaccination_completions_goat_history_idx, goat_sale_allocations_live_goat_uq | keyset, 51 rows, per-row laterals are index hits | 4.3 (q=123: 6.0) | PASS | – |
| GET /admin/goats/sale-allocations/{deal} | sale_allocation.go:ListSaleAllocations | 1 | goat_sale_allocations (154 rows) | – | 1.0 | PASS | – |
| GET /admin/location-review-items | locations repo ListReviewItems | 1 | location_review_items (0 rows) | – | <1 | PASS | – |
| GET /admin/locations/{id} | locations repo GetLocation (locationSelectSQL) | 1 | locations (179), loa, capacity lateral, 2 count subqueries | – | <2 | PASS | – |
| GET /admin/locations/{id}/children | ListChildren (limit 200) | 1 | same, park with 91 children | seq scan `locations child` ×91 (tiny) | 3.6 | PASS | – |
| GET /admin/locations/{id}/aliases, /capacity | ListAliases / ListCapacity | 1 | (0 rows each) | – | <1 | PASS | – |
| GET /admin/locations/{id}/usage | Usage (single 5-subquery statement) | 1 | goats seq (995), glh (204), locations, usg | – | 1.1 | PASS | – |
| GET /admin/notifications/browser-registrations | browserpush handler.go:ListRegistrations → listRegistrationsSQL | 1 | workforce_member_browser_push_registrations (1 row) | – | <1 | PASS | – |
| GET /admin/notifications/designations | notificationaudience handler.go:Matrix → ListDesignations + ListAudiences | 2 | designation_catalog, audiences | – | 0.3 | PASS | – |
| GET /admin/operators | workforce handler.go:ListOperators | 1 | workforce_members (42) | – | 1.9 (search 1.2) | PASS | – |
| GET /admin/operators/{id}, /grants, /devices | GetOperator / ListGrants / ListDevices | 1 each | workforce_members, user_scope_grants, workforce_member_devices (385) | – | 1.0 / 0.5 / 0.4 | PASS | – |
| GET /admin/roster/positions | roster_handler.go:ListPositions → ListPositions + ListDutiesForPositions (batched, not N+1) | 2 | workforce_positions, position_module_duties | – | 1.1 | PASS | – |
| GET /admin/roster/positions/{id} | GetPositionProfile → GetPositionByID + ListDutiesForPositions + GetHolderCoverage | 3 | same | – | 1.5 | PASS | – |
| GET /admin/roster/leave, /leave/{id} | ListLeave / GetLeave | 1 | staff leave | – | 0.2 | PASS | – |
| GET /admin/roster/backup-config | ListBackupConfig | 1 | positions, members, locations | – | 0.8 | PASS | – |
| GET /admin/roster/coverage | ListCoverage | 1 | – | – | 0.6 | PASS | – |
| GET /admin/sops | sop handler.go:ListSOPs → ListSOPs + LatestVersionsFor (batched) | 2 | sop_definitions (21), sop_versions (28) | – | 1.3 | PASS | – |
| GET /admin/sops/{id} | GetSOP + PublishedVersion | 2–3 | same | – | 1.0 | PASS | – |
| GET /admin/sops/{id}/versions/{vid} | GetVersion | 1 | same | – | 0.3 | PASS | – |
| GET /admin/workforce/clock-entries | clock_handler.go:AdminEntries → ListClockPresence + PeopleCatalog (2) | 4 | workforce_clock_entries (262), clock_events (431), members, departments | seq scans on tiny tables | 3.2 | PASS | – |
| GET /admin/workforce/clock-entries/{id} | EntryDetail → ClockEntryDetail (entry + events + recent days) | 4 | same | – | 1.9 | PASS | – |
| GET /admin/workforce/people | people_handler.go:ListPeople → ListPeople + PeopleCatalog (2) | 3 | workforce_members (42), **verification_items_operator_status_idx**: lateral aggregate over ~500 items per member × 42 (21k rows read) **before** sort and LIMIT | 24 ms of real work plus **66 ms JIT** | **89.5** | PASS (borderline) | JIT off (→ ~25 ms). Add a covering index so the lateral is index-only: `CREATE INDEX CONCURRENTLY verification_items_operator_stats_cov_idx ON verification_items (tenant_id, operator_id) INCLUDE (status, auto_resolution) WHERE operator_id IS NOT NULL;` Better: apply ORDER BY/LIMIT to workforce_members in a subquery first, then join the lateral, so only 26 members are aggregated. |
| GET /admin/workforce/people/{id}/access | access_handler.go:GetAccess → LoadPersonAccess + ListParks + ListDesignations | 3 | person_access, person_park_scope, person_module_access (737), designation_catalog | – | 1.7 | PASS | – |
| GET /admin/workforce/designations/{code}/defaults | DesignationDefaults + ListDesignations | 2 | designation_module_defaults, designation_catalog | – | 0.3 | PASS | – |

Not re-tested: `/admin/locations/{id}` variants beyond those above, and `/admin/roster/vaccination-owner` (not in routes2.txt).

## Writes under /admin/* (statement counts, static)
- **Configuration row writes** (POST/PUT/DELETE `/admin/configuration/{register}...`, `/status`): run through `Repository.write`, about 7–9 statements in one transaction:
  1. BEGIN
  2. reserveIdempotency
  3. the store's insert/update/delete
  4. `fenced` exists-check, only when 0 rows are affected
  5. `get` re-read (getSQL, index-pushed)
  6. recordAudit
  7. completeIdempotency
  8. COMMIT

  DELETE also runs `usage` first, which reaches 6 counts for feed_items, including the seq scan of the 83k-row `feed_direction_issue_rows` (≈21 ms). The index above fixes it.
- **Import apply/cancel** (`/configuration-imports/*`, bundles): each enqueue is 1–3 statements. Rows are applied by the async worker in batches (ClaimImportRows / UpdateImportRows batch by `row_no = ANY`), with no per-row round trip on the request path.
- **Locations writes** (POST/PATCH/DELETE `/admin/locations...`, aliases, capacity, review items): 3–6 statements. `insertInvalidations` loops over a fixed small set of reasons.
- **Goats admin writes** (`/admin/goats/*` commit/preview, identifiers, move, exit, and so on): `sale-allocations/confirm` runs `insertSaleAllocations` as **one INSERT per allocated goat inside the transaction**. That is an N-statement write: a 41-goat sale is 41 INSERTs plus the reads. **Fix:** a single `INSERT … SELECT FROM unnest($goat_ids::uuid[], $tags::text[], …)`. Previews reuse `ReadSaleCandidateRows` (1 statement, `goat_id = ANY`). The bulk and census commits were not profiled here.
- **Workforce writes** (operators, grants, devices, roster positions and leave, people, access, title, leave approvals): 1–8 statements each. `PUT /admin/workforce/people/{id}/access` is the heaviest, at about 8 statements (delete and reinsert of module and park scope). `approve leave` calls `lockAndCheckMinOperatorCoverage`, which loops over a small number of positions.
- **SOP writes** (create, version, publish, retire, dry-run): 2–5 statements. There is no per-step loop on these admin routes; the submission-item loop is on the app path.
- **Notifications:** `PUT designations/{alert_key}` is 2–3 statements. The browser-registration and browser-event writes are 1 CTE statement each.

## Non-PASS summary
1. **GET /admin/configuration/workbook/export**: 207 ms, 32 statements, graded **OK**. Fixes: drop the per-page count on export, turn JIT off, and apply the animals LIMIT pushdown.

Everything else is **PASS**. The close calls are `/admin/workforce/people` (89.5 ms, 66 of it JIT), `/admin/configuration/animals?q=` (98 ms) and animals list/export (75–85 ms, which grows linearly with the herd).

---

## [verification/ops slice] DB latency audit — verification / ops / action-center / goats / app-* slice

Backend: `~/mesha/goatos-pr-burst/backend` @ b7b3d8505 (read-only). DB: OCI clone (PG 16.9), tenant `00000000-0000-4000-8000-000000000001`.
Method: real repository methods run through the pgx tracer harness (`scratchpad/va/h/*`, `go test -overlay`, no repo files written), with every captured statement re-run under `EXPLAIN (ANALYZE, BUFFERS)`. Dynamic SQL was also rebuilt by hand and run with `PREPARE`/`EXECUTE` (`scratchpad/va/*.sql`).
**Grades use server-side planning + execution time.** The OCI tunnel adds about 40 ms per statement; on stg, Cloud Run to Cloud SQL is about 1 ms, and the statement count is listed separately.
Clone sizes: verification_items 21.6k, verification_review_events 105k, obligation_instances 87k, vaccination_completions 6.1k, goats 1.7k.

## Cross-cutting finding: JIT is on (`jit=on`, `jit_above_cost=100000`)
Every statement with a large estimated cost pays **0.8–1.0 s of LLVM JIT emission** before it runs. The planner overestimates rows because of CTE scans (for example, 3,317 rows estimated against 119 actual). On the clone this alone turns `/action-center/obligations` and `/workflows/{row_id}` into FAILs, and it adds about 120 ms to `/verification/video-log`.
- **Fix:** `ALTER DATABASE goatos SET jit = off;` or `RuntimeParams["jit"]="off"` in the pgx pool config (`internal/platform/...` pool setup). OLTP reads here never gain from JIT.
- **Check on stg:** run `SHOW jit;` on goatos-stg Cloud SQL. If it is on, this is probably a large share of the stg p95.

## Table

| Endpoint | Handler → repo | Stmts/req (N+1?) | Tables / index | Dominant EXPLAIN (server ms, rows scanned→returned) | Est DB ms | Grade | Fix |
|---|---|---|---|---|---|---|---|
| **GET /verification/oversight-analytics** | verification/adapters/http/review_events_handler.go:GetOversightAnalytics → postgres/oversight_analytics.go:OversightAnalytics | **8 sequential** (no N+1), cached only 2 s (`verificationReadCacheTTL`), cleared on every write | verification_items: pending_scope_idx (stmts 1 and 5), verified_day_idx (2, 6b, 7); **seq scan** for stmts 3 and 4 (30-day window = 11.6k of 21.6k rows); 5b "arrived" **seq scan** (21.6k→5.8k; the `window_start` CTE hides the bound from the planner); stmt 8 **seq scan of verification_review_events 105k** hash-joined to 4.4k decided items | 1) 13.6 (3,333 correlated sampling subplans) 2) 3.3 3) 32 (sort 11.6k for percentile) 4) 12 5) 10 (again 3,333 subplans) 5b) 19–25 6) 12–17 7) 39–65 (105k rows read → 1 row) | **≈145–160 server**; on stg 8 round trips + Cloud SQL CPU ⇒ observed p50 500 / p95 1049 | **OK** (server). Stg wall time is MAX/FAIL | (1) **Merge 8 statements into 3.** Pending (1 + 5) as one `GROUP BY module` with totals computed in Go. Decided-30d (2 + 3 + 4) as one pass: `SELECT module, count(*) FILTER (WHERE verified_at>=now()-'7d'), count(DISTINCT day) FILTER(...7d), percentile_cont(...), count(*) FILTER(status='approved'), ... FROM verification_items WHERE tenant_id=$1 AND verified_at>=now()-'30 days' AND auto_resolution IS NULL GROUP BY module` (measured 41 ms against 48 ms for three statements). Verifier activity (6 + 7) sharing one `decided` CTE. Or send them all as one `pgx.Batch` (1 round trip). (2) **Cover the 30-day scan:** `CREATE INDEX CONCURRENTLY verification_items_decided_cover_idx ON verification_items (tenant_id, verified_at) INCLUDE (module, status, captured_at, verified_by) WHERE verified_at IS NOT NULL AND auto_resolution IS NULL;` (index-only, avoids the 11.6k heap reads). (3) In 5b, inline the bound: `vi.captured_at >= (((now() AT TIME ZONE 'Asia/Kolkata')::date - 13)::timestamp AT TIME ZONE 'Asia/Kolkata')` so the planner uses `video_log_day_idx`. (4) Stmt 7: move the window onto the events side, `AND e.occurred_at >= now() - interval '44 days'` (review cannot precede capture by much), so `verification_review_events_item_actor_time_idx` / `actor_time_idx` drives the read. Better still, store `played / watched_pct` on verification_items when the verdict is written. (5) Replace the per-row sampling subplan with the policy-range join below. (6) **Raise the cache TTL to 60 s** (epoch invalidation is already in place). This is a leadership KPI strip, and a 2 s TTL means nearly every hit misses. It is also per-instance, so consider a shared cache or a materialized snapshot refreshed every minute. |
| GET /verification/queue (verifier, pending backlog) | http/handler.go:ListQueue→listQueue → app/service.go:ListQueue → postgres/repository.go:ListQueue ‖ ListQueueFilterOptions; + review_events.go:WatchStates | 1 page (in parallel with) 4 option stmts (sequential: parks, sheds, **counts**, missed) + 1 watch = 6 | page: sampling_closeout_idx, ~1–2 ms, 52→51; parks 19 ms; sheds 36 ms (3.2k subplans); **counts: seq scan of all 21.6k items + 21.4k correlated sampling subplans = 121 ms**; missed 0.3; watch 4 ms (item_actor_time_idx) | counts 121 | **≈180** | **OK** | **Sampling-predicate rewrite** (`samplingsql.InSample`): replace the correlated `(SELECT sample_percent ... LIMIT 1)` per row with a join on a policy-range CTE: `WITH pol AS (SELECT category, sample_percent, effective_business_date f, lead(effective_business_date) OVER (PARTITION BY category ORDER BY effective_business_date) t FROM verification_sampling_policies WHERE tenant_id=$1) ... LEFT JOIN pol ON pol.category=vi.category AND local_date>=pol.f AND (pol.t IS NULL OR local_date<pol.t) WHERE vi.sampling_bucket < COALESCE(pol.sample_percent,100)`. Measured: counts 121 → **32 ms** with identical semantics. The counts aggregate also grows with all history (approved never leaves), so bound approved/rejected counts to the date filter or serve them from a cached per-status counter. Options are already cached for 2 s; raise to 30–60 s. |
| GET /verification/action-queue (CEO/Director, verification.act) | handler.go:ListActionQueue → same as queue + postgres/repository.go:ListReadyVaccinationBatchClosures | 7 (queue 6 + closures 1) | closures: 2.2k-line CTE; vaccination_completions seq 6.1k, Memoize ×6,098 over sop_submission_items + source_submission_idx | closures 66 ms | **≈250** | **OK** | Same sampling rewrite. Closures: add `vc.status IN ('recorded','accepted')` plus a `batch_id` index path. `CREATE INDEX CONCURRENTLY vaccination_completions_tenant_batch_status_idx ON vaccination_completions (tenant_id, batch_id, status);` Cache the result for 30 s (it only changes on verdict or close). |
| GET /verification/items/{id}/review-facts | handler → app/review_events.go:ItemReviewFacts → GetItem + ReviewEventRepository.ItemReviewFacts | 2 | pk; verification_review_events_item_actor_time_idx | <1 ms each | ~1 | PASS | — |
| GET /verification/video-log | app/video_log.go:VideoLog → postgres/video_log.go:VideoLogShedSummary + VideoLogShedRows | 2 | video_log_day_idx (432 items/day), proof_artifacts_pkey ×838, workforce_members LATERAL ×838 | summary 24; rows **149 with JIT / 32 without** | 55 (no JIT) / 175 (JIT) | PASS (OK with JIT) | Turn JIT off. Optional: resolve operator names with one hash join instead of a LATERAL per row. |
| GET /verification/sampling | app/sampling.go:SamplingOverview → ListSamplingPolicies + ListSamplingDayStats | 2 | policies pk; video_log_day_idx | 0.25 + 0.9 | ~2 | PASS | — |
| **GET /action-center/obligations** (served by processintegrity; obligation's `ListDue` registration is shadowed) | processintegrity/adapters/http/handler.go:ActionCenter → app/service.go:ActionCenter → postgres/repository.go:ListRows→listRowsCanonical (rows ‖ counts) | 2 in parallel (same canonical CTE computed twice) + evidence-media lookup | vaccination_completions seq 6.1k, vaccination_drive_assignment_members seq 6.2k ×3, obligation_batches, goats seq 1.7k, sop_tasks; ~500 JIT functions | **1,142 + 1,110 ms with JIT (≈1.0 s of that is JIT emission)**; 160 + 170 ms without JIT; 82 rows out | **1,150 (JIT) / 170 (no JIT)** | **FAIL** (OK once JIT is off) | (1) JIT off. (2) Compute the canonical CTE once: `SELECT ..., count(*) OVER (PARTITION BY work_state)` or a single `MATERIALIZED` CTE feeding both the page and `GROUP BY work_state` (halves DB CPU). (3) Push `due_at BETWEEN $asOf-? AND $dueBefore` into the `completions` / `due_window_*` base CTEs so the 6.1k vaccination_completions and 3×6.2k drive-member scans are bounded. Indexes: `CREATE INDEX CONCURRENTLY vaccination_drive_assignment_members_tenant_assignment_idx ON vaccination_drive_assignment_members (tenant_id, assignment_id) INCLUDE (goat_id);` and `CREATE INDEX CONCURRENTLY vaccination_completions_tenant_obligation_idx ON vaccination_completions (tenant_id, obligation_id, administered_at DESC);` (4) The read cache already exists; make sure the TTL is ≥ 30 s. |
| **GET /workflows/{row_id}** | processintegrity handler.go:WorkflowDrilldown → repository.go:GetRow → ListRows with `RowID` + `IncludeCompleted=true` | 1 | Same canonical CTE over **all history** (IncludeCompleted); `row_id = $12` applied only at the end (repository.go:1626); **temp spills** (`temp read=622 written=2023` on several nodes) | **1,492 ms with JIT / 586 ms without** → 1 row | **1,500 / 590** | **FAIL** | row_id encodes `batch:<id>:rule:<id>:...:date:<d>` or `obligation:<id>`. **Parse it in GetRow and push `batch_id = $x` (or `obligation_id = $x`) plus the date into the base CTEs**, so only one batch's obligations are materialized (expected < 20 ms). JIT off. Spills: `SET LOCAL work_mem='16MB'` for this read, or the push-down alone removes them. |
| GET /operations/audit | operationsaudit/adapters/http/handler.go:List → repository.go:List | 1 | audit_log (tiny) | 0.15 | <1 | PASS | — |
| GET /operations/audit/summary | handler.go:Summary → repository.go:Summary | ~1 (harness panicked on nil From/To in my synthetic query; handler always sets them) | audit_log (tiny) | — | <5 | PASS (small table) | — |
| GET /operations/dlq, /operations/kernel-health | outbox/adapters/http/handler.go:List / Health → repository.go:ListDeadLetters / Health | 1 / 1 | outbox_messages (tiny) | 0.08 / 0.10 | <1 | PASS | — |
| GET /goats/{goat_id} | identity handler.go:GetGoatPassport → app GetGoatPassport → repo GetGoatByID (+1 per merge hop) | 2 | goats pk, goat_identifiers | 0.5 + 0.1 | ~4 | PASS | — |
| GET /goats/{goat_id}/timeline | identity handler.go:GetGoatTimeline → timeline_corrections_read.go:GetGoatTimeline (sqlc ListGoatTimeline) | 1 | goat timeline idx | 0.07 | <1 | PASS | — |
| GET /goats/{goat_id}/passport | passport/adapters/http/handler.go:GetPassport → passport/app GetPassport → vaccination ListCompletionsByGoat + obligation ListOpenByGoat + GetLastAcceptedForGoat | 3 sequential | vaccination_completions goat idx, obligation_instances goat idx | 0.55 / 0.66 / 0.07 | ~5 | PASS | Optional: run the three in parallel (errgroup) to save 2 round trips. |
| **GET /identifiers/{type}/{value}/resolve** | identity handler.go:ResolveIdentifier → repository.go:FindIdentifierMatches | 1 | — | **SQL error on every call:** `missing FROM-clause entry for table "gsp"`. `goatSummaryColumns()` references `gsp.*` (repository.go:470–472), but this query (repository.go:~302–335) has no `LEFT JOIN goat_shed_partitions gsp` | n/a | **BROKEN (500)** | Add `LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id` after the `cohort` join (same join as repository.go:416). |
| GET /herd-register/summary | counts handler.go:GetSummary → HerdRegisterService.GetSummary → repo GetHerdRegisterSummary | 1 | goats seq 1.7k (whole-herd aggregate, expected) | 5.4 | ~6 | PASS | At 50k+ goats: `CREATE INDEX ... goats (tenant_id, lifecycle_status) INCLUDE (park_id, breed, sex, stage)` for index-only. |
| GET /health/analytics | health/adapters/http/analytics_handler.go:GetHealthAnalytics → postgres/analytics.go:GetHealthAnalytics | 1 on clone (health tables empty, so it returns early) | locations, shed_partitions | 0.1 | <1 on clone | PASS (unproven: no health data on the clone) | Re-audit once health_treatment_sessions and health_diagnosis_runs have volume. |
| GET /health-config/medicines, /health-config/registers(/{id}), /health-config/protocols(/{id}) | health config_handler.go / register_config_handler.go → medicine_catalog.go:ListMedicines, register_authoring.go:ListRegisters/GetRegister, protocol_authoring.go:ListProtocolCatalog/GetProtocolDetail | 1 / 1 / 1 / 1 / 5 (BEGIN, header, steps, history, ROLLBACK) | inventory_items 34, registers 0, health_protocol_versions 56 | <1 each | <5 | PASS | Protocol detail: drop the read-only transaction wrapper (−2 round trips). |
| GET /app/pc-care/worklist | pccare/adapters/http/handler.go:GetWorklist → postgres/tasks.go:ListTasks (`taskFromJoins`) | 2 | `animal_pens` subquery hash-joins **goat_identifiers (3.2k) on `lower(btrim(identifier_value))`, seq ×83 tasks**; 93 rows built → 26 returned | 222 exec + 15 plan | ~240 | **OK (grows as tasks × herd, heading for FAIL)** | Replace the join with `CROSS JOIN LATERAL (SELECT g2.tenant_id, g2.goat_id FROM goat_identifiers g2 WHERE g2.tenant_id=an.tenant_id AND g2.status='active' AND lower(btrim(g2.identifier_value))=lower(btrim(an.scanned_identifier)) LIMIT 1) gi` (measured 37 ms). Paginate before building the animal subqueries. Long term, store the resolved goat_id on pc_care_task_animals, or `CREATE INDEX CONCURRENTLY goat_identifiers_active_norm_idx ON goat_identifiers (tenant_id, lower(btrim(identifier_value))) INCLUDE (goat_id) WHERE status='active';` |
| GET /app/pc-care/rounds/{round_id} | http/rounds.go:GetRound → postgres/rounds.go:GetRound | 2–3 | same `animal_pens` subquery (goat_identifiers seq ×10) | 30 | ~32 | PASS (same growth problem) | Same rewrite. |
| GET /app/pc-care/rounds, /planner/catalog, /planner/parks/{id}/sheds | rounds.go:ListRoundCards, tasks.go:PlannerCatalog/PlannerParkSheds | 3 / 3 / 1–2 | small tables | — | <10 | PASS | — |
| GET /app/pen-visits (+/{task_id}) | penvisits repository.go:ListMine / GetTask | 2 / 1 | pen_visit_tasks | 2.0 | ~3 | PASS | — |
| GET /app/health/work-items(+/{id}), /app/health/observations(+/{id}), /app/health/death-causes | health repository.go:ListWorkItems (4 sequential) / GetWorkItem / diagnosis_repository.go:ListDiagnosisRuns, GetDiagnosisRun; death causes are static | 4 / 2 / 2 / 2 / 0 | health_sessions_worklist_idx (tables empty on clone) | — | ~5 | PASS (tables empty) | Optional: fold summary, markers and filter options into one batch. |
| GET /app/clock/status, /app/clock/presence(+/{id}), /app/leave/requests, /app/leave/approvals, /app/roster/my-coverage, /app/roster/timetable, /app/me | workforce clock_service.go / leave_service.go / roster_service.go / service.go:AppMe | 4 / 4 / 2 / 2 / 2 / 2 / 3 / 2 | workforce_* (under 500 rows), proper indexes | <1 | <5 | PASS | — |
| GET /app/config, /app/sop-versions/{id} | appconfig Compile (0 stmts) / sop GetVersionByID | 0 / 1 | sop_versions pk | <1 | ~1 | PASS | — |
| GET /app/workflows, /app/workflows/subject, /app/workflows/{id} | tasks/adapters/http/handler.go:ListWorkflows / GetWorkflowBySubject / GetWorkflow | 3 (+3 colostrum) / 3 / 2 | workflow_instances_* (42 rows) | <1 | ~4 | PASS | — |
| GET /app/market/survey, /market/config, /market/analytics, /market/reporters | market/app/service.go | 4 / 3 / 1 / 1 | market_price_entries (90 rows) | <1 | ~3 | PASS | — |
| GET /protocols, /protocols/animal-stages, /protocols/versions/{id}; GET /toxin/review | protocol repository.go; toxin repository.go:ListTasks | 1 / 1 / 2; 2 | small | <1 | ~2 | PASS | — |
| GET /ceo-ai/conversations, /{id}/messages, /starters, /admin/trace/{id} | ceoai/persistence/conversation_store.go, observability/store.go | 1 / 2 / 0 / 1 | ceo_ai_* idx | 0.3–0.4 | <1 | PASS | — |
| Writes: POST /verification/items/{id}/verdict, close, submissions/close, review-events, PUT sampling | verification app + repository | verdict ≈ 5–8 (advisory lock + item update + outbox + applier), review-events 1–2 | pk / unique idx | — | <20 | PASS | Each verdict bumps `cacheEpoch`, clearing the oversight and options caches (see the TTL notes above). |
| Writes: POST /admin/goats/{goat_id}/* (exit, critical-death-exit, health, identifiers, retire, identity, move, reproductive, stage) | identity admin_* / goat_lifecycle.go / goat_relocate.go | 4–10 each (idempotency + lock + update + event + outbox) | goats pk, goat_identifiers unique | — | <30 | PASS | — |
| Writes: POST /operations/dlq/replay, /discard; /ceo-ai/*; /market/*; /protocols/*; /health-config/* publish/discard/drafts; /app/clock/in/out; /app/leave/*; /app/health/*; /app/pc-care/rounds(+close); /app/pen-visits/{id}/submit; /app/workflows/*/actions/* ; /app/market/survey/{city} | various | 3–8 each; loops: `workflowMutation` inserts actions in a loop (tasks/postgres/repository.go:955); roster `checkMinOperatorCoverage` runs one query per operator (roster_service.go:1297 → roster_repository.go:1658) | idempotency_keys (47k) unique idx | — | <20 | PASS | Roster per-operator leave check: replace with one `operator_id = ANY($ids)` query. |

## Priority fix list
1. **JIT off** (DB- or pool-level). Removes about 1 s from action-center and workflows, and about 120 ms from video-log. Confirm with `SHOW jit` on stg.
2. **/workflows/{row_id}**: push the parsed row_id filter (batch_id / obligation_id + date) into the base CTEs instead of filtering after the full-history build (590 ms without JIT, FAIL).
3. **/identifiers/{type}/{value}/resolve is broken**: missing `goat_shed_partitions gsp` join in `FindIdentifierMatches`.
4. **/verification/oversight-analytics**: 8 → 3 statements (or one `pgx.Batch`), cache TTL 2 s → 60 s, covering index `verification_items_decided_cover_idx`, bound the review_events scan, inline the 5b window bound.
5. **Sampling predicate rewrite** (`samplingsql.InSample`) as a policy-range join: queue counts 121 → 32 ms, and it helps oversight stmts 1 and 5 and the parks/sheds options.
6. **/action-center/obligations**: compute the canonical CTE once for rows and counts, bound the base CTEs by the due window, add the two vaccination indexes.
7. **/app/pc-care/worklist**: LATERAL `LIMIT 1` identifier lookup (222 → 37 ms).
