package sg.mesha.goatos.core.data.capture

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.data.sync.syncJson
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto

/**
 * MOB-002 Room-first capture coverage (docs/mobile/proof-capture-sync-and-e2e.md §3):
 *  1. [ScanCaptureRepository] dedups a repeat tag at the DB layer (unique index), not just
 *     in-memory.
 *  2. [ProofCaptureRepository] enforces the 5-video cap, persists Room FIRST, and follows a
 *     registration outbox item's status back onto the row (PENDING -> IN_FLIGHT -> SYNCED,
 *     decoding the server proof id from the outbox's echoed result JSON).
 *  3. Process-death restore: a NEW repository instance built over the SAME (still-open) Room
 *     database sees exactly what the first instance wrote — the closest a JVM unit test can get
 *     to proving "kill + relaunch, the drive is still there" without an actual process kill.
 *
 * Robolectric gives a real (not stubbed) `android.database.sqlite` + `Context` under a plain
 * `testDebugUnitTest` JVM run — see [sg.mesha.goatos.core.data.GoatDatabaseCacheTest] for the
 * same pattern already established in this module.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CaptureRepositoryTest {

    private val unconfinedDispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    private fun newDb(): GoatDatabase {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        return Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
    }

    @Test
    fun `repeat scan of the same tag is deduped at the DB layer`() = runTest {
        val db = newDb()
        try {
            val repo = DefaultScanCaptureRepository(db.scannedGoatDao(), dispatchers = unconfinedDispatchers)

            repo.recordScan("task-1", "goat_scan", "TAG-001")
            repo.recordScan("task-1", "goat_scan", "TAG-001") // repeat — must be a no-op
            repo.recordScan("task-1", "goat_scan", "TAG-002")

            val tags = repo.tagsForTask("task-1")
            assertEquals(listOf("TAG-001", "TAG-002"), tags)
            assertEquals(2, repo.observeScannedCount("task-1", "goat_scan").first())
        } finally {
            db.close()
        }
    }

    @Test
    fun `proof capture is written to Room first and the 5-video cap is enforced`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                dispatchers = unconfinedDispatchers,
            )

            val subjects = listOf(ProofSubject.SHED, ProofSubject.VIAL_LOT, ProofSubject.ADMINISTRATION, ProofSubject.EXTRA, ProofSubject.EXTRA)
            subjects.forEachIndexed { index, subject ->
                val result = repo.capture(
                    taskId = "task-9",
                    fieldKey = subject.wireValue,
                    subject = subject,
                    localUri = "file://video-$index.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-9",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                )
                assertTrue("capture #$index (under the cap) must succeed", result is AppResult.Ok)
            }
            assertEquals(5, repo.observeProofs("task-9").first().size)

            // 6th capture — over the cap — must be rejected, WITHOUT a Room write.
            val sixth = repo.capture(
                taskId = "task-9",
                fieldKey = "extra_video",
                subject = ProofSubject.EXTRA,
                localUri = "file://video-6.mp4",
                mimeType = "video/mp4",
                caption = "one too many",
                scopeType = "task",
                scopeId = "task-9",
                capturedStartMs = 1_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = "operator-1",
            )
            assertTrue(sixth is AppResult.Err)
            assertEquals(5, repo.observeProofs("task-9").first().size)

            // Every capture queued a registration write through the SAME durable outbox path.
            assertEquals(5, sync.enqueueCalls.size)
        } finally {
            db.close()
        }
    }

    @Test
    fun `proof row status follows its outbox item PENDING to IN_FLIGHT to SYNCED with server proof id`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                dispatchers = unconfinedDispatchers,
            )

            val captured = (
                repo.capture(
                    taskId = "task-5",
                    fieldKey = "shed_video",
                    subject = ProofSubject.SHED,
                    localUri = "file://shed.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-5",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                ) as AppResult.Ok
                ).value

            // Freshly captured: PENDING, nothing synced yet.
            var row = repo.observeProofs("task-5").first().first { it.id == captured.id }
            assertEquals(CaptureSyncStatus.PENDING, row.syncStatus)

            val itemId = sync.enqueueCalls.single().outboxItemId
            sync.emit(itemId, SyncItemStatus.IN_FLIGHT, resultJson = null)
            row = repo.observeProofs("task-5").first().first { it.id == captured.id }
            assertEquals(CaptureSyncStatus.IN_FLIGHT, row.syncStatus)

            val response = ProofUploadResponseDto(proof = ProofReferenceDto(proofId = "server-proof-123"))
            sync.emit(itemId, SyncItemStatus.SUCCEEDED, resultJson = syncJson.encodeToString(response))
            row = repo.observeProofs("task-5").first().first { it.id == captured.id }
            assertEquals(CaptureSyncStatus.SYNCED, row.syncStatus)
            assertEquals("server-proof-123", row.serverProofId)
        } finally {
            db.close()
        }
    }

    @Test
    fun `a new repository instance over the SAME Room database sees prior captures (process-death restore proxy)`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val firstInstance = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                dispatchers = unconfinedDispatchers,
            )
            val firstScanInstance = DefaultScanCaptureRepository(db.scannedGoatDao(), dispatchers = unconfinedDispatchers)
            firstScanInstance.recordScan("task-7", "goat_scan", "TAG-A")
            firstInstance.capture(
                taskId = "task-7",
                fieldKey = "shed_video",
                subject = ProofSubject.SHED,
                localUri = "file://shed.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "task",
                scopeId = "task-7",
                capturedStartMs = 1_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = "operator-1",
            )

            // Simulate a process-death-and-relaunch: brand-new repository instances (as a fresh
            // ViewModel/Hilt graph would construct), but over the SAME underlying Room database
            // handle — the strongest proxy a JVM unit test can offer for "kill + relaunch".
            val secondInstance = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                dispatchers = unconfinedDispatchers,
            )
            val secondScanInstance = DefaultScanCaptureRepository(db.scannedGoatDao(), dispatchers = unconfinedDispatchers)

            assertEquals(listOf("TAG-A"), secondScanInstance.tagsForTask("task-7"))
            val proofs = secondInstance.observeProofs("task-7").first()
            assertEquals(1, proofs.size)
            assertEquals(ProofSubject.SHED, proofs.single().proofSubject)
        } finally {
            db.close()
        }
    }
}

/** Minimal, deterministic [SyncRepository] test double: records every
 *  [enqueueProofUpload] call and lets the test manually drive its outbox item's status via
 *  [emit], mirroring how [sg.mesha.goatos.core.data.sync.SyncEngine] would really transition it. */
private class FakeSyncRepository : SyncRepository {
    data class EnqueueCall(val idempotencyKey: String, val outboxItemId: String, val request: ProofUploadRequestDto)

    val enqueueCalls = mutableListOf<EnqueueCall>()
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private var nextId = 0

    override fun observeStatus(): StateFlow<SyncStatus> = status

    fun emit(itemId: String, itemStatus: SyncItemStatus, resultJson: String?) {
        status.value = status.value.copy(
            items = status.value.items.map { item ->
                if (item.id == itemId) {
                    item.copy(status = itemStatus, resultJson = resultJson ?: item.resultJson)
                } else {
                    item
                }
            },
        )
    }

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")

    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> {
        val id = "outbox-${nextId++}"
        enqueueCalls += EnqueueCall(idempotencyKey, id, request)
        status.value = status.value.copy(
            items = status.value.items + SyncQueueItem(
                id = id,
                opType = "PROOF_UPLOAD",
                groupKey = groupKey,
                status = SyncItemStatus.QUEUED,
                attemptCount = 0,
                maxAttempts = 8,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                lastError = null,
            ),
        )
        return AppResult.Ok(id)
    }

    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")

    override suspend fun triggerDrain() = Unit
}
