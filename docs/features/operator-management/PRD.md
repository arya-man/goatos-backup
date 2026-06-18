# Operator Management PRD

Status: draft for implementation review.

## Summary

Operator Management is the Goat OS workforce foundation for Android SOP
execution. It is a new product feature, not legacy dashboard parity.

Legacy Slack/App Script flows already contain submitter, uploader, assignee,
health-manager, shed-user, and count `staff` signals, but they do not provide a
canonical roster, login gate, role/scope model, device model, or Android feature
manifest. Goat OS needs those before operators can safely replace Slack forms
with Android SOP submissions.

Build relationship:

```text
Verified login
  -> Goat OS internal actor
  -> active operator profile
  -> role/scope grant + capability + roster state
  -> Android app bootstrap manifest
  -> assigned tasks + pinned SOP versions + offline option caches
  -> SOP submissions through Goat OS app API
```

Operator Management is separate from SOP forms because many modules need it:
SOP tasks, verification, health follow-up, vaccination, feed, count
verification, movement, media proof, device/RFID use, and escalation.

## References

- `context/architecture/final-architecture.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `context/forms/final-forms-sop-engine.md`
- `docs/phases/phase-02-sop-task-engine/PRD.md`
- `docs/phases/phase-02-sop-task-engine/TRD.md`
- `docs/phases/phase-02-sop-task-engine/SOP-CLOSEOUT.md`
- `docs/phases/phase-01-goat-passport/BUILD-STATUS.md`
- Existing auth/RBAC schema:
  `backend/migrations/postgres/000001_phase_1_identity_foundation.sql`
- Existing verified-email bootstrap:
  `backend/migrations/postgres/000023_phase_1_auth_email_grants.sql`
- Legacy source references, read-only:
  `<mesha-workspace>/slack-automation-scripts/`
- Local/private source references, read-only:
  `<mesha-workspace>/source-material/`

## Product Goals

- Maintain a canonical tenant-scoped roster of operators, park heads,
  supervisors, verifiers, and other goat-care workforce users.
- Gate Android login and app bootstrap by active Goat OS grants, active operator
  profile, role, location scope, capability, and device/session state.
- Let admins and park heads manage the minimum operational data needed for
  Android SOP execution: status, team, park/shed scope, capabilities, shifts,
  absence/backfill, device assignment, and escalation owner.
- Convert legacy Slack/App Script submitter signals into reviewed operator
  candidates without committing raw names, emails, phone numbers, or Slack IDs
  to the repo.
- Deliver Android features dynamically through backend app bootstrap: assigned
  tasks, pinned SOP versions, option-source caches, allowed proof/media actions,
  and operator-visible navigation.
- Keep permissions server-authoritative. The Android app may hide controls, but
  backend RBAC and workflow gates decide what an operator can do.
- Preserve audit for login, device registration, assignment, execution, proof,
  rework, revocation, and source-identity mapping.

## Non-Goals

- Do not build payroll HRMS, salary, compliance HR, or leave payroll in this
  feature.
- Do not grant operators BI/admin dashboard access by default.
- Do not make Slack user/channel membership the authority for Goat OS roles.
- Do not hardcode SOP availability or permission checks in the Android app.
- Do not ship arbitrary executable code from the backend to the native app.
  Dynamic forms are declarative DSL plus a native component registry.
- Do not commit raw Slack user exports, staff lists, phone numbers, emails,
  media URLs, or private rows.
- Do not let device possession alone authorize a canonical submission.

## Users

```text
Admin
  creates and maintains operator profiles, capabilities, grants, devices, and
  source-identity mappings.

Park head / supervisor
  manages scoped roster, assignment, absence, backfill, and escalations for
  their park/shed/team where granted.

Operator
  logs into Android, sees only assigned/current work, executes SOPs, uploads
  proof, handles rework, and reviews limited own/team work history.

Verifier
  reviews proof and can request rework where granted.

Source reviewer
  maps legacy Slack/App Script submitter signals to canonical operator profiles
  without committing raw private source rows.
```

## Required Scope Manifest

Production completion for Operator Management v1 requires:

- Admin-web Operator Management surface with searchable roster, profile detail,
  status, role/scope grants, capabilities, teams, shifts, absence/backfill,
  devices, sessions, audit, and source-identity review.
- Tenant-scoped backend APIs for roster CRUD, activation/deactivation,
  capability assignment, scope grants, device registration/revocation,
  app-bootstrap manifest, and source-candidate review.
- Android login/bootstrap path that proves the signed-in actor has an active
  operator profile and active scope grant before any SOP task is returned.
- Generated app/admin clients for the operator and bootstrap APIs.
- Source discovery job or script that inventories legacy Slack/App Script
  submitters and assignees into sanitized candidate evidence.
- Mapping workflow from legacy Slack/source identity to Goat OS actor/profile,
  with unresolved/conflicting candidates routed to review.
- Dynamic Android feature manifest that returns only the modules, task queues,
  SOP versions, option sources, and proof actions allowed for the actor.
- Offline cache rules: pinned SOP versions, scoped option sources, assigned
  tasks, and drafts can be cached; grants and permissions are revalidated by the
  backend on sync/submit.
- Audit and observability for login, app bootstrap, source mapping, grant
  changes, device registration/revocation, task assignment, and submission
  denial.

## Legacy Source Discovery

Before building production imports, inspect reachable legacy/source artifacts
and produce a sanitized operator-candidate inventory.

Known evidence patterns:

| Source pattern | Evidence to extract | Notes |
| --- | --- | --- |
| Slack interaction payloads | Slack `user_id`, action id, callback id, channel, timestamp. | Use for submitter/action attribution. |
| Slack/App Script rows | `submittedBy`, `uploadedBy`, verifier, assignee fields. | Normalize as source identities, not role truth. |
| Shed/user mappings | farm, shed, Slack user id, health-manager or feed assignment. | Candidate scope evidence only. |
| Count rows | `staff` by farm/shed/date/count row. | Candidate count-verification operator evidence. |
| Proof/video systems | uploader, verifier, rework/rectified proof actor. | Candidate proof capability evidence. |

Discovery output must be sanitized before commit:

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

Raw names, emails, phone numbers, Slack IDs, tokens, media URLs, and private row
payloads stay in local/private source stores or production databases with access
controls; they do not belong in committed fixtures.

## Android Product Surface

Operator Android v1 should be task-first:

- login
- app bootstrap/sync status
- My Tasks
- task detail
- dynamic SOP runner from pinned DSL version
- goat scan/search within actor scope
- scoped location/medicine/feed/operator option pickers
- proof capture/upload state
- offline drafts and retry queue
- submitted/rework/rejected status
- own work history and limited team history where granted
- device/session status and support diagnostics

The Android app can receive any supported SOP form permutation through the DSL,
but it must only render components from the native registry. If the backend
publishes a field type, proof action, rule operator, or workflow node that the
installed app cannot execute, bootstrap must mark the SOP version incompatible
instead of letting the operator submit a broken form.

## RBAC

Permission namespace:

```text
operators.read
operators.write
operators.activate
operators.deactivate
operators.map_legacy_source
operators.manage_device
operators.manage_capability
operators.manage_roster
operators.view_audit
app.bootstrap
```

Related Phase 2 task/SOP permissions remain owned by SOP/task modules:

```text
task.read
task.assign
task.execute
task.verify
sop.read
movement.execute
```

Initial role mapping:

- `admin` and `ceo_internal`: full Operator Management access.
- `park_head`: scoped roster, assignment, absence/backfill, and device view for
  granted park/shed/team scopes.
- `operator`: own profile, own active grants, own devices, app bootstrap, own
  assigned tasks, own submission history.
- `verifier`: proof/review queues where granted; no roster admin by default.

Token claims are not role authority. Active database grants and active operator
profile state are authority.

## Acceptance Criteria

Operator Management v1 is accepted when:

1. Admin can create, activate, deactivate, and inspect operator profiles without
   editing raw database rows.
2. Active role/scope grants and capabilities determine Android app bootstrap
   and task visibility.
3. Android app bootstrap returns only compatible features, pinned SOP versions,
   and scoped option-source cache descriptors.
4. Backend rejects task/SOP submissions from inactive operators, revoked grants,
   out-of-scope locations, unsupported app versions, and revoked devices.
5. Legacy Slack/App Script submitter evidence can be imported as sanitized
   candidates and mapped/rejected through review.
6. Device registration and revocation are audited, but device possession alone
   cannot authorize a submission.
7. All writes are idempotent where retryable and emit audit/outbox records where
   operationally relevant.
8. Query paths are bounded and indexed for one-million-goat scale and large
   task/operator histories.

## Open Product Decisions

- Which login mode is preferred for field operators first: verified email,
  phone OTP, managed device, or a staged combination?
- Which roles are allowed to invite/activate operators at park/shed scope?
- What is the minimum source evidence needed to trust a legacy Slack submitter
  as an active Android operator?
- Are shared devices allowed, or must every device be bound to one active actor
  at a time?
- How long should Android offline access remain usable after a grant is revoked
  but before the app reconnects?
- Which operator performance metrics are appropriate for admin/park-head review
  without turning the feature into payroll HRMS?
