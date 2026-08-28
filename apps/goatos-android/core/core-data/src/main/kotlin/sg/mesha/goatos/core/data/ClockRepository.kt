package sg.mesha.goatos.core.data

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.firstOrNull
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.clock.ClockPunchDirection
import sg.mesha.goatos.core.common.clock.MockLocationVerdict
import sg.mesha.goatos.core.common.clock.clockPunchGroupKey
import sg.mesha.goatos.core.common.clock.clockPunchIdempotencyKey
import sg.mesha.goatos.core.data.cache.ClockBlobCacheDao
import sg.mesha.goatos.core.data.cache.ClockBlobCacheEntity
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ClockIntegrityDto
import sg.mesha.goatos.core.network.dto.ClockLocationDto
import sg.mesha.goatos.core.network.dto.ClockPersonDayResponseDto
import sg.mesha.goatos.core.network.dto.ClockPresenceResponseDto
import sg.mesha.goatos.core.network.dto.ClockPunchRequestDto
import sg.mesha.goatos.core.network.dto.ClockStatusResponseDto
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * Everything the device honestly tells us at punch time (docs/features/clock-in-out/plan.md §1).
 * Collected fresh INSIDE every punch tap by the app-module [ClockPunchFactsProvider] — location
 * fix (+ its own isMock), installed fake-GPS apps, developer options, battery, network kind.
 */
data class ClockPunchFacts(
    val location: ClockLocationDto,
    val verdict: MockLocationVerdict,
    val batteryPct: Int?,
    /** `wifi` | `cellular` when online; null when offline/unknown. */
    val networkKind: String?,
    /** True when the device has no validated network — the punch queues and drains later. */
    val offline: Boolean,
)

/**
 * Collects [ClockPunchFacts]. Interface lives here (framework-free seam, exactly like
 * [sg.mesha.goatos.core.data.capture.ProofLocationProvider]); the only Android-touching
 * implementation is the app module's `AppClockPunchFactsProvider`.
 */
fun interface ClockPunchFactsProvider {
    suspend fun capture(): ClockPunchFacts
}

/** What a punch tap produced. */
sealed interface ClockPunchOutcome {
    /** The client-side mock-location gate refused the punch; nothing was enqueued. */
    data class Blocked(val verdict: MockLocationVerdict) : ClockPunchOutcome

    /** The punch is DURABLE on the outbox (offline included); [outboxItemId] is observable. */
    data class Enqueued(val outboxItemId: String) : ClockPunchOutcome

    /** The durable enqueue itself failed (a local error, not a server verdict). */
    data class Failed(val message: String) : ClockPunchOutcome
}

/**
 * Clock In / Clock Out repository (module clock, maintainer decision 2026-08-27 —
 * docs/features/clock-in-out/plan.md §4). Offline-first per
 * docs/decisions/android-offline-first.md: the status read the My Clock screen AND the
 * shell-global reminder banner observe is the Room blob cache; refresh upserts it in the
 * background and a failed refresh keeps the cache visible — never a loading wall when cache
 * exists.
 */
interface ClockRepository {
    /** Room-cached `GET /app/clock/status`; null until the first successful refresh. */
    fun observeStatus(): Flow<ClockStatusResponseDto?>

    /** Refreshes the status cache. Never throws; false on failure (cache kept). */
    suspend fun refreshStatus(): Boolean

    /**
     * The punch tap: captures the device facts, runs the mock-location gate, and — when clean —
     * enqueues the durable outbox write under the STABLE day-scoped idempotency key
     * (`clock:<business_date>:<in|out>`, groupKey `clock:<business_date>`).
     */
    suspend fun punch(direction: ClockPunchDirection): ClockPunchOutcome

    /**
     * One presence page (~20 rows, keyset via [cursor]). Network-first; the FIRST page of the
     * current filter/date is blob-cached so re-entering the Team board offline shows the last
     * board instead of a blank wall. [Result.failure] only when the network failed AND no cache
     * covers this first page.
     */
    suspend fun fetchPresence(
        date: String?,
        parkId: String?,
        designation: String?,
        bucket: String?,
        q: String?,
        cursor: String?,
    ): Result<ClockPresenceResponseDto>

    /** One person-day detail; network-first with blob-cache fallback. */
    suspend fun fetchPersonDay(workforceMemberId: String, date: String?): Result<ClockPersonDayResponseDto>
}

class DefaultClockRepository(
    private val api: AppApi,
    private val dao: ClockBlobCacheDao,
    private val syncRepository: SyncRepository,
    private val factsProvider: ClockPunchFactsProvider,
    private val json: Json = Json { ignoreUnknownKeys = true },
    /** Injectable device clock so key-stability tests can pin the tap instant. */
    private val now: () -> OffsetDateTime = { OffsetDateTime.now() },
) : ClockRepository {

    override fun observeStatus(): Flow<ClockStatusResponseDto?> =
        dao.observe(STATUS_KEY).map { entity ->
            entity?.dtoJson?.let { blob ->
                // exception:exempt corrupt cached blob degrades to "no cache"; the next refresh rewrites it.
                runCatching { json.decodeFromString<ClockStatusResponseDto>(blob) }.getOrNull()
            }
        }

    override suspend fun refreshStatus(): Boolean =
        runCatching { api.getClockStatus() }
            .onSuccess { dto -> upsert(STATUS_KEY, json.encodeToString(ClockStatusResponseDto.serializer(), dto)) }
            .isSuccess

    override suspend fun punch(direction: ClockPunchDirection): ClockPunchOutcome {
        val facts = factsProvider.capture()
        if (facts.verdict.blocksPunch) {
            // The server refuses independently (422 mock_location_detected); blocking here just
            // spares an honest queue slot for a punch that can never land.
            return ClockPunchOutcome.Blocked(facts.verdict)
        }
        val tap = now()
        // The IST business day is the idempotency scope. The SERVER still derives its own
        // business_date from arrival (or captured_at for an offline punch, D1) — this local
        // derivation only scopes the retry key, it never becomes backend truth.
        val businessDate = tap.atZoneSameInstant(IST).toLocalDate().toString()
        val request = ClockPunchRequestDto(
            idempotencyKey = clockPunchIdempotencyKey(businessDate, direction),
            capturedAt = tap.format(DateTimeFormatter.ISO_OFFSET_DATE_TIME),
            offline = facts.offline,
            location = facts.location,
            integrity = ClockIntegrityDto(
                mockLocation = facts.verdict.mockFix,
                mockProviderPackages = facts.verdict.mockApps.map { it.packageName },
                developerOptionsEnabled = facts.verdict.developerOptions,
            ),
            batteryPct = facts.batteryPct,
            networkKind = facts.networkKind,
        )
        return when (
            val result = syncRepository.enqueueClockPunch(
                clockIn = direction == ClockPunchDirection.IN,
                groupKey = clockPunchGroupKey(businessDate),
                idempotencyKey = request.idempotencyKey,
                request = request,
            )
        ) {
            is AppResult.Ok -> ClockPunchOutcome.Enqueued(result.value)
            is AppResult.Err -> ClockPunchOutcome.Failed(result.message)
        }
    }

    override suspend fun fetchPresence(
        date: String?,
        parkId: String?,
        designation: String?,
        bucket: String?,
        q: String?,
        cursor: String?,
    ): Result<ClockPresenceResponseDto> {
        val firstPage = cursor.isNullOrBlank()
        val cacheKey = presenceKey(date, parkId, designation, bucket, q)
        return runCatching {
            api.listClockPresence(
                date = date,
                parkId = parkId,
                designation = designation,
                bucket = bucket,
                q = q,
                limit = PAGE_SIZE,
                cursor = cursor,
            )
        }.onSuccess { dto ->
            // Only the first page of the CURRENT filter is cached — the offline fallback is "the
            // board as last seen", never an unbounded accumulation of every page ever fetched.
            if (firstPage) upsert(cacheKey, json.encodeToString(ClockPresenceResponseDto.serializer(), dto))
        }.recoverCatching { failure ->
            if (!firstPage) throw failure
            readBlob<ClockPresenceResponseDto>(cacheKey) ?: throw failure
        }
    }

    override suspend fun fetchPersonDay(
        workforceMemberId: String,
        date: String?,
    ): Result<ClockPersonDayResponseDto> {
        val cacheKey = "person:$workforceMemberId:${date.orEmpty()}"
        return runCatching { api.getClockPresencePerson(workforceMemberId, date) }
            .onSuccess { dto -> upsert(cacheKey, json.encodeToString(ClockPersonDayResponseDto.serializer(), dto)) }
            .recoverCatching { failure -> readBlob<ClockPersonDayResponseDto>(cacheKey) ?: throw failure }
    }

    private suspend fun upsert(cacheKey: String, blob: String) {
        dao.upsert(ClockBlobCacheEntity(cacheKey = cacheKey, dtoJson = blob, updatedAt = System.currentTimeMillis()))
        dao.enforceCacheBounds()
    }

    private suspend inline fun <reified T> readBlob(cacheKey: String): T? {
        // observe() is the DAO's only read; take the current row without staying subscribed.
        val blob = dao.observe(cacheKey).firstOrNull()?.dtoJson ?: return null
        // exception:exempt corrupt cached blob is a cache miss, not a crash; caller rethrows the network failure.
        return runCatching { json.decodeFromString<T>(blob) }.getOrNull()
    }

    private fun presenceKey(date: String?, parkId: String?, designation: String?, bucket: String?, q: String?): String =
        "presence:${date.orEmpty()}|${parkId.orEmpty()}|${designation.orEmpty()}|${bucket.orEmpty()}|${q.orEmpty()}"

    private companion object {
        const val STATUS_KEY = "status"

        /** Mobile page size (docs/decisions/mobile-data-fetch-anti-patterns.md): ~20, never more. */
        const val PAGE_SIZE = 20
        val IST: ZoneId = ZoneId.of("Asia/Kolkata")
    }
}
