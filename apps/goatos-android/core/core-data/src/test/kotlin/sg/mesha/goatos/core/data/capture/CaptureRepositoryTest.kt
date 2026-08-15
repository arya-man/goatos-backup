package sg.mesha.goatos.core.data.capture

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.File
import java.nio.file.Files
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
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.database.capture.ProofCaptureDao
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
import sg.mesha.goatos.core.database.capture.ProofCaptureStateEventEntity
import sg.mesha.goatos.core.database.capture.ProofProcessingState
import sg.mesha.goatos.core.database.capture.ScannedGoatEntity
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.capture.FileSystemProofArtifactValidator
import sg.mesha.goatos.core.data.sync.GalleryProofSaver
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
            assertEquals("scan:task-1:goat_scan:tag001:obligation:obl-1:ov0", sync.scanCalls[0].idempotencyKey)
            assertEquals("goat-1", sync.scanCalls[0].request.goatId)
            assertEquals("obl-1", sync.scanCalls[0].request.obligationId)
        } finally {
            db.close()
        }
    }

    @Test
    fun `one scanned animal may persist multiple vaccine obligation captures while submitted tag stays distinct`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultScanCaptureRepository(db.scannedGoatDao(), syncRepository = sync, dispatchers = unconfinedDispatchers)

            repo.recordScan("task-1", "goat_scan", "TAG-001", goatId = "goat-1", obligationId = "et-tt", obligationRowVersion = 7)
            repo.recordScan("task-1", "goat_scan", "TAG-001", goatId = "goat-1", obligationId = "ppr", obligationRowVersion = 3)

            assertEquals("submit answers remain one animal/RFID scan", listOf("TAG-001"), repo.tagsForTask("task-1"))
            assertEquals("durable proof state keeps both due vaccine obligations", 2, repo.observeScannedCount("task-1", "goat_scan").first())
            assertEquals(listOf("et-tt", "ppr"), repo.observeScannedTags("task-1", "goat_scan").first().map { it.obligationId })
            assertEquals(
                listOf(
                    "scan:task-1:goat_scan:tag001:obligation:et-tt:ov7",
                    "scan:task-1:goat_scan:tag001:obligation:ppr:ov3",
                ),
                sync.scanCalls.map { it.idempotencyKey },
            )
        } finally {
            db.close()
        }
    }

    @Test
    fun `enqueuePendingScans recovery replays the row's own obligationRowVersion not ov0`() = runTest {
        // P1 fix: app/process re-entry after a crash left durable evidence without a matching
        // outbox row must rebuild the SAME idempotency key the original enqueue would have built —
        // not the ov0 default — or a reopened/rejected obligation's recovery silently dedupes
        // against the OLD cycle's key.
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val repo = DefaultScanCaptureRepository(db.scannedGoatDao(), syncRepository = sync, dispatchers = unconfinedDispatchers)

            // Seed a durable scan row directly at the DB layer (simulating: row was written to
            // Room before the process died, so no matching outbox item exists yet) with a
            // non-zero obligationRowVersion, as a reopened obligation's row would carry.
            db.scannedGoatDao().upsertScan(
                ScannedGoatEntity(
                    id = "seed-row-1",
                    taskId = "task-recovery",
                    partitionKey = "whole",
                    fieldKey = "goat_scan",
                    tag = "tag009",
                    goatId = "goat-9",
                    obligationId = "ppr",
                    capturedAtMs = 1_000L,
                    obligationRowVersion = 9,
                ),
            )

            repo.enqueuePendingScans("task-recovery", "goat_scan")

            assertEquals(1, sync.scanCalls.size)
            assertEquals(
                "recovery must key off the row's persisted row_version, not the ov0 default",
                "scan:task-recovery:goat_scan:tag009:obligation:ppr:ov9",
                sync.scanCalls.single().idempotencyKey,
            )
        } finally {
            db.close()
        }
    }

    @Test
    fun `sibling partitions of one task keep scan and proof evidence separate`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val scans = DefaultScanCaptureRepository(
                db.scannedGoatDao(),
                syncRepository = sync,
                dispatchers = unconfinedDispatchers,
            )
            val proofs = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = false,
            )

            scans.recordScan(
                taskId = "task-partitions",
                fieldKey = ROSTER_SCAN_FIELD_KEY,
                tag = "TAG-SHARED",
                goatId = "goat-1",
                obligationId = "obl-1",
                partitionLabel = "Part 1",
            )
            scans.recordScan(
                taskId = "task-partitions",
                fieldKey = ROSTER_SCAN_FIELD_KEY,
                tag = "TAG-SHARED",
                goatId = "goat-2",
                obligationId = "obl-2",
                partitionLabel = "2",
            )

            val oneProofPerPartition = ProofPolicy(
                proofMode = "shed_level_video",
                subjectScope = "shed",
                expectedSubjects = listOf("shed"),
                maximumCount = 1,
                maximumCountPerSubject = 1,
            )
            proofs.capture(
                taskId = "task-partitions",
                fieldKey = "shed_video",
                subject = ProofSubject.SHED,
                subjectId = "shed-1",
                localUri = "file://part-1.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = "shed-1",
                capturedStartMs = 1L,
                capturedEndMs = 2L,
                capturedByPrincipalId = "operator-1",
                proofPolicy = oneProofPerPartition,
                partitionLabel = "Part 1",
            )
            proofs.capture(
                taskId = "task-partitions",
                fieldKey = "shed_video",
                subject = ProofSubject.SHED,
                subjectId = "shed-1",
                localUri = "file://part-2.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = "shed-1",
                capturedStartMs = 3L,
                capturedEndMs = 4L,
                capturedByPrincipalId = "operator-1",
                proofPolicy = oneProofPerPartition,
                partitionLabel = "2",
            )

            assertEquals(listOf("goat-1"), scans.observeAllForTask("task-partitions", "1").first().map { it.goatId })
            assertEquals(listOf("goat-2"), scans.observeAllForTask("task-partitions", "Part 2").first().map { it.goatId })
            assertEquals(listOf("file://part-1.mp4"), proofs.observeProofs("task-partitions", "1").first().map { it.localUri })
            assertEquals(listOf("file://part-2.mp4"), proofs.observeProofs("task-partitions", "Part 2").first().map { it.localUri })
            assertEquals(listOf("1", "2"), sync.scanCalls.map { it.partitionKey })
            assertTrue(sync.scanCalls[0].idempotencyKey.contains(":partition:1:"))
            assertTrue(sync.scanCalls[1].idempotencyKey.contains(":partition:2:"))
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
                awaitUploadEnqueue = true,
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
                appScope = backgroundScope,
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
                appScope = backgroundScope,
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
    fun `proof upload success retains app private preview files until row cleanup`() = runTest {
        val db = newDb()
        val originalFile = Files.createTempFile("goatos-proof-original-", ".mp4").toFile()
        val processedFile = Files.createTempFile("goatos-proof-processed-", ".mp4").toFile()
        try {
            originalFile.writeText("original")
            processedFile.writeText("processed")
            val sync = FakeSyncRepository()
            val processor = RecordingProofMediaProcessor(
                ProofMediaProcessingResult(
                    outputUri = processedFile.toURI().toString(),
                    outputMimeType = "video/mp4",
                    originalBytes = originalFile.length(),
                    processedBytes = processedFile.length(),
                ),
            )
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = processor,
            )

            val captured = (
                repo.capture(
                    taskId = "task-retain-preview",
                    fieldKey = "feed_distribution_video",
                    subject = ProofSubject.SHED,
                    localUri = originalFile.toURI().toString(),
                    mimeType = "video/mp4",
                    caption = "Feed direction video",
                    scopeType = "task",
                    scopeId = "task-retain-preview",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                    awaitUploadEnqueue = true,
                ) as AppResult.Ok
                ).value

            val itemId = db.proofCaptureDao().findById(captured.id)?.outboxItemId
            assertTrue("Proof should have an outbox item ID", !itemId.isNullOrBlank())
            val response = ProofUploadResponseDto(proof = ProofReferenceDto(proofId = "server-proof-retained"))
            sync.emit(itemId!!, SyncItemStatus.SUCCEEDED, resultJson = syncJson.encodeToString(response))

            val row = repo.observeProofs("task-retain-preview").first().single()
            assertEquals(CaptureSyncStatus.SYNCED, row.syncStatus)
            assertEquals(processedFile.toURI().toString(), row.localUri)
            assertTrue("original proof file is retained for explicit row cleanup", originalFile.exists())
            assertTrue("processed proof file is retained for synced preview", processedFile.exists())
        } finally {
            db.close()
            originalFile.delete()
            processedFile.delete()
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
    fun `observing proofs repairs pending proof with no outbox item`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            db.proofCaptureDao().insert(
                proofEntity(
                    id = "proof-live-orphan",
                    taskId = "task-live-orphan",
                    fieldKey = "feed_distribution_water_video",
                    idempotencyKey = "proof-upload:task-live-orphan:proof-live-orphan",
                ),
            )

            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
            )

            repo.observeProofs("task-live-orphan").first()
            advanceUntilIdle()

            val row = db.proofCaptureDao().findById("proof-live-orphan")
            assertEquals(1, sync.enqueueCalls.size)
            assertEquals("proof-upload:task-live-orphan:proof-live-orphan", sync.enqueueCalls.single().idempotencyKey)
            assertEquals("outbox-0", row?.outboxItemId)
            assertEquals(CaptureSyncStatus.PENDING.name, row?.syncStatus)
        } finally {
            db.close()
        }
    }

    @Test
    fun `observing proofs repairs pending proof whose outbox item is missing`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            db.proofCaptureDao().insert(
                proofEntity(
                    id = "proof-stale-outbox",
                    taskId = "task-stale-outbox",
                    fieldKey = "feed_distribution_water_video",
                    idempotencyKey = "proof-upload:task-stale-outbox:proof-stale-outbox",
                    outboxItemId = "outbox-pruned-before-row-updated",
                ).copy(
                    localUri = "file://processed-water-video.mp4",
                    originalUri = "file://original-water-video.mp4",
                    processedUri = "file://processed-water-video.mp4",
                    processingState = sg.mesha.goatos.core.database.capture.ProofProcessingState.PROCESSED.name,
                    processingAttempted = true,
                ),
            )

            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            repo.observeProofs("task-stale-outbox").first()
            advanceUntilIdle()

            val row = db.proofCaptureDao().findById("proof-stale-outbox")
            assertEquals(1, sync.enqueueCalls.size)
            assertEquals("proof-upload:task-stale-outbox:proof-stale-outbox", sync.enqueueCalls.single().idempotencyKey)
            assertEquals("file://processed-water-video.mp4", sync.enqueueCalls.single().localFilePath)
            assertEquals("outbox-0", row?.outboxItemId)
            assertEquals(CaptureSyncStatus.PENDING.name, row?.syncStatus)
        } finally {
            db.close()
        }
    }

    @Test
    fun `processed proof upload uses human RFID metadata and processed artifact path`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val processor = RecordingProofMediaProcessor(
                ProofMediaProcessingResult(
                    outputUri = "file://processed-proof.mp4",
                    outputMimeType = "video/mp4",
                    originalBytes = 12_000_000L,
                    processedBytes = 2_000_000L,
                    targetVideoBitrate = 6_000_000,
                    targetAudioBitrate = 48_000,
                ),
            )
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = processor,
            )

            repo.capture(
                taskId = "task-vaccine",
                fieldKey = "vaccination_goat_proof",
                subject = ProofSubject.GOAT,
                subjectId = "44444444-4444-4444-4444-444444444444",
                localUri = "file://original-proof.mp4",
                mimeType = "video/mp4",
                caption = "Vaccination · CPT · Castro 1 · ET+TT, PPR",
                rfidTag = "C1-901007000504332",
                scopeType = "task",
                scopeId = "task-vaccine",
                capturedStartMs = 1_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = "operator-1",
                awaitUploadEnqueue = true,
            )
            advanceUntilIdle()

            assertEquals("C1-901007000504332", processor.requests.single().rfidTag)
            assertEquals("Vaccination · CPT · Castro 1 · ET+TT, PPR", processor.requests.single().caption)
            assertEquals("file://processed-proof.mp4", sync.enqueueCalls.single().localFilePath)
            assertEquals("C1-901007000504332", sync.enqueueCalls.single().request.metadata["rfid_tag"]?.jsonPrimitive?.content)
            assertEquals("Vaccination · CPT · Castro 1 · ET+TT, PPR", sync.enqueueCalls.single().request.metadata["caption"]?.jsonPrimitive?.content)
            assertEquals("44444444-4444-4444-4444-444444444444", sync.enqueueCalls.single().request.subjectId)
        } finally {
            db.close()
        }
    }

    @Test
    fun `gallery save happens once for final processed artifact across recovery reenqueues`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val gallery = RecordingGalleryProofSaver()
            val processor = RecordingProofMediaProcessor(
                ProofMediaProcessingResult(
                    outputUri = "file://processed-proof.mp4",
                    outputMimeType = "video/mp4",
                    originalBytes = 12_000_000L,
                    processedBytes = 2_000_000L,
                ),
            )
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = processor,
                galleryProofSaver = gallery,
            )

            val captured = (
                repo.capture(
                    taskId = "task-gallery-once",
                    fieldKey = "feed_distribution_video",
                    subject = ProofSubject.SHED,
                    localUri = "file://original-proof.mp4",
                    mimeType = "video/mp4",
                    caption = "Feed direction proof",
                    scopeType = "task",
                    scopeId = "task-gallery-once",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                    awaitUploadEnqueue = true,
                ) as AppResult.Ok
                ).value

            assertEquals(listOf("file://processed-proof.mp4"), gallery.paths)
            assertEquals("file://processed-proof.mp4", sync.enqueueCalls.single().localFilePath)
            assertEquals("file://processed-proof.mp4", db.proofCaptureDao().findById(captured.id)?.gallerySavedUri)

            db.proofCaptureDao().setOutboxItemId(captured.id, null)
            repo.reconcileRecoverableUploadsNow()

            assertEquals(listOf("file://processed-proof.mp4"), gallery.paths)
            assertEquals(1, processor.requests.size)
            assertEquals(1, sync.enqueueCalls.size)
        } finally {
            db.close()
        }
    }

    @Test
    fun `gallery save event suppresses duplicate gallery copy when saved marker is stale`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val gallery = RecordingGalleryProofSaver()
            val entity = proofEntity(
                id = "proof-gallery-stale-marker",
                taskId = "task-gallery-stale-marker",
                fieldKey = "feed_distribution_video",
                idempotencyKey = "proof-upload:task-gallery-stale-marker:proof-gallery-stale-marker",
            ).copy(
                localUri = "file://processed-proof.mp4",
                originalUri = "file://original-proof.mp4",
                processedUri = "file://processed-proof.mp4",
                processingState = ProofProcessingState.PROCESSED.name,
                processingAttempted = true,
                stateAttempt = 1,
                originalBytes = 12_000_000L,
                processedBytes = 2_000_000L,
                gallerySavedUri = null,
            )
            db.proofCaptureDao().insert(entity)
            db.proofCaptureDao().insertStateEvent(
                ProofCaptureStateEventEntity(
                    id = "event-gallery-completed",
                    proofId = entity.id,
                    fromState = ProofProcessingState.PROCESSED.name,
                    toState = ProofProcessingState.PROCESSED.name,
                    stage = "gallery_save_completed",
                    attempt = 1,
                    occurredAtMs = 2_000L,
                ),
            )
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                galleryProofSaver = gallery,
            )

            repo.reconcileRecoverableUploadsNow()
            advanceUntilIdle()

            assertEquals(emptyList<String>(), gallery.paths)
            assertEquals("file://processed-proof.mp4", db.proofCaptureDao().findById(entity.id)?.gallerySavedUri)
            assertEquals(1, db.proofCaptureDao().countStateEvents(entity.id, "gallery_save_completed"))
            assertEquals("file://processed-proof.mp4", sync.enqueueCalls.single().localFilePath)
        } finally {
            db.close()
        }
    }

    @Test
    fun `processor failure leaves proof awaiting operator retry and never auto-uploads the raw original`() = runTest {
        // P1 fix: processing failure used to fall through to uploadOriginal=true and auto-enqueue
        // the raw original as completed proof -- an overlay-free capture could then silently
        // satisfy a compliance proof gate. The new default (no proof_policy opt-in exists) is:
        // NO enqueue, NO gallery save, state exposes an actionable retry, original stays on disk.
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val gallery = RecordingGalleryProofSaver()
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = ThrowingProofMediaProcessor(),
                galleryProofSaver = gallery,
            )

            val captured = (
                repo.capture(
                    taskId = "task-gallery-fallback",
                    fieldKey = "feed_distribution_video",
                    subject = ProofSubject.SHED,
                    localUri = "file://original-proof.mp4",
                    mimeType = "video/mp4",
                    caption = "Feed direction proof",
                    scopeType = "task",
                    scopeId = "task-gallery-fallback",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                    awaitUploadEnqueue = true,
                ) as AppResult.Ok
                ).value

            // NO original upload op enqueued for this required-overlay flow.
            assertEquals("Corrupt processing must not enqueue any upload", emptyList<String>(), sync.enqueueCalls)
            assertEquals("Corrupt processing must not save to Gallery either", emptyList<String>(), gallery.paths)

            val row = db.proofCaptureDao().findById(captured.id)
            assertEquals(
                "State exposes an actionable retry, not a queued original upload",
                ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                row?.processingState,
            )
            assertEquals("uploadOriginal must stay false — nothing is queued", false, row?.uploadOriginal)
            assertEquals("Original file path preserved (safety kept)", "file://original-proof.mp4", row?.localUri)
            assertEquals("syncStatus stays PENDING — never FAILED for a processing failure", CaptureSyncStatus.PENDING.name, row?.syncStatus)

            // Startup/process-death recovery must not resurrect the auto-upload either — it is
            // idempotent through the same AWAITING_RETRY short-circuit, not a second enqueue.
            repo.reconcileRecoverableUploadsNow()
            assertEquals("Recovery must not enqueue the raw original", emptyList<String>(), sync.enqueueCalls)
        } finally {
            db.close()
        }
    }

    @Test
    fun `retryUpload re-invokes the processor and succeeds once it recovers`() = runTest {
        // P1 fix: the only path back from PROCESSING_FAILED_AWAITING_RETRY is an explicit operator
        // action. retryUpload() resets processingAttempted so prepareFinalArtifact re-runs the
        // media processor; once it succeeds, registration/upload proceeds normally with the
        // PROCESSED (overlay-burned) artifact -- never the raw original.
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val gallery = RecordingGalleryProofSaver()
            val processor = SometimesFailingProofMediaProcessor(
                failFirst = 1,
                successResult = ProofMediaProcessingResult(
                    outputUri = "file://processed-proof.mp4",
                    outputMimeType = "video/mp4",
                    originalBytes = 12_000_000L,
                    processedBytes = 4_000_000L,
                ),
            )
            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = processor,
                galleryProofSaver = gallery,
            )

            val captured = (
                repo.capture(
                    taskId = "task-retry-processing",
                    fieldKey = "feed_distribution_video",
                    subject = ProofSubject.SHED,
                    localUri = "file://original-proof.mp4",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "task",
                    scopeId = "task-retry-processing",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 4_000L,
                    capturedByPrincipalId = "operator-1",
                    awaitUploadEnqueue = true,
                ) as AppResult.Ok
                ).value

            // First attempt failed: awaiting retry, nothing enqueued.
            assertEquals(
                ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                db.proofCaptureDao().findById(captured.id)?.processingState,
            )
            assertEquals(0, sync.enqueueCalls.size)

            // Operator retries; the processor now succeeds.
            val retryResult = repo.retryUpload("task-retry-processing", captured.id)
            assertTrue("Retry succeeds", retryResult is AppResult.Ok)

            val row = db.proofCaptureDao().findById(captured.id)
            // enqueueRegistrationNow's Ok branch advances processingState to REGISTERING_UPLOAD once
            // the outbox write is queued (same as any other successful registration) -- the row
            // still carries the PROCESSED (overlay-burned) artifact's localUri/uploadOriginal, not
            // the raw original's.
            assertEquals("Registration succeeded with the processed artifact, not the raw original", ProofProcessingState.REGISTERING_UPLOAD.name, row?.processingState)
            assertEquals("uploadOriginal must be false for the processed artifact", false, row?.uploadOriginal)
            assertEquals("file://processed-proof.mp4", row?.localUri)
            assertEquals(1, sync.enqueueCalls.size)
            assertEquals("file://processed-proof.mp4", sync.enqueueCalls.single().localFilePath)
            assertEquals(
                "false",
                sync.enqueueCalls.single().request.metadata["upload_original"]?.jsonPrimitive?.content,
            )
        } finally {
            db.close()
        }
    }

    @Test
    fun `observing proofs does not resurrect failed proof with no outbox item`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            db.proofCaptureDao().insert(
                proofEntity(
                    id = "proof-failed-no-outbox",
                    taskId = "task-failed-no-outbox",
                    fieldKey = "feed_distribution_water_video",
                    idempotencyKey = "proof-upload:task-failed-no-outbox:proof-failed-no-outbox",
                ).copy(
                    syncStatus = CaptureSyncStatus.FAILED.name,
                    lastError = "upload failed permanently",
                ),
            )

            val repo = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
            )

            repo.observeProofs("task-failed-no-outbox").first()
            advanceUntilIdle()

            val row = db.proofCaptureDao().findById("proof-failed-no-outbox")
            assertEquals(0, sync.enqueueCalls.size)
            assertEquals(CaptureSyncStatus.FAILED.name, row?.syncStatus)
            assertEquals("upload failed permanently", row?.lastError)
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                appScope = backgroundScope,
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
                appScope = backgroundScope,
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
                appScope = backgroundScope,
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
                appScope = backgroundScope,
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
                appScope = backgroundScope,
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
                appScope = backgroundScope,
                reconcileOnStartup = false,
                dispatchers = unconfinedDispatchers,
                mediaProcessor = IdentityProofMediaProcessor(),
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
                    awaitUploadEnqueue = true,
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
                appScope = backgroundScope,
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
                appScope = backgroundScope,
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

    @Test
    fun `one video covers multiple obligations in vaccination contract (animal grain, prove readiness)`() = runTest {
        // Vaccination contract: One goat with 2 vaccine obligations (e.g., PPR + ET+TT).
        // One proof video captures both obligations' requirements.
        // Expectations:
        // - Roster row grain is animal (ScanProofIdentityTest ensures this)
        // - Done tracked per obligation underneath (via scanned goat rows)
        // - Both obligations' submit readiness satisfied by single clip
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val scans = DefaultScanCaptureRepository(
                db.scannedGoatDao(),
                syncRepository = sync,
                dispatchers = unconfinedDispatchers,
            )
            val proofs = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = false,
            )

            val taskId = "vax-campaign-1"
            val goatId = "goat-multi-vacc"
            val pprObligation = "obl-ppr"
            val ettObligation = "obl-et-tt"
            val tag = "C1-901007000504418"

            // Goat scanned for both obligations (different vaccine schedules on same animal)
            scans.recordScan(
                taskId = taskId,
                fieldKey = ROSTER_SCAN_FIELD_KEY,
                tag = tag,
                goatId = goatId,
                obligationId = pprObligation,
            )
            scans.recordScan(
                taskId = taskId,
                fieldKey = ROSTER_SCAN_FIELD_KEY,
                tag = tag,
                goatId = goatId,
                obligationId = ettObligation,
            )

            // One proof capture for the goat covers both obligations
            val captureResult = proofs.capture(
                taskId = taskId,
                fieldKey = "goat_scan_proof",
                subject = ProofSubject.GOAT,
                subjectId = goatId,
                localUri = "file://multi-vax.mp4",
                mimeType = "video/mp4",
                caption = "Vaccination · Multivacc · $tag · PPR+ET+TT",
                rfidTag = tag,
                scopeType = "task",
                scopeId = taskId,
                capturedStartMs = 1_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = "operator-1",
                proofPolicy = ProofPolicy(
                    proofMode = "per_goat_video",
                    subjectScope = "goat",
                    expectedSubjects = listOf("goat"),
                    maximumCount = 5,
                    maximumCountPerSubject = 5,
                ),
                partitionLabel = null,
                awaitUploadEnqueue = false,
                uploadGroupKey = null,
            )
            assertTrue("Proof capture must succeed", captureResult is AppResult.Ok)

            // Both obligations have scanned evidence
            val allScans = scans.observeAllForTask(taskId).first()
            assertEquals("Both obligations scanned for same goat", 2, allScans.size)
            val scannedObligations = allScans.sortedBy { it.obligationId }.map { it.obligationId }
            assertEquals(
                listOf(ettObligation, pprObligation),
                scannedObligations,
            )
            assertEquals("Both scans for same goat", listOf(goatId, goatId), allScans.sortedBy { it.obligationId }.map { it.goatId })

            // Roster row grain is animal (one proof for goat, two obligations underneath)
            val proofCount = proofs.observeProofs(taskId).first().size
            assertEquals("One proof clip for the goat", 1, proofCount)
            val singleProof = proofs.observeProofs(taskId).first().single()
            assertEquals("Proof subject is the goat", goatId, singleProof.subjectId)
            assertEquals("Proof references tag", tag, singleProof.rfidTag)
        } finally {
            db.close()
        }
    }

    @Test
    fun `captureReplacingLatest removes only the old row after successful capture`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            // taskId must be the actual identity that'll be used in capture()
            val shedId = "shed-1"
            val taskId = "feed-dist:2026-08-13:$shedId:whole:1:normal"
            val fieldKey = "feed_distribution_video"
            val proofs = DefaultProofCaptureRepository(
                db.proofCaptureDao(),
                sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = false,
                mediaProcessor = IdentityProofMediaProcessor(),
            )

            val slot = EvidenceSlot(
                identity = ProofIdentity(
                    flow = ProofFlow.FEED_DISTRIBUTION,
                    taskId = taskId,  // Use the full groupKey as taskId, like the ViewModels do
                    partitionKey = "whole",
                ),
                fieldKey = fieldKey,
            )

            // First capture
            val first = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = "file:///first.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = 1_000L,
                capturedEndMs = 2_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("First capture succeeds", first is AppResult.Ok)
            val firstId = (first as AppResult.Ok).value.id
            assertEquals("One proof in slot", 1, proofs.observeProofs(taskId).first().size)

            // Second capture (replace)
            val second = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = "file:///second.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = 3_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("Second capture succeeds", second is AppResult.Ok)
            val secondId = (second as AppResult.Ok).value.id

            // P1 fix: the old row is NOT retired just because the new capture succeeded locally —
            // it stays active/visible until the new upload reaches SYNCED with a server proof id.
            val beforeSync = proofs.observeProofs(taskId).first()
            assertEquals("Old row survives until the new upload is confirmed SYNCED", 2, beforeSync.size)
            assertTrue("Old row still present pre-sync", beforeSync.any { it.id == firstId })

            sync.completeUpload(db, secondId, "server-proof-second")
            advanceUntilIdle()

            // Verify exactly one row remains: the new one, now that it reached SYNCED
            val remaining = proofs.observeProofs(taskId).first()
            assertEquals("Exactly one proof remains once the replacement is confirmed SYNCED", 1, remaining.size)
            assertEquals("Remaining proof is the new one", secondId, remaining[0].id)
            assertEquals("New proof path is second (processed)", "file:///second.mp4.processed", remaining[0].localUri)
        } finally {
            db.close()
        }
    }

    @Test
    fun `captureReplacingLatest removes all two pre-existing active rows and keeps only the new one`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val shedId = "shed-2"
            val taskId = "feed-dist:2026-08-13:$shedId:whole:1:normal"
            val fieldKey = "feed_distribution_video"
            val proofs = DefaultProofCaptureRepository(
                db.proofCaptureDao(),
                sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = false,
                mediaProcessor = IdentityProofMediaProcessor(),
            )

            val slot = EvidenceSlot(
                identity = ProofIdentity(
                    flow = ProofFlow.FEED_DISTRIBUTION,
                    taskId = taskId,
                    partitionKey = "whole",
                ),
                fieldKey = fieldKey,
            )

            // Manually seed two pre-existing active rows by calling capture() with allowReplacementOverCap=true
            // (simulating a corrupted state where two rows ended up for the same slot)
            val oldCapture1 = proofs.capture(
                taskId = taskId,
                fieldKey = fieldKey,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = "file:///old-1.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = 1_000L,
                capturedEndMs = 2_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
                allowReplacementOverCap = true,
            )
            assertTrue("First seed capture succeeds", oldCapture1 is AppResult.Ok)
            val oldId1 = (oldCapture1 as AppResult.Ok).value.id

            val oldCapture2 = proofs.capture(
                taskId = taskId,
                fieldKey = fieldKey,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = "file:///old-2.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = 3_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
                allowReplacementOverCap = true,
            )
            assertTrue("Second seed capture succeeds", oldCapture2 is AppResult.Ok)
            val oldId2 = (oldCapture2 as AppResult.Ok).value.id

            // Verify two rows exist
            val beforeReplace = proofs.observeProofs(taskId).first()
            assertEquals("Two proofs seeded before replace", 2, beforeReplace.size)

            // Now replace: captureReplacingLatest must remove BOTH old rows
            val newCapture = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = "file:///new-3.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = 5_000L,
                capturedEndMs = 6_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("Replace capture succeeds", newCapture is AppResult.Ok)
            val newId = (newCapture as AppResult.Ok).value.id

            // P1 fix: both old rows survive until the new upload is confirmed SYNCED.
            val beforeSync = proofs.observeProofs(taskId).first()
            assertEquals("All three rows present pre-sync (both old + new)", 3, beforeSync.size)

            sync.completeUpload(db, newId, "server-proof-new-3")
            advanceUntilIdle()

            // Verify exactly one row remains: the new one, once SYNCED
            val afterReplace = proofs.observeProofs(taskId).first()
            assertEquals("Exactly one proof remains after replace (both old removed)", 1, afterReplace.size)
            assertEquals("Remaining proof is the new one", newId, afterReplace[0].id)
            assertEquals("New proof path is the new capture (processed)", "file:///new-3.mp4.processed", afterReplace[0].localUri)
            assertTrue("New ID is different from both old IDs", newId != oldId1 && newId != oldId2)
        } finally {
            db.close()
        }
    }

    @Test
    fun `captureReplacingLatest keeps old untouched when capture fails`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val shedId = "shed-1"
            val taskId = "feed-dist:2026-08-13:$shedId:whole:1:normal"
            val fieldKey = "feed_distribution_video"
            val proofs = DefaultProofCaptureRepository(
                db.proofCaptureDao(),
                sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                proofArtifactValidator = object : ProofArtifactValidator {
                    override fun validateVideoFile(localUri: String): ProofArtifactValidator.ValidationResult =
                        if (localUri.isBlank()) {
                            ProofArtifactValidator.ValidationResult(isValid = false, reason = "blank uri")
                        } else {
                            ProofArtifactValidator.ValidationResult(isValid = true)
                        }
                },
                reconcileOnStartup = false,
            )

            val slot = EvidenceSlot(
                identity = ProofIdentity(
                    flow = ProofFlow.FEED_DISTRIBUTION,
                    taskId = taskId,
                    partitionKey = "whole",
                ),
                fieldKey = fieldKey,
            )

            // First capture
            val first = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = "file:///first.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = 1_000L,
                capturedEndMs = 2_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("First capture succeeds", first is AppResult.Ok)
            val firstId = (first as AppResult.Ok).value.id

            // Second capture fails: invalid file (empty URI)
            val second = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = "", // invalid: empty
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = 3_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("Second capture fails", second is AppResult.Err)

            // Verify first row is untouched
            val remaining = proofs.observeProofs(taskId).first()
            assertEquals("One proof still in slot", 1, remaining.size)
            assertEquals("Old proof survived failed replacement", firstId, remaining[0].id)
            assertEquals("Old proof path unchanged", "file:///first.mp4", remaining[0].localUri)
        } finally {
            db.close()
        }
    }

    @Test
    fun `concurrent captureReplacingLatest converges to exactly one active row`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val shedId = "shed-1"
            val taskId = "feed-dist:2026-08-13:$shedId:whole:1:normal"
            val fieldKey = "feed_distribution_video"
            val proofs = DefaultProofCaptureRepository(
                db.proofCaptureDao(),
                sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = false,
                mediaProcessor = IdentityProofMediaProcessor(),
            )

            val slot = EvidenceSlot(
                identity = ProofIdentity(
                    flow = ProofFlow.FEED_DISTRIBUTION,
                    taskId = taskId,
                    partitionKey = "whole",
                ),
                fieldKey = fieldKey,
            )

            // First capture
            val first = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = "file:///first.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = 1_000L,
                capturedEndMs = 2_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("First capture succeeds", first is AppResult.Ok)

            // Concurrent second replace
            val second = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = "file:///second.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = 3_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("Second replace succeeds", second is AppResult.Ok)

            // Concurrent third replace
            val third = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = "file:///third.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = 5_000L,
                capturedEndMs = 6_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("Third replace succeeds", third is AppResult.Ok)
            val thirdId = (third as AppResult.Ok).value.id

            // P1 fix: three local/queued replaces in a row do not retire anything by themselves —
            // all three stay active until whichever one actually reaches SYNCED.
            val beforeSync = proofs.observeProofs(taskId).first()
            assertEquals("All three replaces stay active pre-sync", 3, beforeSync.size)

            sync.completeUpload(db, thirdId, "server-proof-third")
            advanceUntilIdle()

            // Once the third is confirmed SYNCED, it retires every other same-subject occupant —
            // exactly one active row should remain: the third.
            val remaining = proofs.observeProofs(taskId).first()
            assertEquals("Exactly one proof after concurrent replaces", 1, remaining.size)
            assertEquals("The most recent proof is active", thirdId, remaining[0].id)
            assertEquals("Newest proof path is third (processed)", "file:///third.mp4.processed", remaining[0].localUri)
        } finally {
            db.close()
        }
    }

    // ============================================================================
    // ITEM 7: Cross-subject grain isolation in captureReplacingLatest
    // ============================================================================
    // ITEM 7: Two active rows same task+field different subjectIds → captureReplacingLatest
    // for subject A removes only A's old row, B untouched.
    @Test
    fun `captureReplacingLatest removes only same-subject old rows, leaves sibling subjects untouched`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val taskId = "vacc-task:per-goat"
            val fieldKey = "vaccination_goat_proof"
            val proofs = DefaultProofCaptureRepository(
                db.proofCaptureDao(),
                sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = false,
                mediaProcessor = IdentityProofMediaProcessor(),
            )

            val slot = EvidenceSlot(
                identity = ProofIdentity(
                    flow = ProofFlow.VACCINATION,
                    taskId = taskId,
                    partitionKey = "whole",
                ),
                fieldKey = fieldKey,
            )

            // Capture for goat A
            val aFirst = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.GOAT,
                subjectId = "goat-a",
                localUri = "file:///a-first.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "goat",
                scopeId = "goat-a",
                capturedStartMs = 1_000L,
                capturedEndMs = 2_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("First capture for goat A succeeds", aFirst is AppResult.Ok)
            val aFirstId = (aFirst as AppResult.Ok).value.id

            // Capture for goat B (same task, same field, different subject)
            val bFirst = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.GOAT,
                subjectId = "goat-b",
                localUri = "file:///b-first.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "goat",
                scopeId = "goat-b",
                capturedStartMs = 3_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("First capture for goat B succeeds", bFirst is AppResult.Ok)
            val bFirstId = (bFirst as AppResult.Ok).value.id

            // Verify both rows exist
            var allRows = proofs.observeProofs(taskId).first()
            assertEquals("Two proofs after capturing both subjects", 2, allRows.size)

            // Now replace: captureReplacingLatest for subject A
            val aSecond = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.GOAT,
                subjectId = "goat-a",
                localUri = "file:///a-second.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "goat",
                scopeId = "goat-a",
                capturedStartMs = 5_000L,
                capturedEndMs = 6_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("Second capture for goat A succeeds", aSecond is AppResult.Ok)
            val aSecondId = (aSecond as AppResult.Ok).value.id

            // P1 fix: A's old row is not retired just because the new A capture succeeded locally.
            allRows = proofs.observeProofs(taskId).first()
            assertEquals("Three rows pre-sync: old A, new A, old B", 3, allRows.size)
            assertTrue("Old A row still present pre-sync", allRows.any { it.id == aFirstId })

            sync.completeUpload(db, aSecondId, "server-proof-a-second")
            advanceUntilIdle()

            // Once the new A capture is confirmed SYNCED: ITEM 7 — only A's old row is removed, B's row untouched
            allRows = proofs.observeProofs(taskId).first()
            assertEquals("Two proofs remain: new A + old B", 2, allRows.size)
            val rowIds = allRows.map { it.id }.toSet()
            assertTrue("New A row exists", rowIds.contains(aSecondId))
            assertTrue("Old B row still exists (not removed)", rowIds.contains(bFirstId))
            assertFalse("Old A row removed", rowIds.contains(aFirstId))
        } finally {
            db.close()
        }
    }

    // ITEM 7: Concurrent A/B replaces don't serialize on each other (different mutex keys).
    @Test
    fun `concurrent captureReplacingLatest on different subjects does not serialize mutex`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val taskId = "vacc-task:per-goat:concurrent"
            val fieldKey = "vaccination_goat_proof"
            val proofs = DefaultProofCaptureRepository(
                db.proofCaptureDao(),
                sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = false,
                mediaProcessor = IdentityProofMediaProcessor(),
            )

            val slot = EvidenceSlot(
                identity = ProofIdentity(
                    flow = ProofFlow.VACCINATION,
                    taskId = taskId,
                    partitionKey = "whole",
                ),
                fieldKey = fieldKey,
            )

            // First capture for A
            val aFirst = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.GOAT,
                subjectId = "goat-x",
                localUri = "file:///x-first.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "goat",
                scopeId = "goat-x",
                capturedStartMs = 1_000L,
                capturedEndMs = 2_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("First capture for X succeeds", aFirst is AppResult.Ok)

            // First capture for B
            val bFirst = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.GOAT,
                subjectId = "goat-y",
                localUri = "file:///y-first.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "goat",
                scopeId = "goat-y",
                capturedStartMs = 3_000L,
                capturedEndMs = 4_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("First capture for Y succeeds", bFirst is AppResult.Ok)

            // Concurrent replace A/B on different subjects: must both succeed, not serialize
            val aReplace = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.GOAT,
                subjectId = "goat-x",
                localUri = "file:///x-replace.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "goat",
                scopeId = "goat-x",
                capturedStartMs = 5_000L,
                capturedEndMs = 6_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("Concurrent replace for X succeeds", aReplace is AppResult.Ok)

            val bReplace = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.GOAT,
                subjectId = "goat-y",
                localUri = "file:///y-replace.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "goat",
                scopeId = "goat-y",
                capturedStartMs = 7_000L,
                capturedEndMs = 8_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("Concurrent replace for Y succeeds", bReplace is AppResult.Ok)
            val xReplaceId = (aReplace as AppResult.Ok).value.id
            val yReplaceId = (bReplace as AppResult.Ok).value.id

            // P1 fix: neither replace retires its predecessor until its own upload is SYNCED.
            var allRows = proofs.observeProofs(taskId).first()
            assertEquals("Four rows pre-sync: old X, old Y, new X, new Y", 4, allRows.size)

            sync.completeUpload(db, xReplaceId, "server-proof-x-replace")
            advanceUntilIdle()
            sync.completeUpload(db, yReplaceId, "server-proof-y-replace")
            advanceUntilIdle()

            // ITEM 7: Both replaces succeeded without one blocking the other. Final state, once both
            // are confirmed SYNCED: two active rows.
            allRows = proofs.observeProofs(taskId).first()
            assertEquals("Two active proofs after concurrent replaces", 2, allRows.size)
            val rowsBySubject = allRows.associateBy { it.subjectId }
            assertEquals("X proof path is replace (processed)", "file:///x-replace.mp4.processed", rowsBySubject["goat-x"]?.localUri)
            assertEquals("Y proof path is replace (processed)", "file:///y-replace.mp4.processed", rowsBySubject["goat-y"]?.localUri)
        } finally {
            db.close()
        }
    }

    // ============================================================================
    // REGRESSION TEST (c): Offline-fail → retry → process-death-recovery → exactly one
    // ============================================================================
    // MOB-003 Proof-flow-integration: Offline failure must not leak duplicate outbox items
    // across recovery. When a capture's outbox item fails (FAILED status), retrying it should
    // succeed without duplicating the proof row or outbox item.
    @Test
    fun `offline failure retry and process death recovery produces exactly one outbox item`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val proofs = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = false,
                mediaProcessor = IdentityProofMediaProcessor(),
            )

            val slot = EvidenceSlot(
                identity = ProofIdentity(
                    flow = ProofFlow.VACCINATION,
                    taskId = "vacc-task-1",
                    partitionKey = "whole",
                ),
                fieldKey = "vaccination_video",
            )

            // First capture: succeeds, creates outbox item
            val firstCapture = proofs.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.GOAT,
                subjectId = "goat-1",
                localUri = "file:///proof1.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "goat",
                scopeId = "goat-1",
                capturedStartMs = 1_000L,
                capturedEndMs = 2_000L,
                capturedByPrincipalId = null,
                proofPolicy = ProofPolicy.Default,
                awaitUploadEnqueue = true,
            )
            assertTrue("First capture succeeds", firstCapture is AppResult.Ok)
            val proofId = (firstCapture as AppResult.Ok).value.id

            // Verify one outbox item was enqueued
            assertEquals("One outbox item after first capture", 1, sync.enqueueCalls.size)
            val outboxItemId = sync.enqueueCalls[0].outboxItemId

            // Simulate offline failure: drive the outbox item to FAILED state
            sync.seedConflict(outboxItemId, "network timeout")
            advanceUntilIdle()

            // Retry the upload. A manual retry of an already-registered proof re-arms the SAME
            // outbox item (SyncRepository.retry -> OutboxDao.markRetryReady): it does not mint a
            // new outbox row via enqueueProofUpload, which is reserved for a fresh registration.
            val retryResult = proofs.retryUpload("vacc-task-1", proofId)
            assertTrue("Retry succeeds", retryResult is AppResult.Ok)
            advanceUntilIdle()

            assertEquals("Retry re-arms the existing outbox item", listOf(outboxItemId), sync.retryCalls)
            assertEquals("Retry does not mint a second outbox item", 1, sync.enqueueCalls.size)

            // Simulate process death: create a NEW repository instance over the SAME Room database
            val recoveredProofs = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = true,  // Recovery path runs reconciliation
                mediaProcessor = IdentityProofMediaProcessor(),
            )
            advanceUntilIdle()

            // After recovery, there must be EXACTLY ONE proof row for this proof
            val recoveredRows = recoveredProofs.observeProofs("vacc-task-1").first()
            assertEquals("Exactly one proof row after recovery", 1, recoveredRows.size)
            assertEquals("Same proof ID survives recovery", proofId, recoveredRows[0].id)

            // Verify exactly one outbox item (recovery must not duplicate)
            val expectedIdempotencyKey = "proof-upload:vacc-task-1:${recoveredRows[0].id}"
            val finalEnqueueCalls = sync.enqueueCalls.filter { it.idempotencyKey == expectedIdempotencyKey }
            assertEquals("Exactly one active outbox entry per proof", 1, finalEnqueueCalls.size)
        } finally {
            db.close()
        }
    }

    // INVARIANT regression (field bug 2026-08-15, third occurrence site): Gate 3 backstop
    // validation must be mime-aware — a real JPEG through capture() with the PRODUCTION
    // FileSystemProofArtifactValidator must succeed. Before the fix, the gate ran the video
    // duration probe on photos; OEMs whose probe reports duration=0 for images rejected every
    // valid photo at insert time.
    @Test
    fun `real jpeg passes gate3 backstop validation with production validator`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val proofs = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = false,
                proofArtifactValidator = FileSystemProofArtifactValidator(),
            )
            val jpeg = File.createTempFile("proof-photo", ".jpg")
            try {
                val bitmap = android.graphics.Bitmap.createBitmap(64, 48, android.graphics.Bitmap.Config.ARGB_8888)
                jpeg.outputStream().use { bitmap.compress(android.graphics.Bitmap.CompressFormat.JPEG, 88, it) }
                bitmap.recycle()
                val result = proofs.captureReplacingLatest(
                    slot = EvidenceSlot(
                        identity = ProofIdentity(
                            flow = ProofFlow.VACCINATION,
                            taskId = "photo-task-1",
                            partitionKey = "whole",
                        ),
                        fieldKey = "feed_distribution_feed_weight_photo",
                    ),
                    subject = ProofSubject.GOAT,
                    subjectId = "goat-photo",
                    localUri = jpeg.toURI().toString(),
                    mimeType = "image/jpeg",
                    caption = null,
                    scopeType = "goat",
                    scopeId = "goat-photo",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 2_000L,
                    capturedByPrincipalId = null,
                    proofPolicy = ProofPolicy.Default,
                    awaitUploadEnqueue = false,
                )
                assertTrue(
                    "Real JPEG must pass gate 3: ${(result as? AppResult.Err)?.message}",
                    result is AppResult.Ok,
                )
            } finally {
                jpeg.delete()
            }
        } finally {
            db.close()
        }
    }

    // ============================================================================
    // REGRESSION TEST (d): Malformed-file validation with REAL files
    // ============================================================================
    // MOB-003 Proof-flow-integration: Zero-byte files must be rejected unconditionally.
    // Truncated/garbage files are validated through the SAME [MediaMetadataRetriever] probe
    // FileSystemProofArtifactValidator uses in production -- under Robolectric's shadow retriever,
    // setDataSource() on 4KB of non-MP4 bytes does NOT throw (it never really parses the
    // container), so the probe "succeeds" with a null/zero duration. That is the OTHER definitive
    // rejection branch the validator documents ("probe succeeded but duration invalid" -- not
    // plausible-accept, which requires the probe to actually throw). This still proves a malformed
    // file is rejected end-to-end; it exercises the succeeded-with-bad-metadata path rather than
    // the threw-on-probe path, because that is the path this JVM test environment can reach
    // deterministically.
    @Test
    fun `zero byte file is rejected and truncated file with unreadable metadata is rejected`() = runTest {
        val db = newDb()
        try {
            val sync = FakeSyncRepository()
            val validator = FileSystemProofArtifactValidator()
            val proofs = DefaultProofCaptureRepository(
                dao = db.proofCaptureDao(),
                syncRepository = sync,
                appScope = backgroundScope,
                dispatchers = unconfinedDispatchers,
                reconcileOnStartup = false,
                proofArtifactValidator = validator,
            )

            val slot = EvidenceSlot(
                identity = ProofIdentity(
                    flow = ProofFlow.VACCINATION,
                    taskId = "vacc-task-2",
                    partitionKey = "whole",
                ),
                fieldKey = "vaccination_video",
            )

            // Test zero-byte file: must be rejected
            val tempZeroFile = File.createTempFile("proof-zero", ".mp4")
            try {
                val zeroCapture = proofs.captureReplacingLatest(
                    slot = slot,
                    subject = ProofSubject.GOAT,
                    subjectId = "goat-2",
                    localUri = "file://${tempZeroFile.absolutePath}",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "goat",
                    scopeId = "goat-2",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 2_000L,
                    capturedByPrincipalId = null,
                    proofPolicy = ProofPolicy.Default,
                    awaitUploadEnqueue = false,
                )
                assertTrue(
                    "Zero-byte file capture is rejected: ${(zeroCapture as? AppResult.Err)?.message}",
                    zeroCapture is AppResult.Err,
                )
            } finally {
                tempZeroFile.delete()
            }

            // Test truncated file (4KB garbage, no valid MP4 header, over the 1KB size floor):
            // Robolectric's shadow retriever succeeds on setDataSource() without ever really
            // parsing it, then reports no duration -- the validator's "probe succeeded but
            // duration invalid" branch rejects it, same as production would for any file whose
            // container metadata reads back empty.
            val tempGarbageFile = File.createTempFile("proof-garbage", ".mp4")
            try {
                tempGarbageFile.writeBytes(ByteArray(4096) { it.toByte() })  // 4KB garbage
                val garbageCapture = proofs.captureReplacingLatest(
                    slot = slot,
                    subject = ProofSubject.GOAT,
                    subjectId = "goat-2",
                    localUri = "file://${tempGarbageFile.absolutePath}",
                    mimeType = "video/mp4",
                    caption = null,
                    scopeType = "goat",
                    scopeId = "goat-2",
                    capturedStartMs = 1_000L,
                    capturedEndMs = 2_000L,
                    capturedByPrincipalId = null,
                    proofPolicy = ProofPolicy.Default,
                    awaitUploadEnqueue = true,
                )
                assertTrue(
                    "Truncated file with unreadable metadata is rejected: " +
                        (garbageCapture as? AppResult.Ok)?.value,
                    garbageCapture is AppResult.Err,
                )
                assertEquals(
                    "Rejection names the unreadable duration",
                    "Recording has no valid duration.",
                    (garbageCapture as AppResult.Err).message,
                )
                // Verify it was never enqueued -- validation runs before the outbox write
                assertEquals("Rejected garbage file produces no enqueue call", 0, sync.enqueueCalls.size)
            } finally {
                tempGarbageFile.delete()
            }
        } finally {
            db.close()
        }
    }
}

/** Test helper: drives the outbox item behind [proofId] straight to SUCCEEDED with
 *  [serverProofId], mirroring [SyncEngine]'s real IN_FLIGHT -> SUCCEEDED transition — used by the
 *  captureReplacingLatest deferred-retirement tests (P1 fix), which must observe the NEW row
 *  actually reach SYNCED before the OLD occupant(s) are retired. */
private suspend fun FakeSyncRepository.completeUpload(db: GoatDatabase, proofId: String, serverProofId: String) {
    val outboxItemId = requireNotNull(db.proofCaptureDao().findById(proofId)?.outboxItemId) {
        "proof $proofId has no outboxItemId yet"
    }
    emit(
        outboxItemId,
        SyncItemStatus.SUCCEEDED,
        resultJson = syncJson.encodeToString(ProofUploadResponseDto(proof = ProofReferenceDto(proofId = serverProofId))),
    )
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

private class RecordingProofMediaProcessor(
    private val result: ProofMediaProcessingResult,
) : ProofMediaProcessor {
    val requests = mutableListOf<ProofMediaProcessingRequest>()

    override suspend fun process(request: ProofMediaProcessingRequest): ProofMediaProcessingResult {
        requests += request
        return result
    }
}

private class ThrowingProofMediaProcessor : ProofMediaProcessor {
    override suspend fun process(request: ProofMediaProcessingRequest): ProofMediaProcessingResult =
        error("processor failed")
}

/** Fails the first [failFirst] invocations, then returns [successResult] — models a processor
 *  that recovers (device storage freed, transient decoder issue resolved) so a test can drive
 *  the retryUpload() "retry processing" path (P1 fix) end to end. */
private class SometimesFailingProofMediaProcessor(
    private val failFirst: Int,
    private val successResult: ProofMediaProcessingResult,
) : ProofMediaProcessor {
    private var invocations = 0

    override suspend fun process(request: ProofMediaProcessingRequest): ProofMediaProcessingResult {
        invocations += 1
        if (invocations <= failFirst) error("processor failed (attempt $invocations)")
        return successResult
    }
}

/** Test default "processing succeeds" double for tests that only care about the row reaching a
 *  registered/enqueued/removable state, not about media-processing behavior itself. Distinct from
 *  [ProofMediaProcessor.Noop] (the production constructor default), which deliberately THROWS to
 *  catch un-wired DI — these tests previously relied on Noop's throw plus the OLD
 *  processing-failure fallback (uploadOriginal=true, auto-enqueue) to reach an enqueued row at
 *  all. That fallback no longer exists (P1 fix: a processing failure now leaves the row
 *  PROCESSING_FAILED_AWAITING_RETRY and enqueues nothing), so tests exercising unrelated behavior
 *  (remove, retryUpload, reconcile, replace ordering) need an explicit processor that actually
 *  succeeds. The output URI is deterministically derived from the original so assertions can
 *  compute the expected processed path without a processor-per-test convention. */
private class IdentityProofMediaProcessor : ProofMediaProcessor {
    override suspend fun process(request: ProofMediaProcessingRequest): ProofMediaProcessingResult =
        ProofMediaProcessingResult(
            outputUri = "${request.originalUri}.processed",
            outputMimeType = request.mimeType,
            originalBytes = 2_000_000L,
            processedBytes = 1_000_000L,
        )
}

private class RecordingGalleryProofSaver : GalleryProofSaver {
    val paths = mutableListOf<String>()

    override suspend fun saveProofCopy(localFilePath: String, request: ProofUploadRequestDto, idempotencyKey: String) {
        paths += localFilePath
    }
}

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
    data class EnqueueCall(
        val idempotencyKey: String,
        val outboxItemId: String,
        val request: ProofUploadRequestDto,
        val localFilePath: String,
    )
    data class ScanCall(val idempotencyKey: String, val partitionKey: String, val request: ScanCaptureRequestDto)
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
        partitionKey: String,
        request: ScanCaptureRequestDto,
    ): AppResult<String> {
        scanCalls += ScanCall(idempotencyKey, partitionKey, request)
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
        enqueueCalls += EnqueueCall(idempotencyKey, id, request, localFilePath)
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
        // Mirrors OutboxDao.markRetryReady: a manual retry only re-arms a terminal FAILED row,
        // resetting it to QUEUED and clearing conflict/lastError, so a subsequent followOutboxItem
        // collection does not immediately replay the same conflict back onto the proof row.
        status.value = status.value.copy(
            items = status.value.items.map { item ->
                if (item.id == itemId && item.status == SyncItemStatus.FAILED) {
                    item.copy(status = SyncItemStatus.QUEUED, conflict = false, lastError = null)
                } else {
                    item
                }
            },
        )
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
    override fun observeForTaskPartition(taskId: String, partitionKey: String, limit: Int): Flow<List<ProofCaptureEntity>> =
        delegate.observeForTaskPartition(taskId, partitionKey, limit)
    override suspend fun listForTask(taskId: String, limit: Int): List<ProofCaptureEntity> =
        delegate.listForTask(taskId, limit)
    override suspend fun listForTaskCleanupPage(taskId: String, afterCapturedAtMs: Long, afterId: String, limit: Int): List<ProofCaptureEntity> =
        delegate.listForTaskCleanupPage(taskId, afterCapturedAtMs, afterId, limit)
    override suspend fun listRecoverableUploadsPage(capturedBeforeMs: Long, afterCapturedAtMs: Long, afterId: String, limit: Int): List<ProofCaptureEntity> =
        delegate.listRecoverableUploadsPage(capturedBeforeMs, afterCapturedAtMs, afterId, limit)
    override suspend fun activeCountForSubject(taskId: String, partitionKey: String, subjectId: String): Int =
        delegate.activeCountForSubject(taskId, partitionKey, subjectId)
    override suspend fun activeCountForSubjectType(taskId: String, partitionKey: String, proofSubject: String): Int =
        delegate.activeCountForSubjectType(taskId, partitionKey, proofSubject)
    override suspend fun activeCountForField(taskId: String, partitionKey: String, fieldKey: String): Int =
        delegate.activeCountForField(taskId, partitionKey, fieldKey)
    override suspend fun findById(id: String): ProofCaptureEntity? = delegate.findById(id)
    override suspend fun setOutboxItemId(id: String, outboxItemId: String?) = delegate.setOutboxItemId(id, outboxItemId)
    override suspend fun updateStatus(id: String, status: String, serverProofId: String?, lastError: String?) {
        updateStatusCalls++
        delegate.updateStatus(id, status, serverProofId, lastError)
    }
    override suspend fun updateProcessingState(
        id: String,
        processingState: String,
        attempt: Int,
        processingAttempted: Boolean,
        uploadOriginal: Boolean,
        lastErrorStage: String?,
        lastErrorClass: String?,
        lastErrorRetryable: Boolean?,
        lastErrorMessageHash: String?,
        updatedAtMs: Long,
    ) = delegate.updateProcessingState(
        id = id,
        processingState = processingState,
        attempt = attempt,
        processingAttempted = processingAttempted,
        uploadOriginal = uploadOriginal,
        lastErrorStage = lastErrorStage,
        lastErrorClass = lastErrorClass,
        lastErrorRetryable = lastErrorRetryable,
        lastErrorMessageHash = lastErrorMessageHash,
        updatedAtMs = updatedAtMs,
    )
    override suspend fun updateProcessingArtifact(
        id: String,
        localUri: String,
        mimeType: String,
        processingState: String,
        processingAttempted: Boolean,
        uploadOriginal: Boolean,
        processedUri: String?,
        originalBytes: Long?,
        processedBytes: Long?,
        inputWidth: Int?,
        inputHeight: Int?,
        targetVideoBitrate: Int?,
        targetAudioBitrate: Int?,
        updatedAtMs: Long,
    ) = delegate.updateProcessingArtifact(
        id = id,
        localUri = localUri,
        mimeType = mimeType,
        processingState = processingState,
        processingAttempted = processingAttempted,
        uploadOriginal = uploadOriginal,
        processedUri = processedUri,
        originalBytes = originalBytes,
        processedBytes = processedBytes,
        inputWidth = inputWidth,
        inputHeight = inputHeight,
        targetVideoBitrate = targetVideoBitrate,
        targetAudioBitrate = targetAudioBitrate,
        updatedAtMs = updatedAtMs,
    )
    override suspend fun markGallerySaved(id: String, gallerySavedUri: String, updatedAtMs: Long) =
        delegate.markGallerySaved(id, gallerySavedUri, updatedAtMs)
    override suspend fun insertStateEvent(entity: ProofCaptureStateEventEntity) =
        delegate.insertStateEvent(entity)
    override suspend fun countStateEvents(proofId: String, stage: String): Int =
        delegate.countStateEvents(proofId, stage)
    override suspend fun delete(id: String, taskId: String) = delegate.delete(id, taskId)
    override suspend fun updateCaption(id: String, taskId: String, caption: String) = delegate.updateCaption(id, taskId, caption)
    override suspend fun clearForTask(taskId: String) = delegate.clearForTask(taskId)
    override suspend fun clearAll() = delegate.clearAll()
}
