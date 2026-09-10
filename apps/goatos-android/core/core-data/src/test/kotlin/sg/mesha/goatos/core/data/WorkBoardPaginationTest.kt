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
import sg.mesha.goatos.core.network.dto.WorkBoardCountsDto
import sg.mesha.goatos.core.network.dto.WorkBoardRowDto
import sg.mesha.goatos.core.network.dto.WorkBoardRowsPageDto
import sg.mesha.goatos.core.network.dto.WorkBoardSummaryDto

/**
 * Room-SSOT + bounded-pagination coverage for the Work Board read path (maintainer decision
 * 2026-09-10), asserted against real Room + real Paging rather than a stubbed repository:
 *  1. every network page is bounded to one screen-page and the STRING keyset cursor advances;
 *  2. an absent `next_cursor` ends paging — the mediator never spins on the last page;
 *  3. the whole-filter summary is cached SEPARATELY from the paged rows and survives a failed
 *     refresh, so the tiles never fall back to a blank state or a page-local sum.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class WorkBoardPaginationTest {

    private data class RowsRequest(val limit: Int?, val cursor: String?)

    @Test
    fun `rows page by 20 over the keyset cursor and stop when next_cursor is absent`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val requests = mutableListOf<RowsRequest>()
            val allRows = (1..45).map { index -> row(index) }
            val api = boardApi(allRows, requests)
            val repo = DefaultWorkBoardRepository(api = api, database = database, metaDao = database.workBoardMetaCacheDao())
            val query = WorkBoardQuery(parkId = "park-1", businessDate = "2026-09-10")

            // Scroll to the last row so Paging drives every append page, not just the prefetch window.
            val snapshot = repo.observeRows(query).asSnapshot { scrollTo(index = allRows.lastIndex) }

            // Paging drove more than one page (45 rows over a 20-row page size).
            assertTrue("expected multiple pages, got ${requests.size}", requests.size >= 2)
            // Rule: never request more than one screen-page of rows, at either layer.
            assertTrue(
                "every page must request at most $WORK_BOARD_PAGE_SIZE rows, got ${requests.map { it.limit }}",
                requests.all { it.limit == WORK_BOARD_PAGE_SIZE },
            )
            // Rule: the keyset cursor must actually ADVANCE, or the loop never terminates. The
            // first page carries no cursor; the second carries the first page's last row_key.
            assertNull(requests.first().cursor)
            assertEquals(allRows[WORK_BOARD_PAGE_SIZE - 1].rowKey, requests[1].cursor)
            // The last page carries no next_cursor, so paging stopped there: exactly 3 pages.
            assertEquals(3, requests.size)
            // What the UI holds after scrolling to the end is a BOUNDED window (PagingConfig.maxSize
            // drops the earliest pages), in server order, keyed by row_key — never the whole board.
            assertTrue("the in-memory window must be bounded, got ${snapshot.size}", snapshot.size <= WORK_BOARD_PAGE_SIZE * 3)
            assertEquals(allRows.map { it.rowKey }.takeLast(snapshot.size), snapshot.map { it.rowKey })
            // Every row landed in Room exactly once (the SSOT), so a scroll back re-reads it from disk.
            assertEquals(allRows.size, database.workBoardItemDao().countForQuery(query.roomKey()))
            val remoteKey = database.workBoardRemoteKeyDao().get(query.roomKey())
            assertEquals(true, remoteKey?.endReached)
            assertNull(remoteKey?.nextCursor)
        } finally {
            database.close()
        }
    }

    @Test
    fun `summary is cached beside the rows and survives a failed refresh`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            var failSummary = false
            val allRows = (1..25).map { index -> row(index) }
            val api = boardApi(allRows, mutableListOf()) { failSummary }
            val repo = DefaultWorkBoardRepository(api = api, database = database, metaDao = database.workBoardMetaCacheDao())
            val query = WorkBoardQuery(parkId = "park-1", businessDate = "2026-09-10")

            assertNull("cold cache starts empty", repo.observeSummary(query).first().data)

            repo.observeRows(query).asSnapshot()

            val cached = repo.observeSummary(query).first()
            assertNotNull("the summary must be cached after the first page", cached.data)
            // The tiles are the backend's WHOLE-FILTER counts (total 25, 8 done = every third row),
            // NOT a sum of the 20 rows the first page happened to return.
            assertEquals(25, cached.data?.total)
            assertEquals(8, cached.data?.laneCount(WorkBoardLanes.DONE))
            assertNotNull("a synced read carries a lastSyncedAt stamp", cached.lastSyncedAt)

            // The network goes away: the refresh fails, and the cache must survive untouched so
            // the screen keeps rendering it behind an offline indicator.
            failSummary = true
            assertTrue(repo.refresh(query).isFailure)
            assertEquals(25, repo.observeSummary(query).first().data?.total)

            // A cached row is readable by its row_key without any network call (the L1 detail).
            assertEquals("Pack Castro 2", repo.observeRow(allRows[1].rowKey).first()?.title)
        } finally {
            database.close()
        }
    }

    private fun row(index: Int): WorkBoardRowDto = WorkBoardRowDto(
        module = "feed",
        sourceType = "feed_packing_completion",
        sourceId = "row-$index",
        rowKey = "feed|feed_packing_completion|row-$index",
        parkId = "park-1",
        parkName = "Channapatna",
        businessDate = "2026-09-10",
        workState = if (index % 3 == 0) "completed" else "due",
        lane = if (index % 3 == 0) "done" else "todo",
        severity = "ok",
        title = "Pack Castro $index",
        counts = WorkBoardCountsDto(done = if (index % 3 == 0) 1 else 0, pending = if (index % 3 == 0) 0 else 1),
    )

    private fun boardApi(
        allRows: List<WorkBoardRowDto>,
        requests: MutableList<RowsRequest>,
        failSummary: () -> Boolean = { false },
    ): AppApi = Proxy.newProxyInstance(
        AppApi::class.java.classLoader,
        arrayOf(AppApi::class.java),
    ) { _, method, args ->
        when (method.name) {
            "getWorkBoardRows" -> {
                val limit = args?.get(5) as Int?
                val cursor = args?.get(6) as String?
                requests += RowsRequest(limit = limit, cursor = cursor)
                // The backend keyset: rows strictly AFTER the cursor's row_key, in board order.
                val start = if (cursor == null) 0 else allRows.indexOfFirst { it.rowKey == cursor } + 1
                val page = allRows.drop(start).take(limit ?: WORK_BOARD_PAGE_SIZE)
                val hasMore = start + page.size < allRows.size
                WorkBoardRowsPageDto(
                    rows = page,
                    nextCursor = if (hasMore) page.last().rowKey else null,
                    businessDate = "2026-09-10",
                    parkId = "park-1",
                    modules = listOf("feed"),
                    ownRowsOnly = true,
                )
            }
            "getWorkBoardSummary" -> {
                if (failSummary()) throw IllegalStateException("offline")
                val done = allRows.count { it.lane == "done" }
                WorkBoardSummaryDto(
                    total = allRows.size,
                    byLane = mapOf("todo" to allRows.size - done, "in_progress" to 0, "in_review" to 0, "done" to done),
                    byState = emptyMap(),
                    byModule = mapOf("feed" to allRows.size),
                    needsAttention = 0,
                    modules = listOf("feed"),
                    lanes = WorkBoardLanes.ORDER,
                    businessDate = "2026-09-10",
                    parkId = "park-1",
                    ownRowsOnly = true,
                )
            }
            "toString" -> "WorkBoardApiTestProxy"
            "hashCode" -> 0
            "equals" -> false
            else -> error("unexpected ${method.name}")
        }
    } as AppApi
}
