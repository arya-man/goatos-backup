# Kernel Integrity & 1-5M-Animal Scale

The operational kernel is the core of Goat OS. Review it first and hardest. A
feature that forks its own scheduler, status store, proof flow, or notification
path — instead of plugging into the kernel — is a CRITICAL finding even if it
compiles and passes tests.

Law: `context/architecture/operational-kernel.md` (golden rule),
`context/architecture/operational-kernel-system-design.md` (system design),
`docs/protocol-engine/high-scale-kernel-validation-plan.md` (scale validation).

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
   `outbox_events` / audit rows must commit together (`BEGIN … COMMIT`). Split
   transactions lose the event or the state on failure.
3. **Direct process-status write that skips the flow.** e.g. setting
   `obligation_instances.status = 'completed'` in an app service instead of going
   through verification → completion event → sweeper/booster. Status is durable
   process truth; direct writes drop audit, skip obligations, break booster chains.
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

## 1-5M-animal scale — hard requirement on every change

`AGENTS.md` makes million-animal scale a hard requirement. For any new query,
worker, importer, reporting path, or UI data flow, check the scale shape:
tenant/run-scoped, indexed, chunked/paginated, bounded in memory/goroutines,
idempotent on retry, and query-plan-validated on large tables.

CRITICAL scale violations:

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
   idempotency_key)`. Tests must cover: first call, exact replay, same-key
   different-payload replay, downstream duplicate prevention.
5. **No query-plan validation on a hot path.** DB/migration change touching
   import/animal/event/counter rows at scale without an indexed access path and
   `make validate-sqlc-plans` coverage.
6. **Read-time process state instead of persisted.** Computing durable status
   (missed/overdue/escalation level) at read time when the sweeper should
   materialize it. Read-time compute is non-durable and inconsistent across
   queries. (Projection-level "days overdue" derived for display is fine; the
   canonical status transition must be persisted.)
7. **Dashboards/reports sliced by dimension without the projection rule.** Slicing
   by month/date/breed/farm/shed/load/status/etc. must follow
   `docs/decisions/high-scale-dashboard-projections.md` — durable projections,
   not raw scans.

Migration hygiene at scale: `CREATE INDEX CONCURRENTLY`; no `NOT NULL` without
`DEFAULT` on large existing tables; partition high-volume tables (events, audit,
history, media) by date/scope.

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
      at the pre-move shed
- [ ] Re-entry is **idempotent**: recovering, or the sweeper running, twice does
      NOT create duplicate obligations/drive memberships (deterministic key +
      duplicate-spawn guard)
- [ ] The ≤7-calendar-day rejoin/micro-drive SLA is enforced or measured — there
      is a code path (or reconciler) that guarantees a recovered animal joins a
      drive within a week, and a metric/alert when one is left longer
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
observable — emit a metric for how many mismatches were found and healed.

Existing healing crons to model new ones on (verified in `backend/cmd/`):
`obligation-sweeper` (due promotion + mark-missed + recovery reopen),
`domain-event-processed-sweeper`, `idempotency-key-sweeper`,
`inventory-batch-reconciler`, `counts-mismatch-scan` /
`counts-projection-recompute` / `counts-source-parity-check`,
`location-profile-coverage-check`, `calendar-reminder-sweeper` /
`calendar-escalation-sweeper` / `calendar-vaccination-projector`,
`outbox-relay` + `outbox-dlq`, `sop-review-fanout-retry`, `partition-maintainer`.

Mismatch classes a reviewer confirms are covered (event guarantee OR reconciler):

| Drift | Heal path to require |
|---|---|
| Recovered from defer but not rescheduled >7d | defer-exit recheck / recovery sweeper (see above) |
| Animal due but no obligation row (trigger missed) | generation backfill/reconcile against eligibility |
| Obligation open but animal exited/sold/dead/shifted | stale-cancel / re-scope on state change |
| Drive/batch count ≠ actual eligible animals (missing/extra) | drive-membership reconciliation (candidate scoring / EDF — planner brain; verify built before relying on it) |
| Deadline passed but status never set `missed` | `obligation-sweeper` mark-missed |
| Stuck `in_progress` / abandoned assignment | timeout reconciler → reopen/reassign |
| Orphaned stock reservation (batch cancelled, not released) | `inventory-batch-reconciler` |
| Outbox event never delivered / consumer lag | `outbox-relay` + `outbox-dlq` |
| Read model / projection ≠ source | projection-recompute + parity-check |

If a change adds a new operational invariant with no event guarantee and no
reconciler, that is a HIGH finding — a mismatch will accumulate silently. If you
need a NEW cron, prefer a small dedicated reconciler over widening an existing
sweeper's scope, and make it idempotent + bounded + metered from the start.

## Kernel review checklist

- [ ] Feature emits into the kernel chain; no private scheduler/status/proof/notify engine
- [ ] Canonical state + audit + outbox written in ONE transaction
- [ ] Process status transitions flow through events/sweeper; no direct status write that skips the flow
- [ ] Postgres owns future due work; Cloud Tasks only near-term; frontend owns no canonical state
- [ ] No cross-module table writes — owning module's service/port only
- [ ] Idempotency key + fingerprint persisted in the write txn; `UNIQUE (tenant_id, idempotency_key)`; replay-safe; tests cover replay cases
- [ ] Sweepers/queries bounded: tenant/date filters, indexed, cursor resume, `LIMIT`, keyset pagination
- [ ] No unbounded goroutines / full-herd in-memory loads
- [ ] Hot-path DB/migration changes have indexed access + `make validate-sqlc-plans`
- [ ] Durable status persisted by sweeper; read-time compute only for display derivation
- [ ] New domain extends the generic engine by config, not by copying it
- [ ] Escalations/reminders are durable `notification_requests` via `NotificationGateway`, not logs
- [ ] Recovered-from-defer animals reopen from recovery date and rejoin a drive within 7 days (else micro-drive); re-entry is idempotent and emits a status event
- [ ] Every operational invariant has an event-path guarantee OR an idempotent, bounded, metered reconciler; no silent-drift path with neither
