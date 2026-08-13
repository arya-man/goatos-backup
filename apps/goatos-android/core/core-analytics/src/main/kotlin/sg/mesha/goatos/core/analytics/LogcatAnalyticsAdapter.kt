package sg.mesha.goatos.core.analytics

import android.util.Log

/**
 * Mirrors every analytics event to logcat, in addition to the real sink.
 *
 * Why this exists: during device E2E the only way to see whether an event fired was to wait for it
 * to reach Firebase, or to read `FA-SVC` verbose logs — which are invisible when the app runs under
 * a secondary Android user, and useless when the device has no Play Services. Debugging "I tapped
 * the button and nothing happened" should not depend on a network round trip to Google.
 *
 * Every event is printed as one greppable line:
 *     GoatOSAnalytics  event=verify_video_play_started item_id=... device_id=... journey_id=...
 *
 * Wrap the real adapter rather than replacing it, so debug builds still deliver to Firebase AND
 * print locally. Never install this in a release build: event names and params are internal detail.
 *
 * Logged at INFO, deliberately. At DEBUG these lines never appeared on the E2E handsets at all:
 * several OEM builds (Infinix and Xiaomi among them) drop DEBUG for tags that are not whitelisted
 * via `setprop log.tag.<TAG>`, so the mirror added to make events visible was itself invisible —
 * indistinguishable, from the outside, from the event never firing.
 */
class LogcatAnalyticsAdapter(
    private val delegate: AnalyticsPort,
    private val analyticsContext: AnalyticsContext,
) : AnalyticsPort {

    override fun track(event: String, props: Map<String, String>) {
        val identity = buildString {
            analyticsContext.deviceId?.let { append(" device_id=").append(it) }
            analyticsContext.journeyId?.let { append(" journey_id=").append(it) }
        }
        val params = props.entries.joinToString(" ") { "${it.key}=${it.value}" }
        Log.i(TAG, "event=$event $params$identity")
        delegate.track(event, props)
    }

    override fun setUserId(userId: String?) {
        Log.i(TAG, "user_id=$userId")
        delegate.setUserId(userId)
    }

    override fun setUserProperty(name: String, value: String?) {
        Log.i(TAG, "user_property $name=$value")
        delegate.setUserProperty(name, value)
    }

    private companion object {
        const val TAG = "GoatOSAnalytics"
    }
}
