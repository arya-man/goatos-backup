package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.WORKFLOW_MODULE_COLOSTRUM
import sg.mesha.goatos.core.data.WorkflowVideoDraft
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowChipsDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowOverdueDateDto
import sg.mesha.goatos.feature.counts.WorkflowModuleUi

/**
 * The Colostrum list ViewModel (docs/decisions/colostrum-milk-module.md). Two things are pinned
 * here because both are backend-contract-facing rather than cosmetic: the module key sent on every
 * read, and the absence of an awaiting-video chip.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ColostrumWorkflowListViewModelTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    /**
     * The lens keyword must reach the repository verbatim. A silent fallback to "birth" would still
     * render a plausible list — the kid's whole-workflow cards for that date — which is exactly the
     * kind of wrong-but-believable screen the day grain exists to prevent.
     */
    @Test
    fun `every read is scoped to the colostrum module`() = runTest {
        val repo = RecordingRepository()
        val vm = ColostrumWorkflowListViewModel(repo, NoopAnalytics(), NoopCrashReporter())

        vm.rows.first()
        vm.state.first()

        assertEquals(listOf(WORKFLOW_MODULE_COLOSTRUM), repo.cardModules.distinct())
        assertEquals(listOf(WORKFLOW_MODULE_COLOSTRUM), repo.chipModules.distinct())
        assertEquals(listOf(WORKFLOW_MODULE_COLOSTRUM), repo.overdueModules.distinct())
    }

    /**
     * Verification is enqueued once per WHOLE kid workflow, so one day's feeds can never sit in an
     * awaiting-video bucket. The backend rejects the filter outright, so offering the chip would
     * render a permanent zero that 400s the moment it is tapped.
     */
    @Test
    fun `the chip strip omits awaiting video`() = runTest {
        val repo = RecordingRepository(
            chips = WorkflowChipsDto(all = 6, overdue = 2, due = 3, completed = 1, awaitingVideo = 9),
        )
        val vm = ColostrumWorkflowListViewModel(repo, NoopAnalytics(), NoopCrashReporter())

        val chips = vm.state.first { it.chips.isNotEmpty() }.chips

        assertEquals(listOf("all", "overdue", "due", "completed"), chips.map { it.key })
        assertFalse(
            "awaiting_video is not a colostrum bucket",
            chips.any { it.key == "awaiting_video" },
        )
        // The four shown chips still render the backend's own counts verbatim.
        assertEquals(listOf(6, 2, 3, 1), chips.map { it.count })
    }

    /** Birth keeps the bucket its grain actually produces — this is a colostrum-only narrowing. */
    @Test
    fun `birth keeps its awaiting video chip`() = runTest {
        val repo = RecordingRepository(
            chips = WorkflowChipsDto(all = 6, overdue = 2, due = 3, completed = 1, awaitingVideo = 9),
        )
        val vm = BirthWorkflowListViewModel(repo, NoopAnalytics(), NoopCrashReporter())

        val chips = vm.state.first { it.chips.isNotEmpty() }.chips

        assertTrue(chips.any { it.key == "awaiting_video" && it.count == 9 })
    }

    @Test
    fun `the empty state names colostrum rather than generic follow-up work`() {
        assertEquals(
            "No colostrum feeds for this day.",
            workflowEmptyMessage(WorkflowModuleUi.COLOSTRUM, isOffline = false),
        )
    }

    // -----------------------------------------------------------------------

    private class RecordingRepository(
        private val chips: WorkflowChipsDto = WorkflowChipsDto(),
    ) : WorkflowsRepository {
        val cardModules = mutableListOf<String>()
        val chipModules = mutableListOf<String>()
        val overdueModules = mutableListOf<String>()

        override fun cards(module: String, date: String, filter: String): Flow<PagingData<WorkflowCardDto>> {
            cardModules += module
            return flowOf(PagingData.empty())
        }

        override fun observeChips(module: String, date: String): Flow<WorkflowChipsDto?> {
            chipModules += module
            return flowOf(chips)
        }

        override fun observeOverdueDates(module: String): Flow<List<WorkflowOverdueDateDto>> {
            overdueModules += module
            return flowOf(emptyList())
        }

        override fun observeDetail(workflowId: String, lens: String, date: String): Flow<WorkflowDetailResponseDto?> =
            flowOf(null)

        override fun observeVideoDrafts(workflowId: String): Flow<List<WorkflowVideoDraft>> = flowOf(emptyList())
        override suspend fun listVideoDrafts(workflowId: String): List<WorkflowVideoDraft> = emptyList()
        override suspend fun replaceVideoDraft(draft: WorkflowVideoDraft): WorkflowVideoDraft? = null
        override suspend fun clearVideoDrafts(workflowId: String) = Unit
        override suspend fun markVideoDraftsSubmitting(workflowId: String) = Unit
        override suspend fun refreshDetail(workflowId: String, lens: String, date: String): Result<Unit> =
            Result.success(Unit)

        override suspend fun findCachedCard(workflowId: String): WorkflowCardDto? = null
        override suspend fun markActionAnswered(workflowId: String, actionId: String, answerValue: String) = Unit
        override suspend fun markActionCompleted(workflowId: String, actionId: String, inReview: Boolean) = Unit
    }

}
