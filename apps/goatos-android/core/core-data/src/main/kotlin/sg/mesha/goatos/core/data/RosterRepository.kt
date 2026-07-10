package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto

/**
 * HRMS roster reads for mobile (design doc docs/hr/roster-rbac-design.md): the operator's
 * center Timetable and the authenticated principal's own coverage status. Both routes are
 * operator-scoped (`GET /app/roster/timetable`, `GET /app/roster/my-coverage`) — mobile
 * never reads the admin `/admin/roster` surface (RosterRead-gated; 403s for operators).
 * Thin pass-through over [AppApi]; DTO -> UiState mapping happens in :app.
 *
 * Mobile is READ-ONLY for HRMS (TRD §14) — this repository exposes GETs only. Position/
 * leave/backup CRUD stays web-only.
 */
interface RosterRepository {
    /** GET /app/roster/timetable — the operator's center's enriched position list. */
    suspend fun timetable(centerId: String, limit: Int? = null): EnrichedPositionListResponseDto

    /** GET /app/roster/my-coverage — the authenticated principal's own coverage status. */
    suspend fun myCoverage(): MyCoverageResponseDto
}

class DefaultRosterRepository(
    private val api: AppApi,
) : RosterRepository {
    override suspend fun timetable(centerId: String, limit: Int?): EnrichedPositionListResponseDto =
        api.getOperatorTimetable(centerId, limit)

    override suspend fun myCoverage(): MyCoverageResponseDto = api.getMyCoverage()
}
