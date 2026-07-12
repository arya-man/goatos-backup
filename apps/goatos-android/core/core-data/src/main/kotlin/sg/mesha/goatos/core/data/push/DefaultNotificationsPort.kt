package sg.mesha.goatos.core.data.push

import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.datastore.DeviceStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.HeartbeatDeviceRequestDto
import sg.mesha.goatos.core.network.RegisterDeviceRequestDto
import sg.mesha.goatos.core.notifications.NotificationsPort

/**
 * Registers this device's live FCM registration token with the backend, reusing the SAME
 * device register/heartbeat endpoints [sg.mesha.goatos.core.data.DefaultBootstrapRepository]
 * already calls (docs/mobile — FCM push slice).
 *
 * ### Wire-field decision: the RAW token rides in `push_token_hash`
 * [RegisterDeviceRequestDto.pushTokenHash] / [HeartbeatDeviceRequestDto.pushTokenHash] are
 * named for a hashed value, but this port deliberately sends the RAW FCM registration [token]
 * verbatim in that field: the backend's `messaging.Send(ctx, &messaging.Message{Token: ...})`
 * call needs the literal token FCM issued to address this device — a hash of it cannot deliver
 * a message. `push_token_hash` is the only wire channel this contract exposes for a device's
 * push binding today, so sending the raw token there is the pragmatic path that makes delivery
 * work now. Coordinate with the backend FCM slice (the sibling `goatos-fcm-notif` worktree,
 * which is adding a dedicated raw-token column) before this field is renamed or a distinct
 * field is introduced — when the contract changes, only this class's request-building needs
 * to move to the new field name, not the whole push pipeline.
 *
 * Synchronous [registerToken] (matches [NotificationsPort]): Firebase's `onNewToken` callback
 * is not itself a suspend function, so the actual network call is dispatched onto [appScope]
 * (the app's own singleton lifetime scope, injected — never `GlobalScope`) rather than blocking
 * the caller. Best-effort: a failed registration is logged, not retried inline — the next
 * `onNewToken` (token rotation) or [sg.mesha.goatos.push.PushTokenSync] pass (bootstrap/login)
 * naturally retries.
 */
class DefaultNotificationsPort(
    private val api: AppApi,
    private val deviceStore: DeviceStore,
    private val appScope: CoroutineScope,
    private val appVersion: String,
    private val osVersion: String,
) : NotificationsPort {

    override fun registerToken(token: String) {
        if (token.isBlank()) return
        appScope.launch {
            runCatching {
                val deviceId = deviceStore.deviceId()?.takeIf { it.isNotBlank() }
                if (deviceId != null) {
                    api.heartbeatDevice(
                        deviceId,
                        HeartbeatDeviceRequestDto(
                            appVersion = appVersion,
                            osVersion = osVersion,
                            pushTokenHash = token,
                        ),
                    )
                } else {
                    val response = api.registerDevice(
                        RegisterDeviceRequestDto(
                            appInstallId = deviceStore.appInstallId(),
                            appVersion = appVersion,
                            osVersion = osVersion,
                            pushTokenHash = token,
                        ),
                    )
                    deviceStore.setDeviceId(response.device.deviceId.ifBlank { null })
                }
            }.onFailure { t ->
                Log.w(TAG, "Push token registration failed; will retry on next token rotation/bootstrap.", t)
            }
        }
    }

    private companion object {
        const val TAG = "NotificationsPort"
    }
}
