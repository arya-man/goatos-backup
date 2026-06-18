# Operator Management Admin/Backend Agent Task

Status: implementation task guide.

## Small Prompt

```text
Read AGENTS.md, SKILLS.md, context/README.md,
.agents/skills/goatos-build/SKILL.md,
docs/phases/phase-02-sop-task-engine/PARALLEL-AGENT-RUNBOOK.md,
docs/phases/phase-02-sop-task-engine/INTEGRATION-CHECKLIST.md,
docs/features/operator-management/PRD.md,
docs/features/operator-management/TRD.md,
and docs/features/operator-management/AGENT-TASK-ADMIN.md.

Implement only Operator Management backend/admin-web scope. Do not edit SOP
builder, operator-mobile, or shared route shell files unless this task doc says
so. Leave changed files, validation, artifacts, and blockers in your final
handoff.
```

## Scope

Own Operator Management backend and admin surface:

- workforce/operator migrations
- backend `workforce` module
- operator admin APIs
- app bootstrap APIs owned by Operator Management
- source-candidate import/review APIs
- device registration/revocation APIs
- permission registry entries for `operators.*` and `app.bootstrap`
- admin-web Operator Management feature module
- local tests and query-plan proof for this feature

Do not implement SOP DSL, Shifting domain commands, or Android native UI.

## Read First

- `docs/features/operator-management/PRD.md`
- `docs/features/operator-management/TRD.md`
- `docs/phases/phase-02-sop-task-engine/PRD.md`
- `docs/phases/phase-02-sop-task-engine/TRD.md`
- `context/architecture/final-architecture.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `docs/phases/phase-01-goat-passport/BUILD-STATUS.md`
- `backend/migrations/postgres/000001_phase_1_identity_foundation.sql`
- `backend/migrations/postgres/000023_phase_1_auth_email_grants.sql`

## Owned Files

Preferred ownership:

```text
backend/internal/workforce/**
backend/migrations/postgres/000050*_operator*.sql
backend/migrations/postgres/000051*_workforce*.sql
backend/migrations/postgres/000052*_operator*.sql
contracts/openapi/admin-api.yaml       # only /admin/operators* and /admin/operator-source-candidates*
contracts/openapi/app-api.yaml         # only /app/me, /app/bootstrap, /app/devices*
apps/admin-web/features/operators/**
apps/admin-web/app/(app)/operators/**  # if route modules follow this pattern
```

Shared files allowed only for required permission/bootstrap wiring:

```text
backend/internal/permissions/**
backend/internal/bootstrap/**
backend/internal/platform/auth/**
```

Do not edit shared admin-web shell/nav/smoke files. Provide route metadata to
the coordinator:

```text
label: Operators
href: /operators
icon: Users or UserCog
smoke path: /operators
```

## Migration Range

Use only:

```text
000050-000059
```

If another committed migration uses this range before you start, stop and ask
the coordinator for a new range.

## API Ownership

Admin API:

```text
GET    /admin/operators
POST   /admin/operators
GET    /admin/operators/{operator_id}
PATCH  /admin/operators/{operator_id}
POST   /admin/operators/{operator_id}/activate
POST   /admin/operators/{operator_id}/deactivate
GET    /admin/operators/{operator_id}/grants
POST   /admin/operators/{operator_id}/grants
POST   /admin/operators/{operator_id}/capabilities
DELETE /admin/operators/{operator_id}/capabilities/{capability_id}
GET    /admin/operators/{operator_id}/devices
POST   /admin/operators/{operator_id}/devices/{device_id}/revoke
GET    /admin/operator-source-candidates
POST   /admin/operator-source-candidates/{candidate_id}/map
POST   /admin/operator-source-candidates/{candidate_id}/reject
```

App API:

```text
GET  /app/me
GET  /app/bootstrap
POST /app/devices/register
POST /app/devices/{device_id}/heartbeat
```

Do not own `/app/tasks*` or `/app/sop-versions*`; those belong to the SOP
Builder/Task Engine agent.

## Backend Requirements

Implement or scaffold with tests:

- `workforce_members`
- `workforce_external_identities`
- `workforce_capabilities`
- `workforce_member_capabilities`
- `workforce_roster_assignments`
- `workforce_absences`
- `workforce_member_devices`
- `workforce_member_app_sessions` if needed for bootstrap audit/support
- active profile check
- active grant check through existing permissions module
- capability check
- device active/revoked check
- source-candidate hash/dedup and review state
- audit writes for profile, grant/capability, device, bootstrap denial, and
  source mapping events

Token claims and Slack membership are not authority.

## Admin-Web Requirements

Build a first practical Operator Management route:

- roster list/search
- profile detail
- profile status activate/deactivate controls
- grants/capabilities summary
- device/session list and revoke action
- source-candidate review list
- map/reject candidate actions
- empty/loading/error states

Use generated clients or server-side adapters. Do not call DB, BigQuery, Sheets,
Firestore, GCS, or Slack directly from admin-web.

## Source Discovery

The first implementation may produce a local/dev source-candidate importer or a
dry-run report. It must sanitize:

```text
source_system
source_flow
source_actor_ref_hash
source_actor_ref_type
observed_role_hint
observed_scope_hint
first_seen_at
last_seen_at
submission_count
confidence
open_review_reason
```

Do not commit raw names, emails, phone numbers, Slack IDs, media URLs, tokens,
or private rows.

## Required Proof

- migrations apply locally
- backend tests for active/inactive profile, grant, capability, device,
  source-candidate dedup, and bootstrap denial
- OpenAPI/client generation check
- admin-web typecheck/lint/build for changed UI
- visual QA for `/operators` desktop and narrow view if UI lands
- query-plan proof for operator list, source-candidate review, bootstrap, and
  device heartbeat

## Done Criteria

This track is ready for integration when:

1. Active operator profile, grant, capability, and device state can gate
   `/app/bootstrap`.
2. Admin can manage operators and review source candidates without raw DB edits.
3. Source candidates are sanitized and auditable.
4. Revoked/inactive profile, grant, capability, or device blocks bootstrap or
   task execution handoff.
5. Route metadata is handed to the coordinator.
6. Validation and query-plan proof are included in the handoff.
