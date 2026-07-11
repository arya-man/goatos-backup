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

Pending sign-off ADRs:

- `docs/decisions/vaccination-work-session-bundle.md` - Proposed vaccination
  work-session grouping for combo/bundle drives: promote the existing
  scope/session batch key above per-vaccine batches, keep per-vaccine
  obligations, split oversized sessions by daily administration capacity,
  expose the cap in the same config UI with explanatory help, require matrix
  submissions and per-cell completion idempotency, publish the expanded E2E
  story report, and reconcile the older "batch is the drive/work unit" decision
  before code.
