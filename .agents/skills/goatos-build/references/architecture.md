# Architecture Reference

Load this when touching Goat OS contexts, module boundaries, backend structure,
ports/adapters, devices, R&D, AI authority, or canonical state.

Canonical docs:

- `context/architecture/final-architecture.md`
- `context/architecture/operational-kernel.md`
- `context/agents/ai-agent-context-and-protocols.md`
- `context/execution/next-contracts.md`
- `context/product/glossary.md`
- `docs/decisions/go-backend-stack.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/preventive-care-vaccination/TRD.md`

Rules:

- Backend is a Go modular monolith with strict internal module boundaries.
- Backend implementation stack is locked in `docs/decisions/go-backend-stack.md`:
  use net/http or chi for HTTP adapters, pgx + sqlc-style typed SQL for
  Postgres adapters, goose-style plain SQL migrations, and explicit constructor
  wiring. Do not add Gin, GORM/ORM, runtime DI containers, direct client gRPC,
  or protobuf for browser/mobile product clients without a new ADR.
- Modules own tables and expose defined interfaces.
- Cross-context propagation uses contracts, outbox, Pub/Sub, and APIs.
- The operational kernel is the golden rule for every feature: business event
  -> canonical transaction -> audit/outbox -> trigger evaluation -> obligation
  or work item -> sweeper/scheduler/reminder -> notification/escalation -> proof
  and verification -> read model answering whether the process was followed and
  where it broke.
- The identity foundation uses tenants for isolation, global parties for
  actors, shared-PK org subtype rows, owner_party/custodian_party separation,
  temporal ownership/custody ledgers, and locations as separate physical
  entities. Do not reintroduce `owning_farm_id` as identity/ownership truth.
- Ownership, custody, task assignment, and location are four different things:
  owner_party_id owns value, custodian_party_id is responsible/holding party,
  workforce assignment says who does today's work, and current_location_id says
  where the herd animal physically is.
- Status is decomposed into lifecycle, reproductive, growth_cohort,
  management_stage, health, plus sex. Legacy compound labels are raw evidence,
  not the canonical model.
- Identity merge is redirect-based: merged herd animals keep
  `merged_into_animal_id` pointing at the live survivor animal. Lookup APIs may
  return redirect warnings; normal writes to merged animals fail except admin
  correction/unmerge. Later booking, allocation, and replacement flows must
  resolve the supplied animal through `merged_into_animal_id` before
  availability/uniqueness checks and enforce no-double-promise against the
  survivor `animal_id`.
- Million-animal scale is a hard design rule: chunk by park/shed/cohort/date,
  never full-herd scan in an API, use idempotency, bounded workers/goroutines,
  and indexed/partition-aware tables.
- Google equivalents stay behind ports/adapters: Pub/Sub for event bus, Cloud
  Tasks for near-term timers/retries, Cloud Scheduler plus Cloud Run Jobs for
  sweepers, Cloud SQL/Postgres for canonical truth, GCS for media, Cloud
  Monitoring/Error Reporting and alert webhooks for incident-style escalation,
  and Redis/Memorystore only as cache/lease acceleration, never truth.
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
- AI may propose, classify, summarize, or triage. AI-authored identity
  suggestions must use `ai_proposal`, remain `proposed` or `needs_review`,
  include reasons plus evidence/source links, and must never write as
  `system_rule` or `import_policy`. Governed deterministic automation may use
  `system_rule`/`import_policy` only under approved policy. AI must not silently
  create canonical truth.
- High-risk actions need deterministic rules, evidence IDs, and review gates.

Before editing architecture, check for drift against existing context docs.
