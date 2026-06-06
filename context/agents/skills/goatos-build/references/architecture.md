# Architecture Reference

Load this when touching Goat OS contexts, module boundaries, backend structure,
ports/adapters, devices, R&D, AI authority, or canonical state.

Canonical docs:

- `context/architecture/final-architecture.md`
- `context/agents/ai-agent-context-and-protocols.md`
- `context/execution/next-contracts.md`

Rules:

- Backend is a Go modular monolith with strict internal module boundaries.
- Modules own tables and expose defined interfaces.
- Cross-context propagation uses contracts, outbox, Pub/Sub, and APIs.
- Million-goat scale is a hard design rule: chunk by park/shed/cohort/date,
  never full-herd scan in an API, use idempotency, bounded workers/goroutines,
  and indexed/partition-aware tables.
- Workforce ops is a core Goat OS engine, not payroll HRMS: every task needs
  owner, scope, due time, backup path, escalation path, absence/backfill, and
  audit history.
- Backfill selection must be deterministic: skill match, park/shed/cohort
  scope, current load, shift conflict, task risk, notify assignee and park
  head, and escalate if no qualified backup exists.
- Media must use signed direct upload/download; API never proxies video bytes.
- Observability is required for new APIs/workers: p95/p99 latency, error rate,
  DB pressure, outbox/PubSub/DLQ lag, media failures, and analytics scan cost.
- AI may propose, classify, summarize, or triage. It must not silently create
  canonical truth.
- High-risk actions need deterministic rules, evidence IDs, and review gates.

Before editing architecture, check for drift against existing context docs.
