package sg.mesha.goatos.core.data

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.data.sync.OutboxWiper
import sg.mesha.goatos.core.data.sync.SyncJobsCanceller
import sg.mesha.goatos.core.datastore.FakeDeviceStore
import sg.mesha.goatos.core.datastore.FakeSessionStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.DeviceResponseDto
import sg.mesha.goatos.core.network.DeviceSummaryDto
import sg.mesha.goatos.core.network.FakeAppApi

/**
 * Proof for C35-001: Android logout must be a full clean-slate wipe, not just a token drop.
 * Before this coordinator existed, `SessionViewModel.signOut()` / `ProfileViewModel.signOut()`
 * only called `authRepository.signOut()` + `sessionStore.setBearerToken(null)` — every other
 * authority-sensitive store (Room screen caches, the outbox, device identity, language, the
 * WorkManager sync jobs, and the backend device-deregister call) survived. This test fails
 * against that old behavior (none of these steps existed to invoke) and passes once
 * [LogoutCoordinator.logout] wires every one of them in the documented order.
 */
class LogoutCoordinatorTest {

    private class RecordingAppApi(
        private val delegate: AppApi = FakeAppApi(),
        private val deregisterFailure: Throwable? = null,
    ) : AppApi by delegate {
        var deregisterCallCount = 0
            private set
        var lastDeregisteredDeviceId: String? = null
            private set

        override suspend fun deregisterDevice(deviceId: String): DeviceResponseDto {
            deregisterCallCount++
            lastDeregisteredDeviceId = deviceId
            deregisterFailure?.let { throw it }
            return DeviceResponseDto(device = DeviceSummaryDto(deviceId = deviceId, status = "revoked"))
        }
    }

    private suspend fun buildFixture(
        api: AppApi = RecordingAppApi(),
        existingDeviceId: String? = "device-123",
        existingToken: String? = "existing-token",
        existingLanguage: String = "hi",
    ): Fixture {
        val deviceStore = FakeDeviceStore().apply { existingDeviceId?.let { setDeviceId(it) } }
        val sessionStore = FakeSessionStore().apply {
            existingToken?.let { setBearerToken(it) }
            setLanguage(existingLanguage)
        }
        var screenCacheCleared = false
        var outboxCleared = false
        var jobsCancelled = false
        val coordinator = LogoutCoordinator(
            api = api,
            deviceStore = deviceStore,
            sessionStore = sessionStore,
            screenCacheStore = ScreenCacheStore { screenCacheCleared = true },
            outboxWiper = OutboxWiper { outboxCleared = true },
            syncJobsCanceller = SyncJobsCanceller { jobsCancelled = true },
        )
        return Fixture(
            coordinator = coordinator,
            deviceStore = deviceStore,
            sessionStore = sessionStore,
            screenCacheCleared = { screenCacheCleared },
            outboxCleared = { outboxCleared },
            jobsCancelled = { jobsCancelled },
        )
    }

    private data class Fixture(
        val coordinator: LogoutCoordinator,
        val deviceStore: FakeDeviceStore,
        val sessionStore: FakeSessionStore,
        val screenCacheCleared: () -> Boolean,
        val outboxCleared: () -> Boolean,
        val jobsCancelled: () -> Boolean,
    )

    @Test
    fun `logout wipes every authority-sensitive local store and deregisters the device`() = runTest {
        val api = RecordingAppApi()
        val fixture = buildFixture(api = api)
        var vendorSignedOut = false

        fixture.coordinator.logout(signOutVendorAuth = { vendorSignedOut = true })

        assertEquals("device deregister attempted exactly once", 1, api.deregisterCallCount)
        assertEquals("device-123", api.lastDeregisteredDeviceId)
        assertTrue("vendor auth sign-out invoked", vendorSignedOut)
        assertTrue("every Room screen cache table wiped", fixture.screenCacheCleared())
        assertTrue("outbox wiped", fixture.outboxCleared())
        assertTrue("WorkManager sync/retry jobs cancelled", fixture.jobsCancelled())
        assertNull("session bearer token cleared", fixture.sessionStore.currentToken())
        assertEquals("language reset to default", "en", fixture.sessionStore.currentLanguage())
        assertNull("device id cleared", fixture.deviceStore.deviceId())
    }

    @Test
    fun `logout still wipes local state when the backend deregister call fails`() = runTest {
        val api = RecordingAppApi(deregisterFailure = java.io.IOException("offline"))
        val fixture = buildFixture(api = api)
        var vendorSignedOut = false

        fixture.coordinator.logout(signOutVendorAuth = { vendorSignedOut = true })

        assertEquals("deregister was still attempted", 1, api.deregisterCallCount)
        assertTrue("a failed remote call never blocks vendor sign-out", vendorSignedOut)
        assertTrue("a failed remote call never blocks the cache wipe", fixture.screenCacheCleared())
        assertTrue("a failed remote call never blocks the outbox wipe", fixture.outboxCleared())
        assertTrue("a failed remote call never blocks job cancellation", fixture.jobsCancelled())
        assertNull("a failed remote call never blocks the session clear", fixture.sessionStore.currentToken())
        assertNull("a failed remote call never blocks the device clear", fixture.deviceStore.deviceId())
    }

    @Test
    fun `logout skips the deregister call when no device is registered yet`() = runTest {
        val api = RecordingAppApi()
        val fixture = buildFixture(api = api, existingDeviceId = null)

        fixture.coordinator.logout(signOutVendorAuth = {})

        assertEquals("nothing to deregister", 0, api.deregisterCallCount)
        assertTrue("local wipe still runs with no device registered", fixture.screenCacheCleared())
        assertTrue(fixture.outboxCleared())
        assertTrue(fixture.jobsCancelled())
    }

    @Test
    fun `deregister runs before vendor auth signs out, so the request still authenticates as the departing user`() = runTest {
        val steps = mutableListOf<String>()
        val api = object : AppApi by FakeAppApi() {
            override suspend fun deregisterDevice(deviceId: String): DeviceResponseDto {
                steps += "deregister"
                return DeviceResponseDto(device = DeviceSummaryDto(deviceId = deviceId, status = "revoked"))
            }
        }
        val fixture = buildFixture(api = api)

        fixture.coordinator.logout(signOutVendorAuth = { steps += "vendor_sign_out" })

        assertEquals(listOf("deregister", "vendor_sign_out"), steps)
    }
}
