package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.data.capture.ScannedGoatRow
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto

/** In-memory [ScanCaptureRepository] test double — real dedup semantics (unique per
 *  task+field+tag), no Room. Mirrors [sg.mesha.goatos.core.data.capture.DefaultScanCaptureRepository]'s
 *  observable contract closely enough to drive [SubmitViewModel] tests. */
class FakeScanCaptureRepository : ScanCaptureRepository {
    private val rows = mutableListOf<ScannedGoatRow>()
    private val flow = MutableStateFlow<List<ScannedGoatRow>>(emptyList())
    var recordScanCalls: Int = 0
        private set

    override fun observeScannedTags(taskId: String, fieldKey: String): Flow<List<ScannedGoatRow>> =
        flow.map { list -> list.filter { it.fieldKey == fieldKey } }

    override fun observeScannedCount(taskId: String, fieldKey: String): Flow<Int> =
        flow.map { list -> list.count { it.fieldKey == fieldKey } }

    override fun observeAllForTask(taskId: String): Flow<List<ScannedGoatRow>> = flow

    override suspend fun recordScan(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String?,
        obligationId: String?,
    ) {
        recordScanCalls++
        if (rows.none { it.fieldKey == fieldKey && it.tag == tag }) {
            rows += ScannedGoatRow(
                fieldKey = fieldKey,
                tag = tag,
                goatId = goatId,
                obligationId = obligationId,
                capturedAtMs = rows.size.toLong(),
            )
            flow.value = rows.toList()
        }
    }

    override suspend fun tagsForTask(taskId: String): List<String> = rows.map { it.tag }

    override suspend fun clearForTask(taskId: String) {
        rows.clear()
        flow.value = emptyList()
    }
}

/** In-memory [ProofCaptureRepository] test double — enforces the same 5-video cap the Room-
 *  backed implementation does, records every [capture] call's arguments for assertions. */
class FakeProofCaptureRepository(private val maxProofs: Int = 5) : ProofCaptureRepository {
    data class CaptureCall(
        val fieldKey: String,
        val subject: ProofSubject,
        val localUri: String,
        val capturedStartMs: Long,
        val capturedEndMs: Long,
        val capturedByPrincipalId: String?,
    )

    private val rows = mutableListOf<ProofCaptureRow>()
    private val flow = MutableStateFlow<List<ProofCaptureRow>>(emptyList())
    val captureCalls = mutableListOf<CaptureCall>()
    private var nextId = 0

    override fun observeProofs(taskId: String): Flow<List<ProofCaptureRow>> = flow

    override suspend fun capture(
        taskId: String,
        fieldKey: String,
        subject: ProofSubject,
        localUri: String,
        mimeType: String,
        caption: String?,
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
    ): AppResult<ProofCaptureRow> {
        captureCalls += CaptureCall(fieldKey, subject, localUri, capturedStartMs, capturedEndMs, capturedByPrincipalId)
        if (rows.size >= maxProofs) return AppResult.Err("Maximum $maxProofs proof videos reached for this drive.")
        val row = ProofCaptureRow(
            id = "proof-${nextId++}",
            fieldKey = fieldKey,
            proofSubject = subject,
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

/** Defaults to an operator profile present (capture allowed) — pass `profile = null` to test
 *  the approver-only/leadership role-blocked path. */
class FakeCaptureBootstrapRepository(
    private val profile: BootstrapOperatorProfileDto? = BootstrapOperatorProfileDto(operatorId = "operator-1", primaryRoleHint = "operator"),
) : BootstrapRepository {
    override suspend fun loadNavState(): NavState = NavState.Empty
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto? = profile
}
