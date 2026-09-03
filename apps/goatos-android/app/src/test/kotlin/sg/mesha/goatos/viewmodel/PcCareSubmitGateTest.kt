package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.cache.PcCareScanStatus
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.feature.pccare.PcCareProofPreviewKind
import sg.mesha.goatos.feature.pccare.PcCareSlotState
import sg.mesha.goatos.feature.pccare.PcCareTaskEvent
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain

/**
 * The whole-task submit gate: blocked while any expected slot is missing on any scanned animal;
 * a PEER's server-attributed proof satisfies a slot; and the confirm RECOMPUTES from the current
 * durable state — a stashed submittable set is never trusted.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PcCareSubmitGateTest {
    private val dispatcher = UnconfinedTestDispatcher()
    private val json = Json { ignoreUnknownKeys = true }

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private val singleSlotDetail = pcCareTaskDtoFixture(
        category = "deworming",
        expectedSlots = listOf(sg.mesha.goatos.core.network.dto.PcCareSlotDto(fieldKey = "video", label = "Deworming video")),
    )

    private fun peerSlotJson(fieldKey: String, name: String = "Darshan") =
        """[{"field_key":"$fieldKey","proof_ref":"proof-$fieldKey","captured_by_name":"$name"}]"""

    private fun localProof(
        fieldKey: String,
        syncStatus: CaptureSyncStatus = CaptureSyncStatus.PENDING,
        serverProofId: String? = null,
        processingState: String = "CAPTURED_ORIGINAL",
        processedUri: String? = null,
    ) = ProofCaptureRow(
        id = "proof-$fieldKey",
        fieldKey = fieldKey,
        proofSubject = ProofSubject.OTHER,
        subjectId = fieldKey.substringBefore(':'),
        localUri = "file:///$fieldKey.mp4",
        processedUri = processedUri,
        mimeType = "video/mp4",
        caption = null,
        capturedAtMs = 1L,
        capturedStartMs = 0L,
        capturedEndMs = 1_000L,
        capturedByPrincipalId = null,
        syncStatus = syncStatus,
        serverProofId = serverProofId,
        outboxItemId = "outbox-$fieldKey",
        lastError = null,
        processingState = processingState,
    )

    @Test
    fun `blocked while an expected slot is missing`() {
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        val evaluation = pcCareEvaluateSubmit(singleSlotDetail.expectedSlots, animals, emptyList(), json)
        assertFalse(evaluation.ready)
        assertEquals("1 animal still needs videos", evaluation.blockedReason)
    }

    @Test
    fun `a peer's server-attributed proof unblocks the slot`() {
        val animals = listOf(
            pcCareAnimalEntity(tag = "t1", serverSlotsJson = peerSlotJson("video")),
        )
        val evaluation = pcCareEvaluateSubmit(singleSlotDetail.expectedSlots, animals, emptyList(), json)
        assertTrue(evaluation.ready)
        assertEquals(listOf("t1"), evaluation.submittableTags)
    }

    @Test
    fun `a local queued proof unblocks durable task submit before upload finishes`() {
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        val proofs = listOf(localProof(pcCareSlotProofFieldKey("t1", "video")))

        val evaluation = pcCareEvaluateSubmit(singleSlotDetail.expectedSlots, animals, proofs, json)

        assertTrue(evaluation.ready)
        assertEquals(listOf("t1"), evaluation.submittableTags)
    }

    @Test
    fun `a local animal slot proof exposes processed preview path while uploading`() {
        val chip = pcCareSlotChip(
            slot = singleSlotDetail.expectedSlots.single(),
            normalizedTag = "t1",
            animalProofs = listOf(
                localProof(
                    pcCareSlotProofFieldKey("t1", "video"),
                    processedUri = "file:///processed/t1-video.mp4",
                ),
            ),
            serverSlots = emptyList(),
            capturingSlotKey = null,
        )

        assertEquals(PcCareSlotState.WORKING, chip.state)
        assertEquals("file:///processed/t1-video.mp4", chip.previewPath)
        assertEquals(PcCareProofPreviewKind.VIDEO, chip.previewKind)
    }

    @Test
    fun `a failed local proof does not unblock durable task submit`() {
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        val proofs = listOf(localProof(pcCareSlotProofFieldKey("t1", "video"), CaptureSyncStatus.FAILED))

        val evaluation = pcCareEvaluateSubmit(singleSlotDetail.expectedSlots, animals, proofs, json)

        assertFalse(evaluation.ready)
        assertEquals("1 animal still needs videos", evaluation.blockedReason)
    }

    @Test
    fun `a record-again local proof does not unblock durable task submit`() {
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        val proofs = listOf(
            localProof(
                pcCareSlotProofFieldKey("t1", "video"),
                syncStatus = CaptureSyncStatus.FAILED,
                processingState = "DEAD_LETTER",
            ),
        )

        val evaluation = pcCareEvaluateSubmit(singleSlotDetail.expectedSlots, animals, proofs, json)

        assertFalse(evaluation.ready)
        assertEquals("1 animal still needs videos", evaluation.blockedReason)
    }

    @Test
    fun `a scan still on its way blocks even when every slot is filled`() {
        val animals = listOf(
            pcCareAnimalEntity(
                tag = "t1",
                scanSyncStatus = PcCareScanStatus.PENDING,
                serverSlotsJson = peerSlotJson("video"),
            ),
        )
        val evaluation = pcCareEvaluateSubmit(singleSlotDetail.expectedSlots, animals, emptyList(), json)
        assertFalse(evaluation.ready)
        assertEquals("Waiting for network — 1 scan still sending", evaluation.blockedReason)
    }

    @Test
    fun `confirm recomputes — a state degraded between arm and confirm never submits`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = singleSlotDetail
        repo.animalsFlow.value = listOf(
            pcCareAnimalEntity(tag = "t1", serverSlotsJson = peerSlotJson("video")),
        )
        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect { } }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.Submit)
        runCurrent()
        assertTrue(vm.state.value.showSubmitConfirmation)

        // Between arm and confirm, the durable state DEGRADES: a fresh poll shows the peer's
        // proof withdrawn (rework upstream), so the slot is open again.
        repo.animalsFlow.value = listOf(pcCareAnimalEntity(tag = "t1"))
        runCurrent()

        vm.onEvent(PcCareTaskEvent.ConfirmSubmit)
        runCurrent()

        assertEquals("stashed set must not be trusted", 0, repo.submitCalls.size)
        assertFalse(vm.state.value.showSubmitConfirmation)
        assertEquals("1 animal still needs videos", vm.state.value.message)
        collectJob.cancel()
    }

    @Test
    fun `confirm submits with the observed row version and the latch resets on failure`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = singleSlotDetail.copy(rowVersion = 7)
        repo.animalsFlow.value = listOf(
            pcCareAnimalEntity(tag = "t1", serverSlotsJson = peerSlotJson("video")),
        )
        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect { } }
        runCurrent()

        // First attempt fails terminally: the single-flight latch must reset.
        repo.failNextSubmit = true
        vm.onEvent(PcCareTaskEvent.Submit)
        runCurrent()
        vm.onEvent(PcCareTaskEvent.ConfirmSubmit)
        runCurrent()
        assertEquals(0, repo.submitCalls.size)
        assertFalse(vm.state.value.submitInFlight)
        assertFalse(vm.state.value.submitQueued)

        // Second attempt succeeds and carries the observed (bumped) row version.
        vm.onEvent(PcCareTaskEvent.Submit)
        runCurrent()
        vm.onEvent(PcCareTaskEvent.ConfirmSubmit)
        runCurrent()
        assertEquals(listOf("task-1" to 7), repo.submitCalls)
        assertTrue(vm.state.value.submitQueued)
        collectJob.cancel()
    }

    @Test
    fun `terminal failed submit outbox row unlocks the task instead of staying in review`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val sync = MinimalPcCareSyncRepository()
        repo.detailFlow.value = singleSlotDetail.copy(rowVersion = 7)
        repo.animalsFlow.value = listOf(
            pcCareAnimalEntity(tag = "t1", serverSlotsJson = peerSlotJson("video")),
        )
        val vm = buildPcCareTaskViewModel(repo, syncRepository = sync)
        val collectJob = launch { vm.state.collect { } }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.Submit)
        runCurrent()
        vm.onEvent(PcCareTaskEvent.ConfirmSubmit)
        runCurrent()
        assertTrue(vm.state.value.submitQueued)

        sync.emit(
            itemId = "submit-outbox-1",
            status = SyncItemStatus.FAILED,
            conflict = true,
            lastError = "proof_incomplete",
        )
        runCurrent()

        assertFalse(vm.state.value.submitQueued)
        assertTrue(vm.state.value.submitEnabled)
        assertEquals("Task submit failed. Check the proofs and try again.", vm.state.value.message)
        collectJob.cancel()
    }
}

internal fun buildPcCareTaskViewModel(
    repo: FakePcCareRepository,
    proofRepo: FakeProofCaptureRepository = FakeProofCaptureRepository(),
    proofSource: sg.mesha.goatos.capture.FakeProofCaptureSource = sg.mesha.goatos.capture.FakeProofCaptureSource(),
    photoSource: sg.mesha.goatos.capture.FakePhotoCaptureSource = sg.mesha.goatos.capture.FakePhotoCaptureSource(),
    reader: PcCareFakeReaderPort = PcCareFakeReaderPort(),
    analytics: FakeAnalyticsPort = FakeAnalyticsPort(),
    syncRepository: SyncRepository = MinimalPcCareSyncRepository(),
    approveView: Boolean = false,
): PcCareTaskViewModel = PcCareTaskViewModel(
    repository = repo,
    proofCaptureRepository = proofRepo,
    captureDrafts = InMemoryCaptureDraftRepository(),
    proofCaptureSource = proofSource,
    photoCaptureSource = photoSource,
    reader = reader,
    syncRepository = syncRepository,
    analytics = analytics,
    crashReporter = NoopCrashReporter(),
    savedStateHandle = SavedStateHandle(
        buildMap {
            put("task_id", "task-1")
            put("title", "Hoof trimming")
            if (approveView) put("approve", "1")
        },
    ),
)
