package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * GET /control-tower/vaccination -> ControlTowerResponse (summary{} + alerts[]).
 * snake_case wire format. Enums (severity, work_state) are kept as String so a new
 * backend enum value never breaks the parse.
 */
@Serializable
data class ControlTowerSummaryDto(
    @SerialName("process_intact") val processIntact: Boolean = true,
    @SerialName("critical_count") val criticalCount: Int = 0,
    @SerialName("warning_count") val warningCount: Int = 0,
    @SerialName("open_gap_count") val openGapCount: Int = 0,
    @SerialName("verification_backlog") val verificationBacklog: Int = 0,
    @SerialName("config_or_sop_blockers") val configOrSopBlockers: Int = 0,
)

@Serializable
data class ControlTowerAlertDto(
    @SerialName("row_id") val rowId: String = "",
    @SerialName("severity") val severity: String = "",
    @SerialName("work_state") val workState: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("detail") val detail: String = "",
    @SerialName("scope_label") val scopeLabel: String = "",
    @SerialName("evidence_summary") val evidenceSummary: String = "",
    @SerialName("proof_summary") val proofSummary: String = "",
    @SerialName("proof_state") val proofState: String = "",
    @SerialName("verification_state") val verificationState: String = "",
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("park_name") val parkName: String? = null,
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("shed_name") val shedName: String? = null,
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("drive_name") val driveName: String? = null,
    @SerialName("owner") val owner: ProcessIntegrityOwnerDto? = null,
    @SerialName("next_action") val nextAction: String = "",
    @SerialName("evidence_link") val evidenceLink: String? = null,
    @SerialName("obligation_id") val obligationId: String = "",
    @SerialName("drive_capacity_state") val driveCapacityState: String? = null,
    @SerialName("drive_animals_required") val driveAnimalsRequired: Int = 0,
    @SerialName("drive_animals_assigned") val driveAnimalsAssigned: Int = 0,
    @SerialName("drive_operator_cap") val driveOperatorCap: Int = 0,
    @SerialName("drive_available_operators") val driveAvailableOperators: Int = 0,
    @SerialName("drive_latest_safe_date") val driveLatestSafeDate: String? = null,
    @SerialName("drive_medical_defer_reason") val driveMedicalDeferReason: String? = null,
)

@Serializable
data class ControlTowerResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("summary") val summary: ControlTowerSummaryDto = ControlTowerSummaryDto(),
    @SerialName("alerts") val alerts: List<ControlTowerAlertDto> = emptyList(),
    @SerialName("total_count") val totalCount: Int = 0,
    @SerialName("next_cursor") val nextCursor: String? = null,
)
