package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.double
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.capture.CapturedPhoto
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEventsAnimalPurchase
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.AnimalPurchaseAnswers
import sg.mesha.goatos.core.data.AnimalPurchaseRepository
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.QueuedAnimalPurchaseAnimal
import sg.mesha.goatos.core.data.SalesDealScopeMeta
import sg.mesha.goatos.core.data.SalesLeadSide
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.data.WorkflowVideoDraft
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalCreateRequestDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnswerRowDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseConditionDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseCountsDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadCreateRequestDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseMediaItemDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseMediaSlotDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseOptionDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseOptionsDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseQuestionDto
import sg.mesha.goatos.core.network.dto.SaleAllocationDto
import sg.mesha.goatos.core.network.dto.SaleAllocationRequestDto
import sg.mesha.goatos.core.network.dto.SaleCandidatePageDto
import sg.mesha.goatos.core.network.dto.SaleLocationsDto
import sg.mesha.goatos.core.network.dto.SalePreviewDto
import sg.mesha.goatos.core.network.dto.SaleTaggingDealDto
import sg.mesha.goatos.core.network.dto.SaleTaggingQueueDto
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesFpoLeadDto
import sg.mesha.goatos.core.network.dto.SalesLeadBoardMetaDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.core.network.dto.VendorOptionDto
import sg.mesha.goatos.core.network.dto.VendorOptionsDto
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowChipsDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowNextActionDto
import sg.mesha.goatos.core.network.dto.WorkflowOverdueDateDto
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalCreateEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadCreateEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadDetailEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadField
import sg.mesha.goatos.feature.vendors.AnimalPurchaseQuestionKind
import sg.mesha.goatos.feature.vendors.VendorsTone
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus

/**
 * The Animal purchases state holders (maintainer decision 2026-09-13,
 * docs/decisions/animal-purchases.md).
 *
 * What these hold: backend copy reaches the cards VERBATIM; a form never closes on enqueue and
 * opens the recorded load only under the SERVER's id; a refused write shows the server's own
 * sentence and stays open; the served SOP questionnaire renders in order, paged by its sections,
 * with `only_if` questions hidden; every capture is its own proof row on the draft's lane and
 * the create references them per slot; the phone's check mirrors the server's and names the
 * first failing question; the server's `422 field` lands on the question it named; and answers,
 * captures and the page survive a process death.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class AnimalPurchaseViewModelsTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `a load card renders backend title, summary and whole-load counts verbatim`() {
        val card = AnimalPurchaseLoadDto(
            loadId = "load-1",
            title = "Load 132 · Kumar Traders",
            summary = "CBE · about 40 animals · 12 recorded",
            counts = AnimalPurchaseCountsDto(total = 12, pending = 7, accepted = 4, rejected = 1),
        ).toCardUi()

        assertEquals("load-1", card.listKey)
        assertEquals("Load 132 · Kumar Traders", card.title)
        assertEquals("CBE · about 40 animals · 12 recorded", card.summary)
        assertEquals(7, card.pending)
        assertEquals(4, card.accepted)
        assertEquals(1, card.rejected)
    }

    @Test
    fun `an animal card keeps the server decision chip and tone and composes only the number line`() {
        val accepted = animal(decision = "accepted", decisionLabel = "Accepted", decisionTone = "ok", decidedBy = "Ravi", note = "Good frame")
            .copy(ageMonths = 8, weightKg = 24.0).toCardUi()
        assertEquals("Accepted", accepted.decisionLabel)
        assertEquals(VendorsTone.OK, accepted.decisionTone)
        assertEquals("Ravi", accepted.decidedByName)
        assertEquals("Good frame", accepted.decisionNote)
        assertEquals("8 months · 24 kg", accepted.ageWeightLine)

        val rejected = animal(decision = "rejected", decisionLabel = "Rejected", decisionTone = "bad").toCardUi()
        assertEquals(VendorsTone.DANGER, rejected.decisionTone)
        assertEquals("", rejected.ageWeightLine)

        val pending = animal(decision = "pending", decisionLabel = "Awaiting decision", decisionTone = "neutral").toCardUi()
        assertEquals(VendorsTone.NEUTRAL, pending.decisionTone)
        assertEquals("", pending.decidedByName)
        // A legacy row (questionnaire_version 0) keeps its single video for explicit detail play,
        // but the card never binds the original MP4 as an auto-loaded preview.
        val legacy = animal(decision = "pending", decisionLabel = "Awaiting decision", decisionTone = "neutral")
            .copy(mediaUrl = "https://signed/legacy.mp4", mediaMime = "video/mp4").toCardUi()
        assertEquals("", legacy.fieldVerdictLabel)
        assertEquals("", legacy.previewUrl)
        assertFalse(legacy.previewIsPhoto)
    }

    @Test
    fun `a questionnaire row shows the field verdict chip and previews its first capture`() {
        val card = animal(decision = "pending", decisionLabel = "Awaiting decision", decisionTone = "neutral").copy(
            questionnaireVersion = 1,
            fieldVerdict = "on_hold",
            fieldVerdictLabel = "On hold on farm",
            mediaSlots = listOf(
                AnimalPurchaseMediaSlotDto(slot = "animal", title = "Goat video", items = listOf(AnimalPurchaseMediaItemDto(proofRef = "p-2", mediaUrl = "https://signed/animal.mp4", thumbnailUrl = "https://cdn.example/animal-poster.jpg", mediaMime = "video/mp4"))),
                AnimalPurchaseMediaSlotDto(slot = "teeth", title = "Photo of teeth", items = listOf(AnimalPurchaseMediaItemDto(proofRef = "p-1", mediaUrl = "https://signed/teeth.jpg", mediaMime = "image/jpeg"))),
            ),
        ).toCardUi()
        assertEquals("On hold on farm", card.fieldVerdictLabel)
        assertEquals(VendorsTone.WARN, card.fieldVerdictTone)
        assertEquals("https://cdn.example/animal-poster.jpg", card.previewUrl)
        assertFalse(card.previewIsPhoto)
        assertEquals("p-2", card.previewIdentity)
        assertEquals(VendorsTone.OK, animalPurchaseFieldVerdictTone("selected"))
    }

    @Test
    fun `a proof download route is resolved against the api base so the phone fetches it instead of opening a file`() {
        // The API returns a ROOT-RELATIVE proof route; handed to the loader as-is it was opened as a
        // local file and every card read "Photo unavailable" (maintainer report 2026-09-14).
        assertEquals(
            "http://localhost:8080/app/proofs/p-1/download/signed?sig=x",
            animalPurchaseMediaUrl("/app/proofs/p-1/download/signed?sig=x", apiBaseUrl = "http://localhost:8080/"),
        )
        assertEquals("https://signed/teeth.jpg", animalPurchaseMediaUrl("https://signed/teeth.jpg", apiBaseUrl = "http://localhost:8080/"))
        assertEquals("", animalPurchaseMediaUrl("  ", apiBaseUrl = "http://localhost:8080/"))
        val card = animal(decision = "pending", decisionLabel = "Awaiting decision", decisionTone = "neutral").copy(
            questionnaireVersion = 1,
            mediaSlots = listOf(AnimalPurchaseMediaSlotDto(slot = "teeth", title = "Photo of teeth", items = listOf(AnimalPurchaseMediaItemDto(proofRef = "p-1", mediaUrl = "/app/proofs/p-1/download/signed", mediaMime = "image/jpeg")))),
        ).toCardUi()
        assertTrue(card.previewUrl.startsWith("http"))
        assertTrue(card.previewUrl.endsWith("/app/proofs/p-1/download/signed"))
    }

    @Test
    fun `an animal record groups the served answer rows by section in order and lists every media slot`() {
        val dto = animal(decision = "rejected", decisionLabel = "Rejected", decisionTone = "bad").copy(
            questionnaireVersion = 1,
            fieldVerdict = "selected",
            fieldVerdictLabel = "Selected on farm",
            decidedByName = "Manohar",
            decisionNote = "Too thin",
            answerRows = listOf(
                AnimalPurchaseAnswerRowDto(section = "", questionId = "species", question = "Goat or sheep", answer = "Goat"),
                AnimalPurchaseAnswerRowDto(section = "", questionId = "teeth", question = "How many teeth?", answer = "0", attention = true),
                AnimalPurchaseAnswerRowDto(section = "Face visual productivity check", questionId = "anaemic", question = "Is the animal anaemic?", answer = "No"),
                AnimalPurchaseAnswerRowDto(section = "Face visual productivity check", questionId = "watery_eyes", question = "Watery eyes?", answer = "Yes - clear", attention = true),
                AnimalPurchaseAnswerRowDto(section = "Your verdict", questionId = "field_verdict", question = "Decision", answer = "Selected"),
            ),
            mediaSlots = listOf(
                AnimalPurchaseMediaSlotDto(slot = "teeth", title = "Photo of teeth", items = listOf(AnimalPurchaseMediaItemDto(proofRef = "p-1", mediaUrl = "/app/proofs/p-1/download/signed", mediaMime = "image/jpeg"))),
                AnimalPurchaseMediaSlotDto(slot = "weight", title = "Weight media", items = emptyList()),
                AnimalPurchaseMediaSlotDto(slot = "animal", title = "Goat video", items = listOf(AnimalPurchaseMediaItemDto(proofRef = "p-2", mediaUrl = "https://signed/animal.mp4", mediaMime = "video/mp4"))),
            ),
        )
        val copy = mapOf(
            "animal.decided_by" to "Decided by",
            "animal.detail.answers" to "What was recorded",
            "animal.detail.media" to "Photos and videos",
            "animal.detail.attention" to "Needs a close look",
        )
        val ui = dto.toDetailUi(AnimalPurchaseLoadDto(loadId = "load-1", title = "Load 132 · Kumar Traders"), copy)
        assertEquals("Load 132 · Kumar Traders", ui.loadTitle)
        assertEquals("Rejected", ui.decisionLabel)
        assertEquals(VendorsTone.DANGER, ui.decisionTone)
        assertEquals("Selected on farm", ui.fieldVerdictLabel)
        assertEquals("Decided by Manohar", ui.decidedByLine)
        assertEquals("Too thin", ui.decisionNote)
        // Sections keep the served order: the unheaded first page, then each heading once.
        assertEquals(listOf("", "Face visual productivity check", "Your verdict"), ui.sections.map { it.title })
        assertEquals(listOf("species", "teeth"), ui.sections[0].rows.map { it.questionId })
        assertEquals(listOf(false, true), ui.sections[0].rows.map { it.attention })
        assertEquals("Yes - clear", ui.sections[1].rows[1].answer)
        // An empty slot is not rendered; every served capture is, with a fetchable link.
        assertEquals(listOf("teeth", "animal"), ui.mediaSlots.map { it.slot })
        assertTrue(ui.mediaSlots[0].items.single().url.startsWith("http"))
        assertTrue(ui.mediaSlots[0].items.single().isPhoto)
        assertEquals("https://signed/animal.mp4", ui.mediaSlots[1].items.single().url)
        assertEquals("What was recorded", ui.answersTitle)
        assertEquals("Needs a close look", ui.attentionLabel)
    }

    @Test
    fun `the animal record screen reads the cached row live and refreshes the load on open`() = runTest(dispatcher) {
        val repo = FakeAnimalPurchaseRepository()
        val vm = AnimalPurchaseAnimalDetailViewModel(
            repository = repo,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("load_id" to "load-1", "candidate_id" to "c-1")),
        )
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()
        assertEquals("", vm.state.value.title)
        repo.animal.value = animal(decision = "pending", decisionLabel = "Awaiting decision", decisionTone = "neutral").copy(
            candidateId = "c-1",
            loadId = "load-1",
            title = "Animal 3 · Female goat",
            questionnaireVersion = 1,
            answerRows = listOf(AnimalPurchaseAnswerRowDto(section = "", questionId = "species", question = "Goat or sheep", answer = "Goat")),
        )
        advanceUntilIdle()
        assertEquals("Animal 3 · Female goat", vm.state.value.title)
        assertEquals("Load 132 · Kumar Traders", vm.state.value.loadTitle)
        assertEquals("Goat", vm.state.value.sections.single().rows.single().answer)
        job.cancel()
    }

    @Test
    fun `saving a load follows the queued row and opens the load under the server id`() = runTest(dispatcher) {
        val sync = RecordingAnimalPurchaseSyncRepository()
        val vm = loadCreateViewModel(sync)
        advanceUntilIdle()

        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.LOAD_REF, "132"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.VENDOR, "vendor-1"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.FARM, "CBE"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.Submit)
        advanceUntilIdle()

        // Queued, not closed: the id the screen must open is the SERVER's, not known yet.
        assertEquals(1, sync.loadCreates.size)
        assertEquals("132", sync.loadCreates.single().request.loadRef)
        assertEquals(VendorsWriteStatus.QUEUED, vm.state.value.writeStatus)
        assertNull(vm.state.value.createdLoadId)

        sync.row("ap-row-1").value = item("ap-row-1", SyncItemStatus.SUCCEEDED, resultJson = """{"load_id":"load-9","title":"Load 132"}""")
        advanceUntilIdle()

        assertEquals(VendorsWriteStatus.SYNCED, vm.state.value.writeStatus)
        assertEquals("load-9", vm.state.value.createdLoadId)
    }

    @Test
    fun `a refused load shows the server sentence and leaves the form open`() = runTest(dispatcher) {
        val sync = RecordingAnimalPurchaseSyncRepository()
        val vm = loadCreateViewModel(sync)
        advanceUntilIdle()

        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.LOAD_REF, "132"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.VENDOR, "vendor-1"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.FARM, "CBE"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.Submit)
        advanceUntilIdle()

        sync.row("ap-row-1").value = item("ap-row-1", SyncItemStatus.FAILED, conflict = true, lastError = "Load number 132 is already used.")
        advanceUntilIdle()

        assertEquals(VendorsWriteStatus.FAILED, vm.state.value.writeStatus)
        assertEquals("Load number 132 is already used.", vm.state.value.writeMessage)
        assertNull(vm.state.value.createdLoadId)
        // The form is open again: a corrected number can be submitted as a NEW record.
        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.LOAD_REF, "133"))
        assertEquals("133", vm.state.value.values[AnimalPurchaseLoadField.LOAD_REF])
    }

    @Test
    fun `the questionnaire renders in served order, paged by section, and only_if questions hide`() = runTest(dispatcher) {
        val vm = animalCreateViewModel()
        advanceUntilIdle()

        val page1 = vm.state.value
        assertEquals(4, page1.stepCount)
        assertEquals(0, page1.stepIndex)
        assertTrue(page1.isFirstPage)
        assertFalse(page1.isLastPage)
        assertEquals("Inspect an animal", page1.pageTitle)
        // Served order, `pregnant` (only_if sex=female) absent until the sex is answered.
        assertEquals(listOf("species", "goat_id", "teeth_media", "sex", "weight_kg"), page1.questions.map { it.id })
        assertEquals(AnimalPurchaseQuestionKind.MEDIA, page1.questions[2].kind)
        assertTrue(page1.questions[2].acceptsPhoto)
        assertTrue(page1.questions[2].acceptsVideo)
        assertEquals("0.5–300 kg", page1.questions[4].rangeLine)

        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("sex", "female"))
        assertEquals(listOf("species", "goat_id", "teeth_media", "sex", "pregnant", "weight_kg"), vm.state.value.questions.map { it.id })
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("pregnant", "yes"))
        assertEquals("yes", vm.state.value.scalarAnswers["pregnant"])

        // Flipping the sex hides the question AND drops its answer.
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("sex", "male"))
        assertFalse(vm.state.value.questions.any { it.id == "pregnant" })
        assertNull(vm.state.value.scalarAnswers["pregnant"])
    }

    @Test
    fun `next validates only its page and a refused page names the first failing question`() = runTest(dispatcher) {
        val proofs = FakeProofCaptureRepository(maxProofs = 50)
        val photos = FakePhotoSource("file:///teeth.jpg")
        val vm = animalCreateViewModel(proofs = proofs, photos = photos)
        advanceUntilIdle()

        // Nothing answered: the first required question on THIS page is named, the page stays.
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        assertEquals(0, vm.state.value.stepIndex)
        assertEquals("species", vm.state.value.scrollToQuestionId)
        assertEquals("Fields marked * are required.", vm.state.value.questions.first { it.id == "species" }.error)

        // A number out of the served range is refused with the range line, not a farm sentence.
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("species", "goat"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged("goat_id", "V-17"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TakePhoto("teeth_media"))
        advanceUntilIdle()
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("sex", "male"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged("weight_kg", "500"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        assertEquals(0, vm.state.value.stepIndex)
        assertEquals("0.5–300 kg", vm.state.value.questions.first { it.id == "weight_kg" }.error)

        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged("weight_kg", "24.5"))
        assertNull(vm.state.value.questions.first { it.id == "weight_kg" }.error)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        assertEquals(1, vm.state.value.stepIndex)
        assertEquals("Face visual productivity check", vm.state.value.pageTitle)
        assertEquals(listOf("face_scabs"), vm.state.value.questions.map { it.id })

        // Back shows the first page again WITH the photo taken on it.
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.PreviousPage)
        advanceUntilIdle()
        assertEquals(0, vm.state.value.stepIndex)
        val teeth = vm.state.value.questions.first { it.id == "teeth_media" }
        assertEquals(listOf("file:///teeth.jpg"), teeth.captures.map { it.localUri })
        assertFalse(teeth.captures.single().isVideo)
    }

    @Test
    fun `a page left with no applicable question is skipped`() = runTest(dispatcher) {
        // A catalog whose second section holds ONLY female questions: a male animal never sees it.
        val repo = FakeAnimalPurchaseRepository(
            questionnaire = listOf(
                AnimalPurchaseQuestionDto(id = "sex", kind = "choice", title = "Gender", required = true, options = listOf(AnimalPurchaseOptionDto("female", "Female"), AnimalPurchaseOptionDto("male", "Male"))),
                AnimalPurchaseQuestionDto(id = "sec_milk", kind = "section", title = "Milk"),
                AnimalPurchaseQuestionDto(id = "milk_yield", kind = "text", title = "Milk yield", onlyIf = AnimalPurchaseConditionDto("sex", "female")),
                AnimalPurchaseQuestionDto(id = "sec_decision", kind = "section", title = "Your verdict"),
                AnimalPurchaseQuestionDto(id = "field_verdict", kind = "choice", title = "Decision", required = true, options = listOf(AnimalPurchaseOptionDto("selected", "Selected"))),
            ),
        )
        val vm = animalCreateViewModel(repository = repo)
        advanceUntilIdle()
        assertEquals(2, vm.state.value.stepCount)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("sex", "male"))
        assertEquals(2, vm.state.value.stepCount)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        assertEquals("Your verdict", vm.state.value.pageTitle)
        assertTrue(vm.state.value.isLastPage)

        vm.onEvent(AnimalPurchaseAnimalCreateEvent.PreviousPage)
        advanceUntilIdle()
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("sex", "female"))
        assertEquals(3, vm.state.value.stepCount)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        assertEquals("Milk", vm.state.value.pageTitle)
    }

    @Test
    fun `submit builds the answers JSON and references every capture per slot on the draft lane`() = runTest(dispatcher) {
        val sync = RecordingAnimalPurchaseSyncRepository()
        val videos = FakeProofCaptureSource()
        videos.queue(CapturedVideo(localUri = "file:///udder.mp4", startedAtMs = 1_000L, endedAtMs = 9_000L))
        val photos = FakePhotoSource("file:///teeth-1.jpg", "file:///teeth-2.jpg")
        val proofs = FakeProofCaptureRepository(maxProofs = 50)
        val analytics = RecordingAnalytics()
        val vm = animalCreateViewModel(sync = sync, captureSource = videos, photos = photos, proofs = proofs, analytics = analytics)
        advanceUntilIdle()

        fillPage1(vm)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TakePhoto("teeth_media"))
        advanceUntilIdle()
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TakePhoto("teeth_media"))
        advanceUntilIdle()
        assertEquals(2, vm.state.value.questions.first { it.id == "teeth_media" }.captures.size)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("face_scabs", "other"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged("face_scabs_other", "left ear"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        assertEquals(2, vm.state.value.stepIndex)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.RecordVideo("udder_media"))
        advanceUntilIdle()
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged("milk_yield", "1.5 L"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        assertTrue(vm.state.value.isLastPage)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("field_verdict", "selected"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.Submit)
        advanceUntilIdle()

        // Three captures, each its OWN proof row: photos register with an image mime, the video
        // with a video mime, all on ONE FIFO lane per DRAFT so every upload drains before the
        // create that resolves them.
        assertEquals(3, proofs.captureCalls.size)
        assertTrue(proofs.captureCalls.all { it.uploadGroupKey!!.startsWith("animal-purchase:animal:ap-animal:") })
        assertTrue(proofs.captureCalls.all { it.subjectId == LOAD_ID })
        assertEquals(listOf("image/jpeg", "image/jpeg", "video/mp4"), proofs.captureCalls.map { it.mimeType })
        assertEquals(3, analytics.events.count { it.name == AnalyticsEventsAnimalPurchase.ANIMAL_VIDEO_CAPTURED })

        val queued = sync.animalCreates.single()
        assertEquals(LOAD_ID, queued.loadId)
        // PROCUREMENT SOP: the version the form rendered rides with the answers, so the backend
        // validates against THAT version even if a newer one was published meanwhile.
        assertEquals(7, queued.request.questionnaireVersion)
        val answers = queued.request.answers
        assertEquals("goat", answers["species"]!!.jsonPrimitive.content)
        assertEquals("V-17", answers["goat_id"]!!.jsonPrimitive.content)
        assertEquals("female", answers["sex"]!!.jsonPrimitive.content)
        assertEquals("no", answers["pregnant"]!!.jsonPrimitive.content)
        assertEquals(24.5, answers["weight_kg"]!!.jsonPrimitive.double, 0.0)
        assertEquals("other", answers["face_scabs"]!!.jsonPrimitive.content)
        assertEquals("left ear", answers["face_scabs_other"]!!.jsonPrimitive.content)
        assertEquals("1.5 L", answers["milk_yield"]!!.jsonPrimitive.content)
        assertEquals("selected", answers["field_verdict"]!!.jsonPrimitive.content)
        // The male-only question never rides along.
        assertNull(answers["scrotum_cm"])
        // Media ride by REFERENCE per slot, in capture order; the request's own media map is
        // empty at rest — the dispatcher fills it once the uploads have drained.
        assertEquals(setOf("teeth", "udder"), queued.proofOutboxItemIds.keys)
        assertEquals(2, queued.proofOutboxItemIds.getValue("teeth").size)
        assertEquals(1, queued.proofOutboxItemIds.getValue("udder").size)
        assertTrue(queued.proofOutboxItemIds.values.flatten().all { it.isNotBlank() })
        assertTrue(queued.request.media.isEmpty())
        assertEquals(VendorsWriteStatus.QUEUED, vm.state.value.writeStatus)

        sync.row("ap-animal-1").value = item("ap-animal-1", SyncItemStatus.SUCCEEDED, resultJson = "{}")
        advanceUntilIdle()
        assertEquals(VendorsWriteStatus.SYNCED, vm.state.value.writeStatus)
        assertTrue(vm.state.value.closeAfterSave)
    }

    @Test
    fun `the server's 422 field lands on the question it named, on that question's page`() = runTest(dispatcher) {
        val sync = RecordingAnimalPurchaseSyncRepository()
        val vm = animalCreateViewModel(
            sync = sync,
            captureSource = FakeProofCaptureSource().also { it.queue(CapturedVideo(localUri = "file:///udder.mp4", startedAtMs = 1L, endedAtMs = 2L)) },
            photos = FakePhotoSource("file:///teeth.jpg"),
        )
        advanceUntilIdle()
        walkToLastPage(vm)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("field_verdict", "selected"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.Submit)
        advanceUntilIdle()
        assertEquals(1, sync.animalCreates.size)

        sync.row("ap-animal-1").value = item("ap-animal-1", SyncItemStatus.FAILED, conflict = true, lastError = "Say where, for: Any signs of scabby skin?", lastErrorField = "face_scabs")
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals(VendorsWriteStatus.FAILED, state.writeStatus)
        assertEquals("Say where, for: Any signs of scabby skin?", state.writeMessage)
        // The verdict page was on screen; the named question lives on the face page.
        assertEquals(1, state.stepIndex)
        assertEquals("face_scabs", state.scrollToQuestionId)
        assertEquals("Say where, for: Any signs of scabby skin?", state.questions.first { it.id == "face_scabs" }.error)
        // The form is open again on that page: the corrected answer can be re-submitted.
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged("face_scabs_other", "right ear"))
        assertNull(vm.state.value.questions.first { it.id == "face_scabs" }.error)
    }

    @Test
    fun `the phone refuses a form the server would, naming the first failing question`() {
        val required = "Fields marked * are required."
        val copy = mapOf("required.hint" to required, "animal.other.hint" to "Say where", "animal.send_failed" to "Could not send")
        val answered = AnimalPurchaseAnswers(scalar = mapOf("species" to "goat", "goat_id" to "V-1", "sex" to "female", "pregnant" to "no", "face_scabs" to "other", "field_verdict" to "selected"))
        val media = mapOf("teeth" to listOf("ob-1"), "udder" to listOf("ob-2"))
        // "other" chosen without its text: the server's rule, the backend's copy.
        assertEquals(
            AnimalPurchaseQuestionFailure("face_scabs", "Say where"),
            animalPurchaseFirstFailure(QUESTIONNAIRE, answered, media, emptySet(), copy),
        )
        val withOther = answered.withScalar("face_scabs_other", "nose")
        assertNull(animalPurchaseFirstFailure(QUESTIONNAIRE, withOther, media, emptySet(), copy))
        // A required media slot empty, then a capture whose upload gave up, then a slot over its cap.
        assertEquals("udder_media", animalPurchaseFirstFailure(QUESTIONNAIRE, withOther, media - "udder", emptySet(), copy)?.questionId)
        assertEquals(
            AnimalPurchaseQuestionFailure("teeth_media", "Could not send"),
            animalPurchaseFirstFailure(QUESTIONNAIRE, withOther, media, setOf("ob-1"), copy),
        )
        assertEquals("teeth_media", animalPurchaseFirstFailure(QUESTIONNAIRE, withOther, mapOf("teeth" to listOf("a", "b", "c"), "udder" to listOf("ob-2")), emptySet(), copy)?.questionId)
        // A hidden question is never required: the female-only pregnant question is skipped for a male.
        val male = AnimalPurchaseAnswers(scalar = mapOf("species" to "goat", "goat_id" to "V-1", "sex" to "male", "face_scabs" to "no", "field_verdict" to "selected"))
        assertNull(animalPurchaseFirstFailure(QUESTIONNAIRE, male, media, emptySet(), copy))
        // An out-of-range number names the range, numbers not copy.
        assertEquals(
            AnimalPurchaseQuestionFailure("scrotum_cm", "5–60 cm"),
            animalPurchaseFirstFailure(QUESTIONNAIRE, male.withScalar("scrotum_cm", "2"), media, emptySet(), copy),
        )
    }

    @Test
    fun `a capture whose upload is still retrying offline saves, one whose upload gave up is retried per capture`() = runTest(dispatcher) {
        val sync = RecordingAnimalPurchaseSyncRepository()
        val proofs = FakeProofCaptureRepository(maxProofs = 50)
        val vm = animalCreateViewModel(
            sync = sync,
            captureSource = FakeProofCaptureSource().also { it.queue(CapturedVideo(localUri = "file:///udder.mp4", startedAtMs = 1L, endedAtMs = 2L)) },
            photos = FakePhotoSource("file:///teeth.jpg"),
            proofs = proofs,
        )
        advanceUntilIdle()
        fillPage1(vm)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TakePhoto("teeth_media"))
        advanceUntilIdle()
        val teethRowId = "proof-outbox-1"
        // Offline: the upload row has failed twice and will retry. The proof mirror stays
        // IN_FLIGHT and the capture is still the capture.
        sync.row(teethRowId).value = item(teethRowId, SyncItemStatus.FAILED, attemptCount = 2, maxAttempts = 8)
        proofs.markSynced(id = "proof-0", serverProofId = "", syncStatus = "IN_FLIGHT")
        advanceUntilIdle()
        assertFalse(vm.state.value.questions.first { it.id == "teeth_media" }.captures.single().uploadFailed)

        // Gave up: every retry spent. The capture stays on screen (never hidden) marked failed,
        // Next refuses on THAT question, and a retry re-arms that same upload row.
        sync.row(teethRowId).value = item(teethRowId, SyncItemStatus.FAILED, attemptCount = 8, maxAttempts = 8)
        proofs.markSynced(id = "proof-0", serverProofId = "", syncStatus = "FAILED")
        advanceUntilIdle()
        val failed = vm.state.value.questions.first { it.id == "teeth_media" }.captures.single()
        assertTrue(failed.uploadFailed)
        assertEquals("file:///teeth.jpg", failed.localUri)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        assertEquals(0, vm.state.value.stepIndex)
        assertEquals("Could not send · tap to retry", vm.state.value.questions.first { it.id == "teeth_media" }.error)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.RetryCapture("teeth_media", failed.proofId))
        advanceUntilIdle()
        assertEquals(listOf(teethRowId), sync.retries)

        // Re-armed and retrying again: the capture is usable and the form walks on and saves.
        sync.row(teethRowId).value = item(teethRowId, SyncItemStatus.FAILED, attemptCount = 1, maxAttempts = 8)
        proofs.markSynced(id = "proof-0", serverProofId = "", syncStatus = "IN_FLIGHT")
        advanceUntilIdle()
        walkFromPage1ToLastPage(vm)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("field_verdict", "on_hold"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.Submit)
        advanceUntilIdle()
        assertEquals(listOf(teethRowId), sync.animalCreates.single().proofOutboxItemIds.getValue("teeth"))
    }

    @Test
    fun `removing a capture drops its proof row and a full slot offers no further capture`() = runTest(dispatcher) {
        val proofs = FakeProofCaptureRepository(maxProofs = 50)
        val vm = animalCreateViewModel(proofs = proofs, photos = FakePhotoSource("file:///a.jpg", "file:///b.jpg", "file:///c.jpg"))
        advanceUntilIdle()
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TakePhoto("teeth_media"))
        advanceUntilIdle()
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TakePhoto("teeth_media"))
        advanceUntilIdle()
        val captures = vm.state.value.questions.first { it.id == "teeth_media" }.captures
        assertEquals(2, captures.size)
        // The slot is at its cap (max_files 2): a third capture is never attempted.
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TakePhoto("teeth_media"))
        advanceUntilIdle()
        assertEquals(2, proofs.captureCalls.size)

        vm.onEvent(AnimalPurchaseAnimalCreateEvent.RemoveCapture("teeth_media", captures.first().proofId))
        advanceUntilIdle()
        assertEquals(listOf(captures.first().proofId), proofs.removedProofIds)
        assertEquals(listOf("file:///b.jpg"), vm.state.value.questions.first { it.id == "teeth_media" }.captures.map { it.localUri })
    }

    // --- fixtures ---------------------------------------------------------------------------

    /** Every non-media answer of page 1: a female goat, tag V-17, not pregnant, 24.5 kg. */
    private fun fillPage1(vm: AnimalPurchaseAnimalCreateViewModel) {
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("species", "goat"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged("goat_id", "V-17"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("sex", "female"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("pregnant", "no"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged("weight_kg", "24.5"))
    }

    /** Answers pages 2 and 3 (one photo already taken on page 1) and lands on the verdict page. */
    private fun TestScope.walkFromPage1ToLastPage(vm: AnimalPurchaseAnimalCreateViewModel) {
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("face_scabs", "no"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        if (vm.state.value.questions.first { it.id == "udder_media" }.captures.isEmpty()) {
            vm.onEvent(AnimalPurchaseAnimalCreateEvent.RecordVideo("udder_media"))
            advanceUntilIdle()
        }
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        assertTrue(vm.state.value.isLastPage)
    }

    /** Fills page 1 (with its teeth photo) and walks to the verdict page. */
    private fun TestScope.walkToLastPage(vm: AnimalPurchaseAnimalCreateViewModel) {
        fillPage1(vm)
        if (vm.state.value.questions.first { it.id == "teeth_media" }.captures.isEmpty()) {
            vm.onEvent(AnimalPurchaseAnimalCreateEvent.TakePhoto("teeth_media"))
            advanceUntilIdle()
        }
        walkFromPage1ToLastPage(vm)
    }

    private fun TestScope.loadCreateViewModel(sync: RecordingAnimalPurchaseSyncRepository): AnimalPurchaseLoadCreateViewModel =
        AnimalPurchaseLoadCreateViewModel(
            savedStateHandle = SavedStateHandle(),
            repository = FakeAnimalPurchaseRepository(),
            salesRepository = StubSalesRepository(),
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        ).also { vm -> backgroundScope.launch { vm.state.collect { } } }

    private fun TestScope.animalCreateViewModel(
        sync: RecordingAnimalPurchaseSyncRepository = RecordingAnimalPurchaseSyncRepository(),
        captureSource: FakeProofCaptureSource = FakeProofCaptureSource(),
        photos: FakePhotoSource = FakePhotoSource(),
        proofs: FakeProofCaptureRepository = FakeProofCaptureRepository(maxProofs = 50),
        analytics: RecordingAnalytics = RecordingAnalytics(),
        savedStateHandle: SavedStateHandle = SavedStateHandle(mapOf("load_id" to LOAD_ID)),
        repository: FakeAnimalPurchaseRepository = FakeAnimalPurchaseRepository(),
    ): AnimalPurchaseAnimalCreateViewModel = AnimalPurchaseAnimalCreateViewModel(
        savedStateHandle = savedStateHandle,
        repository = repository,
        proofCaptureRepository = proofs,
        proofCaptureSource = captureSource,
        photoCaptureSource = photos,
        syncRepository = sync,
        bootstrapRepository = TenantOnlyBootstrapRepository(),
        analytics = analytics,
        crashReporter = NoopCrashReporter(),
        appContext = RuntimeEnvironment.getApplication(),
    ).also { vm -> backgroundScope.launch { vm.state.collect { } } }

    private fun animal(decision: String, decisionLabel: String, decisionTone: String, decidedBy: String = "", note: String = "") =
        AnimalPurchaseAnimalDto(
            candidateId = "cand-1",
            loadId = LOAD_ID,
            title = "Animal 1 · Female goat",
            decision = decision,
            decisionLabel = decisionLabel,
            decisionTone = decisionTone,
            decidedByName = decidedBy,
            decisionNote = note,
        )


    /**
     * PROCUREMENT IS SOP-DRIVEN END TO END (2026-09-20): the load's steps card is the BACKEND's
     * counters and next step, verbatim. The phone resolves the workflow by SUBJECT -- the load it
     * is showing -- and counts nothing itself, so a step the farm adds on the web appears here
     * with no app release.
     */
    @Test
    fun `the load's steps card renders the backend's own counters`() = runTest(dispatcher) {
        val repo = FakeAnimalPurchaseRepository()
        val workflows = FakeLoadStepsRepository(
            workflowId = "wf-load-1",
            detail = WorkflowDetailResponseDto(
                workflowId = "wf-load-1",
                actionsDone = 2,
                actionsTotal = 7,
                nextAction = WorkflowNextActionDto(key = "arrival_video", title = "Record the animals arriving"),
            ),
        )
        val vm = AnimalPurchaseLoadDetailViewModel(
            repository = repo,
            workflows = workflows,
            syncRepository = RecordingAnimalPurchaseSyncRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("load_id" to LOAD_ID)),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("wf-load-1", state.stepsWorkflowId)
        assertEquals("2 of 7 done", state.stepsProgressLine)
        assertEquals("Next: Record the animals arriving", state.stepsNextLine)
        assertFalse(state.stepsUnavailable)
        // Resolved by the LOAD, on the intake template -- never by guessing a workflow id.
        assertEquals(listOf("animal_purchase_intake" to LOAD_ID), workflows.subjectReads)
    }

    private companion object {
        const val LOAD_ID = "load-1"

        fun item(id: String, status: SyncItemStatus, conflict: Boolean = false, lastError: String? = null, resultJson: String? = null, attemptCount: Int = 0, maxAttempts: Int = 5, lastErrorField: String? = null) = SyncQueueItem(
            id = id, opType = "ANIMAL_PURCHASE_LOAD_CREATE", idempotencyKey = "k", groupKey = "g", status = status,
            attemptCount = attemptCount, maxAttempts = maxAttempts, conflict = conflict, createdAt = 0L, updatedAt = 0L, lastError = lastError,
            lastErrorField = lastErrorField, resultJson = resultJson,
        )
    }

    @Test
    fun `an animal saved without signal is listed on the load as waiting to send`() = runTest(dispatcher) {
        val repo = FakeAnimalPurchaseRepository()
        val sync = RecordingAnimalPurchaseSyncRepository()
        val vm = AnimalPurchaseLoadDetailViewModel(
            repository = repo,
            workflows = FakeLoadStepsRepository(),
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("load_id" to LOAD_ID)),
        )
        backgroundScope.launch { vm.state.collect {} }
        repo.queued.value = listOf(
            QueuedAnimalPurchaseAnimal(
                outboxItemId = "ob-1", loadId = LOAD_ID, queuedAtMs = 1L,
                request = AnimalPurchaseAnimalCreateRequestDto(
                    answers = buildJsonObject { put("species", "goat"); put("sex", "female"); put("breed", "Sirohi"); put("weight_kg", 24.0); put("goat_id", "V-17") },
                ),
                proofOutboxItemIds = listOf("proof-1", "proof-2"),
            ),
        )
        advanceUntilIdle()
        val queued = vm.state.value.queuedAnimals.single()
        // The title is derived from the answered species/sex through the served vocabulary.
        assertEquals("Female goat", queued.title)
        assertEquals("24 kg", queued.ageWeightLine)
        // No copy row for the waiting chip in this fake: the built-in fallback is what shows.
        assertEquals("Waiting to send", queued.waitingLabel)
        assertEquals("Sirohi", queued.breed)
        assertEquals("V-17", queued.tempTag)
        assertFalse(queued.sendFailed)

        // ONE of the capture uploads spent every retry while offline: the row says so and offers
        // a retry that re-arms only the dead upload.
        sync.row("proof-2").value = item("proof-2", SyncItemStatus.FAILED, attemptCount = 8, maxAttempts = 8)
        advanceUntilIdle()
        assertTrue(vm.state.value.queuedAnimals.single().sendFailed)
        vm.onEvent(AnimalPurchaseLoadDetailEvent.RetryQueued("ob-1"))
        advanceUntilIdle()
        assertEquals(listOf("proof-2"), sync.retries)
    }


    @Test
    fun `answers, the page and the draft key survive a process death and the form comes back as left`() = runTest(dispatcher) {
        // The SavedStateHandle is what Android hands back after a death; the ViewModel is new.
        val handle = SavedStateHandle(mapOf("load_id" to LOAD_ID))
        val proofs = FakeProofCaptureRepository(maxProofs = 50)
        val first = animalCreateViewModel(savedStateHandle = handle, proofs = proofs, photos = FakePhotoSource("file:///teeth.jpg"))
        advanceUntilIdle()
        fillPage1(first)
        first.onEvent(AnimalPurchaseAnimalCreateEvent.TakePhoto("teeth_media"))
        advanceUntilIdle()
        first.onEvent(AnimalPurchaseAnimalCreateEvent.NextPage)
        advanceUntilIdle()
        first.onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged("face_scabs", "other"))
        first.onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged("face_scabs_other", "left ear"))
        assertEquals(1, first.state.value.stepIndex)
        val draftBefore = handle.get<String>("animalPurchase.animal.draftKey")

        // Same proof store (Room outlives the process); a new ViewModel over the same handle.
        val reborn = animalCreateViewModel(savedStateHandle = handle, proofs = proofs)
        advanceUntilIdle()
        val state = reborn.state.value
        assertEquals(1, state.stepIndex)
        assertEquals("goat", state.scalarAnswers["species"])
        assertEquals("V-17", state.scalarAnswers["goat_id"])
        assertEquals("female", state.scalarAnswers["sex"])
        assertEquals("other", state.scalarAnswers["face_scabs"])
        assertEquals("left ear", state.scalarAnswers["face_scabs_other"])
        // Same draft key, so the durable captures taken before the death are the ones it reads.
        assertEquals(draftBefore, handle.get<String>("animalPurchase.animal.draftKey"))
        reborn.onEvent(AnimalPurchaseAnimalCreateEvent.PreviousPage)
        advanceUntilIdle()
        assertEquals(listOf("file:///teeth.jpg"), reborn.state.value.questions.first { it.id == "teeth_media" }.captures.map { it.localUri })
    }
}

    /** A compact served questionnaire in the SOP's shape: identity/condition, then three sections. */
    private val QUESTIONNAIRE: List<AnimalPurchaseQuestionDto> = listOf(
        AnimalPurchaseQuestionDto(id = "species", kind = "choice", title = "Goat or sheep", required = true, options = listOf(AnimalPurchaseOptionDto("goat", "Goat"), AnimalPurchaseOptionDto("sheep", "Sheep"))),
        AnimalPurchaseQuestionDto(id = "goat_id", kind = "text", title = "Goat ID", hint = "The tag the vendor uses.", required = true),
        AnimalPurchaseQuestionDto(id = "teeth_media", kind = "media", title = "Photo of teeth", required = true, slot = "teeth", maxFiles = 2, accepts = listOf("photo", "video")),
        AnimalPurchaseQuestionDto(id = "sex", kind = "choice", title = "Gender of the animal?", required = true, options = listOf(AnimalPurchaseOptionDto("female", "Female"), AnimalPurchaseOptionDto("male", "Male"))),
        AnimalPurchaseQuestionDto(id = "pregnant", kind = "choice", title = "Is the animal pregnant?", required = true, options = listOf(AnimalPurchaseOptionDto("no", "No"), AnimalPurchaseOptionDto("yes", "Yes")), onlyIf = AnimalPurchaseConditionDto("sex", "female")),
        AnimalPurchaseQuestionDto(id = "weight_kg", kind = "number", title = "Weight of the animal in KG", unit = "kg", min = 0.5, max = 300.0),
        AnimalPurchaseQuestionDto(id = "sec_face", kind = "section", title = "Face visual productivity check"),
        AnimalPurchaseQuestionDto(id = "face_scabs", kind = "choice", title = "Any signs of scabby skin?", required = true, options = listOf(AnimalPurchaseOptionDto("no", "No"), AnimalPurchaseOptionDto("other", "Yes")), allowOther = true),
        AnimalPurchaseQuestionDto(id = "sec_udder", kind = "section", title = "Udder or testicles", hint = "Look closely."),
        AnimalPurchaseQuestionDto(id = "udder_media", kind = "media", title = "Udder or testicles media", required = true, slot = "udder", maxFiles = 1, accepts = listOf("photo", "video")),
        AnimalPurchaseQuestionDto(id = "milk_yield", kind = "text", title = "Milk yield", onlyIf = AnimalPurchaseConditionDto("sex", "female")),
        AnimalPurchaseQuestionDto(id = "scrotum_cm", kind = "number", title = "Male scrotum circumference in CM", unit = "cm", min = 5.0, max = 60.0, onlyIf = AnimalPurchaseConditionDto("sex", "male")),
        AnimalPurchaseQuestionDto(id = "sec_decision", kind = "section", title = "Your verdict"),
        AnimalPurchaseQuestionDto(id = "field_verdict", kind = "choice", title = "Decision", required = true, options = listOf(AnimalPurchaseOptionDto("selected", "Selected"), AnimalPurchaseOptionDto("on_hold", "On Hold"))),
    )

/** In-memory [AnimalPurchaseRepository]: Room's role is played by state flows. */
private class FakeAnimalPurchaseRepository(
    questionnaire: List<AnimalPurchaseQuestionDto> = QUESTIONNAIRE,
) : AnimalPurchaseRepository {
    private val options = MutableStateFlow<AnimalPurchaseOptionsDto?>(
        AnimalPurchaseOptionsDto(
            species = listOf(AnimalPurchaseOptionDto("goat", "Goat"), AnimalPurchaseOptionDto("sheep", "Sheep")),
            sexes = listOf(AnimalPurchaseOptionDto("female", "Female"), AnimalPurchaseOptionDto("male", "Male")),
            conditions = listOf(AnimalPurchaseOptionDto("healthy", "Healthy")),
            farms = listOf(AnimalPurchaseOptionDto("CBE", "CBE"), AnimalPurchaseOptionDto("CPT", "CPT")),
            questionnaire = questionnaire,
            questionnaireVersion = 7,
            copy = mapOf(
                "animal.form.title" to "Inspect an animal",
                "animal.form.hint" to "The procurement SOP, one animal at a time.",
                "required.hint" to "Fields marked * are required.",
                "animal.other.hint" to "Say where",
                "animal.send_failed" to "Could not send · tap to retry",
            ),
        ),
    )
    private val load = MutableStateFlow<AnimalPurchaseLoadDto?>(AnimalPurchaseLoadDto(loadId = "load-1", title = "Load 132 · Kumar Traders"))

    override fun loads(): Flow<PagingData<AnimalPurchaseLoadDto>> = flowOf(PagingData.from(emptyList()))
    override fun observeCanRecord(): Flow<Boolean> = flowOf(true)
    override suspend fun invalidateLoads() = Unit
    override fun observeOptions(): Flow<AnimalPurchaseOptionsDto?> = options
    override suspend fun refreshOptions() = Unit
    override fun observeLoad(loadId: String): Flow<AnimalPurchaseLoadDto?> = load.map { it?.takeIf { l -> l.loadId == loadId } }
    override fun animals(loadId: String): Flow<PagingData<AnimalPurchaseAnimalDto>> = flowOf(PagingData.from(emptyList()))
    val animal = MutableStateFlow<AnimalPurchaseAnimalDto?>(null)
    override fun observeAnimal(loadId: String, candidateId: String): Flow<AnimalPurchaseAnimalDto?> =
        animal.map { it?.takeIf { a -> a.loadId == loadId && a.candidateId == candidateId } }
    override suspend fun invalidateAnimals(loadId: String) = Unit
    override suspend fun refreshLoad(loadId: String) = Unit
    override suspend fun persistServerLoad(load: AnimalPurchaseLoadDto) = Unit
    override suspend fun persistServerAnimal(animal: AnimalPurchaseAnimalDto) = Unit
    val queued = MutableStateFlow<List<QueuedAnimalPurchaseAnimal>>(emptyList())
    override fun observeQueuedAnimals(loadId: String): Flow<List<QueuedAnimalPurchaseAnimal>> = queued
}

/** Records every animal-purchase enqueue and lets a test drive each queued row's outcome. */
private class RecordingAnimalPurchaseSyncRepository : SyncRepository by RecordingToxinSyncRepository() {
    data class LoadCreate(val draftKey: String, val request: AnimalPurchaseLoadCreateRequestDto)
    data class AnimalCreate(val draftKey: String, val loadId: String, val request: AnimalPurchaseAnimalCreateRequestDto, val proofOutboxItemIds: Map<String, List<String>>)

    val loadCreates = mutableListOf<LoadCreate>()
    val animalCreates = mutableListOf<AnimalCreate>()
    private val rows = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()

    fun row(itemId: String): MutableStateFlow<SyncQueueItem?> = rows.getOrPut(itemId) { MutableStateFlow(null) }

    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = row(itemId)

    override suspend fun enqueueAnimalPurchaseLoadCreate(draftKey: String, request: AnimalPurchaseLoadCreateRequestDto): AppResult<String> {
        loadCreates += LoadCreate(draftKey, request)
        return AppResult.Ok("ap-row-${loadCreates.size}")
    }

    override suspend fun enqueueAnimalPurchaseAnimalCreate(
        draftKey: String,
        loadId: String,
        request: AnimalPurchaseAnimalCreateRequestDto,
        proofOutboxItemIds: Map<String, List<String>>,
    ): AppResult<String> {
        animalCreates += AnimalCreate(draftKey, loadId, request, proofOutboxItemIds)
        return AppResult.Ok("ap-animal-${animalCreates.size}")
    }

    val retries = mutableListOf<String>()
    override suspend fun retry(itemId: String): AppResult<Unit> {
        retries += itemId
        return AppResult.Ok(Unit)
    }
}

/** In-app photo camera double: hands back the queued files in order, null (cancelled) once spent. */
private class FakePhotoSource(vararg files: String) : PhotoCaptureSource {
    private val queue = ArrayDeque(files.toList())
    val contexts = mutableListOf<PhotoCaptureContext>()
    override suspend fun capturePhoto(context: PhotoCaptureContext): CapturedPhoto? {
        contexts += context
        val file = queue.removeFirstOrNull() ?: return null
        return CapturedPhoto(localUri = file, capturedAtMs = 1_000L + contexts.size)
    }
}

/** The one bootstrap fact the add-animal form needs: the tenant the captures are scoped to. */
private class TenantOnlyBootstrapRepository : BootstrapRepository {
    override suspend fun loadNavState(): NavState = error("unused")
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto? = null
    override suspend fun actorTenantId(): String = "tenant-1"
}

/** The vendor picklist and nothing else of the Sales module. */
private class StubSalesRepository : SalesRepository {
    override fun deals(farm: String): Flow<PagingData<SalesDealDto>> = flowOf(PagingData.from(emptyList()))
    override fun observeDealScope(farm: String): Flow<SalesDealScopeMeta?> = flowOf(null)
    override suspend fun invalidateDeals(farm: String) = Unit
    override fun observeDeal(dealId: String): Flow<SalesDealDto?> = flowOf(null)
    override fun observeOptions(): Flow<SalesOptionsDto?> = flowOf(null)
    override suspend fun refreshOptions() = Unit
    override fun observeVendorOptions(): Flow<VendorOptionsDto?> =
        flowOf(VendorOptionsDto(vendors = listOf(VendorOptionDto("vendor-1", "Kumar Traders"))))
    override suspend fun refreshVendorOptions() = Unit
    override suspend fun persistServerDeal(deal: SalesDealDto) = Unit
    override fun buyerLeads(search: String, status: String): Flow<PagingData<SalesBuyerLeadDto>> = flowOf(PagingData.from(emptyList()))
    override fun fpoLeads(search: String, status: String): Flow<PagingData<SalesFpoLeadDto>> = flowOf(PagingData.from(emptyList()))
    override fun observeLeadMeta(side: SalesLeadSide, search: String, status: String): Flow<SalesLeadBoardMetaDto?> = flowOf(null)
    override suspend fun refreshLeadMeta(side: SalesLeadSide) = true
    override suspend fun invalidateLeads(side: SalesLeadSide, search: String, status: String) = Unit
    override suspend fun persistServerBuyerLead(lead: SalesBuyerLeadDto) = Unit
    override suspend fun persistServerFpoLead(lead: SalesFpoLeadDto) = Unit
    override suspend fun saleLocations(): AppResult<SaleLocationsDto> = error("unused")
    override suspend fun saleCandidates(parkId: String, shedId: String?, partitionLabels: List<String>, query: String?, cursor: String?): AppResult<SaleCandidatePageDto> = error("unused")
    override suspend fun saleAllocation(dealId: String): AppResult<SaleAllocationDto> = error("unused")
    override suspend fun previewAllocation(request: SaleAllocationRequestDto): AppResult<SalePreviewDto> = error("unused")
    override suspend fun confirmAllocation(idempotencyKey: String, request: SaleAllocationRequestDto): AppResult<SaleAllocationDto> = error("unused")
    override fun observeTaggingQueue(): Flow<SaleTaggingQueueDto?> = flowOf(null)
    override suspend fun refreshTaggingQueue(): AppResult<SaleTaggingQueueDto> = error("unused")
    override suspend fun taggingQueuePage(cursor: String): AppResult<SaleTaggingQueueDto> = error("unused")
    override fun observeTaggingDeal(dealId: String): Flow<SaleTaggingDealDto?> = flowOf(null)
    override suspend fun refreshTaggingDeal(dealId: String): AppResult<SaleTaggingDealDto> = error("unused")
}

/**
 * The load's SOP steps (PROCUREMENT IS SOP-DRIVEN END TO END, 2026-09-20): the workflow the backend
 * opened for this load, resolved by subject. Every other method is the interface's own default.
 */
private class FakeLoadStepsRepository(
    private val workflowId: String = "",
    private val detail: WorkflowDetailResponseDto? = null,
) : WorkflowsRepository {
    var subjectReads: List<Pair<String, String>> = emptyList()
        private set

    override fun cards(module: String, date: String, filter: String): Flow<PagingData<WorkflowCardDto>> = flowOf(PagingData.empty())
    override fun observeChips(module: String, date: String): Flow<WorkflowChipsDto?> = flowOf(null)
    override fun observeOverdueDates(module: String): Flow<List<WorkflowOverdueDateDto>> = flowOf(emptyList())
    override fun observeDetail(workflowId: String, lens: String, date: String): Flow<WorkflowDetailResponseDto?> = flowOf(detail)
    override fun observeVideoDrafts(workflowId: String): Flow<List<WorkflowVideoDraft>> = flowOf(emptyList())
    override suspend fun listVideoDrafts(workflowId: String): List<WorkflowVideoDraft> = emptyList()
    override suspend fun replaceVideoDraft(draft: WorkflowVideoDraft): WorkflowVideoDraft? = null
    override suspend fun clearVideoDrafts(workflowId: String) = Unit
    override suspend fun markVideoDraftsSubmitting(workflowId: String) = Unit
    override suspend fun refreshDetail(workflowId: String, lens: String, date: String): Result<Unit> = Result.success(Unit)
    override suspend fun refreshDetailBySubject(templateKey: String, subjectRefId: String): Result<String> {
        subjectReads = subjectReads + (templateKey to subjectRefId)
        return Result.success(workflowId)
    }

    override suspend fun findCachedCard(workflowId: String): WorkflowCardDto? = null
    override suspend fun markActionAnswered(workflowId: String, actionId: String, answerValue: String) = Unit
    override suspend fun markActionCompleted(workflowId: String, actionId: String, inReview: Boolean) = Unit
}
