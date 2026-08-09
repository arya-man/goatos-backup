# Goat OS Phase PRD/TRD Index

Detailed PRD/TRD docs are written one phase at a time, immediately before that
phase is implemented.

Rule:

```text
phase selected
-> PRD written and reviewed
-> TRD written and reviewed
-> prevention matrix and internal dependency/commit order recorded
-> relevant skill/reference files updated if the phase adds permanent rules
-> legacy/source discovery resolves evidence-backed implementation details and
   surfaces only genuine maintainer decisions
-> implementation starts
-> failing-before production-path proof
-> implementation + recurrence control + guard/self-test/ordinary-CI wiring
-> tests/load checks/migration checks + recovery/observability proof
-> post-code docs/context/skills sync against actual code
-> next phase
```

Do not write detailed plans for every future phase at once. Keep future phases
in `context/product/goat-os-feature-phases.md` until they are ready to build.

## Required In Every Phase

Every phase PRD/TRD must repeat the non-negotiable architecture invariants.
This prevents the core design from depending on memory or chat history.

```text
Go backend = modular monolith with strict module boundaries.
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
Every phase follows
`context/execution/defect-prevention-execution-contract.md`: each invariant has
a failing-before production-path regression, the strongest applicable
persistent or structural recurrence control, an adversarial self-test when a
structural guard applies (otherwise the stronger-control rationale and relevant
production-path proof), ordinary affected `make ci-local` routing, operational
recovery, and current-SHA counter-review. A feature is not complete when only
its happy path and unit test are green.
Every phase follows the operational kernel golden rule in
`context/architecture/operational-kernel.md`: business event -> canonical
transaction -> audit/outbox -> trigger evaluation -> obligation/work item ->
sweeper/reminder/deadline alert -> notification/escalation -> proof/
verification -> read model that answers whether the process was followed and
where it broke.
This chain is a non-deviation lock. A module may own domain facts but may not
create, retain as canonical, or exempt a private task authority, scheduler,
owner fallback, overdue calculation, reminder/escalation ladder, verification
queue, or screen-only follow-up state.
If the shared task adapter is not ready, the feature remains shadowed or blocked.
Every phase declares an Idempotency & Replay section for all mutating routes,
imports, workers, webhooks, mobile submissions, server actions, outbox
producers/consumers, and state transitions. It must list key source, semantic
request fingerprint, storage table/index, transaction boundary, replay response,
same-key different-payload behavior, downstream event dedupe, and required
tests.
Every phase declares its analytics slice: which events/projections/API metrics
it emits now, and how those flow into BigQuery/Tinybird/Cube/Metabase without
letting product code query those tools directly.
Every phase declares its monitoring slice: API latency, DB pressure, outbox lag,
DLQ/error counts, and the alert thresholds that matter for that phase.
Every phase declares agent-context impact: if the phase adds a new module,
vendor/tool, API pattern, form rule, analytics rule, or operational workflow,
update `.agents/skills/goatos-build/references/` before implementation.
Every phase must inspect available legacy/source artifacts before treating a
business rule as unknown. Agents produce evidence-backed proposals with source
paths, counts, and confidence. A genuinely unresolved authority decision is
recorded fail-closed while all independent work continues; routine scheduling,
review, conflict resolution, or merge work is never handed to the maintainer.
Raw PII or private source rows must not be committed.
Every phase touching herd-animal identity, species/breed labels, park/shed
scope, shed tags, lifecycle/stage, pregnancy/lactation/warm-up/fattening, feed
safety, weighing, handling, medicine administration, park roles, or feed
sessions must start from
`context/source-findings/goats-and-parks-source-findings.md` as the base
herd/park source and must record an explicit source/owner decision before
building conflicting semantics.
When a phase or policy pack replaces a legacy Slack/Sheets/App Script workflow,
it must declare the `legacy capability parity floor`, `known legacy gaps to
close`, and `import/replay mapping`. Capability parity means preserving useful
source signals and workflow intent while closing weak validation and manual
remembering gaps.
Critical-action phases must also declare classification authority and bypass
handling: what evidence computes the final action classification, and how
lower-level primitives are blocked, wrapped, or restricted until the pack owns
the transition.
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
  docs/preventive-care-vaccination/PRD.md
  docs/preventive-care-vaccination/TRD.md
  docs/preventive-care-vaccination/V1-FOUNDATION-SPEC.md

Old Animal Passport and generic SOP task-engine phase folders were deleted
because they described the old import-review, identity-review, generic SOP/admin,
counts, mortality, and legacy-sync track. Use the active docs above plus
`context/frontend/current-admin-web-scope.md` for the current Admin Config + Preventive Care (PC)
Vaccination + vaccination execution context slice.
```
