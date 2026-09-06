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
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.network.dto.PcCareSlotDto
import sg.mesha.goatos.feature.pccare.PcCareSlotState

/**
 * A hardware reader scan arriving WHILE a slot video is being recorded must record the scanned
 * ANIMAL into the task but never retarget the in-flight capture — the capture's (tag, slot)
 * identity is fixed at Record-tap time (the weighing mid-recording defect class).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PcCareScanWhileRecordingTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private val detail = pcCareTaskDtoFixture(
        category = "deworming",
        expectedSlots = listOf(PcCareSlotDto(fieldKey = "video", label = "Deworming video")),
    )

    @Test
    fun `reader scan mid-recording records the animal but never retargets the capture`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = detail
        repo.animalsFlow.value = listOf(pcCareAnimalEntity(tag = "t1"))
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource()
        val reader = PcCareFakeReaderPort()
        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo, proofSource = proofSource, reader = reader)
        val collectJob = launch { vm.state.collect { } }
        runCurrent()

        // Open t1's camera and HOLD it open (the recording is in flight).
        val gate = proofSource.queueGate()
        vm.onEvent(sg.mesha.goatos.feature.pccare.PcCareTaskEvent.RecordSlot("t1", "video"))
        runCurrent()

        // A NEW animal is scanned by the reader while the camera is recording t1.
        reader.emitRead("t2")
        runCurrent()
        assertTrue(
            "the scanned animal must be recorded into the task",
            repo.animalsFlow.value.any { it.normalizedTag == "t2" },
        )

        // The recording finishes; the capture must land on t1's slot — never t2's.
        gate.complete(CapturedVideo(localUri = "file:///clip.mp4", startedAtMs = 0L, endedAtMs = 12_000L))
        runCurrent()

        assertEquals(1, proofRepo.captureCalls.size)
        val call = proofRepo.captureCalls.single()
        assertEquals(pcCareSlotProofFieldKey("t1", "video"), call.fieldKey)
        // The proof platform's create validation requires a UUID subject, so the subject is the
        // TASK; the animal identity stays pinned on the rfid tag and the slot field key.
        assertEquals("task-1", call.subjectId)
        assertEquals("t1", call.rfidTag)
        // The slot registration rode the same fixed identity.
        assertEquals(listOf("task-1", "t1", "video"), repo.slotRegistrations.single().take(3))
        collectJob.cancel()
    }

    @Test
    fun `a second Record tap while a capture is in flight is refused`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = detail
        repo.animalsFlow.value = listOf(pcCareAnimalEntity(tag = "t1"), pcCareAnimalEntity(tag = "t2"))
        val proofSource = FakeProofCaptureSource()
        val vm = buildPcCareTaskViewModel(repo, proofSource = proofSource)
        val collectJob = launch { vm.state.collect { } }
        runCurrent()

        val gate = proofSource.queueGate()
        vm.onEvent(sg.mesha.goatos.feature.pccare.PcCareTaskEvent.RecordSlot("t1", "video"))
        runCurrent()

        // A second Record tap (another animal's slot) while the camera is open does nothing.
        vm.onEvent(sg.mesha.goatos.feature.pccare.PcCareTaskEvent.RecordSlot("t2", "video"))
        runCurrent()
        assertEquals(1, proofSource.captureCount)
        assertEquals("Finish the current video first.", vm.state.value.message)

        gate.complete(null)
        runCurrent()
        collectJob.cancel()
    }

    @Test
    fun `failed deworming slot retries saved video instead of opening camera again`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = detail
        repo.animalsFlow.value = listOf(pcCareAnimalEntity(tag = "t1", scannedByName = "Amit Kumar"))
        val proofRepo = FakeProofCaptureRepository()
        proofRepo.seedProofs(
            ProofCaptureRow(
                id = "proof-failed-video",
                fieldKey = pcCareSlotProofFieldKey("t1", "video"),
                proofSubject = ProofSubject.OTHER,
                subjectId = "task-1",
                localUri = "file:///saved-deworming-proof.mp4",
                mimeType = "video/mp4",
                caption = "Deworming video · t1",
                rfidTag = "t1",
                capturedAtMs = 1L,
                capturedStartMs = 0L,
                capturedEndMs = 12_000L,
                capturedByPrincipalId = null,
                syncStatus = CaptureSyncStatus.FAILED,
                serverProofId = null,
                outboxItemId = "proof-upload-outbox",
                lastError = "network failed",
            ),
        )
        val proofSource = FakeProofCaptureSource()
        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo, proofSource = proofSource)
        val collectJob = launch { vm.state.collect { } }
        runCurrent()

        val slot = vm.state.value.animals.single().slots.single()
        assertEquals(PcCareSlotState.FAILED, slot.state)
        assertEquals("Upload failed. Retrying", slot.statusLabel)

        vm.onEvent(sg.mesha.goatos.feature.pccare.PcCareTaskEvent.RecordSlot("t1", "video"))
        runCurrent()

        assertTrue(proofRepo.captureCalls.isEmpty())
        assertEquals(listOf("proof-failed-video"), proofRepo.retryUploadIds)
        assertEquals("Saved video upload retry queued.", vm.state.value.message)
        collectJob.cancel()
    }

    @Test
    fun `animal slot video emits ordered durable diagnostics for every local handoff`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = detail
        repo.animalsFlow.value = listOf(pcCareAnimalEntity(tag = "t1", scannedByName = "Amit Kumar"))
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource(
            mutableListOf(CapturedVideo(localUri = "file:///clip.mp4", startedAtMs = 0L, endedAtMs = 12_000L)),
        )
        val analytics = FakeAnalyticsPort()
        val vm = buildPcCareTaskViewModel(
            repo = repo,
            proofRepo = proofRepo,
            proofSource = proofSource,
            analytics = analytics,
        )
        val collectJob = launch { vm.state.collect { } }
        runCurrent()

        vm.onEvent(sg.mesha.goatos.feature.pccare.PcCareTaskEvent.RecordSlot("t1", "video"))
        runCurrent()

        val eventNames = analytics.events.map { it.first }
        assertEquals(
            listOf(
                AnalyticsEvents.PC_CARE_SLOT_CAPTURE_STARTED,
                AnalyticsEvents.PC_CARE_SLOT_CAPTURE_RESULT,
                AnalyticsEvents.PC_CARE_SLOT_ROOM_WRITTEN,
                AnalyticsEvents.PC_CARE_SLOT_UPLOAD_ENQUEUED,
                AnalyticsEvents.PC_CARE_SLOT_REGISTRATION,
                AnalyticsEvents.PC_CARE_SLOT_CAPTURED,
            ),
            eventNames.filter { it.startsWith("pc_care_slot_") },
        )

        val byName = analytics.events.associateBy { it.first }
        val started = requireNotNull(byName[AnalyticsEvents.PC_CARE_SLOT_CAPTURE_STARTED])
        assertEquals("video", started.second[AnalyticsEvents.Params.KIND])
        assertEquals("video", started.second["media_kind"])

        val captureResult = requireNotNull(byName[AnalyticsEvents.PC_CARE_SLOT_CAPTURE_RESULT])
        assertEquals("success", captureResult.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("video", captureResult.second[AnalyticsEvents.Params.KIND])
        assertEquals("t1", captureResult.second[AnalyticsEvents.Params.RFID])
        assertEquals("t1", captureResult.second["normalized_rfid"])
        assertEquals("video", captureResult.second[AnalyticsEvents.Params.FIELD])
        assertEquals(pcCareSlotProofFieldKey("t1", "video"), captureResult.second["field_key"])

        val roomWritten = requireNotNull(byName[AnalyticsEvents.PC_CARE_SLOT_ROOM_WRITTEN])
        assertEquals("success", roomWritten.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("proof-0", roomWritten.second["local_proof_row_id"])

        val uploadEnqueued = requireNotNull(byName[AnalyticsEvents.PC_CARE_SLOT_UPLOAD_ENQUEUED])
        assertEquals("success", uploadEnqueued.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("proof-0", uploadEnqueued.second["local_proof_row_id"])
        assertEquals("proof-outbox-1", uploadEnqueued.second[AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])

        val registration = requireNotNull(byName[AnalyticsEvents.PC_CARE_SLOT_REGISTRATION])
        assertEquals("enqueued", registration.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("slot-outbox-1", registration.second[AnalyticsEvents.Params.OUTBOX_ITEM_ID])

        val captured = requireNotNull(byName[AnalyticsEvents.PC_CARE_SLOT_CAPTURED])
        assertEquals("success", captured.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("video", captured.second[AnalyticsEvents.Params.KIND])
        assertEquals("video", captured.second["media_kind"])
        assertEquals("slot-outbox-1", captured.second[AnalyticsEvents.Params.OUTBOX_ITEM_ID])
        collectJob.cancel()
    }
}
