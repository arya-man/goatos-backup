package sg.mesha.goatos.core.data

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheDao
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.MilkPreparationPageDto

/**
 * Regression for the 2026-08-27 date-bar defect: refresh(preparationDate) cached the
 * server's DATELESS (today) response under the SELECTED date's cache key, so stepping the
 * Milk Preparation chevrons back rendered today's numbers beneath a past-day label. The
 * selected date must reach the API call itself.
 */
class MilkPreparationRepositoryDateTest {

    @Test
    fun `refresh sends the selected preparation date to the API`() = runBlocking {
        val requestedDates = mutableListOf<String?>()
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getMilkPreparation(
                preparationDate: String?,
                parkId: String?,
                limit: Int,
                offset: Int,
            ): MilkPreparationPageDto {
                requestedDates += preparationDate
                return MilkPreparationPageDto()
            }
        }
        val cache = InMemoryMetaCache()

        val repo = DefaultMilkPreparationRepository(api = api, cache = cache)
        repo.refresh("2026-08-20").getOrThrow()

        assertEquals(
            "the selected date must be the date the API is asked for — a dateless fetch " +
                "writes today's payload under the past day's cache key",
            listOf<String?>("2026-08-20"),
            requestedDates,
        )
        assertEquals(
            "the fetched page is cached under the same selected date's key",
            listOf("__milk_preparation__:2026-08-20"),
            cache.upsertedKeys,
        )
    }

    private class InMemoryMetaCache : CountsBreakdownMetaCacheDao {
        val upsertedKeys = mutableListOf<String>()
        private val rows = MutableStateFlow<Map<String, CountsBreakdownMetaCacheEntity>>(emptyMap())

        override fun observe(cacheKey: String): Flow<CountsBreakdownMetaCacheEntity?> =
            MutableStateFlow(rows.value[cacheKey])

        override suspend fun upsert(entity: CountsBreakdownMetaCacheEntity) {
            upsertedKeys += entity.cacheKey
            rows.value = rows.value + (entity.cacheKey to entity)
        }

        override suspend fun delete(cacheKey: String) {
            rows.value = rows.value - cacheKey
        }

        override suspend fun count(): Int = rows.value.size

        override suspend fun totalBytes(): Long = rows.value.values.sumOf { it.dtoJson.length.toLong() }

        override suspend fun deleteOldest(n: Int) = Unit
    }
}
