package sg.mesha.goatos.core.data

import android.util.Log
import kotlinx.coroutines.CancellationException
import sg.mesha.goatos.core.data.sync.OutboxWiper
import sg.mesha.goatos.core.data.sync.SyncJobsCanceller
import sg.mesha.goatos.core.datastore.DeviceStore
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.network.AppApi

/**
 * The ONE logout path (C35-001 fix): a full clean-slate wipe of every authority-sensitive
 * piece of local state, shared by `SessionViewModel` and `ProfileViewModel` so neither can
 * drift into a partial wipe. Before this, both call sites only signed out of Firebase and
 * dropped the bearer token — nine Room cache tables, the write outbox, the device identity,
 * the persisted language, and the periodic/retry WorkManager jobs all survived logout, so the
 * next principal on the device could see the prior user's data and push/device binding.
 *
 * Sequencing is deliberate, not incidental:
 *  1. [deregisterDeviceBestEffort] — MUST run BEFORE [signOutVendorAuth] / any local token
 *     clear, because the backend call needs the still-live session to authenticate as the
 *     departing actor (the network layer's bearer interceptor reads the live Firebase user /
 *     dev-bearer token per request; once vendor auth signs out or the token is cleared, the
 *     call would 401 and could never actually revoke the right device).
 *  2. [signOutVendorAuth] — the Firebase/dev-bearer-specific sign-out. `core-data` cannot
 *     depend on `:app`'s `AuthRepository`, so the caller supplies this as a callback; the
 *     coordinator still owns exactly where in the sequence it runs.
 *  2.5. [clearPushAndAnalyticsIdentity] — decouples THIS device's FCM push-token binding and
 *     Firebase Analytics identity (setUserId + role/park/tenant user properties) from the
 *     departing principal, the same "no authority-sensitive state survives logout" rule as
 *     every other step. Same constraint as [signOutVendorAuth]: the real
 *     `FirebaseMessaging.deleteToken()` / `AnalyticsPort` call lives in `:app`
 *     ([sg.mesha.goatos.push.PushLogoutCleanup]), so it is supplied as a callback rather than
 *     a constructor dependency `core-data` cannot hold. Defaulted to a no-op so existing
 *     callers/tests that don't care about push/analytics compile unchanged.
 *  3. Every screen-facing Room cache table ([ScreenCacheStore]) and the write outbox
 *     ([OutboxWiper]) are wiped — nothing from the departing session survives on disk.
 *  4. The periodic + retry WorkManager sync jobs are cancelled ([SyncJobsCanceller]) so
 *     nothing tries to drain (or resurrect a retry for) an outbox that was just wiped.
 *  5. [SessionStore] and [DeviceStore] are cleared last (token, language, app_install_id,
 *     device_id) — the next principal starts from a fresh install identity.
 */
class LogoutCoordinator(
    private val api: AppApi,
    private val deviceStore: DeviceStore,
    private val sessionStore: SessionStore,
    private val screenCacheStore: ScreenCacheStore,
    private val outboxWiper: OutboxWiper,
    private val syncJobsCanceller: SyncJobsCanceller,
    private val clearPushAndAnalyticsIdentity: () -> Unit = {},
    private val feedCompletionLocalStore: FeedCompletionLocalStore = FeedCompletionLocalStore(),
) {
    suspend fun logout(signOutVendorAuth: () -> Unit) {
        deregisterDeviceBestEffort()
        runCatching { signOutVendorAuth() }
        runCatching { clearPushAndAnalyticsIdentity() }
        // In-MEMORY authority-sensitive state. The Room wipe below cannot reach it: this overlay
        // is a process singleton, so without this the departing operator's optimistic feed
        // completions stayed visible to the next principal signing in on the same device.
        feedCompletionLocalStore.clear()
        screenCacheStore.clearAll()
        outboxWiper.clearAll()
        syncJobsCanceller.cancelAll()
        sessionStore.clear()
        deviceStore.clear()
    }

    /** Best-effort: an unreachable backend or an already-revoked device must never block the
     *  local wipe that follows — a failed remote call is far less harmful than a partial local
     *  logout. Logged (not swallowed) so a stuck push-token binding stays diagnosable. */
    private suspend fun deregisterDeviceBestEffort() {
        val deviceId = runCatching { deviceStore.deviceId() }
            .getOrNull()
            ?.takeIf { it.isNotBlank() }
            ?: return
        try {
            api.deregisterDevice(deviceId)
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (t: Throwable) {
            Log.w(TAG, "Device deregister failed for $deviceId; continuing local logout wipe.", t)
        }
    }

    private companion object {
        const val TAG = "LogoutCoordinator"
    }
}
