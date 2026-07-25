package sg.mesha.goatos.push

import com.google.firebase.messaging.FirebaseMessaging
import sg.mesha.goatos.core.analytics.AnalyticsContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Logout clean-slate (C35-001 continuation): decouples THIS device's push token + Firebase
 * Analytics identity from the departing principal, so a shared field phone's next sign-in never
 * inherits the prior operator's analytics identity or (until FCM re-registers on the next
 * login/token-rotation) push binding.
 *
 * Wired as [sg.mesha.goatos.core.data.LogoutCoordinator]'s `clearPushAndAnalyticsIdentity`
 * callback — see that class's KDoc for exactly where in the logout sequence this runs.
 * Best-effort: `deleteToken()` is fire-and-forget (Play Services completes it asynchronously
 * regardless of whether the returned `Task` is awaited), matching the "must never block local
 * sign-out" rule the whole coordinator follows. `FLAVOR` is intentionally left set — it is
 * build-fixed, not principal-specific (mirrors [sg.mesha.goatos.boot.BootstrapViewModel]'s
 * `applyAnalyticsIdentity`, which also never clears it).
 */
@Singleton
class PushLogoutCleanup @Inject constructor(
    private val analytics: AnalyticsPort,
    private val analyticsContext: AnalyticsContext,
) {
    fun clear() {
        runCatching { FirebaseMessaging.getInstance().deleteToken() }

        analyticsContext.role = null
        analyticsContext.parkScope = null
        analyticsContext.deviceId = null

        analytics.setUserId(null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.ROLE, null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.PRIMARY_PARK, null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.PARK_ID, null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.TENANT, null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.EMAIL, null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.DEVICE_ID, null)
    }
}
