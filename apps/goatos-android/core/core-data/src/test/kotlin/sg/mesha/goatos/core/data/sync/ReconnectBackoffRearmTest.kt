package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesDealLineWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto
import java.io.IOException

/**
 * Seen on the phone (Sales E2E 2026-09-26): a sale queued during an outage sat for up to ~6
 * minutes after the network came back, because reconnect only kicked a drain and the row's
 * backoff (up to ~18 min at the cap) still said "not yet". A reconnect re-arms every waiting,
 * still-retryable row so it drains at once -- without resetting its attempt budget, and at most
 * once per short window so a flapping network cannot burn the budget.
 */
class ReconnectBackoffRearmTest {
    private val dispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    @Test
    fun `a sale waiting out its backoff is sent the moment the network comes back`() = runBlocking {
        val store = FakeOutboxStore()
        var now = 1_000_000L
        var serverUp = false
        var sent = 0
        val api = object : AppApi by FakeAppApi() {
            override suspend fun createSalesDeal(idempotencyKey: String, request: SalesDealWriteDto): SalesDealDto {
                if (!serverUp) throw IOException("unreachable")
                sent++
                return SalesDealDto(dealId = "d-new")
            }
        }
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true }, dispatchers = dispatchers, clock = { now })
        val repo = DefaultSyncRepository(store = store, engine = engine, connectivityGate = { true },
            appScope = CoroutineScope(Dispatchers.Unconfined), dispatchers = dispatchers, clock = { now })

        val sale = (repo.enqueueSalesDealCreate("c-sale", SalesDealWriteDto(
            saleDate = "2026-09-26", farm = "CPT", buyerName = "Buyer", buyerVendorId = "v",
            lines = listOf(SalesDealLineWriteDto(productType = "Sheep", breed = "Anantapur Sheep", animalCount = 1.0, salesValue = 11000.0)),
        )) as AppResult.Ok).value
        // A long outage: the backoff climbs to its cap.
        repeat(12) { repo.triggerDrain(); now += 20L * 60 * 1000 }
        repo.triggerDrain()
        now += 1_000
        val waiting = store.findById(sale)!!
        assertTrue("the row is still inside its backoff", waiting.nextAttemptAt > now + 60_000)
        val attemptsBefore = waiting.attemptCount

        serverUp = true
        repo.onConnectivityRegained()

        assertEquals("sent on reconnect, not minutes later", 1, sent)
        val landed = store.findById(sale)!!
        assertEquals("SUCCEEDED", landed.status)
        assertEquals("the retry budget is not reset", attemptsBefore, landed.attemptCount)
    }

    @Test
    fun `a flapping network re-arms at most once per window`() = runBlocking {
        val store = FakeOutboxStore()
        var now = 1_000_000L
        var calls = 0
        val api = object : AppApi by FakeAppApi() {
            override suspend fun createSalesDeal(idempotencyKey: String, request: SalesDealWriteDto): SalesDealDto {
                calls++
                throw IOException("unreachable")
            }
        }
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true }, dispatchers = dispatchers, clock = { now })
        val repo = DefaultSyncRepository(store = store, engine = engine, connectivityGate = { true },
            appScope = CoroutineScope(Dispatchers.Unconfined), dispatchers = dispatchers, clock = { now })
        repo.enqueueSalesDealCreate("c-sale", SalesDealWriteDto(
            saleDate = "2026-09-26", farm = "CPT", buyerName = "Buyer", buyerVendorId = "v",
            lines = listOf(SalesDealLineWriteDto(productType = "Sheep", breed = "Anantapur Sheep", animalCount = 1.0, salesValue = 11000.0)),
        ))
        repeat(6) { repo.triggerDrain(); now += 20L * 60 * 1000 }
        repo.triggerDrain()
        now += 1_000
        val before = calls

        repo.onConnectivityRegained() // re-armed: one attempt
        now += 2_000
        repo.onConnectivityRegained() // flap two seconds later: backoff honoured
        now += 2_000
        repo.onConnectivityRegained()

        assertEquals(before + 1, calls)
    }
}
