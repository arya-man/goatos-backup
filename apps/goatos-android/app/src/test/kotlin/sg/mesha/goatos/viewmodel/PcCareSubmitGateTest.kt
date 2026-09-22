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
import sg.mesha.goatos.core.analytics.AnalyticsEvents

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

    // An OPTIONAL capture may be skipped: the server gates the submit on required_slot_keys
    // alone, so gating on every slot made `required = false` mean nothing on the phone — the
    // operator could never finish a task carrying one (PR 349 review, second pass).
    private val optionalExtraDetail = pcCareTaskDtoFixture(
        category = "deworming",
        expectedSlots = listOf(
            sg.mesha.goatos.core.network.dto.PcCareSlotDto(fieldKey = "video", label = "Deworming video", required = true),
            sg.mesha.goatos.core.network.dto.PcCareSlotDto(fieldKey = "dose_photo", label = "Dose photo", kind = "photo", required = false),
        ),
    )

    @Test
    fun `an optional capture never holds the submit`() {
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        val proofs = listOf(localProof(pcCareSlotProofFieldKey("t1", "video")))

        val evaluation = pcCareEvaluateSubmit(optionalExtraDetail.expectedSlots, animals, proofs, json)

        assertTrue(evaluation.ready)
        assertEquals(listOf("t1"), evaluation.submittableTags)
    }

    @Test
    fun `the compulsory capture still holds the submit when the optional one is shot`() {
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        val proofs = listOf(localProof(pcCareSlotProofFieldKey("t1", "dose_photo")))

        val evaluation = pcCareEvaluateSubmit(optionalExtraDetail.expectedSlots, animals, proofs, json)

        assertFalse(evaluation.ready)
        assertEquals("1 animal still needs videos", evaluation.blockedReason)
    }

    @Test
    fun `an optional capture still uploading is waited for`() {
        // Recorded but not yet queued: submitting over it would land the task without evidence
        // the operator actually captured, optional or not.
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        val proofs = listOf(
            localProof(pcCareSlotProofFieldKey("t1", "video")),
            localProof(pcCareSlotProofFieldKey("t1", "dose_photo")).copy(outboxItemId = null),
        )

        val evaluation = pcCareEvaluateSubmit(optionalExtraDetail.expectedSlots, animals, proofs, json)

        assertFalse(evaluation.ready)
        assertEquals("1 video still uploading", evaluation.blockedReason)
    }

    @Test
    fun `a card that sends no required flag keeps every capture compulsory`() {
        // An older server sends no `required`; the wire default is TRUE, so nothing loosens.
        val legacy = pcCareTaskDtoFixture(
            category = "deworming",
            expectedSlots = listOf(sg.mesha.goatos.core.network.dto.PcCareSlotDto(fieldKey = "video", label = "Deworming video")),
        )
        val evaluation = pcCareEvaluateSubmit(legacy.expectedSlots, listOf(pcCareAnimalEntity(tag = "t1")), emptyList(), json)
        assertFalse(evaluation.ready)
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
    fun `accepted scan analytics carries scan outbox join keys`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val analytics = FakeAnalyticsPort()
        repo.detailFlow.value = singleSlotDetail

        val vm = buildPcCareTaskViewModel(repo, analytics = analytics)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.ScanInputChanged("RF-042"))
        vm.onEvent(PcCareTaskEvent.SubmitTypedScan)
        runCurrent()

        val event = analytics.events.last { it.first == AnalyticsEvents.PC_CARE_SCAN_ACCEPTED }
        assertEquals("task-1", event.second["task_id"])
        assertEquals("RF-042", event.second[AnalyticsEvents.Params.RFID])
        assertEquals("rf-042", event.second["normalized_rfid"])
        assertEquals("scan-outbox-1", event.second[AnalyticsEvents.Params.OUTBOX_ITEM_ID])
        assertEquals("pc-care:scan:task-1", event.second[AnalyticsEvents.Params.GROUP_KEY])
        assertEquals("pc-care:scan:task-1:rf-042", event.second[AnalyticsEvents.Params.IDEMPOTENCY_KEY])
        collectJob.cancel()
    }

    @Test
    fun `uploaded local animal slot proof is not green until pc care slot register is visible`() {
        val fieldKey = pcCareSlotProofFieldKey("t1", "video")
        val chip = pcCareSlotChip(
            slot = singleSlotDetail.expectedSlots.single(),
            normalizedTag = "t1",
            animalProofs = listOf(
                localProof(
                    fieldKey,
                    syncStatus = CaptureSyncStatus.SYNCED,
                    serverProofId = "server-proof-t1-video",
                    processingState = "UPLOADED",
                ),
            ),
            serverSlots = emptyList(),
            capturingSlotKey = null,
        )

        assertEquals(PcCareSlotState.WORKING, chip.state)
        assertEquals("Video uploaded, saving to task...", chip.statusLabel)
        assertFalse(chip.canRecord)
        assertEquals("server-proof-t1-video", chip.serverProofId)
    }

    @Test
    fun `uploaded local animal slot proof turns green only after pc care slot register lands`() {
        val fieldKey = pcCareSlotProofFieldKey("t1", "video")
        val chip = pcCareSlotChip(
            slot = singleSlotDetail.expectedSlots.single(),
            normalizedTag = "t1",
            animalProofs = listOf(
                localProof(
                    fieldKey,
                    syncStatus = CaptureSyncStatus.SYNCED,
                    serverProofId = "server-proof-t1-video",
                    processingState = "UPLOADED",
                ),
            ),
            serverSlots = listOf(
                sg.mesha.goatos.core.network.dto.PcCareAnimalSlotDto(
                    fieldKey = "video",
                    proofRef = "server-proof-t1-video",
                ),
            ),
            capturingSlotKey = null,
        )

        assertEquals(PcCareSlotState.SYNCED, chip.state)
        assertEquals("Video sent", chip.statusLabel)
        assertEquals("server-proof-t1-video", chip.serverProofId)
    }

    @Test
    fun `animal slot backend ack analytics fires when pc care slot register is visible`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val analytics = FakeAnalyticsPort()
        repo.detailFlow.value = singleSlotDetail.copy(captureMode = "per_animal")
        repo.animalsFlow.value = listOf(
            pcCareAnimalEntity(
                tag = "T1",
                serverSlotsJson = """[{"field_key":"video","proof_ref":"server-proof-t1-video"}]""",
            ),
        )

        val vm = buildPcCareTaskViewModel(repo, analytics = analytics)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        val event = analytics.events.last { it.first == AnalyticsEvents.PC_CARE_SLOT_BUSINESS_ACK }
        assertEquals("business_ack_visible", event.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("backend_task_detail", event.second[AnalyticsEvents.Params.SOURCE])
        assertEquals("video", event.second["slot_field_key"])
        assertEquals("t1:video", event.second["field_key"])
        assertEquals("server-proof-t1-video", event.second["server_proof_id"])
        assertEquals("T1", event.second[AnalyticsEvents.Params.RFID])
        collectJob.cancel()
    }

    @Test
    fun `animal slot preview actions include proof trace ids and task context`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val proofRepo = FakeProofCaptureRepository()
        val analytics = FakeAnalyticsPort()
        val fieldKey = pcCareSlotProofFieldKey("t1", "video")
        repo.detailFlow.value = singleSlotDetail.copy(captureMode = "per_animal")
        repo.animalsFlow.value = listOf(pcCareAnimalEntity(tag = "t1", scannedByName = "Amit Kumar"))
        proofRepo.seedProofs(
            localProof(
                fieldKey = fieldKey,
                syncStatus = CaptureSyncStatus.SYNCED,
                serverProofId = "server-proof-t1-video",
            ),
        )
        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo, analytics = analytics)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.ProofPreviewAction("video", "video", "share", tagKey = "t1"))

        val event = analytics.events.last()
        assertEquals(AnalyticsEvents.PC_CARE_SLOT_PROOF_PREVIEW, event.first)
        assertEquals("share", event.second[AnalyticsEvents.Params.ACTION])
        assertEquals("preview_action", event.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("proof-$fieldKey", event.second["local_proof_row_id"])
        assertEquals("outbox-$fieldKey", event.second[AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])
        assertEquals("server-proof-t1-video", event.second["server_proof_id"])
        assertEquals("deworming", event.second["category"])
        assertEquals("per_animal", event.second["capture_mode"])
        assertEquals("t1", event.second["normalized_rfid"])
        collectJob.cancel()
    }

    @Test
    fun `animal slot proof upload terminal analytics include proof and outbox ids`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val proofRepo = FakeProofCaptureRepository()
        val analytics = FakeAnalyticsPort()
        val sync = MinimalPcCareSyncRepository()
        val fieldKey = pcCareSlotProofFieldKey("t1", "video")
        repo.detailFlow.value = singleSlotDetail.copy(captureMode = "per_animal")
        repo.animalsFlow.value = listOf(pcCareAnimalEntity(tag = "t1", scannedByName = "Amit Kumar"))
        proofRepo.seedProofs(localProof(fieldKey = fieldKey, syncStatus = CaptureSyncStatus.PENDING))
        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo, analytics = analytics, syncRepository = sync)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        proofRepo.markSynced("proof-$fieldKey", serverProofId = "server-proof-t1-video")
        runCurrent()
        sync.emit(itemId = "outbox-$fieldKey", status = SyncItemStatus.SUCCEEDED)
        runCurrent()

        val event = analytics.events.last { it.first == AnalyticsEvents.PC_CARE_SLOT_UPLOAD_SYNCED }
        assertEquals("proof_upload_sync", event.second[AnalyticsEvents.Params.ACTION])
        assertEquals("success", event.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("proof_upload_outbox", event.second[AnalyticsEvents.Params.SOURCE])
        assertEquals("proof-$fieldKey", event.second["local_proof_row_id"])
        assertEquals("outbox-$fieldKey", event.second[AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])
        assertEquals("server-proof-t1-video", event.second["server_proof_id"])
        assertEquals("video", event.second[AnalyticsEvents.Params.FIELD])
        assertEquals(fieldKey, event.second["field_key"])
        assertEquals("t1", event.second["normalized_rfid"])
        collectJob.cancel()
    }

    @Test
    fun `animal slot submit analytics include proof row outbox and terminal status`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val proofRepo = FakeProofCaptureRepository()
        val analytics = FakeAnalyticsPort()
        val sync = MinimalPcCareSyncRepository()
        val fieldKey = pcCareSlotProofFieldKey("t1", "video")
        repo.detailFlow.value = singleSlotDetail.copy(rowVersion = 7, captureMode = "per_animal")
        repo.animalsFlow.value = listOf(pcCareAnimalEntity(tag = "t1"))
        proofRepo.seedProofs(localProof(fieldKey = fieldKey, syncStatus = CaptureSyncStatus.PENDING))
        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo, analytics = analytics, syncRepository = sync)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.Submit)
        runCurrent()
        vm.onEvent(PcCareTaskEvent.ConfirmSubmit)
        runCurrent()
        sync.emit(itemId = "submit-outbox-1", status = SyncItemStatus.SUCCEEDED)
        runCurrent()

        val enqueued = analytics.events.first { it.first == AnalyticsEvents.PC_CARE_SLOT_SUBMIT }
        assertEquals("enqueued", enqueued.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("submit-outbox-1", enqueued.second[AnalyticsEvents.Params.OUTBOX_ITEM_ID])
        assertEquals("proof-$fieldKey", enqueued.second["local_proof_row_id"])
        assertEquals("outbox-$fieldKey", enqueued.second[AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])
        assertEquals(fieldKey, enqueued.second["field_keys"])
        val synced = analytics.events.last { it.first == AnalyticsEvents.PC_CARE_SLOT_SUBMIT }
        assertEquals("sync_success", synced.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("submit-outbox-1", synced.second[AnalyticsEvents.Params.OUTBOX_ITEM_ID])
        assertEquals("proof-$fieldKey", synced.second["local_proof_row_id"])
        collectJob.cancel()
    }

    @Test
    fun `confirm recreates missing animal slot register before final submit`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val proofRepo = FakeProofCaptureRepository()
        val fieldKey = pcCareSlotProofFieldKey("t1", "video")
        repo.detailFlow.value = singleSlotDetail.copy(rowVersion = 7, captureMode = "per_animal")
        repo.animalsFlow.value = listOf(pcCareAnimalEntity(tag = "t1", scanSyncStatus = PcCareScanStatus.SYNCED))
        proofRepo.seedProofs(
            localProof(
                fieldKey = fieldKey,
                syncStatus = CaptureSyncStatus.SYNCED,
                serverProofId = "server-proof-t1-video",
                processingState = "UPLOADED",
            ),
        )
        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()
        repo.slotRegistrations.clear()

        vm.onEvent(PcCareTaskEvent.Submit)
        runCurrent()
        vm.onEvent(PcCareTaskEvent.ConfirmSubmit)
        runCurrent()

        assertEquals(
            listOf(listOf("task-1", "t1", "video", "outbox-$fieldKey")),
            repo.slotRegistrations,
        )
        assertEquals(listOf("task-1" to 7), repo.submitCalls)
        assertTrue(vm.state.value.submitQueued)
        collectJob.cancel()
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
    title: String = "Hoof trimming",
    category: String = "",
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
            put("title", title)
            if (category.isNotBlank()) put("category", category)
            if (approveView) put("approve", "1")
        },
    ),
)
