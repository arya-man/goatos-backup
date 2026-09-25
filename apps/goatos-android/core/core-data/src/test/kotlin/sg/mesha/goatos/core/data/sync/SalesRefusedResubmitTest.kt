package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import retrofit2.HttpException
import retrofit2.Response
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesDealLineWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto

/** A sale the server refused is fixed on the same form and sent again -- it must actually be sent. */
class SalesRefusedResubmitTest {
    private val dispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    private fun draft(quantity: Double) = SalesDealWriteDto(
        saleDate = "2026-09-25", farm = "CPT", buyerName = "Buyer", buyerVendorId = "vendor",
        lines = listOf(SalesDealLineWriteDto(productType = "Feed", breed = "Maize", quantity = quantity, ratePerUnit = 5.0, salesValue = 0.0)),
    )

    @Test
    fun `a stock-refused sale corrected on the form is sent again with the corrected quantity`() = runBlocking {
        val store = FakeOutboxStore()
        val sent = mutableListOf<SalesDealWriteDto>()
        val api = object : AppApi by FakeAppApi() {
            override suspend fun createSalesDeal(idempotencyKey: String, request: SalesDealWriteDto): SalesDealDto {
                sent += request
                val qty = request.lines.first().quantity ?: 0.0
                if (qty > 10.0 && !request.stockShortfallAcknowledged) {
                    throw HttpException(Response.error<Unit>(422,
                        """{"error":"feed_stock_confirmation_required","message":"CPT Maize has 10 kg"}""".toResponseBody("application/json".toMediaType())))
                }
                return SalesDealDto(dealId = "recorded")
            }
        }
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true }, dispatchers = dispatchers, clock = { 0L })
        val repo = DefaultSyncRepository(store = store, engine = engine, connectivityGate = { true },
            appScope = CoroutineScope(Dispatchers.Unconfined), dispatchers = dispatchers, clock = { 0L })

        val first = (repo.enqueueSalesDealCreate("client", draft(20.0)) as AppResult.Ok).value
        repo.triggerDrain()
        assertTrue(store.findById(first)!!.toSyncQueueItem().needsSalesStockConfirmation)

        val second = repo.enqueueSalesDealCreate("client", draft(8.0))
        assertTrue("resubmit refused: $second", second is AppResult.Ok)
        repo.triggerDrain()
        assertEquals(8.0, sent.last().lines.first().quantity)
        assertEquals("SUCCEEDED", store.findById((second as AppResult.Ok).value)!!.status)
    }

    @Test
    fun `a validation-refused sale sent again unchanged reaches the server again`() = runBlocking {
        val store = FakeOutboxStore()
        var calls = 0
        val api = object : AppApi by FakeAppApi() {
            override suspend fun createSalesDeal(idempotencyKey: String, request: SalesDealWriteDto): SalesDealDto {
                calls++
                throw HttpException(Response.error<Unit>(422,
                    """{"error":"validation_failed","message":"must be more than zero","field":"lines[1].sales_value"}""".toResponseBody("application/json".toMediaType())))
            }
        }
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true }, dispatchers = dispatchers, clock = { 0L })
        val repo = DefaultSyncRepository(store = store, engine = engine, connectivityGate = { true },
            appScope = CoroutineScope(Dispatchers.Unconfined), dispatchers = dispatchers, clock = { 0L })
        val first = (repo.enqueueSalesDealCreate("client", draft(5.0)) as AppResult.Ok).value
        repo.triggerDrain()
        val refused = store.findById(first)!!.toSyncQueueItem()
        assertTrue(refused.conflict)
        assertEquals("lines[1].sales_value", refused.lastErrorField)
        repo.enqueueSalesDealCreate("client", draft(5.0))
        repo.triggerDrain()
        assertEquals(2, calls)
    }
}
