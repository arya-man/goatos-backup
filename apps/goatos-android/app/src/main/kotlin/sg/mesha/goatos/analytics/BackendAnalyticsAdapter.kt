package sg.mesha.goatos.analytics

import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.analytics.AnalyticsContext
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.standardEventParams
import sg.mesha.goatos.core.network.AppAnalyticsEventRequestDto
import sg.mesha.goatos.core.network.AppApi
import javax.inject.Provider

/**
 * Backend mirror for product analytics. Firebase/GA4 is useful, but not trustworthy enough as the
 * only receipt surface while the console can lag or be misconfigured.
 *
 * Delivery is explicitly BEST-EFFORT, not durable: [track] fires the network call on [appScope]
 * and does not retry, queue, or persist it. If the device is offline or the request otherwise
 * fails, the event is silently dropped from the backend's perspective — only a [Log.w] breadcrumb
 * (event name + error) survives, and only for as long as this process/logcat session lives. Do not
 * treat backend analytics as a forensic-grade proof source for any event that can occur while the
 * device is offline; nothing here currently reconciles a dropped send. Full durability (a Room-backed
 * outbox with retry, mirroring the existing offline outbox pattern elsewhere in the app) is an
 * intentional follow-up, not yet implemented.
 */
class BackendAnalyticsAdapter(
    private val apiProvider: Provider<AppApi>,
    private val appScope: CoroutineScope,
    private val analyticsContext: AnalyticsContext,
    private val clock: () -> Long = System::currentTimeMillis,
) : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {
        if (event.isBlank()) return
        val mergedProps = analyticsContext.standardEventParams() + props
        val request = AppAnalyticsEventRequestDto(
            eventName = event,
            properties = mergedProps,
            clientEventTimeMs = clock(),
            flavor = BuildConfig.FLAVOR,
            appVersionName = BuildConfig.VERSION_NAME,
            appVersionCode = BuildConfig.VERSION_CODE,
        )
        appScope.launch {
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

    private companion object {
        const val TAG = "GoatAnalytics"
    }
}
