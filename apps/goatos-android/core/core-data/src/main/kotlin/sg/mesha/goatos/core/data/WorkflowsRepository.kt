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
import kotlinx.coroutines.flow.firstOrNull
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.time.Instant
import sg.mesha.goatos.core.data.cache.WorkflowCardEntity
import sg.mesha.goatos.core.data.cache.WorkflowChipsCacheEntity
import sg.mesha.goatos.core.data.cache.WorkflowDetailCacheEntity
import sg.mesha.goatos.core.data.cache.WorkflowRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.data.sync.OutboxStore
import sg.mesha.goatos.core.data.sync.WorkflowActionAnswerPayload
import sg.mesha.goatos.core.data.sync.WorkflowActionCompletePayload
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowChipsDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowNextActionDto

/** One screen-page of workflow cards. The backend caps `GET /app/workflows` at 20 server-side; the
 *  client asks for the same bound so both layers page identically
 *  (docs/decisions/mobile-data-fetch-anti-patterns.md). */
const val WORKFLOW_PAGE_SIZE = 20

/** How many (module | date | filter) scopes keep their cached card rows. Bounds the table across
 *  day/chip churn: two modules x a few recently visited days/filters. */
private const val WORKFLOW_CACHED_QUERIES = 8
private const val WORKFLOW_MAX_CACHED_ROWS = WORKFLOW_PAGE_SIZE * 3
private const val MAX_ACTIVE_WORKFLOW_WRITES = WORKFLOW_MAX_CACHED_ROWS * 2
private val WORKFLOW_ACTION_OP_TYPES = listOf(
    OutboxOpType.WORKFLOW_ACTION_ANSWER.name,
    OutboxOpType.WORKFLOW_ACTION_COMPLETE.name,
)

data class WorkflowVideoDraft(
    val id: String,
    val workflowId: String,
    val actionId: String,
    val subjectGoatId: String,
    val localUri: String,
    val mimeType: String,
    val startedAtMs: Long,
    val endedAtMs: Long,
    val captureSource: String,
    val syncStatus: String = WORKFLOW_DRAFT_STATUS,
)

/**
 * The Birth/Death follow-up workflow read models (docs/decisions/birth-death-workflows.md):
 * the per-goat card list (`GET /app/workflows`), the day's chip counts, and the drill-in detail
 * (`GET /app/workflows/{id}`).
 *
 * Offline-first (docs/decisions/android-offline-first.md): Room is the UI's single source of truth.
 * The list screens observe a Room PagingSource filled by a RemoteMediator; the chips and the detail
 * are JSON-blob caches observed as Flows. Action WRITES do not live here — they go through the
 * durable outbox (`sync/SyncRepository.enqueueWorkflowAction*`); this repository only applies the
 * optimistic local Room update so the tapped action flips immediately and reconciles on refresh.
 */
interface WorkflowsRepository {
    /** The paged card list for one module/date/filter scope. Both layers page identically at
     *  [WORKFLOW_PAGE_SIZE] over the backend's opaque keyset cursor. */
    fun cards(module: String, date: String, filter: String): Flow<PagingData<WorkflowCardDto>>

    /** The day's chip counts for (module, date) — refreshed by every list page load. */
    fun observeChips(module: String, date: String): Flow<WorkflowChipsDto?>

    /** The cached drill-in detail. Emits null on a cold cache; [refreshDetail] repopulates. */
    fun observeDetail(workflowId: String): Flow<WorkflowDetailResponseDto?>

    /** Durable, app-private death evidence. Drafts do not create network/outbox work. */
    fun observeVideoDrafts(workflowId: String): Flow<List<WorkflowVideoDraft>>
    suspend fun listVideoDrafts(workflowId: String): List<WorkflowVideoDraft>

    /** Replaces the one draft at (workflow, action), returning the old file owner for cleanup. */
    suspend fun replaceVideoDraft(draft: WorkflowVideoDraft): WorkflowVideoDraft?

    suspend fun clearVideoDrafts(workflowId: String)

    /** Locks both durable drafts after Submit while their proof+completion outbox group drains. */
    suspend fun markVideoDraftsSubmitting(workflowId: String)

    /** Fetches the detail and upserts Room (which re-emits). Failure leaves the cache visible. */
    suspend fun refreshDetail(workflowId: String): Result<Unit>

    /** The cached card by id (offline-first drill-in header while the detail fetch runs). */
    suspend fun findCachedCard(workflowId: String): WorkflowCardDto?

    /**
     * Optimistic local completion of an answered question: flips the cached detail action to
     * `completed` with [answerValue] the moment the answer is durably queued, so the row reads as
     * done immediately. The backend remains the authority; the next refresh reconciles.
     */
    suspend fun markActionAnswered(workflowId: String, actionId: String, answerValue: String)

    /** Optimistic local completion of an `action` step. [inReview] renders verification-gated
     *  (requires_video) completions as in-review rather than done. */
    suspend fun markActionCompleted(workflowId: String, actionId: String, inReview: Boolean)
}

class DefaultWorkflowsRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val outboxStore: OutboxStore,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : WorkflowsRepository {

    @OptIn(ExperimentalPagingApi::class)
    override fun cards(module: String, date: String, filter: String): Flow<PagingData<WorkflowCardDto>> {
        val key = scopeKey(module, date, filter)
        return Pager(
            config = PagingConfig(
                pageSize = WORKFLOW_PAGE_SIZE,
                initialLoadSize = WORKFLOW_PAGE_SIZE,
                // Prefetch the next page as the operator reaches ~item 17-18 of the current one.
                prefetchDistance = 3,
                enablePlaceholders = false,
                // Hard ceiling on rows retained in memory: three pages, then the far side is
                // dropped and re-read from Room on scroll back.
                maxSize = WORKFLOW_PAGE_SIZE * 3,
            ),
            remoteMediator = WorkflowRemoteMediator(
                module = module,
                date = date,
                filter = filter,
                api = api,
                database = database,
                outboxStore = outboxStore,
                json = json,
                clock = clock,
            ),
            pagingSourceFactory = { database.workflowCardDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<WorkflowCardDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    // offline-first-guard:ignore: Room-backed — reads workflowChipsCacheDao.observe(); heuristic misses the dao read through the .map/readCachedJson helper.
    override fun observeChips(module: String, date: String): Flow<WorkflowChipsDto?> =
        database.workflowChipsCacheDao().observe(chipsKey(module, date))
            .map { entity ->
                readCachedJson<WorkflowChipsDto>(
                    json = json,
                    cacheKey = chipsKey(module, date),
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.workflowChipsCacheDao().delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    // offline-first-guard:ignore: Room-backed — reads workflowDetailCacheDao.observe(); heuristic misses the dao read through the .map/readCachedJson helper.
    override fun observeDetail(workflowId: String): Flow<WorkflowDetailResponseDto?> =
        database.workflowDetailCacheDao().observe(workflowId)
            .map { entity ->
                readCachedJson<WorkflowDetailResponseDto>(
                    json = json,
                    cacheKey = workflowId,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.workflowDetailCacheDao().delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    override fun observeVideoDrafts(workflowId: String): Flow<List<WorkflowVideoDraft>> =
        database.proofCaptureDao().observeWorkflowDeathDrafts(workflowId)
            .map { rows -> rows.map { it.toWorkflowVideoDraft() } }
            .flowOn(Dispatchers.Default)

    override suspend fun listVideoDrafts(workflowId: String): List<WorkflowVideoDraft> =
        database.proofCaptureDao().observeWorkflowDeathDrafts(workflowId).firstOrNull()
            ?.map { it.toWorkflowVideoDraft() }.orEmpty()

    override suspend fun replaceVideoDraft(draft: WorkflowVideoDraft): WorkflowVideoDraft? =
        database.proofCaptureDao().replaceWorkflowDeathDraft(draft.toEntity())?.toWorkflowVideoDraft()

    override suspend fun clearVideoDrafts(workflowId: String) =
        database.proofCaptureDao().clearWorkflowDeathDrafts(workflowId)

    override suspend fun markVideoDraftsSubmitting(workflowId: String) =
        database.proofCaptureDao().markWorkflowDeathDraftsSubmitting(workflowId)

    override suspend fun refreshDetail(workflowId: String): Result<Unit> = runCatching {
        // Sample on both sides of the request. If a command finishes while GET is in flight, the
        // pre-request sample protects against installing the older GET snapshot; if it is queued
        // during the GET, the post-request sample protects it. A command already terminal before
        // the first sample has committed/rejected, so backend truth is authoritative.
        val activeBefore = activeWorkflowActionIds(workflowId)
        val detail = api.getWorkflow(workflowId)
        val activeAfter = activeWorkflowActionIds(workflowId)
        val detailDao = database.workflowDetailCacheDao()
        val cached = detailDao.observe(workflowId).firstOrNull()
            ?.let { runCatching { json.decodeFromString<WorkflowDetailResponseDto>(it.dtoJson) }.getOrNull() }
        val reconciled = detail.withActiveWorkflowActionsPreserved(
            cached = cached,
            activeActionIds = activeBefore + activeAfter,
        )
        detailDao.upsert(
            WorkflowDetailCacheEntity(
                cacheKey = workflowId,
                dtoJson = json.encodeToString(reconciled),
                updatedAt = clock(),
            ),
        )
        detailDao.enforceCacheBounds()
    }

    private suspend fun activeWorkflowActionIds(workflowId: String): Set<String> =
        outboxStore.findActiveForGroup(
            groupKey = workflowId,
            opTypes = WORKFLOW_ACTION_OP_TYPES,
            limit = MAX_ACTIVE_WORKFLOW_WRITES,
        ).mapNotNull(::workflowActionId).toSet()

    private fun workflowActionId(item: OutboxEntity): String? = runCatching {
        when (item.opType) {
            OutboxOpType.WORKFLOW_ACTION_ANSWER.name ->
                json.decodeFromString<WorkflowActionAnswerPayload>(item.payloadJson).actionId
            OutboxOpType.WORKFLOW_ACTION_COMPLETE.name ->
                json.decodeFromString<WorkflowActionCompletePayload>(item.payloadJson).actionId
            else -> null
        }
    }.getOrNull()

    override suspend fun findCachedCard(workflowId: String): WorkflowCardDto? =
        database.workflowCardDao().findById(workflowId)
            ?.let { runCatching { json.decodeFromString<WorkflowCardDto>(it.dtoJson) }.getOrNull() }

    override suspend fun markActionAnswered(workflowId: String, actionId: String, answerValue: String) =
        mutateCachedDetail(workflowId) { detail ->
            detail.copy(
                actions = detail.actions.map { action ->
                    if (action.actionId == actionId) {
                        action.copy(status = STATUS_COMPLETED, answerValue = answerValue)
                    } else {
                        action
                    }
                },
            )
        }

    override suspend fun markActionCompleted(workflowId: String, actionId: String, inReview: Boolean) =
        mutateCachedDetail(workflowId) { detail ->
            detail.copy(
                actions = detail.actions.map { action ->
                    if (action.actionId == actionId) {
                        action.copy(status = if (inReview) STATUS_IN_REVIEW else STATUS_COMPLETED)
                    } else {
                        action
                    }
                },
            )
        }

    /**
     * Rewrites the cached detail through [transform], recomputing the done counter over the SAME
     * grain the backend maintains (main-section operator actions; colostrum sessions and hidden
     * approvals never count toward `actions_total`). Purely optimistic UI state — the next [refreshDetail] reconciles with the
     * backend truth. A missing/corrupt cache row is a no-op (the screen refetches anyway).
     */
    private suspend fun mutateCachedDetail(
        workflowId: String,
        transform: (WorkflowDetailResponseDto) -> WorkflowDetailResponseDto,
    ) {
        val detailDao = database.workflowDetailCacheDao()
        val entity = detailDao.observe(workflowId).firstOrNull() ?: return
        val detail = runCatching { json.decodeFromString<WorkflowDetailResponseDto>(entity.dtoJson) }.getOrNull() ?: return
        val mutated = transform(detail)
        // Keep the Room SSOT immediately usable while the proof upload + completion outbox group
        // drains. `in_review` means this operator has finished the step locally: count it and unlock
        // its successor. A verifier rework is a different status and the next refresh re-locks the
        // sequence. The backend repeats the hard write gate and remains authoritative on reconcile.
        val optimistic = mutated.withOptimisticOperatorSequence()
        val updatedAt = clock()
        database.withTransaction {
            detailDao.upsert(
                WorkflowDetailCacheEntity(
                    cacheKey = workflowId,
                    dtoJson = json.encodeToString(optimistic),
                    updatedAt = updatedAt,
                ),
            )
            val cardDao = database.workflowCardDao()
            val updatedCards = cardDao.findAllById(workflowId, WORKFLOW_CACHED_QUERIES).mapNotNull { entity ->
                runCatching { json.decodeFromString<WorkflowCardDto>(entity.dtoJson) }.getOrNull()
                    ?.withOptimisticOperatorProgress(optimistic, updatedAt)
                    ?.let { card -> entity.copy(dtoJson = json.encodeToString(card), updatedAt = updatedAt) }
            }
            if (updatedCards.isNotEmpty()) cardDao.upsertAll(updatedCards)
        }
    }

    private companion object {
        const val STATUS_COMPLETED = "completed"
        const val STATUS_IN_REVIEW = "in_review"
        const val SECTION_MAIN = "main"
        const val TYPE_APPROVAL = "approval"
    }
}

private const val WORKFLOW_DEATH_DRAFT_SUBJECT = "workflow_death_draft"
private const val WORKFLOW_DRAFT_STATUS = "WORKFLOW_DRAFT"

private fun WorkflowVideoDraft.toEntity() = ProofCaptureEntity(
    id = id,
    taskId = workflowId,
    fieldKey = actionId,
    proofSubject = WORKFLOW_DEATH_DRAFT_SUBJECT,
    subjectId = subjectGoatId,
    localUri = localUri,
    mimeType = mimeType,
    capturedAtMs = startedAtMs,
    capturedStartMs = startedAtMs,
    capturedEndMs = endedAtMs,
    syncStatus = WORKFLOW_DRAFT_STATUS,
    idempotencyKey = "workflow-draft:$workflowId:$actionId:$startedAtMs",
    captureSource = captureSource,
)

private fun ProofCaptureEntity.toWorkflowVideoDraft() = WorkflowVideoDraft(
    id = id,
    workflowId = taskId,
    actionId = fieldKey,
    subjectGoatId = subjectId.orEmpty(),
    localUri = localUri,
    mimeType = mimeType,
    startedAtMs = capturedStartMs,
    endedAtMs = capturedEndMs,
    captureSource = captureSource,
    syncStatus = syncStatus,
)

/** Copies the optimistic detail's operator-grain progress into every cached outer card. Approval
 * and verifier state remain backend-owned; `awaitingVerification` is deliberately untouched. */
internal fun WorkflowCardDto.withOptimisticOperatorProgress(
    detail: WorkflowDetailResponseDto,
    nowMs: Long,
): WorkflowCardDto {
    val operatorActions = detail.actions
        .filter { it.section == "main" && it.actionType != "approval" }
        .sortedBy { it.seq }
    fun operatorFinished(status: String): Boolean = status == "completed" || status == "in_review"
    val next = operatorActions.firstOrNull { !operatorFinished(it.status) }
    return copy(
        actionsDone = operatorActions.count { operatorFinished(it.status) },
        actionsTotal = operatorActions.size,
        nextAction = next?.let { action ->
            WorkflowNextActionDto(
                key = action.actionKey,
                title = action.title,
                dueAt = action.dueAt,
                overdue = action.dueAt?.let { due ->
                    runCatching { Instant.parse(due).toEpochMilli() < nowMs }.getOrDefault(false)
                } ?: false,
            )
        },
    )
}

/** A stale list response cannot erase operator progress while its durable command is active. */
internal fun WorkflowCardDto.withActiveCachedProgressPreserved(
    cached: WorkflowCardDto?,
    hasActiveCommand: Boolean,
): WorkflowCardDto = if (hasActiveCommand && cached != null && cached.actionsDone > actionsDone) {
    copy(
        actionsDone = cached.actionsDone,
        actionsTotal = cached.actionsTotal,
        nextAction = cached.nextAction,
    )
} else {
    this
}

/** Pure optimistic projection used while a durable action outbox group drains. Both `completed`
 * and `in_review` are finished from the operator's perspective; only a later `rework` reopens it. */
internal fun WorkflowDetailResponseDto.withOptimisticOperatorSequence(): WorkflowDetailResponseDto {
    fun operatorFinished(action: sg.mesha.goatos.core.network.dto.WorkflowActionDto): Boolean =
        action.status == "completed" || action.status == "in_review"

    val sequencedActions = actions.map { action ->
        if (action.actionType == "approval") {
            action
        } else {
            val hasIncompletePredecessor = actions.any { previous ->
                previous.actionType != "approval" &&
                    previous.section == action.section &&
                    previous.seq < action.seq &&
                    !operatorFinished(previous)
            }
            action.copy(blocked = hasIncompletePredecessor)
        }
    }
    val done = sequencedActions.count {
        it.section == "main" && it.actionType != "approval" && operatorFinished(it)
    }
    return copy(actions = sequencedActions, actionsDone = done)
}

/** Reconciles server truth with commands that are still durable and active locally. */
internal fun WorkflowDetailResponseDto.withActiveWorkflowActionsPreserved(
    cached: WorkflowDetailResponseDto?,
    activeActionIds: Set<String>,
): WorkflowDetailResponseDto {
    if (cached == null || activeActionIds.isEmpty()) return withOptimisticOperatorSequence()
    val cachedById = cached.actions.associateBy { it.actionId }
    val overlaid = copy(
        actions = actions.map { serverAction ->
            val localAction = cachedById[serverAction.actionId]
            if (
                serverAction.actionId in activeActionIds &&
                localAction != null &&
                (localAction.status == "completed" || localAction.status == "in_review")
            ) {
                serverAction.copy(
                    status = localAction.status,
                    answerValue = localAction.answerValue,
                )
            } else {
                serverAction
            }
        },
    )
    return overlaid.withOptimisticOperatorSequence()
}

private fun scopeKey(module: String, date: String, filter: String): String =
    cacheKey("workflows", module, date, filter)

private fun chipsKey(module: String, date: String): String =
    cacheKey("workflow-chips", module, date)

/**
 * Fills Room from the backend page-by-page over the endpoint's opaque keyset cursor, and persists
 * the day's chip counts alongside every REFRESH (the chips ride the list response). Room stays the
 * single source of truth: this never hands rows to the UI, it only writes them.
 */
@OptIn(ExperimentalPagingApi::class)
private class WorkflowRemoteMediator(
    private val module: String,
    private val date: String,
    private val filter: String,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val outboxStore: OutboxStore,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, WorkflowCardEntity>() {
    private val queryKey = scopeKey(module, date, filter)

    /**
     * ALWAYS refresh on open: this is a work queue answering "which goats are waiting on me RIGHT
     * NOW". Paging renders the cached Room window immediately while the refresh runs; a failed
     * refresh leaves those cached rows on screen behind the screen's stale banner
     * (docs/decisions/android-offline-first.md — refresh-on-open, stale-while-revalidate).
     */
    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, WorkflowCardEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.workflowRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isNullOrBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val cachedBefore = database.workflowCardDao()
                .findForQuery(queryKey, WORKFLOW_MAX_CACHED_ROWS)
                .mapNotNull { entity ->
                    runCatching { json.decodeFromString<WorkflowCardDto>(entity.dtoJson) }.getOrNull()
                        ?.let { entity.workflowId to it }
                }
                .toMap()
            val activeBefore = activeWorkflowGroups(cachedBefore.keys)
            val response = api.listWorkflows(
                module = module,
                date = date.ifBlank { null },
                filter = filter.ifBlank { null },
                pageSize = WORKFLOW_PAGE_SIZE,
                cursor = cursor,
            )
            val activeAfter = activeWorkflowGroups(response.items.map { it.workflowId })
            val activeGroups = activeBefore + activeAfter
            val reconciledItems = response.items.map { serverCard ->
                serverCard.withActiveCachedProgressPreserved(
                    cached = cachedBefore[serverCard.workflowId],
                    hasActiveCommand = serverCard.workflowId in activeGroups,
                )
            }
            // An absent next_cursor is the contract's own end-of-pages signal; a cursor that did
            // not ADVANCE is also treated as the end (a backend echoing the same cursor would
            // otherwise spin this mediator forever — the banned non-terminating pagination loop).
            val nextCursor = response.nextCursor?.takeIf { it.isNotBlank() }
            val endReached = nextCursor == null || nextCursor == cursor
            val updatedAt = clock()
            // Page rows, their cursor, and the day's chips commit TOGETHER.
            database.withTransaction {
                val cardDao = database.workflowCardDao()
                val remoteKeyDao = database.workflowRemoteKeyDao()
                val startIndex = if (loadType == LoadType.REFRESH) {
                    // A refresh re-reads the day from the top: drop the scope's rows so a card
                    // completed elsewhere disappears instead of lingering.
                    cardDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                    0
                } else {
                    cardDao.countFor(queryKey)
                }
                cardDao.upsertAll(
                    reconciledItems.mapIndexed { index, item ->
                        WorkflowCardEntity(
                            queryKey = queryKey,
                            workflowId = item.workflowId,
                            // Server keyset order preserved by offsetting the page's own index.
                            sortIndex = startIndex + index,
                            dtoJson = json.encodeToString(item),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    WorkflowRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor,
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                // The chips are the backend's own per-day counts, independent of page size; a
                // refresh of ANY filter scope re-serves the same (module, date) chips envelope.
                database.workflowChipsCacheDao().upsert(
                    WorkflowChipsCacheEntity(
                        cacheKey = chipsKey(module, date),
                        dtoJson = json.encodeToString(response.chips),
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    cardDao.deleteRowsOutsideNewestQueries(WORKFLOW_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(WORKFLOW_CACHED_QUERIES)
                }
            }
            database.workflowChipsCacheDao().enforceCacheBounds()
            MediatorResult.Success(endOfPaginationReached = endReached)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            // Room keeps whatever it already had: a failed page load surfaces as a Paging
            // LoadState.Error next to the cached rows, never as a wipe.
            MediatorResult.Error(error)
        }
    }

    private suspend fun activeWorkflowGroups(groupKeys: Collection<String>): Set<String> {
        if (groupKeys.isEmpty()) return emptySet()
        return outboxStore.findActiveForGroups(
            groupKeys = groupKeys.toList(),
            opTypes = WORKFLOW_ACTION_OP_TYPES,
            limit = MAX_ACTIVE_WORKFLOW_WRITES,
        ).mapTo(mutableSetOf()) { it.groupKey }
    }
}
