package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.currentTime
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.WorkflowVideoDraft
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowChipsDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowOverdueDateDto

/**
 * A synced birth report's kid and mother workflows are opened by the server's event consumer a few
 * seconds AFTER the report lands (Realme E2E 2026-09-17: submit 04:33:04, list read 04:33:07,
 * workflows 04:33:11). The list the operator returns to must look again, briefly and boundedly,
 * rather than show "1 child workflow is ready" above a list without it.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class WorkflowListFollowUpRefreshTest {

    @Test
    fun `the list refreshes until the new workflow arrives, then stops`() = runTest {
        val repo = ChipsRepository(all = 3)
        val vm = BirthWorkflowListViewModel(repo, NoopAnalytics(), NoopCrashReporter())
        var refreshes = 0

        vm.followUpAfterSubmission {
            refreshes++
            // The consumer opens the workflow in time for the second look.
            if (refreshes == 2) repo.chips.value = WorkflowChipsDto(all = 4)
        }

        assertEquals(2, refreshes)
        assertTrue("bounded: never polls past ~15 s", currentTime <= 15_000L)
    }

    @Test
    fun `a workflow that never arrives stops after a bounded number of looks`() = runTest {
        val repo = ChipsRepository(all = 3)
        val vm = DeathWorkflowListViewModel(repo, NoopAnalytics(), NoopCrashReporter())
        var refreshes = 0

        vm.followUpAfterSubmission { refreshes++ }

        assertEquals(WORKFLOW_FOLLOW_UP_DELAYS_MS.size, refreshes)
        assertTrue(currentTime <= 15_000L)
    }

    private class ChipsRepository(all: Int) : WorkflowsRepository {
        val chips = MutableStateFlow<WorkflowChipsDto?>(WorkflowChipsDto(all = all))
        override fun cards(module: String, date: String, filter: String): Flow<PagingData<WorkflowCardDto>> =
            flowOf(PagingData.empty())
        override fun observeChips(module: String, date: String): Flow<WorkflowChipsDto?> = chips
        override fun observeOverdueDates(module: String): Flow<List<WorkflowOverdueDateDto>> = flowOf(emptyList())
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
