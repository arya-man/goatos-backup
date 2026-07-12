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

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ExecutionRepositoryPaginationTest {
    private data class Request(val shedId: String, val taskId: String, val cursor: String?, val limit: Int?)

    @Test
    fun `bounded cursor pages append through Room without duplicates`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()
            repository.appendScanRoster(SHED_ID, TASK_ID, "cursor-1", PAGE_SIZE).getOrThrow()
            repository.appendScanRoster(SHED_ID, TASK_ID, "cursor-2", PAGE_SIZE).getOrThrow()

            val cached = repository.observeScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).first().data!!
            assertEquals(TOTAL_ROWS, cached.rows.size)
            assertEquals(TOTAL_ROWS, cached.rows.map { it.obligationId }.distinct().size)
            assertNull(cached.nextCursor)
            assertEquals(listOf(null, "cursor-1", "cursor-2"), requests.map { it.cursor })
            assertTrue(requests.all { it.shedId == SHED_ID && it.taskId == TASK_ID && it.limit == PAGE_SIZE })
        }
    }

    @Test
    fun `stale cursor is rejected before network and cache stays intact`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            val result = repository.appendScanRoster(SHED_ID, TASK_ID, "wrong-cursor", PAGE_SIZE)

            assertTrue(result.exceptionOrNull() is ScanRosterCursorException)
            assertEquals(1, requests.size)
            val cached = repository.observeScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).first().data!!
            assertEquals(PAGE_SIZE, cached.rows.size)
            assertEquals("cursor-1", cached.nextCursor)
        }
    }

    @Test
    fun `offline continuation preserves Room cursor for retry`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()
            backend.offlineCursor = "cursor-1"

            assertTrue(repository.appendScanRoster(SHED_ID, TASK_ID, "cursor-1", PAGE_SIZE).isFailure)
            val stale = repository.observeScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).first().data!!
            assertEquals(PAGE_SIZE, stale.rows.size)
            assertEquals("cursor-1", stale.nextCursor)

            backend.offlineCursor = null
            repository.appendScanRoster(SHED_ID, TASK_ID, "cursor-1", PAGE_SIZE).getOrThrow()
            val recovered = repository.observeScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).first().data!!
            assertEquals(PAGE_SIZE * 2, recovered.rows.size)
            assertEquals("cursor-2", recovered.nextCursor)
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
                            taskId = args[1] as String,
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
                    database.scanRosterCacheDao(),
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

    private companion object {
        const val SHED_ID = "shed-a"
        const val TASK_ID = "task-a"
        const val PAGE_SIZE = 20
        const val TOTAL_ROWS = 45
    }
}
