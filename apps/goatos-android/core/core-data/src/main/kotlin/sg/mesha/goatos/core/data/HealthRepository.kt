package sg.mesha.goatos.core.data

import androidx.paging.ExperimentalPagingApi
import androidx.paging.LoadType
import androidx.paging.Pager
import androidx.paging.PagingConfig
import androidx.paging.PagingData
import androidx.paging.PagingState
import androidx.paging.RemoteMediator
import androidx.paging.map
import androidx.room.withTransaction
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.HealthDiagnosisQueueItemEntity
import sg.mesha.goatos.core.data.cache.HealthDiagnosisQueueKeyEntity
import sg.mesha.goatos.core.data.cache.HealthDiagnosisRunEntity
import sg.mesha.goatos.core.data.cache.HealthPageMetaEntity
import sg.mesha.goatos.core.data.cache.HealthRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.HealthWorkItemDetailEntity
import sg.mesha.goatos.core.data.cache.HealthWorkItemEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.HealthDiagnosisProposalResponseDto
import sg.mesha.goatos.core.network.dto.HealthDiagnosisQueueItemDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemDetailDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemPageDto

const val HEALTH_PAGE_SIZE = 20
private const val HEALTH_MAX_MEMORY_ROWS = HEALTH_PAGE_SIZE * 3
private const val HEALTH_RETAINED_SCOPES = 12
private const val HEALTH_RETAINED_DETAILS = 80

data class HealthFilters(
    val ageBand: String,
    val date: String,
    val status: String = "",
    val diseaseKey: String = "",
    val parkId: String = "",
    val shedId: String = "",
    val session: String = "",
) {
    companion object

    val scopeKey: String
        get() = listOf(ageBand, date, status, diseaseKey, parkId, shedId, session)
            .joinToString("|") { it.trim().lowercase() }
}

interface HealthRepository {
    fun workItems(filters: HealthFilters): Flow<PagingData<HealthWorkItemDto>>
    fun observePageMeta(filters: HealthFilters): Flow<HealthWorkItemPageDto?>
    fun observeDetail(healthSessionId: String): Flow<HealthWorkItemDetailDto?>
    suspend fun refreshWorkItems(filters: HealthFilters): Result<Unit>
    suspend fun refreshCaseOptions(ageBand: String, date: String): Result<Unit>
    suspend fun refreshDetail(healthSessionId: String): Result<Unit>
    suspend fun markCompleted(healthSessionId: String)
    suspend fun reconcileSuccessfulTreatmentCompletion(healthSessionId: String): Result<Unit>
    suspend fun reconcileRejectedTreatmentCompletion(healthSessionId: String): Result<Unit>

    /**
     * The register's assessment of one animal, read from Room.
     *
     * Carries the animal's NAME alongside the proposal. The proposal itself has no
     * room for it -- it is about findings, not identity -- and the screen must
     * never fall back to a uuid.
     */
    fun observeDiagnosisRun(diagnosisRunId: String): Flow<CachedDiagnosisRun?>

    /** Re-reads one run from the server. The server is the authority on its status. */
    suspend fun refreshDiagnosisRun(diagnosisRunId: String): Result<Unit>

    /**
     * The Health Director's queue: assessments awaiting a decision, newest first.
     *
     * A keyset page, Room-backed, so the Director opening it in a shed sees their
     * cached queue rather than a blank wall.
     */
    fun diagnosisQueue(filters: DiagnosisQueueFilters): Flow<PagingData<HealthDiagnosisQueueItemDto>>

    /** Whether this user may decide, and whether the queue has been read at all. */
    fun observeDiagnosisQueueMeta(filters: DiagnosisQueueFilters): Flow<DiagnosisQueueMeta?>
}

/**
 * One cached assessment: who it is about, and what the register made of it.
 *
 * The name is a separate field rather than something read off the proposal,
 * because the proposal is about FINDINGS and holds no identity at all.
 */
data class CachedDiagnosisRun(
    val goatDisplayId: String,
    val proposal: HealthDiagnosisProposalResponseDto,
)

/**
 * Which slice of the queue to show.
 *
 * Status is a string rather than an enum so the server stays the authority on what
 * statuses exist; blank means the server's own default (work awaiting a decision).
 */
data class DiagnosisQueueFilters(
    val status: String = "",
    val goatId: String = "",
) {
    val scopeKey: String
        get() = listOf(status, goatId).joinToString("|") { it.trim().lowercase() }
}

/** Page-level facts that are not rows. */
data class DiagnosisQueueMeta(
    /** Backend-owned: whether THIS user may decide any of it. Never inferred from a role. */
    val mayConfirm: Boolean,
    val endReached: Boolean,
)

class DefaultHealthRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : HealthRepository {

    @OptIn(ExperimentalPagingApi::class)
    override fun workItems(filters: HealthFilters): Flow<PagingData<HealthWorkItemDto>> = Pager(
        config = PagingConfig(
            pageSize = HEALTH_PAGE_SIZE,
            initialLoadSize = HEALTH_PAGE_SIZE,
            prefetchDistance = 3,
            enablePlaceholders = false,
            maxSize = HEALTH_MAX_MEMORY_ROWS,
        ),
        remoteMediator = HealthRemoteMediator(filters, api, database, json, clock),
        pagingSourceFactory = { database.healthWorkItemDao().pagingSource(filters.scopeKey) },
    ).flow.map { page ->
        page.map { json.decodeFromString<HealthWorkItemDto>(it.dtoJson) }
    }.flowOn(Dispatchers.Default)

    override fun observePageMeta(filters: HealthFilters): Flow<HealthWorkItemPageDto?> =
        database.healthPageMetaDao().observe(filters.scopeKey).map { entity ->
            entity?.let { runCatching { json.decodeFromString<HealthWorkItemPageDto>(it.dtoJson) }.getOrNull() }
        }.flowOn(Dispatchers.Default)

    override fun observeDetail(healthSessionId: String): Flow<HealthWorkItemDetailDto?> =
        database.healthWorkItemDetailDao().observe(healthSessionId).map { entity ->
            entity?.let { runCatching { json.decodeFromString<HealthWorkItemDetailDto>(it.dtoJson) }.getOrNull() }
        }.flowOn(Dispatchers.Default)

    override fun observeDiagnosisRun(diagnosisRunId: String): Flow<CachedDiagnosisRun?> =
        database.healthDiagnosisRunDao().observe(diagnosisRunId).map { entity ->
            val row = entity ?: return@map null
            // exception:exempt cached-read fallback; an undecodable cached blob emits null so the
            // screen shows its loading state and the refresh already in flight replaces the row.
            val proposal = runCatching {
                json.decodeFromString<HealthDiagnosisProposalResponseDto>(row.dtoJson)
            }.getOrNull() ?: return@map null
            CachedDiagnosisRun(goatDisplayId = row.goatDisplayId, proposal = proposal)
        }.flowOn(Dispatchers.Default)

    @OptIn(ExperimentalPagingApi::class)
    override fun diagnosisQueue(filters: DiagnosisQueueFilters): Flow<PagingData<HealthDiagnosisQueueItemDto>> = Pager(
        config = PagingConfig(
            pageSize = HEALTH_PAGE_SIZE,
            initialLoadSize = HEALTH_PAGE_SIZE,
            prefetchDistance = 3,
            enablePlaceholders = false,
            maxSize = HEALTH_MAX_MEMORY_ROWS,
        ),
        remoteMediator = DiagnosisQueueRemoteMediator(filters, api, database, json, clock),
        pagingSourceFactory = { database.healthDiagnosisQueueDao().pagingSource(filters.scopeKey) },
    ).flow.map { page ->
        page.map { json.decodeFromString<HealthDiagnosisQueueItemDto>(it.dtoJson) }
    }.flowOn(Dispatchers.Default)

    override fun observeDiagnosisQueueMeta(filters: DiagnosisQueueFilters): Flow<DiagnosisQueueMeta?> =
        database.healthDiagnosisQueueKeyDao().observe(filters.scopeKey).map { entity ->
            entity?.let { DiagnosisQueueMeta(mayConfirm = it.mayConfirm, endReached = it.endReached) }
        }.flowOn(Dispatchers.Default)

    override suspend fun refreshDiagnosisRun(diagnosisRunId: String): Result<Unit> = runCatching {
        val run = api.getHealthObservation(diagnosisRunId)
        val dao = database.healthDiagnosisRunDao()
        val cached = dao.get(diagnosisRunId)
        dao.upsert(
            HealthDiagnosisRunEntity(
                diagnosisRunId = diagnosisRunId,
                goatId = run.goatId,
                // The SERVER owns the animal's name. It used to fall back to whatever this
                // device had cached, which is empty on the Director's phone -- they never saw
                // the submit response -- so the assessment header rendered blank.
                goatDisplayId = run.goatDisplayId.ifBlank { cached?.goatDisplayId.orEmpty() },
                status = run.status,
                observedAtMs = cached?.observedAtMs ?: clock(),
                dtoJson = json.encodeToString(
                    HealthDiagnosisProposalResponseDto(
                        diagnosisRunId = run.diagnosisRunId,
                        status = run.status,
                        proposal = run.proposal,
                        confirmable = run.confirmable,
                        mayConfirm = run.mayConfirm,
                    ),
                ),
                updatedAt = clock(),
            ),
        )
    }

    override suspend fun refreshWorkItems(filters: HealthFilters): Result<Unit> = runCatching {
        val response = api.listHealthWorkItems(
            ageBand = filters.ageBand,
            date = filters.date,
            status = filters.status.ifBlank { null },
            diseaseKey = filters.diseaseKey.ifBlank { null },
            parkId = filters.parkId.ifBlank { null },
            shedId = filters.shedId.ifBlank { null },
            session = filters.session.ifBlank { null },
            cursor = null,
            limit = HEALTH_PAGE_SIZE,
        )
        val now = clock()
        database.withTransaction {
            val key = filters.scopeKey
            val itemDao = database.healthWorkItemDao()
            itemDao.deleteScope(key)
            database.healthRemoteKeyDao().delete(key)
            itemDao.upsertAll(response.items.mapIndexed { index, item ->
                HealthWorkItemEntity(key, item.healthSessionId, index, json.encodeToString(item), now)
            })
            database.healthRemoteKeyDao().upsert(
                HealthRemoteKeyEntity(key, response.nextCursor, response.nextCursor == null, now),
            )
            database.healthPageMetaDao().upsert(
                HealthPageMetaEntity(key, json.encodeToString(response.copy(items = emptyList())), now),
            )
            itemDao.deleteOutsideNewestScopes(HEALTH_RETAINED_SCOPES)
            database.healthRemoteKeyDao().deleteOutsideNewestScopes(HEALTH_RETAINED_SCOPES)
        }
    }

    override suspend fun refreshCaseOptions(ageBand: String, date: String): Result<Unit> = runCatching {
        val filters = HealthFilters(ageBand = ageBand, date = date)
        val page = api.listHealthWorkItems(
            ageBand = ageBand,
            date = date,
            status = null,
            diseaseKey = null,
            parkId = null,
            shedId = null,
            session = null,
            cursor = null,
            limit = 1,
        )
        database.healthPageMetaDao().upsert(
            HealthPageMetaEntity(filters.scopeKey, json.encodeToString(page.copy(items = emptyList())), clock()),
        )
    }

    override suspend fun refreshDetail(healthSessionId: String): Result<Unit> = runCatching {
        val detail = api.getHealthWorkItem(healthSessionId)
        val now = clock()
        database.withTransaction {
            val detailDao = database.healthWorkItemDetailDao()
            detailDao.upsert(
                HealthWorkItemDetailEntity(healthSessionId, json.encodeToString(detail), now),
            )
            detailDao.deleteOldestBeyond(HEALTH_RETAINED_DETAILS)

            val canonicalItem = detail.toWorkItemDto()
            val itemDao = database.healthWorkItemDao()
            val cachedRows = itemDao.findAll(healthSessionId)
            val rows = cachedRows.filter { it.scopeAccepts(detail.status) }.map { row ->
                row.copy(dtoJson = json.encodeToString(canonicalItem), updatedAt = now)
            }
            if (rows.isNotEmpty()) itemDao.upsertAll(rows)
            cachedRows.filterNot { it.scopeAccepts(detail.status) }.forEach { row ->
                itemDao.delete(row.scopeKey, healthSessionId)
            }
        }
    }

    override suspend fun markCompleted(healthSessionId: String) {
        val now = clock()
        database.withTransaction {
            val itemDao = database.healthWorkItemDao()
            val rows = itemDao.findAll(healthSessionId).mapNotNull { row ->
                runCatching { json.decodeFromString<HealthWorkItemDto>(row.dtoJson) }.getOrNull()
                    ?.copy(status = "completed")
                    ?.let { row.copy(dtoJson = json.encodeToString(it), updatedAt = now) }
            }
            if (rows.isNotEmpty()) itemDao.upsertAll(rows)

            val detailDao = database.healthWorkItemDetailDao()
            val completedDetail = detailDao.get(healthSessionId)?.let { row ->
                runCatching { json.decodeFromString<HealthWorkItemDetailDto>(row.dtoJson) }.getOrNull()
                    ?.copy(status = "completed")
                    ?.let { row.copy(dtoJson = json.encodeToString(it), updatedAt = now) }
            }
            if (completedDetail != null) detailDao.upsert(completedDetail)
        }
    }

    override suspend fun reconcileSuccessfulTreatmentCompletion(healthSessionId: String): Result<Unit> =
        reconcileTreatmentCompletion(healthSessionId)

    override suspend fun reconcileRejectedTreatmentCompletion(healthSessionId: String): Result<Unit> {
        val cachedDetailCompleted = database.healthWorkItemDetailDao().get(healthSessionId)
            ?.let { entity ->
                runCatching { json.decodeFromString<HealthWorkItemDetailDto>(entity.dtoJson) }
                    .onFailure { android.util.Log.w("HealthRepository", "failed to decode cached treatment detail", it) }
                    .getOrNull()
            }
            ?.status.equals("completed", ignoreCase = true)
        val cachedListCompleted = database.healthWorkItemDao().findAll(healthSessionId).any { row ->
            runCatching { json.decodeFromString<HealthWorkItemDto>(row.dtoJson) }
                .onFailure { android.util.Log.w("HealthRepository", "failed to decode cached treatment row", it) }
                .getOrNull()
                ?.status.equals("completed", ignoreCase = true)
        }
        return if (cachedDetailCompleted || cachedListCompleted) {
            reconcileTreatmentCompletion(healthSessionId)
        } else {
            Result.success(Unit)
        }
    }

    private suspend fun reconcileTreatmentCompletion(healthSessionId: String): Result<Unit> {
        val cachedScopes = database.healthWorkItemDao().findAll(healthSessionId)
            .mapNotNull { HealthFilters.fromScopeKey(it.scopeKey) }
        val detailRefresh = refreshDetail(healthSessionId)
        if (detailRefresh.isFailure) return detailRefresh
        val detail = database.healthWorkItemDetailDao().get(healthSessionId)
            ?.let { entity ->
                runCatching { json.decodeFromString<HealthWorkItemDetailDto>(entity.dtoJson) }
                    .onFailure { android.util.Log.w("HealthRepository", "failed to decode refreshed treatment detail", it) }
                    .getOrNull()
            }
            ?: return Result.failure(IllegalStateException("Health detail missing after refresh: $healthSessionId"))
        val scopes = buildList {
            addAll(cachedScopes)
            add(HealthFilters(ageBand = detail.ageBand, date = detail.businessDate))
            if (detail.diseaseKey.isNotBlank()) {
                add(HealthFilters(ageBand = detail.ageBand, date = detail.businessDate, diseaseKey = detail.diseaseKey))
            }
        }.distinctBy(HealthFilters::scopeKey)
        scopes.forEach { filters ->
            val result = refreshWorkItems(filters)
            if (result.isFailure) return result
        }
        return Result.success(Unit)
    }
}

private fun HealthWorkItemEntity.scopeAccepts(status: String): Boolean {
    val statusFilter = scopeKey.split('|', limit = 7).getOrNull(2).orEmpty()
    return statusFilter.isBlank() || statusFilter.equals(status, ignoreCase = true)
}

private fun HealthFilters.Companion.fromScopeKey(scopeKey: String): HealthFilters? {
    val parts = scopeKey.split('|', limit = 7)
    if (parts.size != 7) return null
    return HealthFilters(
        ageBand = parts[0],
        date = parts[1],
        status = parts[2],
        diseaseKey = parts[3],
        parkId = parts[4],
        shedId = parts[5],
        session = parts[6],
    )
}

private fun HealthWorkItemDetailDto.toWorkItemDto() = HealthWorkItemDto(
    healthSessionId = healthSessionId,
    caseId = caseId,
    goatId = goatId,
    goatDisplayId = goatDisplayId,
    diseaseKey = diseaseKey,
    diseaseName = diseaseName,
    ageBand = ageBand,
    dayNo = dayNo,
    durationDays = durationDays,
    businessDate = businessDate,
    session = session,
    dueAt = dueAt,
    status = status,
    parkId = parkId,
    parkLabel = parkLabel,
    shedId = shedId,
    shedLabel = shedLabel,
    stepCount = stepCount,
    medicationCount = medicationCount,
    hasCriticalStep = hasCriticalStep,
)

@OptIn(ExperimentalPagingApi::class)
private class HealthRemoteMediator(
    private val filters: HealthFilters,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, HealthWorkItemEntity>() {
    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, HealthWorkItemEntity>,
    ): MediatorResult = try {
        if (loadType == LoadType.PREPEND) return MediatorResult.Success(endOfPaginationReached = true)
        val key = filters.scopeKey
        val cursor = when (loadType) {
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remote = database.healthRemoteKeyDao().get(key)
                if (remote?.endReached == true) return MediatorResult.Success(true)
                remote?.nextCursor
            }
            LoadType.PREPEND -> null
        }
        val response = api.listHealthWorkItems(
            ageBand = filters.ageBand,
            date = filters.date,
            status = filters.status.ifBlank { null },
            diseaseKey = filters.diseaseKey.ifBlank { null },
            parkId = filters.parkId.ifBlank { null },
            shedId = filters.shedId.ifBlank { null },
            session = filters.session.ifBlank { null },
            cursor = cursor,
            limit = HEALTH_PAGE_SIZE,
        )
        val now = clock()
        database.withTransaction {
            val itemDao = database.healthWorkItemDao()
            if (loadType == LoadType.REFRESH) {
                itemDao.deleteScope(key)
                database.healthRemoteKeyDao().delete(key)
            }
            val base = if (loadType == LoadType.APPEND) itemDao.count(key) else 0
            itemDao.upsertAll(response.items.mapIndexed { index, item ->
                HealthWorkItemEntity(key, item.healthSessionId, base + index, json.encodeToString(item), now)
            })
            database.healthRemoteKeyDao().upsert(
                HealthRemoteKeyEntity(key, response.nextCursor, response.nextCursor == null, now),
            )
            database.healthPageMetaDao().upsert(
                HealthPageMetaEntity(key, json.encodeToString(response.copy(items = emptyList())), now),
            )
            itemDao.deleteOutsideNewestScopes(HEALTH_RETAINED_SCOPES)
            database.healthRemoteKeyDao().deleteOutsideNewestScopes(HEALTH_RETAINED_SCOPES)
        }
        MediatorResult.Success(response.nextCursor == null)
    } catch (cancellation: CancellationException) {
        throw cancellation
    } catch (t: Throwable) {
        MediatorResult.Error(t)
    }
}

/**
 * Fills the Director's queue from the server, one keyset page at a time.
 *
 * REFRESH clears the scope and starts from the head; APPEND continues from the
 * server-issued cursor. There is no PREPEND: the queue is newest-first and new
 * work arriving at the head is picked up by the next refresh, not by paging
 * backwards past rows the Director has already scrolled through.
 */
@OptIn(ExperimentalPagingApi::class)
private class DiagnosisQueueRemoteMediator(
    private val filters: DiagnosisQueueFilters,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, HealthDiagnosisQueueItemEntity>() {
    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, HealthDiagnosisQueueItemEntity>,
    ): MediatorResult = try {
        if (loadType == LoadType.PREPEND) return MediatorResult.Success(endOfPaginationReached = true)
        val key = filters.scopeKey
        val cursor = when (loadType) {
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remote = database.healthDiagnosisQueueKeyDao().get(key)
                if (remote?.endReached == true) return MediatorResult.Success(true)
                remote?.nextCursor
            }
            LoadType.PREPEND -> null
        }
        val response = api.listHealthObservations(
            status = filters.status.ifBlank { null },
            goatId = filters.goatId.ifBlank { null },
            cursor = cursor,
            limit = HEALTH_PAGE_SIZE,
        )
        val now = clock()
        database.withTransaction {
            val itemDao = database.healthDiagnosisQueueDao()
            if (loadType == LoadType.REFRESH) {
                itemDao.deleteScope(key)
                database.healthDiagnosisQueueKeyDao().delete(key)
            }
            // The row's position is the BACKEND's order, offset by what is already
            // cached on an append. Re-deriving it locally would re-rank a list the
            // server ranked by severity.
            val base = if (loadType == LoadType.APPEND) itemDao.count(key) else 0
            itemDao.upsertAll(
                response.items.mapIndexed { index, item ->
                    HealthDiagnosisQueueItemEntity(
                        scopeKey = key,
                        diagnosisRunId = item.diagnosisRunId,
                        sortIndex = base + index,
                        dtoJson = json.encodeToString(item),
                        updatedAt = now,
                    )
                },
            )
            database.healthDiagnosisQueueKeyDao().upsert(
                HealthDiagnosisQueueKeyEntity(
                    scopeKey = key,
                    nextCursor = response.nextCursor,
                    endReached = response.nextCursor == null,
                    mayConfirm = response.mayConfirm,
                    updatedAt = now,
                ),
            )
            itemDao.deleteOutsideNewestScopes(HEALTH_RETAINED_SCOPES)
            database.healthDiagnosisQueueKeyDao().deleteOutsideNewestScopes(HEALTH_RETAINED_SCOPES)
        }
        MediatorResult.Success(response.nextCursor == null)
    } catch (cancellation: CancellationException) {
        throw cancellation
    } catch (t: Throwable) {
        MediatorResult.Error(t)
    }
}
