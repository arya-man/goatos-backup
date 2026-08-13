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
        assertEquals("RFID-123", params[AnalyticsEvents.Params.RFID])
        assertEquals("uploaded", params[AnalyticsEvents.Params.OUTCOME])
        assertEquals("ready", params[AnalyticsEvents.Params.REASON])
        assertEquals("feed_packing", params["feature_surface"])
        assertEquals("processed", params["processing_state"])
        assertEquals("1_5mb", params["processed_size_bucket"])
        assertEquals("synced", params["proof_upload_status"])
        assertEquals("retrying", params["submit_status"])
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
}
