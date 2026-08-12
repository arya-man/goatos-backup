package sg.mesha.goatos.core.data

import androidx.room.withTransaction
import java.util.Locale
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
import sg.mesha.goatos.core.data.cache.ScanRosterRowDao
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.StatusCount
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.data.capture.ROSTER_SCAN_FIELD_KEY
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.ScanTagClassificationDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto

private val SERVER_DONE_ROSTER_STATUSES = setOf("done", "completed")

/**
 * Roster statuses that mean the server considers this animal OUTSTANDING, whatever local scan
 * evidence exists. A sent-back animal WAS scanned -- that is why it has a capture and a
 * scannedAt -- but the verifier refused its proof, so it is work again.
 */
private val SERVER_OUTSTANDING_ROSTER_STATUSES = setOf("rejected", "due", "pending")

/**
 * Vaccination execution screen area: the execution row list, the per-shed
 * drilldown, and the per-animal scan roster. Offline-first
 * (docs/decisions/android-offline-first.md): Room is the UI's single source of truth.
 *
 * The execution-row and shed-drilldown reads are JSON-blob-by-scope caches
 * ([ExecutionRowsCacheDao] / [ExecutionShedCacheDao]). The scan roster is the exception, and
 * deliberately so (docs/decisions/mobile-data-fetch-anti-patterns.md): it is a per-animal SSOT
 * ([ScanRosterRowDao]), never a whole-collection blob. [refreshScanRoster] walks the whole shed
 * roster page-by-page into that table (each network page stays ~20 rows, streamed straight into the
 * DAO); the scan LIST then renders a bounded keyset window ([observeScanRosterRows]), RFID
 * validation matches the full roster ([findScanRosterByTag]), counters are full-roster GROUP BY
 * aggregates, and the submit proof gate resolves the full DONE set ([observeScanRosterDoneGoatIds]).
 *
 * observeX is cache-first and reactive; refreshX is the network side of stale-while-revalidate — it
 * upserts Room on success and leaves the cache untouched on failure. DTO -> UiState mapping stays in :app.
 */
interface ExecutionRepository {
    suspend fun rows(
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        openOnly: Boolean? = null,
        limit: Int? = null,
        cursor: String? = null,
        includeFilterOptions: Boolean = false,
    ): VaccinationExecutionResponseDto

    /** Cache-first stream for this filter scope: emits immediately with whatever Room has
     *  (null data on a cold cache) and re-emits after every successful [refreshRows]. */
    fun observeRows(
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        openOnly: Boolean? = null,
        limit: Int? = null,
        includeFilterOptions: Boolean = false,
    ): Flow<Resource<VaccinationExecutionResponseDto>>

    /** Fetches and upserts Room on success; on failure returns the failure and leaves the
     *  cache untouched — the caller surfaces stale/offline, never a blank screen. */
    suspend fun refreshRows(
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        openOnly: Boolean? = null,
        limit: Int? = null,
        includeFilterOptions: Boolean = false,
    ): Result<Unit>

    /** Appends the next execution page into the same Room-backed first-page scope. */
    suspend fun appendRows(
        cursor: String,
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        openOnly: Boolean? = null,
        limit: Int? = null,
        includeFilterOptions: Boolean = false,
    ): Result<Unit>

    suspend fun shed(
        shedId: String,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
        partitionLabel: String? = null,
    ): VaccinationExecutionShedDrilldownDto

    /** Cache-first stream for this shed drilldown scope. */
    fun observeShed(
        shedId: String,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
        partitionLabel: String? = null,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>>

    /** Fetches and upserts Room on success; leaves the cache untouched on failure. */
    suspend fun refreshShed(
        shedId: String,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
        partitionLabel: String? = null,
    ): Result<Unit>

    /** The scan LIST read: a bounded keyset WINDOW of the per-row SSOT (never the whole collection),
     *  ordered by backend roster position and re-emitted on every roster upsert. The ViewModel grows
     *  [windowSize] as the user scrolls; the full roster already lives in Room after [refreshScanRoster],
     *  so paging is a local window advance, not a network call — page-N animals work offline. */
    fun observeScanRosterRows(
        shedId: String,
        taskId: String? = null,
        windowSize: Int,
        partitionLabel: String? = null,
    ): Flow<List<ScanRosterRowEntity>>

    /** Full-roster row count for this shed/task scope — drives `hasMore` (window < total). */
    fun observeScanRosterTotal(shedId: String, taskId: String?, partitionLabel: String? = null): Flow<Int>

    /** Distinct goat ids of every DONE/completed animal in the FULL roster (page-independent). The
     *  submit proof gate requires a synced proof for each; see [scanRosterRowsByGoatIds]. */
    fun observeScanRosterDoneGoatIds(shedId: String, taskId: String?, partitionLabel: String? = null): Flow<List<String>>

    /** Rows for a bounded goat-id set — the proof-incomplete animals the submit gate surfaces for
     *  retry/replace, so action-needed animals outside the visible window are still shown. */
    suspend fun scanRosterRowsByGoatIds(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
        partitionLabel: String? = null,
    ): List<ScanRosterRowEntity>

    /** Walks the WHOLE shed roster page-by-page into the per-row SSOT ([ScanRosterRowDao]) — each
     *  network page stays ~20 rows, streamed straight into the DAO with its backend `seq` order, and
     *  the whole replace is atomic (a mid-walk failure rolls back and leaves the prior roster intact).
     *  On success the full roster is local, so tag validation, counters, the windowed list, and the
     *  proof gate all resolve without another network round trip. No whole-collection blob is written. */
    suspend fun refreshScanRoster(
        shedId: String,
        taskId: String? = null,
        limit: Int? = null,
        partitionLabel: String? = null,
    ): Result<Unit>

    /** R50-007: Find a roster row by shed and normalized tag (searches the full roster, not just
     *  the loaded page). Returns null if the tag is not found in this shed. */
    suspend fun findScanRosterByTag(
        shedId: String,
        taskId: String?,
        normalizedTag: String,
        partitionLabel: String? = null,
    ): ScanRosterRowEntity?

    /** Returns every vaccine-obligation row for the animal matching this tag. */
    suspend fun findScanRosterRowsByTag(
        shedId: String,
        taskId: String?,
        normalizedTag: String,
        partitionLabel: String? = null,
    ): List<ScanRosterRowEntity> =
        listOfNotNull(findScanRosterByTag(shedId, taskId, normalizedTag, partitionLabel))

    /** Finds a tag across cached partition scopes for the same task. */
    suspend fun findScanRosterRowsByTaskAndTag(taskId: String, normalizedTag: String): List<ScanRosterRowEntity> = emptyList()

    suspend fun classifyScanTag(
        shedId: String,
        taskId: String,
        tag: String,
        partitionLabel: String? = null,
    ): ScanTagClassificationDto = ScanTagClassificationDto(tag = tag)

    /** R50-008: Get status-based counts for a shed (full roster, independent of loaded page). */
    suspend fun getScanRosterStatusCounts(shedId: String, taskId: String?, partitionLabel: String? = null): List<StatusCount>

    /** R50-008: Observable full-roster status aggregates for a shed. Re-emits on every roster
     *  upsert; the ViewModel combines this with the paged roster so counters are identical for
     *  page size 1 and 20. */
    fun observeScanRosterStatusCounts(shedId: String, taskId: String?, partitionLabel: String? = null): Flow<List<StatusCount>>

    /** R50-008: Effective status aggregates for a bounded goat-id set (the local unsynced overlay). */
    suspend fun getScanRosterStatusCountsFor(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
        partitionLabel: String? = null,
    ): List<StatusCount>
}

class DefaultExecutionRepository(
    private val api: AppApi,
    private val rowsDao: ExecutionRowsCacheDao,
    private val shedDao: ExecutionShedCacheDao,
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
        openOnly: Boolean?,
        limit: Int?,
        cursor: String?,
        includeFilterOptions: Boolean,
    ): VaccinationExecutionResponseDto =
        api.listVaccinationExecution(parkId, workState, asOf, dueBefore, openOnly, limit, cursor, includeFilterOptions)

    override fun observeRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Flow<Resource<VaccinationExecutionResponseDto>> {
        val key = cacheKey(parkId, workState, asOf, dueBefore, openOnly?.toString(), includeFilterOptions.toString(), limit?.toString())
        return rowsDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = runCatching {
        val dto = rows(parkId, workState, asOf, dueBefore, openOnly, limit, cursor = null, includeFilterOptions = includeFilterOptions)
        val key = cacheKey(parkId, workState, asOf, dueBefore, openOnly?.toString(), includeFilterOptions.toString(), limit?.toString())
        rowsDao.upsert(ExecutionRowsCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        rowsDao.enforceCacheBounds()
    }

    override suspend fun appendRows(
        cursor: String,
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = runCatching {
        rowsAppendMutex.withLock {
            val key = cacheKey(parkId, workState, asOf, dueBefore, openOnly?.toString(), includeFilterOptions.toString(), limit?.toString())
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
            val page = rows(parkId, workState, asOf, dueBefore, openOnly, limit, cursor, includeFilterOptions = false)
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
        partitionLabel: String?,
    ): VaccinationExecutionShedDrilldownDto =
        api.getVaccinationExecutionShed(shedId, asOf, dueBefore, limit, partitionLabel)

    override fun observeShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
        partitionLabel: String?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> {
        val key = cacheKey(shedId, executionPartitionKey(partitionLabel), asOf, dueBefore, limit?.toString())
        return shedDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
        partitionLabel: String?,
    ): Result<Unit> = runCatching {
        val dto = shed(shedId, asOf, dueBefore, limit, partitionLabel)
        val key = cacheKey(shedId, executionPartitionKey(partitionLabel), asOf, dueBefore, limit?.toString())
        shedDao.upsert(ExecutionShedCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        shedDao.enforceCacheBounds()
    }

    // Internal network fetch of one bounded scan-roster page. Not part of the screen-facing contract:
    // the offline-first read the UI observes is [observeScanRosterRows] (Room SSOT); this only feeds
    // [refreshScanRoster]'s walk into that SSOT.
    private suspend fun scanRoster(
        shedId: String,
        taskId: String?,
        cursor: String?,
        limit: Int?,
        partitionLabel: String?,
    ): ScanRosterResponseDto =
        api.getScanRoster(shedId, taskId, cursor, limit, partitionLabel)

    override fun observeScanRosterRows(
        shedId: String,
        taskId: String?,
        windowSize: Int,
        partitionLabel: String?,
    ): Flow<List<ScanRosterRowEntity>> =
        scanRosterRowDao.observeRowsWindow(scanRosterRowScopeKey(shedId, taskId, partitionLabel), windowSize)
            .flowOn(Dispatchers.Default)

    override fun observeScanRosterTotal(shedId: String, taskId: String?, partitionLabel: String?): Flow<Int> =
        scanRosterRowDao.observeScopeTotal(scanRosterRowScopeKey(shedId, taskId, partitionLabel)).flowOn(Dispatchers.Default)

    override fun observeScanRosterDoneGoatIds(shedId: String, taskId: String?, partitionLabel: String?): Flow<List<String>> =
        scanRosterRowDao.observeDoneGoatIds(scanRosterRowScopeKey(shedId, taskId, partitionLabel)).flowOn(Dispatchers.Default)

    override suspend fun scanRosterRowsByGoatIds(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
        partitionLabel: String?,
    ): List<ScanRosterRowEntity> =
        if (goatIds.isEmpty()) emptyList()
        else scanRosterRowDao.rowsByGoatIds(scanRosterRowScopeKey(shedId, taskId, partitionLabel), goatIds)

    override suspend fun refreshScanRoster(
        shedId: String,
        taskId: String?,
        limit: Int?,
        partitionLabel: String?,
    ): Result<Unit> = runCatching {
        scanAppendMutex.withLock {
            val rowScope = scanRosterRowScopeKey(shedId, taskId, partitionLabel)
            // Walk the WHOLE shed roster keyset page-by-page over the NETWORK first (each page stays
            // ~20 rows), staging the entities in one bounded per-shed buffer with backend `seq` order.
            // No DB transaction is held across the network I/O — a long multi-page walk must not block
            // proof-capture / outbox writers on the single SQLite write lock. `seq` records the backend
            // roster order so the windowed UI read is stable; forward progress is guaranteed by
            // seenCursors (a repeated cursor is a backend defect, not silent truncation). Only the
            // network DTOs are transient per page; the buffer holds one bounded copy of the roster
            // (a single shed = hundreds of animals), not the roster twice (R50-008).
            val staged = ArrayList<ScanRosterRowEntity>() // mobile-guard:ignore: transient function-local sync buffer, GC'd on return; one bounded per-shed roster copy, not a persisted blob
            val seenCursors = mutableSetOf<String>() // mobile-guard:ignore: function-local, GC'd on return; bounded by one shed's page count, not a persistent field
            var seq = 0L
            var cursor: String? = null
            var fetchTaskId = taskId
            var authoritativeForTask = !taskId.isNullOrBlank()
            while (true) {
                val page = try {
                    scanRoster(shedId, fetchTaskId, cursor = cursor, limit = limit, partitionLabel = partitionLabel)
                } catch (error: Throwable) {
                    if (cursor != null || taskId.isNullOrBlank() || !error.isHttpNotFound()) throw error
                    fetchTaskId = null
                    authoritativeForTask = false
                    scanRoster(shedId, taskId = null, cursor = null, limit = limit, partitionLabel = partitionLabel)
                }
                page.rows.forEach { staged += it.toRowEntity(rowScope, shedId, taskId, seq++, clock()) }
                val next = page.nextCursor ?: break
                if (!seenCursors.add(next)) {
                    throw ScanRosterCursorException("scan roster backend returned a non-advancing cursor")
                }
                cursor = next
            }
            // Publish in ONE short transaction AFTER the whole walk succeeds: a mid-walk network
            // failure throws before we touch the DB, so the previously-persisted roster is left intact
            // (offline-safe atomic replace) and the write lock is held only for the local upsert.
            val rows = staged.distinctBy { it.id }
            val serverDoneObligationIds = rows
                .asSequence()
                .filter { it.isServerDone() }
                .mapNotNull { it.obligationId.takeIf(String::isNotBlank) }
                .distinct()
                .toList()
            database.withTransaction {
                scanRosterRowDao.deleteForScope(rowScope)
                scanRosterRowDao.upsertAll(rows)
                // Cleanup stale scan records when proofs are rejected.
                // When the server no longer reports an animal as done (vaccination_completions deleted),
                // remove its SYNCED scan record so it doesn't persist as a false "scanned" marker.
                //
                // Which branch runs is decided from the ORIGINAL caller intent (the `taskId`
                // function parameter), never from `fetchTaskId`/`authoritativeForTask` alone:
                // - taskId != null AND authoritativeForTask: a genuine task-scoped roster fetch.
                //   Safe to prune by task id against the server's per-task view.
                // - taskId == null: a deliberate shed-wide roster fetch. The walk covers every
                //   obligation in the shed, so pruning by obligation id is authoritative.
                // - taskId != null AND NOT authoritativeForTask: the task-scoped endpoint 404'd
                //   and we fell back to the shed-wide endpoint ONLY to have something to show.
                //   That response was never requested as "the complete outstanding set" for this
                //   task — deciding authority from the data shape (a full shed page) while keying
                //   the delete on a different scope (obligation id, no task filter) is exactly the
                //   bug this comment used to invite: it deletes another task's/animal's synced
                //   evidence just because this task's endpoint was unavailable. Do nothing here;
                //   the next successful task-scoped or shed-wide refresh will prune correctly.
                when {
                    !taskId.isNullOrBlank() && authoritativeForTask ->
                        database.scannedGoatDao().pruneSyncedFieldToServerDone(
                            taskId = taskId,
                            partitionKey = executionPartitionKey(partitionLabel),
                            fieldKey = ROSTER_SCAN_FIELD_KEY,
                            serverDoneObligationIds = serverDoneObligationIds,
                        )
                    taskId.isNullOrBlank() ->
                        database.scannedGoatDao().pruneSyncedByRejectedObligations(
                            partitionKey = executionPartitionKey(partitionLabel),
                            fieldKey = ROSTER_SCAN_FIELD_KEY,
                            rejectedObligationIds = rows
                                .mapNotNull { it.obligationId.takeIf { id -> id.isNotBlank() } }
                                .distinct()
                                .toMutableList()
                                .apply {
                                    // Keep obligations that are still done on the server
                                    removeAll(serverDoneObligationIds.toSet())
                                },
                        )
                    else -> Unit // task-scoped fallback to shed-wide: not authoritative, prune nothing
                }
            }
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

    override suspend fun findScanRosterByTag(shedId: String, taskId: String?, normalizedTag: String, partitionLabel: String?): ScanRosterRowEntity? =
        scanRosterRowDao.findByTag(scanRosterRowScopeKey(shedId, taskId, partitionLabel), normalizedTag)

    override suspend fun findScanRosterRowsByTag(shedId: String, taskId: String?, normalizedTag: String, partitionLabel: String?): List<ScanRosterRowEntity> =
        scanRosterRowDao.findRowsByTag(scanRosterRowScopeKey(shedId, taskId, partitionLabel), normalizedTag)

    override suspend fun findScanRosterRowsByTaskAndTag(taskId: String, normalizedTag: String): List<ScanRosterRowEntity> =
        scanRosterRowDao.findRowsByTaskAndTag(taskId, normalizedTag)

    override suspend fun classifyScanTag(
        shedId: String,
        taskId: String,
        tag: String,
        partitionLabel: String?,
    ): ScanTagClassificationDto = api.classifyScanTag(shedId, taskId, tag, partitionLabel)

    override suspend fun getScanRosterStatusCounts(shedId: String, taskId: String?, partitionLabel: String?): List<StatusCount> =
        scanRosterRowDao.countByStatus(scanRosterRowScopeKey(shedId, taskId, partitionLabel))

    override fun observeScanRosterStatusCounts(shedId: String, taskId: String?, partitionLabel: String?): Flow<List<StatusCount>> =
        scanRosterRowDao.observeCountsByStatus(scanRosterRowScopeKey(shedId, taskId, partitionLabel)).flowOn(Dispatchers.Default)

    override suspend fun getScanRosterStatusCountsFor(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
        partitionLabel: String?,
    ): List<StatusCount> =
        if (goatIds.isEmpty()) emptyList()
        else scanRosterRowDao.countByStatusForGoats(scanRosterRowScopeKey(shedId, taskId, partitionLabel), goatIds)
}

private fun sg.mesha.goatos.core.network.dto.ScanRosterRowDto.toRowEntity(
    scopeKey: String,
    shedId: String,
    taskId: String?,
    seq: Long,
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
    vaccineLabel = humanizeVaccineLabel(vaccineLabel),
    status = status,
    scannedAtMs = scannedAt?.let(::parseServerInstantMs),
    obligationId = obligationId,
    seq = seq,
    updatedAt = now,
    obligationRowVersion = obligationRowVersion,
    shedName = shedName,
    partitionLabel = partitionLabel.orEmpty(),
    sourceShedName = sourceShedName.orEmpty(),
    operationalLocationDisplay = operationalLocationDisplay,
)

private fun ScanRosterRowEntity.isServerDone(): Boolean {
    val serverStatus = status.lowercase(Locale.US)
    // The STATUS decides, not the scan timestamp. A rejected animal keeps its scannedAt forever
    // (it really was scanned), so treating any timestamp as "done" marked a sent-back animal as
    // finished on the server, excluded it from the rejection prune, and left its local capture in
    // place -- the operator saw a green tick and "Proof synced" on the very animal he was supposed
    // to redo, and a re-scan was refused as "Already scanned".
    if (serverStatus in SERVER_OUTSTANDING_ROSTER_STATUSES) return false
    return scannedAtMs != null || serverStatus in SERVER_DONE_ROSTER_STATUSES
}

private fun humanizeVaccineLabel(raw: String): String {
    val trimmed = raw.trim()
    if (trimmed.isBlank()) return trimmed
    val withoutPrefix = trimmed
        .replace("Preventive Care Vaccination Matrix", "", ignoreCase = true)
        .replace("preventive_care_vaccination_matrix", "", ignoreCase = true)
        .trim(' ', '-', '·', '_')
    val alreadyHuman = withoutPrefix
        .replace(Regex("\\s*[·-]\\s*Dose\\s+\\d+\\b", RegexOption.IGNORE_CASE), "")
        .trim()
    val code = alreadyHuman
        .lowercase(Locale.ENGLISH)
        .replace(Regex("[^a-z0-9+]+"), "_")
        .trim('_')
        .removeSuffix("_first")
        .replace(Regex("_(?:dose_)?\\d+$"), "")
        .replace(Regex("_(adult|kid)_w\\d+$"), "")
        .replace(Regex("_(adult|kid)$"), "")
    return when (code) {
        "et_tt", "ettt", "et+tt" -> "ET+TT"
        "blue_tongue", "bt" -> "Blue Tongue"
        "ppr" -> "PPR"
        "fmd" -> "FMD"
        "goat_pox", "goatpox" -> "Goat Pox"
        "sheep_pox", "sheeppox" -> "Sheep Pox"
        "hs" -> "HS"
        else -> alreadyHuman.ifBlank { trimmed }
    }
}

private fun Throwable.isHttpNotFound(): Boolean =
    javaClass.name == "retrofit2.HttpException" &&
        runCatching { javaClass.getMethod("code").invoke(this) as? Int }
            .onFailure { android.util.Log.d("ExecutionRepository", "isHttpNotFound: reflection failed", it) }
            .getOrNull() == 404

internal fun scanRosterRowScopeKey(shedId: String, taskId: String?, partitionLabel: String?): String =
    cacheKey(shedId, executionPartitionKey(partitionLabel), taskId ?: "shed-wide")

internal fun executionPartitionKey(raw: String?): String {
    val normalized = raw.orEmpty().trim().lowercase()
        .replace(Regex("^part[\\s]+"), "")
    return normalized.ifBlank { "whole" }
}

internal fun canonicalRosterTag(tag: String): String = tag.filter(Char::isLetterOrDigit).lowercase()

private fun parseServerInstantMs(raw: String): Long? =
    runCatching { java.time.Instant.parse(raw).toEpochMilli() }
        .onFailure { android.util.Log.d("ExecutionRepository", "parseServerInstantMs: parse failed for '$raw'", it) }
        .getOrNull()

class ScanRosterCursorException(message: String) : IllegalStateException(message)
class ExecutionRowsCursorException(message: String) : IllegalStateException(message)

internal fun mergeExecutionRowsPage(
    current: VaccinationExecutionResponseDto,
    page: VaccinationExecutionResponseDto,
): VaccinationExecutionResponseDto = page.copy(
    totalCount = maxOf(current.totalCount, page.totalCount),
    // The FRESH page wins. This used to prefer the cached options, which meant a filter vocabulary
    // could never be replaced once cached: a principal who could see both parks left their park
    // chips behind for the next principal, so a CBE-scoped operator was offered a CPT chip and
    // could pull up another park's sheds. Cached options are only a fallback for a continuation
    // page, which legitimately omits them.
    filterOptions = page.filterOptions ?: current.filterOptions,
    rows = (current.rows + page.rows).distinctBy { row -> // mobile-guard:ignore: cursor-gated single-page append into a TTL+row/byte-capped blob (enforceCacheBounds)
        listOf(
            row.parkId,
            row.shedId,
            executionPartitionKey(row.partitionLabel ?: row.partition),
            row.animalStage,
            row.driveId.orEmpty(),
            row.batchId.orEmpty(),
            row.sopTaskId.orEmpty(),
            row.obligationId.orEmpty(),
        ).joinToString("|")
    },
)
