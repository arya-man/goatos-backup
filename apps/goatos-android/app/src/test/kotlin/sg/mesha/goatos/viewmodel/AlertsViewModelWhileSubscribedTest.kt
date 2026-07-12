package sg.mesha.goatos.viewmodel

import androidx.arch.core.executor.testing.InstantTaskExecutorRule
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.currentTime
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import kotlin.test.assertEquals

/**
 * MOB-010 guardrail: verify that [AlertsViewModel.state] uses WhileSubscribed(5_000) lifecycle,
 * so the upstream repository flow is only collected while the UI is subscribed.
 *
 * Without WhileSubscribed, the forever-collector drains battery/memory by re-processing Room
 * updates even when the screen is backgrounded. With WhileSubscribed(5_000), the collection
 * stops within 5 seconds of the last subscriber leaving, and restarts within 5s when a new
 * subscriber joins — warm Back navigation without permanent upstream work.
 */
class AlertsViewModelWhileSubscribedTest {
    @get:Rule
    val instantExecutor = InstantTaskExecutorRule()

    @Test
    fun `upstream flow is collected only while state has subscribers`() = runTest {
        val repo = FakeControlTowerRepository()

        val viewModel = AlertsViewModel(repo)

        // Initially, no subscriber to state → upstream should not be collected yet
        assertEquals(0, repo.activeSummaryCollectors, "upstream should have 0 collectors initially")

        // Subscribe to state
        val stateJob = launch {
            viewModel.state.collect { /* consume state */ }
        }
        advanceUntilIdle()

        // After subscription, upstream should be collected
        assertEquals(1, repo.activeSummaryCollectors, "upstream should have 1 collector after subscription")

        // Cancel the subscriber (unsubscribe)
        stateJob.cancel()
        advanceUntilIdle()

        // Immediately after cancel, collection may still be active (within the 5s window)
        val collectorsBeforeWait = repo.activeSummaryCollectors
        assert(collectorsBeforeWait >= 0, "collectors should be >= 0 immediately after cancel")

        // Wait past the 5_000ms WhileSubscribed timeout
        advanceTimeBy(6_000)
        advanceUntilIdle()

        // After 6+ seconds with no subscribers, upstream collection should be cancelled
        assertEquals(0, repo.activeSummaryCollectors, "upstream should have 0 collectors after 6s timeout")

        // Re-subscribe to verify it restarts
        val stateJob2 = launch {
            viewModel.state.collect { /* consume state */ }
        }
        advanceUntilIdle()

        assertEquals(1, repo.activeSummaryCollectors, "upstream should restart after re-subscription")

        stateJob2.cancel()
    }

    /**
     * Fake repository that tracks active collectors to verify lifecycle behavior.
     */
    private class FakeControlTowerRepository : ControlTowerRepository {
        private val _summaryFlow = MutableStateFlow<Resource<ControlTowerResponseDto>>(Resource())
        var activeSummaryCollectors = 0
            private set

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
        ): Flow<Resource<ControlTowerResponseDto>> {
            // Count active collectors: each call to Flow.collect increments this
            return object : Flow<Resource<ControlTowerResponseDto>> {
                override suspend fun collect(collector: kotlinx.coroutines.flow.FlowCollector<Resource<ControlTowerResponseDto>>) {
                    activeSummaryCollectors++
                    try {
                        _summaryFlow.collect(collector)
                    } finally {
                        activeSummaryCollectors--
                    }
                }
            }
        }

        override suspend fun refreshSummary(
            parkId: String?,
            shedId: String?,
            workState: String?,
            severity: String?,
            dueBefore: String?,
            asOf: String?,
            cursor: String?,
            limit: Int?,
        ): Result<Unit> = Result.success(Unit)
    }
}
