package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.CountsCaptureCardCacheEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CountsCaptureCardResponseDto

/**
 * The Add birth / Add death form's SOP CAPTURE CARD (maintainer decisions 4 and 7, 2026-09-16).
 *
 * Room is the source the form renders from ([observeCard]); [refreshCard] re-reads the published
 * card on every open, so a publish on /counts/sops reaches the phone on its next refresh with no
 * app update. Null means the phone has never loaded the card: the form then renders plainly and
 * sends no `sop_capture` (the server's older-app rule), which is exactly today's behaviour.
 */
interface CountsCaptureCardRepository {
    fun observeCard(kind: String): Flow<CountsCaptureCardResponseDto?>
    suspend fun refreshCard(kind: String): Result<Unit>
}

class DefaultCountsCaptureCardRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : CountsCaptureCardRepository {

    override fun observeCard(kind: String): Flow<CountsCaptureCardResponseDto?> =
        database.countsCaptureCardCacheDao().observe(kind)
            .map { entity ->
                // exception:exempt an undecodable cached card is data, not a fault -- the next
                // refresh replaces it, and the form renders plainly until then.
                entity?.dtoJson?.let { raw -> runCatching { json.decodeFromString<CountsCaptureCardResponseDto>(raw) }.getOrNull() }
            }
            .flowOn(Dispatchers.Default)

    override suspend fun refreshCard(kind: String): Result<Unit> = runCatching {
        val card = api.getCountsCaptureCard(kind)
        database.countsCaptureCardCacheDao().upsert(
            CountsCaptureCardCacheEntity(scopeKey = kind, dtoJson = json.encodeToString(card), updatedAt = clock()),
        )
    }
}
