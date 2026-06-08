# Phase PRD/TRD Reference

Load this before implementing, reviewing, or changing any delivery phase.

Canonical docs:

- `docs/phases/README.md`
- active phase `docs/phases/<phase>/PRD.md`
- active phase `docs/phases/<phase>/TRD.md`
- active phase `docs/phases/<phase>/BUILD-STATUS.md`, if present
- active phase discovery/proposal docs, if present, such as
  `docs/phases/phase-01-goat-passport/legacy-discovery-proposals.md`
- `context/product/goat-os-feature-phases.md`
- `context/execution/two-dev-build-plan.md`
- `context/source-findings/drive-docs-findings.md`
- `context/source-findings/assignment-promise-keeper-findings.md`

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
  PRD/TRD/BUILD-STATUS/context/skills/agent shims where the implementation
  changed reality.
- Keep references short. Do not duplicate the full PRD/TRD inside skill files.
- Use phase docs for "what to build"; use skill references for "what context to
  load and which rules must not be forgotten."

Current Phase 1 locked foundation:

```text
goat_id immutable internal ID; display_id human ID.
RFID-first import; old tag scope = number + normalized park.
tenant isolation + global parties + shared-PK org subtype.
owner_party / custodian_party / location / task assignment are separate.
ownership share_bps uses integer bps and deferred invariant.
custody has temporal history; operator task assignment is not custody.
status is decomposed into lifecycle/reproductive/growth/management/health + sex.
F2-Male/F2-Female maps to growth_cohort=F2 only; sex comes from Gender source.
breed strings normalize to breed_id reference rows/aliases before counters.
merges set `merged_into_goat_id`; lookups redirect to the survivor; P8 booking,
allocation, and replacement resolve to the survivor before availability and
no-double-promise checks.
events/audit/history are partition-aware; idempotency is explicit.
Phase 1 stores policy inputs but does not execute customer/festival eligibility.
```

Current Phase 1 build status:

```text
Before continuing Phase 1 implementation, read
docs/phases/phase-01-goat-passport/BUILD-STATUS.md.

It records the built contracts, migrations, Go read API foundation, verified
edge-case fixes, temporary X-GoatOS-Tenant-ID scaffold, sqlc deferment, and
remaining work.
```

Cross-phase facts already captured:

```text
verification/proof engine is platform-wide.
promise safety requires P3 health, P5 movement/weight/feed, P8 allocation, and
P4/P8 open-promise sweeper.
Phase 5B crop/fodder/farmer module is conditional on Goat OS owning feed
production, otherwise define integration.
legacy Slack forms and health symptom fields are canonical source inputs.
legacy BigQuery catalog is source inventory for analytics parity, not truth.
Promise Keeper assignment logic is a Phase 8 reference, not production runtime.
```

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
