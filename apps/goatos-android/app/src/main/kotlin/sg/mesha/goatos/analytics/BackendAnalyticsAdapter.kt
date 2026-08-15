package sg.mesha.goatos.analytics

import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.analytics.AnalyticsContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
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
 * incidents -- IS now durable. A failed send for one of those event names is persisted to
 * [queue] (a minimal, capped, file-backed queue -- see [DurableAnalyticsQueue]'s kdoc for what it
 * does and does NOT do) and retried opportunistically the next time [track] runs on ANY event
 * (see [drainQueuedEvents]). This is intentionally not full parity with the app's existing
 * Room-backed write outbox: no WorkManager-scheduled background drain, no exponential backoff,
 * and only NETWORK-ACTIVITY-triggered draining rather than a dedicated connectivity callback --
 * both would need new DI wiring into `di/AnalyticsModule.kt` / `GoatOsApplication.kt`, which is
 * out of scope for this fix. Non-critical events remain exactly as fire-and-forget as before.
 *
 * Every request carries a [CLIENT_EVENT_ID_PARAM] property -- a fresh, per-call
 * [UUID.randomUUID] -- so the backend CAN dedupe a rare double-send (a send that actually reached
 * the backend but whose local queue removal was then lost to a process death before the next
 * drain runs). `AppAnalyticsEventRequestDto` (`core/core-network`) has no dedicated dedupe field
 * today, so this is carried as a normal property rather than a first-class request field; true
 * exactly-once delivery is NOT guaranteed, only at-most-once-per-confirmed-local-removal, and
 * rare duplicates on the backend are an accepted, documented tradeoff of not extending the DTO.
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
        )
        val isCritical = event in CRITICAL_EVENT_ALLOWLIST
        appScope.launch {
            // Opportunistic drain: any live network activity from this adapter is itself evidence
            // connectivity may be back, so flush previously-queued critical events first. See
            // this class's kdoc for why this call site -- not a ConnectivityManager callback or a
            // WorkManager job -- is the drain trigger.
            runCatching { drainQueuedEvents() }
                .onFailure { Log.w(TAG, "Durable analytics queue drain failed", it) }

            runCatching { apiProvider.get().recordAnalyticsEvent(request) }
                .onFailure { error ->
                    Log.w(TAG, "Backend analytics failed for event=$event", error)
                    if (isCritical) {
                        queue.enqueue(
                            QueuedAnalyticsEvent(
                                clientEventId = clientEventId,
                                eventName = event,
                                properties = mergedProps,
                                clientEventTimeMs = request.clientEventTimeMs,
                                flavor = request.flavor,
                                appVersionName = request.appVersionName,
                                appVersionCode = request.appVersionCode,
                            ),
                        )
                    }
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
         * feed-distribution live-status transitions, teammate proof-capture reads, and weighing
         * capture failures. Deliberately small and hand-picked -- everything else stays
         * fire-and-forget, matching this adapter's pre-existing best-effort contract.
         */
        val CRITICAL_EVENT_ALLOWLIST = setOf(
            // core-data's CaptureRepository tracks this by string literal (proofProcessingFailedEvent)
            // rather than an AnalyticsEvents constant; the wire value is pinned here to match.
            "proof_processing_failed",
            AnalyticsEvents.SYNC_WRITE_DEAD,
            AnalyticsEvents.FEED_DISTRIBUTION_LIVE_STATUS_CHANGED,
            AnalyticsEvents.FEED_DISTRIBUTION_TEAMMATE_CAPTURES_READ,
            AnalyticsEvents.WEIGHING_CAPTURE_FAILURE,
            AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_FAILURE,
            AnalyticsEvents.WEIGHING_PROOF_CAPTURE_FAILURE,
        )

        private const val TAG = "GoatAnalytics"
    }
}
