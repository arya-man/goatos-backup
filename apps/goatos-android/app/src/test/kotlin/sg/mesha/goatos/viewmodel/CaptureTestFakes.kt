package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.TaskDetail
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
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
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.ShedCompletionSummaryDto

/** In-memory [ScanCaptureRepository] test double — real dedup semantics (unique per
 *  task+field+tag), no Room. Mirrors [sg.mesha.goatos.core.data.capture.DefaultScanCaptureRepository]'s
 *  observable contract closely enough to drive [SubmitViewModel] tests. */
class FakeScanCaptureRepository : ScanCaptureRepository {
    private val rows = mutableListOf<ScannedGoatRow>()
    private val rowTaskIds = mutableListOf<String>()
    private val flow = MutableStateFlow<List<ScannedGoatRow>>(emptyList())
    var recordScanCalls: Int = 0
        private set
    var enqueuePendingScansCalls: Int = 0
        private set

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
        if (rows.none { it.partitionKey == partitionKey && it.fieldKey == fieldKey && it.tag == tag }) {
            rows += ScannedGoatRow(
                fieldKey = fieldKey,
                tag = tag,
                goatId = goatId,
                obligationId = obligationId,
                capturedAtMs = capturedAtMs ?: rows.size.toLong(),
                partitionKey = partitionKey,
            )
            rowTaskIds += taskId
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
        if (rows.any { it.partitionKey == partitionKey && it.fieldKey == fieldKey && it.tag == tag }) return false
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
        rows.filter { it.partitionKey == testPartitionKey(partitionLabel) }.map { it.tag }

    fun rowsForTask(taskId: String): List<ScannedGoatRow> =
        rows.zip(rowTaskIds).filter { (_, rowTaskId) -> rowTaskId == taskId }.map { (row, _) -> row }

    override suspend fun clearForTask(taskId: String) {
        rows.clear()
        rowTaskIds.clear()
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
    data class CaptureCall(
        val fieldKey: String,
        val subject: ProofSubject,
        val subjectId: String?,
        // Free-flow weighing does NOT resolve an RFID tag to an animal id, so its proof rows carry
        // subjectId = null and identify the animal by CAPTION (the scanned tag). Attribution tests
        // for that module need the caption, not just the subject id.
        val caption: String?,
        val localUri: String,
        val capturedStartMs: Long,
        val capturedEndMs: Long,
        val capturedByPrincipalId: String?,
    )

    private val rows = mutableListOf<ProofCaptureRow>()
    private val flow = MutableStateFlow<List<ProofCaptureRow>>(emptyList())
    val captureCalls = mutableListOf<CaptureCall>()
    private var nextId = 0

    override fun observeProofs(taskId: String, partitionLabel: String?): Flow<List<ProofCaptureRow>> =
        flow.map { list -> list.filter { it.partitionKey == testPartitionKey(partitionLabel) } }

    fun seedProofs(vararg proofRows: ProofCaptureRow) {
        rows += proofRows
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
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
        proofPolicy: ProofPolicy,
        partitionLabel: String?,
    ): AppResult<ProofCaptureRow> {
        captureCalls += CaptureCall(fieldKey, subject, subjectId, caption, localUri, capturedStartMs, capturedEndMs, capturedByPrincipalId)
        // R50-027 / shed-level vaccination proof: mirror production repository cap selection.
        // Per-goat proof uses per-subject cap; shed-level proof uses the SOP's shed total cap
        // because the whole shed is the proof subject.
        val effectiveMaxProofs = if (proofPolicy.isShedLevelVideo && subject == ProofSubject.SHED) {
            proofPolicy.maximumCount
        } else {
            proofPolicy.maximumCountPerSubject
        }
        val partitionKey = testPartitionKey(partitionLabel)
        val activeRows = rows.count {
            it.partitionKey == partitionKey &&
                it.subjectId == subjectId &&
                it.syncStatus != CaptureSyncStatus.FAILED
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
            lastError = null,
            partitionKey = partitionKey,
        )
        rows += row
        flow.value = rows.toList()
        return AppResult.Ok(row)
    }

    override suspend fun updateCaption(taskId: String, id: String, caption: String): AppResult<Unit> {
        val index = rows.indexOfFirst { it.id == id }
        if (index >= 0) {
            rows[index] = rows[index].copy(caption = caption)
            flow.value = rows.toList()
        }
        return AppResult.Ok(Unit)
    }

    override suspend fun remove(taskId: String, id: String): AppResult<Unit> {
        rows.removeAll { it.id == id }
        flow.value = rows.toList()
        return AppResult.Ok(Unit)
    }

    fun markSynced(id: String, serverProofId: String) {
        val index = rows.indexOfFirst { it.id == id }
        if (index >= 0) {
            rows[index] = rows[index].copy(syncStatus = CaptureSyncStatus.SYNCED, serverProofId = serverProofId)
            flow.value = rows.toList()
        }
    }

    fun markFailed(id: String, error: String) {
        val index = rows.indexOfFirst { it.id == id }
        if (index >= 0) {
            rows[index] = rows[index].copy(syncStatus = CaptureSyncStatus.FAILED, lastError = error)
            flow.value = rows.toList()
        }
    }

    fun markInFlight(id: String) {
        val index = rows.indexOfFirst { it.id == id }
        if (index >= 0) {
            rows[index] = rows[index].copy(syncStatus = CaptureSyncStatus.IN_FLIGHT)
            flow.value = rows.toList()
        }
    }

    override suspend fun retryUpload(taskId: String, id: String): AppResult<Unit> {
        val index = rows.indexOfFirst { it.id == id }
        if (index >= 0 && rows[index].syncStatus == CaptureSyncStatus.FAILED) {
            rows[index] = rows[index].copy(syncStatus = CaptureSyncStatus.PENDING, lastError = null)
            flow.value = rows.toList()
        }
        return AppResult.Ok(Unit)
    }

    override suspend fun clearForTask(taskId: String) {
        rows.clear()
        flow.value = emptyList()
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

/** Minimal [TasksRepository] test double for [ScanViewModel]'s R50-027 proof-policy read — only
 *  [observeTaskDetail] is exercised (the roster scan screen never lists/refreshes tasks itself).
 *  Defaults to [ProofPolicy.Default] (no [detail] supplied) so existing scan tests that do not
 *  care about proof policy keep their historical hardcoded-constant behavior unchanged. */
class FakeTasksRepositoryForCapture(
    private val detail: TaskDetail? = null,
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
