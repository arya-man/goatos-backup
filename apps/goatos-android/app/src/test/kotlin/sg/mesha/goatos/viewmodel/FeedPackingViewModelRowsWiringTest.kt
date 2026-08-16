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
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.network.dto.FeedPackingRowDto

/**
 * Integration test for FeedPackingViewModel.rows end-to-end: the ViewModel must pass
 * feedCompletionStore.submittedForReviewKeys into the map operator, and toRowUi must apply
 * overlayFeedLifecycleStatus to each row.
 *
 * This test drives the REAL `rows` Flow with a real FeedCompletionLocalStore (not a fake),
 * proving that the wiring integrates the two correctly. Stubbing-the-overlay tests
 * (FeedPackingSubmittedForReviewOverlayTest) pass even with the overlay deleted from the
 * ViewModel; this test FAILS if you remove feedCompletionStore.submittedForReviewKeys from
 * the combine(...) or remove the overlayFeedLifecycleStatus(...) call from toRowUi.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class FeedPackingViewModelRowsWiringTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun packingRow(
        shedId: String = "shed-1",
        partitionLabel: String? = "Pen A",
        workflow: String = "experiment",
        sessionNo: Int = 1,
        lifecycleStatus: String = "pending",
    ) = FeedPackingRowDto(
        parkId = "park-1",
        parkLabel = "Farm 1",
        shedId = shedId,
        shedLabel = "Shed 1",
        partitionLabel = partitionLabel,
        operationalLocationDisplay = "",
        sessionNo = sessionNo,
        sessionLabel = "Session 1",
        workflow = workflow,
        experimentArm = "",
        headCount = 10,
        items = emptyList(),
        totalKg = "50.0",
        status = "unknown",
        completed = false,
        lifecycleStatus = lifecycleStatus,
        reworkReason = "",
        blockedReasons = emptyList(),
    )

    @Test
    fun `rows overlay - a submitted row reads pending_verification before backend sync`() =
        runTest(dispatcher) {
            val store = FeedCompletionLocalStore()
            val repo = FakeFeedRepository()
            val viewModel = FeedPackingViewModel(
                repo = repo,
                feedCompletionStore = store,
                analytics = NoopAnalytics(),
                crashReporter = NoopCrashReporter(),
            )

            val row = packingRow(lifecycleStatus = "pending")
            repo.setPackingRowsPage(PagingData.from(listOf(row)))

            // Collect the rows Flow: initially, backend says pending, overlay says pending.
            val initial = viewModel.rows.asSnapshot()
            assertEquals(1, initial.size)
            assertEquals("pending", initial[0].lifecycleStatus)

            // Mark submitted in store: next emission should render pending_verification.
            store.markSubmittedForReview(
                FeedCompletionLocalStore.key(row.shedId, row.partitionLabel, row.sessionNo, row.workflow)
            )

            // Force re-emission by changing filters (which re-drives combine -> flatMapLatest).
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
        val viewModel = FeedPackingViewModel(
            repo = repo,
            feedCompletionStore = store,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
        )

        val row = packingRow(lifecycleStatus = "pending")
        repo.setPackingRowsPage(PagingData.from(listOf(row)))

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
            val viewModel = FeedPackingViewModel(
                repo = repo,
                feedCompletionStore = store,
                analytics = NoopAnalytics(),
                crashReporter = NoopCrashReporter(),
            )

            val penA = packingRow(partitionLabel = "Pen A", lifecycleStatus = "pending")
            val penB = packingRow(partitionLabel = "Pen B", lifecycleStatus = "pending")
            repo.setPackingRowsPage(PagingData.from(listOf(penA, penB)))

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
