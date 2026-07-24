package sg.mesha.goatos.boot

import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
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
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.data.LogoutCoordinator
import sg.mesha.goatos.core.data.ScreenCacheStore
import sg.mesha.goatos.core.data.sync.OutboxWiper
import sg.mesha.goatos.core.data.sync.SyncJobsCanceller
import sg.mesha.goatos.core.data.sync.SyncJobsScheduler
import sg.mesha.goatos.core.datastore.FakeDeviceStore
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.DeviceResponseDto
import sg.mesha.goatos.core.network.DeviceSummaryDto
import sg.mesha.goatos.core.network.FakeAppApi

@OptIn(ExperimentalCoroutinesApi::class)
class SessionViewModelAnalyticsTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private class FakeSessionStore : SessionStore {
        val tokenFlow = MutableStateFlow<String?>("existing-session")
        private val langFlow = MutableStateFlow("hi")
        override val bearerToken: Flow<String?> = tokenFlow
        override suspend fun currentToken(): String? = tokenFlow.value
        override suspend fun setBearerToken(token: String?) { tokenFlow.value = token }
        override val language: Flow<String> = langFlow
        override suspend fun currentLanguage(): String = langFlow.value
        override suspend fun setLanguage(code: String) { langFlow.value = code }
        override suspend fun clear() {
            tokenFlow.value = null
            langFlow.value = "en"
        }
    }

    private class FakeAuthRepository : AuthRepository {
        var signedOut = false
        override suspend fun signInWithEmailPassword(email: String, password: String): Result<Unit> = Result.success(Unit)
        override suspend fun signInWithGoogle(activityContext: Context): Result<Unit> = Result.success(Unit)
        override suspend fun sendPasswordReset(email: String): Result<Unit> = Result.success(Unit)
        override suspend fun currentIdToken(forceRefresh: Boolean): String? = null
        override fun signOut() { signedOut = true }
    }

    /** Records whether the deregister endpoint was hit — proves [SessionViewModel.signOut]
     *  wires into [LogoutCoordinator], not just a local token drop (C35-001). */
    private class RecordingAppApi : AppApi by FakeAppApi() {
        var deregisterCallCount = 0
            private set

        override suspend fun deregisterDevice(deviceId: String): DeviceResponseDto {
            deregisterCallCount++
            return DeviceResponseDto(device = DeviceSummaryDto(deviceId = deviceId, status = "revoked"))
        }
    }

    private fun buildLogoutCoordinator(
        api: AppApi,
        deviceStore: FakeDeviceStore,
        sessionStore: SessionStore,
        onCacheCleared: () -> Unit = {},
        onOutboxCleared: () -> Unit = {},
        onJobsCancelled: () -> Unit = {},
    ): LogoutCoordinator = LogoutCoordinator(
        api = api,
        deviceStore = deviceStore,
        sessionStore = sessionStore,
        screenCacheStore = ScreenCacheStore { onCacheCleared() },
        outboxWiper = OutboxWiper { onOutboxCleared() },
        syncJobsCanceller = SyncJobsCanceller { onJobsCancelled() },
    )

    @Test
    fun `signOut clears the session and logs sign_out`() = runTest {
        val analytics = RecordingAnalytics()
        val store = FakeSessionStore()
        val auth = FakeAuthRepository()
        val api = RecordingAppApi()
        val deviceStore = FakeDeviceStore().apply { setDeviceId("device-abc") }
        var cacheCleared = false
        var outboxCleared = false
        var jobsCancelled = false
        val logoutCoordinator = buildLogoutCoordinator(
            api = api,
            deviceStore = deviceStore,
            sessionStore = store,
            onCacheCleared = { cacheCleared = true },
            onOutboxCleared = { outboxCleared = true },
            onJobsCancelled = { jobsCancelled = true },
        )
        var relaunched = false
        val vm = SessionViewModel(
            store, auth, analytics, logoutCoordinator, SyncJobsScheduler { }, api,
            SessionRelauncher { relaunched = true },
        )

        vm.signOut()
        advanceUntilIdle()

        assertTrue("auth repository sign-out invoked", auth.signedOut)
        assertTrue("process relaunched for a clean in-memory slate", relaunched)
        assertNull("session token cleared", store.tokenFlow.value)
        assertEquals("device deregister attempted", 1, api.deregisterCallCount)
        assertTrue("Room screen caches wiped", cacheCleared)
        assertTrue("outbox wiped", outboxCleared)
        assertTrue("sync jobs cancelled", jobsCancelled)
        assertNull("device id cleared", deviceStore.deviceId())
        assertTrue(
            "sign_out event recorded",
            analytics.events.any { it.name == AnalyticsEvents.SIGN_OUT },
        )
    }
}
