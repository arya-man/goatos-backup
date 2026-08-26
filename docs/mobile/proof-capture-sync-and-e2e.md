# Vaccination Proof Capture, Room-First Sync, and E2E (MOB-002)

Status: design rule for the mobile vaccination Submit capture flow. Companion to
[`rfid-keyboard-reader.md`](./rfid-keyboard-reader.md) (BT-HID scan input) and
[`android-offline-first.md`](../decisions/android-offline-first.md) (Room SSOT).
The Submit form itself is **server-driven** — the field set comes from the SOP
`form_dsl` served by the backend (`GET /app/tasks/{id}` → `sopVersion.form_dsl`);
mobile renders it dynamically via `FormSpec` + `FormRunner`. This doc covers only
the last-mile capture + sync behind the already-rendered `goat_scan` and
`video_proof` controls.

## 1. RFID goat scan (BT-HID)

The field reader pairs with the phone as a **Bluetooth HID keyboard-wedge** — no
vendor SDK, no visible or invisible `EditText` (see `rfid-keyboard-reader.md`).

- Reads arrive as hardware key events (digits) terminated by Enter/newline.
- The scan screen holds input focus and intercepts key events at the Compose
  layer (`Modifier.onPreviewKeyEvent` on a focus-requesting container). It buffers
  characters until the terminator, then emits the completed tag id.
- Capture is abstracted behind a `ScanSource` port so the transport is swappable
  and testable:

  ```
  interface ScanSource { val tags: Flow<String> }        // one completed tag per scan
  class BtHidScanSource(...) : ScanSource                 // production: KeyEvent -> buffer -> tag
  class FakeScanSource(...)  : ScanSource                 // tests: feed tags directly
  ```

- Each completed tag is written to **Room first** (see §3), appended to the
  drive's scanned-goat list (deduped by tag), and only then reflected in the UI.
  The UI never owns the scan list as transient state — Room is the source of truth.
- The RFID capture timestamp is the medical administration timestamp. Android
  sends the device-capture epoch milliseconds with the draft scan, backend stores
  it on `sop_task_scan_captures.captured_at`, and vaccination fan-out persists it
  as `vaccination_completions.administered_at` for that goat. Submission time is
  only a fallback for non-scan legacy paths. UI must display the scanned time in
  the operator's local/India time zone so the operator can see the exact recorded
  vaccination time.

## 2. Video proof capture

Vaccination proof grain is SOP-controlled by backend `proof_policy`, never by a
hardcoded Android/frontend assumption. Supported modes:

- `proof_mode=per_goat_video`, `subject_scope=goat`: every scanned goat needs
  at least one completed live in-app camera clip before shed/drive finalization.
  Proof is linked with `subject_type=goat` and that goat's UUID. One clear
  handling clip can cover all vaccines administered to that goat in the same
  handling. Up to five clips may be attached to one goat row.
- `proof_mode=shed_level_video`, `subject_scope=shed`: the shed submit screen
  shows a shed-level proof field. One shed video is mandatory; up to five shed
  videos are allowed. The current SOP allows both live camera and gallery picker
  (`allowed_capture_sources=["in_app_camera","gallery_picker"]`) because the
  proof is a long shed-level submission video, not a per-goat anti-fraud clip.

The backend-owned `proof_policy` declares the mode, subject scope, clip limits,
allowed capture sources, and verify-before-apply. Android must render from that
policy. Backend submit/readiness must validate against the same policy. Changing
the SOP from per-goat to shed-level must not delete the other mode.

## 2a. Capture-source rules

Per-goat proof clips remain **LIVE in-app camera only**. Shed-level proof clips
may use camera or gallery only when the SOP explicitly allows gallery.

- **Per-goat banned:** file picker, gallery import, `ACTION_GET_CONTENT`,
  `ACTION_PICK`, and generic gallery-capable choosers. A per-goat proof only
  means something if the operator is physically present recording live, right
  now.
- **Shed-level allowed when SOP says so:** gallery picker for long shed-level
  submission videos. The selected `content://` video is immediately copied into
  app-private storage before Room/outbox sees it, so background upload does not
  depend on temporary picker permission.
- Shed-level video capture still uses the same Room/outbox/upload contract as
  per-goat proof. The submit button must gate on at least one completed shed
  proof and must cap the proof count at the SOP-declared maximum, currently five.
  Do not implement a screen-local "one video is enough" assumption; read the min,
  max, subject, and allowed sources from the backend task/SOP payload.
- **Implementation:** live camera uses in-app CameraX
  (`androidx.camera:camera-video` `Recorder`/`VideoCapture`, `androidx.camera:camera-view`'s
  `PreviewView`) — see `InAppVideoRecorderOverlay`
  (`apps/goatos-android/app/.../capture/InAppVideoRecorder.kt`). Camera files
  are written to app-private storage; gallery files are copied to app-private
  cache before upload.
- **Freshness/attribution metadata:** every video proof stores device-clock
  capture/import start (`capturedStartMs`) and stop (`capturedEndMs`) plus the
  operator principal id (`capturedByPrincipalId`) on the Room proof row and in
  upload metadata (`capture_source`, `captured_start_ms`, `captured_end_ms`,
  `duration_ms`, `captured_by_principal_id`).

Capture is abstracted behind a `ProofCaptureSource` port. Each captured/picked
video is written to Room first (§3) as a proof row with its SOP subject, then
queued for upload.

## 2b. Shared video processing before upload

All operator camera video uploads must use the shared pipeline in
[`proof-video-processing-pipeline.md`](./proof-video-processing-pipeline.md).
Screens such as weighing, vaccination, feed, shifting, and future proof modules
must not build their own compression/upload queues.

This pipeline is for operator execution flows only. Leadership, verifier,
admin, and read-only surfaces may inspect proofs through server-authorized
review/read flows, but they must not create phone-camera proof videos.

The pipeline:

- stores the original capture in Room/app-owned storage first
- captures timestamp, logged-in operator, mandatory precise GPS, and
  best-effort Android `Geocoder` address at recording start
- burns a compact bottom-right audit overlay into the final video/photo file
  itself
- applies adaptive WhatsApp-style H.264/AAC compression based on input
  resolution, original bitrate, proof module, and proof readability needs
- runs only one compression job at a time; uploads may run in parallel with the
  next compression job
- records Firebase Analytics/Performance/Crashlytics signals at every stage
- saves the final selected artifact to Gallery and uploads that same artifact:
  processed video/photo on success, original only after a recorded processing
  failure

A pass-through processor is not acceptable. Successful processing must create a
new compressed MP4 with burned overlay for video proof, or a new overlaid image
for photo proof. Upload success alone is not proof that processing succeeded if
the uploaded artifact is still the original capture.

Operator UI may show business status such as `Compressing proof...` and
`Uploading proof...`, but must not expose codec, Room, outbox, GCS, idempotency,
or other implementation terms.

## 2c. Shared camera analytics contract

Every feature that opens the shared proof camera must emit two layers of events:
the feature-specific caller events and the common camera events. This applies to
vaccination, PC Care/vaccine Stock, weighing, feed, shifting, and any future
operator proof flow.

Caller events must be emitted before and after the camera handoff, because the
camera surface only knows the prompt/source, not the business gate that opened it.
At minimum, each caller must log:

- screen visible, including feature surface, task/session id, lifecycle status,
  and whether the screen is editable or locked
- exact row/button tapped to open camera, including field/slot key, media kind,
  subject id when present, and source surface
- camera result returned: success, cancelled, or failure
- Room write or failure for the captured proof row
- upload/outbox id visible or missing after the Room settle window
- business registration/finalization enqueue, success, or failure
- submit tapped, submit blocked, submit confirmation shown, submit enqueue
  success/failure, and manual refresh started/completed for proof submit screens

Common camera events are owned by the shared CameraX surfaces:
`proof_camera_screen_viewed`, flash toggle, shutter/record start, record stop,
cancel, retry/retake/use when the surface has those actions, and
`proof_camera_capture_result`. The common event `source` is derived from
`ProofCapturePrompt`; callers must choose the correct prompt instead of sending a
blank/default prompt.

Camera screens are camera-only. Do not add business proof previews, submit
buttons, verifier copy, or feature-specific review UI inside
`InAppVideoRecorderOverlay` or `PhotoCaptureLauncher`. Review/replace/submit
belongs on the caller screen after the camera returns, using the processed
Room-backed preview from `ProofMediaPreview`.

## 3. Room-first, single source of truth, background sync

The on-device database is the single source of truth for both scans and proof
videos. Nothing is "submitted" straight to the network.

- Every scan and every captured video is persisted to Room **before** any network
  call, each with an explicit sync status (`PENDING`, `IN_FLIGHT`, `SYNCED`,
  `FAILED`). The UI renders that status per row — identical mental model to the
  Android Photos / Google Drive "uploading / synced" indicators.
- Video rows include processing state before upload: original captured,
  location/address resolving, compressing/overlaying, processed, upload queued,
  uploading, uploaded, failed/retrying, or dead-letter. Processing failure does
  not lose proof; it flips the row to upload the original file and records the
  exception through the telemetry ports.
- The row's final local upload URI is the single artifact handed to both Gallery
  save and proof upload. On healthy processing this URI is the processed file;
  on fallback it is the original file with `upload_original=true` and an
  attached processing-failure event.
- Every scan and clip creates its own draft outbox record immediately. Shed and
  drive submit buttons only validate/finalize already-synced records; they are
  not bulk-upload triggers.
- Sync runs both directions: the write outbox posts draft scans, registers and
  uploads proof blobs, then finalizes the submission; server responses (accept /
  rework / verification outcome) are written back into Room and re-emitted.
- **Background sync survives app close and reboot.** A user-originated upload runs
  under a foreground service with an ongoing progress notification (again,
  mirroring Photos/Drive). WorkManager owns process-death, reboot, and
  `BOOT_COMPLETED` recovery from the durable Room outbox; app startup must never
  promote the `dataSync` foreground service because Android 15+ rejects that boot
  context. A foreground-promotion rejection defers safely to WorkManager instead
  of crashing or losing the queued write.
- Idempotency: each submission + each proof upload carries a stable idempotency
  key persisted with the row, so retries never double-post (see AGENTS.md write-path
  idempotency rule).

### 3a. Proof idempotency guardrail

Do not rework proof upload grouping or replacement without preserving the past
shed-proof hardening fixes.

- A proof upload's **idempotency key belongs to one captured clip**, not to a
  shed, task, goat, or "latest proof" slot. Retrying that upload must reuse the
  same key and the same payload. Re-recording/replacing proof must create a new
  proof row with a new proof-upload idempotency key.
- The outbox `groupKey` is only an ordering/concurrency partition. Changing it
  must not change the idempotency key, payload fingerprint, subject, scope, or
  server proof identity of an existing row.
- Same idempotency key with different subject/scope/payload is a bug. Android's
  outbox rejects it via request fingerprint; backend proof creation also rejects
  it via proof request fingerprint.
- 2026-08-26 PC Care stock phone E2E exposed a fresh-capture race where recovery
  can enqueue the same proof id before the normal processed/overlay upload path
  records its outbox id. The visible signature is
  `proof_upload_enqueue_failed reason="Idempotency key already belongs to a
  different queued write"` followed by `proof_upload_registered` and
  `pc_care_stock_proof_registration`, while the Stock detail remains on
  `Saving photo` / `Proof is still uploading`. A passing fix must prove, on a
  device, that the row gets a usable proof-upload outbox id, reaches
  `proof_upload_completed`, refreshes the task proof, and enables submit.
- Never delete a local proof row/file just because the UI wants to replace it.
  If the upload is queued or failed, cancel/retry through the outbox. If it is
  in flight, refuse deletion until it settles. If it is already synced, delete
  the server proof artifact first, and keep the local row if server deletion
  fails or the proof is already attached to a submission.
- Shed-level proof and per-animal proof may use different `groupKey`s for field
  throughput, but both must keep one stable idempotency key per captured video.
  Do not "fix" ordering by reusing a shed/task-level proof key across multiple
  clips; that recreates the shed-submit idempotency loop.

## 4. Mandatory permissions gate

Capture needs camera, Bluetooth (HID + connect/scan), precise location, storage,
and notifications. These are **mandatory**:

- The app requests all required permissions at startup.
- If any required permission (camera / Bluetooth / precise location /
  notifications / storage) is denied, the app **blocks and does not proceed**
  past the gate until every one is granted. There is no degraded path — a
  vaccination drive cannot be captured or proven without them.
- If Android still allows a runtime permission prompt, the blocked gate asks
  again from the same screen. If the operator selected "Don't ask again" or the
  OS will not show the prompt, the gate opens the app's Android Settings page
  and tells the operator to enable the missing permission there before returning.
- Video proof recording also blocks until the app has a fresh precise location
  fix within policy accuracy/age. Android `Geocoder` failure may fall back to
  lat/lng text, but missing precise location may not.

## 5. Role gating

- **Capture + submit:** ground operator only. They scan goats, capture videos, and submit
  the drive. Role is derived from `/app/bootstrap` (an operator profile
  present = capture allowed); the capture surface — and the mandatory
  permission gate in front of it — never render for a principal with no
  operator profile.
- **Verify:** the dedicated verifier section is generic across modules.
  Vaccination is active; Counts and Feed Direction are visible as under
  construction. Verifiers approve/reject goat proof items and provide a reason
  for rework. They cannot scan or capture.
- **Operational close:** scoped Park Heads/Directors and tenant-wide CEO/CxO
  leadership see fully approved drive submissions. One close action atomically
  closes the drive and emits one accepted medical transition per goat. The
  medical date remains the operator's `administered_at`.

## 6. E2E testing (emulator, no BT/camera hardware)

The BT-HID and camera transports are the only hardware-bound parts, and both are
behind ports (`ScanSource`, `ProofCaptureSource`), so the full flow is E2E-testable
in the emulator with **no physical reader and no real camera**:

- **Scan path:** an instrumented test injects the exact hardware key events the
  BT-HID reader would emit — via `UiAutomation.injectInputEvent` /
  `Instrumentation.sendKeyDownUpSync`, or `adb shell input text "<tag>"` followed
  by `adb shell input keyevent 66` (Enter). This drives the real
  KeyEvent → buffer → tag → Room path. Unit/Robolectric tests use `FakeScanSource`.
- **Video path:** the emulator's virtual camera can record, or the test injects a
  fixture file through `ProofCaptureSource` — proving the Room-first persist +
  processing state + Gallery-save selection + upload-queue + status transitions
  without a real lens. Tests must fail pass-through/no-op processors that mark
  the original file as successfully processed.
- **Sync path:** a `MockEngine`/fake backend drives the outbox → upload → response
  round-trip and the Room status transitions (PENDING → IN_FLIGHT → SYNCED /
  FAILED), plus process-death restore (kill + relaunch, drive still present and
  resumable).
- **What still needs a physical device (final QA only):** real Bluetooth pairing
  with the actual reader, real camera capture quality, and visual inspection
  that the Gallery/uploaded success artifact is compressed with a burned overlay.
  Everything else — scan→Room, SOP proof min/max, mandatory-permission gate,
  background upload, fallback-original upload on processing failure, role
  gating, and submit→verify→leadership close is covered by emulator and
  isolated-backend E2E.

## STG GCS Verification

Local proof capture/storage tests do not prove STG GCS wiring.

For staging, verify bucket/env/IAM/signed URL/upload/DB linkage using:

`docs/runbooks/stg-gcs-video-upload-wiring.md`

## 7. Anti-pattern guardrails this must respect

- No visible or invisible `EditText` on the scan surface (`rfid-keyboard-reader.md`).
- Room is SSOT; no network-only screen reads; scans/videos persist before UI
  (`android-offline-first.md`).
- Lists (scanned goats, proof rows) paginate / stay bounded per the mobile
  list-fetch rule; never render an unbounded accumulated blob.
- Mobile lists must auto-fetch the next page when the user approaches the end of
  the viewport and may show only a spinner/progress footer. Do not expose a
  manual "Load more" button on operator work queues, scan rosters, verification
  queues, alerts, or drawers.
- Server-driven form: field set, labels, descriptions, required/optional, and the
  video cap come from `form_dsl` / `proof_policy`, not hardcoded on the client.
- Android proof media guard: feature code cannot own compression/upload/Firebase
  plumbing, and the shared app processor cannot accept pass-through/no-op
  processing as success.
