package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.feature.pccare.PcCareWorklistEvent
import java.time.LocalDate
import java.time.ZoneId

@OptIn(ExperimentalCoroutinesApi::class)
class PcCareWorklistDateWindowTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `execution worklist refuses old dates so old finished cards cannot be selected`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val vm = buildWorklistViewModel(repo)
        val stateJob = collectState(vm)
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))

        vm.bind("inventory_vaccine", "Vaccine stock")
        runCurrent()
        assertEquals(today.toString(), vm.state.value.dateLabel)

        vm.onEvent(PcCareWorklistEvent.SelectDate(today.minusDays(1)))
        runCurrent()

        assertEquals(today.toString(), vm.state.value.dateLabel)
        assertEquals(emptyList<PcCareWorklistQueryDate>(), repo.worklistQueries.map { PcCareWorklistQueryDate(it.date) })
        stateJob.cancel()
    }

    @Test
    fun `execution worklist keeps today selectable so a same-day done card can remain visible`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val vm = buildWorklistViewModel(repo)
        val stateJob = collectState(vm)
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))

        vm.bind("inventory_vaccine", "Vaccine stock")
        vm.onEvent(PcCareWorklistEvent.SelectDate(today))
        runCurrent()

        assertEquals(today.toString(), vm.state.value.dateLabel)
        stateJob.cancel()
    }

    @Test
    fun `execution worklist uses today's current-or-carry query so delayed carry-over stays visible`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        repo.worklistTasks = listOf(
            pcCareTaskDtoFixture(
                taskId = "carry-inventory",
                category = "inventory_vaccine",
                workState = "delayed",
            ).copy(
                plannedBusinessDate = today.minusDays(1).toString(),
                dueBusinessDate = today.toString(),
            ),
        )
        val vm = buildWorklistViewModel(repo)
        val stateJob = collectState(vm)
        val rowsJob = kotlinx.coroutines.CoroutineScope(dispatcher).launch {
            vm.rows.collect {}
        }

        vm.bind("inventory_vaccine", "Vaccine stock")
        runCurrent()

        assertEquals(
            listOf(PcCareWorklistQueryDate(today.toString())),
            repo.worklistQueries.map { PcCareWorklistQueryDate(it.date) },
        )
        assertEquals("Delayed", repo.worklistTasks.single().toCardUi(emptySet()).statusLabel)
        rowsJob.cancel()
        stateJob.cancel()
    }

    @Test
    fun `execution worklist allows the seven day vaccine stock planning window`() = runTest(dispatcher) {
        val repo = FakePcCareRepository()
        val vm = buildWorklistViewModel(repo)
        val stateJob = collectState(vm)
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        val inventoryTaskDate = today.plusDays(7)

        vm.bind("inventory_vaccine", "Vaccine stock")
        vm.onEvent(PcCareWorklistEvent.SelectDate(inventoryTaskDate))
        runCurrent()

        assertEquals(inventoryTaskDate.toString(), vm.state.value.dateLabel)
        stateJob.cancel()
    }

    private fun buildWorklistViewModel(repo: FakePcCareRepository) = PcCareWorklistViewModel(
        repository = repo,
        submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) },
        analytics = FakeAnalyticsPort(),
        crashReporter = NoopCrashReporter(),
    )

    private fun collectState(vm: PcCareWorklistViewModel): Job = kotlinx.coroutines.CoroutineScope(dispatcher).launch {
        vm.state.collect {}
    }

    private data class PcCareWorklistQueryDate(val date: String)
}
