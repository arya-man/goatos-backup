# RFID Scan Capture Sync

Status: implemented on Android and backend app API. This runbook documents the
RFID vaccination scan path landed in `fix(rfid): autosync scan captures`.

## Scope

This flow is for RFID tag captures during the operator vaccination scan roster.
It is not a media/proof upload path and does not use GCS. Scan captures are
structured DB records, then Submit validates/finalizes them into the SOP task
submission.

## Runtime Flow

```text
BLE/HID RFID reader
-> Android hardware key capture
-> ScanViewModel.onTagRead(...)
-> ScanCaptureRepository.recordScan(...)
-> Room scanned_goats row
-> outbox SCAN_CAPTURE item
-> POST /app/tasks/{task_id}/scan-captures
-> backend sop_task_scan_captures draft row
-> SubmitTask merges draft captures into GOAT_SCAN answers
-> normal SOP validation/finalization
```

The scan screen updates immediately from local Room-backed state so the operator
does not lose visible progress if the network is slow. The backend draft row
protects progress after sync even if the operator leaves the scan screen before
pressing Submit.

## Android Local Contract

The production scan path persists only real RFID reads. Manual ring taps are a
visual overlay and must not write a `GOAT_SCAN` capture.

`ScanViewModel` matches the tag against the current roster, then calls
`ScanCaptureRepository.recordScan(...)` with:

- `taskId`
- `ROSTER_SCAN_FIELD_KEY`
- normalized RFID tag
- matched `goatId`
- matched `obligationId`

`DefaultScanCaptureRepository` writes the Room row first. Only a successful
insert enqueues `SCAN_CAPTURE` in the durable outbox. The outbox idempotency key
is stable:

```text
scan:{taskId}:{fieldKey}:{normalizedTag}
```

Duplicate RFID reads must be non-destructive:

- same task, field, and normalized tag does not add another Room row;
- the same duplicate does not enqueue another backend write;
- the UI should surface "already scanned" feedback instead of appending another
  scan-feed row.

## Backend Contract

The app API endpoint is:

```http
POST /app/tasks/{task_id}/scan-captures
Idempotency-Key: scan:{taskId}:{fieldKey}:{normalizedTag}
```

The backend validates:

- tenant and authenticated actor;
- task exists for that tenant;
- idempotency key is present;
- RFID tag and normalized tag are non-empty;
- optional goat/obligation IDs are valid UUIDs when supplied;
- the scan field is allowed by the task form.

Accepted draft captures are stored in `sop_task_scan_captures`.

The table has two important uniqueness guards:

```text
(tenant_id, idempotency_key)
(tenant_id, task_id, field_key, normalized_tag)
```

Those are the backend safety net for replay, offline retry, app restart, and
operator duplicate scans.

## Submit Contract

Submit is no longer the only place RFID tags enter the backend.

On `SubmitTask`, the SOP service loads `sop_task_scan_captures` for the task and
merges them into the submitted `GOAT_SCAN` answers before validation. The final
submission still runs normal SOP validation and completion logic. This means:

- scanned work can survive app restart or network retry after the draft sync;
- Submit validates and finalizes, rather than being the first capture write;
- if the Submit form has missing scan answers, backend draft captures fill the
  gaps;
- duplicate draft rows are suppressed by DB constraints before Submit sees them.

`ROSTER_SCAN_FIELD_KEY` is a sentinel for the roster scan surface. It is resolved
only to the intended `GOAT_SCAN` field when the form has exactly one roster scan
target. It must not fan out into unrelated future scan fields.

## Offline And Retry Behavior

If the phone is offline:

- Room keeps the local capture;
- the outbox keeps the `SCAN_CAPTURE` operation;
- the operator sees the local draft state;
- sync retries through the normal outbox engine;
- the backend idempotency key makes retries safe.

If the app process dies before sync:

- the Room scan row remains;
- the repository/outbox state resumes when the app starts;
- duplicate local `recordScan(...)` calls are still no-ops for the same tag.

If the app dies after backend draft sync but before Submit:

- the backend draft row remains;
- Submit later merges that row into the final task submission.

## Pagination Behavior

The scan roster can be paginated. The UI must expose continuation loading when
the backend returns `nextCursor`, and scan classification must not be limited to
only the first rendered page. A tag on a later page must resolve to the correct
row after the page is loaded, not fall through as an unknown tag.

## Test Coverage

Keep these contracts pinned with focused tests:

- Android ViewModel: scan same RFID twice -> one Room capture, one feed row, one
  DONE transition.
- Android ViewModel: manual tap -> no `GOAT_SCAN` Room capture.
- Android capture repository: duplicate `(task, field, tag)` insert is a no-op
  and enqueues sync once.
- Android sync engine: `SCAN_CAPTURE` dispatches to
  `/app/tasks/{task_id}/scan-captures` with the stable idempotency key.
- Backend SOP service: `RecordScanCapture` validates and persists draft rows.
- Backend SOP submit: draft scan captures merge into final `GOAT_SCAN` answers.

Useful focused checks:

```bash
./gradlew :app:testDevDebugUnitTest --tests sg.mesha.goatos.viewmodel.ScanViewModelTest
./gradlew :core:core-data:testDebugUnitTest --tests sg.mesha.goatos.core.data.capture.CaptureRepositoryTest
./gradlew :core:core-data:testDebugUnitTest --tests sg.mesha.goatos.core.data.sync.SyncEngineTest
go test ./internal/sop/...
```
