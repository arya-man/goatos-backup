package sg.mesha.goatos.push

import android.util.Log
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import dagger.hilt.android.AndroidEntryPoint
import sg.mesha.goatos.core.designsystem.R as DesignSystemR
import sg.mesha.goatos.core.notifications.NotificationsPort
import javax.inject.Inject

/**
 * FCM entry point (docs: FCM push slice, registered in AndroidManifest with the
 * `com.google.firebase.MESSAGING_EVENT` intent-filter). Two Firebase callbacks:
 *
 *  - [onNewToken] fires once per token mint/rotation (install, reinstall, app-data clear, or a
 *    security-driven Firebase-side rotation) — forwarded straight to
 *    [NotificationsPort.registerToken], the SAME seam [PushTokenSync] uses on launch/login.
 *  - [onMessageReceived] builds + posts a system notification for BOTH a data-only message and
 *    a notification+data message. This matters because FCM only auto-displays the
 *    `notification` block while the app is backgrounded/killed; while the app process is in the
 *    FOREGROUND, `onMessageReceived` is the ONLY thing that puts anything on screen — so this
 *    class must build the notification itself rather than relying on the payload's `notification`
 *    block to self-display. The full data payload rides along on the tap `PendingIntent` (see
 *    [PushNotifications]) so a tap deep-links to the right screen either way.
 *
 * Both OS callbacks are wrapped in `runCatching` — matching [PushLogoutCleanup] and
 * [AndroidPushTokenSync]'s standard for every other Firebase call site in this app. A build
 * flavor without a committed `firebase.xml` (`prod` today) can still have this service
 * instantiated and invoked by the OS; any Firebase/init failure inside these callbacks must
 * degrade push to a no-op, never crash the process the OS just woke up. (Analytics identity uses
 * a different safety story — [sg.mesha.goatos.core.analytics.FirebaseAnalyticsAdapter] is only
 * bound for flavors with a confirmed Firebase project, so it needs no self-guard.)
 */
@AndroidEntryPoint
class GoatOsMessagingService : FirebaseMessagingService() {

    @Inject lateinit var notificationsPort: NotificationsPort
    @Inject lateinit var pushNotifications: PushNotifications

    override fun onNewToken(token: String) {
        super.onNewToken(token)
        runCatching {
            notificationsPort.registerToken(token)
        }.onFailure { t ->
            Log.w(TAG, "onNewToken handling failed; push registration skipped for this rotation.", t)
        }
    }

    override fun onMessageReceived(message: RemoteMessage) {
        super.onMessageReceived(message)
        runCatching {
            val data = message.data
            val display = pushDisplayText(
                data = data,
                notificationTitle = message.notification?.title,
                notificationBody = message.notification?.body,
                defaultTitle = getString(DesignSystemR.string.push_default_title),
            )
            val title = display.title
            val body = display.body
            pushNotifications.show(title = title, body = body, payload = data)
            // TODO(backend): delivery/read ACK. AppApi has no "notification delivered/read" endpoint
            // today (checked core-network's AppApi — out of scope for the mobile FCM slice to invent
            // one). Once the backend FCM slice (goatos-fcm-notif worktree) adds one, call it here with
            // the message's `message.messageId` (delivery) and again from the tap path
            // (MainActivity.handlePushIntent, read confirmation) with the SAME idempotency-key
            // discipline every other outbox write in this app uses.
        }.onFailure { t ->
            Log.w(TAG, "onMessageReceived handling failed; notification dropped for this message.", t)
        }
    }

    private companion object {
        const val TAG = "GoatOsMessagingService"
    }
}

data class PushDisplayText(
    val title: String,
    val body: String,
)

fun pushDisplayText(
    data: Map<String, String>,
    notificationTitle: String?,
    notificationBody: String?,
    defaultTitle: String,
): PushDisplayText {
    val title = data[PushExtras.TITLE]?.trim()?.takeIf { it.isNotBlank() }
        ?: notificationTitle?.trim()?.takeIf { it.isNotBlank() }
        ?: defaultTitle
    val body = data[PushExtras.BODY]?.trim()?.takeIf { it.isNotBlank() }
        ?: notificationBody?.trim().orEmpty()
    return PushDisplayText(title = title, body = body)
}
