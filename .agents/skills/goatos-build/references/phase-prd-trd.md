# Phase PRD/TRD Reference

Load this before implementing, reviewing, or changing any delivery phase.

## Active Override — Protocol Engine Phase 0

For current Preventive Care (PC) vaccination, vaccination execution context, protocol config,
obligation, and inventory-ledger work, the active build spec is:

```text
docs/protocol-engine/PHASE-0-CHECKLIST.md
docs/protocol-engine/IMPLEMENTATION-PLAN.md
docs/protocol-engine/obligation-engine.md
docs/protocol-engine/state-machines.md
docs/protocol-engine/migration-and-cutover.md
context/architecture/operational-kernel.md
docs/preventive-care-vaccination/PRD.md
docs/preventive-care-vaccination/TRD.md
```

Use those files as the source of truth for migrations `000070-074`, scoped
protocol rulesets, obligations, inventory ledger, Config activation policy, and
million-animal query shape.

Canonical docs:

- `docs/phases/README.md`
- `context/product/goat-os-feature-phases.md`
- `context/execution/two-dev-build-plan.md`
- `context/source-findings/drive-docs-findings.md`
- `context/source-findings/customer-promise-safety-findings.md`

Rules:

- For Protocol Engine / Preventive Care (PC) Vaccination work, read the active docs above before
  code. Do not rely on deleted old phase docs for the protocol/obligation
  schema or frontend direction.
- Write and review the detailed PRD/TRD for the selected phase before code.
- Do not generate detailed PRD/TRD files for every future phase at once.
- Every phase must repeat the architecture invariants: modular monolith,
  ports/adapters, OpenAPI clients, JSON Schema events/forms, gRPC/protobuf only
  for internal high-volume seams, idempotency, audit, outbox, RBAC,
  observability, load tests, and analytics/monitoring slice.
- Every phase must include the operational kernel checklist from
  `context/architecture/operational-kernel.md`: trigger, canonical transaction,
  obligation/work item, sweeper, reminder/deadline alert, notification/
  escalation, proof/verification, read-model process-integrity view, local/cloud
  parity, and million-animal scale proof.
- If the phase adds a permanent module/tool/workflow/rule, update the relevant
  skill reference in `.agents/skills/goatos-build/references/` before coding.
- Before asking humans to answer business decisions from memory, inspect
  reachable legacy/source artifacts and produce evidence-backed proposals with
  source paths, counts, confidence, and open policy-only decisions. Do not commit
  raw private rows, PII, tokens, or media URLs.
- If a phase or policy pack replaces a legacy Slack/Sheets/App Script workflow,
  the PRD/TRD must declare the `legacy capability parity floor`, `known legacy
  gaps to close`, and `import/replay mapping`. Capability parity preserves
  useful source signals and workflow intent; it must not preserve weak legacy
  validation as the target behavior.
- Critical-action PRD/TRDs must also declare classification authority and
  bypass handling: what evidence computes the final action classification, and
  how lower-level primitives are blocked, wrapped, or restricted until the pack
  owns the transition.
- After coding, run a closeout sync: compare code, migrations, contracts,
  tests, adapters, workflows, and operational checks against PRD/TRD and update
  PRD/TRD/BUILD-STATUS/context/skills/agent shims where the implementation
  changed reality.
- Keep references short. Do not duplicate the full PRD/TRD inside skill files.
- Use phase docs for "what to build"; use skill references for "what context to
  load and which rules must not be forgotten."
- For protocol/config work, never treat "demo" or "sandbox" as a rule state.
  Separate draft/published immutability from active/inactive selection. For
  vaccination, one company `vaccination.matrix` version is active and a park
  override replaces it only for that park.

Locked identity foundation:

```text
Path B mixed-herd identity supersedes earlier goat-only wording:
animal_id immutable internal ID; display_id human ID.
animal_identifier_1 and animal_identifier_2 are required parallel external
field/business IDs for every accepted herd animal; they are not old/new IDs.
canonical target table = herd_animals, not goats.
species comes from species_catalog; seed goat and sheep, allow future species without DDL/code branches.
breeds belong to species; Anantapur Sheep is sheep, not a goat breed.
every clean current animal resolves to one current shed/tag; goats and sheep may share the same shed/tag.
Raw source identifier column names stay import provenance only; canonical DB/API/UI
names use animal_identifier_1/animal_identifier_2, with duplicate checks scoped by
the relevant identifier value plus normalized park where source data requires it.
tenant isolation + global parties + shared-PK org subtype.
owner_party / custodian_party / location / task assignment are separate.
ownership share_bps uses integer bps and deferred invariant.
custody has temporal history; operator task assignment is not custody.
status is decomposed into lifecycle/reproductive/growth/management/health + sex.
F2-Male/F2-Female maps to growth_cohort=F2 only; sex comes from Gender source.
breed strings normalize to breed_id reference rows/aliases before counters.
merges set `merged_into_animal_id`; lookups redirect to the survivor; P8 booking,
allocation, and replacement resolve to the survivor before availability and
no-double-promise checks.
events/audit/history are partition-aware; idempotency is explicit.
The current vaccination slice stores policy inputs but does not invent
customer/festival eligibility or vaccine schedule values.
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
Customer promise safety logic is a Phase 8 reference, not production runtime.
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
