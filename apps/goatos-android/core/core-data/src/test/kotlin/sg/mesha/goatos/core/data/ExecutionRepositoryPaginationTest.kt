package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.IOException
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.ScanRosterRowDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto

/**
 * Scan roster is a per-row SSOT ([ScanRosterRowDao]), never a whole-collection JSON blob
 * (docs/decisions/mobile-data-fetch-anti-patterns.md). [DefaultExecutionRepository.refreshScanRoster]
 * walks the WHOLE shed roster page-by-page straight into that table; the UI reads a bounded keyset
 * window, tag lookup + counters + the submit proof-gate resolve the full roster. These tests prove:
 * the whole roster lands in the SSOT with backend `seq` order, the windowed read stays bounded and
 * pages forward LOCALLY (no extra network — page-N works offline), the refresh is atomic (a mid-walk
 * failure leaves the prior roster intact), and no whole-collection blob is written.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ExecutionRepositoryPaginationTest {
    private data class Request(val shedId: String, val taskId: String?, val cursor: String?, val limit: Int?)

    @Test
    fun `execution continuation merges unique rows and advances cursor`() {
        val first = VaccinationExecutionResponseDto(
            rows = listOf(executionRow("shed-a", "task-a"), executionRow("shed-b", "task-b")),
            totalCount = 3,
            nextCursor = "execution-cursor-1",
        )
        val second = VaccinationExecutionResponseDto(
            rows = listOf(executionRow("shed-b", "task-b"), executionRow("shed-c", "task-c")),
            totalCount = 3,
            nextCursor = null,
        )

        val merged = mergeExecutionRowsPage(first, second)

        assertEquals(listOf("shed-a", "shed-b", "shed-c"), merged.rows.map { it.shedId })
        assertEquals(3, merged.totalCount)
        assertNull(merged.nextCursor)
    }

    @Test
    fun `refresh walks the whole roster into the per-row SSOT ordered by backend seq`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = ::numberedPage

            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            // Every row landed in the SSOT (full roster), independent of the UI page size.
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
            assertEquals(TOTAL_ROWS, repository.getScanRosterStatusCounts(SHED_ID, TASK_ID).sumOf { it.count })
            // The UI list read is a BOUNDED window in backend order (obligation-1..20), not the whole set.
            val window = repository.observeScanRosterRows(SHED_ID, TASK_ID, windowSize = PAGE_SIZE).first()
            assertEquals(PAGE_SIZE, window.size)
            assertEquals((1..PAGE_SIZE).map { "obligation-$it" }, window.map { it.obligationId })
            assertEquals((0L until PAGE_SIZE.toLong()).toList(), window.map { it.seq })
            // A page-3 tag resolves against the full SSOT even though the UI window only shows page 1.
            assertEquals("obligation-45", repository.findScanRosterByTag(SHED_ID, TASK_ID, "tag45")?.obligationId)
            // The refresh walked exactly the keyset chain once; no whole-collection blob fetch.
            assertEquals(listOf(null, "cursor-1", "cursor-2"), requests.map { it.cursor })
        }
    }

    @Test
    fun `windowed read pages forward locally after refresh with no extra network (offline)`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()
            val networkAfterRefresh = requests.size

            // Growing the window reveals page-2 / page-N animals with NO further network — the whole
            // roster is already local, so a scrolling operator (even offline) sees every animal.
            assertEquals(PAGE_SIZE, repository.observeScanRosterRows(SHED_ID, TASK_ID, PAGE_SIZE).first().size)
            assertEquals(PAGE_SIZE * 2, repository.observeScanRosterRows(SHED_ID, TASK_ID, PAGE_SIZE * 2).first().size)
            val full = repository.observeScanRosterRows(SHED_ID, TASK_ID, PAGE_SIZE * 3).first()
            assertEquals(TOTAL_ROWS, full.size)
            // Page-2 and page-3 animals are present and in order.
            assertEquals("obligation-21", full[20].obligationId)
            assertEquals("obligation-45", full.last().obligationId)
            assertEquals(networkAfterRefresh, requests.size) // ZERO extra network for local paging
        }
    }

    @Test
    fun `refresh is atomic - a mid-walk failure leaves the prior roster intact`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())

            // A later refresh fails on page 2 (offline). The transaction rolls back; the previously
            // persisted full roster must survive so the operator can still scan offline.
            backend.offlineCursor = "cursor-1"
            assertTrue(repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).isFailure)
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
            assertEquals("obligation-45", repository.findScanRosterByTag(SHED_ID, TASK_ID, "tag45")?.obligationId)
        }
    }

    @Test
    fun `non-advancing cursor is rejected and the prior roster is intact`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            backend.response = { _ -> ScanRosterResponseDto(source = "api", rows = emptyList(), nextCursor = "cursor-stuck") }
            val result = repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE)
            assertTrue(result.exceptionOrNull() is ScanRosterCursorException)
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
        }
    }

    @Test
    fun `done goat ids and rows-by-goat resolve the full-roster done set for the proof gate`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = { cursor ->
                if (cursor == null) {
                    ScanRosterResponseDto(
                        source = "api",
                        rows = listOf(
                            ScanRosterRowDto(goatId = "goat-1", primaryTag = "tag-1", vaccineLabel = "FMD", status = "done", obligationId = "obl-1"),
                            ScanRosterRowDto(goatId = "goat-2", primaryTag = "tag-2", vaccineLabel = "FMD", status = "due", obligationId = "obl-2"),
                            ScanRosterRowDto(goatId = "goat-3", primaryTag = "tag-3", vaccineLabel = "FMD", status = "completed", obligationId = "obl-3"),
                        ),
                        nextCursor = null,
                    )
                } else {
                    error("single page")
                }
            }
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            val done = repository.observeScanRosterDoneGoatIds(SHED_ID, TASK_ID).first().toSet()
            assertEquals(setOf("goat-1", "goat-3"), done)
            val rows = repository.scanRosterRowsByGoatIds(SHED_ID, TASK_ID, listOf("goat-1", "goat-3"))
            assertEquals(setOf("obl-1", "obl-3"), rows.map { it.obligationId }.toSet())
            assertTrue(repository.scanRosterRowsByGoatIds(SHED_ID, TASK_ID, emptyList()).isEmpty())
        }
    }

    @Test
    fun `shed-wide and task-scoped rosters keep separate SSOT scopes`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, taskId = null, limit = PAGE_SIZE).getOrThrow()
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, taskId = null).first())
            // The task scope is still empty until its own refresh.
            assertEquals(0, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())

            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, taskId = null).first())
        }
    }

    @Test
    fun `large multi-page roster streams every page into the SSOT with a bounded window`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = ::largeNumberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            // The UI window stays bounded to one page even though the SSOT holds all 80 rows.
            assertEquals(PAGE_SIZE, repository.observeScanRosterRows(SHED_ID, TASK_ID, PAGE_SIZE).first().size)
            assertEquals(LARGE_TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
            // Page-3 and page-4 animals are present in the SSOT for tag lookup.
            assertEquals("obligation-55", repository.findScanRosterByTag(SHED_ID, TASK_ID, "tag55")?.obligationId)
            assertEquals("obligation-80", repository.findScanRosterByTag(SHED_ID, TASK_ID, "tag80")?.obligationId)
            assertEquals(listOf(null, "cursor-1", "cursor-2", "cursor-3"), requests.map { it.cursor })
        }
    }

    private suspend fun withRepository(
        block: suspend (DefaultExecutionRepository, Backend, MutableList<Request>) -> Unit,
    ) {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val requests = mutableListOf<Request>()
            val backend = Backend()
            val api = Proxy.newProxyInstance(AppApi::class.java.classLoader, arrayOf(AppApi::class.java)) { proxy, method, args ->
                when (method.name) {
                    "getScanRoster" -> {
                        val request = Request(
                            shedId = args?.get(0) as String,
                            taskId = args[1] as String?,
                            cursor = args[2] as String?,
                            limit = args[3] as Int?,
                        )
                        requests += request
                        if (backend.offlineCursor != null && backend.offlineCursor == request.cursor) {
                            throw IOException("offline")
                        }
                        backend.response(request.cursor)
                    }
                    "toString" -> "ScanRosterAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi
            block(
                DefaultExecutionRepository(
                    api,
                    database.executionRowsCacheDao(),
                    database.executionShedCacheDao(),
                    database.scanRosterRowDao(),
                    database,
                    clock = { 42L },
                ),
                backend,
                requests,
            )
        } finally {
            database.close()
        }
    }

    private class Backend {
        var offlineCursor: String? = null
        var response: (String?) -> ScanRosterResponseDto = { error("response not configured") }
    }

    private fun numberedPage(cursor: String?): ScanRosterResponseDto {
        val pageIndex = when (cursor) {
            null -> 0
            else -> cursor.removePrefix("cursor-").toInt()
        }
        val start = pageIndex * PAGE_SIZE + 1
        val rows = (start..TOTAL_ROWS).take(PAGE_SIZE).map { index ->
            ScanRosterRowDto(
                goatId = "goat-$index",
                primaryTag = "tag-$index",
                vaccineLabel = "FMD",
                status = "due",
                obligationId = "obligation-$index",
            )
        }
        val next = if (start + rows.size - 1 < TOTAL_ROWS) "cursor-${pageIndex + 1}" else null
        return ScanRosterResponseDto(source = "api", rows = rows, nextCursor = next)
    }

    private fun largeNumberedPage(cursor: String?): ScanRosterResponseDto {
        val pageIndex = when (cursor) {
            null -> 0
            else -> cursor.removePrefix("cursor-").toInt()
        }
        val start = pageIndex * PAGE_SIZE + 1
        val rows = (start..LARGE_TOTAL_ROWS).take(PAGE_SIZE).map { index ->
            ScanRosterRowDto(
                goatId = "goat-$index",
                primaryTag = "tag$index",
                vaccineLabel = "FMD",
                status = "due",
                obligationId = "obligation-$index",
            )
        }
        val next = if (start + rows.size - 1 < LARGE_TOTAL_ROWS) "cursor-${pageIndex + 1}" else null
        return ScanRosterResponseDto(source = "api", rows = rows, nextCursor = next)
    }

    private fun executionRow(shedId: String, taskId: String) = VaccinationExecutionRowDto(
        parkId = "park-a",
        shedId = shedId,
        shedName = shedId,
        animalStage = "K1",
        sopTaskId = taskId,
    )

    private companion object {
        const val SHED_ID = "shed-a"
        const val TASK_ID = "task-a"
        const val PAGE_SIZE = 20
        const val TOTAL_ROWS = 45
        const val LARGE_TOTAL_ROWS = 80
    }
}
