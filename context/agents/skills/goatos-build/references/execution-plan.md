# Execution Plan Reference

Load this when splitting work across two developers/agents, planning delivery
order, setting dev/stg/prod, designing load tests, or deciding start points.

Canonical docs:

- `context/execution/two-dev-build-plan.md`
- `context/execution/env-load-test-and-doc-hygiene.md`
- `context/execution/next-contracts.md`

Rules:

- Delivery phases are sequencing, not weak architecture versions.
- Phase 0 starts with git, security, dashboard gating, and token rotation.
- Contracts/schema/migration spike must come early.
- Developer A owns backend/data/contracts/infra/CI.
- Developer B owns admin-web/operator-mobile/analytics UI with mock APIs until
  backend is ready.
- Load tests need pass/fail SLOs before running.
- Synthetic data must match real skew from existing data, not uniform fantasy data.
- Historical planning files live outside `context/` under
  `docs/archive/planning-history/`; do not use them for build
  instructions unless explicitly asked.
