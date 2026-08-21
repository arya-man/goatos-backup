package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.feature.pccare.PcCareTaskEvent

/**
 * Free-flow duplicate rule at the ViewModel level: the SAME tag scanned again shows the
 * "Already scanned" notice and enqueues NOTHING new — the durable scan row already exists.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PcCareDuplicateScanTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `duplicate scan shows Already scanned and does not re-enqueue`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = pcCareTaskDtoFixture()
        val analytics = FakeAnalyticsPort()
        val vm = buildPcCareTaskViewModel(repo, analytics = analytics)
        val collectJob = launch { vm.state.collect { } }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.ScanInputChanged("RF-1234"))
        vm.onEvent(PcCareTaskEvent.SubmitTypedScan)
        runCurrent()
        assertEquals(1, repo.scanEnqueues)

        // The SAME tag again (whitespace + case noise included — normalization is shared).
        vm.onEvent(PcCareTaskEvent.ScanInputChanged("  rf-1234 "))
        vm.onEvent(PcCareTaskEvent.SubmitTypedScan)
        runCurrent()

        assertEquals("second scan must not enqueue", 1, repo.scanEnqueues)
        assertTrue(
            "notice should carry the tag: ${vm.state.value.scanNotice}",
            vm.state.value.scanNotice.startsWith("Already scanned"),
        )
        assertTrue(analytics.events.any { it.first == AnalyticsEvents.PC_CARE_SCAN_DUPLICATE })
        collectJob.cancel()
    }

    @Test
    fun `a locked task refuses new scans`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        repo.detailFlow.value = pcCareTaskDtoFixture(status = "pending_verification")
        val vm = buildPcCareTaskViewModel(repo)
        val collectJob = launch { vm.state.collect { } }
        runCurrent()

        vm.onEvent(PcCareTaskEvent.ScanInputChanged("RF-9"))
        vm.onEvent(PcCareTaskEvent.SubmitTypedScan)
        runCurrent()

        assertEquals(0, repo.scanEnqueues)
        assertTrue(vm.state.value.isLocked)
        collectJob.cancel()
    }
}
