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
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.verify.VerifyDecisionUnavailableReason
import sg.mesha.goatos.feature.verify.VerifyDetailEvent

/**
 * Test the detail screen's auto-close behavior when the last item is decided.
 *
 * BUG-047: After a verifier decides the LAST pending item, the detail screen should close and
 * return to the queue ("Queue clear"), not stay open rendering "No video attached to this item".
 *
 * This test validates:
 * 1. When every item in a group becomes decided (status != PENDING), autoCloseAfterDecision=true
 *    is set, and entries becomes empty (items leave the queue cache).
 * 2. When a single entry has genuinely empty media, entries is not empty — the entry still exists,
 *    autoCloseAfterDecision stays false, and no close should be emitted.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class VerifyDetailAutoCloseTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModel(
        repo: VerificationRepository,
        sync: AutoCloseSyncRepository = AutoCloseSyncRepository(),
    ) = VerifyDetailViewModel(
        repo = repo,
        syncRepo = sync,
        analytics = AutoCloseAnalytics(),
        crashReporter = AutoCloseCrashReporter(),
        savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "vaccination_proof")),
    )

    // The auto-close DECISION path is deliberately not unit-tested here. Three successive
    // attempts to model it failed inside the fake, not the production code: the fake did not
    // publish decided status into the observed flow, deciding the item before Approve tripped
    // the approve gate, and markVerificationItemDecidedLocally was stubbed to Unit -- the exact
    // call the ViewModel makes to retire the item before refreshing. Each version passed or
    // failed for reasons that had nothing to do with the screen. Rather than keep a test that
    // asserts the harness, the close behaviour is verified on device against the real backend.
    // The screen-side rule under test is one line: close when state.autoCloseAfterDecision.

    @Test
    fun `approving the only pending entry requests immediate auto-close`() =
        runTest(dispatcher) {
            val repo = AutoCloseRepository(itemCount = 1)
            val sync = AutoCloseSyncRepository(repo)
            val vm = viewModel(repo, sync)
            backgroundScope.launch { vm.state.collect {} }
            advanceUntilIdle()

            assertEquals("entry should exist before approve", 1, vm.state.value.entries.size)
            assertTrue("approve should be enabled before approve", vm.state.value.isApproveEnabled)

            vm.onEvent(VerifyDetailEvent.Approve(itemId = "item-1"))
            advanceUntilIdle()

            assertTrue(
                "single-entry approve should close the detail instead of waiting on a blank refetch state",
                vm.state.value.autoCloseAfterDecision,
            )
        }

    @Test
    fun `an entry with genuinely empty media stays in entries and does not trigger auto-close`() =
        runTest(dispatcher) {
            val sync = AutoCloseSyncRepository()
            val vm = viewModel(AutoCloseRepository(itemCount = 1, emptyMedia = true), sync)
            backgroundScope.launch { vm.state.collect {} }
            advanceUntilIdle()

            // Item exists but has no media
            assertEquals("entry should exist even with empty media", 1, vm.state.value.entries.size)
            assertTrue("entry media should be empty", vm.state.value.entries.first().media.isEmpty())
            assertFalse(
                "autoCloseAfterDecision should be false on an item with no media",
                vm.state.value.autoCloseAfterDecision,
            )
            // Approve should be disabled (no evidence to watch)
            assertFalse(
                "approve should be disabled when media is unavailable",
                vm.state.value.isApproveEnabled,
            )
            assertEquals(
                VerifyDecisionUnavailableReason.EVIDENCE_UNAVAILABLE,
                vm.state.value.decisionUnavailableReason,
            )
        }
}

private class AutoCloseRepository(
    private val itemCount: Int = 1,
    private val groupKey: String = "item-1",
    private val emptyMedia: Boolean = false,
) : VerificationRepository {
    private fun makeItems(): List<VerificationQueueItem> =
        (1..itemCount).map { i ->
            VerificationQueueItem(
                itemId = "item-$i",
                category = "vaccination_proof",
                status = VerificationStatus.PENDING,
                rowVersion = i,
                evidenceAvailable = !emptyMedia,
                media = if (emptyMedia) {
                    emptyList()
                } else {
                    listOf(
                        VerificationMediaItem(
                            proofId = "proof-$i",
                            downloadUrl = "/proof-$i.mp4",
                            mimeType = "video/mp4",
                            durationMs = 9_000,
                        ),
                    )
                },
                subjectLabel = "Animal $i",
                source = VerificationSourceRef(
                    module = "vaccination",
                    refType = "vaccination_goat",
                    refId = "goat-$i",
                    submissionId = groupKey, // All items share the same submission ID to group together
                ),
            )
        }

    private val mutableItems = makeItems().toMutableList()
    private val queueFlow = MutableStateFlow(Resource(data = VerificationQueueResponseDto(items = mutableItems)))

    override suspend fun queue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto = queueFlow.value.data ?: VerificationQueueResponseDto()
    override fun observeQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> = queueFlow

    override suspend fun refreshQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> {
        // Simulate queue refresh: remove any items that are no longer pending (decided items leave the queue)
        mutableItems.retainAll { it.status == VerificationStatus.PENDING }
        queueFlow.value = Resource(data = VerificationQueueResponseDto(items = mutableItems.toList()))
        return Result.success(Unit)
    }

    override suspend fun appendQueue(cursor: String, category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override fun observeActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> = queueFlow

    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) = Unit
    // NOT a stub: this is the call the ViewModel makes to retire a decided item locally before
    // it refreshes, and stubbing it left the item PENDING in the observed flow, so `stillPending`
    // stayed true and autoCloseAfterDecision was never set. The fake has to honour it for the
    // test to be exercising the production path at all.
    override suspend fun markVerificationItemDecidedLocally(itemId: String) = markItemDecided(itemId)

    override fun observeLeadershipVideos(category: String?, windowSize: Int) = flowOf(emptyList<sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi>())
    override fun observeLeadershipTitle(category: String?, windowSize: Int) = flowOf("")
    override suspend fun refreshLeadershipVideos(category: String?, windowSize: Int, reset: Boolean) =
        sg.mesha.goatos.core.common.AppResult.Ok(Unit)

    // Publishes into the OBSERVED flow, which is what the real system does: the verdict lands
    // server-side and the queue read then reports the item as decided. Mutating the backing
    // list alone left observeQueue emitting the stale PENDING row, so the ViewModel's
    // `stillPending` check never saw the decision and autoCloseAfterDecision stayed false --
    // the fake, not the production code, was the thing under test.
    fun markItemDecided(itemId: String) {
        val index = mutableItems.indexOfFirst { it.itemId == itemId }
        if (index >= 0) {
            mutableItems[index] = mutableItems[index].copy(status = VerificationStatus.APPROVED)
            queueFlow.value = Resource(data = VerificationQueueResponseDto(items = mutableItems.toList()))
        }
    }
}

// Holds the queue fake so a verdict has the SERVER-SIDE effect it has in production: the
// item becomes decided and the queue read reports it that way. Deciding the item from the
// test body BEFORE calling Approve instead made the approve gate short-circuit
// (isApproveEnabled false on an already-decided entry), so the verdict never ran at all.
private class AutoCloseSyncRepository(private val queue: AutoCloseRepository? = null) : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf()
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> {
        queue?.markItemDecided(itemId)
        return AppResult.Ok("outbox-1")
    }

    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun findOutboxItem(itemId: String): AppResult<SyncQueueItem?> =
        AppResult.Ok(
            SyncQueueItem(
                id = itemId,
                idempotencyKey = "test-idempotency-key",
                opType = "verification_verdict",
                groupKey = "item-1",
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

private class AutoCloseAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) = Unit
    override fun setUserProperty(name: String, value: String?) = Unit
    override fun setUserId(id: String?) = Unit
}

private class AutoCloseCrashReporter : CrashReporter {
    override fun log(message: String) = Unit
    override fun recordException(throwable: Throwable, message: String?) = Unit
    override fun setCustomKey(key: String, value: String) = Unit
}
