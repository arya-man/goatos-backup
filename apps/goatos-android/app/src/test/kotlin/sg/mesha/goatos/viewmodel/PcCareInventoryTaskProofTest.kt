package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.CapturedPhoto
import sg.mesha.goatos.capture.FakePhotoCaptureSource
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.pcCareTaskProofRegisterRefreshHook
import sg.mesha.goatos.core.network.dto.PcCareSlotDto
import sg.mesha.goatos.core.network.dto.PcCareTaskProofDto
import sg.mesha.goatos.feature.pccare.PcCareSlotState
import sg.mesha.goatos.feature.pccare.PcCareTaskEvent

@OptIn(ExperimentalCoroutinesApi::class)
class PcCareInventoryTaskProofTest {
    private val dispatcher = UnconfinedTestDispatcher()
    private val stockPhotoSlot = PcCareSlotDto(
        fieldKey = "stock_fridge_photo",
        label = "Fridge stock photo",
        description = "Take a clear photo of vaccine stock in the fridge",
    )
    private val stockVideoSlot = PcCareSlotDto(
        fieldKey = "stock_fridge_video",
        label = "Fridge stock video",
        description = "Show vaccine stock in the fridge",
    )

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun proof(
        fieldKey: String = stockVideoSlot.fieldKey,
        syncStatus: CaptureSyncStatus,
        serverProofId: String? = null,
        outboxItemId: String? = null,
        mimeType: String = "video/mp4",
    ) = ProofCaptureRow(
        id = "proof-stock-$fieldKey",
        fieldKey = fieldKey,
        proofSubject = ProofSubject.OTHER,
        subjectId = "task-1",
        localUri = if (mimeType.startsWith("image/", ignoreCase = true)) "file:///stock.jpg" else "file:///stock.mp4",
        mimeType = mimeType,
        caption = "Fridge stock proof",
        capturedAtMs = 10L,
        capturedStartMs = 0L,
        capturedEndMs = 1_000L,
        capturedByPrincipalId = null,
        syncStatus = syncStatus,
        serverProofId = serverProofId,
        outboxItemId = outboxItemId,
        lastError = null,
    )

    @Test
    fun `inventory task proof gate blocks until fridge photo and video are uploaded`() {
        val expectedSlots = listOf(stockPhotoSlot, stockVideoSlot)
        val missing = pcCareEvaluateTaskProofSubmit(expectedSlots, emptyList(), emptyList(), capturingSlotKey = null)
        assertFalse(missing.ready)
        assertEquals("Record the fridge stock photo and video first", missing.blockedReason)

        val uploading = pcCareEvaluateTaskProofSubmit(
            expectedSlots,
            listOf(
                proof(stockPhotoSlot.fieldKey, CaptureSyncStatus.SYNCED, "server-proof-photo"),
                proof(stockVideoSlot.fieldKey, CaptureSyncStatus.PENDING),
            ),
            emptyList(),
            null,
        )
        assertFalse(uploading.ready)
        assertEquals("Proof is still uploading", uploading.blockedReason)

        val photoOnly = pcCareEvaluateTaskProofSubmit(
            expectedSlots,
            listOf(proof(stockPhotoSlot.fieldKey, CaptureSyncStatus.SYNCED, "server-proof-photo", mimeType = "image/jpeg")),
            emptyList(),
            null,
        )
        assertFalse(photoOnly.ready)
        assertEquals("Record the fridge stock photo and video first", photoOnly.blockedReason)

        val videoOnly = pcCareEvaluateTaskProofSubmit(
            expectedSlots,
            listOf(proof(stockVideoSlot.fieldKey, CaptureSyncStatus.SYNCED, "server-proof-video")),
            emptyList(),
            null,
        )
        assertFalse(videoOnly.ready)
        assertEquals("Record the fridge stock photo and video first", videoOnly.blockedReason)

        val uploaded = pcCareEvaluateTaskProofSubmit(
            expectedSlots,
            listOf(
                proof(stockPhotoSlot.fieldKey, CaptureSyncStatus.SYNCED, "server-proof-photo", mimeType = "image/jpeg"),
                proof(stockVideoSlot.fieldKey, CaptureSyncStatus.SYNCED, "server-proof-video"),
            ),
            emptyList(),
            null,
        )
        assertTrue(uploaded.ready)

        val capturedElsewhere = pcCareEvaluateTaskProofSubmit(
            expectedSlots,
            emptyList(),
            listOf(
                PcCareTaskProofDto(slotKey = stockPhotoSlot.fieldKey, proofRef = "server-proof-photo", capturedByName = "Chandrakant"),
                PcCareTaskProofDto(slotKey = stockVideoSlot.fieldKey, proofRef = "server-proof-video", capturedByName = "Chandrakant"),
            ),
            null,
        )
        assertTrue(capturedElsewhere.ready)
    }

    @Test
    fun `inventory task proof chip uses task-level field key`() {
        val empty = pcCareBuildTaskProofSlot(stockVideoSlot, emptyList(), emptyList(), capturingSlotKey = null)
        assertEquals(PcCareSlotState.EMPTY, empty.state)
        assertTrue(empty.canRecord)

        val captured = pcCareBuildTaskProofSlot(stockVideoSlot, listOf(proof(syncStatus = CaptureSyncStatus.SYNCED, serverProofId = "server-proof-1")), emptyList(), null)
        assertEquals(PcCareSlotState.SYNCED, captured.state)
        assertEquals("Proof sent", captured.statusLabel)
        assertTrue(captured.canRecord)

        val serverCaptured = pcCareBuildTaskProofSlot(
            stockVideoSlot,
            emptyList(),
            listOf(PcCareTaskProofDto(slotKey = stockVideoSlot.fieldKey, proofRef = "server-proof-2", capturedByName = "Chandrakant")),
            null,
        )
        assertEquals(PcCareSlotState.PEER, serverCaptured.state)
        assertEquals("Captured by Chandrakant", serverCaptured.statusLabel)
        assertTrue(serverCaptured.canRecord)
    }

    @Test
    fun `server task proof from another device satisfies only the matching inventory screen row`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = pcCareTaskDtoFixture(
            category = "inventory_vaccine",
            expectedSlots = listOf(stockPhotoSlot, stockVideoSlot),
            taskProofs = listOf(
                PcCareTaskProofDto(
                    slotKey = stockVideoSlot.fieldKey,
                    proofRef = "server-proof-video",
                    capturedByName = "Chandrakant",
                ),
            ),
        ).copy(captureMode = "task_proof")

        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        val state = vm.state.value
        assertEquals(PcCareSlotState.EMPTY, state.taskProofPhotoSlot?.state)
        assertEquals(PcCareSlotState.PEER, state.taskProofVideoSlot?.state)
        assertEquals("Captured by Chandrakant", state.taskProofVideoSlot?.statusLabel)
        assertFalse(state.submitEnabled)
        assertEquals("Record the fridge stock photo and video first", state.submitBlockedReason)
        collectJob.cancel()
    }

    @Test
    fun `inventory task falls back to stock proof rows when cached capture mode is stale`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = pcCareTaskDtoFixture(
            category = "inventory_vaccine",
            expectedSlots = listOf(stockVideoSlot),
        ).copy(captureMode = "")

        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        val state = vm.state.value
        assertEquals(stockPhotoSlot.fieldKey, state.taskProofPhotoSlot?.fieldKey)
        assertEquals(stockVideoSlot.fieldKey, state.taskProofVideoSlot?.fieldKey)
        assertTrue(state.animals.isEmpty())
        assertFalse(state.submitEnabled)
        assertEquals("Record the fridge stock photo and video first", state.submitBlockedReason)
        collectJob.cancel()
    }

    @Test
    fun `inventory task proof can be satisfied by live camera photo`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val proofRepo = FakeProofCaptureRepository()
        val photoSource = FakePhotoCaptureSource(
            mutableListOf(
                CapturedPhoto(
                    localUri = "file:///stock.jpg",
                    mimeType = "image/jpeg",
                    capturedAtMs = 1_000L,
                    captureSource = "in_app_camera",
                ),
            ),
        )
        repo.detailFlow.value = pcCareTaskDtoFixture(
            category = "inventory_vaccine",
            expectedSlots = listOf(stockPhotoSlot, stockVideoSlot),
        ).copy(captureMode = "task_proof")

        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo, photoSource = photoSource)
        runCurrent()
        vm.onEvent(PcCareTaskEvent.RecordTaskProof(stockPhotoSlot.fieldKey, "photo"))
        runCurrent()

        assertEquals(1, photoSource.captureCount)
        assertEquals("image/jpeg", proofRepo.captureCalls.single().mimeType)
        assertEquals(listOf(listOf("task-1", stockPhotoSlot.fieldKey, "proof-outbox-1")), repo.taskProofRegistrations)
    }

    @Test
    fun `inventory task proof can be satisfied by live camera video`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource(
            mutableListOf(
                CapturedVideo(
                    localUri = "file:///stock.mp4",
                    mimeType = "video/mp4",
                    startedAtMs = 1_000L,
                    endedAtMs = 4_000L,
                    captureSource = "in_app_camera",
                ),
            ),
        )
        repo.detailFlow.value = pcCareTaskDtoFixture(
            category = "inventory_vaccine",
            expectedSlots = listOf(stockPhotoSlot, stockVideoSlot),
        ).copy(captureMode = "task_proof")

        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo, proofSource = proofSource)
        runCurrent()
        vm.onEvent(PcCareTaskEvent.RecordTaskProof(stockVideoSlot.fieldKey, "video"))
        runCurrent()

        assertEquals(1, proofSource.captureCount)
        assertEquals("video/mp4", proofRepo.captureCalls.single().mimeType)
        assertEquals(listOf(listOf("task-1", stockVideoSlot.fieldKey, "proof-outbox-1")), repo.taskProofRegistrations)
    }

    @Test
    fun `task proof refresh hook decodes task proof payload`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val hook = pcCareTaskProofRegisterRefreshHook(repo)

        hook.onSuccess(
            """{"task_id":"task-1","slot_field_key":"stock_fridge_video","proof_outbox_item_id":"proof-outbox-7"}""",
        )

        assertEquals(1, repo.pollCount)
    }

    @Test
    fun `refresh re-enqueues orphaned inventory task proof registration`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val proofRepo = FakeProofCaptureRepository()
        repo.detailFlow.value = pcCareTaskDtoFixture(
            category = "inventory_vaccine",
            expectedSlots = listOf(stockPhotoSlot, stockVideoSlot),
        ).copy(captureMode = "task_proof")
        proofRepo.seedProofs(proof(stockVideoSlot.fieldKey, CaptureSyncStatus.PENDING, outboxItemId = "proof-outbox-orphan"))

        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo)
        runCurrent()
        repo.taskProofRegistrations.clear()
        vm.onEvent(PcCareTaskEvent.Refresh)
        runCurrent()

        assertEquals(listOf(listOf("task-1", stockVideoSlot.fieldKey, "proof-outbox-orphan")), repo.taskProofRegistrations)
        assertEquals(emptyList<List<String>>(), repo.slotRegistrations)
    }

    @Test
    fun `refresh repairs stale stock photo rows that were stored under video slot`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val proofRepo = FakeProofCaptureRepository()
        repo.detailFlow.value = pcCareTaskDtoFixture(
            category = "inventory_vaccine",
            expectedSlots = listOf(stockPhotoSlot, stockVideoSlot),
        ).copy(captureMode = "task_proof")
        proofRepo.seedProofs(
            proof(
                fieldKey = stockVideoSlot.fieldKey,
                syncStatus = CaptureSyncStatus.PENDING,
                outboxItemId = "proof-outbox-stale-photo",
                mimeType = "image/jpeg",
            ),
        )

        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo)
        runCurrent()
        repo.taskProofRegistrations.clear()
        vm.onEvent(PcCareTaskEvent.Refresh)
        runCurrent()

        assertEquals(listOf(listOf("task-1", stockPhotoSlot.fieldKey, "proof-outbox-stale-photo")), repo.taskProofRegistrations)
    }
}
