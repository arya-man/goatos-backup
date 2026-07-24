package sg.mesha.goatos.viewmodel

import android.content.Context
import android.view.KeyEvent
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.FlowCollector
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
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
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.rfid.RfidRead
import sg.mesha.goatos.rfid.RfidReaderDevice
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus

/**
 * MOB-010 guardrail: proves [ProfileViewModel.state] is exposed with
 * `stateIn(SharingStarted.WhileSubscribed(5_000))` over a combine that includes
 * [RfidReaderPort.status] as the SOLE subscriber path, so the RFID hardware status stream is
 * collected ONLY while the UI is subscribed to [ProfileViewModel.state] — not forever.
 *
 * The prior forever-`launch { ... stateIn(WhileSubscribed).collect { } }` inside `init` kept the
 * RFID hardware stream alive permanently (that forever `collect` was itself the sole subscriber
 * of the inner `stateIn`, so it never saw zero subscribers). WhileSubscribed(5_000) on the
 * EXPOSED [ProfileViewModel.state] stops the upstream collection ~5s after the last subscriber
 * leaves and restarts it on return — mirroring [AlertsViewModelWhileSubscribedTest].
 *
 * Uses [UnconfinedTestDispatcher] so a launched collector subscribes eagerly/synchronously,
 * while `advanceTimeBy` still drives the WhileSubscribed stop timeout.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ProfileViewModelWhileSubscribedTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `RFID reader status is collected only while state has subscribers`() = runTest(dispatcher) {
        val reader = CountingRfidReaderPort()
        val viewModel = ProfileViewModel(
            bootstrap = FakeBootstrapRepository(),
            authRepository = FakeAuthRepository(),
            reader = reader,
            logoutCoordinator = buildLogoutCoordinator(),
            relauncher = {},
        )

        // No UI subscriber yet -> WhileSubscribed keeps the upstream cold.
        assertEquals(0, reader.activeStatusCollectors)

        // Subscribe (simulates the screen collecting state) — Unconfined subscribes synchronously.
        val job1 = launch { viewModel.state.collect {} }
        assertEquals(1, reader.activeStatusCollectors)

        // Unsubscribe (screen backgrounded). Within the 5s window it stays warm...
        job1.cancel()
        assertEquals(1, reader.activeStatusCollectors)

        // ...but after the 5s WhileSubscribed timeout it stops — no forever collection.
        advanceTimeBy(6_000)
        assertEquals(0, reader.activeStatusCollectors)

        // Returning to the screen restarts the upstream collection.
        val job2 = launch { viewModel.state.collect {} }
        assertEquals(1, reader.activeStatusCollectors)

        job2.cancel()
    }

    private fun buildLogoutCoordinator(): LogoutCoordinator = LogoutCoordinator(
        api = FakeAppApi(),
        deviceStore = FakeDeviceStore(),
        sessionStore = FakeSessionStore(),
        screenCacheStore = ScreenCacheStore {},
        outboxWiper = OutboxWiper {},
        syncJobsCanceller = SyncJobsCanceller {},
    )

    private class FakeBootstrapRepository : BootstrapRepository {
        override suspend fun loadNavState(): NavState = NavState(NavChrome.MINIMAL, emptyList())
        override suspend fun operatorProfile(): BootstrapOperatorProfileDto? =
            BootstrapOperatorProfileDto(displayName = "Test Operator")
    }

    private class FakeAuthRepository : AuthRepository {
        override suspend fun signInWithEmailPassword(email: String, password: String): Result<Unit> = Result.success(Unit)
        override suspend fun signInWithGoogle(activityContext: Context): Result<Unit> = Result.success(Unit)
        override suspend fun sendPasswordReset(email: String): Result<Unit> = Result.success(Unit)
        override suspend fun currentIdToken(forceRefresh: Boolean): String? = null
        override fun currentEmail(): String? = null
        override fun signOut() {}
    }

    /** Fake reader port whose [status] StateFlow tracks how many collectors are currently active. */
    private class CountingRfidReaderPort : RfidReaderPort {
        private val upstream = MutableStateFlow(RfidReaderStatus.NOT_PAIRED)
        var activeStatusCollectors = 0
            private set

        override val status: StateFlow<RfidReaderStatus> = object : StateFlow<RfidReaderStatus> {
            override val replayCache: List<RfidReaderStatus> get() = upstream.replayCache
            override val value: RfidReaderStatus get() = upstream.value
            override suspend fun collect(collector: FlowCollector<RfidReaderStatus>): Nothing {
                activeStatusCollectors++
                try {
                    upstream.collect(collector)
                } finally {
                    activeStatusCollectors--
                }
            }
        }

        override val reads: SharedFlow<RfidRead> = MutableSharedFlow()
        override val readerName: StateFlow<String?> = MutableStateFlow(null)
        override val devices: StateFlow<List<RfidReaderDevice>> = MutableStateFlow(emptyList())
        override fun refreshStatus() {}
        override fun openSystemPairing() {}
        override fun setCaptureEnabled(enabled: Boolean) {}
        override fun onKeyEvent(event: KeyEvent): Boolean = false
    }
}
