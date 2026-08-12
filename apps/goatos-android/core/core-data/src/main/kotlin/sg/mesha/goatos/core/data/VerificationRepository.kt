package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.VerificationQueueCacheDao
import sg.mesha.goatos.core.data.cache.VerificationQueueCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationReviewEventBatchRequestDto
import sg.mesha.goatos.core.network.dto.VerificationReviewEventRequestDto

/**
 * The verifier-only workspace's reusable media queue (context/architecture/verifier-app-and-flow.md
 * + context/architecture/verification-module-design.md). Offline-first
 * (docs/decisions/android-offline-first.md): Room is the UI's single source of truth via
 * [observeQueue], a cache-first Flow scoped by [category], backed by
 * [VerificationQueueCacheDao]. [refreshQueue] is the network side of stale-while-revalidate
 * (first page only); [appendQueue] persists the NEXT keyset page into the SAME Room-backed
 * scope so pagination binds both the network fetch and the observed Room read (mobile-guard
 * rule — never an in-memory-only page 2+).
 */
interface VerificationRepository {
    suspend fun queue(
        category: String? = null,
        status: String? = null,
        businessDate: String? = null,
        missed: Boolean? = null,
        parkId: String? = null,
        shedId: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): VerificationQueueResponseDto

    /** Cache-first stream for this category scope: emits immediately with whatever Room has
     *  (null data on a cold cache) and re-emits after every successful [refreshQueue]/[appendQueue]. */
    fun observeQueue(
        category: String? = null,
        status: String? = null,
        businessDate: String? = null,
        missed: Boolean? = null,
        parkId: String? = null,
        shedId: String? = null,
        limit: Int? = null,
    ): Flow<Resource<VerificationQueueResponseDto>>

    /** Fetches the first page and upserts Room on success; on failure returns the failure and
     *  leaves the cache untouched — the caller surfaces stale/offline, never a blank screen. */
    suspend fun refreshQueue(
        category: String? = null,
        status: String? = null,
        businessDate: String? = null,
        missed: Boolean? = null,
        parkId: String? = null,
        shedId: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    /** Appends the next keyset page into the same Room-backed category scope. */
    suspend fun appendQueue(
        cursor: String,
        category: String? = null,
        status: String? = null,
        businessDate: String? = null,
        missed: Boolean? = null,
        parkId: String? = null,
        shedId: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    fun observeActionQueue(
        category: String? = null,
        parkId: String? = null,
        shedId: String? = null,
        limit: Int? = null,
    ): Flow<Resource<VerificationQueueResponseDto>>

    suspend fun refreshActionQueue(
        category: String? = null,
        parkId: String? = null,
        shedId: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    /** Optimistically removes a just-closed vaccination batch from the cached leadership action
     *  queue. The backend remains the source of truth; this only prevents stale offline cache from
     *  keeping a successful Close button visible until a later refresh. */
    suspend fun markVaccinationBatchClosedLocally(
        batchId: String,
        category: String? = null,
        parkId: String? = null,
        shedId: String? = null,
        limit: Int? = null,
    )

    /** Optimistically removes a verifier-decided item from cached pending queues after the
     *  verdict outbox row has SUCCEEDED. This keeps the verifier queue honest when the backend
     *  write landed but the follow-up refresh is temporarily offline/stale. */
    suspend fun markVerificationItemDecidedLocally(itemId: String)

    /** Best-effort backend audit rows for verifier-only review actions. */
    suspend fun recordReviewEvents(events: List<VerificationReviewEventRequestDto>): Result<Unit> = Result.success(Unit)

    /** Cache-first stream for leadership videos (full trail: pending/approved/rejected/closed).
     *  Returns a bounded window of verification items rendered as UI models. */
    fun observeLeadershipVideos(
        category: String? = null,
        windowSize: Int = 20,
    ): Flow<List<sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi>>

    /** Fetches and caches the first page of leadership videos. */
    /** Backend-owned screen title (queue contract's module label). Empty until first fetch. */
    fun observeLeadershipTitle(category: String? = null, windowSize: Int): Flow<String>

    // windowSize MUST match the value passed to observeLeadershipVideos: the cache key is derived
    // from every query parameter including the limit, so refreshing with a different window writes
    // a row the observing Flow never reads, and the screen renders "No videos yet" over a
    // successful fetch.
    suspend fun refreshLeadershipVideos(
        category: String? = null,
        windowSize: Int,
        reset: Boolean = true,
    ): AppResult<Unit>
}

class DefaultVerificationRepository(
    private val api: AppApi,
    private val queueDao: VerificationQueueCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : VerificationRepository {

    private val appendMutex = Mutex()

    override suspend fun queue(
        category: String?,
        status: String?,
        businessDate: String?,
        missed: Boolean?,
        parkId: String?,
        shedId: String?,
        limit: Int?,
        cursor: String?,
    ): VerificationQueueResponseDto = api.listVerificationQueue(
        category = category,
        status = status,
        businessDate = businessDate,
        missed = missed,
        parkId = parkId,
        shedId = shedId,
        cursor = cursor,
        limit = limit,
    )

    override fun observeQueue(
        category: String?,
        status: String?,
        businessDate: String?,
        missed: Boolean?,
        parkId: String?,
        shedId: String?,
        limit: Int?,
    ): Flow<Resource<VerificationQueueResponseDto>> {
        val key = scopeKey(category, status, businessDate, missed, parkId, shedId, limit)
        return queueDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = runCatching {
        val dto = queue(category, status, businessDate, missed, parkId, shedId, limit, cursor = null)
        val key = scopeKey(category, status, businessDate, missed, parkId, shedId, limit)
        queueDao.upsert(VerificationQueueCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        queueDao.enforceCacheBounds()
    }

    override suspend fun appendQueue(cursor: String, category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = runCatching {
        appendMutex.withLock {
            val key = scopeKey(category, status, businessDate, missed, parkId, shedId, limit)
            val currentEntity = queueDao.get(key)
            val current = readCachedJson<VerificationQueueResponseDto>(
                json = json,
                cacheKey = key,
                dtoJson = currentEntity?.dtoJson,
                updatedAt = currentEntity?.updatedAt,
                now = clock(),
                quarantine = { queueDao.delete(it) },
            ).data ?: throw VerificationQueueCursorException("verification queue continuation has no cached first page")
            if (current.nextCursor != cursor) {
                throw VerificationQueueCursorException("verification queue cursor is stale or belongs to another category")
            }
            val page = queue(category, status, businessDate, missed, parkId, shedId, limit, cursor)
            if (page.nextCursor == cursor) {
                throw VerificationQueueCursorException("verification queue backend returned a non-advancing cursor")
            }
            queueDao.upsert(
                VerificationQueueCacheEntity(
                    cacheKey = key,
                    dtoJson = json.encodeToString(mergeVerificationQueuePage(current, page)),
                    updatedAt = clock(),
                ),
            )
        }
    }

    override fun observeActionQueue(
        category: String?,
        parkId: String?,
        shedId: String?,
        limit: Int?,
    ): Flow<Resource<VerificationQueueResponseDto>> {
        val key = actionScopeKey(category, parkId, shedId, limit)
        return queueDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = runCatching {
        val dto = api.listVerificationActionQueue(category = category, parkId = parkId, shedId = shedId, cursor = null, limit = limit)
        val key = actionScopeKey(category, parkId, shedId, limit)
        queueDao.upsert(VerificationQueueCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        queueDao.enforceCacheBounds()
    }

    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) {
        val trimmedBatchId = batchId.trim()
        if (trimmedBatchId.isEmpty()) return
        val key = actionScopeKey(category, parkId, shedId, limit)
        val currentEntity = queueDao.get(key) ?: return
        val current = readCachedJson<VerificationQueueResponseDto>(
            json = json,
            cacheKey = key,
            dtoJson = currentEntity.dtoJson,
            updatedAt = currentEntity.updatedAt,
            now = clock(),
            quarantine = { queueDao.delete(it) },
        ).data ?: return
        val updated = current.copy(
            driveClosures = current.driveClosures.filterNot { it.batchId == trimmedBatchId },
        )
        queueDao.upsert(
            VerificationQueueCacheEntity(
                cacheKey = key,
                dtoJson = json.encodeToString(updated),
                updatedAt = clock(),
            ),
        )
    }

    override suspend fun markVerificationItemDecidedLocally(itemId: String) {
        val trimmedItemId = itemId.trim()
        if (trimmedItemId.isEmpty()) return
        queueDao.getByPrefix(VERIFY_QUEUE_CACHE_PREFIX).forEach { entity ->
            val current = readCachedJson<VerificationQueueResponseDto>(
                json = json,
                cacheKey = entity.cacheKey,
                dtoJson = entity.dtoJson,
                updatedAt = entity.updatedAt,
                now = clock(),
                quarantine = { queueDao.delete(it) },
            ).data ?: return@forEach
            val updated = current.removeItem(trimmedItemId) ?: return@forEach
            queueDao.upsert(
                VerificationQueueCacheEntity(
                    cacheKey = entity.cacheKey,
                    dtoJson = json.encodeToString(updated),
                    updatedAt = clock(),
                ),
            )
        }
    }

    override suspend fun recordReviewEvents(events: List<VerificationReviewEventRequestDto>): Result<Unit> = runCatching {
        if (events.isNotEmpty()) {
            api.recordVerificationReviewEvents(VerificationReviewEventBatchRequestDto(events))
        }
    }

    private suspend fun VerificationQueueCacheEntity?.toResource(key: String): Resource<VerificationQueueResponseDto> {
        val cached = readCachedJson<VerificationQueueResponseDto>(
            json = json,
            cacheKey = key,
            dtoJson = this?.dtoJson,
            updatedAt = this?.updatedAt,
            now = clock(),
            quarantine = { queueDao.delete(it) },
        )
        return Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
    }

    private fun scopeKey(
        category: String?,
        status: String?,
        businessDate: String?,
        missed: Boolean?,
        parkId: String?,
        shedId: String?,
        limit: Int?,
    ): String = cacheKey(
        VERIFY_QUEUE_CACHE_PREFIX,
        category,
        status,
        businessDate,
        missed?.toString(),
        parkId,
        shedId,
        limit?.toString(),
    )

    private fun actionScopeKey(category: String?, limit: Int?): String =
        actionScopeKey(category, null, null, limit)

    private fun actionScopeKey(category: String?, parkId: String?, shedId: String?, limit: Int?): String =
        cacheKey("verification-action-queue", category, parkId, shedId, limit?.toString())

    override fun observeLeadershipVideos(
        category: String?,
        windowSize: Int,
    ): Flow<List<sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi>> {
        // Leadership sees full trail: status=all returns pending + approved + rejected + closed
        return observeQueue(
            category = category,
            status = "all", // Backend's no-filter sentinel (handler.statusAll)
            limit = windowSize,
        ).map { resource ->
            resource.data?.items?.mapIndexed { index, item ->
                sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi(
                    id = item.itemId,
                    title = item.subjectLabel?.ifEmpty { "Proof ${index + 1}" } ?: "Proof ${index + 1}",
                    status = item.status,
                    statusLabel = formatStatus(item.status),
                    statusTone = when (item.status.lowercase()) {
                        "pending_verification", "pending" -> "neutral"
                        "approved" -> "success"
                        "rework" -> "error"
                        "closed" -> "neutral"
                        else -> "neutral"
                    },
                    timestamp = item.capturedAt?.takeIf { it.isNotEmpty() }
                        ?.let { formatCapturedAt(it) } ?: "Unknown time",
                    proofCount = (item.media.size).coerceAtLeast(1),
                    videoUrls = item.media.map { it.downloadUrl },
                    summary = item.subjectLabel.orEmpty(), // Use backend copy, no composition
                    // The verifier's reason for sending it back. Backend-authored, rendered
                    // verbatim -- leadership saw "rework" with no explanation without it.
                    verdictReason = item.verdictReason?.takeIf { it.isNotBlank() },
                )
            }.orEmpty()
        }
    }

    override fun observeLeadershipTitle(category: String?, windowSize: Int): Flow<String> =
        observeQueue(category = category, status = "all", limit = windowSize)
            .map { it.data?.filterOptions?.moduleLabel.orEmpty() }

    override suspend fun refreshLeadershipVideos(
        category: String?,
        windowSize: Int,
        reset: Boolean,
    ): AppResult<Unit> =
        // refreshQueue is runCatching-based: it NEVER throws, it returns the failure. Discarding
        // that Result reported success on every failed fetch, so the gallery rendered its empty
        // state with no error while nothing was ever cached. Propagate it.
        refreshQueue(category = category, status = "all", limit = windowSize).fold(
            onSuccess = { AppResult.Ok(Unit) },
            onFailure = { AppResult.Err("Failed to refresh leadership videos", it) },
        )

    private fun formatStatus(status: String): String = when (status.lowercase()) {
        "pending_verification", "pending" -> "Pending Review"
        "approved" -> "Approved"
        "rejected" -> "Sent back"
        "rework" -> "Needs Rework"
        "closed" -> "Closed"
        else -> status
    }

    /**
     * Farm-readable capture time in IST. A raw ISO instant ("2026-08-05T22:09:49.971Z") is
     * machine copy and reached a leadership screen; Goat OS business meaning is always
     * Asia/Kolkata, never UTC. Unparseable input falls back to the original string rather than
     * blanking the row.
     */
    private fun formatCapturedAt(raw: String): String = runCatching {
        java.time.Instant.parse(raw)
            .atZone(java.time.ZoneId.of("Asia/Kolkata"))
            .format(java.time.format.DateTimeFormatter.ofPattern("d MMM yyyy, h:mm a"))
    }.getOrElse { raw }
}

class VerificationQueueCursorException(message: String) : IllegalStateException(message)

/** Retain at most three 20-row pages so manual infinite scroll remains useful without allowing
 * the JSON cache row to grow for the lifetime of the verifier session. Older rows are evicted as
 * the keyset window advances; a refresh restores the first page. */
private const val MAX_CACHED_VERIFICATION_QUEUE_ITEMS = 60

internal fun mergeVerificationQueuePage(
    current: VerificationQueueResponseDto,
    page: VerificationQueueResponseDto,
): VerificationQueueResponseDto {
    val boundedItems = (current.items + page.items)
        .distinctBy { it.itemId }
        .takeLast(MAX_CACHED_VERIFICATION_QUEUE_ITEMS)
    return page.copy(items = boundedItems)
}

private const val VERIFY_QUEUE_CACHE_PREFIX = "verify-queue"

private fun VerificationQueueResponseDto.removeItem(itemId: String): VerificationQueueResponseDto? {
    if (items.none { it.itemId == itemId }) return null
    val remaining = items.filterNot { it.itemId == itemId }
    val remainingParkIds = remaining.mapNotNull { it.parkId }.toSet()
    val remainingShedIds = remaining.mapNotNull { it.shedId }.toSet()
    return copy(
        items = remaining,
        filterOptions = filterOptions.copy(
            parks = filterOptions.parks.orEmpty().filter { it.id in remainingParkIds },
            sheds = filterOptions.sheds.orEmpty().filter { it.id in remainingShedIds },
        ),
    )
}
