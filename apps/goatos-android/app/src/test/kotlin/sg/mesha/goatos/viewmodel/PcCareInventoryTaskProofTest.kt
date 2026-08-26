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
    private val stockSlot = PcCareSlotDto(
        fieldKey = "stock_fridge_video",
        label = "Fridge stock proof",
        description = "Show vaccine stock in the fridge",
    )

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun proof(
        syncStatus: CaptureSyncStatus,
        serverProofId: String? = null,
        outboxItemId: String? = null,
    ) = ProofCaptureRow(
        id = "proof-stock",
        fieldKey = stockSlot.fieldKey,
        proofSubject = ProofSubject.OTHER,
        subjectId = "task-1",
        localUri = "file:///stock.mp4",
        mimeType = "video/mp4",
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
    fun `inventory task proof gate blocks until the fridge proof is uploaded`() {
        val missing = pcCareEvaluateTaskProofSubmit(listOf(stockSlot), emptyList(), emptyList(), capturingSlotKey = null)
        assertFalse(missing.ready)
        assertEquals("Record the fridge stock proof first", missing.blockedReason)

        val uploading = pcCareEvaluateTaskProofSubmit(listOf(stockSlot), listOf(proof(CaptureSyncStatus.PENDING)), emptyList(), null)
        assertFalse(uploading.ready)
        assertEquals("Proof is still uploading", uploading.blockedReason)

        val uploaded = pcCareEvaluateTaskProofSubmit(listOf(stockSlot), listOf(proof(CaptureSyncStatus.SYNCED, "server-proof-1")), emptyList(), null)
        assertTrue(uploaded.ready)

        val capturedElsewhere = pcCareEvaluateTaskProofSubmit(
            listOf(stockSlot),
            emptyList(),
            listOf(PcCareTaskProofDto(slotKey = stockSlot.fieldKey, proofRef = "server-proof-2", capturedByName = "Chandrakant")),
            null,
        )
        assertTrue(capturedElsewhere.ready)
    }

    @Test
    fun `inventory task proof chip uses task-level field key`() {
        val empty = pcCareBuildTaskProofSlot(stockSlot, emptyList(), emptyList(), capturingSlotKey = null)
        assertEquals(PcCareSlotState.EMPTY, empty.state)
        assertTrue(empty.canRecord)

        val captured = pcCareBuildTaskProofSlot(stockSlot, listOf(proof(CaptureSyncStatus.SYNCED, "server-proof-1")), emptyList(), null)
        assertEquals(PcCareSlotState.SYNCED, captured.state)
        assertEquals("Proof sent", captured.statusLabel)
        assertTrue(captured.canRecord)

        val serverCaptured = pcCareBuildTaskProofSlot(
            stockSlot,
            emptyList(),
            listOf(PcCareTaskProofDto(slotKey = stockSlot.fieldKey, proofRef = "server-proof-2", capturedByName = "Chandrakant")),
            null,
        )
        assertEquals(PcCareSlotState.PEER, serverCaptured.state)
        assertEquals("Captured by Chandrakant", serverCaptured.statusLabel)
        assertTrue(serverCaptured.canRecord)
    }

    @Test
    fun `server task proof from another device satisfies inventory screen`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = pcCareTaskDtoFixture(
            category = "inventory_vaccine",
            expectedSlots = listOf(stockSlot),
            taskProofs = listOf(
                PcCareTaskProofDto(
                    slotKey = stockSlot.fieldKey,
                    proofRef = "server-proof-remote",
                    capturedByName = "Chandrakant",
                ),
            ),
        ).copy(captureMode = "task_proof")

        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        val state = vm.state.value
        assertEquals(PcCareSlotState.PEER, state.taskProofSlot?.state)
        assertEquals("Captured by Chandrakant", state.taskProofSlot?.statusLabel)
        assertTrue(state.submitEnabled)
        assertEquals("", state.submitBlockedReason)
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
            expectedSlots = listOf(stockSlot),
        ).copy(captureMode = "task_proof")

        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo, photoSource = photoSource)
        runCurrent()
        vm.onEvent(PcCareTaskEvent.RecordTaskProof(stockSlot.fieldKey, "photo"))
        runCurrent()

        assertEquals(1, photoSource.captureCount)
        assertEquals("image/jpeg", proofRepo.captureCalls.single().mimeType)
        assertEquals(listOf(listOf("task-1", stockSlot.fieldKey, "proof-outbox-1")), repo.taskProofRegistrations)
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
            expectedSlots = listOf(stockSlot),
        ).copy(captureMode = "task_proof")

        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo, proofSource = proofSource)
        runCurrent()
        vm.onEvent(PcCareTaskEvent.RecordTaskProof(stockSlot.fieldKey, "video"))
        runCurrent()

        assertEquals(1, proofSource.captureCount)
        assertEquals("video/mp4", proofRepo.captureCalls.single().mimeType)
        assertEquals(listOf(listOf("task-1", stockSlot.fieldKey, "proof-outbox-1")), repo.taskProofRegistrations)
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
            expectedSlots = listOf(stockSlot),
        ).copy(captureMode = "task_proof")
        proofRepo.seedProofs(proof(CaptureSyncStatus.PENDING, outboxItemId = "proof-outbox-orphan"))

        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo)
        runCurrent()
        repo.taskProofRegistrations.clear()
        vm.onEvent(PcCareTaskEvent.Refresh)
        runCurrent()

        assertEquals(listOf(listOf("task-1", stockSlot.fieldKey, "proof-outbox-orphan")), repo.taskProofRegistrations)
        assertEquals(emptyList<List<String>>(), repo.slotRegistrations)
    }
}
