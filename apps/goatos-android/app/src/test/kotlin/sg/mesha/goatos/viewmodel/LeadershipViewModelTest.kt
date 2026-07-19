package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
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
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.data.VaccinationInsightsRepository
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VaccinationCoverageResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationGapsResponseDto
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationSourceRef
import sg.mesha.goatos.feature.leadership.LeadershipEvent

/**
 * R50-030 regression: leadership drive closure follows the REAL outbox row to its terminal
 * state instead of clearing the "closing" spinner the instant the write is enqueued. A
 * SUCCEEDED close must clear [sg.mesha.goatos.viewmodel.LeadershipViewModel]'s in-flight flag
 * AND refresh the closure action queue (so the just-closed drive drops off the list); a
 * conflict or exhausted-retry (dead-letter) outcome must still clear the spinner so the
 * operator is not stuck on a permanent "closing…" state, but must NOT refresh — there is
 * nothing new to show and a refresh would just churn the list under a failed action.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class LeadershipViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `succeeded closure clears the spinner and refreshes the action queue`() = runTest(dispatcher) {
        val verification = FakeVerificationRepository(closureItem("sub-1"))
        val sync = FakeCloseSyncRepository()
        val vm = LeadershipViewModel(
            controlTower = FakeControlTowerRepository(),
            insights = FakeVaccinationInsightsRepository(),
            verification = verification,
            syncRepository = sync,
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        val refreshesBeforeClose = verification.refreshActionQueueCalls

        vm.onEvent(LeadershipEvent.CloseVerificationDrive("sub-1"))
        advanceUntilIdle()
        assertTrue(
            "enqueue succeeded but the outbox item has not resolved yet -> still queueing",
            vm.state.value.verificationClosures.single { it.submissionId == "sub-1" }.isQueueing,
        )

        sync.emit("outbox-1", SyncItemStatus.SUCCEEDED)
        advanceUntilIdle()

        assertFalse(
            "R50-030: a SUCCEEDED close must clear the closing spinner",
            vm.state.value.verificationClosures.single { it.submissionId == "sub-1" }.isQueueing,
        )
        assertEquals(
            "R50-030: a SUCCEEDED close must refresh the action queue so the closed drive drops off",
            refreshesBeforeClose + 1,
            verification.refreshActionQueueCalls,
        )
    }

    @Test
    fun `conflict outcome clears the spinner without refreshing the queue`() = runTest(dispatcher) {
        val verification = FakeVerificationRepository(closureItem("sub-2"))
        val sync = FakeCloseSyncRepository()
        val vm = LeadershipViewModel(
            controlTower = FakeControlTowerRepository(),
            insights = FakeVaccinationInsightsRepository(),
            verification = verification,
            syncRepository = sync,
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        val refreshesBeforeClose = verification.refreshActionQueueCalls

        vm.onEvent(LeadershipEvent.CloseVerificationDrive("sub-2"))
        advanceUntilIdle()
        sync.emit("outbox-1", SyncItemStatus.FAILED, conflict = true)
        advanceUntilIdle()

        assertFalse(
            "R50-030: a rejected (conflict) close must still clear the spinner, not leave it stuck",
            vm.state.value.verificationClosures.single { it.submissionId == "sub-2" }.isQueueing,
        )
        assertEquals(
            "R50-030: a conflict has nothing new to show -> must NOT trigger a refresh",
            refreshesBeforeClose,
            verification.refreshActionQueueCalls,
        )
    }

    @Test
    fun `dead-letter outcome clears the spinner without refreshing the queue`() = runTest(dispatcher) {
        val verification = FakeVerificationRepository(closureItem("sub-3"))
        val sync = FakeCloseSyncRepository()
        val vm = LeadershipViewModel(
            controlTower = FakeControlTowerRepository(),
            insights = FakeVaccinationInsightsRepository(),
            verification = verification,
            syncRepository = sync,
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        val refreshesBeforeClose = verification.refreshActionQueueCalls

        vm.onEvent(LeadershipEvent.CloseVerificationDrive("sub-3"))
        advanceUntilIdle()
        // Dead-letter: FAILED, not a conflict, retries exhausted (attemptCount >= maxAttempts).
        sync.emit("outbox-1", SyncItemStatus.FAILED, conflict = false, attemptCount = 8, maxAttempts = 8)
        advanceUntilIdle()

        assertFalse(
            "R50-030: an exhausted-retry (dead-letter) close must still clear the spinner",
            vm.state.value.verificationClosures.single { it.submissionId == "sub-3" }.isQueueing,
        )
        assertEquals(
            "R50-030: dead-letter has nothing new to show -> must NOT trigger a refresh",
            refreshesBeforeClose,
            verification.refreshActionQueueCalls,
        )
    }

    private fun closureItem(submissionId: String) = VerificationQueueItem(
        itemId = "item-$submissionId",
        category = "vaccination_proof",
        shedLabel = "Shed 1",
        source = VerificationSourceRef(submissionId = submissionId),
    )
}

private class FakeControlTowerRepository : ControlTowerRepository {
    override suspend fun summary(
        parkId: String?, shedId: String?, workState: String?, severity: String?,
        dueBefore: String?, asOf: String?, cursor: String?, limit: Int?,
    ): ControlTowerResponseDto = error("unused")

    override fun observeSummary(
        parkId: String?, shedId: String?, workState: String?, severity: String?,
        dueBefore: String?, asOf: String?, cursor: String?, limit: Int?,
    ): Flow<Resource<ControlTowerResponseDto>> = flowOf(Resource(data = null))

    override suspend fun refreshSummary(
        parkId: String?, shedId: String?, workState: String?, severity: String?,
        dueBefore: String?, asOf: String?, cursor: String?, limit: Int?,
    ): Result<Unit> = Result.success(Unit)
}

private class FakeVaccinationInsightsRepository : VaccinationInsightsRepository {
    override suspend fun gaps(parkId: String?, limit: Int?, cursor: String?): VaccinationGapsResponseDto = error("unused")
    override fun observeGaps(parkId: String?, limit: Int?, cursor: String?): Flow<Resource<VaccinationGapsResponseDto>> =
        flowOf(Resource(data = null))
    override suspend fun refreshGaps(parkId: String?, limit: Int?, cursor: String?): Result<Unit> = Result.success(Unit)
    override suspend fun appendGaps(cursor: String, parkId: String?, limit: Int?): Result<Unit> = error("unused")
    override suspend fun coverage(parkId: String?, asOf: String?, dueBefore: String?, limit: Int?): VaccinationCoverageResponseDto = error("unused")
    override fun observeCoverage(parkId: String?, asOf: String?, dueBefore: String?, limit: Int?): Flow<Resource<VaccinationCoverageResponseDto>> =
        flowOf(Resource(data = null))
    override suspend fun refreshCoverage(parkId: String?, asOf: String?, dueBefore: String?, limit: Int?): Result<Unit> = Result.success(Unit)
}

/** Serves exactly one closure row (constant across refreshes) and counts
 *  [refreshActionQueue] calls so the test can assert a SUCCEEDED close refreshes the queue
 *  while a conflict/dead-letter close does not. */
private class FakeVerificationRepository(private val item: VerificationQueueItem) : VerificationRepository {
    var refreshActionQueueCalls = 0
        private set

    override suspend fun queue(category: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto = error("unused")
    override fun observeQueue(category: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> = error("unused")
    override suspend fun refreshQueue(category: String?, limit: Int?): Result<Unit> = error("unused")
    override suspend fun appendQueue(cursor: String, category: String?, limit: Int?): Result<Unit> = error("unused")

    override fun observeActionQueue(category: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> =
        flowOf(Resource(data = VerificationQueueResponseDto(items = listOf(item))))

    override suspend fun refreshActionQueue(category: String?, limit: Int?): Result<Unit> {
        refreshActionQueueCalls++
        return Result.success(Unit)
    }
}

/** Minimal [SyncRepository] test double driving [LeadershipViewModel.closeVerificationDrive]'s
 *  enqueue -> follow-to-terminal path. [emit] mirrors how the real outbox/[SyncEngine] would
 *  transition the row once enqueued. */
private class FakeCloseSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    var drainCount = 0
        private set
    private var nextId = 0

    override fun observeStatus(): StateFlow<SyncStatus> = status

    override suspend fun enqueueVerificationSubmissionClose(submissionId: String): AppResult<String> {
        val id = "outbox-${++nextId}"
        status.value = status.value.copy(
            items = status.value.items + SyncQueueItem(
                id = id,
                opType = "VERIFICATION_SUBMISSION_CLOSE",
                groupKey = submissionId,
                status = SyncItemStatus.QUEUED,
                attemptCount = 0,
                maxAttempts = 8,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                lastError = null,
            ),
        )
        return AppResult.Ok(id)
    }

    fun emit(
        itemId: String,
        newStatus: SyncItemStatus,
        conflict: Boolean = false,
        attemptCount: Int = 0,
        maxAttempts: Int = 8,
    ) {
        status.value = status.value.copy(
            items = status.value.items.map { item ->
                if (item.id == itemId) {
                    item.copy(status = newStatus, conflict = conflict, attemptCount = attemptCount, maxAttempts = maxAttempts)
                } else {
                    item
                }
            },
        )
    }

    override suspend fun triggerDrain() {
        drainCount++
    }

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueScanCapture(taskId: String, groupKey: String, idempotencyKey: String, request: ScanCaptureRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueScanAttempt(taskId: String, groupKey: String, idempotencyKey: String, request: ScanAttemptRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override fun observeItem(itemId: String): kotlinx.coroutines.flow.Flow<SyncQueueItem?> = status.map { s -> s.items.firstOrNull { it.id == itemId } }
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
}
