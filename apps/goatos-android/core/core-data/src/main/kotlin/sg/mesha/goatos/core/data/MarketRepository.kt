package sg.mesha.goatos.core.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.VendorsBlobCacheEntity
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.MarketSurveyAnswerDto
import sg.mesha.goatos.core.network.dto.MarketSurveyCardDto
import sg.mesha.goatos.core.network.dto.MarketSurveyDayDto

/**
 * Market survey reads (maintainer decision 2026-09-14): the day's city cards. Offline-first per
 * docs/decisions/android-offline-first.md: Room is the UI's single source of truth -- the day
 * view renders from a JSON blob in `vendors_blob_cache` (the Procurement module's shared blob
 * table, the Sales-tab shape) keyed `market:day:<business date>`, and a network refresh upserts
 * it. WRITES do not live here: a city's answers ride the durable outbox through
 * `SyncRepository.enqueueMarketSurveyRecord`, and the sync engine reconciles the server's
 * RETURNED card back through [persistServerCard].
 *
 * The card's STATUS stays SERVER-owned (done only when every configured question has a price);
 * [applyLocalAnswers] overlays the queued figures so the list reads "saving" honestly while the
 * write drains, and the server's card overwrites it the moment it lands.
 */
interface MarketRepository {
    /** Room-first day view for one business date (blank = the last refreshed "today"). */
    fun observeDay(businessDate: String): Flow<MarketSurveyDayDto?>

    /** Network -> Room refresh. Non-blocking contract: a failure leaves the cache serving. */
    suspend fun refreshDay(businessDate: String): Result<MarketSurveyDayDto>

    /** Overlays queued answers onto the cached card so the screen shows them before the drain. */
    suspend fun applyLocalAnswers(businessDate: String, cityId: String, answers: List<MarketSurveyAnswerDto>)

    /** Reconciles a drained write's RETURNED card into the cached day. Sync engine only. */
    suspend fun persistServerCard(businessDate: String, card: MarketSurveyCardDto)
}

class DefaultMarketRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : MarketRepository {

    override fun observeDay(businessDate: String): Flow<MarketSurveyDayDto?> =
        database.vendorsBlobCacheDao().observe(dayKey(businessDate))
            .map { entity ->
                readCachedJson<MarketSurveyDayDto>(
                    json = json,
                    cacheKey = dayKey(businessDate),
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.vendorsBlobCacheDao().delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    override suspend fun refreshDay(businessDate: String): Result<MarketSurveyDayDto> = try {
        val day = api.getMarketSurveyDay(businessDate.ifBlank { null })
        // The SERVER names the business day: a blank request resolves to today's IST day, and
        // that resolved date is the cache key so a stale local clock cannot file today under
        // yesterday. The blank key is kept as an alias for the "today" screen.
        persistDay(day.businessDate, day)
        if (businessDate.isBlank()) persistDay("", day)
        Result.success(day)
    } catch (error: CancellationException) {
        throw error
    } catch (error: Exception) {
        Result.failure(error)
    }

    override suspend fun applyLocalAnswers(businessDate: String, cityId: String, answers: List<MarketSurveyAnswerDto>) {
        val priced = answers.associate { it.questionId to it.price }
        updateCard(businessDate, cityId) { card ->
            val questions = card.questions.map { q -> priced[q.questionId]?.let { q.copy(price = it) } ?: q }
            // Counts follow the overlay; the STATUS is left as the server said it, because "done"
            // is the server's judgement about the configured question set, not the phone's.
            card.copy(questions = questions, answered = questions.count { it.price != null })
        }
    }

    override suspend fun persistServerCard(businessDate: String, card: MarketSurveyCardDto) {
        updateCard(businessDate, card.cityId) { card }
    }

    private suspend fun updateCard(businessDate: String, cityId: String, transform: (MarketSurveyCardDto) -> MarketSurveyCardDto) {
        for (key in listOf(businessDate, "").distinct()) {
            val entity = database.vendorsBlobCacheDao().get(dayKey(key)) ?: continue
            val day = runCatching { json.decodeFromString<MarketSurveyDayDto>(entity.dtoJson) }.getOrNull() ?: continue
            if (key.isNotBlank() && day.businessDate != businessDate) continue
            if (key.isBlank() && day.businessDate != businessDate) continue
            val cards = day.cards.map { if (it.cityId == cityId) transform(it) else it }
            val done = cards.count { it.status == CARD_DONE }
            persistDay(key, day.copy(cards = cards, done = done, pending = cards.size - done))
        }
    }

    private suspend fun persistDay(key: String, day: MarketSurveyDayDto) {
        database.vendorsBlobCacheDao().upsert(VendorsBlobCacheEntity(dayKey(key), json.encodeToString(day), clock()))
        database.vendorsBlobCacheDao().enforceCacheBounds()
    }

    private fun dayKey(businessDate: String): String = DAY_KEY_PREFIX + businessDate.trim()

    companion object {
        const val DAY_KEY_PREFIX = "market:day:"
        const val CARD_DONE = "done"
    }
}
