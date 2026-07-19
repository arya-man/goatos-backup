package sg.mesha.goatos.core.data

import androidx.paging.testing.asSnapshot
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownRowDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryCountsDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto

/**
 * Room-SSOT + bounded-pagination coverage for the Counts read path.
 *
 * These are the three properties the mobile fetch/offline rules make non-negotiable, and each is
 * asserted against real Room + real Paging rather than a stubbed repository:
 *  1. every network page is bounded to one screen-page and the offset actually advances;
 *  2. the whole-result KPI envelope is cached SEPARATELY from the paged rows, so a total is never
 *     re-derived from the ~20 rows currently in memory;
 *  3. a failed refresh leaves the cached rollup intact — the screen keeps rendering it instead of
 *     falling back to a blank state.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CountsBreakdownPagingTest {

    private data class BreakdownRequest(val limit: Int?, val offset: Int?)

    @Test
    fun `breakdown pages by 20, advances the offset, and never over-fetches`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val requests = mutableListOf<BreakdownRequest>()
            val allRows = (1..45).map { index ->
                CountsBreakdownRowDto(
                    parkId = "park-1",
                    parkLabel = "CBE",
                    shedId = "shed-$index",
                    shedLabel = "Shed $index",
                    managementStage = "Stage $index",
                    breed = "Boer",
                    sex = "female",
                    count = index,
                )
            }
            val api = breakdownApi(allRows, requests)
            val repo = DefaultCountsRepository(
                api = api,
                database = database,
                summaryDao = database.herdSummaryCacheDao(),
                breakdownMetaDao = database.countsBreakdownMetaCacheDao(),
                shiftingDestinationsDao = database.countsShiftingDestinationsCacheDao(),
            )

            val snapshot = repo.breakdownRows(CountsBreakdownQuery()).asSnapshot()

            // Paging drove more than one page (45 rows over a 20-row page size).
            assertTrue("expected multiple pages, got ${requests.size}", requests.size >= 2)
            // Rule: never request more than one screen-page of rows.
            assertTrue(
                "every page must request at most $COUNTS_BREAKDOWN_PAGE_SIZE rows, got ${requests.map { it.limit }}",
                requests.all { (it.limit ?: 0) <= COUNTS_BREAKDOWN_PAGE_SIZE },
            )
            // Rule: the pagination key must actually advance, or the loop never terminates.
            assertEquals(0, requests.first().offset)
            assertEquals(COUNTS_BREAKDOWN_PAGE_SIZE, requests[1].offset)
            // Rows arrive in server order, de-duplicated by their stable grain key.
            assertEquals(allRows.take(snapshot.size).map { it.grainKey }, snapshot.map { it.grainKey })
        } finally {
            database.close()
        }
    }

    @Test
    fun `totals envelope is cached without the paged rows`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val allRows = (1..25).map { index ->
                CountsBreakdownRowDto(shedId = "shed-$index", managementStage = "Stage $index", count = index)
            }
            val api = breakdownApi(allRows, mutableListOf())
            val repo = DefaultCountsRepository(
                api = api,
                database = database,
                summaryDao = database.herdSummaryCacheDao(),
                breakdownMetaDao = database.countsBreakdownMetaCacheDao(),
                shiftingDestinationsDao = database.countsShiftingDestinationsCacheDao(),
            )
            val query = CountsBreakdownQuery()

            repo.breakdownRows(query).asSnapshot()

            val totals = repo.observeBreakdownTotals(query).first()
            assertNotNull("totals must be cached after the first page", totals.data)
            // The KPI is the backend's whole-result rollup (325 = sum 1..25), NOT a sum of the
            // 20 rows the first page happened to return.
            assertEquals(325, totals.data?.totalCount)
            assertEquals(allRows.size, totals.data?.totalRows)
            // And the envelope carries no rows: they live as normalized Room rows, so this blob
            // can never grow with the cohort.
            assertEquals(emptyList<CountsBreakdownRowDto>(), totals.data?.items)
        } finally {
            database.close()
        }
    }

    @Test
    fun `failed summary refresh keeps the cached rollup visible`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            var failNext = false
            val api = Proxy.newProxyInstance(
                AppApi::class.java.classLoader,
                arrayOf(AppApi::class.java),
            ) { _, method, _ ->
                when (method.name) {
                    "getHerdRegisterSummary" -> {
                        if (failNext) throw IllegalStateException("offline")
                        HerdRegisterSummaryResponseDto(
                            items = listOf(HerdRegisterSummaryCountsDto(activeCount = 7, untaggedKidCount = 2)),
                        )
                    }
                    "toString" -> "CountsSummaryApiTestProxy"
                    "hashCode" -> 0
                    "equals" -> false
                    else -> error("unexpected ${method.name}")
                }
            } as AppApi
            val repo = DefaultCountsRepository(
                api = api,
                database = database,
                summaryDao = database.herdSummaryCacheDao(),
                breakdownMetaDao = database.countsBreakdownMetaCacheDao(),
                shiftingDestinationsDao = database.countsShiftingDestinationsCacheDao(),
            )

            assertNull("cold cache starts empty", repo.observeHerdSummary().first().data)

            assertTrue(repo.refreshHerdSummary().isSuccess)
            val cached = repo.observeHerdSummary().first()
            assertEquals(7, cached.data?.items?.first()?.activeCount)
            assertNotNull("a synced read carries a lastSyncedAt stamp", cached.lastSyncedAt)

            // The network goes away: the refresh fails, and the cache must survive untouched so
            // the screen keeps rendering it behind an offline indicator.
            failNext = true
            assertTrue(repo.refreshHerdSummary().isFailure)
            assertEquals(7, repo.observeHerdSummary().first().data?.items?.first()?.activeCount)
        } finally {
            database.close()
        }
    }

    private fun breakdownApi(
        allRows: List<CountsBreakdownRowDto>,
        requests: MutableList<BreakdownRequest>,
    ): AppApi = Proxy.newProxyInstance(
        AppApi::class.java.classLoader,
        arrayOf(AppApi::class.java),
    ) { _, method, args ->
        when (method.name) {
            "getCountsBreakdown" -> {
                val limit = args?.get(6) as Int?
                val offset = args?.get(7) as Int?
                requests += BreakdownRequest(limit = limit, offset = offset)
                val start = offset ?: 0
                val items = allRows.drop(start).take(limit ?: COUNTS_BREAKDOWN_PAGE_SIZE)
                CountsBreakdownResponseDto(
                    items = items,
                    totalRows = allRows.size,
                    // Whole-result rollups, deliberately NOT equal to the page's own sum.
                    totalCount = allRows.sumOf { it.count },
                    totalAdults = allRows.sumOf { it.count },
                    totalKids = 0,
                )
            }
            "getHerdRegisterSummary" -> HerdRegisterSummaryResponseDto()
            "toString" -> "CountsBreakdownApiTestProxy"
            "hashCode" -> 0
            "equals" -> false
            else -> error("unexpected ${method.name}")
        }
    } as AppApi
}
