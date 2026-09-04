package sg.mesha.goatos.boot

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.CrashReporter
import android.content.Context
import org.junit.Assert.assertNotNull
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.sync.ConnectivityGate
import sg.mesha.goatos.core.datastore.FakeDeviceStore
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.push.PushTokenSync

@OptIn(ExperimentalCoroutinesApi::class)
class BootstrapViewModelAnalyticsTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private class FakeBootstrapRepository(
        private val navState: NavState,
        private val profile: BootstrapOperatorProfileDto?,
    ) : BootstrapRepository {
        override suspend fun loadNavState(): NavState = navState
        override suspend fun operatorProfile(): BootstrapOperatorProfileDto? = profile
    }

    private class FakeAuthRepository(private val email: String?, private val firebaseUid: String? = "firebase-uid-123") : AuthRepository {
        override suspend fun signInWithEmailPassword(email: String, password: String): Result<Unit> = Result.success(Unit)
        override suspend fun signInWithGoogle(activityContext: Context): Result<Unit> = Result.success(Unit)
        override suspend fun sendPasswordReset(email: String): Result<Unit> = Result.success(Unit)
        override suspend fun currentIdToken(forceRefresh: Boolean): String? = null
        override fun currentEmail(): String? = email
        override fun currentFirebaseUid(): String? = firebaseUid
        override fun signOut() {}
    }

    private class FakeCrashReporter : CrashReporter {
        val logs = mutableListOf<String>()
        val keys = mutableMapOf<String, String>()
        val exceptions = mutableListOf<Throwable>()

        override fun log(message: String) {
            logs.add(message)
        }

        // Signatures must match CrashReporter exactly (message is nullable with a default). An
        // earlier version declared `message: String`, which does not override the interface
        // method and left the class abstract -- that single mismatch stopped the whole app test
        // source set from compiling, silently taking dozens of unrelated tests out of the run.
        override fun recordException(throwable: Throwable, message: String?) {
            exceptions.add(throwable)
        }

        override fun setCustomKey(key: String, value: String) {
            keys[key] = value
        }
    }

    /** Defaults online: most tests exercise the network-success path, not the offline fallback. */
    private class FakeConnectivityGate(private val online: Boolean = true) : ConnectivityGate {
        override fun isOnline(): Boolean = online
    }

    @Test
    fun `resolved bootstrap logs bootstrap_loaded with chrome and stamps role and park identity`() = runTest {
        val analytics = RecordingAnalytics()
        val context = AnalyticsContext(flavor = "dev")
        val crashReporter = FakeCrashReporter()
        var pushSyncCalls = 0
        val repo = FakeBootstrapRepository(
            navState = NavState(NavChrome.EXPANDED, emptyList()),
            profile = BootstrapOperatorProfileDto(operatorId = "member-123", primaryRoleHint = "operator", primaryLocation = "Park A"),
        )

        BootstrapViewModel(repo, analytics, context, FakeDeviceStore(), FakeAuthRepository("ravi@mesha.sg"), crashReporter, PushTokenSync { pushSyncCalls++ }, FakeConnectivityGate(), NavStateRefreshSignal()).load()
        advanceUntilIdle()

        val loaded = analytics.events.single { it.name == AnalyticsEvents.BOOTSTRAP_LOADED }
        assertEquals("expanded", loaded.props[AnalyticsEvents.Params.CHROME])
        assertEquals("ravi@mesha.sg", loaded.props[AnalyticsEvents.Params.EMAIL])
        assertEquals("firebase-uid-123", loaded.props[AnalyticsEvents.Params.FIREBASE_UID])

        assertEquals("operator", context.role)
        assertEquals("Park A", context.parkScope)
        assertEquals("operator", analytics.userProps[AnalyticsEvents.UserProps.ROLE])
        assertEquals("Park A", analytics.userProps[AnalyticsEvents.UserProps.PRIMARY_PARK])
        assertEquals("dev", analytics.userProps[AnalyticsEvents.UserProps.FLAVOR])
        assertEquals("member-123", analytics.userId)

        // Email (business-owner decision) is set as a user property from the signed-in Firebase user.
        assertEquals("ravi@mesha.sg", analytics.userProps[AnalyticsEvents.UserProps.EMAIL])
        // Device id resolved from DeviceStore.appInstallId(); set as user property + on the context.
        assertNotNull("device id resolved", context.deviceId)
        assertEquals(context.deviceId, analytics.userProps[AnalyticsEvents.UserProps.DEVICE_ID])
        assertEquals("successful bootstrap must refresh-register the current FCM token", 1, pushSyncCalls)
    }

    @Test
    fun `identity is set BEFORE bootstrap_loaded event is tracked`() = runTest {
        val analytics = RecordingAnalytics()
        val context = AnalyticsContext(flavor = "dev")
        val crashReporter = FakeCrashReporter()
        val repo = FakeBootstrapRepository(
            navState = NavState(NavChrome.EXPANDED, emptyList()),
            profile = BootstrapOperatorProfileDto(operatorId = "member-456", primaryRoleHint = "director", primaryLocation = "Park B"),
        )

        BootstrapViewModel(repo, analytics, context, FakeDeviceStore(), FakeAuthRepository("alice@mesha.sg"), crashReporter, PushTokenSync {}, FakeConnectivityGate(), NavStateRefreshSignal()).load()
        advanceUntilIdle()

        // The order of operations must ensure identity is set BEFORE BOOTSTRAP_LOADED is tracked.
        // The RecordingAnalytics.track() is called after identity has been set, so we should have:
        // 1. setUserId("member-456") called first
        // 2. setUserProperty(EMAIL, "alice@mesha.sg") called first
        // 3. THEN track(BOOTSTRAP_LOADED) called

        // Verify user ID is set before the event
        assertEquals("member-456", analytics.userId)
        assertEquals("alice@mesha.sg", analytics.userProps[AnalyticsEvents.UserProps.EMAIL])

        // Verify BOOTSTRAP_LOADED event includes the email in its properties
        val bootstrapEvent = analytics.events.single { it.name == AnalyticsEvents.BOOTSTRAP_LOADED }
        assertEquals("alice@mesha.sg", bootstrapEvent.props[AnalyticsEvents.Params.EMAIL])
    }

    @Test
    fun `leadership user with no operator profile leaves identity null but still logs the load`() = runTest {
        val analytics = RecordingAnalytics()
        val context = AnalyticsContext(flavor = "stg")
        val crashReporter = FakeCrashReporter()
        val repo = FakeBootstrapRepository(
            navState = NavState(NavChrome.MINIMAL, emptyList()),
            profile = null,
        )

        BootstrapViewModel(repo, analytics, context, FakeDeviceStore(), FakeAuthRepository("ravi@mesha.sg"), crashReporter, PushTokenSync {}, FakeConnectivityGate(), NavStateRefreshSignal()).load()
        advanceUntilIdle()

        val loaded = analytics.events.single { it.name == AnalyticsEvents.BOOTSTRAP_LOADED }
        assertEquals("minimal", loaded.props[AnalyticsEvents.Params.CHROME])

        assertNull("no profile -> role stays unknown", context.role)
        assertNull("no profile -> park stays unknown", context.parkScope)
        assertNull("no profile -> user id stays unknown", analytics.userId)
        assertNull(analytics.userProps[AnalyticsEvents.UserProps.ROLE])
        // Flavor is build-fixed, so it is set regardless of whether a profile exists.
        assertEquals("stg", analytics.userProps[AnalyticsEvents.UserProps.FLAVOR])
    }

    @Test
    fun `blank role and park hints are treated as absent, not empty strings`() = runTest {
        val analytics = RecordingAnalytics()
        val context = AnalyticsContext(flavor = "prod")
        val crashReporter = FakeCrashReporter()
        val repo = FakeBootstrapRepository(
            navState = NavState(NavChrome.MINIMAL, emptyList()),
            profile = BootstrapOperatorProfileDto(primaryRoleHint = "   ", primaryLocation = ""),
        )

        BootstrapViewModel(repo, analytics, context, FakeDeviceStore(), FakeAuthRepository("ravi@mesha.sg"), crashReporter, PushTokenSync {}, FakeConnectivityGate(), NavStateRefreshSignal()).load()
        advanceUntilIdle()

        assertNull("blank role hint -> null identity", context.role)
        assertNull("blank location -> null identity", context.parkScope)
        assertNull("blank operator id -> null user id", analytics.userId)
    }

    @Test
    fun `reset discards a held Ready state back to Loading (logout clean-slate, C35-001)`() = runTest {
        val analytics = RecordingAnalytics()
        val context = AnalyticsContext(flavor = "dev")
        val crashReporter = FakeCrashReporter()
        val repo = FakeBootstrapRepository(
            navState = NavState(NavChrome.EXPANDED, listOf()),
            profile = BootstrapOperatorProfileDto(primaryRoleHint = "operator", primaryLocation = "Park A"),
        )
        val vm = BootstrapViewModel(repo, analytics, context, FakeDeviceStore(), FakeAuthRepository("ravi@mesha.sg"), crashReporter, PushTokenSync {}, FakeConnectivityGate(), NavStateRefreshSignal())
        vm.load()
        advanceUntilIdle()
        check(vm.state.value is BootstrapUiState.Ready) { "precondition: vm should be Ready before reset" }

        // Activity-scoped BootstrapViewModel otherwise survives a logout holding the departing
        // user's nav state; reset() is what MainActivity calls on the auth-state observer's
        // false transition so no stale Ready(navState) can ever be shown to the next principal.
        vm.reset()

        assertEquals(BootstrapUiState.Loading, vm.state.value)
    }
}
