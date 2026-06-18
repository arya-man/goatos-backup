# Counter-Family Parallel Agent Runbook

Status: execution guide for Locations, Counts, and Mortality agents.

Use this runbook when running the three counter-family features in parallel.
The PRD/TRD files remain the product and technical contract; this file is the
operating checklist that tells agents how to execute without stepping on each
other.

## Simple Agent Prompt

```text
Read AGENTS.md, SKILLS.md, context/README.md,
.agents/skills/goatos-build/SKILL.md,
docs/features/counter-family/AGENT-RUNBOOK.md,
docs/features/counter-family/INTEGRATION-CHECKLIST.md,
and your assigned docs/features/<feature>/AGENT-TASK.md.

Implement only your assigned feature. Use live BigQuery/Sheets/Drive read-only
discovery, then prove the feature against local Postgres. Do not use stale local
source dumps. Do not deploy to dev. Produce source discovery, local DB proof,
parity artifacts, browser QA evidence, and EXPLAIN/query-plan proof before
asking for integration.
```

## Agent Split

Run these agents in parallel:

- Locations Agent: canonical location tree, aliases, seeded CBE/CPT/HF
  protections, capacity, usage checks, review, Locations UI/API.
- Counts Agent: legacy Counts sync, source rows, snapshots, projections, API,
  UI parity, coverage registry, shadow parity, query plans.
- Mortality Agent: mortality source rows, events, projections, logical-event
  dedup, denominator provenance, API, UI parity, rate gates.

Parallel work is allowed, but final integration order is:

```text
Locations -> Counts -> Mortality
```

Counts can build while Locations is in progress, but its final parity depends on
Locations aliases. Mortality can build its event spine while Counts is in
progress, but final rate completion depends on identity/count denominator
projections.

## Shared Rules

- Read the assigned PRD/TRD in full before editing.
- Stay inside the assigned feature unless the task doc explicitly names a shared
  file.
- Use live BQ/Sheets/Drive sources read-only for discovery and parity.
- Do not use stale local exports as source truth.
- Do not commit raw private source rows, sheet dumps, credential files, or
  service-account JSON.
- Frontend and mobile code must call Goat OS APIs only.
- BQ/Sheets are temporary upstreams into backend sync jobs, never dashboard
  runtime dependencies.
- Every API and worker must be tenant scoped, idempotent, audited, and
  observable.
- Pending/source-unavailable states are internal/dev states for required scope.
  They block production completion.
- Do not deploy to dev until the integration checklist passes locally.

## Live Source Access

Required live-source access is owned jointly by the data/source owner and the
feature implementer.

An agent must record:

- source name and project/dataset or sheet/drive identifier
- access method used
- source watermark or last modified time
- row counts or sample counts
- schema/column evidence
- blocked source errors
- whether the source is required or supporting

If a required source is blocked, record `source_unavailable`, keep prior
projection data stale when safe, and stop production-complete claims. Product
must explicitly remove the affected section from required scope before that
blocker stops blocking completion.

## Local-First Proof

Each agent proves the feature locally before integration:

1. Apply migrations to local Postgres.
2. Run live-source discovery in dry-run/read-only mode.
3. Run local sync/rebuild against local Postgres.
4. Verify API responses from local backend.
5. Generate numeric parity artifacts.
6. Run frontend/browser QA locally when the feature has UI.
7. Run query-plan or load proof for hot reads and high-volume rebuild paths.
8. Leave artifacts under `.codex-goatos-render/<feature>/`.

Do not skip local DB proof because the code compiles.

## One-Million-Goat Proof

One-million-goat readiness requires a credible scale artifact, not a note.

Acceptable proof:

- synthetic local/staging fixture at the relevant scale, such as one-million
  goat/current-fact rows or equivalent projection rows
- `EXPLAIN` or `EXPLAIN ANALYZE` fixtures proving tenant/date/section scoped
  indexed access with bounded row estimates
- chunked worker plan proof for rebuilds that touch large source/goat/event
  tables

Hot API reads must not scan goats, source rows, or all-history facts at request
time. They must read bounded projection rows.

## Browser QA

For UI features, run local admin-web and verify:

- desktop and narrow/mobile viewport
- no text overflow, chart clipping, or layout jumps
- legacy-equivalent tab ordering and labels
- stale/source-unavailable/rebuilding/never-synced states
- no frontend imports or calls to BQ/Sheets/Drive/raw DB
- screenshot comparison against the legacy dashboard when a legacy analogue
  exists

Use the repository's visual smoke command when the backend/admin-web can run:

```text
npm --prefix apps/admin-web run smoke:visual:live
```

## Required Artifacts

Each feature must produce the relevant artifacts below before integration:

- source discovery notes
- local sync/rebuild notes
- numeric parity comparison
- canonical shadow parity comparison where cutover is involved
- query-plan/load proof
- browser screenshot review for UI work
- known deltas with `match`, `explained_delta`, `unexplained_delta`, or
  `pending_source` status as defined in the feature TRD

Any `unexplained_delta` blocks completion. Any `pending_source`,
`pending_source_coverage`, or `source_unavailable` status blocks production
completion for required scope unless product explicitly removes that scope from
the feature PRD/TRD.

## Coordinator Responsibilities

The coordinator owns:

- resolving shared migration/OpenAPI conflicts
- ensuring merge order: Locations, then Counts, then Mortality
- checking all agents used live source discovery
- checking local DB proof exists before dev deploy
- running the integration checklist
- pushing/deploying to dev only after local proof passes

The coordinator must not accept "works on my dev data" as scale proof.
