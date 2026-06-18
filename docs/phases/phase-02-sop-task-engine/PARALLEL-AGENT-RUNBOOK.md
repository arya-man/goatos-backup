# Phase 2 Operator/SOP Parallel Agent Runbook

Status: execution guide for parallel agents.

Use this when splitting Phase 2 into small prompts for multiple agents. The PRD
and TRD files remain the product and technical contract; this runbook is the
coordination layer so agents can work in parallel without overwriting each
other.

## Single Launch Prompt

```text
Repo: /Users/ravi/mesha/goatos
ASSIGNMENT: operator-admin | operator-android | sop-builder

Read AGENTS.md, SKILLS.md, context/README.md,
.agents/skills/goatos-build/SKILL.md, and
docs/phases/phase-02-sop-task-engine/PARALLEL-AGENT-RUNBOOK.md.

Follow the assignment route in the runbook. Do only that assignment. Do not
commit, push, create a PR, deploy, or change cloud/GitHub config. Leave the
handoff required by the runbook and task doc.
```

Do not paste feature scope, migration ranges, OpenAPI ownership, file lists,
validation commands, or done criteria into the launch prompt. Those details live
in this runbook, the integration checklist, and the assigned task doc. If an
agent needs more detail, it must read the referenced docs before acting.

## Agent Split

Run these agents in parallel:

- `ASSIGNMENT: operator-admin`:
  `docs/features/operator-management/AGENT-TASK-ADMIN.md`
- `ASSIGNMENT: operator-android`:
  `docs/features/operator-management/AGENT-TASK-ANDROID.md`
- `ASSIGNMENT: sop-builder`:
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

OpenAPI ownership is by path prefix, but `admin-api.yaml` and `app-api.yaml`
are shared physical files. Merge contract edits serially in the final
integration order, then regenerate `packages/api-client` once after both
backend contract tracks land.

Coordinator owns shared route-shell files unless explicitly delegated:

```text
apps/admin-web/components/layout/app-sidebar.tsx
apps/admin-web/components/layout/navbar.tsx
apps/admin-web/lib/routes.ts
apps/admin-web/scripts/smoke-visual-live.mjs
packages/api-client generated outputs after contract merge
```

If counter-family work is running at the same time, route-shell edits are shared
with that program's Locations/coordinator integration. Serialize shell/nav/smoke
edits across both programs; Phase 2 feature agents should hand off route
metadata instead of editing those files directly.

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
