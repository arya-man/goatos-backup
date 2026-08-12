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
