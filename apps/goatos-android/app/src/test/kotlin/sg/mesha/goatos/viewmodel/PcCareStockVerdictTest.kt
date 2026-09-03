package sg.mesha.goatos.viewmodel

// The vaccine-stock director gate (maintainer decision 2026-09-02): park operators record the
// fridge proof; the PC Director opens the submitted task read-only, sees the videos, and
// approves or sends back with a reason. The verdict bar renders ONLY for the approve-capable
// viewer on a task in review — never a camera, never for an operator.

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
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.network.dto.PcCareSlotDto
import sg.mesha.goatos.feature.pccare.PcCareTaskEvent

@OptIn(ExperimentalCoroutinesApi::class)
class PcCareStockVerdictTest {
    private val dispatcher = UnconfinedTestDispatcher()
    private val stockSlots = listOf(
        PcCareSlotDto(fieldKey = "stock_fridge_photo", label = "Fridge stock photo"),
        PcCareSlotDto(fieldKey = "stock_fridge_video", label = "Fridge stock video"),
    )

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun submittedStockRepo(): FakePcCareRepository = FakePcCareRepository().apply {
        detailFlow.value = pcCareTaskDtoFixture(
            category = "inventory_vaccine",
            expectedSlots = stockSlots,
        ).copy(captureMode = "task_proof", status = "pending_verification")
    }

    @Test
    fun `verdict bar is offered only to the approver and only while in review`() = runTest(dispatcher) {
        val repo = submittedStockRepo()
        val approver = buildPcCareTaskViewModel(repo, approveView = true)
        val collectJob = launch { approver.state.collect {} }
        runCurrent()
        assertTrue("approver on a submitted stock task must see the verdict bar", approver.state.value.verdictOffered)
        assertTrue("the approver's screen stays read-only for capture", approver.state.value.isLocked)
        collectJob.cancel()

        // An operator (no approve capability) on the same task: no verdict bar.
        val operator = buildPcCareTaskViewModel(submittedStockRepo())
        val operatorJob = launch { operator.state.collect {} }
        runCurrent()
        assertFalse(operator.state.value.verdictOffered)
        operatorJob.cancel()

        // The approver on a task NOT yet submitted: no verdict bar either.
        val openRepo = FakePcCareRepository().apply {
            detailFlow.value = pcCareTaskDtoFixture(
                category = "inventory_vaccine",
                expectedSlots = stockSlots,
            ).copy(captureMode = "task_proof", status = "open")
        }
        val early = buildPcCareTaskViewModel(openRepo, approveView = true)
        val earlyJob = launch { early.state.collect {} }
        runCurrent()
        assertFalse(early.state.value.verdictOffered)
        earlyJob.cancel()
    }

    @Test
    fun `approve sends the approve verdict and reflects the completed task`() = runTest(dispatcher) {
        val repo = submittedStockRepo()
        val vm = buildPcCareTaskViewModel(repo, approveView = true)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.ApproveStock)
        runCurrent()

        assertEquals(listOf(Triple("task-1", "approve", "")), repo.stockVerdicts)
        assertFalse(vm.state.value.verdictInFlight)
        assertFalse("a completed task no longer offers the verdict bar", vm.state.value.verdictOffered)
        collectJob.cancel()
    }

    @Test
    fun `reject requires a typed reason and carries it verbatim`() = runTest(dispatcher) {
        val repo = submittedStockRepo()
        val vm = buildPcCareTaskViewModel(repo, approveView = true)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.OpenRejectStock)
        runCurrent()
        assertTrue(vm.state.value.showRejectDialog)

        // A blank reason never sends — the operators re-recording are owed a sentence.
        vm.onEvent(PcCareTaskEvent.ConfirmRejectStock)
        runCurrent()
        assertEquals(emptyList<Triple<String, String, String>>(), repo.stockVerdicts)

        vm.onEvent(PcCareTaskEvent.RejectStockReasonChanged("The clip does not show the FMD shelf"))
        vm.onEvent(PcCareTaskEvent.ConfirmRejectStock)
        runCurrent()
        assertEquals(
            listOf(Triple("task-1", "reject", "The clip does not show the FMD shelf")),
            repo.stockVerdicts,
        )
        assertFalse(vm.state.value.showRejectDialog)
        collectJob.cancel()
    }

    @Test
    fun `a failed verdict surfaces the backend message and keeps the bar`() = runTest(dispatcher) {
        val repo = submittedStockRepo()
        repo.stockVerdictResult = AppResult.Err("This task is not awaiting approval")
        val vm = buildPcCareTaskViewModel(repo, approveView = true)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.ApproveStock)
        runCurrent()

        assertEquals("This task is not awaiting approval", vm.state.value.message)
        assertFalse(vm.state.value.verdictInFlight)
        collectJob.cancel()
    }

    @Test
    fun `an operator viewer cannot send a verdict even by raw event`() = runTest(dispatcher) {
        val repo = submittedStockRepo()
        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect {} }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.ApproveStock)
        runCurrent()

        assertEquals(emptyList<Triple<String, String, String>>(), repo.stockVerdicts)
        collectJob.cancel()
    }
}
