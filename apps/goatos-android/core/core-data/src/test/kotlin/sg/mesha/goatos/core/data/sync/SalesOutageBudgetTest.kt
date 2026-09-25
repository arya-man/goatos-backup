package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadDto
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesDealLineWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealPaymentWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto
import java.io.IOException

/**
 * Sales writes must outlive an unreachable server. On the phone (2026-09-26) a sale and a
 * receipt recorded while the API could not be reached went terminal after the default eight
 * attempts (about four minutes of backoff) and were never sent after the phone reconnected.
 */
class SalesOutageBudgetTest {
    private val dispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    @Test
    fun `a sale, a receipt and a lead survive a long outage and land once the server is back`() = runBlocking {
        val store = FakeOutboxStore()
        var now = 0L
        var serverUp = false
        val sent = mutableListOf<String>()
        val api = object : AppApi by FakeAppApi() {
            private fun reach() { if (!serverUp) throw IOException("unreachable") }
            override suspend fun createSalesDeal(idempotencyKey: String, request: SalesDealWriteDto): SalesDealDto {
                reach(); sent += "sale"; return SalesDealDto(dealId = "d-new")
            }
            override suspend fun createSalesDealPayment(dealId: String, idempotencyKey: String, request: SalesDealPaymentWriteDto): SalesDealDto {
                reach(); sent += "receipt"; return SalesDealDto(dealId = dealId)
            }
            override suspend fun createSalesBuyerLead(idempotencyKey: String, request: SalesBuyerLeadWriteDto): SalesBuyerLeadDto {
                reach(); sent += "lead"; return SalesBuyerLeadDto(leadId = "l-new")
            }
        }
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true }, dispatchers = dispatchers, clock = { now })
        val repo = DefaultSyncRepository(store = store, engine = engine, connectivityGate = { true },
            appScope = CoroutineScope(Dispatchers.Unconfined), dispatchers = dispatchers, clock = { now })

        val sale = (repo.enqueueSalesDealCreate("c-sale", SalesDealWriteDto(
            saleDate = "2026-09-26", farm = "CPT", buyerName = "Buyer", buyerVendorId = "v",
            lines = listOf(SalesDealLineWriteDto(productType = "Sheep", breed = "Anantapur Sheep", animalCount = 1.0, salesValue = 11000.0)),
        )) as AppResult.Ok).value
        val receipt = (repo.enqueueSalesDealPaymentWrite("c-pay", "d-1", SalesPaymentOp.CREATE, "", SalesDealPaymentWriteDto(receivedOn = "2026-09-26", amountRupees = 1500.0)) as AppResult.Ok).value
        val lead = (repo.enqueueSalesPipelineWrite(SalesPipelinePayload(clientId = "c-lead", kind = SalesPipelineKind.BUYER_LEAD, buyerLead = SalesBuyerLeadWriteDto(buyerName = "E2E Offline Lead"))) as AppResult.Ok).value

        // Twenty passes of an unreachable server, each one past its row's backoff.
        repeat(20) {
            repo.triggerDrain()
            now += 20L * 60 * 1000
        }
        for (id in listOf(sale, receipt, lead)) {
            assertFalse("row $id died during the outage", store.findById(id)!!.toSyncQueueItem().isDeadLetter)
        }

        serverUp = true
        repeat(3) { repo.triggerDrain(); now += 20L * 60 * 1000 }
        assertEquals(listOf("sale", "receipt", "lead").sorted(), sent.sorted())
        for (id in listOf(sale, receipt, lead)) assertEquals("SUCCEEDED", store.findById(id)!!.status)
    }
}
