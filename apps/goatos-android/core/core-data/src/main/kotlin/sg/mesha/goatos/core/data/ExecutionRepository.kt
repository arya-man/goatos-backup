package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheEntity
import sg.mesha.goatos.core.data.cache.ScanRosterCacheDao
import sg.mesha.goatos.core.data.cache.ScanRosterCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
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
        taskId: String,
        cursor: String? = null,
        limit: Int? = null,
    ): ScanRosterResponseDto

    /** Cache-first stream for this shed's scan roster. */
    fun observeScanRoster(
        shedId: String,
        taskId: String,
        limit: Int? = null,
    ): Flow<Resource<ScanRosterResponseDto>>

    /** Fetches and upserts Room on success; leaves the cache untouched on failure. */
    suspend fun refreshScanRoster(
        shedId: String,
        taskId: String,
        limit: Int? = null,
    ): Result<Unit>

    /** Appends exactly the current server continuation page to the Room-backed scope. */
    suspend fun appendScanRoster(
        shedId: String,
        taskId: String,
        cursor: String,
        limit: Int? = null,
    ): Result<Unit>
}

class DefaultExecutionRepository(
    private val api: AppApi,
    private val rowsDao: ExecutionRowsCacheDao,
    private val shedDao: ExecutionShedCacheDao,
    private val scanRosterDao: ScanRosterCacheDao,
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
    ): Flow<Resource<VaccinationExecutionResponseDto>> =
        rowsDao.observe(cacheKey(parkId, workState, asOf, dueBefore, limit?.toString()))
            .map { it.toResource() }
            .flowOn(Dispatchers.Default)

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
            val current = rowsDao.get(key)
                ?.let { json.decodeFromString<VaccinationExecutionResponseDto>(it.dtoJson) }
                ?: throw ExecutionRowsCursorException("execution continuation has no cached first page")
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
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> =
        shedDao.observe(cacheKey(shedId, asOf, dueBefore, limit?.toString()))
            .map { it.toResource() }
            .flowOn(Dispatchers.Default)

    override suspend fun refreshShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        val dto = shed(shedId, asOf, dueBefore, limit)
        val key = cacheKey(shedId, asOf, dueBefore, limit?.toString())
        shedDao.upsert(ExecutionShedCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
    }

    override suspend fun scanRoster(
        shedId: String,
        taskId: String,
        cursor: String?,
        limit: Int?,
    ): ScanRosterResponseDto = api.getScanRoster(shedId, taskId, cursor, limit)

    override fun observeScanRoster(
        shedId: String,
        taskId: String,
        limit: Int?,
    ): Flow<Resource<ScanRosterResponseDto>> =
        scanRosterDao.observe(scanRosterScopeKey(shedId, taskId, limit))
            .map { it.toResource() }
            .flowOn(Dispatchers.Default)

    override suspend fun refreshScanRoster(
        shedId: String,
        taskId: String,
        limit: Int?,
    ): Result<Unit> = runCatching {
        val dto = scanRoster(shedId, taskId, cursor = null, limit = limit)
        val key = scanRosterScopeKey(shedId, taskId, limit)
        scanRosterDao.upsert(ScanRosterCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
    }

    override suspend fun appendScanRoster(
        shedId: String,
        taskId: String,
        cursor: String,
        limit: Int?,
    ): Result<Unit> = runCatching {
        scanAppendMutex.withLock {
            val key = scanRosterScopeKey(shedId, taskId, limit)
            val current = scanRosterDao.get(key)
                ?.let { json.decodeFromString<ScanRosterResponseDto>(it.dtoJson) }
                ?: throw ScanRosterCursorException("scan roster continuation has no cached first page")
            if (current.nextCursor != cursor) {
                throw ScanRosterCursorException("scan roster cursor is stale or belongs to another task")
            }
            val page = scanRoster(shedId, taskId, cursor = cursor, limit = limit)
            if (page.nextCursor == cursor) {
                throw ScanRosterCursorException("scan roster backend returned a non-advancing cursor")
            }
            val merged = mergeScanRosterPage(current, page)
            scanRosterDao.upsert(
                ScanRosterCacheEntity(cacheKey = key, dtoJson = json.encodeToString(merged), updatedAt = clock()),
            )
        }
    }

    private fun ExecutionRowsCacheEntity?.toResource(): Resource<VaccinationExecutionResponseDto> =
        Resource(
            data = this?.let { runCatching { json.decodeFromString<VaccinationExecutionResponseDto>(it.dtoJson) }.getOrNull() },
            lastSyncedAt = this?.updatedAt,
        )

    private fun ExecutionShedCacheEntity?.toResource(): Resource<VaccinationExecutionShedDrilldownDto> =
        Resource(
            data = this?.let { runCatching { json.decodeFromString<VaccinationExecutionShedDrilldownDto>(it.dtoJson) }.getOrNull() },
            lastSyncedAt = this?.updatedAt,
        )

    private fun ScanRosterCacheEntity?.toResource(): Resource<ScanRosterResponseDto> =
        Resource(
            data = this?.let { runCatching { json.decodeFromString<ScanRosterResponseDto>(it.dtoJson) }.getOrNull() },
            lastSyncedAt = this?.updatedAt,
        )

    private fun scanRosterScopeKey(shedId: String, taskId: String, limit: Int?): String =
        cacheKey(shedId, taskId, limit?.toString())
}

class ScanRosterCursorException(message: String) : IllegalStateException(message)
class ExecutionRowsCursorException(message: String) : IllegalStateException(message)

internal fun mergeExecutionRowsPage(
    current: VaccinationExecutionResponseDto,
    page: VaccinationExecutionResponseDto,
): VaccinationExecutionResponseDto = page.copy(
    totalCount = maxOf(current.totalCount, page.totalCount),
    rows = (current.rows + page.rows).distinctBy { row ->
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
    rows = (current.rows + page.rows).distinctBy { row ->
        row.obligationId.takeIf { it.isNotBlank() }
            ?: listOf(row.goatId, row.primaryTag, row.vaccineLabel).joinToString("|")
    },
)
