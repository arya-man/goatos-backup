# Decisions

Use this folder for future ADRs when a decision changes the locked architecture.

Do not rewrite history silently. If a frozen decision changes, add an ADR and
link it from `context/README.md`.

Active ADRs:

- `docs/decisions/go-backend-stack.md` - Go backend stack: net/http or chi,
  pgx + sqlc-style typed SQL, goose-style SQL migrations, explicit wiring, no
  ORM/DI container by default, no Phase 1 gRPC/protobuf.
- `docs/decisions/calendar-ownership.md` - Calendar owner taxonomy and event
  admission rules: only dated human actions become Calendar events; stable
  owner keys, including the `all` filter and reserved `sales_commerce` key;
  vaccination Calendar scope for the current Preventive Care (PC) slice; system crons excluded
  unless they raise human work.
- `docs/decisions/observability.md` - Observability and logging: single
  `platform/observability` logger seam, env-selected sink
  (`stdout_json`/`otlp`/`gcm`, OTLP over HTTP), log-once-at-boundaries,
  recover-and-log panics; goat identifiers are business data (log them), only
  secrets are redacted.

- `docs/decisions/notification-delivery.md` - Notification & event delivery on
  GCP (the "we used SQS" answer): transactional Postgres outbox → outbox relay →
  Pub/Sub → Cloud Tasks (near-term dispatch) + Cloud Scheduler (sweeper) →
  notification delivery queue → replaceable multi-channel gateway
  (Slack/email/FCM/incident); idempotent, lease-based, DLQ; Postgres is the
  calendar, the queue is transport.
- `docs/decisions/fcm-device-lifecycle.md` - FCM registration-token lifecycle:
  couple on launch/login, decouple on logout; store only a token hash on
  `workforce_member_devices`; backend contract shipped, mobile SDK wiring TODO
  gated on the `goatos-prod` Firebase project.

Pending sign-off ADRs:

- `docs/decisions/vaccination-notification-rules.md` - Proposed vaccination
  notification rule layer on top of the built pipeline: the reminder cadence
  ladder (advance notice at D-7, daily reminders, due-today), park-scoped vs
  all-park-leadership audience resolution, the overdue→missed escalation ladder
  (PHC SLAs), a reusable declarative `notification_policy` framework future
  obligation features plug into, and the channel roadmap (FCM now; SMS/email/
  WhatsApp/voice/Slack documented).
- `docs/decisions/vaccination-work-session-bundle.md` - Proposed vaccination
  work-session grouping for combo/bundle drives: promote the existing
  scope/session batch key above per-vaccine batches, keep per-vaccine
  obligations, split oversized sessions by daily administration capacity,
  expose the cap in the same config UI with explanatory help, require matrix
  submissions and per-cell completion idempotency, publish the expanded E2E
  story report, and reconcile the older "batch is the drive/work unit" decision
  before code.
