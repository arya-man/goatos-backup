package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.SaleRefreshResult
import sg.mesha.goatos.core.data.SalesDealScopeMeta
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.network.dto.SaleAllocationDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.feature.vendors.SalesListEvent
import sg.mesha.goatos.ui.Routes
import java.lang.reflect.Proxy

/** The ledger's count answers the filter in force, and a sale the phone does not hold says so. */
@OptIn(ExperimentalCoroutinesApi::class)
class SalesLedgerStatesTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `switching farm never shows the other filter's count`() = runTest(dispatcher) {
        val repo = LedgerRepo(scopes = mapOf("" to SalesDealScopeMeta(total = 128, syncedAt = 1_000L)))
        val vm = SalesListViewModel(repo, Quiet, Silent, RecordingToxinSyncRepository())
        backgroundScope.launch { vm.state.collect {} }
        assertEquals("128 sales", vm.state.value.countLine)
        assertEquals(1_000L, vm.state.value.lastSyncedAt)
        vm.onEvent(SalesListEvent.SelectFarm("CBE"))
        assertEquals("CBE has not loaded: no count at all, never All's 128", "", vm.state.value.countLine)
        repo.publish("CBE", SalesDealScopeMeta(total = 10, syncedAt = 2_000L))
        assertEquals("10 sales", vm.state.value.countLine)
    }

    @Test
    fun `a sale this phone does not hold shows a not-found state instead of an empty screen`() = runTest(dispatcher) {
        val vm = SaleDetailViewModel(SavedStateHandle(mapOf(Routes.SALE_ID_ARG to "gone")), LedgerRepo(), RecordingToxinSyncRepository(), NoWorkflowsRepo, Quiet, Silent)
        backgroundScope.launch { vm.state.collect {} }
        val state = vm.state.value
        assertFalse(state.isLoading)
        assertTrue(state.notFound)
    }

    @Test
    fun `opening a sale re-reads it, and a sale the server no longer has says so`() = runTest(dispatcher) {
        val repo = LedgerRepo(refresh = SaleRefreshResult.GONE)
        val vm = SaleDetailViewModel(SavedStateHandle(mapOf(Routes.SALE_ID_ARG to "gone")), repo, RecordingToxinSyncRepository(), NoWorkflowsRepo, Quiet, Silent)
        backgroundScope.launch { vm.state.collect {} }
        assertEquals(listOf("gone"), repo.refreshed)
        assertTrue(vm.state.value.gone)
        assertFalse("gone is its own state, not the not-synced one", vm.state.value.notFound)
        vm.onEvent(sg.mesha.goatos.feature.vendors.SaleDetailEvent.Refresh)
        assertEquals("pull to refresh re-reads it too", listOf("gone", "gone"), repo.refreshed)
    }

    @Test
    fun `a sale the phone holds is not reported missing`() = runTest(dispatcher) {
        val repo = LedgerRepo(deal = SalesDealDto(dealId = "d-1", buyerName = "Ramesh Traders"))
        val vm = SaleDetailViewModel(SavedStateHandle(mapOf(Routes.SALE_ID_ARG to "d-1")), repo, RecordingToxinSyncRepository(), NoWorkflowsRepo, Quiet, Silent)
        backgroundScope.launch { vm.state.collect {} }
        assertFalse(vm.state.value.notFound)
        assertEquals("Ramesh Traders", vm.state.value.title)
    }
}

private inline fun <reified T> stub(): T = Proxy.newProxyInstance(T::class.java.classLoader, arrayOf(T::class.java)) { _, m, _ ->
    error("unused ${m.name}")
} as T

private object Quiet : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) = Unit
    override fun setUserProperty(name: String, value: String?) = Unit
    override fun setUserId(id: String?) = Unit
}

private object Silent : CrashReporter {
    override fun log(message: String) = Unit
    override fun recordException(throwable: Throwable, message: String?) = Unit
    override fun setCustomKey(key: String, value: String) = Unit
}

private object NoWorkflowsRepo : WorkflowsRepository by stub<WorkflowsRepository>() {
    override suspend fun refreshDetailBySubject(templateKey: String, subjectRefId: String): Result<String> = Result.success("")
    override fun observeDetail(workflowId: String, lens: String, date: String) = flowOf(null)
}

private class LedgerRepo(
    scopes: Map<String, SalesDealScopeMeta> = emptyMap(),
    private val deal: SalesDealDto? = null,
    private val refresh: SaleRefreshResult = SaleRefreshResult.UNREACHABLE,
) : SalesRepository by stub<SalesRepository>() {
    val refreshed = mutableListOf<String>()
    override suspend fun refreshDeal(dealId: String): SaleRefreshResult {
        refreshed += dealId
        return refresh
    }

    private val metas = mutableMapOf<String, MutableStateFlow<SalesDealScopeMeta?>>().apply {
        scopes.forEach { (farm, meta) -> put(farm, MutableStateFlow(meta)) }
    }

    fun publish(farm: String, meta: SalesDealScopeMeta) {
        metas.getOrPut(farm) { MutableStateFlow(null) }.value = meta
    }

    override fun observeDealScope(farm: String): Flow<SalesDealScopeMeta?> = metas.getOrPut(farm) { MutableStateFlow(null) }
    override fun observeOptions(): Flow<SalesOptionsDto?> = flowOf(SalesOptionsDto(farms = listOf("CBE", "CPT")))
    override suspend fun refreshOptions() = Unit
    override fun observeDeal(dealId: String): Flow<SalesDealDto?> = flowOf(deal)
    override suspend fun invalidateDeals(farm: String) = Unit
    override suspend fun saleAllocation(dealId: String): AppResult<SaleAllocationDto> = AppResult.Err("offline")
}
