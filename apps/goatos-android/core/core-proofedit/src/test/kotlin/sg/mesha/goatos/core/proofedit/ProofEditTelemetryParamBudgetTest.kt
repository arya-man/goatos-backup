package sg.mesha.goatos.core.proofedit

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Every param this flow emits must already be on `FIREBASE_PARAM_ALLOWLIST`
 * (`core-analytics/FirebaseAnalyticsAdapter.kt`). A key absent from that list is dropped before
 * the event reaches GA4 — the flow looks instrumented and reports nothing, with no error anywhere.
 *
 * That is not hypothetical: `capture_source`, `mime_type` and `proof_subject` were assumed to be
 * allowlisted, were emitted on every delivered/abandoned event, and were silently discarded.
 *
 * The allowlist is `private` in another module, so it is mirrored here. If the two drift this test
 * is the thing that says so — and the allowlist is at its 25-entry GA4 ceiling, so the fix is
 * always to stop emitting the key, never to append one (appending drops the last entry instead).
 */
class ProofEditTelemetryParamBudgetTest {

    /** Mirror of FIREBASE_PARAM_ALLOWLIST. Keep in sync when that list changes. */
    private val firebaseAllowlist = setOf(
        "device_id", "journey_id", "role", "primary_park",
        "proof_id", "task_id", "field_key", "feature_surface", "rfid_tag",
        "outcome", "reason", "processing_state", "duration_bucket",
        "proof_upload_status", "submit_status",
        "result", "slot_mask", "retry_count", "source", "local_slot_state",
        "feed_weight_source", "previous", "next", "status", "kind",
    )

    @Test
    fun `allowlist mirror is still exactly at the GA4 ceiling`() {
        assertEquals(
            "FIREBASE_PARAM_ALLOWLIST must stay at 25; overflow is dropped in list order",
            25,
            firebaseAllowlist.size,
        )
    }

    @Test
    fun `every emitted param is allowlisted`() {
        ProofEditTelemetry.Params.ALL.forEach { key ->
            assertTrue(
                "param '$key' is not on FIREBASE_PARAM_ALLOWLIST and would be silently dropped",
                key in firebaseAllowlist,
            )
        }
    }

    @Test
    fun `the three params that were wrongly assumed allowlisted stay out`() {
        listOf("capture_source", "mime_type", "proof_subject").forEach { key ->
            assertFalse("$key is not allowlisted", key in firebaseAllowlist)
            assertFalse("$key must not be emitted", key in ProofEditTelemetry.Params.ALL)
        }
    }

    @Test
    fun `the flow reports a step and an identity for tracing a stuck capture`() {
        assertTrue(ProofEditTelemetry.Params.PROCESSING_STATE in ProofEditTelemetry.Params.ALL)
        assertTrue(ProofEditTelemetry.Params.FEATURE_SURFACE in ProofEditTelemetry.Params.ALL)
        assertTrue(ProofEditTelemetry.Params.RFID_TAG in ProofEditTelemetry.Params.ALL)
    }
}
