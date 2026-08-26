package sg.mesha.goatos.core.analytics

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Guards the analytics taxonomy + context that egress and dashboards depend on. Event/property
 * names are a contract: renaming one silently breaks a funnel, so their exact string values are
 * pinned here on purpose (a rename must consciously update this test).
 */
class AnalyticsContractTest {

    @Test
    fun `event names are the exact snake_case contract`() {
        assertEquals("app_open", AnalyticsEvents.APP_OPEN)
        // NOT "session_start": that is a Firebase RESERVED name, and Firebase silently refused
        // every one of these events on device -- "Invalid public event name. Event will not be
        // logged (FE): session_start". The session-start event simply did not exist in analytics
        // until it was renamed. Keep the app_ prefix; reverting this reintroduces a permanently
        // dropped event that looks fine in code and produces nothing in the dashboard.
        assertEquals("app_session_start", AnalyticsEvents.SESSION_START)
        assertEquals("bootstrap_loaded", AnalyticsEvents.BOOTSTRAP_LOADED)
        assertEquals("login_attempt", AnalyticsEvents.LOGIN_ATTEMPT)
        assertEquals("login_success", AnalyticsEvents.LOGIN_SUCCESS)
        assertEquals("login_session_ready", AnalyticsEvents.LOGIN_SESSION_READY)
        assertEquals("login_failure", AnalyticsEvents.LOGIN_FAILURE)
        assertEquals("bootstrap_failed", AnalyticsEvents.BOOTSTRAP_FAILED)
        assertEquals("password_reset_requested", AnalyticsEvents.PASSWORD_RESET_REQUESTED)
        assertEquals("password_reset_sent", AnalyticsEvents.PASSWORD_RESET_SENT)
        assertEquals("sign_out", AnalyticsEvents.SIGN_OUT)
        assertEquals("feed_row_tapped", AnalyticsEvents.FEED_ROW_TAPPED)
        assertEquals("feed_transport_viewed", AnalyticsEvents.FEED_TRANSPORT_VIEWED)
        assertEquals("feed_transport_opened", AnalyticsEvents.FEED_TRANSPORT_OPENED)
        assertEquals("feed_transport_video_captured", AnalyticsEvents.FEED_TRANSPORT_VIDEO_CAPTURED)
        assertEquals("feed_transport_submitted", AnalyticsEvents.FEED_TRANSPORT_SUBMITTED)
        assertEquals("feed_transport_failure", AnalyticsEvents.FEED_TRANSPORT_FAILURE)
        assertEquals("feed_distribution_teammate_captures_read", AnalyticsEvents.FEED_DISTRIBUTION_TEAMMATE_CAPTURES_READ)
        assertEquals("feed_distribution_teammate_proof_adopted", AnalyticsEvents.FEED_DISTRIBUTION_TEAMMATE_PROOF_ADOPTED)
        assertEquals("feed_distribution_submit_sources", AnalyticsEvents.FEED_DISTRIBUTION_SUBMIT_SOURCES)
        assertEquals("feed_distribution_live_status_changed", AnalyticsEvents.FEED_DISTRIBUTION_LIVE_STATUS_CHANGED)
        assertEquals("proof_camera_screen_viewed", AnalyticsEvents.PROOF_CAMERA_SCREEN_VIEWED)
        assertEquals("proof_camera_flash_tapped", AnalyticsEvents.PROOF_CAMERA_FLASH_TAPPED)
        assertEquals("proof_camera_shutter_tapped", AnalyticsEvents.PROOF_CAMERA_SHUTTER_TAPPED)
        assertEquals("proof_camera_record_started", AnalyticsEvents.PROOF_CAMERA_RECORD_STARTED)
        assertEquals("proof_camera_record_stop_tapped", AnalyticsEvents.PROOF_CAMERA_RECORD_STOP_TAPPED)
        assertEquals("proof_camera_cancel_tapped", AnalyticsEvents.PROOF_CAMERA_CANCEL_TAPPED)
        assertEquals("proof_camera_retry_tapped", AnalyticsEvents.PROOF_CAMERA_RETRY_TAPPED)
        assertEquals("proof_camera_retake_tapped", AnalyticsEvents.PROOF_CAMERA_RETAKE_TAPPED)
        assertEquals("proof_camera_use_tapped", AnalyticsEvents.PROOF_CAMERA_USE_TAPPED)
        assertEquals("proof_camera_capture_result", AnalyticsEvents.PROOF_CAMERA_CAPTURE_RESULT)
        assertEquals("pc_care_stock_proof_screen_visible", AnalyticsEvents.PC_CARE_STOCK_PROOF_SCREEN_VISIBLE)
        assertEquals("pc_care_stock_proof_action_tapped", AnalyticsEvents.PC_CARE_STOCK_PROOF_ACTION_TAPPED)
        assertEquals("pc_care_stock_proof_capture_result", AnalyticsEvents.PC_CARE_STOCK_PROOF_CAPTURE_RESULT)
        assertEquals("pc_care_stock_proof_room_written", AnalyticsEvents.PC_CARE_STOCK_PROOF_ROOM_WRITTEN)
        assertEquals("pc_care_stock_proof_upload_enqueued", AnalyticsEvents.PC_CARE_STOCK_PROOF_UPLOAD_ENQUEUED)
        assertEquals("pc_care_stock_proof_registration", AnalyticsEvents.PC_CARE_STOCK_PROOF_REGISTRATION)
        assertEquals("pc_care_stock_proof_sync", AnalyticsEvents.PC_CARE_STOCK_PROOF_SYNC)
        assertEquals("pc_care_stock_proof_submit", AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT)
        assertEquals("pc_care_stock_proof_submit_enqueued", AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT_ENQUEUED)
    }

    @Test
    fun `param and user-property keys are the exact contract`() {
        assertEquals("method", AnalyticsEvents.Params.METHOD)
        assertEquals("reason", AnalyticsEvents.Params.REASON)
        assertEquals("chrome", AnalyticsEvents.Params.CHROME)
        assertEquals("session_no", AnalyticsEvents.Params.SESSION_NO)
        assertEquals("email", AnalyticsEvents.Params.EMAIL)
        assertEquals("auth_uid", AnalyticsEvents.Params.FIREBASE_UID)
        assertEquals("role", AnalyticsEvents.UserProps.ROLE)
        assertEquals("email", AnalyticsEvents.UserProps.EMAIL)
        assertEquals("primary_park", AnalyticsEvents.UserProps.PRIMARY_PARK)
        assertEquals("park_id", AnalyticsEvents.UserProps.PARK_ID)
        assertEquals("flavor", AnalyticsEvents.UserProps.FLAVOR)
        assertEquals("tenant", AnalyticsEvents.UserProps.TENANT)
        assertEquals("result", AnalyticsEvents.Params.RESULT)
        assertEquals("slot_mask", AnalyticsEvents.Params.SLOT_MASK)
        assertEquals("retry_count", AnalyticsEvents.Params.RETRY_COUNT)
        assertEquals("source", AnalyticsEvents.Params.SOURCE)
        assertEquals("local_slot_state", AnalyticsEvents.Params.LOCAL_SLOT_STATE)
        assertEquals("feed_weight_source", AnalyticsEvents.Params.FEED_WEIGHT_SOURCE)
        assertEquals("feed_video_source", AnalyticsEvents.Params.FEED_VIDEO_SOURCE)
        assertEquals("water_video_source", AnalyticsEvents.Params.WATER_VIDEO_SOURCE)
        assertEquals("previous", AnalyticsEvents.Params.PREVIOUS)
        assertEquals("next", AnalyticsEvents.Params.NEXT)
        assertEquals("status", AnalyticsEvents.Params.STATUS)
        assertEquals("kind", AnalyticsEvents.Params.KIND)
    }

    @Test
    fun `context starts identity-free and carries the build-fixed flavor`() {
        val ctx = AnalyticsContext(flavor = "dev")
        assertEquals("dev", ctx.flavor)
        assertNull("role is unknown until bootstrap resolves", ctx.role)
        assertNull("park is unknown until bootstrap resolves", ctx.parkScope)

        ctx.role = "operator"
        ctx.parkScope = "park-1"
        assertEquals("operator", ctx.role)
        assertEquals("park-1", ctx.parkScope)
    }

    @Test
    fun `noop analytics accepts events, properties, and user id without throwing`() {
        val analytics: AnalyticsPort = NoopAnalytics()
        // The whole point of the Noop is that no call path can throw or block.
        analytics.track(AnalyticsEvents.APP_OPEN)
        analytics.track(AnalyticsEvents.LOGIN_ATTEMPT, mapOf(AnalyticsEvents.Params.METHOD to "email"))
        analytics.setUserProperty(AnalyticsEvents.UserProps.ROLE, "operator")
        analytics.setUserProperty(AnalyticsEvents.UserProps.ROLE, null)
        analytics.setUserId("member-123")
        analytics.setUserId(null)
        assertTrue(true)
    }

    @Test
    fun `firebase event params keep proof telemetry small and backend-only details out`() {
        val params = firebaseEventParams(
            mapOf(
                AnalyticsEvents.Params.DEVICE_ID to "device-1",
                AnalyticsEvents.Params.JOURNEY_ID to "journey-1",
                AnalyticsEvents.UserProps.TENANT to "tenant-1",
                "actor_id" to "operator-1",
                AnalyticsEvents.UserProps.ROLE to "operator",
                "proof_id" to "proof-1",
                "task_id" to "task-1",
                "field_key" to "feed_packing_video",
                "rfid_tag" to "RFID-123",
                AnalyticsEvents.Params.RFID to "RFID-123",
                AnalyticsEvents.Params.OUTCOME to "uploaded",
                AnalyticsEvents.Params.REASON to "ready",
                "feature_surface" to "feed_packing",
                // Backend-mirror-only diagnostics: dropped from the Firebase envelope in the P1
                // fix so the allowlist fits GA4's real 25-param hard cap (see
                // FIREBASE_MAX_EVENT_PARAMS's comment). BackendAnalyticsAdapter still receives all
                // of these via the full, uncapped props map -- only the Firebase mirror is compact.
                "capture_source" to "camera",
                "mime_type" to "video/mp4",
                "processing_state" to "processed",
                "processing_attempt" to "1",
                "upload_original" to "false",
                "location_status" to "available",
                "geocoder_status" to "ok",
                "duration_bucket" to "15_30s",
                "original_size_bucket" to "10_25mb",
                "processed_size_bucket" to "1_5mb",
                "proof_upload_status" to "synced",
                "submit_status" to "retrying",
                "attempt_count" to "2",
                "max_attempts" to "5",
                "geocoded_address" to "full street address should stay out of firebase",
                "latitude" to "12.3456789",
                "longitude" to "77.1234567",
                "gps_accuracy_m" to "9.0",
                "input_width" to "1920",
                "input_height" to "1080",
                "object_key" to "local/proofs/full/backend/detail.mp4",
                "random_future_param" to "should not backfill into firebase",
            ),
        )

        assertTrue(params.size <= FIREBASE_MAX_EVENT_PARAMS)
        assertEquals("proof-1", params["proof_id"])
        assertEquals("feed_packing_video", params["field_key"])
        assertEquals("RFID-123", params["rfid_tag"])
        assertEquals("uploaded", params[AnalyticsEvents.Params.OUTCOME])
        assertEquals("ready", params[AnalyticsEvents.Params.REASON])
        assertEquals("feed_packing", params["feature_surface"])
        assertEquals("processed", params["processing_state"])
        assertEquals("synced", params["proof_upload_status"])
        assertEquals("retrying", params["submit_status"])
        // Dropped-from-Firebase diagnostics: still not backfilled from arbitrary props.
        assertNull("Params.RFID is a duplicate of rfid_tag; dropped to stay within the 25-cap", params[AnalyticsEvents.Params.RFID])
        assertNull(params["capture_source"])
        assertNull(params["mime_type"])
        assertNull(params["processing_attempt"])
        assertNull(params["upload_original"])
        assertNull(params["location_status"])
        assertNull(params["geocoder_status"])
        assertNull(params["original_size_bucket"])
        assertNull(params["processed_size_bucket"])
        assertNull(params["attempt_count"])
        assertNull(params["max_attempts"])
        assertNull(params["subject_id"])
        assertNull(params["geocoded_address"])
        assertNull(params["latitude"])
        assertNull(params["longitude"])
        assertNull(params["gps_accuracy_m"])
        assertNull(params["input_width"])
        assertNull(params["input_height"])
        assertNull(params["object_key"])
        assertNull(params["random_future_param"])
    }

    @Test
    fun `firebase event params keep the surviving proof-flow telemetry params within the 25-cap`() {
        // P1 fix (2026-08-15): FIREBASE_MAX_EVENT_PARAMS is a HARD 25 -- GA4's own platform
        // ceiling, not just a locally-chosen budget. To fit the proof-flow params (split-operator
        // slot info, submit source, retry/failure reason, live-status transition) within that cap,
        // feed_video_source and water_video_source were dropped from the Firebase envelope
        // (feed_weight_source alone represents the "which slot source" diagnostic there); the full
        // triple still reaches the backend mirror via BackendAnalyticsAdapter on the same call
        // site. This test proves the adapter output actually contains every SURVIVING param, not
        // just that the constants exist, and that the two dropped ones are genuinely dropped.
        val params = firebaseEventParams(
            mapOf(
                AnalyticsEvents.Params.RESULT to "success_slots",
                AnalyticsEvents.Params.SLOT_MASK to "weight_feed",
                AnalyticsEvents.Params.RETRY_COUNT to "2",
                AnalyticsEvents.Params.SOURCE to "sync_tap",
                AnalyticsEvents.Params.LOCAL_SLOT_STATE to "local_present",
                AnalyticsEvents.Params.FEED_WEIGHT_SOURCE to "local_outbox",
                AnalyticsEvents.Params.FEED_VIDEO_SOURCE to "server_ref",
                AnalyticsEvents.Params.WATER_VIDEO_SOURCE to "missing",
                AnalyticsEvents.Params.PREVIOUS to "editable",
                AnalyticsEvents.Params.NEXT to "readonly",
                AnalyticsEvents.Params.STATUS to "pending_verification",
                AnalyticsEvents.Params.KIND to "video",
                "failure_kind" to "processed_video_track_truncated",
            ),
        )

        assertTrue(params.size <= FIREBASE_MAX_EVENT_PARAMS)
        assertEquals("success_slots", params[AnalyticsEvents.Params.RESULT])
        assertEquals("weight_feed", params[AnalyticsEvents.Params.SLOT_MASK])
        assertEquals("2", params[AnalyticsEvents.Params.RETRY_COUNT])
        assertEquals("sync_tap", params[AnalyticsEvents.Params.SOURCE])
        assertEquals("local_present", params[AnalyticsEvents.Params.LOCAL_SLOT_STATE])
        assertEquals("local_outbox", params[AnalyticsEvents.Params.FEED_WEIGHT_SOURCE])
        assertEquals("editable", params[AnalyticsEvents.Params.PREVIOUS])
        assertEquals("readonly", params[AnalyticsEvents.Params.NEXT])
        assertEquals("pending_verification", params[AnalyticsEvents.Params.STATUS])
        assertEquals("video", params[AnalyticsEvents.Params.KIND])
        assertNull("dropped to fit the 25-cap; full value still reaches the backend mirror", params[AnalyticsEvents.Params.FEED_VIDEO_SOURCE])
        assertNull("dropped to fit the 25-cap; full value still reaches the backend mirror", params[AnalyticsEvents.Params.WATER_VIDEO_SOURCE])
        assertNull("dropped to fit the 25-cap; full value still reaches the backend mirror", params["failure_kind"])
    }

    @Test
    fun `every firebase event stays within GA4's hard 25-param cap`() {
        // GA4 enforces this ceiling platform-side, unconditionally: every param past the 25th on
        // a single logged event is silently dropped at ingestion, with no client-visible error.
        // Every declared event in AnalyticsEvents flows through the SAME firebaseEventParams
        // filter (FirebaseAnalyticsAdapter.track), so proving the filter's own upper bound proves
        // it for every event without needing to enumerate call sites individually.
        assertTrue(
            "FIREBASE_MAX_EVENT_PARAMS must not exceed GA4's real platform cap of 25",
            FIREBASE_MAX_EVENT_PARAMS <= 25,
        )

        // Saturate with every allowlisted key (plus decoys, which must never be picked up) to
        // prove the bound holds even when a caller supplies more params than the cap allows.
        val saturatedProps = buildMap {
            put(AnalyticsEvents.Params.DEVICE_ID, "v")
            put(AnalyticsEvents.Params.JOURNEY_ID, "v")
            put(AnalyticsEvents.UserProps.ROLE, "v")
            put(AnalyticsEvents.UserProps.PRIMARY_PARK, "v")
            put("proof_id", "v")
            put("task_id", "v")
            put("field_key", "v")
            put("feature_surface", "v")
            put("rfid_tag", "v")
            put(AnalyticsEvents.Params.OUTCOME, "v")
            put(AnalyticsEvents.Params.REASON, "v")
            put("processing_state", "v")
            put("duration_bucket", "v")
            put("proof_upload_status", "v")
            put("submit_status", "v")
            put(AnalyticsEvents.Params.RESULT, "v")
            put(AnalyticsEvents.Params.SLOT_MASK, "v")
            put(AnalyticsEvents.Params.RETRY_COUNT, "v")
            put(AnalyticsEvents.Params.SOURCE, "v")
            put(AnalyticsEvents.Params.LOCAL_SLOT_STATE, "v")
            put(AnalyticsEvents.Params.FEED_WEIGHT_SOURCE, "v")
            put(AnalyticsEvents.Params.PREVIOUS, "v")
            put(AnalyticsEvents.Params.NEXT, "v")
            put(AnalyticsEvents.Params.STATUS, "v")
            put(AnalyticsEvents.Params.KIND, "v")
            repeat(50) { i -> put("decoy_param_$i", "should never be picked up") }
        }

        for (event in listOf(
            AnalyticsEvents.FEED_DISTRIBUTION_TEAMMATE_CAPTURES_READ,
            AnalyticsEvents.FEED_DISTRIBUTION_SUBMIT_SOURCES,
            AnalyticsEvents.FEED_DISTRIBUTION_LIVE_STATUS_CHANGED,
            AnalyticsEvents.VACCINATION_PROOF_CAPTURE_SUCCESS,
            AnalyticsEvents.WEIGHING_SUBMIT_SUCCESS,
        )) {
            // event name does not affect firebaseEventParams -- it uses one shared allowlist for
            // every event -- but iterating declared events documents the guarantee applies to all.
            val params = firebaseEventParams(saturatedProps)
            assertTrue("event=$event produced ${params.size} params, exceeding the 25-cap", params.size <= 25)
        }
    }
}
