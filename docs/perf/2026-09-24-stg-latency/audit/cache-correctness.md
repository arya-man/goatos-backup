# PR 389 cache correctness audit (read-your-writes / stale data)

Date: 24/09/2026. Branch `perf/stg-burst-and-login`, diffed against `origin/main`.
Mandate: no cache may show wrong or stale data after a write; nothing may touch live data.
Method: static trace of every cache's SQL -> base tables -> every writer in the repo, plus a
two-instance DB-backed probe on a throwaway OCI Postgres container (created and removed in this
audit; stg and the shared OCI goatos DB were not touched).

## 1. Cache inventory

| Cache | Where | Key | TTL / stale window | Eviction triggers |
|---|---|---|---|---|
| `analytics` readcache (one process-wide instance) | `backend/internal/platform/readcache/cache.go`, wired `bootstrap/api.go:654` | tenant + sorted parks + `weighing:`/`growthdirector:` + normalized params | coherent: 60 s fresh **+ 5 min stale-while-revalidate**; listener down: 30 s, no stale serving; 512-entry LRU | `commitAndEvict` (weighing, NOTIFY in tx + local evict), `PutAssumptions` (NOTIFY + local evict), `commitIssueAndEvict` (feed issue: NOTIFY only), listener reconnect => EvictAll; per-tenant generation drops pre-write loads |
| - weighing reads on it | `weighing/adapters/postgres/growth.go:218`, `shed_weights.go:77`, `weighing_dates.go:105`, `weight_demographics.go:357` | as above | as above | as above |
| - Growth Director reads on it | `growthdirector/adapters/postgres/fcr.go:417`, `growth_director.go:136`, `feed_weight_band.go:429`, `assumptions.go:247` (growth settings) | as above | as above | as above |
| herd-signals live cohort / pen-median / summary | `herdsignals/app/live_cohort_cache.go` | tenant + filter parts | 12 s fresh; after NOTIFY-invalidation or TTL, **stale served up to 60 s** to plain GETs (stream reads use requireFresh) | `herd_signals_live` NOTIFY from ingest (`repository.go:264`) and mapping bind/unmap/replace (`mapping.go:390,518,634`); reconnect => all |
| feeddirection feed-analytics cache (pre-existing, unchanged) | `feeddirection/adapters/postgres/analytics.go:23` | per-repo map | 30 s | local-only epoch bump on every feeddirection commit; no cross-instance eviction |
| vaccination read caches | none in the PR diff | - | - | not touched by PR 389 |
| admin-web `shortReadCache` | `apps/admin-web/lib/api/server.ts:34` | endpoint + backend URL + tenant + sha256(bearer)[:16] + sorted query | 30 s per Node process | cleared before and after every non-GET sent through `timedBackendFetch` from **this process only** |
| admin-web `inFlightReadCache` (assumptions, sale prices) | `server.ts:38`, `:1458`, `:1478` | same | 0 s (only shares a pending request) | same |
| admin-web session sync dedupe | `lib/auth/session-sync-dedupe.ts` | uid / token fingerprint in sessionStorage | browser session | sign-out `forget`; not a data cache (only suppresses duplicate session events) |
| auth email allowlist (changed in PR) | `permissions/adapters/postgres/email_allowlist.go:75` | tenant | source TTL | none; on load error the last-known-good set is still honoured |
| Android | PR touches outbox/sync/boot only (`OutboxStore`, `SyncEngine`, Room v7); no new read cache | - | - | - |

## 2. Tables the analytics readcache depends on (traced from the SQL)

weighing reads: `weighing_campaigns`, `weighing_campaign_sheds` (incl. `status`, returned as `bucket_status` by shed weights), `weighing_observations`, `weighing_shed_observations`, `weighing_shed_load_tags`, `goats` (`lifecycle_status`, `shed_id`, sex/stage), `goat_identifiers`, `goat_shed_partitions`, `shed_partitions`, `locations`.
Growth Director adds: `feed_direction_issues`, `feed_direction_issue_rows`, `feed_purchases` (FCR price), `procurement_load_goats`, `growth_sale_price_assumptions`, `growth_assumptions`, `animal_stage_lookup`, `workforce_members` (set-by name), `pens` (view over locations/partitions).

## 3. Matrix: cache x table x writer -> invalidated?

Y = NOTIFY in tx + local evict. Y* = NOTIFY only (writer instance converges via its own listener, ~50 ms). N = no invalidation -> stale up to **360 s** (60 s fresh + 300 s SWR) while the listener is healthy, 30 s when it is down.

| Table | Writer | analytics readcache | admin-web 30 s |
|---|---|---|---|
| weighing_* (observations, shed obs, campaigns, campaign sheds) | weighing repo: submit/lump/resubmit/withdraw/supersede (`repository.go:263..4179`), close/reopen (`close.go:191..448`), verdict (`verification_verdict.go:80,151`), weight correction (`weight_correction.go:172,250`), rework digest (`rework_digest.go:256,320`) — also the Android sync path, which calls the same repo | Y | TTL 30 s (other admin-web instances; own instance Y) |
| weighing_campaign_sheds.status | kernel carry-over close (`weighing/adapters/postgres/kernel.go:267`, worker process, `commitAndInvalidateReadCache` publishes nothing) | **N** (shed-weights `bucket_status`) | TTL 30 s |
| weighing_* | `cmd/seed-weighing-fixture` | N (dev seed only) | - |
| feed_direction_issues / _rows | issue/amend/lock (`feeddirection/adapters/postgres/issues_repository.go:75,113,221,269`) | **Y\*** (`read_cache_evict.go:17`, no local evict) | TTL 30 s |
| growth_assumptions, growth_sale_price_assumptions | `PutAssumptions` (`growthdirector/adapters/postgres/assumptions.go:432-438`) | Y | in-flight only (fresh) |
| goats, goat_shed_partitions | goat relocate (`identity/adapters/postgres/goat_relocate.go:61`), lifecycle/exit (`goat_lifecycle.go:1157`), admin create (`admin_goat_create.go:320`), census correction (`census_correction.go:144`), shed stage (`shed_stage.go:216`), configuration animals store (`configuration/adapters/postgres/stores_animals.go`) | **N** | TTL 30 s |
| goat_shed_partitions | partition move (`obligation/adapters/postgres/partition_move.go:53,79`) | **N** | TTL 30 s |
| goats, goat_identifiers, goat_shed_partitions, procurement_load_goats | procurement load receive (`procurement/adapters/postgres/repository.go:310,330`, commits `:201..1694`) | **N** | TTL 30 s |
| goat_identifiers | herd-signals tag mapping (`herdsignals/adapters/postgres/mapping.go:187,214,267`) | **N** (notifies herd_signals_live only) | TTL 30 s |
| feed_purchases | purchase edit/import API (`procurement/adapters/postgres/feed_purchase_repository.go:287,457`), `cmd/import-feed-purchases/main.go:162` | **N** (FCR cost) | TTL 30 s |
| locations, shed_partitions | `locations/adapters/postgres/write.go:72..473`, `configuration/adapters/postgres/stores_places.go`, `tasks/adapters/postgres/newborn_placement.go` | **N** | TTL 30 s |
| animal_stage_lookup | `configuration/adapters/postgres/stores_animals.go` | **N** (weighed stages in assumptions) | - |
| workforce_members | workforce/permissions repos | N (display name only; low) | - |
| any | migrations/backfills (applied at deploy; instances restart -> caches empty) | n/a | n/a |
| SQL triggers | none write into the tables above (checked migrations with `CREATE TRIGGER`) | n/a | n/a |
| herd_signal_* live tables | ingest (`repository.go:264`), mapping (`mapping.go:390,518,634`) | herd-signals cache: invalidated, but **stale served up to 60 s** to non-stream GETs (`live_cohort_cache.go:85`) | TTL 30 s not applied (herd-signals reads not in shortReadCache) |

## 4. Read-your-writes probe (two instances, throwaway DB)

Throwaway `postgres:16.9-alpine` container on the OCI box (own name/port, removed after). Two
`readcache.Cache` instances, each with its own `readcache.Listener`, one shared pool; a scratch
table stood in for the analytics input, and the writes used the exact commit shapes the
production writers use. Temporary test file was deleted, not committed.

| Case | Write shape (production writers) | Writer instance | Sibling instance | Result |
|---|---|---|---|---|
| T1 | NotifyTx + commit + local Evict (weighing, assumptions) | immediate | 23 ms | PASS |
| T2 | NotifyTx + commit, no local Evict (feed issue/amend/lock) | **first read after the write returned the pre-write value**; converged in 55 ms | 21 ms | FAIL (read-your-writes) |
| T3 | plain commit, no NOTIFY (identity, partition move, procurement, feed purchases, locations, kernel close) | still stale after 2 s | still stale after 2 s | FAIL (stale up to 360 s by config) |
| T4 | tenant-wide NotifyTx (verdict, correction) | - | 43 ms; also cleared other parks | PASS |
| T5 | load started before a write, finished after | pre-write value returned only to the waiter already queued; not stored (DroppedStores=1); next read post-write | - | PASS |

Not run: the full HTTP E2E against a locally started API x2 with seeded realistic weighing data
per page action (weighing submit, verdict, close/reopen, park move, correction, feed
issue/amend/lock, purchase import, relocate/lifecycle/create, partition move, procurement load,
assumptions, notification mark-read, herd-signals ingest). The per-writer outcome for those is
fixed by which commit shape the writer uses (table above); T1-T5 cover every shape. Notification
mark-read has no cache in the PR. A builder should add that HTTP E2E as the regression gate for
the fixes below.

## 5. Admin-web

- Own write: `timedBackendFetch` clears both caches before the non-GET and again after the response, before the server action returns, so the redirect/refresh on the same Node instance re-reads. A read racing the write cannot be re-inserted (the `finally` only deletes its own entry). PASS.
- Gap: on a network error or throw after the backend committed, the post-write clear is skipped (`server.ts:809` catch path). The pre-write clear still ran, so exposure is limited to a read that started during the write. Low.
- Cross-instance: with more than one admin-web instance, the user's next request can land on a sibling that still holds the 30 s entry: stale up to 30 s, stacked on the backend's up-to-360 s for N writers. The comment at `server.ts:30-33` ("backend's own weighing analytics cache is already 2 minutes") is inaccurate: it is 60 s + 5 min SWR.
- Leakage: key includes tenant, backend URL and a sha256 fingerprint of the bearer token, so there is no cross-user or cross-tenant sharing. Failures are not cached. PASS.

## 6. Business-data mutations added by the PR

Every INSERT/UPDATE/DELETE/DDL the PR adds is in the `analytics.*` schema:
`analytics.rollup_day_watermark` upsert, `analytics.rollup_dispatch` claim upsert,
`analytics.app_events_archive` insert/update (`verified_at`, `deleted_at`), migrations 000400/000401
(rollup_run constraint, autovacuum reloptions; down-migration drops only the new analytics tables).
No business table gains a writer.

Archive delete (`backend/cmd/analytics-rollup/retention.go:85-91`): deletes only
`analytics.app_events`, only rows at or before the exported `(received_at, event_id)` key, only
after the object is uploaded and verified (size + MD5, or sha256 + row count for an existing
object), the ledger row is written with `verified_at`, and `count(covered) <= archived` is
rechecked. No bucket configured = no delete. Residual risk (low): the covered-count check and
the batched DELETE are not one transaction, so a backfilled row with an explicit `received_at`
inside the archived key range, inserted between the check and the delete, would be deleted
unarchived. Normal ingest (`received_at = now()`) cannot hit this.

## 7. Stale-risk items (N) and recommended fixes

1. Identity writers (`goat_relocate.go:61`, `goat_lifecycle.go:1157`, `admin_goat_create.go:320`, `census_correction.go:144`, `shed_stage.go:216`), partition move (`partition_move.go:53,79`), procurement load (`procurement/.../repository.go:310,330`) change goats / partitions / load goats read by Weights, weight demographics, FCR and feed-by-weight-band. Fix: call `readcache.NotifyTx(ctx, tx, tenantID, parkIDs...)` before commit in each (tenant-wide if the park is unknown), and a local `Evict` when the repo has the cache. Better: one `readcache.Invalidator` port injected into these repos, plus a guard test listing every writer of the dependency tables.
2. Feed purchases (`feed_purchase_repository.go:287,457`, `cmd/import-feed-purchases/main.go:162`) change FCR cost. Same fix. The cmd importer can NOTIFY in its tx (siblings listen).
3. Feed issue/amend/lock (`feeddirection/adapters/postgres/read_cache_evict.go:17`) never evicts locally: the probe showed the writer's own first read returning pre-write FCR, and with the listener down the entry is kept for up to 30 s. Fix: inject the shared `analyticsReadCache` into `feeddirectionpg.Repository` (`bootstrap/api.go:806`) and `Evict(tenant, park)` after commit.
4. Kernel carry-over close (`weighing/adapters/postgres/kernel.go:267`) sets `weighing_campaign_sheds.status='closed'` in the worker with no NOTIFY; shed weights shows `bucket_status`. The comment at `growth.go:76` ("change no analytics input") is wrong for this claim. Fix: NotifyTx in that `afterClaim` tx (NOTIFY reaches API listeners from the worker too).
5. Herd-signals tag mapping (`mapping.go:187-267`) writes `goat_identifiers`, which the analytics readcache reads. Fix: also `readcache.NotifyTx` there.
6. Locations / partitions / stage lookup config writers (`locations/.../write.go`, `configuration/.../stores_places.go`, `stores_animals.go`, `tasks/.../newborn_placement.go`): same NotifyTx (tenant-wide).
7. SWR amplifies every missed writer from 60 s to 360 s. Until items 1-6 have a guard test, either set `StaleGrace` to 0 or cap it at a few seconds (`readcache/cache.go` `DefaultOptions`).
8. Herd-signals live cache serves an invalidated entry up to 60 s to plain GETs (`live_cohort_cache.go:85`), so a mapping bind followed by a page reload can show the old mapping. Fix: treat `e.stale` (NOTIFY-invalidated) as a miss for non-stream reads; keep SWR only for TTL expiry.
9. Admin-web cross-instance 30 s (`apps/admin-web/lib/api/server.ts:34`): acceptable only if documented as bounded. Correct the comment at `:30-33`. Also clear on the thrown-fetch path (`:809`).
10. Email allowlist (`email_allowlist.go:79-81`): on a failed reload, an email revoked since the last good load is still allowed past the TTL. Fail closed for entries older than a hard cap.
