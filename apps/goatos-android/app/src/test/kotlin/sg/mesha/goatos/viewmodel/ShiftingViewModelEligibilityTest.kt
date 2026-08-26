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
import kotlinx.coroutines.test.TestScope
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
import sg.mesha.goatos.core.analytics.AnalyticsEvents
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
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownRowDto
import sg.mesha.goatos.core.network.dto.CountsBreedsResponseDto
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.CountsDestinationShedDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.GoatLocationPathDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.counts.SHIFTING_CATEGORY_BREEDING
import sg.mesha.goatos.feature.counts.SHIFTING_CATEGORY_DELIVERY
import sg.mesha.goatos.feature.counts.SHIFTING_CATEGORY_FLUSHING
import sg.mesha.goatos.feature.counts.SHIFTING_CATEGORY_GROWTH
import sg.mesha.goatos.feature.counts.SHIFTING_CATEGORY_HEALTH
import sg.mesha.goatos.feature.counts.SHIFTING_CATEGORY_SPACING
import sg.mesha.goatos.feature.counts.SHIFTING_STAGE_MODE_DESTINATION
import sg.mesha.goatos.feature.counts.SHIFTING_STAGE_MODE_KEEP_CURRENT
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.feature.counts.ShiftingEvent
import sg.mesha.goatos.rfid.FakeScanSource
import sg.mesha.goatos.rfid.ScanSource

@OptIn(ExperimentalCoroutinesApi::class)
class ShiftingViewModelEligibilityTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `dead RFID is rejected by lookup before it can be selected`() = runTest(dispatcher) {
        val vm = newViewModel(listOf(animal(lifecycle = "dead")))
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()

        assertTrue(vm.state.value.animalMatches.isEmpty())
        assertTrue(vm.state.value.selectedAnimals.isEmpty())
        assertFalse(vm.state.value.canSubmit)
        assertEquals("This animal is no longer active and cannot be shifted.", vm.state.value.animalLookupMessage)
    }

    @Test
    fun `animal lookup results are deduped by goat id before they reach lazy list keys`() = runTest(dispatcher) {
        val vm = newViewModel(
            listOf(
                animal(lifecycle = "alive", shedName = "Castro 1"),
                animal(lifecycle = "alive", shedName = "Castro 1"),
            ),
        )
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()

        assertEquals(listOf(GOAT_ID), vm.state.value.animalMatches.map { it.goatId })
        assertNull(vm.state.value.animalLookupMessage)
    }

    @Test
    fun `selecting a live goat locks destination farm to its current farm`() = runTest(dispatcher) {
        val vm = newViewModel(listOf(animal(lifecycle = "alive")))
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))

        assertEquals(CBE_PARK_ID, vm.state.value.destinationParkId)
        assertEquals(listOf(CBE_SHED_ID), vm.state.value.shedsForSelectedPark.map { it.shedId })

        // Even an injected/stale UI event cannot switch this goat to another farm.
        vm.onEvent(ShiftingEvent.SelectDestinationPark(CPT_PARK_ID))
        assertEquals(CBE_PARK_ID, vm.state.value.destinationParkId)
    }

    @Test
    fun `successful shifting submission clears the draft and requests return to Actions`() = runTest(dispatcher) {
        val sync = NoopShiftingSyncRepository()
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), sync)
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID))
        vm.onEvent(ShiftingEvent.Submit)
        advanceUntilIdle()

        sync.succeed("shift-outbox")
        advanceUntilIdle()

        assertEquals("", vm.state.value.animalQuery)
        assertTrue(vm.state.value.selectedAnimals.isEmpty())
        // A successful child form returns to Actions; it must not leave its success banner on the
        // now-empty form, which is the current broken behaviour.
        assertNull(vm.state.value.lastRecordedMessage)
        assertTrue(vm.state.value.returnToActions)
        vm.onEvent(ShiftingEvent.NavigationHandled)
        assertFalse(vm.state.value.returnToActions)
    }

    @Test
    fun `invalid shifting submit attempt is tracked before enqueue`() = runTest(dispatcher) {
        val sync = NoopShiftingSyncRepository()
        val analytics = NoopShiftingAnalytics()
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), sync, analytics = analytics)
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.Submit)

        assertNull(sync.lastShiftingRequest)
        assertEquals(AnalyticsEvents.SUBMIT_BLOCKED, analytics.events.single().first)
        assertEquals("shifting", analytics.events.single().second[AnalyticsEvents.Params.KIND])
        assertEquals(
            "Find and select the animals that moved.",
            analytics.events.single().second[AnalyticsEvents.Params.REASON],
        )
    }

    @Test
    fun `confirmed shifting submit records breadcrumbs and shows queued immediately`() = runTest(dispatcher) {
        val sync = NoopShiftingSyncRepository()
        val analytics = NoopShiftingAnalytics()
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), sync, analytics = analytics)
        advanceUntilIdle()

        selectAnimalAndPen(vm)
        vm.onEvent(ShiftingEvent.RequestSubmitConfirmation)
        assertTrue(vm.state.value.showSubmitConfirmation)

        vm.onEvent(ShiftingEvent.Submit)
        advanceUntilIdle()

        assertEquals(CountsWriteStatus.QUEUED, vm.state.value.result.status)
        assertEquals("Saved on this phone. It will sync automatically.", vm.state.value.result.message)
        assertEquals(
            listOf(
                AnalyticsEvents.COUNTS_SHIFTING_CONFIRM_OPENED,
                AnalyticsEvents.COUNTS_SHIFTING_SUBMIT_ATTEMPTED,
                AnalyticsEvents.COUNTS_SHIFTING_SUBMITTED,
            ),
            analytics.events.map { it.first },
        )
        assertEquals("1", analytics.events.last().second[AnalyticsEvents.Params.ANIMAL_COUNT])
    }

    /**
     * The operator picks an animal and a destination — nothing else. Submit must unlock on those
     * two facts alone.
     *
     * Before the 2026-08-03 rule the form also demanded a management-stage choice, so `canSubmit`
     * stayed false after selecting a shed and the operator had to answer a third dropdown. This
     * asserts that gate is gone, which is the whole point of the change from the operator's side.
     */
    @Test
    fun `animal plus destination is enough to submit`() = runTest(dispatcher) {
        val vm = newViewModel(listOf(animal(lifecycle = "alive")))
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        assertFalse(vm.state.value.canSubmit)

        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID))
        assertTrue(vm.state.value.canSubmit)
    }

    @Test
    fun `every rendered shifting category is selectable and travels on submit`() = runTest(dispatcher) {
        val categories = listOf(
            SHIFTING_CATEGORY_GROWTH,
            SHIFTING_CATEGORY_HEALTH,
            SHIFTING_CATEGORY_BREEDING,
            SHIFTING_CATEGORY_DELIVERY,
            SHIFTING_CATEGORY_SPACING,
            SHIFTING_CATEGORY_FLUSHING,
        )

        categories.forEach { category ->
            val sync = NoopShiftingSyncRepository()
            val vm = newViewModel(listOf(animal(lifecycle = "alive")), sync)
            advanceUntilIdle()

            selectAnimalAndPen(vm)
            vm.onEvent(ShiftingEvent.SelectCategory(category))
            assertEquals(category, vm.state.value.category)

            vm.onEvent(ShiftingEvent.Submit)
            advanceUntilIdle()

            assertEquals(category, sync.lastShiftingRequest?.category)
        }
    }

    // -------------------------------------------------------------------------------------------
    // Partition carrying — the maintainer-reported defect: FROM must show the animal's PARTITION,
    // and MOVE TO must be able to target a different partition of the SAME shed, or any partition
    // of any other shed. Source: source->UI->submit payload. Destination: catalog->UI->submit.
    // -------------------------------------------------------------------------------------------

    @Test
    fun `selecting an animal in a partitioned shed carries its partition into the UI`() = runTest(dispatcher) {
        val vm = newViewModel(listOf(animal(lifecycle = "alive", partitionLabel = "1")))
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))

        // The DTO -> UI mapping preserves the animal's current partition rather than dropping it,
        // which is the root cause of the reported "Coimbatore · Yashoda" FROM chip that could not
        // say which partition the animal was actually in.
        assertEquals("1", vm.state.value.selectedAnimals.single().partitionLabel)
    }

    @Test
    fun `a same-shed cross-partition move is expressible and carries the destination partition on submit`() =
        runTest(dispatcher) {
            val sync = NoopShiftingSyncRepository()
            val destinations = listOf(
                CountsDestinationParkDto(
                    parkId = CBE_PARK_ID,
                    name = "Coimbatore",
                    sheds = listOf(
                        CountsDestinationShedDto(shedId = YASHODA_SHED_ID, name = "Yashoda", partitionLabel = "1"),
                        CountsDestinationShedDto(shedId = YASHODA_SHED_ID, name = "Yashoda", partitionLabel = "2"),
                        CountsDestinationShedDto(shedId = YASHODA_SHED_ID, name = "Yashoda", partitionLabel = "3"),
                    ),
                ),
            )
            val vm = newViewModel(
                listOf(animal(lifecycle = "alive", shedId = YASHODA_SHED_ID, shedName = "Yashoda", partitionLabel = "1")),
                sync,
                destinations,
            )
            advanceUntilIdle()
            vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
            vm.onEvent(ShiftingEvent.LookupAnimals)
            advanceUntilIdle()
            vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))

            // Three distinct dropdown entries exist for the ONE shed_id, one per partition — the
            // exact shape that used to be an unexpressible "bare Shed dropdown".
            assertEquals(3, vm.state.value.shedsForSelectedPark.size)

            vm.onEvent(ShiftingEvent.SelectDestinationShed(YASHODA_SHED_ID, "2"))
            assertEquals("2", vm.state.value.destinationPartitionLabel)
            assertTrue(vm.state.value.canSubmit)

            vm.onEvent(ShiftingEvent.Submit)
            advanceUntilIdle()
            sync.succeed("shift-outbox")
            advanceUntilIdle()

            val sent = sync.lastShiftingRequest
            assertEquals(YASHODA_SHED_ID, sent?.destinationShedId)
            assertEquals("2", sent?.destinationPartitionLabel)
        }

    @Test
    fun `a non-partitioned shed offers exactly one destination entry with no partition label`() = runTest(dispatcher) {
        val destinations = listOf(
            CountsDestinationParkDto(
                parkId = CBE_PARK_ID,
                name = "Coimbatore",
                sheds = listOf(
                    CountsDestinationShedDto(shedId = CBE_SHED_ID, name = "Old Yashoda", partitionLabel = null),
                ),
            ),
        )
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), destinations = destinations)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))

        // Exactly one entry, and it carries no partition — the operator is never forced to pick a
        // fake "whole" partition for a shed that does not have any.
        assertEquals(1, vm.state.value.shedsForSelectedPark.size)
        assertNull(vm.state.value.shedsForSelectedPark.single().partitionLabel)

        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID, null))
        assertTrue(vm.state.value.canSubmit)
        assertNull(vm.state.value.destinationPartitionLabel)
    }

    /**
     * The tag toggle's happy path (maintainer decision 2026-08-15): the form defaults to the
     * destination pen's tag, the operator may decline it, and whichever they choose is what the
     * write carries.
     */
    @Test
    fun `the tag toggle defaults to the pen's tag and sends the operator's choice`() = runTest(dispatcher) {
        val sync = NoopShiftingSyncRepository()
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), sync, destinations = taggedPen())
        advanceUntilIdle()
        selectAnimalAndPen(vm)

        // Default: the pre-toggle behaviour, so an operator who ignores the control gets today's.
        assertEquals(SHIFTING_STAGE_MODE_DESTINATION, vm.state.value.stageMode)
        assertTrue(vm.state.value.canUseDestinationStage)
        assertEquals("Mother", vm.state.value.destinationStageLabel)
        assertNull(vm.state.value.destinationStageReason)

        vm.onEvent(ShiftingEvent.SelectStageMode(SHIFTING_STAGE_MODE_KEEP_CURRENT))
        assertEquals(SHIFTING_STAGE_MODE_KEEP_CURRENT, vm.state.value.stageMode)

        vm.onEvent(ShiftingEvent.Submit)
        advanceUntilIdle()
        // The mode is sent EXPLICITLY, never left to a server default the form cannot see.
        assertEquals(SHIFTING_STAGE_MODE_KEEP_CURRENT, sync.lastShiftingRequest?.stageMode)
    }

    /**
     * A pen that cannot supply a tag: the option is not selectable, the backend's reason is carried
     * verbatim for the greyed-out control, and the mode is forced to keep-current so the form can
     * never claim a tag the raise would not apply.
     */
    @Test
    fun `a pen with no usable tag forces keep-current and carries the backend's reason`() = runTest(dispatcher) {
        val reason = "This destination holds a mix of tags"
        val sync = NoopShiftingSyncRepository()
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), sync, destinations = untaggedPen(reason))
        advanceUntilIdle()
        selectAnimalAndPen(vm)

        assertFalse(vm.state.value.canUseDestinationStage)
        assertEquals(SHIFTING_STAGE_MODE_KEEP_CURRENT, vm.state.value.stageMode)
        // Rendered verbatim: the phone never composes this from a blank tag.
        assertEquals(reason, vm.state.value.destinationStageReason)
        assertNull(vm.state.value.destinationStageLabel)

        // Asking for the pen's tag anyway is IGNORED rather than accepted-and-downgraded.
        vm.onEvent(ShiftingEvent.SelectStageMode(SHIFTING_STAGE_MODE_DESTINATION))
        assertEquals(SHIFTING_STAGE_MODE_KEEP_CURRENT, vm.state.value.stageMode)

        vm.onEvent(ShiftingEvent.Submit)
        advanceUntilIdle()
        assertEquals(SHIFTING_STAGE_MODE_KEEP_CURRENT, sync.lastShiftingRequest?.stageMode)
    }

    /**
     * Switching from a tagged pen to an untagged one must snap the toggle back. Without the
     * re-resolution the form would keep showing "use destination tag" for a pen that has none, the raise
     * would silently fall back to keep-current, and the operator would never be told.
     */
    @Test
    fun `changing to a pen with no tag snaps the toggle back to keep-current`() = runTest(dispatcher) {
        val destinations = listOf(
            CountsDestinationParkDto(
                parkId = CBE_PARK_ID,
                name = "Coimbatore",
                sheds = listOf(
                    CountsDestinationShedDto(
                        shedId = CBE_SHED_ID, name = "Castro", partitionLabel = "1",
                        operationalLocationDisplay = "Castro - 1", destinationStage = "Mother",
                    ),
                    CountsDestinationShedDto(
                        shedId = CBE_SHED_ID, name = "Castro", partitionLabel = "2",
                        operationalLocationDisplay = "Castro - 2",
                        destinationStageReason = "This destination has no tag set",
                    ),
                ),
            ),
        )
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), destinations = destinations)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))

        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID, "1"))
        assertEquals(SHIFTING_STAGE_MODE_DESTINATION, vm.state.value.stageMode)

        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID, "2"))
        assertEquals(SHIFTING_STAGE_MODE_KEEP_CURRENT, vm.state.value.stageMode)
        assertEquals("This destination has no tag set", vm.state.value.destinationStageReason)
    }

    /** The lookup is async, so the scope must idle before the match can be selected. */
    private fun TestScope.selectAnimalAndPen(vm: ShiftingViewModel) {
        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID, null))
    }

    private fun taggedPen() = listOf(
        CountsDestinationParkDto(
            parkId = CBE_PARK_ID,
            name = "Coimbatore",
            sheds = listOf(
                CountsDestinationShedDto(
                    shedId = CBE_SHED_ID, name = "Yashoda", partitionLabel = null,
                    operationalLocationDisplay = "Yashoda", destinationStage = "Mother",
                ),
            ),
        ),
    )

    private fun untaggedPen(reason: String) = listOf(
        CountsDestinationParkDto(
            parkId = CBE_PARK_ID,
            name = "Coimbatore",
            sheds = listOf(
                CountsDestinationShedDto(
                    shedId = CBE_SHED_ID, name = "Yashoda", partitionLabel = null,
                    operationalLocationDisplay = "Yashoda", destinationStageReason = reason,
                ),
            ),
        ),
    )

    private fun newViewModel(
        matches: List<GoatSearchItemDto>,
        syncRepository: NoopShiftingSyncRepository = NoopShiftingSyncRepository(),
        destinations: List<CountsDestinationParkDto>? = null,
        scanSource: ScanSource = FakeScanSource(),
        analytics: NoopShiftingAnalytics = NoopShiftingAnalytics(),
    ) = ShiftingViewModel(
        syncRepository = syncRepository,
        countsRepository = FakeShiftingCountsRepository(matches, destinations),
        analytics = analytics,
        crashReporter = NoopShiftingCrashReporter(),
        scanSource = scanSource,
        savedStateHandle = SavedStateHandle(),
    )

    // -----------------------------------------------------------------------
    // Bulk shifting (maintainer decision 2026-08-21): scanning APPENDS.
    // -----------------------------------------------------------------------

    @Test
    fun `selecting several animals builds one movement carrying every goat id`() = runTest(dispatcher) {
        val first = animal("alive")
        val second = animal("alive").copy(goatId = SECOND_GOAT_ID, displayId = "G-000326")
        val sync = NoopShiftingSyncRepository()
        val vm = newViewModel(listOf(first, second), sync)
        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(ShiftingEvent.SelectAnimal(SECOND_GOAT_ID))
        // Re-scanning an animal already in the basket must not duplicate it: on a crowded pen the
        // same tag really does get read twice, and a duplicate goat id is a movement of one animal
        // counted as two.
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        advanceUntilIdle()

        assertEquals(2, vm.state.value.selectedAnimals.size)
        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID))
        vm.onEvent(ShiftingEvent.RequestSubmitConfirmation)
        advanceUntilIdle()
        // Nothing is recorded by opening the confirmation: the read-back is a gate, not a submit.
        assertTrue(vm.state.value.showSubmitConfirmation)
        assertNull(sync.lastShiftingRequest)

        vm.onEvent(ShiftingEvent.Submit)
        advanceUntilIdle()
        assertEquals(listOf(GOAT_ID, SECOND_GOAT_ID), sync.lastShiftingRequest?.goatIds)
    }

    @Test
    fun `removing an animal drops only that one from the movement`() = runTest(dispatcher) {
        val first = animal("alive")
        val second = animal("alive").copy(goatId = SECOND_GOAT_ID, displayId = "G-000326")
        val sync = NoopShiftingSyncRepository()
        val vm = newViewModel(listOf(first, second), sync)
        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(ShiftingEvent.SelectAnimal(SECOND_GOAT_ID))
        vm.onEvent(ShiftingEvent.RemoveAnimal(GOAT_ID))
        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID))
        vm.onEvent(ShiftingEvent.Submit)
        advanceUntilIdle()

        assertEquals(listOf(SECOND_GOAT_ID), sync.lastShiftingRequest?.goatIds)
    }

    @Test
    fun `an animal on another farm is refused rather than joining the movement`() = runTest(dispatcher) {
        val here = animal("alive")
        val elsewhere = animal("alive").copy(
            goatId = SECOND_GOAT_ID,
            locationPath = GoatLocationPathDto(
                operationalLocationDisplay = "Channapatna / Gandhi 1",
                parkId = CPT_PARK_ID,
                parkName = "Channapatna",
                shedId = YASHODA_SHED_ID,
                shedName = "Gandhi 1",
                partitionLabel = null,
            ),
        )
        val vm = newViewModel(listOf(here, elsewhere))
        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(ShiftingEvent.SelectAnimal(SECOND_GOAT_ID))
        advanceUntilIdle()

        // Goats never move between farms, so the second animal never joins the basket, and the
        // operator is told why in farm words.
        assertEquals(listOf(GOAT_ID), vm.state.value.selectedAnimals.map { it.goatId })
        assertEquals(
            "That animal is on another farm. A movement can only carry animals from one farm.",
            vm.state.value.animalLookupMessage,
        )
    }

    @Test
    fun `a scanned tag lands in the lookup field and releases the reader`() = runTest(dispatcher) {
        val scanSource = FakeScanSource()
        val vm = newViewModel(listOf(animal("alive")), scanSource = scanSource)
        vm.onEvent(ShiftingEvent.ToggleRfidScan)
        advanceUntilIdle()
        assertTrue(scanSource.isStarted)

        scanSource.emit("CBE-ASSUMED-RFID-00001")
        advanceUntilIdle()

        assertEquals("CBE-ASSUMED-RFID-00001", vm.state.value.animalQuery)
        // The reader is app-wide: leaving it enabled would swallow the comment field's typing.
        assertFalse(scanSource.isStarted)
        assertFalse(vm.state.value.scanningAnimalTag)
        // Scanning an animal means wanting to find it, so the lookup ran without a second tap.
        assertEquals(listOf(GOAT_ID), vm.state.value.animalMatches.map { it.goatId })
    }

    private fun animal(
        lifecycle: String,
        shedId: String = CBE_SHED_ID,
        shedName: String = "Castro 1",
        partitionLabel: String? = null,
    ) = GoatSearchItemDto(
        goatId = GOAT_ID,
        displayId = "G-000325",
        animalIdentifier1 = "CBE-ASSUMED-RFID-00001",
        lifecycleStatus = lifecycle,
        locationPath = GoatLocationPathDto(
            operationalLocationDisplay = "Coimbatore / $shedName",
            parkId = CBE_PARK_ID,
            parkName = "Coimbatore",
            shedId = shedId,
            shedName = shedName,
            partitionLabel = partitionLabel,
        ),
    )

    private companion object {
        const val GOAT_ID = "d8337607-6e21-41c9-a703-a7b73ae4e545"
        const val SECOND_GOAT_ID = "f1a2b3c4-6e21-41c9-a703-a7b73ae4e546"
        const val CBE_PARK_ID = "00000000-0000-4000-8000-000000003001"
        const val CBE_SHED_ID = "43071c6e-3b00-47a9-860c-1bbacb570575"
        const val CPT_PARK_ID = "00000000-0000-4000-8000-000000003002"
        const val YASHODA_SHED_ID = "63071c6e-3b00-47a9-860c-1bbacb570576"
    }
}

private class FakeShiftingCountsRepository(
    private val matches: List<GoatSearchItemDto>,
    private val destinations: List<CountsDestinationParkDto>? = null,
) : CountsRepository {
    override fun observeHerdSummary(lifecycleStatus: String?, parkId: String?, breed: String?, sex: String?): Flow<Resource<HerdRegisterSummaryResponseDto>> =
        flowOf(Resource(data = HerdRegisterSummaryResponseDto()))
    override suspend fun refreshHerdSummary(lifecycleStatus: String?, parkId: String?, breed: String?, sex: String?): Result<Unit> = Result.success(Unit)
    override fun observeBreakdownTotals(query: CountsBreakdownQuery): Flow<Resource<CountsBreakdownResponseDto>> =
        flowOf(Resource(data = CountsBreakdownResponseDto()))
    override fun observeBirthBreeds(): Flow<Resource<CountsBreedsResponseDto>> =
        flowOf(Resource(data = CountsBreedsResponseDto()))
    override suspend fun refreshBirthBreeds(): Result<Unit> = Result.success(Unit)
    override fun breakdownRows(query: CountsBreakdownQuery): Flow<PagingData<CountsBreakdownRowDto>> = flowOf(PagingData.empty())
    override fun observeShiftingDestinations(): Flow<Resource<CountsShiftingDestinationsResponseDto>> =
        MutableStateFlow(
            Resource(
                data = CountsShiftingDestinationsResponseDto(
                    managementStages = listOf("K0", "K1", "Mother"),
                    parks = destinations ?: listOf(
                        CountsDestinationParkDto(
                            parkId = "00000000-0000-4000-8000-000000003001",
                            name = "Coimbatore",
                            sheds = listOf(CountsDestinationShedDto("43071c6e-3b00-47a9-860c-1bbacb570575", "Castro 2")),
                        ),
                        CountsDestinationParkDto(
                            parkId = "00000000-0000-4000-8000-000000003002",
                            name = "Channapatna",
                            sheds = listOf(CountsDestinationShedDto("53071c6e-3b00-47a9-860c-1bbacb570575", "Castro 2")),
                        ),
                    ),
                ),
            ),
        )
    override suspend fun refreshShiftingDestinations(): Result<Unit> = Result.success(Unit)
    override suspend fun lookupAnimals(query: String, parkId: String?, shedId: String?): Result<List<GoatSearchItemDto>> = Result.success(matches)
}

private class NoopShiftingSyncRepository : SyncRepository {
    var lastShiftingRequest: CountsShiftingEventRequestDto? = null
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    fun succeed(itemId: String) {
        val now = System.currentTimeMillis()
        status.value = SyncStatus(
            online = true,
            pendingCount = 0,
            inFlightCount = 0,
            failedCount = 0,
            deadLetterCount = 0,
            lastSyncAt = now,
            items = listOf(
                SyncQueueItem(
                    id = itemId,
                    idempotencyKey = "test-idempotency-key",
                    opType = "COUNTS_SHIFTING",
                    groupKey = CBE_SHED_ID_FOR_SYNC,
                    status = SyncItemStatus.SUCCEEDED,
                    attemptCount = 1,
                    maxAttempts = 5,
                    conflict = false,
                    createdAt = now,
                    updatedAt = now,
                    lastError = null,
                ),
            ),
        )
    }
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
    override suspend fun enqueueCountsShifting(groupKey: String, idempotencyKey: String, request: CountsShiftingEventRequestDto): AppResult<String> {
        lastShiftingRequest = request
        return AppResult.Ok("shift-outbox")
    }

    private companion object {
        const val CBE_SHED_ID_FOR_SYNC = "43071c6e-3b00-47a9-860c-1bbacb570575"
    }
}

private class NoopShiftingAnalytics : AnalyticsPort {
    val events = mutableListOf<Pair<String, Map<String, String>>>()
    override fun track(event: String, props: Map<String, String>) {
        events += event to props
    }
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class NoopShiftingCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}
