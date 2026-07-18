package sg.mesha.goatos.viewmodel

import android.content.Context
import android.view.KeyEvent
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.LogoutCoordinator
import sg.mesha.goatos.core.data.ScreenCacheStore
import sg.mesha.goatos.core.data.sync.OutboxWiper
import sg.mesha.goatos.core.data.sync.SyncJobsCanceller
import sg.mesha.goatos.core.datastore.FakeDeviceStore
import sg.mesha.goatos.core.datastore.FakeSessionStore
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.DeviceResponseDto
import sg.mesha.goatos.core.network.DeviceSummaryDto
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.rfid.RfidRead
import sg.mesha.goatos.rfid.RfidReaderDevice
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus

/**
 * Proof for C35-001: the You/Settings sign-out entry point ([ProfileViewModel.signOut]) must
 * share the SAME full clean-slate [LogoutCoordinator] path as the session gate's sign-out
 * ([sg.mesha.goatos.boot.SessionViewModel.signOut]) — not just drop the local bearer token.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ProfileViewModelLogoutTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private class FakeBootstrapRepository : BootstrapRepository {
        override suspend fun loadNavState(): NavState = NavState(NavChrome.MINIMAL, emptyList())
        override suspend fun operatorProfile(): BootstrapOperatorProfileDto? =
            BootstrapOperatorProfileDto(displayName = "Test Operator")
    }

    private class FakeAuthRepository : AuthRepository {
        var signedOut = false
        override suspend fun signInWithEmailPassword(email: String, password: String): Result<Unit> = Result.success(Unit)
        override suspend fun signInWithGoogle(activityContext: Context): Result<Unit> = Result.success(Unit)
        override suspend fun sendPasswordReset(email: String): Result<Unit> = Result.success(Unit)
        override suspend fun currentIdToken(forceRefresh: Boolean): String? = null
        override fun signOut() { signedOut = true }
    }

    private class FakeRfidReaderPort : RfidReaderPort {
        override val status: StateFlow<RfidReaderStatus> = MutableStateFlow(RfidReaderStatus.NOT_PAIRED)
        override val readerName: StateFlow<String?> = MutableStateFlow(null)
        override val devices: StateFlow<List<RfidReaderDevice>> = MutableStateFlow(emptyList())
        override val reads: SharedFlow<RfidRead> = MutableSharedFlow()
        override fun refreshStatus() {}
        override fun openSystemPairing() {}
        override fun setCaptureEnabled(enabled: Boolean) {}
        override fun onKeyEvent(event: KeyEvent): Boolean = false
    }

    private class RecordingAppApi : AppApi by FakeAppApi() {
        var deregisterCallCount = 0
            private set

        override suspend fun deregisterDevice(deviceId: String): DeviceResponseDto {
            deregisterCallCount++
            return DeviceResponseDto(device = DeviceSummaryDto(deviceId = deviceId, status = "revoked"))
        }
    }

    @Test
    fun `signOut delegates to the shared LogoutCoordinator and wipes local state`() = runTest {
        val auth = FakeAuthRepository()
        val api = RecordingAppApi()
        val deviceStore = FakeDeviceStore().apply { setDeviceId("device-xyz") }
        val sessionStore = FakeSessionStore().apply { setBearerToken("existing-token") }
        var cacheCleared = false
        var outboxCleared = false
        var jobsCancelled = false
        val logoutCoordinator = LogoutCoordinator(
            api = api,
            deviceStore = deviceStore,
            sessionStore = sessionStore,
            screenCacheStore = ScreenCacheStore { cacheCleared = true },
            outboxWiper = OutboxWiper { outboxCleared = true },
            syncJobsCanceller = SyncJobsCanceller { jobsCancelled = true },
        )
        val vm = ProfileViewModel(
            bootstrap = FakeBootstrapRepository(),
            authRepository = auth,
            reader = FakeRfidReaderPort(),
            logoutCoordinator = logoutCoordinator,
        )

        vm.signOut()
        advanceUntilIdle()

        assertTrue("vendor auth sign-out invoked", auth.signedOut)
        assertEquals("device deregister attempted", 1, api.deregisterCallCount)
        assertTrue("Room screen caches wiped", cacheCleared)
        assertTrue("outbox wiped", outboxCleared)
        assertTrue("sync jobs cancelled", jobsCancelled)
        assertNull("session token cleared", sessionStore.currentToken())
        assertNull("device id cleared", deviceStore.deviceId())
    }
}
