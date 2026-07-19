package sg.mesha.goatos.core.data

import androidx.room.withTransaction
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheEntity
import sg.mesha.goatos.core.data.cache.ScanRosterCacheDao
import sg.mesha.goatos.core.data.cache.ScanRosterCacheEntity
import sg.mesha.goatos.core.data.cache.ScanRosterRowDao
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.StatusCount
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto

/**
 * Vaccination execution screen area: the execution row list, the per-shed
 * drilldown, and the per-animal scan roster. Offline-first
 * (docs/decisions/android-offline-first.md): Room is the UI's single source of truth for all
 * three reads, each backed by its own cache table ([ExecutionRowsCacheDao] / [ExecutionShedCacheDao] /
 * [ScanRosterCacheDao]). observeX is cache-first and reactive; refreshX is the network side of
 * stale-while-revalidate — it upserts Room on success and leaves the cache untouched on
 * failure. rows/shed/scanRoster are kept as the plain network calls refreshX wraps.
 * DTO -> UiState mapping stays in :app.
 */
interface ExecutionRepository {
    suspend fun rows(
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): VaccinationExecutionResponseDto

    /** Cache-first stream for this filter scope: emits immediately with whatever Room has
     *  (null data on a cold cache) and re-emits after every successful [refreshRows]. */
    fun observeRows(
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): Flow<Resource<VaccinationExecutionResponseDto>>

    /** Fetches and upserts Room on success; on failure returns the failure and leaves the
     *  cache untouched — the caller surfaces stale/offline, never a blank screen. */
    suspend fun refreshRows(
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    /** Appends the next execution page into the same Room-backed first-page scope. */
    suspend fun appendRows(
        cursor: String,
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    suspend fun shed(
        shedId: String,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): VaccinationExecutionShedDrilldownDto

    /** Cache-first stream for this shed drilldown scope. */
    fun observeShed(
        shedId: String,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>>

    /** Fetches and upserts Room on success; leaves the cache untouched on failure. */
    suspend fun refreshShed(
        shedId: String,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    /** Per-animal scan roster (RFID tags + due vaccine) for a shed. */
    suspend fun scanRoster(
        shedId: String,
        taskId: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): ScanRosterResponseDto

    /** Cache-first stream for this shed's scan roster. */
    fun observeScanRoster(
        shedId: String,
        taskId: String? = null,
        limit: Int? = null,
    ): Flow<Resource<ScanRosterResponseDto>>

    /** Fetches and upserts Room on success; leaves the cache untouched on failure. */
    suspend fun refreshScanRoster(
        shedId: String,
        taskId: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    /** Fetches every bounded scan-roster page for this shed/task and keeps the merged roster in
     *  Room. The BLE screen needs a complete local tag index before accepting hardware reads;
     *  otherwise a valid goat on page 2 can be misclassified as an unknown tag. */
    suspend fun refreshCompleteScanRoster(
        shedId: String,
        taskId: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    /** Appends exactly the current server continuation page to the Room-backed scope. */
    suspend fun appendScanRoster(
        shedId: String,
        taskId: String? = null,
        cursor: String,
        limit: Int? = null,
    ): Result<Unit>

    /** R50-007: Find a roster row by shed and normalized tag (searches the full roster, not just
     *  the loaded page). Returns null if the tag is not found in this shed. */
    suspend fun findScanRosterByTag(shedId: String, taskId: String?, normalizedTag: String): ScanRosterRowEntity?

    /** R50-008: Get status-based counts for a shed (full roster, independent of loaded page). */
    suspend fun getScanRosterStatusCounts(shedId: String, taskId: String?): List<StatusCount>

    /** R50-008: Observable full-roster status aggregates for a shed. Re-emits on every roster
     *  upsert; the ViewModel combines this with the paged roster so counters are identical for
     *  page size 1 and 20. */
    fun observeScanRosterStatusCounts(shedId: String, taskId: String?): Flow<List<StatusCount>>

    /** R50-008: Status aggregates for a bounded obligation-id set (the local unsynced overlay). */
    suspend fun getScanRosterStatusCountsFor(shedId: String, taskId: String?, obligationIds: List<String>): List<StatusCount>
}

class DefaultExecutionRepository(
    private val api: AppApi,
    private val rowsDao: ExecutionRowsCacheDao,
    private val shedDao: ExecutionShedCacheDao,
    private val scanRosterDao: ScanRosterCacheDao,
    private val scanRosterRowDao: ScanRosterRowDao,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : ExecutionRepository {
    private val rowsAppendMutex = Mutex()
    private val scanAppendMutex = Mutex()
    override suspend fun rows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
        cursor: String?,
    ): VaccinationExecutionResponseDto =
        api.listVaccinationExecution(parkId, workState, asOf, dueBefore, limit, cursor)

    override fun observeRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): Flow<Resource<VaccinationExecutionResponseDto>> {
        val key = cacheKey(parkId, workState, asOf, dueBefore, limit?.toString())
        return rowsDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        val dto = rows(parkId, workState, asOf, dueBefore, limit, cursor = null)
        val key = cacheKey(parkId, workState, asOf, dueBefore, limit?.toString())
        rowsDao.upsert(ExecutionRowsCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        rowsDao.enforceCacheBounds()
    }

    override suspend fun appendRows(
        cursor: String,
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        rowsAppendMutex.withLock {
            val key = cacheKey(parkId, workState, asOf, dueBefore, limit?.toString())
            val currentEntity = rowsDao.get(key)
            val current = readCachedJson<VaccinationExecutionResponseDto>(
                json = json,
                cacheKey = key,
                dtoJson = currentEntity?.dtoJson,
                updatedAt = currentEntity?.updatedAt,
                now = clock(),
                quarantine = { rowsDao.delete(it) },
            ).data ?: throw ExecutionRowsCursorException("execution continuation has no cached first page")
            if (current.nextCursor != cursor) {
                throw ExecutionRowsCursorException("execution cursor is stale or belongs to another filter")
            }
            val page = rows(parkId, workState, asOf, dueBefore, limit, cursor)
            if (page.nextCursor == cursor) {
                throw ExecutionRowsCursorException("execution backend returned a non-advancing cursor")
            }
            rowsDao.upsert(
                ExecutionRowsCacheEntity(
                    cacheKey = key,
                    dtoJson = json.encodeToString(mergeExecutionRowsPage(current, page)),
                    updatedAt = clock(),
                ),
            )
        }
    }

    override suspend fun shed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): VaccinationExecutionShedDrilldownDto =
        api.getVaccinationExecutionShed(shedId, asOf, dueBefore, limit)

    override fun observeShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> {
        val key = cacheKey(shedId, asOf, dueBefore, limit?.toString())
        return shedDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        val dto = shed(shedId, asOf, dueBefore, limit)
        val key = cacheKey(shedId, asOf, dueBefore, limit?.toString())
        shedDao.upsert(ExecutionShedCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        shedDao.enforceCacheBounds()
    }

    override suspend fun scanRoster(
        shedId: String,
        taskId: String?,
        cursor: String?,
        limit: Int?,
    ): ScanRosterResponseDto = api.getScanRoster(shedId, taskId, cursor, limit)

    override fun observeScanRoster(
        shedId: String,
        taskId: String?,
        limit: Int?,
    ): Flow<Resource<ScanRosterResponseDto>> {
        val key = scanRosterScopeKey(shedId, taskId, limit)
        return scanRosterDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshScanRoster(
        shedId: String,
        taskId: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        scanAppendMutex.withLock {
            val key = scanRosterScopeKey(shedId, taskId, limit)
            val rowScope = scanRosterRowScopeKey(shedId, taskId)
            // R50-007/R50-008: fetch the WHOLE shed roster keyset page-by-page, RemoteMediator-style
            // (each network page stays ~20 rows), staging rows locally so a mid-walk network failure
            // fails the whole refresh and leaves both caches untouched (atomic terminal publish).
            // Forward progress is guaranteed by seenCursors (a repeated cursor is a backend defect,
            // not silently truncated) rather than a fixed page-count cutoff.
            val first = scanRoster(shedId, taskId, cursor = null, limit = limit)
            val entities = first.rows.mapTo(mutableListOf()) { it.toRowEntity(rowScope, shedId, taskId, clock()) }
            var cursor = first.nextCursor
            val seenCursors = mutableSetOf<String>() // mobile-guard:ignore: function-local, GC'd on return; bounded by one shed's total page count, not a persistent field
            while (cursor != null) {
                if (!seenCursors.add(cursor)) {
                    throw ScanRosterCursorException("scan roster backend returned a non-advancing cursor")
                }
                val page = scanRoster(shedId, taskId, cursor = cursor, limit = limit)
                page.rows.mapTo(entities) { it.toRowEntity(rowScope, shedId, taskId, clock()) }
                cursor = page.nextCursor
            }
            // The UI blob still holds only the first ~20-row page (the observed window stays bounded,
            // paged forward by appendScanRoster as the user scrolls); the per-row SSOT holds every
            // roster row for indexed tag lookup + GROUP BY aggregates, published atomically with it.
            database.withTransaction {
                scanRosterDao.upsert(
                    ScanRosterCacheEntity(cacheKey = key, dtoJson = json.encodeToString(first), updatedAt = clock()),
                )
                scanRosterRowDao.replaceScope(rowScope, entities.distinctBy { it.id })
            }
            scanRosterDao.enforceCacheBounds()
        }
    }

    override suspend fun refreshCompleteScanRoster(
        shedId: String,
        taskId: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        scanAppendMutex.withLock {
            val key = scanRosterScopeKey(shedId, taskId, limit)
            val rowScope = scanRosterRowScopeKey(shedId, taskId)
            // The BLE screen needs a complete local tag index AND a fully rendered list before
            // accepting hardware reads, so (unlike refreshScanRoster) the observed blob itself
            // accumulates every page (mobile-guard:ignore below), staged locally and published
            // atomically at the terminal cursor together with the row-level SSOT.
            var merged = scanRoster(shedId, taskId, cursor = null, limit = limit)
            val entities = merged.rows.mapTo(mutableListOf()) { it.toRowEntity(rowScope, shedId, taskId, clock()) }
            var cursor = merged.nextCursor
            val seenCursors = mutableSetOf<String>() // mobile-guard:ignore: function-local, GC'd on return; bounded by one shed's total page count, not a persistent field
            while (cursor != null) {
                if (!seenCursors.add(cursor)) {
                    throw ScanRosterCursorException("scan roster backend returned a non-advancing cursor")
                }
                val page = scanRoster(shedId, taskId, cursor = cursor, limit = limit)
                page.rows.mapTo(entities) { it.toRowEntity(rowScope, shedId, taskId, clock()) }
                merged = mergeScanRosterPage(merged, page)
                cursor = page.nextCursor
            }
            database.withTransaction {
                scanRosterDao.upsert(
                    ScanRosterCacheEntity(cacheKey = key, dtoJson = json.encodeToString(merged), updatedAt = clock()),
                )
                scanRosterRowDao.replaceScope(rowScope, entities.distinctBy { it.id })
            }
            scanRosterDao.enforceCacheBounds()
        }
    }

    override suspend fun appendScanRoster(
        shedId: String,
        taskId: String?,
        cursor: String,
        limit: Int?,
    ): Result<Unit> = runCatching {
        scanAppendMutex.withLock {
            val key = scanRosterScopeKey(shedId, taskId, limit)
            val currentEntity = scanRosterDao.get(key)
            val current = readCachedJson<ScanRosterResponseDto>(
                json = json,
                cacheKey = key,
                dtoJson = currentEntity?.dtoJson,
                updatedAt = currentEntity?.updatedAt,
                now = clock(),
                quarantine = { scanRosterDao.delete(it) },
            ).data ?: throw ScanRosterCursorException("scan roster continuation has no cached first page")
            if (current.nextCursor != cursor) {
                throw ScanRosterCursorException("scan roster cursor is stale or belongs to another task/shed-wide scope")
            }
            val page = scanRoster(shedId, taskId, cursor = cursor, limit = limit)
            if (page.nextCursor == cursor) {
                throw ScanRosterCursorException("scan roster backend returned a non-advancing cursor")
            }
            val merged = mergeScanRosterPage(current, page)
            scanRosterDao.upsert(
                ScanRosterCacheEntity(cacheKey = key, dtoJson = json.encodeToString(merged), updatedAt = clock()),
            )
            // R50-007: keep the appended page's rows fresh in the per-row SSOT too
            val rowScope = scanRosterRowScopeKey(shedId, taskId)
            scanRosterRowDao.upsertAll(page.rows.map { it.toRowEntity(rowScope, shedId, taskId, clock()) })
        }
    }

    private suspend fun ExecutionRowsCacheEntity?.toResource(key: String): Resource<VaccinationExecutionResponseDto> {
        val cached = readCachedJson<VaccinationExecutionResponseDto>(
            json = json,
            cacheKey = key,
            dtoJson = this?.dtoJson,
            updatedAt = this?.updatedAt,
            now = clock(),
            quarantine = { rowsDao.delete(it) },
        )
        return Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
    }

    private suspend fun ExecutionShedCacheEntity?.toResource(key: String): Resource<VaccinationExecutionShedDrilldownDto> {
        val cached = readCachedJson<VaccinationExecutionShedDrilldownDto>(
            json = json,
            cacheKey = key,
            dtoJson = this?.dtoJson,
            updatedAt = this?.updatedAt,
            now = clock(),
            quarantine = { shedDao.delete(it) },
        )
        return Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
    }

    private suspend fun ScanRosterCacheEntity?.toResource(key: String): Resource<ScanRosterResponseDto> {
        val cached = readCachedJson<ScanRosterResponseDto>(
            json = json,
            cacheKey = key,
            dtoJson = this?.dtoJson,
            updatedAt = this?.updatedAt,
            now = clock(),
            quarantine = { scanRosterDao.delete(it) },
        )
        return Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
    }

    private fun scanRosterScopeKey(shedId: String, taskId: String?, limit: Int?): String =
        cacheKey(shedId, taskId ?: "shed-wide", limit?.toString())

    override suspend fun findScanRosterByTag(shedId: String, taskId: String?, normalizedTag: String): ScanRosterRowEntity? =
        scanRosterRowDao.findByTag(scanRosterRowScopeKey(shedId, taskId), normalizedTag)

    override suspend fun getScanRosterStatusCounts(shedId: String, taskId: String?): List<StatusCount> =
        scanRosterRowDao.countByStatus(scanRosterRowScopeKey(shedId, taskId))

    override fun observeScanRosterStatusCounts(shedId: String, taskId: String?): Flow<List<StatusCount>> =
        scanRosterRowDao.observeCountsByStatus(scanRosterRowScopeKey(shedId, taskId)).flowOn(Dispatchers.Default)

    override suspend fun getScanRosterStatusCountsFor(
        shedId: String,
        taskId: String?,
        obligationIds: List<String>,
    ): List<StatusCount> =
        if (obligationIds.isEmpty()) emptyList()
        else scanRosterRowDao.countByStatusForObligations(scanRosterRowScopeKey(shedId, taskId), obligationIds)
}

private fun sg.mesha.goatos.core.network.dto.ScanRosterRowDto.toRowEntity(
    scopeKey: String,
    shedId: String,
    taskId: String?,
    now: Long,
): ScanRosterRowEntity = ScanRosterRowEntity(
    id = "$scopeKey#${obligationId.ifBlank { "$goatId#$primaryTag" }}",
    scopeKey = scopeKey,
    shedId = shedId,
    taskId = taskId ?: "shed-wide",
    goatId = goatId,
    primaryTag = primaryTag,
    secondaryTag = secondaryTag,
    normalizedPrimaryTag = canonicalRosterTag(primaryTag),
    normalizedSecondaryTag = secondaryTag?.let(::canonicalRosterTag)?.takeIf { it.isNotBlank() },
    vaccineLabel = vaccineLabel,
    status = status,
    obligationId = obligationId,
    updatedAt = now,
)

private fun scanRosterRowScopeKey(shedId: String, taskId: String?): String =
    cacheKey(shedId, taskId ?: "shed-wide")

internal fun canonicalRosterTag(tag: String): String = tag.filter(Char::isLetterOrDigit).lowercase()

class ScanRosterCursorException(message: String) : IllegalStateException(message)
class ExecutionRowsCursorException(message: String) : IllegalStateException(message)

internal fun mergeExecutionRowsPage(
    current: VaccinationExecutionResponseDto,
    page: VaccinationExecutionResponseDto,
): VaccinationExecutionResponseDto = page.copy(
    totalCount = maxOf(current.totalCount, page.totalCount),
    rows = (current.rows + page.rows).distinctBy { row -> // mobile-guard:ignore: cursor-gated single-page append into a TTL+row/byte-capped blob (enforceCacheBounds)
        listOf(
            row.parkId,
            row.shedId,
            row.animalStage,
            row.driveId.orEmpty(),
            row.batchId.orEmpty(),
            row.sopTaskId.orEmpty(),
            row.obligationId.orEmpty(),
        ).joinToString("|")
    },
)

internal fun mergeScanRosterPage(
    current: ScanRosterResponseDto,
    page: ScanRosterResponseDto,
): ScanRosterResponseDto = page.copy(
    rows = (current.rows + page.rows).distinctBy { row -> // mobile-guard:ignore: capped blob (enforceCacheBounds); per-row truth lives in scan_roster_row
        row.obligationId.takeIf { it.isNotBlank() }
            ?: listOf(row.goatId, row.primaryTag, row.vaccineLabel).joinToString("|")
    },
)
