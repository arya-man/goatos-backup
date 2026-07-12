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

The vaccination SOP requires proof video(s). Per the current maintainer rule:

- **Minimum 1 video, maximum 5.**
- **`shed_video` (proof_subject `shed`) is mandatory.** `vial_lot_video` and
  `administration_video` are optional. The operator may add extra videos beyond
  the three named subjects (e.g. multiple sheds/batches in one drive) up to the
  cap of 5 total.
- Each field carries a plain `description` (form DSL supports `description`) so the
  operator sees what each video is for. Admin edits these in the SOP form-builder;
  they are served in `form_dsl`, never hardcoded on the client.

Field meanings (authored in the SOP, shown to the operator):

| Subject | Field | Proves |
| --- | --- | --- |
| `shed` | shed_video (mandatory) | which shed / drive / group being vaccinated |
| `vial_lot` | vial_lot_video (optional) | the vaccine vial + lot/batch number (traceability) |
| `administration` | administration_video (optional) | the actual injection — dose given, not just logged |

Capture is abstracted behind a `ProofCaptureSource` port (CameraX in production,
a fake/injected file in tests) for the same testability reasons as `ScanSource`.
Each captured video is written to Room first (§3) as a proof row with its
`proof_subject`, then queued for upload.

## 3. Room-first, single source of truth, background sync

The on-device database is the single source of truth for both scans and proof
videos. Nothing is "submitted" straight to the network.

- Every scan and every captured video is persisted to Room **before** any network
  call, each with an explicit sync status (`PENDING`, `IN_FLIGHT`, `SYNCED`,
  `FAILED`). The UI renders that status per row — identical mental model to the
  Android Photos / Google Drive "uploading / synced" indicators.
- Sync runs both directions: the write outbox posts the submission + uploads the
  proof blobs; server responses (accept / rework / verification outcome) are
  written back into Room, which re-emits to the UI.
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

- **Capture + submit:** ground operator / park manager only. They scan goats,
  capture videos, and submit the drive.
- **Approve / reject:** park-manager-and-above (Director / CxO / CEO) do **not**
  capture. They only review the submitted proof videos and approve or request
  rework — matching the SOP workflow `operator_submission → proof_verification →
  accepted | rework`.
- Role is derived from `/app/bootstrap`; the capture surface is not shown to
  approver-only roles.

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
  scan→Room, up-to-5 video capture, mandatory-permission gate, background upload,
  role gating, submit→approve/reject — is covered by emulator E2E.

## 7. Anti-pattern guardrails this must respect

- No visible or invisible `EditText` on the scan surface (`rfid-keyboard-reader.md`).
- Room is SSOT; no network-only screen reads; scans/videos persist before UI
  (`android-offline-first.md`).
- Lists (scanned goats, proof rows) paginate / stay bounded per the mobile
  list-fetch rule; never render an unbounded accumulated blob.
- Server-driven form: field set, labels, descriptions, required/optional, and the
  video cap come from `form_dsl` / `proof_policy`, not hardcoded on the client.
