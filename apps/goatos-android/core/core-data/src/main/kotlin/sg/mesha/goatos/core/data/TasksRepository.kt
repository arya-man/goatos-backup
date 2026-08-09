package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.ShedCompletionSummaryCacheDao
import sg.mesha.goatos.core.data.cache.ShedCompletionSummaryCacheEntity
import sg.mesha.goatos.core.data.cache.TaskDetailCacheDao
import sg.mesha.goatos.core.data.cache.TaskDetailCacheEntity
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.forms.FormOption
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.forms.toFormSpec
import sg.mesha.goatos.core.data.forms.toProofPolicy
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ShedCompletionSummaryDto
import sg.mesha.goatos.core.network.dto.SubmissionSummaryDto
import sg.mesha.goatos.core.network.dto.TaskDetailResponseDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto
import sg.mesha.goatos.core.network.dto.TaskOptionValuesResponseDto

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

    /** Cache-first stream for one task's shed-completion summary: emits immediately with whatever
     *  Room has (null on a cold cache) and re-emits after every successful
     *  [refreshShedCompletionSummary]. The submit-readiness acknowledgement summary is viewable
     *  offline, so it is Room-backed like every other screen-facing read. */
    fun observeShedCompletionSummary(taskId: String, shedId: String? = null, partitionLabel: String? = null): Flow<ShedCompletionSummaryDto?>

    /** Network side of stale-while-revalidate for the shed-completion summary: fetches and upserts
     *  Room on success; leaves the cache untouched on failure so a stale cached summary survives. */
    suspend fun refreshShedCompletionSummary(taskId: String, shedId: String? = null, partitionLabel: String? = null): Result<Unit>
}

class DefaultTasksRepository(
    private val api: AppApi,
    private val taskDetailDao: TaskDetailCacheDao,
    private val shedCompletionSummaryDao: ShedCompletionSummaryCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : TasksRepository {
    override suspend fun taskDetail(taskId: String): TaskDetail = fetchTaskDetail(taskId).toDomain()

    // offline-first-guard:ignore: Room-backed — reads taskDetailDao.observe(); heuristic misses the dao read through the .map/toResource helper.
    override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> =
        taskDetailDao.observe(taskId)
            .map { entity -> entity.toResource(taskId) }
            .flowOn(Dispatchers.Default)

    // offline-first-guard:ignore: Room-backed — upserts via taskDetailDao.upsert() inside runCatching; heuristic misses the upsert through the runCatching block.
    override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = runCatching {
        val dto = fetchTaskDetail(taskId)
        taskDetailDao.upsert(
            TaskDetailCacheEntity(cacheKey = taskId, dtoJson = json.encodeToString(dto), updatedAt = clock()),
        )
        taskDetailDao.enforceCacheBounds()
    }

    // offline-first-guard:ignore: Room-backed — reads shedCompletionSummaryDao.observe(); heuristic misses the dao read through the .map/readCachedJson helper.
    override fun observeShedCompletionSummary(taskId: String, shedId: String?, partitionLabel: String?): Flow<ShedCompletionSummaryDto?> =
        shedCompletionSummaryDao.observe(shedCompletionSummaryCacheKey(taskId, shedId, partitionLabel))
            .map { entity ->
                readCachedJson<ShedCompletionSummaryDto>(
                    json = json,
                    cacheKey = shedCompletionSummaryCacheKey(taskId, shedId, partitionLabel),
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { shedCompletionSummaryDao.delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    // offline-first-guard:ignore: Room-backed — upserts via shedCompletionSummaryDao.upsert() inside runCatching; heuristic misses the upsert through the runCatching block.
    override suspend fun refreshShedCompletionSummary(taskId: String, shedId: String?, partitionLabel: String?): Result<Unit> = runCatching {
        val dto = api.getShedCompletionSummary(taskId, shedId, partitionLabel)
        shedCompletionSummaryDao.upsert(
            ShedCompletionSummaryCacheEntity(
                cacheKey = shedCompletionSummaryCacheKey(taskId, shedId, partitionLabel),
                dtoJson = json.encodeToString(dto),
                updatedAt = clock(),
            ),
        )
        shedCompletionSummaryDao.enforceCacheBounds()
    }

    private suspend fun fetchTaskDetail(taskId: String): TaskDetailResponseDto {
        val detail = api.getAppTask(taskId)
        if (!detail.task.taskType.equals("vaccination", ignoreCase = true)) return detail
        val requiredSources = detail.sopVersion?.toFormSpec()?.fields.orEmpty()
            .mapNotNull { it.optionSource?.takeIf(String::isNotBlank) }
            .toSet()
        if (requiredSources.isEmpty()) return detail
        val optionValues = api.getTaskOptionValues(taskId)
        val returnedSources = optionValues.sources.mapTo(mutableSetOf()) { it.source }
        check(returnedSources.containsAll(requiredSources)) {
            "task option-values response omitted required source keys"
        }
        return detail.copy(optionValues = optionValues)
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

private fun shedCompletionSummaryCacheKey(taskId: String, shedId: String?, partitionLabel: String?): String =
    listOf(
        taskId,
        shedId?.takeIf { it.isNotBlank() } ?: "task-wide",
        partitionLabel?.trim()?.lowercase()?.takeIf { it.isNotBlank() } ?: "whole",
    ).joinToString("|")

private fun TaskDetailResponseDto.toDomain(): TaskDetail = TaskDetail(
    task = task.copy(
        sopVersionId = task.sopVersionId.ifBlank { sopVersion?.sopVersionId.orEmpty() },
    ),
    form = (sopVersion?.toFormSpec() ?: FormSpec.Empty).withOptionValues(optionValues),
    submissions = submissions,
    proofPolicy = sopVersion?.toProofPolicy() ?: ProofPolicy.Default,
)

private fun FormSpec.withOptionValues(values: TaskOptionValuesResponseDto?): FormSpec {
    if (values == null || fields.none { !it.optionSource.isNullOrBlank() }) return this
    val bySource = values.sources.associateBy { it.source }
    return copy(fields = fields.map { field ->
        val sourceKey = field.optionSource?.takeIf(String::isNotBlank) ?: return@map field
        val source = bySource[sourceKey] ?: return@map field
        field.copy(
            options = source.options.map { option ->
                FormOption(
                    value = option.value,
                    label = option.label.ifBlank { option.value },
                    disabled = option.disabled,
                    disabledReason = option.disabledReason,
                )
            },
            disabledReason = source.disabledReason,
        )
    })
}
