# Goat OS Context Index

This folder is the future home for build-time context used by engineers and AI agents.

Rule: one fact should have one authoritative home. Other docs may summarize, but should link back here or to the source document.

Active-doc grep:

```bash
rg "term" context
```

Do not include `docs/archive/planning-history/` in normal agent
searches. That folder is historical only.

## Authoritative Architecture Sources

```text
Goat OS product feature phases
  context/product/goat-os-feature-phases.md

Detailed phase PRD/TRD docs
  docs/phases/

Goat OS coverage checklist (frozen migration audit, not living architecture)
  context/product/goat-os-coverage-map.md

General/Slack source findings
  context/source-findings/drive-docs-findings.md

Promise Keeper assignment source findings
  context/source-findings/assignment-promise-keeper-findings.md

Final architecture
  context/architecture/final-architecture.md

Forms and SOP engine
  context/forms/final-forms-sop-engine.md

Analytics, BI, AI, telemetry, infra cost controls
  context/analytics/final-analytics-infra.md

Frontend, mobile, backend adapter architecture
  context/frontend/final-frontend-mobile-backend-architecture.md

AI agent context and API protocol decisions
  context/agents/ai-agent-context-and-protocols.md

Backend stack ADR
  docs/decisions/go-backend-stack.md

Goat OS agent skill bundle and reference map
  .agents/skills/goatos-build/SKILL.md

Environment, load testing, and doc hygiene
  context/execution/env-load-test-and-doc-hygiene.md

Two-developer build plan
  context/execution/two-dev-build-plan.md

Target repo structure
  context/execution/target-repo-structure.md

Next execution artifacts
  context/execution/next-contracts.md
```

These files supersede older root-level planning docs where they disagree. In particular, old staging labels are historical planning notes, not the final architecture. The final position is: forms, analytics, streaming, telemetry, verification, and cost controls are production-grade from the start; the system grows by configuration, modules, scale, and tuning under real load.

## Historical / Source Documents

```text
Historical planning files live outside this canonical context tree:
  docs/archive/planning-history/

Those files are superseded. Do not use them as build instructions unless a
human explicitly asks for historical comparison.
```

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
  Keep existing dashboard/mobile UI where useful.
  Build missing task-first Goat OS screens.
  Android Field App owns conditional SOP form execution; Slack forms are legacy/migration input only.

scratch vs salvage:
  from scratch = backend, data model, canonical truth, app-api
  salvage = proven UI/UX, chart components, video recorder, SOP overlay, offline upload queue
```
