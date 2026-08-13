# Architecture Reference

Load this when touching Goat OS contexts, module boundaries, backend structure,
ports/adapters, devices, R&D, AI authority, or canonical state.

Canonical docs:

- `context/architecture/final-architecture.md`
- `context/architecture/operational-kernel.md`
- `docs/architecture/operational-read-model-contract.md`
- `context/agents/ai-agent-context-and-protocols.md`
- `context/execution/next-contracts.md`
- `context/product/glossary.md`
- `docs/decisions/go-backend-stack.md`
- `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/preventive-care-vaccination/TRD.md`

Current deployment scale target (authority):

- The accepted ADR `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`
  is the authority for operational-kernel deployment scale and worker topology.
  The current release envelope is **~5,000 animals today, up to ~50,000 within
  the year**, served by **one modular kernel worker** next to the API and
  Postgres, with **zero Cloud Scheduler crons, zero scheduled Cloud Run Jobs,
  and zero projection tables** in the normal runtime. APIs read canonical tables
  through bounded, indexed, keyset-paginated SQL; the scale-out ladder adds one
  narrowly owned projection, then one extracted worker, then partitions/queues,
  one measured hotspot at a time. One-million-animal topology is **future scale
  / research work**, not a present release requirement. Where a rule below states
  a one-million-animal invariant, read it as correctness guidance plus future
  scale work for this envelope, per that ADR.

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
- Shared operational read models are contract-first and grain-explicit. Calendar,
  Control Tower, Action Center, Protocol Adherence, Workflows, admin-web detail
  pages, Android execution/proof screens, and reporting must consume common
  backend-owned facts rather than recomputing private screen truth. For any new
  vertical/module or shared surface change, apply
  `docs/architecture/operational-read-model-contract.md`: declare canonical
  write owner, work-item identity, scope/time grain, status bucket
  disjointness/overlap, whole-result summary behavior, evidence model, and every
  consuming surface.
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
- Scale-shape correctness is a hard design rule at every envelope: chunk by
  park/shed/cohort/date, never full-herd scan in an API, use idempotency,
  bounded workers/goroutines, and indexed tables. These properties are cheap and
  mandatory today at 5k-to-50k. The one-million-animal target that originally
  motivated this rule is future scale / research work per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`; it justifies the
  correct query/worker shape now, but it does not justify standing partitions,
  extra queues, or separate projector services until measured workload requires
  them. Partition-awareness in particular is a future-scale property here, not a
  present-runtime requirement (see the partition-aware note below).
- Google equivalents stay behind ports/adapters: Pub/Sub for event bus, Cloud
  Tasks for near-term timers/retries, Cloud SQL/Postgres for canonical truth, GCS
  for media, Cloud Monitoring/Error Reporting and alert webhooks for
  incident-style escalation, and Redis/Memorystore only as cache/lease
  acceleration, never truth. Sweeper/scheduling runtime for the current envelope
  is **one long-running kernel worker** with in-process cadence classes (event
  consumer, fast delivery, operational, obligation generation, housekeeping), not
  a fleet of Cloud Scheduler crons plus scheduled Cloud Run Jobs — that split
  topology is retired to future-scale extraction per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. Keep the
  port/adapter seam so a single stage can later be pulled into its own worker
  without rewriting producers.
- Local GCP-kernel behavior parity is executable through
  `compose.local-kernel.yml` and `docs/runbooks/local-gcp-kernel-parity.md`:
  official Pub/Sub emulator with the production publisher/subscriber, Docker
  Postgres, and the same API/job binaries. Cloud Tasks/IAM/GCS managed behavior
  remains a separate staging contract proof; do not introduce third-party
  Kafka/Temporal/Cloud-Tasks fakes into the canonical kernel.
- High-volume histories/events/audit tables are designed partition-ready, but
  for the current 5k-to-50k envelope they run as ordinary indexed tables:
  monthly partitioning and its maintenance command are recoverable future-scale
  work reintroduced only for the first measured history/event-table hotspot, per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. Keep the logical
  columns/constraints partition-friendly so a hot table can be converted without
  a schema rewrite. Idempotency lives in explicit idempotency-key records, not in
  client hope.
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
