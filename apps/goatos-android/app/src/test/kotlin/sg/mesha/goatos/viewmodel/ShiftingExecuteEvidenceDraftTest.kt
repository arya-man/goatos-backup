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
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.ShiftingActionsMeta
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.CountsShiftingFeedRequirementDto
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ShiftingSopCardDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto
import sg.mesha.goatos.core.network.dto.WeighingSopOptionDto
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto
import sg.mesha.goatos.feature.counts.SECTION_COMPLETION
import sg.mesha.goatos.feature.counts.SECTION_HIGH_PRIORITY
import sg.mesha.goatos.feature.counts.ShiftingExecuteEvent

/**
 * The Shifting EXECUTE screen follows the movement's PINNED shifting SOP (2026-09-16): the completion
 * card, plus the high-priority card for a high movement. These pin, through the real ViewModel and
 * slot controllers:
 *  - a recorded capture survives Back + re-open (a NEW ViewModel with a FRESH SavedStateHandle);
 *  - every completion for one movement carries ONE stable key, and an accepted one clears the drafts;
 *  - the screen renders the pinned card (an authored `either` slot, a required question gating submit);
 *  - a clip an older build stored under `shifting` / `packing` / `feeding` fills its seeded slot;
 *  - a changed Feed Config empties ONLY the high-priority card;
 *  - a verifier rework empties both cards once, keeps the answers, and never re-fills a slot with the
 *    rejected take -- while a clip recorded after the rework survives re-opening.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShiftingExecuteEvidenceDraftTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() {
        ShiftingExecuteViewModel.clock = System::currentTimeMillis
        Dispatchers.resetMain()
    }

    private fun capture(vm: ShiftingExecuteViewModel, key: String, section: String = SECTION_COMPLETION, kind: String? = null) =
        vm.onEvent(ShiftingExecuteEvent.CaptureSlot(section, key, kind))

    @Test
    fun `a recorded video survives leaving and re-entering the movement`() = runTest(dispatcher) {
        val repo = FakeShiftingPendingRepository()
        val drafts = FakeCaptureDraftRepository()
        val sync = FakeShiftingSyncRepository()
        val proofRepo = FakeProofCaptureRepository()

        val first = newViewModel(repo, drafts, sync, proofRepo)
        advanceUntilIdle()
        capture(first, SEEDED_VIDEO)
        advanceUntilIdle()
        assertTrue("the capture should mark the slot recorded", first.state.value.completionCard.slots.single().captured)
        assertTrue("one video is all a low-priority seeded move needs", first.state.value.canComplete)
        assertEquals(1, proofRepo.captureCalls.size)

        val reentered = newViewModel(repo, drafts, sync, proofRepo)
        advanceUntilIdle()
        assertTrue("re-entering must keep the recorded video", reentered.state.value.completionCard.slots.single().captured)
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
        capture(first, SEEDED_VIDEO)
        advanceUntilIdle()
        first.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()
        val firstKey = sync.completeKeys.single()
        // A seeded submission with no answers keeps the pre-SOP key.
        assertEquals("counts-shifting-complete:$MOVEMENT_ID", firstKey)
        assertEquals(setOf(SEEDED_VIDEO), sync.lastSlotProofs.keys)

        val reentered = newViewModel(repo, drafts, sync)
        advanceUntilIdle()
        reentered.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()
        assertEquals("every completion for one movement must carry the same key", listOf(firstKey, firstKey), sync.completeKeys)
    }

    @Test
    fun `an accepted completion clears the drafts`() = runTest(dispatcher) {
        val repo = FakeShiftingPendingRepository(priority = "high")
        val drafts = FakeCaptureDraftRepository()
        val sync = FakeShiftingSyncRepository()

        val vm = newViewModel(repo, drafts, sync)
        advanceUntilIdle()
        capture(vm, SEEDED_VIDEO)
        advanceUntilIdle()
        capture(vm, SEEDED_PACKING, SECTION_HIGH_PRIORITY)
        advanceUntilIdle()
        capture(vm, SEEDED_FEEDING, SECTION_HIGH_PRIORITY)
        advanceUntilIdle()
        vm.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()
        assertTrue(drafts.rows.isNotEmpty())

        sync.succeed(sync.completeItemIds.last())
        advanceUntilIdle()
        assertTrue("a finished movement keeps no draft, on either card or the markers", drafts.rows.isEmpty())
    }

    @Test
    fun `a high-priority move keeps each of its seeded captures across re-entry and sends all of them`() = runTest(dispatcher) {
        val repo = FakeShiftingPendingRepository(priority = "high")
        val drafts = FakeCaptureDraftRepository()
        val sync = FakeShiftingSyncRepository()
        val proofRepo = FakeProofCaptureRepository()

        val first = newViewModel(repo, drafts, sync, proofRepo)
        advanceUntilIdle()
        capture(first, SEEDED_VIDEO)
        advanceUntilIdle()
        capture(first, SEEDED_PACKING, SECTION_HIGH_PRIORITY)
        advanceUntilIdle()
        assertFalse("two of three captures is not submittable", first.state.value.canComplete)

        val reentered = newViewModel(repo, drafts, sync, proofRepo)
        advanceUntilIdle()
        assertTrue(reentered.state.value.completionCard.slots.single().captured)
        assertEquals(listOf(true, false), reentered.state.value.highPriorityCard.slots.map { it.captured })

        capture(reentered, SEEDED_FEEDING, SECTION_HIGH_PRIORITY)
        advanceUntilIdle()
        assertTrue("all three recorded — now submittable", reentered.state.value.canComplete)
        reentered.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()
        assertEquals(listOf(SEEDED_VIDEO, SEEDED_PACKING, SEEDED_FEEDING), sync.lastSlotProofs.keys.toList())
        assertEquals("fp-1", sync.lastFingerprint)
        assertEquals(3, proofRepo.captureCalls.size)
    }

    @Test
    fun `each capture event carries its own slot field and section`() = runTest(dispatcher) {
        val repo = FakeShiftingPendingRepository(priority = "high")
        val analytics = RecordingEvidenceAnalytics()
        val vm = newViewModel(repo, FakeCaptureDraftRepository(), FakeShiftingSyncRepository(), analytics = analytics)
        advanceUntilIdle()
        capture(vm, SEEDED_VIDEO)
        advanceUntilIdle()
        capture(vm, SEEDED_PACKING, SECTION_HIGH_PRIORITY)
        advanceUntilIdle()

        val events = analytics.events.filter { it.first == AnalyticsEvents.COUNTS_SHIFTING_EXECUTE_VIDEO_CAPTURED }.map { it.second }
        assertEquals(listOf(SEEDED_VIDEO, SEEDED_PACKING), events.map { it[AnalyticsEvents.Params.FIELD] })
        assertEquals(listOf("completion", "high_priority"), events.map { it["section"] })
        assertTrue(events.all { it["group_key"] == MOVEMENT_ID && it[AnalyticsEvents.Params.SHED_ID] == DEST_SHED_ID })
    }

    // ---- ShiftingExecuteSopCardTest ---------------------------------------------------------

    @Test
    fun `the pinned card is rendered and its required question gates the submit`() = runTest(dispatcher) {
        val card = ShiftingSopCardDto(
            version = 4,
            stage = "completion",
            instruction = "Film the walk.",
            proofs = listOf(
                WeighingRemovalProofSlotDto(key = "walk_video", title = "Walk video", kind = "video", required = true),
                WeighingRemovalProofSlotDto(key = "pen_after", title = "Pen after", kind = "either", required = false),
            ),
            questions = listOf(
                WeighingSopQuestionDto(id = "calm", kind = "choice", title = "Animals calm?", required = true, options = listOf(WeighingSopOptionDto("yes", "Yes"), WeighingSopOptionDto("no", "No"))),
            ),
        )
        val repo = FakeShiftingPendingRepository(sop = card)
        val sync = FakeShiftingSyncRepository()
        val vm = newViewModel(repo, FakeCaptureDraftRepository(), sync)
        advanceUntilIdle()
        val rendered = vm.state.value.completionCard
        assertEquals("Film the walk.", rendered.instruction)
        assertEquals(listOf("walk_video" to "Walk video", "pen_after" to "Pen after"), rendered.slots.map { it.key to it.title })
        assertEquals(listOf("video", "either"), rendered.slots.map { it.kind })

        capture(vm, "walk_video")
        advanceUntilIdle()
        assertFalse("the required question is unanswered", vm.state.value.canComplete)
        vm.onEvent(ShiftingExecuteEvent.Answer(SECTION_COMPLETION, "calm", "yes"))
        advanceUntilIdle()
        assertTrue(vm.state.value.canComplete)

        vm.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()
        assertEquals(setOf("walk_video"), sync.lastSlotProofs.keys)
        assertEquals(JsonObject(mapOf("calm" to JsonPrimitive("yes"))), sync.lastAnswers)
        assertTrue("answers fold a digest into the key", sync.completeKeys.single().startsWith("counts-shifting-complete:$MOVEMENT_ID:"))
    }

    // ---- ShiftingExecuteLegacyDraftFillsSeededSlotTest ---------------------------------------

    @Test
    fun `clips an older build stored under step names fill their seeded slots`() = runTest(dispatcher) {
        val drafts = FakeCaptureDraftRepository()
        drafts.putProof(CaptureFlow.SHIFTING, MOVEMENT_ID, "shifting", "old-outbox-shifting")
        drafts.putProof(CaptureFlow.SHIFTING, MOVEMENT_ID, "packing", "old-outbox-packing", fingerprint = "fp-1")
        drafts.putProof(CaptureFlow.SHIFTING, MOVEMENT_ID, "feeding", "old-outbox-feeding", fingerprint = "fp-1")
        val sync = FakeShiftingSyncRepository()
        val vm = newViewModel(FakeShiftingPendingRepository(priority = "high"), drafts, sync)
        advanceUntilIdle()

        assertTrue(vm.state.value.completionCard.slots.single().captured)
        assertEquals(listOf(true, true), vm.state.value.highPriorityCard.slots.map { it.captured })
        assertTrue("an older build's three clips are a complete high-priority submission", vm.state.value.canComplete)
        vm.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()
        assertEquals(
            mapOf(
                SEEDED_VIDEO to FeedSlotProofSourcePayload(outboxItemId = "old-outbox-shifting"),
                SEEDED_PACKING to FeedSlotProofSourcePayload(outboxItemId = "old-outbox-packing"),
                SEEDED_FEEDING to FeedSlotProofSourcePayload(outboxItemId = "old-outbox-feeding"),
            ),
            sync.lastSlotProofs,
        )
    }

    // ---- ShiftingExecuteFeedConfigChangeResetsOnlyHighPrioritySlotsTest ------------------------

    @Test
    fun `a changed feed config empties only the high-priority card`() = runTest(dispatcher) {
        val repo = FakeShiftingPendingRepository(priority = "high")
        val drafts = FakeCaptureDraftRepository()
        val sync = FakeShiftingSyncRepository()
        val first = newViewModel(repo, drafts, sync)
        advanceUntilIdle()
        capture(first, SEEDED_VIDEO)
        advanceUntilIdle()
        capture(first, SEEDED_PACKING, SECTION_HIGH_PRIORITY)
        advanceUntilIdle()
        capture(first, SEEDED_FEEDING, SECTION_HIGH_PRIORITY)
        advanceUntilIdle()
        assertTrue(first.state.value.canComplete)

        repo.fingerprint = "fp-2"
        ShiftingExecuteViewModel.clock = { CLIP_BASE_MS + 1_000_000 }
        val reopened = newViewModel(repo, drafts, sync)
        advanceUntilIdle()
        assertTrue("the completion capture survives", reopened.state.value.completionCard.slots.single().captured)
        assertEquals("both feed captures are emptied", listOf(false, false), reopened.state.value.highPriorityCard.slots.map { it.captured })
        assertFalse(reopened.state.value.canComplete)
        assertEquals(ShiftingExecuteViewModel.FEED_CONFIG_REFRESHED, reopened.state.value.videoMessage)
    }

    // ---- ShiftingExecuteReworkClearsSlotsKeepsAnswersTest ------------------------------------

    @Test
    fun `a rework empties the slots once keeps the answers and never refills the rejected take`() = runTest(dispatcher) {
        val card = ShiftingSopCardDto(
            version = 2,
            stage = "completion",
            proofs = listOf(WeighingRemovalProofSlotDto(key = "walk_video", title = "Walk video", kind = "video", required = true)),
            questions = listOf(WeighingSopQuestionDto(id = "note", kind = "text", title = "Note", required = true)),
        )
        val repo = FakeShiftingPendingRepository(sop = card)
        val drafts = FakeCaptureDraftRepository()
        val sync = FakeShiftingSyncRepository()
        val proofRepo = FakeProofCaptureRepository()
        val source = SteppingProofSource()

        val first = newViewModel(repo, drafts, sync, proofRepo, source = source)
        advanceUntilIdle()
        capture(first, "walk_video")
        advanceUntilIdle()
        first.onEvent(ShiftingExecuteEvent.Answer(SECTION_COMPLETION, "note", "moved at noon"))
        advanceUntilIdle()
        first.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()
        sync.succeed(sync.completeItemIds.last())
        advanceUntilIdle()

        // The verifier rejects: the server row reads rejected and carries the stored answers.
        repo.verificationState = "rejected"
        repo.primaryActionKey = "execute"
        repo.sopAnswers = JsonObject(mapOf("note" to JsonPrimitive("moved at noon")))
        ShiftingExecuteViewModel.clock = { source.lastStartedAt + 1 }
        val rework = newViewModel(repo, drafts, sync, proofRepo, source = source)
        advanceUntilIdle()
        assertFalse("the rejected take never refills the slot", rework.state.value.completionCard.slots.single().captured)
        assertEquals("moved at noon", rework.state.value.completionCard.answers["note"])
        assertEquals(ShiftingExecuteViewModel.REWORK_REQUIRED, rework.state.value.videoMessage)

        capture(rework, "walk_video")
        advanceUntilIdle()
        assertTrue(rework.state.value.completionCard.slots.single().captured)

        // Re-opening mid-rework keeps the fresh clip (the reset applies once).
        val reopened = newViewModel(repo, drafts, sync, proofRepo, source = source)
        advanceUntilIdle()
        assertTrue("a clip recorded after the rework survives re-opening", reopened.state.value.completionCard.slots.single().captured)
        assertTrue(reopened.state.value.canComplete)
        reopened.onEvent(ShiftingExecuteEvent.MarkDone)
        advanceUntilIdle()
        assertTrue("the resubmit names the fresh take, not the rejected one", sync.lastSlotProofs["walk_video"]?.outboxItemId != sync.firstSlotProofs?.get("walk_video")?.outboxItemId)
    }

    private fun newViewModel(
        repo: FakeShiftingPendingRepository,
        drafts: FakeCaptureDraftRepository,
        sync: FakeShiftingSyncRepository,
        proofCaptureRepository: FakeProofCaptureRepository = FakeProofCaptureRepository(),
        analytics: AnalyticsPort = RecordingEvidenceAnalytics(),
        source: ProofCaptureSource = SteppingProofSource(),
    ) = ShiftingExecuteViewModel(
        repo = repo,
        drafts = drafts,
        syncRepository = sync,
        proofCaptureSource = source,
        photoCaptureSource = NoopPhotoCaptureSource(),
        proofCaptureRepository = proofCaptureRepository,
        analytics = analytics,
        crashReporter = NoopEvidenceCrashReporter(),
        // A FRESH handle every time: this is what "Back then re-open" does to the destination.
        savedStateHandle = SavedStateHandle(mapOf("shifting_event_id" to MOVEMENT_ID)),
    )

    internal companion object {
        const val MOVEMENT_ID = "11111111-1111-4111-8111-111111111111"
        const val DEST_SHED_ID = "22222222-2222-4222-8222-222222222222"
        const val SEEDED_VIDEO = "shifting_shifting_video"
        const val SEEDED_PACKING = "shifting_packing_video"
        const val SEEDED_FEEDING = "shifting_feeding_video"
        const val CLIP_BASE_MS = 1_700_000_000_000
    }
}

/** The cached movement, mutable so a test can model a server refresh between two openings. */
private class FakeShiftingPendingRepository(
    private val priority: String = "low",
    private val sop: ShiftingSopCardDto? = null,
) : ShiftingPendingRepository {
    var fingerprint = "fp-1"
    var verificationState = "unverified"
    var primaryActionKey = "execute"
    var sopAnswers: JsonObject? = null
    override val actionsMeta: StateFlow<ShiftingActionsMeta> = MutableStateFlow(ShiftingActionsMeta())

    override fun pending(date: String, status: String): Flow<PagingData<CountsShiftingPendingExecutionItemDto>> =
        flowOf(PagingData.empty())

    override suspend fun forgetExecuted(shiftingEventId: String) = Unit

    override suspend fun findCached(shiftingEventId: String) = CountsShiftingPendingExecutionItemDto(
        shiftingEventId = shiftingEventId,
        eventStatus = "authorized",
        verificationState = verificationState,
        primaryActionKey = primaryActionKey,
        priority = priority,
        category = "growth",
        destinationShedId = ShiftingExecuteEvidenceDraftTest.DEST_SHED_ID,
        destinationShedName = "Gandhi 2",
        sourceShedName = "Gandhi 1",
        animalCount = 4,
        feedRequirement = if (priority == "high") CountsShiftingFeedRequirementDto(status = "ready", fingerprint = fingerprint) else null,
        sop = sop,
        sopAnswers = sopAnswers,
    )
}

/** In-memory stand-in for the Room-backed capture-draft store, keyed by (flow, entity) as Room is. */
private class FakeCaptureDraftRepository : CaptureDraftRepository {
    val rows = mutableMapOf<Pair<String, String>, CaptureDraft>()
    private val progress = MutableStateFlow<Map<String, Int>>(emptyMap())

    override suspend fun find(flowKey: String, entityId: String): CaptureDraft = rows[flowKey to entityId] ?: CaptureDraft()

    override fun observe(flowKey: String, entityId: String): Flow<CaptureDraft> = MutableStateFlow(rows[flowKey to entityId] ?: CaptureDraft())

    override fun observeProgress(flowKey: String, limit: Int): Flow<Map<String, Int>> = progress

    override suspend fun putProof(flowKey: String, entityId: String, step: String, outboxItemId: String, fingerprint: String?) {
        val current = rows[flowKey to entityId] ?: CaptureDraft()
        rows[flowKey to entityId] = current.copy(proofs = current.proofs + (step to outboxItemId), fingerprint = fingerprint ?: current.fingerprint)
        publish()
    }

    override suspend fun clearProof(flowKey: String, entityId: String, step: String) {
        val current = rows[flowKey to entityId] ?: return
        rows[flowKey to entityId] = current.copy(proofs = current.proofs - step)
        publish()
    }

    override suspend fun putSubmit(flowKey: String, entityId: String, idempotencyKey: String?, outboxItemId: String?) {
        val current = rows[flowKey to entityId] ?: CaptureDraft()
        rows[flowKey to entityId] = current.copy(submitIdempotencyKey = idempotencyKey, submitOutboxItemId = outboxItemId)
        publish()
    }

    override suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>) {
        val current = rows[flowKey to entityId] ?: CaptureDraft()
        rows[flowKey to entityId] = current.copy(answers = answers)
        publish()
    }

    override suspend fun clear(flowKey: String, entityId: String) {
        rows.remove(flowKey to entityId)
        publish()
    }

    private fun publish() {
        progress.value = rows.filterKeys { it.first == CaptureFlow.SHIFTING }.entries.associate { it.key.second to it.value.capturedCount }
    }
}

private class FakeShiftingSyncRepository : SyncRepository {
    val completeKeys = mutableListOf<String>()
    val completeItemIds = mutableListOf<String>()
    var lastSlotProofs: Map<String, FeedSlotProofSourcePayload> = emptyMap()
    var firstSlotProofs: Map<String, FeedSlotProofSourcePayload>? = null
    var lastAnswers: JsonObject? = null
    var lastFingerprint: String? = null
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    fun succeed(itemId: String) {
        val now = 1_700_000_000_000
        status.value = SyncStatus(
            online = true, pendingCount = 0, inFlightCount = 0, failedCount = 0, deadLetterCount = 0, lastSyncAt = now,
            items = listOf(
                SyncQueueItem(
                    id = itemId, idempotencyKey = "k", opType = "SHIFTING_COMPLETE", groupKey = "group",
                    status = sg.mesha.goatos.core.data.sync.SyncItemStatus.SUCCEEDED, attemptCount = 1, maxAttempts = 5,
                    conflict = false, createdAt = now, updatedAt = now, lastError = null,
                ),
            ),
        )
    }

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)

    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = AppResult.Ok("proof-x")

    override suspend fun enqueueShiftingComplete(
        groupKey: String,
        idempotencyKey: String,
        destinationTag: String?,
        proofOutboxItemId: String,
        feedPackingProofOutboxItemId: String?,
        feedGivenProofOutboxItemId: String?,
        feedConfigFingerprint: String?,
        slotProofs: Map<String, FeedSlotProofSourcePayload>,
        answers: JsonObject?,
    ): AppResult<String> {
        completeKeys += idempotencyKey
        if (firstSlotProofs == null) firstSlotProofs = slotProofs
        lastSlotProofs = slotProofs
        lastAnswers = answers
        lastFingerprint = feedConfigFingerprint
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

/** A live camera whose every clip starts one second after the previous one. */
private class SteppingProofSource : ProofCaptureSource {
    var lastStartedAt = ShiftingExecuteEvidenceDraftTest.CLIP_BASE_MS
    override suspend fun captureVideo(captureContext: ProofCaptureContext?): CapturedVideo {
        lastStartedAt += 1_000
        return CapturedVideo(localUri = "file:///tmp/clip-$lastStartedAt.mp4", mimeType = "video/mp4", captureSource = "in_app_camera", startedAtMs = lastStartedAt, endedAtMs = lastStartedAt + 500)
    }

    override suspend fun pickVideo(): CapturedVideo = captureVideo()
}

private class RecordingEvidenceAnalytics : AnalyticsPort {
    val events = mutableListOf<Pair<String, Map<String, String>>>()
    override fun track(event: String, props: Map<String, String>) {
        events += event to props
    }
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class NoopEvidenceCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}

/**
 * The Actions list says how much of each task is done. Since the SHIFTING SOP the requirement is the
 * movement's PINNED cards' compulsory slots; a row cached before the SOP reads as the seed.
 */
class ShiftingActionsEvidenceProgressTest {
    @Test
    fun `a seeded low task needs one capture and a seeded high task needs three`() {
        assertEquals(1, shiftingRequiredCaptures(CountsShiftingPendingExecutionItemDto(priority = "low")))
        assertEquals(3, shiftingRequiredCaptures(CountsShiftingPendingExecutionItemDto(priority = "High")))
    }

    @Test
    fun `an authored card counts only its compulsory slots, the high card only for a high task`() {
        val completion = ShiftingSopCardDto(
            proofs = listOf(
                WeighingRemovalProofSlotDto(key = "a", required = true),
                WeighingRemovalProofSlotDto(key = "b", required = false),
            ),
        )
        val high = ShiftingSopCardDto(proofs = listOf(WeighingRemovalProofSlotDto(key = "c", required = true)))
        assertEquals(1, shiftingRequiredCaptures(CountsShiftingPendingExecutionItemDto(priority = "low", sop = completion, highPrioritySop = high)))
        assertEquals(2, shiftingRequiredCaptures(CountsShiftingPendingExecutionItemDto(priority = "high", sop = completion, highPrioritySop = high)))
    }
}
