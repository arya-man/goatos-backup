package sg.mesha.goatos.core.data.push

import java.io.IOException
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.datastore.FakeDeviceStore
import sg.mesha.goatos.core.network.DeviceResponseDto
import sg.mesha.goatos.core.network.DeviceSummaryDto
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.HeartbeatDeviceRequestDto
import sg.mesha.goatos.core.network.RegisterDeviceRequestDto

class DefaultNotificationsPortTest {

    @Test
    fun registerTokenRetriesWhenFirstAttemptRunsBeforeAuthIsReady() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val scope = TestScope(dispatcher)
        val api = RecordingAppApi(failFirstRegister = true)
        val deviceStore = FakeDeviceStore()
        val port = DefaultNotificationsPort(
            api = api,
            deviceStore = deviceStore,
            appScope = scope,
            appVersion = "0.1.0-stg",
            osVersion = "Android 16",
        )

        port.registerToken("fcm-token-1")

        runCurrent()
        assertEquals(1, api.registerRequests.size)
        assertNull(deviceStore.deviceId())

        advanceTimeBy(5_000)
        runCurrent()

        assertEquals(2, api.registerRequests.size)
        assertEquals("fcm-token-1", api.registerRequests.last().fcmToken)
        assertEquals("device-after-retry", deviceStore.deviceId())
    }

    @Test
    fun registerTokenHeartbeatsExistingDeviceWithRawFcmToken() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val scope = TestScope(dispatcher)
        val api = RecordingAppApi()
        val deviceStore = FakeDeviceStore()
        deviceStore.setDeviceId("device-existing")
        val port = DefaultNotificationsPort(
            api = api,
            deviceStore = deviceStore,
            appScope = scope,
            appVersion = "0.1.0-stg",
            osVersion = "Android 16",
        )

        port.registerToken("fcm-token-2")

        runCurrent()

        assertEquals(emptyList<RegisterDeviceRequestDto>(), api.registerRequests)
        assertEquals(listOf("device-existing" to "fcm-token-2"), api.heartbeatRequests)
    }

    @Test
    fun blankTokenDoesNotRegisterOrHeartbeat() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val scope = TestScope(dispatcher)
        val api = RecordingAppApi()
        val deviceStore = FakeDeviceStore()
        val port = DefaultNotificationsPort(
            api = api,
            deviceStore = deviceStore,
            appScope = scope,
            appVersion = "0.1.0-stg",
            osVersion = "Android 16",
        )

        port.registerToken("   ")
        runCurrent()

        assertEquals(emptyList<RegisterDeviceRequestDto>(), api.registerRequests)
        assertEquals(emptyList<Pair<String, String?>>(), api.heartbeatRequests)
    }

    private class RecordingAppApi(
        private val failFirstRegister: Boolean = false,
    ) : sg.mesha.goatos.core.network.AppApi by FakeAppApi() {
        val registerRequests = mutableListOf<RegisterDeviceRequestDto>()
        val heartbeatRequests = mutableListOf<Pair<String, String?>>()

        override suspend fun registerDevice(request: RegisterDeviceRequestDto): DeviceResponseDto {
            registerRequests += request
            if (failFirstRegister && registerRequests.size == 1) {
                throw IOException("auth/session not ready")
            }
            return DeviceResponseDto(
                device = DeviceSummaryDto(
                    deviceId = if (failFirstRegister) "device-after-retry" else "device-first",
                    appInstallId = request.appInstallId,
                    status = "active",
                ),
            )
        }

        override suspend fun heartbeatDevice(
            deviceId: String,
            request: HeartbeatDeviceRequestDto,
        ): DeviceResponseDto {
            heartbeatRequests += deviceId to request.fcmToken
            return DeviceResponseDto(
                device = DeviceSummaryDto(
                    deviceId = deviceId,
                    status = "active",
                ),
            )
        }
    }
}
