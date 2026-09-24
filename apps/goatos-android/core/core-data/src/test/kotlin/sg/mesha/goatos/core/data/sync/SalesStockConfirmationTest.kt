package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.ResponseBody.Companion.toResponseBody
import retrofit2.HttpException
import retrofit2.Response
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.*

class SalesStockConfirmationTest {
    private val dispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    @Test
    fun `offline sale survives repository recreation then explicit confirmation sends the same write once`() = runBlocking {
        exerciseConfirmation(close = false)
    }

    @Test
    fun `offline status close uses the same persisted confirmation path`() = runBlocking {
        exerciseConfirmation(close = true)
    }

    private suspend fun exerciseConfirmation(close: Boolean) {
        val store = FakeOutboxStore()
        var online = false
        val keys = mutableListOf<String>()
        val requests = mutableListOf<SalesDealWriteDto>()
        var accepted = 0
        fun reject(): Nothing = throw HttpException(Response.error<Unit>(422,
            """{"error":"feed_stock_confirmation_required","message":"CPT Maize has 10 kg and this sale takes 20 kg"}"""
                .toResponseBody("application/json".toMediaType())))
        val api = object : AppApi by FakeAppApi() {
            override suspend fun createSalesDeal(idempotencyKey: String, request: SalesDealWriteDto): SalesDealDto {
                keys += idempotencyKey
                requests += request
                if (!request.stockShortfallAcknowledged) reject()
                accepted++
                return SalesDealDto(dealId = "recorded")
            }
            override suspend fun setSalesDealStatus(dealId: String, idempotencyKey: String, request: SalesDealStatusWriteDto): SalesDealDto {
                assertEquals("existing-deal", dealId)
                assertEquals("Deal Closed", request.status)
                if (!request.stockShortfallAcknowledged) reject()
                accepted++
                return SalesDealDto(dealId = dealId)
            }
        }
        fun session(): DefaultSyncRepository {
            val engine = SyncEngine(store = store, api = api, connectivityGate = { online }, dispatchers = dispatchers, clock = { 0L })
            return DefaultSyncRepository(store = store, engine = engine, connectivityGate = { online },
                appScope = CoroutineScope(Dispatchers.Unconfined), dispatchers = dispatchers, clock = { 0L })
        }
        val draft = SalesDealWriteDto(saleDate = "2026-09-25", farm = "CPT", buyerName = "Buyer", buyerVendorId = "vendor",
            lines = listOf(SalesDealLineWriteDto(productType = "Feed", breed = "Maize", quantity = 20.0, ratePerUnit = 5.0, salesValue = 0.0)))
        val queued = if (close) session().enqueueSalesDealStatusSet("client", "existing-deal", "Deal Closed")
            else session().enqueueSalesDealCreate("client", draft)
        val id = (queued as AppResult.Ok).value
        val original = store.findById(id)!!
        assertEquals("QUEUED", original.status)
        online = true
        // A new repository/engine has no form state; only the durable row remains.
        val restored = session()
        restored.triggerDrain()
        assertTrue(store.findById(id)!!.toSyncQueueItem().needsSalesStockConfirmation)
        // The old Retry All path only reset scheduling, so it repeated the refusal unchanged.
        store.markRetryReady(id, 0L)
        restored.triggerDrain()
        assertTrue(store.findById(id)!!.toSyncQueueItem().needsSalesStockConfirmation)
        assertEquals(0, accepted)
        assertTrue(restored.retry(id) is AppResult.Err)
        assertEquals(original.payloadJson, store.findById(id)!!.payloadJson)
        assertTrue(restored.confirmSalesStock(id) is AppResult.Ok)
        val saved = store.findById(id)!!
        assertEquals("SUCCEEDED", saved.status)
        assertEquals(original.idempotencyKey, saved.idempotencyKey)
        assertEquals(original.groupKey, saved.groupKey)
        assertEquals(1, accepted)
        assertTrue(restored.confirmSalesStock(id) is AppResult.Err)
        assertEquals(1, accepted)
        if (!close) {
            assertEquals(List(3) { original.idempotencyKey }, keys)
            assertEquals(draft, requests.first())
            assertEquals(draft.copy(stockShortfallAcknowledged = true), requests.last())
        }
    }
}
