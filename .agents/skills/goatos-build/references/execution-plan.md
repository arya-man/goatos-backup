# Execution Plan Reference

Load this when splitting work across two developers/agents, planning delivery
order, setting dev/stg/prod, designing load tests, or deciding start points.

## Current Start Point — Protocol Engine Phase 0

The active start point for current work is **Protocol Engine Phase 0**:

```text
docs/protocol-engine/PHASE-0-CHECKLIST.md
docs/protocol-engine/IMPLEMENTATION-PLAN.md
docs/protocol-engine/obligation-engine.md
```

Implementation order:

```text
1. Lock migrations 000070-074.
2. Generate sqlc.
3. Add protocol/obligation/inventory domains and repositories.
4. Add the outbox Pub/Sub publisher adapter while keeping the logging adapter.
5. Add idempotency guards and query-plan validation for million-goat hot paths.
6. Add tests.
```

No published protocol rules, no fake vaccine/feed values, and no frontend direct
DB/BQ/Sheets reads in Phase 0.

Canonical docs:

- `context/execution/two-dev-build-plan.md`
- `context/execution/env-load-test-and-doc-hygiene.md`
- `context/execution/next-contracts.md`

Rules:

- Delivery phases are sequencing, not weak architecture versions.
- Historical repo Phase 0 started with git/security/dashboard gating. The
  current Protocol Engine Phase 0 starts with migrations `000070-074` and the
  empty protocol/obligation/inventory engine.
- Contracts/schema/migration spike must come early.
- Developer A owns backend/data/contracts/infra/CI.
- Developer B owns admin-web/operator-mobile/analytics UI with mock APIs until
  backend is ready.
- Load tests need pass/fail SLOs before running.
- Synthetic data must match real skew from existing data, not uniform fantasy data.
- Historical planning files live outside `context/` under
  `docs/archive/planning-history/`; do not use them for build
  instructions unless explicitly asked.
