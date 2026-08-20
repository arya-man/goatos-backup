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
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.ShiftingActionsMeta
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.counts.ShiftingExecuteEvent

/**
 * Codex P1: [ShiftingExecuteViewModel.reRecord] used to delete the old outbox proof and clear the
 * draft BEFORE opening the camera for the replacement, so a cancel/failure after that discard lost
 * the old take outright. The fix routes re-record through `captureVideo(..., replacing = true)`,
 * which durably captures the new clip via
 * [sg.mesha.goatos.core.data.capture.ProofCaptureRepository.captureReplacingLatest] FIRST and only
 * then re-points the draft at it -- the repository itself only discards the old row/outbox item
 * after the new capture returns [AppResult.Ok] (Manohar ordering). Mirrors the same-contract
 * conversions pinned by `FeedTransportRecaptureOrderingTest` and `MilkFeedingViewModelTest`.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShiftingExecuteRecaptureOrderingTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun newViewModel(
        proofSource: QueuedProofCaptureSource,
        proofCaptureRepository: FakeProofCaptureRepository,
        drafts: OrderingFakeCaptureDraftRepository,
        sync: OrderingFakeShiftingSyncRepository,
    ) = ShiftingExecuteViewModel(
        repo = OrderingFakeShiftingPendingRepository(),
        drafts = drafts,
        syncRepository = sync,
        proofCaptureSource = proofSource,
        proofCaptureRepository = proofCaptureRepository,
        analytics = OrderingNoopAnalytics(),
        crashReporter = OrderingNoopCrashReporter(),
        savedStateHandle = SavedStateHandle(mapOf("shifting_event_id" to MOVEMENT_ID)),
    )

    @Test
    fun `camera cancel on re-record leaves the old proof untouched`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        // ONE clip queued: the first RecordVideo consumes it, so the re-record's camera call
        // returns null -- exactly what a cancelled capture looks like.
        val proofSource = QueuedProofCaptureSource(
            mutableListOf(CapturedVideo(localUri = "file:///old-clip.mp4", startedAtMs = 0L, endedAtMs = 1_000L)),
        )
        val drafts = OrderingFakeCaptureDraftRepository()
        val sync = OrderingFakeShiftingSyncRepository()
        val viewModel = newViewModel(proofSource, proofCaptureRepository, drafts, sync)
        advanceUntilIdle()

        viewModel.onEvent(ShiftingExecuteEvent.RecordVideo)
        advanceUntilIdle()
        assertTrue("first capture must succeed", viewModel.state.value.videoCaptured)
        assertEquals(1, proofCaptureRepository.captureCalls.size)

        viewModel.onEvent(ShiftingExecuteEvent.ReRecordVideo)
        advanceUntilIdle()

        assertEquals(
            "a cancelled re-record must not write a second proof",
            1,
            proofCaptureRepository.captureCalls.size,
        )
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals(
            "the old proof row must survive a cancelled re-record",
            1,
            survivingRows.size,
        )
        assertEquals("file:///old-clip.mp4", survivingRows.first().localUri)
        assertTrue("the old proof must remain reported as captured", viewModel.state.value.videoCaptured)
        assertEquals(
            "the draft must still point at the old proof's outbox item",
            1,
            drafts.rows[MOVEMENT_ID]?.proofs?.size ?: 0,
        )
    }

    @Test
    fun `a failed re-record keeps the old proof visible and submittable`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofSource = QueuedProofCaptureSource(
            mutableListOf(
                CapturedVideo(localUri = "file:///old-clip.mp4", startedAtMs = 0L, endedAtMs = 1_000L),
                CapturedVideo(localUri = "file:///new-clip.mp4", startedAtMs = 2_000L, endedAtMs = 3_000L),
            ),
        )
        val drafts = OrderingFakeCaptureDraftRepository()
        val sync = OrderingFakeShiftingSyncRepository()
        val viewModel = newViewModel(proofSource, proofCaptureRepository, drafts, sync)
        advanceUntilIdle()

        viewModel.onEvent(ShiftingExecuteEvent.RecordVideo)
        advanceUntilIdle()
        assertTrue(viewModel.state.value.videoCaptured)
        assertEquals(1, proofCaptureRepository.captureCalls.size)

        // The re-record's camera call succeeds, but the repository capture/enqueue call fails --
        // the old row must not have been discarded first.
        proofCaptureRepository.failNextCapture = true
        viewModel.onEvent(ShiftingExecuteEvent.ReRecordVideo)
        advanceUntilIdle()

        assertEquals(
            "a failed re-record must not delete the old proof",
            1,
            proofCaptureRepository.captureCalls.size,
        )
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals(
            "the old proof row must survive a failed re-record",
            1,
            survivingRows.size,
        )
        assertEquals("file:///old-clip.mp4", survivingRows.first().localUri)
        assertTrue(
            "the old proof must still read as captured after a failed re-record",
            viewModel.state.value.videoCaptured,
        )
        assertEquals(
            "the draft must still point at the old proof's outbox item, not be cleared on a failed retake",
            1,
            drafts.rows[MOVEMENT_ID]?.proofs?.size ?: 0,
        )
    }

    @Test
    fun `a successful re-record ends with exactly the new proof active`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofSource = QueuedProofCaptureSource(
            mutableListOf(
                CapturedVideo(localUri = "file:///old-clip.mp4", startedAtMs = 0L, endedAtMs = 1_000L),
                CapturedVideo(localUri = "file:///new-clip.mp4", startedAtMs = 2_000L, endedAtMs = 3_000L),
            ),
        )
        val drafts = OrderingFakeCaptureDraftRepository()
        val sync = OrderingFakeShiftingSyncRepository()
        val viewModel = newViewModel(proofSource, proofCaptureRepository, drafts, sync)
        advanceUntilIdle()

        viewModel.onEvent(ShiftingExecuteEvent.RecordVideo)
        advanceUntilIdle()
        assertEquals(1, proofCaptureRepository.captureCalls.size)

        viewModel.onEvent(ShiftingExecuteEvent.ReRecordVideo)
        advanceUntilIdle()

        assertEquals(
            "a successful re-record must issue a second capture call",
            2,
            proofCaptureRepository.captureCalls.size,
        )
        // P1 FIX: captureReplacingLatest defers removal of old rows until the new row reaches SYNCED.
        proofCaptureRepository.driveAllPendingRetirements()
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals(
            "exactly the new proof must remain active after a successful re-record",
            1,
            survivingRows.size,
        )
        assertEquals("file:///new-clip.mp4", survivingRows.first().localUri)
        assertTrue(viewModel.state.value.videoCaptured)
        assertEquals(
            "the draft must point at exactly one outbox item after a successful re-record",
            1,
            drafts.rows[MOVEMENT_ID]?.proofs?.size ?: 0,
        )
    }

    private companion object {
        const val MOVEMENT_ID = "11111111-1111-4111-8111-111111111111"
    }
}

/** Returns each queued video in order, then `null` for every call after the queue is empty --
 *  models a cancelled/empty camera result without needing the full [ProofCaptureSource] surface. */
private class QueuedProofCaptureSource(
    private val results: MutableList<CapturedVideo>,
) : ProofCaptureSource {
    override suspend fun captureVideo(captureContext: ProofCaptureContext?): CapturedVideo? =
        if (results.isNotEmpty()) results.removeAt(0) else null

    override suspend fun pickVideo(): CapturedVideo? = captureVideo()
}

private class OrderingFakeShiftingPendingRepository : ShiftingPendingRepository {
    override val actionsMeta: StateFlow<ShiftingActionsMeta> = MutableStateFlow(ShiftingActionsMeta())

    override fun pending(date: String, status: String): Flow<PagingData<CountsShiftingPendingExecutionItemDto>> =
        flowOf(PagingData.empty())

    override suspend fun forgetExecuted(shiftingEventId: String) = Unit

    override suspend fun findCached(shiftingEventId: String) = CountsShiftingPendingExecutionItemDto(
        shiftingEventId = shiftingEventId,
        eventStatus = "authorized",
        primaryActionKey = "execute",
        priority = "low",
        category = "growth",
        destinationShedId = "22222222-2222-4222-8222-222222222222",
        destinationShedName = "Gandhi 2",
        sourceShedName = "Gandhi 1",
        animalCount = 4,
        feedRequirement = null,
    )
}

private class OrderingFakeCaptureDraftRepository : CaptureDraftRepository {
    val rows = mutableMapOf<String, CaptureDraft>()
    private val progress = MutableStateFlow<Map<String, Int>>(emptyMap())

    override suspend fun find(flowKey: String, entityId: String): CaptureDraft =
        rows[entityId] ?: CaptureDraft()

    override fun observe(flowKey: String, entityId: String): Flow<CaptureDraft> =
        MutableStateFlow(rows[entityId] ?: CaptureDraft())

    override fun observeProgress(flowKey: String, limit: Int): Flow<Map<String, Int>> = progress

    override suspend fun putProof(
        flowKey: String,
        entityId: String,
        step: String,
        outboxItemId: String,
        fingerprint: String?,
    ) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(
            proofs = current.proofs + (step to outboxItemId),
            fingerprint = fingerprint ?: current.fingerprint,
        )
    }

    override suspend fun clearProof(flowKey: String, entityId: String, step: String) {
        val current = rows[entityId] ?: return
        rows[entityId] = current.copy(proofs = current.proofs - step)
    }

    override suspend fun putSubmit(
        flowKey: String,
        entityId: String,
        idempotencyKey: String?,
        outboxItemId: String?,
    ) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(submitIdempotencyKey = idempotencyKey, submitOutboxItemId = outboxItemId)
    }

    override suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(answers = answers)
    }

    override suspend fun clear(flowKey: String, entityId: String) {
        rows.remove(entityId)
    }
}

private class OrderingFakeShiftingSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private var proofEnqueues = 0

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> {
        proofEnqueues += 1
        return AppResult.Ok("proof-$proofEnqueues")
    }

    override suspend fun enqueueShiftingComplete(
        groupKey: String,
        idempotencyKey: String,
        destinationTag: String?,
        proofOutboxItemId: String,
        feedPackingProofOutboxItemId: String?,
        feedGivenProofOutboxItemId: String?,
        feedConfigFingerprint: String?,
    ): AppResult<String> = AppResult.Ok("complete-1")

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}

private class OrderingNoopAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class OrderingNoopCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}
