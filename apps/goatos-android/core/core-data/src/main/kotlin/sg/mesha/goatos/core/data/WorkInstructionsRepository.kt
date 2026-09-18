package sg.mesha.goatos.core.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.WorkflowChipsCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.GeneralSopDto
import sg.mesha.goatos.core.network.dto.GeneralSopsResponseDto
import sg.mesha.goatos.core.network.dto.StartWorkflowRequestDto

/**
 * GENERAL work instructions (SOP studio phase 2, docs/decisions/sop-studio.md): the SOPs a
 * person may start by hand, and the start itself.
 *
 * Offline-first for the READ (docs/decisions/android-offline-first.md): the startable list is
 * a small backend-owned JSON blob persisted in Room and observed by the screen; a refresh
 * upserts it and the cache re-emits. It rides the existing `workflow_chips_cache` blob table
 * under its own key namespace -- one more (key, json) row of the workflow feature, not a new
 * table -- so the list survives process death and a dead network the same way the day's chips
 * do. The START is an online write on purpose: the run's id is minted by the server and the
 * next screen needs it at once; a dead network is reported as such, never queued blind.
 */
interface WorkInstructionsRepository {
    fun observeStartable(): Flow<List<GeneralSopDto>>

    /** Refreshes the startable list from the backend; a failure leaves the cache visible. */
    suspend fun refresh(): Result<Unit>

    /** Starts one run under [runKey] (the idempotency key IS the run) and returns its workflow id. */
    suspend fun start(sopCode: String, runKey: String): Result<String>
}

class DefaultWorkInstructionsRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : WorkInstructionsRepository {

    // offline-first-guard:ignore: Room-backed — reads workflowChipsCacheDao.observe(); heuristic misses the dao read through the .map/readCachedJson helper.
    override fun observeStartable(): Flow<List<GeneralSopDto>> =
        database.workflowChipsCacheDao().observe(STARTABLE_KEY)
            .map { entity ->
                readCachedJson<GeneralSopsResponseDto>(
                    json = json,
                    cacheKey = STARTABLE_KEY,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.workflowChipsCacheDao().delete(it) },
                ).data?.sops.orEmpty()
            }
            .flowOn(Dispatchers.Default)

    override suspend fun refresh(): Result<Unit> = runCatching {
        val response = api.listGeneralSops()
        val dao = database.workflowChipsCacheDao()
        dao.upsert(WorkflowChipsCacheEntity(cacheKey = STARTABLE_KEY, dtoJson = json.encodeToString(response), updatedAt = clock()))
        dao.enforceCacheBounds()
    }.onFailure { if (it is CancellationException) throw it }

    // A WRITE, not a read: opens a run under the caller's idempotency key (a replay returns the
    // same run). The list it came from is Room-backed above; the run itself is then observed on the
    // shared workflow detail, which has its own cache.
    override suspend fun start(sopCode: String, runKey: String): Result<String> = runCatching { // offline-first-guard:ignore: write path (start a run under an idempotency key), not a screen read
        api.startWorkflow(runKey, StartWorkflowRequestDto(sopCode = sopCode)).workflowId
    }

    private companion object {
        val STARTABLE_KEY = cacheKey("work-instructions", "startable")
    }
}
