package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.PositionListResponseDto
import sg.mesha.goatos.core.network.dto.StaffLeaveListResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationOwnerResponseDto

/**
 * HRMS roster reads (design doc docs/hr/roster-rbac-design.md): the Timetable screen's
 * position seats, staff leave (for coverage-window end dates), and the resolved
 * vaccination owner for a scope + date (coverage banner). Thin pass-through over
 * [AppApi]; DTO -> UiState mapping happens in :app.
 *
 * Mobile is READ-ONLY for HRMS (TRD §14) — this repository exposes GETs only. Position/
 * leave/backup CRUD stays web-only.
 */
interface RosterRepository {
    suspend fun positions(
        workforceMemberId: String? = null,
        scopeType: String? = null,
        scopeId: String? = null,
        positionCode: String? = null,
        status: String? = null,
        limit: Int? = null,
    ): PositionListResponseDto

    suspend fun leave(
        workforceMemberId: String? = null,
        scopeType: String? = null,
        scopeId: String? = null,
        status: String? = null,
        limit: Int? = null,
    ): StaffLeaveListResponseDto

    suspend fun vaccinationOwner(scopeType: String, scopeId: String, date: String): VaccinationOwnerResponseDto
}

class DefaultRosterRepository(
    private val api: AppApi,
) : RosterRepository {
    override suspend fun positions(
        workforceMemberId: String?,
        scopeType: String?,
        scopeId: String?,
        positionCode: String?,
        status: String?,
        limit: Int?,
    ): PositionListResponseDto =
        api.listStaffPositions(workforceMemberId, scopeType, scopeId, positionCode, status, limit)

    override suspend fun leave(
        workforceMemberId: String?,
        scopeType: String?,
        scopeId: String?,
        status: String?,
        limit: Int?,
    ): StaffLeaveListResponseDto =
        api.listStaffLeave(workforceMemberId, scopeType, scopeId, status, limit)

    override suspend fun vaccinationOwner(scopeType: String, scopeId: String, date: String): VaccinationOwnerResponseDto =
        api.getVaccinationOwner(scopeType, scopeId, date)
}
