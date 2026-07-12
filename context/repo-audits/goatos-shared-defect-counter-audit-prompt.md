# GoatOS Shared Defect Counter-Audit Prompt

Use this prompt in Claude and Codex when auditing the pinned C35 closure range
plus the recent-50 Goat OS bug-fix window. It is intentionally defect-only,
anti-anchoring, local-CI-first, architecture-heavy, and performance-scale heavy.

```text
ROLE
You are a senior Goat OS fault auditor. The recent commits are intended bug
fixes. Decide whether they killed root cause or only patched symptoms that will
regress.

This audit may run simultaneously in Claude and Codex. Both agents must converge
on one shared counter-reviewed bug ledger. Do not produce separate final
Claude/Codex lists.

CANONICAL OUTPUT FILE
/Users/ravi/mesha/goatos/context/repo-audits/last-35-commits-consolidated-bug-ledger.md

The filename is historical. Do not shrink the review to 35 commits because of
the filename. The required audit scope is the pinned C35 closure range plus the
recent-50 commit window described below, unless the user supplies a different
explicit range.

The ledger is the shared truth surface. Draft sections are allowed; the final
deliverable is one reconciled ledger.

LOCAL CI/CD COST CONTROL
GitHub Actions is paused for now because remote CI is burning cost.

Do not push to main by default. Do not casually push commits that trigger GitHub
Actions. Do not use GitHub Actions as the default proof path.

Default validation is local-first across:
- backend
- admin-web/frontend
- Android/mobile
- contracts/API generation
- migrations/sqlc
- guardrails
- local E2E/report generation where runnable

Authoritative local gate:
- `make ci-local` is the first-choice local CI mirror when GitHub Actions is
  paused. It mirrors the required workflow gates locally and must be centered in
  the audit before inventing a hand-rolled validation matrix.

Required named local targets to check when relevant:
- `make ci-local`
- `make clinical-defer-guard`
- `make validate-sqlc-plans`
- `make scale-guard`
- `make mobile-guard`
- `make android-doctor`

For every claimed guardrail, record:
- local command that runs it
- whether it passed locally
- whether it is wired to GitHub Actions
- whether GitHub Actions is paused/skipped
- what local replacement is required

REMOTE PUSH RULE WHILE GITHUB ACTIONS IS PAUSED
Do not push to main unless the user explicitly authorizes remote sync and CI-skip
behavior is confirmed.

If remote sync is explicitly authorized:
- confirm skip-CI behavior before pushing
- GitHub generally honors `[skip ci]` / `[ci skip]` for push and pull_request
  workflow triggers, but do not rely on that for non-standard triggers without
  checking
- if skip-CI behavior or trigger scope is uncertain, do not push
- record the decision in Sync Notes

If no push happens, record LOCAL_ONLY_SYNC in Sync Notes.

PARALLEL COLLABORATION PROTOCOL
Before every material read/write cycle:
1. Verify current repo, branch, HEAD, and dirty files.
2. Reopen the canonical local ledger from disk.
3. Preserve peer-agent content already present.
4. Do not overwrite peer findings, counters, or settlement rows.
5. Write only the section you own:
   - Claude edits only Claude Draft Findings, Claude counters, and Claude-stamped
     notes.
   - Codex edits only Codex Draft Findings, Codex counters, and Codex-stamped
     notes.
   - Do not rewrite the whole ledger file to "clean it up."
6. Shared sections are append-only:
   - Settlement Log, Consolidated Final Ledger, Sync Notes, and shared guardrail
     backlog rows must be added as stamped rows.
   - Never rewrite, reorder, squash, or delete a peer's row.
   - Dedupe by adding a new settlement row that references both IDs, not by
     editing a peer's text.
7. If the ledger changed since you opened it, reopen from disk, re-apply only
   your section edit, and retry. If your section moved, locate it again. Never
   fall back to a full-file overwrite.

After every material ledger update:
1. Save the ledger.
2. Commit locally immediately after each material ledger edit so concurrent
   agents serialize their saves.
3. Commit only the ledger and directly related audit metadata.
4. Do not include unrelated dirty files.
5. Before the next write, reopen from disk again and preserve any peer commit
   that landed meanwhile.
6. Do not push unless explicitly authorized under the remote push rule above.

Conflict rule:
- Preserve both agents' rows.
- Dedupe only in Consolidated Final Ledger.
- Resolve by evidence, not agent authority.
- Last-writer-wins is a defect. If a write would erase peer content, abort,
  reopen from disk, and retry as an append/section-local edit.

CORE RULE
Do not merely validate, repeat, or clean up the old ledger.

The prior ledger is evidence to reconcile, not the answer key. You must run an
independent fresh audit from git diff, current files, tests, guardrails, runtime
paths, and local validation commands. If the final result matches the old ledger,
prove that a fresh hunt found no additional surviving defects.

SOURCE OF TRUTH
Use filesystem and git, not memory.

Current repo:
/Users/ravi/mesha/goatos

PINNED HISTORICAL CLOSURE RANGE RULE
Do not use a rolling `HEAD~35..HEAD` window. That window has drifted and no
longer represents the original C35 defect/fix wave.

Use an explicit pinned range for this audit. Default current C35 closure range:

git diff fe5f3188^..HEAD

Anchor meaning:
- `fe5f3188` = `fix(vaccination): enforce mandatory clinical defer set (C35-010 P0)`
- use `fe5f3188^..HEAD` to include that anchor fix and every later C35/MOB/CL
  closure commit
- if the user supplies a newer explicit base/head, use the user-supplied range
  and record it in Review Metadata

MANDATORY RECENT-50 WINDOW
In addition to the pinned C35 closure range, inspect the last 50 commits from
current HEAD for fresh defects, repeated anti-patterns, and guardrail gaps.

Do not treat "last 35 from current HEAD" as the audited C35 range. The rolling
recent window is 50 commits; the historical C35 range remains pinned.

Prior shared defect ledger, missing from current main/current checkout but
present in commit 310b8969:
git show 310b8969:context/execution/last-35-commits-defect-ledger-2026-07-11.md

Load and reconcile:
- context/repo-audits/consolidated-ledger-defect-closure-program.md
- context/execution/operational-kernel-stability-closure-handoff.md
- context/execution/scale-audit-fix-e2e-report-2026-07-11.md
- context/architecture/operational-kernel.md
- context/architecture/operational-kernel-system-design.md
- docs/phases/README.md
- docs/decisions/scale-anti-patterns.md
- docs/decisions/high-scale-dashboard-projections.md
- docs/decisions/mobile-data-fetch-anti-patterns.md
- docs/decisions/mobile-fetch-fix-backlog.md
- tools/scale-guard/baseline.txt
- tools/scale-guard/README.md
- tools/perf/hot-paths.vaccination.json
- tools/perf/api-latency-gate.mjs
- AGENTS.md
- CLAUDE.md
- CODEX.md
- .claude/settings.json
- .codex/hooks.json
- GitHub issues / PR comments if available

Business/domain source discovery:
- Use Graphify/source docs when available for business truth, especially
  Vaccination Rules, PHC/ops handbooks, GoatOS event-engine/vaccination-flow
  source material, SOP/workflow docs, and source spreadsheets/ledgers.
- If those business sources are unavailable in the current checkout/session,
  say so in Review Metadata and mark affected business-rule conclusions as
  NEEDS_OPS_CONFIRMATION instead of guessing.
- Do not treat code as the only business source. A clean implementation of the
  wrong workflow is still a defect.

Filesystem discovery rules:
- Do not rely on remembered filenames.
- Search the filesystem.
- Worktrees are likely duplicate working copies unless explicitly needed.
- Real review candidates usually live in goatos/docs/decisions/ and
  goatos/context/execution/.
- If a referenced bug list is absent from disk, check git history before
  declaring it missing.
- If BUG-* IDs exist only in chat/session artifacts, say so honestly and do not
  invent filenames.

Graph/code-review tools may be used for navigation and impact only. If stale,
say so and do not use them as proof. Final proof must be file:line, commit,
test, local command, guardrail, query plan, or runtime config evidence.

RECENT ANTI-PATTERN DISCOVERY
Before writing findings, inspect the last 50 commits, in addition to the pinned
C35 range, to learn the recurring anti-patterns this repo has been fixing.

Run:
- git log --oneline -50
- git log --name-only --pretty=format:'--- %h %s' -50 -- backend apps/admin-web apps/goatos-android tools docs context

Extract patterns around:
- N+1 / N+2 / nested fanout
- no batching / fake batching
- slow API responses
- missing CQRS/read-model/projection
- compute-on-read CTEs
- missing or unsafe cache strategy
- Redis used incorrectly or missing only where a real transient cache/lock/rate
  limit is justified
- stale or false-green latency gates
- no query-plan proof
- missing keyset pagination
- fake frontend pagination over capped payloads
- cold-load cache misses
- SSR full materialization
- Promise.all fanout storms
- mobile over-fetch and Room unbounded reads
- timezone drift: non-Asia/Kolkata business dates, notification timing, or
  midnight boundary mistakes
- frontend/mobile business computations that should be backend-owned
- business-rule drift from vaccination rules, SOPs, PHC/operator workflow, source
  spreadsheets, or real farm responsibilities
- screens/API flows that are technically bounded but operationally wrong for the
  people doing the work
- worker loops without cursor/lease/idempotency
- guardrails that pass because debt is baselined, scoped too narrowly, or not
  run locally

Do not assume a pattern is fixed because a commit subject says fixed. Re-open the
code and proof.

SCOPE
Review the pinned C35 range and the recent-50 window on the current GoatOS
checkout. Do not use rolling `HEAD~35..HEAD` unless the user explicitly asks for
a separate legacy rolling-window review.

Start by writing:
- current branch and HEAD
- exact reviewed range, including base/head SHAs and whether it is the default
  `fe5f3188^..HEAD` C35 closure range or a user-supplied range
- exact recent-50 commit list from current HEAD
- commit list
- changed-file summary
- last-50-commit anti-pattern summary
- whether the canonical ledger exists in current checkout
- whether the 310b8969 ledger was loaded
- which prior issue/backlog docs were loaded
- whether any graph/review index is stale
- GitHub Actions status: paused/skipped/explicitly authorized
- local validation commands available for backend/frontend/mobile

Review:
- defects introduced by the pinned C35 range or recent-50 commits
- pre-existing defects still present in touched code
- prior defects claimed fixed by recent commits
- guardrails that claim coverage but do not prove production behavior
- missing local CI/CD gates that would let the same class regress

INDEPENDENT AUDIT FIRST
Before opening the prior BUG-* ledger:
1. Get exact pinned C35 range; default to `fe5f3188^..HEAD` unless the user
   supplied a different explicit range.
2. Get exact recent-50 commit list from current HEAD.
3. List commits and changed files for both scopes.
4. Group changed files by capability:
   - backend kernel
   - vaccination rules
   - workers/sweepers
   - migrations/indexes/query plans
   - admin-web
   - Android/mobile
   - timezone/date/notification computation
   - backend-owned computed API fields
   - contracts/API
   - tests/E2E
   - guardrails/CI/perf
   - local developer validation
5. Inspect current code and diff directly.
6. Generate fresh candidate findings with temporary IDs NEW-CANDIDATE-001,
   NEW-CANDIDATE-002, etc. Do not confuse these with final ledger IDs.
7. Save the fresh draft to the shared ledger.
8. Only then load prior ledger and reconcile.

If no fresh findings beyond prior ledger survive, write Fresh Hunt Null Result:
- files/capabilities checked
- bug classes searched
- local commands run or missing
- negative evidence
- why no additional defects survived

ANTI-ANCHORING RULE
Do not copy prior findings into final ledger unless re-proven against current
code.

For every current ledger item:
- primary live IDs: C35-*, MOB-*, CL-*, NEW-E2E-*
- legacy imported IDs from the 310b8969 ledger: BUG-*, OCK-*, RVF-*

For each item:
- re-check current file:line evidence
- re-check prod reachability
- re-check tests/guardrails
- re-check local validation coverage
- re-check whether later commits fixed/countered it
- map to a fresh finding only if it still survives

If current code disproves old ledger, mark COUNTERED/FIXED WITH PROOF.
If current code shows different root cause, create new final finding and map old
ID as superseded.

PARALLEL CLAUDE/CODEX RULE
Each edit must include:
- agent name: Claude or Codex
- timestamp
- branch/HEAD
- source commit range reviewed
- sync mode: LOCAL_ONLY_SYNC or REMOTE_SYNC_AUTHORIZED

If peer content exists:
- counter-review it ID by ID
- add counter rows to Cross-Agent Counters
- settle disagreements in Settlement Log
- dedupe identical root bugs only in Consolidated Final Ledger
- settle by evidence, not agent authority

If peer content does not exist:
- create agent-stamped draft section
- mark AWAITING_PEER_COUNTER
- do not push remotely unless explicitly authorized

Ledger is not final until every Claude finding has Codex counter and every Codex
finding has Claude counter, or missing side is explicitly marked peer counter
unavailable.

REQUIRED LEDGER SECTIONS
1. Review Metadata
2. Sync Notes
3. Loaded Evidence Sources
4. Business Source Traceability
5. Recent Anti-Pattern Summary From Last 50 Commits
6. Independent Fresh Audit
7. Agent Drafts
   - Claude Draft Findings
   - Codex Draft Findings
8. Prior Issue Reconciliation
9. Cross-Agent Counters
10. Settlement Log
11. Consolidated Final Ledger
12. Fresh Hunt Null Result, if applicable
13. Dropped / Countered / Out-of-Scope Findings
14. Deduped Guardrail Backlog
15. Local CI/CD Improvement Backlog
16. Final Summary Table

ONE-MILLION SCALE ACCEPTANCE BAR
Treat 1M animals as a release invariant.

For every finding, prior issue, counter, and fixed claim, ask whether behavior
still works at:
- 1M animals
- high vaccination obligation volume
- many parks/sheds
- repeated worker retries
- multi-page mobile/web lists
- long-running history
- concurrent sweepers/consumers
- large read-model/projection tables

Business scale:
- Vaccination scheduling must not scan herd on request paths.
- Cohort generation must be tenant/park/shed scoped, resumable, idempotent.
- Sick/quarantine/death/recovery rules must work in batch.
- Missed/overdue/deferred/done transitions must remain correct under bulk
  sweeps.
- Capacity caps, cap=0, backup manager, park scoping, and ownership rules must
  hold across large drives.
- Existing vaccination history must compute next due dates without goat x rule
  N+1 lookups.

Kernel scale:
- event -> txn -> audit/outbox -> trigger -> obligation -> sweeper ->
  notify/escalate -> proof -> read model -> answer must be bounded, resumable,
  observable.
- No request path may replay canonical kernel tables when projection/read model
  is required.
- Workers must use leases, cursors, idempotency keys, retry-safe failure records.
- No silent ACK/drop of kernel work at scale.

Technical scale:
- No unbounded queries, full-herd scans, deep OFFSET, non-sargable hot filters,
  or per-row DB calls.
- Migrations must avoid long locks, full-table rewrites, and non-concurrent
  indexes on large tables.
- Read models/projections must have query-plan proof.
- Guardrails must fail on new scale debt, not only report baseline debt.

1M operations means:
- one million animals and the operational events around them, not merely one
  million rows sitting idle
- repeated daily/weekly commands, reminders, proof uploads, SOP tasks, status
  transitions, projection refreshes, mobile syncs, repairs, and replays
- tenant/park/shed/date scoped throughput with fairness and backpressure, not a
  single happy-path benchmark

Latency bar at 1M scale:
- Ideal hot read/API target: p50 and p95 near or below 100 ms on representative
  local/live data.
- Bare-minimum release target for normal hot paths: p50 <= 100 ms, p95 <= 300
  ms, p99 <= 500 ms.
- Temporary hard ceiling: p99 <= 1000 ms only for explicitly justified
  non-hot/complex paths. A routine hot path above 1000 ms is not release-safe.
- Any 300/500/1000 ms claim must say which percentile it refers to, which data
  volume was used, and whether the run was current SHA.
- No p90/p95/p99 proof on realistic cardinality means "not proven", even if a
  guardrail file exists.
- User-facing writes should ACK quickly and move heavy fanout/rebuild work to
  bounded async execution with correctness proof.

For every final bug, state:
- 1M-safe
- 1M-unsafe
- not proven

BUSINESS / DOMAIN CORRECTNESS MANDATORY LENS
This audit is not allowed to be only a technical review. GoatOS can be fast and
still wrong if the feature does not match the real farm workflow, protocol, role
responsibility, or source ledger.

For vaccination and every future feature, verify:
- source-of-business-truth: which protocol, SOP, handbook, source sheet, issue,
  or product decision proves the intended behavior
- actors and responsibilities: who creates, approves, executes, verifies,
  escalates, repairs, and owns each state
- workflow/state machine: planned, due, overdue, deferred, missed, cancelled,
  done, failed, repaired, quarantined/sick/dead/recovered, or domain-equivalent
  states are legal, reachable, and auditable
- eligibility and exclusion rules: species, age/cohort, shed/park, health status,
  history, booster/repeat rules, stock/capacity, and explicit exceptions
- operational reality: scan flow, task assignment, backup manager, proof capture,
  SOP linkage, stock/vial consumption, empty states, offline work, and escalation
  match how the farm actually operates
- source-ledger parity: dashboard/API/mobile counts and statuses reconcile with
  source spreadsheets/legacy ledgers when those are the migration truth
- role/RBAC/tenancy: the right actor sees and mutates the right scope only
- auditability: every medical/operational decision has a reason, source event,
  actor, timestamp, and repair trail
- business failure mode: wrong animal, wrong vaccine, wrong date, missed booster,
  duplicate task, invisible deferral, wrong manager, wrong park/shed, wrong count,
  or misleading empty state
- future-feature fit: the same business lens must work for breeding, health,
  treatments, deaths, procurement, SOPs, tasks, counts, and inventory

Business mismatch finding rule:
- If code is technically clean but implements the wrong business behavior, record
  a confirmed or plausible defect.
- If the business source is missing, record NEEDS_OPS_CONFIRMATION with exact
  missing source/evidence, not "looks fine."
- Do not let a passing unit test prove business correctness unless the test is
  traceable to source rules or an explicit product decision.

1M OPERATIONS KERNEL ARCHITECTURE CATEGORY CHECKLIST
We are on GCP, but this checklist is about system design invariants, not vendor
box-ticking. GCP services such as Pub/Sub, Cloud Tasks, Cloud Run, Cloud SQL,
Memorystore, Cloud Storage, and schedulers are implementation choices; they do
not by themselves prove the architecture is correct.

For the operational kernel and every major capability, check these categories:
- transactional outbox: canonical write and event publication intent commit
  atomically
- at-least-once safety: every consumer/projector is idempotent and deduplicated
- ordered partition keys: tenant/park/shed/animal/task/aggregate ordering exists
  where business transitions require it
- replay/versioning: event schemas are versioned; replay checkpoints,
  poison-event quarantine, deterministic rebuilds, and schema migration behavior
  are defined
- incremental projections: update impacted tenant/park/shed/date aggregates;
  recurring full-tenant rebuilds are repair tools, not normal request flow
- freshness contracts: read models expose version/projected_at/as_of/serving
  state; stale data is visibly degraded, rejected, or policy-accepted
- pre-aggregated counters: badges, Control Tower, Action Center, Protocol
  Adherence, calendar markers, and summary counts cannot GROUP BY million-row
  projections on every screen
- partitioning and retention: histories, outbox/inbox, projections, audit logs,
  and mobile sync cursors have tenant/time partitions, bounded indexes, archival,
  and chunked pruning
- backpressure: bounded worker pages, concurrency limits, retry budgets, DLQs,
  rate limits, and load shedding prevent replay/repair from crushing Postgres
- workflow orchestration: verification, stock consumption, completion, boosters,
  escalations, and repairs use idempotent process-manager/saga behavior with
  explicit compensation, not distributed synchronous chains
- concurrency control: row versions, unique idempotency keys, compare-and-set
  transitions, narrow locks, and duplicate-submit protection
- API discipline: keyset pagination, summary DTOs, byte budgets, strict filters,
  deadlines, cancellation, per-tenant rate limits, and no unbounded export path
  posing as a screen API
- cache above truth: Redis/local caches may cache hot projection responses or
  coordination tokens, but must use versioned keys and never become correctness
  source
- observability: per-stage lag, queue depth, projection age, retry count, DLQ
  count, query latency, cardinality, and trace correlation from command -> outbox
  -> event -> worker -> projection -> API
- disaster/replay operations: checkpointed rebuilds, shadow projection versions,
  atomic version swap, parity checks, rollback to last-known-good, and operator
  runbooks
- mobile offline synchronization: cursor/checkpoint sync, conflict rules, bounded
  local storage, mutation idempotency, cache purge, and stale/offline labels

Flag a finding if any category is absent, ad hoc, unbounded, not observable, not
tested, not reusable across domains, or only documented as "we use GCP service X"
without proving the invariant.

OPERATIONAL KERNEL AS REUSABLE PLATFORM LENS
The kernel is not a vaccination-only implementation detail. Vaccination is
today's domain; future domains such as breeding, health, procurement, deaths,
treatments, SOPs, and tasks must be able to plug into the same kernel patterns
without copy-paste rewrites.

Audit the operational kernel as a common platform:
- Domain-specific rules live behind small ports/interfaces/configuration, not
  hardcoded switches scattered through shared kernel code.
- Shared infrastructure owns outbox, inbox, idempotency, leases, retry policy,
  DLQ, replay, repair, projection refresh, freshness metadata, and observability.
- New domains should register commands/events/projectors/workers/repair actions
  through explicit contracts, not fork the kernel.
- Apply SOLID pragmatically:
  - single responsibility: command handling, event transport, projection writes,
    retry, and repair are not tangled together.
  - open/closed: adding a new domain should add a module/registration, not edit
    many central switch statements.
  - dependency inversion: domain logic depends on kernel ports/contracts, not
    concrete scheduler/PubSub/DB implementation details.
- Retry/backoff/DLQ/replay must be platform-level behavior with per-domain
  policy where needed; it must not be reinvented differently per feature.
- Extensibility proof: for any kernel finding, ask "How would tomorrow's feature
  plug into this safely at 1M scale?"

Flag findings when:
- vaccination fixes special-case the kernel in a way future domains cannot reuse
- retry/DLQ/replay/repair exists only as ad hoc feature code
- adding a new domain requires duplicating outbox/consumer/projection plumbing
- shared kernel code imports or assumes one business domain unnecessarily
- platform behavior is configurable but has no validation, defaults, or fail-closed
  behavior

TIMEZONE / BACKEND-OWNED COMPUTATION MANDATORY LENS
GoatOS business time is India time. Treat `Asia/Kolkata` (IST, UTC+05:30) as the
canonical timezone for business days, schedules, reminders, notification times,
overdue/deferred/done boundaries, calendar grouping, reports, and audit labels,
unless a future multi-timezone design is explicitly introduced.

Strict timezone rules:
- Code/config must prefer the IANA timezone `Asia/Kolkata`; do not depend on
  ambiguous `IST`, browser locale, device locale, container localtime, database
  session timezone, or CI runner timezone.
- Persist instants in UTC where appropriate, but business dates, day buckets,
  due windows, notification windows, and calendar keys must be derived by backend
  logic using `Asia/Kolkata`.
- Midnight/day-boundary behavior must be tested around UTC/IST crossover times.
- Notifications, reminders, sweeper cutoffs, deferral expiry, overdue status,
  and scheduled jobs must be computed on the backend in `Asia/Kolkata`, not by
  web/mobile clients.
- Any timestamp/date sent to clients must include enough canonical fields for
  dumb rendering: instant, business_date, timezone/as_of where relevant, and
  server-computed status/label.

Backend computation ownership:
- Backend owns business calculations and sends computed state to web/mobile:
  due/overdue/deferred/done, next_due_date, eligibility, priority, capacity,
  escalation, ownership, calendar markers, counts, paging cursors, and empty-state
  reasons.
- Frontend and mobile are renderers. They may format server-provided values and
  perform lightweight display transforms, but must not own business truth or
  recalculate scheduling, eligibility, overdue, cohort, cap, priority, or
  notification decisions.
- Client-side validation may exist only as UX assistance; backend validation is
  authoritative and must reject bad transitions even if a client is stale/offline.
- Offline mobile may render cached backend-computed state with freshness/as_of
  metadata. It must not invent new business statuses from device time except for
  clearly labeled local-only hints that cannot mutate canonical state.
- API contracts should expose computed fields explicitly instead of forcing every
  client to reimplement business rules.

Flag findings when:
- JS/Kotlin/client code computes due/overdue/eligibility/priority/capacity or
  notification timing from raw dates.
- code uses local machine timezone, browser/device timezone, `new Date()` /
  `LocalDate.now()` / `time.Now()` without an explicit `Asia/Kolkata` business
  zone for domain decisions.
- backend returns raw facts and expects web/mobile to derive business truth.
- mobile offline behavior changes business status based on device time without
  backend-computed as_of/freshness.
- notification/sweeper tests do not cover UTC/IST midnight boundaries.
- API response shapes lack computed fields, timezone, business_date, or as_of
  needed for dumb clients.

PERFORMANCE / CQRS / BATCHING / CACHE MANDATORY LENS
This is a first-class lens, not a sub-bullet.

Explicitly hunt for:
- N+1 DB calls: Query/QueryRow/Exec/SendBatch inside loops.
- N+2 / nested fanout: request -> service loop -> repo/client/port loop ->
  DB/API.
- fake batching: method named Batch or ByIDs still loops singular calls
  internally.
- serial waterfalls: await-in-loop, sequential API calls, sequential DB calls.
- unpriced Promise.all: parallel fanout with unbounded cardinality.
- capped raw fetch plus in-memory aggregation pretending to be complete business
  truth.
- compute-on-read god CTEs on hot request paths.
- fallback from projection to canonical replay when projection is
  stale/failed/missing.
- read-side grouping that belongs in a projector/read model.
- CQRS missing: write path has canonical truth but no durable read model for hot
  reads.
- CQRS partial: one endpoint reads projection but sibling endpoints still replay
  raw tables.
- keyset missing: OFFSET or cursor omitted on growable lists.
- query-plan gap: no EXPLAIN/plan gate for hot query.
- index gap: no composite index matching tenant/scope/status/sort cursor.
- payload gap: API returns oversized rows/blob/data when UI needs
  markers/counts/page.
- latency-gate false confidence: gate exists but not run locally, not required,
  not current SHA, too small data, no skew, or only measures without optimizing.
- cache false confidence: cache hides slowness but is stale, unbounded,
  cross-user, not invalidated, or not source-of-truth safe.
- Redis misuse/missing strategy:
  - Redis is not source of truth.
  - Postgres projections/read models are preferred for business truth.
  - Redis may be valid for ephemeral cache, locks, rate limiting, or hot
    transient lookup only when invalidation/TTL/fallback is explicit.
  - If a hot path has no Redis/cache, decide whether that is safe because
    projection/indexes are enough, or unsafe because cold-load/repeated hot read
    remains slow.
  - Do not demand Redis blindly; demand an explicit cache/read-model strategy
    with correctness proof.

DISTRIBUTED SYSTEM / EVENT-DRIVEN ARCHITECTURE MANDATORY LENS
Do not review GoatOS as a basic CRUD app. Review it like a constrained
marketplace-scale operational system: closer in shape to Uber/Tinder/ecommerce
backends than to a simple admin panel.

Core architecture constraints:
- Postgres canonical tables remain the source of truth for business facts.
- Pub/Sub, Cloud Tasks, schedulers, workers, Redis, local caches, and mobile
  stores are execution/read/transport layers, not canonical truth.
- Event-driven flow and CQRS projections are expected for hot operational
  surfaces; request-time reconstruction is suspect by default.
- Outbox/inbox/retry/DLQ/replay/projection plumbing should be reusable kernel
  infrastructure, with domain-specific policy injected through contracts.
- Do not add complexity blindly. Every async component must have a clear reason:
  latency isolation, retry safety, fanout, scheduling, backpressure, or read
  model freshness.

Explicitly audit:
- transactional outbox: event written atomically with the business mutation
- inbox/processed-event ledger: consumers are idempotent under at-least-once
  delivery
- event contracts: versioned, schema-compatible, tenant/park/shed scoped, no
  ambiguous payload semantics
- ordering: where ordering matters, there is a monotonic fence, sequence, event
  time policy, or explicit conflict resolution
- duplicate delivery: exact replay and same-key-different-payload behavior are
  defined and tested
- retries: retryable vs poison failures are separated; retries do not pile up or
  repeat side effects
- DLQ/repair: poison messages are visible, replayable/discardable with operator
  reason, and do not silently ACK/drop work
- backpressure: workers and relays use bounded batches, leases, rate limits, and
  queue-lag observability
- sagas/workflows: multi-step business processes have compensating or repair
  paths; partial completion cannot strand invisible work
- projection ownership: exactly one writer owns each read model, or multi-writer
  rules are explicit and conflict-safe
- projection freshness: APIs expose freshness/as_of/version metadata where stale
  reads matter
- projection rebuild: rebuild/prune is version-swapped or otherwise avoids
  stop-the-world gaps and serving empty/partial business truth
- fanout: notification/proof/SOP/read-model fanout is bounded, idempotent, and
  observable
- scheduler/worker deployment: local and deployed worker config must fail closed
  if required actor, tenant, protocol, or task-creator config is missing
- consistency model: each API/screen states whether it is strong, read-your-write,
  eventual, stale-allowed, or fail-closed
- replay/rebuild: historical event replay or read-model rebuild cannot corrupt
  current state or double-create obligations/tasks
- extensibility: adding tomorrow's domain can reuse the same command/event/
  projector/retry/DLQ contracts without cloning vaccination-specific code

For each major capability, record the architecture pattern:
- synchronous command only
- transactional command + outbox
- Pub/Sub/event consumer
- scheduled worker/sweeper
- Cloud Tasks/job queue
- CQRS projection/read model
- Redis/cache/local Room cache
- manual repair/DLQ path

Flag a finding when the chosen pattern is too simple for the scale/consistency
need, or too complex without correctness proof.

Hot paths that require special scrutiny:
- Control Tower
- Action Center
- Protocol Adherence
- Calendar
- /vaccination/sheds
- /vaccination/execution
- /vaccination/operations
- SOP Library
- Herd Register
- mobile bootstrap/cold load
- mobile calendar/day/detail/scan/tasks/review queue
- obligation sweeper
- domain consumer
- projection recompute/prune
- outbox relay and DLQ repair

For each hot path, record:
- business source/protocol and actor workflow it serves
- data source: canonical tables / projection / cache / Room / network
- architecture path: sync command / outbox / Pub/Sub / worker / projection /
  cache / repair path
- pagination: keyset / offset / none
- batching: set-based / fake batch / per-row
- query-plan proof: present/missing/stale
- latency proof: local current-SHA p90/p95/p99 present/missing/false-green
- latency target verdict: ideal <=100 ms; bare-minimum p50<=100/p95<=300/
  p99<=500; p99<=1000 only with explicit temporary justification
- cache strategy: none justified / unsafe none / Redis / local cache / Room /
  projection
- consistency and freshness model: strong/read-your-write/eventual/stale-allowed
  /fail-closed, with as_of/version proof where needed
- 1M verdict

SCREENSHOT-DERIVED CLOSURE CHECK
Specifically verify this closure gap:
- Some paths may now be architecturally correct on main: CT, Action Center,
  Protocol Adherence, Calendar, SOP list N+1.
- Do not mark complete unless /vaccination/sheds, /vaccination/execution, and
  /vaccination/operations are also proven bounded and projection/read-model
  safe.
- A shed projection alone does not certify execution and operations endpoints.
- A latency CI commit is a gate, not an optimization by itself.
- Bootstrap cold-load caching and frontend SOP request cleanup must be present
  and proven locally.
- No honest 1M API certification exists until projection paths, query plans,
  bounded payloads, and local/live p90/p95/p99 gates all pass together against
  the 100/300/500/1000 ms bar.

LOCAL CI/CD IMPROVEMENT LENS
Audit the local developer validation path itself. Findings should include
missing or weak local CI/CD coverage.

Check whether there is one reliable local command, or a small documented command
set, that validates:

Backend:
- Go unit tests
- targeted integration tests where Docker/Postgres is available
- migrations validation
- sqlc generation/checks
- query-plan validation
- scale guard
- clinical defer guard for C35-010 medical-safety regressions
- API latency gate where local stack supports it
- kernel/outbox/sweeper/domain-consumer replay tests

Frontend/admin-web:
- typecheck
- lint
- build
- route/API contract checks
- mock-fidelity checks
- request-plan/fanout checks
- fake pagination checks
- local E2E smoke where local stack supports it

Mobile/Android:
- compile
- unit tests
- Room migration/schema checks
- mobile list-fetch guard
- `make android-doctor` for pinned JDK/SDK/device/emulator diagnosis
- `make android-dev-run` for local phone/emulator app run where interactive
  validation is needed
- baseline profile / macrobenchmark guard where locally runnable
- memory/offline/cache/session isolation tests
- API contract compatibility checks

Contracts/API:
- OpenAPI validation
- generated clients up to date
- backend/frontend/mobile contract drift checks

Reports:
- E2E report generation must be reproducible locally
- published report must not claim remote/staging proof if only local proof ran

If a bug would only be caught by a GitHub Action that is currently paused, that
is a finding. Propose a local guardrail or local make target that catches it
before push.

MOBILE / ANDROID MANDATORY LENS
Mobile is mandatory if Android/mobile/API-contract/cache/worklist/calendar/scan
/task files changed.

Check:
- Room is UI source of truth.
- Network refreshes/upserts Room; screens observe Room.
- No screen powered directly by network-only data unless justified.
- Pagination exists in backend keyset cursor and bounded Room read.
- No 50/100/200/1000 row fetches unless proven bounded.
- No growing JSON blob cache.
- Large lists use Paging 3 + RemoteMediator + Room PagingSource.
- ViewModel state uses stateIn(WhileSubscribed(5_000)), not forever collectors.
- DTO parsing/mapping once, off Main, preferably flowOn(Dispatchers.Default).
- Lazy lists use stable keys; no eager Column/forEach for data lists.
- Empty/loading/error/offline states come from Room-backed state.
- Offline re-entry renders cached data correctly.
- Sign-out/session change purges protected caches or namespaces by
  tenant/principal/role/authority revision.
- No Context/View/Activity in ViewModels.
- Hot device flows cancelled/disabled on clear.
- RFID/scan paths cap buffers and do O(1) tag matching.
- Android API-contract changes are backward-compatible or versioned.

MOBILE / ANDROID DEEP BEST-PRACTICE LENS
Do not stop at "the screen works." Audit Android like a production offline-first
Compose app.

Best-practice source check:
- If Context7 MCP or another approved docs connector is available, check current
  Android Jetpack Compose, Paging, Room, coroutines, lifecycle, and performance
  guidance before finalizing mobile findings.
- If live docs are unavailable, say so in the ledger and mark best-practice
  verification as model/repo-derived, not externally refreshed.
- Prefer repo patterns and official Android guidance over ad hoc local style.

Architecture and data flow:
- UI must not call Retrofit/network clients directly.
- ViewModels should call repositories/use cases; repositories own network +
  Room synchronization.
- Room is the screen source of truth for read models; network refreshes/upserts
  Room, then UI observes Room.
- Backend owns business computations. Mobile stores and renders backend-computed
  due/overdue/deferred/done, priority, calendar marker, empty-state, and
  notification state; it must not recompute business truth from device time.
- Direct network-only reads are allowed only for explicit one-shot commands or
  clearly non-cacheable operations, and must have documented empty/error/offline
  behavior.
- Sync must be idempotent, cursor-aware, and safe across app restart, offline
  re-entry, sign-out, and authority/role change.

Coroutines and threading:
- No blocking I/O, JSON parsing, DTO mapping, filtering, sorting, date parsing,
  or O(n) roster matching on Main.
- Use Dispatchers.IO for I/O and Dispatchers.Default for CPU-heavy mapping.
- Use structured concurrency; avoid GlobalScope and unmanaged jobs.
- Collect flows lifecycle-aware; prefer collectAsStateWithLifecycle in Compose.
- Avoid forever collectors in ViewModels; prefer stateIn(WhileSubscribed(5_000))
  where appropriate.
- Ensure retry/backoff loops are bounded, cancellable, and do not pile up.
- Any remaining display-only date formatting must use explicit `Asia/Kolkata`
  business values from backend state, not device timezone assumptions.

Compose recomposition and UI performance:
- Minimize unnecessary recomposition.
- Use stable immutable UI state and stable item keys for LazyColumn/LazyGrid.
- Avoid creating new lambdas/objects/lists in tight composable recomposition
  paths when they can be remembered or derived.
- Use remember, derivedStateOf, snapshotFlow, and LaunchedEffect keys correctly;
  flag stale or overly broad effect keys.
- Avoid heavy work inside composables.
- Avoid eager Column/forEach for data lists; use lazy containers with keys.
- Check list item identity, scroll behavior, loading footers, pull-to-refresh,
  and load-more triggers for duplicate loads.
- Check for jank, cold-start regression, excessive allocations, and battery
  pressure where relevant.

Memory leaks and lifecycle:
- ViewModels must not hold Activity, View, Fragment, or unscoped Context.
- Use @ApplicationContext only where a context is truly required.
- Register/unregister callbacks, listeners, RFID/device streams, and broadcast
  receivers correctly.
- Ensure hot flows/device readers are stopped or disabled on clear/logout.
- Cache tables and in-memory buffers need TTL/caps/pruning where growable.
- Check for cross-user/cross-role local state leakage after sign-out.

Compose design-system fidelity:
- Follow the GoatOS/Mesha Android design system and existing theme tokens.
- Do not hardcode hex colors in feature UI unless the design system explicitly
  defines that token there.
- Do not hardcode typography, spacing, shapes, elevation, or one-off component
  styles when theme tokens/components exist.
- Do not introduce one-off buttons/cards/chips if a shared component exists.
- Check dark/light/theme behavior, disabled states, empty states, loading states,
  error states, touch targets, accessibility labels, and localization.
- UI must be mock/design-system faithful without hiding unbacked controls.

Mobile validation expectations:
- Prefer local Android compile and unit tests as the minimum bar.
- Add/require targeted tests for Room migration/schema, repository sync,
  ViewModel state, offline re-entry, pagination/load-more, cache purge, and
  session isolation.
- Use macrobenchmark/baseline-profile/memory guards where locally runnable.
- If a mobile performance or lifecycle issue is only catchable by a paused
  GitHub Action, record the missing local guardrail as a finding.

Mobile L0/L1/L2/L3:
- L0 overview: markers/dots/counts only, not full event lists.
- L1 day/worklist: keyset page about 20 rows, infinite scroll.
- L2 shed/drive/task detail: keyset page, carries task_id/batch_id/shed identity.
- L3 vaccine capture/scan roster: keyset page about 20, never whole cohort.
- Navigation must carry exact task/shed/batch identity. Do not choose first
  assigned task after scan.
- Back/refresh/offline transitions preserve selected level and identity.
- Empty states distinguish no data, not loaded, offline no cache, forbidden,
  backend error.

MOBILE SOURCES
Read and apply:
- docs/decisions/mobile-data-fetch-anti-patterns.md
- docs/decisions/mobile-fetch-fix-backlog.md
- tools/agent-hooks/check-mobile-list-fetch.mjs
- mobile guard output if available
- prior mobile C35-*/MOB-* entries from the live ledger
- legacy mobile BUG-* entries from the 310b8969 ledger, if still relevant

Explicitly verify:
- Calendar overview uses day markers.
- Calendar day list consumes next_cursor.
- Scan roster sends task_id/cursor, receives next_cursor, stores per-item data.
- Tasks are offline-first, Room-backed, paginated.
- Review queue and leadership do not over-fetch.
- Outbox does not observe SELECT * forever and prunes terminal rows safely.
- Room queries have LIMIT/PagingSource.
- Mobile does not drop events after first page.
- Mobile does not leak one user's cache to another user.

REVIEW LENSES
Rank by changed files:
1. Fix quality / root-cause regression
2. Business/domain correctness and source-rule traceability
3. 1M operations kernel architecture category checklist
4. Performance, batching, CQRS, cache, latency
5. Distributed/event-driven architecture
6. Operational kernel as reusable/extensible platform
7. Retry, backoff, DLQ, replay, repair, idempotency
8. 1M scale with 100/300/500/1000 ms API latency bar
9. Strict Asia/Kolkata timezone and backend-owned computation
10. Kernel flow
11. Vaccination business rules
12. Seed/data integrity
13. Date/time math
14. Concurrency/idempotency
15. Migrations/indexes/query plans
16. Web/admin
17. Mobile
18. Security/RBAC/tenancy
19. Observability and guardrails
20. Local CI/CD coverage

FRESH FINDING REQUIREMENT
For each changed capability, actively search:
- untested regression
- sibling call site not fixed
- business-case mismatch with protocol/SOP/source ledger/operator workflow
- missing or unverified business source for implemented behavior
- wrong actor, wrong responsibility, wrong workflow state, wrong escalation, or
  misleading empty state
- source-ledger parity gap between code/API/UI/mobile and migration/business truth
- N+1 / N+2 / fake batch
- slow API / missing p95/p99 proof
- missing 1M operations architecture category: outbox, idempotency, ordering,
  replay/versioning, incremental projection, freshness, pre-aggregated counter,
  partition/retention, backpressure, workflow orchestration, concurrency, API
  discipline, cache-above-truth, observability, disaster/replay, or mobile sync
- missing CQRS projection/read model
- missing or unsafe event-driven boundary
- missing outbox/inbox/DLQ/replay semantics
- feature-specific retry/DLQ/replay code that should be shared kernel behavior
- missing platform extension point for tomorrow's domain
- projection ownership/freshness/rebuild gap
- Pub/Sub/worker/scheduler config drift
- unsafe or absent cache strategy
- false-green latency or E2E test
- p95/p99 latency above the 100/300/500/1000 ms bar or measured on toy data
- timezone drift: non-Asia/Kolkata business date, notification, scheduler,
  overdue, or report calculation
- client-owned business computation that backend/API should own
- API shape that forces web/mobile to recalculate business state
- stale guardrail
- missing local validation gate
- missing E2E story
- mobile over-fetch/offline leak
- projection fallback
- worker retry/idempotency failure
- unbounded retry/backoff, invisible poison messages, missing repair console, or
  replay path that can double-apply side effects
- migration scale hazard
- authz/tenant leak
- missing observability

Do not invent findings. If none survive, document negative evidence.

EVIDENCE RULE
Every surviving finding needs:
- file:line
- commit or prior issue source
- prod reachability
- failure scenario
- if business/domain related: source rule/SOP/ledger/product decision, expected
  actor workflow, actual code/API/UI behavior, and mismatch impact
- if 1M operations architecture related: checklist category, current mechanism,
  missing invariant, cardinality/throughput scenario, and recovery/observability
  proof or gap
- if scale/performance related: data volume, p50/p95/p99 numbers, threshold
  breached, and whether proof ran on current SHA
- if time/business-computation related: exact timezone source, backend vs client
  owner, UTC/IST boundary case, API fields involved, and stale/offline behavior
- current test/guardrail/local-command gap
- strongest counterargument
- final disposition

Drop guesses. If evidence incomplete, mark PLAUSIBLE or UNPROVEN.

CROSS-AGENT COUNTER FORMAT
Counter ID:
Target finding:
Countering agent:
Claim challenged:
Evidence for:
Evidence against:
Prod reachability decision:
Business/domain decision:
Scale decision:
1M operations architecture category decision:
Performance/CQRS/cache decision:
Distributed architecture decision:
Kernel platform/extensibility decision:
Retry/DLQ/replay decision:
Timezone/backend-computation decision:
Local CI/CD decision:
Test/guardrail decision:
Disposition: CONFIRMED / DOWNGRADED / DROPPED / MERGED / NEEDS_MORE_PROOF
Settlement note:

SETTLEMENT RULE
A finding enters Consolidated Final Ledger only if:
- it survives self-review, and
- it survives peer counter-review, or
- peer counter unavailable is explicitly recorded

If Claude and Codex disagree:
- keep both arguments
- decide in Settlement Log by evidence
- if unresolved, mark PLAUSIBLE/UNPROVEN, not CONFIRMED

E2E / GUARDRAIL / LOCAL CI CHECK
For each finding, check:
- backend/tests/e2e
- business-rule tests traceable to source protocol/SOP/ledger/product decision
- source-ledger parity tests or reconciliation reports where migration truth
  exists
- actor workflow tests for role, responsibility, escalation, repair, and empty
  states
- GitHub Pages E2E reports, but only as report evidence, not remote CI proof
- make ci-local
- make clinical-defer-guard
- make scale-guard
- make mobile-guard
- make validate-sqlc-plans
- make android-doctor, when Android environment/device readiness is relevant
- API latency gate
- process-integrity latency gate
- 1M operations architecture category tests/checks for outbox, idempotency,
  ordering, replay/versioning, incremental projections, freshness, counters,
  partitioning, backpressure, workflow orchestration, concurrency, API discipline,
  cache-above-truth, observability, disaster/replay, and mobile sync
- outbox/inbox/retry/DLQ/replay/repair tests
- timezone/date-boundary tests for Asia/Kolkata business dates and notifications
- API-contract tests proving backend-computed fields are present for dumb clients
- query-plan validation
- local backend/frontend/mobile commands
- CI workflows, noting GitHub Actions is paused
- hooks/agent guardrails

Classify:
- covered locally
- missing local guardrail
- false-green
- stale evidence
- remote-only and currently paused

FINDING FORMAT
ID:
Priority: P0/P1/P2/P3
Title:
Status: open/countered/fixed/out-of-scope
Origin: introduced/pre-existing/prior-ledger/new
Verdict: CONFIRMED/PLAUSIBLE/UNPROVEN
Business/domain verdict: source-matched/business-mismatch/needs-ops-confirmation/
not applicable
Scale verdict: 1M-safe/1M-unsafe/not proven
1M operations architecture verdict: invariant-safe/category-gap/vendor-only-proof/
not proven
Performance verdict: bounded/projection-backed/cache-safe/slow-risk/not proven
Architecture verdict: sync-safe/event-driven-safe/projection-safe/over-simple/
over-complex/not proven
Kernel/platform verdict: reusable/extensible/feature-coupled/retry-unsafe/
not proven
Latency verdict: ideal-100ms/met-300-500-bar/over-1000ms/not measured/not proven
Timezone/computation verdict: Asia-Kolkata-safe/backend-owned/client-owned/
ambiguous/not proven
Local CI verdict: covered locally/missing/failing/not run/remote-only paused
Prior mapping: C35-*, MOB-*, CL-*, NEW-E2E-*, backlog item, GitHub issue
Legacy mapping, if imported from 310b8969: BUG-*, OCK-*, RVF-*
First reported by: Claude/Codex/prior ledger
Peer counter status: countered/awaiting peer/peer unavailable
Layman explanation:
Evidence:
Business source / expected workflow:
Prod reachability:
Failure scenario:
Business impact:
Root-cause-or-band-aid verdict:
Counterargument:
Why it survives / why downgraded:
E2E / guardrail / local CI status:
Fix sketch:
Guardrail needed:

PRIORITY RULE
P0: data loss, wrong medical action, tenant/security boundary break, prod outage
P1: perf-at-scale, broken core business rule, business/source-ledger mismatch,
major mobile/offline leak, false-green release gate
P2: correctness edge case, bounded scale debt, weak guardrail, missing local
regression test
P3: maintainability only

STOP RULE
Do not fix anything. Do not produce competing final lists. Output one shared,
counter-reviewed, locally synced ledger with:
- independent fresh findings
- prior captured bugs
- anti-pattern findings
- existing issue ledgers
- business/source-rule traceability and business-mismatch review
- last-50-commit anti-pattern summary
- 1M operations kernel architecture category review
- N+1/N+2/batching/CQRS/cache/latency review
- distributed/event-driven architecture review
- reusable kernel platform / SOLID extensibility review
- retry/backoff/DLQ/replay/repair review
- strict Asia/Kolkata timezone and backend-owned computation review
- 1M-scale business/kernel/technical review
- mobile best-practice review
- local CI/CD improvement review
- Claude/Codex peer counters
- settlement decisions
- final common-ground disposition

If final ledger matches the old ledger, explicitly prove this happened because
fresh audit and peer counter-review found no additional surviving defects, not
because the old ledger was treated as the answer.
```
