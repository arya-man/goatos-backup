package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.network.dto.PcCareRoundCardDto
import sg.mesha.goatos.feature.pccare.PcCarePlanEvent

/**
 * THE PLANNER'S CARD CARRIES ITS VERBS. When the list became round-grained the old per-pen
 * monitor card stopped being rendered, and END THIS WORK / START IT AGAIN went with it — the
 * backend kept close/reopen, the phone silently lost them, and screenshots of the new list did
 * not show their absence to anyone reading for something else. These pin the verbs onto the
 * card that is actually on screen.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PcCareRoundCardActionsTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModel(repository: FakePcCareRepository) = PcCarePlanViewModel(
        repository = repository,
        submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) },
        analytics = NoopAnalytics(),
        crashReporter = NoopCrashReporter(),
    )

    private fun card(
        roundId: String = "round-1",
        singleTaskId: String = "",
        workState: String = "",
        penCount: Int = 3,
    ) = PcCareRoundCardDto(
        cardKey = roundId.ifBlank { singleTaskId },
        roundId = roundId,
        singleTaskId = singleTaskId,
        category = "deworming",
        categoryLabel = "Deworming",
        parkName = "CPT",
        plannedBusinessDate = "2026-09-11",
        dueBusinessDate = "2026-09-11",
        status = "open",
        workState = workState,
        penCount = penCount,
        penLabels = listOf("Castro 1", "Castro 2", "Castro 3").take(penCount),
    )

    private suspend fun cardsOn(vm: PcCarePlanViewModel) = vm.state.value.roundCards

    @Test
    fun `live work offers END THIS WORK on the card itself`() = runTest(dispatcher) {
        val repo = FakePcCareRepository().apply { roundCards = listOf(card()) }
        val vm = viewModel(repo)
        vm.bindMonitor("deworming", "Deworming")
        advanceUntilIdle()

        val ui = cardsOn(vm).single()
        assertTrue("live work must offer End this work", ui.closable)
        assertFalse("live work is not reopenable", ui.reopenable)
    }

    @Test
    fun `ending a ROUND ends the whole round, not one pen`() = runTest(dispatcher) {
        val repo = FakePcCareRepository().apply { roundCards = listOf(card()) }
        val vm = viewModel(repo)
        vm.bindMonitor("deworming", "Deworming")
        advanceUntilIdle()

        vm.onEvent(PcCarePlanEvent.AskCloseCard("round-1", "round-1", ""))
        vm.onEvent(PcCarePlanEvent.CloseCard("round-1", "", "pens emptied for repairs"))
        advanceUntilIdle()

        assertEquals(listOf("round-1" to "pens emptied for repairs"), repo.closedRounds)
        assertTrue("a round must not be ended one task at a time", repo.closed.isEmpty())
    }

    @Test
    fun `a round of one ends its single task`() = runTest(dispatcher) {
        val repo = FakePcCareRepository().apply {
            roundCards = listOf(card(roundId = "", singleTaskId = "task-9", penCount = 1))
        }
        val vm = viewModel(repo)
        vm.bindMonitor("deworming", "Deworming")
        advanceUntilIdle()

        vm.onEvent(PcCarePlanEvent.CloseCard("", "task-9", "pen empty"))
        advanceUntilIdle()

        assertEquals(listOf("task-9" to "pen empty"), repo.closed)
        assertTrue("a round of one has no round to end", repo.closedRounds.isEmpty())
    }

    @Test
    fun `an ENDED round of one offers START IT AGAIN and no longer offers ending`() = runTest(dispatcher) {
        val repo = FakePcCareRepository().apply {
            roundCards = listOf(card(roundId = "", singleTaskId = "task-9", workState = "closed", penCount = 1))
        }
        val vm = viewModel(repo)
        vm.bindMonitor("deworming", "Deworming")
        advanceUntilIdle()

        val ui = cardsOn(vm).single()
        assertTrue("ended work must be restartable", ui.reopenable)
        assertFalse("ended work cannot be ended again", ui.closable)

        vm.onEvent(PcCarePlanEvent.ReopenTask("task-9"))
        advanceUntilIdle()
        assertEquals(listOf("task-9"), repo.reopened)
    }

    /**
     * The chip must say ENDED, not "Open". Closing leaves every pen's status at 'open' and
     * moves only its work_state, so a status-only chip called deliberately ended work "Open" —
     * on the Completed tab, where it was the only thing telling the reader what had happened.
     */
    @Test
    fun `an ended round reads Ended, never Open`() = runTest(dispatcher) {
        val repo = FakePcCareRepository().apply { roundCards = listOf(card(workState = "closed")) }
        val vm = viewModel(repo)
        vm.bindMonitor("deworming", "Deworming")
        advanceUntilIdle()

        assertEquals("Ended", cardsOn(vm).single().statusLabel)
    }

    @Test
    fun `a finished round reads Done`() = runTest(dispatcher) {
        val repo = FakePcCareRepository().apply { roundCards = listOf(card(workState = "completed")) }
        val vm = viewModel(repo)
        vm.bindMonitor("deworming", "Deworming")
        advanceUntilIdle()

        assertEquals("Done", cardsOn(vm).single().statusLabel)
    }
}
