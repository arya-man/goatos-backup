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
        val capturedStartMs: Long,
        val capturedEndMs: Long,
        val capturedByPrincipalId: String?,
    )

    // The production DAO scopes every query (activeCountForField/activeCountForSubject/
    // observeForTaskPartition) by taskId + partitionKey, not partitionKey alone. [ProofCaptureRow]
    // itself has no taskId column (it's the UI-facing projection), so this fake tracks it
    // out-of-band alongside each row rather than folding two different tasks' rows together.
    private data class TrackedRow(val taskId: String, val row: ProofCaptureRow)

    private val rows = mutableListOf<TrackedRow>()
    private val flow = MutableStateFlow<List<TrackedRow>>(emptyList())
    val captureCalls = mutableListOf<CaptureCall>()
    private var nextId = 0

    override fun observeProofs(taskId: String, partitionLabel: String?): Flow<List<ProofCaptureRow>> =
        flow.map { list ->
            list.filter { it.taskId == taskId && it.row.partitionKey == testPartitionKey(partitionLabel) }
                .map { it.row }
        }

    fun seedProofs(vararg proofRows: ProofCaptureRow) {
        rows += proofRows.map { TrackedRow(taskId = "task-1", row = it) }
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
        captureCalls += CaptureCall(fieldKey, subject, subjectId, caption, rfidTag, localUri, capturedStartMs, capturedEndMs, capturedByPrincipalId)
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
        proofPolicy.maximumCountPerField?.let { perFieldCap ->
            // Mirrors activeCountForField: a DELIVERED row (serverProofId set) is history, not an
            // in-flight duplicate, so it does not hold the slot. Keeping it counted here would make
            // this fake disagree with the DAO and hide the reopened-pen case. Scoped by taskId too —
            // the real DAO query is taskId+partitionKey+fieldKey, and feed capture embeds the pen in
            // taskId (the capture group key), not just partitionKey.
            val activeForField = rows.count {
                it.taskId == taskId &&
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
            it.taskId == taskId &&
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
            outboxItemId = if (awaitUploadEnqueue) "proof-outbox-${nextId}" else null,
            lastError = null,
            partitionKey = partitionKey,
        )
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

    fun getProofById(id: String): ProofCaptureRow? = rows.find { it.row.id == id }?.row

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
        rows.removeAll { it.taskId == taskId }
        flow.value = rows.toList()
    }

    override suspend fun activeCount(slot: EvidenceSlot): Int {
        val partitionKey = testPartitionKey(slot.identity.partitionKey.takeUnless { it == "whole" })
        return rows.count {
            it.taskId == slot.identity.taskId &&
                it.row.partitionKey == partitionKey &&
                it.row.fieldKey == slot.fieldKey &&
                it.row.syncStatus != CaptureSyncStatus.FAILED &&
                it.row.serverProofId == null
        }
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

