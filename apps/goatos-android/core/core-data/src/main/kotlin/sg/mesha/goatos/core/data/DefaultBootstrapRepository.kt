package sg.mesha.goatos.core.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import sg.mesha.goatos.core.network.BootstrapError

import sg.mesha.goatos.core.datastore.DeviceStore
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.BootstrapDto
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.HeartbeatDeviceRequestDto
import sg.mesha.goatos.core.network.RegisterDeviceRequestDto
import sg.mesha.goatos.core.network.asBootstrapError
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
    /**
     * Whether this phone will actually SHOW what we send it (the OS notification switch, supplied
     * by :app). Reported on register AND on every heartbeat, so the backend can mark a device
     * push-muted instead of counting an OS-dropped push as delivered. `null` = not reported.
     */
    private val notificationsEnabled: () -> Boolean? = { null },
    /**
     * Where device reconciliation (heartbeat / first-time register) runs. Production passes the
     * application scope so it never gates nav readiness. When null (legacy/tests) it runs inline
     * but is capped at [DEVICE_RECONCILE_BUDGET_MS], so a slow device call can delay Ready by at
     * most that budget instead of a full OkHttp call timeout.
     */
    private val deviceReconcileScope: CoroutineScope? = null,
) : BootstrapRepository {
    companion object {
        const val DEVICE_RECONCILE_BUDGET_MS: Long = 2_000
    }

    /** Single-flight: rapid bootstraps (resume, quiet refresh) must not register twice. */
    private val reconcileMutex = Mutex()

    override suspend fun loadNavState(): NavState =
        try {
            val deviceId = deviceStore?.deviceId()
            val dto = api.bootstrap(deviceId)
            cache?.save(dto)
            // Device bookkeeping is best-effort and must never hold the shell on Loading: on
            // 1.0.40 a slow heartbeat during a network blip delayed Ready by a whole call timeout.
            val scope = deviceReconcileScope
            if (scope != null) {
                scope.launch { reconcileDevice(dto) }
            } else {
                withTimeoutOrNull(DEVICE_RECONCILE_BUDGET_MS) { reconcileDevice(dto) }
            }
            dto.toNavState()
        } catch (t: Throwable) {
            // Map network-layer errors to domain-level bootstrap errors.
            // Auth/permission failures (401/403) must NOT fall back to cache — a stale
            // cached shell can never mask a rejected/expired token, revoked device, missing
            // grant, or switched user. Connectivity failures may fall back and retry.
            val bootstrapError = t.asBootstrapError()
            when (bootstrapError) {
                is BootstrapError.AuthSessionExpired -> throw bootstrapError
                // Access not provisioned yet: surface it, but never destroy local work.
                is BootstrapError.AccessNotProvisioned -> throw bootstrapError
                is BootstrapError.ConnectivityFailure -> {
                    // Try to fall back to cached bootstrap on connectivity failure only.
                    cache?.load()?.toNavState() ?: throw bootstrapError
                }
            }
        }

    override suspend fun operatorProfile(): BootstrapOperatorProfileDto? =
        (
            cache?.load()
                ?: runCatching { api.bootstrap(deviceStore?.deviceId()).also { cache?.save(it) } }
                    .onFailure { android.util.Log.e("DefaultBootstrapRepository", "fetch bootstrap for operatorProfile failed", it) }
                    .getOrNull()
        )?.operatorProfile

    override suspend fun actorTenantId(): String? =
        (
            cache?.load()
                ?: runCatching { api.bootstrap(deviceStore?.deviceId()).also { cache?.save(it) } }
                    .onFailure { android.util.Log.e("DefaultBootstrapRepository", "fetch bootstrap for actorTenantId failed", it) }
                    .getOrNull()
        )?.actor?.tenantId?.ifBlank { null }

    override suspend fun feedWaterRemovalCutoffTime(): String? =
        (
            cache?.load()
                ?: runCatching { api.bootstrap(deviceStore?.deviceId()).also { cache?.save(it) } }
                    .onFailure { android.util.Log.e("DefaultBootstrapRepository", "fetch bootstrap for feedWaterRemovalCutoffTime failed", it) }
                    .getOrNull()
        )?.feedWaterRemovalCutoffTime?.ifBlank { null }

    /** Remember a known device id, or register this install when the backend needs it. */
    private suspend fun reconcileDevice(dto: BootstrapDto) = reconcileMutex.withLock { reconcileDeviceLocked(dto) }

    private suspend fun reconcileDeviceLocked(dto: BootstrapDto) {
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
                        HeartbeatDeviceRequestDto(
                            appVersion = appVersion,
                            osVersion = osVersion,
                            notificationsEnabled = notificationsEnabled(),
                        ),
                    )
                }
            }
            return
        }

        // Not registered yet — register this install. Best-effort: a failure here must not
        // fail the whole bootstrap (nav still renders), so it is swallowed and the next
        // launch retries.
        // A concurrent bootstrap (queued on the mutex) may have registered this install already;
        // its dto predates that, so trust the store rather than registering a second time.
        if (!store.deviceId().isNullOrBlank()) return
        // exception:exempt best-effort device registration; the next bootstrap retries it
        runCatching {
            val response = api.registerDevice(
                RegisterDeviceRequestDto(
                    appInstallId = store.appInstallId(),
                    appVersion = appVersion,
                    osVersion = osVersion,
                    notificationsEnabled = notificationsEnabled(),
                ),
            )
            store.setDeviceId(response.device.deviceId.ifBlank { null })
        }
    }
}
