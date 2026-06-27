# Goat OS Context Index

This folder is the future home for build-time context used by engineers and AI agents.

Rule: one fact should have one authoritative home. Other docs may summarize, but should link back here or to the source document.

Active-doc grep:

```bash
rg "term" context
```

Historical planning/archive docs were removed from the active tree. Do not use
git history or source material for build direction unless a human explicitly
asks for archaeology.

## Authoritative Architecture Sources

```text
Goat OS product feature phases
  context/product/goat-os-feature-phases.md

Detailed phase PRD/TRD docs
  docs/phases/

Feature-level PRD/TRD docs
  docs/features/

Goat OS coverage checklist (frozen migration audit, not living architecture)
  context/product/goat-os-coverage-map.md

General/Slack source findings
  context/source-findings/drive-docs-findings.md

Customer promise safety source findings
  context/source-findings/customer-promise-safety-findings.md

Final architecture
  context/architecture/final-architecture.md

Operational kernel golden rule
  context/architecture/operational-kernel.md

Operational kernel system design and diagram
  context/architecture/operational-kernel-system-design.md

Forms and SOP engine
  context/forms/final-forms-sop-engine.md

Analytics, BI, AI, telemetry, infra cost controls
  context/analytics/final-analytics-infra.md

Frontend, mobile, backend adapter architecture
  context/frontend/final-frontend-mobile-backend-architecture.md

Current admin-web frontend scope and removed old routes
  context/frontend/current-admin-web-scope.md

AI agent context and API protocol decisions
  context/agents/ai-agent-context-and-protocols.md

Backend stack ADR
  docs/decisions/go-backend-stack.md

Calendar ownership and vaccination Calendar scope ADR
  docs/decisions/calendar-ownership.md

High-scale dashboard projection architecture
  docs/decisions/high-scale-dashboard-projections.md

Goat OS agent skill bundle and reference map
  .agents/skills/goatos-build/SKILL.md

Environment, load testing, and doc hygiene
  context/execution/env-load-test-and-doc-hygiene.md

SOP + vaccination backend handoff
  context/execution/sop-vaccination-backend-handoff.md

Vaccination process-integrity backend handoff
  context/execution/vaccination-process-integrity-backend-handoff.md

Vaccination process-integrity frontend handoff
  context/frontend/vaccination-process-integrity-frontend-handoff.md

Procurement/source-entry backend handoff
  context/execution/procurement-source-entry-backend-handoff.md

Procurement/source-entry frontend handoff
  context/frontend/procurement-source-entry-frontend-handoff.md

Procurement source-entry -> vaccination E2E plan
  context/execution/procurement-vaccination-e2e-plan.md

Admin-web full E2E checklist and contract-driven UI gap ledger
  context/execution/admin-web-e2e-checklist.md

Admin-web backend UI contract and current surface inventory
  context/frontend/admin-web-backend-ui-contract.md

Vaccination pre-E2E readiness audit
  context/execution/vaccination-pre-e2e-readiness-audit.md

Vaccination trigger closure parallel handoff
  context/execution/vaccination-trigger-closure-parallel-handoff.md

Calendar vaccination slice parallel handoff
  context/execution/calendar-vaccination-slice-parallel-handoff.md

Vaccination roster expansion follow-up
  context/execution/vaccination-roster-expansion-followup.md

Two-developer build plan
  context/execution/two-dev-build-plan.md

Target repo structure
  context/execution/target-repo-structure.md

Next execution artifacts
  context/execution/next-contracts.md
```

These files supersede older root-level planning docs where they disagree. In particular, old staging labels are historical planning notes, not the final architecture. The final position is: forms, analytics, streaming, telemetry, verification, and cost controls are production-grade from the start; the system grows by configuration, modules, scale, and tuning under real load.

## Historical / Source Documents

Old planning archives and old phase ladders are intentionally absent from the
active repo. They are superseded by the current context, protocol-engine, and
PHC vaccination docs. Use git history only for explicit historical comparison.

## Ownership Targets

```text
context/product/
  CEO language, workflows, glossary
  start with context/product/glossary.md for business terms and legacy codes

context/source-findings/
  derived findings from private/source docs; never raw PII or contacts

context/architecture/
  contexts, wires, ports, deploy model, infra decisions

context/security/
  auth, permissions, RBAC, dashboard visibility, public exposure checklist

context/frontend/
  existing UI reuse, missing screens, role navigation, Android operator surface

context/repo-audits/
  current repo inventory and migration notes

context/agents/
  Codex/Claude context structure, agent files, protocol decisions, skill bundles
```

## Immediate Notes

```text
security:
  Current dashboard deployments must be gated before further sharing.

slack:
  Slack is notification/migration surface, not canonical SOP execution.
  Inbound Slack form ingestion is legacy cutover only and must pass through Goat OS APIs, permissions, validation, audit, and idempotency.

frontend:
  Product taxonomy is fixed: a vertical is a business operating domain
  (PHC, Parks, Procurement, Admin/Data Ops, Counts, Breeding, Inventory,
  HR/People, Farmer Network); a module is a workflow/product inside a vertical
  (PHC -> Vaccination, Procurement -> Source Entry, or future Parks-owned
  modules). Parks is only scope/context for vaccination execution; it is not the
  owner of a vaccination module. Control Tower, Action Center, Protocol
  Adherence, and Workflows are
  top-level command lenses, not verticals/modules; Config and SOP Library are
  top-level Admin/Data Ops authority screens.
  Current admin-web review scope supersedes the old dashboard/admin product
  surface. Follow context/frontend/current-admin-web-scope.md: build only the
  current Control Tower shell, PHC/Vaccination, Admin Config, vaccination
  execution context (rendered inside /vaccination), Admin/Data Ops
  SOP Library for vaccination SOP policy, and contextual Goat Passport detail
  surfaces. Parks is a vertical, but it is NOT a vaccination product route,
  module, or sidebar entry. Old
  Operations/Legacy/counts/import routes and old generic SOP/task pages are removed
  from active admin-web until explicitly brought back.
  For the current process-integrity slice, Control Tower, Action Center,
  Protocol Adherence, and Workflow drilldowns are not useless side screens:
  they are four lenses on the same vaccination truth. Read
  context/execution/vaccination-process-integrity-backend-handoff.md and
  context/frontend/vaccination-process-integrity-frontend-handoff.md before
  changing backend projections, admin-web routes, or mock-matching dashboard
  surfaces.
  Procurement/source-entry is a separate slice. The business rule is documented:
  a goat journey can start at purchase/source/holding farm before main park
  arrival, including source warmup and pre-dispatch rejection. Read
  context/execution/procurement-source-entry-backend-handoff.md and
  context/frontend/procurement-source-entry-frontend-handoff.md before building
  that slice. When backend contracts exist, prove the seeded source-entry ->
  accepted-intake -> vaccination path from
  context/execution/procurement-vaccination-e2e-plan.md before adding broad CRUD
  or calling the slice done. Do not mix procurement into the current
  vaccination-only UI unless scope is explicitly reopened.
  Command-room/authority surfaces are top-level only: Control Tower, Action
  Center, Protocol Adherence, Workflows, Config, and SOP Library must not be
  duplicated under procurement, PHC, Parks, or any future vertical as routes,
  compatibility redirects, tabs, or nav items. A vertical can feed those screens
  through a selected domain/category/filter/lens such as `?domain=procurement`
  or `?category=vaccination`, but agents must not create nested routes like
  `/vaccination/adherence`, `/vaccination/config`,
  `/procurement/source-entry/action-center`, `/procurement/source-entry/control-tower`,
  or `/parks/vaccination/{anything}` as a product path.

  There is no `/parks/vaccination` exception. Vaccination execution belongs under
  PHC/Vaccination: use `/vaccination` and
  `/vaccination/execution/sheds/{shed_id}` directly. Scope chrome rule: keep
  park/date/source scope in the top bar or behind Filters. Do not repeat
  "Scope", "All parks", source, or date chips inside page bodies.
  Android Field App owns conditional SOP form execution; Slack forms are legacy/migration input only.

scratch vs salvage:
  from scratch = backend, data model, canonical truth, app-api
  salvage = proven UI/UX, chart components, video recorder, SOP overlay, offline upload queue
```
