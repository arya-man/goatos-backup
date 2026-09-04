package sg.mesha.goatos.core.data.weighing

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.WEIGHING_PAGE_SIZE
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardDto
import sg.mesha.goatos.core.network.userFacingMessage

// telemetry:exempt data-layer repository; the fasting ViewModels in :app own the
// AnalyticsPort/CrashReporter wiring for every card open, capture, and submit.

/**
 * ONE cached feed & water removal card — ONE ROW PER SHED (maintainer correction #2, 2026-09-03:
 * the list serves one card per shed, so the cache's grain is (fasting task, campaign shed)) —
 * the Room SSOT behind the removal section on the operator's weighing list and the removal
 * detail screen.
 *
 * The whole backend row rides as [dtoJson] (the PC Care / Toxin detail-cache shape): the card is
 * rendered verbatim from backend-owned copy, so normalising its fields into columns would only
 * invite client-side re-composition. [sortIndex] is the server page order; [status] and
 * [removalBusinessDate] are lifted out ONLY as indexed read keys.
 */
@Entity(
    tableName = "weighing_fasting_card",
    primaryKeys = ["fastingTaskId", "campaignShedId"],
    indices = [
        Index(value = ["sortIndex"]),
        Index(value = ["status"]),
    ],
)
data class WeighingFastingCardEntity(
    val fastingTaskId: String,
    val campaignShedId: String,
    val sortIndex: Long,
    val status: String,
    val removalBusinessDate: String,
    val dtoJson: String,
    val updatedAt: Long,
)

/** The fasting list's server cursor. One row (scopeKey = "mine"); the list is caller-scoped. */
@Entity(tableName = "weighing_fasting_remote_key")
data class WeighingFastingRemoteKeyEntity(
    @PrimaryKey val scopeKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface WeighingFastingCardDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(rows: List<WeighingFastingCardEntity>)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(row: WeighingFastingCardEntity)

    /** BOUNDED keyset window — never observeAll; the phone renders ~7-10 cards at once. */
    @Query("SELECT * FROM weighing_fasting_card ORDER BY sortIndex ASC LIMIT :limit")
    fun observeWindow(limit: Int): Flow<List<WeighingFastingCardEntity>>

    @Query(
        "SELECT fastingTaskId, campaignShedId, sortIndex, status, removalBusinessDate, dtoJson, updatedAt FROM weighing_fasting_card " +
            "WHERE fastingTaskId = :fastingTaskId AND campaignShedId = :campaignShedId LIMIT 1",
    )
    fun observeCard(fastingTaskId: String, campaignShedId: String): Flow<WeighingFastingCardEntity?>

    @Query(
        "SELECT fastingTaskId, campaignShedId, sortIndex, status, removalBusinessDate, dtoJson, updatedAt FROM weighing_fasting_card " +
            "WHERE fastingTaskId = :fastingTaskId AND campaignShedId = :campaignShedId LIMIT 1",
    )
    suspend fun getCard(fastingTaskId: String, campaignShedId: String): WeighingFastingCardEntity?

    @Query("DELETE FROM weighing_fasting_card")
    suspend fun deleteAll()

    @Query("SELECT MAX(sortIndex) FROM weighing_fasting_card")
    suspend fun maxSortIndex(): Long?

    @androidx.room.Transaction
    suspend fun replaceAll(rows: List<WeighingFastingCardEntity>) {
        deleteAll()
        upsertAll(rows)
    }

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertRemoteKey(key: WeighingFastingRemoteKeyEntity)

    @Query("SELECT * FROM weighing_fasting_remote_key WHERE scopeKey = :scopeKey LIMIT 1")
    suspend fun remoteKey(scopeKey: String): WeighingFastingRemoteKeyEntity?

    @Query("SELECT * FROM weighing_fasting_remote_key WHERE scopeKey = :scopeKey LIMIT 1")
    fun observeRemoteKey(scopeKey: String): Flow<WeighingFastingRemoteKeyEntity?>
}

/** One per-shed feed & water removal card as the UI layer reads it — the backend DTO plus nothing. */
data class WeighingFastingCard(
    val dto: WeighingFastingShedCardDto,
)

/** The cached fasting list plus whether the first page has ever landed. */
data class WeighingFastingListCache(
    val cards: List<WeighingFastingCard> = emptyList(),
    val hasCache: Boolean = false,
    val canLoadMore: Boolean = false,
)

/**
 * Room-SSOT repository for the feed & water removal cards (docs/decisions/android-offline-first.md):
 * screens observe Room; [refresh]/[append] only write into it. A failed refresh leaves the cached
 * cards on screen — the card is read at 8 PM in a shed, where the network is worst.
 */
interface WeighingFastingRepository {
    /** A bounded window of cached cards. Never a network pass-through. */
    fun observeCards(windowSize: Int = WEIGHING_PAGE_SIZE): Flow<WeighingFastingListCache>

    /** ONE cached shed card, live — the detail screen's status source of truth. */
    fun observeCard(fastingTaskId: String, campaignShedId: String): Flow<WeighingFastingCard?>

    /** Re-reads page 1 into Room (reset). Returns rows written. */
    suspend fun refresh(): AppResult<Int>

    /** Appends the next keyset page using the stored cursor. */
    suspend fun append(): AppResult<Int>

    /**
     * Signed download URL for a submitted clip so the card can render its preview after a
     * reinstall or on a read-only card. Best effort: null on any failure, retried on the next
     * open/refresh.
     */
    suspend fun fetchProofDownloadUrl(proofId: String): String?
}

class DefaultWeighingFastingRepository(
    private val api: AppApi?,
    private val dao: WeighingFastingCardDao,
    private val clock: () -> Long = System::currentTimeMillis,
) : WeighingFastingRepository {

    private val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
    }

    override fun observeCards(windowSize: Int): Flow<WeighingFastingListCache> =
        combine(
            dao.observeWindow(windowSize.coerceAtLeast(1)),
            dao.observeRemoteKey(SCOPE_KEY),
        ) { rows, key ->
            WeighingFastingListCache(
                cards = rows.mapNotNull { decode(it.dtoJson) },
                hasCache = key != null,
                canLoadMore = key?.endReached == false,
            )
        }

    override fun observeCard(fastingTaskId: String, campaignShedId: String): Flow<WeighingFastingCard?> =
        dao.observeCard(fastingTaskId, campaignShedId).map { row -> row?.let { decode(it.dtoJson) } }

    override suspend fun refresh(): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err(NOT_CONFIGURED)
        runCatching {
            val page = client.listWeighingFastingShedCards(cursor = null, limit = WEIGHING_PAGE_SIZE)
            val now = clock()
            dao.replaceAll(page.fastingShedCards.mapIndexed { index, dto -> dto.toEntity(index.toLong(), now) })
            dao.upsertRemoteKey(
                WeighingFastingRemoteKeyEntity(
                    scopeKey = SCOPE_KEY,
                    nextCursor = page.nextCursor?.takeIf { it.isNotBlank() },
                    endReached = page.nextCursor.isNullOrBlank(),
                    updatedAt = now,
                ),
            )
            AppResult.Ok(page.fastingShedCards.size)
        }.getOrElse { AppResult.Err(it.userFacingMessage(REFRESH_FALLBACK), it) }
    }

    override suspend fun fetchProofDownloadUrl(proofId: String): String? = withContext(Dispatchers.IO) { // offline-first-guard:ignore: signed URL is single-use and time-limited by the server; caching it in Room would serve an expired/invalid link instead of failing honestly
        val client = api ?: return@withContext null
        if (proofId.isBlank()) return@withContext null
        try {
            client.getProofDownloadUrl(proofId)
        } catch (cancellation: kotlinx.coroutines.CancellationException) {
            throw cancellation
        } catch (_: Exception) {
            null
        }
    }

    override suspend fun append(): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err(NOT_CONFIGURED)
        val key = dao.remoteKey(SCOPE_KEY)
        val cursor = key?.nextCursor?.takeIf { it.isNotBlank() && !key.endReached }
            ?: return@withContext AppResult.Ok(0)
        runCatching {
            val page = client.listWeighingFastingShedCards(cursor = cursor, limit = WEIGHING_PAGE_SIZE)
            val now = clock()
            val base = (dao.maxSortIndex() ?: -1L) + 1L
            dao.upsertAll(page.fastingShedCards.mapIndexed { index, dto -> dto.toEntity(base + index, now) })
            dao.upsertRemoteKey(
                WeighingFastingRemoteKeyEntity(
                    scopeKey = SCOPE_KEY,
                    nextCursor = page.nextCursor?.takeIf { it.isNotBlank() },
                    endReached = page.nextCursor.isNullOrBlank(),
                    updatedAt = now,
                ),
            )
            AppResult.Ok(page.fastingShedCards.size)
        }.getOrElse { AppResult.Err(it.userFacingMessage(REFRESH_FALLBACK), it) }
    }

    private fun WeighingFastingShedCardDto.toEntity(sortIndex: Long, now: Long) = WeighingFastingCardEntity(
        fastingTaskId = fastingTaskId,
        campaignShedId = campaignShedId,
        sortIndex = sortIndex,
        status = status,
        removalBusinessDate = removalBusinessDate,
        dtoJson = json.encodeToString(WeighingFastingShedCardDto.serializer(), this),
        updatedAt = now,
    )

    private fun decode(dtoJson: String): WeighingFastingCard? =
        // exception:exempt a corrupt cached row renders as absent rather than crashing the list;
        // the next successful refresh rewrites it.
        runCatching { WeighingFastingCard(json.decodeFromString(WeighingFastingShedCardDto.serializer(), dtoJson)) }
            .getOrNull()

    private companion object {
        const val SCOPE_KEY = "mine"
        const val NOT_CONFIGURED = "This screen is not available right now."
        const val REFRESH_FALLBACK = "Couldn't refresh. The saved list is shown."
    }
}
