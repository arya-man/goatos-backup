package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.data.weighing.WeighingFastingCard
import sg.mesha.goatos.core.data.weighing.WeighingFastingListCache
import sg.mesha.goatos.core.data.weighing.WeighingFastingRepository
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardDto
import sg.mesha.goatos.feature.weighing.WeighingFastingDetailEvent
import sg.mesha.goatos.feature.weighing.WeighingFastingSlotKind
import sg.mesha.goatos.ui.Routes

/**
 * The removal screen's per-shed locks (maintainer correction #2, 2026-09-03 — ONE CARD PER SHED,
 * ONE SUBMIT PER SHED; the detail screen shows only its own shed):
 *
 *  1. SUBMIT REQUIRES BOTH OF THIS SHED'S VIDEOS — one clip must never queue a submit the
 *     backend would refuse (mutation check: make [WeighingFastingDetailViewModel]'s submit gate
 *     require only the feed clip and `submit blocked until both videos are recorded` goes red);
 *  2. A REWORK card requires FRESH clips — the prior act's slots reset when the card comes back;
 *  3. PER-SLOT UPLOAD LANES — each clip drains on its own upload group while the submit sits on
 *     the (task, shed)-grain group;
 *  4. The submit's outbox payload references the clips by PROOF OUTBOX ITEM ID, never proof ids.
 */
@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class WeighingFastingDetailViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModel(
        proofCaptureRepository: FakeProofCaptureRepository = FakeProofCaptureRepository(),
        proofCaptureSource: FakeProofCaptureSource = FakeProofCaptureSource(),
        photoCaptureSource: sg.mesha.goatos.capture.FakePhotoCaptureSource = sg.mesha.goatos.capture.FakePhotoCaptureSource(),
        syncRepository: RecordingFastingSyncRepository = RecordingFastingSyncRepository(),
        fastingRepository: FakeWeighingFastingRepository = FakeWeighingFastingRepository(),
        analytics: sg.mesha.goatos.core.analytics.AnalyticsPort = NoopAnalytics(),
        savedStateHandle: SavedStateHandle = SavedStateHandle(
            mapOf(
                Routes.WEIGHING_FASTING_TASK_ARG to "task-1",
                Routes.WEIGHING_FASTING_SHED_ARG to "shed-b",
                Routes.WEIGHING_FASTING_TITLE_ARG to "Remove feed & water · Castro 2",
            ),
        ),
    ) = WeighingFastingDetailViewModel(
        fastingRepository = fastingRepository,
        proofCaptureRepository = proofCaptureRepository,
        proofCaptureSource = proofCaptureSource,
        photoCaptureSource = photoCaptureSource,
        syncRepository = syncRepository,
        analytics = analytics,
        crashReporter = NoopCrashReporter(),
        appContext = ApplicationProvider.getApplicationContext(),
        savedStateHandle = savedStateHandle,
    )

    private fun video(uri: String) = CapturedVideo(
        localUri = uri,
        mimeType = "video/mp4",
        startedAtMs = 1L,
        endedAtMs = 2L,
    )

    private fun card(
        status: String = "open",
        reworkReason: String? = null,
        feedProofRef: String? = null,
        waterProofRef: String? = null,
    ) = WeighingFastingCard(
        WeighingFastingShedCardDto(
            fastingTaskId = "task-1",
            campaignShedId = "shed-b",
            shedLabel = "Castro 2",
            subjectLabel = "Remove feed & water · Castro 2",
            status = status,
            removalBusinessDate = "2026-09-03",
            reworkReason = reworkReason,
            feedProofRef = feedProofRef,
            waterProofRef = waterProofRef,
        ),
    )


    @Test
    fun `authored feed and water captures never alias legacy drafts and survive restoration`() = runTest(dispatcher) {
        val keys = listOf("feed_video", "water_video", "feed", "water")
        val handle = SavedStateHandle(mapOf(
            Routes.WEIGHING_FASTING_TASK_ARG to "task-1",
            Routes.WEIGHING_FASTING_SHED_ARG to "shed-b",
        ))
        val repo = FakeWeighingFastingRepository()
        repo.cardFlow.value = WeighingFastingCard(card().dto.copy(proofs = keys.map {
            sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto(key = it, title = it)
        }))
        val sync = RecordingFastingSyncRepository()
        val vm = viewModel(fastingRepository = repo, syncRepository = sync, savedStateHandle = handle,
            proofCaptureSource = FakeProofCaptureSource(keys.map { video("/proof/$it.mp4") }.toMutableList()))
        advanceUntilIdle()
        keys.forEachIndexed { index, key ->
            vm.onEvent(WeighingFastingDetailEvent.RecordSlot(key))
            advanceUntilIdle()
            assertEquals("each compulsory slot needs its OWN capture", index == keys.lastIndex, vm.state.value.submitEnabled)
        }
        // The original seeded draft keys must still be readable across the app upgrade.
        assertEquals("proof-outbox-1", handle.get<String>("weighing_fasting_proof_item_id:shed-b:feed"))
        assertEquals("proof-outbox-2", handle.get<String>("weighing_fasting_proof_item_id:shed-b:water"))
        val restored = viewModel(fastingRepository = repo, syncRepository = sync,
            savedStateHandle = SavedStateHandle(handle.keys().associateWith { handle.get<Any?>(it) }))
        advanceUntilIdle()
        assertTrue(restored.state.value.submitEnabled)
        assertEquals(keys.map { "/proof/$it.mp4" }, restored.state.value.slots.map { it.previewPath })
        restored.onEvent(WeighingFastingDetailEvent.Submit)
        advanceUntilIdle()
        assertEquals(keys.toSet(), sync.lastFastingProofItems.keys)
        assertEquals(4, sync.lastFastingProofItems.values.toSet().size)
        assertEquals("proof-outbox-1", sync.lastFastingProofItems["feed_video"])
        assertEquals("proof-outbox-3", sync.lastFastingProofItems["feed"])
    }

    /**
     * WEIGHING SOP (maintainer decision 2026-09-15): the card renders the task's pinned SOP copy
     * and asks its authored questions; a required one left blank blocks the submit BY NAME, and
     * the answers ride the queued submit in the backend's wire shape (a conditional whose
     * condition failed is not sent). Mutation check: drop `firstMissingAnswer()` from
     * `recomputeSubmit` and this goes red on the first assertion after the clips.
     */
    @Test
    fun `authored removal questions gate the submit and ride it as answers`() = runTest(dispatcher) {
        val sync = RecordingFastingSyncRepository()
        val source = FakeProofCaptureSource(mutableListOf(video("/proof/b-feed.mp4"), video("/proof/b-water.mp4")))
        val fastingRepository = FakeWeighingFastingRepository()
        val vm = viewModel(proofCaptureSource = source, syncRepository = sync, fastingRepository = fastingRepository)
        val base = card().dto
        fastingRepository.cardFlow.value = WeighingFastingCard(
            base.copy(
                instruction = "Empty every trough before dark.",
                proofs = listOf(
                    sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto(key = "feed_video", title = "Feed away", hint = "Show the empty trough."),
                    sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto(key = "water_video", title = "Water away"),
                ),
                questions = listOf(
                    sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto(
                        id = "all_pens", kind = "choice", title = "Every pen emptied?", required = true,
                        options = listOf(
                            sg.mesha.goatos.core.network.dto.WeighingSopOptionDto("yes", "Yes"),
                            sg.mesha.goatos.core.network.dto.WeighingSopOptionDto("no", "No"),
                        ),
                    ),
                    sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto(
                        id = "why_not", kind = "text", title = "Why not", required = true,
                        onlyIf = sg.mesha.goatos.core.network.dto.WeighingSopConditionDto("all_pens", "no"),
                    ),
                    sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto(id = "buckets", kind = "number", title = "Buckets removed", unit = "buckets"),
                ),
            ),
        )
        advanceUntilIdle()
        assertEquals("Empty every trough before dark.", vm.state.value.instruction)
        assertEquals("the SOP's slot wording wins", "Feed away", vm.state.value.feedSlot.title)
        assertEquals("Show the empty trough.", vm.state.value.feedSlot.hint)
        assertEquals("the conditional is hidden until its answer holds", listOf("all_pens", "buckets"), vm.state.value.questions.filter { it.applies }.map { it.id })

        vm.onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.FEED))
        advanceUntilIdle()
        vm.onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.WATER))
        advanceUntilIdle()
        assertEquals("both clips in, the required question still blocks", false, vm.state.value.submitEnabled)
        assertTrue(vm.state.value.submitBlockedReason.contains("Every pen emptied?"))

        vm.onEvent(WeighingFastingDetailEvent.SetAnswer("all_pens", "no"))
        advanceUntilIdle()
        assertTrue("answering no reveals the conditional", vm.state.value.questions.first { it.id == "why_not" }.applies)
        assertTrue(vm.state.value.submitBlockedReason.contains("Why not"))

        vm.onEvent(WeighingFastingDetailEvent.SetAnswer("all_pens", "yes"))
        vm.onEvent(WeighingFastingDetailEvent.SetAnswer("why_not", "stale"))
        vm.onEvent(WeighingFastingDetailEvent.SetAnswer("buckets", "12"))
        advanceUntilIdle()
        assertEquals(true, vm.state.value.submitEnabled)

        vm.onEvent(WeighingFastingDetailEvent.Submit)
        advanceUntilIdle()
        assertEquals(1, sync.fastingSubmits.size)
        val answers = sync.lastFastingAnswers
        assertEquals("\"yes\"", answers["all_pens"].toString())
        assertEquals("a number rides as a number", "12.0", answers["buckets"].toString())
        assertEquals("a conditional whose condition failed is not sent", null, answers["why_not"])
    }

    @Test
    fun `hidden ancestor hides descendants and excludes their answers from submit`() = runTest(dispatcher) {
        val sync = RecordingFastingSyncRepository()
        val source = FakeProofCaptureSource(mutableListOf(video("/proof/chain-feed.mp4"), video("/proof/chain-water.mp4")))
        val repository = FakeWeighingFastingRepository()
        val vm = viewModel(proofCaptureSource = source, syncRepository = sync, fastingRepository = repository)
        val options = listOf(
            sg.mesha.goatos.core.network.dto.WeighingSopOptionDto("yes", "Yes"),
            sg.mesha.goatos.core.network.dto.WeighingSopOptionDto("no", "No"),
        )
        repository.cardFlow.value = WeighingFastingCard(card().dto.copy(questions = listOf(
            sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto(id = "first", kind = "choice", title = "First", required = true, options = options),
            sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto(id = "second", kind = "choice", title = "Second", required = true, options = options,
                onlyIf = sg.mesha.goatos.core.network.dto.WeighingSopConditionDto("first", "yes")),
            sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto(id = "third", kind = "text", title = "Third", required = true,
                onlyIf = sg.mesha.goatos.core.network.dto.WeighingSopConditionDto("second", "yes")),
        )))
        advanceUntilIdle()
        vm.onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.FEED))
        advanceUntilIdle()
        vm.onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.WATER))
        advanceUntilIdle()
        vm.onEvent(WeighingFastingDetailEvent.SetAnswer("first", "yes"))
        vm.onEvent(WeighingFastingDetailEvent.SetAnswer("second", "yes"))
        advanceUntilIdle()
        assertTrue(vm.state.value.submitBlockedReason.contains("Third"))
        vm.onEvent(WeighingFastingDetailEvent.SetAnswer("first", "no"))
        advanceUntilIdle()
        assertEquals(listOf("first"), vm.state.value.questions.filter { it.applies }.map { it.id })
        assertTrue(vm.state.value.submitEnabled)
        // Draft answers can remain for toggling back, but must never activate hidden children.
        vm.onEvent(WeighingFastingDetailEvent.SetAnswer("third", "stale answer"))
        vm.onEvent(WeighingFastingDetailEvent.SetAnswer("first", "yes"))
        advanceUntilIdle()
        assertEquals(3, vm.state.value.questions.count { it.applies })
        vm.onEvent(WeighingFastingDetailEvent.SetAnswer("first", "no"))
        vm.onEvent(WeighingFastingDetailEvent.Submit)
        advanceUntilIdle()
        assertEquals(1, sync.fastingSubmits.size)
        assertEquals(setOf("first"), sync.lastFastingAnswers.keys)
    }

    @Test
    fun `submit blocked until both videos are recorded`() = runTest(dispatcher) {
        val sync = RecordingFastingSyncRepository()
        val source = FakeProofCaptureSource(mutableListOf(video("/proof/b-feed.mp4")))
        val fastingRepository = FakeWeighingFastingRepository()
        val vm = viewModel(proofCaptureSource = source, syncRepository = sync, fastingRepository = fastingRepository)
        fastingRepository.cardFlow.value = card()
        advanceUntilIdle()

        // The backend-owned card title, verbatim, is the screen's title body.
        assertEquals("Remove feed & water · Castro 2", vm.state.value.title)

        vm.onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.FEED))
        advanceUntilIdle()

        assertEquals("one of two clips is not this shed's evidence", false, vm.state.value.submitEnabled)
        vm.onEvent(WeighingFastingDetailEvent.Submit)
        advanceUntilIdle()
        assertEquals("a shed still owed a clip must never queue a submit", 0, sync.fastingSubmits.size)
        assertTrue(
            "the block states its reason instead of a dead button",
            vm.state.value.submitBlockedReason.isNotBlank(),
        )

        source.queue(video("/proof/b-water.mp4"))
        vm.onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.WATER))
        advanceUntilIdle()
        assertEquals(true, vm.state.value.submitEnabled)

        vm.onEvent(WeighingFastingDetailEvent.Submit)
        advanceUntilIdle()

        val submit = sync.fastingSubmits.single()
        assertEquals("task-1", submit.fastingTaskId)
        assertEquals("the submit names ITS shed", "shed-b", submit.campaignShedId)
        assertEquals("the submit lane is scoped to the (task, shed) pair", "weighing-fasting:task-1:shed-b", submit.groupKey)
        // Fresh clips are referenced by their proof OUTBOX ITEM IDS, never proof ids.
        assertTrue(submit.feedProofOutboxItemId.startsWith("proof-outbox-"))
        assertTrue(submit.waterProofOutboxItemId.startsWith("proof-outbox-"))
        // STABLE key derived from the (task, shed) plus the two clip outbox ids — a retry replays
        // for free while a re-shoot (new proofs) is a new act under a new key.
        assertEquals(
            "weighing-fasting-submit:task-1:shed-b:${submit.feedProofOutboxItemId}|${submit.waterProofOutboxItemId}",
            submit.idempotencyKey,
        )
        assertEquals(true, vm.state.value.submitQueued)
    }

    @Test
    fun `capture preview submit and outbox analytics carry fasting proof trace ids`() = runTest(dispatcher) {
        val analytics = FakeAnalyticsPort()
        val sync = RecordingFastingSyncRepository()
        val source = FakeProofCaptureSource(
            mutableListOf(video("/proof/feed.mp4"), video("/proof/water.mp4")),
        )
        val fastingRepository = FakeWeighingFastingRepository()
        val vm = viewModel(
            proofCaptureSource = source,
            syncRepository = sync,
            fastingRepository = fastingRepository,
            analytics = analytics,
        )
        fastingRepository.cardFlow.value = card()
        advanceUntilIdle()

        vm.onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.FEED))
        advanceUntilIdle()
        vm.onEvent(WeighingFastingDetailEvent.PreviewAction(WeighingFastingSlotKind.FEED, "share"))
        advanceUntilIdle()
        sync.emitItem(
            SyncQueueItem(
                id = "proof-outbox-1",
                idempotencyKey = "proof-upload-feed",
                opType = "PROOF_UPLOAD",
                groupKey = "weighing-fasting:task-1:shed-b:feed",
                status = SyncItemStatus.FAILED,
                attemptCount = 5,
                maxAttempts = 5,
                conflict = false,
                createdAt = 1L,
                updatedAt = 2L,
                lastError = "upload_403",
            ),
        )
        advanceUntilIdle()

        val captured = analytics.events.single {
            it.first == sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_REMOVAL_SLOT_CAPTURED &&
                it.second[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.ACTION] == "captured"
        }.second
        assertEquals("proof-0", captured["local_proof_row_id"])
        assertEquals("proof-outbox-1", captured[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])
        assertEquals("shed-b", captured[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.CAMPAIGN_SHED_ID])
        assertEquals("weighing-fasting:task-1:shed-b", captured["scope_key"])

        val preview = analytics.events.single {
            it.first == sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_REMOVAL_PROOF_PREVIEW_ACTION
        }.second
        assertEquals("share", preview[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.ACTION])
        assertEquals("proof-outbox-1", preview[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])

        val uploadFailure = analytics.events.single {
            it.first == sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_REMOVAL_FAILURE &&
                it.second[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.SOURCE] == "proof_outbox_observer"
        }.second
        assertEquals("sync_terminal_failure", uploadFailure[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.OUTCOME])
        assertEquals("upload_403", uploadFailure[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.REASON])
        assertEquals("proof-outbox-1", uploadFailure[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])

        vm.onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.WATER))
        advanceUntilIdle()
        vm.onEvent(WeighingFastingDetailEvent.Submit)
        advanceUntilIdle()

        val submitted = analytics.events.single {
            it.first == sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_REMOVAL_SUBMITTED &&
                it.second[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.SOURCE] == "submit_button"
        }.second
        assertEquals("outbox-fasting-1", submitted[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.OUTBOX_ITEM_ID])
        assertEquals("proof-outbox-1", submitted["feed_proof_outbox_item_id"])
        assertEquals("proof-outbox-2", submitted["water_proof_outbox_item_id"])
    }

    @Test
    fun `a card sent back after a queued submit requires fresh clips`() = runTest(dispatcher) {
        val sync = RecordingFastingSyncRepository()
        val fastingRepository = FakeWeighingFastingRepository()
        // A recreated ViewModel still holding the PRIOR act's slot item ids and queued submit.
        val handle = SavedStateHandle(
            mapOf(
                Routes.WEIGHING_FASTING_TASK_ARG to "task-1",
                Routes.WEIGHING_FASTING_SHED_ARG to "shed-b",
                Routes.WEIGHING_FASTING_TITLE_ARG to "Remove feed & water · Castro 2",
                "weighing_fasting_proof_item_id:shed-b:feed" to "proof-outbox-old-feed",
                "weighing_fasting_proof_item_id:shed-b:water" to "proof-outbox-old-water",
                "weighing_fasting_submit_outbox_item_id" to "outbox-fasting-old",
            ),
        )
        val vm = viewModel(syncRepository = sync, fastingRepository = fastingRepository, savedStateHandle = handle)
        fastingRepository.cardFlow.value = card(
            status = "rework",
            reworkReason = "The water trough is still full in the video.",
        )
        advanceUntilIdle()

        // The judged act's clips no longer count: the slots reset to Record video.
        assertEquals("a rework card requires FRESH clips", false, vm.state.value.submitEnabled)
        assertEquals(false, vm.state.value.submitQueued)
        assertEquals(false, vm.state.value.feedSlot.captured)
        assertEquals(false, vm.state.value.waterSlot.captured)
        // The verifier's own sentence about THIS shed, verbatim.
        assertEquals("The water trough is still full in the video.", vm.state.value.reworkReason)
        assertEquals("a sent-back card is recordable again", false, vm.state.value.isReadOnly)

        vm.onEvent(WeighingFastingDetailEvent.Submit)
        advanceUntilIdle()
        assertEquals("no submit may reuse the judged act's clips", 0, sync.fastingSubmits.size)
    }

    @Test
    fun `each clip uploads on its own lane and the submit lane is neither of them`() = runTest(dispatcher) {
        val captures = FakeProofCaptureRepository()
        val source = FakeProofCaptureSource(
            mutableListOf(video("/proof/b-feed.mp4"), video("/proof/b-water.mp4")),
        )
        val sync = RecordingFastingSyncRepository()
        val fastingRepository = FakeWeighingFastingRepository()
        val vm = viewModel(
            proofCaptureRepository = captures,
            proofCaptureSource = source,
            syncRepository = sync,
            fastingRepository = fastingRepository,
        )
        fastingRepository.cardFlow.value = card()
        advanceUntilIdle()

        vm.onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.FEED))
        advanceUntilIdle()
        vm.onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.WATER))
        advanceUntilIdle()
        vm.onEvent(WeighingFastingDetailEvent.Submit)
        advanceUntilIdle()

        val uploadGroups = captures.captureCalls.map { it.uploadGroupKey }
        assertEquals(2, uploadGroups.size)
        assertEquals(
            "each clip is independent field work — one backed-off upload must not strand the other",
            2,
            uploadGroups.distinct().size,
        )
        val submitGroup = sync.fastingSubmits.single().groupKey
        uploadGroups.forEach { lane ->
            assertNotEquals(
                "the submit must not queue behind either clip's upload lane; it waits by resolving outbox ids",
                submitGroup,
                lane,
            )
        }
        // The per-slot capture identity carries the SHED in the field key.
        assertEquals(
            setOf(
                "weighing_fasting_feed_video_shed-b",
                "weighing_fasting_water_video_shed-b",
            ),
            captures.captureCalls.map { it.fieldKey }.toSet(),
        )
    }

    @Test
    fun `a submitted card renders read-only from the Room row`() = runTest(dispatcher) {
        val fastingRepository = FakeWeighingFastingRepository()
        val vm = viewModel(fastingRepository = fastingRepository)
        fastingRepository.cardFlow.value = card(status = "pending_verification")
        advanceUntilIdle()

        assertEquals(true, vm.state.value.isReadOnly)
        assertEquals(false, vm.state.value.submitEnabled)
        assertTrue(vm.state.value.lockNotice.isNotBlank())
    }

    @Test
    fun `server backed read-only preview survives repeated Room emissions`() = runTest(dispatcher) {
        val fastingRepository = FakeWeighingFastingRepository().apply {
            proofDownloadUrl = "https://example.test/proof/feed.mp4"
        }
        val vm = viewModel(fastingRepository = fastingRepository)
        val submittedCard = card(
            status = "pending_verification",
            feedProofRef = "server-feed-proof",
        )
        fastingRepository.cardFlow.value = submittedCard
        advanceUntilIdle()

        assertTrue(vm.state.value.feedSlot.remoteUrl?.endsWith("/app/proofs/server-feed-proof/download") == true)
        assertEquals("server-feed-proof", vm.state.value.feedSlot.serverProofId)

        fastingRepository.cardFlow.value = submittedCard.copy()
        advanceUntilIdle()

        assertTrue(vm.state.value.feedSlot.remoteUrl?.endsWith("/app/proofs/server-feed-proof/download") == true)
        assertEquals("server-feed-proof", vm.state.value.feedSlot.serverProofId)
    }
}

internal class FakeWeighingFastingRepository : WeighingFastingRepository {
    val cardFlow = MutableStateFlow<WeighingFastingCard?>(null)
    val listFlow = MutableStateFlow(WeighingFastingListCache())
    var refreshCount = 0
        private set

    override fun observeCards(windowSize: Int): Flow<WeighingFastingListCache> = listFlow

    override fun observeCard(fastingTaskId: String, campaignShedId: String): Flow<WeighingFastingCard?> = cardFlow

    var proofDownloadUrl: String? = null

    override suspend fun fetchProofDownloadUrl(proofId: String): String? = proofDownloadUrl

    override suspend fun refresh(): AppResult<Int> {
        refreshCount++
        return AppResult.Ok(0)
    }

    override suspend fun append(): AppResult<Int> = AppResult.Ok(0)
}

internal class RecordingFastingSyncRepository : SyncRepository {
    data class FastingSubmit(
        val groupKey: String,
        val idempotencyKey: String,
        val fastingTaskId: String,
        val campaignShedId: String,
        val feedProofOutboxItemId: String,
        val waterProofOutboxItemId: String,
    )

    val fastingSubmits = mutableListOf<FastingSubmit>()

    var lastFastingAnswers: kotlinx.serialization.json.JsonObject = kotlinx.serialization.json.JsonObject(emptyMap())
    var lastFastingProofItems: Map<String, String> = emptyMap()
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()

    override fun observeStatus(): StateFlow<SyncStatus> = status.asStateFlow()

    override fun observeItem(itemId: String): Flow<SyncQueueItem?> =
        items.getOrPut(itemId) { MutableStateFlow(null) }

    fun emitItem(item: SyncQueueItem) {
        items.getOrPut(item.id) { MutableStateFlow(null) }.value = item
    }

    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)

    // Abstract members this test never exercises — inert stubs.
    override suspend fun enqueueShedSubmit(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto,
    ): AppResult<String> = AppResult.Err("not used in this test")

    override suspend fun enqueueReschedule(
        obligationId: String,
        groupKey: String,
        idempotencyKey: String,
        request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto,
    ): AppResult<String> = AppResult.Err("not used in this test")

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: sg.mesha.goatos.core.network.dto.ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = AppResult.Err("not used in this test")

    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> =
        AppResult.Err("not used in this test")

    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> =
        AppResult.Err("not used in this test")

    override suspend fun enqueueVerificationVerdict(
        itemId: String,
        decision: String,
        reason: String?,
        rowVersion: Int,
        measurement: sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto?,
    ): AppResult<String> = AppResult.Err("not used in this test")

    override suspend fun triggerDrain() = Unit

    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)

    override suspend fun enqueueWeighingFastingSubmit(
        groupKey: String,
        idempotencyKey: String,
        fastingTaskId: String,
        campaignShedId: String,
        feedProofOutboxItemId: String,
        waterProofOutboxItemId: String,
        answers: kotlinx.serialization.json.JsonObject,
        proofOutboxItems: Map<String, String>,
    ): AppResult<String> {
        lastFastingAnswers = answers
        lastFastingProofItems = proofOutboxItems
        fastingSubmits += FastingSubmit(
            groupKey,
            idempotencyKey,
            fastingTaskId,
            campaignShedId,
            feedProofOutboxItemId,
            waterProofOutboxItemId,
        )
        return AppResult.Ok("outbox-fasting-${fastingSubmits.size}")
    }
}


/**
 * WEIGHING SOP, second 2026-09-15 decision: the card's captures are the SOP's slots. Three slots
 * -- a compulsory video, a compulsory PHOTO, an optional either -- render in order; the photo slot
 * opens the photo camera; the optional slot may be skipped; the submit is blocked by name until
 * every compulsory capture exists, and then carries {slot key: outbox item} for exactly the
 * captured slots under a slot-keyed idempotency key. Mutation check: make firstMissingCapture
 * ignore `required` and the "optional may be skipped" assertion goes red.
 */
@RunWith(RobolectricTestRunner::class)
class WeighingFastingDetailViewModelSlotsTest {
    private val dispatcher = kotlinx.coroutines.test.StandardTestDispatcher()

    @org.junit.Test
    fun `a photo slot's status names a photo, a video slot a video`() {
        val photo = WeighingFastingDetailViewModel.proofStatusLabel(sg.mesha.goatos.feature.weighing.WeighingFastingCaptureKind.PHOTO, sg.mesha.goatos.feature.weighing.WeighingFastingSlotStatus.SYNCED)
        val video = WeighingFastingDetailViewModel.proofStatusLabel(sg.mesha.goatos.feature.weighing.WeighingFastingCaptureKind.VIDEO, sg.mesha.goatos.feature.weighing.WeighingFastingSlotStatus.SYNCED)
        val unknown = WeighingFastingDetailViewModel.proofStatusLabel(null, sg.mesha.goatos.feature.weighing.WeighingFastingSlotStatus.FAILED)
        org.junit.Assert.assertEquals("Photo sent", photo)
        org.junit.Assert.assertEquals("Video sent", video)
        org.junit.Assert.assertTrue(unknown, unknown.startsWith("Video didn't go through"))
    }

    @org.junit.Before
    fun setUp() = kotlinx.coroutines.Dispatchers.setMain(dispatcher)

    @org.junit.After
    fun tearDown() = kotlinx.coroutines.Dispatchers.resetMain()

    private fun slot(key: String, kind: String, required: Boolean, title: String) =
        sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto(key = key, title = title, kind = kind, required = required)

    @org.junit.Test
    fun `authored slots drive the camera mode, the submit gate and the payload`() = kotlinx.coroutines.test.runTest(dispatcher) {
        val sync = RecordingFastingSyncRepository()
        val video = FakeProofCaptureSource(mutableListOf(CapturedVideo(localUri = "/proof/feed.mp4", mimeType = "video/mp4", startedAtMs = 1L, endedAtMs = 2L)))
        val photo = sg.mesha.goatos.capture.FakePhotoCaptureSource(mutableListOf(sg.mesha.goatos.capture.CapturedPhoto(localUri = "/proof/trough.jpg", capturedAtMs = 3L)))
        val fastingRepository = FakeWeighingFastingRepository()
        val vm = WeighingFastingDetailViewModel(
            fastingRepository = fastingRepository,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = video,
            photoCaptureSource = photo,
            syncRepository = sync,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    Routes.WEIGHING_FASTING_TASK_ARG to "task-1",
                    Routes.WEIGHING_FASTING_SHED_ARG to "shed-b",
                    Routes.WEIGHING_FASTING_TITLE_ARG to "Remove feed & water · Castro 2",
                ),
            ),
        )
        fastingRepository.cardFlow.value = WeighingFastingCard(
            WeighingFastingShedCardDto(
                fastingTaskId = "task-1", campaignShedId = "shed-b", shedLabel = "Castro 2",
                subjectLabel = "Remove feed & water · Castro 2", status = "open", removalBusinessDate = "2026-09-03",
                proofs = listOf(
                    slot("feed_video", "video", true, "Feed removed"),
                    slot("trough_photo", "photo", true, "Empty trough"),
                    slot("gate", "either", false, "Gate closed"),
                ),
            ),
        )
        advanceUntilIdle()
        assertEquals(listOf("feed_video", "trough_photo", "gate"), vm.state.value.slots.map { it.slotKey })
        assertEquals(listOf("video", "photo", "either"), vm.state.value.slots.map { it.captureKind })
        assertEquals(listOf(true, true, false), vm.state.value.slots.map { it.required })
        assertTrue(vm.state.value.submitBlockedReason.contains("Feed removed"))

        vm.onEvent(WeighingFastingDetailEvent.RecordSlot("feed_video"))
        advanceUntilIdle()
        assertEquals("the video slot opened the video camera", 1, video.captureCount)
        assertEquals(0, photo.captureCount)
        assertTrue("the next compulsory capture is named", vm.state.value.submitBlockedReason.contains("Empty trough"))

        vm.onEvent(WeighingFastingDetailEvent.RecordSlot("trough_photo"))
        advanceUntilIdle()
        assertEquals("the photo slot opened the photo camera", 1, photo.captureCount)
        assertEquals("photo", vm.state.value.slots.first { it.slotKey == "trough_photo" }.capturedKind)
        assertEquals("the optional slot may be skipped", true, vm.state.value.submitEnabled)

        vm.onEvent(WeighingFastingDetailEvent.Submit)
        advanceUntilIdle()
        val submit = sync.fastingSubmits.single()
        assertEquals(setOf("feed_video", "trough_photo"), sync.lastFastingProofItems.keys)
        assertTrue("water is not one of this document's slots", submit.waterProofOutboxItemId.isEmpty())
        assertTrue(submit.idempotencyKey.startsWith("weighing-fasting-submit:task-1:shed-b:feed_video=") && submit.idempotencyKey.contains("|trough_photo="))
    }

    @org.junit.Test
    fun `a corrected answer over the same captures is a new submission, not a conflicting replay`() {
        val vm = WeighingFastingDetailViewModel(
            fastingRepository = FakeWeighingFastingRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = sg.mesha.goatos.capture.FakePhotoCaptureSource(mutableListOf()),
            syncRepository = RecordingFastingSyncRepository(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(mapOf(Routes.WEIGHING_FASTING_TASK_ARG to "task-1", Routes.WEIGHING_FASTING_SHED_ARG to "shed-b")),
        )
        val proofs = mapOf("feed_video" to "item-f", "water_video" to "item-w")
        val sixty = kotlinx.serialization.json.buildJsonObject { put("water_buckets_removed", kotlinx.serialization.json.JsonPrimitive(60)) }
        val twelve = kotlinx.serialization.json.buildJsonObject { put("water_buckets_removed", kotlinx.serialization.json.JsonPrimitive(12)) }
        // No answers keeps the pre-SOP key shape, so a queued draft replays under its own key.
        assertEquals("weighing-fasting-submit:task-1:shed-b:item-f|item-w", vm.submitIdempotencyKey(proofs))
        val refused = vm.submitIdempotencyKey(proofs, sixty)
        val corrected = vm.submitIdempotencyKey(proofs, twelve)
        assertTrue(refused != corrected)
        assertEquals("the same answers replay under the same key", refused, vm.submitIdempotencyKey(proofs, sixty))
        assertTrue(refused.startsWith("weighing-fasting-submit:task-1:shed-b:item-f|item-w:a="))
    }

    @org.junit.Test
    fun `a card reopened with no local state renders an either-slot photo as a photo`() = kotlinx.coroutines.test.runTest(dispatcher) {
        val fastingRepository = FakeWeighingFastingRepository()
        val vm = WeighingFastingDetailViewModel(
            fastingRepository = fastingRepository,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = sg.mesha.goatos.capture.FakePhotoCaptureSource(mutableListOf()),
            syncRepository = RecordingFastingSyncRepository(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            // A fresh SavedStateHandle: no locally remembered capture kind for any slot.
            savedStateHandle = SavedStateHandle(mapOf(Routes.WEIGHING_FASTING_TASK_ARG to "task-1", Routes.WEIGHING_FASTING_SHED_ARG to "shed-b")),
        )
        fastingRepository.cardFlow.value = WeighingFastingCard(
            WeighingFastingShedCardDto(
                fastingTaskId = "task-1", campaignShedId = "shed-b", shedLabel = "Castro 2",
                subjectLabel = "Remove feed & water · Castro 2", status = "pending_verification", removalBusinessDate = "2026-09-03",
                proofs = listOf(slot("feed_video", "video", true, "Feed removed"), slot("gate", "either", false, "Gate closed")),
                proofRefs = mapOf("feed_video" to "ref-feed", "gate" to "ref-gate"),
                proofKinds = mapOf("feed_video" to "video", "gate" to "photo"),
            ),
        )
        advanceUntilIdle()
        val gate = vm.state.value.slots.first { it.slotKey == "gate" }
        assertEquals("the server's recorded kind picks the player, not the video default", "photo", gate.capturedKind)
        assertEquals("video", vm.state.value.slots.first { it.slotKey == "feed_video" }.capturedKind)
    }

    /**
     * PR #274 review round 2, finding 3: after process death the constructor knows only the seeded
     * two slots. An AUTHORED slot whose capture is still in the outbox arrives with the card from
     * Room; its upload must keep reporting -- here a later failure -- on the card.
     */
    @org.junit.Test
    fun `an authored slot's upload keeps reporting after process death`() = kotlinx.coroutines.test.runTest(dispatcher) {
        val sync = RecordingFastingSyncRepository()
        val fastingRepository = FakeWeighingFastingRepository()
        val vm = WeighingFastingDetailViewModel(
            fastingRepository = fastingRepository,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = sg.mesha.goatos.capture.FakePhotoCaptureSource(mutableListOf()),
            syncRepository = sync,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            // The persisted draft of a custom "gate" slot survived process death; the seeded
            // slots were never captured.
            savedStateHandle = SavedStateHandle(
                mapOf(
                    Routes.WEIGHING_FASTING_TASK_ARG to "task-1",
                    Routes.WEIGHING_FASTING_SHED_ARG to "shed-b",
                    "weighing_fasting_proof_item_id:shed-b:gate" to "proof-outbox-gate",
                ),
            ),
        )
        advanceUntilIdle()
        fastingRepository.cardFlow.value = WeighingFastingCard(
            WeighingFastingShedCardDto(
                fastingTaskId = "task-1", campaignShedId = "shed-b", shedLabel = "Castro 2",
                subjectLabel = "Remove feed & water · Castro 2", status = "open", removalBusinessDate = "2026-09-03",
                proofs = listOf(slot("feed_video", "video", true, "Feed removed"), slot("gate", "either", false, "Gate closed")),
            ),
        )
        advanceUntilIdle()
        assertTrue("the persisted capture is on the card", vm.state.value.slots.first { it.slotKey == "gate" }.captured)
        sync.emitItem(
            SyncQueueItem(
                id = "proof-outbox-gate", idempotencyKey = "proof-upload-gate", opType = "PROOF_UPLOAD",
                groupKey = "weighing-fasting:task-1:shed-b:gate", status = SyncItemStatus.FAILED,
                attemptCount = 5, maxAttempts = 5, conflict = false, createdAt = 1L, updatedAt = 2L, lastError = "upload_403",
            ),
        )
        advanceUntilIdle()
        val gate = vm.state.value.slots.first { it.slotKey == "gate" }
        assertEquals("the later failure reaches the authored slot", sg.mesha.goatos.feature.weighing.WeighingFastingSlotStatus.FAILED, gate.status)
        assertEquals("upload_403", gate.statusLabel)
    }
}
