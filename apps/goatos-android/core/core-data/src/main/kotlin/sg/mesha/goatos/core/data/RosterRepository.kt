package sg.mesha.goatos.core.data

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheDao
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheDao
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheEntity
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto

/**
 * HRMS roster reads for mobile (design doc docs/hr/roster-rbac-design.md): the operator's
 * center Timetable and the authenticated principal's own coverage status. Both routes are
 * operator-scoped (`GET /app/roster/timetable`, `GET /app/roster/my-coverage`) — mobile
 * never reads the admin `/admin/roster` surface (RosterRead-gated; 403s for operators).
 *
 * Offline-first (docs/decisions/android-offline-first.md): timetable and coverage flows
 * are sourced from Room cache; refresh methods upsert cache on success. A screen must
 * never show a blank state on re-entry when cached data exists. An empty cache on cold
 * start is honest; a failed refresh keeps the last cached data visible and surfaces a
 * sync indicator or distinct offline/error state.
 *
 * Mobile is READ-ONLY for HRMS (TRD §14) — this repository exposes GETs only. Position/
 * leave/backup CRUD stays web-only.
 */
interface RosterRepository {
    /**
     * GET /app/roster/timetable — the operator's center's enriched position list.
     * Observes Room cache; refresh via [refreshTimetable].
     */
    fun observeTimetable(centerId: String): Flow<EnrichedPositionListResponseDto?>

    /**
     * GET /app/roster/my-coverage — the authenticated principal's own coverage status.
     * Observes Room cache; refresh via [refreshCoverage].
     */
    fun observeCoverage(): Flow<MyCoverageResponseDto?>

    /**
     * Refresh timetable from the API and upsert Room cache on success. Returns Success on
     * successful refresh; Failure on any error (network or otherwise). The ViewModel can
     * call isConnectivityFailure() on the exception to distinguish offline from server errors.
     * On failure, the existing cache is kept.
     */
    suspend fun refreshTimetable(centerId: String, limit: Int? = null): Result<Unit>

    /**
     * Refresh coverage from the API and upsert Room cache on success. Never throws:
     * a network failure keeps the existing cache and returns `false`. Returns `true`
     * when the cache was refreshed from the network.
     */
    suspend fun refreshCoverage(): Boolean
}

class DefaultRosterRepository(
    private val api: AppApi,
    private val timetableDao: RosterTimetableCacheDao,
    private val coverageDao: RosterCoverageCacheDao,
) : RosterRepository {

    override fun observeTimetable(centerId: String): Flow<EnrichedPositionListResponseDto?> =
        timetableDao.observe(centerId).map { entity ->
            entity?.dtoJson?.let { json -> Json.decodeFromString<EnrichedPositionListResponseDto>(json) }
        }

    override fun observeCoverage(): Flow<MyCoverageResponseDto?> =
        coverageDao.observe("coverage").map { entity ->
            entity?.dtoJson?.let { json -> Json.decodeFromString<MyCoverageResponseDto>(json) }
        }

    override suspend fun refreshTimetable(centerId: String, limit: Int?): Result<Unit> =
        runCatching {
            api.getOperatorTimetable(centerId, limit)
        }.onSuccess { dto ->
            timetableDao.upsert(
                RosterTimetableCacheEntity(
                    cacheKey = centerId,
                    dtoJson = Json.encodeToString(EnrichedPositionListResponseDto.serializer(), dto),
                    updatedAt = System.currentTimeMillis(),
                )
            )
            timetableDao.enforceCacheBounds()
        }.map { }
        // onFailure: keeps the exception so the ViewModel can classify it as connectivity or not

    override suspend fun refreshCoverage(): Boolean =
        runCatching {
            api.getMyCoverage()
        }.onSuccess { dto ->
            coverageDao.upsert(
                RosterCoverageCacheEntity(
                    dtoJson = Json.encodeToString(MyCoverageResponseDto.serializer(), dto),
                    updatedAt = System.currentTimeMillis(),
                )
            )
            coverageDao.enforceCacheBounds()
        }.isSuccess
        // onFailure: keep cache; false lets the ViewModel keep the last known state.
}
