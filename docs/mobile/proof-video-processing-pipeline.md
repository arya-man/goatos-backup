# Mobile Proof Video Processing Pipeline

Status: design contract for the shared Android operator camera-proof pipeline.
This applies to weighing individual videos, weighing shed videos, vaccination
proof, feed proof, shifting proof, and future operator camera workflows that
upload video from the phone.

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
- Operator-only capture: only signed-in operator execution flows may create
  camera proofs. Leadership, verifier, admin, and read-only flows can review or
  inspect proofs only through their server-authorized surfaces.

## Non-goals

- Do not run more than one video compression job in parallel.
- Do not depend on Google Maps Geocoding API or paid geocoding for the first
  version.
- Do not expose internal words such as Room, outbox, codec, bitrate, or
  idempotency in operator UI.
- Do not upload video directly from UI state. Room is the source of truth.
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

The overlay is burned into the output video pixels during the transcode. It is
not an ExoPlayer view overlay and not a separate caption.

Required lines:

```text
<local timestamp>
Operator: <logged-in display name>
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
DEAD_LETTER
```

If compression, overlay rendering, metadata extraction, codec selection, muxing,
or file write fails, record the exception and enqueue the original file for
upload:

```text
compression_error -> Crashlytics non-fatal + analytics event
                  -> processed_file = original_file
                  -> upload_original = true
                  -> continue upload
```

Only dead-letter when both processed/original upload paths are exhausted or the
backend rejects the proof in a non-retryable way.

## Operator UI Contract

Screens such as weighing scan, vaccination scan, feed proof, and shifting proof
must render the same business statuses:

- `Preparing proof...`
- `Compressing proof...`
- `Uploading proof...`
- `Proof uploaded`
- `Upload failed. Retrying`
- `Record again`

Do not show internal implementation labels such as Room, outbox, encoder,
Media3, GCS, signed URL, idempotency, payload, or API.

The operator can continue scanning or working while queued proofs process. A
submit/finalize action may remain disabled when the module requires uploaded
proof before submit.

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
- Room migration/schema test for queue state
- WorkManager retry/process-death test
- Firebase event test using fake analytics/crash/perf ports
- device smoke test on one low/mid-range phone with actual camera video
- visual frame extraction test proving the overlay is burned into the video file
  and is not just a player view

For UI proof, use product copy only. Screenshots containing internal words fail
review.
