package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.JsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedPhoto
import sg.mesha.goatos.capture.FakePhotoCaptureSource
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.data.CountsCaptureCardRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.network.dto.CountsCaptureCardDto
import sg.mesha.goatos.core.network.dto.CountsCaptureCardResponseDto
import sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto
import sg.mesha.goatos.feature.counts.AddBirthEvent
import sg.mesha.goatos.feature.counts.AddBirthField
import sg.mesha.goatos.rfid.FakeScanSource

/**
 * The Add birth form follows the published SOP CAPTURE CARD (maintainer decisions 4 and 7,
 * 2026-09-16) -- and, by the deploy-day rule, behaves exactly as today until one is published.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AddBirthViewModelSopCaptureTest {
    private val dispatcher = StandardTestDispatcher()
    private lateinit var sync: RecordingAddSyncRepository
    private lateinit var cards: FakeCountsCaptureCardRepository
    private lateinit var photos: FakePhotoCaptureSource
    private lateinit var proofs: FakeProofCaptureRepository
    private lateinit var drafts: InMemoryCaptureDraftRepository

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
        sync = RecordingAddSyncRepository()
        cards = FakeCountsCaptureCardRepository()
        photos = FakePhotoCaptureSource()
        proofs = FakeProofCaptureRepository()
        drafts = InMemoryCaptureDraftRepository()
    }

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun newViewModel(savedStateHandle: SavedStateHandle = SavedStateHandle()) = AddBirthViewModel(
        sync,
        FakeAddCountsRepository(),
        FakeScanSource(),
        NoopAddAnalyticsPort(),
        NoopAddCrashReporter(),
        savedStateHandle,
        cards,
        drafts,
        FakeProofCaptureSource(),
        photos,
        proofs,
    )

    private fun fillPlainForm(vm: AddBirthViewModel) {
        vm.onEvent(AddBirthEvent.SelectPark(PARK_ID))
        vm.onEvent(AddBirthEvent.SelectShed(SHED_ID))
        vm.onEvent(AddBirthEvent.EditField(AddBirthField.BREED, "beetal"))
        vm.onEvent(AddBirthEvent.EditField(AddBirthField.DAM_ID, "RFID-MOTHER-001"))
    }

    @Test
    fun `seeded behaviour - no published card sends no capture and gates as today`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()
        fillPlainForm(vm)
        advanceUntilIdle()

        assertTrue(vm.state.value.captureCard.isEmpty)
        assertTrue(vm.state.value.canSubmit)
        vm.onEvent(AddBirthEvent.Submit)
        advanceUntilIdle()

        assertEquals("beetal", sync.lastBirth?.breed)
        assertNull("no card published: the request carries no sop_capture", sync.lastBirthCapture)
        assertEquals("the card is re-read when the form opens", listOf("birth"), cards.refreshed)
    }

    @Test
    fun `a published card holds submit until its slot is captured and its question answered`() = runTest(dispatcher) {
        cards.publish(birthCard())
        val vm = newViewModel()
        advanceUntilIdle()
        fillPlainForm(vm)
        advanceUntilIdle()

        assertEquals(listOf("kid_photo"), vm.state.value.captureCard.slots.map { it.slotKey })
        assertFalse("the compulsory photo is not captured yet", vm.state.value.canSubmit)

        photos.queue(CapturedPhoto(localUri = "file:///kid.jpg", capturedAtMs = 1_000L))
        vm.onEvent(AddBirthEvent.CaptureSlot("kid_photo", null))
        advanceUntilIdle()
        assertFalse("the compulsory question is not answered yet", vm.state.value.canSubmit)

        vm.onEvent(AddBirthEvent.Answer("mother_ok", "Suckling well"))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)

        vm.onEvent(AddBirthEvent.Submit)
        advanceUntilIdle()

        val capture = sync.lastBirthCapture ?: error("capture payload missing")
        assertEquals(SOP_VERSION_ID, capture.sopVersionId)
        assertEquals(setOf("kid_photo"), capture.slotProofs.keys)
        assertTrue(capture.slotProofs.getValue("kid_photo").outboxItemId!!.isNotBlank())
        assertEquals(JsonPrimitive("Suckling well"), capture.answers["mother_ok"])
        val call = proofs.captureCalls.single()
        assertEquals("a birth's report proof is filed under the pen", ProofSubject.SHED, call.subject)
        assertEquals(SHED_ID, call.subjectId)
    }

    @Test
    fun `captured slot and answer survive ViewModel recreation before submit`() = runTest(dispatcher) {
        cards.publish(birthCard())
        val saved = SavedStateHandle()
        val first = newViewModel(saved)
        advanceUntilIdle()
        fillPlainForm(first)
        photos.queue(CapturedPhoto(localUri = "file:///kid.jpg", capturedAtMs = 1_000L))
        first.onEvent(AddBirthEvent.CaptureSlot("kid_photo", null))
        first.onEvent(AddBirthEvent.Answer("mother_ok", "Suckling well"))
        advanceUntilIdle()

        val reopened = newViewModel(saved)
        advanceUntilIdle()

        assertTrue("the recreated form still sees the photo slot", reopened.state.value.captureCard.slots.single().captured)
        assertEquals("Suckling well", reopened.state.value.captureCard.answers["mother_ok"])
        fillPlainForm(reopened)
        advanceUntilIdle()
        assertTrue(reopened.state.value.canSubmit)
        reopened.onEvent(AddBirthEvent.Submit)
        advanceUntilIdle()

        assertEquals(proofs.allRows().single().outboxItemId, sync.lastBirthCapture?.slotProofs?.get("kid_photo")?.outboxItemId)
        assertEquals(JsonPrimitive("Suckling well"), sync.lastBirthCapture?.answers?.get("mother_ok"))
    }

    @Test
    fun `a questions-only card needs no media`() = runTest(dispatcher) {
        cards.publish(birthCard().let { it.copy(card = it.card.copy(proofs = emptyList())) })
        val vm = newViewModel()
        advanceUntilIdle()
        fillPlainForm(vm)
        vm.onEvent(AddBirthEvent.Answer("mother_ok", "Fine"))
        advanceUntilIdle()

        assertTrue(vm.state.value.canSubmit)
        vm.onEvent(AddBirthEvent.Submit)
        advanceUntilIdle()
        assertEquals(emptyMap<String, Any>(), sync.lastBirthCapture?.slotProofs)
        assertEquals(JsonPrimitive("Fine"), sync.lastBirthCapture?.answers?.get("mother_ok"))
    }

    @Test
    fun `the next birth starts a fresh capture draft`() = runTest(dispatcher) {
        cards.publish(birthCard())
        val vm = newViewModel()
        advanceUntilIdle()
        fillPlainForm(vm)
        photos.queue(CapturedPhoto(localUri = "file:///kid.jpg", capturedAtMs = 1_000L))
        vm.onEvent(AddBirthEvent.CaptureSlot("kid_photo", null))
        vm.onEvent(AddBirthEvent.Answer("mother_ok", "Fine"))
        advanceUntilIdle()
        vm.onEvent(AddBirthEvent.Submit)
        advanceUntilIdle()

        sync.emitBirthSucceeded()
        advanceUntilIdle()

        val card = vm.state.value.captureCard
        assertEquals(listOf("kid_photo"), card.slots.map { it.slotKey })
        assertFalse("the next report's slot starts empty", card.slots.single().captured)
        assertTrue(card.answers.isEmpty())
    }

    private fun birthCard() = CountsCaptureCardResponseDto(
        kind = "birth",
        sopCode = "counts.birth",
        sopVersionId = SOP_VERSION_ID,
        versionLabel = "v2",
        card = CountsCaptureCardDto(
            instruction = "Photograph the kid beside its mother.",
            proofs = listOf(WeighingRemovalProofSlotDto(key = "kid_photo", title = "Kid with mother", kind = "photo", required = true)),
            questions = listOf(WeighingSopQuestionDto(id = "mother_ok", kind = "text", title = "How is the mother?", required = true)),
        ),
    )

    private companion object {
        const val PARK_ID = "11111111-1111-1111-1111-111111111111"
        const val SHED_ID = "33333333-3333-3333-3333-333333333333"
        const val SOP_VERSION_ID = "55555555-5555-5555-5555-555555555555"
    }
}

/** Room-backed card store stand-in: [publish] is a refresh landing in Room. */
internal class FakeCountsCaptureCardRepository : CountsCaptureCardRepository {
    private val flows = mutableMapOf<String, MutableStateFlow<CountsCaptureCardResponseDto?>>()
    val refreshed = mutableListOf<String>()
    private fun flow(kind: String) = flows.getOrPut(kind) { MutableStateFlow(null) }

    fun publish(card: CountsCaptureCardResponseDto) {
        flow(card.kind).value = card
    }

    override fun observeCard(kind: String): Flow<CountsCaptureCardResponseDto?> = flow(kind)
    override suspend fun refreshCard(kind: String): Result<Unit> {
        refreshed += kind
        return Result.success(Unit)
    }
}
