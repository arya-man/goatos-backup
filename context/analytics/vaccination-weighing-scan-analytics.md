# Vaccination And Weighing Scan Analytics

This is the source of truth for Android vaccination and weighing scan-screen
analytics. Load this before answering questions about whether scan/proof/finalize
events reached Firebase or backend.

## Routing

Every app analytics event must fan out to both sinks:

- Firebase Analytics through `FirebaseAnalyticsAdapter`.
- Backend audit through `POST /app/analytics/events`, stored as
  `audit_log.action = 'app.analytics.event'` and
  `audit_log.resource_type = 'app_analytics_event'`.

The backend analytics event is independent from domain sync tables. Domain Room
outbox failure must not prevent the app from attempting analytics delivery.

Common parameters expected on every event:

- `device_id`
- `tenant_id`
- `actor_id`
- `email`
- `role`
- `primary_park`
- `park_id`
- `flavor`
- `app_version_name`
- `app_version_code`

Firebase release proof event:

- `firebase_analytics_proof`

Use it to prove the installed APK can talk to Firebase before checking the
feature funnel.

## Backend Tables

Use these tables for the full journey:

| Table | Purpose | Query signal |
| --- | --- | --- |
| `audit_log` | Product analytics from Android. This is the canonical backend analytics sink. | `action = 'app.analytics.event'`; event name is in metadata/after state as `event_name`. |
| `proof_artifacts` | Media proof rows. In vaccination/weighing scan screens, a row here is also practical scan+proof evidence for that RFID/tag because the camera opens from the scanned row flow. It still does not prove the separate product analytics event fired. | `field_key`, `captured_by_principal_id`, `subject_id`, `scope_id`, `metadata->>'rfid'`, `metadata->>'scanned_identifier'`, `metadata->>'park_id'`, `created_at`, proof status fields. |
| `sop_task_scan_attempts` | Legacy/domain scan attempt audit for SOP-style task flows. Do not treat this as product analytics. | RFID/tag attempt rows when that domain path writes them. |
| `sop_task_scan_captures` | Legacy/domain scan capture audit for SOP-style task flows. Do not treat this as product analytics. | Durable domain scan rows when that domain path writes them. |
| `sop_submissions` | SOP/task submit records, when the task engine path is used. | Submit/finalize rows for SOP-backed flows. |
| `sop_submission_items` | Per-item submitted facts for SOP submissions. | Per-goat/per-item facts under an SOP submission. |
| `verification_items` | Verification/work review facts, when the flow creates them. | What verifier-facing item was created. |

Important: for vaccination/weighing scan screens, `proof_artifacts` is valid
received scan+proof activity for the RFID/tag, because the proof UI is entered
through the scan row. RFID/tag is the primary evidence key; goat/animal ids are
only lookup context and must not be treated as a substitute because one goat can
have multiple tags. However, `proof_artifacts` can be populated while
`sop_task_scan_attempts`/`sop_task_scan_captures` are empty, because proof upload
and the separate explicit scan audit/product analytics paths are different. To
answer "did the operator scan and submit proof in the app", `proof_artifacts` is
valid evidence. To answer "did every RFID/button/dialog analytics event fire",
check `audit_log` and Firebase.

## Vaccination Journey

Firebase event names and backend `audit_log` event names:

- `vaccination_operator_action`
- `vaccination_scan_attempt`
- `vaccination_scan_capture_queued`
- `vaccination_proof_capture_attempt`
- `vaccination_proof_capture_success`
- `vaccination_proof_upload_queued`
- `vaccination_proof_capture_failure`

Expected operator actions include:

- `back`
- `manual_scan_tap`
- `toggle_scan_list`
- `finalize_shed`
- `load_more`
- `reconnect_reader`
- `open_shed_switcher`
- `dismiss_shed_switcher`
- `switch_shed`
- `capture_video`
- `capture_proof`
- `retry_proof`
- `arm_proof_replacement`
- `select_vaccine_group`
- `open_status_tile`
- `rfid_scan_outbox_attempt`

RFID scan behavior:

1. On hardware RFID read, the app emits `vaccination_operator_action` with
   `action = 'rfid_scan_outbox_attempt'` and
   `room_outbox_status = 'attempting'` before calling the Room scan outbox.
2. If Room queues the scan, the app emits `vaccination_scan_capture_queued` with
   `room_outbox_status = 'queued'`.
3. If Room reports duplicate/already scanned, the app emits
   `vaccination_scan_capture_queued` with the duplicate/already-scanned status.
4. If Room throws, the app emits `vaccination_scan_capture_queued` with
   `room_outbox_status = 'failed'` and `reason`.

Vaccination scan/proof parameters to check:

- `task_id`
- `shed_id`
- `rfid` / `scanned_identifier`
- `primary_tag`
- `secondary_tag`
- `tag_candidates`
- `goat_id` only as lookup context, not the primary evidence key
- `obligation_id`
- `vaccine`
- `field_key`
- `captured_at_ms`
- `proof_id`
- `sync_status`
- `server_proof_id`

Vaccination proof rows:

- `proof_artifacts.field_key = 'vaccination_goat_proof'`
- `proof_artifacts.metadata->>'rfid'`
- `proof_artifacts.metadata->>'scanned_identifier'`
- `proof_artifacts.metadata->>'primary_tag'`
- `proof_artifacts.metadata->>'secondary_tag'`
- `proof_artifacts.metadata->>'tag_candidates'`
- `proof_artifacts.metadata->>'shed_id'`
- `proof_artifacts.metadata->>'task_id'`

## Weighing Journey

Firebase event names and backend `audit_log` event names:

- `weighing_viewed`
- `weighing_operator_action`
- `weighing_read_failure`
- `weighing_capture_attempt`
- `weighing_scan_capture_queued`
- `weighing_proof_upload_queued`
- `weighing_capture_success`
- `weighing_capture_failure`
- `weighing_submit_attempt`
- `weighing_submit_confirmation_opened`
- `weighing_submit_confirmation_cancelled`
- `weighing_submit_confirmation_confirmed`
- `weighing_submit_blocked`
- `weighing_submit_success`
- `weighing_submit_failure`

Expected operator actions include:

- `reopen_assignment`
- `close_shed_campaign`
- `close_campaign`
- `scan_input_change`
- `weight_input_change`
- `animal_count_input_change`
- `animal_weight_input_change`
- `select_animal`
- `submit_typed_scan`
- `record_individual`
- `submit_individual_scope`
- `submit_confirmation_opened`
- `submit_confirmation_cancelled`
- `submit_confirmation_confirmed`
- `submit_shed_partition`
- `capture_shed_video`
- `capture_shed_video_replacement`
- `retry_shed_video`
- `retry_shed_video_queued`
- `retry_shed_video_failed`
- `remove_shed_video`
- `remove_shed_video_done`
- `remove_shed_video_failed`
- `replace_shed_video`
- `rfid_scan_outbox_attempt`
- `retry_video`
- `retry_video_queued`
- `retry_video_failed`
- `reupload_video`

RFID scan behavior:

1. On hardware RFID read, the app emits `weighing_operator_action` with
   `action = 'rfid_scan_outbox_attempt'` and
   `room_outbox_status = 'attempting'` before calling the Room scan outbox.
2. If Room queues the scan, the app emits `weighing_scan_capture_queued` with
   `room_outbox_status = 'queued'`.
3. If Room reports a duplicate, the app emits `weighing_scan_capture_queued`
   with `room_outbox_status = 'duplicate'`.
4. If Room throws, the app emits `weighing_scan_capture_queued` with
   `room_outbox_status = 'failed'` and `reason`.

Weighing scan/proof parameters to check:

- `campaign_id`
- `work_group_id`
- `shed_id`
- `park_id`
- `park_name`
- `expected_location_id`
- `expected_location_label`
- `item_id`
- `category`
- `rfid` / `scanned_identifier`
- `primary_tag`
- `secondary_tag`
- `tag_candidates`
- `animal_id` only as lookup context, not the primary evidence key
- `weight_kg`
- `field_key`
- `captured_at_ms`
- `proof_id`
- `sync_status`
- `server_proof_id`
- `room_outbox_status`

Weighing proof rows:

- `proof_artifacts.field_key = 'weighing_individual_video'`
- `proof_artifacts.field_key = 'weighing_shed_partition_video'`
- `proof_artifacts.metadata->>'rfid'`
- `proof_artifacts.metadata->>'scanned_identifier'`
- `proof_artifacts.metadata->>'primary_tag'`
- `proof_artifacts.metadata->>'secondary_tag'`
- `proof_artifacts.metadata->>'tag_candidates'`
- `proof_artifacts.metadata->>'park_id'`
- `proof_artifacts.metadata->>'park_name'`
- `proof_artifacts.metadata->>'campaign_shed_id'`

## Standard Checks

Firebase DebugView / StreamView:

1. Filter to Android app id `sg.mesha.goatos.stg`.
2. Confirm `firebase_analytics_proof` after app launch.
3. Perform one vaccination scan and one weighing scan.
4. Confirm the matching feature events above with the same device, app version,
   actor, RFID/tag, task/shed/work identifiers, proof id, and sync status.

Backend:

```sql
select
  created_at,
  principal_id,
  resource_id,
  metadata->>'event_name' as event_name,
  metadata->'properties' as properties
from audit_log
where action = 'app.analytics.event'
  and resource_type = 'app_analytics_event'
  and metadata->>'event_name' in (
    'firebase_analytics_proof',
    'vaccination_operator_action',
    'vaccination_scan_capture_queued',
    'vaccination_proof_upload_queued',
    'weighing_operator_action',
    'weighing_scan_capture_queued',
    'weighing_proof_upload_queued'
  )
order by created_at desc
limit 100;
```

Proof cross-check:

```sql
select
  created_at,
  field_key,
  captured_by_principal_id,
  subject_id,
  scope_id,
  metadata
from proof_artifacts
where field_key in (
  'vaccination_goat_proof',
  'weighing_individual_video',
  'weighing_shed_partition_video'
)
order by created_at desc
limit 100;
```

If `proof_artifacts` has rows but `audit_log` does not have matching
`app.analytics.event` rows, the app did receive scan+proof activity for those
items, but the explicit analytics sink is broken or missing for that journey.

## Release Guard

`./gradlew :app:validateStgReleaseAnalytics` must pass before staging release or
Firebase App Distribution upload. It checks:

- staging telemetry is enabled
- staging Firebase app/package configuration is present
- analytics fans out to Firebase and backend
- Firebase collection is explicitly enabled
- backend route `POST /app/analytics/events` exists
- vaccination/weighing critical scan/proof event constants exist
- vaccination/weighing ViewModels emit the critical scan/proof events
- this runbook exists
