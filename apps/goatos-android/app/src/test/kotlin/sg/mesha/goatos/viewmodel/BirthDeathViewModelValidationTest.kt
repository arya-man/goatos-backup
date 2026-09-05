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
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CountsBreakdownQuery
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
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
import sg.mesha.goatos.feature.counts.BIRTH_ID_KIND_TEMPORARY
import sg.mesha.goatos.feature.counts.BirthDeathEvent
import sg.mesha.goatos.feature.counts.BirthDeathField
import sg.mesha.goatos.feature.counts.BirthDeathMode
import kotlinx.coroutines.test.TestScope
import sg.mesha.goatos.core.data.DeathCauseVocabulary
import sg.mesha.goatos.core.network.dto.DeathCauseCatalogDto
import sg.mesha.goatos.core.network.dto.DeathCauseOptionDto
import sg.mesha.goatos.feature.counts.BirthDeathUiState
import sg.mesha.goatos.feature.counts.DeathCauseKind
import sg.mesha.goatos.feature.counts.DeathCauseOptionUi
import sg.mesha.goatos.rfid.FakeScanSource

/**
 * BirthDeathViewModel is now SELECTOR/SCAN-driven, so these lock in the two things the PR#11
 * follow-up existed to fix:
 *
 *  - **Birth placement is chosen, not typed.** Park and shed come from the shifting-destinations
 *    catalog (names shown, ids submitted) and BOTH are REQUIRED — an operator can never record a
 *    newborn into no shed or into a hand-typed UUID.
 *  - **The death target is a resolved animal, not a typed UUID + record version.** The operator
 *    searches a tag, selects one animal, and the death write carries that animal's OWN `row_version`
 *    from the search result — never a value the operator typed.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class BirthDeathViewModelValidationTest {
    private val dispatcher = StandardTestDispatcher()

    private lateinit var syncRepository: RecordingCountsSyncRepository
    private lateinit var countsRepository: FakeBirthDeathCountsRepository
    private lateinit var deathCauses: FakeDeathCauseVocabulary
    private lateinit var scanSource: FakeScanSource
    private lateinit var analytics: AnalyticsPort
    private lateinit var crashReporter: CrashReporter
    private lateinit var savedStateHandle: SavedStateHandle

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
        syncRepository = RecordingCountsSyncRepository()
        countsRepository = FakeBirthDeathCountsRepository()
        deathCauses = FakeDeathCauseVocabulary()
        scanSource = FakeScanSource()
        analytics = NoopAnalyticsPort()
        crashReporter = NoopTestCrashReporter()
        savedStateHandle = SavedStateHandle()
    }

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun newViewModel() = BirthDeathViewModel(
        syncRepository,
        countsRepository,
        deathCauses,
        scanSource,
        analytics,
        crashReporter,
        savedStateHandle,
    )

    private fun completeRequiredBirthMetadata(vm: BirthDeathViewModel) {
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.BREED, BREED_KEY))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DAM_ID, MOTHER_RFID))
    }

    // --- Birth: placement is chosen and required ---------------------------------------------

    @Test
    fun `birth submit disabled until park and shed are selected`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle() // let the destinations catalog emit

        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2020-01-01"))
        completeRequiredBirthMetadata(vm)

        assertFalse(
            "Placement is required — submit stays disabled with no park/shed chosen",
            vm.state.value.canSubmit,
        )

        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        assertFalse("Shed still unchosen", vm.state.value.canSubmit)

        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))
        assertTrue("Both placement ids chosen — submit enabled", vm.state.value.canSubmit)
    }

    @Test
    fun `birth submits the selected placement ids, never typed text`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2020-01-01"))
        completeRequiredBirthMetadata(vm)
        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))

        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastBirth
        assertTrue("A birth was enqueued", request != null)
        assertEquals(PARK_ID, request!!.parkId)
        assertEquals(SHED_ID, request.shedId)
    }

    // --- Birth: single identifier, auto entry date, chosen breed ------------------------------

    @Test
    fun `birth needs only one identifier and auto-stamps today's entry date`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        // Only one CHILD identifier is required; mother RFID and breed are mandatory metadata.
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2020-01-01"))
        completeRequiredBirthMetadata(vm)
        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))
        assertTrue("Child identifier + mother + breed + dob + placement submit", vm.state.value.canSubmit)

        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastBirth
        assertTrue("A birth was enqueued", request != null)
        assertEquals("Only the primary identifier is sent", "Goat001", request!!.animalIdentifier1)
        assertEquals("A blank second RFID is sent as null", null, request.animalIdentifier2)
        // Entry date is stamped automatically to today's business date (Asia/Kolkata), never typed.
        val today = java.time.LocalDate.now(java.time.ZoneId.of("Asia/Kolkata"))
            .format(java.time.format.DateTimeFormatter.ISO_LOCAL_DATE)
        assertEquals("Entry date is auto-stamped to today", today, request.entryDate)
    }

    @Test
    fun `birth sends the optional second permanent RFID when provided`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        // A newborn given two permanent ear tags: both identifiers are sent (second is optional).
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "RFID-A"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG2, "RFID-B"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2020-01-01"))
        completeRequiredBirthMetadata(vm)
        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))
        assertTrue("Two distinct RFIDs + dob + placement submits", vm.state.value.canSubmit)

        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastBirth
        assertTrue("A birth was enqueued", request != null)
        assertEquals("Primary RFID sent", "RFID-A", request!!.animalIdentifier1)
        assertEquals("Second RFID sent as animal_identifier_2", "RFID-B", request.animalIdentifier2)
    }

    @Test
    fun `a second RFID equal to the first blocks submit`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "RFID-A"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2020-01-01"))
        completeRequiredBirthMetadata(vm)
        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))
        assertTrue("Valid before the duplicate second RFID", vm.state.value.canSubmit)

        // Case-insensitive duplicate of the first RFID must block submit (backend enforces this too).
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG2, "rfid-a"))
        assertTrue("A duplicate second RFID blocks submit", !vm.state.value.canSubmit)
        assertEquals(
            "The second RFID must differ from the first.",
            vm.state.value.validationMessage,
        )
    }

    @Test
    fun `a temporary tag never carries a second permanent RFID`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        // Even if a stale tag2 lingered, the temporary path must not send a second permanent RFID.
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG2, "RFID-B"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.ID_KIND, BIRTH_ID_KIND_TEMPORARY))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "TMP-77"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2020-01-01"))
        completeRequiredBirthMetadata(vm)
        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))

        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastBirth
        assertTrue("A birth was enqueued", request != null)
        assertEquals("Temporary tag is the primary identity", "TMP-77", request!!.temporaryIdentifier)
        assertEquals("No permanent primary on the temporary path", null, request.animalIdentifier1)
        assertEquals("No second permanent RFID on the temporary path", null, request.animalIdentifier2)
    }

    @Test
    fun `breed options come from the herd's backend facet, and the chosen key is submitted`() =
        runTest(dispatcher) {
            val vm = newViewModel()
            advanceUntilIdle() // let the breed facet emit from the cached breakdown envelope

            assertEquals(
                "Breed options are the backend breed facet, not a hardcoded list",
                listOf(BREED_KEY),
                vm.state.value.breedOptions.map { it.key },
            )

            vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
            vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2020-01-01"))
            vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.BREED, BREED_KEY))
            vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DAM_ID, MOTHER_RFID))
            vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
            vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))

            vm.onEvent(BirthDeathEvent.Submit)
            advanceUntilIdle()

            assertEquals(
                "The selected breed facet key is submitted verbatim",
                BREED_KEY,
                syncRepository.lastBirth?.breed,
            )
        }

    @Test
    fun `breed options are fetched even when the breed cache is cold`() = runTest(dispatcher) {
        // A field operator has CountsWrite (to record births) but not the CountsRead the Counts
        // Breakdown breed facet needs, so sourcing breeds from there 403s and the picker used to stay
        // disabled (empty options). The ViewModel must fetch the vocabulary from the operator
        // `/app/counts/breeds` endpoint itself. With the cold cache the picker is disabled at first…
        countsRepository = FakeBirthDeathCountsRepository(breedCacheColdUntilRefresh = true)
        val vm = newViewModel()

        // …and becomes usable only because init triggered a breeds refresh that warmed the cache.
        advanceUntilIdle()

        assertTrue(
            "The ViewModel fetched the breed vocabulary itself",
            countsRepository.refreshBreedsCalls >= 1,
        )
        assertEquals(
            "Breed options are populated from the fetched vocabulary, not left empty",
            listOf(BREED_KEY),
            vm.state.value.breedOptions.map { it.key },
        )
    }

    @Test
    fun `future date of birth blocks submit`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))
        completeRequiredBirthMetadata(vm)
        // A dob after today's auto entry date must fail closed — the backend rule dob <= entry_date
        // reduces to dob <= today once entry date is auto-stamped.
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2999-01-01"))

        assertFalse("A future date of birth cannot be submitted", vm.state.value.canSubmit)
    }

    @Test
    fun `selecting a different park resets the shed`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))
        assertEquals(SHED_ID, vm.state.value.shedId)

        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID_2))
        assertEquals("Changing park clears the shed", "", vm.state.value.shedId)
    }

    // --- Death: target is a resolved animal carrying its own row_version ----------------------

    @Test
    fun `death submit disabled until an animal is selected`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Found dead in the shed"))

        assertFalse(
            "No animal selected yet — submit stays disabled",
            vm.state.value.canSubmit,
        )
    }

    @Test
    fun `death submit enabled after selecting a searched animal`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        vm.onEvent(BirthDeathEvent.EditAnimalQuery("TAG-77"))
        vm.onEvent(BirthDeathEvent.LookupAnimals)
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Found dead in the shed"))

        assertTrue("Animal + reason present — submit enabled", vm.state.value.canSubmit)
    }

    @Test
    fun `death write carries the selected animal's row_version, not a typed value`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        vm.onEvent(BirthDeathEvent.EditAnimalQuery("TAG-77"))
        vm.onEvent(BirthDeathEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(BirthDeathEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Found dead in the shed"))

        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastDeath
        assertTrue("A death was enqueued", request != null)
        assertEquals("Death targets the resolved goat_id", GOAT_ID, request!!.goatId)
        assertEquals("row_version comes from the search result", ANIMAL_ROW_VERSION, request.rowVersion)
    }

    // --- Birth: the permanent identifiers are SCANNABLE ---------------------------------------

    @Test
    fun `a scanned tag fills the primary identifier and releases the reader`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.ToggleRfidScan(BirthDeathField.TAG))
        advanceUntilIdle()
        assertTrue("The reader is listening", scanSource.isStarted)
        assertEquals(BirthDeathField.TAG, vm.state.value.scanningField)

        scanSource.emit("982000123456789")
        advanceUntilIdle()

        assertEquals("The scan lands in animal_identifier_1", "982000123456789", vm.state.value.tag)
        // One ear tag is one identifier: capture stops so the NEXT animal's tag cannot silently
        // overwrite the one just scanned.
        assertEquals("Capture released after the read", null, vm.state.value.scanningField)
        assertFalse("The BT-HID reader was stopped", scanSource.isStarted)
    }

    @Test
    fun `tapping the scanning field again stops the reader`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.ToggleRfidScan(BirthDeathField.TAG))
        advanceUntilIdle()
        vm.onEvent(BirthDeathEvent.ToggleRfidScan(BirthDeathField.TAG))
        advanceUntilIdle()

        assertEquals(null, vm.state.value.scanningField)
        assertFalse(scanSource.isStarted)
    }

    @Test
    fun `scanning the second identifier hands the reader over from the first`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.ToggleRfidScan(BirthDeathField.TAG))
        advanceUntilIdle()
        vm.onEvent(BirthDeathEvent.ToggleRfidScan(BirthDeathField.TAG2))
        advanceUntilIdle()

        assertEquals("Only the second field is listening", BirthDeathField.TAG2, vm.state.value.scanningField)

        scanSource.emit("982000987654321")
        advanceUntilIdle()

        assertEquals("The scan lands in animal_identifier_2", "982000987654321", vm.state.value.tag2)
        assertEquals("The first identifier is untouched", "", vm.state.value.tag)
    }

    @Test
    fun `switching to the temporary path releases the reader`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.ToggleRfidScan(BirthDeathField.TAG))
        advanceUntilIdle()
        // A provisional tag is hand-written — both permanent-RFID fields disappear, so a scan in
        // progress has lost its destination and must not keep eating hardware key events.
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.ID_KIND, BIRTH_ID_KIND_TEMPORARY))
        advanceUntilIdle()

        assertEquals(null, vm.state.value.scanningField)
        assertFalse(scanSource.isStarted)
    }

    @Test
    fun `switching to the death mode releases the reader`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.ToggleRfidScan(BirthDeathField.TAG))
        advanceUntilIdle()
        vm.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        advanceUntilIdle()

        assertEquals("Death has no identifier field to scan into", null, vm.state.value.scanningField)
        assertFalse(scanSource.isStarted)
    }

    @Test
    fun `a scanned identifier submits exactly as a typed one would`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.ToggleRfidScan(BirthDeathField.TAG))
        advanceUntilIdle()
        scanSource.emit("982000123456789")
        advanceUntilIdle()
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2026-07-01"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.BREED, BREED_KEY))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DAM_ID, MOTHER_RFID))
        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))

        assertTrue("A scanned tag satisfies the submit gate", vm.state.value.canSubmit)
        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastBirth
        assertTrue("A birth was enqueued", request != null)
        assertEquals(
            "The scanned tag reaches the wire payload unchanged",
            "982000123456789",
            request!!.animalIdentifier1,
        )
    }

    private companion object {
        const val PARK_ID = "11111111-1111-1111-1111-111111111111"
        const val PARK_ID_2 = "22222222-2222-2222-2222-222222222222"
        const val SHED_ID = "33333333-3333-3333-3333-333333333333"
        const val GOAT_ID = "44444444-4444-4444-4444-444444444444"
        const val ANIMAL_ROW_VERSION = 7
        const val BREED_KEY = "beetal"
        const val MOTHER_RFID = "982000000000001"
    }
    // --- Death: cause of death -------------------------------------------------------------
    //
    // Every one of these is a way the field gets this wrong, not a way the code gets it wrong:
    // the operator taps "due to disease" and walks away without choosing; they choose and then
    // change their mind; the phone has never seen the list; the list changes under a choice
    // already made. Each has a different right answer and none of them may be a silent one.

    @Test
    fun `a normal death carries no cause and still needs its written account`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)

        // The account is the ONLY record of why a normal death happened, so submit is held.
        assertFalse(vm.state.value.canSubmit)

        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Found down in the morning."))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)

        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()
        val request = syncRepository.lastDeath!!
        // ABSENT, not blank and not a placeholder: absence is what "no disease was established"
        // means, and it is what every death recorded before this feature existed also carries.
        assertNull(request.deathCauseKey)
        assertNull(request.deathCauseKind)
    }

    @Test
    fun `a disease death cannot be submitted until the disease is chosen`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Off feed for two days."))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)

        // The toggle alone is a CLAIM with nothing behind it. Letting it through would record a
        // death the operator said was caused by something as one caused by nothing.
        vm.onEvent(BirthDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        advanceUntilIdle()
        assertFalse(vm.state.value.canSubmit)
        assertEquals(
            "Choose the disease, or record this as a normal death.",
            vm.state.value.validationMessage,
        )

        vm.onEvent(BirthDeathEvent.SelectDeathCause("MASTITIS"))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)
    }

    @Test
    fun `a disease death carries the key AND the kind the catalog supplied`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)
        vm.onEvent(BirthDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        vm.onEvent(BirthDeathEvent.SelectDeathCause("BLOAT"))
        advanceUntilIdle()

        // The note is OPTIONAL once a disease is named -- the coded cause already answers why --
        // so submit is open with the account left blank.
        assertTrue(vm.state.value.canSubmit)

        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()
        val request = syncRepository.lastDeath!!
        assertEquals("BLOAT", request.deathCauseKey)
        // The KIND travels with the key, taken verbatim from the catalog. The same string can live
        // in two vocabularies, so a key that arrives without it cannot be read back.
        assertEquals("register_rule", request.deathCauseKind)
        assertEquals("", request.reason)
    }

    @Test
    fun `switching back to a normal death drops the disease from the write`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)
        vm.onEvent(BirthDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        vm.onEvent(BirthDeathEvent.SelectDeathCause("MASTITIS"))
        advanceUntilIdle()
        assertEquals("MASTITIS", vm.state.value.selectedDeathCause?.key)

        vm.onEvent(BirthDeathEvent.SelectDeathCauseKind(DeathCauseKind.NORMAL))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Found down in the morning."))
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()
        val request = syncRepository.lastDeath!!
        // An abandoned diagnosis must not ride along invisibly on a death the operator has just
        // said was not caused by one.
        assertNull(request.deathCauseKey)
        assertNull(request.deathCauseKind)
    }

    @Test
    fun `the disease search filters on what the operator reads, not the rule id`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()
        vm.onEvent(BirthDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        vm.onEvent(BirthDeathEvent.EditDeathCauseQuery("foot"))
        advanceUntilIdle()
        assertEquals(listOf("FOOT_ROT"), vm.state.value.matchingDeathCauses.map { it.key })

        // Case-insensitive: an operator typing with one thumb does not capitalise.
        vm.onEvent(BirthDeathEvent.EditDeathCauseQuery("MAST"))
        advanceUntilIdle()
        assertEquals(listOf("MASTITIS"), vm.state.value.matchingDeathCauses.map { it.key })

        // A blank query lists the whole vocabulary rather than nothing.
        vm.onEvent(BirthDeathEvent.EditDeathCauseQuery(""))
        advanceUntilIdle()
        assertEquals(3, vm.state.value.matchingDeathCauses.size)
    }

    @Test
    fun `a phone that has never seen the disease list says so instead of offering an empty one`() =
        runTest(dispatcher) {
            deathCauses.catalog.value = null
            deathCauses.refreshError = IllegalStateException("offline")
            val vm = newViewModel()
            advanceUntilIdle()

            // An empty dropdown would read as "this farm has no diseases" and would file a disease
            // death as normal with nobody noticing. The screen renders this message beside a dimmed
            // DISEASE choice.
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
        val vm = newViewModel()
        advanceUntilIdle()

        // The moment an operator needs this list is the moment they are standing in a pen with no
        // signal. A failed refresh must never blank what the device already holds.
        assertEquals(3, vm.state.value.deathCauseOptions.size)
        assertNull(vm.state.value.deathCauseMessage)
    }

    @Test
    fun `a disease that stops being offered is dropped rather than submitted`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)
        vm.onEvent(BirthDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        vm.onEvent(BirthDeathEvent.SelectDeathCause("MASTITIS"))
        advanceUntilIdle()
        assertTrue(vm.state.value.canSubmit)

        // The registers were edited and this rule is gone. Keeping the selection would send a key
        // the server now rejects -- and it would reject it only AFTER the operator had left the
        // form believing the death was recorded.
        deathCauses.catalog.value = DeathCauseCatalogDto(
            options = listOf(DeathCauseOptionDto(key = "BLOAT", kind = "register_rule", label = "Bloat")),
        )
        advanceUntilIdle()

        assertNull(vm.state.value.selectedDeathCause)
        assertFalse(vm.state.value.canSubmit)
    }

    @Test
    fun `recording another death keeps the disease list and drops the disease`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()
        selectTheDeadAnimal(vm)
        vm.onEvent(BirthDeathEvent.SelectDeathCauseKind(DeathCauseKind.DISEASE))
        vm.onEvent(BirthDeathEvent.SelectDeathCause("MASTITIS"))
        advanceUntilIdle()
        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.RecordAnother)
        advanceUntilIdle()

        // The VOCABULARY survives -- re-fetching between two deaths would leave the second animal
        // without a dropdown on a phone that has since lost signal.
        assertEquals(3, vm.state.value.deathCauseOptions.size)
        // The SELECTION does not: carrying it forward would file a diagnosis nobody made for the
        // next animal.
        assertNull(vm.state.value.selectedDeathCause)
        assertEquals(DeathCauseKind.NORMAL, vm.state.value.deathCauseKind)
    }

    /**
     * THE GATE ITSELF, exercised directly rather than through the event handler.
     *
     * This exists because the obvious test does not bite. Going through
     * SelectDeathCauseKind(NORMAL) passes whether or not the gate is there, since that handler
     * ALSO clears the selection -- so a mutation removing the toggle check from
     * `submittedDeathCause` survived the whole suite. Two things can put the state into the
     * dangerous shape without touching that handler: a state restored across process death, and
     * any future code that sets the kind without routing through the handler. The gate is what
     * makes those safe, so it needs an assertion that only the gate can satisfy.
     */
    @Test
    fun `a disease under a normal toggle is never what the write carries`() {
        val disease = DeathCauseOptionUi(key = "MASTITIS", kind = "register_rule", label = "Mastitis")

        val abandoned = BirthDeathUiState(
            mode = BirthDeathMode.DEATH,
            deathCauseKind = DeathCauseKind.NORMAL,
            selectedDeathCause = disease,
        )
        assertNull(abandoned.submittedDeathCause)

        val claimed = abandoned.copy(deathCauseKind = DeathCauseKind.DISEASE)
        assertEquals(disease, claimed.submittedDeathCause)
    }

    /** Search for and select the one animal the fake lookup returns. */
    private fun TestScope.selectTheDeadAnimal(vm: BirthDeathViewModel) {
        vm.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        vm.onEvent(BirthDeathEvent.EditAnimalQuery("TAG-77"))
        vm.onEvent(BirthDeathEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(BirthDeathEvent.SelectAnimal("44444444-4444-4444-4444-444444444444"))
        advanceUntilIdle()
    }
}


// --- Fakes -----------------------------------------------------------------------------------

/** Captures the enqueued birth/death requests so a test can assert the exact wire payload. */
private class RecordingCountsSyncRepository : SyncRepository {
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
}

/**
 * Mirrors Room: the destinations catalog emits one park with one shed, and a tag lookup resolves to
 * one animal carrying its own row_version — the exact contract the death write round-trips.
 *
 * The breed vocabulary models the real cache/refresh split: [observeBirthBreeds] reads a Room-backed
 * flow, and only [refreshBirthBreeds] (backed by the operator `/app/counts/breeds` endpoint) warms
 * it. When [breedCacheColdUntilRefresh] is set, the vocabulary starts EMPTY (the dropdown would be
 * disabled) and appears only once the ViewModel fetches it — modelling a field operator with no
 * Counts (CountsRead) access whose breed cache was never warmed.
 */
private class FakeBirthDeathCountsRepository(
    private val breedCacheColdUntilRefresh: Boolean = false,
) : CountsRepository {
    private fun breedsWithBeetal() = CountsBreedsResponseDto(
        // The birth breed dropdown reuses the herd's OWN breed vocabulary; the fake supplies one.
        breeds = listOf(
            CountsBreakdownSeriesPointDto(key = "beetal", label = "Beetal", count = 12),
        ),
    )

    private val birthBreeds = MutableStateFlow(
        Resource(
            data = if (breedCacheColdUntilRefresh) CountsBreedsResponseDto() else breedsWithBeetal(),
        ),
    )

    var refreshBreedsCalls = 0
        private set

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

    override fun observeBirthBreeds(): Flow<Resource<CountsBreedsResponseDto>> = birthBreeds

    override suspend fun refreshBirthBreeds(): Result<Unit> {
        refreshBreedsCalls++
        // A real refresh writes the vocabulary into the cache the observe flow reads.
        birthBreeds.value = Resource(data = breedsWithBeetal())
        return Result.success(Unit)
    }

    override fun breakdownRows(query: CountsBreakdownQuery): Flow<PagingData<CountsBreakdownRowDto>> =
        flowOf(PagingData.empty<CountsBreakdownRowDto>()).map { it }

    override fun observeShiftingDestinations(): Flow<Resource<CountsShiftingDestinationsResponseDto>> =
        MutableStateFlow(
            Resource(
                data = CountsShiftingDestinationsResponseDto(
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
                        CountsDestinationParkDto(
                            parkId = "22222222-2222-2222-2222-222222222222",
                            name = "South Park",
                            sheds = listOf(
                                CountsDestinationShedDto(
                                    shedId = "55555555-5555-5555-5555-555555555555",
                                    name = "Shed B",
                                ),
                            ),
                        ),
                    ),
                ),
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

/**
 * The disease vocabulary, as small as the port allows. [catalog] is a MutableStateFlow so a test
 * can make the list arrive late, arrive empty, or change under a selection the operator has
 * already made -- the three ways this dropdown can be wrong in the field.
 */
private class FakeDeathCauseVocabulary : DeathCauseVocabulary {
    val catalog = MutableStateFlow<DeathCauseCatalogDto?>(
        DeathCauseCatalogDto(
            options = listOf(
                DeathCauseOptionDto(key = "MASTITIS", kind = "register_rule", label = "Mastitis"),
                DeathCauseOptionDto(key = "BLOAT", kind = "register_rule", label = "Bloat"),
                DeathCauseOptionDto(key = "FOOT_ROT", kind = "register_rule", label = "Foot rot"),
            ),
        ),
    )

    /** When set, [refreshDeathCauses] fails -- the offline phone. */
    var refreshError: Throwable? = null

    override fun observeDeathCauses(): Flow<DeathCauseCatalogDto?> = catalog

    override suspend fun refreshDeathCauses(): Result<Unit> =
        refreshError?.let { Result.failure(it) } ?: Result.success(Unit)
}

private class NoopAnalyticsPort : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class NoopTestCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}
