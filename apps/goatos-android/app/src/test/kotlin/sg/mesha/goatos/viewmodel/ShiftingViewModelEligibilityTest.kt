package sg.mesha.goatos.viewmodel

import android.view.KeyEvent
import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
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
import sg.mesha.goatos.feature.counts.SHIFTING_STAGE_MODE_DESTINATION
import sg.mesha.goatos.feature.counts.SHIFTING_STAGE_MODE_KEEP_CURRENT
import sg.mesha.goatos.feature.counts.ShiftingEvent
import sg.mesha.goatos.rfid.RfidRead
import sg.mesha.goatos.rfid.RfidReaderDevice
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus

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

    // -------------------------------------------------------------------------------------------
    // The basket (maintainer decisions 2026-08-18 and 2026-08-20): one shifting carries MULTIPLE
    // animals from ONE FARM — any of its sheds/pens; the destination is the shared thing — added
    // one by one via search/scan + tap, each removable — and submits one movement whose goat_ids
    // is the whole basket.
    // -------------------------------------------------------------------------------------------

    @Test
    fun `multiple animals from one pen accumulate and submit as one movement`() = runTest(dispatcher) {
        val sync = NoopShiftingSyncRepository()
        val vm = newViewModel(
            listOf(
                animal(lifecycle = "alive"),
                animal(lifecycle = "alive", goatId = GOAT_ID_B, tag = "CBE-ASSUMED-RFID-00002"),
            ),
            sync,
        )
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))

        // A successful add clears the query and match list so the next scan starts clean.
        assertEquals(1, vm.state.value.selectedAnimals.size)
        assertEquals("", vm.state.value.animalQuery)
        assertTrue(vm.state.value.animalMatches.isEmpty())

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID_B))
        assertEquals(2, vm.state.value.selectedAnimals.size)

        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID))
        assertTrue(vm.state.value.canSubmit)
        vm.onEvent(ShiftingEvent.Submit)
        advanceUntilIdle()

        // ONE movement carrying the whole basket — never one write per animal.
        assertEquals(listOf(GOAT_ID, GOAT_ID_B), sync.lastShiftingRequest?.goatIds)
    }

    @Test
    fun `an animal from a different shed of the same farm joins the basket`() = runTest(dispatcher) {
        // Maintainer decision 2026-08-20 (relaxing the 2026-08-18 one-pen rule): one shifting may
        // gather animals from several sheds/pens — the DESTINATION is the shared thing.
        val vm = newViewModel(
            listOf(
                animal(lifecycle = "alive"),
                animal(
                    lifecycle = "alive", goatId = GOAT_ID_B, tag = "CBE-ASSUMED-RFID-00002",
                    shedId = YASHODA_SHED_ID, shedName = "Yashoda",
                ),
            ),
        )
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID_B))

        assertEquals(listOf(GOAT_ID, GOAT_ID_B), vm.state.value.selectedAnimals.map { it.goatId })
        assertNull(vm.state.value.animalLookupMessage)
    }

    @Test
    fun `an animal on a different farm is refused and the basket is unchanged`() = runTest(dispatcher) {
        // Goats never move between parks (movement lock 2026-07-19), so a cross-farm add is
        // refused with the fix named — mirroring the backend's mixed_source_parks rejection —
        // rather than queueing a write that is certain to fail in the outbox.
        val vm = newViewModel(
            listOf(
                animal(lifecycle = "alive"),
                animal(
                    lifecycle = "alive", goatId = GOAT_ID_B, tag = "CBE-ASSUMED-RFID-00002",
                    parkId = CPT_PARK_ID,
                ),
            ),
        )
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID_B))

        assertEquals(listOf(GOAT_ID), vm.state.value.selectedAnimals.map { it.goatId })
        assertEquals(
            "This animal is on a different farm. All animals in one shifting must be on the same farm — submit this one, then raise another shifting for the other farm.",
            vm.state.value.animalLookupMessage,
        )
    }

    @Test
    fun `a same-shed different-partition animal joins the basket`() = runTest(dispatcher) {
        // 2026-08-20: pens no longer gate the basket — Castro 1 and Castro 2 animals move together.
        val vm = newViewModel(
            listOf(
                animal(lifecycle = "alive", partitionLabel = "1"),
                animal(
                    lifecycle = "alive", goatId = GOAT_ID_B, tag = "CBE-ASSUMED-RFID-00002",
                    partitionLabel = "2",
                ),
            ),
        )
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID_B))

        assertEquals(listOf(GOAT_ID, GOAT_ID_B), vm.state.value.selectedAnimals.map { it.goatId })
    }

    @Test
    fun `removing the last animal clears the pinned farm and destination`() = runTest(dispatcher) {
        val vm = newViewModel(listOf(animal(lifecycle = "alive")))
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID))
        assertEquals(CBE_PARK_ID, vm.state.value.destinationParkId)

        vm.onEvent(ShiftingEvent.RemoveAnimal(GOAT_ID))

        assertTrue(vm.state.value.selectedAnimals.isEmpty())
        // The next group may stand somewhere else entirely, so nothing stays pinned.
        assertEquals("", vm.state.value.destinationParkId)
        assertEquals("", vm.state.value.destinationShedId)
        assertFalse(vm.state.value.canSubmit)
    }

    // -------------------------------------------------------------------------------------------
    // Duplicate-row crash regression (2026-08-19) + RFID gun scan auto-add.
    // -------------------------------------------------------------------------------------------

    /**
     * Regression for the field crash of 2026-08-19: the backend's old join shape returned a goat
     * once PER MATCHED IDENTIFIER (172 STG animals carry a secondary RFID and a legacy tag under
     * one identifier type), and two match rows sharing one goat_id blew up the LazyColumn's
     * unique-key contract — `IllegalArgumentException: Key "match-<goat_id>" was already used` —
     * closing the app the moment "Find animal" returned. The server grain is fixed at the source;
     * this pins the client-side net so a duplicated server row can never crash a screen again.
     */
    @Test
    fun `duplicate lookup rows for one goat collapse to one match row`() = runTest(dispatcher) {
        val duplicated = animal(lifecycle = "alive")
        val vm = newViewModel(listOf(duplicated, duplicated.copy(animalIdentifier2 = "CBE-1880")))
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()

        assertEquals(listOf(GOAT_ID), vm.state.value.animalMatches.map { it.goatId })
    }

    @Test
    fun `a gun scan resolving to one eligible animal is added to the basket without a tap`() = runTest(dispatcher) {
        val reader = FakeShiftingRfidReader()
        val vm = newViewModel(
            listOf(
                animal(lifecycle = "alive"),
                animal(lifecycle = "alive", goatId = GOAT_ID_B, tag = "CBE-ASSUMED-RFID-00002"),
            ),
            reader = reader,
            filterByTag = true,
        )
        advanceUntilIdle()

        reader.scan("CBE-ASSUMED-RFID-00001")
        advanceUntilIdle()

        // Scan → auto-add: no tap, query cleared for the next scan, farm pinned from the animal.
        assertEquals(listOf(GOAT_ID), vm.state.value.selectedAnimals.map { it.goatId })
        assertEquals("", vm.state.value.animalQuery)
        assertTrue(vm.state.value.animalMatches.isEmpty())
        assertEquals(CBE_PARK_ID, vm.state.value.destinationParkId)

        reader.scan("CBE-ASSUMED-RFID-00002")
        advanceUntilIdle()
        assertEquals(listOf(GOAT_ID, GOAT_ID_B), vm.state.value.selectedAnimals.map { it.goatId })
    }

    @Test
    fun `a gun scan from a different shed of the same farm auto-adds`() = runTest(dispatcher) {
        val reader = FakeShiftingRfidReader()
        val vm = newViewModel(
            listOf(
                animal(lifecycle = "alive"),
                animal(
                    lifecycle = "alive", goatId = GOAT_ID_B, tag = "CBE-ASSUMED-RFID-00002",
                    shedId = YASHODA_SHED_ID, shedName = "Yashoda",
                ),
            ),
            reader = reader,
            filterByTag = true,
        )
        advanceUntilIdle()

        reader.scan("CBE-ASSUMED-RFID-00001")
        advanceUntilIdle()
        reader.scan("CBE-ASSUMED-RFID-00002")
        advanceUntilIdle()

        // 2026-08-20: sheds do not gate the basket; the scan rhythm walks pen to pen.
        assertEquals(listOf(GOAT_ID, GOAT_ID_B), vm.state.value.selectedAnimals.map { it.goatId })
    }

    @Test
    fun `a gun scan from a different farm is refused with the mixed-farm message`() = runTest(dispatcher) {
        val reader = FakeShiftingRfidReader()
        val vm = newViewModel(
            listOf(
                animal(lifecycle = "alive"),
                animal(
                    lifecycle = "alive", goatId = GOAT_ID_B, tag = "CBE-ASSUMED-RFID-00002",
                    parkId = CPT_PARK_ID,
                ),
            ),
            reader = reader,
            filterByTag = true,
        )
        advanceUntilIdle()

        reader.scan("CBE-ASSUMED-RFID-00001")
        advanceUntilIdle()
        reader.scan("CBE-ASSUMED-RFID-00002")
        advanceUntilIdle()

        // Same farm guard as the tap path — a scan cannot smuggle a second park into the basket.
        assertEquals(listOf(GOAT_ID), vm.state.value.selectedAnimals.map { it.goatId })
        assertEquals(
            "This animal is on a different farm. All animals in one shifting must be on the same farm — submit this one, then raise another shifting for the other farm.",
            vm.state.value.animalLookupMessage,
        )
    }

    @Test
    fun `scanning an already-added animal reports it and clears the query for the next scan`() = runTest(dispatcher) {
        val reader = FakeShiftingRfidReader()
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), reader = reader, filterByTag = true)
        advanceUntilIdle()

        reader.scan("CBE-ASSUMED-RFID-00001")
        advanceUntilIdle()
        reader.scan("CBE-ASSUMED-RFID-00001")
        advanceUntilIdle()

        assertEquals(listOf(GOAT_ID), vm.state.value.selectedAnimals.map { it.goatId })
        assertEquals("This animal is already in this shifting.", vm.state.value.animalLookupMessage)
        assertEquals("", vm.state.value.animalQuery)
    }

    @Test
    fun `a gun scan matching two different animals lists them for an explicit tap`() = runTest(dispatcher) {
        val reader = FakeShiftingRfidReader()
        // Two DIFFERENT goats behind one scanned string (a shared legacy tag): auto-picking either
        // would move an animal nobody chose, so both are listed and the basket stays empty.
        val vm = newViewModel(
            listOf(
                animal(lifecycle = "alive"),
                animal(lifecycle = "alive", goatId = GOAT_ID_B, tag = "CBE-ASSUMED-RFID-00001"),
            ),
            reader = reader,
            filterByTag = true,
        )
        advanceUntilIdle()

        reader.scan("CBE-ASSUMED-RFID-00001")
        advanceUntilIdle()

        assertTrue(vm.state.value.selectedAnimals.isEmpty())
        assertEquals(listOf(GOAT_ID, GOAT_ID_B), vm.state.value.animalMatches.map { it.goatId })
    }

    /**
     * The focused-field path: when the operator has the search field focused, the gun's
     * keystrokes type into the editor (bypassing the wedge capture) and the screen turns the
     * terminating Enter into [ShiftingEvent.LookupAnimalsAutoAdd]. The read must get the same
     * auto-add as the capture path.
     */
    @Test
    fun `a gun read typed into the focused field auto-adds via LookupAnimalsAutoAdd`() = runTest(dispatcher) {
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), filterByTag = true)
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimalsAutoAdd)
        advanceUntilIdle()

        assertEquals(listOf(GOAT_ID), vm.state.value.selectedAnimals.map { it.goatId })
        assertEquals("", vm.state.value.animalQuery)
    }

    @Test
    fun `rfid capture follows the screen's active state`() = runTest(dispatcher) {
        val reader = FakeShiftingRfidReader()
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), reader = reader)
        advanceUntilIdle()

        vm.setRfidCaptureActive(true)
        assertTrue(reader.captureEnabled)
        vm.setRfidCaptureActive(false)
        assertFalse(reader.captureEnabled)
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
        reader: FakeShiftingRfidReader = FakeShiftingRfidReader(),
        filterByTag: Boolean = false,
    ) = ShiftingViewModel(
        syncRepository = syncRepository,
        countsRepository = FakeShiftingCountsRepository(matches, destinations, filterByTag),
        analytics = NoopShiftingAnalytics(),
        crashReporter = NoopShiftingCrashReporter(),
        rfidReader = reader,
        savedStateHandle = SavedStateHandle(),
    )

    private fun animal(
        lifecycle: String,
        shedId: String = CBE_SHED_ID,
        shedName: String = "Castro 1",
        partitionLabel: String? = null,
        goatId: String = GOAT_ID,
        tag: String = "CBE-ASSUMED-RFID-00001",
        parkId: String = CBE_PARK_ID,
    ) = GoatSearchItemDto(
        goatId = goatId,
        displayId = "G-000325",
        animalIdentifier1 = tag,
        lifecycleStatus = lifecycle,
        locationPath = GoatLocationPathDto(
            operationalLocationDisplay = "Coimbatore / $shedName",
            parkId = parkId,
            parkName = "Coimbatore",
            shedId = shedId,
            shedName = shedName,
            partitionLabel = partitionLabel,
        ),
    )

    private companion object {
        const val GOAT_ID = "d8337607-6e21-41c9-a703-a7b73ae4e545"
        const val GOAT_ID_B = "e9448718-7f32-42da-b814-b8c84bf5f656"
        const val CBE_PARK_ID = "00000000-0000-4000-8000-000000003001"
        const val CBE_SHED_ID = "43071c6e-3b00-47a9-860c-1bbacb570575"
        const val CPT_PARK_ID = "00000000-0000-4000-8000-000000003002"
        const val YASHODA_SHED_ID = "63071c6e-3b00-47a9-860c-1bbacb570576"
    }
}

private class FakeShiftingCountsRepository(
    private val matches: List<GoatSearchItemDto>,
    private val destinations: List<CountsDestinationParkDto>? = null,
    /** When true, [lookupAnimals] resolves like the real endpoint — only rows whose tag matches. */
    private val filterByTag: Boolean = false,
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
    override suspend fun lookupAnimals(query: String, parkId: String?, shedId: String?): Result<List<GoatSearchItemDto>> =
        Result.success(if (filterByTag) matches.filter { it.animalIdentifier1 == query } else matches)
}

/** Test double for the keyboard-wedge reader: [scan] emits one completed tag read. */
private class FakeShiftingRfidReader : RfidReaderPort {
    private val readsFlow = MutableSharedFlow<RfidRead>()
    override val status: StateFlow<RfidReaderStatus> = MutableStateFlow(RfidReaderStatus.READY)
    override val reads: SharedFlow<RfidRead> = readsFlow
    override val readerName: StateFlow<String?> = MutableStateFlow("Test reader")
    override val devices: StateFlow<List<RfidReaderDevice>> = MutableStateFlow(emptyList())

    var captureEnabled: Boolean = false
        private set

    suspend fun scan(tag: String) {
        readsFlow.emit(RfidRead(tag = tag, capturedAtDeviceMs = 1L))
    }

    override fun refreshStatus() {}
    override fun openSystemPairing() {}
    override fun setCaptureEnabled(enabled: Boolean) {
        captureEnabled = enabled
    }
    override fun setCompletionKeySwallowEnabled(enabled: Boolean) {}
    override fun onKeyEvent(event: KeyEvent): Boolean = false
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
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
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
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class NoopShiftingCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}
