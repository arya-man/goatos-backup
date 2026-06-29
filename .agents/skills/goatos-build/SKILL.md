---
name: goatos-build
description: Build, review, or modify Goat OS across backend, forms, mobile, dashboards, analytics, infra, contracts, and AI-agent context. Use when working on Goat OS product code, contracts, architecture, migration, or execution planning.
version: 0.1.0
user-invocable: true
argument-hint: "[area: backend|forms|mobile|dashboard|analytics|infra|contracts|migration|review]"
---

# Goat OS Build Skill

Use this skill for Goat OS engineering work. This file is the single entry point
for references. Do not route from memory alone.

## Required First Step — 4-Layer Lookup

Work layers in order. Stop when the question is answered. Never jump to files first.

**Layer 1 — CRG** (code structure: callers, imports, blast radius)
```
repo_root: <absolute path of your goatos checkout>   # git rev-parse --show-toplevel
Cold/review/diff task      -> get_minimal_context_tool, then one targeted graph query
Known-symbol traversal     -> query_graph_tool callers_of/callees_of/imports_of/tests_for
Keyword/domain lookup      -> semantic_search_nodes_tool, then query_graph_tool
Changing code              -> detect_changes_tool + get_impact_radius_tool
Single file/function read  -> read the file; use graph only if impact is unclear
```

**Layer 2 — Graphify** (business context + technical docs). Query both in parallel:

**mesha_docs_graph** — wiki SOPs, farm workflows, vaccination protocols, org model (maintainer-local only):
```
MCP: mesha_docs_graph or mesha_visual_graph
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph /Users/ravi/mesha/graphify-out/graph.json
```

**goatos-docs graph** — TRDs, ADRs, protocol engine, obligation engine, frontend scope, skill refs (locally generated; run `make ai-rebuild-docs` if missing):
```
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph ./graphify-out/graph.json
```
Skip mesha_docs_graph if not configured. If the local goatos-docs graph is
missing, run `make ai-rebuild-docs` or fall back to the relevant source docs.

**Layer 3 — Skill references** (architecture decisions, TRDs, contracts)
Load `context/README.md`, then only the ONE reference doc from the table below
that CRG + Graphify point toward. Do not load all references blindly.

**Layer 4 — Grep/Read** (CRG blind spots only)
HTTP route strings, middleware wiring, config values, SQL strings, uncommitted code,
any `callers_of = 0` that seems wrong.

## Current Active Build Path

For protocol/config-driven operations work, the active implementation path is
**Protocol Engine Phase 0**. Use these as the source of truth for PHC
vaccination, feed direction, protocol rules, obligations, inventory ledger,
Action Center, Protocol Adherence, and Phase 0 implementation:

```text
docs/protocol-engine/PHASE-0-CHECKLIST.md
docs/protocol-engine/IMPLEMENTATION-PLAN.md
docs/protocol-engine/obligation-engine.md
docs/protocol-engine/state-machines.md
docs/protocol-engine/migration-and-cutover.md
context/architecture/operational-kernel.md
docs/phc-vaccination/TRD.md
docs/feed-direction/TRD.md
docs/decisions/calendar-ownership.md
context/execution/calendar-vaccination-slice-parallel-handoff.md
context/execution/admin-web-e2e-checklist.md
mock/goatos-dashboard-mock.html
```

Use older `docs/phases/*` docs for existing built repo patterns and historical
status only. They do not override the protocol-engine Phase 0 contract above.

Operational kernel golden rule: every feature must plug into the shared
trigger -> obligation -> sweeper/reminder -> notification/escalation -> proof ->
verification -> read-model chain from
`context/architecture/operational-kernel.md`. The end goal is always process
integrity: was the expected process followed, where broken, who owns next action,
what evidence proves it, and what alert/escalation fired when a deadline crossed.

Calendar is currently reopened only as the PHC Vaccination due-work command lens
at `/calendar`. It must follow the Calendar ownership ADR and vaccination slice
handoff: generic `CalendarEvent` contract, vaccination-specific detail blocks,
bounded date windows, protected route registration, durable nudge/snooze writes,
and seeded Postgres E2E proof. Do not treat the full mock Calendar taxonomy as
built product.

## Reference Guide

| Reference | Load when |
| --- | --- |
| `references/repo-structure.md` | Creating/scaffolding `goatos/`, moving docs, setting up `.claude`, `.agents`, `AGENTS.md`, `CLAUDE.md`, or deciding where files live |
| `references/architecture.md` | Reviewing or changing contexts, modules, boundaries, ports/adapters, backend shape, or AI authority rules |
| `references/backend-impl.md` | Extending/reviewing built Go backend code, current API wiring, identity passport/identifier surfaces, protocol/obligation/vaccination modules, backend package patterns, request middleware, tenant-scoped queries, sqlc follow-up, observability and logging, panic recovery, or log sinks |
| `references/forms-sop.md` | Working on SOP forms, form DSL, builder/editor, native runner, task engine, verification flow, or Slack-form replacement |
| `references/contracts-events.md` | Adding/changing OpenAPI, JSON Schema, protobuf, event envelope, decision records, generated clients, or contract drift checks |
| `references/frontend-mobile.md` | Reusing existing dashboard/mobile UI, changing admin-web/operator-mobile, role-aware dashboard, RBAC UI visibility, or app data adapters |
| `references/analytics-infra.md` | Analytics, BI, AI analyst, BigQuery, Tinybird, Cube, dbt, Metabase, telemetry, cost guardrails, or dashboard metric source |
| `references/execution-plan.md` | Splitting work across two developers/agents, Protocol Engine Phase 0, delivery phases, dev/stg/prod setup, load testing, SLOs, or migration spike |
| `references/phase-prd-trd.md` | Starting or reviewing a phase PRD/TRD, Protocol Engine Phase 0, checking phase scope, or ensuring phase docs update agent references before code |
| `references/source-findings.md` | Using facts from General/Slack docs, customer promise safety findings, legacy source docs, or checking whether source facts reached canonical docs |
| `references/existing-repos.md` | Inspecting or migrating from `dashboard`, `vgoats-dashboard`, `procurement_app`, `slack-automation-scripts`, or `website` reference repos |
| `references/security-ops.md` | Dashboard gating, Slack token rotation, secrets, IAM tiers, Google Cloud context, Cloud SQL data pulls, prod read-only agent access, or auth/RBAC concerns |

## Reference Doc Convention

Use one skill with many references. The committed source skill lives here:

```text
goatos/.agents/skills/goatos-build/
  SKILL.md
  references/<topic>.md
```

Claude discovers the same skill through a symlink or generated copy:

```text
goatos/.claude/skills/goatos-build -> ../../.agents/skills/goatos-build
```

Do not hand-maintain two skill copies.

Do not create separate skills like `goatos-analytics`, `goatos-forms`,
`goatos-mobile` unless the trigger surface becomes truly independent. Goat OS is
one product; this skill is the navigation layer.

## Must

- Read wide, write narrow.
- Lock to the user-approved slice. Shared/generic infrastructure may be built
  only to serve that slice, and visible UI/API handoffs must not present future
  verticals as live product.
- Use `context/` as architecture truth.
- Use generated contracts instead of hand-copying DTOs.
- Before coding a phase, read its PRD/TRD and update skill references if the
  phase adds a permanent rule/module/tool/workflow.
- After coding a phase, run the PRD/TRD/context/skill closeout sync so docs
  describe what was actually built.
- Keep AI as proposer/triage, never canonical authority.
- Preserve module boundaries in the Go modular monolith.
- Keep frontend/mobile behind app APIs and generated clients.
- Treat backend-driven UI contracts as a golden rule. Admin-web/operator-mobile
  must render backend-owned OpenAPI/app contracts for navigation, route labels,
  page/section titles, table columns, filter/sort/page-size semantics, chips,
  tabs, row-click params, drawer/action copy, disabled reasons, and
  summary/detail field sets. Frontend may own only layout, CSS, responsive
  density, icon-token mapping, focus/hover behavior, and local open/closed or
  selected-row state. When a page still has hardcoded product text or control
  semantics, migrate it into the backend contract or record the temporary gap in
  `context/frontend/` before handoff. Stable UI text that needs runtime
  governance is stored in tenant-scoped `admin_ui_config_entries` and compiled
  into `/admin-web/bootstrap`; live/domain values stay in canonical module DB
  tables and are compiled as DB families. UI config entries must not relabel
  live/module-owned option groups or override semantic option metadata such as
  source-system publishability.
- Keep official analytics metrics behind Cube.
- Keep Slack/Sheets/App Script as legacy reference/migration only.
- When source artifacts add lasting facts, sync the relevant `context/` doc and
  this skill's reference map in the same closeout.
- For protocol/config work, keep workflow status separate from source/review
  trust: `protocol_versions.status` is `draft`/`published`/`retired`;
  source-backed approval lives under `rule_dsl.source`.

## Must Not

- Do not duplicate architecture facts inside `AGENTS.md`, `CLAUDE.md`, or skill refs.
- Do not add direct BigQuery queries to React pages.
- Do not add direct Firestore/GCS writes to the operator app.
- Do not let AI-created decisions become canonical without deterministic validation and evidence.
- Do not copy old repos blindly into `goatos/`; dashboard frontends are the
  intentional exception and should be snapshot/cloned into `goatos/apps/` for
  safe rewiring while live repos stay untouched.
- Do not treat the public website as Goat OS core.
- Do not use demo/sandbox as protocol rule concepts. Unsourced or unapproved
  rules remain `draft` with a `not source-backed` warning; only source-backed,
  approved rules may publish.
