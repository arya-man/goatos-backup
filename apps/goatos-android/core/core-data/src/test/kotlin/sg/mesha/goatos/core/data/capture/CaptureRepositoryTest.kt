package sg.mesha.goatos.core.data.capture

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.File
import java.nio.file.Files
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.database.capture.ProofCaptureDao
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
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
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
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
@OptIn(ExperimentalCoroutinesApi::class)
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
            val sync = FakeSyncRepository()
            val repo = DefaultScanCaptureRepository(db.scannedGoatDao(), syncRepository = sync, dispatchers = unconfinedDispatchers)

            repo.recordScan("task-1", "goat_scan", "TAG-001", goatId = "goat-1", obligationId = "obl-1")
            repo.recordScan("task-1", "goat_scan", "TAG-001") // repeat — must be a no-op
            repo.recordScan("task-1", "goat_scan", "TAG-002")

            val tags = repo.tagsForTask("task-1")
            assertEquals(listOf("TAG-001", "TAG-002"), tags)
            assertEquals(2, repo.observeScannedCount("task-1", "goat_scan").first())
            assertEquals(2, sync.scanCalls.size)
            assertEquals("scan:task-1:goat_scan:tag001:ov0", sync.scanCalls[0].idempotencyKey)
            assertEquals("goat-1", sync.scanCalls[0].request.goatId)
            assertEquals("obl-1", sync.scanCalls[0].request.obligationId)
        } finally {
            db.close()
        }
    }

    @Test
    fun `accepted re-scan of a tag whose earlier SYNCED capture was reopened still enqueues`() = runTest {
        // Reproduces the silent-data-loss defect: a tag was captured and SYNCED to the server in
        // an earlier session (e.g. a verifier rejected the proof and the obligation reopened —
        // the obligation KEEPS its id, only its status flips). A later ACCEPTED re-scan of the
        // SAME tag for the SAME task/field must still produce BOTH a durable local capture row
        // AND an enqueued backend scan-capture — not a silent no-op behind the unique
        // (taskId, fieldKey, tag) index. On the pre-fix `dao.insert(OnConflictStrategy.IGNORE)`
        // path this test FAILS: the second recordScan is swallowed, sync.scanCalls stays at 1,
        // and the row's syncStatus/capturedAtMs are never refreshed for the new cycle.
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultScanCaptureRepository(db.scannedGoatDao(), syncRepository = sync, dispatchers = unconfinedDispatchers)

            // Session 1 (yesterday): tag captured for obligation "obl-1" at row_version 1 and
            // acknowledged by the server (server-side idempotency_key ends "...:ov1").
            repo.recordScan(
                "task-1",
                ROSTER_SCAN_FIELD_KEY,
                "901007000504418",
                goatId = "goat-1",
                obligationId = "obl-1",
                obligationRowVersion = 1,
                capturedAtMs = 1_000L,
            )
            repo.markLocalScanSynced("task-1", ROSTER_SCAN_FIELD_KEY, "901007000504418")

            // Session 2 (today): the verifier rejected the proof, the SAME obligation "obl-1"
            // reopened (row_version bumped 1 -> 2 server-side, echoed on the next roster fetch),
            // and the operator's re-scan is ACCEPTED (roster reported it PENDING).
            repo.recordScan(
                "task-1",
                ROSTER_SCAN_FIELD_KEY,
                "901007000504418",
                goatId = "goat-1",
                obligationId = "obl-1",
                obligationRowVersion = 2,
                capturedAtMs = 2_000L,
            )

            // 1) Durable local capture: still exactly one row for this tag (dedup preserved,
            //    not duplicated), but it now reflects the NEW cycle.
            val rows = repo.observeScannedTags("task-1", ROSTER_SCAN_FIELD_KEY).first()
            assertEquals(1, rows.size)
            assertEquals(2_000L, rows.single().capturedAtMs)
            assertEquals(CaptureSyncStatus.PENDING, rows.single().syncStatus)

            // 2) Enqueued backend capture: TWO scan-capture outbox writes now exist — the
            //    original synced one and the fresh one for the reopened cycle.
            assertEquals(2, sync.scanCalls.size)
            assertEquals(2_000L, sync.scanCalls[1].request.capturedAtMs)
            assertEquals("obl-1", sync.scanCalls[1].request.obligationId)

            // 3) THE KEY POINT (maintainer-reported hole): the two outbox writes must carry
            //    DIFFERENT idempotency keys. Same-tag/same-task/same-field alone builds an
            //    IDENTICAL key across the reopen, which the backend then treats as a replay of
            //    an already-recorded capture and silently drops — the request is sent (Room
            //    layer fixed) but discarded on arrival. Keying on obligationRowVersion is what
            //    makes cycle 2's capture a genuinely NEW key the server has never seen.
            val keyCycle1 = sync.scanCalls[0].idempotencyKey
            val keyCycle2 = sync.scanCalls[1].idempotencyKey
            assertTrue("reopened-cycle capture must use a NEW idempotency key, got same key twice: $keyCycle1", keyCycle1 != keyCycle2)
            assertTrue(keyCycle1.endsWith(":ov1"))
            assertTrue(keyCycle2.endsWith(":ov2"))
        } finally {
            db.close()
        }
    }

    @Test
    fun `network retry of the same scan cycle keeps the same idempotency key`() = runTest {
        // Companion to the reopen test: retrying the SAME accepted scan (same obligationRowVersion
        // — no domain reopen happened, just a dropped response / connectivity retry) must NOT mint
        // a new key, or every retry would duplicate the capture server-side. This is the guardrail
        // against "just add a timestamp" (explicitly rejected in the maintainer's brief).
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultScanCaptureRepository(db.scannedGoatDao(), syncRepository = sync, dispatchers = unconfinedDispatchers)

            repo.recordScan(
                "task-2",
                ROSTER_SCAN_FIELD_KEY,
                "901007000504419",
                goatId = "goat-2",
                obligationId = "obl-2",
                obligationRowVersion = 1,
                capturedAtMs = 1_000L,
            )
            // Local retry before the row synced: dedup at the DB layer keeps this a no-op (no
            // second outbox call), independent of the server-side key story above.
            repo.recordScan(
                "task-2",
                ROSTER_SCAN_FIELD_KEY,
                "901007000504419",
                goatId = "goat-2",
                obligationId = "obl-2",
                obligationRowVersion = 1,
                capturedAtMs = 1_050L,
            )

            assertEquals(1, sync.scanCalls.size)
            assertTrue(sync.scanCalls[0].idempotencyKey.endsWith(":ov1"))
        } finally {
            db.close()
        }
    }

    @Test
    fun `true repeat scan while original capture is still unsynced stays deduped`() = runTest {
        // Companion to the reopen test above: the maintainer's explicit instruction is that a
        // repeat read of the SAME tag in the SAME bucket before the original even reached the
        // server must NOT weaken dedup. Only a SYNCED-then-rescanned row is treated as fresh.
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultScanCaptureRepository(db.scannedGoatDao(), syncRepository = sync, dispatchers = unconfinedDispatchers)

            repo.recordScan("task-1", "goat_scan", "TAG-900", goatId = "goat-9", obligationId = "obl-9", capturedAtMs = 1_000L)
            repo.recordScan("task-1", "goat_scan", "TAG-900", goatId = "goat-9", obligationId = "obl-9", capturedAtMs = 1_500L)

            val rows = repo.observeScannedTags("task-1", "goat_scan").first()
            assertEquals(1, rows.size)
            assertEquals(1_000L, rows.single().capturedAtMs) // untouched — still the first write
            assertEquals(1, sync.scanCalls.size)
        } finally {
            db.close()
        }
    }

    @Test
    fun `scan attempts are append-only and each attempt is queued for backend audit`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultScanAttemptRepository(
                db.rfidScanAttemptDao(),
                syncRepository = sync,
                dispatchers = unconfinedDispatchers,
                clock = { 123L + sync.attemptCalls.size },
                idGenerator = { "attempt-${sync.attemptCalls.size}" },
            )

            repo.recordAttempt(
                taskId = "task-1",
                fieldKey = ROSTER_SCAN_FIELD_KEY,
                tag = "901007000504418",
                goatId = "goat-1",
                obligationId = "obl-1",
                outcome = RfidScanAttemptOutcome.ACCEPTED,
                tagRole = RfidScanTagRole.PRIMARY,
                reason = null,
                capturedAtMs = 10_001L,
            )
            repo.recordAttempt(
                taskId = "task-1",
                fieldKey = ROSTER_SCAN_FIELD_KEY,
                tag = "901007000504419",
                goatId = "goat-1",
                obligationId = "obl-1",
                outcome = RfidScanAttemptOutcome.DUPLICATE,
                tagRole = RfidScanTagRole.SECONDARY,
                reason = "goat_already_scanned",
                capturedAtMs = 10_002L,
            )

            val attempts = repo.attemptsForTask("task-1")
            assertEquals(listOf("901007000504418", "901007000504419"), attempts.map { it.tag })
            assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED, RfidScanAttemptOutcome.DUPLICATE), attempts.map { it.outcome })
            assertEquals(listOf(10_001L, 10_002L), attempts.map { it.capturedAtMs })
            assertEquals(2, sync.attemptCalls.size)
            assertEquals("scan-attempt:task-1:attempt-0", sync.attemptCalls[0].idempotencyKey)
            assertEquals("scan-attempt:task-1:attempt-1", sync.attemptCalls[1].idempotencyKey)
            assertEquals(10_001L, sync.attemptCalls[0].request.capturedAtMs)
            assertEquals(10_002L, sync.attemptCalls[1].request.capturedAtMs)
            assertEquals("secondary", sync.attemptCalls[1].request.tagRole)
            assertEquals("goat_already_scanned", sync.attemptCalls[1].request.reason)
        } finally {
            db.close()
        }
    }

    @Test
    fun `goat proof replacement is written even after historical proof cap`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            repeat(5) { index ->
                val result = repo.capture(
                    taskId = "task-9",
                    fieldKey = "vaccination_goat_proof",
                    subject = ProofSubject.GOAT,
                    subjectId = "goat-9",
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

            // Goat proof replacement must not be blocked by historical audit rows. The scan and
            // submit surfaces select the latest synced proof as the active one for the goat.
            val sixth = repo.capture(
                taskId = "task-9",
                fieldKey = "vaccination_goat_proof",
                subject = ProofSubject.GOAT,
                subjectId = "goat-9",
                localUri = "file://video-6.mp4",
                mimeType = "video/mp4",
                caption = "replacement",
                scopeType = "task",
                scopeId = "task-9",
                capturedStartMs = 1_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = "operator-1",
            )
            assertTrue(sixth is AppResult.Ok)
            assertEquals(6, repo.observeProofs("task-9").first().size)

            // Every capture queued a registration write through the SAME durable outbox path.
            assertEquals(6, sync.enqueueCalls.size)
        } finally {
            db.close()
        }
    }

    @Test
    fun `R50-027 generic (no-subjectId) shed proof capture is capped, not unlimited`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            // A shed proof has no per-goat subjectId. Before the fix the per-subject count saw 0
            // and the cap never applied, so generic proofs were unbounded.
            repeat(5) { index ->
                val result = repo.capture(
                    taskId = "task-shed",
                    fieldKey = "vaccination_shed_proof",
                    subject = ProofSubject.SHED,
                    subjectId = null,
                    localUri = "file://shed-$index.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-shed",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                )
                assertTrue("shed capture #$index (under cap) must succeed", result is AppResult.Ok)
            }
            val sixth = repo.capture(
                taskId = "task-shed",
                fieldKey = "vaccination_shed_proof",
                subject = ProofSubject.SHED,
                subjectId = null,
                localUri = "file://shed-6.mp4",
                mimeType = "video/mp4",
                caption = "one too many",
                scopeType = "task",
                scopeId = "task-shed",
                capturedStartMs = 1_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = "operator-1",
            )
            assertTrue("generic shed cap must reject the 6th, not accept unlimited", sixth is AppResult.Err)
            assertEquals(5, repo.observeProofs("task-shed").first().size)
        } finally {
            db.close()
        }
    }

    @Test
    fun `terminally failed proof rows do not exhaust the active capture cap`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            val capturedIds = (0 until 5).map { index ->
                (
                    repo.capture(
                        taskId = "task-failed-cap",
                        fieldKey = "vaccination_goat_proof",
                        subject = ProofSubject.GOAT,
                        subjectId = "goat-failed",
                        localUri = "file://failed-$index.mp4",
                        mimeType = "video/mp4",
                        caption = null,
                        scopeType = "task",
                        scopeId = "task-failed-cap",
                        capturedStartMs = 1_000L,
                        capturedEndMs = 4_000L,
                        capturedByPrincipalId = "operator-1",
                    ) as AppResult.Ok
                    ).value.id
            }
            capturedIds.forEach { id ->
                db.proofCaptureDao().updateStatus(id, CaptureSyncStatus.FAILED.name, null, "network gave up")
            }

            val replacement = repo.capture(
                taskId = "task-failed-cap",
                fieldKey = "vaccination_goat_proof",
                subject = ProofSubject.GOAT,
                subjectId = "goat-failed",
                localUri = "file://replacement.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "task",
                scopeId = "task-failed-cap",
                capturedStartMs = 5_000L,
                capturedEndMs = 8_000L,
                capturedByPrincipalId = "operator-1",
            )

            assertTrue("failed rows must not permanently burn the 5-video cap", replacement is AppResult.Ok)
            assertEquals(CaptureSyncStatus.PENDING, repo.observeProofs("task-failed-cap").first().first().syncStatus)
        } finally {
            db.close()
        }
    }

    @Test
    fun `retryUpload re-arms a failed proof outbox row`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            val captured = (
                repo.capture(
                    taskId = "task-retry-proof",
                    fieldKey = "administration_video",
                    subject = ProofSubject.ADMINISTRATION,
                    localUri = "file://admin.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-retry-proof",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                ) as AppResult.Ok
                ).value
            advanceUntilIdle()
            // R50-029: reconciliation may re-enqueue during startup, but setOutboxItemId should prevent it
            // Get the outbox item ID from the row directly to avoid depending on enqueue count
            val itemId = db.proofCaptureDao().findById(captured.id)?.outboxItemId
            assertTrue("Proof should have an outbox item ID after capture", !itemId.isNullOrBlank())
            db.proofCaptureDao().updateStatus(captured.id, CaptureSyncStatus.FAILED.name, null, "network gave up")

            val retry = repo.retryUpload("task-retry-proof", captured.id)

            assertTrue(retry is AppResult.Ok)
            assertEquals(listOf(itemId), sync.retryCalls)
            assertEquals(CaptureSyncStatus.PENDING, repo.observeProofs("task-retry-proof").first().single().syncStatus)
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
                reconcileOnStartup = false,
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

            // R50-029: get the outbox item ID from the row to avoid depending on enqueue call count
            val itemId = db.proofCaptureDao().findById(captured.id)?.outboxItemId
            assertTrue("Proof should have an outbox item ID", !itemId.isNullOrBlank())
            sync.emit(itemId!!, SyncItemStatus.IN_FLIGHT, resultJson = null)
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
    fun `proof upload success without a server proof id is quarantined as failed`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            val captured = (
                repo.capture(
                    taskId = "task-corrupt-proof-result",
                    fieldKey = "administration_video",
                    subject = ProofSubject.ADMINISTRATION,
                    localUri = "file://admin.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-corrupt-proof-result",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                ) as AppResult.Ok
                ).value

            // R50-029: get the outbox item ID from the row to avoid depending on enqueue call count
            val itemId = db.proofCaptureDao().findById(captured.id)?.outboxItemId
            assertTrue("Proof should have an outbox item ID", !itemId.isNullOrBlank())
            repo.observeProofs("task-corrupt-proof-result").first().first { it.id == captured.id }
            sync.emit(itemId!!, SyncItemStatus.SUCCEEDED, resultJson = """{"proof":{}}""")

            val row = repo.observeProofs("task-corrupt-proof-result").first().first { it.id == captured.id }
            assertEquals(CaptureSyncStatus.FAILED, row.syncStatus)
            assertEquals(null, row.serverProofId)
            assertEquals("Proof upload finished without a server proof id. Record this video again.", row.lastError)
        } finally {
            db.close()
        }
    }

    @Test
    fun `startup reconciles proof row inserted before outbox enqueue`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            db.proofCaptureDao().insert(
                proofEntity(
                    id = "proof-orphan",
                    taskId = "task-orphan",
                    fieldKey = "shed_video",
                    idempotencyKey = "proof-upload:task-orphan:proof-orphan",
                ),
            )

            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            // Drive reconciliation explicitly (awaited) rather than racing the fire-and-forget init.
            repo.reconcileRecoverableUploadsNow()

            val row = db.proofCaptureDao().findById("proof-orphan")
            assertEquals(1, sync.enqueueCalls.size)
            assertEquals("proof-upload:task-orphan:proof-orphan", sync.enqueueCalls.single().idempotencyKey)
            assertEquals("outbox-0", row?.outboxItemId)
            assertEquals(CaptureSyncStatus.PENDING.name, row?.syncStatus)
        } finally {
            db.close()
        }
    }

    @Test
    fun `entity default capture_source stays in lockstep with the ProofPolicy default`() {
        // The proof_capture.captureSource column default (core-database, DEFAULT_CAPTURE_SOURCE)
        // and ProofPolicy.Default.captureSource (core-data) are two hardcoded literals that MUST
        // agree — a fresh capture with no explicit policy and a backfilled legacy row must carry the
        // SAME capture_source. They live in different modules (no shared const possible), so this
        // test is the drift guard the "kept in lockstep" comments rely on.
        assertEquals(
            sg.mesha.goatos.core.data.forms.ProofPolicy.Default.captureSource,
            sg.mesha.goatos.core.database.capture.DEFAULT_CAPTURE_SOURCE,
        )
    }

    @Test
    fun `startup recovery re-registers with the row's persisted capture_source not a default (BUG 7-res)`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            // A proof captured under a NON-default source, persisted to Room before the process died
            // (outboxItemId still null → the recovery walk must re-enqueue it). The recovery path has
            // no in-memory ProofPolicy: before the SSOT fix it fell back to ProofPolicy.Default
            // ("in_app_camera"), silently rewriting this proof's capture_source.
            db.proofCaptureDao().insert(
                proofEntity(
                    id = "proof-src",
                    taskId = "task-src",
                    fieldKey = "administration_video",
                    idempotencyKey = "proof-upload:task-src:proof-src",
                    captureSource = "external_upload",
                ),
            )

            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            repo.reconcileRecoverableUploadsNow()

            assertEquals(1, sync.enqueueCalls.size)
            val captureSource = (sync.enqueueCalls.single().request.metadata["capture_source"] as? JsonPrimitive)?.content
            assertEquals(
                "recovery must re-send the persisted capture_source, not the ProofPolicy.Default fallback",
                "external_upload",
                captureSource,
            )
        } finally {
            db.close()
        }
    }

    @Test
    fun `startup reattaches existing proof outbox item and follows it through synced`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            sync.seed("outbox-existing", SyncItemStatus.QUEUED)
            db.proofCaptureDao().insert(
                proofEntity(
                    id = "proof-existing",
                    taskId = "task-existing",
                    fieldKey = "administration_video",
                    idempotencyKey = "proof-upload:task-existing:proof-existing",
                    outboxItemId = "outbox-existing",
                ),
            )

            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            advanceUntilIdle()
            repo.reconcileRecoverableUploadsNow()

            assertEquals("an existing outbox id must be followed, not re-enqueued", 0, sync.enqueueCalls.size)

            sync.emit("outbox-existing", SyncItemStatus.IN_FLIGHT, resultJson = null)
            advanceUntilIdle()
            var row = repo.observeProofs("task-existing").first().single()
            assertEquals(CaptureSyncStatus.IN_FLIGHT, row.syncStatus)

            val response = ProofUploadResponseDto(proof = ProofReferenceDto(proofId = "server-proof-existing"))
            sync.emit("outbox-existing", SyncItemStatus.SUCCEEDED, resultJson = syncJson.encodeToString(response))
            advanceUntilIdle()
            row = repo.observeProofs("task-existing").first().single()
            assertEquals(CaptureSyncStatus.SYNCED, row.syncStatus)
            assertEquals("server-proof-existing", row.serverProofId)
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
                reconcileOnStartup = false,
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
            advanceUntilIdle()

            // Simulate a process-death-and-relaunch: brand-new repository instances (as a fresh
            // ViewModel/Hilt graph would construct), but over the SAME underlying Room database
            // handle — the strongest proxy a JVM unit test can offer for "kill + relaunch".
            val secondInstance = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            val secondScanInstance = DefaultScanCaptureRepository(db.scannedGoatDao(), dispatchers = unconfinedDispatchers)
            advanceUntilIdle()

            assertEquals(listOf("TAG-A"), secondScanInstance.tagsForTask("task-7"))
            val proofs = secondInstance.observeProofs("task-7").first()
            assertEquals(1, proofs.size)
            assertEquals(ProofSubject.SHED, proofs.single().proofSubject)
        } finally {
            db.close()
        }
    }

    @Test
    fun `remove QUEUED proof cancels outbox item FIRST then deletes row and file (BUG 8)`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            val captured = (
                repo.capture(
                    taskId = "task-bug8-queued",
                    fieldKey = "shed_video",
                    subject = ProofSubject.SHED,
                    localUri = "file://bug8-queued.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-bug8-queued",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                ) as AppResult.Ok
            ).value

            val outboxItemId = db.proofCaptureDao().findById(captured.id)?.outboxItemId
            assertTrue("outbox item should exist", !outboxItemId.isNullOrBlank())

            // Remove the proof — must cancel outbox FIRST
            val result = repo.remove("task-bug8-queued", captured.id)
            assertTrue("remove must succeed", result is AppResult.Ok)

            // Verify: row is deleted
            val rowAfter = db.proofCaptureDao().findById(captured.id)
            assertEquals("proof row must be deleted", null, rowAfter)

            // Verify: sync used the ATOMIC guarded cancel (not the unconditional deleteOutboxItem),
            // and the item was actually cancelled before the row/file were removed.
            assertEquals("outbox item must be cancelled via the guarded path", 1, sync.cancelCalls.size)
            assertEquals(outboxItemId, sync.cancelCalls[0])
            assertEquals("must NOT use the unconditional deleteOutboxItem", 0, sync.deleteOutboxCalls.size)
        } finally {
            db.close()
        }
    }

    @Test
    fun `remove IN_FLIGHT proof refuses deletion to prevent orphan server proof (BUG 8)`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            val captured = (
                repo.capture(
                    taskId = "task-bug8-inflight",
                    fieldKey = "administration_video",
                    subject = ProofSubject.ADMINISTRATION,
                    localUri = "file://bug8-inflight.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-bug8-inflight",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                ) as AppResult.Ok
            ).value

            val outboxItemId = db.proofCaptureDao().findById(captured.id)?.outboxItemId
            assertTrue("outbox item should exist", !outboxItemId.isNullOrBlank())

            // Simulate the outbox item being IN_FLIGHT
            sync.emit(outboxItemId!!, SyncItemStatus.IN_FLIGHT, resultJson = null)
            advanceUntilIdle()

            // Try to remove — must REFUSE because upload is in progress
            val result = repo.remove("task-bug8-inflight", captured.id)
            assertTrue("remove must fail for IN_FLIGHT upload", result is AppResult.Err)

            // Verify: row is NOT deleted
            val rowAfter = db.proofCaptureDao().findById(captured.id)
            assertEquals("proof row must NOT be deleted", captured.id, rowAfter?.id)

            // Verify: no deleteOutboxItem was called
            assertEquals("deleteOutboxItem must NOT be called for IN_FLIGHT", 0, sync.deleteOutboxCalls.size)
        } finally {
            db.close()
        }
    }

    @Test
    fun `remove refuses when guarded cancel misses and item raced to a retryable FAILED (BUG 8 residual)`() = runTest {
        val db = newDb()
        try {
            // Simulate: the guarded cancel loses the race (item was IN_FLIGHT at the DELETE), then the
            // dispatcher's markFailed flips it to a RETRYABLE FAILED before we re-check. The old code
            // saw "not IN_FLIGHT" and proceeded to delete the file, stranding a retryable upload of a
            // now-missing file. remove() must REFUSE here.
            val sync = FakeSyncRepository(cancelAlwaysMisses = true)
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            val captured = (
                repo.capture(
                    taskId = "task-bug8-flip",
                    fieldKey = "shed_video",
                    subject = ProofSubject.SHED,
                    localUri = "file://bug8-flip.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-bug8-flip",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                ) as AppResult.Ok
                ).value
            val outboxItemId = db.proofCaptureDao().findById(captured.id)?.outboxItemId!!
            // The item is now a retryable FAILED (raced there after our guarded cancel missed).
            sync.emit(outboxItemId, SyncItemStatus.FAILED, resultJson = null)

            val result = repo.remove("task-bug8-flip", captured.id)
            assertTrue("must refuse while a retryable FAILED outbox row still references the file", result is AppResult.Err)
            assertEquals("proof row must NOT be deleted", captured.id, db.proofCaptureDao().findById(captured.id)?.id)
        } finally {
            db.close()
        }
    }

    @Test
    fun `remove surfaces deleteOutboxItem failure and preserves row and file (BUG 8)`() = runTest {
        val db = newDb()
        try {
            val syncWithFailure = FakeSyncRepository(cancelOutboxItemFailure = "network error")
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = syncWithFailure,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            val captured = (
                repo.capture(
                    taskId = "task-bug8-failure",
                    fieldKey = "shed_video",
                    subject = ProofSubject.SHED,
                    localUri = "file://bug8-failure.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-bug8-failure",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                ) as AppResult.Ok
            ).value

            // Try to remove — outbox delete will fail
            val result = repo.remove("task-bug8-failure", captured.id)
            assertTrue("remove must surface the error", result is AppResult.Err)
            assertTrue(result is AppResult.Err && result.message.contains("network error"))

            // Verify: row is NOT deleted (orphan prevention)
            val rowAfter = db.proofCaptureDao().findById(captured.id)
            assertEquals("proof row must NOT be deleted on outbox failure", captured.id, rowAfter?.id)
        } finally {
            db.close()
        }
    }

    @Test
    fun `clearForTask with proofs beyond cap deletes every local file (R50-029 BUG 1)`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            // Insert 11,000 proofs for the task (beyond the MAX_PROOFS_PER_TASK = 10,000 cap)
            val taskId = "task-large-proof-set"
            repeat(11_000) { index ->
                val uri = "file://large-proof-$index.mp4"
                db.proofCaptureDao().insert(
                    proofEntity(
                        id = "proof-$index",
                        taskId = taskId,
                        fieldKey = "shed_video",
                        idempotencyKey = "proof-upload:$taskId:proof-$index",
                    ).copy(localUri = uri),
                )
            }

            // Verify all rows exist before clear
            val allRowsBefore = db.proofCaptureDao().listForTask(taskId, limit = Int.MAX_VALUE)
            assertEquals("should have exactly 11,000 rows before clear", 11_000, allRowsBefore.size)

            // Call clearForTask — with the BUG, it only reads 10,000 rows and only deletes those 10,000 files
            repo.clearForTask(taskId)

            // Verify: ALL rows are deleted from DB (this is the critical assertion)
            val allRowsAfter = db.proofCaptureDao().listForTask(taskId, limit = Int.MAX_VALUE)
            assertEquals("all 11,000 rows must be deleted even though read is capped at 10,000", 0, allRowsAfter.size)
        } finally {
            db.close()
        }
    }

    @Test
    fun `clearForTask deletes every status and file only for the requested task across pages`() = runTest {
        val db = newDb()
        val tempDir = Files.createTempDirectory("proof-clear-task-test").toFile()
        try {
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = FakeSyncRepository(),
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            val targetTaskId = "task-to-clear"
            val otherTaskId = "task-to-preserve"
            val statuses = listOf(
                CaptureSyncStatus.PENDING,
                CaptureSyncStatus.SYNCED,
                CaptureSyncStatus.FAILED,
            )
            val rowCount = ProofCaptureDao.TASK_CLEANUP_PAGE_SIZE + 4
            val targetFiles = mutableListOf<File>()
            val otherFiles = mutableListOf<File>()

            // More than one cleanup page, with another task interleaved at every keyset position.
            repeat(rowCount) { index ->
                val capturedAtMs = 1_000L + index
                val targetStatus = statuses[index % statuses.size]
                val otherStatus = statuses[(index + 1) % statuses.size]
                val targetFile = File(tempDir, "target-$index.mp4").apply { writeText("target") }
                val otherFile = File(tempDir, "other-$index.mp4").apply { writeText("other") }
                targetFiles += targetFile
                otherFiles += otherFile
                db.proofCaptureDao().insert(
                    proofEntity(
                        id = "target-$index",
                        taskId = targetTaskId,
                        fieldKey = "shed_video",
                        idempotencyKey = "proof-upload:$targetTaskId:$index",
                    ).copy(
                        localUri = targetFile.toURI().toString(),
                        capturedAtMs = capturedAtMs,
                        syncStatus = targetStatus.name,
                        serverProofId = if (targetStatus == CaptureSyncStatus.SYNCED) {
                            "server-target-$index"
                        } else {
                            null
                        },
                    ),
                )
                db.proofCaptureDao().insert(
                    proofEntity(
                        id = "other-$index",
                        taskId = otherTaskId,
                        fieldKey = "shed_video",
                        idempotencyKey = "proof-upload:$otherTaskId:$index",
                    ).copy(
                        localUri = otherFile.toURI().toString(),
                        capturedAtMs = capturedAtMs,
                        syncStatus = otherStatus.name,
                        serverProofId = if (otherStatus == CaptureSyncStatus.SYNCED) {
                            "server-other-$index"
                        } else {
                            null
                        },
                    ),
                )
            }

            repo.clearForTask(targetTaskId)

            assertTrue(db.proofCaptureDao().listForTask(targetTaskId, Int.MAX_VALUE).isEmpty())
            assertEquals(rowCount, db.proofCaptureDao().listForTask(otherTaskId, Int.MAX_VALUE).size)
            assertTrue("every target-task file must be deleted", targetFiles.none { it.exists() })
            assertTrue("other-task files must be preserved", otherFiles.all { it.exists() })
        } finally {
            db.close()
            tempDir.deleteRecursively()
        }
    }

    @Test
    fun `retryUpload of a FAILED shed proof re-enqueues with scope_type=shed (BUG P1 fix F1a)`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            // Capture a shed proof (scope_type="shed", scope_id=<shedId>)
            val shedId = "shed-123"
            val captured = (
                repo.capture(
                    taskId = "task-shed-retry",
                    fieldKey = "vaccination_shed_proof",
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = "file://shed-retry.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "shed",
                    scopeId = shedId,
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                ) as AppResult.Ok
            ).value
            advanceUntilIdle()

            // Manually mark it as failed and clear outboxItemId to simulate a recovery path hit
            val entity = db.proofCaptureDao().findById(captured.id)!!
            db.proofCaptureDao().updateStatus(entity.id, CaptureSyncStatus.FAILED.name, null, "network gave up")
            val outboxId = entity.outboxItemId
            assertTrue("should have outbox item after capture", !outboxId.isNullOrBlank())

            // Clear outboxItemId to force the recovery path (P1 bug site)
            // We'll manually set it to null by re-inserting without it (simulating the race condition)
            db.proofCaptureDao().delete(entity.id, entity.taskId)
            db.proofCaptureDao().insert(
                entity.copy(outboxItemId = null, syncStatus = CaptureSyncStatus.FAILED.name),
            )

            // Now retry — must use recoveryScope to re-enqueue with "shed" scope, not hardcoded "task".
            // The outbox dedupes on the row's stable idempotencyKey, so enqueueCalls stays at one
            // entry; the recovery re-invocation is observed via allEnqueueRequests instead.
            val retryInvocations = sync.allEnqueueRequests.size
            repo.retryUpload("task-shed-retry", captured.id)
            advanceUntilIdle() // enqueueRegistration launches on appScope (UNDISPATCHED) — await it

            assertEquals("retry must re-invoke enqueue", retryInvocations + 1, sync.allEnqueueRequests.size)
            val retryRequest = sync.allEnqueueRequests.last()
            assertEquals("retry must use scope_type=shed for shed proofs (F1a fix)", "shed", retryRequest.scopeType)
            assertEquals("retry must use scope_id=shed_id (F1a fix)", shedId, retryRequest.scopeId)
        } finally {
            db.close()
        }
    }

    @Test
    fun `reconcileRecoverableUploadsNow with a shed proof re-enqueues with scope_type=shed (BUG P1 fix F1a)`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            // Simulate a crashed recovery: proof inserted but outboxItemId never set
            val shedId = "shed-recovery"
            db.proofCaptureDao().insert(
                proofEntity(
                    id = "proof-shed-orphan",
                    taskId = "task-shed-orphan",
                    fieldKey = "vaccination_shed_proof",
                    idempotencyKey = "proof-upload:task-shed-orphan:proof-shed-orphan",
                ).copy(
                    proofSubject = ProofSubject.SHED.wireValue,
                    subjectId = shedId,
                ),
            )

            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            repo.reconcileRecoverableUploadsNow()

            assertEquals("recovery must enqueue the shed proof", 1, sync.enqueueCalls.size)
            val recoveryCall = sync.enqueueCalls.single()
            assertEquals("recovery must derive scope_type=shed from entity (F1a fix)", "shed", recoveryCall.request.scopeType)
            assertEquals("recovery must use the persisted subjectId as scope_id (F1a fix)", shedId, recoveryCall.request.scopeId)
        } finally {
            db.close()
        }
    }

    @Test
    fun `reconcileOutboxTerminalState does not call updateStatus when state matches (F4 no-op guard)`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val spyDao = CountingProofCaptureDao(db.proofCaptureDao())
            val repo = DefaultProofCaptureRepository(
                dao = spyDao,
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            // Capture a proof
            val captured = (
                repo.capture(
                    taskId = "task-f4-noop",
                    fieldKey = "shed_video",
                    subject = ProofSubject.SHED,
                    localUri = "file://f4-noop.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-f4-noop",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                ) as AppResult.Ok
            ).value
            advanceUntilIdle()

            val itemId = db.proofCaptureDao().findById(captured.id)?.outboxItemId
            assertTrue("should have outbox item", !itemId.isNullOrBlank())

            // Set the proof to FAILED with a specific error (direct DB write, not through the spy)
            db.proofCaptureDao().updateStatus(captured.id, CaptureSyncStatus.FAILED.name, null, "network gave up")

            // The recovered outbox item is a definitive dead-letter/conflict carrying the SAME
            // lastError the row already has. This is the churn case the F4 guard fixes: the
            // dead-letter branch previously re-wrote FAILED+lastError on every emission.
            sync.seedConflict(itemId!!, "network gave up")
            val rowBefore = db.proofCaptureDao().findById(captured.id)!!
            assertEquals(CaptureSyncStatus.FAILED.name, rowBefore.syncStatus)
            assertEquals("network gave up", rowBefore.lastError)

            // Reset the write counter so we measure ONLY reconcile-driven writes.
            spyDao.updateStatusCalls = 0
            repo.observeProofs("task-f4-noop").first()
            advanceUntilIdle()

            // F4: the guard must SKIP the write entirely when the recovered state already matches —
            // proving the command-in-query no longer re-writes (and re-emits) on every emission.
            assertEquals("matching-state reconcile must issue ZERO updateStatus writes", 0, spyDao.updateStatusCalls)
            val rowAfter = db.proofCaptureDao().findById(captured.id)!!
            assertEquals("state must match before and after", rowBefore.syncStatus, rowAfter.syncStatus)
            assertEquals("error must match before and after", rowBefore.lastError, rowAfter.lastError)

            // Positive control: make the ROW's lastError diverge from the recovered dead-letter
            // item; reconcile MUST now write to reconcile it. This proves the counter detects a
            // real write, so the ZERO assertion above is meaningful, not a dead assertion.
            db.proofCaptureDao().updateStatus(captured.id, CaptureSyncStatus.FAILED.name, null, "stale local error")
            spyDao.updateStatusCalls = 0
            repo.observeProofs("task-f4-noop").first()
            advanceUntilIdle()
            assertTrue("changed-state reconcile must issue a write", spyDao.updateStatusCalls >= 1)
            assertEquals("row lastError reconciled to the outbox item", "network gave up", db.proofCaptureDao().findById(captured.id)!!.lastError)
        } finally {
            db.close()
        }
    }

    @Test
    fun `proof completion follows specific item id even when many items terminalize (R50-029 BUG 2)`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            // Capture a proof with its outbox item
            val targetProof = (
                repo.capture(
                    taskId = "task-many-items",
                    fieldKey = "shed_video",
                    subject = ProofSubject.SHED,
                    localUri = "file://target-proof.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-many-items",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                ) as AppResult.Ok
                ).value

            val targetOutboxItemId = db.proofCaptureDao().findById(targetProof.id)?.outboxItemId
            assertTrue("target proof should have an outbox item ID", !targetOutboxItemId.isNullOrBlank())

            // Seed many OTHER outbox items that will terminalize
            val otherItemIds = mutableListOf<String>()
            repeat(30) { index ->
                val itemId = "outbox-other-$index"
                otherItemIds.add(itemId)
                sync.seed(itemId, SyncItemStatus.QUEUED)
            }

            // Verify the proof starts in PENDING state
            var row = repo.observeProofs("task-many-items").first().first { it.id == targetProof.id }
            assertEquals(CaptureSyncStatus.PENDING, row.syncStatus)

            // Move the target item to IN_FLIGHT
            sync.emit(targetOutboxItemId!!, SyncItemStatus.IN_FLIGHT, resultJson = null)
            advanceUntilIdle()
            // Room invalidation is dispatched independently of the coroutine-test scheduler.
            // Wait for the repository-visible state instead of sampling the first stale emission.
            row = repo.observeProofs("task-many-items")
                .first { proofs ->
                    proofs.firstOrNull { it.id == targetProof.id }?.syncStatus == CaptureSyncStatus.IN_FLIGHT
                }
                .first { it.id == targetProof.id }
            assertEquals(CaptureSyncStatus.IN_FLIGHT, row.syncStatus)

            // Now terminalize all 30 OTHER items — with the BUG, the target item may fall out of the window
            val response = ProofUploadResponseDto(proof = ProofReferenceDto(proofId = "server-proof-many-items"))
            otherItemIds.forEach { itemId ->
                sync.emit(itemId, SyncItemStatus.SUCCEEDED, resultJson = syncJson.encodeToString(response))
            }
            advanceUntilIdle()

            // Move the target item to SUCCEEDED — it must still be followed even though 30 other items
            // have terminalized in between
            sync.emit(targetOutboxItemId, SyncItemStatus.SUCCEEDED, resultJson = syncJson.encodeToString(response))
            advanceUntilIdle()

            // Verify: the target proof reached SYNCED despite the other 30 items dropping out
            row = repo.observeProofs("task-many-items")
                .first { proofs ->
                    proofs.firstOrNull { it.id == targetProof.id }?.syncStatus == CaptureSyncStatus.SYNCED
                }
                .first { it.id == targetProof.id }
            assertEquals("target proof must reach SYNCED even when many items drop out", CaptureSyncStatus.SYNCED, row.syncStatus)
            assertEquals("server-proof-many-items", row.serverProofId)
        } finally {
            db.close()
        }
    }

    @Test
    fun `removing synced proof deletes server artifact before local row`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            val entity = proofEntity(
                id = "proof-synced-remove",
                taskId = "task-remove",
                fieldKey = "shed_video",
                idempotencyKey = "proof-upload:task-remove:proof-synced-remove",
            ).copy(
                syncStatus = CaptureSyncStatus.SYNCED.name,
                serverProofId = "server-proof-remove",
            )
            db.proofCaptureDao().insert(entity)

            val result = repo.remove("task-remove", "proof-synced-remove")

            assertTrue(result is AppResult.Ok)
            assertEquals(listOf("server-proof-remove"), sync.deleteUploadedProofCalls)
            assertEquals(null, db.proofCaptureDao().findById("proof-synced-remove"))
        } finally {
            db.close()
        }
    }

    @Test
    fun `removing synced proof keeps local row when server delete fails`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository(deleteUploadedProofFailure = "proof is already attached")
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = CoroutineScope(Dispatchers.Unconfined),
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )
            val entity = proofEntity(
                id = "proof-synced-keep",
                taskId = "task-remove",
                fieldKey = "shed_video",
                idempotencyKey = "proof-upload:task-remove:proof-synced-keep",
            ).copy(
                syncStatus = CaptureSyncStatus.SYNCED.name,
                serverProofId = "server-proof-keep",
            )
            db.proofCaptureDao().insert(entity)

            val result = repo.remove("task-remove", "proof-synced-keep")

            assertTrue(result is AppResult.Err)
            assertEquals(listOf("server-proof-keep"), sync.deleteUploadedProofCalls)
            assertEquals(entity, db.proofCaptureDao().findById("proof-synced-keep"))
        } finally {
            db.close()
        }
    }
}

private fun proofEntity(
    id: String,
    taskId: String,
    fieldKey: String,
    idempotencyKey: String,
    outboxItemId: String? = null,
    captureSource: String = "in_app_camera",
) = ProofCaptureEntity(
    id = id,
    taskId = taskId,
    fieldKey = fieldKey,
    proofSubject = ProofSubject.SHED.wireValue,
    subjectId = null,
    localUri = "file://$id.mp4",
    mimeType = "video/mp4",
    caption = null,
    capturedAtMs = 1_000L,
    capturedStartMs = 1_000L,
    capturedEndMs = 4_000L,
    capturedByPrincipalId = "operator-1",
    syncStatus = CaptureSyncStatus.PENDING.name,
    idempotencyKey = idempotencyKey,
    outboxItemId = outboxItemId,
    captureSource = captureSource,
)

/** Minimal, deterministic [SyncRepository] test double: records every
 *  [enqueueProofUpload] call and lets the test manually drive its outbox item's status via
 *  [emit], mirroring how [sg.mesha.goatos.core.data.sync.SyncEngine] would really transition it. */
private class FakeSyncRepository(
    private val deleteOutboxItemFailure: String? = null,
    private val cancelOutboxItemFailure: String? = null,
    private val deleteUploadedProofFailure: String? = null,
    // Simulates the guarded cancel losing the race to the dispatcher (item was IN_FLIGHT at the
    // instant of the DELETE): returns Ok(false) and removes nothing, regardless of observed status.
    private val cancelAlwaysMisses: Boolean = false,
) : SyncRepository {
    data class EnqueueCall(val idempotencyKey: String, val outboxItemId: String, val request: ProofUploadRequestDto)
    data class ScanCall(val idempotencyKey: String, val request: ScanCaptureRequestDto)
    data class AttemptCall(val idempotencyKey: String, val request: ScanAttemptRequestDto)

    val enqueueCalls = mutableListOf<EnqueueCall>()
    // Every enqueueProofUpload invocation, recorded BEFORE the idempotency-dedup short-circuit, so a
    // recovery re-enqueue of an already-known key is still observable (the dedup keeps enqueueCalls
    // at one entry, but the recovery path still invokes enqueue with its derived scope).
    val allEnqueueRequests = mutableListOf<ProofUploadRequestDto>()
    val scanCalls = mutableListOf<ScanCall>()
    val attemptCalls = mutableListOf<AttemptCall>()
    val retryCalls = mutableListOf<String>()
    val deleteOutboxCalls = mutableListOf<String>()
    val cancelCalls = mutableListOf<String>()
    val deleteUploadedProofCalls = mutableListOf<String>()
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private var nextId = 0

    override fun observeStatus(): StateFlow<SyncStatus> = status

    override suspend fun findOutboxItem(itemId: String): AppResult<SyncQueueItem?> =
        AppResult.Ok(status.value.items.firstOrNull { it.id == itemId })

    // Turn the existing outbox row (seeded by capture()'s enqueue) into a definitively-rejected
    // conflict/dead-letter with a controllable lastError, so the reconcile dead-letter branch is
    // exercised. Replaces in place (never appends a duplicate id) so findOutboxItem resolves it.
    fun seedConflict(itemId: String, lastError: String) {
        status.value = status.value.copy(
            items = status.value.items.map { item ->
                if (item.id == itemId) {
                    item.copy(status = SyncItemStatus.FAILED, conflict = true, lastError = lastError)
                } else {
                    item
                }
            },
        )
    }

    fun seed(itemId: String, itemStatus: SyncItemStatus, resultJson: String? = null) {
        status.value = status.value.copy(
            items = status.value.items + syncQueueItem(itemId, itemStatus, resultJson),
        )
    }

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

    override suspend fun enqueueScanCapture(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: ScanCaptureRequestDto,
    ): AppResult<String> {
        scanCalls += ScanCall(idempotencyKey, request)
        return AppResult.Ok("scan-outbox-${scanCalls.size}")
    }

    override suspend fun enqueueScanAttempt(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: ScanAttemptRequestDto,
    ): AppResult<String> {
        attemptCalls += AttemptCall(idempotencyKey, request)
        return AppResult.Ok("attempt-outbox-${attemptCalls.size}")
    }

    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> {
        allEnqueueRequests += request
        // Mirror the real outbox's idempotency (ON CONFLICT (tenant, idempotency_key)): a repeat
        // enqueue of the SAME proof (capture() and the init-block reconciliation both enqueue the
        // same stable idempotencyKey) returns the EXISTING outbox id instead of minting a new one.
        // Without this the fake handed out outbox-0 AND outbox-1 and the winner raced the assertions.
        enqueueCalls.firstOrNull { it.idempotencyKey == idempotencyKey }?.let { return AppResult.Ok(it.outboxItemId) }
        val id = "outbox-${nextId++}"
        enqueueCalls += EnqueueCall(idempotencyKey, id, request)
        status.value = status.value.copy(
            items = status.value.items + syncQueueItem(id, SyncItemStatus.QUEUED, groupKey = groupKey),
        )
        return AppResult.Ok(id)
    }

    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> {
        retryCalls += itemId
        return AppResult.Ok(Unit)
    }

    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = status.map { s ->
        s.items.firstOrNull { it.id == itemId }
    }

    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> {
        deleteOutboxCalls += itemId
        return if (deleteOutboxItemFailure != null) {
            AppResult.Err(deleteOutboxItemFailure)
        } else {
            AppResult.Ok(Unit)
        }
    }

    // Status-guarded cancel mirroring the real DAO's `DELETE ... WHERE status IN ('QUEUED','FAILED')`:
    // an IN_FLIGHT item is NOT cancellable (returns false), a QUEUED/FAILED one is removed and true.
    override suspend fun cancelOutboxItemIfPending(itemId: String): AppResult<Boolean> {
        cancelCalls += itemId
        cancelOutboxItemFailure?.let { return AppResult.Err(it) }
        if (cancelAlwaysMisses) return AppResult.Ok(false)
        val item = status.value.items.firstOrNull { it.id == itemId }
        val cancellable = item != null &&
            (item.status == SyncItemStatus.QUEUED || item.status == SyncItemStatus.FAILED)
        if (cancellable) {
            status.value = status.value.copy(items = status.value.items.filterNot { it.id == itemId })
        }
        return AppResult.Ok(cancellable)
    }

    override suspend fun deleteUploadedProof(proofId: String): AppResult<Unit> {
        deleteUploadedProofCalls += proofId
        deleteUploadedProofFailure?.let { return AppResult.Err(it) }
        return AppResult.Ok(Unit)
    }

    override suspend fun triggerDrain() = Unit
}

private fun syncQueueItem(
    id: String,
    status: SyncItemStatus,
    resultJson: String? = null,
    groupKey: String = "task",
    conflict: Boolean = false,
    lastError: String? = null,
) = SyncQueueItem(
    id = id,
    opType = "PROOF_UPLOAD",
    idempotencyKey = "test-idempotency-key",
    groupKey = groupKey,
    status = status,
    attemptCount = 0,
    maxAttempts = 8,
    conflict = conflict,
    createdAt = 0L,
    updatedAt = 0L,
    lastError = lastError,
    resultJson = resultJson,
)

/**
 * Delegating [ProofCaptureDao] that counts [updateStatus] invocations so a test can prove the F4
 * guard actually SKIPS the write (command-in-query removed), not merely that row values are
 * unchanged. All other methods pass through to the real Room DAO.
 */
private class CountingProofCaptureDao(private val delegate: ProofCaptureDao) : ProofCaptureDao {
    var updateStatusCalls = 0

    override suspend fun insert(entity: ProofCaptureEntity) = delegate.insert(entity)
    override fun observeWorkflowDeathDrafts(workflowId: String): Flow<List<ProofCaptureEntity>> =
        delegate.observeWorkflowDeathDrafts(workflowId)
    override suspend fun findWorkflowDeathDraft(workflowId: String, actionId: String): ProofCaptureEntity? =
        delegate.findWorkflowDeathDraft(workflowId, actionId)
    override suspend fun deleteWorkflowDeathDraft(workflowId: String, actionId: String) =
        delegate.deleteWorkflowDeathDraft(workflowId, actionId)
    override suspend fun clearWorkflowDeathDrafts(workflowId: String) =
        delegate.clearWorkflowDeathDrafts(workflowId)
    override suspend fun markWorkflowDeathDraftsSubmitting(workflowId: String) =
        delegate.markWorkflowDeathDraftsSubmitting(workflowId)
    override fun observeForTask(taskId: String, limit: Int): Flow<List<ProofCaptureEntity>> =
        delegate.observeForTask(taskId, limit)
    override suspend fun listForTask(taskId: String, limit: Int): List<ProofCaptureEntity> =
        delegate.listForTask(taskId, limit)
    override suspend fun listForTaskCleanupPage(taskId: String, afterCapturedAtMs: Long, afterId: String, limit: Int): List<ProofCaptureEntity> =
        delegate.listForTaskCleanupPage(taskId, afterCapturedAtMs, afterId, limit)
    override suspend fun listRecoverableUploadsPage(capturedBeforeMs: Long, afterCapturedAtMs: Long, afterId: String, limit: Int): List<ProofCaptureEntity> =
        delegate.listRecoverableUploadsPage(capturedBeforeMs, afterCapturedAtMs, afterId, limit)
    override suspend fun activeCountForSubject(taskId: String, subjectId: String): Int =
        delegate.activeCountForSubject(taskId, subjectId)
    override suspend fun activeCountForSubjectType(taskId: String, proofSubject: String): Int =
        delegate.activeCountForSubjectType(taskId, proofSubject)
    override suspend fun findById(id: String): ProofCaptureEntity? = delegate.findById(id)
    override suspend fun setOutboxItemId(id: String, outboxItemId: String) = delegate.setOutboxItemId(id, outboxItemId)
    override suspend fun updateStatus(id: String, status: String, serverProofId: String?, lastError: String?) {
        updateStatusCalls++
        delegate.updateStatus(id, status, serverProofId, lastError)
    }
    override suspend fun delete(id: String, taskId: String) = delegate.delete(id, taskId)
    override suspend fun updateCaption(id: String, taskId: String, caption: String) = delegate.updateCaption(id, taskId, caption)
    override suspend fun clearForTask(taskId: String) = delegate.clearForTask(taskId)
    override suspend fun clearAll() = delegate.clearAll()
}
