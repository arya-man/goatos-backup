package sg.mesha.goatos.boot

import android.content.Context
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
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
import org.junit.Assert.assertNotNull
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
        var signedOut = false
        override fun signOut() { signedOut = true }
    }

    /** Session-event endpoint that fails like the field phone did, or hangs. */
    private class SessionEventApi(
        private val failWith: Throwable? = null,
        private val delayMs: Long = 0,
        private val failuresBeforeSuccess: Int = Int.MAX_VALUE,
    ) : AppApi by FakeAppApi() {
        var attempts = 0
        var delivered = 0
        val attemptTimes = mutableListOf<Long>()
        var clock: () -> Long = { 0L }
        override suspend fun recordAuthSessionEvent(request: AuthSessionEventRequestDto) {
            attempts++
            attemptTimes += clock()
            if (delayMs > 0) delay(delayMs)
            if (failWith != null && attempts <= failuresBeforeSuccess) throw failWith
            delivered++
        }
    }

    private fun TestScope.buildVm(store: SessionStore, api: AppApi, auth: OkFirebase = OkFirebase()): SessionViewModel =
        SessionViewModel(
            store,
            auth,
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
        ).also { it.ioDispatcher = StandardTestDispatcher(testScheduler) }

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

    private fun httpError(code: Int, retryAfter: String? = null): retrofit2.HttpException {
        val raw = okhttp3.Response.Builder()
            .request(okhttp3.Request.Builder().url("https://api.goatos.mesha.sg/auth/session-events").build())
            .protocol(okhttp3.Protocol.HTTP_1_1)
            .code(code)
            .message("HTTP $code")
            .apply { if (retryAfter != null) header("Retry-After", retryAfter) }
            .build()
        val body = okhttp3.ResponseBody.create(null, "{}")
        return retrofit2.HttpException(retrofit2.Response.error<Any>(body, raw))
    }

    private suspend fun TestScope.signInAndSettle(vm: SessionViewModel) {
        vm.signInWithEmail("operator@mesha.sg", "secret")
        runCurrent()
        advanceUntilIdle()
    }

    @Test
    fun `session-events 403 (email not allowed) signs the user out with no session marker`() = runTest {
        assumeFirebaseFlavor()
        val store = TimedSessionStore { testScheduler.currentTime }
        val auth = OkFirebase()
        val api = SessionEventApi(failWith = httpError(403))
        val vm = buildVm(store, api, auth)

        signInAndSettle(vm)

        assertNull("a denied principal keeps no session marker", store.tokenFlow.value)
        assertTrue("Firebase is signed out", auth.signedOut)
        assertEquals("a translatable login error, not UNKNOWN", "WORKSPACE_UNAVAILABLE", vm.uiState.value.errorReason?.name)
        assertNull("no hard-coded English detail", vm.uiState.value.errorDetail)
        assertEquals("a denial is never retried", 1, api.attempts)
    }

    @Test
    fun `4xx client errors and serialization errors are never retried`() = runTest {
        assumeFirebaseFlavor()
        for (failure in listOf<Throwable>(
            httpError(400), httpError(404), httpError(409), httpError(422),
            kotlinx.serialization.SerializationException("bad body"),
        )) {
            val store = TimedSessionStore { testScheduler.currentTime }
            val api = SessionEventApi(failWith = failure)
            signInAndSettle(buildVm(store, api))
            assertEquals("no retry for $failure", 1, api.attempts)
            assertEquals("a non-denial failure keeps the session", FIREBASE_SESSION_MARKER, store.tokenFlow.value)
        }
    }

    @Test
    fun `503 with Retry-After is retried no sooner than the server asked`() = runTest {
        assumeFirebaseFlavor()
        val store = TimedSessionStore { testScheduler.currentTime }
        val api = SessionEventApi(failWith = httpError(503, retryAfter = "20"), failuresBeforeSuccess = 1)
        api.clock = { testScheduler.currentTime }
        signInAndSettle(buildVm(store, api))

        assertEquals(2, api.attempts)
        val gap = api.attemptTimes[1] - api.attemptTimes[0]
        assertTrue("second attempt honours Retry-After=20s, gap=${gap}ms", gap in 20_000L..25_000L)
        assertEquals(1, api.delivered)
    }

    @Test
    fun `logout cancels an in-flight session-event retry so it never posts for the next user`() = runTest {
        assumeFirebaseFlavor()
        val store = TimedSessionStore { testScheduler.currentTime }
        val api = SessionEventApi(failWith = java.io.IOException("offline"))
        val sender = AuthSessionEventSender(api, backgroundScope)
        val vm = SessionViewModel(
            store, OkFirebase(), RecordingAnalytics(),
            LogoutCoordinator(
                api = api,
                deviceStore = FakeDeviceStore(),
                sessionStore = store,
                screenCacheStore = ScreenCacheStore { },
                outboxWiper = OutboxWiper { },
                syncJobsCanceller = SyncJobsCanceller { },
                cancelPendingSessionEvents = sender::cancel,
            ),
            SyncJobsScheduler { }, api, SessionRelauncher { }, sender,
        ).also { it.ioDispatcher = StandardTestDispatcher(testScheduler) }
        vm.signInWithEmail("operator@mesha.sg", "secret")
        runCurrent()
        runCurrent()
        val attemptsBeforeLogout = api.attempts
        assertTrue("an attempt was made", attemptsBeforeLogout >= 1)

        vm.signOut()
        runCurrent()
        advanceTimeBy(600_000)

        assertEquals("no retry after logout", attemptsBeforeLogout, api.attempts)
    }

    @Test
    fun `a denial that arrives after the login screen is gone still signs the user out`() = runTest {
        assumeFirebaseFlavor()
        val store = TimedSessionStore { testScheduler.currentTime }
        val auth = OkFirebase()
        val api = SessionEventApi(failWith = httpError(403), delayMs = 5_000)
        val vm = buildVm(store, api, auth)

        vm.signInWithEmail("operator@mesha.sg", "secret")
        runCurrent()
        runCurrent()
        assertEquals("session open while the event is in flight", FIREBASE_SESSION_MARKER, store.tokenFlow.value)

        // The Activity finishes: the ViewModel scope dies before the 403 lands.
        vm.viewModelScope.cancel()
        advanceUntilIdle()

        assertNull("the denial is still applied", store.tokenFlow.value)
        assertTrue(auth.signedOut)
    }

    /** Only observeStatus() is used by the switch-account confirm; everything else fails loudly. */
    /**
     * [snapshotPending] is what the in-memory observeStatus() snapshot says; [storedPending] is
     * what the outbox actually holds (the direct store query). After a cold start the snapshot
     * reads 0 until Room first emits, so the two can disagree.
     */
    private fun syncRepositoryWithPending(
        snapshotPending: Int,
        storedPending: Int = snapshotPending,
    ): sg.mesha.goatos.core.data.sync.SyncRepository {
        val status = kotlinx.coroutines.flow.MutableStateFlow(
            sg.mesha.goatos.core.data.sync.SyncStatus.empty(online = false).copy(pendingCount = snapshotPending),
        )
        return java.lang.reflect.Proxy.newProxyInstance(
            javaClass.classLoader,
            arrayOf(sg.mesha.goatos.core.data.sync.SyncRepository::class.java),
        ) { _, method, _ ->
            when (method.name) {
                "observeStatus" -> status
                "unsyncedCountNow" -> storedPending
                else -> error("unexpected ${method.name}")
            }
        } as sg.mesha.goatos.core.data.sync.SyncRepository
    }

    private suspend fun TestScope.buildSwitchVm(
        store: SessionStore,
        auth: OkFirebase,
        pending: Int,
        storedPending: Int = pending,
    ): SessionViewModel {
        val api = SessionEventApi()
        return SessionViewModel(
            store, auth, RecordingAnalytics(),
            LogoutCoordinator(
                api = api,
                deviceStore = FakeDeviceStore(),
                sessionStore = store,
                screenCacheStore = ScreenCacheStore { },
                outboxWiper = OutboxWiper { },
                syncJobsCanceller = SyncJobsCanceller { },
            ),
            SyncJobsScheduler { }, api, SessionRelauncher { },
            AuthSessionEventSender(api, backgroundScope),
            syncRepositoryWithPending(pending, storedPending),
        ).also {
            it.ioDispatcher = StandardTestDispatcher(testScheduler)
            // Let the flavor's cold-start session bring-up finish BEFORE the switch is exercised.
            // In the dev flavor with a blank baked DEV_BEARER_TOKEN (land-main / CI builds), that
            // bring-up correctly wipes a non-dev marker (logout + vendor signOut), which used to
            // race the switch and look like "wiped before confirm". The account-switch confirm is
            // flavor-independent, so re-open a signed-in session after startup has settled.
            advanceUntilIdle()
            if (store.currentToken().isNullOrBlank()) store.setBearerToken(FIREBASE_SESSION_MARKER)
            auth.signedOut = false
        }
    }

    private fun activeSessionTokenForFlavor(): String =
        BuildConfig.DEV_BEARER_TOKEN.takeIf {
            authModeForFlavor(BuildConfig.FLAVOR) == AuthMode.DEV_BEARER && it.isNotBlank()
        } ?: FIREBASE_SESSION_MARKER

    @Test
    fun `switching account with unsynced work asks first and does not wipe until confirmed`() = runTest {
        val store = TimedSessionStore { testScheduler.currentTime }.apply { tokenFlow.value = activeSessionTokenForFlavor() }
        val auth = OkFirebase()
        val vm = buildSwitchVm(store, auth, pending = 3)

        vm.requestSignInWithAnotherAccount()
        advanceUntilIdle()
        assertEquals("confirm shows the unsynced count", 3, vm.uiState.value.switchAccountPendingCount)
        assertNotNull("session still present; dev startup may replace the Firebase marker with the baked bearer", store.tokenFlow.value)
        assertFalse(auth.signedOut)

        vm.dismissSignInWithAnotherAccount()
        advanceUntilIdle()
        assertNull(vm.uiState.value.switchAccountPendingCount)
        assertNotNull("cancel keeps the session", store.tokenFlow.value)
        assertFalse(auth.signedOut)

        vm.requestSignInWithAnotherAccount()
        vm.confirmSignInWithAnotherAccount()
        advanceUntilIdle()
        assertNull("confirmed switch signs out", store.tokenFlow.value)
        assertTrue(auth.signedOut)
    }

    @Test
    fun `switching account with nothing unsynced signs out straight away`() = runTest {
        val store = TimedSessionStore { testScheduler.currentTime }.apply { tokenFlow.value = activeSessionTokenForFlavor() }
        val auth = OkFirebase()
        val vm = buildSwitchVm(store, auth, pending = 0)

        vm.requestSignInWithAnotherAccount()
        advanceUntilIdle()

        assertNull(vm.uiState.value.switchAccountPendingCount)
        assertNull(store.tokenFlow.value)
        assertTrue(auth.signedOut)
    }

    @Test
    fun `cold start - snapshot still 0 but the outbox holds 3 - the confirm is still shown`() = runTest {
        val store = TimedSessionStore { testScheduler.currentTime }.apply { tokenFlow.value = activeSessionTokenForFlavor() }
        val auth = OkFirebase()
        val vm = buildSwitchVm(store, auth, pending = 0, storedPending = 3)

        vm.requestSignInWithAnotherAccount()
        advanceUntilIdle()

        assertEquals("count comes from the store, not the stale snapshot", 3, vm.uiState.value.switchAccountPendingCount)
        assertNotNull("session still present; dev startup may replace the Firebase marker with the baked bearer", store.tokenFlow.value)
        assertFalse(auth.signedOut)
    }

    private fun TestScope.buildDeniedVm(
        store: SessionStore,
        auth: OkFirebase,
        api: SessionEventApi,
        unsynced: Int,
    ): SessionViewModel = SessionViewModel(
        store, auth, RecordingAnalytics(),
        LogoutCoordinator(
            api = api,
            deviceStore = FakeDeviceStore(),
            sessionStore = store,
            screenCacheStore = ScreenCacheStore { },
            outboxWiper = OutboxWiper { },
            syncJobsCanceller = SyncJobsCanceller { },
        ),
        SyncJobsScheduler { }, api, SessionRelauncher { },
        AuthSessionEventSender(api, backgroundScope, listOf(1_000L, 1_000L)).also { lastSender = it },
        syncRepositoryWithPending(unsynced, unsynced),
    ).also { it.ioDispatcher = StandardTestDispatcher(testScheduler) }

    private var lastSender: AuthSessionEventSender? = null
    private fun sender(@Suppress("UNUSED_PARAMETER") vm: SessionViewModel) = lastSender!!

    @Test
    fun `403 with unsynced work keeps the session instead of wiping the outbox`() = runTest {
        assumeFirebaseFlavor()
        val store = TimedSessionStore { testScheduler.currentTime }
        val auth = OkFirebase()
        val api = SessionEventApi(failWith = httpError(403))
        val vm = buildDeniedVm(store, auth, api, unsynced = 3)

        vm.signInWithEmail("operator@mesha.sg", "secret")
        advanceUntilIdle()

        assertEquals("session kept so the unsynced work survives", FIREBASE_SESSION_MARKER, store.tokenFlow.value)
        assertFalse("Firebase not signed out", auth.signedOut)
        assertTrue("shell is routed to the no-access screen", sender(vm).accessDenied.value)
    }

    @Test
    fun `401 on session-events is a token problem, not a denial - retried, never signed out`() = runTest {
        assumeFirebaseFlavor()
        val store = TimedSessionStore { testScheduler.currentTime }
        val auth = OkFirebase()
        val api = SessionEventApi(failWith = httpError(401))
        val vm = buildDeniedVm(store, auth, api, unsynced = 0)

        vm.signInWithEmail("operator@mesha.sg", "secret")
        advanceUntilIdle()

        assertEquals(FIREBASE_SESSION_MARKER, store.tokenFlow.value)
        assertFalse(auth.signedOut)
        assertTrue("401 is retried", api.attempts > 1)
    }
}
