# Goat OS Current Whole-Project Remediation Ledger

Reviewed repository: `vgoats/goatos`

Reviewed snapshot: `97e2b462cc4cc39e78b3c99209a09f739c3d2e31`

Review date: 2026-08-09

This is the current implementation queue for the whole-project audit. The
[refresh audit](whole-project-audit-c8b475f6-refresh.md) and its
[baseline](whole-project-audit-d98a8a6.md) remain immutable evidence records;
this file revalidates their stable IDs on fresh `origin/main`, records current
closures and new findings, and turns the result into ordered fix instructions.

This is a source review, not proof of the deployed Cloud Run revision, live
database schema, DLQ contents, bucket policy, device delivery, or historical
production data. Verify those surfaces in the closure packet for each affected
batch.

## Current tally

- Stable historical namespace: 130 IDs, of which `FEED-001` and `FEED-002`
  were already closed in the refresh audit.
- Source-fixed but closure-pending on this snapshot: `AUTH-006`, `FEED-019`.
- Newly discovered: `KERN-011`, `VERIFY-001`, `FEED-036`, `FEED-037`,
  `MOB-010`.
- Current namespace: **135 IDs** — 130 historical plus 5 new.
- Current open ledger: **133 findings (`1 P0`, `82 P1`, `50 P2`)**.
- Closed historical rows: **2 P1** — `FEED-001`, `FEED-002`.
- Source-fixed, closure-pending P1 rows: `AUTH-006`, `FEED-019`. They remain
  in the open total until their missing owning-layer regressions and proof
  packets land.
- Narrowed but still open: `KERN-001`, `FEED-005`, `FEED-007`, `MOB-002`.

Feature-lane reconciliation:

| Lane | Open | Closed | Current note |
|---|---:|---:|---|
| Mobile/offline (`MOB`) | 10 (`1 P0`, `4 P1`, `5 P2`) | 0 | `MOB-002` narrowed |
| Security (`SEC`) | 3 P1 | 0 | all open |
| Authorization (`AUTH`) | 7 (`6 P1`, `1 P2`) | 0 | `AUTH-006` source-fixed, closure-pending |
| Identity (`ID`) | 1 P1 | 0 | open |
| Proof (`PROOF`) | 1 P1 | 0 | open |
| Data/analytics (`DATA`) | 3 (`2 P1`, `1 P2`) | 0 | all open |
| Admin/investor web (`WEB`) | 9 (`3 P1`, `6 P2`) | 0 | all open |
| CEO AI (`CEO`) | 5 (`2 P1`, `3 P2`) | 0 | all open |
| Operations (`OPS`) | 2 (`1 P1`, `1 P2`) | 0 | all open |
| Release (`REL`) | 11 (`8 P1`, `3 P2`) | 0 | all open |
| CI (`CI`) | 9 (`2 P1`, `7 P2`) | 0 | all open |
| Shifting (`SHIFT`) | 3 P1 | 0 | all open |
| Vaccination (`VAX`) | 8 (`7 P1`, `1 P2`) | 0 | all open |
| Weighing (`WEIGH`) | 6 (`4 P1`, `2 P2`) | 0 | isolated repair lane |
| Procurement (`PROC`) | 5 P1 | 0 | all open |
| Shared kernel (`KERN`) | 11 (`9 P1`, `2 P2`) | 0 | `KERN-001` narrowed; `KERN-011` new |
| Feed (`FEED`) | 35 (`20 P1`, `15 P2`) | 2 P1 | `FEED-005/007` narrowed; `FEED-019` closure-pending |
| Herd/health (`HERD`) | 3 P2 | 0 | all open |
| Verification/Milk (`VERIFY`) | 1 P1 | 0 | new |

This matrix reconciles to the same 133 open and 2 closed rows and makes every
reviewed feature family explicit.

Severity means:

- `P0`: stop-ship isolation or catastrophic integrity risk.
- `P1`: serious security, correctness, data-loss, or outage risk.
- `P2`: important reliability, performance, release-control, UX, or
  accessibility defect.

## How to use this ledger

1. Work a root batch, not a convenient symptom. Do not mark an individual row
   closed while another layer can recreate the same failure.
2. Preserve stable IDs in commits and proof packets.
3. Apply the current closure gate below: production writer and reader proof,
   retry/replay/idempotency, authorization, real PostgreSQL where applicable,
   contract/client proof, observability, relevant ordinary CI, and independent
   counter-review.
4. Do not edit applied migrations. Use the next free forward migration at the
   time of implementation and re-check the migration tail immediately before
   naming it.
5. A static fix is not a deployed fix. Record the exact deployed revision,
   schema version, repair outcome, and remaining DLQ/backlog after rollout.

The older `consolidated-ledger-defect-closure-program.md` belongs to the
separate last-35 namespace. Its proof ideas may be reused, but its C35 ordering,
whole-old-ledger requirements, and historical push instructions do not govern
this queue. Land current work through the repository's `make land-main` gate.

### Current closure gate

A row closes only when one current-SHA proof packet contains every applicable
axis below:

- root cause and affected readers/writers, with duplicate symptoms merged;
- canonical transaction, audit/history, outbox, and failure visibility;
- retry, duplicate, replay, stale-version, concurrency, lease, and recovery;
- cross-tenant, cross-park, cross-grant, and ownership denial;
- populated real-PostgreSQL migration and production-path proof;
- bounded query plan and pagination/memory proof where row count can grow;
- OpenAPI/generated-client/web/Android/Room/offline proof where applicable;
- metrics, alerts, DLQ/repair, and deployed-revision/schema verification;
- ordinary affected-component CI plus the relevant guard self-tests;
- an independent counter-review that reconciles every count and status.

A compile, mock, screenshot, skipped workflow, prose assertion, successful
enqueue, or source-only audit is not closure proof by itself.

## Fix order

### N0 — repair notification writes and recover lost contact

**Blocks:** notification correctness and every new escalation activation.

**IDs:** `KERN-001`, `KERN-011`.

Authoring N0 may begin immediately, but its migration must not deploy until the
minimum relevant F0 gate is green: correct no-transaction runner behavior,
constraint/`DO` validator coverage, lock-timeout and restart tests, populated
upgrade proof, clean exact-SHA landing, and schema verification. Those checks
may ship atomically with N0 if an emergency path is required.

The database `notification_requests_type_check` omits two values already
written by production paths:

- `obligation_missed` from
  `backend/internal/notificationbridge/obligation_missed_notify.go`;
- literal `verification_withdrawn` from
  `backend/internal/notificationbridge/verification_notify_consumer.go`.

`KERN-001` is narrowed, not closed: a miss with at least one resolved recipient
fails the insert; an unclaimed module or zero-recipient miss silently queues
nothing. `KERN-011` is the equivalent withdrawn-verification failure when a
verifier device resolves.

Implementation instructions:

1. Create a forward-only `-- +goose NO TRANSACTION` migration using the next
   free version (`000141` at the audited SHA). Include exactly one Up and one
   Down marker. Make Down explicitly refuse with a clear irreversible-migration
   error; a successful no-op Down would let migration metadata lie while the
   expanded constraint remains.
2. Never drop the active constraint before its replacement exists. Add a
   restart-safe `_v2` superset constraint as `NOT VALID`, validate it, drop the
   old constraint, then rename `_v2`. Guards must assert the exact expected
   definition and `convalidated`, not mere name existence, and handle
   old+unvalidated-v2, old+validated-v2, and already-expanded canonical states.
   Never drop the old guard unless v2 is proven correct and validated. Set
   bounded lock/statement timeouts and `RESET lock_timeout` and
   `RESET statement_timeout` before exit.
3. Add **both** `obligation_missed` and `verification_withdrawn`. Preserve every
   existing allowed value.
4. Move notification types into one canonical registry/constants package.
   Replace raw producer literals. Add a regression guard that enumerates every
   actual queue producer, not merely identifiers named `NotificationType*`, and
   compares the complete registry with `pg_get_constraintdef`.
5. Extend migration validation and sqlc schema mirrors; run `make sqlc-check`.
6. Add real-PostgreSQL production-path tests for operator and leadership missed
   notifications, withdrawn-verifier notification, lock contention, migration
   interruption/rerun, and an upgrade from the current committed schema.
7. Recover obligation misses by expected branch/device idempotency key, not “any request exists
   for this obligation.” Reconcile operator and leadership branches separately;
   persist `queued`, `no_recipient`, `unclaimed_module`, or `failed` per branch.
   Replay retained DLQ events, then run a bounded keyset database repair for
   older/partial events. Verify zero unresolved repair rows after deployment.
8. Recover withdrawn-verification failures separately: replay retained failed
   `verification.item.closed` events, then make and record an explicit policy
   choice for older rows—repair only still-actionable current recipients, or no
   historical push because a stale withdrawal notice could mislead. Never send
   historical withdrawals blindly.

Closure requires the source proof above plus live schema verification, exact
deployed revision, DLQ/database repair counts, and confirmation that a new
miss/withdrawal produces the expected named-person requests.

### F0 — make the release and migration lane trustworthy

**Blocks:** further hot-table changes and task-kernel schema rollout.

**IDs:** `REL-001..011`, `CI-001..009`, `OPS-001`.

Implementation instructions:

1. Reconcile or disable the active staging deploy workflow so it matches the
   deploy contract.
2. Require fresh `origin/main`, exact `HEAD == origin/main`, no tracked or
   untracked dirt, and an exact-SHA green receipt. Build an explicit archive,
   not the ambient directory.
3. Pin base images by digest. Publish and verify full-SHA immutable image
   identities and deployed digests.
4. Fetch and peel remote tags before treating an existing release tag as valid.
   Make release-contract tests hermetic and structurally verify the image check.
5. Repair the hot-migration validator: stable Up/Down parsing, current hot-table
   inventory, constraint DDL, DML inside `DO $$...$$`, immutable-baseline debt,
   and adversarial fixtures for migrations 136–140.
6. Route migrations and fixtures to the correct checks. Require disposable real
   PostgreSQL Up execution and safe/refusing Down behavior where applicable.
7. Repair the schema introduced by migration 137 using the next free forward
   migration and an expand/contract path compatible with old and new binaries.
   Never edit applied `000137` or reproduce its destructive Down behavior.
8. Make certifying web builds use clean `npm ci` under the pinned Node version;
   fix the known Android/operator/workspace test defects before making their
   aggregate gates mandatory.
9. Make `/readyz` fail closed when the applied-schema version cannot be read.
10. For existing constraint/index hazards in `REL-001/002`, use forward-only,
    object-appropriate repairs: add-before-drop CHECK constraints with separate
    validation, `CREATE INDEX CONCURRENTLY`, bounded lock/statement timeouts,
    restart-state assertions, and no edits to applied files.
11. Expand ordinary Android CI beyond `:app`; route fixture changes to their
    real consumers; enforce clean Node/npm installs; repair missing
    `feature-auth` test dependencies and stale permissions/calendar assertions;
    supply a reproducible prod Firebase configuration contract; give
    operator-mobile its own TypeScript plus `nav_chrome`; isolate the investor
    React/Next workspace and prove a clean aggregate build.

Proof must cover dirty/untracked rejection, tag mismatch, exact SHA/digest,
validator self-tests including nested procedural DML, populated upgrade,
lock-timeout/restart behavior, old/new binary overlap, and normal PR routing.

### S0 — isolation, authorization, proof, and durable data

**Blocks:** exposing generic task or proof commands across modules.

**IDs:** `MOB-001`, `SEC-001..003`, `AUTH-001..007`, `ID-001`,
`PROOF-001`, `DATA-001..002`, `WEB-001`.

Implementation instructions:

1. Fix `MOB-001` before further offline mutation work: cancel and await every
   old WorkManager chain, advance a persisted session generation, reject all
   writes from an old generation, transactionally wipe Room/outbox, then clear
   identity. Prove logout/login A-to-B while an A worker is paused cannot
   resurrect any A row, token, cache, or request.
2. Quarantine the investor surface from deployment until every route has real
   authentication and tenant/farm authorization. Parameterize all BigQuery
   filters and upgrade both Next.js lines to supported patched versions.
3. Introduce one capability-bound scope object containing the permission-bearing
   grant and its allowed parks. Stop combining unrelated role permissions with
   an arbitrary first park.
4. Check stored tenant/park/subject scope transactionally in health, shifting,
   counts, feed filters, proof get/download/finalize, and all ID-based commands.
   Add vertical ownership checks for generic tier roles.
5. Gate direct critical-death exit behind the approved Counts request and both
   required proofs in one transaction.
6. Enforce proof size, MIME, digest, object generation, tenant quota, and replay
   server-side. Replace proof hard delete with tombstone plus durable,
   version-aware media deletion/retry.
7. Filter and group GA4 rollups by tenant before writing tenant-labelled facts.
8. Keep actor/duty-specific verifier navigation out of a shared bootstrap cache,
   or include actor and duty revision in its key.
9. For source-fixed `AUTH-006`, add the missing direct owning-layer regression
   proving an allowed park succeeds and a stored out-of-scope task is denied;
   complete its current-SHA closure packet before changing status.

Proof requires role × grant × park × route tests, cross-tenant/cross-park real
PostgreSQL tests, a Park-A/Park-B E2E, direct-death denial, hostile proof-upload
cases, two-tenant analytics fixtures, and two-verifier cache isolation.

### D0 — correct module source truth before adding adapters

**Blocks:** activating those modules in the generic task tier.

**IDs:** `SHIFT-001..003`, `VAX-002..009`, `PROC-001..005`,
`HERD-001..003`, `KERN-002..010`, `VERIFY-001`.

Work can be parallelized by owning module, but the adapter for a module must not
land before its source behavior is corrected.

1. Fix Shifting destination/park authority and relocation atomicity. Model the
   temporary-placement episode, expected return/checkpoint, clinical
   extension/defer, and ICU/quarantine entry/exit gates required by
   `SHIFT-001/002`; then repair the projected-count correctness root.
2. Fix Vaccination assignment/scope/rescope, roster fallback, bounded schedules,
   lifecycle/date vocabulary, and alert pagination before using it as the first
   generic-task vertical.
3. Fix Procurement membership/proof/upsert/cancel/intake-loop integrity.
4. Fix Herd birth under a lock that proves the mother is alive and unexited;
   snapshot operational location/partition; and perform exact idempotent replay
   detection before mutable-state validation.
5. Harden shared kernel primitives: verifier enqueue in the canonical
   transaction, loud task-routing failures, durable apply receipts, correct
   stale-attempt accounting, bounded stage deadlines, fair set-based outbox
   claiming, lease fencing tokens, and bounded SOP reads.
6. Fix `VERIFY-001`: register profiles keyed by producer modules
   `milk_preparation` and `milk_feeding`; set each profile's `dutyModule` to
   `milk`; keep each verification category's `NavigationModule` as `milk`; seed
   and repair the ratified `milk/verify` duty; route pending, rework, approved,
   closed, and withdrawn events. Do not invent a leadership owner if no
   canonical seat is recorded. Because unknown modules were acknowledged as
   no-ops, add a bounded, idempotent keyset repair for still-actionable Milk
   verification items and record `queued`, `no_route`, `no_recipient`, or
   `failed` for every expected named-person branch. Explicitly decide whether
   historical approved/closed notices are replayed.

Required Milk proof: each producer creates a pending item, the correct Milk
verifier can list and decide it, a non-duty actor gets 403, seed closeout proves
the duty exists, every expected named-person notification branch is queued or
has an explicit no-route/no-recipient outcome, and no unknown-module event is
silently dropped.

### W0 — repair Weighing inside its protected boundary

**IDs:** `WEIGH-001..006`.

Fix these findings in Weighing-owned schema, services, and tests. Do not make
Weighing read `task_nodes`, `sop_*`, `obligation_*`, roster ownership, animal
lifecycle, generic cadence, or overdue/missed concepts. A later leadership
oversight view may consume Weighing outbox events outward only, after an explicit
scope ruling and any required guard amendment. Generic work state must never
gate weigh capture or close.

### M0 — repair Feed and mobile offline behavior by root

**IDs:** `FEED-003..037`, `MOB-002..010`.

Fix in this order:

1. Define one database/Go partition token and use it in completion uniqueness,
   idempotency, issue fingerprints, audit, and events. Use an object-appropriate,
   restart-safe expand/contract migration: a dual-compatible v2 key, duplicate
   preflight, `CREATE UNIQUE INDEX CONCURRENTLY`, then a short lock-bounded
   swap/drop only after old-binary compatibility is proven. PostgreSQL unique
   constraints cannot use the CHECK-constraint `NOT VALID` recipe.
2. Bind completion to immutable issued work: issue ID and revision/fingerprint,
   exact park/shed/pen/session/date/workflow lookup, stored expected ration, and
   fail-closed verification. Reject imaginary and stale work.
   Add typed per-feed-item actual quantity, planned snapshot, derived
   shortage/variance, persistence/API/Android capture, and verifier/audit/event
   output; test underfill, overfill, and replay.
3. Validate proof role/media/subject; generate a fresh submit attempt after
   rework; cancel offline proof only with compare-and-set; migrate or quarantine
   legacy completion/draft keys and the retired endpoint.
4. Overlay durable outbox state on task lists. Never auto-dismiss Transport on
   `QUEUED`; show “saved on phone / waiting to sync,” allow explicit Back, and
   auto-dismiss only on `SUCCEEDED`. A later 403/dead-letter must remain visible
   and recoverable on the task/list/sync surface after the capture ViewModel is
   gone (`MOB-010`). Test terminal failure both before and after leaving.
5. Carry packing pen identity and transport labels through repository, service,
   verification, and HTTP output. Derive verifier pages and Alerts from the
   complete registry, not page one or a coarse hardcoded module list.
   Source authorized park choices before Packing/Transport requires `park_id`;
   prove a two-park actor can start blank, select either allowed park, and cannot
   name a third (`FEED-022`).
6. Enforce catalog membership, row versions, explicit restore, serialized first
   writes, finite/precision-safe decimals, canonical errors, and complete batch
   affected-ID audit in Feed configuration.
7. Add missing first-row controls and pagination. Distinguish load/failure/empty,
   honor location availability, announce outcomes, and repair filter logic:
   `FEED-036` is the always-filtered empty copy caused by truthy `[]`;
   `FEED-037` is the NUL-versus-space multi-select comparison.
8. Bound active-outbox reads without decoding every payload, share one Room
   migration/version source, enforce Retrofit/OpenAPI parity, localize messages,
   and quarantine corrupt cached pages.

Required proof includes cross-pen/idempotency and stale-issued-work real
PostgreSQL tests, Android upgrade/replay/race tests, a 10k-outbox stress case,
runtime contract parity, and mounted UI tests for `FEED-019`, `FEED-029`,
`FEED-032`, `FEED-036`, and `FEED-037`. `FEED-019` remains source-fixed but open
until the mounted regression and current closure packet land.

### A0 — remaining P1 application defects

**IDs:** `WEB-002..003`, `CEO-001..002`.

This lane can proceed beside kernel design, but a shared surface must be fixed
before its kernel consumer ships.

1. Fail the admin root closed when its control-tower contract is absent.
2. Replace the Procurement source-entry N+1 detail fanout with an enriched list
   or bounded batch endpoint and assert request count.
3. Make CEO cache content-only while always persisting and auditing a distinct
   actor/conversation turn.
4. Replace the in-memory UTC CEO budget with atomic durable India-day
   reservation/reconciliation and prove multi-replica, restart, and IST-midnight
   behavior.

### R0 — remaining P2 cleanup

**IDs:** `DATA-003`, `WEB-004..009`, `CEO-003..005`, `OPS-002` plus the P2
rows already included in F0, W0, and M0.

This lane does not block kernel design, but any touched shared surface must be
fixed before its kernel consumer ships.

1. Pass India business date through remaining stock reads and test the
   midnight boundary.
2. Paginate/fail closed operator availability, add stable modal idempotency,
   return global audit facets/counts, allow refresh-cookie recovery, and bound
   investor warehouse queries.
3. Add CEO cursor pagination and a scheduled, monitored rollup; implement or
   remove advertised conversation detail.
4. Define proof coverage from required operations and linked artifacts, not
   audit-event naming.
5. Remove the admin Weighing surface or record and guard an explicit
   supersession of current scope.

## Current ID map

Every open stable ID is present below. “Narrowed” means its original root still
requires a fix; it is included in the open total.

| Severity | Open IDs |
|---|---|
| P0 | `MOB-001` |
| P1 | `SEC-001..003`; `AUTH-001..006`; `ID-001`; `PROOF-001`; `DATA-001..002`; `WEB-001..003`; `CEO-001..002`; `OPS-001`; `REL-001..006`, `REL-010..011`; `CI-001..002`; `SHIFT-001..003`; `VAX-002..008`; `WEIGH-001..004`; `PROC-001..005`; `KERN-001..007`, `KERN-010..011`; `FEED-003..006`, `FEED-008..023`; `MOB-002..003`, `MOB-008`, `MOB-010`; `VERIFY-001` |
| P2 | `AUTH-007`; `DATA-003`; `WEB-004..009`; `CEO-003..005`; `OPS-002`; `REL-007..009`; `CI-003..009`; `VAX-009`; `WEIGH-005..006`; `KERN-008..009`; `HERD-001..003`; `FEED-007`, `FEED-024..037`; `MOB-004..007`, `MOB-009` |
| Closed P1 | `FEED-001`; `FEED-002` |

The ranges above are inclusive. They reconcile mechanically to 133 open
(`1 + 82 + 50`) and 2 closed.

## Current closure notes

- `AUTH-006` (source-fixed, closure-pending): Transport submission now derives allowed parks from the
  capability-bearing grants and checks the stored task park before proof/write.
  It remains open until a direct owning-layer forbidden-branch regression and
  the current closure packet land.
- `FEED-001`: completion natural keys now include pen identity.
- `FEED-002`: experiment authoring carries partition identity.
- `FEED-019` (source-fixed, closure-pending): the common Feed configuration form unmounts after confirmed
  success, so uncontrolled values cannot leak into the next pen. Add the mounted
  component regression and current closure packet before closing it.

Current narrowed scopes:

- `FEED-005`: date/pen capture identity is fixed; rework resubmission still
  reuses the prior successful submit idempotency key.
- `FEED-007`: Distribution is fixed and packing shed name can resolve through
  `ShedID`; only new packing `partition_label` is still dropped because the
  PostgreSQL result is empty and the service forwards it.
- `MOB-002`: shared generic capture cancellation is fixed; unconditional deletes
  remain in Packing, Distribution, and Transport re-record paths.
- `KERN-001`: recipient-bearing missed notifications fail the database check;
  zero-recipient and unclaimed-module misses silently queue nothing.

## Review coverage and boundary

The source recheck began from all tracked files at the audited SHA: 3,886 files,
including 1,378 Go files, 553 Kotlin/KTS files, 597 TypeScript/JavaScript files,
147 SQL files, 120 PostgreSQL migration files, and 1,108 test-path files. Every
stable ledger ID was rechecked against its production reader/writer and relevant
tests; recent commits were reviewed separately for changed behavior. This
compact implementation queue intentionally does not repeat every older
file-and-line evidence block; the linked baseline/refresh preserve those audit
records, and every implementation must refresh its selected anchors before
claiming closure.

Targeted backend Feed/Verification/Workforce tests passed. Relevant admin Feed
tests passed; one unrelated aggregate test lacked its React dependency. The
full Android Gradle suite, physical device, cloud deployment, live database, and
production repair were not run as part of this source-only ledger refresh.

Known red controls at this snapshot:

- the hot-migration validator reports 9 hard violations;
- `feature-auth` unit tests do not compile because test dependencies are absent;
- `core-permissions` has 3 failing tests and `feature-calendar` has 1;
- operator-mobile typecheck fails `MODULE_NOT_FOUND` because it hardcodes
  admin-web's TypeScript installation;
- fresh production npm audits report admin 5 advisories (4 high) and investor
  16 advisories (10 high).
