# Phase PRD/TRD Reference

Load this before implementing, reviewing, or changing any delivery phase.

Canonical docs:

- `docs/phases/README.md`
- active phase `docs/phases/<phase>/PRD.md`
- active phase `docs/phases/<phase>/TRD.md`
- active phase discovery/proposal docs, if present, such as
  `docs/phases/phase-01-goat-passport/legacy-discovery-proposals.md`
- `context/product/goat-os-feature-phases.md`
- `context/execution/two-dev-build-plan.md`

Rules:

- Write and review the detailed PRD/TRD for the selected phase before code.
- Do not generate detailed PRD/TRD files for every future phase at once.
- Every phase must repeat the architecture invariants: modular monolith,
  ports/adapters, OpenAPI clients, JSON Schema events/forms, gRPC/protobuf only
  for internal high-volume seams, idempotency, audit, outbox, RBAC,
  observability, load tests, and analytics/monitoring slice.
- If the phase adds a permanent module/tool/workflow/rule, update the relevant
  skill reference in `.agents/skills/goatos-build/references/` before coding.
- Before asking humans to answer business decisions from memory, inspect
  reachable legacy/source artifacts and produce evidence-backed proposals with
  source paths, counts, confidence, and open policy-only decisions. Do not commit
  raw private rows, PII, tokens, or media URLs.
- After coding, run a closeout sync: compare code, migrations, contracts,
  tests, adapters, workflows, and operational checks against PRD/TRD and update
  PRD/TRD/context/skills/agent shims where the implementation changed reality.
- Keep references short. Do not duplicate the full PRD/TRD inside skill files.
- Use phase docs for "what to build"; use skill references for "what context to
  load and which rules must not be forgotten."

Phase implementation checklist:

```text
phase PRD reviewed
phase TRD reviewed
agent reference impact checked
legacy/source discovery completed where relevant
evidence-backed proposals reviewed by human for business decisions
contracts drafted/updated
tests/load expectations named
security/RBAC impact checked
observability metrics/alerts named
implementation starts
implementation completed
code/contracts/migrations/tests compared against PRD/TRD
context docs updated for long-lived truth changes
skill refs updated for future agent routing/rules
AGENTS.md updated only for new repo-wide rules
CLAUDE.md/CODEX.md remain shims unless tool behavior requires otherwise
guardrails passed
```
