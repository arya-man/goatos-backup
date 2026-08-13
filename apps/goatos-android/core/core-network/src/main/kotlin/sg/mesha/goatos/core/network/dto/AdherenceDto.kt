package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * GET /vaccination/adherence -> ProtocolAdherenceResponse (summary{} + rows[]).
 * snake_case wire format. Expected-vs-actual protocol adherence for the Protocol
 * Adherence command lens.
 */
@Serializable
data class AdherenceSummaryDto(
    @SerialName("expected_count") val expectedCount: Int = 0,
    @SerialName("completed_count") val completedCount: Int = 0,
    @SerialName("open_gap_count") val openGapCount: Int = 0,
    @SerialName("deferred_count") val deferredCount: Int = 0,
    @SerialName("process_intact_count") val processIntactCount: Int = 0,
    // adherence_percent is a JSON number (may be fractional); Double is safe.
    @SerialName("adherence_percent") val adherencePercent: Double = 0.0,
)

@Serializable
data class AdherenceRowDto(
    @SerialName("row_id") val rowId: String = "",
    @SerialName("shed_name") val shedName: String = "",
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("expected") val expected: String = "",
    @SerialName("actual") val actual: String = "",
    @SerialName("gap") val gap: String = "",
    @SerialName("severity") val severity: String = "",
    @SerialName("owner") val owner: ProcessIntegrityOwnerDto? = null,
    @SerialName("next_action") val nextAction: String = "",
    @SerialName("evidence") val evidence: ProcessIntegrityEvidenceDto = ProcessIntegrityEvidenceDto(),
    @SerialName("work_state") val workState: String = "",
    @SerialName("drive_capacity_state") val driveCapacityState: String? = null,
    @SerialName("drive_animals_required") val driveAnimalsRequired: Int = 0,
    @SerialName("drive_animals_assigned") val driveAnimalsAssigned: Int = 0,
    @SerialName("drive_operator_cap") val driveOperatorCap: Int = 0,
    @SerialName("drive_available_operators") val driveAvailableOperators: Int = 0,
    @SerialName("drive_latest_safe_date") val driveLatestSafeDate: String? = null,
    @SerialName("drive_medical_defer_reason") val driveMedicalDeferReason: String? = null,
)

@Serializable
data class ProtocolAdherenceResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("summary") val summary: AdherenceSummaryDto = AdherenceSummaryDto(),
    @SerialName("rows") val rows: List<AdherenceRowDto> = emptyList(),
    @SerialName("total_count") val totalCount: Int = 0,
    @SerialName("next_cursor") val nextCursor: String? = null,
)
