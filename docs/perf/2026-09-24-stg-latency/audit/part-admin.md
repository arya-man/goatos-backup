# DB latency audit: /admin/* and /admin-web/* GETs (excluding /admin/locations list and /admin-web/bootstrap)

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
