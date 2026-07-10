package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// HRMS roster wire DTOs (contracts/openapi/admin-api.yaml "HR Roster" tag, design doc
// docs/hr/roster-rbac-design.md). Mobile is READ-ONLY for HRMS (TRD §14): these mirror the
// admin-web CRUD contract's GET responses only — no create/update DTOs are modeled here.

/**
 * One fixed operational position seat (Position schema). Every field is backend truth;
 * the Timetable screen only glue-maps enums (tier/week_off_weekday/status) to short
 * labels — the same allowance AlertRow's tone gets for its pill.
 *
 * [workforceMemberId] is the HR roster holder id. The contract does not (yet) expose a
 * resolved display name for it — see TimetableViewModel's KDoc for the open gap.
 */
@Serializable
data class PositionDto(
    @SerialName("position_id") val positionId: String = "",
    @SerialName("workforce_member_id") val workforceMemberId: String? = null,
    @SerialName("scope_type") val scopeType: String = "",
    @SerialName("scope_id") val scopeId: String = "",
    @SerialName("position_code") val positionCode: String = "",
    @SerialName("position_tier") val positionTier: String = "",
    @SerialName("is_backup_slot") val isBackupSlot: Boolean = false,
    @SerialName("backup_group_code") val backupGroupCode: String? = null,
    @SerialName("week_off_weekday") val weekOffWeekday: String? = null,
    @SerialName("status") val status: String = "",
)

@Serializable
data class PositionListResponseDto(
    val items: List<PositionDto> = emptyList(),
    @SerialName("trace_id") val traceId: String = "",
)

/**
 * One staff leave/absence row (StaffLeave schema). Mobile only reads this to resolve a
 * coverage window's end date for the CoverageBanner ("until <ends_at>") — never to
 * submit leave (web CRUD only).
 */
@Serializable
data class StaffLeaveDto(
    @SerialName("absence_id") val absenceId: String = "",
    @SerialName("workforce_member_id") val workforceMemberId: String = "",
    @SerialName("scope_type") val scopeType: String = "",
    @SerialName("scope_id") val scopeId: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("starts_at") val startsAt: String = "",
    @SerialName("ends_at") val endsAt: String = "",
    @SerialName("replacement_member_id") val replacementMemberId: String? = null,
)

@Serializable
data class StaffLeaveListResponseDto(
    val items: List<StaffLeaveDto> = emptyList(),
    @SerialName("trace_id") val traceId: String = "",
)

/**
 * The resolved vaccination owner for a scope + date (VaccinationOwner schema, design doc
 * S4.8). [ownerSource] is one of position_holder/replacement/week_off_backup/none.
 */
@Serializable
data class VaccinationOwnerDto(
    @SerialName("scope_type") val scopeType: String = "",
    @SerialName("scope_id") val scopeId: String = "",
    @SerialName("date") val date: String = "",
    @SerialName("position_id") val positionId: String? = null,
    @SerialName("owner_workforce_member_id") val ownerWorkforceMemberId: String? = null,
    @SerialName("owner_source") val ownerSource: String = "none",
    @SerialName("reason") val reason: String? = null,
    @SerialName("escalation_park_head_member_id") val escalationParkHeadMemberId: String? = null,
)

@Serializable
data class VaccinationOwnerResponseDto(
    val owner: VaccinationOwnerDto = VaccinationOwnerDto(),
    @SerialName("trace_id") val traceId: String = "",
)
