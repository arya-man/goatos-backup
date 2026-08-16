package sg.mesha.goatos.core.data.push

import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.datastore.FakeDeviceStore
import sg.mesha.goatos.core.network.DeviceResponseDto
import sg.mesha.goatos.core.network.DeviceSummaryDto
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.HeartbeatDeviceRequestDto
import sg.mesha.goatos.core.network.RegisterDeviceRequestDto

/**
 * A phone with alerts switched off still holds a perfectly valid push token, so the push gateway
 * accepts every message for it and reports success while the OS drops it. The only way the backend
 * can tell that apart from a real delivery is if the phone says so — on register AND on every
 * heartbeat, because someone can switch alerts off in system settings long after registering.
 */
class PushMutedReportingTest {

    @Test
    fun registerReportsThatThisPhoneWillNotShowAlerts() = runTest {
        val scope = TestScope(StandardTestDispatcher(testScheduler))
        val api = RecordingApi()
        val port = DefaultNotificationsPort(
            api = api,
            deviceStore = FakeDeviceStore(),
            appScope = scope,
            appVersion = "0.1.0",
            osVersion = "Android 16",
            notificationsEnabled = { false },
        )

        // No deviceId yet: registerTokenOnce spends DEVICE_ID_WAIT_ATTEMPTS deferring behind
        // delay() before it is allowed to register without one — advance past those delays.
        port.registerToken("fcm-token")
        advanceUntilIdle()

        assertEquals(1, api.registerRequests.size)
        assertEquals(false, api.registerRequests.single().notificationsEnabled)
    }

    @Test
    fun heartbeatReportsAlertsComingBackOn() = runTest {
        val scope = TestScope(StandardTestDispatcher(testScheduler))
        val api = RecordingApi()
        val deviceStore = FakeDeviceStore()
        deviceStore.setDeviceId("device-1")
        val port = DefaultNotificationsPort(
            api = api,
            deviceStore = deviceStore,
            appScope = scope,
            appVersion = "0.1.0",
            osVersion = "Android 16",
            notificationsEnabled = { true },
        )

        port.registerToken("fcm-token")
        runCurrent()

        assertEquals(1, api.heartbeatRequests.size)
        assertEquals(true, api.heartbeatRequests.single().notificationsEnabled)
    }

    /** An app build that cannot answer must stay unknown, never assert it is reachable. */
    @Test
    fun anUnansweredSwitchIsReportedAsUnknown() = runTest {
        val scope = TestScope(StandardTestDispatcher(testScheduler))
        val api = RecordingApi()
        val port = DefaultNotificationsPort(
            api = api,
            deviceStore = FakeDeviceStore(),
            appScope = scope,
            appVersion = "0.1.0",
            osVersion = "Android 16",
        )

        port.registerToken("fcm-token")
        advanceUntilIdle()

        assertEquals(null, api.registerRequests.single().notificationsEnabled)
    }

    /** The switch is read at REPORT time, not captured once at construction: someone turning
     *  alerts off mid-session must be reported as muted on the very next report. */
    @Test
    fun theSwitchIsReadAtReportTime() = runTest {
        val scope = TestScope(StandardTestDispatcher(testScheduler))
        val api = RecordingApi()
        val deviceStore = FakeDeviceStore()
        deviceStore.setDeviceId("device-1")
        var enabled = true
        val port = DefaultNotificationsPort(
            api = api,
            deviceStore = deviceStore,
            appScope = scope,
            appVersion = "0.1.0",
            osVersion = "Android 16",
            notificationsEnabled = { enabled },
        )

        port.registerToken("token-a")
        runCurrent()
        enabled = false
        port.registerToken("token-b")
        runCurrent()

        assertEquals(listOf(true, false), api.heartbeatRequests.map { it.notificationsEnabled })
    }

    private class RecordingApi : sg.mesha.goatos.core.network.AppApi by FakeAppApi() {
        val registerRequests = mutableListOf<RegisterDeviceRequestDto>()
        val heartbeatRequests = mutableListOf<HeartbeatDeviceRequestDto>()

        override suspend fun registerDevice(request: RegisterDeviceRequestDto): DeviceResponseDto {
            registerRequests += request
            return DeviceResponseDto(device = DeviceSummaryDto(deviceId = "device-1", status = "active"))
        }

        override suspend fun heartbeatDevice(
            deviceId: String,
            request: HeartbeatDeviceRequestDto,
        ): DeviceResponseDto {
            heartbeatRequests += request
            return DeviceResponseDto(device = DeviceSummaryDto(deviceId = deviceId, status = "active"))
        }
    }
}
