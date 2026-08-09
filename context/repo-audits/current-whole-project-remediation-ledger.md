# Goat OS Current Whole-Project Remediation Ledger

Reviewed repository: `vgoats/goatos`

Reviewed snapshot: `97e2b462cc4cc39e78b3c99209a09f739c3d2e31`

Fresh-main counter-review snapshot:
`4ec92c99e75fe83d27b50b4fcabad73be6cc1d6f`

Final counter-review correction snapshot:
`07640ad5388ffb6ed87701ea7f4f522c92fb3e82`

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

- Stable historical namespace: 130 IDs: 94 adjudicated baseline rows after
  refuted `VAX-001` was removed, plus 36 refresh rows. `FEED-001` and
  `FEED-002` were already closed in the refresh audit. `VAX-001` is intentionally
  absent, not an uncounted open or closed row.
- Source-fixed but closure-pending on this snapshot: `FEED-019`.
- Newly discovered: `KERN-011`, `VERIFY-001`, `FEED-036`, `FEED-037`,
  `MOB-010`, `KERN-012`, `SHIFT-004`.
- Current namespace: **137 IDs** — 130 historical plus 7 new.
- Current open ledger: **135 findings (`1 P0`, `83 P1`, `51 P2`)**.
- Closed historical rows: **2 P1** — `FEED-001`, `FEED-002`.
- Source-fixed, closure-pending P1 row: `FEED-019`. It remains in the open total
  until its missing owning-layer regression and proof packet land.
- Narrowed but still open: `AUTH-006`, `KERN-001`, `FEED-005`, `FEED-007`,
  `MOB-002`.

Feature-lane reconciliation:

| Lane | Open | Closed | Current note |
|---|---:|---:|---|
| Mobile/offline (`MOB`) | 10 (`1 P0`, `4 P1`, `5 P2`) | 0 | `MOB-002` narrowed |
| Security (`SEC`) | 3 P1 | 0 | all open |
| Authorization (`AUTH`) | 7 (`6 P1`, `1 P2`) | 0 | `AUTH-006` narrowed/open |
| Identity (`ID`) | 1 P1 | 0 | open |
| Proof (`PROOF`) | 1 P1 | 0 | open |
| Data/analytics (`DATA`) | 3 (`2 P1`, `1 P2`) | 0 | all open |
| Admin/investor web (`WEB`) | 9 (`3 P1`, `6 P2`) | 0 | all open |
| CEO AI (`CEO`) | 5 (`2 P1`, `3 P2`) | 0 | all open |
| Operations (`OPS`) | 2 (`1 P1`, `1 P2`) | 0 | all open |
| Release (`REL`) | 11 (`8 P1`, `3 P2`) | 0 | all open |
| CI (`CI`) | 9 (`2 P1`, `7 P2`) | 0 | all open |
| Shifting (`SHIFT`) | 4 (`3 P1`, `1 P2`) | 0 | `SHIFT-004` new |
| Vaccination (`VAX`) | 8 (`7 P1`, `1 P2`) | 0 | all open |
| Weighing (`WEIGH`) | 6 (`4 P1`, `2 P2`) | 0 | isolated repair lane |
| Procurement (`PROC`) | 5 P1 | 0 | all open |
| Shared kernel (`KERN`) | 12 (`10 P1`, `2 P2`) | 0 | `KERN-001` narrowed; `KERN-011/012` new |
| Feed (`FEED`) | 35 (`20 P1`, `15 P2`) | 2 P1 | `FEED-005/007` narrowed; `FEED-019` closure-pending |
| Herd/health (`HERD`) | 3 P2 | 0 | all open |
| Verification/Milk (`VERIFY`) | 1 P1 | 0 | new |

This matrix reconciles to the same 135 open and 2 closed rows and makes every
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

**IDs:** `KERN-001`, `KERN-011`, `KERN-012`.

Authoring N0 may begin immediately, but its migration must not deploy until the
minimum relevant F0 gate is green: correct no-transaction runner behavior,
constraint/`DO` validator coverage, lock-timeout and restart tests, populated
upgrade proof, clean exact-SHA landing, and schema verification. Those checks
may ship atomically with N0 if an emergency path is required.

The notification constraint rejects two values already written by production
paths, while the escalation-role constraint rejects roles the Calendar writer is
prepared to select for future non-Vaccination obligation events:

- `obligation_missed` from
  `backend/internal/notificationbridge/obligation_missed_notify.go`;
- literal `verification_withdrawn` from
  `backend/internal/notificationbridge/verification_notify_consumer.go`.
- `obligation_escalations_role_check` omits `growth_director`, `feed_director`,
  and `health_director`, although Calendar selects them for Weighing, Feed, and
  Counts/Shifting L3 escalation.

`KERN-001` is narrowed, not closed: a miss with at least one resolved recipient
fails the insert; an unclaimed module or zero-recipient miss silently queues
nothing. `KERN-011` is the equivalent withdrawn-verification failure when a
verifier device resolves. `KERN-012` is latent while Vaccination is the only
obligation producer, but it blocks safe multi-module escalation activation.

Implementation instructions:

1. Create a forward-only `-- +goose NO TRANSACTION` migration using the next
   free version (`000141` at the audited SHA). Include exactly one Up and one
   Down marker. Make Down explicitly refuse with a clear irreversible-migration
   error; a successful no-op Down would let migration metadata lie while the
   expanded constraint remains.
2. Never drop an active constraint before its own replacement exists. Implement
   separate restart-safe state machines for
   `notification_requests_type_check_v2` and
   `obligation_escalations_role_check_v2`: add each superset as `NOT VALID`,
   validate it, drop only its corresponding old constraint, then rename it.
   Guards must assert the exact expected definition and `convalidated`, not mere
   name existence, and handle old+unvalidated-v2, old+validated-v2, and
   already-expanded canonical states independently. Never drop an old guard
   unless its v2 is proven correct and validated. Set bounded lock/statement
   timeouts and `RESET lock_timeout` and `RESET statement_timeout` before exit.
3. Add **both** `obligation_missed` and `verification_withdrawn`, and expand the
   escalation-role constraint for the three emitted director roles. Preserve
   every existing allowed value.
4. Move notification types and escalation roles into canonical registries.
   Replace raw producer literals. Add regression guards that enumerate every
   actual queue/role producer, not merely identifiers named `NotificationType*`,
   and compare the complete registries with `pg_get_constraintdef`.
5. Extend migration validation and regenerate all six configured sqlc schema
   mirrors (Identity, Protocol, Obligation, Inventory, Vaccination, Feed); run
   `make sqlc-check`. Keep
   `vaccination_reminder_cadence_fires_type_check` as an intentional cadence-only
   registry: require exact equality to that cadence registry and separately
   prove the cadence registry is a subset of the global notification-type
   registry. Do not blindly widen it.
6. Add real-PostgreSQL production-path tests for operator and leadership missed
   notifications, withdrawn-verifier notification, lock contention, migration
   interruption/rerun, and an upgrade from the current committed schema.
7. Recover obligation misses by expected branch/person/route idempotency key,
   not “any request exists for this obligation.” Use the same actionability
   fence for recovery and new events: lock and re-read canonical status, source
   version, successor, and rescope state, then insert every notification request
   and persist every operator/leadership branch outcome in that same database
   transaction. Completed, canceled, waived, superseded, or otherwise
   no-longer-actionable work records `since_resolved`/`superseded` and sends
   nothing. Reconcile the current actionable named people and current routes
   separately for operator and leadership, recording resolution basis/time;
   device churn makes an exact historical device set unknowable. Persist
   `queued`, `no_recipient`, `unclaimed_module`, `failed`, or the
   no-longer-actionable outcome per branch. Replay retained DLQ events, then run
   a bounded keyset database repair for older/partial events. Verify zero
   unresolved repair rows after deployment.
8. Recover withdrawn-verification failures separately: replay retained failed
   `verification.item.closed` events, then make and record an explicit policy
   choice for older rows—repair only still-actionable current recipients, or no
   historical push because a stale withdrawal notice could mislead. Never send
   historical withdrawals blindly.
9. Persist the same branch outcomes for new events, not only recovery. Give
   `no_recipient`, `unclaimed_module`, and `failed` an alert owner and bounded
   reconciler; a warning log is not durable delivery evidence. Immediately
   before external delivery, fail closed by rechecking the canonical
   actionability/source-version fence or an atomically maintained equivalent
   suppression fence. A queued row is not permission to contact someone after
   the work completed, canceled, waived, superseded, or rescoped.

The restart-safe migration must also handle `canonical absent + validated v2`
by renaming v2, and must fail closed on `canonical absent + unvalidated or
malformed v2`. Test interruption and rerun at every autocommit boundary,
including the interval after the old constraint is dropped and before v2 is
renamed. A no-op Down is allowed only for an explicitly classified irreversible
data repair; a schema rollback that leaves the expanded constraint in place must
refuse and have an execution test.

Closure requires the source proof above plus live schema verification, exact
deployed revision, DLQ/database repair counts, confirmation that a new
miss/withdrawal produces the expected named-person requests, and production-path
proof that L3 Weighing, Feed, Counts, and Shifting escalations insert using their
emitted director roles.

### F0 — make the release and migration lane trustworthy

**Blocks:** further hot-table changes and task-kernel schema rollout.

**IDs:** `REL-001..011`, `CI-001..009`, `OPS-001`.

Implementation instructions:

1. Reconcile or disable the active staging deploy workflow so it matches the
   deploy contract.
2. Preserve the already-built `make land-main` freshness, clean-tree, rebase,
   exact-SHA receipt, race recheck, and remote verification. Separately harden
   staging release: remove or tightly govern `GOATOS_ALLOW_DIRTY_RELEASE` and
   `GOATOS_BYPASS_LOCAL_CI`, require the full SHA and matching receipt, and build
   Docker/Cloud Deploy from an explicit clean archive rather than ambient `.`.
3. Pin base images by digest. Publish and verify full-SHA immutable image
   identities and deployed digests.
4. Fetch and peel remote tags before treating an existing release tag as valid.
   Make release-contract tests hermetic and structurally verify the image check.
5. Repair the hot-migration validator: parse `NO TRANSACTION` once per whole file
   with runner-identical semantics; reject same-transaction `ADD ... NOT VALID`
   plus `VALIDATE` on a populated hot table; inspect constraint DDL and DML inside
   `DO $$...$$`; and key accepted historical debt by stable migration version,
   rule, section, table/object, and operation rather than rendered messages.
   Add `verification_items`, `feed_direction_issue_rows`,
   `feed_config_write_log`, `feed_distribution_completions`, and
   `feed_packing_completions` to the proven hot-table inventory. Add adversarial
   marker-position, same-transaction, procedural-DML, and migrations 136–140
   fixtures.
6. Run the static hot-migration validator unconditionally for relevant
   backend/migration changes in ordinary `ci-local` and hosted CI, not only the
   staging PR gate. Require disposable real PostgreSQL Up execution and
   safe/refusing Down behavior as a separate pre-deploy proof.
7. F0 owns the one schema expand/contract repair for the identity introduced by
   migration 137, using the canonical token selected with M0 and remaining
   compatible with old and new binaries. M0 owns the associated writer, reader,
   idempotency, audit, event, and client repair; it must not create a second
   competing re-key. Never edit applied `000137` or reproduce its destructive
   Down behavior.
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

1. Fix `MOB-001` before further offline mutation work. Quiesce and await every
   old WorkManager chain, `UploadForegroundService` drain, enqueue-triggered
   app-scope drain, and connectivity/reconnect drain. Advance a persisted session
   generation before clearing data and reject every late write from an old
   generation. Goat data and outbox are separate Room databases, so do not claim
   a cross-database transaction: clear each transactionally with a crash-
   recoverable generation-first protocol, then clear identity and relaunch the
   new session. Prove logout/login A-to-B while each write vector is paused
   cannot resurrect any A row, token, cache, or request.
2. Quarantine the investor surface from deployment until every route has real
   authentication and tenant/farm authorization. Enforce that quarantine with a
   CI/deployment allowlist that refuses an investor image, service, credentials,
   or route until the gate is explicitly cleared. Parameterize all BigQuery
   filters and upgrade both Next.js lines to supported patched versions.
3. Finish adoption of the existing capability-aware `ParkScopeDecision`; migrate
   the remaining capability-blind production callers and guard-ban new blind
   authorization entry points. Keep the permission-bearing grant and allowed
   parks together; add grant provenance only if the selected command needs it.
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
9. `AUTH-006` remains narrowed/open: an empty `AuthorizedParkIDs` currently means
   unrestricted, and Transport checks scope before the repository transaction.
   Carry an explicit restricted/unrestricted decision into the locked write and
   recheck the stored task park under the same row lock. Add direct allowed,
   forbidden, and empty-scope regressions before changing status.
10. Close the generic SOP cross-park hole under `AUTH-001`: submissions,
    scan-captures, and scan-attempts must lock the polymorphically scoped task,
    resolve its stored `scope_type/scope_id` to the authoritative park set in the
    write transaction, and require the capability-bearing grant to cover that
    scope even when `assigned_to` is NULL. Fail closed on ambiguous/unresolvable
    scope; an explicitly tenant-wide task needs tenant-wide authority. Prove
    Park-A cannot act on an assigned or unassigned Park-B task.

Proof requires role × grant × park × route tests, cross-tenant/cross-park real
PostgreSQL tests, a Park-A/Park-B E2E, direct-death denial, hostile proof-upload
cases, two-tenant analytics fixtures, and two-verifier cache isolation.

### D0 — correct module source truth before adding adapters

**Blocks:** activating those modules in the generic task tier.

**IDs:** `SHIFT-001..004`, `VAX-002..009`, `PROC-001..005`,
`HERD-001..003`, `KERN-002..010`, `VERIFY-001`.

Work can be parallelized by owning module, but the adapter for a module must not
land before its source behavior is corrected.

1. Fix Shifting destination/park authority and relocation atomicity. Model the
   temporary-placement episode, expected return/checkpoint, clinical
   extension/defer, and ICU/quarantine entry/exit gates required by
   `SHIFT-001/002`; then repair the projected-count correctness root. Delete the
   unwired duplicate `counts/adapters/verificationbridge` Shifting enqueuer and
   make the live `countsbridge` adapter the single guarded owner (`SHIFT-004`).
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
   Extend the hand-maintained producer-coverage guard with both Milk producer
   modules. Keep predeclared Health categories classified as reserved/non-
   producing and block the first Health producer until its profile, coverage,
   route, and production-path proof land.

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
| P1 | `SEC-001..003`; `AUTH-001..006`; `ID-001`; `PROOF-001`; `DATA-001..002`; `WEB-001..003`; `CEO-001..002`; `OPS-001`; `REL-001..006`, `REL-010..011`; `CI-001..002`; `SHIFT-001..003`; `VAX-002..008`; `WEIGH-001..004`; `PROC-001..005`; `KERN-001..007`, `KERN-010..012`; `FEED-003..006`, `FEED-008..023`; `MOB-002..003`, `MOB-008`, `MOB-010`; `VERIFY-001` |
| P2 | `AUTH-007`; `DATA-003`; `WEB-004..009`; `CEO-003..005`; `OPS-002`; `REL-007..009`; `CI-003..009`; `SHIFT-004`; `VAX-009`; `WEIGH-005..006`; `KERN-008..009`; `HERD-001..003`; `FEED-007`, `FEED-024..037`; `MOB-004..007`, `MOB-009` |
| Closed P1 | `FEED-001`; `FEED-002` |

The ranges above are inclusive. They reconcile mechanically to 135 open
(`1 + 83 + 51`) and 2 closed.

## Current closure notes

- `AUTH-006` (narrowed/open): Transport submission derives allowed parks from
  capability-bearing grants, but empty scope still means unrestricted and the
  park check occurs before the locked repository write. Move the decision and
  recheck into the transaction before claiming source-fixed.
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
- `KERN-012`: Calendar emits three module-director roles that the committed
  `obligation_escalations` CHECK rejects; the defect is latent until a
  non-Vaccination obligation reaches L3.
- `SHIFT-004`: the live Shifting verification enqueuer is wired through
  `countsbridge`; a second, behaviorally stale adapter under
  `counts/adapters/verificationbridge` is dead and must be removed.

Pinned fresh-finding evidence:

- `KERN-012`: the
  [committed role constraint](https://github.com/vgoats/goatos/blob/4ec92c99e75fe83d27b50b4fcabad73be6cc1d6f/backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql#L14702-L14716)
  omits roles used by the
  [Calendar escalation insert](https://github.com/vgoats/goatos/blob/4ec92c99e75fe83d27b50b4fcabad73be6cc1d6f/backend/internal/calendar/adapters/postgres/repository.go#L1353-L1368)
  and
  [module-director mapping](https://github.com/vgoats/goatos/blob/4ec92c99e75fe83d27b50b4fcabad73be6cc1d6f/backend/internal/calendar/adapters/postgres/repository.go#L1544-L1575).
- `SHIFT-004`: the
  [dead duplicate adapter](https://github.com/vgoats/goatos/blob/4ec92c99e75fe83d27b50b4fcabad73be6cc1d6f/backend/internal/counts/adapters/verificationbridge/shifting_enqueue.go#L11-L42)
  differs from the
  [live countsbridge adapter](https://github.com/vgoats/goatos/blob/4ec92c99e75fe83d27b50b4fcabad73be6cc1d6f/backend/internal/countsbridge/shifting_verification_enqueue.go#L18-L55),
  which is the one
  [wired in production bootstrap](https://github.com/vgoats/goatos/blob/4ec92c99e75fe83d27b50b4fcabad73be6cc1d6f/backend/internal/bootstrap/api.go#L635-L636).

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

The 68-item external improvement review, produced from
`b492146461ff1d5c4994a20f339daf69ce273630`, was then independently
counter-checked against fresh `origin/main` at
`4ec92c99e75fe83d27b50b4fcabad73be6cc1d6f`, with the final disputed claims
rechecked at `07640ad5388ffb6ed87701ea7f4f522c92fb3e82`. The affected
notification, roster, task, release, security, and module paths were re-read on
those fresh SHAs. Valid corrections are merged above and into the companion
kernel plan; duplicate descriptions were merged into one owning batch, partial
claims were narrowed, and unsupported preferences were not promoted to
findings. The final claim-level scorecard is 48 accepted, 15 partially
accepted/narrowed, and 5 rejected. This update changes documentation only; it
is not product-code or deployed-state closure proof.

Known red controls at this snapshot:

- the hot-migration validator reports 9 hard violations;
- `feature-auth` unit tests do not compile because test dependencies are absent;
- `core-permissions` has 3 failing tests and `feature-calendar` has 1;
- operator-mobile typecheck fails `MODULE_NOT_FOUND` because it hardcodes
  admin-web's TypeScript installation;
- fresh production npm audits report admin 5 advisories (4 high) and investor
  16 advisories (10 high).
