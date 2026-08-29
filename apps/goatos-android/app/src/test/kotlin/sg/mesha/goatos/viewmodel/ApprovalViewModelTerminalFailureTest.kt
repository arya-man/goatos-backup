package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsApprovalRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.CountsApprovalListItemDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.counts.ApprovalEvent

@OptIn(ExperimentalCoroutinesApi::class)
class ApprovalViewModelTerminalFailureTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `terminal decision failure keeps the approval cached and makes it actionable again`() = runTest(dispatcher) {
        val repository = RecordingApprovalRepository()
        val syncRepository = ApprovalSyncRepository()
        val viewModel = ApprovalViewModel(
            approvalRepository = repository,
            syncRepository = syncRepository,
            analytics = NoopApprovalAnalytics(),
            crashReporter = NoopApprovalCrashReporter(),
            savedStateHandle = SavedStateHandle(),
        )

        viewModel.onEvent(ApprovalEvent.Approve(REQUEST_ID))
        advanceUntilIdle()

        assertNull("enqueue alone must not destructively delete the cached row", repository.forgottenRequestId)
        assertEquals(REQUEST_ID, viewModel.state.value.decidingRequestId)

        syncRepository.emitTerminalFailure("Server rejected this decision.")
        advanceUntilIdle()

        assertNull(repository.forgottenRequestId)
        assertNull(viewModel.state.value.decidingRequestId)
        assertTrue(viewModel.state.value.isError)
    }

    private companion object {
        const val REQUEST_ID = "approval-1"
    }
}

private class RecordingApprovalRepository : CountsApprovalRepository {
    var forgottenRequestId: String? = null

    override fun approvals(status: String): Flow<PagingData<CountsApprovalListItemDto>> =
        flowOf(PagingData.empty())

    override suspend fun forgetDecided(approvalRequestId: String) {
        forgottenRequestId = approvalRequestId
    }
}

private class ApprovalSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val item = MutableStateFlow<SyncQueueItem?>(null)

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = item

    override suspend fun enqueueCountsApprovalDecision(
        requestId: String,
        approve: Boolean,
        reason: String?,
        idempotencyKey: String,
    ): AppResult<String> = AppResult.Ok(OUTBOX_ID)

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit

    fun emitTerminalFailure(message: String) {
        item.value = SyncQueueItem(
            id = OUTBOX_ID,
            opType = "COUNTS_APPROVAL_APPROVE",
            idempotencyKey = "approval-key",
            groupKey = "approval-1",
            status = SyncItemStatus.FAILED,
            attemptCount = 1,
            maxAttempts = 3,
            conflict = true,
            createdAt = 1L,
            updatedAt = 2L,
            lastError = message,
        )
    }

    private companion object {
        const val OUTBOX_ID = "approval-outbox-1"
    }
}

private class NoopApprovalAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) = Unit
    override fun setUserProperty(name: String, value: String?) = Unit
    override fun setUserId(id: String?) = Unit
}

private class NoopApprovalCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) = Unit
    override fun log(message: String) = Unit
    override fun setCustomKey(key: String, value: String) = Unit
}
