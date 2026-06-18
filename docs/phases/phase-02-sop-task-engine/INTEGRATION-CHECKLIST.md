# Phase 2 Operator/SOP Integration Checklist

Status: coordinator checklist.

Run this after the Operator Admin/Backend, Operator Android, and SOP
Builder/Task Engine agents finish local work and before any dev deploy.

## Repo And Scope

- Target repo is `https://github.com/vgoats/goatos.git`.
- Branch/worktree contains only intended Goat OS changes.
- No live legacy repo was modified.
- No raw source rows, Slack exports, names, phone numbers, emails, media URLs,
  tokens, service-account JSON, or private dumps are committed.
- Each implementation still matches its PRD/TRD and AGENT-TASK file.

## Merge Order

1. Operator Management backend/contracts/admin feature.
2. SOP/task backend/contracts/admin builder feature.
3. Generated clients.
4. Operator Android app integration.
5. Shared route-shell/smoke integration by coordinator.
6. End-to-end Shifting proof.

## Shared Contracts

- Migration ranges are respected:
  - Operator Management: `000050`-`000059`
  - SOP/task engine: `000060`-`000069`
- No duplicate migration prefixes.
- Migrations apply cleanly to a fresh local Postgres database.
- OpenAPI ownership is respected:
  - Operator Management owns `/admin/operators*`,
    `/admin/operator-source-candidates*`, `/app/me`, `/app/bootstrap`,
    `/app/devices*`.
  - SOP/task owns `/admin/sops*`, `/admin/tasks*`, `/app/tasks*`,
    `/app/sop-versions*`.
- Shared `admin-api.yaml` and `app-api.yaml` edits were merged serially before
  generated clients were regenerated.
- SOP JSON Schemas exist and are contract-checked:
  - `sop-form-version.schema.json`
  - `sop-submission.schema.json`
  - `sop-preview-dry-run.schema.json`
  - `sop-proof-policy.schema.json`
- Generated API clients are regenerated and consumers compile.
- Route registry fails closed for all new protected routes.

## Operator Management Gate

- Active operator profile is required for Android bootstrap.
- Active DB grants are required; token role claims are not authority.
- Capability checks gate task/SOP visibility.
- Device/session state gates bootstrap and submit.
- Revoked/inactive profile, grant, capability, device, or app version blocks
  task execution.
- Legacy Slack/App Script submitter evidence imports only sanitized candidates.
- Source-candidate mapping/rejection is audited.
- Operator admin surface can create, activate, deactivate, inspect, and review
  profiles without raw DB edits.

## SOP/Task Gate

- Admin can create/edit/validate/publish/retire a Shifting SOP version.
- Builder preview and Android runner use the same pinned DSL semantics.
- Unsupported app field types/rule operators/proof actions block execution.
- Assigned task list returns only scoped, active, compatible tasks.
- Backend revalidates pinned SOP version, task state, operator profile, grants,
  capabilities, device/session, goat state, location state, proof policy,
  idempotency, and domain gates at submit time.
- Accepted Shifting writes module-owned movement/location events, not generic
  form-table mutations.
- Rework/rejection/void/correction paths preserve original and rectified proof.

## Android Gate

- Android app logs in and fetches bootstrap.
- Android renders only visible navigation/features from bootstrap.
- My Tasks, task detail, pinned SOP version download, dynamic form rendering,
  proof capture/upload state, offline draft, retry queue, and per-goat result
  errors work locally or in emulator/simulator proof.
- Offline validation is UX only; backend denial is displayed correctly after
  sync.
- No canonical data is stored in Zustand/MMKV beyond scoped cache/drafts/sync
  queue.
- No direct Firestore/GCS/DB writes exist in product code.

## Admin-Web Gate

- Operator Management route renders locally.
- SOP Builder route renders locally.
- Task/proof/rework review route renders locally or has an explicit Phase 2
  deferral note tied to PRD acceptance.
- Shared route shell contains final route labels and links once, added by the
  coordinator.
- If counter-family work is also in flight, shared shell/nav/smoke edits are
  serialized across both programs.
- Desktop and narrow screenshots are reviewed for changed admin-web routes.
- No visible `Goat OS`/`VGoat` token leaks in rendered admin-web UI.

## Source And Legacy Gate

- Shifting legacy source fields/rules were checked against
  `shifting_death_automation.js` or sanitized source findings.
- Legacy submitter/assignee/uploader/verifier signals were inventoried into
  sanitized candidate evidence.
- Slack remains notification or temporary bridge only.
- No inbound Slack bridge bypasses Goat OS auth, permissions, validation, audit,
  idempotency, or domain gates.

## Scale And Observability

- Operator list, source-candidate review, bootstrap, task queue, SOP version
  fetch, submission history, and proof queues are indexed and bounded.
- Query-plan or equivalent proof exists for hot reads.
- Media upload path uses backend-issued upload intents; API does not proxy video
  bytes.
- Metrics/logs exist for bootstrap failures, denied submissions, task queue lag,
  proof/media failures, outbox lag, DLQ/error counts, and DB pressure.
- No API loads all operators, all tasks, all goats, all submissions, or all
  source candidates into memory.

## Required Validation

Run the narrowest relevant commands from changed areas, including where present:

```text
make validate-migrations
make api-client-generate
make api-client-check
go test ./internal/workforce/... ./internal/sop/... ./internal/tasks/... ./internal/submissions/... ./internal/movement/... ./internal/permissions ./internal/bootstrap
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run build
npm --prefix apps/admin-web run smoke:visual:live
```

Operator-mobile validation commands must be added by the Android agent once the
package lands.

## Dev Deploy Gate

Dev deploy is allowed only after:

- local migrations pass
- generated contracts/clients are current
- backend tests pass for changed modules
- admin-web checks and visual proof pass
- Android local/emulator proof exists
- end-to-end Shifting path works with active Operator Management bootstrap
- integration checklist has no unresolved required blockers

Individual feature agents must not deploy.
