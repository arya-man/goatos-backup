package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// HRMS roster wire DTOs (contracts/openapi/app-api.yaml "Operator Roster" tag, design doc
// docs/hr/roster-rbac-design.md). Mobile is READ-ONLY for HRMS (TRD §14): these mirror the
// operator-scoped app-api GETs only — no create/update DTOs are modeled here, and mobile
// never reads the admin `/admin/roster` surface (RosterRead-gated; 403s for operators).

/**
 * One fixed operational position seat, enriched with resolved display fields
 * (EnrichedPosition schema, `GET /app/roster/timetable`). The Timetable screen only
 * glue-maps enums (tier/status) to short labels — every value is backend-provided,
 * including [personDisplayName] (the seat holder's real name, never a raw UUID).
 */
@Serializable
data class EnrichedPositionDto(
    @SerialName("position_id") val positionId: String = "",
    @SerialName("workforce_member_id") val workforceMemberId: String? = null,
    @SerialName("position_code") val positionCode: String = "",
    @SerialName("position_title") val positionTitle: String? = null,
    @SerialName("position_tier") val positionTier: String = "",
    @SerialName("scope_type") val scopeType: String = "",
    @SerialName("scope_id") val scopeId: String = "",
    @SerialName("center_label") val centerLabel: String? = null,
    @SerialName("person_display_name") val personDisplayName: String? = null,
    @SerialName("hr_designation_grade") val hrDesignationGrade: String? = null,
    @SerialName("tier") val tier: String? = null,
    @SerialName("week_off") val weekOff: String? = null,
    @SerialName("backup_group") val backupGroup: String? = null,
    @SerialName("is_backup_slot") val isBackupSlot: Boolean = false,
    @SerialName("status") val status: String = "",
    @SerialName("valid_from") val validFrom: String = "",
    @SerialName("valid_to") val validTo: String? = null,
)

@Serializable
data class EnrichedPositionListResponseDto(
    val items: List<EnrichedPositionDto> = emptyList(),
    @SerialName("trace_id") val traceId: String = "",
)

/**
 * The authenticated operator's current coverage status (MyCoverage schema,
 * `GET /app/roster/my-coverage`, design doc S4.6/S4.8). Per TRD §14 dumb-renderer,
 * [bannerText] is server-composed (Asia/Kolkata) — the client never derives its own
 * wording from [windowStart]/[windowEnd].
 */
@Serializable
data class MyCoverageDto(
    @SerialName("has_coverage") val hasCoverage: Boolean = false,
    @SerialName("position_id") val positionId: String? = null,
    @SerialName("covering_person_name") val coveringPersonName: String? = null,
    @SerialName("covering_position_title") val coveringPositionTitle: String? = null,
    @SerialName("window_start") val windowStart: String? = null,
    @SerialName("window_end") val windowEnd: String? = null,
    @SerialName("banner_text") val bannerText: String? = null,
    @SerialName("timezone") val timezone: String = "",
)

@Serializable
data class MyCoverageResponseDto(
    val coverage: MyCoverageDto = MyCoverageDto(),
    @SerialName("trace_id") val traceId: String = "",
)
