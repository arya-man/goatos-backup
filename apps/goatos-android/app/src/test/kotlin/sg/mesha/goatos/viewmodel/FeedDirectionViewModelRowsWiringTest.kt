package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import androidx.paging.testing.asSnapshot
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.FeedDirectionRowDto
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.MutableStateFlow

/**
 * Integration test for FeedDirectionViewModel.rows end-to-end: the ViewModel must pass
 * feedCompletionStore.submittedForReviewKeys into the map operator, and toRowUi must apply
 * overlayFeedLifecycleStatus to each row.
 *
 * This test drives the REAL `rows` Flow with a real FeedCompletionLocalStore, proving the
 * wiring integrates the two correctly. Stubbing-the-overlay tests pass even with the overlay
 * deleted; this test FAILS if feedCompletionStore.submittedForReviewKeys is removed from the
 * combine or the overlayFeedLifecycleStatus call is removed from toRowUi.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class FeedDirectionViewModelRowsWiringTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun directionRow(
        shedId: String = "shed-1",
        partitionLabel: String? = "Pen A",
        workflow: String = "experiment",
        sessionNo: Int = 1,
        lifecycleStatus: String = "pending",
    ) = FeedDirectionRowDto(
        parkId = "park-1",
        parkLabel = "Farm 1",
        shedId = shedId,
        shedLabel = "Shed 1",
        partitionLabel = partitionLabel,
        shedTag = "tag-1",
        breed = "breed-1",
        rationGroup = "group-1",
        experimentArm = "",
        sessionLabel = "Session 1",
        sessionNo = sessionNo,
        workflow = workflow,
        headCount = 10,
        headCountInformational = false,
        items = emptyList(),
        sessionTotalKg = "50.0",
        blocked = false,
        overduePending = false,
        completed = false,
        lifecycleStatus = lifecycleStatus,
    )

    @Test
    fun `rows overlay - a submitted row reads pending_verification before backend sync`() =
        runTest(dispatcher) {
            val store = FeedCompletionLocalStore()
            val repo = FakeFeedRepository()
            val viewModel = FeedDirectionViewModel(
                repo = repo,
                feedCompletionStore = store,
                bootstrapRepository = FakeFeedDirectionBootstrapRepository(),
                analytics = NoopAnalytics(),
                crashReporter = NoopCrashReporter(),
            )

            val row = directionRow(lifecycleStatus = "pending")
            repo.setDirectionRowsPage(PagingData.from(listOf(row)))

            // Collect the rows Flow: initially, backend says pending, overlay says pending.
            val initial = viewModel.rows.asSnapshot()
            assertEquals(1, initial.size)
            assertEquals("pending", initial[0].lifecycleStatus)

            // Mark submitted in store: next emission should render pending_verification.
            store.markSubmittedForReview(
                FeedCompletionLocalStore.key(row.shedId, row.partitionLabel, row.sessionNo, row.workflow)
            )

            // Force re-emission by changing filters.
            val reworkedRows = viewModel.rows.asSnapshot()
            assertEquals(1, reworkedRows.size)
            assertEquals(
                "submitted row must render pending_verification via the overlay",
                "pending_verification",
                reworkedRows[0].lifecycleStatus,
            )
        }

    @Test
    fun `rows overlay - a non-submitted row stays pending`() = runTest(dispatcher) {
        val store = FeedCompletionLocalStore()
        val repo = FakeFeedRepository()
        val viewModel = FeedDirectionViewModel(
            repo = repo,
            feedCompletionStore = store,
            bootstrapRepository = FakeFeedDirectionBootstrapRepository(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
        )

        val row = directionRow(lifecycleStatus = "pending")
        repo.setDirectionRowsPage(PagingData.from(listOf(row)))

        val snapshot = viewModel.rows.asSnapshot()
        assertEquals(1, snapshot.size)
        assertEquals(
            "a non-submitted pending row has no overlay",
            "pending",
            snapshot[0].lifecycleStatus,
        )
    }

    @Test
    fun `rows overlay - grain scope - submitting one pen doesn't flip another`() =
        runTest(dispatcher) {
            val store = FeedCompletionLocalStore()
            val repo = FakeFeedRepository()
            val viewModel = FeedDirectionViewModel(
                repo = repo,
                feedCompletionStore = store,
                bootstrapRepository = FakeFeedDirectionBootstrapRepository(),
                analytics = NoopAnalytics(),
                crashReporter = NoopCrashReporter(),
            )

            val penA = directionRow(partitionLabel = "Pen A", lifecycleStatus = "pending")
            val penB = directionRow(partitionLabel = "Pen B", lifecycleStatus = "pending")
            repo.setDirectionRowsPage(PagingData.from(listOf(penA, penB)))

            val initial = viewModel.rows.asSnapshot()
            assertEquals(2, initial.size)

            store.markSubmittedForReview(
                FeedCompletionLocalStore.key(penA.shedId, penA.partitionLabel, penA.sessionNo, penA.workflow)
            )

            val after = viewModel.rows.asSnapshot()
            assertEquals(2, after.size)
            assertEquals("pending_verification", after[0].lifecycleStatus)
            assertEquals("pending", after[1].lifecycleStatus)
        }
}

private class FakeFeedDirectionBootstrapRepository : BootstrapRepository {
    override suspend fun loadNavState(): NavState =
        NavState(
            NavChrome.MINIMAL,
            emptyList(),
        )

    override suspend fun operatorProfile(): BootstrapOperatorProfileDto? =
        BootstrapOperatorProfileDto(displayName = "Test")
}
