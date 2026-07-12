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
import sg.mesha.goatos.core.network.dto.VaccinationGapRowDto
import sg.mesha.goatos.core.network.dto.VaccinationGapsResponseDto

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class VaccinationInsightsRepositoryPaginationTest {

    @Test
    fun `continuation merges unique animals and advances cursor`() {
        val first = page(listOf("goat-1", "goat-2"), next = "cursor-1")
        val second = page(listOf("goat-2", "goat-3"), next = null)

        val merged = mergeVaccinationGapsPage(first, second)

        assertEquals(listOf("goat-1", "goat-2", "goat-3"), merged.rows.map { it.goatId })
        assertNull(merged.nextCursor)
    }

    @Test
    fun `bounded cursor page appends through observed Room cache`() = runTest {
        withRepository { repository, backend, requests ->
            backend.responses[null] = page((1..20).map { "goat-$it" }, next = "cursor-1")
            backend.responses["cursor-1"] = page((20..25).map { "goat-$it" }, next = null)

            repository.refreshGaps(limit = PAGE_SIZE).getOrThrow()
            repository.appendGaps(cursor = "cursor-1", limit = PAGE_SIZE).getOrThrow()

            val cached = repository.observeGaps(limit = PAGE_SIZE).first().data!!
            assertEquals(25, cached.rows.size)
            assertEquals(25, cached.rows.map { it.goatId }.distinct().size)
            assertNull(cached.nextCursor)
            assertEquals(listOf(null, "cursor-1"), requests)
        }
    }

    @Test
    fun `offline continuation keeps cached rows and cursor for retry`() = runTest {
        withRepository { repository, backend, _ ->
            backend.responses[null] = page((1..20).map { "goat-$it" }, next = "cursor-1")
            backend.responses["cursor-1"] = page((21..25).map { "goat-$it" }, next = null)
            repository.refreshGaps(limit = PAGE_SIZE).getOrThrow()
            backend.offlineCursor = "cursor-1"

            assertTrue(repository.appendGaps(cursor = "cursor-1", limit = PAGE_SIZE).isFailure)
            val stale = repository.observeGaps(limit = PAGE_SIZE).first().data!!
            assertEquals(20, stale.rows.size)
            assertEquals("cursor-1", stale.nextCursor)

            backend.offlineCursor = null
            repository.appendGaps(cursor = "cursor-1", limit = PAGE_SIZE).getOrThrow()
            val recovered = repository.observeGaps(limit = PAGE_SIZE).first().data!!
            assertEquals(25, recovered.rows.size)
            assertNull(recovered.nextCursor)
        }
    }

    private suspend fun withRepository(
        block: suspend (DefaultVaccinationInsightsRepository, Backend, MutableList<String?>) -> Unit,
    ) {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val requests = mutableListOf<String?>()
            val backend = Backend()
            val api = Proxy.newProxyInstance(AppApi::class.java.classLoader, arrayOf(AppApi::class.java)) { proxy, method, args ->
                when (method.name) {
                    "getVaccinationGaps" -> {
                        val cursor = args?.get(2) as String?
                        requests += cursor
                        if (backend.offlineCursor != null && backend.offlineCursor == cursor) throw IOException("offline")
                        backend.responses[cursor] ?: error("missing response for cursor $cursor")
                    }
                    "toString" -> "VaccinationGapsAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi
            block(
                DefaultVaccinationInsightsRepository(
                    api = api,
                    gapsDao = database.insightsGapsCacheDao(),
                    coverageDao = database.insightsCoverageCacheDao(),
                    clock = { 42L },
                ),
                backend,
                requests,
            )
        } finally {
            database.close()
        }
    }

    private fun page(ids: List<String>, next: String?) = VaccinationGapsResponseDto(
        rows = ids.map { id -> VaccinationGapRowDto(goatId = id, displayId = id) },
        nextCursor = next,
    )

    private class Backend {
        val responses = mutableMapOf<String?, VaccinationGapsResponseDto>()
        var offlineCursor: String? = null
    }

    private companion object {
        const val PAGE_SIZE = 20
    }
}
