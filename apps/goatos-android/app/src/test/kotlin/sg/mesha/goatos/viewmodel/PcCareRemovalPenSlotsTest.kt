package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
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
        ).copy(captureMode = "task_proof")
    }

    @Test
    fun `feed water removal task title never shows the internal category key`() = runTest(dispatcher) {
        val repo = removalRepo(emptyList())
        val vm = buildPcCareTaskViewModel(
            repo = repo,
            title = "feed_water_removal",
            category = "feed_water_removal",
        )
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        assertEquals("Remove feed & water", vm.state.value.title)
        collectJob.cancel()
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
    fun `a partial cached round removal refreshes and renders still missing pens on reopen`() = runTest(dispatcher) {
        val gandhi = PcCareRemovalPenDto(
            removalPenId = "pen-a",
            gatedTaskId = "task-a",
            penLabel = "Gandhi 1 - Part 1",
            feedProofRef = "server-feed-a",
            waterProofRef = "server-water-a",
        )
        val godel = PcCareRemovalPenDto(removalPenId = "pen-b", gatedTaskId = "task-b", penLabel = "Godel 1 - Part 1")
        val yashoda = PcCareRemovalPenDto(removalPenId = "pen-c", gatedTaskId = "task-c", penLabel = "Yashoda 1 - Part 2")
        val repo = removalRepo(listOf(gandhi, godel, yashoda))
        repo.emitRemovalPens(listOf(gandhi))

        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals("feed/water detail should repair a partial cached pen list", 1, repo.removalPenRefreshCalls)
        val slots = vm.state.value.taskProofSlots
        assertEquals("three pens x two videos", 6, slots.size)
        assertEquals(
            listOf(
                "task-a::feed_video",
                "task-a::water_video",
                "task-b::feed_video",
                "task-b::water_video",
                "task-c::feed_video",
                "task-c::water_video",
            ),
            slots.map { it.fieldKey },
        )
        assertEquals(sg.mesha.goatos.feature.pccare.PcCareSlotState.SYNCED, slots[0].state)
        assertEquals(sg.mesha.goatos.feature.pccare.PcCareSlotState.SYNCED, slots[1].state)
        assertEquals(sg.mesha.goatos.feature.pccare.PcCareSlotState.EMPTY, slots[2].state)
        assertEquals(sg.mesha.goatos.feature.pccare.PcCareSlotState.EMPTY, slots[3].state)
        assertEquals(sg.mesha.goatos.feature.pccare.PcCareSlotState.EMPTY, slots[4].state)
        assertEquals(sg.mesha.goatos.feature.pccare.PcCareSlotState.EMPTY, slots[5].state)
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

    @Test
    fun `feed water removal pen refs from backend turn matching pen slots green`() = runTest(dispatcher) {
        val gatedTaskId = "eee9fdaa-4bd5-468b-818d-5b7072e24e31"
        val repo = removalRepo(
            listOf(
                PcCareRemovalPenDto(
                    removalPenId = "pen-a",
                    gatedTaskId = gatedTaskId,
                    penLabel = "Castro 1",
                    feedProofRef = "server-proof-feed",
                    waterProofRef = "server-proof-water",
                ),
            ),
        )
        repo.proofDownloadUrls["server-proof-feed"] = "https://proof.local/feed.mp4"
        repo.proofDownloadUrls["server-proof-water"] = "https://proof.local/water.mp4"

        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect {} }
        advanceUntilIdle()

        val slots = vm.state.value.taskProofSlots.associateBy { it.fieldKey }
        val feed = slots.getValue("$gatedTaskId::feed_video")
        val water = slots.getValue("$gatedTaskId::water_video")
        assertEquals(sg.mesha.goatos.feature.pccare.PcCareSlotState.SYNCED, feed.state)
        assertEquals(sg.mesha.goatos.feature.pccare.PcCareSlotState.SYNCED, water.state)
        assertEquals("Proof sent", feed.statusLabel)
        assertEquals("https://proof.local/feed.mp4", feed.previewPath)
        assertEquals("https://proof.local/water.mp4", water.previewPath)
        assertTrue(vm.state.value.submitEnabled)
        collectJob.cancel()
    }

    @Test
    fun `feed water removal pen refs emit backend ack analytics`() = runTest(dispatcher) {
        val analytics = FakeAnalyticsPort()
        val gatedTaskId = "eee9fdaa-4bd5-468b-818d-5b7072e24e31"
        val repo = removalRepo(
            listOf(
                PcCareRemovalPenDto(
                    removalPenId = "pen-a",
                    gatedTaskId = gatedTaskId,
                    penLabel = "Castro 1",
                    feedProofRef = "server-proof-feed",
                    waterProofRef = "server-proof-water",
                ),
            ),
        )

        val vm = buildPcCareTaskViewModel(repo, analytics = analytics)
        val collectJob = launch { vm.state.collect {} }
        advanceUntilIdle()

        val events = analytics.events.filter { it.first == AnalyticsEvents.PC_CARE_TASK_PROOF_BUSINESS_ACK }
        assertEquals(2, events.size)
        assertTrue(events.any { it.second["field_key"] == "$gatedTaskId::feed_video" && it.second["server_proof_id"] == "server-proof-feed" })
        assertTrue(events.any { it.second["field_key"] == "$gatedTaskId::water_video" && it.second["server_proof_id"] == "server-proof-water" })
        assertTrue(events.all { it.second[AnalyticsEvents.Params.OUTCOME] == "business_ack_visible" })
        assertTrue(events.all { it.second[AnalyticsEvents.Params.SOURCE] == "backend_removal_pen" })
        collectJob.cancel()
    }

    @Test
    fun `feed water removal pen refs pass submit tap and confirm gates`() = runTest(dispatcher) {
        val gatedTaskId = "eee9fdaa-4bd5-468b-818d-5b7072e24e31"
        val repo = removalRepo(
            listOf(
                PcCareRemovalPenDto(
                    removalPenId = "pen-a",
                    gatedTaskId = gatedTaskId,
                    penLabel = "Castro 1",
                    feedProofRef = "server-proof-feed",
                    waterProofRef = "server-proof-water",
                ),
            ),
        )

        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(PcCareTaskEvent.Submit)
        runCurrent()
        assertTrue(vm.state.value.showSubmitConfirmation)
        vm.onEvent(PcCareTaskEvent.ConfirmSubmit)
        runCurrent()

        assertEquals(listOf("task-1" to 1), repo.submitCalls)
        assertTrue(vm.state.value.submitQueued)
        collectJob.cancel()
    }

    @Test
    fun `feed water preview actions include task proof trace ids`() = runTest(dispatcher) {
        val analytics = FakeAnalyticsPort()
        val gatedTaskId = "eee9fdaa-4bd5-468b-818d-5b7072e24e31"
        val slotKey = "$gatedTaskId::feed_video"
        val repo = removalRepo(
            listOf(PcCareRemovalPenDto(removalPenId = "pen-a", gatedTaskId = gatedTaskId, penLabel = "Castro 1")),
        )
        val proofRepo = FakeProofCaptureRepository()
        proofRepo.seedProofs(
            ProofCaptureRow(
                id = "proof-pen-feed",
                fieldKey = slotKey,
                proofSubject = ProofSubject.OTHER,
                subjectId = "task-1",
                localUri = "file:///feed.mp4",
                mimeType = "video/mp4",
                caption = "Feed removal proof",
                capturedAtMs = 10L,
                capturedStartMs = 0L,
                capturedEndMs = 1_000L,
                capturedByPrincipalId = null,
                syncStatus = CaptureSyncStatus.SYNCED,
                serverProofId = "server-proof-feed",
                outboxItemId = "proof-outbox-feed",
                lastError = null,
                processingState = "UPLOADED",
            ),
        )
        val vm = buildPcCareTaskViewModel(
            repo = repo,
            proofRepo = proofRepo,
            analytics = analytics,
            title = "Remove feed & water",
            category = "feed_water_removal",
        )
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.ProofPreviewAction(slotKey, "video", "fullscreen_open"))

        val event = analytics.events.last()
        assertEquals(AnalyticsEvents.PC_CARE_FEED_WATER_PROOF_PREVIEW, event.first)
        assertEquals("fullscreen_open", event.second[AnalyticsEvents.Params.ACTION])
        assertEquals("preview_action", event.second[AnalyticsEvents.Params.OUTCOME])
        assertEquals("proof-pen-feed", event.second["local_proof_row_id"])
        assertEquals("proof-outbox-feed", event.second[AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])
        assertEquals("server-proof-feed", event.second["server_proof_id"])
        assertEquals("feed_water_removal", event.second["category"])
        assertEquals("task_proof", event.second["capture_mode"])
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

    @Test
    fun `a pen removal video captured from camera is accepted by base slot mime and registered to that pen`() = runTest(dispatcher) {
        val gatedTaskId = "eee9fdaa-4bd5-468b-818d-5b7072e24e31"
        val repo = removalRepo(
            listOf(PcCareRemovalPenDto(removalPenId = "pen-a", gatedTaskId = gatedTaskId, penLabel = "Godel 1 - Part 2")),
        )
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource(
            mutableListOf(
                CapturedVideo(
                    localUri = "file:///water.mp4",
                    mimeType = "video/mp4",
                    startedAtMs = 1_000L,
                    endedAtMs = 4_000L,
                    captureSource = "in_app_camera",
                ),
            ),
        )
        val vm = buildPcCareTaskViewModel(repo, proofRepo = proofRepo, proofSource = proofSource)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        val waterField = "$gatedTaskId::water_video"
        vm.onEvent(PcCareTaskEvent.RecordTaskProof(waterField, "video"))
        runCurrent()

        assertEquals(1, proofSource.captureCount)
        assertEquals("video/mp4", proofRepo.captureCalls.single().mimeType)
        assertEquals(waterField, proofRepo.captureCalls.single().fieldKey)
        assertEquals(listOf(listOf("task-1", "water_video", "proof-outbox-1", gatedTaskId)), repo.taskProofRegistrations)
        assertTrue(vm.state.value.message?.contains("Wrong proof type") != true)
        collectJob.cancel()
    }
}
