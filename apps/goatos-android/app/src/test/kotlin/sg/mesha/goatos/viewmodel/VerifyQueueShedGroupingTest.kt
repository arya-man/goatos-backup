package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
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
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationMediaItem
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationSourceRef
import sg.mesha.goatos.core.network.dto.VerificationStatus

/**
 * The queue must show ONE CARD PER SHED SUBMISSION, not one per animal, once the backend moves
 * to a per-goat verification_item (source_ref_type=vaccination_goat, shared source_submission_id).
 * A 40-animal shed regressing to 40 queue rows is exactly the bug this covers.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class VerifyQueueShedGroupingTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun fiveGoatItems(): List<VerificationQueueItem> = (1..5).map { n ->
        VerificationQueueItem(
            itemId = "goat-item-$n",
            category = "vaccination_proof",
            status = VerificationStatus.PENDING,
            rowVersion = n,
            subjectLabel = "Goat #$n",
            shedLabel = "Gandhi 1",
            source = VerificationSourceRef(refType = "vaccination_goat", submissionId = "submission-shed-1"),
            media = listOf(VerificationMediaItem(proofId = "proof-$n", downloadUrl = "/proof-$n.mp4")),
        )
    }

    @Test
    fun `5 per-animal items sharing one submission render as exactly one queue card`() = runTest(dispatcher) {
        val repo = ShedQueueRepository(fiveGoatItems())
        val vm = VerifyQueueViewModel(repo = repo, syncRepo = QueueGroupingNoopSyncRepository(), analytics = NoopAnalytics(), savedStateHandle = SavedStateHandle())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(1, vm.state.value.rows.size)
        val card = vm.state.value.rows.single()
        assertEquals("submission-shed-1", card.id)
        assertTrue("card title must name the shed", card.title.contains("Gandhi 1"))
        assertTrue("card title must show progress, e.g. '5 goats · 5 to review'", card.title.contains("5 goats"))
        assertTrue(card.title.contains("5 to review"))
    }

    @Test
    fun `a legacy single item with no siblings still renders its own card`() = runTest(dispatcher) {
        val legacy = VerificationQueueItem(
            itemId = "legacy-1",
            category = "vaccination_proof",
            status = VerificationStatus.PENDING,
            subjectLabel = "Gandhi 1",
            shedLabel = "Gandhi 1",
            source = VerificationSourceRef(refType = "sop_submission"),
            media = listOf(VerificationMediaItem(proofId = "clip-1", downloadUrl = "/clip-1.mp4")),
        )
        val repo = ShedQueueRepository(listOf(legacy))
        val vm = VerifyQueueViewModel(repo = repo, syncRepo = QueueGroupingNoopSyncRepository(), analytics = NoopAnalytics(), savedStateHandle = SavedStateHandle())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(1, vm.state.value.rows.size)
        assertEquals("legacy-1", vm.state.value.rows.single().id)
    }
}

private class ShedQueueRepository(private val items: List<VerificationQueueItem>) : VerificationRepository {
    private val response = VerificationQueueResponseDto(items = items)

    override suspend fun queue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto = response
    override fun observeQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> =
        flowOf(Resource(data = response))

    override suspend fun refreshQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override suspend fun appendQueue(cursor: String, category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override fun observeActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> =
        flowOf(Resource(data = response))

    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) = Unit
    override suspend fun markVerificationItemDecidedLocally(itemId: String) = Unit
}

private class QueueGroupingNoopSyncRepository : SyncRepository {
    override fun observeStatus() = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf()
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationBatchClose(batchId: String): AppResult<String> = AppResult.Ok("close-$batchId")
    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}
