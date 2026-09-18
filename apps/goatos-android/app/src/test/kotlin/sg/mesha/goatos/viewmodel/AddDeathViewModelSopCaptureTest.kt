package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakePhotoCaptureSource
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.data.DeathCauseVocabulary
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.network.dto.CountsCaptureCardDto
import sg.mesha.goatos.core.network.dto.CountsCaptureCardResponseDto
import sg.mesha.goatos.core.network.dto.DeathCauseCatalogDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto
import sg.mesha.goatos.feature.counts.AddDeathEvent

/**
 * The Add death form follows the published SOP CAPTURE CARD, and without one sends exactly the
 * request it sends today (deploy-day rule).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AddDeathViewModelSopCaptureTest {
    private val dispatcher = StandardTestDispatcher()
    private lateinit var sync: RecordingAddSyncRepository
    private lateinit var cards: FakeCountsCaptureCardRepository
    private lateinit var videos: FakeProofCaptureSource
    private lateinit var proofs: FakeProofCaptureRepository
    private lateinit var drafts: InMemoryCaptureDraftRepository

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
        sync = RecordingAddSyncRepository()
        cards = FakeCountsCaptureCardRepository()
        videos = FakeProofCaptureSource()
        proofs = FakeProofCaptureRepository()
        drafts = InMemoryCaptureDraftRepository()
    }

    @After
    fun tearDown() = Dispatchers.resetMain()

    private val counts = FakeAddCountsRepository()

    private fun newViewModel(savedStateHandle: SavedStateHandle = SavedStateHandle()) = AddDeathViewModel(
        sync,
        counts,
        EmptyDeathCauses(),
        NoopAddAnalyticsPort(),
        NoopAddCrashReporter(),
        savedStateHandle,
        cards,
        drafts,
        videos,
        FakePhotoCaptureSource(),
        proofs,
    )

    private fun selectAnimalWithAccount(vm: AddDeathViewModel) {
        vm.onEvent(AddDeathEvent.EditAnimalQuery("TAG-77"))
        vm.onEvent(AddDeathEvent.LookupAnimals)
        dispatcher.scheduler.advanceUntilIdle()
        vm.onEvent(AddDeathEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(AddDeathEvent.EditReason("Found dead in the pen"))
    }

    @Test
    fun `seeded behaviour - no published card sends no capture and gates as today`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()
        selectAnimalWithAccount(vm)
        advanceUntilIdle()

        assertTrue(vm.state.value.captureCard.isEmpty)
        assertTrue(vm.state.value.canSubmit)
        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()

        assertEquals(GOAT_ID, sync.lastDeath?.goatId)
        assertNull("no card published: the request carries no sop_capture", sync.lastDeathCapture)
    }

    @Test
    fun `a published card files the report video under the animal and sends it`() = runTest(dispatcher) {
        cards.publish(deathCard())
        val vm = newViewModel()
        advanceUntilIdle()
        selectAnimalWithAccount(vm)
        advanceUntilIdle()
        assertFalse("the compulsory video is not captured yet", vm.state.value.canSubmit)

        videos.queue(CapturedVideo(localUri = "file:///carcass.mp4", startedAtMs = 1_000L, endedAtMs = 5_000L))
        vm.onEvent(AddDeathEvent.CaptureSlot("carcass_video", null))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)

        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()

        val capture = sync.lastDeathCapture ?: error("capture payload missing")
        assertEquals(SOP_VERSION_ID, capture.sopVersionId)
        assertEquals(setOf("carcass_video"), capture.slotProofs.keys)
        val call = proofs.captureCalls.single()
        assertEquals(ProofSubject.GOAT, call.subject)
        assertEquals(GOAT_ID, call.subjectId)
    }

    @Test
    fun `captured report video survives ViewModel recreation before submit`() = runTest(dispatcher) {
        cards.publish(deathCard())
        val saved = SavedStateHandle()
        val first = newViewModel(saved)
        advanceUntilIdle()
        selectAnimalWithAccount(first)
        videos.queue(CapturedVideo(localUri = "file:///carcass.mp4", startedAtMs = 1_000L, endedAtMs = 5_000L))
        first.onEvent(AddDeathEvent.CaptureSlot("carcass_video", null))
        advanceUntilIdle()

        val reopened = newViewModel(saved)
        advanceUntilIdle()

        assertTrue("the recreated form still sees the video slot", reopened.state.value.captureCard.slots.single().captured)
        selectAnimalWithAccount(reopened)
        advanceUntilIdle()
        assertTrue(reopened.state.value.canSubmit)
        reopened.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()

        assertEquals(proofs.allRows().single().outboxItemId, sync.lastDeathCapture?.slotProofs?.get("carcass_video")?.outboxItemId)
    }

    @Test
    fun `switching the animal drops the proofs recorded for the first one and never sends them`() = runTest(dispatcher) {
        cards.publish(deathCard())
        counts.lookupResults = listOf(
            GoatSearchItemDto(goatId = GOAT_ID, displayId = "G-77", animalIdentifier1 = "TAG-77", lifecycleStatus = "alive", rowVersion = 7),
            GoatSearchItemDto(goatId = OTHER_GOAT_ID, displayId = "G-78", animalIdentifier1 = "TAG-78", lifecycleStatus = "alive", rowVersion = 3),
        )
        val vm = newViewModel()
        advanceUntilIdle()
        selectAnimalWithAccount(vm)
        advanceUntilIdle()

        videos.queue(CapturedVideo(localUri = "file:///first.mp4", startedAtMs = 1_000L, endedAtMs = 5_000L))
        vm.onEvent(AddDeathEvent.CaptureSlot("carcass_video", null))
        advanceUntilIdle()
        assertTrue("the first animal's video is captured", vm.state.value.canSubmit)

        // The operator picked the wrong animal and switches to the right one.
        vm.onEvent(AddDeathEvent.SelectAnimal(OTHER_GOAT_ID))
        advanceUntilIdle()
        assertEquals(OTHER_GOAT_ID, vm.state.value.selectedAnimal?.goatId)
        assertFalse("a video filed under the first animal must not satisfy the second animal's report", vm.state.value.canSubmit)
        assertTrue(vm.state.value.captureCard.slots.none { it.captured })

        videos.queue(CapturedVideo(localUri = "file:///second.mp4", startedAtMs = 6_000L, endedAtMs = 9_000L))
        vm.onEvent(AddDeathEvent.CaptureSlot("carcass_video", null))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)
        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()

        assertEquals(OTHER_GOAT_ID, sync.lastDeath?.goatId)
        assertEquals(listOf(GOAT_ID, OTHER_GOAT_ID), proofs.captureCalls.map { it.subjectId })
        val firstOutbox = proofs.allRows().single { it.subjectId == GOAT_ID }.outboxItemId
        val secondOutbox = proofs.allRows().single { it.subjectId == OTHER_GOAT_ID }.outboxItemId
        val sent = sync.lastDeathCapture?.slotProofs?.get("carcass_video")?.outboxItemId
        assertEquals("the report sends the second animal's video", secondOutbox, sent)
        assertTrue("the first animal's video is never sent", sent != firstOutbox)
    }

    @Test
    fun `a refused report shows the server's words and the corrected report goes out under a fresh key`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()
        selectAnimalWithAccount(vm)
        advanceUntilIdle()
        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()
        assertEquals(1, sync.deathKeys.size)

        sync.emitItem(
            sg.mesha.goatos.core.data.sync.SyncQueueItem(
                id = "outbox-death-1",
                idempotencyKey = sync.deathKeys.single(),
                opType = "COUNTS_DEATH",
                groupKey = GOAT_ID,
                status = sg.mesha.goatos.core.data.sync.SyncItemStatus.FAILED,
                attemptCount = 1,
                maxAttempts = 8,
                conflict = true,
                createdAt = 1L,
                updatedAt = 1L,
                lastError = "Write the other answer for: Suspected cause",
                resultJson = null,
            ),
        )
        advanceUntilIdle()
        assertEquals(sg.mesha.goatos.feature.counts.CountsWriteStatus.FAILED, vm.state.value.result.status)
        assertEquals("Write the other answer for: Suspected cause", vm.state.value.result.message)

        // The operator corrects the form: the refusal clears and the report can go again.
        vm.onEvent(AddDeathEvent.EditReason("Found dead in the pen at dawn"))
        advanceUntilIdle()
        assertEquals(sg.mesha.goatos.feature.counts.CountsWriteStatus.IDLE, vm.state.value.result.status)
        assertTrue(vm.state.value.canSubmit)
        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()

        assertEquals(2, sync.deathKeys.size)
        assertTrue("the corrected report is a new write, never the refused key", sync.deathKeys[0] != sync.deathKeys[1])
    }

    @Test
    fun `other without its written answer blocks the submit on the phone and names the question`() = runTest(dispatcher) {
        cards.publish(
            CountsCaptureCardResponseDto(
                kind = "death",
                sopCode = "counts.death",
                sopVersionId = SOP_VERSION_ID,
                card = CountsCaptureCardDto(
                    questions = listOf(
                        sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto(
                            id = "cause",
                            kind = "choice",
                            title = "Suspected cause",
                            required = true,
                            options = listOf(
                                sg.mesha.goatos.core.network.dto.WeighingSopOptionDto("bloat", "Bloat"),
                                sg.mesha.goatos.core.network.dto.WeighingSopOptionDto("other", "Other"),
                            ),
                            allowOther = true,
                        ),
                    ),
                ),
            ),
        )
        val vm = newViewModel()
        advanceUntilIdle()
        selectAnimalWithAccount(vm)
        vm.onEvent(AddDeathEvent.Answer("cause", "other"))
        advanceUntilIdle()

        assertFalse("the server would refuse other without its text", vm.state.value.canSubmit)
        assertEquals("Write the other answer for: Suspected cause", vm.state.value.validationMessage)
        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()
        assertNull("nothing is queued for a refusal the phone can already see", sync.lastDeath)

        vm.onEvent(AddDeathEvent.Answer("cause_other", "Snake bite"))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)
    }

    private fun deathCard() = CountsCaptureCardResponseDto(
        kind = "death",
        sopCode = "counts.death",
        sopVersionId = SOP_VERSION_ID,
        card = CountsCaptureCardDto(
            proofs = listOf(WeighingRemovalProofSlotDto(key = "carcass_video", title = "Carcass", kind = "video", required = true)),
        ),
    )

    private class EmptyDeathCauses : DeathCauseVocabulary {
        override fun observeDeathCauses(): Flow<DeathCauseCatalogDto?> = flowOf(null)
        override suspend fun refreshDeathCauses(): Result<Unit> = Result.success(Unit)
    }

    private companion object {
        const val GOAT_ID = "44444444-4444-4444-4444-444444444444"
        const val OTHER_GOAT_ID = "55555555-5555-5555-5555-555555555555"
        const val SOP_VERSION_ID = "66666666-6666-6666-6666-666666666666"
    }
}
