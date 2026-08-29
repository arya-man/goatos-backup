package sg.mesha.goatos.core.data

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.take
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.launch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.clock.ClockPunchDirection
import sg.mesha.goatos.core.common.clock.MockLocationVerdict
import sg.mesha.goatos.core.common.clock.MockProviderApp
import sg.mesha.goatos.core.common.clock.clockPunchGroupKey
import sg.mesha.goatos.core.common.clock.clockPunchIdempotencyKey
import sg.mesha.goatos.core.data.cache.ClockBlobCacheDao
import sg.mesha.goatos.core.data.cache.ClockBlobCacheEntity
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.ClockLocationDto
import sg.mesha.goatos.core.network.dto.ClockPunchRequestDto
import java.time.OffsetDateTime

/**
 * Clock punch decision + idempotency-key contract (module clock, maintainer decision 2026-08-27
 * — docs/features/clock-in-out/plan.md §4.1/§4.2).
 *
 * The two halves the brief pins:
 *  1. the PURE mock-location verdict — a mock fix or ANY installed fake-GPS app blocks the
 *     punch; developer options alone never do;
 *  2. idempotency-key STABILITY — the key is day-scoped and derived, never timestamp-suffixed,
 *     so two taps (or a retry after process death) mint the SAME key, while tomorrow and the
 *     opposite direction mint different ones — and the repository really enqueues under it,
 *     with the day group key, refusing to enqueue at all when the verdict blocks.
 */
class ClockPunchTest {

    // --- 1. the pure verdict ------------------------------------------------------------------

    @Test
    fun `clean verdict does not block`() {
        assertFalse(MockLocationVerdict.Clean.blocksPunch)
    }

    @Test
    fun `mock fix alone blocks`() {
        val verdict = MockLocationVerdict(mockFix = true, mockApps = emptyList(), developerOptions = false)
        assertTrue(verdict.blocksPunch)
    }

    @Test
    fun `installed fake gps app alone blocks and names the app`() {
        val verdict = MockLocationVerdict(
            mockFix = false,
            mockApps = listOf(MockProviderApp("com.fake.gps", "Fake GPS Location")),
            developerOptions = false,
        )
        assertTrue(verdict.blocksPunch)
        assertEquals(listOf("Fake GPS Location"), verdict.appLabels)
    }

    @Test
    fun `developer options alone never block`() {
        // Blocking on dev options would lock out our own dev/test phones (plan §4.2 item 3).
        val verdict = MockLocationVerdict(mockFix = false, mockApps = emptyList(), developerOptions = true)
        assertFalse(verdict.blocksPunch)
    }

    // --- 2. key stability ---------------------------------------------------------------------

    @Test
    fun `idempotency key is day-scoped and stable, never timestamped`() {
        val first = clockPunchIdempotencyKey("2026-08-28", ClockPunchDirection.IN)
        val second = clockPunchIdempotencyKey("2026-08-28", ClockPunchDirection.IN)
        assertEquals(first, second)
        assertEquals("clock:2026-08-28:in", first)
        assertEquals("clock:2026-08-28:out", clockPunchIdempotencyKey("2026-08-28", ClockPunchDirection.OUT))
        // A new day is a genuinely new act under a new key.
        assertEquals("clock:2026-08-29:in", clockPunchIdempotencyKey("2026-08-29", ClockPunchDirection.IN))
        assertEquals("clock:2026-08-28", clockPunchGroupKey("2026-08-28"))
    }

    @Test
    fun `repository enqueues under the stable key and day group across repeated taps`() = runTest {
        val sync = RecordingSyncRepository()
        val repo = repository(sync, verdict = MockLocationVerdict.Clean)

        val first = repo.punch(ClockPunchDirection.IN)
        val second = repo.punch(ClockPunchDirection.IN)

        assertTrue(first is ClockPunchOutcome.Enqueued)
        assertTrue(second is ClockPunchOutcome.Enqueued)
        assertEquals(2, sync.calls.size)
        // Same tap, same day → the SAME key both times (a retry replays, never punches twice).
        assertEquals("clock:2026-08-28:in", sync.calls[0].idempotencyKey)
        assertEquals(sync.calls[0].idempotencyKey, sync.calls[1].idempotencyKey)
        assertEquals("clock:2026-08-28", sync.calls[0].groupKey)
        // The body's own idempotency_key mirrors the outbox key (contract: body wins).
        assertEquals("clock:2026-08-28:in", sync.calls[0].request.idempotencyKey)
        assertTrue(sync.calls[0].clockIn)
    }

    @Test
    fun `the IST day scopes the key even when the device offset is not IST`() = runTest {
        val sync = RecordingSyncRepository()
        // 2026-08-28 21:00 UTC = 2026-08-29 02:30 IST → the IST day is the 29th.
        val repo = repository(
            sync,
            verdict = MockLocationVerdict.Clean,
            now = { OffsetDateTime.parse("2026-08-28T21:00:00Z") },
        )
        repo.punch(ClockPunchDirection.OUT)
        assertEquals("clock:2026-08-29:out", sync.calls.single().idempotencyKey)
    }

    @Test
    fun `a blocking verdict refuses the punch before anything is enqueued`() = runTest {
        val sync = RecordingSyncRepository()
        val repo = repository(
            sync,
            verdict = MockLocationVerdict(
                mockFix = false,
                mockApps = listOf(MockProviderApp("com.fake.gps", "Fake GPS Location")),
                developerOptions = false,
            ),
        )
        val outcome = repo.punch(ClockPunchDirection.IN)
        assertTrue(outcome is ClockPunchOutcome.Blocked)
        assertEquals(0, sync.calls.size)
    }

    // Location is MANDATORY (maintainer decision 2026-08-29): a punch without a real
    // coordinate-bearing fix never queues — the server would refuse it 422 anyway, so
    // enqueuing would only dead-letter a punch that can never land.
    @Test
    fun `a punch without a real location fix never enqueues`() = runTest {
        val locationless = listOf(
            ClockLocationDto(status = "permission_missing") to true,
            ClockLocationDto(status = "unavailable") to false,
            // A "captured" claim with no coordinates is a fix nobody has.
            ClockLocationDto(status = "captured") to false,
        )
        for ((location, wantPermissionMissing) in locationless) {
            val sync = RecordingSyncRepository()
            val repo = repository(sync, MockLocationVerdict.Clean, location = location)
            val outcome = repo.punch(ClockPunchDirection.IN)
            assertTrue("location $location must refuse", outcome is ClockPunchOutcome.NoLocation)
            assertEquals(wantPermissionMissing, (outcome as ClockPunchOutcome.NoLocation).permissionMissing)
            assertEquals(0, sync.calls.size)
        }
    }

    @Test
    fun `pending punch from previous IST day remains visible after midnight`() = runTest {
        val repo = repository(
            RecordingSyncRepository(),
            MockLocationVerdict.Clean,
            now = { OffsetDateTime.parse("2026-08-29T00:05:00+05:30") },
            activePunchGroups = { opType ->
                flowOf(
                    if (opType == "CLOCK_IN") {
                        setOf(clockPunchGroupKey("2026-08-28"))
                    } else {
                        emptySet()
                    },
                )
            },
        )

        assertEquals("clock_in_old", repo.observePendingPunch().first())
    }

    @Test
    fun `pending punch from current IST day remains blocking`() = runTest {
        val repo = repository(
            RecordingSyncRepository(),
            MockLocationVerdict.Clean,
            now = { OffsetDateTime.parse("2026-08-29T00:05:00+05:30") },
            activePunchGroups = { opType ->
                flowOf(
                    if (opType == "CLOCK_OUT") {
                        setOf(clockPunchGroupKey("2026-08-29"))
                    } else {
                        emptySet()
                    },
                )
            },
        )

        assertEquals("clock_out", repo.observePendingPunch().first())
    }

    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    @Test
    fun `pending punch flips to old when IST day changes without outbox emission`() = runTest {
        var now = OffsetDateTime.parse("2026-08-28T23:59:59.900+05:30")
        val emissions = mutableListOf<String?>()
        val repo = repository(
            RecordingSyncRepository(),
            MockLocationVerdict.Clean,
            now = { now },
            activePunchGroups = { opType ->
                flowOf(
                    if (opType == "CLOCK_IN") {
                        setOf(clockPunchGroupKey("2026-08-28"))
                    } else {
                        emptySet()
                    },
                )
            },
        )

        val job = backgroundScope.launch {
            repo.observePendingPunch().take(2).toList(emissions)
        }
        advanceTimeBy(100)
        now = OffsetDateTime.parse("2026-08-29T00:00:00+05:30")
        advanceTimeBy(1)
        job.join()

        assertEquals(listOf("clock_in", "clock_in_old"), emissions)
    }

    // --- fixtures -----------------------------------------------------------------------------

    private fun repository(
        sync: RecordingSyncRepository,
        verdict: MockLocationVerdict,
        now: () -> OffsetDateTime = { OffsetDateTime.parse("2026-08-28T10:15:00+05:30") },
        location: ClockLocationDto = ClockLocationDto(status = "captured", latitude = 12.9, longitude = 77.5),
        activePunchGroups: (opType: String) -> Flow<Set<String>> = { flowOf(emptySet()) },
    ): DefaultClockRepository = DefaultClockRepository(
        api = FakeAppApi(),
        dao = InMemoryClockDao(),
        syncRepository = sync,
        factsProvider = {
            ClockPunchFacts(
                location = location,
                verdict = verdict,
                batteryPct = 80,
                networkKind = "wifi",
                offline = false,
            )
        },
        activePunchGroups = activePunchGroups,
        now = now,
    )

    private class RecordingSyncRepository : SyncRepository {
        data class Call(
            val clockIn: Boolean,
            val groupKey: String,
            val idempotencyKey: String,
            val request: ClockPunchRequestDto,
        )

        val calls = mutableListOf<Call>()

        override suspend fun enqueueClockPunch(
            clockIn: Boolean,
            groupKey: String,
            idempotencyKey: String,
            request: ClockPunchRequestDto,
        ): AppResult<String> {
            calls += Call(clockIn, groupKey, idempotencyKey, request)
            return AppResult.Ok("outbox-${calls.size}")
        }

        // Abstract members the clock path never touches — stubbed inert.
        override fun observeStatus(): kotlinx.coroutines.flow.StateFlow<sg.mesha.goatos.core.data.sync.SyncStatus> =
            kotlinx.coroutines.flow.MutableStateFlow(sg.mesha.goatos.core.data.sync.SyncStatus.empty(online = true))
        override fun observeItem(itemId: String): Flow<sg.mesha.goatos.core.data.sync.SyncQueueItem?> = flowOf(null)
        override suspend fun enqueueShedSubmit(
            taskId: String,
            groupKey: String,
            idempotencyKey: String,
            request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto,
        ): AppResult<String> = AppResult.Err("unused")
        override suspend fun enqueueReschedule(
            obligationId: String,
            groupKey: String,
            idempotencyKey: String,
            request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto,
        ): AppResult<String> = AppResult.Err("unused")
        override suspend fun enqueueProofUpload(
            groupKey: String,
            idempotencyKey: String,
            request: sg.mesha.goatos.core.network.dto.ProofUploadRequestDto,
            localFilePath: String,
            durationMs: Long?,
        ): AppResult<String> = AppResult.Err("unused")
        override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> =
            AppResult.Err("unused")
        override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> =
            AppResult.Err("unused")
        override suspend fun enqueueVerificationVerdict(
            itemId: String,
            decision: String,
            reason: String?,
            rowVersion: Int,
            measurement: sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto?,
        ): AppResult<String> = AppResult.Err("unused")
        override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Err("unused")
        override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Err("unused")
        override suspend fun triggerDrain() = Unit
    }

    private class InMemoryClockDao : ClockBlobCacheDao {
        private val rows = mutableMapOf<String, ClockBlobCacheEntity>()
        override fun observe(cacheKey: String): Flow<ClockBlobCacheEntity?> = flowOf(rows[cacheKey])
        override suspend fun upsert(entity: ClockBlobCacheEntity) {
            rows[entity.cacheKey] = entity
        }
        override suspend fun delete(cacheKey: String) {
            rows.remove(cacheKey)
        }
        override suspend fun count(): Int = rows.size
        override suspend fun totalBytes(): Long = rows.values.sumOf { it.dtoJson.length.toLong() }
        override suspend fun deleteOldest(n: Int) {
            rows.entries.sortedBy { it.value.updatedAt }.take(n).forEach { rows.remove(it.key) }
        }
    }
}
