package sg.mesha.goatos.analytics

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import sg.mesha.goatos.core.analytics.AnalyticsContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.network.AppAnalyticsEventRequestDto
import sg.mesha.goatos.core.network.AppAnalyticsEventResponseDto
import sg.mesha.goatos.core.network.AppApi
import javax.inject.Provider

/**
 * P2 backend-analytics-durability: proves [BackendAnalyticsAdapter] persists a CRITICAL event
 * (see [BackendAnalyticsAdapter.CRITICAL_EVENT_ALLOWLIST]) when the network send fails, and
 * drains it exactly once the next time the backend is reachable. Non-critical events are proven
 * to stay fire-and-forget (unchanged pre-fix behavior).
 */
@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class BackendAnalyticsAdapterTest {

    private lateinit var context: Context

    @Before
    fun setUp() {
        context = ApplicationProvider.getApplicationContext()
    }

    /** Fake [AppApi] whose `recordAnalyticsEvent` can be toggled to fail every call, and records
     *  every request it ever received (successful or not) for assertion. */
    private class FakeAppApi(@Volatile var shouldFail: Boolean) : AppApi by sg.mesha.goatos.core.network.FakeAppApi() {
        val received = mutableListOf<AppAnalyticsEventRequestDto>()

        override suspend fun recordAnalyticsEvent(request: AppAnalyticsEventRequestDto): AppAnalyticsEventResponseDto {
            received += request
            if (shouldFail) throw java.io.IOException("offline")
            return AppAnalyticsEventResponseDto(accepted = true)
        }
    }

    private fun adapter(api: FakeAppApi, scope: TestScope, queue: DurableAnalyticsQueue) =
        BackendAnalyticsAdapter(
            apiProvider = Provider { api },
            appScope = scope,
            analyticsContext = AnalyticsContext(flavor = "dev"),
            clock = { 42L },
            queue = queue,
        )

    @Test
    fun `a critical event is persisted asynchronously when the send fails offline`() = runTest {
        val api = FakeAppApi(shouldFail = true)
        val queue = DurableAnalyticsQueue(context, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())
        val backend = adapter(api, this, queue)

        backend.track(AnalyticsEvents.SYNC_WRITE_DEAD, mapOf(AnalyticsEvents.Params.REASON to "conflict"))
        advanceUntilIdle()

        assertEquals(1, queue.size())
    }

    @Test
    fun `a non-critical event is NOT persisted when the send fails -- stays fire-and-forget`() = runTest {
        val api = FakeAppApi(shouldFail = true)
        val queue = DurableAnalyticsQueue(context, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())
        val backend = adapter(api, this, queue)

        backend.track(AnalyticsEvents.APP_OPEN)
        advanceUntilIdle()

        assertEquals(0, queue.size())
    }

    @Test
    fun `a queued critical event drains exactly once on the next successful track call`() = runTest {
        val api = FakeAppApi(shouldFail = true)
        val queue = DurableAnalyticsQueue(context, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())
        val backend = adapter(api, this, queue)

        // First call fails and queues the critical event.
        backend.track(AnalyticsEvents.WEIGHING_CAPTURE_FAILURE, mapOf(AnalyticsEvents.Params.REASON to "no_video"))
        advanceUntilIdle()
        assertEquals(1, queue.size())
        // The failed initial attempt is also recorded by the fake; only sends AFTER recovery count.
        api.received.clear()

        // Backend becomes reachable again; the NEXT track() call (any event) opportunistically
        // drains the queued one before sending its own.
        api.shouldFail = false
        backend.track(AnalyticsEvents.APP_OPEN)
        advanceUntilIdle()

        assertEquals(0, queue.size())
        val drainedEventNames = api.received.map { it.eventName }
        assertTrue(
            "expected the queued weighing_capture_failure to have been drained, got $drainedEventNames",
            drainedEventNames.contains(AnalyticsEvents.WEIGHING_CAPTURE_FAILURE),
        )
        // Exactly once: not resent by a subsequent drain once removed from the queue.
        assertEquals(1, drainedEventNames.count { it == AnalyticsEvents.WEIGHING_CAPTURE_FAILURE })

        api.received.clear()
        backend.track(AnalyticsEvents.APP_OPEN)
        advanceUntilIdle()
        assertTrue(api.received.none { it.eventName == AnalyticsEvents.WEIGHING_CAPTURE_FAILURE })
    }

    @Test
    fun `every request carries a client_event_id for backend dedupe`() = runTest {
        val api = FakeAppApi(shouldFail = false)
        val queue = DurableAnalyticsQueue(context, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())
        val backend = adapter(api, this, queue)

        backend.track(AnalyticsEvents.APP_OPEN)
        advanceUntilIdle()

        val clientEventId = api.received.single().properties[BackendAnalyticsAdapter.CLIENT_EVENT_ID_PARAM]
        assertTrue("client_event_id must be present and non-blank", !clientEventId.isNullOrBlank())
    }

    @Test
    fun `feed distribution failure is durable and drains from the queue`() = runTest {
        val api = FakeAppApi(shouldFail = true)
        val queue = DurableAnalyticsQueue(context, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())
        val backend = adapter(api, this, queue)

        backend.track(
            AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
            mapOf("kind" to "feed_video", AnalyticsEvents.Params.REASON to "missing_upload_outbox"),
        )
        advanceUntilIdle()
        assertEquals(1, queue.size())
        val originalId = api.received.single().clientEventId
        api.received.clear()

        api.shouldFail = false
        backend.track(AnalyticsEvents.APP_OPEN)
        advanceUntilIdle()

        assertEquals(0, queue.size())
        val drained = api.received.single { it.eventName == AnalyticsEvents.FEED_DISTRIBUTION_FAILURE }
        assertEquals(originalId, drained.clientEventId)
        assertEquals("feed_video", drained.properties["kind"])
        assertEquals("missing_upload_outbox", drained.properties[AnalyticsEvents.Params.REASON])
    }

    @Test
    fun `proof camera capture result is durable and preserves camera diagnostics on replay`() = runTest {
        val api = FakeAppApi(shouldFail = true)
        val queue = DurableAnalyticsQueue(context, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())
        val backend = adapter(api, this, queue)

        backend.track(
            AnalyticsEvents.PROOF_CAMERA_CAPTURE_RESULT,
            mapOf(
                AnalyticsEvents.Params.KIND to "photo",
                AnalyticsEvents.Params.SOURCE to "inventory_vaccine_stock",
                AnalyticsEvents.Params.RESULT to "success",
                AnalyticsEvents.Params.STATUS to "torch_on",
                "torch_mode" to "auto",
                "low_light" to "true",
            ),
        )
        advanceUntilIdle()
        assertEquals(1, queue.size())
        val originalId = api.received.single().clientEventId
        api.received.clear()

        api.shouldFail = false
        backend.track(AnalyticsEvents.APP_OPEN)
        advanceUntilIdle()

        assertEquals(0, queue.size())
        val drained = api.received.single { it.eventName == AnalyticsEvents.PROOF_CAMERA_CAPTURE_RESULT }
        assertEquals(originalId, drained.clientEventId)
        assertEquals("photo", drained.properties[AnalyticsEvents.Params.KIND])
        assertEquals("inventory_vaccine_stock", drained.properties[AnalyticsEvents.Params.SOURCE])
        assertEquals("success", drained.properties[AnalyticsEvents.Params.RESULT])
        assertEquals("torch_on", drained.properties[AnalyticsEvents.Params.STATUS])
        assertEquals("auto", drained.properties["torch_mode"])
        assertEquals("true", drained.properties["low_light"])
    }

    @Test
    fun `the critical event allowlist covers the documented forensic events`() {
        val allowlist = BackendAnalyticsAdapter.CRITICAL_EVENT_ALLOWLIST
        assertTrue(allowlist.contains("proof_processing_failed"))
        assertTrue(allowlist.contains(AnalyticsEvents.PROOF_CAMERA_SCREEN_VIEWED))
        assertTrue(allowlist.contains(AnalyticsEvents.PROOF_CAMERA_FLASH_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.PROOF_CAMERA_SHUTTER_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.PROOF_CAMERA_RECORD_STARTED))
        assertTrue(allowlist.contains(AnalyticsEvents.PROOF_CAMERA_RECORD_STOP_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.PROOF_CAMERA_CANCEL_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.PROOF_CAMERA_RETRY_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.PROOF_CAMERA_RETAKE_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.PROOF_CAMERA_USE_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.PROOF_CAMERA_CAPTURE_RESULT))
        assertTrue(allowlist.contains(AnalyticsEvents.PC_CARE_STOCK_PROOF_SCREEN_VISIBLE))
        assertTrue(allowlist.contains(AnalyticsEvents.PC_CARE_STOCK_PROOF_ACTION_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.PC_CARE_STOCK_PROOF_CAPTURE_RESULT))
        assertTrue(allowlist.contains(AnalyticsEvents.PC_CARE_STOCK_PROOF_ROOM_WRITTEN))
        assertTrue(allowlist.contains(AnalyticsEvents.PC_CARE_STOCK_PROOF_UPLOAD_ENQUEUED))
        assertTrue(allowlist.contains(AnalyticsEvents.PC_CARE_STOCK_PROOF_REGISTRATION))
        assertTrue(allowlist.contains(AnalyticsEvents.PC_CARE_STOCK_PROOF_SYNC))
        assertTrue(allowlist.contains(AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT))
        assertTrue(allowlist.contains(AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT_ENQUEUED))
        assertTrue(allowlist.contains(AnalyticsEvents.SYNC_WRITE_DEAD))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_OPENED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_CAPTURE_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_PROOF_CAPTURED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_PROOF_UPLOAD_SYNCED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_PROOF_REUPLOAD_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_SUBMIT_BLOCKED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_SYNC_TAPPED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_WEIGHT_PHOTO_CAPTURED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_VIDEO_CAPTURED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_WATER_PROOF_CAPTURED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_SUBMITTED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_LIVE_STATUS_CHANGED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_TEAMMATE_CAPTURES_READ))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_TEAMMATE_PROOF_ADOPTED))
        assertTrue(allowlist.contains(AnalyticsEvents.FEED_DISTRIBUTION_FAILURE))
        assertTrue(allowlist.contains(AnalyticsEvents.WEIGHING_CAPTURE_FAILURE))
        assertTrue(allowlist.contains(AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_FAILURE))
        assertTrue(allowlist.contains(AnalyticsEvents.WEIGHING_PROOF_CAPTURE_FAILURE))
    }

    /** External review 2026-08-16: FEED_DISTRIBUTION_SUBMIT_SOURCES is the ONLY event carrying
     *  all three split-operator slot sources (Firebase drops two under the 25-param cap) — it
     *  must be durable, and the resend must reuse the SAME client_event_id + all three fields. */
    @Test
    fun `submit sources event is durable and resends with same id and all three source fields`() = runTest {
        val api = FakeAppApi(shouldFail = true)
        val queue = DurableAnalyticsQueue(context, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())
        val backend = adapter(api, this, queue)

        backend.track(
            AnalyticsEvents.FEED_DISTRIBUTION_SUBMIT_SOURCES,
            mapOf(
                "feed_weight_source" to "local",
                "feed_video_source" to "teammate",
                "water_video_source" to "teammate",
            ),
        )
        advanceUntilIdle()
        assertEquals("failed send of submit-sources must queue", 1, queue.size())
        val originalId = api.received.single().clientEventId
        assertTrue("live send carries top-level client_event_id", !originalId.isNullOrBlank())
        api.received.clear()

        api.shouldFail = false
        backend.track(AnalyticsEvents.APP_OPEN)
        advanceUntilIdle()

        assertEquals(0, queue.size())
        val resent = api.received.single { it.eventName == AnalyticsEvents.FEED_DISTRIBUTION_SUBMIT_SOURCES }
        assertEquals("resend must reuse the ORIGINAL client_event_id", originalId, resent.clientEventId)
        assertEquals("local", resent.properties["feed_weight_source"])
        assertEquals("teammate", resent.properties["feed_video_source"])
        assertEquals("teammate", resent.properties["water_video_source"])
    }

}
