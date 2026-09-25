package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.data.sync.SalesPipelinePayload
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.SaleAllocationDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.core.network.dto.SalesProductOptionDto
import sg.mesha.goatos.core.network.dto.SalesStatusOptionDto
import sg.mesha.goatos.core.network.dto.VendorOptionDto
import sg.mesha.goatos.core.network.dto.VendorOptionsDto
import sg.mesha.goatos.feature.vendors.SaleCreateEvent
import sg.mesha.goatos.feature.vendors.SaleDetailEvent
import sg.mesha.goatos.feature.vendors.SaleField
import sg.mesha.goatos.feature.vendors.SaleLineField
import sg.mesha.goatos.feature.vendors.SalePaymentField
import sg.mesha.goatos.feature.vendors.SalesBuyerLeadField
import sg.mesha.goatos.feature.vendors.SalesLeadBoardEvent
import sg.mesha.goatos.feature.vendors.SalesPipelinePanel
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus
import sg.mesha.goatos.ui.Routes
import java.lang.reflect.Proxy

/**
 * The phone's Sales writes must be as safe as the ledger they write to: a double tap records ONE
 * payment and ONE lead, a refused write can be corrected and sent again on the SAME key (the
 * outbox re-opens its dead row -- see core-data SalesRefusedResubmitTest), the server's own
 * sentence reaches the person beside the box it names, and nothing the backend refuses
 * (a rate of zero) is queued in the first place.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SalesWriteSafetyTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    // ---------------------------------------------------------------- record a sale

    private fun createVm(sync: SalesSync) = SaleCreateViewModel(SavedStateHandle(), FakeSales(), sync, NoAnalytics, NoCrash)

    private fun SaleCreateViewModel.fillFeedSale(rate: String) {
        onEvent(SaleCreateEvent.FieldChanged(SaleField.FARM, "CPT"))
        val lineId = state.value.lines.first().id
        onEvent(SaleCreateEvent.LineChanged(lineId, SaleLineField.PRODUCT_TYPE, "Feed"))
        onEvent(SaleCreateEvent.LineChanged(lineId, SaleLineField.BREED, "Maize"))
        onEvent(SaleCreateEvent.LineChanged(lineId, SaleLineField.QUANTITY, "20"))
        onEvent(SaleCreateEvent.LineChanged(lineId, SaleLineField.RATE_PER_UNIT, rate))
        onEvent(SaleCreateEvent.BuyerPicked("v-1"))
    }

    @Test
    fun `a rate of zero is refused on the phone exactly as the backend refuses it`() = runTest(dispatcher) {
        val sync = SalesSync()
        val vm = createVm(sync)
        backgroundScope.launch { vm.state.collect {} }
        vm.fillFeedSale(rate = "0")
        vm.onEvent(SaleCreateEvent.Submit)
        assertEquals("nothing the server would refuse is queued", 0, sync.creates.size)
        assertTrue(vm.state.value.lines.first().errors.containsKey(SaleLineField.RATE_PER_UNIT))
    }

    @Test
    fun `a refused sale shows the server's own sentence beside the line it names`() = runTest(dispatcher) {
        val sync = SalesSync()
        val vm = createVm(sync)
        backgroundScope.launch { vm.state.collect {} }
        vm.fillFeedSale(rate = "5")
        vm.onEvent(SaleCreateEvent.Submit)
        assertEquals(1, sync.creates.size)
        val reason = "Line 1 sale value must be more than zero."
        sync.reject("row-1", code = "sales_invalid_lines[1].rate_per_unit", reason = reason)
        val state = vm.state.value
        assertEquals(VendorsWriteStatus.FAILED, state.writeStatus)
        assertEquals(reason, state.writeMessage)
        assertEquals(reason, state.lines.first().errors[SaleLineField.RATE_PER_UNIT])
        assertEquals("the line lives on the first step", 0, state.step)
    }

    @Test
    fun `a refused sale corrected and sent again rides the same key so the outbox re-opens it`() = runTest(dispatcher) {
        val sync = SalesSync()
        val vm = createVm(sync)
        backgroundScope.launch { vm.state.collect {} }
        vm.fillFeedSale(rate = "5")
        vm.onEvent(SaleCreateEvent.Submit)
        sync.reject("row-1", code = "sales_invalid_buyer_name", reason = "Buyer name is required.")
        assertEquals("Buyer name is required.", vm.state.value.fieldErrors[SaleField.BUYER_NAME])
        vm.onEvent(SaleCreateEvent.FieldChanged(SaleField.BUYER_NAME, "Ramesh"))
        vm.onEvent(SaleCreateEvent.Submit)
        assertEquals(2, sync.creates.size)
        assertEquals(sync.creates[0].first, sync.creates[1].first)
    }

    // ---------------------------------------------------------------- payments on a sale

    private fun detailVm(sync: SalesSync) = SaleDetailViewModel(
        SavedStateHandle(mapOf(Routes.SALE_ID_ARG to "deal-1")), FakeSales(), sync, NoWorkflows, NoAnalytics, NoCrash,
    )

    private fun SaleDetailViewModel.typePayment(amount: String) {
        onEvent(SaleDetailEvent.OpenPayment(""))
        onEvent(SaleDetailEvent.PaymentFieldChanged(SalePaymentField.AMOUNT, amount))
    }

    @Test
    fun `a double tap on save records one payment`() = runTest(dispatcher) {
        val sync = SalesSync()
        val vm = detailVm(sync)
        backgroundScope.launch { vm.state.collect {} }
        vm.typePayment("5000")
        sync.gate = CompletableDeferred()
        vm.onEvent(SaleDetailEvent.SavePayment)
        vm.onEvent(SaleDetailEvent.SavePayment)
        sync.gate!!.complete(Unit)
        assertEquals(1, sync.payments.size)
    }

    @Test
    fun `a refused payment saved again keeps its key and a new receipt gets a new one`() = runTest(dispatcher) {
        val sync = SalesSync()
        val vm = detailVm(sync)
        backgroundScope.launch { vm.state.collect {} }
        vm.typePayment("5000")
        vm.onEvent(SaleDetailEvent.SavePayment)
        sync.reject("row-1", code = "sales_invalid_amount_rupees", reason = "Amount is more than the balance.")
        assertNotNull("the editor stays open with what was typed", vm.state.value.paymentEditor)
        vm.onEvent(SaleDetailEvent.PaymentFieldChanged(SalePaymentField.AMOUNT, "4000"))
        vm.onEvent(SaleDetailEvent.SavePayment)
        assertEquals(2, sync.payments.size)
        assertEquals(sync.payments[0], sync.payments[1])
        sync.succeed("row-2")
        vm.typePayment("100")
        vm.onEvent(SaleDetailEvent.SavePayment)
        assertEquals(3, sync.payments.size)
        assertNotEquals(sync.payments[1], sync.payments[2])
    }

    @Test
    fun `a status the server accepted says it is saved, never that it waits for the network`() = runTest(dispatcher) {
        // Seen on the phone (2026-09-26): closing a sale online read "Status saved. It reaches the
        // ledger when the phone is online." after the server already held it.
        val sync = SalesSync()
        val vm = detailVm(sync)
        backgroundScope.launch { vm.state.collect {} }
        vm.onEvent(SaleDetailEvent.ChangeStatus("Deal Closed"))
        assertEquals(1, sync.statuses.size)
        sync.succeed("row-1")
        val message = vm.state.value.editMessage
        assertEquals("Status saved.", message)
        assertTrue(!message.contains("online"))
    }

    @Test
    fun `a final status asks first and is written only when confirmed`() = runTest(dispatcher) {
        // Seen on the phone (2026-09-26): picking Deal Failed from the dropdown wrote it at once --
        // final, and it sends the sale's tagged animals back to their pens.
        val sync = SalesSync()
        val vm = detailVm(sync)
        backgroundScope.launch { vm.state.collect {} }
        vm.onEvent(SaleDetailEvent.ChangeStatus("Deal Failed"))
        assertEquals("nothing is written on the pick", 0, sync.statuses.size)
        assertEquals("Deal Failed", vm.state.value.finalStatusPending)
        vm.onEvent(SaleDetailEvent.DismissFinalStatus)
        assertEquals("", vm.state.value.finalStatusPending)
        assertEquals(0, sync.statuses.size)
        vm.onEvent(SaleDetailEvent.ChangeStatus("Deal Failed"))
        vm.onEvent(SaleDetailEvent.ConfirmFinalStatus)
        assertEquals(listOf("Deal Failed"), sync.statuses)
        // An ordinary status is still written as it is picked.
        vm.onEvent(SaleDetailEvent.ChangeStatus("Advance Paid"))
        assertEquals(listOf("Deal Failed"), sync.statuses.take(1))
    }

    @Test
    fun `an unread tag list never claims the sale has no animals tagged`() = runTest(dispatcher) {
        // Seen offline on the phone (2026-09-26): a sale with two tagged goats read "No animals
        // tagged yet" because the tag read failed and the blank line fell back to that sentence.
        val animalSale = FakeSales { SalesDealDto(dealId = it, buyerName = "Mahendran", productType = "Goat", breed = "Beetal", animalCount = 2.0, status = "Deal Closed") }
        val vm = SaleDetailViewModel(SavedStateHandle(mapOf(Routes.SALE_ID_ARG to "deal-1")), animalSale, SalesSync(), NoWorkflows, NoAnalytics, NoCrash)
        backgroundScope.launch { vm.state.collect {} }
        assertEquals("Tagged animals show when the phone is online.", vm.state.value.taggedLine)
    }

    // ---------------------------------------------------------------- lead boards

    private fun leadVm(sync: SalesSync) = SalesLeadBoardViewModel(
        SavedStateHandle(mapOf(Routes.SALES_PANEL_ARG to SalesPipelinePanel.BUYER_LEADS.name)), FakeSales(), sync, NoAnalytics, NoCrash,
    )

    @Test
    fun `a double tap on a new lead queues one lead`() = runTest(dispatcher) {
        val sync = SalesSync()
        val vm = leadVm(sync)
        backgroundScope.launch { vm.state.collect {} }
        vm.onEvent(SalesLeadBoardEvent.OpenForm)
        vm.onEvent(SalesLeadBoardEvent.FieldChanged(SalesBuyerLeadField.BUYER_NAME.name, "Ramesh"))
        sync.gate = CompletableDeferred()
        vm.onEvent(SalesLeadBoardEvent.Submit)
        vm.onEvent(SalesLeadBoardEvent.Submit)
        sync.gate!!.complete(Unit)
        vm.onEvent(SalesLeadBoardEvent.Submit)
        assertEquals("taps before AND after the write is queued are the same lead", 1, sync.leads.size)
    }

    @Test
    fun `a refused lead keeps the typed form and is sent again on the same key`() = runTest(dispatcher) {
        val sync = SalesSync()
        val vm = leadVm(sync)
        backgroundScope.launch { vm.state.collect {} }
        vm.onEvent(SalesLeadBoardEvent.OpenForm)
        vm.onEvent(SalesLeadBoardEvent.FieldChanged(SalesBuyerLeadField.BUYER_NAME.name, "Ramesh"))
        vm.onEvent(SalesLeadBoardEvent.FieldChanged(SalesBuyerLeadField.PHONE_NUMBER.name, "12"))
        vm.onEvent(SalesLeadBoardEvent.Submit)
        sync.reject("row-1", code = "sales_invalid_phone_number", reason = "Phone number is not a phone number.")
        val form = vm.state.value.form
        assertNotNull("the typed lead survives the refusal", form)
        assertEquals("Ramesh", form!!.values[SalesBuyerLeadField.BUYER_NAME.name])
        assertEquals("Phone number is not a phone number.", vm.state.value.writeMessage)
        assertEquals("the sentence also lands on the box it names", "Phone number is not a phone number.", form.fieldErrors[SalesBuyerLeadField.PHONE_NUMBER.name])
        vm.onEvent(SalesLeadBoardEvent.FieldChanged(SalesBuyerLeadField.PHONE_NUMBER.name, "9876543210"))
        vm.onEvent(SalesLeadBoardEvent.Submit)
        assertEquals(2, sync.leads.size)
        assertEquals(sync.leads[0].clientId, sync.leads[1].clientId)
        sync.succeed("row-2")
        assertEquals("a saved lead closes its form", null, vm.state.value.form)
    }
}

// ------------------------------------------------------------------------------------ fakes

private inline fun <reified T> unused(): T = Proxy.newProxyInstance(T::class.java.classLoader, arrayOf(T::class.java)) { _, m, _ ->
    error("unused ${m.name}")
} as T

private object NoAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) = Unit
    override fun setUserProperty(name: String, value: String?) = Unit
    override fun setUserId(id: String?) = Unit
}

private object NoCrash : CrashReporter {
    override fun log(message: String) = Unit
    override fun recordException(throwable: Throwable, message: String?) = Unit
    override fun setCustomKey(key: String, value: String) = Unit
}

private object NoWorkflows : WorkflowsRepository by unused<WorkflowsRepository>() {
    override suspend fun refreshDetailBySubject(templateKey: String, subjectRefId: String): Result<String> = Result.success("")
    override fun observeDetail(workflowId: String, lens: String, date: String) = flowOf(null)
}

private class FakeSales(
    private val deal: (String) -> SalesDealDto = { SalesDealDto(dealId = it, buyerName = "Ramesh Traders") },
) : SalesRepository by unused<SalesRepository>() {
    private val options = SalesOptionsDto(
        farms = listOf("CPT"),
        productTypes = listOf("Goat", "Feed"),
        products = listOf(
            SalesProductOptionDto(name = "Goat", code = "goat", kind = "animal", unit = "number", pricedPerUnit = false),
            SalesProductOptionDto(name = "Feed", code = "feed", kind = "feed", unit = "kg", pricedPerUnit = true),
        ),
        breeds = mapOf("Goat" to listOf("Osmanabadi"), "Feed" to listOf("Maize")),
        statuses = listOf(SalesStatusOptionDto("Deal Closed", "Deal Closed", "ok")),
        defaultStatus = "Deal Closed",
    )
    override fun observeOptions(): Flow<SalesOptionsDto?> = flowOf(options)
    override suspend fun refreshOptions() = Unit
    override fun observeVendorOptions(): Flow<VendorOptionsDto?> =
        flowOf(VendorOptionsDto(vendors = listOf(VendorOptionDto(vendorId = "v-1", businessName = "Ramesh Traders", city = "Ramanagara"))))
    override suspend fun refreshVendorOptions() = Unit
    override fun observeDeal(dealId: String): Flow<SalesDealDto?> = flowOf(deal(dealId))
    override suspend fun invalidateDeals(farm: String) = Unit
    override suspend fun refreshDeal(dealId: String) = sg.mesha.goatos.core.data.SaleRefreshResult.UNREACHABLE
    override suspend fun saleAllocation(dealId: String): AppResult<SaleAllocationDto> = AppResult.Err("offline")
    override fun observeLeadMeta(side: sg.mesha.goatos.core.data.SalesLeadSide, search: String, status: String) = flowOf(null)
    override suspend fun refreshLeadMeta(side: sg.mesha.goatos.core.data.SalesLeadSide) = Unit
    override suspend fun invalidateLeads(side: sg.mesha.goatos.core.data.SalesLeadSide, search: String, status: String) = Unit
}

/** Records the three Sales enqueues and lets a test settle each outbox row by hand. */
private class SalesSync : SyncRepository by RecordingToxinSyncRepository() {
    val creates = mutableListOf<Pair<String, SalesDealWriteDto>>()
    val payments = mutableListOf<String>()
    val leads = mutableListOf<SalesPipelinePayload>()
    private var rows = 0
    /** When set, every enqueue waits on it -- the way a real Room insert suspends -- so a test can tap twice mid-write. */
    var gate: CompletableDeferred<Unit>? = null
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()

    private suspend fun queued(): AppResult<String> {
        gate?.await()
        val id = "row-${++rows}"
        items.getOrPut(id) { MutableStateFlow(null) }.value = item(id, SyncItemStatus.QUEUED)
        return AppResult.Ok(id)
    }

    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = items.getOrPut(itemId) { MutableStateFlow(null) }

    override suspend fun enqueueSalesDealCreate(clientId: String, request: SalesDealWriteDto): AppResult<String> {
        creates += clientId to request
        return queued()
    }

    override suspend fun enqueueSalesDealPaymentWrite(
        clientId: String, dealId: String, op: String, paymentId: String,
        request: sg.mesha.goatos.core.network.dto.SalesDealPaymentWriteDto?,
    ): AppResult<String> {
        payments += clientId
        return queued()
    }

    val statuses = mutableListOf<String>()

    override suspend fun enqueueSalesDealStatusSet(clientId: String, dealId: String, status: String, acknowledgeStock: Boolean): AppResult<String> {
        statuses += status
        return queued()
    }

    override suspend fun enqueueSalesPipelineWrite(payload: SalesPipelinePayload): AppResult<String> {
        leads += payload
        return queued()
    }

    fun reject(id: String, code: String, reason: String) {
        items.getValue(id).value = item(id, SyncItemStatus.FAILED).copy(conflict = true, lastError = reason, lastErrorCode = code)
    }

    fun succeed(id: String) {
        items.getValue(id).value = item(id, SyncItemStatus.SUCCEEDED)
    }

    private fun item(id: String, status: SyncItemStatus) = SyncQueueItem(
        id = id, opType = "SALES", idempotencyKey = id, groupKey = "g", status = status,
        attemptCount = 0, maxAttempts = 5, conflict = false, createdAt = 0L, updatedAt = 0L, lastError = null,
    )
}
