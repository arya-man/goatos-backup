package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsEventsVerification
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.network.dto.ControlTowerAlertDto
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.feature.profile.AlertsEvent

/**
 * Full-journey coverage for the Alerts screen (previously zero analytics — nothing recorded
 * whether a CEO/operator ever opened this surface, tapped an alert, or dismissed the batch).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AlertsAnalyticsTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `screen view and refresh outcome fire once the initial load settles`() = runTest(dispatcher) {
        val analytics = AlertsRecordingAnalytics()
        val repo = FakeAlertsControlTowerRepository()
        val vm = AlertsViewModel(repo, analytics)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue(
            "alerts_viewed should fire once the loading placeholder clears",
            analytics.events.any { it.first == AnalyticsEventsVerification.ALERTS_VIEWED },
        )
        assertTrue(
            analytics.events.any { it.first == AnalyticsEventsVerification.ALERTS_REFRESH_ATTEMPTED },
        )
        assertTrue(
            analytics.events.any { it.first == AnalyticsEventsVerification.ALERTS_REFRESH_SUCCEEDED },
        )
        assertEquals(
            "alerts_viewed must fire exactly once per ViewModel lifetime",
            1,
            analytics.events.count { it.first == AnalyticsEventsVerification.ALERTS_VIEWED },
        )
    }

    @Test
    fun `a refresh failure emits alerts_refresh_failed with a reason`() = runTest(dispatcher) {
        val analytics = AlertsRecordingAnalytics()
        val repo = FakeAlertsControlTowerRepository(refreshResult = Result.failure(IllegalStateException("boom")))
        val vm = AlertsViewModel(repo, analytics)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val failure = analytics.events.first { it.first == AnalyticsEventsVerification.ALERTS_REFRESH_FAILED }
        assertEquals("IllegalStateException", failure.second[AnalyticsEventsVerification.Params.REASON])
    }

    @Test
    fun `tapping an alert row fires alert_tapped with its id`() = runTest(dispatcher) {
        val analytics = AlertsRecordingAnalytics()
        val repo = FakeAlertsControlTowerRepository()
        repo.emit(ControlTowerResponseDto(alerts = listOf(ControlTowerAlertDto(rowId = "alert-1", title = "t", detail = "d", severity = "warn"))))
        val vm = AlertsViewModel(repo, analytics)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(AlertsEvent.OpenAlert("alert-1"))
        advanceUntilIdle()

        val tapped = analytics.events.first { it.first == AnalyticsEventsVerification.ALERT_TAPPED }
        assertEquals("alert-1", tapped.second[AnalyticsEventsVerification.Params.ALERT_ID])
    }

    @Test
    fun `mark all read fires alert_mark_all_read with the unread count`() = runTest(dispatcher) {
        val analytics = AlertsRecordingAnalytics()
        val repo = FakeAlertsControlTowerRepository()
        repo.emit(
            ControlTowerResponseDto(
                alerts = listOf(
                    ControlTowerAlertDto(rowId = "alert-1", title = "t1", detail = "d1", severity = "warn"),
                    ControlTowerAlertDto(rowId = "alert-2", title = "t2", detail = "d2", severity = "info"),
                ),
            ),
        )
        val vm = AlertsViewModel(repo, analytics)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(AlertsEvent.MarkAllRead)
        advanceUntilIdle()

        val markAll = analytics.events.first { it.first == AnalyticsEventsVerification.ALERT_MARK_ALL_READ }
        assertEquals("2", markAll.second[AnalyticsEventsVerification.Params.UNREAD_COUNT])
    }
}

private class AlertsRecordingAnalytics : AnalyticsPort {
    val events = mutableListOf<Pair<String, Map<String, String>>>()
    override fun track(event: String, props: Map<String, String>) {
        events += event to props
    }
    override fun setUserProperty(name: String, value: String?) = Unit
    override fun setUserId(id: String?) = Unit
}

private class FakeAlertsControlTowerRepository(
    private val refreshResult: Result<Unit> = Result.success(Unit),
) : ControlTowerRepository {
    private val upstream = MutableStateFlow(Resource<ControlTowerResponseDto>(data = null))

    fun emit(dto: ControlTowerResponseDto?) {
        upstream.value = Resource(data = dto)
    }

    override suspend fun summary(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): ControlTowerResponseDto = ControlTowerResponseDto(alerts = emptyList())

    override fun observeSummary(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): Flow<Resource<ControlTowerResponseDto>> = upstream

    override suspend fun refreshSummary(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): Result<Unit> = refreshResult
}
