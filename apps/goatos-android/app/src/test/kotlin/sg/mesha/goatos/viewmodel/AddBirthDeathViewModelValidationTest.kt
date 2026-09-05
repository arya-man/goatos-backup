package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import kotlinx.coroutines.test.TestScope
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CountsBreakdownQuery
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownRowDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownSeriesPointDto
import sg.mesha.goatos.core.network.dto.CountsBreedsResponseDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.CountsDestinationShedDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.GoatLocationPathDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.core.network.dto.DeathCauseCatalogDto
import sg.mesha.goatos.core.network.dto.DeathCauseOptionDto
import sg.mesha.goatos.core.data.DeathCauseVocabulary
import sg.mesha.goatos.feature.counts.AddBirthEvent
import sg.mesha.goatos.feature.counts.AddBirthField
import sg.mesha.goatos.feature.counts.AddDeathEvent
import sg.mesha.goatos.feature.counts.AddDeathUiState
import sg.mesha.goatos.feature.counts.DeathCauseKind
import sg.mesha.goatos.feature.counts.DeathCauseOptionUi
import sg.mesha.goatos.feature.counts.WorkflowModuleUi
import sg.mesha.goatos.rfid.FakeScanSource
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * The split add forms (docs/decisions/birth-death-workflows.md, maintainer decisions 2026-07-27):
 *
 *  - **Birth sends NO identifier** — neither `animal_identifier_1` nor `temporary_identifier` —
 *    because the SERVER auto-generates the provisional `CBE-#####`/`CPT-#####` tag; the client cannot pre-empt it.
 *  - **DOB and entry date are locked to today** (Asia/Kolkata) and the NEW `time_of_birth` field
 *    ships in `HH:MM`, validated client-side against the same 24-hour pattern the contract pins.
 *  - **Placement stays chosen-and-required**; death keeps its resolved-animal + row_version
 *    contract and its 3–500 character reason gate — carried over from the combined form unchanged.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AddBirthDeathViewModelValidationTest {
    private val dispatcher = StandardTestDispatcher()

    private lateinit var syncRepository: RecordingAddSyncRepository
    private lateinit var countsRepository: FakeAddCountsRepository
    private lateinit var deathCauses: FakeAddDeathCauseVocabulary
    private lateinit var scanSource: FakeScanSource
    private lateinit var analytics: AnalyticsPort
    private lateinit var crashReporter: CrashReporter
    private lateinit var savedStateHandle: SavedStateHandle

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
        syncRepository = RecordingAddSyncRepository()
        countsRepository = FakeAddCountsRepository()
        deathCauses = FakeAddDeathCauseVocabulary()
        scanSource = FakeScanSource()
        analytics = NoopAddAnalyticsPort()
        crashReporter = NoopAddCrashReporter()
        savedStateHandle = SavedStateHandle()
    }

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun newBirthViewModel() = AddBirthViewModel(
        syncRepository,
        countsRepository,
        scanSource,
        analytics,
        crashReporter,
        savedStateHandle,
    )

    private fun newDeathViewModel() = AddDeathViewModel(
        syncRepository,
        countsRepository,
        deathCauses,
        analytics,
        crashReporter,
        savedStateHandle,
    )

    // --- Birth ---------------------------------------------------------------------------------

    @Test
    fun `birth submit disabled until park and shed are selected`() = runTest(dispatcher) {
        val vm = newBirthViewModel()
        advanceUntilIdle()

        assertFalse("Placement required — no park/shed chosen yet", vm.state.value.canSubmit)

        vm.onEvent(AddBirthEvent.SelectPark(PARK_ID))
        assertFalse("Shed still unchosen", vm.state.value.canSubmit)

        vm.onEvent(AddBirthEvent.SelectShed(SHED_ID))
        assertFalse("Breed and mother RFID are still required", vm.state.value.canSubmit)
        completeBirthMetadata(vm)
        assertTrue("Placement, breed, and mother RFID chosen — submit enabled", vm.state.value.canSubmit)
    }

    @Test
    fun `birth prefills DOB to today and time of birth to a valid HH-MM`() = runTest(dispatcher) {
        val vm = newBirthViewModel()
        advanceUntilIdle()

        assertEquals("DOB is locked to today's business date", todayIst(), vm.state.value.dob)
        assertTrue(
            "Time of birth prefills to a valid 24h HH:MM",
            Regex("""^([01][0-9]|2[0-3]):[0-5][0-9]$""").matches(vm.state.value.timeOfBirth),
        )
    }

    @Test
    fun `an invalid time of birth blocks submit until corrected`() = runTest(dispatcher) {
        val vm = newBirthViewModel()
        advanceUntilIdle()
        vm.onEvent(AddBirthEvent.SelectPark(PARK_ID))
        vm.onEvent(AddBirthEvent.SelectShed(SHED_ID))
        completeBirthMetadata(vm)
        assertTrue(vm.state.value.canSubmit)

        vm.onEvent(AddBirthEvent.EditField(AddBirthField.TIME_OF_BIRTH, "25:99"))
        assertFalse("An out-of-range time blocks submit", vm.state.value.canSubmit)
        assertEquals("Enter the time of birth as HH:MM (24-hour).", vm.state.value.validationMessage)

        vm.onEvent(AddBirthEvent.EditField(AddBirthField.TIME_OF_BIRTH, "07:20"))
        assertTrue("A corrected time re-enables submit", vm.state.value.canSubmit)
    }

    @Test
    fun `birth submits with NO identifier, today's dates, and the time of birth`() = runTest(dispatcher) {
        val vm = newBirthViewModel()
        advanceUntilIdle()

        vm.onEvent(AddBirthEvent.SelectPark(PARK_ID))
        vm.onEvent(AddBirthEvent.SelectShed(SHED_ID))
        completeBirthMetadata(vm)
        vm.onEvent(AddBirthEvent.SelectLitterSize(2))
        vm.onEvent(AddBirthEvent.EditField(AddBirthField.TIME_OF_BIRTH, "07:20"))
        vm.onEvent(AddBirthEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastBirth
        assertTrue("A birth was enqueued", request != null)
        // The server mints the provisional CBE-#####/CPT-##### tag; the app must not send ANY identity.
        assertNull("No permanent RFID is sent", request!!.animalIdentifier1)
        assertNull("No temporary tag is sent", request.temporaryIdentifier)
        assertNull("No second RFID is sent", request.animalIdentifier2)
        assertEquals("DOB is locked to today", todayIst(), request.dob)
        assertEquals("Entry date is stamped to today", todayIst(), request.entryDate)
        assertEquals("The HH:MM time of birth ships on the wire", "07:20", request.timeOfBirth)
        assertEquals(PARK_ID, request.parkId)
        assertEquals(SHED_ID, request.shedId)
        assertEquals("beetal", request.breed)
        assertEquals("RFID-MOTHER-001", request.damId)
        assertEquals(2, request.litterSize)
    }

    @Test
    fun `record another starts a clean independent delivery`() = runTest(dispatcher) {
        val vm = newBirthViewModel()
        advanceUntilIdle()
        vm.onEvent(AddBirthEvent.SelectPark(PARK_ID))
        vm.onEvent(AddBirthEvent.SelectShed(SHED_ID))
        completeBirthMetadata(vm)
        vm.onEvent(AddBirthEvent.SelectLitterSize(2))
        vm.onEvent(AddBirthEvent.Submit)
        advanceUntilIdle()

        vm.onEvent(AddBirthEvent.RecordAnother)
        advanceUntilIdle()

        assertEquals("", vm.state.value.parkId)
        assertEquals("", vm.state.value.shedId)
        assertEquals("", vm.state.value.breed)
        assertEquals("", vm.state.value.damId)
        assertEquals(1, vm.state.value.litterSize)
        assertFalse("A separate delivery must be filled independently", vm.state.value.canSubmit)
    }

    @Test
    fun `successful birth sync clears the completed form instead of preserving sibling values`() = runTest(dispatcher) {
        val vm = newBirthViewModel()
        advanceUntilIdle()
        vm.onEvent(AddBirthEvent.SelectPark(PARK_ID))
        vm.onEvent(AddBirthEvent.SelectShed(SHED_ID))
        completeBirthMetadata(vm)
        vm.onEvent(AddBirthEvent.SelectLitterSize(2))
        vm.onEvent(AddBirthEvent.Submit)
        advanceUntilIdle()

        syncRepository.emitBirthSucceeded()
        advanceUntilIdle()

        assertEquals("", vm.state.value.parkId)
        assertEquals("", vm.state.value.shedId)
        assertEquals("", vm.state.value.breed)
        assertEquals("", vm.state.value.damId)
        assertEquals(1, vm.state.value.litterSize)
        assertTrue("The host must return to the Birth list after server acceptance", vm.state.value.returnToBirthList)
        assertEquals(
            "Birth recorded. 2 child workflows are ready; herd-count approval is separate.",
            vm.state.value.submissionNotice,
        )

        vm.onEvent(AddBirthEvent.NavigationHandled)
        assertFalse("The navigation signal is one-shot", vm.state.value.returnToBirthList)
    }

    @Test
    fun `empty Birth list describes child workflow work rather than count approval`() {
        assertEquals(
            "No birth follow-up work for this day.",
            workflowEmptyMessage(WorkflowModuleUi.BIRTH, isOffline = false),
        )
    }

    @Test
    fun `mother RFID scanner fills the required canonical lookup input`() = runTest(dispatcher) {
        val vm = newBirthViewModel()
        advanceUntilIdle()

        vm.onEvent(AddBirthEvent.ToggleMotherRfidScan)
        assertTrue(scanSource.isStarted)
        advanceUntilIdle()
        scanSource.emit(" RFID-MOTHER-009 ")
        advanceUntilIdle()

        assertEquals("RFID-MOTHER-009", vm.state.value.damId)
        assertFalse(vm.state.value.scanningMotherRfid)
        assertFalse(scanSource.isStarted)
    }

    // --- Death ---------------------------------------------------------------------------------

    @Test
    fun `death submit disabled until an animal is selected and a reason given`() = runTest(dispatcher) {
        val vm = newDeathViewModel()
        advanceUntilIdle()

        vm.onEvent(AddDeathEvent.EditAnimalQuery("TAG-77"))
        vm.onEvent(AddDeathEvent.LookupAnimals)
        advanceUntilIdle()
        assertFalse("No selection yet", vm.state.value.canSubmit)

        vm.onEvent(AddDeathEvent.SelectAnimal(GOAT_ID))
        assertFalse("Reason still missing", vm.state.value.canSubmit)

        vm.onEvent(AddDeathEvent.EditReason("Found dead in the shed"))
        assertTrue("Animal + reason present — submit enabled", vm.state.value.canSubmit)
    }

    @Test
    fun `a reason over 500 characters blocks submit`() = runTest(dispatcher) {
        val vm = newDeathViewModel()
        advanceUntilIdle()
        vm.onEvent(AddDeathEvent.EditAnimalQuery("TAG-77"))
        vm.onEvent(AddDeathEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(AddDeathEvent.SelectAnimal(GOAT_ID))

        vm.onEvent(AddDeathEvent.EditReason("x".repeat(501)))
        assertFalse(vm.state.value.canSubmit)
        assertEquals("Keep the account under 500 characters.", vm.state.value.validationMessage)
    }

    @Test
    fun `death write carries the selected animal's goat_id and row_version`() = runTest(dispatcher) {
        val vm = newDeathViewModel()
        advanceUntilIdle()
        vm.onEvent(AddDeathEvent.EditAnimalQuery("TAG-77"))
        vm.onEvent(AddDeathEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(AddDeathEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(AddDeathEvent.EditReason("Found dead in the shed"))

        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastDeath
        assertTrue("A death was enqueued", request != null)
        assertEquals("Death targets the resolved goat_id", GOAT_ID, request!!.goatId)
        assertEquals("row_version comes from the search result", ANIMAL_ROW_VERSION, request.rowVersion)
        // The dead+died pairing stays the DTO's server-enforced guardrail constants.
        assertEquals("dead", request.lifecycleStatus)
        assertEquals("died", request.exitReason)
    }

    private fun todayIst(): String =
        LocalDate.now(ZoneId.of("Asia/Kolkata")).format(DateTimeFormatter.ISO_LOCAL_DATE)

    private fun completeBirthMetadata(vm: AddBirthViewModel) {
        vm.onEvent(AddBirthEvent.EditField(AddBirthField.BREED, "beetal"))
        vm.onEvent(AddBirthEvent.EditField(AddBirthField.DAM_ID, "RFID-MOTHER-001"))
    }

    // --- Death: cause of death -------------------------------------------------------------
    //
    // Each of these is a way the FIELD gets this wrong, not a way the code does: the operator taps
    // "due to disease" and walks away without choosing; they choose and then change their mind;
    // the phone has never seen the list; the list changes under a choice already made. Every one
    // has a different right answer, and none of them may be a silent one — a death is terminal, so
    // there is no later correction.

    @Test
    fun `a normal death carries no cause and still needs its written account`() = runTest(dispatcher) {
        val vm = newDeathViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)

        // The account is the ONLY record of why a normal death happened, so submit is held.
        assertFalse(vm.state.value.canSubmit)

        vm.onEvent(AddDeathEvent.EditReason("Found down in the morning."))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)

        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()
        val request = syncRepository.lastDeath!!
        // ABSENT, not blank and not a placeholder: absence is what "no disease was established"
        // means, and it is what every death recorded before this feature existed also carries.
        assertNull(request.deathCauseKey)
        assertNull(request.deathCauseKind)
    }

    @Test
    fun `a disease death cannot be submitted until the disease is chosen`() = runTest(dispatcher) {
        val vm = newDeathViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)
        vm.onEvent(AddDeathEvent.EditReason("Off feed for two days."))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)

        // The toggle alone is a CLAIM with nothing behind it.
        vm.onEvent(AddDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        advanceUntilIdle()
        assertFalse(vm.state.value.canSubmit)
        assertEquals(
            "Choose the disease, or record this as a normal death.",
            vm.state.value.validationMessage,
        )

        vm.onEvent(AddDeathEvent.SelectDeathCause("MASTITIS"))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)
    }

    @Test
    fun `a disease death carries the key AND the kind the catalog supplied`() = runTest(dispatcher) {
        val vm = newDeathViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)
        vm.onEvent(AddDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        vm.onEvent(AddDeathEvent.SelectDeathCause("BLOAT"))
        advanceUntilIdle()

        // The note is OPTIONAL once a disease is named — the coded cause has answered why — so
        // submit is open with the account left blank.
        assertTrue(vm.state.value.canSubmit)

        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()
        val request = syncRepository.lastDeath!!
        assertEquals("BLOAT", request.deathCauseKey)
        // The KIND travels with the key, verbatim from the catalog: the same string can live in
        // two vocabularies, so a key that arrives without it cannot be read back.
        assertEquals("register_rule", request.deathCauseKind)
        assertEquals("", request.reason)
    }

    @Test
    fun `switching back to a normal death drops the disease from the write`() = runTest(dispatcher) {
        val vm = newDeathViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)
        vm.onEvent(AddDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        vm.onEvent(AddDeathEvent.SelectDeathCause("MASTITIS"))
        advanceUntilIdle()
        assertEquals("MASTITIS", vm.state.value.selectedDeathCause?.key)

        vm.onEvent(AddDeathEvent.SelectDeathCauseKind(DeathCauseKind.NORMAL))
        vm.onEvent(AddDeathEvent.EditReason("Found down in the morning."))
        advanceUntilIdle()
        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastDeath!!
        assertNull(request.deathCauseKey)
        assertNull(request.deathCauseKind)
    }

    /**
     * THE GATE ITSELF, exercised on the state rather than through the toggle handler.
     *
     * The obvious test does not bite: SelectDeathCauseKind(NORMAL) also clears the selection, so a
     * mutation removing the toggle check from `submittedDeathCause` survives it. Two things reach
     * that shape without the handler — a state restored across process death, and any future
     * caller that sets the kind directly — and the gate is what makes both safe.
     */
    @Test
    fun `a disease under a normal toggle is never what the write carries`() {
        val disease = DeathCauseOptionUi(key = "MASTITIS", kind = "register_rule", label = "Mastitis")
        val abandoned = AddDeathUiState(
            deathCauseKind = DeathCauseKind.NORMAL,
            selectedDeathCause = disease,
        )
        assertNull(abandoned.submittedDeathCause)
        assertEquals(disease, abandoned.copy(deathCauseKind = DeathCauseKind.DISEASE).submittedDeathCause)
    }

    @Test
    fun `the disease search filters on what the operator reads, not the rule id`() = runTest(dispatcher) {
        val vm = newDeathViewModel()
        advanceUntilIdle()
        vm.onEvent(AddDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        vm.onEvent(AddDeathEvent.EditDeathCauseQuery("foot"))
        advanceUntilIdle()
        assertEquals(listOf("FOOT_ROT"), vm.state.value.matchingDeathCauses.map { it.key })

        // Case-insensitive: an operator typing with one thumb does not capitalise.
        vm.onEvent(AddDeathEvent.EditDeathCauseQuery("MAST"))
        advanceUntilIdle()
        assertEquals(listOf("MASTITIS"), vm.state.value.matchingDeathCauses.map { it.key })

        // A blank query lists the whole vocabulary rather than nothing.
        vm.onEvent(AddDeathEvent.EditDeathCauseQuery(""))
        advanceUntilIdle()
        assertEquals(3, vm.state.value.matchingDeathCauses.size)
    }

    @Test
    fun `a phone that has never seen the disease list says so instead of offering an empty one`() =
        runTest(dispatcher) {
            deathCauses.catalog.value = null
            deathCauses.refreshError = IllegalStateException("offline")
            val vm = newDeathViewModel()
            advanceUntilIdle()

            // An empty dropdown reads as "this farm has no diseases" and would file a disease
            // death as normal with nobody noticing. The screen renders this beside a dimmed
            // DISEASE choice, so the operator knows what the product could not reach.
            assertTrue(vm.state.value.deathCauseOptions.isEmpty())
            assertEquals(
                "The disease list isn't on this phone yet. Connect once to load it, or record a " +
                    "normal death and describe what you saw.",
                vm.state.value.deathCauseMessage,
            )
        }

    @Test
    fun `a cached list keeps serving when the refresh fails`() = runTest(dispatcher) {
        deathCauses.refreshError = IllegalStateException("offline")
        val vm = newDeathViewModel()
        advanceUntilIdle()

        // The moment an operator needs this list is the moment they are standing in a pen with no
        // signal. A failed refresh must never blank what the device already holds.
        assertEquals(3, vm.state.value.deathCauseOptions.size)
        assertNull(vm.state.value.deathCauseMessage)
    }

    @Test
    fun `a disease that stops being offered is dropped rather than submitted`() = runTest(dispatcher) {
        val vm = newDeathViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)
        vm.onEvent(AddDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        vm.onEvent(AddDeathEvent.SelectDeathCause("MASTITIS"))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)

        // The registers were edited and this rule is gone. Keeping the selection would send a key
        // the server now rejects — and it would reject it only AFTER the operator had left the
        // form believing an irreversible record had been made.
        deathCauses.catalog.value = DeathCauseCatalogDto(
            options = listOf(DeathCauseOptionDto(key = "BLOAT", kind = "register_rule", label = "Bloat")),
        )
        advanceUntilIdle()

        assertNull(vm.state.value.selectedDeathCause)
        assertFalse(vm.state.value.canSubmit)
    }

    @Test
    fun `recording another death keeps the disease list and drops the disease`() = runTest(dispatcher) {
        val vm = newDeathViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)
        vm.onEvent(AddDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        vm.onEvent(AddDeathEvent.SelectDeathCause("MASTITIS"))
        advanceUntilIdle()
        vm.onEvent(AddDeathEvent.Submit)
        advanceUntilIdle()

        vm.onEvent(AddDeathEvent.RecordAnother)
        advanceUntilIdle()

        // The VOCABULARY survives — re-fetching between two deaths would leave the second animal
        // without a dropdown on a phone that has since lost signal.
        assertEquals(3, vm.state.value.deathCauseOptions.size)
        // The SELECTION does not: carrying it forward would file a diagnosis nobody made.
        assertNull(vm.state.value.selectedDeathCause)
        assertEquals(DeathCauseKind.NORMAL, vm.state.value.deathCauseKind)
    }

    // --- Death opened from a treatment screen ------------------------------------------------
    //
    // The animal died mid-course. The person treating it is standing on the Health screen, so the
    // death is raised from there — through the SAME form, the same producer and the same approval
    // gate, with the animal and the disease already filled in.

    @Test
    fun `a death opened from a treatment screen arrives with the animal and disease filled in`() =
        runTest(dispatcher) {
            savedStateHandle["animal"] = "TAG-77"
            savedStateHandle["cause"] = "MASTITIS"
            val vm = newDeathViewModel()
            advanceUntilIdle()

            assertEquals("TAG-77", vm.state.value.animalQuery)
            assertEquals(DeathCauseKind.DISEASE, vm.state.value.deathCauseKind)
            // Resolved from a REAL catalog row, so the kind travels with the key. Synthesising an
            // option from the bare string handed over would put a key on the wire that no list
            // ever offered.
            assertEquals("MASTITIS", vm.state.value.selectedDeathCause?.key)
            assertEquals("register_rule", vm.state.value.selectedDeathCause?.kind)

            // The search is NOT run for them: a lookup firing on its own would put a list of
            // animals on screen nobody asked for, on the one form where a stray tap is
            // irreversible. The animal is still unconfirmed, so submit is held.
            assertTrue(vm.state.value.animalMatches.isEmpty())
            assertNull(vm.state.value.selectedAnimal)
            assertFalse(vm.state.value.canSubmit)
        }

    @Test
    fun `a pre-engine case hands over no disease and the form opens on the search`() =
        runTest(dispatcher) {
            // A case opened before the diagnosis engine carries only its treatment card, so the
            // treatment screen has no rule to hand over. Blank is a real state, not missing data.
            savedStateHandle["animal"] = "TAG-77"
            savedStateHandle["cause"] = ""
            val vm = newDeathViewModel()
            advanceUntilIdle()

            assertEquals("TAG-77", vm.state.value.animalQuery)
            // NORMAL, not DISEASE: the form must not assert a disease death it was given no
            // disease for. The operator chooses, exactly as they would from the ＋ path.
            assertEquals(DeathCauseKind.NORMAL, vm.state.value.deathCauseKind)
            assertNull(vm.state.value.selectedDeathCause)
        }

    @Test
    fun `a handed-over disease the register no longer names leaves the operator searching`() =
        runTest(dispatcher) {
            savedStateHandle["animal"] = "TAG-77"
            savedStateHandle["cause"] = "RETIRED_RULE"
            val vm = newDeathViewModel()
            advanceUntilIdle()

            // The toggle still says DISEASE — the treatment screen was standing on one — but
            // nothing is selected, so submit is held until the operator names it from the list.
            // Passing the unknown key through would be rejected server-side only AFTER they had
            // left the form believing an irreversible record had been made.
            assertEquals(DeathCauseKind.DISEASE, vm.state.value.deathCauseKind)
            assertNull(vm.state.value.selectedDeathCause)
            selectTheDeadAnimal(vm)
            assertFalse(vm.state.value.canSubmit)
            assertEquals(
                "Choose the disease, or record this as a normal death.",
                vm.state.value.validationMessage,
            )
        }

    @Test
    fun `the plain add-death form is untouched by the prefill path`() = runTest(dispatcher) {
        // The ＋ button opens the bare route, so neither argument is present. Nothing about the
        // form may change for it — this is the path every death has taken until now.
        val vm = newDeathViewModel()
        advanceUntilIdle()

        assertEquals("", vm.state.value.animalQuery)
        assertEquals(DeathCauseKind.NORMAL, vm.state.value.deathCauseKind)
        assertNull(vm.state.value.selectedDeathCause)
    }

    /** Search for and select the one animal the fake lookup returns. */
    private fun TestScope.selectTheDeadAnimal(vm: AddDeathViewModel) {
        vm.onEvent(AddDeathEvent.EditAnimalQuery("TAG-77"))
        vm.onEvent(AddDeathEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(AddDeathEvent.SelectAnimal(GOAT_ID))
        advanceUntilIdle()
    }

    private companion object {
        const val PARK_ID = "11111111-1111-1111-1111-111111111111"
        const val SHED_ID = "33333333-3333-3333-3333-333333333333"
        const val GOAT_ID = "44444444-4444-4444-4444-444444444444"
        const val ANIMAL_ROW_VERSION = 7
    }
}

// --- Fakes -----------------------------------------------------------------------------------

/** Captures the enqueued birth/death requests so a test can assert the exact wire payload. */
// internal rather than private: AddBirthPlacementTest drives the same add-birth view-model with
// different destination payloads and reuses these fakes instead of forking a second copy of the
// whole CountsRepository surface, which would drift.
internal class RecordingAddSyncRepository : SyncRepository {
    var lastBirth: CountsBirthEventRequestDto? = null
    var lastDeath: CountsDeathEventRequestDto? = null

    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
    override suspend fun enqueueCountsBirth(groupKey: String, idempotencyKey: String, request: CountsBirthEventRequestDto): AppResult<String> {
        lastBirth = request
        return AppResult.Ok("outbox-birth-1")
    }
    override suspend fun enqueueCountsDeath(groupKey: String, idempotencyKey: String, request: CountsDeathEventRequestDto): AppResult<String> {
        lastDeath = request
        return AppResult.Ok("outbox-death-1")
    }

    fun emitBirthSucceeded() {
        status.value = SyncStatus(
            online = true,
            pendingCount = 0,
            inFlightCount = 0,
            failedCount = 0,
            deadLetterCount = 0,
            lastSyncAt = 1L,
            items = listOf(
                SyncQueueItem(
                    id = "outbox-birth-1",
                    idempotencyKey = "test-idempotency-key",
                    opType = "COUNTS_BIRTH",
                    groupKey = "birth-1",
                    status = SyncItemStatus.SUCCEEDED,
                    attemptCount = 1,
                    maxAttempts = 5,
                    conflict = false,
                    createdAt = 1L,
                    updatedAt = 1L,
                    lastError = null,
                    resultJson = "{\"approval_request_id\":\"9015d472-3fdc-4920-b10f-830478096e64\",\"request_type\":\"birth\",\"status\":\"pending\",\"raised_at\":\"2026-07-28T12:24:17+05:30\",\"idempotent_replay\":false,\"children\":[{\"goat_id\":\"00000000-0000-0000-0000-000000000001\",\"temporary_identifier\":\"CBE-12345\",\"child_ordinal\":1},{\"goat_id\":\"00000000-0000-0000-0000-000000000002\",\"temporary_identifier\":\"CBE-67890\",\"child_ordinal\":2}]}",
                ),
            ),
        )
    }
}

/** Mirrors Room: one park with one shed, one breed, one resolvable animal with a row_version. */
internal class FakeAddCountsRepository : CountsRepository {
    override fun observeHerdSummary(
        lifecycleStatus: String?,
        parkId: String?,
        breed: String?,
        sex: String?,
    ): Flow<Resource<HerdRegisterSummaryResponseDto>> =
        MutableStateFlow(Resource(data = HerdRegisterSummaryResponseDto()))

    override suspend fun refreshHerdSummary(
        lifecycleStatus: String?,
        parkId: String?,
        breed: String?,
        sex: String?,
    ): Result<Unit> = Result.success(Unit)

    override fun observeBreakdownTotals(
        query: CountsBreakdownQuery,
    ): Flow<Resource<CountsBreakdownResponseDto>> =
        MutableStateFlow(Resource(data = CountsBreakdownResponseDto()))

    override fun observeBirthBreeds(): Flow<Resource<CountsBreedsResponseDto>> =
        MutableStateFlow(
            Resource(
                data = CountsBreedsResponseDto(
                    breeds = listOf(CountsBreakdownSeriesPointDto(key = "beetal", label = "Beetal", count = 12)),
                ),
            ),
        )

    override suspend fun refreshBirthBreeds(): Result<Unit> = Result.success(Unit)

    override fun breakdownRows(query: CountsBreakdownQuery): Flow<PagingData<CountsBreakdownRowDto>> =
        flowOf(PagingData.empty())

    /**
     * Settable so a test can serve a park with one kid pen, several, or none. Defaults to the
     * record_later shape (no birth_placement), which is exactly what a backend predating the
     * newborn-placement contract returns.
     */
    var destinations: CountsShiftingDestinationsResponseDto = CountsShiftingDestinationsResponseDto(
        parks = listOf(
            CountsDestinationParkDto(
                parkId = "11111111-1111-1111-1111-111111111111",
                name = "North Park",
                sheds = listOf(
                    CountsDestinationShedDto(
                        shedId = "33333333-3333-3333-3333-333333333333",
                        name = "Shed A",
                    ),
                ),
            ),
        ),
    )

    override fun observeShiftingDestinations(): Flow<Resource<CountsShiftingDestinationsResponseDto>> =
        MutableStateFlow(
            Resource(
                data = destinations,
            ),
        )

    override suspend fun refreshShiftingDestinations(): Result<Unit> = Result.success(Unit)

    override suspend fun lookupAnimals(
        query: String,
        parkId: String?,
        shedId: String?,
    ): Result<List<GoatSearchItemDto>> = Result.success(
        listOf(
            GoatSearchItemDto(
                goatId = "44444444-4444-4444-4444-444444444444",
                displayId = "G-77",
                animalIdentifier1 = "TAG-77",
                sex = "female",
                lifecycleStatus = "alive",
                rowVersion = 7,
                locationPath = GoatLocationPathDto(operationalLocationDisplay = "North Park / Shed A"),
            ),
        ),
    )
}

internal class NoopAddAnalyticsPort : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

internal class NoopAddCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}

/**
 * The disease vocabulary, as small as the narrow port allows. [catalog] is a MutableStateFlow so a
 * test can make the list arrive late, arrive empty, or change under a selection the operator has
 * already made — the three ways this dropdown goes wrong in the field.
 */
private class FakeAddDeathCauseVocabulary : DeathCauseVocabulary {
    val catalog = MutableStateFlow<DeathCauseCatalogDto?>(
        DeathCauseCatalogDto(
            options = listOf(
                DeathCauseOptionDto(key = "MASTITIS", kind = "register_rule", label = "Mastitis"),
                DeathCauseOptionDto(key = "BLOAT", kind = "register_rule", label = "Bloat"),
                DeathCauseOptionDto(key = "FOOT_ROT", kind = "register_rule", label = "Foot rot"),
            ),
        ),
    )

    /** When set, [refreshDeathCauses] fails — the offline phone. */
    var refreshError: Throwable? = null

    override fun observeDeathCauses(): Flow<DeathCauseCatalogDto?> = catalog

    override suspend fun refreshDeathCauses(): Result<Unit> =
        refreshError?.let { Result.failure(it) } ?: Result.success(Unit)
}
