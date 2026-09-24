package sg.mesha.goatos.core.data

import kotlinx.coroutines.delay
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.datastore.FakeDeviceStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.BootstrapDeviceStateDto
import sg.mesha.goatos.core.network.BootstrapDto
import sg.mesha.goatos.core.network.DeviceResponseDto
import sg.mesha.goatos.core.network.DeviceSummaryDto
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.HeartbeatDeviceRequestDto
import sg.mesha.goatos.core.network.RegisterDeviceRequestDto

/**
 * Field incident (app 1.0.40, brief DNS drop): bootstrap awaited the device heartbeat/register
 * inside [DefaultBootstrapRepository.loadNavState], so a slow device call held the shell on
 * Loading for up to a full OkHttp call timeout even though the nav payload was already in hand.
 * Device reconciliation is best-effort bookkeeping and must never gate Ready.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class BootstrapDeviceReconcileNonBlockingTest {

    private class SlowDeviceApi(
        private val registered: Boolean,
        private val deviceCallDelayMs: Long,
    ) : AppApi by FakeAppApi() {
        var heartbeatCalls = 0
        var registerCalls = 0

        override suspend fun bootstrap(deviceId: String?): BootstrapDto = BootstrapDto(
            deviceState = BootstrapDeviceStateDto(
                required = true,
                device = if (registered) DeviceSummaryDto(deviceId = "device-known") else null,
            ),
        )

        override suspend fun heartbeatDevice(deviceId: String, request: HeartbeatDeviceRequestDto): DeviceResponseDto {
            heartbeatCalls++
            delay(deviceCallDelayMs)
            return DeviceResponseDto(device = DeviceSummaryDto(deviceId = deviceId, status = "active"))
        }

        override suspend fun registerDevice(request: RegisterDeviceRequestDto): DeviceResponseDto {
            registerCalls++
            delay(deviceCallDelayMs)
            return DeviceResponseDto(device = DeviceSummaryDto(deviceId = "device-new", status = "active"))
        }
    }

    @Test
    fun `a 30s heartbeat does not delay nav readiness`() = runTest {
        val api = SlowDeviceApi(registered = true, deviceCallDelayMs = 30_000)
        val repo = DefaultBootstrapRepository(api = api, deviceStore = FakeDeviceStore())

        val start = testScheduler.currentTime
        repo.loadNavState()
        val timeToReadyMs = testScheduler.currentTime - start
        println("BEFORE/AFTER metric: heartbeat=30000ms time_to_ready_ms=$timeToReadyMs")

        assertTrue(
            "nav must be ready without waiting on the heartbeat (took ${timeToReadyMs}ms)",
            timeToReadyMs <= DefaultBootstrapRepository.DEVICE_RECONCILE_BUDGET_MS,
        )
        advanceUntilIdle()
        assertEquals("heartbeat still attempted", 1, api.heartbeatCalls)
    }

    @Test
    fun `a 30s register does not delay nav readiness and still records the device id`() = runTest {
        val api = SlowDeviceApi(registered = false, deviceCallDelayMs = 30_000)
        val store = FakeDeviceStore()
        val repo = DefaultBootstrapRepository(
            api = api,
            deviceStore = store,
            deviceReconcileScope = backgroundScope,
        )

        val start = testScheduler.currentTime
        repo.loadNavState()
        val timeToReadyMs = testScheduler.currentTime - start
        println("BEFORE/AFTER metric: register=30000ms (app scope) time_to_ready_ms=$timeToReadyMs")

        assertEquals("nav ready immediately when reconcile runs in the app scope", 0L, timeToReadyMs)
        // backgroundScope work is not drained by advanceUntilIdle; advance past the slow call.
        advanceTimeBy(30_001)
        assertEquals(1, api.registerCalls)
        assertEquals("registration completes in the background", "device-new", store.deviceId())
    }

    @Test
    fun `rapid bootstraps register the device only once`() = runTest {
        val api = SlowDeviceApi(registered = false, deviceCallDelayMs = 1_000)
        val store = FakeDeviceStore()
        val repo = DefaultBootstrapRepository(api = api, deviceStore = store, deviceReconcileScope = backgroundScope)

        repo.loadNavState()
        repo.loadNavState()
        repo.loadNavState()
        advanceTimeBy(5_000)

        assertEquals("single-flight registration", 1, api.registerCalls)
        assertEquals("device-new", store.deviceId())
    }
}
