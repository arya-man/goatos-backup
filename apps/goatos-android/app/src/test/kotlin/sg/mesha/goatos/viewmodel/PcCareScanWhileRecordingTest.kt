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
import sg.mesha.goatos.core.network.dto.PcCareSlotDto

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
        assertEquals("t1", call.subjectId)
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
}
