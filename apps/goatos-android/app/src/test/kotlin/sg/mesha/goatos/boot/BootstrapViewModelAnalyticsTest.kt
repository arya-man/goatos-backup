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
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto

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

    @Test
    fun `resolved bootstrap logs bootstrap_loaded with chrome and stamps role and park identity`() = runTest {
        val analytics = RecordingAnalytics()
        val context = AnalyticsContext(flavor = "dev")
        val repo = FakeBootstrapRepository(
            navState = NavState(NavChrome.EXPANDED, emptyList()),
            profile = BootstrapOperatorProfileDto(primaryRoleHint = "operator", primaryLocation = "Park A"),
        )

        BootstrapViewModel(repo, analytics, context)
        advanceUntilIdle()

        val loaded = analytics.events.single { it.name == AnalyticsEvents.BOOTSTRAP_LOADED }
        assertEquals("expanded", loaded.props[AnalyticsEvents.Params.CHROME])

        assertEquals("operator", context.role)
        assertEquals("Park A", context.parkScope)
        assertEquals("operator", analytics.userProps[AnalyticsEvents.UserProps.ROLE])
        assertEquals("Park A", analytics.userProps[AnalyticsEvents.UserProps.PRIMARY_PARK])
        assertEquals("dev", analytics.userProps[AnalyticsEvents.UserProps.FLAVOR])
    }

    @Test
    fun `leadership user with no operator profile leaves identity null but still logs the load`() = runTest {
        val analytics = RecordingAnalytics()
        val context = AnalyticsContext(flavor = "stg")
        val repo = FakeBootstrapRepository(
            navState = NavState(NavChrome.MINIMAL, emptyList()),
            profile = null,
        )

        BootstrapViewModel(repo, analytics, context)
        advanceUntilIdle()

        val loaded = analytics.events.single { it.name == AnalyticsEvents.BOOTSTRAP_LOADED }
        assertEquals("minimal", loaded.props[AnalyticsEvents.Params.CHROME])

        assertNull("no profile -> role stays unknown", context.role)
        assertNull("no profile -> park stays unknown", context.parkScope)
        assertNull(analytics.userProps[AnalyticsEvents.UserProps.ROLE])
        // Flavor is build-fixed, so it is set regardless of whether a profile exists.
        assertEquals("stg", analytics.userProps[AnalyticsEvents.UserProps.FLAVOR])
    }

    @Test
    fun `blank role and park hints are treated as absent, not empty strings`() = runTest {
        val analytics = RecordingAnalytics()
        val context = AnalyticsContext(flavor = "prod")
        val repo = FakeBootstrapRepository(
            navState = NavState(NavChrome.MINIMAL, emptyList()),
            profile = BootstrapOperatorProfileDto(primaryRoleHint = "   ", primaryLocation = ""),
        )

        BootstrapViewModel(repo, analytics, context)
        advanceUntilIdle()

        assertNull("blank role hint -> null identity", context.role)
        assertNull("blank location -> null identity", context.parkScope)
    }

    @Test
    fun `reset discards a held Ready state back to Loading (logout clean-slate, C35-001)`() = runTest {
        val analytics = RecordingAnalytics()
        val context = AnalyticsContext(flavor = "dev")
        val repo = FakeBootstrapRepository(
            navState = NavState(NavChrome.EXPANDED, listOf()),
            profile = BootstrapOperatorProfileDto(primaryRoleHint = "operator", primaryLocation = "Park A"),
        )
        val vm = BootstrapViewModel(repo, analytics, context)
        advanceUntilIdle()
        check(vm.state.value is BootstrapUiState.Ready) { "precondition: vm should be Ready before reset" }

        // Activity-scoped BootstrapViewModel otherwise survives a logout holding the departing
        // user's nav state; reset() is what MainActivity calls on the auth-state observer's
        // false transition so no stale Ready(navState) can ever be shown to the next principal.
        vm.reset()

        assertEquals(BootstrapUiState.Loading, vm.state.value)
    }
}
