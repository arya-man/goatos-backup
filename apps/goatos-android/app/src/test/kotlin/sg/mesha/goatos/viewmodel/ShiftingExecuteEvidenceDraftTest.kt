package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.ShiftingActionsMeta
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
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
 * The reported failure (maintainer, 2026-07-30): record the shifting video, press Back, re-open the
 * SAME movement — the screen had forgotten the video and demanded a re-record, while the first clip
 * uploaded anyway.
 *
 * "Press Back and re-open" is modelled the way it actually happens: the destination leaves the
 * backstack, so a NEW ViewModel is built with a FRESH [SavedStateHandle]. Only durable storage can
 * carry the evidence across that boundary, which is what these tests pin.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShiftingExecuteEvidenceDraftTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `a recorded video survives leaving and re-entering the movement`() = runTest(dispatcher) {
        val repo = FakeShiftingPendingRepository()
        val drafts = FakeCaptureDraftRepository()
        val sync = FakeShiftingSyncRepository()
        val proofRepo = FakeProofCaptureRepository()

        val first = newViewModel(repo, drafts, sync, proofRepo)
        advanceUntilIdle()
        first.onEvent(ShiftingExecuteEvent.RecordVideo)
        advanceUntilIdle()
        assertTrue("the capture should mark the video recorded", first.state.value.videoCaptured)
        assertTrue("one video is all a low-priority move needs", first.state.value.canComplete)
        assertEquals(1, proofRepo.captureCalls.size)

        // Back: the destination is popped, so its SavedStateHandle is gone. Re-entry builds a new VM.
        val reentered = newViewModel(repo, drafts, sync, proofRepo)
        advanceUntilIdle()

        assertTrue("re-entering must keep the recorded video", reentered.state.value.videoCaptured)
        assertTrue("and must stay submittable", reentered.state.value.canComplete)
        assertEquals("re-entry must not ask for a second recording", 1, proofRepo.captureCalls.size)
    }

    @Test
    fun `completion after re-entry reuses the movement's idempotency key`() = runTest(dispatcher) {
        val repo = FakeShiftingPendingRepository()
        val drafts = FakeCaptureDraftRepository()
        val sync = FakeShiftingSyncRepository()

        val first = newViewModel(repo, drafts, sync)
        advanceUntilIdle()
        first.onEvent(ShiftingExecuteEvent.RecordVideo)
        advanceUntilIdle()
        first.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()
        val firstKey = sync.completeKeys.single()

        // The operator leaves and comes back while the completion is still draining, then taps
        // "Mark done" again. A re-minted key would let the server apply the movement TWICE.
        val reentered = newViewModel(repo, drafts, sync)
        advanceUntilIdle()
        reentered.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()

        assertEquals("every completion for one movement must carry the same key", listOf(firstKey, firstKey), sync.completeKeys)
    }

    @Test
    fun `an accepted completion clears the draft`() = runTest(dispatcher) {
        val repo = FakeShiftingPendingRepository()
        val drafts = FakeCaptureDraftRepository()
        val sync = FakeShiftingSyncRepository()

        val vm = newViewModel(repo, drafts, sync)
        advanceUntilIdle()
        vm.onEvent(ShiftingExecuteEvent.RecordVideo)
        advanceUntilIdle()
        vm.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()
        assertEquals(1, drafts.rows.size)

        sync.succeed(sync.completeItemIds.last())
        advanceUntilIdle()

        assertNull("a finished movement keeps no draft", drafts.rows[MOVEMENT_ID])
    }

    @Test
    fun `a high-priority move keeps each of its three videos across re-entry`() = runTest(dispatcher) {
        val repo = FakeShiftingPendingRepository(priority = "high")
        val drafts = FakeCaptureDraftRepository()
        val sync = FakeShiftingSyncRepository()
        val proofRepo = FakeProofCaptureRepository()

        val first = newViewModel(repo, drafts, sync, proofRepo)
        advanceUntilIdle()
        first.onEvent(ShiftingExecuteEvent.RecordVideo)
        advanceUntilIdle()
        first.onEvent(ShiftingExecuteEvent.RecordFeedPackingVideo)
        advanceUntilIdle()
        assertFalse("two of three videos is not submittable", first.state.value.canComplete)

        val reentered = newViewModel(repo, drafts, sync, proofRepo)
        advanceUntilIdle()
        assertTrue(reentered.state.value.videoCaptured)
        assertTrue(reentered.state.value.feedPackingVideoCaptured)
        assertFalse("the feed-given video is still missing", reentered.state.value.feedGivenVideoCaptured)

        reentered.onEvent(ShiftingExecuteEvent.RecordFeedGivenVideo)
        advanceUntilIdle()
        assertTrue("all three recorded — now submittable", reentered.state.value.canComplete)
        assertEquals(3, proofRepo.captureCalls.size)
    }

    private fun newViewModel(
        repo: FakeShiftingPendingRepository,
        drafts: FakeCaptureDraftRepository,
        sync: FakeShiftingSyncRepository,
        proofCaptureRepository: FakeProofCaptureRepository = FakeProofCaptureRepository(),
    ) = ShiftingExecuteViewModel(
        repo = repo,
        drafts = drafts,
        syncRepository = sync,
        proofCaptureSource = AlwaysCapturingProofSource(),
        proofCaptureRepository = proofCaptureRepository,
        analytics = NoopEvidenceAnalytics(),
        crashReporter = NoopEvidenceCrashReporter(),
        // A FRESH handle every time: this is what "Back then re-open" does to the destination.
        savedStateHandle = SavedStateHandle(mapOf("shifting_event_id" to MOVEMENT_ID)),
    )

    private companion object {
        const val MOVEMENT_ID = "11111111-1111-4111-8111-111111111111"
        const val DEST_SHED_ID = "22222222-2222-4222-8222-222222222222"
    }
}

/** In-memory stand-in for the Room-backed draft store plus the cached movement. */
private class FakeShiftingPendingRepository(
    private val priority: String = "low",
) : ShiftingPendingRepository {
    override val actionsMeta: StateFlow<ShiftingActionsMeta> = MutableStateFlow(ShiftingActionsMeta())

    override fun pending(date: String, status: String): Flow<PagingData<CountsShiftingPendingExecutionItemDto>> =
        flowOf(PagingData.empty())

    override suspend fun forgetExecuted(shiftingEventId: String) = Unit

    override suspend fun findCached(shiftingEventId: String) = CountsShiftingPendingExecutionItemDto(
        shiftingEventId = shiftingEventId,
        eventStatus = "authorized",
        primaryActionKey = "execute",
        priority = priority,
        category = "growth",
        destinationShedId = "22222222-2222-4222-8222-222222222222",
        destinationShedName = "Gandhi 2",
        sourceShedName = "Gandhi 1",
        animalCount = 4,
        feedRequirement = if (priority == "high") {
            sg.mesha.goatos.core.network.dto.CountsShiftingFeedRequirementDto(
                status = "ready",
                fingerprint = "fp-1",
            )
        } else {
            null
        },
    )

}

/** In-memory stand-in for the Room-backed shared capture-draft store. */
private class FakeCaptureDraftRepository : CaptureDraftRepository {
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
        publish()
    }

    override suspend fun clearProof(flowKey: String, entityId: String, step: String) {
        val current = rows[entityId] ?: return
        rows[entityId] = current.copy(proofs = current.proofs - step)
        publish()
    }

    override suspend fun putSubmit(
        flowKey: String,
        entityId: String,
        idempotencyKey: String?,
        outboxItemId: String?,
    ) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(submitIdempotencyKey = idempotencyKey, submitOutboxItemId = outboxItemId)
        publish()
    }

    override suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(answers = answers)
        publish()
    }

    override suspend fun clear(flowKey: String, entityId: String) {
        rows.remove(entityId)
        publish()
    }

    private fun publish() {
        progress.value = rows.mapValues { it.value.capturedCount }
    }
}

private class FakeShiftingSyncRepository : SyncRepository {
    var proofEnqueues: Int = 0
        private set
    val completeKeys = mutableListOf<String>()
    val completeItemIds = mutableListOf<String>()
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    fun succeed(itemId: String) {
        val now = 1_700_000_000_000
        status.value = SyncStatus(
            online = true,
            pendingCount = 0,
            inFlightCount = 0,
            failedCount = 0,
            deadLetterCount = 0,
            lastSyncAt = now,
            items = listOf(
                SyncQueueItem(
                    id = itemId,
                    idempotencyKey = "test-idempotency-key",
                    opType = "SHIFTING_COMPLETE",
                    groupKey = "group",
                    status = sg.mesha.goatos.core.data.sync.SyncItemStatus.SUCCEEDED,
                    attemptCount = 1,
                    maxAttempts = 5,
                    conflict = false,
                    createdAt = now,
                    updatedAt = now,
                    lastError = null,
                ),
            ),
        )
    }

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
    ): AppResult<String> {
        completeKeys += idempotencyKey
        val id = "complete-${completeKeys.size}"
        completeItemIds += id
        return AppResult.Ok(id)
    }

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}

private class AlwaysCapturingProofSource : ProofCaptureSource {
    private var count = 0
    override suspend fun captureVideo(captureContext: ProofCaptureContext?): CapturedVideo {
        count += 1
        return CapturedVideo(
            localUri = "file:///tmp/clip-$count.mp4",
            mimeType = "video/mp4",
            captureSource = "in_app_camera",
            startedAtMs = 1_700_000_000_000,
            endedAtMs = 1_700_000_005_000,
        )
    }

    override suspend fun pickVideo(): CapturedVideo = captureVideo()
}

private class NoopEvidenceAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class NoopEvidenceCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}

/**
 * The Actions list ("back page") must say how much of each task is done — total required videos and
 * how many are recorded — so an operator scanning the list can see what is part-finished without
 * opening every row (maintainer request 2026-07-30).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShiftingActionsEvidenceProgressTest {
    @Test
    fun `a low-priority task needs one video and a high-priority task needs three`() {
        val low = row(priority = "Low")
        val high = row(priority = "High")

        assertEquals(1, low.withProgress(capturedCount = 0).videosRequired)
        assertEquals(3, high.withProgress(capturedCount = 0).videosRequired)
        assertEquals(0, high.withProgress(capturedCount = 0).videosCaptured)
    }

    @Test
    fun `progress counts only the videos this movement has recorded`() {
        val progressed = row(priority = "High").withProgress(capturedCount = 2)

        assertEquals(2, progressed.videosCaptured)
        assertEquals(3, progressed.videosRequired)
        assertTrue("an operator-actionable task shows its progress", progressed.showsEvidenceProgress)
    }

    @Test
    fun `a finished movement shows no progress`() {
        val done = row(priority = "Low", primaryActionKey = "none").withProgress(capturedCount = 0)
        assertFalse(done.showsEvidenceProgress)
    }

    /** Mirrors ShiftingPendingViewModel.withEvidenceProgress, which is private to the ViewModel. */
    private fun sg.mesha.goatos.feature.counts.ShiftingPendingRowUi.withProgress(
        capturedCount: Int,
    ): sg.mesha.goatos.feature.counts.ShiftingPendingRowUi {
        val required = if (priority.equals("high", ignoreCase = true)) 3 else 1
        return copy(videosRequired = required, videosCaptured = minOf(capturedCount, required))
    }

    private fun row(priority: String, primaryActionKey: String = "execute") =
        sg.mesha.goatos.feature.counts.ShiftingPendingRowUi(
            shiftingEventId = "move-1",
            sourceLabel = "Gandhi 1",
            destinationLabel = "Gandhi 2",
            priority = priority,
            category = "Growth",
            animalCount = 4,
            approvedAtLabel = "2026-07-30",
            primaryActionKey = primaryActionKey,
        )
}
