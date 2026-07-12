# Consolidated Ledger Defect Closure Program

This is the mandatory execution contract for closing findings in
`context/repo-audits/last-35-commits-consolidated-bug-ledger.md`. It applies to
backend, Postgres, contracts, admin-web, Android, infrastructure, CI/CD, tests,
and documentation. The ledger says **what is broken**; this file says **what
counts as fixed**.

## Small Prompt for a New Session

```text
Execute the consolidated ledger-closure program as coordinator. Parallelize only independent findings. For every fix, commit real-flow E2E, shared Claude/Codex anti-pattern rules, mechanical guards/self-tests, and required CI—nothing memory-only. Counter-review, update the one ledger, then push the fully gated result with git mesha-push main. Do not merge stg or deploy.
```

`AGENTS.md` routes that prompt to this file and the live ledger. Never copy the
ledger count into the prompt: reconstruct it from the checked-out file and
current HEAD because the count will change.

## Parallel Agent Execution Contract

Parallel work is allowed only when it reduces elapsed time without creating
competing truth or overlapping edits:

1. One coordinator owns the integration branch, assignment map, canonical
   ledger, status/count edits, integration, and final PR handoff.
2. Before spawning workers, the coordinator maps dependencies and affected
   files from current code. Parallelize only root findings with disjoint
   production paths and tests. Shared contracts, migrations, generated code,
   CI workflows, common fixtures, and ledger edits are serialized.
3. Give each worker explicit finding IDs, file ownership, an isolated
   branch/worktree, and the required proof axes. Reserve one available agent
   slot for the coordinator. If overlap appears, stop the conflicting worker
   and sequence the work.
4. Workers must not edit the canonical ledger/count, merge, push to `main` or
   `stg`, deploy, or absorb unrelated cleanup. They return a focused commit,
   failing-before evidence, the completed proof packet, and known limitations.
5. The coordinator counter-reviews the claimed root fix against current code,
   integrates it, reruns impacted proof on the integrated HEAD, and only then
   updates the ledger. A worker's green test or self-review is not closure.
6. Duplicate-root discoveries merge into one row. Cross-layer fixes that cannot
   be split safely stay with one owner even if they touch backend, contract,
   web, Android, SQL, or CI together.
7. End with one clean integration branch plus exact current-SHA proof. After
   every ledger row is fixed with proof and the integrated HEAD passes the full
   applicable gate, re-verify the Mesha/VGoats authority tuple, integrate onto
   latest `main`, and publish only with `git mesha-push main`. That alias must
   fail when `MESHA_GITHUB_PAT` is absent; never substitute a configured `gh`
   identity or plain `git push` from this multi-company workspace.
8. Pushing `main` advances the head of the existing `main -> stg` PR. Do not
   merge that PR or deploy to staging/production without a separate explicit
   instruction.

## 1. Start From Ground Truth

Before editing:

1. Read `AGENTS.md`, `context/README.md`, `SKILLS.md`, the ledger, this program,
   and the applicable build/review skill references.
2. Record repository root, branch, full HEAD SHA, base/range, worktree status,
   active ledger tally, and unrelated dirty files. Preserve unrelated work.
3. Query CRG and Graphify first as required by `AGENTS.md`; then verify every
   conclusion in current code, migrations, SQL, configuration, tests, and
   workflows. A stale graph is navigation only, never proof.
4. Reproduce or write a failing test for the selected root defect before fixing
   it. If the recorded evidence no longer holds, counter the row with code and
   update the ledger instead of implementing a fictional fix.
5. Work on one root defect, or one genuinely inseparable cluster, at a time.
   Priority order is P0, P1, P2, P3 unless a dependency or external block is
   recorded. Do not mix cleanup or broad refactors into the batch.

## 2. The Closure Law

A ledger row may change from `open` to `fixed with proof` only when all of the
following exist for the same HEAD SHA:

1. A failing reproduction that reaches the real faulty boundary.
2. A root-cause fix across every affected layer, not a timeout increase,
   oversized limit, permissive fallback, swallowed error, UI-only patch, or
   test-only rewrite.
3. A narrow regression test at the owning layer.
4. Cross-layer E2E proof through production routes and production persistence.
5. A mechanical static or architectural guard when the anti-pattern can recur.
6. Adversarial self-tests proving the guard fails on representative bad code
   and passes good code.
7. A required ordinary-PR CI job. Missing, skipped, cancelled, zero-job, or
   `continue-on-error` is failure, not green.
8. Performance, scale, memory, retry, security, offline, and observability proof
   applicable to the changed path.
9. A current-SHA proof packet with commands, results, artifacts, limitations,
   and no generated-report drift left unstaged accidentally.
10. Independent Claude/Codex counter-review against code. Disputes are settled
    by reproducible evidence, not agent authority.

Compilation, typecheck, screenshots, mocked repositories, seeded derived rows,
or one happy-path unit test alone never close a product defect.

## 2A. Guardrail Deliverable Required Per Fix

When a fix exposes a recurring failure pattern, closure includes preventing the
same class of defect from returning:

1. Add or strengthen the rule in the shared committed agent source:
   `.agents/skills/goatos-code-review/` for review lenses, or `AGENTS.md` for an
   always-on working agreement. `CLAUDE.md` and `CODEX.md` are thin shims to
   `AGENTS.md`; never create divergent Claude-only and Codex-only rule copies.
2. Add or extend a mechanical guard over the actual affected tree. Prefer a
   zero-tolerance check; when existing debt makes that impossible, use a
   current no-new-debt ratchet with an owner, exact scope, reason, and expiry.
3. Add adversarial guard self-tests proving representative bad code fails and
   good code passes. A grep script with no self-test is not a guardrail.
4. Wire the guard and the applicable regression/E2E test into required ordinary-
   PR CI. Manual, nightly, staging-only, skipped, cancelled, startup-failure,
   zero-job, or `continue-on-error` execution does not protect `main`.
5. E2E must traverse the real production-shaped flow: public API or app route,
   real service/worker, production repository and Postgres/Room persistence,
   generated contract/client, and the real web/Android consumer where affected.
   Fixtures may seed only external inputs; mocked owners or pre-seeded derived
   results cannot certify closure.
6. Record the agent-rule change, guard, self-test, CI job, real-flow E2E command,
   artifact, and current SHA in the finding's proof packet. If a new guard is
   genuinely inapplicable, state the concrete reason instead of omitting it.

## 2B. Committed Rule Durability Gate

Chat, agent memory, local-only hooks, generated graphs, attachments, and
uncommitted handoff notes are context, not durable Goat OS rules. Any reusable
instruction discovered while fixing the ledger must land in Git in the same
fix series:

- always-on Claude + Codex working rules -> `AGENTS.md`; `CLAUDE.md` and
  `CODEX.md` remain committed shims that both route to it;
- review lenses and anti-pattern checklists ->
  `.agents/skills/goatos-code-review/` and the applicable committed reference;
- build and architecture execution rules -> `.agents/skills/goatos-build/`,
  `context/`, or the authoritative ADR/TRD/runbook;
- mechanical enforcement and adversarial self-tests -> `tools/agent-hooks/`
  or the owning test package;
- required merge gates -> `.github/workflows/` plus the matching local command
  or Make target.

Before declaring a finding fixed, the coordinator must prove every new rule,
guard, self-test, and workflow is tracked (`git ls-files <path>`), present at
the integrated HEAD (`git show HEAD:<path>`), and included in the current-SHA
proof packet. If another fresh Claude or Codex session cannot discover and
enforce the rule from a clean clone, the rule does not exist and the finding
stays open.

## 3. Proof Packet Required Per Fix

Add a concise proof block to the ledger or linked execution artifact:

```text
Finding / root cluster:
Branch + full HEAD:
Root cause:
Changed production paths:
Failing reproduction before fix:
Layer tests:
Real Postgres / SQL evidence:
Contract + API evidence:
Frontend / Android E2E evidence:
Retry / idempotency fault matrix:
Pagination boundary matrix:
Performance / query-count / memory evidence:
Security / tenant / role / scope evidence:
Static guard + adversarial self-test:
Ordinary-PR required check and current-SHA artifact:
Known limits or external blocks:
Independent counter-review verdict:
Ledger status/count reconciliation:
```

Mark an inapplicable line `N/A` with a concrete reason. Blank proof is failure.

## 4. Backend and Real-Database Gate

Backend closure must use the production repository/service/handler path and a
real supported Postgres instance where SQL behavior matters.

- Exercise tenant, role, park/shed, date, status, cursor, and row-version scope.
- Run migrations from a representative prior version and on a populated DB;
  verify rollback/forward-recovery expectations and concurrent live writes.
- Use actual SQL and capture `EXPLAIN (ANALYZE, BUFFERS)` at realistic
  cardinality. Assert row counts, query counts, scanned rows, indexes, sort/temp
  spill, lock duration, transaction duration, and statement timeout behavior.
- Never accept an ORM/mock/in-memory fake as proof of Postgres constraints,
  NULL behavior, collation, keyset ordering, isolation, locking, or plans.
- Canonical state, audit, idempotency reservation, and outbox must commit in the
  required transaction. Projections are rebuildable read models, not truth.
- Broad dashboard reads must come from bounded projections/counters. No request
  may reconstruct million-row process state, make N+1 calls, or silently fall
  back to an unbounded canonical replay.
- Every worker has bounded batch size, bounded concurrency, deterministic
  continuation, lease/claim ownership, progress telemetry, and a matching hot
  index/plan gate.
- Never swallow DB errors, convert them to empty/success, or prune the last-known
  good projection before its replacement is serving.

Use the live budgets and commands in `docs/runbooks/performance-gates.md`.
Thresholds must be declared before measuring, not selected after seeing results.

## 5. Retry, Idempotency, and Failure Injection

For every write, worker, relay, consumer, upload, or sync path, test this matrix:

- failure before transaction; during transaction; after commit before response;
  response lost after success; process death and restart;
- concurrent identical requests and concurrent conflicting fingerprints;
- transient network/DB errors, 429, retryable 5xx, timeout, and cancellation;
- permanent validation/auth/conflict 4xx with no blind retry;
- exponential backoff with jitter, maximum attempts/age, WorkManager/worker
  constraints, and no retry storm or synchronized herd;
- stable idempotency key and request fingerprint across retries; database unique
  enforcement; identical replay result; explicit conflict for different input;
- side-effect commit followed by finalization failure; prove semantic
  idempotency, transactional inbox/outbox, or compensating recovery;
- poison message to DLQ, replay from DLQ, alerting, and no loss/duplicate action.

A test that only retries until green is not resilience proof. Blanket CI retries
that hide flaky behavior are forbidden.

## 6. Pagination Contract, End to End

Any potentially growing collection must use one bounded contract across SQL,
API, generated client, frontend/Android repository, local cache, and UI.

- Prefer keyset pagination. The order must be deterministic and include a
  unique final tie-breaker at the actual grouped/output grain.
- Cursor payload is versioned, opaque, scope-bound, validated, tamper-resistant
  as required, and rejected across tenant/filter/role changes.
- Test 0, 1, page-size, page-size+1, multiple pages, duplicate visible sort
  values, NULL values, deleted/inserted rows, equal timestamps, and last page.
- Assert no skipped or duplicated rows, stable backend order, bounded payload,
  correct `next_cursor`, and separate total/count semantics where required.
- Never fetch every page in SSR, download both tabs, set `limit=1000`, paginate
  an already-bulk-fetched array, use OFFSET on scale paths, or present page one
  as the full dataset.
- Android target is approximately 20-row matching keyset windows through Room
  and Paging 3/RemoteMediator (or an equally proven bounded design). Cursor,
  page/window identity, refresh/load-more, process death, offline continuation,
  and end-of-list state must all be persisted and tested.

For scan roster specifically, closure of C35-006/C35-018 must prove Android
reads the backend `next_cursor`, sends it on continuation, stores bounded pages
in Room, and renders every animal exactly once. FIXCHK-004 is evidence attached
to those root findings, not a separate count.

## 7. API and Contract Gate

- OpenAPI/schema is authoritative; generated clients and DTOs must be regenerated
  in the same change. No loose map may substitute for a typed contract test.
- Assert exact field names, required/optional/null semantics, status codes,
  error envelope, idempotency behavior, cursor, scope, and row/version tokens.
- Exercise malformed input, oversized payload, cancellation, timeout, slow
  client, partial media failure, authorization, and stale-write conflict.
- Bound request/response bytes and downstream calls. Record p50/p90/p95/p99 and
  error rate on the real HTTP path with the current SHA.
- Server permissiveness must not conceal a missing client workflow contract.

FIXCHK-003 closes only when the test requires non-empty `next_cursor`, rejects
stale camelCase-only output, and the generated/client contract agrees.

## 8. Admin-Web Gate

- SSR/server loaders call bounded backend endpoints. No direct DB access,
  `searchAll*`, fetch-all loops, client-side business pagination, overlapping
  duplicate fetches, or fanout detail calls.
- Backend contracts own labels, status, order, filters, options, permissions,
  and workflow truth. The browser does not recreate business rules.
- Prove loading, populated, empty, partial, error, denied, stale, conflict, and
  retry states with the real route. An error must never masquerade as empty.
- Record request count, payload size, TTFB, LCP, INP, CLS, JS bundle impact, and
  page p95/p99 against declared budgets. Test responsive and keyboard/screen
  reader behavior and visually compare mock-governed screens.
- No token/secret leakage to client bundles, HTML, logs, screenshots, or reports.
- E2E seeds external/canonical inputs only; production workers/projectors must
  create derived state. Hardcoded derived dashboard rows are false proof.

## 9. Android Architecture and Quality Gate

Every screen read follows this single-source-of-truth flow:

```text
network response -> transactionally upsert Room -> DAO Flow/PagingSource -> ViewModel StateFlow -> lifecycle-aware Compose UI
```

Room is the screen source of truth. Network-only repositories, response-blob
caches, direct continuation pages held only in ViewModel memory, and silent
network fallback are forbidden.

Mandatory checks:

- L0→L1→L2→L3 navigation carries stable task, tenant, park, shed, batch, SOP,
  row/version, and draft identity. Process death/deep links restore the same
  workflow; back navigation cannot submit the wrong work.
- Large lists use bounded Room pages/Paging 3, stable keys and content types.
  No eager full list, `SELECT *` unbounded observer, index identity, missing
  cursor, or client business re-sort/filter.
- UI exposes honest loading, cached/stale, refreshing, empty, error, denied,
  conflict, offline, queued, syncing, failed, and retry states. Sample/fake
  operational data is never shown as truth.
- Writes are offline-first where required: durable Room outbox, stable
  idempotency, WorkManager constraints/backoff/jitter, conflict truth, bounded
  retention, and exact form/proof payload. Network is not the local truth store.
- Decode/map/diff work runs off Main. Screen flows use lifecycle-aware
  collection and `WhileSubscribed`; no `GlobalScope`, forever screen
  collectors, leaked callbacks, hardware handles, Activity/View in ViewModel,
  or unbounded feed/cache/outbox.
- Run unit, Room migration, instrumentation, navigation, Compose, accessibility,
  offline/process-death, and account-switch tests.
- Run baseline profile and Macrobenchmark on the declared low-end profile;
  capture cold/warm start, frame timing/scroll, scan feedback, transition,
  submit latency, APK size, heap/GC, repeated shed cycles, battery, StrictMode,
  and leak assertion. Use budgets in `docs/mobile/performance-and-memory.md`.

### Logout Means Destructive Clean Slate

Logout must wipe every app-owned trace of the previous authority before another
principal can use the app:

- all Room databases/tables, paging keys, caches, drafts, outbox rows, and
  pending uploads;
- every DataStore/SharedPreferences/key-value entry, including app language,
  install ID, device ID, feature flags, and preferences; zero app-owned state is
  retained;
- WorkManager unique/periodic/retry jobs, alarms, callbacks, BLE/RFID/camera
  handles, notifications, and in-flight coroutines;
- app files, media/proof temp files, image/network caches, SavedState, navigation
  stack, singleton/app-scope state, bootstrap/role data, and in-memory tokens;
- Firebase/auth credentials and server device/FCM binding through the supported
  deregistration flow.

Certification seeds every persistence/job/state surface, logs out, kills and
restarts offline, switches tenant/account, and proves zero old rows, files,
state, work, UI, or transmissions survive. New persistence or scheduled-work
types must register in a mechanically checked logout inventory.

## 10. Authorization and Business-Time Gate

- Test tenant-wide, park-scoped, shed-scoped where supported, same-scope,
  cross-scope, no-scope, expired/revoked, and account-switch principals through
  the full router/middleware/handler/repository chain.
- Middleware authorization and SQL clamping must agree. Neither a broad
  bootstrap role nor missing grants may silently become unrestricted access.
- FIXCHK-001 requires real `/app/vaccination/...` route tests proving a scoped
  operator is allowed in-scope and denied cross-park/no-scope.
- Medical and operational dates use the location business timezone through the
  canonical business-time abstraction. Raw UTC is only for instants/audit, not
  due/expiry/calendar-day decisions.
- FIXCHK-002 requires a frozen clock around UTC 18:29/18:30 and IST midnight,
  proving SQL FEFO order, availability, disabled reason, and displayed state use
  one `Asia/Kolkata` date.
- Wrong medical action is P0 regardless of how cleanly it compiles.

## 11. Architecture and System-Design Gate

- Preserve domain/application/port/adapter boundaries. Domain code has no HTTP,
  SQL, cloud SDK, Android, or Next.js dependency.
- Modules do not write another module's private tables or fork private status,
  scheduler, notification, proof, verification, audit, or idempotency engines.
- Every operational feature joins the canonical kernel chain: transaction →
  audit/outbox → trigger/obligation → sweeper → escalation → proof → verification
  → projection/leadership answer.
- Contracts cross boundaries through generated schemas/clients; repository and
  UI models do not leak across modules.
- Use CRG impact/test queries after editing. Update ADR/TRD/context and generated
  architecture artifacts when behavior or ownership changes.
- Add or extend boundary/dependency guards for a recurring violation; a comment
  saying “do not do this” is not enforcement.

## 12. Observability and Production-Safety Gate

- Add structured logs, trace propagation, metrics, and actionable errors at new
  API, DB, worker, outbox, retry, DLQ, projection, upload, and mobile-sync
  boundaries. Never log secrets or proof media.
- Measure DB connections/CPU/IO/locks/temp spill, queue lag, retry/DLQ depth,
  worker progress, API percentiles, web vitals, Android crash/ANR/jank/heap, and
  projection freshness.
- Define alerts, owner, runbook, rollback/disable path, and safe deploy order.
  A metric with no threshold/owner is not a guardrail.
- Promotion uses the exact tested artifact/SHA. Staging evidence cannot certify a
  rebuilt or different production artifact.

## 13. Mechanically Banned Anti-Patterns

Static guards and review must reject new occurrences of these patterns unless a
time-bounded, owner-approved exception proves bounded safety.

Backend/SQL:

- missing tenant/scope predicate; broad `SELECT *`; OFFSET on growing tables;
- query/call inside high-cardinality loop; unbounded `Promise.all`/goroutines;
- N+1 worker writes; fetch-all then filter/sort/count; missing hot index/plan;
- `time.Now().UTC()` for business day; swallowed DB/error-to-empty; permissive
  fail-open auth; timeout increase presented as optimization;
- non-transactional state+audit+outbox; retry without idempotency/unique key;
- unbounded migration/backfill/prune or backfill-before-capture live-write gap.

Admin-web:

- SSR fetch-all, fetch-every-page, both-tab eager fetch, duplicate marker/detail
  calls, client business truth, fake operational rows, loose contract maps,
  direct DB/private API access, error-as-empty, unbounded bundle/media.

Android:

- network→UI or network-only screen repositories; whole-response JSON blobs as
  truth; `limit=1000`; missing cursor/load-more; memory-only continuation;
- unbounded DAO `Flow<List<...>>`, cache/feed/outbox; Main-thread decode/IO;
- non-lifecycle collection, `GlobalScope`, leaked listener/Context/hardware;
- unkeyed/index-keyed dynamic Lazy items; fake state; missing workflow identity;
- logout that removes only bearer/Firebase token while retaining any app state.

Tests/CI:

- mock-only “E2E”; pre-seeded derived state; assertion on the wrong/optional key;
- grep guard with no adversarial self-test; diff-only guard presented as
  whole-tree clean; stale baseline that permanently blesses offenders;
- missing/skipped/zero-job check treated as pass; `continue-on-error`, `|| true`,
  blanket retry, unrelated cached artifact, or prose/screenshot as execution.

## 14. CI/CD Enforcement

Every ordinary pull request must run one required integrity workflow that makes
missing sub-gates visible and fails closed.

- Always run guard self-tests and whole-tree/ratchet checks. Path filters may
  save work only when dependency/impact analysis proves a layer is irrelevant;
  they may not hide backend-contract/mobile/frontend coupling.
- No permanent baseline. Existing debt uses an immediate no-new-debt ratchet;
  each fix reduces the baseline, and the whole-tree gate becomes zero-tolerance
  when its category is cleared. Exceptions need owner, reason, issue, exact
  scope, expiry, and cannot waive P0/P1 correctness or security.
- Branch protection requires all relevant checks. Missing, skipped, cancelled,
  startup-failure, or zero-job status blocks merge.
- Upload machine-readable and human-readable current-SHA reports: tests,
  migrations, SQL plans, query counts, load/latency percentiles, E2E stories,
  Android benchmarks/leaks, web vitals, and guard self-tests.
- Nightly/staging runs exercise 1M/5M-equivalent data, concurrency, retry/DLQ,
  migration/backfill, low-end Android, browser visual/accessibility, and long-run
  heap/battery. Ordinary PR still carries fast representative gates.
- Deployment promotes the same tested artifact, verifies migrations/workers and
  health, supports rollback/feature disable, and blocks on SLO regression.

## 15. Verified Command Inventory

Use only commands present in the current repo and record exact results. At this
revision the core inventory includes:

```bash
make ai-doctor
bash tools/agent-hooks/check-boundaries.sh
bash tools/agent-hooks/check-contract-drift.sh
bash tools/agent-hooks/check-e2e-kernel-integrity.sh
make scale-guard
make mobile-guard-audit
make test
make sqlc-check
make validate-sqlc-plans
make validate-hot-index-migrations
make validate-migrations
make high-scale-kernel-e2e-data
make high-scale-kernel-e2e-certification
make process-integrity-latency-gate
make api-latency-gate
make admin-web-e2e-smoke
npm --prefix apps/admin-web ci
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run build
```

The current staged Android compile/unit command is:

```bash
cd apps/goatos-android
./gradlew :app:assembleStgRelease :app:testStgReleaseUnitTest --no-configuration-cache --stacktrace
```

That command is a floor, not complete mobile certification. Add and require the
applicable lint, Room migration, instrumentation/navigation, baseline profile,
Macrobenchmark, Compose metrics, leak/heap, offline, process-death, and logout
jobs as the ledger fixes land. Verify the Makefile, package scripts, Gradle
tasks, workflow, and threshold sources again at execution time; this inventory
may drift.

## 16. Ledger Update and Independent Counter

After the fix and proof packet:

1. Run the Goat OS code-review skill over the exact diff and impact radius.
2. Ask the other agent to counter the claimed root fix and every proof axis.
3. If challenged, reproduce the dispute from code/SQL/test output. Do not vote.
4. Merge duplicate-root discoveries into the existing row; retain their exact
   evidence in audit history without inflating counts.
5. Recalculate statuses and priorities mechanically. Update every headline,
   table, reconciliation paragraph, guardrail mapping, and final count together.
6. Mark fixed only on the current HEAD. If code changes after proof, rerun the
   impacted packet and publish evidence for the new SHA.

Stop with the row open when required authority, credentials, production-scale
environment, device, or external system is unavailable. Record the blocker and
the strongest local proof; never convert “not tested” into “fixed.”
