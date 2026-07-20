package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsEvidenceRefDto
import sg.mesha.goatos.core.network.dto.CountsGoatLifecycleResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventResponseDto
import java.io.IOException

/**
 * The must-not-double-submit contract for the three Counts writes.
 *
 * Birth, death, and shifting are the writes where a duplicate is not a cosmetic bug: it invents an
 * animal, exits one twice, or double-counts a movement. These tests pin the two behaviours that
 * prevent it end to end through the REAL outbox (no stubbed repository):
 *  1. the stored idempotency key is passed to the backend VERBATIM on every retry — never
 *     regenerated, which is what lets a server-committed-but-client-unrecorded attempt dedupe;
 *  2. re-enqueuing the SAME key with the SAME payload is a no-op that returns the ORIGINAL row,
 *     while the same key with a DIFFERENT payload is refused rather than silently hiding a
 *     changed write behind the older queued one.
 */
class CountsOutboxWriteTest {

    private val unconfinedDispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    /** Captures the header key each Counts route was called with, and can fail on demand. */
    private class CountsApi(delegate: AppApi = FakeAppApi()) : AppApi by delegate {
        val shiftingKeys = mutableListOf<String>()
        val birthKeys = mutableListOf<String>()
        val deathKeys = mutableListOf<String>()
        var failuresRemaining = 0

        override suspend fun recordCountsShiftingEvent(
            idempotencyKey: String,
            request: CountsShiftingEventRequestDto,
        ): CountsShiftingEventResponseDto {
            shiftingKeys += idempotencyKey
            if (failuresRemaining > 0) {
                failuresRemaining--
                throw IOException("network down")
            }
            return CountsShiftingEventResponseDto(shiftingEventId = "shift-1")
        }

        override suspend fun recordCountsBirthEvent(
            idempotencyKey: String,
            request: CountsBirthEventRequestDto,
        ): CountsGoatLifecycleResponseDto {
            birthKeys += idempotencyKey
            return CountsGoatLifecycleResponseDto()
        }

        override suspend fun recordCountsDeathEvent(
            idempotencyKey: String,
            request: CountsDeathEventRequestDto,
        ): CountsGoatLifecycleResponseDto {
            deathKeys += idempotencyKey
            return CountsGoatLifecycleResponseDto()
        }
    }

    private fun repository(api: AppApi, online: Boolean = true): DefaultSyncRepository {
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = api,
            connectivityGate = { online },
            dispatchers = unconfinedDispatchers,
            clock = { 0L },
        )
        return DefaultSyncRepository(
            store = store,
            engine = engine,
            connectivityGate = { online },
            appScope = CoroutineScope(Dispatchers.Unconfined),
            dispatchers = unconfinedDispatchers,
            clock = { 0L },
        )
    }

    private fun shiftingRequest(goatId: String = "goat-1") = CountsShiftingEventRequestDto(
        destinationParkId = "park-1",
        destinationShedId = "shed-9",
        priority = "low",
        category = "growth",
        goatIds = listOf(goatId),
    )

    private fun birthRequest() = CountsBirthEventRequestDto(
        animalIdentifier1 = "TAG-1",
        species = "goat",
        sex = "female",
        dob = "2026-07-01",
        entryDate = "2026-07-01",
        evidenceRefs = listOf(CountsEvidenceRefDto(evidenceId = "counts-birth-death:draft-1")),
    )

    private fun deathRequest() = CountsDeathEventRequestDto(
        goatId = "goat-1",
        reason = "found dead in shed",
        evidenceRefs = listOf(CountsEvidenceRefDto(evidenceId = "counts-birth-death:draft-2")),
        rowVersion = 3,
    )

    @Test
    fun `shifting reuses the same idempotency key across retries`() = runBlocking {
        val api = CountsApi()
        // First attempt fails at the transport layer; the row backs off and is retried.
        api.failuresRemaining = 1
        val repo = repository(api)

        val enqueued = repo.enqueueCountsShifting(
            groupKey = "shed-9",
            idempotencyKey = "counts-shifting:draft-1",
            request = shiftingRequest(),
        )
        assertTrue(enqueued is AppResult.Ok)
        repo.retry((enqueued as AppResult.Ok).value)

        assertEquals(2, api.shiftingKeys.size)
        // The whole point: attempt 2 carries the SAME key, so the backend can recognise it as a
        // replay of the first movement instead of recording a second one.
        assertEquals("counts-shifting:draft-1", api.shiftingKeys[0])
        assertEquals(api.shiftingKeys[0], api.shiftingKeys[1])
    }

    @Test
    fun `re-enqueuing an identical write returns the original row instead of duplicating`() = runBlocking {
        val api = CountsApi()
        // Offline: the row stays queued so a second enqueue hits the idempotency guard rather
        // than a already-drained row.
        val repo = repository(api, online = false)

        val first = repo.enqueueCountsBirth("TAG-1", "counts-birth-death:draft-1", birthRequest())
        val second = repo.enqueueCountsBirth("TAG-1", "counts-birth-death:draft-1", birthRequest())

        assertTrue(first is AppResult.Ok)
        assertTrue(second is AppResult.Ok)
        assertEquals((first as AppResult.Ok).value, (second as AppResult.Ok).value)
        assertEquals("exactly one queued birth", 1, repo.observeStatus().value.items.size)
    }

    @Test
    fun `same key with a changed payload is refused, never silently collapsed`() = runBlocking {
        val api = CountsApi()
        val repo = repository(api, online = false)

        val first = repo.enqueueCountsShifting("shed-9", "counts-shifting:draft-1", shiftingRequest(goatId = "goat-1"))
        // A genuinely different movement (a DIFFERENT ANIMAL) must not be swallowed by the older
        // queued row just because it reused the key — relocating the wrong goat is the exact
        // failure the fingerprint check exists to prevent.
        val changed = repo.enqueueCountsShifting("shed-9", "counts-shifting:draft-1", shiftingRequest(goatId = "goat-2"))

        assertTrue(first is AppResult.Ok)
        assertTrue("a changed payload under a used key must be rejected", changed is AppResult.Err)
        assertEquals("the original write is untouched", 1, repo.observeStatus().value.items.size)
    }

    @Test
    fun `each write dispatches to its own route with its own key`() = runBlocking {
        val api = CountsApi()
        val repo = repository(api)

        repo.enqueueCountsBirth("TAG-1", "counts-birth-death:birth-1", birthRequest())
        repo.enqueueCountsDeath("goat-1", "counts-birth-death:death-1", deathRequest())
        repo.enqueueCountsShifting("shed-9", "counts-shifting:move-1", shiftingRequest())

        assertEquals(listOf("counts-birth-death:birth-1"), api.birthKeys)
        assertEquals(listOf("counts-birth-death:death-1"), api.deathKeys)
        assertEquals(listOf("counts-shifting:move-1"), api.shiftingKeys)
        // Distinct keys per logical write: a birth and a death recorded in the same session must
        // never share an identity.
        assertNotEquals(api.birthKeys.first(), api.deathKeys.first())
    }

    @Test
    fun `a death always carries the dead plus died guardrail pairing`() {
        // The pairing is enforced server-side; this pins that the client's DTO defaults cannot
        // drift away from it, so a caller can never construct a weaker death request.
        val request = deathRequest()
        assertEquals(CountsDeathEventRequestDto.DEATH_LIFECYCLE_STATUS, request.lifecycleStatus)
        assertEquals(CountsDeathEventRequestDto.DEATH_EXIT_REASON, request.exitReason)
        assertEquals("dead", request.lifecycleStatus)
        assertEquals("died", request.exitReason)
    }
}
