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
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.network.dto.PcCareRemovalPenDto
import sg.mesha.goatos.core.network.dto.PcCareSlotDto
import sg.mesha.goatos.feature.pccare.PcCareTaskEvent

/**
 * A ROUND's feed & water removal is ONE card proved PEN BY PEN (maintainer decision
 * 2026-09-05): one feed video and one water video per pen, because a single clip stretched
 * over four pens proves nothing and the verifier cannot tell which pen was actually emptied.
 *
 * A LEGACY single-pen removal has no pens and keeps the flat two-slot face unchanged.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PcCareRemovalPenSlotsTest {
    private val dispatcher = UnconfinedTestDispatcher()

    private val feedSlot = PcCareSlotDto(fieldKey = "feed_video", label = "Feed removal video")
    private val waterSlot = PcCareSlotDto(fieldKey = "water_video", label = "Water removal video")

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun removalRepo(pens: List<PcCareRemovalPenDto>) = FakePcCareRepository().apply {
        removalPens = pens
        detailFlow.value = pcCareTaskDtoFixture(
            category = "feed_water_removal",
            expectedSlots = listOf(feedSlot, waterSlot),
        )
    }

    @Test
    fun `a round removal shows both videos for every pen, each labelled by its pen`() = runTest(dispatcher) {
        val repo = removalRepo(
            listOf(
                PcCareRemovalPenDto(removalPenId = "pen-a", gatedTaskId = "task-a", penLabel = "Godel 1 - Part 3"),
                PcCareRemovalPenDto(removalPenId = "pen-b", gatedTaskId = "task-b", penLabel = "Castro 2"),
            ),
        )
        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        val slots = vm.state.value.taskProofSlots
        assertEquals("two pens x two videos", 4, slots.size)
        // The key carries the PEN as well as the slot: without it the same two slot names
        // repeat once per pen and the second pen's videos overwrite the first pen's.
        assertEquals(
            listOf("task-a::feed_video", "task-a::water_video", "task-b::feed_video", "task-b::water_video"),
            slots.map { it.fieldKey },
        )
        // The pen label is backend-composed and rendered verbatim, so the operator can tell
        // which pen each clip is for.
        assertTrue(slots[0].label.startsWith("Godel 1 - Part 3 · "))
        assertTrue(slots[2].label.startsWith("Castro 2 · "))
        collectJob.cancel()
    }

    @Test
    fun `a legacy single-pen removal keeps the flat two-slot face`() = runTest(dispatcher) {
        val repo = removalRepo(emptyList())
        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        val slots = vm.state.value.taskProofSlots
        assertEquals(2, slots.size)
        assertEquals(listOf("feed_video", "water_video"), slots.map { it.fieldKey })
        collectJob.cancel()
    }

    /**
     * RECOVERY: the app dies after the video uploads but before its registration is enqueued.
     * On reopen, reconcileSlotRegistrations() must reattach that clip to ITS PEN.
     *
     * The pen-scoped key is "<gated task id>::<slot>" and it CONTAINS A COLON, so the
     * ANIMAL-slot branch (`fieldKey.contains(':')`) claimed it first and called
     * registerSlotProof with tag="<uuid>" and slot=":feed_video". That is not a missed retry —
     * it is a WRONG WRITE, filing a pen's removal video as some animal's scan proof.
     */
    @Test
    fun `an interrupted pen removal video is reattached to its pen, never to an animal`() = runTest(dispatcher) {
        val gatedTaskId = "eee9fdaa-4bd5-468b-818d-5b7072e24e31"
        val repo = removalRepo(
            listOf(PcCareRemovalPenDto(removalPenId = "pen-a", gatedTaskId = gatedTaskId, penLabel = "Castro 1")),
        )
        val proofRepo = FakeProofCaptureRepository()
        proofRepo.seedProofs(
            ProofCaptureRow(
                id = "proof-pen-feed",
                fieldKey = "$gatedTaskId::feed_video",
                proofSubject = ProofSubject.OTHER,
                subjectId = "task-1",
                localUri = "file:///feed.mp4",
                mimeType = "video/mp4",
                caption = "Feed removal proof",
                capturedAtMs = 10L,
                capturedStartMs = 0L,
                capturedEndMs = 1_000L,
                capturedByPrincipalId = null,
                syncStatus = CaptureSyncStatus.PENDING,
                serverProofId = null,
                outboxItemId = "proof-outbox-pen",
                lastError = null,
                processingState = "CAPTURED_ORIGINAL",
            ),
        )

        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()
        repo.taskProofRegistrations.clear()
        repo.slotRegistrations.clear()
        vm.onEvent(PcCareTaskEvent.Refresh)
        runCurrent()

        assertEquals(
            "a pen's removal video must never be filed as an animal scan proof",
            emptyList<List<String>>(),
            repo.slotRegistrations,
        )
        assertEquals(
            listOf(listOf("task-1", "feed_video", "proof-outbox-pen", gatedTaskId)),
            repo.taskProofRegistrations,
        )
        collectJob.cancel()
    }
}
