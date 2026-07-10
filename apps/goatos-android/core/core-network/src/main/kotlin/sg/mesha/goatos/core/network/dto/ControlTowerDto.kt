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
    @SerialName("owner_missing_count") val ownerMissingCount: Int = 0,
    @SerialName("config_or_sop_blockers") val configOrSopBlockers: Int = 0,
)

@Serializable
data class ControlTowerAlertDto(
    @SerialName("row_id") val rowId: String = "",
    @SerialName("severity") val severity: String = "",
    @SerialName("work_state") val workState: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("detail") val detail: String = "",
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("park_name") val parkName: String? = null,
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("shed_name") val shedName: String? = null,
    @SerialName("drive_name") val driveName: String? = null,
    @SerialName("owner") val owner: ProcessIntegrityOwnerDto? = null,
    @SerialName("next_action") val nextAction: String = "",
    @SerialName("evidence_link") val evidenceLink: String? = null,
    @SerialName("obligation_id") val obligationId: String = "",
)

@Serializable
data class ControlTowerResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("summary") val summary: ControlTowerSummaryDto = ControlTowerSummaryDto(),
    @SerialName("alerts") val alerts: List<ControlTowerAlertDto> = emptyList(),
    @SerialName("total_count") val totalCount: Int = 0,
    @SerialName("next_cursor") val nextCursor: String? = null,
)
