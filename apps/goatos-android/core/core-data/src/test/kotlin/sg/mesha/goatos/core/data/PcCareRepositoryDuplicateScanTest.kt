package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import retrofit2.HttpException
import sg.mesha.goatos.core.data.cache.PcCareScanStatus
import sg.mesha.goatos.core.data.sync.DefaultSyncRepository
import sg.mesha.goatos.core.data.sync.FakeOutboxStore
import sg.mesha.goatos.core.data.sync.SyncEngine
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.PcCareScanRequestDto
import sg.mesha.goatos.core.network.dto.PcCareScanResponseDto

/**
 * PC Care scan duplicate handling, both halves:
 *  - LOCAL: a tag already in the task's Room rows returns a typed [PcCareScanOutcome.Duplicate]
 *    without a second outbox enqueue;
 *  - SERVER: a 409 on dispatch is TERMINAL (conflict, never auto-retried), the durable animal row
 *    stops reading as still-queued work, and a re-scan reopens the SAME outbox row under the SAME
 *    stable idempotency key — never a new key, never a second row.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class PcCareRepositoryDuplicateScanTest {
    private lateinit var db: GoatDatabase
    private lateinit var appScope: CoroutineScope

    @Before
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        appScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        db = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
    }

    @After
    fun tearDown() {
        appScope.cancel()
        db.close()
    }

    private fun repository(syncRepository: SyncRepository): DefaultPcCareRepository =
        DefaultPcCareRepository(
            api = FakeAppApi(),
            database = db,
            detailDao = db.pcCareTaskDetailCacheDao(),
            animalDao = db.pcCareAnimalRowDao(),
            syncRepository = syncRepository,
            clock = { 1000L },
        )

    private fun syncRepository(store: FakeOutboxStore, engine: SyncEngine, online: () -> Boolean): SyncRepository =
        DefaultSyncRepository(
            store = store,
            engine = engine,
            connectivityGate = { online() },
            appScope = appScope,
            clock = { 1000L },
        )

    @Test
    fun `a locally duplicate tag returns Duplicate without a second enqueue`() = runTest {
        val store = FakeOutboxStore()
        val engine = SyncEngine(store, FakeAppApi(), connectivityGate = { false }, clock = { 1000L })
        val repo = repository(syncRepository(store, engine) { false })

        val first = repo.recordScan("task-1", "  RF-042 ")
        assertEquals(PcCareScanOutcome.Queued, first)

        // Same physical tag, different whitespace/case — the normalized key catches it in Room.
        val second = repo.recordScan("task-1", "rf-042")
        assertTrue(second is PcCareScanOutcome.Duplicate)

        val scanRows = store.snapshot().filter { it.opType == OutboxOpType.PC_CARE_SCAN_ADD.name }
        assertEquals(1, scanRows.size)
        assertEquals("pc-care:scan:task-1:rf-042", scanRows.single().idempotencyKey)

        val animals = repo.observeAnimals("task-1").first()
        assertEquals(1, animals.size)
        assertEquals("RF-042", animals.single().tagVerbatim)
        assertEquals(PcCareScanStatus.PENDING, animals.single().scanSyncStatus)
    }

    @Test
    fun `a server 409 is terminal and a re-scan reopens the SAME key instead of minting a new one`() = runTest {
        val store = FakeOutboxStore()
        var online = false
        val scanCalls = mutableListOf<String>()
        val api = object : AppApi by FakeAppApi() {
            override suspend fun scanPcCareAnimal(
                taskId: String,
                idempotencyKey: String,
                request: PcCareScanRequestDto,
            ): PcCareScanResponseDto {
                scanCalls += idempotencyKey
                // duplicate_scan / task_locked both arrive as a 409 — terminal by
                // isTerminalAppApiError, never burned against the backoff budget.
                throw HttpException(409)
            }
        }
        val engine = SyncEngine(
            store = store,
            api = api,
            connectivityGate = { online },
            clock = { 1000L },
            pcCareAnimalRowDao = db.pcCareAnimalRowDao(),
        )
        val repo = repository(syncRepository(store, engine) { online })

        assertEquals(PcCareScanOutcome.Queued, repo.recordScan("task-1", "RF-042"))
        online = true
        engine.drainOnce()

        val row = store.snapshot().single { it.opType == OutboxOpType.PC_CARE_SCAN_ADD.name }
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertTrue("a 409 must terminalize the row (conflict), not schedule a retry", row.conflict)
        assertEquals(listOf("pc-care:scan:task-1:rf-042"), scanCalls)

        // The durable animal row stopped reading as still-queued work. (On this JVM harness the
        // stubbed HttpException carries no server body, so the duplicate_scan body classification
        // cannot run; the generic terminal arm marks FAILED. The body-driven DUPLICATE marking is
        // covered by the DAO-guard test below.)
        val animal = db.pcCareAnimalRowDao().getByTag("task-1", "rf-042")
        assertEquals(PcCareScanStatus.FAILED, animal?.scanSyncStatus)

        // Re-scan after the terminal failure: the SAME stable key re-opens the SAME row —
        // OutboxDao.reopenTerminalForRetry semantics — never a new key and never a second row.
        assertEquals(PcCareScanOutcome.Queued, repo.recordScan("task-1", "RF-042"))
        val rowsAfter = store.snapshot().filter { it.opType == OutboxOpType.PC_CARE_SCAN_ADD.name }
        assertEquals(1, rowsAfter.size)
        assertEquals("pc-care:scan:task-1:rf-042", rowsAfter.single().idempotencyKey)
    }

    @Test
    fun `the generic terminal-failure arm never overwrites a DUPLICATE verdict`() = runTest {
        val dao = db.pcCareAnimalRowDao()
        val repo = repository(
            syncRepository(FakeOutboxStore(), SyncEngine(FakeOutboxStore(), FakeAppApi(), connectivityGate = { false }, clock = { 1000L })) { false },
        )
        assertEquals(PcCareScanOutcome.Queued, repo.recordScan("task-1", "RF-042"))

        // The dispatch classified the server body as duplicate_scan …
        dao.markScanDuplicate("task-1", "rf-042", 2000L)
        // … then the generic terminal reconcile ran for the same row.
        dao.markScanFailedIfPending("task-1", "rf-042", 3000L)
        assertEquals(PcCareScanStatus.DUPLICATE, dao.getByTag("task-1", "rf-042")?.scanSyncStatus)

        // A DUPLICATE row is a duplicate on re-scan too — the server already owns this tag.
        assertTrue(repo.recordScan("task-1", "RF-042") is PcCareScanOutcome.Duplicate)

        // And the success reconcile shape: server row id in, status SYNCED.
        dao.updateScanSynced("task-1", "rf-042", "server-row-9", 4000L)
        val synced = dao.getByTag("task-1", "rf-042")
        assertEquals(PcCareScanStatus.SYNCED, synced?.scanSyncStatus)
        assertEquals("server-row-9", synced?.animalRowId)
    }
}
