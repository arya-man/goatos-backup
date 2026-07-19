# Proof Video And Verification Flow

Status: current-state + target contract, grounded in the Android app, app API,
admin API, and backend RBAC as of 2026-07-17.

This document answers one question clearly: whether Android proof video is
complete, and which roles may capture, submit, approve, reject, or only read.

## Current Answer

Android video proof is implemented as a full backend-integrated flow in this
branch. It is not just camera UI.

The backend proof-media foundation exists:

- `POST /app/proofs/uploads` creates a server-owned proof record and returns a
  signed upload target.
- `PUT <upload_url>` uploads the media bytes directly to storage. In local dev,
  the returned target can be the local fallback route
  `PUT /app/proofs/{proof_id}/upload?tenant_id=...&expires=...&sig=...`.
- `POST /app/proofs/{proof_id}/complete` finalizes the proof with metadata.
- Local proof downloads resolve to a signed backend route
  `GET /app/proofs/{proof_id}/download/signed?tenant_id=...&expires=...&sig=...`
  so admin-web/video playback can stream from Ravi's laptop without GCS/CDN.
- GCS storage can issue signed PUT/GET URLs when the backend is configured with
  `GOATOS_MEDIA_STORAGE=gcs`.

The Android app now has the integration pieces:

- CameraX live in-app video recording into app-private storage;
- `video_proof` fields rendered from backend `form_dsl`;
- durable Room/outbox proof rows;
- `POST /app/proofs/uploads` proof registration;
- raw byte `PUT` to the returned signed `upload_url`;
- `POST /app/proofs/{proof_id}/complete` after upload;
- submit gating that waits for `PROOF_UPLOAD` to reach `SYNCED`;
- task submission with only completed server `proof_refs`, never local Room ids.

## Backend Integration Scope

Yes: backend integration is part of the Android proof-video plan.

This is not a camera-only UI task. The Android work is only done when the native
client is integrated with the backend task, proof, upload, completion, and review
contracts.

Backend pieces already available:

- task detail read: `GET /app/tasks/{task_id}`;
- SOP form/proof policy source: `TaskResponse.sop_version.form_dsl` and
  `TaskResponse.sop_version.proof_policy`;
- proof upload registration: `POST /app/proofs/uploads`;
- direct media upload target: returned `upload_url`, `upload_method`, and
  required headers;
- proof completion: `POST /app/proofs/{proof_id}/complete`;
- task submission: `POST /app/tasks/{task_id}/submissions`;
- verifier queue/verdict: `GET /verification/queue` and
  `POST /verification/items/{item_id}/verdict`;
- scoped leadership closeout: `GET /verification/action-queue` and
  `POST /verification/submissions/{submission_id}/close`;
- RBAC gate map: `TaskExecute` only for ground-operator scan/proof/finalize,
  `VerificationReview` for the verifier, and `VerificationAct` for scoped
  Park Head/Director/CEO/CxO closure.

Android integration completed in this branch:

- fetch task detail and render the real backend `form_dsl`;
- evaluate required form/proof rules locally for UX while relying on server
  validation as authority;
- register a proof before uploading media;
- upload the captured video bytes to the returned signed URL, not to a hardcoded
  Retrofit path;
- complete the proof after upload and persist the returned proof reference;
- submit real answers and completed proof refs through the outbox;
- show backend rejection/rework reasons when validation or review fails;
- keep all writes idempotent and retryable without duplicate proof artifacts or
  duplicate task submissions.

## Target Execution Flow

The target operator flow is:

1. Operator signs in to the one common Goat OS Android app.
2. App calls `/app/bootstrap` and receives the runtime role lens, grants, scope,
   visible navigation, and disabled reasons. Roles are runtime data, not app
   flavors.
3. Operator opens assigned work from `/app/tasks` or the vaccination execution
   route.
4. App fetches task detail from `/app/tasks/{task_id}` so it has the pinned SOP
   version, `form_dsl`, `proof_policy`, and prior submissions.
5. Operator fills the backend-declared form fields only. Android must not invent
   fields, labels, proof subjects, options, or required logic.
6. For each scanned goat, capture one or more clips from that goat row:
   - launch CameraX video capture;
   - store the captured file locally while offline if needed;
   - call `POST /app/proofs/uploads` with proof type, MIME type, scope, subject,
     and metadata;
   - upload bytes with raw `PUT` to the returned `upload_url` using the returned
     headers;
   - call `POST /app/proofs/{proof_id}/complete` with size, duration, MIME type,
     hash/storage metadata;
   - keep the returned `ProofReference` in the submission state.
7. Every scan and clip is already draft-synced. Operator finalizes with
   `POST /app/tasks/{task_id}/submissions` containing real answers and
   completed goat-subject proof refs; finalization is not a bulk upload.
8. Backend revalidates form version, required answers, proof policy,
   permissions, task state, row version, idempotency, and workflow gates.
9. Finalized submission creates one verifier item per goat. Rejection opens
   goat rework; after every goat is approved, scoped leadership closes the
   drive atomically.

Video bytes must not proxy through the Goat OS API. The API creates and
completes proof records; storage receives the raw media bytes directly.

## Approve / Reject Meaning

Product language:

- **Approve** means the verifier accepts one goat's submitted proof.
- **Reject** means the verifier requests rework from the operator for that goat.

Current API language:

- `POST /verification/items/{item_id}/verdict` = verifier approve or reject.
- Reject requires a reason and keeps that goat vaccination open for rework.
- `POST /verification/submissions/{submission_id}/close` = leadership closes
  the drive atomically after every goat proof is approved.

Backend review preconditions:

- Task must be in `submitted` or `needs_review`.
- A reviewable SOP submission must exist.
- If the SOP version proof policy requires proof, the latest reviewable
  submission must contain completed proof refs.
- Expected proof subjects must be present.
- Vaccination completion fanout must be wired and have at least one reviewable
  completion when the task requires completion fanout.

State outcomes:

- Approve records independent proof acceptance but does not yet accept the
  medical completion.
- Reject records rework and reopens that goat completion.
- Leadership drive closure fans out accepted completion state and retains the
  operator's original `administered_at` as the vaccination date.
- Rework must notify or surface the operator/park owner so the field work can be
  redone with corrected proof.

## Role Model

The canonical roles in the current permission map are:

- `operator`
- `verifier`
- `park_head`
- `pc_director`
- `admin`
- `ceo_internal`

RBAC is server-authoritative. Android and admin-web may hide buttons, but the
backend route permission and task/scope checks decide the truth.

| Role | Can capture / submit assigned proof? | Can approve proof? | Can reject / request rework? | Notes |
| --- | --- | --- | --- | --- |
| `operator` | Yes, through `TaskExecute`: create proof upload, upload/complete proof, submit task. | No. | No. | On the proof/verification axis: executes assigned field work, cannot self-close or verify. This is NOT a blanket "operators never write lifecycle" rule — operators also record birth/death in the Counts module (see `docs/runbooks/android-dev-device.md`); that write is out of scope for this table. |
| `verifier` | Not in the current coarse permission map: no `TaskExecute` and no `AppBootstrap` by default. | Yes, through `TaskVerify`. | Yes, through `TaskVerify`. | Current product rule says verifier reviews via admin-web. If verifier mobile review is desired, bootstrap/app access must be reconciled. |
| `park_head` | Yes at coarse RBAC level (`TaskExecute`) and can use app bootstrap. | Yes at coarse RBAC level (`TaskVerify`). | Yes at coarse RBAC level (`TaskVerify`). | Product flow should use this for leadership follow-up/review, not self-approval of the same work. |
| `pc_director` | Yes at coarse RBAC level (`TaskExecute`) and can use app/admin bootstrap. | Yes at coarse RBAC level (`TaskVerify`). | Yes at coarse RBAC level (`TaskVerify`). | Director/CxO tier can close and verify; use scope and separation-of-duty checks. On the Counts axis (out of scope for this table): `pc_director` holds neither `counts.read` nor `counts.write` as of the 2026-07-18 decision, so the Counts module does not appear in its nav at all — see `docs/runbooks/android-dev-device.md`. |
| `admin` | Yes. | Yes. | Yes. | Full admin authority for setup, task assignment, SOP, and review. |
| `ceo_internal` | Yes. | Yes. | Yes. | Superuser-style internal role; same separation-of-duty rule still applies unless an explicit override is approved. |

Important: the role table above has two layers. Coarse RBAC answers "does this
role have the route permission?" The product workflow must also enforce scope,
assignment, current task state, row version, proof policy, and separation of
duty.

## Separation Of Duty

The expected product rule is:

- capture/submit is field execution;
- approve/reject is independent verification;
- an operator must not approve their own proof;
- a leadership role with both execute and verify permissions must not use both
  sides on the same task unless an explicit override workflow exists and is
  audited;
- verifier review should record who reviewed, when, reason, result, and the
  proof/completion IDs affected.

If a future flow allows mobile review by `park_head`, `pc_director`,
`ceo_internal`, or `verifier`, the app must show reviewer actions only when the
backend bootstrap/action contract says the current actor can review that
specific task. It must not decide from role strings alone.

## Surface Rules

Operator Android:

- can see assigned work and allowed shed/park scope;
- can capture video/photo proof once implemented;
- can upload/complete proof through the app API signed-upload flow;
- can submit task responses;
- can see queued/syncing/failed/submitted/rework status;
- cannot approve, reject, publish SOPs, edit proof policy, or mutate protocol
  rules.

Leadership Android today:

- role-aware read/follow-up surfaces may show drive status, overdue work,
  coverage, data gaps, roster/assignment context, and allowed actions returned by
  bootstrap;
- current verification closeout is documented as admin-web, not a completed
  mobile reviewer flow.

Admin-web:

- owns SOP authoring/publish, task assignment, proof/rework queues, and current
  approve/reject review actions;
- must not provide browser camera capture for field proof;
- can only use proof/review actions when a real `task_id`, submission, row
  version, completed proof refs, and reviewable completion context exist.

Backend:

- owns permissions, scope, row-version conflict detection, idempotency, proof
  policy validation, submission validation, audit, review fanout, and storage
  provider selection;
- must fail closed when proof, task, scope, role, row version, or fanout context
  is missing.

## Implementation Checklist To Call Android Done

Android proof video is done only when all of these are true:

- `video_proof` fields render from backend `form_dsl` in the real submit flow.
- CameraX records a local video file behind `device-camera`.
- Upload registration calls `POST /app/proofs/uploads`.
- Android uploads raw bytes to the returned signed `upload_url` with the
  returned headers.
- Android calls `POST /app/proofs/{proof_id}/complete`.
- The completed proof refs are added to `SubmitTaskRequestDto.proof_refs`.
- The task submission sends real answers, not empty answers.
- Offline outbox survives app restart and drains in order without duplicate
  proof or duplicate submission creation.
- Rejected proof returns to the operator as rework with the reviewer reason.
- Reviewer approve/reject remains server-authoritative and cannot be faked by
  UI state.
- Tests cover no-proof rejection, successful proof upload + submit, upload
  retry, complete retry, and rework.

## Source Anchors

- Android status and missing work:
  `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/SubmitViewModel.kt`
  and `apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/sync/SyncEngine.kt`
- Proof upload DTO/contract:
  `apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/ProofUploadDto.kt`
- App API proof routes:
  `contracts/openapi/app-api.yaml`
- Admin approve/rework routes:
  `contracts/openapi/admin-api.yaml`
- Backend proof handlers/storage:
  `backend/internal/proof/adapters/http/handler.go` and
  `backend/internal/proof/adapters/storage/gcs/storage.go`
- RBAC permissions:
  `backend/internal/permissions/permissions.go` and
  `backend/internal/permissions/routes.go`
- Current mobile build notes:
  `docs/mobile/recording-form-build-notes.md`
