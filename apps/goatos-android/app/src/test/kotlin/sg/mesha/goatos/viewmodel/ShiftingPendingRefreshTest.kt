package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import app.cash.turbine.test
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.ShiftingActionsMeta
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto
import sg.mesha.goatos.feature.counts.ShiftingPendingEvent

/**
 * A refresh must actually REFETCH.
 *
 * `Refresh` used to be `_selection.value = _selection.value.copy()`. Selection is a data class, so
 * that produced an EQUAL value; MutableStateFlow conflates on equality, emitted nothing, and left
 * flatMapLatest subscribed to the SAME page. Both the RefreshOnResume hook and the sync button ran
 * through that path, so a screen already on the back stack could never pick up server state.
 *
 * Observed 2026-08-13: the shifting bell kept showing "1" after the movement was completed, while
 * the backend's previous_dates for the same window was already empty. The badge is rendered from
 * the meta that arrives WITH a page load, so no refetch means no new badge — forever, as long as
 * the view model stayed alive.
 *
 * This asserts the observable consequence (a second subscription to the paged read), not the
 * private nonce, so the test survives any future refresh mechanism that genuinely refetches.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShiftingPendingRefreshTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `refresh resubscribes the paged read so fresh server state can arrive`() = runTest(dispatcher) {
        val repo = CountingShiftingPendingRepository()
        val viewModel = ShiftingPendingViewModel(
            repo = repo,
            drafts = NoopDraftRepository(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
        )

        viewModel.rows.test {
            awaitItem()
            advanceUntilIdle()
            assertEquals("initial subscription", 1, repo.pendingSubscriptions)

            viewModel.onEvent(ShiftingPendingEvent.Refresh)
            advanceUntilIdle()
            assertEquals(
                "a refresh must refetch; an equal Selection is conflated away and never reaches the repo",
                2,
                repo.pendingSubscriptions,
            )

            viewModel.onEvent(ShiftingPendingEvent.Refresh)
            advanceUntilIdle()
            assertEquals("every refresh refetches, not just the first", 3, repo.pendingSubscriptions)

            cancelAndIgnoreRemainingEvents()
        }
    }
}

private class CountingShiftingPendingRepository : ShiftingPendingRepository {
    var pendingSubscriptions = 0
        private set

    override val actionsMeta: StateFlow<ShiftingActionsMeta> = MutableStateFlow(ShiftingActionsMeta())

    override fun pending(
        date: String,
        status: String,
    ): Flow<PagingData<CountsShiftingPendingExecutionItemDto>> = flow {
        pendingSubscriptions++
        emit(PagingData.empty())
    }

    override suspend fun forgetExecuted(shiftingEventId: String) = Unit

    override suspend fun findCached(shiftingEventId: String): CountsShiftingPendingExecutionItemDto? = null
}

/** Draft state is irrelevant here: this test is about whether the PAGED READ is resubscribed. */
private class NoopDraftRepository : CaptureDraftRepository {
    override suspend fun find(flowKey: String, entityId: String): CaptureDraft = CaptureDraft()

    override fun observe(flowKey: String, entityId: String): Flow<CaptureDraft> =
        MutableStateFlow(CaptureDraft())

    override fun observeProgress(flowKey: String, limit: Int): Flow<Map<String, Int>> =
        MutableStateFlow(emptyMap())

    override suspend fun putProof(
        flowKey: String,
        entityId: String,
        step: String,
        outboxItemId: String,
        fingerprint: String?,
    ) = Unit

    override suspend fun clearProof(flowKey: String, entityId: String, step: String) = Unit

    override suspend fun putSubmit(
        flowKey: String,
        entityId: String,
        idempotencyKey: String?,
        outboxItemId: String?,
    ) = Unit

    override suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>) = Unit

    override suspend fun clear(flowKey: String, entityId: String) = Unit
}
