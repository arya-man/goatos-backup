# Goat OS Phase PRD/TRD Index

Detailed PRD/TRD docs are written one phase at a time, immediately before that
phase is implemented.

Rule:

```text
phase selected
-> PRD written and reviewed
-> TRD written and reviewed
-> relevant skill/reference files updated if the phase adds permanent rules
-> legacy/source discovery produces evidence-backed proposals before asking humans
-> implementation starts
-> tests/load checks/migration checks
-> post-code docs/context/skills sync against actual code
-> next phase
```

Do not write detailed plans for every future phase at once. Keep future phases
in `context/product/goat-os-feature-phases.md` until they are ready to build.

## Required In Every Phase

Every phase PRD/TRD must repeat the non-negotiable architecture invariants.
This prevents the core design from depending on memory or chat history.

```text
Go backend = modular monolith with strict module ownership.
Domain logic follows SOLID dependency inversion: it talks through
ports/interfaces, not vendor SDKs directly.
Adapters wrap replaceable tools: auth, storage, analytics, media, devices,
notifications, AI, form runners, and external APIs.
Adapter selection is wired in bootstrap/factory code, not scattered through
business logic.
Web/mobile clients use REST/JSON APIs described by OpenAPI and generated clients.
Forms, events, decisions, DLQ repair payloads, and imports use JSON Schema where
payload compatibility matters.
Protobuf/gRPC is allowed only behind the app API boundary for real internal
high-volume workloads or future split services.
Browser and React Native clients must not use direct gRPC unless a new ADR
replaces the current protocol decision.
Every phase keeps idempotency, audit, outbox, RBAC scope, observability, and
load-test expectations explicit.
Every phase declares its analytics slice: which events/projections/API metrics
it emits now, and how those flow into BigQuery/Tinybird/Cube/Metabase without
letting product code query those tools directly.
Every phase declares its monitoring slice: API latency, DB pressure, outbox lag,
DLQ/error counts, and the alert thresholds that matter for that phase.
Every phase declares agent-context impact: if the phase adds a new module,
vendor/tool, API pattern, form rule, analytics rule, or operational workflow,
update `.agents/skills/goatos-build/references/` before implementation.
Every phase must inspect available legacy/source artifacts before asking the
business to answer from memory. Agents should produce evidence-backed proposals
with source paths, counts, and confidence, then ask for confirmation. Raw PII or
private source rows must not be committed.
Every phase closes with a docs sync: compare implemented code, contracts,
migrations, adapters, tests, and operational behavior against PRD/TRD/context
and update docs, `SKILLS.md`, `AGENTS.md`, `CLAUDE.md`/`CODEX.md` shims, and
`.agents/skills/goatos-build/references/` where the code changed the truth.
Current guardrails are not full architecture proof. Until Go depguard and
TypeScript dependency-cruiser style boundary checks are wired, a passing
`check-boundaries.sh` means "basic safety checks passed", not "all module,
adapter, analytics, and SDK boundaries are mechanically enforced".
```

## Active Phase Docs

```text
Current active path:
  docs/protocol-engine/PHASE-0-CHECKLIST.md
  docs/protocol-engine/IMPLEMENTATION-PLAN.md
  docs/protocol-engine/obligation-engine.md
  docs/protocol-engine/state-machines.md
  docs/phc-vaccination/PRD.md
  docs/phc-vaccination/TRD.md
  docs/phc-vaccination/V1-FOUNDATION-SPEC.md

Old Goat Passport and generic SOP task-engine phase folders were deleted
because they described the old import-review, identity-review, generic SOP/admin,
counts, mortality, and legacy-sync track. Use the active docs above plus
`context/frontend/current-admin-web-scope.md` for the current Admin Config + PHC
Vaccination + Parks vaccination execution slice.
```
