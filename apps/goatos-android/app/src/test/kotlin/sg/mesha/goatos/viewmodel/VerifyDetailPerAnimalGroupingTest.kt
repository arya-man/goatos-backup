package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
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
import sg.mesha.goatos.feature.verify.VerifyDetailEvent

/**
 * Regression coverage for the shed-vs-animal verdict defect: a shed with N per-animal proof
 * clips must let the verifier reject ONE animal's clip without deciding — or even touching —
 * her shed-mates' still-pending clips. See VerifyDetailViewModel's `verificationGroupKey`
 * grouping and per-entry [sg.mesha.goatos.feature.verify.VerifyDetailEntryUiState].
 */
@OptIn(ExperimentalCoroutinesApi::class)
class VerifyDetailPerAnimalGroupingTest {
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
            evidenceAvailable = true,
            source = VerificationSourceRef(refType = "vaccination_goat", submissionId = "submission-shed-1"),
            media = listOf(
                VerificationMediaItem(proofId = "proof-$n", downloadUrl = "/proof-$n.mp4", mimeType = "video/mp4"),
            ),
        )
    }

    private fun legacyBundledItem(): VerificationQueueItem = VerificationQueueItem(
        itemId = "legacy-1",
        category = "vaccination_proof",
        status = VerificationStatus.PENDING,
        rowVersion = 1,
        subjectLabel = "Gandhi 1",
        shedLabel = "Gandhi 1",
        evidenceAvailable = true,
        // No shared submissionId with any other item — a legacy bundled submission.
        source = VerificationSourceRef(refType = "sop_submission"),
        media = listOf(
            VerificationMediaItem(proofId = "clip-1", downloadUrl = "/clip-1.mp4", mimeType = "video/mp4"),
            VerificationMediaItem(proofId = "clip-2", downloadUrl = "/clip-2.mp4", mimeType = "video/mp4"),
            VerificationMediaItem(proofId = "clip-3", downloadUrl = "/clip-3.mp4", mimeType = "video/mp4"),
        ),
    )

    private fun viewModel(
        groupId: String,
        repo: VerificationRepository,
        sync: GroupingSyncRepository = GroupingSyncRepository(),
    ) = VerifyDetailViewModel(
        repo = repo,
        syncRepo = sync,
        analytics = NoopAnalytics(),
        crashReporter = NoopCrashReporter(),
        savedStateHandle = SavedStateHandle(mapOf("itemId" to groupId, "category" to "vaccination_proof")),
    )

    @Test
    fun `5 per-animal items sharing one source_submission_id render as ONE group with 5 independently decidable entries`() = runTest(dispatcher) {
        val vm = viewModel(groupId = "submission-shed-1", repo = GroupingRepository(fiveGoatItems()))
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals(5, state.entries.size)
        assertEquals(setOf("goat-item-1", "goat-item-2", "goat-item-3", "goat-item-4", "goat-item-5"), state.entries.map { it.itemId }.toSet())
        assertTrue("every entry should be independently decidable", state.entries.all { it.isApproveEnabled && it.isRejectEnabled })
        assertFalse(state.isGroupFullyDecided)
    }

    @Test
    fun `rejecting one entry leaves the other four undecided and does not decide or close the group`() = runTest(dispatcher) {
        val sync = GroupingSyncRepository()
        val vm = viewModel(groupId = "submission-shed-1", repo = GroupingRepository(fiveGoatItems()), sync = sync)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(VerifyDetailEvent.Reject(reason = "wrong shed shown in clip", itemId = "goat-item-2"))
        advanceUntilIdle()

        // Only the ONE targeted animal's item id reached the outbox — never the group/shed id,
        // and never a sibling animal's item id.
        assertEquals(listOf("goat-item-2"), sync.enqueuedItemIds)
        assertEquals(listOf("rejected"), sync.enqueuedDecisions)

        // The fake repository is static (as a real Room-backed queue would be, until the
        // sweeper/backend actually applies this one decision and a refresh observes it) — so
        // every entry, INCLUDING goat-item-2 itself, is still reported PENDING here. That is
        // exactly the point: the client never locally fabricates the other four animals'
        // outcomes, and the group is never marked fully decided off of one verdict.
        val state = vm.state.value
        assertEquals(5, state.entries.size)
        assertFalse(state.isGroupFullyDecided)
        assertTrue(state.entries.all { it.statusTone == sg.mesha.goatos.feature.verify.VerifyTone.PENDING })
    }

    @Test
    fun `a legacy multi-media single item still renders and is judgeable as one unit`() = runTest(dispatcher) {
        val vm = viewModel(groupId = "legacy-1", repo = GroupingRepository(listOf(legacyBundledItem())))
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals(1, state.entries.size)
        val entry = state.entries.single()
        assertEquals("legacy-1", entry.itemId)
        assertEquals(3, entry.media.size)
        assertTrue(entry.isApproveEnabled)
        assertTrue(entry.isRejectEnabled)
        // Legacy single-entry group mirrors the flat top-level fields too, so existing
        // single-item call sites keep working unchanged.
        assertEquals(3, state.media.size)
        assertTrue(state.isApproveEnabled)
    }

    @Test
    fun `rejecting with an empty reason is refused`() = runTest(dispatcher) {
        val sync = GroupingSyncRepository()
        val vm = viewModel(groupId = "submission-shed-1", repo = GroupingRepository(fiveGoatItems()), sync = sync)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(VerifyDetailEvent.Reject(reason = "   ", itemId = "goat-item-3"))
        advanceUntilIdle()

        assertTrue("a blank reason must never reach the outbox", sync.enqueuedItemIds.isEmpty())
    }
}

private class GroupingRepository(private val items: List<VerificationQueueItem>) : VerificationRepository {
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
    override fun observeLeadershipVideos(category: String?, windowSize: Int) = flowOf(emptyList<sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi>())
    override fun observeLeadershipTitle(category: String?, windowSize: Int) = flowOf("")
    override suspend fun refreshLeadershipVideos(category: String?, windowSize: Int, reset: Boolean) =
        sg.mesha.goatos.core.common.AppResult.Ok(Unit)
}

private class GroupingSyncRepository : SyncRepository {
    val enqueuedItemIds = mutableListOf<String>()
    val enqueuedDecisions = mutableListOf<String>()
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf()
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> {
        enqueuedItemIds += itemId
        enqueuedDecisions += decision
        return AppResult.Ok("outbox-${enqueuedItemIds.size}")
    }

    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun findOutboxItem(itemId: String): AppResult<SyncQueueItem?> =
        AppResult.Ok(
            SyncQueueItem(
                id = itemId,
                idempotencyKey = "test-idempotency-key",
                opType = "verification_verdict",
                groupKey = itemId,
                status = SyncItemStatus.SUCCEEDED,
                attemptCount = 1,
                maxAttempts = 3,
                conflict = false,
                createdAt = 1,
                updatedAt = 2,
                lastError = null,
            ),
        )

    override suspend fun triggerDrain() = Unit
}

private class NoopAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) = Unit
    override fun setUserProperty(name: String, value: String?) = Unit
    override fun setUserId(id: String?) = Unit
}

private class NoopCrashReporter : CrashReporter {
    override fun log(message: String) = Unit
    override fun recordException(throwable: Throwable, message: String?) = Unit
    override fun setCustomKey(key: String, value: String) = Unit
}
