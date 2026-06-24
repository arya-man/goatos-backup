# SOP + Vaccination Backend Handoff

Date: 2026-06-24

Purpose: give the next backend session one source of truth for building the SOP
backend foundation needed by PHC/Vaccination while Claude builds the SOP Library
frontend from the mock.

## Read First

Use these docs before changing code:

```text
AGENTS.md
SKILLS.md
context/README.md
context/frontend/current-admin-web-scope.md
context/forms/final-forms-sop-engine.md
.agents/skills/goatos-build/references/forms-sop.md
.agents/skills/goatos-build/references/backend-impl.md
.agents/skills/goatos-build/references/contracts-events.md
docs/protocol-engine/PHASE-0-CHECKLIST.md
docs/protocol-engine/IMPLEMENTATION-PLAN.md
docs/protocol-engine/obligation-engine.md
docs/protocol-engine/state-machines.md
docs/phc-vaccination/PRD.md
docs/phc-vaccination/TRD.md
docs/phc-vaccination/V1-FOUNDATION-SPEC.md
mock/goatos-dashboard-mock.html
```

Graphify/source cross-check already done for this handoff:

```text
Procurement DB [Goats]
  source_file: graphify-out/converted/Procurement DB [Goats]_dda03a25.md
  relevant nodes: Procurement SOP Selection DB Sheet,
  Procurement Health-Check Selection SOP, Selection Decision (Selected / Rejected),
  SOP Vitals Criteria, SOP Teeth Count Criteria, SOP Anaemia and Jaundice Check.

Mesha director/source docs
  relevant concept: central video verification/proof gating is cross-cutting.
```

Use procurement/handbook evidence to shape generic SOP semantics. Do not build
the Procurement vertical now.

## Non-Negotiable Scope Lock

The shared SOP/proof/media foundation can be generic. The visible product slice
is not generic yet.

For the current Admin/Data Ops `/sops` surface:

```text
show: vaccination SOPs only (`vaccination.drive`, `vaccination.*`)
hide: shifting, procurement, HR, counts, breeding, feed, inventory, and other
      non-vaccination SOP inventory
```

Domain chips may remain for mock fidelity only if non-vaccination domains are
zero/inactive/disabled and do not read as built product. The New SOP builder must
default to vaccination drive/session semantics: vaccine batch, cold-chain/proof,
verification, and repeat-per-goat. This rule applies to screenshots, handoff
notes, seeded frontend fixtures, visual tests, and review language.

Vaccination execution context (park/shed/stage/defer/blocker/owner context) is
NOT a separate Parks module; it renders inside /vaccination.

## Legacy Reference Audit

These legacy/source artifacts were checked for this handoff. Treat them as
evidence and UX salvage only; do not revive their runtime data paths.

```text
/Users/ravi/mesha/procurement_app/src/constants/sop.ts
  useful: timed SOP steps, sub-checks, gender-scoped steps, timestamp offsets.

/Users/ravi/mesha/procurement_app/src/components/SOPOverlay.tsx
  useful: operator-facing step overlay during video proof capture.

/Users/ravi/mesha/procurement_app/src/screens/SOPEditorScreen.tsx
  useful: add/remove/edit/reorder SOP steps, validation, dirty-state handling.

/Users/ravi/mesha/procurement_app/src/screens/VideoRecorderScreen.tsx
  useful: camera permission flow, timed step progression, local proof capture.

/Users/ravi/mesha/procurement_app/src/store/uploadQueueStore.ts
/Users/ravi/mesha/procurement_app/src/services/uploadQueue.ts
  useful: offline/local queue, upload progress, retry/backoff, stuck-upload
  recovery, storage-space checks.

/Users/ravi/mesha/slack-automation-scripts/procurement_db.js
  useful: procurement thread-to-proof matching, duplicate file guards, video
  prompt/writeback behavior.

/Users/ravi/mesha/slack-automation-scripts/video_verification_system.js
/Users/ravi/mesha/slack-automation-scripts/health_db_automation.js
  useful: video proof verification/rejection concepts, thread media matching,
  status mirroring, and treatment/diagnosis proof upload behavior.

/Users/ravi/mesha/dashboard/app/api/vaccination/route.ts
/Users/ravi/mesha/dashboard/app/(dashboard)/vaccination/page.tsx
  useful: legacy vaccination display columns only. Do not copy the BigQuery
  route or old dashboard UI into Goat OS.
```

Canonical Goat OS replacements:

```text
Firestore collections/config       -> Postgres sop_* + app/admin APIs
Firebase Storage direct uploads    -> signed media/proof upload API + GCS
Slack thread proof/writeback       -> proof artifacts + verification workflow
BigQuery vaccination_dashboard     -> Postgres projections/read models
old dashboard vaccination table    -> mock-matching PHC/Parks/Admin surfaces
```

## Current Decision

Vaccination is not done until SOP is real.

Build:

```text
Generic SOP version/proof foundation
  + Admin/Data Ops SOP Library route support
  + Vaccination Session SOP seed/version
  + protocol rule/version linkage to sop_version_id + proof_policy
  + batch/drive -> sop_task -> submission -> verification -> vaccination_completion
```

Do not build every SOP family now. The walking slice is vaccination.

## Existing Backend To Reuse

Already present from migration `000060_sop_task_engine_foundation.sql`:

```text
sop_definitions
sop_versions
sop_tasks
sop_submissions
sop_submission_items
movement_commands
```

Existing service/routes:

```text
backend/internal/sop
GET    /admin/sops
POST   /admin/sops
GET    /admin/sops/{sop_id}
POST   /admin/sops/{sop_id}/versions
GET    /admin/sops/{sop_id}/versions/{sop_version_id}
POST   /admin/sops/{sop_id}/versions/{sop_version_id}/dry-run
POST   /admin/sops/{sop_id}/versions/{sop_version_id}/publish
POST   /admin/sops/{sop_id}/versions/{sop_version_id}/retire
GET    /app/tasks
GET    /app/tasks/{task_id}
GET    /app/sop-versions/{sop_version_id}
POST   /app/tasks/{task_id}/submissions
```

Existing contracts:

```text
contracts/openapi/admin-api.yaml
contracts/openapi/app-api.yaml
packages/api-client/src/generated/*
```

Existing SOP validator/evaluator is a skeleton. It supports only a narrow set of
field types and minimal required/proof validation. Treat it as the base to
extend, not as final.

## Current Repo Status To Respect

Do not restart from the old Phase 0 assumption. As of this handoff, the repo has
these migration/module foundations in place:

```text
backend/migrations/postgres/000070_goats_provenance_lifecycle.sql
backend/migrations/postgres/000071_location_profiles.sql
backend/migrations/postgres/000072_inventory_foundation.sql
backend/migrations/postgres/000073_protocol_engine.sql
backend/migrations/postgres/000074_obligation_engine.sql
backend/migrations/postgres/000075_vaccination_module.sql
backend/migrations/postgres/000076_obligation_rescoped_event.sql
backend/migrations/postgres/000077_vaccination_submission_item_index.sql
backend/migrations/postgres/000078_vaccination_review_queue_index.sql
backend/migrations/postgres/000079_feed_direction_module.sql

backend/internal/protocol
backend/internal/obligation
backend/internal/inventory
backend/internal/vaccination
backend/internal/parks
backend/internal/sop
```

So the next session should inspect and extend what exists. Do not duplicate the
protocol/obligation/inventory/vaccination schemas or create parallel drive/task
tables.

Important current mismatch:

```text
000075 seeds a draft SOP definition/version:
  code: vaccination.drive
  label: Vaccination drive

But the seed is only a structural placeholder and its form_dsl uses "steps" and
old field names/types such as video, scan, yesno, datetime, review. The current
SOP service expects schema_version + fields and validates supported field types.
```

Pending backend work must replace/upgrade that seed into a valid canonical
`form_dsl` and `proof_policy` before treating it as executable.

Doc drift to keep in mind:

```text
Some active PHC/protocol docs were written while the repo stopped at 000060 and
describe 000070-000078 as future work. The repo now contains 000070-000079 and
matching protocol/obligation/inventory/vaccination/parks/feed packages. Use
those docs for product intent and state-machine shape, but verify current
schema/code before creating anything new.
```

## Backend Gaps To Close

### 1. Extend SOP DSL support

Docs and mock require these canonical semantics:

```text
fields:
  text
  number
  date_time
  boolean
  select
  multiselect
  goat_scan
  rfid_scan
  goat_lookup
  shed_picker
  cohort_picker
  vaccine_batch_picker
  medicine_picker
  session_picker
  photo_proof
  video_proof

rules:
  visible_if
  required_if
  enabled_if
  proof_required_if
  branch_to
  repeat_for_each_goat
  block_submission_if
  requires_supervisor_if
  validation_rule
  calculated_value

option sources:
  backend-owned named sources
  scoped by park/team/operator/task
  offline cache policy
```

Current `internal/sop/app/service.go` does not fully support those yet. Add the
missing supported field types and deterministic validation/evaluation in small
steps. Do not add arbitrary JavaScript or hidden network calls in rules.

### 2. Preserve repeat-per-goat as first-class

`repeat_for_each_goat` is not a normal field. The final model must support:

```text
one task/form -> many goats
one parent sop_submission
N sop_submission_items
N goat-linked module events/completions when applicable
per-goat idempotency
partial completion
offline resume
batch-level proof or per-goat proof depending on proof_policy
```

Vaccination needs this for shed/cohort drives.

### 3. Seed real Vaccination Session SOP skeleton

Allowed: seed a structural SOP/proof skeleton. Not allowed: seed invented
vaccine schedule values as approved protocol rules.

Use the mock and PHC docs to seed a `vaccination.session` SOP version roughly
like this:

```text
title: Vaccination Session
trigger: cron / protocol window
gates:
  video proof
  cold-chain check
  vaccine batch picker
  repeat for each goat
  per-goat idempotency

fields:
  vaccine_batch_picker / FEFO lot selection
  expiry valid check
  cold_chain_verified boolean
  goat_scan or rfid_scan
  dose_ml_given number
  route_site select
  adverse_reaction boolean
  administration proof video
```

Proof policy should cover:

```text
required: true
types: video
minimum_count
subject_scope: batch or per_goat
verify_before_apply: true
expected subjects: shed/cohort, vial/lot, administration, quantity
```

If the current backend cannot represent a field exactly, prefer extending the
DSL over pretending the feature works.

### 4. Close proof/media artifact wiring

Current repo reality observed on 2026-06-24: the backend now has a proof module,
server-issued proof records, local backend-owned storage, and GCS signed URL
storage selection behind `GOATOS_MEDIA_STORAGE`. The remaining work is to wire
that proof path into vaccination SOP submission, verification, rework, and E2E
tests without bypassing the backend proof contract.

The media/proof path must keep the same API contract in local and production:

```text
signed upload request
  -> proof_id/proof_ref created server-side
  -> object key/path controlled by backend
  -> content hash / size / mime / duration metadata captured
  -> subject scope recorded: batch, goat, shed, vial/lot, administration
  -> upload_state completed before verification can accept
  -> proof_refs on sop_submissions reference those proof records
  -> verifier sees proof metadata without trusting raw client URLs
```

Storage must sit behind a backend-owned media/proof port:

```text
production adapter:
  GCS signed upload/download URLs
  storage_provider = gcs

local/dev adapter:
  backend-owned local filesystem storage
  storage_provider = local
  suggested env:
    GOATOS_MEDIA_STORAGE=local|gcs
    GOATOS_LOCAL_MEDIA_DIR=.goatos-local-media/proofs

required proof record shape:
  proof_id
  storage_provider
  object_key/path
  content_hash/hash
  mime
  size_bytes
  duration_ms when available
  upload_state
  tenant_id
  scope_type/scope_id
  subject_type/subject_id
  uploaded_by
  created_at/uploaded_at
```

Local uploads still create server-issued proof records. Frontend/mobile must
never write directly to arbitrary local disk or GCS. Local proof viewing and
download should go through a backend route or signed/dev URL, not raw filesystem
paths. Tests should use temp directories or fake storage adapters, never real
GCS.

Do not copy Firebase Storage direct uploads, Slack file permalinks, or local
client file paths as the canonical proof model. Legacy paths are references
only.

### 5. Link protocol config to SOP

Protocol docs require:

```text
protocol_versions.sop_version_id
protocol_rules.sop_version_id
protocol_versions.proof_policy
protocol_rules.proof_policy
rule_dsl.schedule[].sop_version
rule_dsl.schedule[].proof_policy
```

Publish/impact preview must flag:

```text
missing sop_version
missing proof_policy
unsupported SOP compatibility
no assigned operator
effective-date conflicts
stock risks
```

Do not publish source values unless `source.review_status='approved'` and the
source is real. Draft/manual/extracted rules remain not source-backed and
generate no production work.

### 6. Wire drive execution end to end

Target chain:

```text
published protocol_version/rule
  -> obligation_instances
  -> obligation_batches grouped by shed/cohort
  -> one sop_task pinned to sop_version_id
  -> operator submits sop_submission + sop_submission_items
  -> verification accepts/rejects/rework
  -> vaccination_completions link obligation_id + batch_id + sop_submission_item_id
  -> obligation_status_events + outbox_messages
  -> inventory_stock_movements reserve/consume/release
  -> Parks/PHC/Action Center status projections
```

The batch is the drive/work unit. Do not create a parallel
`vaccination_drives` table.

### 7. Keep status model consistent

Action Center/Parks/PHC must continue to use:

```text
due
overdue
scheduled
in_progress
proof_pending
verification_pending
rejected
deferred
blocked
owner_missing
completed
```

SOP task states map into these statuses, but do not replace the obligation
status ledger.

## Million-Goat Scale Rules

Every backend query must be tenant-scoped, indexed, paginated/chunked, and
bounded in memory.

Hot paths that need plan validation:

```text
SOP admin list by tenant/status
task queue by assignee/state/due
task queue by scope/state/due
submission history by task
submission item history by goat
obligation due-window scan
batch-by-scope/status scan
Vaccination execution projection
FEFO lot pick
inventory movements by lot/batch
```

Do not add:

```text
full-table goat scans
unbounded task/submission scans
JSONB filters without supporting expression indexes on hot paths
dashboard raw BigQuery/Sheets reads
direct browser/mobile DB/GCS/Firestore access
```

If a new query can touch large tables, add/update `make validate-sqlc-plans`
coverage or a package-level exact `EXPLAIN` integration test.

## Full Pending Checklist

Use this as the task checklist for the new backend session:

```text
Repo/doc setup
  [ ] Check git status; preserve Claude/Codex dirty work.
  [ ] Re-read this handoff plus the Read First docs.
  [ ] Confirm /sops remains the new SOP Library only, not old /tasks.
  [ ] Check current migrations/code before trusting doc text that says
      protocol/obligation/inventory/vaccination are absent or future.

Legacy/reference audit
  [ ] Use procurement_app only for SOP overlay, camera, editor, and upload-queue
      behavior ideas; do not copy Firestore/Firebase runtime paths.
  [ ] Use Slack/App Script only for proof, rejection, correction, and thread
      matching semantics; do not keep Slack/Sheets as canonical execution.
  [ ] Use old dashboard vaccination screen only as a legacy display reference;
      do not copy BigQuery API routes or old UI structure.

Claude frontend integration
  [ ] Review Claude's /sops implementation against the mock.
  [ ] Confirm it uses real admin API helpers and generated types.
  [ ] Capture backend/API gaps found by Claude.

SOP DSL/backend
  [ ] Extend supported field types: boolean, goat_scan, shed_picker,
      cohort_picker, vaccine_batch_picker, medicine_picker, session_picker.
  [ ] Add aliases only when safe: yes/no -> boolean, RFID -> rfid_scan,
      shed picker -> location/shed picker. Do not hide unsupported semantics.
  [ ] Add declarative rule validation for visible_if, required_if, enabled_if,
      proof_required_if, block_submission_if, requires_supervisor_if.
  [ ] Model repeat_for_each_goat as a first-class DSL flag/section.
  [ ] Add deterministic dry-run behavior for required fields, visibility,
      proof policy, and blocked submission states.
  [ ] Keep backend as authority; client/offline evaluation is UX only.

Proof/media backend
  [x] Server-issued proof upload/proof artifact APIs exist in the current tree:
      /app/proofs/uploads, /app/proofs/{proof_id}/upload,
      /app/proofs/{proof_id}/complete, /app/proofs/{proof_id}/download.
  [x] Storage sits behind a media/proof port: GCS signed URLs in prod;
      backend-owned local filesystem adapter in dev.
  [x] Env shape exists: GOATOS_MEDIA_STORAGE=local|gcs and
      GOATOS_LOCAL_MEDIA_DIR=.goatos-local-media/proofs for local storage.
  [ ] Wire vaccination SOP submissions to require server-issued proof records:
      proof_id, storage_provider, object path/key, content_hash/hash, mime,
      size/duration, upload_state, subject_type/subject_id, tenant/scope,
      uploaded_by.
  [ ] Ensure sop_submissions.proof_refs points at server-issued proof records,
      not arbitrary Firebase/Slack/client URLs or raw local paths.
  [ ] Local proof viewing/download must go through backend routes or signed/dev
      URLs, not raw filesystem paths.
  [ ] Tests must use temp dirs or fake storage adapters, not real GCS.
  [ ] Gate verification on completed proof uploads and proof_policy.

Vaccination SOP seed
  [ ] Replace/upgrade the draft vaccination.drive seed from 000075 into valid
      schema_version + fields + proof_policy shape.
  [ ] Prefer a forward migration/backfill if 000075 is already applied in dev.
  [ ] Keep the seed structural only; no invented vaccine schedule values.
  [ ] Ensure the seed can be listed by /admin/sops and used by /sops UI.

Contracts/API
  [ ] Check admin OpenAPI SOP schemas against frontend needs.
  [ ] Add typed SOP metadata only if raw form_dsl/proof_policy parsing is not
      enough for the UI; otherwise avoid contract churn.
  [ ] Regenerate api client after OpenAPI changes.
  [ ] Run contract drift/generated-client checks.

RBAC/routes
  [ ] Confirm /admin/sops and /app/tasks routes are registered in permissions.
  [ ] Confirm SOP read/write/publish and task read/execute/verify grants match
      CEO/COO/admin/operator/verifier expectations.
  [ ] If local backend returns route_not_registered, restart the backend binary
      before blaming frontend.

Protocol/config linkage
  [ ] Ensure protocol_versions/protocol_rules sop_version_id and proof_policy
      are populated/validated for vaccination rules.
  [ ] Impact preview must flag missing SOP, missing proof policy, unsupported
      SOP compatibility, no operator, stock risk, and date-window conflicts.
  [ ] Publish must remain gated by source-backed approved rule data.

Execution wiring
  [ ] Verify batch creation spawns one sop_task pinned to the correct SOP version.
  [ ] Verify task submission creates sop_submission and per-goat
      sop_submission_items for repeat-per-goat vaccination drives.
  [ ] Verify proof-required submissions go to needs_review, not accepted.
  [ ] Verify proof accept/reject/rework transitions are reflected in SOP task,
      obligation, vaccination, Parks, and PHC read models.
  [ ] Wire accepted submission items to vaccination_completions with
      obligation_id, batch_id, goat_id, sop_submission_item_id, lot, dose,
      route/site, cold-chain, adverse reaction, and idempotency.
  [ ] Wire stock reserve/consume/release through inventory_stock_movements.
  [ ] Wire booster generation from actual administered_at after accepted
      completion.

Read models/status
  [ ] Vaccination execution context must show SOP/proof/verification status from
      real DB rows only.
  [ ] PHC/adherence must reflect completed, rejected, missed, deferred, blocked,
      proof_pending, verification_pending, owner_missing.
  [ ] Do not count canceled/dead/sold obligations as actionable.

Scale and tests
  [ ] Add or update indexed query-plan checks for any new large-table query.
  [ ] Run SOP, protocol, obligation, inventory, vaccination, parks, permissions
      tests.
  [ ] Run make validate-sqlc-plans.
  [ ] If contracts/frontend changed, run admin-web typecheck/lint/build and
      check:mock-fidelity.

End-to-end proof
  [ ] Restart local backend so new routes/RBAC are active.
  [ ] Seed or create one valid draft/published SOP and safe dev protocol rule.
  [ ] Run config -> obligation -> batch -> sop_task -> submission -> verify ->
      vaccination_completion -> Parks/PHC status smoke.
  [ ] Run live visual smoke when backend/admin-web can run.

Docs closeout
  [ ] Update this handoff if implementation changes the truth.
  [ ] Update context/forms/protocol/phc docs only when behavior differs from
      the current docs.
  [ ] Do not mark Vaccination done until SOP/proof/verification/completion is
      end-to-end.
```

## Backend Work Order For New Session

1. Verify current dirty tree status and avoid reverting Claude/Codex work.
2. Read the files listed in "Read First".
3. Skim the "Legacy Reference Audit" paths for behavior only; do not copy their
   Firebase/Slack/BigQuery runtime paths.
4. Inspect current `internal/sop`, `internal/protocol`, `internal/obligation`,
   `internal/vaccination`, `internal/parks`, OpenAPI contracts, and generated
   clients.
5. Extend SOP DSL validation/evaluation for missing field types and rule
   constructs needed by the SOP Library and Vaccination Session.
6. Confirm or add the minimal proof/media artifact API and storage port:
   GCS signed URLs in production, backend-owned local filesystem in dev,
   and `proof_refs` as server-issued records rather than arbitrary URLs.
7. Add/adjust OpenAPI schemas if the frontend needs structured SOP metadata
   rather than raw `form_dsl` parsing.
8. Upgrade the existing `vaccination.drive` seed/version into valid structural
   config only.
9. Add protocol publish/impact checks for SOP/proof linkage.
10. Wire batch creation/execution to pinned `sop_task` where missing.
11. Wire accepted SOP submission items to `vaccination_completions`, inventory
   movements, obligation status events, and outbox messages.
12. Update Parks/PHC read models only from real DB state, no mocks.
13. Run focused backend tests, contract generation/checks, plan checks, and
    admin-web typecheck/build if contracts change.

## Test Checklist

Backend:

```bash
go test ./internal/sop/... ./internal/protocol/... ./internal/obligation/... ./internal/vaccination/... ./internal/parks/... ./internal/permissions/...
make validate-sqlc-plans
```

Frontend/contracts when OpenAPI or generated clients change:

```bash
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run build
git diff --check
```

End-to-end acceptance when backend and Claude frontend are both ready:

```text
Admin creates/publishes SOP version
Config links vaccination protocol dose to sop_version/proof_policy
engine creates obligation + shed batch
batch spawns sop_task
operator submits proof
verifier accepts
vaccination_completion is written
stock movement is written
Parks row changes from proof/verification pending to completed
PHC/adherence views reflect the completion
```

## Do Not Do

```text
Do not build a generic old /tasks product page.
Do not revive old SOP UI or old admin primitives.
Do not build Procurement, Feed, Breeding, or Health SOP execution now.
Do not invent vaccine schedule values.
Do not mark vaccination done until SOP/proof/verification/completion is wired.
Do not treat Claude's frontend mock match as backend completion.
```
