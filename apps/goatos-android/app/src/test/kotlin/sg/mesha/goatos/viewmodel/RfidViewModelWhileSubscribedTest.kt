package sg.mesha.goatos.viewmodel

import android.view.KeyEvent
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.rfid.RfidRead
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus

/**
 * MOB-010 guardrail: proves [RfidViewModel.state] is exposed with
 * `stateIn(SharingStarted.WhileSubscribed(5_000))`, so the upstream RFID hardware status
 * stream is collected ONLY while the UI is subscribed — not forever.
 *
 * The prior forever-`collect` bridge kept draining hardware power/battery by polling the RFID
 * reader and Bluetooth stack even when the screen was backgrounded. WhileSubscribed(5_000)
 * stops the upstream collection ~5s after the last subscriber leaves and restarts it on return.
 *
 * Uses [UnconfinedTestDispatcher] (sharing runTest's scheduler) so a launched collector subscribes
 * eagerly/synchronously, while `advanceTimeBy` still drives the WhileSubscribed stop timeout.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class RfidViewModelWhileSubscribedTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `upstream RFID status stream is collected only while state has subscribers`() = runTest(dispatcher) {
        val reader = FakeRfidReaderPort()
        val viewModel = RfidViewModel(reader)

        // No UI subscriber yet -> WhileSubscribed keeps the upstream cold.
        assertEquals(0, reader.activeStatusCollectors)

        // Subscribe (simulates the screen collecting state) — Unconfined subscribes synchronously.
        val job1 = launch { viewModel.state.collect {} }
        assertEquals(1, reader.activeStatusCollectors)

        // Unsubscribe (screen backgrounded). Within the 5s window it stays warm...
        job1.cancel()
        assertEquals(1, reader.activeStatusCollectors)

        // ...but after the 5s WhileSubscribed timeout it stops — no forever collection,
        // no hardware drain.
        advanceTimeBy(6_000)
        assertEquals(0, reader.activeStatusCollectors)

        // Returning to the screen restarts the upstream collection.
        val job2 = launch { viewModel.state.collect {} }
        assertEquals(1, reader.activeStatusCollectors)

        job2.cancel()
    }

    /**
     * Fake RFID reader port. A [MutableStateFlow] natively tracks how many collectors are currently
     * active via [MutableStateFlow.subscriptionCount] — which is exactly the signal WhileSubscribed
     * drives — so [activeStatusCollectors] reads that instead of a hand-rolled counting Flow wrapper
     * (the port's `status` is a `StateFlow`, so a plain wrapper Flow no longer satisfies the contract).
     */
    private class FakeRfidReaderPort : RfidReaderPort {
        private val _status = MutableStateFlow(RfidReaderStatus.NOT_PAIRED)
        override val status: StateFlow<RfidReaderStatus> = _status
        override val reads: SharedFlow<RfidRead> = MutableSharedFlow()

        val activeStatusCollectors: Int get() = _status.subscriptionCount.value

        override fun refreshStatus() {}
        override fun openSystemPairing() {}
        override fun setCaptureEnabled(enabled: Boolean) {}
        override fun onKeyEvent(event: KeyEvent): Boolean = false
    }
}
