# Mobile Proof Video Processing Pipeline

Status: design contract for the shared Android operator camera-proof pipeline.
This applies to weighing individual videos, weighing shed videos, vaccination
proof, feed proof, shifting proof, workflow proof, milk proof, and future
operator camera workflows that upload video from the phone.

Implementation acceptance: Android must ship this as a real media processor,
not a pass-through placeholder. The shared `ProofCaptureRepository` path selects
one final proof artifact before upload, saves that same artifact to Gallery, and
queues that same artifact for upload. A successful video processing step must
produce a new compressed MP4 with the audit overlay burned into pixels. A
successful photo processing step must produce a new image with the audit overlay
burned into pixels. Returning the original URI, copying bytes unchanged, or
recording "processed" while `processed_uri == original_uri` is a failed
implementation, even when the upload succeeds. The original artifact is
saved/uploaded only when compression, overlay rendering, metadata extraction,
codec selection, muxing, or processed-file writing fails.

Current coverage is intentionally explicit. The shared path now covers:

- Vaccination scan proof and vaccination submit video fields.
- Weighing individual proof and weighing lump-sum/shed proof.
- Feed complete proof, feed distribution feed proof, feed distribution water
  photo/video proof, feed packing proof, and feed transport proof.
- Shifting execute proof.
- Milk preparation proof and milk feeding proof.
- Workflow action videos and death workflow draft videos uploaded on submit.

Health is intentionally not listed as covered yet because the current Android
health screens do not open camera/proof capture. When a health proof surface is
added, it must go through this same shared path; the Android proof-video guard
blocks feature/ViewModel direct proof uploads, feature-local Firebase calls,
feature-local media processing, and feature-local workers.

Feature/ViewModel code must not upload captured files directly through
`SyncRepository.enqueueProofUpload`. The CI guard blocks direct feature-layer
proof-upload enqueue calls so any new operator camera surface goes through the
shared processing, Gallery-save, fallback, Room state-event, Firebase analytics,
and backend-event path.

Operator capture permission coverage is also explicit: camera, microphone, and
precise location are mandatory on every supported Android version; Android 12+
operator routes additionally require both Nearby Devices runtime permissions
(`BLUETOOTH_CONNECT` and `BLUETOOTH_SCAN`); Android 13+ requires
`POST_NOTIFICATIONS`.

This doc extends:

- `docs/mobile/proof-capture-sync-and-e2e.md`
- `docs/mobile/system-design.md`
- `apps/goatos-android/docs/TELEMETRY.md`

## Goals

- One shared Android pipeline for capture, compression, burned-in audit overlay,
  upload, retry, and telemetry.
- WhatsApp-style compression: small enough for field networks while preserving
  proof readability and speech.
- Durable Room-backed queue so every video survives app close, crash, reboot,
  weak network, and retry.
- Firebase Analytics, Performance, and Crashlytics instrumentation at every
  step so failures are diagnosable from device logs and backend audit rows.
- Fail open for proof bytes: if compression or overlay processing fails, upload
  the original captured video rather than losing the proof.
- Never fail open as normal acceptance: a healthy processing path uploads and
  saves the processed artifact; the original-upload path is reserved for a
  recorded processing failure.
- Operator-only capture: only signed-in operator execution flows may create
  camera proofs. Leadership, verifier, admin, and read-only flows can review or
  inspect proofs only through their server-authorized surfaces.
- Feature-aligned UI states: each operator surface renders the proof status in
  its own production shape, not a generic proof card. Vaccination scan looks like
  scan, weighing individual looks like captured animal rows, lump-sum weighing
  looks like the five-video shed form, feed/milk/shifting/workflow screens keep
  their own step rows.
- Review/preview stays outside the live camera surface. The recorder captures
  and stops; feature screens show the resulting proof preview with the shared
  `ProofMediaPreview` and instrumented proof player.

## Non-goals

- Do not run more than one video compression job in parallel.
- Do not depend on Google Maps Geocoding API or paid geocoding for the first
  version.
- Do not expose internal words such as Room, outbox, codec, bitrate, or
  idempotency in operator UI.
- Do not upload video directly from UI state. Room is the source of truth.
- Do not accept a no-op media processor. A successful processor result must be a
  new processed artifact, not the original URI with unchanged byte counts.
- Do not add feature-local calls to `SyncRepository.enqueueProofUpload` for
  phone-camera proof capture. Capture surfaces must go through the shared proof
  capture/orchestration path so processing, Gallery save, fallback, retry, Room
  events, and Firebase events stay consistent.
- Do not create feature-local ExoPlayer instances for operator proof previews.
  Use the shared proof-media player path so signed-url playback failures are
  visible through the common telemetry client.
- Do not create feature-local camera launchers that skip shared lifecycle
  analytics. `BindVideoCaptureSource`/`InAppVideoRecorderOverlay` own durable
  `proof_camera_*` and `proof_gallery_picker_*` events for every feature that
  opens the phone camera.
- Do not let proof-dependent submit/completion writes spend their retry budget
  while referenced proof-upload rows are still queued, in flight, or backed off.
  They must wait without attempt burn and submit only after proof ids are
  available, while true missing/corrupt proof references remain terminal.
- Do not allow non-operator flows to create phone-camera proof videos through
  this pipeline.

## Capture Metadata

At recording start, persist a `VideoProofDraft` row before processing begins:

- local original file URI/path
- module (`weighing`, `vaccination`, `feed`, `shifting`, etc.)
- scope type/id and subject type/id
- captured start/end timestamps
- logged-in principal id and display name
- app install id, app version, build flavor, device model, Android SDK
- precise GPS latitude/longitude/accuracy/timestamp
- reverse-geocoded address, if available
- requested proof policy snapshot
- one stable idempotency key per captured clip

Precise location is mandatory for operator video proof capture. The app must
request fine location, confirm precise location is enabled, obtain a fresh fix
within the policy accuracy/age threshold, and block recording when it cannot.
If the operator denies location and Android can still show the runtime prompt,
the app asks again from the blocked permission gate. If the operator selected
"Don't ask again" or Android will not show the prompt, the app opens the Goat OS
Android app settings page so the operator can enable precise location there.
The proof row stores the location attempt/result before capture proceeds.

Address text should first use Android's built-in `Geocoder` on the device. This
does not require Goat OS to enable Google Maps Geocoding API billing. Because
`Geocoder` is device/service dependent, empty, slow, or failed address
resolution is expected and must fall back to lat/lng plus accuracy. This
fallback is only for address text; it is not a fallback for missing precise
location.

## Burned Audit Overlay

The overlay is burned into the output media pixels. For videos this happens
during transcode; for photos this happens when writing the processed image. It
is not an ExoPlayer view overlay, Compose preview overlay, separate caption,
metadata tag, or server-side review adornment.

Required lines:

```text
<local timestamp>
Operator: <logged-in display name>
RFID: <tag>          # when camera opened from an RFID/scanned animal row
<address line 1 or lat/lng>
<address line 2 or accuracy>
```

Placement:

- bottom-right corner
- no external right/bottom margin
- content-sized width based on the longest rendered line plus internal padding
- semi-transparent black background
- white text with dark shadow
- small enough to avoid covering the weighing scale, goat body, RFID action,
  syringe/medicine site, or feed/water proof area

Implementation rule:

```text
box_width = longest_text_line_width + left_padding + right_padding
box_height = line_heights + line_spacing + top_padding + bottom_padding
x = video_width - box_width
y = video_height - box_height
```

If a module knows the scale display or proof subject is always in the bottom
right, the module may request bottom-left placement through proof policy. The
default is bottom-right.

## Compression Profiles

The pipeline uses one pass:

```text
decode frame -> draw overlay -> encode compressed frame -> mux MP4
```

Do not compress first and then reopen the compressed file to add overlay. That
causes extra time and quality loss.

Use H.264 MP4 for compatibility. Audio is required for operator speech in
weighing and other verbal proof workflows; compress it, do not remove it.

Default audio profile:

```text
AAC, mono, 24 kHz, 32-48 kbps
```

Use `48 kbps` when field noise makes speech hard to understand; otherwise
`32 kbps` is acceptable for breed/gender/weight narration.

Video profile is adaptive:

| Input / proof need | Target |
|---|---|
| 1080p and scale/digits are small | 6-8 Mbps |
| 720p normal proof | 3.5-4.5 Mbps |
| 720p WhatsApp-style proof | 2.0-3.0 Mbps, only after visual QA |
| Low-res portrait around 480x850 | 700 kbps-1.2 Mbps |
| Already low bitrate | cap target at roughly 60-80% of original video bitrate |

Never blindly use one bitrate for every video. A 720p 14 Mbps clip can shrink
heavily; a long low-res 1.4 Mbps clip may not.

Suggested selector:

```text
profile_target = by_resolution_and_module(input_width, input_height, module)
target_video_bitrate = min(profile_target, original_video_bitrate * 0.7)
target_video_bitrate = max(target_video_bitrate, module_minimum_bitrate)
```

Module minimums must be conservative for proof readability:

- weighing individual: preserve scale readability and speech
- vaccination: preserve animal handling and injection/proof action
- feed/shifting: preserve action context and operator narration

Photo proof processing has the same artifact contract as video proof: decode
the captured image, draw the required audit overlay into the bitmap, write a new
JPEG/PNG/WebP file in app-owned storage, save that final file to Gallery, and
upload that final file. A photo pass-through is not accepted.

## Android Implementation

Use a shared Android service/repository layer, not per-screen custom pipelines.
Recommended components:

- CameraX for live camera capture
- AndroidX Media3 Transformer for native transcode + overlay
- Room for draft/process/upload state
- WorkManager for durable background processing and upload
- app-owned local storage for original and processed files
- `AnalyticsPort`, `PerformanceTracer`, and `CrashReporter` for telemetry

Compression concurrency:

```text
maxCompressionJobs = 1
maxUploadJobs = 2
```

Do not choose compression concurrency from free RAM. The bottleneck is hardware
encoder availability, thermal state, battery, and IO. Multiple simultaneous
encodes can fail, throttle, overheat, or become slower overall. Upload may run
while the next video compresses.

## Queue State Machine

Each captured video has a durable Room state:

```text
CAPTURED_ORIGINAL
  -> LOCATION_RESOLVING
  -> PROCESSING_MEDIA
  -> PROCESSED
  -> REGISTERING_UPLOAD
  -> UPLOADING
  -> UPLOAD_CONFIRMED
  -> ATTACHED_TO_SUBMISSION
```

Failure states:

```text
PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED
REGISTER_FAILED_RETRYING
UPLOAD_FAILED_RETRYING
UPLOAD_ORIGINAL_FAILED_RETRYING
DEAD_LETTER
```

Persist every transition, not only the current enum. A support/debugger flow
must be able to answer "what happened to this clip?" from the phone database,
Firebase logs, backend audit, and upload object metadata without reproducing
the bug.

Minimum Room fields per proof row:

| Field | Purpose |
|---|---|
| `proof_id` | Local stable id for UI/support and idempotency correlation |
| `module` | `weighing`, `vaccination`, `feed`, `shifting`, etc. |
| `feature_surface` | Exact caller: `weighing_individual`, `weighing_lumpsum`, `vaccination_scan`, `vaccination_shed`, etc. |
| `proof_mode` | `per_animal`, `per_shed`, `step_video`, `optional_video` |
| `subject_type` / `subject_id` | Animal, shed, task, workflow action, feed batch, transport leg |
| `slot_index` / `slot_required` | Required for multi-video UI: e.g. vaccination/weighing shed Video 1 mandatory, Video 2-5 optional |
| `state` | Current state from the state machine above |
| `state_attempt` | Incremented by WorkManager attempt, not by UI recomposition |
| `processing_attempted` | True after the first compression/overlay attempt starts |
| `upload_original` | True when processing failed and original must be uploaded |
| `original_file_uri` / `processed_file_uri` | App-private files only |
| `original_bytes` / `processed_bytes` | Size comparison and support diagnosis |
| `input_width` / `input_height` / `duration_ms` | Bucket selection and malformed-media diagnosis |
| `target_video_bitrate` / `target_audio_bitrate` | Stored as numbers; log only buckets to Firebase |
| `location_status` / `gps_accuracy_m` / `geocoder_status` | Location proof and address fallback diagnosis |
| `last_error_stage` / `last_error_class` / `last_error_retryable` | Immediate failure lookup |
| `last_error_message_hash` | Debug correlation without storing raw exception text if sensitive |
| `backend_proof_id` / `upload_session_id` / `object_generation` | Server/GCS correlation |
| `created_at` / `updated_at` / `uploaded_at` / `attached_at` | Timing and SLA |

Also persist append-only transition history:

```text
proof_state_events(proof_id, from_state, to_state, stage, attempt, occurred_at,
                   duration_ms, bytes_in, bytes_out, error_class, retryable)
```

Room is the phone source of truth for operator UI. The backend remains the
canonical source after upload/attach is confirmed.

If compression, overlay rendering, metadata extraction, codec selection, muxing,
or file write fails, record the exception and enqueue the original file for
upload:

```text
compression_error -> Crashlytics non-fatal + analytics event
                  -> processed_file = original_file
                  -> upload_original = true
                  -> processing_attempted = true
                  -> continue upload
```

Processing is attempted at most once per captured clip. If processing fails,
persist `upload_original=true` on the Room row. Any later retry for that same
clip must skip compression/overlay and retry the original-file upload directly.
Do not loop through compression again for a clip that already entered
`PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED`.

If upload fails after successful processing, retry the processed file. If upload
fails after processing fallback, retry the original file. The Gallery copy and
upload payload must always point at the same final selected artifact: processed
on success, original only after a recorded processing failure. The operator
action is still just Retry/Record again; the app decides which file to upload
from durable state.

Only dead-letter when both processed/original upload paths are exhausted or the
backend rejects the proof in a non-retryable way.

## Operator UI Contract

Screens such as weighing scan, vaccination scan, feed proof, and shifting proof
must render the same business statuses:

- `Preparing proof...`
- `Compressing proof...`
- `Uploading proof...`
- `Uploading original proof...`
- `Proof uploaded`
- `Upload failed. Retrying`
- `Retrying original proof upload...`
- `Record again`

Do not show internal implementation labels such as Room, outbox, encoder,
Media3, GCS, signed URL, idempotency, payload, or API.

The operator can continue scanning or working while queued proofs process. A
submit/finalize action may remain disabled when the module requires uploaded
proof before submit.

Known operator camera-proof surfaces:

| Feature surface | UI shape | Proof slots |
|---|---|---|
| `vaccination_scan` | Vaccination RFID scan screen | one per scanned animal |
| `vaccination_shed` | Shed proof video list | 5 max; Video 1 mandatory, Videos 2-5 optional |
| `weighing_individual` | Weighing captured-animal card list | one per scanned animal |
| `weighing_lumpsum` | Total weight / animal count form with video list | 5 max; Video 1 mandatory, Videos 2-5 optional |
| `birth_death_workflow` | Workflow action/evidence rows | per required action |
| `shifting_execute` | Movement/high-priority action rows | per required movement/feed proof |
| `feed_distribution` | Feed/water completion surface | required/optional per server policy |
| `feed_complete_optional` | Legacy feed completion surface | optional video |
| `feed_packing` | Packing completion surface | mandatory packing video |
| `feed_transport` | Transport handoff surface | required transport video |
| `milk_preparation` | Preparation step checklist | per step requiring proof |
| `milk_feeding` | Feeding attempt/checklist | per feeding proof requirement |

Every surface must bind to the same Room queue and state vocabulary. The UI may
look different per feature, but the status source and retry/fallback behavior
must be common.

## Firebase And Logs

Emit Firebase Analytics events and Performance traces through existing ports.
Do not call Firebase SDKs directly from feature code.

Analytics events:

```text
proof_capture_started{module, proof_mode, subject_type}
proof_capture_completed{module, duration_bucket, size_bucket}
proof_location_started{module}
proof_location_resolved{module, source: geocoder|lat_lng_fallback, duration_bucket}
proof_processing_started{module, input_size_bucket, input_resolution_bucket, target_bitrate_bucket}
proof_processing_completed{module, output_size_bucket, duration_bucket, upload_original: false}
proof_processing_failed{module, error_class, upload_original: true}
proof_upload_registered{module, proof_id}
proof_upload_started{module, size_bucket, upload_original}
proof_upload_completed{module, duration_bucket, bytes_bucket}
proof_upload_failed{module, error_class, retryable}
proof_dead_lettered{module, stage, error_class}
```

Required event parameters on every proof event:

```text
proof_id
module
feature_surface
proof_mode
subject_type
slot_required
state
attempt
app_version
build_flavor
device_model_bucket
network_type
upload_original
```

Never send raw address, lat/lng, local file path, signed URL, animal PII, or
free-form exception text to Firebase Analytics. Use buckets/hashes.

Performance traces:

```text
proof_location_resolution
proof_video_processing
proof_video_upload
proof_end_to_end_capture_to_uploaded
```

Crashlytics:

- record every caught exception as non-fatal
- attach non-PII custom keys: module, stage, app version, device model, SDK,
  input size bucket, output size bucket, target bitrate bucket, upload-original
  fallback flag
- do not attach raw address text, GPS coordinates, Firebase token, signed URL,
  or local file path

Backend audit metadata must receive the same client identity headers already
documented in `apps/goatos-android/docs/TELEMETRY.md`.

Debug lookup matrix:

| User-visible issue | First place to look | Expected breadcrumb |
|---|---|---|
| "Stuck on Compressing proof..." | Room `proof_video_queue` + `proof_state_events` | state `PROCESSING_MEDIA`, attempt, started timestamp |
| Processing failed but upload continued | Crashlytics + `proof_processing_failed` + Room row | `upload_original=true`, `PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED` |
| "Uploading original proof..." for too long | WorkManager run + Firebase `proof_upload_started` | original bytes bucket, attempt, network type |
| Retry button appears | Room state + backend upload session | `UPLOAD_FAILED_RETRYING` or `UPLOAD_ORIGINAL_FAILED_RETRYING` |
| Video missing after submit | backend audit + `proof_upload_completed` | `backend_proof_id`, `object_generation`, `ATTACHED_TO_SUBMISSION` |
| Overlay missing in verifier playback | visual frame extraction test + processing event | `proof_processing_completed`, processed file URI exists |
| Address missing | Room location/geocoder fields | precise GPS present, `geocoder_status=failed|empty`, lat/lng fallback used |

## Size Buckets And Benchmarks

Benchmarks from Infinix X6873 using staging GCS samples on 2026-08-11:

| Video | Input | Android profile | Output | Processing |
|---|---:|---|---:|---:|
| 12.77 sec weighing, 720p, ~14 Mbps | 22.73 MB | 4.0 Mbps + burned overlay | ~6.7 MB | ~2.5-3.0 sec |
| Same short video, WhatsApp-style | 22.73 MB | 2.0 Mbps + burned overlay | 3.50 MB | 2.738 sec |
| Long low-res video, 478x850, video stream 530 sec | 105.48 MB | 800 kbps + burned overlay, clipped to video duration | 76.83 MB | 60.514 sec |

The long sample has inconsistent stream/container duration: video is about 530
seconds while audio/container reports about 674 seconds. The processing pipeline
must detect and handle malformed media instead of assuming duration metadata is
clean.

## Verification Requirements

Before enabling this for a module:

- unit-test bitrate bucket selection
- unit-test overlay sizing and bottom-right placement
- unit-test fallback-to-original on processing exception
- unit-test that a pass-through/no-op processor result is rejected: processed
  video/photo success must not return the original URI or unchanged output bytes
- unit-test that Gallery save and upload receive the same final artifact
  selected by processing/fallback
- Room migration/schema test for queue state
- WorkManager retry/process-death test
- Firebase event test using fake analytics/crash/perf ports
- device smoke test on one low/mid-range phone with actual camera video
- visual frame extraction test proving the overlay is burned into the video file
  and is not just a player view
- Paparazzi/Showkase coverage for every touched feature surface and every
  operator-visible state
- screenshots for all simulated states, not only happy paths: preparing,
  compressing, uploading, uploaded, processing-failed-original-upload,
  upload-failed-retrying, retrying-original-upload, and record-again/dead-letter
- physical-device E2E with actual camera capture for the implemented shared
  pipeline: capture -> location -> burned overlay -> compression -> upload ->
  attach, plus processing-exception fallback to original upload
- physical-device E2E proof that the processed Gallery artifact is the uploaded
  artifact on success, and that the original artifact is saved/uploaded only for
  a recorded processing failure
- product-shaped UI fixtures: screenshots must resemble the actual feature
  screens, not generic cards, unless the actual feature screen itself is a card
  list
- CI/static guard that feature code cannot call Firebase SDKs directly; it must
  use `AnalyticsPort`, `PerformanceTracer`, and `CrashReporter`
- CI/static guard that feature screens do not create their own compression or
  upload implementations; they must call the shared proof-video pipeline
- CI/static guard that the app-layer proof media processor is not a no-op or
  pass-through and that the final processed/fallback artifact feeds both Gallery
  save and upload
- CI/static guard that production UI strings do not expose internal terms such
  as Room, outbox, codec, Media3, GCS, signed URL, idempotency, or bitrate
- skill/reference docs updated in `.agents/skills/goatos-build/references/`
  whenever the pipeline contract changes

For UI proof, use product copy only. Screenshots containing internal words fail
review.
