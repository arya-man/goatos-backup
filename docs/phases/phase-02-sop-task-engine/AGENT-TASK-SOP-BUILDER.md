# SOP Builder And Task Engine Agent Task

Status: implementation task guide.

## Small Prompt

```text
Read AGENTS.md, SKILLS.md, context/README.md,
.agents/skills/goatos-build/SKILL.md,
docs/phases/phase-02-sop-task-engine/PARALLEL-AGENT-RUNBOOK.md,
docs/phases/phase-02-sop-task-engine/INTEGRATION-CHECKLIST.md,
docs/phases/phase-02-sop-task-engine/PRD.md,
docs/phases/phase-02-sop-task-engine/TRD.md,
docs/phases/phase-02-sop-task-engine/SOP-CLOSEOUT.md,
docs/phases/phase-02-sop-task-engine/AGENT-TASK-SOP-BUILDER.md,
and docs/features/operator-management/PRD.md for the bootstrap dependency.

Implement only SOP/task backend contracts and admin-web SOP Builder scope. Do
not edit Operator Management internals or operator-mobile. Do not commit, push,
create a PR, deploy, or change cloud/GitHub config unless the coordinator
explicitly asks in this thread. Leave changed files, validation, route metadata,
and blockers in your final handoff.
```

## Scope

Own the reusable SOP/task engine and admin builder:

- SOP JSON Schemas
- SOP definitions/version persistence
- deterministic DSL validation/evaluator or server-side validation foundation
- preview/dry-run endpoint
- publish/retire lifecycle
- task lifecycle and assignment contracts
- submission/idempotency contracts
- Shifting SOP v1 seed/fixture
- movement command handoff boundary
- admin-web SOP Builder
- admin-web task/proof/rework review surface
- local tests and query-plan proof for SOP/task paths

Do not own Operator Management profile/grants/devices/bootstrap implementation.
Consume that through interfaces/contracts.

## Read First

- `docs/phases/phase-02-sop-task-engine/PRD.md`
- `docs/phases/phase-02-sop-task-engine/TRD.md`
- `docs/phases/phase-02-sop-task-engine/SOP-CLOSEOUT.md`
- `context/forms/final-forms-sop-engine.md`
- `docs/features/operator-management/PRD.md`
- `docs/features/operator-management/TRD.md`
- `.agents/skills/goatos-build/references/forms-sop.md`
- `.agents/skills/goatos-build/references/contracts-events.md`

## Owned Files

Preferred ownership:

```text
backend/internal/sop/**
backend/internal/tasks/**
backend/internal/submissions/**
backend/internal/movement/**        # Shifting command boundary only
backend/migrations/postgres/000060*_sop*.sql
backend/migrations/postgres/000061*_tasks*.sql
backend/migrations/postgres/000062*_submissions*.sql
contracts/jsonschema/sop-form-version.schema.json
contracts/jsonschema/sop-submission.schema.json
contracts/jsonschema/sop-preview-dry-run.schema.json
contracts/jsonschema/sop-proof-policy.schema.json
contracts/openapi/admin-api.yaml    # only /admin/sops* and /admin/tasks*
contracts/openapi/app-api.yaml      # only /app/tasks* and /app/sop-versions*
apps/admin-web/features/sops/**
apps/admin-web/features/tasks/**
```

Shared files allowed only for required route/permission/bootstrap wiring:

```text
backend/internal/permissions/**
backend/internal/bootstrap/**
packages/forms-dsl/**
```

Do not edit Operator Management internals:

```text
backend/internal/workforce/**
apps/admin-web/features/operators/**
docs/features/operator-management/AGENT-TASK-ADMIN.md
```

Do not edit `apps/operator-mobile/**`.

If a shared shell, generated-client, or Operator Management contract change is
required, document the exact file and expected edit in the handoff for the
coordinator.

## Migration Range

Use only:

```text
000060-000069
```

If another committed migration uses this range before you start, stop and ask
the coordinator for a new range.

## API Ownership

Admin API:

```text
GET    /admin/sops
POST   /admin/sops
GET    /admin/sops/{sop_id}
POST   /admin/sops/{sop_id}/versions
GET    /admin/sops/{sop_id}/versions/{sop_version_id}
POST   /admin/sops/{sop_id}/versions/{sop_version_id}/dry-run
POST   /admin/sops/{sop_id}/versions/{sop_version_id}/publish
POST   /admin/sops/{sop_id}/versions/{sop_version_id}/retire
GET    /admin/tasks
GET    /admin/tasks/{task_id}
POST   /admin/tasks/{task_id}/assign
POST   /admin/tasks/{task_id}/verify
POST   /admin/tasks/{task_id}/rework
```

App API:

```text
GET  /app/tasks
GET  /app/tasks/{task_id}
GET  /app/sop-versions/{sop_version_id}
POST /app/tasks/{task_id}/submissions
```

Do not own `/app/bootstrap`, `/app/devices*`, `/admin/operators*`, or
`/admin/operator-source-candidates*`.

## Backend Requirements

Implement or scaffold with tests:

- SOP definition/version tables
- task tables and lifecycle states
- submission and per-goat item result tables
- proof policy references
- idempotency keys for mobile submits
- pinned SOP version validation
- server-side DSL validation
- dry-run/preview result shape
- publish/retire state transitions
- Shifting SOP v1 seed or fixture
- domain handoff to movement module for accepted Shifting
- audit/outbox for meaningful task/submission transitions

SOP engine must not become a generic table writer. Domain modules apply
canonical truth.

## Admin-Web Requirements

Build first practical SOP Builder:

- SOP list
- Shifting SOP version detail
- structured DSL editor
- field palette
- form canvas
- rule builder
- workflow/proof policy blocks
- Android preview pane
- scenario/dry-run validation result
- publish/retire controls
- task queue
- proof/rework review surface

Do not require a polished drag/drop canvas for v1, but workflow state blocks
must be visually clear enough for approvals, verification, rejection, rework,
and blocked-review paths.

Do not call DB, BigQuery, Sheets, Firestore, GCS, or Slack directly from
admin-web.

## Operator Management Dependency

Consume these as interfaces/contracts:

```text
active operator profile
active role/scope grant
capability
device/session state
app bootstrap compatibility
```

If Operator Management is not implemented yet, use a narrow fake port in tests
and document the integration seam. Do not duplicate workforce tables or grants
inside SOP/task modules.

## Required Proof

- migrations apply locally
- JSON schemas validate sample Shifting SOP and submission payloads
- OpenAPI/client generation check
- backend tests for publish/retire, dry-run, task assignment, submit,
  idempotency replay/conflict, stale version, permission denial, and rework
- admin-web typecheck/lint/build
- visual QA for SOP Builder and task/rework screens if UI lands
- query-plan proof for task queue, assigned tasks, submission history, and
  proof/rework queues

## Route Metadata For Coordinator

Provide this in handoff if routes land:

```text
label: SOP Builder
href: /sops
icon: Workflow or ClipboardList
smoke path: /sops

label: Tasks
href: /tasks
icon: ListChecks
smoke path: /tasks
```

## Done Criteria

This track is ready for integration when:

1. Admin can create/validate/publish/retire Shifting SOP v1.
2. Builder preview/dry-run and backend validation use the same schema semantics.
3. App task APIs return pinned SOP versions and task details through generated
   clients.
4. Submission path is idempotent and rejects stale/incompatible SOP versions.
5. Accepted Shifting handoff creates a movement-domain command/event boundary.
6. Rework/proof/rejection states are represented.
7. Validation, route metadata, and query-plan proof are included in the handoff.
