# Execution Plan Reference

Load this when splitting work across two developers/agents, planning delivery
order, setting dev/stg/prod, designing load tests, or deciding start points.

## Current Start Point — Protocol Engine Phase 0

Always load `context/architecture/operational-kernel.md` when planning a new
feature, split, load test, backend/frontend parallel handoff, or cloud/local
deployment path.

The active start point for current work is **Protocol Engine Phase 0**:

```text
docs/protocol-engine/PHASE-0-CHECKLIST.md
docs/protocol-engine/IMPLEMENTATION-PLAN.md
docs/protocol-engine/obligation-engine.md
context/architecture/operational-kernel.md
```

Implementation order:

```text
1. Lock migrations 000070-074.
2. Generate sqlc.
3. Add protocol/obligation/inventory domains and repositories.
4. Add the outbox Pub/Sub publisher adapter while keeping the logging adapter.
5. Add idempotency guards and query-plan validation for million-animal hot paths.
6. Add tests.
```

No published protocol rules, no fake vaccine/feed values, and no frontend direct
DB/BQ/Sheets reads in Phase 0.

Canonical docs:

- `context/execution/two-dev-build-plan.md`
- `context/execution/env-load-test-and-doc-hygiene.md`
- `context/execution/next-contracts.md`
- `context/execution/calendar-vaccination-slice-parallel-handoff.md`
- `docs/decisions/calendar-ownership.md`

Rules:

- Delivery phases are sequencing, not weak architecture versions.
- Historical repo Phase 0 started with git/security/dashboard gating. The
  current Protocol Engine Phase 0 starts with migrations `000070-074` and the
  empty protocol/obligation/inventory engine.
- Contracts/schema/migration spike must come early.
- Developer A owns backend/data/contracts/infra/CI.
- Developer B owns admin-web/operator-mobile/analytics UI with mock APIs until
  backend is ready.
- Every parallel handoff must include the operational kernel checklist:
  trigger, canonical transaction, audit/outbox, obligation/work item, sweeper,
  reminder/deadline alert, notification/escalation, proof/verification,
  read-model/process-integrity view, observability, local/cloud parity, and
  million-animal scale proof.
- For Calendar vaccination work, Developer A owns the protected backend routes,
  generic `CalendarEvent` contract, Postgres projection, nudge/snooze
  persistence, seed/E2E proof, and scale/query-plan checks. Developer B owns the
  `/calendar` UI, mock-fidelity drawer, active owner pills, generated-client
  integration, and seeded E2E click-through proof.
- Load tests need pass/fail SLOs before running.
- Synthetic data must match real skew from existing data, not uniform fantasy data.
- Historical planning/archive files were removed from the active tree; do not
  use git history or source material for build instructions unless explicitly
  asked.
