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
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.network.dto.PcCareSlotDto
import sg.mesha.goatos.feature.pccare.PcCarePlanOption

/**
 * FUMIGATION (maintainer instruction 2026-09-30) is PEN work: the operator records the pen's own
 * captures -- the seeded card is a mixing video and a spraying video -- and scans no animal.
 * The phone renders the SERVED slot list (the removal card's generic face), never the vaccine
 * fridge pair, which is what a task_proof task fell back to before.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PcCareFumigationTaskTest {
    private val dispatcher = UnconfinedTestDispatcher()

    private val mixing = PcCareSlotDto(fieldKey = "mixing_video", label = "Mixing video", kind = "video", required = true)
    private val spraying = PcCareSlotDto(fieldKey = "spraying_video", label = "Spraying video", kind = "video", required = true)

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun fumigationRepo() = FakePcCareRepository().apply {
        detailFlow.value = pcCareTaskDtoFixture(
            category = "fumigation",
            expectedSlots = listOf(mixing, spraying),
        ).copy(captureMode = "task_proof")
    }

    @Test
    fun `a fumigation task shows the pen's served videos, not the fridge pair`() = runTest(dispatcher) {
        val vm = buildPcCareTaskViewModel(fumigationRepo(), title = "Fumigation", category = "fumigation")
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        val state = vm.state.value
        assertTrue("pen work is task-proof capture", state.taskProofMode)
        assertEquals(listOf("mixing_video", "spraying_video"), state.taskProofSlots.map { it.fieldKey })
        assertEquals(listOf("Mixing video", "Spraying video"), state.taskProofSlots.map { it.label })
        assertNull("no fridge photo on a pen spray", state.taskProofPhotoSlot)
        assertNull("no fridge video on a pen spray", state.taskProofVideoSlot)
        assertEquals("Fumigation", state.title)
        collectJob.cancel()
    }

    @Test
    fun `the tab route settles the pen face before the detail arrives`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val vm = buildPcCareTaskViewModel(repo, title = "Fumigation", category = "fumigation")
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        // No scan-and-record row flashes on the way in.
        assertTrue(vm.state.value.taskProofMode)
        collectJob.cancel()
    }

    @Test
    fun `Plan is offered only on a tab the person may plan`() {
        val fumigationOnly = listOf(PcCarePlanOption("fumigation", "Fumigation"))
        assertTrue(pcCarePlanAllowedOnTab(canPlan = true, tabCategory = "fumigation", plannableCategories = fumigationOnly))
        assertFalse(pcCarePlanAllowedOnTab(canPlan = true, tabCategory = "deworming", plannableCategories = fumigationOnly))
        // No planning capability: never, whatever the catalog lists.
        assertFalse(pcCarePlanAllowedOnTab(canPlan = false, tabCategory = "fumigation", plannableCategories = fumigationOnly))
        // Catalog not loaded yet: no button rather than one that may be refused.
        assertFalse(pcCarePlanAllowedOnTab(canPlan = true, tabCategory = "fumigation", plannableCategories = emptyList()))
    }
}
