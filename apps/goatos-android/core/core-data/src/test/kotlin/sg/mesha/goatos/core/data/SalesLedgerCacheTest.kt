package sg.mesha.goatos.core.data

import androidx.paging.ExperimentalPagingApi
import androidx.paging.LoadType
import androidx.paging.PagingConfig
import androidx.paging.PagingState
import androidx.paging.RemoteMediator
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.SalesDealItemEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadDto
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadPageDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesDealPageDto

/**
 * The Sales ledger's Room cache, driven through its real RemoteMediator.
 *
 * - A single-deal write (create, receipt, status) must never drop the scopes' paging cursors: that
 *   made the next page read as "end of list" and let the next mediator write prune every other
 *   farm's cached rows.
 * - The whole-filter count belongs to ONE farm scope and survives in Room, so "All: 45" is never
 *   shown on the CBE filter.
 * - The detail binds to the ledger's own row, not to an evictable detail blob, and a sale recorded
 *   on this phone reaches the scopes it belongs to.
 */
@OptIn(ExperimentalPagingApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class SalesLedgerCacheTest {

    private class Backend : AppApi by FakeAppApi() {
        val requests = mutableListOf<Pair<String?, Int?>>()
        val leadRequests = mutableListOf<Int?>()
        override suspend fun getSalesDeals(farm: String?, limit: Int?, offset: Int?): SalesDealPageDto {
            requests += farm to offset
            val start = offset ?: 0
            val total = if (farm == null) 45 else 10
            val count = minOf(limit ?: 20, total - start).coerceAtLeast(0)
            return SalesDealPageDto(
                deals = (start until start + count).map { deal(if (farm == null) "all-$it" else "$farm-$it", farm ?: "CPT") },
                total = total, limit = limit ?: 20, offset = start,
            )
        }

        var single: SalesDealDto? = null
        var singleCalls = 0
        override suspend fun getSalesDeal(dealId: String): SalesDealDto {
            singleCalls++
            return single ?: throw retrofit2.HttpException(
                retrofit2.Response.error<Unit>(404, "{\"error\":\"not_found\"}".toResponseBody(null)),
            )
        }

        override suspend fun getSalesBuyerLeads(limit: Int?, offset: Int?, search: String?, status: String?): SalesBuyerLeadPageDto {
            leadRequests += offset
            val start = offset ?: 0
            return SalesBuyerLeadPageDto(
                leads = (start until minOf(start + (limit ?: 20), 45)).map { SalesBuyerLeadDto(leadId = "lead-$it", buyerName = "Buyer $it") },
                total = 45,
            )
        }
    }

    private fun <T : Any> state(): PagingState<Int, T> = PagingState(emptyList(), null, PagingConfig(20), 0)

    private suspend fun withRepo(block: suspend (DefaultSalesRepository, GoatDatabase, Backend) -> Unit) {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java).allowMainThreadQueries().build()
        try {
            val backend = Backend()
            block(DefaultSalesRepository(backend, database), database, backend)
        } finally {
            database.close()
        }
    }

    @Test
    fun `saving one deal keeps every scope's cursor so the ledger still pages and other farms stay cached`() = runTest {
        withRepo { repo, db, backend ->
            val cbe = repo.dealMediator("CBE")
            assertTrue(cbe.load(LoadType.REFRESH, state()) is RemoteMediator.MediatorResult.Success)
            val all = repo.dealMediator("")
            all.load(LoadType.REFRESH, state())

            repo.persistServerDeal(deal("all-3", "CPT").copy(buyerName = "Paid in full"))

            val next = all.load(LoadType.APPEND, state())
            assertTrue(next is RemoteMediator.MediatorResult.Success)
            assertEquals("the ledger fetched its second page instead of stopping", "" to 20, (backend.requests.last().first ?: "") to backend.requests.last().second)
            assertTrue("the CBE scope's rows survive the All scope's next write", db.salesDealItemDao().rowsForDeal("CBE-0").isNotEmpty())
            assertEquals("Paid in full", repo.observeDeal("all-3").first()?.buyerName)
        }
    }

    /**
     * Sales phone E2E 2026-09-26: refreshing at the bottom of the ledger threw the person back to
     * the top. REFRESH wiped the scope and kept page one only, so row 44 no longer existed to
     * return to. A refresh re-reads the pages up to where the person is, one page per request.
     */
    @Test
    fun `a refresh at the bottom of the ledger keeps every row up to where the person is`() = runTest {
        withRepo { repo, db, backend ->
            val all = repo.dealMediator("")
            all.load(LoadType.REFRESH, state())
            all.load(LoadType.APPEND, state())
            all.load(LoadType.APPEND, state())
            val key = db.salesDealItemDao().rowsForDeal("all-44").single().queryKey
            assertEquals(45, db.salesDealItemDao().countForQuery(key))
            backend.requests.clear()

            val atTheBottom = PagingState<Int, SalesDealItemEntity>(emptyList(), 44, PagingConfig(20), 0)
            assertTrue(all.load(LoadType.REFRESH, atTheBottom) is RemoteMediator.MediatorResult.Success)

            assertEquals("row 44 is still there to scroll back to", 45, db.salesDealItemDao().countForQuery(key))
            assertEquals(44, db.salesDealItemDao().rowsForDeal("all-44").single().sortIndex)
            assertEquals("page by page, never one oversized request", listOf(0, 20, 40), backend.requests.map { it.second })
            assertTrue("the ledger still knows it is at its end", all.load(LoadType.APPEND, atTheBottom) is RemoteMediator.MediatorResult.Success)
            assertEquals(listOf(0, 20, 40), backend.requests.map { it.second })
        }
    }

    @Test
    fun `a refresh at the top still reads only the first page`() = runTest {
        withRepo { repo, db, backend ->
            val all = repo.dealMediator("")
            all.load(LoadType.REFRESH, state())
            backend.requests.clear()
            all.load(LoadType.REFRESH, PagingState<Int, SalesDealItemEntity>(emptyList(), 3, PagingConfig(20), 0))
            assertEquals(listOf(0), backend.requests.map { it.second })
        }
    }

    @Test
    fun `saving one lead keeps the board's cursor`() = runTest {
        withRepo { repo, _, backend ->
            val board = repo.leadMediator(SalesLeadSide.BUYER, "", "")
            board.load(LoadType.REFRESH, state())
            repo.persistServerBuyerLead(SalesBuyerLeadDto(leadId = "lead-2", buyerName = "Renamed"))
            val next = board.load(LoadType.APPEND, state())
            assertTrue(next is RemoteMediator.MediatorResult.Success)
            assertEquals(listOf<Int?>(0, 20), backend.leadRequests)
        }
    }

    @Test
    fun `each farm scope owns its own count and it survives in Room`() = runTest {
        withRepo { repo, _, _ ->
            repo.dealMediator("").load(LoadType.REFRESH, state())
            assertEquals(45, repo.observeDealScope("").first()?.total)
            assertNull("a farm never loaded shows no count, not All's", repo.observeDealScope("CBE").first())
            repo.dealMediator("CBE").load(LoadType.REFRESH, state())
            assertEquals(10, repo.observeDealScope("CBE").first()?.total)
            assertEquals(45, repo.observeDealScope("").first()?.total)
            assertNotNull(repo.observeDealScope("CBE").first()?.syncedAt)
        }
    }

    @Test
    fun `the detail reads the ledger row even after the detail blob is evicted`() = runTest {
        withRepo { repo, db, _ ->
            repo.dealMediator("").load(LoadType.REFRESH, state())
            db.vendorsBlobCacheDao().deleteOldest(10_000)
            assertEquals("all-0", repo.observeDeal("all-0").first()?.dealId)
        }
    }

    @Test
    fun `a sale recorded on this phone reaches the cached scopes it belongs to`() = runTest {
        withRepo { repo, db, _ ->
            repo.dealMediator("").load(LoadType.REFRESH, state())
            repo.dealMediator("CBE").load(LoadType.REFRESH, state())
            repo.persistServerDeal(deal("new-1", "CBE"))
            val scopes = db.salesDealItemDao().rowsForDeal("new-1")
            assertEquals(2, scopes.size)
            val top = scopes.map(SalesDealItemEntity::sortIndex)
            assertTrue("shown first in its scopes", top.all { it < 0 })
            assertEquals("new-1", repo.observeDeal("new-1").first()?.dealId)
            // A farm the sale is NOT at is untouched.
            repo.persistServerDeal(deal("new-2", "CPT"))
            assertEquals(1, db.salesDealItemDao().rowsForDeal("new-2").size)
        }
    }

    @Test
    fun `opening a sale re-reads that one sale and updates the row the detail shows`() = runTest {
        withRepo { repo, _, backend ->
            repo.dealMediator("").load(LoadType.REFRESH, state())
            backend.single = deal("all-4", "CPT").copy(status = "Deal Failed")
            assertEquals(SaleRefreshResult.FRESH, repo.refreshDeal("all-4"))
            assertEquals(1, backend.singleCalls)
            assertEquals("Deal Failed", repo.observeDeal("all-4").first()?.status)
        }
    }

    @Test
    fun `a sale the server no longer has is reported gone and dropped from the phone`() = runTest {
        withRepo { repo, db, backend ->
            repo.dealMediator("").load(LoadType.REFRESH, state())
            backend.single = null
            assertEquals(SaleRefreshResult.GONE, repo.refreshDeal("all-5"))
            assertNull(repo.observeDeal("all-5").first())
            assertTrue(db.salesDealItemDao().rowsForDeal("all-5").isEmpty())
        }
    }

    private companion object {
        fun deal(id: String, farm: String) = SalesDealDto(dealId = id, farm = farm, buyerName = "Buyer $id", saleDate = "2026-09-25")
    }
}
