# Mobile recording-form + proof capture — build notes (branch `feat/mobile-recording-form`)

Lets an operator actually record a vaccination drive: fill the SOP form → capture
video proof → submit real answers. Backend is fully ready; this is client-only.

## Why it was blocked
`SubmitViewModel` sent EMPTY answers, so every submit hit the backend's
"required"/"proof_required" validation and `SyncEngine` substituted the honest
"form capture lands in a later build" message. The app never fetched or modeled the
form schema.

## Backend contract (already built — do NOT change)
- `GET /app/tasks/{task_id}` → `TaskResponse { task, sop_version{ form_dsl, proof_policy }, submissions }`.
  `form_dsl` = `{ schema_version:"goatos.sop-form.v1", fields:[…], rules:[…] }`.
  - field: `{ key, label, type, required, repeat?, options?[] }`
    types: `boolean · number · text · goat_scan|goat_lookup|animal_id_scan ·
    vaccine_batch_picker · location_picker · video_proof`
  - rule: `{ type, field?, when:{ field, operator, value? }, message? }`
    types: `visible_if · required_if · enabled_if · proof_required_if ·
    block_submission_if · requires_supervisor_if`
- Proof (signed-URL, direct-to-GCS — bytes never go through the API):
  `POST /app/proofs/uploads` → `{ proof{proof_id}, upload_url, upload_method, headers, expires_at }`
  → `PUT <upload_url>` (raw bytes; local-dev fallback endpoint `PUT /app/proofs/{id}/upload?tenant_id=...&expires=...&sig=...`)
  → `POST /app/proofs/{id}/complete`.
- Submit: `POST /app/tasks/{task_id}/submissions` with `{ answers:{key→value}, proof_refs:[…] }`.
- Perms: proof endpoints are `TaskExecute`-gated.

## Done on this branch
- **1/6 data contract** (`955a9507`): `AppApi.getAppTask` + `TaskDetailResponseDto`/`SopVersionDto`.
- **2/6 form domain** (`1b52f359`): `core-data/forms/FormSpec.kt` — `Map<form_dsl>.toFormSpec()`
  → typed fields + rules (alias-normalized, unknown-type-safe). `TasksRepository.taskDetail(taskId)`.
  5 parser tests.

## Remaining
- **3/6 form runner UI** — render `FormSpec.fields` by type in the record/submit screen:
  boolean→toggle, number→numeric field, text→text field, `goat_scan`→reuse the existing
  RFID scan flow (`feature-scan`), pickers→dropdown (options inline or fetched), `video_proof`
  →launch camera. Honor `visible_if`/`required_if`/`proof_required_if`; `block_submission_if`
  disables submit with the rule's message. **Match the mock** `mock/vaccination-mobile-mock.html`
  (record/submit screen). Keep chrome Hilt-free params like the drawer/offline-banner did.
- **4/6 camera + proof upload** — CameraX video capture (CAMERA perm already declared; add
  CameraX to `libs.versions.toml`). New `ProofUploader`: CreateUpload → raw OkHttp `PUT` to
  `upload_url` (NOT a Retrofit fixed-path call) → Complete → `ProofReferenceDto`. Add
  `completeProof` to `AppApi`.
- **5/6 submit wiring** — `SubmitViewModel` collects answers + proof_refs from the runner,
  sends a real `SubmitTaskRequestDto`; delete the empty-answers path. Keep the outbox/idempotency
  path intact (enqueueShedSubmit already carries the request).
- **6/6 tests + Paparazzi golden** for the runner; a submit-with-answers ViewModel test.

## Guardrails
- Golden frontend rule: backend owns field labels/types/options — never invent form fields;
  render only what `form_dsl` declares. Unknown type → read-only placeholder.
- Work in THIS worktree (own git index) — main checkout is edited by a parallel Firebase
  session (auth/FCM/config). Rebase onto latest main before the final merge.
