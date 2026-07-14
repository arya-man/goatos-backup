package sg.mesha.goatos.update

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class UpdateGateViewModelTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    /** A gate whose decision is scripted per call, so refresh() flapping can be exercised. */
    private class FakeUpdateGate(private val decisions: MutableList<UpdateDecision>) : UpdateGate {
        var checks = 0
            private set

        override suspend fun check(): UpdateDecision {
            checks++
            return if (decisions.isEmpty()) UpdateDecision.Allowed else decisions.removeAt(0)
        }
    }

    @Test
    fun `allowed gate leaves the app open`() = runTest {
        val vm = UpdateGateViewModel(FakeUpdateGate(mutableListOf(UpdateDecision.Allowed)))
        advanceUntilIdle()
        assertEquals(UpdateGateUiState.Allowed, vm.state.value)
    }

    @Test
    fun `force update decision blocks with the update url`() = runTest {
        val vm = UpdateGateViewModel(FakeUpdateGate(mutableListOf(UpdateDecision.ForceUpdate("u"))))
        advanceUntilIdle()
        assertEquals(UpdateGateUiState.Blocked("u"), vm.state.value)
    }

    @Test
    fun `block is sticky - a later allowed refresh does not reopen the gate`() = runTest {
        // First check blocks; a subsequent refresh returns Allowed (e.g. a fail-open on a
        // failed fetch). The gate must stay blocked until the app is actually updated.
        val gate = FakeUpdateGate(mutableListOf(UpdateDecision.ForceUpdate("u"), UpdateDecision.Allowed))
        val vm = UpdateGateViewModel(gate)
        advanceUntilIdle()
        assertEquals(UpdateGateUiState.Blocked("u"), vm.state.value)

        vm.refresh()
        advanceUntilIdle()
        assertEquals(UpdateGateUiState.Blocked("u"), vm.state.value)
    }

    @Test
    fun `refresh re-checks the gate and can block a previously allowed session`() = runTest {
        val gate = FakeUpdateGate(mutableListOf(UpdateDecision.Allowed, UpdateDecision.ForceUpdate("u")))
        val vm = UpdateGateViewModel(gate)
        advanceUntilIdle()
        assertEquals(UpdateGateUiState.Allowed, vm.state.value)

        vm.refresh()
        advanceUntilIdle()
        assertEquals(UpdateGateUiState.Blocked("u"), vm.state.value)
    }
}
