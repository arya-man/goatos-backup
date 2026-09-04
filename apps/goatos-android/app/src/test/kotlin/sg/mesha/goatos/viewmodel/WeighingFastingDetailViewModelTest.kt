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
        syncRepository: RecordingFastingSyncRepository = RecordingFastingSyncRepository(),
        fastingRepository: FakeWeighingFastingRepository = FakeWeighingFastingRepository(),
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
        syncRepository = syncRepository,
        analytics = NoopAnalytics(),
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
    ) = WeighingFastingCard(
        WeighingFastingShedCardDto(
            fastingTaskId = "task-1",
            campaignShedId = "shed-b",
            shedLabel = "Castro 2",
            subjectLabel = "Remove feed & water · Castro 2",
            status = status,
            removalBusinessDate = "2026-09-03",
            reworkReason = reworkReason,
        ),
    )

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
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()

    override fun observeStatus(): StateFlow<SyncStatus> = status.asStateFlow()

    override fun observeItem(itemId: String): Flow<SyncQueueItem?> =
        items.getOrPut(itemId) { MutableStateFlow(null) }

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
    ): AppResult<String> {
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
