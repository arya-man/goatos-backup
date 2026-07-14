# Kernel Integrity & Scale (5k-50k current envelope; 1-5M future gate)

The operational kernel is the core of Goat OS. Review it first and hardest. A
feature that forks its own scheduler, status store, proof flow, or notification
path — instead of plugging into the kernel — is a CRITICAL finding even if it
compiles and passes tests.

**Current release scale target: 5,000-50,000 animals.** Per the accepted ADR
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the active
deployment optimizes for the 5k-to-50k envelope: APIs read canonical indexed
SQL, one modular kernel worker replaces the split job fleet, and query-plan
proof runs at the upper bound (~500k obligation rows = 50k animals × retained
obligations) under single-worker cadence/backlog validation. The
one-million / 1-5M-animal topology is **future certification work, not a present
release requirement** — retained below as the FUTURE scale gate, not deleted.
This is a scope reframe, not a safety downgrade: every anti-pattern, the
idempotency/migration/timezone bars, and the kernel golden lens below stay in
force. When the two disagree on the *present* release bar, the ADR wins; on
*correctness shape* (indexing, boundedness, idempotency), both agree.

Law: `context/architecture/operational-kernel.md` (golden rule),
`context/architecture/operational-kernel-system-design.md` (system design),
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md` (current 5k-50k
deployment authority), `docs/protocol-engine/high-scale-kernel-validation-plan.md`
(future 1-5M scale validation).

**Verify against source, not memory.** This file names tables, statuses, cron
binaries, and thresholds only to tell you WHAT to check and WHERE to confirm it.
Values shown inline are illustrative and drift — always re-read the cited
committed source (the latest migration `CHECK` constraint, seeded config, the
rules doc, the `Makefile`, `backend/cmd/`) at review time before asserting a
finding on an exact value.

## The kernel chain and where each stage lives

Every operational feature must answer: what was expected, was it followed, where
did it break, who owns the next action, what is due by when, what evidence proves
it, and what escalation fires when a deadline passes. That maps to this chain:

| Stage | Code home |
|---|---|
| Business event (API/handler) | `backend/cmd/api/` (HTTP server main), module `adapters/http/` |
| Canonical transaction | module `app/service.go` (writes state + audit + outbox atomically) |
| Audit + outbox (same txn) | `backend/internal/operationsaudit/`, `backend/internal/outbox/`, `backend/internal/platform/outbox/` |
| Outbox relay → Pub/Sub | `backend/cmd/outbox-relay/` |
| Trigger evaluation | `backend/internal/protocol/app/`, module `app/*_handler.go` |
| Obligation generation | `backend/internal/obligation/` (`obligation_instances`, `obligation_batches`) |
| Sweeper / time spine | `backend/cmd/obligation-sweeper/`, `backend/internal/obligation/app/` |
| Reminder / escalation | `backend/internal/calendar/`, `backend/cmd/calendar-*-sweeper/` |
| Notification | `backend/internal/notification/`, `backend/cmd/notification-dispatcher/` |
| Proof / verification | `backend/internal/proof/`, `backend/internal/sop/` |
| Read model / projection | `backend/internal/calendar/adapters/postgres/` and module projections |
| Leadership answer | Control Tower / Action Center / Protocol Adherence / Calendar contracts |

Postgres (`obligation_instances`) is the source of truth for future/far-future
due work — **not** Cloud Tasks or Pub/Sub state. Cloud Tasks is only for
near-term retries/reminders (minutes-hours). Losing a task must never lose work.

## CRITICAL kernel violations (block merge)

1. **Private engine instead of the kernel.** New feature builds its own
   scheduler / status field / proof capture / notification sender rather than
   emitting into the kernel chain. Every process joins the shared kernel.
2. **State + audit + outbox not in one transaction.** Canonical write and its
   outbox row (the outbox table lives in `backend/internal/outbox/adapters/postgres/`
   and its `CREATE TABLE` migration — verify the table name there rather than
   trusting a name in this doc) plus audit rows must commit together
   (`BEGIN … COMMIT`). Split transactions lose the event or the state on failure.
3. **Direct process-status write that skips the flow.** e.g. setting an
   `obligation_instances.status` terminal value in an app service instead of
   going through verification → completion event → sweeper/booster. Status is
   durable process truth; direct writes drop audit, skip obligations, break
   booster chains. See "Legal-status transition review" below.
4. **Cloud Tasks / Pub/Sub / frontend as the calendar.** Future obligations must
   materialize as Postgres rows. Frontend must not own canonical
   due/overdue/escalation/verification state — backend derives it and the UI renders.
5. **Cross-module table write.** A module writing another module's tables (e.g.
   `vaccination` issuing `UPDATE obligation_instances`) instead of calling the
   owning module's service through a port. Breaks encapsulation and idempotency.
6. **Notification/escalation only in logs.** Reminders and escalations must be
   durable rows (`notification_requests`) sent via the `NotificationGateway`
   port and acknowledged/resolved — not a `log.Warn`. A dropped log = silently
   missed escalation.
7. **Critical action bypasses the guardrail/policy-pack contract.** High-risk
   animal, stock, movement, proof, exit/death, quarantine/ICU, sale/allocation,
   or other critical transitions must go through the shared guardrail shape:
   action request, scoped evidence, policy-pack/version, deterministic decision,
   approval/exception/proof/obligation/escalation plan, audit, outbox, and
   projection. Lower-level mutation primitives are adapters, not a bypass.

## Legal-status transition review (obligation state machine)

Obligation status is durable process truth with a fixed allowed set and a
directed state machine. Two failure classes to catch: (a) code writes a status
value that is not in the `CHECK` constraint (a migration will reject it, or a
future migration relaxes it and stale code assumes the old set), and (b) code
performs an illegal transition — most dangerously, mutating a terminal state.

**Verify the allowed set against the CHECK constraint in the LATEST obligation
migration, not against this list.** Find it with
`ls backend/migrations/postgres | grep -iE 'obligation|deferred|missed'` and read
the newest one that `ALTER … ADD CONSTRAINT … _status_check`. As of the current
tree that constraint (migration `000097_obligation_deferred_and_missed_deadline_index.sql`)
allows — *illustrative, re-verify*: `scheduled`, `due`, `in_progress`,
`deferred`, `completed`, `missed`, `waived`, `canceled`, `superseded` (default
`scheduled`). `pending` and `assigned` are NOT obligation statuses — `pending`
appears only as a computed read-model view label (e.g. `proof_pending`), never
as a persisted DB status. Legal transitions are documented in
`docs/protocol-engine/state-machines.md` and `docs/protocol-engine/obligation-engine.md`.

Review checkpoints:
- [ ] Every status literal written to `obligation_instances.status` is in the
      latest migration's `CHECK` set; no `pending`/`assigned`/ad-hoc value
- [ ] Transitions follow the documented state machine (e.g. `scheduled→due`,
      `due→in_progress`, `deferred→scheduled` on recovery); no illegal jumps
- [ ] **Terminal states are immutable.** `completed`, `missed`, `waived`,
      `canceled`, `superseded` are history, not mutable state. A correction or
      rework is a NEW obligation/work record + status event, never an in-place
      edit of a terminal row
- [ ] Each transition emits a status event into the obligation status-events
      ledger (auditable) and, where relevant, an outbox event — not a silent
      column write

## Critical action / policy-pack review

When a change introduces or exposes a high-risk action, verify the policy-pack
contract from `context/architecture/operational-kernel-system-design.md` and
`docs/features/critical-animal-action-guardrails.md`:

- [ ] The action has an explicit type, subject refs, requested transition,
      tenant/park/shed/date/owner scope, actor/authority, idempotency key,
      evidence refs, policy pack, and immutable policy version
- [ ] Evidence-derived classification is computed by the pack/adapters, not from
      names, tags, free text, or UI labels alone
- [ ] Decisions are deterministic: `allow`, `block`, `require_approval`,
      `require_exception`, `defer`, or `create_process_exception`, with disabled
      reasons and risk/severity recorded
- [ ] Approval/exception paths record who can approve, segregation-of-duty rules,
      reason, expiry, proof requirements, and escalation behavior
- [ ] Lower-level primitives are blocked, wrapped, or restricted until the pack
      owns the critical transition; unwrapped live paths return a guardrail
      required error or create a durable process exception
- [ ] Request + evaluation + state change + audit + outbox commit atomically;
      replay of same idempotency key returns the same result and same-key
      different-payload conflicts with no side effects
- [ ] Missing host-module integration still records a durable deferred marker or
      process exception with owner/SLA/read-model visibility instead of silently
      doing nothing
- [ ] Destructive correction paths are explicit: wrong critical reports or
      actions are voided/reversed/corrected with audit/history/outbox and
      preserved lineage, never hidden by delete-and-recreate or hard-delete
      unless a cited source explicitly allows that lifecycle
- [ ] Tests cover allow, block, approval, exception, replay, idempotency conflict,
      missing evidence, stale state, primitive bypass, and skewed/high-scale load

## Scale shape — hard requirement on every change (5k-50k now, 1-5M future)

The correctness *shape* is a hard requirement at every scale. For any new query,
worker, importer, reporting path, or UI data flow, check the scale shape:
tenant/run-scoped, indexed, chunked/paginated, bounded in memory/goroutines,
idempotent on retry, and query-plan-validated on large tables. None of that
relaxes under the 5k-50k envelope — a query that is unbounded, unindexed, or
fans out per animal is still a defect at 500k rows, and the same code is what
must eventually certify at 1-5M.

The scale *acceptance bar* is what the ADR reframes. The **current** release
gate proves the query-plan/backlog shape at the upper bound of the envelope
(~500k obligation rows at 50k animals) under the single kernel worker's
cadences, per `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. The
1M / 1-5M full-chain acceptance bar and its `high-scale-kernel-e2e-*`
certification stay the **future** gate (see "High-scale certification gates"
below) — required before the multi-worker topology ships, not before the 5k-50k
release. A green 5k plan is never proof for the 50k aggregate, and a green 50k
plan is never proof for the future 1M certification; keep the three claims
distinct.

CRITICAL scale violations (banned in `backend/internal/**` at every scale; the
five ADR-exempted canonical screen reads are the only carve-out — see the
read-model note below):

1. **Unbounded fan-out.** `for _, a := range allAnimals { go … }` or loading all
   animals into memory. Use a bounded worker pool / fixed batch size. At 1M
   animals this OOM-kills Cloud Run.
2. **Full-table scan without tenant/date filter.** Sweeper/list query lacks
   `tenant_id` + `due_at`/`status` filters or an indexed access path. Scans time
   out and leak across tenants. Require: chunked by park/date/tenant, cursor
   resume, `LIMIT`.
3. **OFFSET pagination on a large table.** Use keyset pagination
   (`WHERE id > $last ORDER BY id LIMIT n`). No OFFSET beyond a small bound on
   tables that can exceed ~100K rows.
4. **Missing idempotency contract.** Every mutating API/worker/importer/webhook/
   state-transition/outbox producer-consumer must accept or derive a stable
   idempotency key, persist the key + a semantic request fingerprint in the same
   transaction as the side effects, replay the original result without re-running
   side effects, and reject same-key/different-payload. `ON CONFLICT DO UPDATE`
   that only sets `idempotency_key = EXCLUDED.idempotency_key` is NOT sufficient
   when later code still mutates state. Enforce a DB `UNIQUE (tenant_id,
   idempotency_key)` (confirm the exact unique constraint on the target table's
   `CREATE TABLE` migration). Tests must cover: first call, exact replay,
   same-key different-payload replay, downstream duplicate prevention.
5. **No query-plan validation on a hot path.** DB/migration change touching
   import/animal/event/counter rows at scale without an indexed access path and
   `make validate-sqlc-plans` coverage. That target proves static/index
   reachability; high-scale/staging claims also need seeded `ANALYZE` plus
   `EXPLAIN (ANALYZE, BUFFERS)` at realistic row counts proving the planner
   chooses the index/projection path **without** `enable_seqscan=off` (see
   `docs/protocol-engine/high-scale-kernel-validation-plan.md`).
6. **Read-time process state instead of persisted.** Computing durable status
   (missed/overdue/escalation level) at read time when the sweeper should
   materialize it. Read-time compute is non-durable and inconsistent across
   queries. (Projection-level "days overdue" derived for display is fine; the
   canonical status transition must be persisted.)
7. **Dashboards/reports sliced by dimension without the projection rule —
   except the five ADR-exempted canonical screen reads.** Slicing by
   month/date/breed/farm/shed/load/status/etc. must follow
   `docs/decisions/high-scale-dashboard-projections.md` — durable projections,
   not raw scans — for every path EXCEPT the five screens the 5k-50k ADR moves
   to canonical reads. Under `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`
   the active runtime keeps **0 of the five named screen projections**
   (`calendar_event_projections`, `process_integrity_projection_rows`,
   `vaccination_shed_projection_rows`, `vaccination_execution_projection_rows`,
   `vaccination_operations_projection_rows`): those screens read canonical
   indexed SQL — a keyset-paginated ~20-row **list read**, plus an indexed
   **summary aggregate** for rollups/counts. These reads are explicitly exempted
   from the compute-on-read ban (see the read-model note below); the ban still
   holds for every other path. `vaccination_eligibility_rollups` and the counts
   summaries survive as durable read models. A projection-backed API that is
   still projection-backed must expose the standard freshness envelope
   (`as_of`/`last_success_at`, `freshness_status`, `serving_state`, source
   watermark/unavailable sources, stale/rebuild flags, projection version); a
   stale or approximate response must say so. A canonical read cannot be stale
   relative to the canonical write, so it carries no freshness envelope and needs
   no reconciler — but it MUST be query-plan-tested at both list and aggregate
   shapes against the ~500k upper-bound row count.
8. **N+1 per-animal queries in a loop.** A list, generation, projection, or
   drive-planning path that issues one DB round-trip per animal (fetch history/
   proof/compatibility/eligibility/label per row inside an animal loop) instead of
   one bulk read per keyset page. Bounded goroutines and keyset paging do NOT help
   if each page still fans out to a query per animal — at 1M animals that is 1M
   round-trips. Require: history/proof/compatibility reads are **bulk per page,
   not N+1 per animal**, and per-run lookups (e.g. active protocol version) are
   cached per park/run, not re-queried per animal
   (`docs/protocol-engine/high-scale-kernel-validation-plan.md`). The most-missed
   shape here is the **nested / cross-boundary fan-out** ("N+2"): the in-loop call
   is not a raw `.Query/.Exec` but a ctx-taking call to an injected I/O dependency
   (repo/reader/port/client/roster/ownership) whose driver call is one adapter
   layer down. The raw-driver `n-plus-one` guard cannot see it — `make scale-guard`
   catches it as the separate `n-plus-one-fanout` rule (baselined offenders in
   `tools/scale-guard/baseline.txt`; full definition in
   `docs/decisions/scale-anti-patterns.md`). When reviewing a loop, follow the
   in-loop call INTO its adapter: a per-item service/port call that reads the DB
   one row at a time is the same defect as an inline N+1 and must batch to a single
   `*ByIDs` / `= ANY($1)` read.

## Read-model default & the scoped scale-guard exemption (5k-50k)

Default read model under the 5k-50k envelope: screens read canonical indexed
SQL, not derived projections. Per
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the two read shapes
are distinct and must not be conflated under the word "bounded":

- **List reads** (day list, shed list, per-shed capture) are keyset-paginated at
  ~20 rows and are genuinely bounded regardless of herd size.
- **Summary aggregates** (Control Tower gaps, adherence rollups,
  process-integrity counts) cannot be keyset-paginated; they are indexed scans
  whose cost grows with open-obligation count. Acceptable at this envelope, but
  the first extraction candidate, and they MUST be plan-tested against the
  ~500k upper-bound row count, not only the 5k list case.

Only these five named screen reads default to canonical SQL and are exempted
from the seven scale anti-patterns above: `calendar_event_projections`,
`process_integrity_projection_rows`, `vaccination_shed_projection_rows`,
`vaccination_execution_projection_rows`, and
`vaccination_operations_projection_rows` (0 projectors/tables in the active
runtime). `vaccination_eligibility_rollups` and the counts summaries remain
durable read models. **compute-on-read stays banned everywhere else.**

The exemption is machine-scoped, never blanket. `make scale-guard` (the CI
`guardrails` job) still blocks the compute-on-read/god-CTE shape mechanically;
the five reads clear it only through the sanctioned scoped annotation
`// scale-guard:ignore: 5k-50k-envelope; see operational-kernel-5k-50k-scale-envelope.md`
on each read (plus a `tools/scale-guard/baseline.txt` entry where required) AND
a passing query-plan test at both list and aggregate shapes. Review checkpoints
for any change touching one of these five reads:

- [ ] The read carries the scoped `scale-guard:ignore: 5k-50k-envelope`
      annotation citing the ADR — not a blanket guard disable, not a bare
      `scale-guard:ignore` with no envelope tag
- [ ] The exempted read is query-plan-tested at BOTH the list (~20-row keyset)
      and the aggregate shapes against the ~500k upper-bound row count; an
      exempted read that is not plan-tested is a defect
- [ ] The guard remains fully active for every other path in
      `backend/internal/**`; no global disable, no widening the exemption beyond
      the five named screens
- [ ] A screen that later earns its own projection drops the annotation and
      returns under full guard enforcement (see the scale-out ladder in the ADR)

## Business audit vs technical logs

Business audit rows and domain status/event ledgers are product truth. Structured
logs, metrics, traces, and panic logs are engineering diagnosis. Do not accept a
technical log line, metric, or DLQ counter as the business audit record for a
state transition, proof decision, exception, assignment, snooze/nudge, deadline
change, policy publish, or replay/discard action.

Review checkpoints:
- [ ] Mutating product actions decide explicitly which durable product record is
      written: business audit row, domain status/event ledger row, outbox event,
      or all of them. The decision is not replaced by `log.Info` / `log.Warn`.
- [ ] Routine read-only views, polling requests, hovers, and page views do NOT
      flood business audit rows unless product policy explicitly requires access
      review. They may produce technical access logs/metrics.
- [ ] Operations Audit surfaces durable audit/status/proof history, not raw
      engineering logs.

Migration hygiene at scale: for populated hot tables, require a no-lock rollout:
`CREATE INDEX CONCURRENTLY` / `DROP INDEX CONCURRENTLY` in `-- +goose NO
TRANSACTION` sections; add `CHECK`/`FOREIGN KEY` constraints as `NOT VALID` and
validate separately; attach uniqueness/primary keys via `USING INDEX`; do not add
direct `UNIQUE`, `PRIMARY KEY`, `EXCLUDE`, `CHECK`, or `FOREIGN KEY` constraints
to populated hot tables without an explicit reviewed rollout. Run
`make validate-hot-index-migrations` and `make validate-migrations` for migration
changes, plus `make validate-sqlc-plans` for hot queries.

High-scale certification gates (the FUTURE 1-5M gate, not the current release
bar): the 1M full-chain acceptance targets remain the certification gate for the
future multi-worker topology, per
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. They are NOT the
5k-50k release bar — the current release proves the query-plan/backlog shape at
the ~500k upper-bound row count under the single kernel worker. A kernel/scale
change that touches the future high-scale path must still either run or clearly
mark not-applicable/not-implemented for `make high-scale-kernel-e2e-data`,
`make high-scale-kernel-e2e-all`, or strict
`make high-scale-kernel-e2e-certification`, and reports must list measured
threshold, pass/fail, and evidence for each relevant case. (These are the real
high-scale targets in the `Makefile`; do not invent others.) Do not cite a 1M
certification result as the 5k-50k release bar, and do not treat the 5k-50k
release as satisfying the future 1M certification — keep the two gates distinct.

Do not let a report overclaim its certification level. Local `make
high-scale-kernel-e2e-*` output is local behavior proof unless the report also
proves the staging floor from
`docs/protocol-engine/high-scale-kernel-validation-plan.md`. A `target_eval`
run proves target evaluation/page/cache/query shape only; it is NOT 1M
full-chain proof, production readiness, or cloud readiness. A full-chain/staging
claim must name the fixed benchmark profile (currently the `goatos-stg`
1M profile in `docs/runbooks/google-cloud-environments.md`, verify the current
id there), canonical command/equivalent invocation, git SHA, migration/fixture/
dataset checksums, environment shape, stage counters, predeclared thresholds,
pass/fail rows, and evidence links. Load-test evidence must use realistic skew,
never write-load-test prod, and never ship graphs without pass/fail verdicts.

Additional hard scale checks reviewers must name when touched:
- [ ] **Tenant-fair / noisy-neighbor** behavior for workers and relays: one huge
      tenant or park cannot starve quiet tenants; claiming is fair (round-robin /
      per-tenant caps / partitioned queues), and quiet tenants still make
      progress under skewed load
- [ ] **Concurrent-claim safety.** Work claims use `FOR UPDATE SKIP LOCKED`, a
      lease/advisory-lock single-flight, or another double-claim-safe pattern —
      **bare `FOR UPDATE` is not sufficient** (it serializes, it does not prevent
      two instances from each promoting/claiming/reserving a disjoint-but-
      overlapping set). Two sweeper/relay/dispatcher instances running at once
      must never double-promote, double-claim, or double-reserve. Stale/expired
      claims (crashed worker holding a lease) are reclaimable by a reaper
- [ ] **Consumer-side redelivery + out-of-order dedupe (Pub/Sub is at-least-once).**
      Every NEW event consumer records processed-event identity (see the
      processed-events table's `CREATE TABLE` migration + composite PK; do not
      assume the name — locate it) and is a no-op on redelivery AND on
      out-of-order arrival. Ack only after durable handling. A consumer that
      applies an effect per delivery without a dedupe guard is a HIGH finding
- [ ] Pub/Sub / event consumers configure **flow control, ack deadlines, retry
      backoff with jitter, and circuit-breaker/open states**; retry is bounded;
      DLQ replay/discard is an explicit audited operator/repair action with zero
      duplicate business effects
- [ ] **Outbox relay-lag / backlog-age SLO.** Outbox and worker backlog expose an
      oldest-unsent / oldest-unprocessed **age** metric with an alert threshold —
      not just durable rows and not just a raw counter. A backlog that grows
      unbounded with no age SLO is silent drift
- [ ] **Idempotency-key table GC / retention / bloat.** Idempotency scopes have a
      replay window, expiry/retention policy, and bounded index growth; new
      namespaces are covered by the `idempotency-key-sweeper` cron and by the
      `domain-event-processed-sweeper` for processed-event rows
- [ ] **Partition rollover / retention + hot-set archive.** Partition
      rollover/coverage is maintained (`partition-maintainer` /
      `goatos_ensure_partition_coverage`); closed/cold history is archived out of
      hot read paths and does not accumulate in the hot set
- [ ] **Backfill-vs-live race.** Backfills/imports cannot race live kernel writes;
      an optimistic lock / `row_version` (or other convergent-repair) protects a
      row that both a backfill and a live handler may touch. Backfill never
      clobbers a newer live write
- [ ] **Durable run/progress ledger.** Long-running generation, backfill, import,
      projection rebuild, and large reconciliation jobs persist run/progress rows
      with run id, idempotency key, attempt, heartbeat, page/cursor/shard state,
      stale reclaim, completion, and failure state before claiming high-scale or
      parallel safety. Crash/retry resumes bounded work without duplicate effects
      or full-run-only replay unless the review explicitly accepts that limitation
- [ ] **Saga partial-failure / compensation.** Multi-step flows (procurement
      intake, drive execution, booster chains, drive→proof→completion) record
      partial-failure state and compensation/repair on mid-step failure: no
      orphaned reservations, obligations, proof rows, half-applied fanout, or
      dangling batch membership. Apply steps are idempotent so a resumed saga
      converges; a compensating action exists for each committed step
- [ ] **Timezone / calendar-day correctness** — see the dedicated section below
- [ ] Scheduler/job wiring is present in every claimed environment (`dev`,
      `stg`, `prod`) or the review explicitly says which environments are not
      production-ready yet; local/dev cron proof is not prod parity

## Timezone / calendar-day correctness (CRITICAL — both audits)

All date/deadline math must resolve in the **animal's location timezone**, not
raw server UTC. Goat OS currently operates on the IST business calendar by
default (`Asia/Kolkata` through `locations.timezone`; verify the current default
in migrations/config). A day-rollover or DST bug here silently marks an animal
missed, recovers it on the wrong calendar day, or plans a drive a day off, and
none of it throws.

Regression class to check against: obligation sweepers, vaccination generation,
Counts/Feed target dates, Calendar reminders, FEFO stock validity, and
user-facing read APIs must route business-day decisions through
`backend/internal/platform/biztime` or an explicit location timezone. Any
date/deadline decision that reaches a user-facing calendar day MUST convert
through the location zone before comparing. Raw UTC may appear only for
non-calendar instants such as audit/event storage, retention cutoffs, and
deterministic event/idempotency keys, not for medical/business day decisions.

Review checkpoints (confirm each when a change touches date/deadline math):
- [ ] `due_at`, `window_end`, "sweeper today", and missed-marking compare
      calendar days in the **location timezone** (IST by default), never raw
      `UTC().Date()` / server-date bucketing
- [ ] The **7-day recovery rejoin** window and any **batching/hold** window are
      counted in location-local calendar days; a recovery late on a local day
      does not slip to the next day because the server was already past midnight
      UTC
- [ ] **DST and day-rollover** are handled: a deadline near local midnight, or in
      a zone with DST, does not cross a calendar-day boundary by accident
- [ ] **Recovery-date-vs-server-date** never crosses a day boundary: the reopened
      obligation is dated from the animal-local/IST recovery day, not the
      server's UTC day
- [ ] Tests exercise a location whose local day differs from the UTC day at the
      moment of evaluation (e.g. late-evening `Asia/Kolkata`)

## SOLID / generic-engine review

The obligation engine (`backend/internal/obligation/`) is generic and data-driven
— it powers vaccination today and feed-direction / future domains without core
changes. When reviewing a new domain or rule, confirm it **extends via
configuration, not by editing the engine core**:

- New domain adds: protocol rules in `backend/internal/protocol/` (DSL), a sweeper
  strategy/config, an event handler in the module's `app/*_handler.go`, and a read
  model — with no change to obligation-engine internals.
- Notification channels extend by implementing the `NotificationGateway` port
  (`backend/internal/notification/ports/`) with a new adapter — no core change.
- Proof/verification extends by declaring a domain proof policy — the generic
  accept/reject/rework engine (`backend/internal/sop/`, `backend/internal/proof/`)
  is unchanged.

Red flag: a "new domain" that copies the obligation/sweeper/proof machinery into
its own module instead of configuring the generic engine. That is a
duplicate-engine violation — push it back onto the shared kernel.

## Defer-recovery re-entry & reconciliation (healing) review

Two failure modes matter as much as the happy path: (a) an animal that came back
from a safety block never gets re-scheduled, and (b) the event path silently
dropped work and nothing ever heals the drift. Review both explicitly.

### Recovery re-entry — no goat left alone more than 1 week

When an animal exits a defer state (sick / under treatment / ICU / quarantine /
pregnancy months 4-5 / post-breeding hold), the missed vaccination obligation
must be reopened **from the recovery date**, and the planner must rejoin it to
the nearest compatible same-park drive **within 7 calendar days of recovery**; if
none is that close, create a micro-drive. No recovered animal is left waiting more
than a week. (Rule source: `docs/preventive-care-vaccination/vaccination-rules.md`,
`docs/protocol-engine/obligation-engine.md`. Code: `backend/internal/obligation/app/sweeper.go`,
`backend/internal/vaccination/app/generation.go` / `generation_handler.go`,
`backend/internal/obligation/adapters/postgres/recovery_cancel_integration_test.go`.)

Review checkpoints (all must hold):
- [ ] Exit-of-defer emits a recheck/recovery event (or is swept), and reopens the
      obligation dated from recovery — not from the original stale due date, not
      at the pre-move shed. The `deferred → scheduled` transition is a legal
      state-machine edge (see legal-status review)
- [ ] Re-entry is **idempotent**: recovering, or the sweeper running, twice does
      NOT create duplicate obligations/drive memberships (deterministic key +
      duplicate-spawn guard)
- [ ] The ≤7-calendar-day rejoin/micro-drive SLA is enforced or measured — there
      is a code path (or reconciler) that guarantees a recovered animal joins a
      drive within a week (counted in **location-local** days, per the timezone
      section), and a metric/alert when one is left longer
- [ ] Recovery emits a `missed → recovered` status event into the obligation
      status-events ledger (auditable), and consumes/releases any stale
      reservation from the missed cycle
- [ ] Retry-safety: a redelivered recovery event, or a re-run sweeper, converges
      to the same state (tests cover first event, exact replay, and
      recover-while-already-reopened)

### Reconciliation / healing sweepers — catch what the event path dropped

The event path (trigger → obligation → drive) is the primary route, but events
can be lost, arrive out of order, or race a state change. Every operational
invariant therefore needs EITHER an event-path guarantee OR a periodic,
**idempotent, bounded, tenant/date-scoped** reconciler that heals drift and is
safe to run repeatedly (it converges, never double-acts). Reconcilers must be
observable — emit a **metered mismatch counter AND an alert/SLO on it**. A
metric no one alerts on is still silent drift: require both the counter and a
threshold/alert that fires when mismatches exceed expected steady-state.

**Existing healing crons to model new ones on** (each verified present in
`backend/cmd/`; re-run `ls backend/cmd` before citing — the list drifts):
`obligation-sweeper` (due promotion + mark-missed + recovery reopen),
`domain-event-processed-sweeper`, `idempotency-key-sweeper`,
`inventory-batch-reconciler`, `counts-mismatch-scan` /
`counts-projection-recompute` / `counts-source-parity-check`,
`location-profile-coverage-check`, `calendar-reminder-sweeper` /
`calendar-escalation-sweeper` / `calendar-vaccination-projector`,
`outbox-relay` + `outbox-dlq`, `sop-review-fanout-retry`, `partition-maintainer`.

Do NOT model on or assume these — they are NOT built as binaries: there is no
`in-progress-timeout` reconciler and no `drive-membership` reconciler in
`backend/cmd/`. `device-gateway`, `goatos-api`, and `sweeper` are stub
directories (`.gitkeep` only), not runnable crons. If a review needs one of the
unbuilt reconcilers, flag it as **required-but-unbuilt**, do not assume it ships.

Mismatch classes a reviewer confirms are covered (event guarantee OR reconciler):

| Drift | Heal path to require |
|---|---|
| Recovered from defer but not rescheduled >7d | defer-exit recheck / recovery sweeper (see above) |
| Animal due but no obligation row (trigger missed) | generation backfill/reconcile against eligibility |
| Obligation open but animal exited/sold/dead/shifted | stale-cancel / re-scope on state change |
| Drive/batch count ≠ actual eligible animals (missing/extra) | drive-membership reconciliation — **planner brain, NOT built as a cron; verify before relying on it** |
| Deadline passed but status never set `missed` | `obligation-sweeper` mark-missed |
| Stuck `in_progress` / abandoned assignment | timeout reconciler → reopen/reassign — **no `in-progress-timeout` binary exists yet; mark required/unbuilt, do not assume one ships** |
| Orphaned stock reservation (batch canceled, not released) | `inventory-batch-reconciler` |
| Outbox event never delivered / consumer lag | `outbox-relay` + `outbox-dlq` |
| Read model / projection ≠ source | projection-recompute + parity-check |

### Scan-day execution reconciliation

Drive execution day is where physical reality diverges from the planned batch,
and every divergence needs a durable, idempotent heal path — not an operator
eyeballing a list. When reviewing execution/scan-day code, confirm each class is
detected and reconciled (missing → re-plan/defer; extra → attach or reject;
proof gaps → rework; stock/cold-chain → block + escalate), and that re-running
the reconciliation is a no-op:
- [ ] **Missing animals** — planned but not scanned: obligation stays open / is
      re-planned or deferred, never silently dropped
- [ ] **Extra animals** — scanned but not in the batch: attached with an
      obligation or rejected with a reason, not silently absorbed
- [ ] **Shifted animals** — moved shed/park since planning: re-scoped, not
      double-counted at the stale location
- [ ] **Unreadable tags** — RFID/old-tag unreadable: routed to a manual-resolve
      queue, not counted as complete
- [ ] **New defer states on the day** — sick/ICU/quarantine discovered at scan:
      moved to `deferred` (recovery re-entry applies), not marked missed
- [ ] **Death / sale on the day** — obligation `canceled`/re-scoped; no orphaned
      reservation or half-applied fanout
- [ ] **Proof rejection** — rework path fires; completion not recorded on a
      rejected proof
- [ ] **Stock shortfall** — insufficient vaccine batch: blocks completion and
      escalates, does not mark done anyway
- [ ] **Cold-chain failure** — temperature/excursion breach: batch blocked and
      escalated, proof records the failure
- [ ] Re-running the scan-day reconciliation converges (idempotent), and counts
      reconcile (planned = completed + deferred + canceled + carried-over)

If a change adds a new operational invariant with no event guarantee and no
reconciler, that is a HIGH finding — a mismatch will accumulate silently. If you
need a NEW cron, prefer a small dedicated reconciler over widening an existing
sweeper's scope, and make it idempotent + bounded + metered + alerted from the
start.

## Kernel review checklist

- [ ] Feature emits into the kernel chain; no private scheduler/status/proof/notify engine
- [ ] Canonical state + audit + outbox written in ONE transaction
- [ ] Process status transitions flow through events/sweeper; no direct status write that skips the flow
- [ ] Status literals match the latest obligation migration's `CHECK` set; transitions are legal; terminal states immutable (correction = new work)
- [ ] Postgres owns future due work; Cloud Tasks only near-term; frontend owns no canonical state
- [ ] No cross-module table writes — owning module's service/port only
- [ ] Idempotency key + fingerprint persisted in the write txn; `UNIQUE (tenant_id, idempotency_key)`; replay-safe; tests cover replay cases
- [ ] Work claims are double-claim-safe (`FOR UPDATE SKIP LOCKED` / lease / advisory), NOT bare `FOR UPDATE`; stale claims reclaimable
- [ ] Every new event consumer dedupes at-least-once redelivery + out-of-order via a processed-event identity; acks only after durable handling
- [ ] Sweepers/queries bounded: tenant/date filters, indexed, cursor resume, `LIMIT`, keyset pagination
- [ ] No unbounded goroutines / full-herd in-memory loads
- [ ] No N+1 per-animal queries: history/proof/compatibility/label reads are bulk per keyset page, not one round-trip per animal; per-run lookups cached per park/run
- [ ] Hot-path DB/migration changes have indexed access + `make validate-sqlc-plans`
- [ ] Scale/staging proof for hot reads includes seeded `ANALYZE` +
      `EXPLAIN (ANALYZE, BUFFERS)` without `enable_seqscan=off`, not only static
      plan/index lint
- [ ] Projection-backed APIs expose freshness/serving-state/source-watermark
      envelopes and label stale or approximate data; the five 5k-50k canonical
      screen reads instead carry the scoped `scale-guard:ignore: 5k-50k-envelope`
      annotation + a list-and-aggregate query-plan test at the ~500k upper bound,
      and `compute-on-read` stays banned everywhere else
- [ ] Long-running generation/backfill/import/projection jobs have durable
      run/progress rows with heartbeat, cursor/page/shard resume, stale reclaim,
      and completion/failure state
- [ ] Migration changes on populated hot tables run `make validate-hot-index-migrations` + `make validate-migrations`
- [ ] Current 5k-50k changes prove the query-plan/backlog shape at the ~500k
      upper-bound row count under the single kernel worker; the FUTURE 1-5M
      `make high-scale-kernel-e2e-*` certification is run or explicitly reported
      only when the change touches the future high-scale path — the two gates are
      distinct, and a 1M cert is not the 5k-50k release bar; 1M/full-chain claims
      still distinguish local behavior proof vs `target_eval` vs staging
      `full_chain` and include the benchmark profile, per-stage counters,
      thresholds, and pass/fail evidence
- [ ] Durable status persisted by sweeper; read-time compute only for display derivation
- [ ] Date/deadline math (due/window/recovery/batching/missed/"today") resolves in the location timezone, not raw UTC; DST/day-rollover safe
- [ ] Partition rollover/retention maintained; cold history archived out of hot paths; idempotency/processed-event tables have GC/retention
- [ ] Backfills/imports cannot clobber live writes (optimistic lock / row_version)
- [ ] Pub/Sub flow control, ack deadline, bounded+jittered retry, DLQ, circuit-breaker states reviewed
- [ ] Multi-step sagas have compensation/repair; no orphaned reservations/obligations/proof/fanout on mid-failure
- [ ] Scan-day execution reconciles missing/extra/shifted/unreadable/defer/death/sale/proof-reject/stock/cold-chain, idempotently
- [ ] Reconciler mismatch metrics are alerted (SLO), and outbox/backlog expose an oldest-unsent age SLO — a counter alone is not enough
- [ ] Tenant-fair claiming; a huge tenant/park cannot starve quiet ones
- [ ] New domain extends the generic engine by config, not by copying it
- [ ] Escalations/reminders are durable `notification_requests` via `NotificationGateway`, not logs
- [ ] Critical-action corrections use void/reversal/audit records, not hidden
      delete-and-recreate; destructive deletes have an explicit lifecycle rule
      and preserve business history
- [ ] Recovered-from-defer animals reopen from recovery date and rejoin a drive within 7 (location-local) days (else micro-drive); re-entry is idempotent and emits a status event
- [ ] Every operational invariant has an event-path guarantee OR an idempotent, bounded, metered+alerted reconciler; no silent-drift path with neither; unbuilt reconcilers flagged required, not assumed
