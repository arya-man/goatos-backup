package sg.mesha.goatos.core.data

import androidx.paging.ExperimentalPagingApi
import androidx.paging.LoadType
import androidx.paging.PagingConfig
import androidx.paging.PagingState
import androidx.paging.RemoteMediator
import androidx.paging.testing.asSnapshot
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.FeedPurchaseItemEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.FeedPurchaseDto
import sg.mesha.goatos.core.network.dto.FeedPurchasePageDto

/**
 * The Feed Purchases ledger keeps the person's place, the way the Sales ledger learned to on
 * 2026-09-26: a refresh (pull, or coming back from a purchase) must bring back the row they were
 * looking at instead of starting the ledger again from row 0.
 */
@OptIn(ExperimentalPagingApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class FeedPurchaseLedgerCacheTest {

    private class Backend : AppApi by FakeAppApi() {
        val requests = mutableListOf<Int?>()
        override suspend fun getFeedPurchases(farm: String?, delivery: String?, limit: Int?, offset: Int?): FeedPurchasePageDto {
            requests += offset
            val start = offset ?: 0
            val count = minOf(limit ?: 20, TOTAL - start).coerceAtLeast(0)
            return FeedPurchasePageDto(
                purchases = (start until start + count).map { FeedPurchaseDto(feedPurchaseId = "p-$it", feedItem = "Feed $it") },
                total = TOTAL, limit = limit ?: 20, offset = start,
            )
        }
    }

    private suspend fun withRepo(block: suspend (DefaultVendorsRepository, GoatDatabase, Backend) -> Unit) {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java).allowMainThreadQueries().build()
        try {
            val backend = Backend()
            block(DefaultVendorsRepository(backend, database), database, backend)
        } finally {
            database.close()
        }
    }

    @Test
    fun `a refresh through the real pager comes back to the purchase the person was looking at`() = runTest {
        withRepo { repo, _, _ ->
            val shown = repo.feedPurchases("", "").asSnapshot {
                scrollTo(44)
                refresh()
            }
            assertTrue("row 44 is on screen after the refresh; got ${shown.map { it.feedPurchaseId }}", shown.any { it.feedPurchaseId == "p-44" })
        }
    }

    private fun at(anchor: Int?) = PagingState<Int, FeedPurchaseItemEntity>(emptyList(), anchor, PagingConfig(20), 0)

    @Test
    fun `a refresh at the bottom re-reads every page up to the person's place, one page per request`() = runTest {
        withRepo { repo, _, backend ->
            val all = repo.feedPurchaseMediator("", "")
            all.load(LoadType.REFRESH, at(null))
            all.load(LoadType.APPEND, at(null))
            all.load(LoadType.APPEND, at(null))
            backend.requests.clear()

            assertTrue(all.load(LoadType.REFRESH, at(44)) is RemoteMediator.MediatorResult.Success)
            assertEquals("page by page, never one oversized request", listOf(0, 20, 40), backend.requests)
            val append = all.load(LoadType.APPEND, at(44))
            assertTrue("the ledger still knows it is at its end", (append as RemoteMediator.MediatorResult.Success).endOfPaginationReached)
            assertEquals(listOf(0, 20, 40), backend.requests)
        }
    }

    @Test
    fun `a refresh at the top still reads only the first page`() = runTest {
        withRepo { repo, _, backend ->
            val all = repo.feedPurchaseMediator("", "")
            all.load(LoadType.REFRESH, at(null))
            backend.requests.clear()
            all.load(LoadType.REFRESH, at(3))
            assertEquals(listOf(0), backend.requests)
        }
    }

    @Test
    fun `saving one load keeps the ledger's cursor so it still pages past it`() = runTest {
        withRepo { repo, _, backend ->
            val all = repo.feedPurchaseMediator("", "")
            all.load(LoadType.REFRESH, at(null))
            repo.persistServerFeedPurchase(FeedPurchaseDto(feedPurchaseId = "p-3", feedItem = "Edited"))
            backend.requests.clear()
            val next = all.load(LoadType.APPEND, at(null))
            assertEquals("the ledger fetched its second page instead of stopping", listOf(20), backend.requests)
            assertTrue(!(next as RemoteMediator.MediatorResult.Success).endOfPaginationReached)
        }
    }

    private companion object {
        const val TOTAL = 45
    }
}
