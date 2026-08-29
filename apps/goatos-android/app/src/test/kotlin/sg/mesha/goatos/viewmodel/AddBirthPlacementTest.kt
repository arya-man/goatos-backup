package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
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
import sg.mesha.goatos.core.network.dto.CountsBirthPlacementDto
import sg.mesha.goatos.core.network.dto.CountsBirthPlacementPenDto
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.CountsDestinationShedDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.feature.counts.AddBirthEvent
import sg.mesha.goatos.rfid.FakeScanSource

/**
 * A newborn is placed in its park's KID PEN (maintainer decision 2026-08-20).
 *
 * The form renders the backend's answer; it never decides which pens are kid pens. These tests
 * therefore drive the view-model with real `birth_placement` payloads and assert on what the
 * operator is offered and what the form would send.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AddBirthPlacementTest {
    private val dispatcher = StandardTestDispatcher()

    private lateinit var syncRepository: RecordingAddSyncRepository
    private lateinit var countsRepository: FakeAddCountsRepository
    private lateinit var scanSource: FakeScanSource

    private val parkId = "11111111-1111-1111-1111-111111111111"
    private val kidShedId = "22222222-2222-2222-2222-222222222222"
    private val buckShedId = "33333333-3333-3333-3333-333333333333"

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
        syncRepository = RecordingAddSyncRepository()
        countsRepository = FakeAddCountsRepository()
        scanSource = FakeScanSource()
    }

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun newViewModel() = AddBirthViewModel(
        syncRepository,
        countsRepository,
        scanSource,
        NoopAddAnalyticsPort(),
        NoopAddCrashReporter(),
        SavedStateHandle(),
    )

    /** A park whose sheds include a non-kid pen, plus whichever kid pens the placement declares. */
    private fun catalogWith(placement: CountsBirthPlacementDto) =
        CountsShiftingDestinationsResponseDto(
            parks = listOf(
                CountsDestinationParkDto(
                    parkId = parkId,
                    name = "Coimbatore",
                    sheds = listOf(
                        CountsDestinationShedDto(
                            shedId = kidShedId, name = "Yashoda",
                            partitionLabel = "5", operationalLocationDisplay = "Yashoda 5",
                        ),
                        CountsDestinationShedDto(
                            shedId = buckShedId, name = "Castro",
                            partitionLabel = "1", operationalLocationDisplay = "Castro 1",
                        ),
                    ),
                    birthPlacement = placement,
                ),
            ),
        )

    private fun pen(shedID: String, label: String, display: String) = CountsBirthPlacementPenDto(
        shedId = shedID, shedName = display.substringBeforeLast(" "),
        partitionLabel = label, operationalLocationDisplay = display,
    )

    // Exactly one kid pen: the operator picks the PARK and the kid is already placed. No shed
    // choice is offered, because there is only one right answer and asking invites the wrong one.
    @Test
    fun `a park with one kid pen places the newborn without asking`() = runTest(dispatcher) {
        countsRepository.destinations = catalogWith(
            CountsBirthPlacementDto(
                mode = CountsBirthPlacementDto.MODE_AUTOMATIC,
                notice = "Kids born in this park go to Yashoda 5.",
                pens = listOf(pen(kidShedId, "5", "Yashoda 5")),
            ),
        )
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(AddBirthEvent.SelectPark(parkId))

        val state = vm.state.value
        assertEquals("the kid pen is selected by choosing the park", kidShedId, state.shedId)
        assertEquals("the pen half must survive with the shed half", "5", state.partitionLabel)
        assertTrue("automatic placement shows the pen read-only", state.birthPlacement.isAutomatic)
        assertEquals(
            "the backend's own label is rendered, never a locally joined one",
            "Yashoda 5",
            state.birthPlacement.automaticPen?.displayLabel,
        )
    }

    // Several kid pens: the operator chooses, but ONLY among kid pens. The Buck pen is in the
    // park's shed list and must not be offered.
    @Test
    fun `a park with several kid pens offers only those pens`() = runTest(dispatcher) {
        countsRepository.destinations = catalogWith(
            CountsBirthPlacementDto(
                mode = CountsBirthPlacementDto.MODE_CHOOSE,
                notice = "Choose which kid pen this kid goes into.",
                pens = listOf(pen(kidShedId, "5", "Yashoda 5"), pen(kidShedId, "6", "Yashoda 6")),
            ),
        )
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(AddBirthEvent.SelectPark(parkId))
        val state = vm.state.value

        assertTrue("several kid pens means the operator chooses", state.birthPlacement.isChoice)
        assertEquals("no pen is pre-picked when there is a real choice", "", state.shedId)
        assertEquals("only the kid pens are offered", 2, state.placementOptions.size)
        assertFalse(
            "the Buck pen is in the park's shed list but must never be a newborn option",
            state.placementOptions.any { it.shedId == buckShedId },
        )

        vm.onEvent(AddBirthEvent.SelectShed("$kidShedId|6"))
        assertEquals("the chosen pen's partition is kept exactly as the backend spelled it",
            "6", vm.state.value.partitionLabel)
    }

    // No kid pen: the operator picks from the FULL cascade so the birth is never lost over missing
    // setup, and the kid's care steps carry Record shed.
    @Test
    fun `a park with no kid pen falls back to the full shed cascade`() = runTest(dispatcher) {
        countsRepository.destinations = catalogWith(
            CountsBirthPlacementDto(
                mode = CountsBirthPlacementDto.MODE_RECORD_LATER,
                notice = "This park has no kid pen set yet.",
                pens = emptyList(),
            ),
        )
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(AddBirthEvent.SelectPark(parkId))
        val state = vm.state.value

        assertTrue(state.birthPlacement.isRecordLater)
        assertEquals("every active pen of the park stays selectable", 2, state.placementOptions.size)
        assertTrue(
            "including pens that are not kid pens — the operator records where the kid actually is",
            state.placementOptions.any { it.shedId == buckShedId },
        )
    }

    // A response from a backend that predates this contract carries no birth_placement at all. It
    // must behave exactly as the form did before — the full cascade — never an empty picker.
    @Test
    fun `a response without a placement contract keeps the previous behaviour`() = runTest(dispatcher) {
        countsRepository.destinations = catalogWith(CountsBirthPlacementDto())
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(AddBirthEvent.SelectPark(parkId))
        val state = vm.state.value

        assertTrue("the safe default is the pre-existing behaviour", state.birthPlacement.isRecordLater)
        assertEquals(2, state.placementOptions.size)
        assertNull("nothing is auto-selected without a contract", state.birthPlacement.automaticPen)
    }
}
