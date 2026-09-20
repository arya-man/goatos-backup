# Dashboard automation plan v3 landing progress

## Scope

Update the design document only: explicit four-layer architecture including the AI agent; OCI Always
Free runner; separate exact-SHA post-main certification and daily read-only production smoke; explicit
agent cost, performance, safety, and cleanup controls.

## Done

- Replaced the PC-centric v2.5 design with the OCI-oriented v3 design.
- Made static guards, PostgreSQL integration, Playwright E2E, and AI-agent review first-class layers.
- Defined agent sampling, escalation, structured findings, budget exhaustion, and cost receipts.
- Added self-growing coverage, non-hard-coded SQL/contract drift, exact-SHA lifecycle, production
  non-mutation boundaries, storage headroom, cleanup, and acceptance criteria.

## Pending

- Final diff/whitespace validation and repository landing receipt.
- Push to `origin/main`.
- Implementation intentionally awaits further discussion with Ravi.

## Exact tests and E2E

- `git diff --check`: passed.
- Manual diff review: passed; only the plan and this progress receipt are in scope.
- Markdown linter: unavailable on this host, recorded rather than installed for a docs-only change.
- Independent judge review: passed after its findings were incorporated into v3.
- `make land-main`: pending.
- Browser E2E: not applicable to this documentation-only change; no runtime code changed.

## Known failures

- None recorded yet.

## Before and after metrics

- Before: agent review was buried as visual tier 4; runner assumed Ravi's PC; no numeric agent budget;
  post-main certification and production smoke were conflated.
- After: four named layers; 100% deterministic surface sweep with agent review of all failures/new/risky
  surfaces plus rotating 10% passes; USD 1/run and USD 25/month initial API caps.

## Judge status

Passed. The judge required exact-SHA identity, separation of production and disposable-preview data,
explicit four-layer failure semantics, OCI Always Free refusal/cleanup controls, cost caps, secret
redaction, reviewed baseline promotion, and removal of autonomous healing from initial scope. All are
present in v3.

## Current SHA

Base: `e565e0d291b89ec4ae31d9003f028cca5ed9638b` (`origin/main` at worktree creation).

## Deployment state

Documentation only. No automation implemented, enabled, or deployed.
