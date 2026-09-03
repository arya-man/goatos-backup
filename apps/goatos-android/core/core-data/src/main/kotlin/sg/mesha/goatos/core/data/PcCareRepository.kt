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
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.cache.CacheGovernance
import sg.mesha.goatos.core.data.cache.PcCareAnimalRowDao
import sg.mesha.goatos.core.data.cache.PcCareAnimalRowEntity
import sg.mesha.goatos.core.data.cache.PcCareScanStatus
import sg.mesha.goatos.core.data.cache.PcCareTaskDetailCacheDao
import sg.mesha.goatos.core.data.cache.PcCareTaskDetailCacheEntity
import sg.mesha.goatos.core.data.cache.PcCareTaskItemEntity
import sg.mesha.goatos.core.data.cache.PcCareTaskRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.PcCareAnimalRowDto
import sg.mesha.goatos.core.network.dto.PcCareCreateTaskRequestDto
import sg.mesha.goatos.core.network.dto.PcCarePlannerCatalogDto
import sg.mesha.goatos.core.network.dto.PcCarePlannerShedsDto
import sg.mesha.goatos.core.network.dto.PcCareStockVerdictRequestDto
import sg.mesha.goatos.core.network.dto.PcCareTaskDto

/** One screen-page of PC Care tasks — bounds BOTH the network request and the Room window
 *  (docs/decisions/mobile-data-fetch-anti-patterns.md). */
const val PC_CARE_PAGE_SIZE = 20

/** How many distinct filter scopes keep their cached worklist rows. */
private const val PC_CARE_CACHED_QUERIES = 8

/** How many recently-touched tasks keep their scanned-animal rows (bounded eviction). */
private const val PC_CARE_CACHED_ANIMAL_TASKS = 12

/** Bounded observed window for one task's scanned-animal list — never an unbounded observeAll. */
private const val PC_CARE_ANIMAL_LIST_LIMIT = 300 // mobile-guard:ignore: hard ceiling on ONE task's scan roster (a pen holds well under 300 animals), not a screen page — the capture screen needs every scanned row to gate submit

/** Captures poll page size and page cap — write-through per page, bounded total. */
private const val PC_CARE_CAPTURES_PAGE_LIMIT = 20
private const val PC_CARE_CAPTURES_MAX_PAGES = 50

/** Roster fetch page size and page cap — one bounded pen roster, written through page by page. */
private const val PC_CARE_ROSTER_PAGE_LIMIT = 20
private const val PC_CARE_ROSTER_MAX_PAGES = 15 // mobile-guard:ignore: bounded write-through fill of ONE pen's tap roster into Room (hard 300 ceiling), mirroring PC_CARE_ANIMAL_LIST_LIMIT's rationale

/** Namespaces the roster blob beside the task-detail blob in the same bounded cache table. */
private fun rosterCacheKey(taskId: String): String = "roster:$taskId"

/** Bump whenever the cached task-row JSON changes shape incompatibly (see PACKING_CACHE_SHAPE's
 *  kdoc in FeedRepository.kt for why a stale-shape row must be orphaned, never leniently decoded). */
private const val PC_CARE_CACHE_SHAPE = "task-v1"

/**
 * Filter scope for the PC Care operator worklist: one category tab on one business date — both
 * backend query parameters of `GET /app/pc-care/worklist`.
 */
data class PcCareWorklistQuery(
    val category: String,
    val date: String,
    /**
     * False: the operator worklist (`/app/pc-care/worklist`, MY assigned tasks only).
     * True: the plan/monitor flat list (`/app/pc-care/tasks`, every task in scope) — the face a
     * category tab shows a planner/monitor. Distinct Room namespace so the caches never mix.
     */
    val monitor: Boolean = false,
) {
    fun roomKey(): String =
        cacheKey(PC_CARE_CACHE_SHAPE, if (monitor) "monitor" else "work", category, date, PC_CARE_PAGE_SIZE.toString())
}

/** Outcome of recording one scan locally. The ViewModel owns the copy; this is typed state. */
sealed interface PcCareScanOutcome {
    /** The scan is durable in Room and queued for sync. */
    data object Queued : PcCareScanOutcome

    /** This tag is already in the task's Room rows (scanned here or synced from a peer). */
    data class Duplicate(val scannedByName: String) : PcCareScanOutcome

    /** The write could not be queued (see [reason] — diagnostic, not operator copy). */
    data class Failed(val reason: String) : PcCareScanOutcome
}

/**
 * PC Care reads and writes (module pc_care, maintainer decision 2026-08-21). Offline-first per
 * docs/decisions/android-offline-first.md: Room is the UI's single source of truth — the worklist,
 * the task detail, and the scanned-animal list all render from Room, network refreshes upsert
 * Room, and every write rides the outbox.
 */
interface PcCareRepository {
    /** The paged operator worklist, a Room PagingSource filled by a RemoteMediator. */
    fun worklistRows(query: PcCareWorklistQuery): Flow<PagingData<PcCareTaskDto>>

    /**
     * Drops the freshness marker for one worklist query so the NEXT pager for it refetches from
     * the network instead of TTL-skipping. Called on an explicit refresh (sync icon, resume) and
     * after a local write that changes the list (plan-create, cancel) — a just-created task must
     * appear without waiting out the cache TTL. Cached rows keep serving until fresh rows land.
     */
    suspend fun invalidateWorklist(query: PcCareWorklistQuery)

    /** Room-first task detail; null while nothing is cached yet (corrupt/expired rows quarantine). */
    fun observeTaskDetail(taskId: String): Flow<PcCareTaskDto?>

    /**
     * LIVE lifecycle status for one task from the same Room rows the worklist renders — the
     * [sg.mesha.goatos.core.data.cache.PcCareTaskItemDao.observeRowForTask] shape, so a capture
     * screen left open across a peer's submit/verdict sees it without re-entry. Null while no
     * worklist page has ever cached this task.
     */
    fun observeTaskRowStatus(taskId: String): Flow<String?>

    /** Network -> Room detail refresh. Non-blocking contract: a failure leaves the cache serving. */
    suspend fun refreshTaskDetail(taskId: String)

    /** One task's scanned animals from Room, newest first, bounded. */
    fun observeAnimals(taskId: String): Flow<List<PcCareAnimalRowEntity>>

    /**
     * The roster_pick tap list from Room: the RFIDs of animals currently in the task's pen.
     * Empty while nothing is cached yet.
     */
    fun observeRoster(taskId: String): Flow<List<String>>

    /** Network -> Room roster refresh (roster_pick tasks). A failure leaves the cache serving. */
    suspend fun refreshRoster(taskId: String)

    /**
     * ONE peer-visibility poll pass: fetches the task detail plus the captures pages and writes
     * everything THROUGH Room (peer scans, slot attribution, and the submit lock all land in the
     * same rows the screens observe — never into memory).
     */
    suspend fun pollTaskOnce(taskId: String)

    /**
     * Records one scan: normalizes the tag (trim+lowercase key, verbatim preserved), checks Room
     * for a local duplicate, then inserts the durable PENDING row and enqueues the outbox write.
     */
    suspend fun recordScan(taskId: String, tagVerbatim: String): PcCareScanOutcome

    /** Attaches one slot's recorded video (its PROOF_UPLOAD outbox row) to one scanned animal. */
    suspend fun registerSlotProof(
        taskId: String,
        normalizedTag: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ): AppResult<String>

    /** Attaches one task-level proof, used by inventory_vaccine fridge stock checks. */
    suspend fun registerTaskProof(
        taskId: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ): AppResult<String>

    /** Resolves a completed proof id to a short-lived playback URL for previews. */
    suspend fun proofDownloadUrl(proofId: String): AppResult<String>

    /** Enqueues the whole-task submit under the stable per-(task, rowVersion) key. */
    suspend fun submitTask(taskId: String, rowVersion: Int): AppResult<String>

    /**
     * Reconciles a successful submit's POST-dispatch server result into every Room copy of the
     * task (worklist rows across scopes + the detail cache). Called by the sync engine only.
     */
    suspend fun persistTaskSubmitResult(taskId: String, status: String, rowVersion: Int, animalCount: Int)

    // ---- Planner (CEO create wizard) ----------------------------------------------------------

    suspend fun plannerCatalog(): PcCarePlannerCatalogDto

    suspend fun plannerParkSheds(
        parkId: String,
        category: String,
        date: String,
        cursor: String? = null,
    ): PcCarePlannerShedsDto

    suspend fun createTask(idempotencyKey: String, request: PcCareCreateTaskRequestDto): PcCareTaskDto

    suspend fun cancelTask(taskId: String)

    /**
     * The PC Director's approve/reject on a submitted vaccine-stock task (maintainer decision
     * 2026-09-02). A live online call — the director is looking at the videos when deciding —
     * whose echoed task is written through to the Room caches so every list re-renders the new
     * status immediately.
     */
    suspend fun recordStockVerdict(taskId: String, verdict: String, reason: String): AppResult<PcCareTaskDto>
}

class DefaultPcCareRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val detailDao: PcCareTaskDetailCacheDao,
    private val animalDao: PcCareAnimalRowDao,
    private val syncRepository: SyncRepository,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : PcCareRepository {

    @OptIn(ExperimentalPagingApi::class)
    override fun worklistRows(query: PcCareWorklistQuery): Flow<PagingData<PcCareTaskDto>> {
        val key = query.roomKey()
        return Pager(
            config = PagingConfig(
                pageSize = PC_CARE_PAGE_SIZE,
                initialLoadSize = PC_CARE_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = PC_CARE_PAGE_SIZE * 3,
            ),
            remoteMediator = PcCareTaskRemoteMediator(query, api, database, json, clock),
            pagingSourceFactory = { database.pcCareTaskItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<PcCareTaskDto>(entity.dtoJson) } }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun invalidateWorklist(query: PcCareWorklistQuery) {
        // Deleting the remote key makes the next mediator initialize() LAUNCH_INITIAL_REFRESH;
        // the item rows are left in place so the screen keeps rendering until fresh rows land.
        database.pcCareTaskRemoteKeyDao().delete(query.roomKey())
    }

    override fun observeTaskDetail(taskId: String): Flow<PcCareTaskDto?> =
        detailDao.observe(taskId)
            .map { entity ->
                readCachedJson<PcCareTaskDto>(
                    json = json,
                    cacheKey = taskId,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { detailDao.delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    override fun observeTaskRowStatus(taskId: String): Flow<String?> =
        database.pcCareTaskItemDao()
            .observeRowForTask(taskId)
            .map { entity -> entity?.let { json.decodeFromString<PcCareTaskDto>(it.dtoJson).status } }
            .distinctUntilChanged()
            .flowOn(Dispatchers.Default)

    override suspend fun refreshTaskDetail(taskId: String) {
        // exception:exempt expected refresh failure (offline/timeout/5xx); the cache keeps serving
        // and the next successful open/poll repairs it — the non-blocking refresh contract.
        runCatching { upsertDetail(api.getPcCareTask(taskId)) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "pc_care_detail_refresh_failed task=$taskId", it)
            }
    }

    override fun observeAnimals(taskId: String): Flow<List<PcCareAnimalRowEntity>> =
        animalDao.observeAnimals(taskId, PC_CARE_ANIMAL_LIST_LIMIT)

    override fun observeRoster(taskId: String): Flow<List<String>> =
        detailDao.observe(rosterCacheKey(taskId))
            .map { entity ->
                readCachedJson<List<String>>(
                    json = json,
                    cacheKey = rosterCacheKey(taskId),
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { detailDao.delete(it) },
                ).data.orEmpty()
            }
            .flowOn(Dispatchers.Default)

    override suspend fun refreshRoster(taskId: String) {
        // exception:exempt expected refresh failure (offline/timeout/5xx); the cached roster keeps
        // serving and the next open/refresh repairs it — the non-blocking refresh contract.
        runCatching {
            val identifiers = mutableListOf<String>() // mobile-guard:ignore: function-local roster buffer, capped by PC_CARE_ROSTER_MAX_PAGES * PC_CARE_ROSTER_PAGE_LIMIT and persisted per page
            var cursor: String? = null
            var pages = 0
            // Write-through per page so a big pen shows its first tags immediately.
            while (pages < PC_CARE_ROSTER_MAX_PAGES) {
                val page = api.getPcCareTaskRoster(taskId, cursor, PC_CARE_ROSTER_PAGE_LIMIT)
                identifiers += page.identifiers
                detailDao.upsert(
                    PcCareTaskDetailCacheEntity(
                        cacheKey = rosterCacheKey(taskId),
                        dtoJson = json.encodeToString(identifiers.toList()),
                        updatedAt = clock(),
                    ),
                )
                cursor = page.nextCursor.ifBlank { null } ?: break
                pages++
            }
            detailDao.enforceCacheBounds()
        }.onFailure {
            if (it is CancellationException) throw it
            android.util.Log.w(LOG_TAG, "pc_care_roster_refresh_failed task=$taskId", it)
        }
    }

    override suspend fun pollTaskOnce(taskId: String) {
        // exception:exempt expected poll failure (offline/timeout/5xx); Room keeps serving what it
        // has and the next poll pass catches up — polling is additive, never a loading wall.
        runCatching {
            upsertDetail(api.getPcCareTask(taskId))
            var cursor: String? = null
            var pages = 0
            while (pages < PC_CARE_CAPTURES_MAX_PAGES) {
                val page = api.getPcCareTaskCaptures(
                    taskId = taskId,
                    cursor = cursor,
                    limit = PC_CARE_CAPTURES_PAGE_LIMIT,
                )
                // WRITE-THROUGH per page: peer scans/uploads land in Room as they arrive, so a
                // long task never accumulates in memory before becoming visible.
                mergeServerAnimals(taskId, page.animals)
                cursor = page.nextCursor.ifBlank { null } ?: break
                pages++
            }
            animalDao.deleteRowsOutsideNewestTasks(PC_CARE_CACHED_ANIMAL_TASKS)
        }.onFailure {
            if (it is CancellationException) throw it
            android.util.Log.w(LOG_TAG, "pc_care_poll_failed task=$taskId", it)
        }
    }

    override suspend fun proofDownloadUrl(proofId: String): AppResult<String> = try { // offline-first-guard:ignore: signed proof URL is short-lived; Room caches proof rows, not expiring download URLs
        AppResult.Ok(api.getProofDownloadUrl(proofId))
    } catch (t: Throwable) {
        if (t is CancellationException) throw t
        AppResult.Err("Preview is not available yet", t)
    }

    override suspend fun recordScan(taskId: String, tagVerbatim: String): PcCareScanOutcome {
        val verbatim = tagVerbatim.trim()
        val normalized = normalizePcCareTag(verbatim)
        if (normalized.isEmpty()) return PcCareScanOutcome.Failed("blank tag")
        val existing = animalDao.getByTag(taskId, normalized)
        if (existing != null && existing.scanSyncStatus != PcCareScanStatus.FAILED) {
            // PENDING / SYNCED / DUPLICATE all mean "this tag is already in the task". Only a
            // terminally FAILED scan is re-scannable — re-enqueueing under the SAME stable key
            // reopens the terminal outbox row (OutboxDao.reopenTerminalForRetry), never a new key.
            return PcCareScanOutcome.Duplicate(existing.scannedByName)
        }
        val now = clock()
        animalDao.upsertAll(
            listOf(
                PcCareAnimalRowEntity(
                    taskId = taskId,
                    normalizedTag = normalized,
                    tagVerbatim = verbatim,
                    animalRowId = existing?.animalRowId.orEmpty(),
                    scannedByName = "",
                    scanSyncStatus = PcCareScanStatus.PENDING,
                    serverSlotsJson = existing?.serverSlotsJson.orEmpty(),
                    updatedAt = now,
                ),
            ),
        )
        return when (val queued = syncRepository.enqueuePcCareScanAdd(taskId, verbatim, normalized)) {
            is AppResult.Ok -> PcCareScanOutcome.Queued
            is AppResult.Err -> PcCareScanOutcome.Failed(queued.message)
        }
    }

    override suspend fun registerSlotProof(
        taskId: String,
        normalizedTag: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ): AppResult<String> {
        // The server row id may still be blank (scan syncing) — the dispatcher re-resolves it.
        val animalRowId = animalDao.getByTag(taskId, normalizedTag)?.animalRowId.orEmpty()
        return syncRepository.enqueuePcCareSlotRegister(
            taskId = taskId,
            animalRowId = animalRowId,
            normalizedTag = normalizedTag,
            slotFieldKey = slotFieldKey,
            proofOutboxItemId = proofOutboxItemId,
        )
    }

    override suspend fun registerTaskProof(
        taskId: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ): AppResult<String> =
        syncRepository.enqueuePcCareTaskProofRegister(
            taskId = taskId,
            slotFieldKey = slotFieldKey,
            proofOutboxItemId = proofOutboxItemId,
        )

    override suspend fun submitTask(taskId: String, rowVersion: Int): AppResult<String> =
        syncRepository.enqueuePcCareTaskSubmit(taskId, rowVersion)

    override suspend fun persistTaskSubmitResult(
        taskId: String,
        status: String,
        rowVersion: Int,
        animalCount: Int,
    ) {
        if (status.isBlank()) return
        val now = clock()
        val itemDao = database.pcCareTaskItemDao()
        fun PcCareTaskDto.reconciled(): PcCareTaskDto = copy(
            status = status,
            rowVersion = rowVersion,
            animalCount = if (animalCount > 0) animalCount else this.animalCount,
        )
        database.withTransaction {
            itemDao.upsertAll(
                itemDao.rowsForTask(taskId).map { row ->
                    val dto = json.decodeFromString<PcCareTaskDto>(row.dtoJson).reconciled()
                    row.copy(dtoJson = json.encodeToString(dto), updatedAt = now)
                },
            )
            detailDao.get(taskId)?.let { cached ->
                val dto = json.decodeFromString<PcCareTaskDto>(cached.dtoJson).reconciled()
                detailDao.upsert(cached.copy(dtoJson = json.encodeToString(dto), updatedAt = now))
            }
        }
    }

    // Planner reads/writes are the CEO's create wizard, not an operator read screen: the catalog
    // is consumed once inside a modal flow and a stale cached catalog would offer pens/operators
    // the server has since refused. The two writes are interactive, foreground, awaited acts.
    override suspend fun plannerCatalog(): PcCarePlannerCatalogDto = // offline-first-guard:ignore: CEO wizard vocabulary read consumed inside a modal create flow; a stale cached catalog offers assignees/pens the server will refuse
        api.getPcCarePlannerCatalog()

    override suspend fun plannerParkSheds( // offline-first-guard:ignore: same wizard rationale — existing_task_id dedup must be live, or the wizard double-plans a pen
        parkId: String,
        category: String,
        date: String,
        cursor: String?,
    ): PcCarePlannerShedsDto =
        api.getPcCarePlannerParkSheds(parkId, category, date, cursor, PC_CARE_PAGE_SIZE)

    override suspend fun createTask( // offline-first-guard:ignore: awaited CEO wizard WRITE, not a read screen; the created task is upserted into the Room detail cache below
        idempotencyKey: String,
        request: PcCareCreateTaskRequestDto,
    ): PcCareTaskDto = api.createPcCareTask(idempotencyKey, request).also { upsertDetail(it) }

    override suspend fun cancelTask(taskId: String) {
        api.cancelPcCareTask(taskId)
        detailDao.delete(taskId)
    }

    override suspend fun recordStockVerdict(
        taskId: String,
        verdict: String,
        reason: String,
    ): AppResult<PcCareTaskDto> = try { // offline-first-guard:ignore: a live judgement on live videos; the echoed task is written through to Room below
        val task = api.recordPcCareStockVerdict(
            taskId,
            PcCareStockVerdictRequestDto(verdict = verdict, reason = reason),
        )
        upsertDetail(task)
        persistTaskSubmitResult(taskId, task.status, task.rowVersion, task.animalCount)
        AppResult.Ok(task)
    } catch (t: Throwable) {
        if (t is CancellationException) throw t
        AppResult.Err(stockVerdictFailureMessage(t), t)
    }

    /** Farm-worded failure copy for the director's verdict call — never technical wording. */
    private fun stockVerdictFailureMessage(t: Throwable): String =
        when (httpStatusCodeOf(t)) {
            409 -> "This task is not awaiting approval"
            else -> "Could not save the decision. Check the connection and try again"
        }

    private fun httpStatusCodeOf(t: Throwable): Int? =
        if (t.javaClass.name == "retrofit2.HttpException") {
            // exception:exempt reflection probe on an optional dependency; a failed probe just falls back to generic copy
            runCatching { t.javaClass.getMethod("code").invoke(t) as? Int }.getOrNull()
        } else {
            null
        }

    private suspend fun upsertDetail(dto: PcCareTaskDto) {
        detailDao.upsert(
            PcCareTaskDetailCacheEntity(
                cacheKey = dto.taskId,
                dtoJson = json.encodeToString(dto),
                updatedAt = clock(),
            ),
        )
        detailDao.enforceCacheBounds()
    }

    /**
     * Merges the server's animal rows into Room. Server rows are truth for everything they carry
     * (row id, attribution, slots) — but a LOCAL row that is still PENDING/FAILED for the same tag
     * keeps its local sync status until its own outbox row resolves it, so an offline scan is
     * never re-labelled by a poll that raced it.
     */
    private suspend fun mergeServerAnimals(taskId: String, animals: List<PcCareAnimalRowDto>) {
        if (animals.isEmpty()) return
        val now = clock()
        val merged = animals.map { dto ->
            val normalized = normalizePcCareTag(dto.scannedIdentifier)
            val local = animalDao.getByTag(taskId, normalized)
            PcCareAnimalRowEntity(
                taskId = taskId,
                normalizedTag = normalized,
                tagVerbatim = dto.scannedIdentifier,
                animalRowId = dto.animalRowId,
                scannedByName = dto.scannedByName,
                // The server listing this tag IS the scan's acceptance, whoever scanned it.
                scanSyncStatus = PcCareScanStatus.SYNCED,
                serverSlotsJson = json.encodeToString(dto.slots),
                updatedAt = local?.updatedAt ?: now,
            )
        }
        animalDao.upsertAll(merged)
    }

    private companion object {
        const val LOG_TAG = "GoatOsPcCare"
    }
}

/** trim + lowercase — the ONE normalization the duplicate check and the scan idempotency key share. */
fun normalizePcCareTag(tagVerbatim: String): String = tagVerbatim.trim().lowercase()

	/** Fills Room from `GET /app/pc-care/worklist` page-by-page. The backend pages TASKS directly
	 *  with an opaque keyset cursor, so inserts/cancels between pages cannot shift an offset under
	 *  the operator. */
@OptIn(ExperimentalPagingApi::class)
private class PcCareTaskRemoteMediator(
    private val query: PcCareWorklistQuery,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, PcCareTaskItemEntity>() {
    private val queryKey = query.roomKey()

    override suspend fun initialize(): InitializeAction {
        val cachedAt = database.pcCareTaskRemoteKeyDao().get(queryKey)?.updatedAt
        return if (cachedAt != null && clock() - cachedAt < CacheGovernance.DEFAULT_TTL_MILLIS) {
            InitializeAction.SKIP_INITIAL_REFRESH
        } else {
            InitializeAction.LAUNCH_INITIAL_REFRESH
        }
    }

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, PcCareTaskItemEntity>,
    ): MediatorResult {
		val cursor = when (loadType) {
		    LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
		    LoadType.REFRESH -> null
		    LoadType.APPEND -> {
		        val remoteKey = database.pcCareTaskRemoteKeyDao().get(queryKey)
		            ?: return MediatorResult.Success(endOfPaginationReached = true)
		        if (remoteKey.endReached) return MediatorResult.Success(endOfPaginationReached = true)
		        remoteKey.nextCursor.ifBlank { null }
		    }
		}
        return try {
            val response = if (query.monitor) {
                api.getPcCareTasks(
                    date = query.date,
		            category = query.category,
		            limit = PC_CARE_PAGE_SIZE,
		            cursor = cursor,
		        )
		    } else {
		        api.getPcCareWorklist(
		            category = query.category,
		            date = query.date,
		            limit = PC_CARE_PAGE_SIZE,
		            cursor = cursor,
		        )
		    }
		    val nextCursor = response.nextCursor.ifBlank { null }
		    val endReached = nextCursor == null
            val updatedAt = clock()
            database.withTransaction {
                val itemDao = database.pcCareTaskItemDao()
                val remoteKeyDao = database.pcCareTaskRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                }
                val rowBase = itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.items.mapIndexed { index, row ->
                        PcCareTaskItemEntity(
                            queryKey = queryKey,
                            grainKey = row.taskId,
                            sortIndex = rowBase + index,
                            dtoJson = json.encodeToString(row),
                            updatedAt = updatedAt,
                        )
                    },
                )
		        remoteKeyDao.upsert(
		            PcCareTaskRemoteKeyEntity(
		                queryKey = queryKey,
		                nextCursor = nextCursor.orEmpty(),
		                endReached = endReached,
		                updatedAt = updatedAt,
		            ),
                )
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteRowsOutsideNewestQueries(PC_CARE_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(PC_CARE_CACHED_QUERIES)
                }
            }
            MediatorResult.Success(endOfPaginationReached = endReached)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            MediatorResult.Error(error)
        }
    }
}
