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

## 3. Room-first, single source of truth, background sync

The on-device database is the single source of truth for both scans and proof
videos. Nothing is "submitted" straight to the network.

- Every scan and every captured video is persisted to Room **before** any network
  call, each with an explicit sync status (`PENDING`, `IN_FLIGHT`, `SYNCED`,
  `FAILED`). The UI renders that status per row — identical mental model to the
  Android Photos / Google Drive "uploading / synced" indicators.
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

## 4. Mandatory permissions gate

Capture needs camera, Bluetooth (HID + connect/scan), location (BT dependency on
older Android), storage, and notifications. These are **mandatory**:

- The app requests all required permissions at startup.
- If any required permission (camera / Bluetooth / location / notifications /
  storage) is denied, the app **blocks and does not proceed** past the gate until
  every one is granted. There is no degraded path — a vaccination drive cannot be
  captured or proven without them.

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
  upload-queue + status transitions without a real lens.
- **Sync path:** a `MockEngine`/fake backend drives the outbox → upload → response
  round-trip and the Room status transitions (PENDING → IN_FLIGHT → SYNCED /
  FAILED), plus process-death restore (kill + relaunch, drive still present and
  resumable).
- **What still needs a physical device (final QA only):** real Bluetooth pairing
  with the actual reader, and real camera capture quality. Everything else —
  scan→Room, SOP proof min/max, mandatory-permission gate, background
  upload, role gating, and submit→verify→leadership close is covered by
  emulator and isolated-backend E2E.

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
