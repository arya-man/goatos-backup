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
import java.io.File
import java.net.URL
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.cache.LeadershipTaskDetailCacheEntity
import sg.mesha.goatos.core.data.cache.LeadershipTaskItemEntity
import sg.mesha.goatos.core.data.cache.LeadershipTaskRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.LeadershipAssigneeDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskDetailDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskEditRequestDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskFilterDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskRaiseRequestDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskStatusRequestDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskCommentRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.serverErrorText

/** One screen-page of leadership tasks — bounds BOTH the network request and the Room window
 *  (docs/decisions/mobile-data-fetch-anti-patterns.md). */
const val LEADERSHIP_TASK_PAGE_SIZE = 20

/** How many distinct filter scopes keep their cached list rows. */
private const val LEADERSHIP_TASK_CACHED_QUERIES = 6

/** Bump whenever the cached row JSON changes shape incompatibly. */
private const val LEADERSHIP_TASK_CACHE_SHAPE = "task-v1"

/** Attachment bytes live under this app-private cache subdirectory, one file per proof id. */
private const val ATTACHMENT_CACHE_DIR = "leadership-task-attachments"

/**
 * The whole-page facts the backend composes beside the rows on every list refresh: its title,
 * the filter chips (labels, counts, empty copy), the caller's unseen count and whether the caller
 * may raise. Rendered verbatim; the client derives none of it.
 */
data class LeadershipTaskPageMeta(
    val title: String = "",
    val filters: List<LeadershipTaskFilterDto> = emptyList(),
    val unseenCount: Int = 0,
    val canRaise: Boolean = false,
)

/**
 * Leadership Tasks (maintainer request 2026-09-04). READS are offline-first per
 * docs/decisions/android-offline-first.md: the list and the detail both render from Room and
 * network refreshes upsert Room. WRITES are deliberate ONLINE calls (v1 decision): raising a
 * task needs the server-issued proof ids of its attachments, which the outbox's proof dispatch
 * never hands back, so raise/edit/status/seen go straight to the server and their returned detail
 * is reconciled into Room. Every write takes an idempotency key the CALLER minted once per draft
 * and reuses verbatim on retry, so a retried tap can never double-create.
 */
interface LeadershipTasksRepository {
    /** The paged task list for one backend filter KEY ("" = the backend default). */
    fun tasks(filter: String): Flow<PagingData<LeadershipTaskDto>>

    /** Backend-composed page facts from the LAST list refresh. */
    val pageMeta: StateFlow<LeadershipTaskPageMeta>

    /** Drops one scope's freshness marker so the next pager refetches instead of TTL-skipping. */
    suspend fun invalidateTasks(filter: String)

    /** Room-first task detail; null while nothing is cached yet. */
    fun observeTaskDetail(taskId: String): Flow<LeadershipTaskDto?>

    /** Network -> Room detail refresh. Non-blocking contract: a failure leaves the cache serving. */
    suspend fun refreshTaskDetail(taskId: String)

    /** The CXOs a director may raise a task for (online). */
    suspend fun assignees(): AppResult<List<LeadershipAssigneeDto>>

    /**
     * Uploads one attachment through the existing proof pipeline (register -> PUT + complete) and
     * returns the server-issued proof id the task request carries. [idempotencyKey] is the
     * caller's per-attachment key, reused verbatim on retry so the server returns the SAME proof.
     */
    suspend fun uploadAttachment(
        idempotencyKey: String,
        kind: String,
        fileName: String,
        mimeType: String,
        localFilePath: String,
        durationMs: Long?,
        captureSource: String,
    ): AppResult<String>

    suspend fun raiseTask(idempotencyKey: String, request: LeadershipTaskRaiseRequestDto): AppResult<LeadershipTaskDto>

    suspend fun editTask(taskId: String, idempotencyKey: String, request: LeadershipTaskEditRequestDto): AppResult<LeadershipTaskDto>

    suspend fun changeStatus(taskId: String, idempotencyKey: String, request: LeadershipTaskStatusRequestDto): AppResult<LeadershipTaskDto>

    suspend fun markSeen(taskId: String): AppResult<LeadershipTaskDto>

    /** The assignee's note back on the task. */
    suspend fun setComment(taskId: String, idempotencyKey: String, request: LeadershipTaskCommentRequestDto): AppResult<LeadershipTaskDto>

    /**
     * The local path of one attachment's bytes, downloading into app-private cache on first use.
     * A second call for the same proof returns the cached file without touching the network.
     */
    suspend fun attachmentFile(proofId: String, fileName: String): AppResult<String>
}

class DefaultLeadershipTasksRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val bootstrapRepository: BootstrapRepository,
    /** App-private cache root (`Context.cacheDir`); attachment bytes live in a subdirectory of it. */
    private val cacheRoot: File,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : LeadershipTasksRepository {

    private val _pageMeta = MutableStateFlow(LeadershipTaskPageMeta())
    override val pageMeta: StateFlow<LeadershipTaskPageMeta> = _pageMeta

    @OptIn(ExperimentalPagingApi::class)
    override fun tasks(filter: String): Flow<PagingData<LeadershipTaskDto>> {
        val key = scopeKey(filter)
        return Pager(
            config = PagingConfig(
                pageSize = LEADERSHIP_TASK_PAGE_SIZE,
                initialLoadSize = LEADERSHIP_TASK_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = LEADERSHIP_TASK_PAGE_SIZE * 3,
            ),
            remoteMediator = LeadershipTaskRemoteMediator(
                filter = filter,
                api = api,
                database = database,
                json = json,
                clock = clock,
                onMeta = { meta -> _pageMeta.value = meta },
            ),
            pagingSourceFactory = { database.leadershipTaskItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<LeadershipTaskDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    override suspend fun invalidateTasks(filter: String) {
        database.leadershipTaskRemoteKeyDao().delete(scopeKey(filter))
    }

    override fun observeTaskDetail(taskId: String): Flow<LeadershipTaskDto?> =
        database.leadershipTaskDetailCacheDao().observe(taskId)
            .map { entity ->
                readCachedJson<LeadershipTaskDto>(
                    json = json,
                    cacheKey = taskId,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.leadershipTaskDetailCacheDao().delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    override suspend fun refreshTaskDetail(taskId: String) {
        // exception:exempt expected refresh failure (offline/timeout/5xx); the cache keeps serving
        // and the next successful open/refresh repairs it — the non-blocking refresh contract.
        runCatching { persistServerDetail(api.getLeadershipTask(taskId)) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "leadership_task_detail_refresh_failed task=$taskId", it)
            }
    }

    override suspend fun assignees(): AppResult<List<LeadershipAssigneeDto>> = call {
        api.getLeadershipTaskAssignees().assignees
    }

    override suspend fun uploadAttachment(
        idempotencyKey: String,
        kind: String,
        fileName: String,
        mimeType: String,
        localFilePath: String,
        durationMs: Long?,
        captureSource: String,
    ): AppResult<String> = call {
        val tenantId = bootstrapRepository.actorTenantId()
            ?: throw IllegalStateException("tenant scope unavailable for attachment upload")
        // The SAME two calls the outbox's proof dispatch makes (SyncEngine.dispatchProofUpload):
        // register under the caller's key — the server returns the same proof on a retry, with a
        // fresh signed URL — then PUT the bytes and complete.
        val registered = api.registerProof(
            idempotencyKey,
            ProofUploadRequestDto(
                proofType = PROOF_TYPE_ATTACHMENT,
                mimeType = mimeType,
                scopeType = SCOPE_TYPE_TENANT,
                scopeId = tenantId,
                subjectType = SUBJECT_TYPE_OTHER,
                subjectId = null,
                metadata = mapOf<String, JsonElement>(
                    "attachment_kind" to JsonPrimitive(kind),
                    "file_name" to JsonPrimitive(fileName),
                    "capture_source" to JsonPrimitive(captureSource),
                ),
            ),
        )
        val proofId = registered.proof.proofId
        if (proofId.isBlank()) throw IllegalStateException("proof registration returned no proof id")
        val completed = api.uploadProofBlob(
            proofId = proofId,
            uploadUrl = registered.uploadUrl,
            uploadMethod = registered.uploadMethod,
            uploadHeaders = registered.headers,
            uploadProtocol = registered.uploadProtocol,
            chunkSizeBytes = registered.chunkSizeBytes,
            mimeType = mimeType,
            filePath = localFilePath,
            durationMs = durationMs,
        )
        completed.proof.proofId.ifBlank { proofId }
    }

    override suspend fun raiseTask(idempotencyKey: String, request: LeadershipTaskRaiseRequestDto): AppResult<LeadershipTaskDto> =
        write { api.raiseLeadershipTask(idempotencyKey, request) }

    override suspend fun editTask(taskId: String, idempotencyKey: String, request: LeadershipTaskEditRequestDto): AppResult<LeadershipTaskDto> =
        write { api.editLeadershipTask(taskId, idempotencyKey, request) }

    override suspend fun changeStatus(taskId: String, idempotencyKey: String, request: LeadershipTaskStatusRequestDto): AppResult<LeadershipTaskDto> =
        write { api.changeLeadershipTaskStatus(taskId, idempotencyKey, request) }

    override suspend fun markSeen(taskId: String): AppResult<LeadershipTaskDto> =
        write { api.markLeadershipTaskSeen(taskId) }

    override suspend fun setComment(taskId: String, idempotencyKey: String, request: LeadershipTaskCommentRequestDto): AppResult<LeadershipTaskDto> =
        write { api.setLeadershipTaskComment(taskId, idempotencyKey, request) }

    override suspend fun attachmentFile(proofId: String, fileName: String): AppResult<String> = call {
        withContext(Dispatchers.IO) {
            val dir = File(cacheRoot, ATTACHMENT_CACHE_DIR).apply { mkdirs() }
            val ext = fileName.substringAfterLast('.', "").take(MAX_EXT_CHARS).filter { it.isLetterOrDigit() }
            val target = File(dir, if (ext.isBlank()) proofId else "$proofId.$ext")
            if (target.isFile && target.length() > 0L) return@withContext target.absolutePath
            val url = api.getProofDownloadUrl(proofId)
            val partial = File(dir, "${target.name}.part")
            URL(url).openStream().use { input ->
                partial.outputStream().use { output -> input.copyTo(output) }
            }
            if (!partial.renameTo(target)) {
                partial.copyTo(target, overwrite = true)
                partial.delete()
            }
            target.absolutePath
        }
    }

    /** A write: the server's returned detail is the new truth for the detail AND every cached row. */
    private suspend fun write(block: suspend () -> LeadershipTaskDetailDto): AppResult<LeadershipTaskDto> = call {
        val detail = block()
        persistServerDetail(detail)
        // Every filter's list is now stale (a raise adds a row, a status change moves one
        // between chips); dropping the markers makes the next pager refresh refetch.
        database.leadershipTaskRemoteKeyDao().deleteAll()
        detail.task
    }

    private suspend fun persistServerDetail(detail: LeadershipTaskDetailDto) {
        val task = detail.task
        if (task.taskId.isBlank()) return
        val now = clock()
        val detailDao = database.leadershipTaskDetailCacheDao()
        val itemDao = database.leadershipTaskItemDao()
        val taskJson = json.encodeToString(task)
        database.withTransaction {
            detailDao.upsert(LeadershipTaskDetailCacheEntity(cacheKey = task.taskId, dtoJson = taskJson, updatedAt = now))
            // Every cached list-row copy adopts the server's fresh task so a re-entered list shows
            // the new chip/seen state without a refetch.
            itemDao.upsertAll(itemDao.rowsForTask(task.taskId).map { row -> row.copy(dtoJson = taskJson, updatedAt = now) })
        }
        detailDao.enforceCacheBounds()
    }

    private suspend fun <T> call(block: suspend () -> T): AppResult<T> = try {
        AppResult.Ok(block())
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (error: Exception) {
        // The server's own explanation when it gave one (farm copy, rendered verbatim by the
        // caller); a blank message tells the caller to use its connection fallback line.
        AppResult.Err(message = error.serverErrorText()?.display.orEmpty(), cause = error)
    }

    private fun scopeKey(filter: String): String =
        cacheKey(LEADERSHIP_TASK_CACHE_SHAPE, "leadership-tasks", filter, LEADERSHIP_TASK_PAGE_SIZE.toString())

    private companion object {
        const val LOG_TAG = "GoatOsLeadershipTasks"
        const val PROOF_TYPE_ATTACHMENT = "attachment"
        const val SCOPE_TYPE_TENANT = "tenant"
        const val SUBJECT_TYPE_OTHER = "other"
        const val MAX_EXT_CHARS = 8
    }
}

/**
 * Fills Room from `GET /app/leadership-tasks` page-by-page over the endpoint's opaque keyset
 * cursor. Room stays the single source of truth: this never hands rows to the UI, it only writes
 * them, and the [androidx.paging.PagingSource] re-emits. ALWAYS refresh on open: a task may have
 * been raised, moved or seen on another phone since the last cache write.
 */
@OptIn(ExperimentalPagingApi::class)
private class LeadershipTaskRemoteMediator(
    private val filter: String,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
    private val onMeta: (LeadershipTaskPageMeta) -> Unit,
) : RemoteMediator<Int, LeadershipTaskItemEntity>() {
    private val queryKey = cacheKey(LEADERSHIP_TASK_CACHE_SHAPE, "leadership-tasks", filter, LEADERSHIP_TASK_PAGE_SIZE.toString())

    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, LeadershipTaskItemEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.leadershipTaskRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val response = api.getLeadershipTasks(
                filter = filter.ifBlank { null },
                limit = LEADERSHIP_TASK_PAGE_SIZE,
                cursor = cursor,
            )
            if (loadType == LoadType.REFRESH) {
                // Page facts ride the refresh only: counts are whole-list, so an APPEND page
                // fetched long after the user last looked must not overwrite them.
                onMeta(
                    LeadershipTaskPageMeta(
                        title = response.title,
                        filters = response.filters,
                        unseenCount = response.unseenCount,
                        canRaise = response.canRaise,
                    ),
                )
            }
            val nextCursor = response.nextCursor?.takeIf { it.isNotBlank() }
            // A cursor that did not ADVANCE is also the end, or an echoing backend would spin
            // this mediator forever on one page.
            val endReached = nextCursor == null || nextCursor == cursor
            val updatedAt = clock()
            database.withTransaction {
                val itemDao = database.leadershipTaskItemDao()
                val remoteKeyDao = database.leadershipTaskRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                }
                val rowBase = itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.rows.mapIndexed { index, task ->
                        LeadershipTaskItemEntity(
                            queryKey = queryKey,
                            grainKey = task.taskId,
                            sortIndex = rowBase + index,
                            dtoJson = json.encodeToString(task),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    LeadershipTaskRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor.orEmpty(),
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteRowsOutsideNewestQueries(LEADERSHIP_TASK_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(LEADERSHIP_TASK_CACHED_QUERIES)
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
