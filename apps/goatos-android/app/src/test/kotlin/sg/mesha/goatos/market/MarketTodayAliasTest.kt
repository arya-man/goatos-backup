package sg.mesha.goatos.market

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.DefaultMarketRepository
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.MarketRepository
import sg.mesha.goatos.core.data.cache.VendorsBlobCacheEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.MarketSurveyDayDto

/**
 * The blank cache key `market:day:` is the alias the TODAY screen reads. Opening a PAST day on
 * the market screen must never rewrite it: the operator would come back to "today" and be shown
 * yesterday's prices as though they were this morning's calls.
 *
 * Commit a13e0d41 narrowed the alias write to a genuinely blank (server-resolves-today) request
 * and added [MarketRepository.refreshDayAndTodayAliasIfCurrent] for the one case where a dated
 * refresh legitimately repairs the alias -- when the alias already points at that same day.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class MarketTodayAliasTest {

    @Test
    fun `refreshing a past day leaves the today alias alone`() = runTest {
        withRepository { repository, database, serve ->
            // The alias holds today, as a blank refresh left it.
            serve { MarketSurveyDayDto(businessDate = TODAY, pending = 4) }
            repository.refreshDay("")
            assertEquals(TODAY, cachedAliasDate(database))

            // The operator opens yesterday.
            serve { MarketSurveyDayDto(businessDate = YESTERDAY, pending = 9) }
            repository.refreshDay(YESTERDAY)

            assertEquals(
                "a dated refresh must not file another day under the today alias",
                TODAY,
                cachedAliasDate(database),
            )
        }
    }

    @Test
    fun `refreshDayAndTodayAliasIfCurrent repairs the alias only when it already holds that day`() = runTest {
        withRepository { repository, database, serve ->
            serve { MarketSurveyDayDto(businessDate = TODAY, pending = 4) }
            repository.refreshDay("")

            // Same day, fresher counts -> the alias is brought up to date.
            serve { MarketSurveyDayDto(businessDate = TODAY, pending = 1) }
            repository.refreshDayAndTodayAliasIfCurrent(TODAY)
            assertEquals(TODAY, cachedAliasDate(database))
            assertEquals(1, cachedAlias(database)!!.pending)

            // A different day -> the alias is untouched.
            serve { MarketSurveyDayDto(businessDate = YESTERDAY, pending = 9) }
            repository.refreshDayAndTodayAliasIfCurrent(YESTERDAY)
            assertEquals(TODAY, cachedAliasDate(database))
            assertEquals(1, cachedAlias(database)!!.pending)
        }
    }

    @Test
    fun `an undecodable today alias is quarantined rather than left to rot`() = runTest {
        withRepository { repository, database, serve ->
            database.vendorsBlobCacheDao().upsert(
                VendorsBlobCacheEntity(ALIAS_KEY, "{ this is not a day }", 1L),
            )

            serve { MarketSurveyDayDto(businessDate = YESTERDAY, pending = 9) }
            repository.refreshDayAndTodayAliasIfCurrent(YESTERDAY)

            assertNull(
                "a corrupt alias row must be deleted so the next blank refresh repopulates it",
                database.vendorsBlobCacheDao().get(ALIAS_KEY),
            )
        }
    }

    @Test
    fun `an undecodable cached day is quarantined so the next refresh can repopulate it`() = runTest {
        withRepository { repository, database, _ ->
            val key = DefaultMarketRepository.DAY_KEY_PREFIX + YESTERDAY
            database.vendorsBlobCacheDao().upsert(VendorsBlobCacheEntity(key, "{ not a day }", 1_000L))

            assertNull(
                "a day that no longer decodes must read as absent, never as a half-parsed day",
                repository.observeDay(YESTERDAY).first(),
            )
            assertNull(
                "the corrupt row must be deleted, not skipped and left to fail forever",
                database.vendorsBlobCacheDao().get(key),
            )
        }
    }

    private suspend fun cachedAlias(database: GoatDatabase): MarketSurveyDayDto? =
        database.vendorsBlobCacheDao().get(ALIAS_KEY)?.dtoJson?.let { JSON.decodeFromString(it) }

    private suspend fun cachedAliasDate(database: GoatDatabase): String? = cachedAlias(database)?.businessDate

    private suspend fun withRepository(
        block: suspend (MarketRepository, GoatDatabase, ((String?) -> MarketSurveyDayDto) -> Unit) -> Unit,
    ) {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            var responder: (String?) -> MarketSurveyDayDto = { error("no market day configured") }
            val api = Proxy.newProxyInstance(
                AppApi::class.java.classLoader,
                arrayOf(AppApi::class.java),
            ) { proxy, method, args ->
                when (method.name) {
                    "getMarketSurveyDay" -> responder(args?.getOrNull(0) as String?)
                    "toString" -> "MarketAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi
            block(
                DefaultMarketRepository(api, database, JSON, clock = { 1_000L }),
                database,
            ) { next -> responder = next }
        } finally {
            database.close()
        }
    }

    private companion object {
        const val TODAY = "2026-09-14"
        const val YESTERDAY = "2026-09-13"
        const val ALIAS_KEY = DefaultMarketRepository.DAY_KEY_PREFIX
        val JSON = Json { ignoreUnknownKeys = true }
    }
}
