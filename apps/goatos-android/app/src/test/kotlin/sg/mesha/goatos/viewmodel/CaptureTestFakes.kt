package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.TaskDetail
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.RfidScanAttemptOutcome
import sg.mesha.goatos.core.data.capture.RfidScanAttemptRow
import sg.mesha.goatos.core.data.capture.RfidScanTagRole
import sg.mesha.goatos.core.data.capture.ScanAttemptRepository
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.data.capture.ScannedGoatRow
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.database.capture.ProofProcessingState
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.ShedCompletionSummaryDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto

/** In-memory [ScanCaptureRepository] test double — real dedup semantics (unique per
 *  task+field+tag+obligation), no Room. Mirrors
 *  [sg.mesha.goatos.core.data.capture.DefaultScanCaptureRepository]'s observable contract closely
 *  enough to drive [SubmitViewModel] tests: durable rows are obligation-grained, while submitted
 *  tag answers are still distinct animal/RFID scans. */
class FakeScanCaptureRepository : ScanCaptureRepository {
    private val rows = mutableListOf<ScannedGoatRow>()
    private val flow = MutableStateFlow<List<ScannedGoatRow>>(emptyList())
    var recordScanCalls: Int = 0
        private set
    var enqueuePendingScansCalls: Int = 0
        private set

    /** Test hook: flip every stored capture to SYNCED — the precondition for any server-side
     *  reconciliation (a PENDING capture is offline evidence the server has not seen). */
    fun markAllSynced() {
        rows.replaceAll { it.copy(syncStatus = CaptureSyncStatus.SYNCED) }
        flow.value = rows.toList()
    }

    override fun observeScannedTags(taskId: String, fieldKey: String, partitionLabel: String?): Flow<List<ScannedGoatRow>> =
        flow.map { list ->
            list.filter { it.fieldKey == fieldKey && it.partitionKey == testPartitionKey(partitionLabel) }
        }

    override fun observeScannedCount(taskId: String, fieldKey: String, partitionLabel: String?): Flow<Int> =
        flow.map { list ->
            list.count { it.fieldKey == fieldKey && it.partitionKey == testPartitionKey(partitionLabel) }
        }

    override fun observeAllForTask(taskId: String, partitionLabel: String?): Flow<List<ScannedGoatRow>> =
        flow.map { list -> list.filter { it.partitionKey == testPartitionKey(partitionLabel) } }

    override suspend fun recordScan(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String?,
        obligationId: String?,
        obligationRowVersion: Int,
        capturedAtMs: Long?,
        partitionLabel: String?,
    ) {
        recordScanCalls++
        val partitionKey = testPartitionKey(partitionLabel)
        if (rows.none {
            it.partitionKey == partitionKey &&
                it.fieldKey == fieldKey &&
                it.tag == tag &&
                it.obligationId == obligationId
        }) {
            rows += ScannedGoatRow(
                fieldKey = fieldKey,
                tag = tag,
                goatId = goatId,
                obligationId = obligationId,
                capturedAtMs = capturedAtMs ?: rows.size.toLong(),
                partitionKey = partitionKey,
                obligationRowVersion = obligationRowVersion,
            )
            flow.value = rows.toList()
        }
    }

    override suspend fun recordLocalScanIfAbsent(
        taskId: String,
        fieldKey: String,
        tag: String,
        capturedAtMs: Long?,
        partitionLabel: String?,
    ): Boolean {
        val partitionKey = testPartitionKey(partitionLabel)
        if (rows.any { it.partitionKey == partitionKey && it.fieldKey == fieldKey && it.tag == tag && it.obligationId == null }) return false
        recordScan(taskId, fieldKey, tag, capturedAtMs = capturedAtMs, partitionLabel = partitionLabel)
        return true
    }

    override suspend fun enqueuePendingScans(taskId: String, fieldKey: String, partitionLabel: String?) {
        enqueuePendingScansCalls++
    }

    override suspend fun markLocalScanSynced(taskId: String, fieldKey: String, tag: String, partitionLabel: String?) {
        val partitionKey = testPartitionKey(partitionLabel)
        val index = rows.indexOfFirst {
            it.partitionKey == partitionKey && it.fieldKey == fieldKey && it.tag == tag
        }
        if (index < 0) return
        rows[index] = rows[index].copy(syncStatus = CaptureSyncStatus.SYNCED)
        flow.value = rows.toList()
    }

    override suspend fun tagsForTask(taskId: String, partitionLabel: String?): List<String> =
        rows.filter { it.partitionKey == testPartitionKey(partitionLabel) }.map { it.tag }.distinct()

    fun rowsForTask(taskId: String): List<ScannedGoatRow> = rows.filter { it.fieldKey.isNotBlank() }

    override suspend fun clearForTask(taskId: String) {
        rows.clear()
        flow.value = emptyList()
    }
}

class FakeScanAttemptRepository : ScanAttemptRepository {
    data class AttemptCall(
        val taskId: String,
        val fieldKey: String,
        val tag: String,
        val goatId: String?,
        val obligationId: String?,
        val outcome: RfidScanAttemptOutcome,
        val tagRole: RfidScanTagRole,
        val reason: String?,
        val capturedAtMs: Long?,
    )

    val calls = mutableListOf<AttemptCall>()
    private val rows = mutableListOf<RfidScanAttemptRow>()
    private val flow = MutableStateFlow<List<RfidScanAttemptRow>>(emptyList())

    override fun observeAttempts(taskId: String): Flow<List<RfidScanAttemptRow>> =
        flow.map { list -> list.filter { it.taskId == taskId } }

    override suspend fun recordAttempt(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String?,
        obligationId: String?,
        outcome: RfidScanAttemptOutcome,
        tagRole: RfidScanTagRole,
        reason: String?,
        capturedAtMs: Long?,
    ) {
        calls += AttemptCall(taskId, fieldKey, tag, goatId, obligationId, outcome, tagRole, reason, capturedAtMs)
        rows += RfidScanAttemptRow(
            id = "attempt-${rows.size}",
            taskId = taskId,
            fieldKey = fieldKey,
            tag = tag,
            normalizedTag = tag.filter { it.isLetterOrDigit() }.lowercase(),
            goatId = goatId,
            obligationId = obligationId,
            outcome = outcome,
            tagRole = tagRole,
            reason = reason,
            capturedAtMs = capturedAtMs ?: rows.size.toLong(),
        )
        flow.value = rows.toList()
    }

    override suspend fun attemptsForTask(taskId: String): List<RfidScanAttemptRow> =
        rows.filter { it.taskId == taskId }

    override suspend fun clearForTask(taskId: String) {
        rows.removeAll { it.taskId == taskId }
        flow.value = rows.toList()
    }
}

/** In-memory [ProofCaptureRepository] test double — enforces the same 5-video cap the Room-
 *  backed implementation does, records every [capture] call's arguments for assertions. */
class FakeProofCaptureRepository(private val maxProofs: Int = 5) : ProofCaptureRepository {
    companion object {
        /** Sentinel taskId for seeded rows: matches any observed/queried task. */
        const val SEEDED_ANY_TASK = "__seeded-any-task__"
    }

    data class CaptureCall(
        val fieldKey: String,
        val subject: ProofSubject,
        val subjectId: String?,
        // Free-flow weighing does NOT resolve an RFID tag to an animal id, so its proof rows carry
        // subjectId = null and identify the animal by CAPTION (the scanned tag). Attribution tests
        // for that module need the caption, not just the subject id.
        val caption: String?,
        val rfidTag: String?,
        val localUri: String,
        val mimeType: String,
        val capturedStartMs: Long,
        val capturedEndMs: Long,
        val capturedByPrincipalId: String?,
        val uploadGroupKey: String?,
    )

    // The production DAO scopes every query (activeCountForField/activeCountForSubject/
    // observeForTaskPartition) by taskId + partitionKey, not partitionKey alone. [ProofCaptureRow]
    // itself has no taskId column (it's the UI-facing projection), so this fake tracks it
    // out-of-band alongside each row rather than folding two different tasks' rows together.
    private data class TrackedRow(val taskId: String, val row: ProofCaptureRow)

    private fun TrackedRow.matches(taskId: String) = this.taskId == taskId || this.taskId == SEEDED_ANY_TASK

    private val rows = mutableListOf<TrackedRow>()
    private val flow = MutableStateFlow<List<TrackedRow>>(emptyList())
    val captureCalls = mutableListOf<CaptureCall>()
    private var nextId = 0

    /** When true, the NEXT [capture] call returns [AppResult.Err] instead of writing a row, then
     *  resets itself — drives "cancelled/failed re-capture must not lose the old proof" tests. */
    var failNextCapture: Boolean = false

    /** Simulates a production processing/enqueue failure after the local proof row is saved but
     * before a proof-upload outbox item exists. */
    var omitNextOutboxItem: Boolean = false

    /** Deferred retirement actions, keyed by the NEW proof row id, matching production's durable
     *  supersession contract: old rows are only removed once the new row reaches SYNCED with a
     *  serverProofId. Tests call [driveSlotRetirementIfPending] to simulate this transition. */
    private val pendingSlotRetirement = mutableMapOf<String, suspend () -> Unit>()

    override fun observeProofs(taskId: String, partitionLabel: String?): Flow<List<ProofCaptureRow>> =
        flow.map { list ->
            list.filter {
                (it.taskId == taskId || it.taskId == SEEDED_ANY_TASK) &&
                    it.row.partitionKey == testPartitionKey(partitionLabel)
            }.map { it.row }
        }

    /** All live rows regardless of task scoping — for tests asserting row survival, not scoping. */
    fun allRows(): List<ProofCaptureRow> = rows.map { it.row }

    /**
     * Seeded rows model "a proof already durably exists for whatever task the screen addresses" —
     * seeding tests don't know (and shouldn't reconstruct) the production task key, so seeded rows
     * match ANY observed taskId. Rows written through [capture] keep strict taskId scoping, which
     * is what the pen-grain tests assert.
     */
    fun seedProofs(vararg proofRows: ProofCaptureRow) {
        rows += proofRows.map { TrackedRow(taskId = SEEDED_ANY_TASK, row = it) }
        flow.value = rows.toList()
    }

    override suspend fun capture(
        taskId: String,
        fieldKey: String,
        subject: ProofSubject,
        subjectId: String?,
        localUri: String,
        mimeType: String,
        caption: String?,
        rfidTag: String?,
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
        proofPolicy: ProofPolicy,
        partitionLabel: String?,
        awaitUploadEnqueue: Boolean,
        uploadGroupKey: String?,
        allowReplacementOverCap: Boolean,
    ): AppResult<ProofCaptureRow> {
        if (failNextCapture) {
            failNextCapture = false
            return AppResult.Err("Simulated capture failure.")
        }
        captureCalls += CaptureCall(
            fieldKey,
            subject,
            subjectId,
            caption,
            rfidTag,
            localUri,
            mimeType,
            capturedStartMs,
            capturedEndMs,
            capturedByPrincipalId,
            uploadGroupKey,
        )
        // R50-027 / shed-level vaccination proof: mirror production repository cap selection.
        // Per-goat proof uses per-subject cap; shed-level proof uses the SOP's shed total cap
        // because the whole shed is the proof subject.
        val effectiveMaxProofs = if (proofPolicy.isShedLevelVideo && subject == ProofSubject.SHED) {
            proofPolicy.maximumCount
        } else {
            proofPolicy.maximumCountPerSubject
        }
        val partitionKey = testPartitionKey(partitionLabel)
        // Mirror the production per-SLOT cap. Without this the fake pools every slot under the
        // subject cap, which is exactly the behaviour the real repository stopped doing.
        // allowReplacementOverCap mirrors DefaultProofCaptureRepository.capture()'s own
        // `perFieldCap != null && !allowReplacementOverCap` guard: captureReplacingLatest's
        // transient second row (new capture landing before the old one is removed) must bypass
        // the cap the same way production does, or every field with maximumCountPerField reachable
        // via captureReplacingLatest would wrongly fail its FIRST replace attempt in this fake.
        proofPolicy.maximumCountPerField?.takeUnless { allowReplacementOverCap }?.let { perFieldCap ->
            // Mirrors activeCountForField: a DELIVERED row (serverProofId set) is history, not an
            // in-flight duplicate, so it does not hold the slot. Keeping it counted here would make
            // this fake disagree with the DAO and hide the reopened-pen case. Scoped by taskId too —
            // the real DAO query is taskId+partitionKey+fieldKey, and feed capture embeds the pen in
            // taskId (the capture group key), not just partitionKey.
            val activeForField = rows.count {
                it.matches(taskId) &&
                    it.row.partitionKey == partitionKey &&
                    it.row.fieldKey == fieldKey &&
                    it.row.syncStatus != CaptureSyncStatus.FAILED &&
                    it.row.serverProofId == null
            }
            if (activeForField >= perFieldCap) {
                return AppResult.Err("This proof is already recorded. Use re-capture to replace it.")
            }
        }
        val activeRows = rows.count {
            it.matches(taskId) &&
                it.row.partitionKey == partitionKey &&
                it.row.subjectId == subjectId &&
                it.row.syncStatus != CaptureSyncStatus.FAILED
        }
        if (activeRows >= effectiveMaxProofs) {
            val subjectLabel = when (subject) {
                ProofSubject.GOAT -> "goat"
                ProofSubject.SHED -> "shed"
                ProofSubject.VIAL_LOT -> "vial"
                ProofSubject.ADMINISTRATION -> "administration"
                else -> "subject"
            }
            return AppResult.Err("Maximum $effectiveMaxProofs proof videos reached for this $subjectLabel.")
        }
        val row = ProofCaptureRow(
            id = "proof-${nextId++}",
            fieldKey = fieldKey,
            proofSubject = subject,
            subjectId = subjectId,
            localUri = localUri,
            mimeType = mimeType,
            caption = caption,
            capturedAtMs = capturedStartMs,
            capturedStartMs = capturedStartMs,
            capturedEndMs = capturedEndMs,
            capturedByPrincipalId = capturedByPrincipalId,
            syncStatus = CaptureSyncStatus.PENDING,
            serverProofId = null,
            outboxItemId = if (awaitUploadEnqueue && !omitNextOutboxItem) "proof-outbox-${nextId}" else null,
            lastError = null,
            partitionKey = partitionKey,
            processingState = if (omitNextOutboxItem) {
                ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name
            } else {
                "CAPTURED_ORIGINAL"
            },
        )
        omitNextOutboxItem = false
        rows += TrackedRow(taskId = taskId, row = row)
        flow.value = rows.toList()
        return AppResult.Ok(row)
    }

    override suspend fun updateCaption(taskId: String, id: String, caption: String): AppResult<Unit> {
        val index = rows.indexOfFirst { it.row.id == id }
        if (index >= 0) {
            rows[index] = rows[index].copy(row = rows[index].row.copy(caption = caption))
            flow.value = rows.toList()
        }
        return AppResult.Ok(Unit)
    }

    override suspend fun remove(taskId: String, id: String): AppResult<Unit> {
        rows.removeAll { it.row.id == id }
        flow.value = rows.toList()
        return AppResult.Ok(Unit)
    }

    /** Mark a proof row's sync status. This is a non-suspend version that does NOT fire pending
     *  retirement actions — use [markSyncedAndDriveRetirement] to simulate the row reaching SYNCED
     *  and trigger old-row cleanup. Kept for backward compatibility with existing tests that call
     *  markSynced directly. */
    fun markSynced(id: String, serverProofId: String, syncStatus: String = "SYNCED") {
        val index = rows.indexOfFirst { it.row.id == id }
        if (index >= 0) {
            val status = when (syncStatus.uppercase()) {
                "SYNCED" -> CaptureSyncStatus.SYNCED
                "PENDING" -> CaptureSyncStatus.PENDING
                "FAILED" -> CaptureSyncStatus.FAILED
                "IN_FLIGHT" -> CaptureSyncStatus.IN_FLIGHT
                else -> CaptureSyncStatus.SYNCED
            }
            rows[index] = rows[index].copy(row = rows[index].row.copy(syncStatus = status, serverProofId = serverProofId))
            flow.value = rows.toList()
        }
    }

    /**
     * Mark a proof row as SYNCED and fire any pending slot-retirement action keyed by this row's id.
     * This is the test hook matching production behavior: old rows are removed only when the
     * replacement reaches SYNCED + serverProofId. Tests that verify old-row survival during upload
     * should use [markInFlight] instead (no retirement), then call this method once the new row
     * should complete its upload.
     *
     * Regression test case: old row SURVIVES while replacement is uploading/FAILED.
     * 1. captureReplacingLatest succeeds (new row written)
     * 2. assert old row still exists (markInFlight only, not markSyncedAndDriveRetirement yet)
     * 3. mark new row as SYNCED (this fires retirement)
     * 4. assert old row is now gone
     */
    suspend fun markSyncedAndDriveRetirement(id: String, serverProofId: String) {
        markSynced(id, serverProofId, "SYNCED")
        val action = pendingSlotRetirement.remove(id)
        action?.invoke()
    }

    fun getProofById(id: String): ProofCaptureRow? = rows.find { it.row.id == id }?.row

    /** Fire ALL pending slot-retirement actions and clear the registry. Convenience for tests that
     *  do not care about intermediate upload state and want to simulate the full replace cycle
     *  completing. Tests that verify old-row survival during IN_FLIGHT should NOT call this;
     *  they should instead use [markInFlight] (no retirement) then [markSyncedAndDriveRetirement]
     *  once they want to complete the replacement. */
    suspend fun driveAllPendingRetirements() {
        val toFire = pendingSlotRetirement.values.toList()
        pendingSlotRetirement.clear()
        toFire.forEach { it.invoke() }
    }

    fun markFailed(id: String, error: String) {
        val index = rows.indexOfFirst { it.row.id == id }
        if (index >= 0) {
            rows[index] = rows[index].copy(row = rows[index].row.copy(syncStatus = CaptureSyncStatus.FAILED, lastError = error))
            flow.value = rows.toList()
        }
    }

    fun markInFlight(id: String) {
        val index = rows.indexOfFirst { it.row.id == id }
        if (index >= 0) {
            rows[index] = rows[index].copy(row = rows[index].row.copy(syncStatus = CaptureSyncStatus.IN_FLIGHT))
            flow.value = rows.toList()
        }
    }

    override suspend fun retryUpload(taskId: String, id: String): AppResult<Unit> {
        val index = rows.indexOfFirst { it.row.id == id }
        if (index >= 0 && rows[index].row.syncStatus == CaptureSyncStatus.FAILED) {
            rows[index] = rows[index].copy(row = rows[index].row.copy(syncStatus = CaptureSyncStatus.PENDING, lastError = null))
            flow.value = rows.toList()
        }
        return AppResult.Ok(Unit)
    }

    override suspend fun clearForTask(taskId: String) {
        rows.removeAll { it.matches(taskId) }
        flow.value = rows.toList()
    }

    override suspend fun activeCount(slot: EvidenceSlot): Int {
        val partitionKey = testPartitionKey(slot.identity.partitionKey.takeUnless { it == "whole" })
        return rows.count {
            it.matches(slot.identity.taskId) &&
                it.row.partitionKey == partitionKey &&
                it.row.fieldKey == slot.fieldKey &&
                it.row.syncStatus != CaptureSyncStatus.FAILED &&
                it.row.serverProofId == null
        }
    }

    override suspend fun captureReplacingLatest(
        slot: EvidenceSlot,
        subject: ProofSubject,
        subjectId: String?,
        localUri: String,
        mimeType: String,
        caption: String?,
        rfidTag: String?,
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
        proofPolicy: ProofPolicy,
        awaitUploadEnqueue: Boolean,
        uploadGroupKey: String?,
    ): AppResult<ProofCaptureRow> {
        val taskId = slot.identity.taskId
        val partitionLabel = slot.identity.partitionKey.takeUnless { it == "whole" }
        val result = capture(
            taskId = taskId,
            fieldKey = slot.fieldKey,
            subject = subject,
            subjectId = subjectId,
            localUri = localUri,
            mimeType = mimeType,
            caption = caption,
            rfidTag = rfidTag,
            scopeType = scopeType,
            scopeId = scopeId,
            capturedStartMs = capturedStartMs,
            capturedEndMs = capturedEndMs,
            capturedByPrincipalId = capturedByPrincipalId,
            proofPolicy = proofPolicy,
            partitionLabel = partitionLabel,
            awaitUploadEnqueue = awaitUploadEnqueue,
            uploadGroupKey = uploadGroupKey,
            allowReplacementOverCap = true,
        )
        if (result is AppResult.Ok) {
            val newId = result.value.id
            val partitionKey = testPartitionKey(partitionLabel)
            // P1 FIX: DEFER retirement of the old row(s) until the new row reaches SYNCED with
            // a serverProofId, matching production behavior. This prevents data loss if the new
            // upload fails — the old row stays viable evidence until the new one is durably stored.
            // Register a one-shot retirement action, keyed by the new row's id, that tests invoke
            // via driveSlotRetirementIfPending() to simulate the new row reaching SYNCED.
            val toRemoveIds = rows.filter {
                it.matches(taskId) &&
                    it.row.partitionKey == partitionKey &&
                    it.row.fieldKey == slot.fieldKey &&
                    it.row.syncStatus != CaptureSyncStatus.FAILED &&
                    it.row.id != newId
            }.map { it.row.id }
            if (toRemoveIds.isNotEmpty()) {
                pendingSlotRetirement[newId] = {
                    toRemoveIds.forEach { remove(taskId, it) }
                }
            }
        }
        return result
    }
}

private fun testPartitionKey(partitionLabel: String?): String {
    val normalized = partitionLabel.orEmpty().trim().lowercase()
        .replace(Regex("^part[\\s]+"), "")
    return normalized.ifBlank { "whole" }
}

/** Defaults to an operator profile present (capture allowed) — pass `profile = null` to test
 *  the approver-only/leadership role-blocked path. */
class FakeCaptureBootstrapRepository(
    private val profile: BootstrapOperatorProfileDto? = BootstrapOperatorProfileDto(operatorId = "operator-1", primaryRoleHint = "operator"),
    // Execution capability is backend-compiled into the bootstrap feature flags, NOT inferred from
    // the role hint above. Defaults to authorized so existing capture tests keep their behavior.
    private val featureFlags: Map<String, Boolean> = mapOf("vaccination_execute" to true),
) : BootstrapRepository {
    override suspend fun loadNavState(): NavState = NavState.Empty.copy(featureFlags = featureFlags)
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto? = profile
}

/** Minimal [TasksRepository] test double for [ScanViewModel]'s proof-policy read. Vaccination
 *  execution is per-goat proof by default; scan-only tests must opt out explicitly so the fixture
 *  cannot mask the production camera path. */
class FakeTasksRepositoryForCapture(
    private val detail: TaskDetail? = proofTaskDetail(),
    initialSummary: ShedCompletionSummaryDto? = null,
    private val summaryOnRefresh: ShedCompletionSummaryDto? = initialSummary,
) : TasksRepository {
    private val summaries = mutableMapOf<Pair<String, String?>, MutableStateFlow<ShedCompletionSummaryDto?>>()
    val refreshedShedIds = mutableListOf<String?>()

    override suspend fun taskDetail(taskId: String): TaskDetail = detail ?: error("unused")
    override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> =
        MutableStateFlow(Resource(data = detail))
    override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = Result.success(Unit)
    override fun observeShedCompletionSummary(taskId: String, shedId: String?, partitionLabel: String?): Flow<ShedCompletionSummaryDto?> =
        summaryFlow(taskId, shedId)
    override suspend fun refreshShedCompletionSummary(taskId: String, shedId: String?, partitionLabel: String?): Result<Unit> {
        refreshedShedIds += shedId
        summaryFlow(taskId, shedId).value = summaryOnRefresh
        return Result.success(Unit)
    }

    private fun summaryFlow(taskId: String, shedId: String?): MutableStateFlow<ShedCompletionSummaryDto?> =
        summaries.getOrPut(taskId to shedId) { MutableStateFlow(initialSummaryForKey(shedId)) }

    private fun initialSummaryForKey(shedId: String?): ShedCompletionSummaryDto? =
        if (shedId == null) null else summaryOnRefresh
}

fun noProofTaskDetail(): TaskDetail = TaskDetail(
    task = TaskSummaryDto(taskId = "task-1", scopeType = "shed", scopeId = "shed-1", rowVersion = 1),
    form = FormSpec.Empty,
    proofPolicy = ProofPolicy(
        types = emptyList(),
        required = false,
        proofMode = "none",
        subjectScope = "",
        expectedSubjects = emptyList(),
        minimumCount = 0,
        maximumCount = 0,
        minimumCountPerSubject = 0,
        maximumCountPerSubject = 0,
        allowedCaptureSources = emptyList(),
    ),
)

private fun proofTaskDetail(): TaskDetail = TaskDetail(
    task = TaskSummaryDto(taskId = "task-1", scopeType = "shed", scopeId = "shed-1", rowVersion = 1),
    form = FormSpec.Empty,
    proofPolicy = ProofPolicy.Default,
)

// Test helpers for proof policies across different flows

fun vaccGoatProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "goat_level_video",
        subjectScope = ProofSubject.GOAT.wireValue,
        expectedSubjects = listOf(ProofSubject.GOAT.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
        maximumCount = 5,
        maximumCountPerSubject = 1,
    )

fun genericSubmitProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "shed_level_video",
        subjectScope = ProofSubject.SHED.wireValue,
        expectedSubjects = listOf(ProofSubject.SHED.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
        maximumCount = 1,
        maximumCountPerSubject = 1,
    )

fun weighingIndividualProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "goat_level_video",
        subjectScope = ProofSubject.GOAT.wireValue,
        expectedSubjects = listOf(ProofSubject.GOAT.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
        maximumCount = 5,
        maximumCountPerSubject = 1,
    )

fun weighingShedProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "shed_level_video",
        subjectScope = ProofSubject.SHED.wireValue,
        expectedSubjects = listOf(ProofSubject.SHED.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
        maximumCount = 5,
        maximumCountPerSubject = 5,
    )

fun feedShedProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "shed_level_video",
        subjectScope = ProofSubject.SHED.wireValue,
        expectedSubjects = listOf(ProofSubject.SHED.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
        maximumCount = 5,
    )

/** Shared test double for analytics. Used across multiple test files to avoid redeclaration. */
class FakeAnalyticsPort : sg.mesha.goatos.core.analytics.AnalyticsPort {
    val events = mutableListOf<Pair<String, Map<String, String>>>()
    override fun track(event: String, props: Map<String, String>) { events += event to props }
    override fun setUserProperty(name: String, value: String?) = Unit
    override fun setUserId(id: String?) = Unit
}
