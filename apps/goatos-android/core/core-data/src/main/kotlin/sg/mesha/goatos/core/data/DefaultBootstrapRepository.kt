package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.datastore.DeviceStore
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.BootstrapDto
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.HeartbeatDeviceRequestDto
import sg.mesha.goatos.core.network.RegisterDeviceRequestDto
import sg.mesha.goatos.core.network.toNavState

/**
 * Offline-first bootstrap: fetch fresh, cache it, and fall back to the cached bootstrap
 * when the network fails.
 *
 * Also reconciles the Android device: the persisted [DeviceStore.deviceId] is sent as the
 * bootstrap `device_id` so the backend can return this device's state, and when the backend
 * reports the device is required but not yet registered, the app registers it (best-effort —
 * registration never blocks the nav load).
 * (ETag/contract-revision revalidation + Proto DataStore session land next.)
 */
class DefaultBootstrapRepository(
    private val api: AppApi,
    private val cache: BootstrapCache? = null,
    private val deviceStore: DeviceStore? = null,
    private val appVersion: String = "",
    private val osVersion: String = "",
) : BootstrapRepository {
    override suspend fun loadNavState(): NavState =
        try {
            val deviceId = deviceStore?.deviceId()
            val dto = api.bootstrap(deviceId)
            cache?.save(dto)
            reconcileDevice(dto)
            dto.toNavState()
        } catch (t: java.io.IOException) {
            // Offline-first fallback is ONLY for transport/connectivity failures. An
            // auth/permission failure (401/403 arrives as retrofit HttpException, NOT an
            // IOException) and any other error propagate — so a stale cached shell can
            // never mask a rejected token / revoked device / missing grant / switched user.
            cache?.load()?.toNavState() ?: throw t
        }

    override suspend fun operatorProfile(): BootstrapOperatorProfileDto? =
        (
            cache?.load()
                ?: runCatching { api.bootstrap(deviceStore?.deviceId()).also { cache?.save(it) } }.getOrNull()
        )?.operatorProfile

    override suspend fun actorTenantId(): String? =
        (
            cache?.load()
                ?: runCatching { api.bootstrap(deviceStore?.deviceId()).also { cache?.save(it) } }.getOrNull()
        )?.actor?.tenantId?.ifBlank { null }

    /** Remember a known device id, or register this install when the backend needs it. */
    private suspend fun reconcileDevice(dto: BootstrapDto) {
        val store = deviceStore ?: return
        if (!dto.deviceState.required) return

        val known = dto.deviceState.device
        if (known != null) {
            val id = known.deviceId.ifBlank { null }
            store.setDeviceId(id)
            // Refresh liveness + current app/OS version for this known device. Best-effort:
            // a failed heartbeat must not fail the nav load, so it is swallowed and the next
            // bootstrap retries. This is the only heartbeat trigger — once per bootstrap.
            if (id != null) {
                runCatching {
                    api.heartbeatDevice(
                        id,
                        HeartbeatDeviceRequestDto(appVersion = appVersion, osVersion = osVersion),
                    )
                }
            }
            return
        }

        // Not registered yet — register this install. Best-effort: a failure here must not
        // fail the whole bootstrap (nav still renders), so it is swallowed and the next
        // launch retries.
        runCatching {
            val response = api.registerDevice(
                RegisterDeviceRequestDto(
                    appInstallId = store.appInstallId(),
                    appVersion = appVersion,
                    osVersion = osVersion,
                ),
            )
            store.setDeviceId(response.device.deviceId.ifBlank { null })
        }
    }
}
