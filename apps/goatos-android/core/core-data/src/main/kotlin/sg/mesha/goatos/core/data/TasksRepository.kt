package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.TaskDetailCacheDao
import sg.mesha.goatos.core.data.cache.TaskDetailCacheEntity
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.forms.toFormSpec
import sg.mesha.goatos.core.data.forms.toProofPolicy
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.SubmissionSummaryDto
import sg.mesha.goatos.core.network.dto.TaskDetailResponseDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto

/** A task opened for recording: the summary, its parsed [form] to render, prior submissions, and
 *  the SOP's [proofPolicy] (R50-027) driving proof-capture limits/defaults instead of hardcoded
 *  client constants. */
data class TaskDetail(
    val task: TaskSummaryDto,
    val form: FormSpec,
    val submissions: List<SubmissionSummaryDto> = emptyList(),
    val proofPolicy: ProofPolicy = ProofPolicy.Default,
)

/**
 * Operator Tasks screen area: assigned-task reads only. Mutating task submissions must go
 * through SyncRepository's durable outbox; keep this repository free of a direct submit escape
 * hatch so callers cannot bypass offline replay, idempotency checks, or sync-status reporting.
 *
 * Offline-first (docs/decisions/android-offline-first.md, MOB-001): the one-task-at-a-time
 * [taskDetail] read Submit needs is backed by Room via [observeTaskDetail]/[refreshTaskDetail] —
 * a cold start, process death, or offline reopen renders the last-fetched task + its SOP form
 * from cache instead of a network-only blank/error wall.
 */
interface TasksRepository {
    /** Opens one task WITH its SOP form (parsed from `form_dsl`) so the runner can render it.
     *  A plain network call — [refreshTaskDetail] is the Room-upserting counterpart callers
     *  observing the cache should drive a refresh through. */
    suspend fun taskDetail(taskId: String): TaskDetail

    /** Cache-first stream for one task's detail: emits immediately with whatever Room has
     *  (null data on a cold cache) and re-emits after every successful [refreshTaskDetail].
     *  Never blank on re-entry when a cached row exists for [taskId]. */
    fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>>

    /** Network side of stale-while-revalidate: fetches and upserts Room on success; leaves the
     *  cache untouched on failure so a stale cached task (if any) survives the failed refresh. */
    suspend fun refreshTaskDetail(taskId: String): Result<Unit>
}

class DefaultTasksRepository(
    private val api: AppApi,
    private val taskDetailDao: TaskDetailCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : TasksRepository {
    override suspend fun taskDetail(taskId: String): TaskDetail = api.getAppTask(taskId).toDomain()

    // offline-first-guard:ignore: Room-backed — reads taskDetailDao.observe(); heuristic misses the dao read through the .map/toResource helper.
    override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> =
        taskDetailDao.observe(taskId)
            .map { entity -> entity.toResource(taskId) }
            .flowOn(Dispatchers.Default)

    // offline-first-guard:ignore: Room-backed — upserts via taskDetailDao.upsert() inside runCatching; heuristic misses the upsert through the runCatching block.
    override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = runCatching {
        val dto = api.getAppTask(taskId)
        taskDetailDao.upsert(
            TaskDetailCacheEntity(cacheKey = taskId, dtoJson = json.encodeToString(dto), updatedAt = clock()),
        )
        taskDetailDao.enforceCacheBounds()
    }

    private suspend fun TaskDetailCacheEntity?.toResource(taskId: String): Resource<TaskDetail> {
        val cached = readCachedJson<TaskDetailResponseDto>(
            json = json,
            cacheKey = taskId,
            dtoJson = this?.dtoJson,
            updatedAt = this?.updatedAt,
            now = clock(),
            quarantine = { taskDetailDao.delete(it) },
        )
        return Resource(data = cached.data?.toDomain(), lastSyncedAt = cached.updatedAt)
    }
}

private fun TaskDetailResponseDto.toDomain(): TaskDetail = TaskDetail(
    task = task,
    form = sopVersion?.toFormSpec() ?: FormSpec.Empty,
    submissions = submissions,
    proofPolicy = sopVersion?.toProofPolicy() ?: ProofPolicy.Default,
)
