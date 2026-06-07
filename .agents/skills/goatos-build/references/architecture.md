# Architecture Reference

Load this when touching Goat OS contexts, module boundaries, backend structure,
ports/adapters, devices, R&D, AI authority, or canonical state.

Canonical docs:

- `context/architecture/final-architecture.md`
- `context/agents/ai-agent-context-and-protocols.md`
- `context/execution/next-contracts.md`
- `context/product/glossary.md`
- `docs/decisions/go-backend-stack.md`
- `docs/phases/phase-01-goat-passport/PRD.md`
- `docs/phases/phase-01-goat-passport/TRD.md`

Rules:

- Backend is a Go modular monolith with strict internal module boundaries.
- Backend implementation stack is locked in `docs/decisions/go-backend-stack.md`:
  use net/http or chi for HTTP adapters, pgx + sqlc-style typed SQL for
  Postgres adapters, goose-style plain SQL migrations, and explicit constructor
  wiring. Do not add Gin, GORM/ORM, runtime DI containers, direct client gRPC,
  or protobuf in Phase 1 without a new ADR.
- Modules own tables and expose defined interfaces.
- Cross-context propagation uses contracts, outbox, Pub/Sub, and APIs.
- Phase 1 identity foundation uses tenants for isolation, global parties for
  actors, shared-PK org subtype rows, owner_party/custodian_party separation,
  temporal ownership/custody ledgers, and locations as separate physical
  entities. Do not reintroduce `owning_farm_id` as identity/ownership truth.
- Ownership, custody, task assignment, and location are four different things:
  owner_party_id owns value, custodian_party_id is responsible/holding party,
  workforce assignment says who does today's work, and current_location_id says
  where the goat physically is.
- Status is decomposed into lifecycle, reproductive, growth_cohort,
  management_stage, health, plus sex. Legacy compound labels are raw evidence,
  not the canonical model.
- Identity merge is redirect-based: merged goats keep `merged_into_goat_id`
  pointing at the live survivor goat. Lookup APIs may return redirect warnings;
  normal writes to merged goats fail except admin correction/unmerge. Later
  booking, allocation, and replacement flows must resolve the supplied goat
  through `merged_into_goat_id` before availability/uniqueness checks and enforce
  no-double-promise against the survivor `goat_id`.
- Million-goat scale is a hard design rule: chunk by park/shed/cohort/date,
  never full-herd scan in an API, use idempotency, bounded workers/goroutines,
  and indexed/partition-aware tables.
- High-volume histories/events/audit tables are partition-aware. Idempotency
  lives in explicit idempotency-key records, not in client hope.
- Workforce ops is a core Goat OS engine, not payroll HRMS: every task needs
  owner, scope, due time, backup path, escalation path, absence/backfill, and
  audit history.
- Backfill selection must be deterministic: skill match, park/shed/cohort
  scope, current load, shift conflict, task risk, notify assignee and park
  head, and escalate if no qualified backup exists.
- Media must use signed direct upload/download; API never proxies video bytes.
- Proof/media/verification is a platform engine shared by health, vaccination,
  birth, shifting, feed, procurement, attendance, death, and dispatch.
- Observability is required for new APIs/workers: p95/p99 latency, error rate,
  DB pressure, outbox/PubSub/DLQ lag, media failures, and analytics scan cost.
- Promise safety is continuous: P8/P4 must use scheduled and event-triggered
  sweepers to re-run readiness for open bookings until dispatch/exit and create
  remediation tasks on state changes.
- Crop/fodder/farmer workflow is real legacy scope. Build it only if Goat OS
  owns feed production; otherwise define an integration boundary into feed
  inventory/cost analytics.
- AI may propose, classify, summarize, or triage. It must not silently create
  canonical truth.
- High-risk actions need deterministic rules, evidence IDs, and review gates.

Before editing architecture, check for drift against existing context docs.
