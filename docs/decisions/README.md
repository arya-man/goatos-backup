# Decisions

Use this folder for future ADRs when a decision changes the locked architecture.

Do not rewrite history silently. If a frozen decision changes, add an ADR and
link it from `context/README.md`.

Active ADRs:

- `docs/decisions/task-timing-alerting-violations-and-appeals.md` - Accepted
  timing and accountability policy: planned/available/flexible/hard/clinical
  clocks are separate; Vaccination drives and Weighing have an accepted two-day
  carry-forward band; alerts use an acknowledgement-gated named-person
  waterfall; a hard breach becomes an employee violation only after attribution,
  notice, appeal, and Director decision; HR action is separate and the task
  kernel never changes payroll.

- `docs/decisions/operational-task-kernel-non-deviation.md` - Accepted
  governing decision: every operational module participates in one shared,
  event-driven task/ticketing waterfall. Older module-specific task, scheduler,
  alert-feed, owner, clock, escalation, and verification decisions are
  compatibility sources only where they conflict; strict modules such as
  Weighing integrate outward by durable events without an inbound execution
  dependency.

- `docs/decisions/operational-kernel-5k-50k-scale-envelope.md` - Accepted
  5,000-to-50,000-animal deployment envelope: replace the 17 scheduled Cloud Run
  Job fleet with one HA modular kernel worker, drop disposable projection
  tables/test data, serve canonical keyset+aggregate SQL (with an explicit
  scale-guard reconciliation), and add projections/workers back one measured
  hotspot at a time. Narrows the one-million-scale docs to future work.

- `docs/decisions/one-million-postgres-readiness.md` - PostgreSQL readiness
  contract for one-million scale: explicit workload connection budgets,
  query-shape indexing and scaled planner proof, behavior-driven partitioning
  and retention, incremental projection maintenance, conditional PgBouncer,
  and the remaining certification boundary.
- `docs/decisions/go-backend-stack.md` - Go backend stack: net/http or chi,
  pgx + sqlc-style typed SQL, goose-style SQL migrations, explicit wiring, no
  ORM/DI container by default, no Phase 1 gRPC/protobuf.
- `docs/decisions/calendar-ownership.md` - Calendar owner taxonomy and event
  admission rules: only dated human actions become Calendar events; stable
  owner keys, including the `all` filter and reserved `sales_commerce` key;
  vaccination Calendar scope for the current Preventive Care (PC) slice; system crons excluded
  unless they raise human work.
- `docs/decisions/proof-capture-authorization.md` - Proof/evidence capture is
  authorized by the SAME execution right that authorizes the work it proves,
  never by another vertical's task permission; widen the `/app/proofs` ROUTE via
  `AnyPermissions`, never hand a module role the broad `task.execute`. Enforced
  by `make proof-capture-authorization-guard`.
- `docs/decisions/observability.md` - Observability and logging: single
  `platform/observability` logger seam, env-selected sink
  (`stdout_json`/`otlp`/`gcm`, OTLP over HTTP), log-once-at-boundaries,
  recover-and-log panics; goat identifiers are business data (log them), only
  secrets are redacted.

- `docs/decisions/notification-delivery.md` - Notification & event delivery on
  GCP (the "we used SQS" answer): transactional Postgres outbox → outbox relay →
  Pub/Sub → consolidated kernel-worker cadence stages → durable Postgres
  notification queue → replaceable multi-channel gateway
  (Slack/email/FCM/incident); idempotent, lease-based, exhausted/DLQ evidence;
  Postgres is the calendar. Cloud Tasks/Scheduler are future scale-out adapters,
  not the current normal topology.
- `docs/decisions/fcm-device-lifecycle.md` - FCM registration-token lifecycle:
  couple on launch/login, decouple on logout; store only a token hash on
  `workforce_member_devices`; backend and mobile plumbing exist, while deployed
  credential/device reachability still requires exact-environment proof
  gated on the `goatos-prod` Firebase project.
- `docs/decisions/stale-binary-migration-drift-guard.md` - Stale-binary
  migration-drift guard: `internal/platform/migrationguard.Check` fails
  `cmd/api` startup and `/readyz` fast in EITHER direction (DB ahead of the
  binary's embedded migrations, or binary ahead of an unmigrated/behind DB);
  `GET /version` exposes build SHA + both migration levels + a drift flag.
  CI/CD build-arg wiring for the build SHA is a deliberate follow-up.

Pending sign-off ADRs:

- `docs/decisions/vaccination-notification-rules.md` - Partially implemented
  Vaccination compatibility catalog: the D-7/D-6..D0 reminder cadence exists,
  while legacy due-age escalation still needs replacement by the shared
  named-person run/step/contact waterfall. The accepted drive policy is D+1/D+2
  flexible carry-forward capped by exact animal clinical latest-safe time; it
  does not create a personal violation. FCM is the intended first named-person
  channel; SMS/WhatsApp/voice remain unbuilt.
- `docs/decisions/vaccination-work-session-bundle.md` - Proposed vaccination
  work-session grouping for combo/bundle drives: promote the existing
  scope/session batch key above per-vaccine batches, keep per-vaccine
  obligations, split oversized sessions by daily administration capacity,
  expose the cap in the same config UI with explanatory help, require matrix
  submissions and per-cell completion idempotency, publish the expanded E2E
  story report, and reconcile the older "batch is the drive/work unit" decision
  before code.
