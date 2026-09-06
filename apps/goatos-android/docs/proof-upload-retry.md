# Proof Upload Retry Contract

This flow protects one-time real-world proof tasks such as deworming, vaccination, and hoof
trimming. Once the animal action has happened, the operator must not be sent back to the camera just
because media processing or upload failed.

## User-Facing States

- Missing proof: show the normal capture action.
- Processing failed after the first capture: show Retry. Retry uses the saved original file.
- Processing failed again during Retry: automatically queue the original video upload.
- Upload failed or the local row lost its outbox id: show Retry. Retry reattaches the saved proof to
  the outbox; it does not open the camera.
- Uploaded: show the preview only for one-time PC Care animal slots. Do not show Record again.

Repeatable proof surfaces, such as feed directions, weighing, feed/water removal, and other workflows
where a new real-world recording is acceptable, may continue to expose recapture/record-again actions
according to their feature policy.

## Retry Mechanics

1. The original capture remains in app-private proof storage.
2. First Retry runs the common proof media processor again: resolve source, read/decode metadata,
   burn overlay, create output, transcode or encode.
3. If processing succeeds, the processed file path is queued through the existing proof upload
   outbox and foreground sync service.
4. If processing fails again, the repository switches to original-upload fallback and queues the
   original file through the same outbox path.
5. If a queued outbox row with the same proof idempotency key already exists, proof upload enqueue
   refreshes that row's file payload and returns its id. This prevents a valid saved capture from
   getting stuck as "Upload failed. Retrying" because a previous queued row already owned the key.
6. If that existing outbox row already succeeded, Retry adopts its server proof id and marks the local
   proof synced instead of re-enqueueing.

## Analytics Events

Proof capture emits events through the common repository layer, so feature surfaces receive the same
diagnostics. PC Care passes `feature_surface=pc_care`; events also include proof mode, scope, field
key, media type, duration, attempt, state, and validation details where available.

Important proof retry events:

- `processing_started`
- `processing_completed`
- `processing_failed`
- `processing_retry_failed_original_upload_queued`
- `upload_enqueue_failed`
- `upload_recovered_from_succeeded_outbox`
- `gallery_save_completed`
- `retry_requested`

Failure payloads include:

- `processing_stage`
- `error_class`
- `error_wrapper_class`
- `retryable`
- `device_manufacturer`
- `device_model`
- `android_api`
- `local_uri_scheme`
- `original_uri_scheme`
- `processed_uri_present`
- `local_file_available`
- `original_file_available`
- `processed_file_available`
- `local_file_size_bucket`
- `original_file_size_bucket`
- `processed_file_size_bucket`
- `fallback_state`
- `original_upload_fallback`

This makes it visible whether a failure came from source resolution, overlay burn, transcode/encode,
gallery save, enqueue, or network/server sync.
