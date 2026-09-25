package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
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
import sg.mesha.goatos.boot.NavStateRefreshSignal
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
        assertEquals(setOf(REQUEST_ID), viewModel.state.value.decidingRequestIds)

        syncRepository.emitTerminalFailure(REQUEST_ID, "Server rejected this decision.")
        advanceUntilIdle()

        assertNull(repository.forgottenRequestId)
        assertTrue(viewModel.state.value.decidingRequestIds.isEmpty())
        assertTrue(viewModel.state.value.cardMessages.getValue(REQUEST_ID).isError)
    }

    @Test
    fun `a decided request re-reads the badges so the Approvals count drops at once`() = runTest(dispatcher) {
        // Live phone run 2026-09-25: after an approve the Approvals badge stayed at 22 while the
        // server said 21, until another screen happened to refresh navigation.
        val syncRepository = ApprovalSyncRepository()
        val navRefresh = NavStateRefreshSignal()
        val requests = mutableListOf<Unit>()
        val collector = launch { navRefresh.requests.collect { requests += it } }
        val viewModel = ApprovalViewModel(
            approvalRepository = RecordingApprovalRepository(),
            syncRepository = syncRepository,
            analytics = NoopApprovalAnalytics(),
            crashReporter = NoopApprovalCrashReporter(),
            savedStateHandle = SavedStateHandle(),
            navRefresh = navRefresh,
        )
        viewModel.onEvent(ApprovalEvent.Approve(REQUEST_ID))
        advanceUntilIdle()
        assertTrue("queuing alone must not claim the count changed", requests.isEmpty())
        syncRepository.emitSucceeded(REQUEST_ID)
        advanceUntilIdle()
        assertEquals("a landed decision must ask for the badges once", 1, requests.size)
        collector.cancel()
    }

    // ---- Approvals defects, live E2E 2026-09-25 ----------------------------------------------

    @Test
    fun `a refusal is shown on the card that was decided, in farm words keyed on the server code`() = runTest(dispatcher) {
        val syncRepository = ApprovalSyncRepository()
        val viewModel = newViewModel(syncRepository)

        viewModel.onEvent(ApprovalEvent.Approve(REQUEST_ID))
        advanceUntilIdle()
        syncRepository.emitTerminalFailure(
            REQUEST_ID,
            message = "every step of the death report (its videos, photos and answers) must be recorded before approval",
            code = "death_evidence_incomplete",
        )
        advanceUntilIdle()

        val onCard = viewModel.state.value.cardMessages[REQUEST_ID]
        assertEquals("All the death report steps must be recorded before it can be approved.", onCard?.message)
        assertTrue(onCard!!.isError)
        assertNull("a card's refusal is not a list-level banner", viewModel.state.value.message)
    }

    @Test
    fun `known refusal codes map to farm copy and anything else to a plain retry line`() {
        assertEquals("This request was already decided or changed.", approvalRefusalMessage("approval_already_decided"))
        assertEquals("This request was already decided or changed.", approvalRefusalMessage("idempotency_conflict"))
        assertEquals("You can't decide this request.", approvalRefusalMessage("permission_denied"))
        assertEquals("You can't decide this request.", approvalRefusalMessage("park_scope_forbidden"))
        assertEquals("Couldn't save the decision. Try again.", approvalRefusalMessage(null))
        assertEquals("Couldn't save the decision. Try again.", approvalRefusalMessage("something_new"))
    }

    @Test
    fun `offline, deciding one card never swallows a tap on another card`() = runTest(dispatcher) {
        val syncRepository = ApprovalSyncRepository()
        val viewModel = newViewModel(syncRepository)

        viewModel.onEvent(ApprovalEvent.Approve(REQUEST_ID))
        advanceUntilIdle()
        // The first decision is still queued (no network): a tap on a different card must queue too.
        viewModel.onEvent(ApprovalEvent.Approve(OTHER_REQUEST_ID))
        advanceUntilIdle()

        assertEquals(listOf(REQUEST_ID, OTHER_REQUEST_ID), syncRepository.enqueuedRequestIds)
        assertEquals(setOf(REQUEST_ID, OTHER_REQUEST_ID), viewModel.state.value.decidingRequestIds)

        // A second tap on the SAME card while its decision is on its way is still held.
        viewModel.onEvent(ApprovalEvent.Approve(REQUEST_ID))
        advanceUntilIdle()
        assertEquals(2, syncRepository.enqueuedRequestIds.size)

        // Each decision settles on its own.
        syncRepository.emitSucceeded(OTHER_REQUEST_ID)
        advanceUntilIdle()
        assertEquals(setOf(REQUEST_ID), viewModel.state.value.decidingRequestIds)
    }

    @Test
    fun `raised-at renders DD-MM-YYYY HH-MM in IST and an unparseable value verbatim`() {
        assertEquals("25/09/2026 13:39", formatRaisedAt("2026-09-25T08:09:00Z"))
        assertEquals("not-a-time", formatRaisedAt("not-a-time"))
    }

    private fun newViewModel(syncRepository: ApprovalSyncRepository) = ApprovalViewModel(
        approvalRepository = RecordingApprovalRepository(),
        syncRepository = syncRepository,
        analytics = NoopApprovalAnalytics(),
        crashReporter = NoopApprovalCrashReporter(),
        savedStateHandle = SavedStateHandle(),
    )

    // ---- ApprovalCapturedRowsTest (SHIFTING SOP, 2026-09-16) ---------------------------------
    // A shifting raise capture rides the SAME renderer as a birth/death report capture: tapping it
    // resolves one signed URL into the screen state, and a second tap never re-fetches.

    @Test
    fun `a tapped raise capture resolves its signed url once into the shared capture renderer`() = runTest(dispatcher) {
        val repository = RecordingApprovalRepository()
        val viewModel = ApprovalViewModel(
            approvalRepository = repository,
            syncRepository = ApprovalSyncRepository(),
            analytics = NoopApprovalAnalytics(),
            crashReporter = NoopApprovalCrashReporter(),
            savedStateHandle = SavedStateHandle(),
        )
        viewModel.onEvent(ApprovalEvent.OpenCaptureMedia("proof-r1"))
        advanceUntilIdle()
        viewModel.onEvent(ApprovalEvent.OpenCaptureMedia("proof-r1"))
        advanceUntilIdle()
        assertEquals(listOf("proof-r1"), repository.resolved)
        assertEquals("https://signed/proof-r1", viewModel.state.value.openedMediaUrls["proof-r1"])
    }

    private companion object {
        const val REQUEST_ID = "approval-1"
        const val OTHER_REQUEST_ID = "approval-2"
    }
}

private class RecordingApprovalRepository : CountsApprovalRepository {
    var forgottenRequestId: String? = null

    override fun approvals(status: String): Flow<PagingData<CountsApprovalListItemDto>> =
        flowOf(PagingData.empty())

    override suspend fun forgetDecided(approvalRequestId: String) {
        forgottenRequestId = approvalRequestId
    }

    val resolved = mutableListOf<String>()
    override suspend fun proofDownloadUrl(proofId: String): String? {
        resolved += proofId
        return "https://signed/$proofId"
    }
}

private class ApprovalSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()
    val enqueuedRequestIds = mutableListOf<String>()

    private fun flowFor(itemId: String) = items.getOrPut(itemId) { MutableStateFlow(null) }

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowFor(itemId)

    override suspend fun enqueueCountsApprovalDecision(
        requestId: String,
        approve: Boolean,
        reason: String?,
        idempotencyKey: String,
    ): AppResult<String> {
        enqueuedRequestIds += requestId
        return AppResult.Ok(outboxId(requestId))
    }

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit

    fun emitTerminalFailure(requestId: String, message: String, code: String? = null) {
        flowFor(outboxId(requestId)).value = item(requestId, SyncItemStatus.FAILED).copy(lastError = message, lastErrorCode = code)
    }

    fun emitSucceeded(requestId: String) {
        flowFor(outboxId(requestId)).value = item(requestId, SyncItemStatus.SUCCEEDED)
    }

    private fun item(requestId: String, status: SyncItemStatus) = SyncQueueItem(
        id = outboxId(requestId),
        opType = "COUNTS_APPROVAL_APPROVE",
        idempotencyKey = "counts-approval-approve:$requestId",
        groupKey = requestId,
        status = status,
        attemptCount = 1,
        maxAttempts = 3,
        conflict = status == SyncItemStatus.FAILED,
        createdAt = 1L,
        updatedAt = 2L,
        lastError = null,
    )

    private fun outboxId(requestId: String) = "outbox-$requestId"
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
