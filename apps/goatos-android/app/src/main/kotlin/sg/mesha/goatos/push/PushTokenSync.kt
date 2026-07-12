package sg.mesha.goatos.push

import com.google.android.gms.tasks.Tasks
import com.google.firebase.messaging.FirebaseMessaging
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import sg.mesha.goatos.core.notifications.NotificationsPort
import javax.inject.Inject

/**
 * Best-effort push-token coupling seam, called once after every successful bootstrap
 * ([sg.mesha.goatos.boot.BootstrapViewModel.applyAnalyticsIdentity]) — covers BOTH a fresh
 * sign-in AND a cold start with an already-valid session, since [GoatOsMessagingService]'s
 * `onNewToken` only fires once per token mint/rotation, not on every app open. A `fun interface`
 * (mirrors this codebase's `ScreenCacheStore`/`OutboxWiper`/`SyncJobsCanceller` port pattern) so
 * tests can pass a trivial SAM lambda instead of a mock.
 */
fun interface PushTokenSync {
    fun syncNow()
}

/**
 * Fetches the CURRENT FCM registration token and forwards it to
 * [NotificationsPort.registerToken] — the SAME seam [GoatOsMessagingService.onNewToken] uses —
 * so the backend's device binding is fresh even when the token was already minted before this
 * bootstrap ran.
 *
 * Firebase absence (a build flavor with no committed `firebase.xml`, e.g. `prod` today) fails
 * closed silently via `runCatching`: push is an enhancement, never a gate on auth/nav/bootstrap.
 * Mirrors [sg.mesha.goatos.auth.FirebaseAuthRepository]'s `Tasks.await` + `Dispatchers.IO`
 * pattern for consistency with the rest of this app's Firebase call sites.
 */
class AndroidPushTokenSync(
    private val notificationsPort: NotificationsPort,
    private val appScope: CoroutineScope,
) : PushTokenSync {
    override fun syncNow() {
        appScope.launch {
            val token = runCatching {
                withContext(Dispatchers.IO) { Tasks.await(FirebaseMessaging.getInstance().token) }
            }.getOrNull()
            if (!token.isNullOrBlank()) {
                notificationsPort.registerToken(token)
            }
        }
    }
}
