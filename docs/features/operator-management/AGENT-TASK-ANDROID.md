# Operator Android Agent Task

Status: implementation task guide.

## Launch Route

Use the single launch prompt in
`docs/phases/phase-02-sop-task-engine/PARALLEL-AGENT-RUNBOOK.md` with
`ASSIGNMENT: operator-android`. This file is the track contract for scope,
ownership, validation, and handoff.

## Scope

Own the Android operator app surface:

- app skeleton or reuse from `procurement_app` patterns
- login/bootstrap states
- app bootstrap manifest client and local state
- visible navigation from bootstrap
- My Tasks and task detail
- pinned SOP version download/cache
- native DSL form runner integration
- scoped option-source cache
- offline drafts and idempotent retry queue
- proof capture/upload intent client flow
- denied/revoked/incompatible states
- Android validation and visual/emulator proof

Do not own backend persistence, migrations, admin-web, or OpenAPI route design.

## Read First

- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `context/forms/final-forms-sop-engine.md`
- `docs/features/operator-management/PRD.md`
- `docs/features/operator-management/TRD.md`
- `docs/phases/phase-02-sop-task-engine/PRD.md`
- `docs/phases/phase-02-sop-task-engine/TRD.md`
- `.agents/skills/goatos-build/references/frontend-mobile.md`
- `procurement_app/` as read-only reference from the workspace, if needed

## Owned Files

Preferred ownership:

```text
apps/operator-mobile/**
packages/mobile-forms-runner/**
packages/media-client/**
packages/device-client/**
```

If these packages do not exist, create only the minimum package/app structure
needed for the Android slice and document the bootstrap commands.

Shared generated client usage:

```text
packages/api-client
```

Do not hand-edit generated client files unless the repo already expects that
workflow. If contracts are missing, use typed mocks and record the exact API
gap in the handoff.

If a generated-client or shared package change is required after contracts land,
document the exact command/file expectation for the coordinator.

## Do Not Edit

```text
backend/**
contracts/openapi/**
backend/migrations/**
apps/admin-web/**
docs/features/operator-management/AGENT-TASK-ADMIN.md
docs/phases/phase-02-sop-task-engine/AGENT-TASK-SOP-BUILDER.md
```

## Android Product Requirements

Build the task-first surface:

- login screen or authenticated entry state
- bootstrap loading, denied, incompatible app, revoked device, and no-task states
- visible navigation driven by bootstrap
- My Tasks list
- task detail
- dynamic SOP runner from pinned DSL version
- goat scan/search placeholder or adapter boundary
- scoped location/medicine/feed/operator option pickers through option-source
  cache
- proof capture/upload state through backend-issued upload intents
- offline draft queue
- idempotent submit retry
- per-goat item errors after sync
- rework/rejected/submitted status
- own work history if API/mock supports it

## Dynamic SOP Rules

- Render only native-supported field types, rule operators, workflow states, and
  proof actions.
- If bootstrap marks an SOP version incompatible, show a clear blocked state and
  do not allow submit.
- Never evaluate arbitrary JavaScript from the backend.
- Client validation is UX only; backend revalidation is authority.
- Cache is scoped by actor, tenant, device, app version, task, and SOP version.
- Submitted immutable answers can only change through rework/correction flow.

## Offline And Media Rules

- MMKV/Zustand or equivalent local state may hold UI state, scoped option cache,
  drafts, and sync queue only.
- Do not store canonical goat/backend truth or tokens in general UI state.
- Do not upload media directly to Firebase/GCS without backend-issued upload
  intent.
- API process must not proxy video bytes.
- Retry queue must preserve idempotency key and media completion state.

## Mocking Rules

If backend contracts are not landed:

- create typed mock bootstrap responses
- create typed mock task and SOP version responses
- keep mock data under app-local fixtures
- mark mock-only paths clearly
- avoid hardcoding product permissions in component logic

The final handoff must list every mocked API and the expected real endpoint.

## Required Proof

Run the narrowest available mobile commands, such as:

```text
npm --prefix apps/operator-mobile run typecheck
npm --prefix apps/operator-mobile run lint
npm --prefix apps/operator-mobile test
```

If commands do not exist yet, add scripts or document why they are missing.

Visual/emulator proof:

- desktop browser is not enough for Android work
- provide emulator screenshots or React Native screen captures where possible
- verify login/bootstrap, task list, dynamic form, proof state, offline draft,
  retry/denied state, and narrow device layout

## Done Criteria

This track is ready for integration when:

1. Operator app can bootstrap from real or typed mock API.
2. Navigation and executable SOP versions come from bootstrap, not hardcoded
   role checks.
3. My Tasks and task detail render assigned work.
4. Native SOP runner renders a Shifting-compatible pinned DSL fixture.
5. Offline draft/retry and proof upload intent states exist.
6. Inactive/revoked/incompatible states block execution.
7. Validation and visual/emulator proof are included in the handoff.
