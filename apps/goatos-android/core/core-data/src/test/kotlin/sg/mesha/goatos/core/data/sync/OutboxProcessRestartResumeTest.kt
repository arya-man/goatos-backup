package sg.mesha.goatos.core.data.sync

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OUTBOX_MIGRATION_1_2
import sg.mesha.goatos.core.database.outbox.OUTBOX_MIGRATION_2_3
import sg.mesha.goatos.core.database.outbox.OutboxDatabase
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto

/**
 * Proves the MOB-002 background-upload resume contract with a REAL, file-backed Room database
 * (not [FakeOutboxStore]) across a **simulated process restart**: a first [OutboxDatabase]
 * instance persists rows and is closed (mirrors the app process being killed mid-upload — see
 * `docs/mobile/proof-capture-sync-and-e2e.md` §3 "Background sync survives app close"), then a
 * SECOND, entirely new [OutboxDatabase]/[RoomOutboxStore]/[SyncEngine]/[UploadSyncCoordinator]
 * instance is built over the SAME on-disk database file (mirrors WorkManager starting a fresh
 * `SyncWorker` after process death or boot — a new worker and Hilt graph, but the SAME durable
 * `goatos-outbox.db` file).
 *
 * Both a still-QUEUED (PENDING) row AND a row stranded IN_FLIGHT by the "kill" (never reached
 * `markSucceeded`) are still present after the simulated restart and both drain to SUCCEEDED —
 * proving no queued upload is lost, none is silently dropped, and the stranded IN_FLIGHT row is
 * reclaimed rather than stuck forever (the exact case a real app kill during a proof-video
 * upload produces).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class OutboxProcessRestartResumeTest {

    private val dbName = "test-outbox-restart-${System.nanoTime()}.db"

    private fun openDatabase(): OutboxDatabase {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        return Room.databaseBuilder(context, OutboxDatabase::class.java, dbName)
            .addMigrations(OUTBOX_MIGRATION_1_2, OUTBOX_MIGRATION_2_3)
            .allowMainThreadQueries() // JVM unit test — no real UI thread to protect.
            .build()
    }

    @Test
    fun `PENDING and stranded IN_FLIGHT rows survive a simulated process restart and still drain`() = runTest {
        // --- "Before the kill": first process instance persists two durable writes. ---
        val firstProcessDb = openDatabase()
        val firstProcessStore = RoomOutboxStore(firstProcessDb.outboxDao())

        val pendingRow = OutboxEntity(
            id = "row-pending",
            opType = OutboxOpType.PROOF_UPLOAD.name,
            groupKey = "shed-1",
            idempotencyKey = "proof-key-1",
            payloadJson = syncJson.encodeToString(
                ProofUploadPayload(request = ProofUploadRequestDto(scopeType = "shed", scopeId = "shed-1", subjectType = "shed")),
            ),
            status = OutboxStatus.QUEUED.name,
            attemptCount = 0,
            maxAttempts = DEFAULT_MAX_ATTEMPTS,
            conflict = false,
            createdAt = 0L,
            updatedAt = 0L,
            nextAttemptAt = 0L,
            lastError = null,
            resultJson = null,
        )
        val strandedRow = OutboxEntity(
            id = "row-stranded",
            opType = OutboxOpType.SHED_SUBMIT.name,
            groupKey = "shed-2",
            idempotencyKey = "submit-key-1",
            payloadJson = syncJson.encodeToString(
                ShedSubmitPayload(
                    taskId = "task-1",
                    request = SubmitTaskRequestDto(sopVersionId = "sop-1", idempotencyKey = "submit-key-1"),
                ),
            ),
            status = OutboxStatus.QUEUED.name,
            attemptCount = 0,
            maxAttempts = DEFAULT_MAX_ATTEMPTS,
            conflict = false,
            createdAt = 1L,
            updatedAt = 1L,
            nextAttemptAt = 1L,
            lastError = null,
            resultJson = null,
        )
        firstProcessStore.insert(pendingRow)
        firstProcessStore.insert(strandedRow)
        // The upload for `row-stranded` was mid-flight (markInFlight already ran) when the
        // process died — no markSucceeded/markFailed ever landed.
        firstProcessStore.markInFlight("row-stranded", now = 5L)
        assertEquals(OutboxStatus.QUEUED.name, firstProcessStore.findById("row-pending")!!.status)
        assertEquals(OutboxStatus.IN_FLIGHT.name, firstProcessStore.findById("row-stranded")!!.status)

        // Simulate the kill: close this process's Room connection. The .db FILE survives on disk
        // (Robolectric backs Room with the real shadowed SQLite engine — this is not in-memory).
        firstProcessDb.close()

        // --- "After the restart": a brand-new process opens the SAME database file, exactly as
        // WorkManager -> SyncWorker -> a fresh SyncEngine/OutboxStore graph does after Android
        // restarts the process. ---
        val secondProcessDb = openDatabase()
        val secondProcessStore = RoomOutboxStore(secondProcessDb.outboxDao())

        // The durable rows are still there, in their exact pre-kill state — nothing was lost by
        // the "process death", and nothing silently duplicated.
        assertEquals(OutboxStatus.QUEUED.name, secondProcessStore.findById("row-pending")!!.status)
        assertEquals(OutboxStatus.IN_FLIGHT.name, secondProcessStore.findById("row-stranded")!!.status)

        val api = ScriptedAppApi()
        val secondProcessEngine = SyncEngine(secondProcessStore, api, connectivityGate = { true }, clock = { 10L })
        val coordinator = UploadSyncCoordinator(secondProcessEngine, secondProcessStore)

        val outcome = coordinator.run()

        // Both rows — the plain PENDING one and the one reclaimed from a stranded IN_FLIGHT —
        // drained to completion through the NEW instance, over the SAME underlying Room file.
        assertEquals(UploadSyncCoordinator.Outcome.Idle, outcome)
        assertEquals(OutboxStatus.SUCCEEDED.name, secondProcessStore.findById("row-pending")!!.status)
        assertEquals(OutboxStatus.SUCCEEDED.name, secondProcessStore.findById("row-stranded")!!.status)
        assertEquals(1, api.submitCalls.size) // the SHED_SUBMIT row's one dispatch

        secondProcessDb.close()
    }
}
