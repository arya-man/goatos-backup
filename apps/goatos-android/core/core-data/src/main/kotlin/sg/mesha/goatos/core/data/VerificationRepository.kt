package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.VerificationQueueCacheDao
import sg.mesha.goatos.core.data.cache.VerificationQueueCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto

/**
 * The standalone Verifier section's media queue (context/architecture/verifier-app-and-flow.md
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
        limit: Int? = null,
        cursor: String? = null,
    ): VerificationQueueResponseDto

    /** Cache-first stream for this category scope: emits immediately with whatever Room has
     *  (null data on a cold cache) and re-emits after every successful [refreshQueue]/[appendQueue]. */
    fun observeQueue(
        category: String? = null,
        limit: Int? = null,
    ): Flow<Resource<VerificationQueueResponseDto>>

    /** Fetches the first page and upserts Room on success; on failure returns the failure and
     *  leaves the cache untouched — the caller surfaces stale/offline, never a blank screen. */
    suspend fun refreshQueue(
        category: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    /** Appends the next keyset page into the same Room-backed category scope. */
    suspend fun appendQueue(
        cursor: String,
        category: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    fun observeActionQueue(
        category: String? = null,
        limit: Int? = null,
    ): Flow<Resource<VerificationQueueResponseDto>>

    suspend fun refreshActionQueue(
        category: String? = null,
        limit: Int? = null,
    ): Result<Unit>
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
        limit: Int?,
        cursor: String?,
    ): VerificationQueueResponseDto = api.listVerificationQueue(category, cursor, limit)

    override fun observeQueue(
        category: String?,
        limit: Int?,
    ): Flow<Resource<VerificationQueueResponseDto>> {
        val key = scopeKey(category, limit)
        return queueDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshQueue(category: String?, limit: Int?): Result<Unit> = runCatching {
        val dto = queue(category, limit, cursor = null)
        val key = scopeKey(category, limit)
        queueDao.upsert(VerificationQueueCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        queueDao.enforceCacheBounds()
    }

    override suspend fun appendQueue(cursor: String, category: String?, limit: Int?): Result<Unit> = runCatching {
        appendMutex.withLock {
            val key = scopeKey(category, limit)
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
            val page = queue(category, limit, cursor)
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
        limit: Int?,
    ): Flow<Resource<VerificationQueueResponseDto>> {
        val key = actionScopeKey(category, limit)
        return queueDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshActionQueue(category: String?, limit: Int?): Result<Unit> = runCatching {
        val dto = api.listVerificationActionQueue(category = category, cursor = null, limit = limit)
        val key = actionScopeKey(category, limit)
        queueDao.upsert(VerificationQueueCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        queueDao.enforceCacheBounds()
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

    private fun scopeKey(category: String?, limit: Int?): String = cacheKey("verify-queue", category, limit?.toString())

    private fun actionScopeKey(category: String?, limit: Int?): String =
        cacheKey("verification-action-queue", category, limit?.toString())
}

class VerificationQueueCursorException(message: String) : IllegalStateException(message)

internal fun mergeVerificationQueuePage(
    current: VerificationQueueResponseDto,
    page: VerificationQueueResponseDto,
): VerificationQueueResponseDto = page.copy(
    items = (current.items + page.items).distinctBy { it.itemId },
)
