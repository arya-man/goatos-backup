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

## 2. Video proof capture

The vaccination SOP requires proof on each scanned goat row:

- Every scanned goat needs at least one completed camera clip before shed/drive
  finalization. Proof is linked with `subject_type=goat` and that goat's UUID.
- One clear handling clip can cover all vaccines administered to that goat in
  the same handling. The UI never asks for one video per vaccine.
- An operator can attach up to five clips to one goat row, allowing a second
  angle or corrected clip without task-level shed/vial/administration slots.
- The backend-owned `proof_policy` declares goat scope, clip limits, camera
  source, and verify-before-apply. Android renders status on the goat row and
  finalization validates the already-synced references.

## 2a. Camera-only capture (anti-fraud) — HARD BUSINESS RULE

Every goat proof clip **must be captured by LIVE, in-app camera recording only.**

- **Banned:** any file picker, gallery import, `ACTION_GET_CONTENT`,
  `ACTION_PICK`, or a generic gallery-capable chooser (including the platform
  `ACTION_VIDEO_CAPTURE` intent, which can itself surface a chooser on some
  OEM camera apps) — there is no code path anywhere in the capture surface
  that can turn an existing file on the device into a `video_proof` capture.
- **Why:** the proof only means something if the operator is physically
  present recording live, right now. A picker lets them submit an old or
  unrelated video and fake verification — that defeats the entire point of a
  medical proof video.
- **Implementation:** in-app CameraX live recording
  (`androidx.camera:camera-video` `Recorder`/`VideoCapture`, `androidx.camera:camera-view`'s
  `PreviewView` for the live preview) — see `InAppVideoRecorderOverlay`
  (`apps/goatos-android/app/.../capture/InAppVideoRecorder.kt`). The recorded
  file is written to this app's **own private storage**
  (`Context.filesDir`, never `getExternalFilesDir`/MediaStore/the shared
  gallery), so it is never visible to — or swappable by — any other app.
- **Freshness/attribution metadata:** every capture stores its device-clock
  record start (`capturedStartMs`) and stop (`capturedEndMs`) — duration is
  derived — plus the recording operator's principal id
  (`capturedByPrincipalId`), on the Room proof row and in the metadata sent
  with the registration upload (`captured_start_ms`/`captured_end_ms`/
  `duration_ms`/`captured_by_principal_id`). A verifier can see this was a
  live, timed, attributable recording. Geotag/park-location capture is a
  nice-to-have this build does NOT implement — noted as REMAINING.
- The captured file still goes Room-first (proof row + sync status), then a
  background metadata-registration upload, exactly like every other capture
  in this design — camera-only changes WHERE the bytes come from, not the
  Room-first/outbox pipeline that follows.

The proof subject is always `goat`; the goat row already carries the shed,
drive, vaccine/lot, and administration context. Capture is abstracted behind a
`ProofCaptureSource` port — in production, LIVE
in-app CameraX recording ONLY (§2a, camera-only anti-fraud rule); a
fake/injected file in tests — for the same testability reasons as `ScanSource`.
Each captured video is written to Room first (§3) as a proof row with its
goat `subject_id`, then queued for upload.

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
- **Background sync survives app close.** Uploads run under a foreground service
  with an ongoing notification showing progress (again, mirroring Photos/Drive
  background upload). Closing the app does not lose or pause an in-flight drive.
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
  scan→Room, one-to-five clips per goat, mandatory-permission gate, background
  upload, role gating, and submit→verify→leadership close is covered by
  emulator and isolated-backend E2E.

## 7. Anti-pattern guardrails this must respect

- No visible or invisible `EditText` on the scan surface (`rfid-keyboard-reader.md`).
- Room is SSOT; no network-only screen reads; scans/videos persist before UI
  (`android-offline-first.md`).
- Lists (scanned goats, proof rows) paginate / stay bounded per the mobile
  list-fetch rule; never render an unbounded accumulated blob.
- Server-driven form: field set, labels, descriptions, required/optional, and the
  video cap come from `form_dsl` / `proof_policy`, not hardcoded on the client.
