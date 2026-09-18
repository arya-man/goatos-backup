package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.WorkInstructionsRepository
import sg.mesha.goatos.core.network.dto.GeneralSopDto
import sg.mesha.goatos.feature.counts.WorkInstructionsEvent

/**
 * The Work instructions list (SOP studio phase 2): renders the cached list, a start hands the
 * host the run to open, a failed start says so and keeps the list, a failed refresh keeps the
 * cached rows.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class WorkInstructionsViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private class FakeRepo(
        val startable: MutableStateFlow<List<GeneralSopDto>> = MutableStateFlow(listOf(GeneralSopDto(code = "general.gate_visitor_check", name = "Gate visitor check", description = "At the gate", stepCount = 5))),
        var refreshResult: Result<Unit> = Result.success(Unit),
        var startResult: Result<String> = Result.success("wf-1"),
    ) : WorkInstructionsRepository {
        val startKeys = mutableListOf<String>()
        override fun observeStartable(): Flow<List<GeneralSopDto>> = startable
        override suspend fun refresh(): Result<Unit> = refreshResult
        override suspend fun start(sopCode: String, runKey: String): Result<String> {
            startKeys += runKey
            return startResult
        }
    }

    @Test
    fun `renders the cached list and hands the started run to the host`() = runTest(dispatcher) {
        val repo = FakeRepo()
        val vm = WorkInstructionsViewModel(repo, NoopAnalytics(), NoopCrashReporter())
        vm.bind("Work instructions")
        val state = vm.state.first { it.rows.isNotEmpty() }
        assertEquals("Gate visitor check", state.rows.single().name)
        assertEquals(5, state.rows.single().stepCount)

        vm.onEvent(WorkInstructionsEvent.Start("general.gate_visitor_check"))
        val started = vm.state.first { it.openWorkflowId.isNotBlank() }
        assertEquals("wf-1", started.openWorkflowId)
        assertTrue("the run key is minted per tap and prefixed", repo.startKeys.single().startsWith("work-instruction-run:"))
        vm.onEvent(WorkInstructionsEvent.NavigationHandled)
        assertEquals("", vm.state.first { it.openWorkflowId.isBlank() }.openWorkflowId)
    }

    @Test
    fun `a failed start reports it and keeps the list, and a failed refresh keeps the cache`() = runTest(dispatcher) {
        val repo = FakeRepo(startResult = Result.failure(IllegalStateException("offline")), refreshResult = Result.failure(IllegalStateException("offline")))
        val vm = WorkInstructionsViewModel(repo, NoopAnalytics(), NoopCrashReporter())
        vm.onEvent(WorkInstructionsEvent.Refresh)
        vm.onEvent(WorkInstructionsEvent.Start("general.gate_visitor_check"))
        val state = vm.state.first { it.message.isNotBlank() }
        assertTrue(state.message.contains("try again"))
        assertEquals(1, state.rows.size)
        assertEquals("", state.openWorkflowId)
        assertTrue(state.loadedOnce)
        assertTrue(state.rows.none { it.starting })
    }

    @Test
    fun `retry after a failed start reuses the same run key`() = runTest(dispatcher) {
        val repo = FakeRepo(startResult = Result.failure(IllegalStateException("timeout")))
        val vm = WorkInstructionsViewModel(repo, NoopAnalytics(), NoopCrashReporter())

        vm.onEvent(WorkInstructionsEvent.Start("general.gate_visitor_check"))
        vm.state.first { it.message.isNotBlank() }
        repo.startResult = Result.success("wf-1")
        vm.onEvent(WorkInstructionsEvent.Start("general.gate_visitor_check"))
        vm.state.first { it.openWorkflowId == "wf-1" }

        assertEquals(2, repo.startKeys.size)
        assertEquals(repo.startKeys[0], repo.startKeys[1])
    }
}
