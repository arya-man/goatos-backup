package sg.mesha.goatos.analytics

import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.analytics.AnalyticsContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.standardEventParams
import sg.mesha.goatos.core.network.AppAnalyticsEventRequestDto
import sg.mesha.goatos.core.network.AppApi
import java.util.UUID
import javax.inject.Provider

/**
 * Backend mirror for product analytics. Firebase/GA4 is useful, but not trustworthy enough as the
 * only receipt surface while the console can lag or be misconfigured.
 *
 * Delivery is BEST-EFFORT for most events: [track] fires the network call on [appScope] and does
 * not retry or persist it beyond this process's lifetime. If the device is offline or the request
 * otherwise fails, the event is dropped from the backend's perspective for events NOT in
 * [CRITICAL_EVENT_ALLOWLIST] -- only a [Log.w] breadcrumb (event name + error) survives, and only
 * for as long as this process/logcat session lives.
 *
 * P2 backend-analytics-durability fix (2026-08-15): the small, hand-picked
 * [CRITICAL_EVENT_ALLOWLIST] subset -- the forensic events actually used to debug offline/failure
 * incidents -- IS now durable. Those events are handed to [appScope], written to [queue], then
 * delivered from that queue (a minimal, capped, file-backed queue -- see [DurableAnalyticsQueue]'s
 * kdoc for what it does and does NOT do) and retried opportunistically the next time [track] runs on ANY event
 * (see [drainQueuedEvents]). This is intentionally not full parity with the app's existing
 * Room-backed write outbox: no WorkManager-scheduled background drain, no exponential backoff,
 * and only NETWORK-ACTIVITY-triggered draining rather than a dedicated connectivity callback --
 * both would need new DI wiring into `di/AnalyticsModule.kt` / `GoatOsApplication.kt`, which is
 * out of scope for this fix. Non-critical events remain exactly as fire-and-forget as before.
 *
 * Every request carries a [CLIENT_EVENT_ID_PARAM] property -- a fresh, per-call
 * [UUID.randomUUID] -- carried BOTH as a property and as the first-class
 * `AppAnalyticsEventRequestDto.clientEventId` request field. The backend enforces
 * UNIQUE (tenant_id, client_event_id) with ON CONFLICT DO NOTHING, and queue-drain resends reuse
 * the ORIGINAL id, so a retry after a lost response never double-counts.
 */
class BackendAnalyticsAdapter(
    private val apiProvider: Provider<AppApi>,
    private val appScope: CoroutineScope,
    private val analyticsContext: AnalyticsContext,
    private val clock: () -> Long = System::currentTimeMillis,
    private val queue: DurableAnalyticsQueue = DurableAnalyticsQueue(),
) : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {
        if (event.isBlank()) return
        val clientEventId = UUID.randomUUID().toString()
        val mergedProps = analyticsContext.standardEventParams() + props + mapOf(CLIENT_EVENT_ID_PARAM to clientEventId)
        val request = AppAnalyticsEventRequestDto(
            eventName = event,
            properties = mergedProps,
            clientEventTimeMs = clock(),
            flavor = BuildConfig.FLAVOR,
            appVersionName = BuildConfig.VERSION_NAME,
            appVersionCode = BuildConfig.VERSION_CODE,
            clientEventId = clientEventId,
        )
        val isCritical = event in CRITICAL_EVENT_ALLOWLIST
        val queuedEvent = QueuedAnalyticsEvent(
            clientEventId = clientEventId,
            eventName = event,
            properties = mergedProps,
            clientEventTimeMs = request.clientEventTimeMs,
            flavor = request.flavor,
            appVersionName = request.appVersionName,
            appVersionCode = request.appVersionCode,
        )
        appScope.launch {
            if (isCritical) {
                queue.enqueue(queuedEvent)
            }

            // Opportunistic drain: any live network activity from this adapter is itself evidence
            // connectivity may be back, so flush previously-queued critical events first. See
            // this class's kdoc for why this call site -- not a ConnectivityManager callback or a
            // WorkManager job -- is the drain trigger.
            runCatching { drainQueuedEvents() }
                .onFailure { Log.w(TAG, "Durable analytics queue drain failed", it) }

            if (isCritical) return@launch

            runCatching { apiProvider.get().recordAnalyticsEvent(request) }
                .onFailure { error ->
                    Log.w(TAG, "Backend analytics failed for event=$event", error)
                }
        }
    }

    override fun setUserProperty(name: String, value: String?) {
        if (name.isBlank()) return
        track("analytics_user_property_set", mapOf("name" to name, "has_value" to (!value.isNullOrBlank()).toString()))
    }

    override fun setUserId(id: String?) {
        track("analytics_user_id_set", mapOf("has_value" to (!id.isNullOrBlank()).toString()))
    }

    suspend fun drainQueue() {
        drainQueuedEvents()
    }

    private suspend fun drainQueuedEvents() {
        queue.drain { queued ->
            val dto = AppAnalyticsEventRequestDto(
                eventName = queued.eventName,
                properties = queued.properties,
                clientEventTimeMs = queued.clientEventTimeMs,
                flavor = queued.flavor,
                appVersionName = queued.appVersionName,
                appVersionCode = queued.appVersionCode,
                // Same id as the original attempt — the whole point: the server dedupes the resend.
                clientEventId = queued.clientEventId,
            )
            runCatching { apiProvider.get().recordAnalyticsEvent(dto) }.isSuccess
        }
    }

    companion object {
        /** Request property carrying the backend-dedupe hint -- see this class's kdoc. */
        const val CLIENT_EVENT_ID_PARAM = "client_event_id"

        /**
         * The forensic-debugging event subset that must survive being offline, per the P2
         * backend-analytics-durability fix: proof-processing failures, dead outbox writes,
         * feed-distribution proof funnel transitions, teammate proof-capture reads, and weighing
         * capture failures. Deliberately hand-picked -- everything else stays fire-and-forget,
         * matching this adapter's pre-existing best-effort contract.
         */
        val CRITICAL_EVENT_ALLOWLIST = setOf(
            AnalyticsEvents.PROOF_CAPTURE_COMPLETED,
            AnalyticsEvents.PROOF_CAPTURE_VALIDATION_FAILED,
            AnalyticsEvents.PROOF_PROCESSING_STARTED,
            AnalyticsEvents.PROOF_PROCESSING_COMPLETED,
            AnalyticsEvents.PROOF_PROCESSING_FAILED,
            AnalyticsEvents.PROOF_GALLERY_SAVE_STARTED,
            AnalyticsEvents.PROOF_GALLERY_SAVE_COMPLETED,
            AnalyticsEvents.PROOF_GALLERY_SAVE_FAILED,
            AnalyticsEvents.PROOF_UPLOAD_STARTED,
            AnalyticsEvents.PROOF_UPLOAD_COMPLETED,
            AnalyticsEvents.PROOF_UPLOAD_FAILED,
            AnalyticsEvents.PROOF_UPLOAD_ENQUEUE_FAILED,
            AnalyticsEvents.PROOF_UPLOAD_DRIVER_MISSING,
            AnalyticsEvents.PROOF_UPLOAD_REGISTERED,
            AnalyticsEvents.PROOF_CAMERA_REQUESTED,
            AnalyticsEvents.PROOF_CAMERA_VISIBLE,
            AnalyticsEvents.PROOF_CAMERA_BOUND,
            AnalyticsEvents.PROOF_CAMERA_STREAMING,
            AnalyticsEvents.PROOF_CAMERA_RECORDING_STARTED,
            AnalyticsEvents.PROOF_CAMERA_STOP_TAPPED,
            AnalyticsEvents.PROOF_CAMERA_CANCELLED,
            AnalyticsEvents.PROOF_CAMERA_RETRY_TAPPED,
            AnalyticsEvents.PROOF_CAMERA_FINALIZED,
            AnalyticsEvents.PROOF_CAMERA_FAILED,
            AnalyticsEvents.PROOF_CAMERA_TORCH_ON,
            AnalyticsEvents.PROOF_CAMERA_TORCH_OFF,
            AnalyticsEvents.PROOF_CAMERA_TORCH_FAILED,
            AnalyticsEvents.PROOF_GALLERY_PICKER_OPENED,
            AnalyticsEvents.PROOF_GALLERY_PICKER_CANCELLED,
            AnalyticsEvents.PROOF_GALLERY_PICKER_IMPORTED,
            AnalyticsEvents.PROOF_GALLERY_PICKER_FAILED,
            AnalyticsEvents.SYNC_WRITE_DEPENDENCY_WAIT,
            AnalyticsEvents.SYNC_WRITE_DEAD,
            AnalyticsEvents.PC_CARE_STOCK_PROOF_SCREEN_VISIBLE,
            AnalyticsEvents.PC_CARE_STOCK_PROOF_ACTION_TAPPED,
            AnalyticsEvents.PC_CARE_STOCK_PROOF_CAPTURE_RESULT,
            AnalyticsEvents.PC_CARE_STOCK_PROOF_ROOM_WRITTEN,
            AnalyticsEvents.PC_CARE_STOCK_PROOF_UPLOAD_ENQUEUED,
            AnalyticsEvents.PC_CARE_STOCK_PROOF_REGISTRATION,
            AnalyticsEvents.PC_CARE_STOCK_PROOF_SYNC,
            AnalyticsEvents.PC_CARE_STOCK_PROOF_PREVIEW,
            AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT,
            AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT_ENQUEUED,
            AnalyticsEvents.PC_CARE_SUBMIT_BLOCKED,
            AnalyticsEvents.PC_CARE_SUBMIT_CONFIRMED,
            AnalyticsEvents.VACCINATION_PROOF_CAPTURE_CANCELLED,
            AnalyticsEvents.VACCINATION_PROOF_CAPTURE_FAILURE,
            AnalyticsEvents.FEED_DISTRIBUTION_OPENED,
            AnalyticsEvents.FEED_DISTRIBUTION_CAPTURE_TAPPED,
            AnalyticsEvents.FEED_DISTRIBUTION_PROOF_CAPTURED,
            AnalyticsEvents.FEED_DISTRIBUTION_PROOF_UPLOAD_SYNCED,
            AnalyticsEvents.FEED_DISTRIBUTION_PROOF_REUPLOAD_TAPPED,
            AnalyticsEvents.FEED_DISTRIBUTION_SUBMIT_BLOCKED,
            AnalyticsEvents.FEED_DISTRIBUTION_SYNC_TAPPED,
            AnalyticsEvents.FEED_DISTRIBUTION_WEIGHT_PHOTO_CAPTURED,
            AnalyticsEvents.FEED_DISTRIBUTION_VIDEO_CAPTURED,
            AnalyticsEvents.FEED_DISTRIBUTION_WATER_PROOF_CAPTURED,
            AnalyticsEvents.FEED_DISTRIBUTION_SUBMITTED,
            AnalyticsEvents.FEED_DISTRIBUTION_LIVE_STATUS_CHANGED,
            AnalyticsEvents.FEED_DISTRIBUTION_TEAMMATE_CAPTURES_READ,
            AnalyticsEvents.FEED_DISTRIBUTION_TEAMMATE_PROOF_ADOPTED,
            AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
            // The ONLY event carrying all three split-operator slot sources (weight/feed/water:
            // local vs teammate). Firebase intentionally drops feed_video_source/water_video_source
            // under the 25-param cap, so losing the backend copy loses the forensic record
            // (external review 2026-08-16).
            AnalyticsEvents.FEED_DISTRIBUTION_SUBMIT_SOURCES,
            AnalyticsEvents.FEED_PACKING_COMPLETE_OPENED,
            AnalyticsEvents.FEED_PACKING_VIDEO_CAPTURED,
            AnalyticsEvents.FEED_PACKING_SUBMITTED,
            AnalyticsEvents.FEED_PACKING_COMPLETE_FAILURE,
            AnalyticsEvents.FEED_TRANSPORT_VIEWED,
            AnalyticsEvents.FEED_TRANSPORT_OPENED,
            AnalyticsEvents.FEED_TRANSPORT_VIDEO_CAPTURED,
            AnalyticsEvents.FEED_TRANSPORT_CAPTURE_TAPPED,
            AnalyticsEvents.FEED_TRANSPORT_PROOF_UPLOAD_SYNCED,
            AnalyticsEvents.FEED_TRANSPORT_REUPLOAD_TAPPED,
            AnalyticsEvents.FEED_TRANSPORT_SYNC_TAPPED,
            AnalyticsEvents.FEED_TRANSPORT_SUBMIT_BLOCKED,
            AnalyticsEvents.FEED_TRANSPORT_SUBMITTED,
            AnalyticsEvents.FEED_TRANSPORT_FAILURE,
            AnalyticsEvents.FEED_WASTAGE_VIEWED,
            AnalyticsEvents.FEED_WASTAGE_COMPLETE_OPENED,
            AnalyticsEvents.FEED_WASTAGE_VIDEO_CAPTURED,
            AnalyticsEvents.FEED_WASTAGE_SUBMITTED,
            AnalyticsEvents.FEED_WASTAGE_COMPLETE_FAILURE,
            AnalyticsEvents.WEIGHING_CAPTURE_FAILURE,
            AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_FAILURE,
            AnalyticsEvents.WEIGHING_PROOF_CAPTURE_FAILURE,
            AnalyticsEventsWeighing.WEIGHING_ORPHAN_SYNCED_PROOF_RECOVERED,
            AnalyticsEventsWeighing.WEIGHING_PROOF_ATTACH_NO_OBSERVATION,
            AnalyticsEventsWeighing.WEIGHING_OBSERVATION_ENQUEUE_FAILED,
        )

        private const val TAG = "GoatAnalytics"
    }
}
