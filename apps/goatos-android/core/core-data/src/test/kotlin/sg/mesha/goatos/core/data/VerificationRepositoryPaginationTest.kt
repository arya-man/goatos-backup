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
import sg.mesha.goatos.core.network.dto.VerificationFilterOptionsDto
import sg.mesha.goatos.core.network.dto.VerificationLocationOptionDto
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationStatus

/**
 * Offline-first coverage for [DefaultVerificationRepository] — the standalone Verifier
 * section's queue (context/architecture/verifier-app-and-flow.md), mirroring
 * [ExecutionRepositoryPaginationTest]'s Room round-trip + keyset-continuation shape.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class VerificationRepositoryPaginationTest {
    private data class Request(
        val category: String?,
        val status: String?,
        val businessDate: String?,
        val missedOnly: Boolean?,
        val parkId: String?,
        val shedId: String?,
        val cursor: String?,
        val limit: Int?,
    )

    @Test
    fun `queue merge dedupes by item id and keeps the newer page's cursor`() {
        val first = VerificationQueueResponseDto(
            items = listOf(item("item-1"), item("item-2")),
            nextCursor = "cursor-1",
        )
        val second = VerificationQueueResponseDto(
            items = listOf(item("item-2"), item("item-3")),
            nextCursor = null,
        )

        val merged = mergeVerificationQueuePage(first, second)

        assertEquals(listOf("item-1", "item-2", "item-3"), merged.items.map { it.itemId })
        assertNull(merged.nextCursor)
    }

    @Test
    fun `queue merge evicts the oldest rows after three pages`() {
        val merged = (1..5).fold(VerificationQueueResponseDto(items = emptyList())) { current, pageNumber ->
            mergeVerificationQueuePage(
                current,
                VerificationQueueResponseDto(
                    items = ((pageNumber - 1) * PAGE_SIZE + 1..pageNumber * PAGE_SIZE)
                        .map { item("item-$it") },
                    nextCursor = "cursor-$pageNumber",
                ),
            )
        }

        assertEquals(60, merged.items.size)
        assertEquals("item-41", merged.items.first().itemId)
        assertEquals("item-100", merged.items.last().itemId)
        assertEquals("cursor-5", merged.nextCursor)
    }

    @Test
    fun `refresh then append persists a bounded keyset window through Room`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = { numberedPage(it.cursor) }
            repository.refreshQueue(category = "vaccine", limit = PAGE_SIZE).getOrThrow()
            repository.appendQueue(cursor = "cursor-1", category = "vaccine", limit = PAGE_SIZE).getOrThrow()

            val cached = repository.observeQueue(category = "vaccine", limit = PAGE_SIZE).first().data!!
            assertEquals(TOTAL_ITEMS, cached.items.size)
            assertEquals(TOTAL_ITEMS, cached.items.map { it.itemId }.distinct().size)
            assertNull(cached.nextCursor)
            assertEquals(listOf(null, "cursor-1"), requests.map { it.cursor })
            assertTrue(requests.all { it.category == "vaccine" && it.limit == PAGE_SIZE })
        }
    }

    @Test
    fun `distinct categories are cached in separate scopes`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = { numberedPage(it.cursor) }
            repository.refreshQueue(category = "vaccine", limit = PAGE_SIZE).getOrThrow()
            repository.refreshQueue(category = "diagnosis", limit = PAGE_SIZE).getOrThrow()

            val vaccineScope = repository.observeQueue(category = "vaccine", limit = PAGE_SIZE).first().data!!
            val diagnosisScope = repository.observeQueue(category = "diagnosis", limit = PAGE_SIZE).first().data!!
            assertEquals(PAGE_SIZE, vaccineScope.items.size)
            assertEquals(PAGE_SIZE, diagnosisScope.items.size)
        }
    }

    @Test
    fun `status business date and missed filters are cached in separate scopes`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = { request ->
                VerificationQueueResponseDto(items = listOf(item("${request.status}-${request.businessDate}-${request.missedOnly}")))
            }
            repository.refreshQueue(
                category = "birth",
                status = "pending",
                businessDate = "2026-07-30",
                missed = false,
                limit = PAGE_SIZE,
            ).getOrThrow()
            repository.refreshQueue(
                category = "birth",
                status = "approved",
                businessDate = "2026-07-29",
                missed = false,
                limit = PAGE_SIZE,
            ).getOrThrow()
            repository.refreshQueue(
                category = "birth",
                status = "pending",
                missed = true,
                limit = PAGE_SIZE,
            ).getOrThrow()

            val dueToday = repository.observeQueue(
                category = "birth",
                status = "pending",
                businessDate = "2026-07-30",
                missed = false,
                limit = PAGE_SIZE,
            ).first().data!!
            val approvedYesterday = repository.observeQueue(
                category = "birth",
                status = "approved",
                businessDate = "2026-07-29",
                missed = false,
                limit = PAGE_SIZE,
            ).first().data!!
            val missed = repository.observeQueue(
                category = "birth",
                status = "pending",
                missed = true,
                limit = PAGE_SIZE,
            ).first().data!!

            assertEquals("pending-2026-07-30-false", dueToday.items.single().itemId)
            assertEquals("approved-2026-07-29-false", approvedYesterday.items.single().itemId)
            assertEquals("pending-null-true", missed.items.single().itemId)
            assertEquals(3, requests.map { listOf(it.status, it.businessDate, it.missedOnly) }.distinct().size)
        }
    }

    @Test
    fun `stale cursor is rejected before network and cache stays intact`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = { numberedPage(it.cursor) }
            repository.refreshQueue(category = "vaccine", limit = PAGE_SIZE).getOrThrow()

            val result = repository.appendQueue(cursor = "wrong-cursor", category = "vaccine", limit = PAGE_SIZE)

            assertTrue(result.exceptionOrNull() is VerificationQueueCursorException)
            assertEquals(1, requests.size)
        }
    }

    @Test
    fun `offline append preserves Room cursor for retry`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = { numberedPage(it.cursor) }
            repository.refreshQueue(category = "vaccine", limit = PAGE_SIZE).getOrThrow()
            backend.offlineCursor = "cursor-1"

            assertTrue(repository.appendQueue(cursor = "cursor-1", category = "vaccine", limit = PAGE_SIZE).isFailure)
            val stale = repository.observeQueue(category = "vaccine", limit = PAGE_SIZE).first().data!!
            assertEquals(PAGE_SIZE, stale.items.size)
            assertEquals("cursor-1", stale.nextCursor)

            backend.offlineCursor = null
            repository.appendQueue(cursor = "cursor-1", category = "vaccine", limit = PAGE_SIZE).getOrThrow()
            val recovered = repository.observeQueue(category = "vaccine", limit = PAGE_SIZE).first().data!!
            assertEquals(TOTAL_ITEMS, recovered.items.size)
        }
    }

    @Test
    fun `decided verification item is removed from cached verifier queue scopes`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = {
                VerificationQueueResponseDto(
                    items = listOf(
                        item(id = "item-1", parkId = "park-1", shedId = "shed-1"),
                        item(id = "item-2", parkId = "park-1", shedId = "shed-2"),
                    ),
                    filterOptions = VerificationFilterOptionsDto(
                        parks = listOf(VerificationLocationOptionDto(id = "park-1", label = "North Park")),
                        sheds = listOf(
                            VerificationLocationOptionDto(id = "shed-1", label = "Godel 1"),
                            VerificationLocationOptionDto(id = "shed-2", label = "Godel 2"),
                        ),
                    ),
                )
            }
            repository.refreshQueue(category = "vaccine", limit = PAGE_SIZE).getOrThrow()
            repository.refreshQueue(category = "vaccine", shedId = "shed-2", limit = PAGE_SIZE).getOrThrow()

            repository.markVerificationItemDecidedLocally("item-2")

            val categoryScope = repository.observeQueue(category = "vaccine", limit = PAGE_SIZE).first().data!!
            val shedScope = repository.observeQueue(category = "vaccine", shedId = "shed-2", limit = PAGE_SIZE).first().data!!
            assertEquals(listOf("item-1"), categoryScope.items.map { it.itemId })
            assertEquals(listOf("shed-1"), categoryScope.filterOptions.sheds.orEmpty().map { it.id })
            assertTrue(shedScope.items.none { it.itemId == "item-2" })
        }
    }

    private suspend fun withRepository(
        block: suspend (DefaultVerificationRepository, Backend, MutableList<Request>) -> Unit,
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
                    "listVerificationQueue" -> {
                        val request = Request(
                            category = args?.get(0) as String?,
                            status = args?.get(1) as String?,
                            businessDate = args?.get(2) as String?,
                            missedOnly = args?.get(3) as Boolean?,
                            parkId = args?.get(4) as String?,
                            shedId = args?.get(5) as String?,
                            cursor = args?.get(6) as String?,
                            limit = args?.get(7) as Int?,
                        )
                        requests += request
                        if (backend.offlineCursor != null && backend.offlineCursor == request.cursor) {
                            throw IOException("offline")
                        }
                        backend.response(request)
                    }
                    "toString" -> "VerificationQueueAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi
            block(
                DefaultVerificationRepository(api, database.verificationQueueCacheDao(), clock = { 42L }),
                backend,
                requests,
            )
        } finally {
            database.close()
        }
    }

    private class Backend {
        var offlineCursor: String? = null
        var response: (Request) -> VerificationQueueResponseDto = { error("response not configured") }
    }

    private fun numberedPage(cursor: String?): VerificationQueueResponseDto {
        val pageIndex = when (cursor) {
            null -> 0
            else -> cursor.removePrefix("cursor-").toInt()
        }
        val start = pageIndex * PAGE_SIZE + 1
        val items = (start..TOTAL_ITEMS).take(PAGE_SIZE).map { item("item-$it") }
        val next = if (start + items.size - 1 < TOTAL_ITEMS) "cursor-${pageIndex + 1}" else null
        return VerificationQueueResponseDto(items = items, nextCursor = next)
    }

    private fun item(id: String, parkId: String? = null, shedId: String? = null) = VerificationQueueItem(
        itemId = id,
        category = "vaccine",
        status = VerificationStatus.PENDING,
        parkId = parkId,
        shedId = shedId,
    )

    private companion object {
        const val PAGE_SIZE = 20
        const val TOTAL_ITEMS = 25
    }
}
