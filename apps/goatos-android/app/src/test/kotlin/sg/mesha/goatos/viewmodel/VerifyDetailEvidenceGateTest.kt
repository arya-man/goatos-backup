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
import sg.mesha.goatos.core.network.dto.VerificationStatus
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.verify.VerifyDecisionUnavailableReason
import sg.mesha.goatos.feature.verify.VerifyDetailEvent
import sg.mesha.goatos.feature.verify.VideoPlaybackAction

/**
 * The client half of the verdict evidence gate.
 *
 * A signed download URL is a STRING, not evidence: the stored object behind it can be gone while
 * the link still resolves. Approve is the only irreversible action in this app (there is no
 * un-approve), so it must not stay enabled on evidence the verifier could not actually watch —
 * while Reject/rework must stay open so she is never stranded.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class VerifyDetailEvidenceGateTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModel(repo: VerificationRepository, sync: GateSyncRepository = GateSyncRepository()) =
        VerifyDetailViewModel(
            repo = repo,
            syncRepo = sync,
            analytics = GateAnalytics(),
            crashReporter = GateCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "vaccination_proof")),
        )

    @Test
    fun `approve stays enabled while every proof is linked and nothing has failed to play`() = runTest(dispatcher) {
        val vm = viewModel(GateRepository())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue("approve should be available on a healthy pending item", vm.state.value.isApproveEnabled)
        assertTrue(vm.state.value.isRejectEnabled)
        assertEquals(VerifyDecisionUnavailableReason.NONE, vm.state.value.decisionUnavailableReason)
    }

    @Test
    fun `a proof that fails to play disables approve but never reject`() = runTest(dispatcher) {
        val vm = viewModel(GateRepository())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = "proof-2",
                mimeType = "video/mp4",
                action = VideoPlaybackAction.PLAYBACK_ERROR,
                reason = "source error",
            ),
        )
        advanceUntilIdle()

        assertFalse("approve must go dead once a proof will not play", vm.state.value.isApproveEnabled)
        assertTrue("reject/rework must stay open — it is the only correct move left", vm.state.value.isRejectEnabled)
        assertEquals(VerifyDecisionUnavailableReason.EVIDENCE_UNAVAILABLE, vm.state.value.decisionUnavailableReason)
    }

    @Test
    fun `an approve event is not enqueued once the evidence is unplayable`() = runTest(dispatcher) {
        val sync = GateSyncRepository()
        val vm = viewModel(GateRepository(), sync)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = "proof-1",
                mimeType = "video/mp4",
                action = VideoPlaybackAction.PLAYBACK_ERROR,
                reason = "source error",
            ),
        )
        advanceUntilIdle()
        vm.onEvent(VerifyDetailEvent.Approve())
        advanceUntilIdle()

        assertEquals("no approve may reach the outbox", emptyList<String>(), sync.enqueued)

        // ...and rework still goes through from the same screen state.
        vm.onEvent(VerifyDetailEvent.Reject("proof video will not play, please record it again"))
        advanceUntilIdle()
        assertEquals(listOf("rejected"), sync.enqueued)
    }

    @Test
    fun `a partially linked item cannot be approved - one blank url is a missing proof`() = runTest(dispatcher) {
        val vm = viewModel(GateRepository(secondDownloadUrl = ""))
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertFalse(
            "any() would have approved an item whose second proof link never resolved",
            vm.state.value.isApproveEnabled,
        )
        assertTrue(vm.state.value.isRejectEnabled)
    }

    @Test
    fun `an already decided item enables neither decision`() = runTest(dispatcher) {
        val vm = viewModel(GateRepository(status = VerificationStatus.APPROVED))
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertFalse(vm.state.value.isApproveEnabled)
        assertFalse(vm.state.value.isRejectEnabled)
        assertEquals(VerifyDecisionUnavailableReason.ALREADY_DECIDED, vm.state.value.decisionUnavailableReason)
    }
}

private class GateRepository(
    secondDownloadUrl: String = "/proof-2.mp4",
    status: String = VerificationStatus.PENDING,
) : VerificationRepository {
    private val item = VerificationQueueItem(
        itemId = "item-1",
        category = "vaccination_proof",
        status = status,
        rowVersion = 3,
        // The server says a link was issued for every media_ref — which is exactly the claim that
        // does NOT prove the bytes are there.
        evidenceAvailable = true,
        media = listOf(
            VerificationMediaItem(proofId = "proof-1", downloadUrl = "/proof-1.mp4", mimeType = "video/mp4", durationMs = 9_000),
            VerificationMediaItem(proofId = "proof-2", downloadUrl = secondDownloadUrl, mimeType = "video/mp4", durationMs = 7_000),
        ),
    )
    private val response = VerificationQueueResponseDto(items = listOf(item))

    override suspend fun queue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto = response

    override fun observeQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> = flowOf(Resource(data = response))

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

private class GateSyncRepository : SyncRepository {
    val enqueued = mutableListOf<String>()
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf()
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> {
        enqueued += decision
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

private class GateAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) = Unit
    override fun setUserProperty(name: String, value: String?) = Unit
    override fun setUserId(id: String?) = Unit
}

private class GateCrashReporter : CrashReporter {
    override fun log(message: String) = Unit
    override fun recordException(throwable: Throwable, message: String?) = Unit
    override fun setCustomKey(key: String, value: String) = Unit
}
