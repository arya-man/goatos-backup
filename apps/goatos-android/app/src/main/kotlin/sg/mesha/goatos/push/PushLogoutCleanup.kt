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
 *
 * [AnalyticsContext.deviceId] is likewise intentionally left set: it identifies THIS physical
 * install, not the departing principal (see `DeviceStore.clear` KDoc), and multiple operators
 * sharing one field device is exactly the case the multi-device analytics keying exists to
 * disambiguate. Clearing it here would blank device_id on the very next event (SIGN_OUT) and
 * would only be "fixed" by the next bootstrap re-deriving the SAME persisted id — a no-op wipe
 * that costs a null-attributed event for nothing. [journeyId] IS cleared: it is this work
 * session's id, and logout ends the session.
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
        analyticsContext.journeyId = null

        analytics.setUserId(null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.ROLE, null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.PRIMARY_PARK, null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.PARK_ID, null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.TENANT, null)
        analytics.setUserProperty(AnalyticsEvents.UserProps.EMAIL, null)
        // DEVICE_ID user property is deliberately NOT cleared -- see class KDoc.
    }
}
