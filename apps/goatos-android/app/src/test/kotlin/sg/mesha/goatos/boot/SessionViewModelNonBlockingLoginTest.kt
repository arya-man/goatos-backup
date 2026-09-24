package sg.mesha.goatos.boot

import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withContext
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.core.data.LogoutCoordinator
import sg.mesha.goatos.core.data.ScreenCacheStore
import sg.mesha.goatos.core.data.sync.OutboxWiper
import sg.mesha.goatos.core.data.sync.SyncJobsCanceller
import sg.mesha.goatos.core.data.sync.SyncJobsScheduler
import sg.mesha.goatos.core.datastore.FakeDeviceStore
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.AuthSessionEventRequestDto
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.feature.auth.LoginError
import java.net.UnknownHostException

/**
 * Field incident (app 1.0.40): Firebase email/password sign-in succeeded during a brief DNS drop,
 * but the Goat OS session-event POST failed with UnknownHostException. The old code showed the
 * raw exception text ("Unable to resolve host ...") as LoginError.UNKNOWN and returned BEFORE
 * persisting the session marker, so the operator could not log in at all.
 *
 * Firebase auth is the credential check; the session-event is an audit/claim side effect and
 * must never block or fail login.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SessionViewModelNonBlockingLoginTest {

    private val dispatcher by lazy { StandardTestDispatcher() }

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    /** Records the virtual time the session marker was persisted (= user is logged in). */
    private class TimedSessionStore(private val clock: () -> Long) : SessionStore {
        val tokenFlow = MutableStateFlow<String?>(null)
        var loggedInAtMs: Long? = null
        private val langFlow = MutableStateFlow("en")
        override val bearerToken: Flow<String?> = tokenFlow
        override suspend fun currentToken(): String? = tokenFlow.value
        override suspend fun setBearerToken(token: String?) {
            tokenFlow.value = token
            if (token != null && loggedInAtMs == null) loggedInAtMs = clock()
        }
        override val language: Flow<String> = langFlow
        override suspend fun currentLanguage(): String = langFlow.value
        override suspend fun setLanguage(code: String) { langFlow.value = code }
        override suspend fun clear() { tokenFlow.value = null }
    }

    private class OkFirebase : AuthRepository {
        override suspend fun signInWithEmailPassword(email: String, password: String): Result<Unit> = Result.success(Unit)
        override suspend fun signInWithGoogle(activityContext: Context): Result<Unit> = Result.success(Unit)
        override suspend fun sendPasswordReset(email: String): Result<Unit> = Result.success(Unit)
        override suspend fun currentIdToken(forceRefresh: Boolean): String? = "firebase-id-token"
        override fun currentEmail(): String? = "operator@mesha.sg"
        override fun currentFirebaseUid(): String? = "uid-1"
        override fun signOut() = Unit
    }

    /** Session-event endpoint that fails like the field phone did, or hangs. */
    private class SessionEventApi(
        private val failWith: Throwable? = null,
        private val delayMs: Long = 0,
        private val failuresBeforeSuccess: Int = Int.MAX_VALUE,
    ) : AppApi by FakeAppApi() {
        var attempts = 0
        var delivered = 0
        override suspend fun recordAuthSessionEvent(request: AuthSessionEventRequestDto) {
            attempts++
            if (delayMs > 0) delay(delayMs)
            if (failWith != null && attempts <= failuresBeforeSuccess) throw failWith
            delivered++
        }
    }

    private fun TestScope.buildVm(store: SessionStore, api: AppApi): SessionViewModel =
        SessionViewModel(
            store,
            OkFirebase(),
            RecordingAnalytics(),
            LogoutCoordinator(
                api = api,
                deviceStore = FakeDeviceStore(),
                sessionStore = store,
                screenCacheStore = ScreenCacheStore { },
                outboxWiper = OutboxWiper { },
                syncJobsCanceller = SyncJobsCanceller { },
            ),
            SyncJobsScheduler { },
            api,
            SessionRelauncher { },
        )

    private fun assumeFirebaseFlavor() =
        assumeTrue("Firebase session path is only active outside the dev-bearer flavor", BuildConfig.FLAVOR != "dev")

    @Test
    fun `DNS failure on session-events still yields a logged-in session`() = runTest {
        assumeFirebaseFlavor()
        val store = TimedSessionStore { testScheduler.currentTime }
        val api = SessionEventApi(failWith = UnknownHostException("Unable to resolve host \"api.goatos.mesha.sg\": No address associated with hostname"))
        val vm = buildVm(store, api)

        val start = testScheduler.currentTime
        vm.signInWithEmail("operator@mesha.sg", "secret")
        runCurrent()
        withContext(Dispatchers.IO) { }
        runCurrent()

        println("BEFORE/AFTER metric: session-event=UnknownHostException logged_in=${store.loggedInAtMs != null} time_to_logged_in_ms=${store.loggedInAtMs?.minus(start)}")
        assertEquals("session opened despite the session-event DNS failure", FIREBASE_SESSION_MARKER, store.tokenFlow.value)
        assertEquals("opened immediately, not after a network timeout", start, store.loggedInAtMs)
        val ui = vm.uiState.value
        assertNull("no login error for a background audit failure", ui.errorReason)
        assertFalse(ui.isLoading)
        advanceUntilIdle()
        assertTrue("session-event is still attempted (with retry) in the background", api.attempts >= 2)
    }

    @Test
    fun `slow session-events does not delay login`() = runTest {
        assumeFirebaseFlavor()
        val store = TimedSessionStore { testScheduler.currentTime }
        val api = SessionEventApi(delayMs = 30_000)
        val vm = buildVm(store, api)

        val start = testScheduler.currentTime
        vm.signInWithEmail("operator@mesha.sg", "secret")
        runCurrent()
        withContext(Dispatchers.IO) { }
        runCurrent()
        advanceUntilIdle()

        val elapsed = store.loggedInAtMs?.minus(start)
        println("BEFORE/AFTER metric: session-event=30000ms delay time_to_logged_in_ms=$elapsed")
        assertEquals(FIREBASE_SESSION_MARKER, store.tokenFlow.value)
        assertEquals("login must not wait on the 30s session-event", 0L, elapsed)
        assertEquals("session-event delivered in the background", 1, api.delivered)
    }

    @Test
    fun `a transient session-event failure is retried until delivered`() = runTest {
        assumeFirebaseFlavor()
        val store = TimedSessionStore { testScheduler.currentTime }
        val api = SessionEventApi(failWith = UnknownHostException("Unable to resolve host"), failuresBeforeSuccess = 2)
        val vm = buildVm(store, api)

        vm.signInWithEmail("operator@mesha.sg", "secret")
        runCurrent()
        withContext(Dispatchers.IO) { }
        advanceUntilIdle()

        assertEquals(FIREBASE_SESSION_MARKER, store.tokenFlow.value)
        assertEquals("delivered on the third attempt", 1, api.delivered)
        assertEquals(3, api.attempts)
    }

    @Test
    fun `raw exception text never reaches the login screen`() = runTest {
        assumeFirebaseFlavor()
        val store = TimedSessionStore { testScheduler.currentTime }
        val api = SessionEventApi(failWith = UnknownHostException("Unable to resolve host \"api.goatos.mesha.sg\""))
        val vm = buildVm(store, api)

        vm.signInWithEmail("operator@mesha.sg", "secret")
        runCurrent()
        withContext(Dispatchers.IO) { }
        advanceUntilIdle()

        val ui = vm.uiState.value
        assertNotEquals("never the UNKNOWN raw-text path", LoginError.UNKNOWN, ui.errorReason)
        assertFalse(
            "raw exception text must never be shown: ${ui.errorDetail}",
            ui.errorDetail.orEmpty().contains("Unable to resolve host"),
        )
    }

    @Test
    fun `UnknownHostException maps to the NETWORK message, even when wrapped`() {
        val direct = classifyAuthError(UnknownHostException("Unable to resolve host \"api.goatos.mesha.sg\""))
        assertEquals(LoginError.NETWORK, direct.first)
        assertNull("NETWORK renders the localized string, never exception text", direct.second)

        val wrapped = classifyAuthError(RuntimeException("sign-in bridge failed", UnknownHostException("Unable to resolve host")))
        assertEquals(LoginError.NETWORK, wrapped.first)
        assertNull(wrapped.second)
    }
}
