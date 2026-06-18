# Phase 2 Operator/SOP Parallel Agent Runbook

Status: execution guide for parallel agents.

Use this when splitting Phase 2 into small prompts for multiple agents. The PRD
and TRD files remain the product and technical contract; this runbook is the
coordination layer so agents can work in parallel without overwriting each
other.

## Simple Coordinator Prompt

```text
Read AGENTS.md, SKILLS.md, context/README.md,
.agents/skills/goatos-build/SKILL.md,
docs/phases/phase-02-sop-task-engine/PARALLEL-AGENT-RUNBOOK.md,
docs/phases/phase-02-sop-task-engine/INTEGRATION-CHECKLIST.md,
and the AGENT-TASK file for your assigned track.

Implement only your assigned track. Do not edit another track's owned files.
Do not deploy. Do not commit raw private source data. Produce local tests,
contract checks, visual/mobile proof where applicable, and a concise handoff
listing changed files and remaining blockers.
```

These prompts are intentionally small. The full instructions live in the
committed docs they point to, so agents should not need chat history or memory
from the coordinator to understand scope, ownership, validation, or handoff.

## Agent Split

Run these agents in parallel:

- Operator Admin/Backend Agent:
  `docs/features/operator-management/AGENT-TASK-ADMIN.md`
- Operator Android Agent:
  `docs/features/operator-management/AGENT-TASK-ANDROID.md`
- SOP Builder/Task Engine Agent:
  `docs/phases/phase-02-sop-task-engine/AGENT-TASK-SOP-BUILDER.md`

Parallel work is allowed, but final integration order is:

```text
Operator Management backend/contracts
  -> SOP/task backend/contracts
  -> admin-web route shell integration
  -> operator-mobile integration
  -> end-to-end Shifting proof
```

Android may build against mocks while backend contracts are in flight. SOP
Builder may build admin UI against typed mocks while migrations/API contracts are
in flight. Final acceptance requires generated clients and real backend APIs.

## Shared-Resource Partition

The current committed migration max was `000023` when these docs were written.
The counter-family docs reserve `000024`-`000049`. Preserve these higher ranges
for Phase 2 unless the coordinator updates all task docs before fan-out.

| Agent | Migration range | OpenAPI ownership | Frontend/mobile ownership |
| --- | --- | --- | --- |
| Operator Admin/Backend | `000050`-`000059` | `contracts/openapi/admin-api.yaml` paths under `/admin/operators*` and `/admin/operator-source-candidates*`; `contracts/openapi/app-api.yaml` paths under `/app/me`, `/app/bootstrap`, `/app/devices*` | `apps/admin-web/features/operators/**`; may add local feature components only |
| SOP Builder/Task Engine | `000060`-`000069` | `contracts/openapi/admin-api.yaml` paths under `/admin/sops*`, `/admin/tasks*`; `contracts/openapi/app-api.yaml` paths under `/app/tasks*`, `/app/sop-versions*`; SOP JSON Schemas | `apps/admin-web/features/sops/**`, `apps/admin-web/features/tasks/**` |
| Operator Android | none unless explicitly approved | consumes generated app API client; propose contract gaps to coordinator instead of editing OpenAPI by default | `apps/operator-mobile/**`, mobile packages/adapters assigned in task doc |

Coordinator owns shared route-shell files unless explicitly delegated:

```text
apps/admin-web/components/layout/app-sidebar.tsx
apps/admin-web/components/layout/navbar.tsx
apps/admin-web/lib/routes.ts
apps/admin-web/scripts/smoke-visual-live.mjs
packages/api-client generated outputs after contract merge
```

If an agent exhausts its migration range, it must stop and ask for a new
reserved range. Do not take another agent's range.

## Shared Rules

- Read assigned PRD/TRD/task docs in full before editing.
- Stay in assigned ownership unless the task doc explicitly permits a shared
  file.
- Do not revert changes made by another agent.
- Do not commit, push, create a PR, deploy, change cloud config, or change
  GitHub/IAM/billing from a feature-agent thread unless the coordinator
  explicitly asks in that thread.
- If a required change falls outside owned files, leave a precise integration
  note for the coordinator instead of editing the shared file opportunistically.
- Use generated clients and backend APIs; no frontend/mobile direct DB, BQ,
  Sheets, Firestore, or GCS writes.
- Do not commit raw private rows, Slack exports, phone numbers, emails, media
  URLs, tokens, service-account JSON, or local source dumps.
- All backend writes must be tenant scoped, idempotent where retryable, audited,
  RBAC protected, and observable.
- Android dynamic SOP forms are declarative DSL plus native components only. Do
  not ship arbitrary executable code to the app.
- Token claims and Slack membership are not permission authority. Database
  grants, active operator profile, capabilities, and backend gates are authority.
- Every hot query must be indexed and bounded for one-million-goat scale.
- Do not deploy to dev from individual agents.

## Coordinator Step 0

Before fan-out, record:

- target repo and branch
- clean/dirty files
- current highest migration number
- active reserved migration ranges
- whether counter-family work is running in parallel
- which shared files the coordinator will edit
- whether backend/admin-web/operator-mobile can run locally

Org-boundary rules still apply. No GitHub, Google Cloud, IAM, billing, deploy,
or config-changing command may run until the correct Mesha/VGoats context is
verified.

## Handoff Format

Each agent must end with:

```text
Changed files:
- path

Validation run:
- command/result

Artifacts:
- path, if any

Integration notes:
- route metadata
- OpenAPI/client generation needed
- migrations used
- known blockers
```

## Copy-Paste Agent Prompts

Operator Admin/Backend:

```text
Read AGENTS.md, SKILLS.md, context/README.md, .agents/skills/goatos-build/SKILL.md, docs/phases/phase-02-sop-task-engine/PARALLEL-AGENT-RUNBOOK.md, docs/phases/phase-02-sop-task-engine/INTEGRATION-CHECKLIST.md, docs/features/operator-management/PRD.md, docs/features/operator-management/TRD.md, and docs/features/operator-management/AGENT-TASK-ADMIN.md. Implement only Operator Management backend/admin-web scope. Stay inside the owned files, migration range, and API ownership in the task doc. Do not edit SOP Builder, operator-mobile, shared route shell files, commit, push, create a PR, or deploy unless the coordinator explicitly asks. Final handoff: changed files, validation, artifacts, route metadata, migrations used, blockers.
```

Operator Android:

```text
Read AGENTS.md, SKILLS.md, context/README.md, .agents/skills/goatos-build/SKILL.md, docs/phases/phase-02-sop-task-engine/PARALLEL-AGENT-RUNBOOK.md, docs/phases/phase-02-sop-task-engine/INTEGRATION-CHECKLIST.md, docs/features/operator-management/PRD.md, docs/features/operator-management/TRD.md, docs/phases/phase-02-sop-task-engine/PRD.md, docs/phases/phase-02-sop-task-engine/TRD.md, and docs/features/operator-management/AGENT-TASK-ANDROID.md. Implement only operator-mobile Android bootstrap/task/SOP runner scope. Do not edit backend, migrations, OpenAPI, admin-web, SOP Builder internals, commit, push, create a PR, or deploy unless the coordinator explicitly asks. Use typed mocks where backend contracts are not landed. Final handoff: changed files, validation, screenshots or emulator notes, mocked API gaps, blockers.
```

SOP Builder/Task Engine:

```text
Read AGENTS.md, SKILLS.md, context/README.md, .agents/skills/goatos-build/SKILL.md, docs/phases/phase-02-sop-task-engine/PARALLEL-AGENT-RUNBOOK.md, docs/phases/phase-02-sop-task-engine/INTEGRATION-CHECKLIST.md, docs/phases/phase-02-sop-task-engine/PRD.md, docs/phases/phase-02-sop-task-engine/TRD.md, docs/phases/phase-02-sop-task-engine/SOP-CLOSEOUT.md, docs/phases/phase-02-sop-task-engine/AGENT-TASK-SOP-BUILDER.md, and docs/features/operator-management/PRD.md. Implement only SOP/task backend contracts and admin-web SOP Builder scope. Stay inside the owned files, migration range, and API ownership in the task doc. Do not edit Operator Management internals, operator-mobile, commit, push, create a PR, or deploy unless the coordinator explicitly asks. Final handoff: changed files, validation, artifacts, route metadata, migrations used, blockers.
```
