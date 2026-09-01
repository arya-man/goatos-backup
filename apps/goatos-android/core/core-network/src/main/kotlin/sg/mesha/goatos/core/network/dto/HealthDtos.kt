package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Wire contract for the backend-owned Health treatment read model. */
@Serializable
data class HealthWorkItemDto(
    @SerialName("health_session_id") val healthSessionId: String = "",
    @SerialName("case_id") val caseId: String = "",
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("goat_display_id") val goatDisplayId: String = "",
    @SerialName("disease_key") val diseaseKey: String = "",
    @SerialName("disease_name") val diseaseName: String = "",
    @SerialName("age_band") val ageBand: String = "",
    @SerialName("day_no") val dayNo: Int = 0,
    @SerialName("duration_days") val durationDays: Int = 3,
    @SerialName("business_date") val businessDate: String = "",
    @SerialName("session") val session: String = "",
    @SerialName("due_at") val dueAt: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("shed_label") val shedLabel: String = "",
    @SerialName("step_count") val stepCount: Int = 0,
    @SerialName("medication_count") val medicationCount: Int = 0,
    @SerialName("has_critical_step") val hasCriticalStep: Boolean = false,
)

@Serializable
data class HealthSummaryDto(
    val total: Int = 0,
    val due: Int = 0,
    val scheduled: Int = 0,
    @SerialName("in_progress") val inProgress: Int = 0,
    val completed: Int = 0,
    val rework: Int = 0,
    val held: Int = 0,
    @SerialName("canceled_death") val canceledDeath: Int = 0,
)

@Serializable
data class HealthDateMarkerDto(val date: String = "", val count: Int = 0)

@Serializable
data class HealthFilterOptionDto(val key: String = "", val label: String = "")

@Serializable
data class HealthFilterOptionsDto(
    val diseases: List<HealthFilterOptionDto> = emptyList(),
    val parks: List<HealthFilterOptionDto> = emptyList(),
    val sheds: List<HealthFilterOptionDto> = emptyList(),
)

@Serializable
data class HealthWorkItemPageDto(
    val items: List<HealthWorkItemDto> = emptyList(),
    val summary: HealthSummaryDto = HealthSummaryDto(),
    @SerialName("date_markers") val dateMarkers: List<HealthDateMarkerDto> = emptyList(),
    @SerialName("filter_options") val filterOptions: HealthFilterOptionsDto = HealthFilterOptionsDto(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)

@Serializable
data class HealthOpenCaseRequestDto(
    @SerialName("goat_id") val goatId: String,
    @SerialName("disease_key") val diseaseKey: String,
    @SerialName("age_band") val ageBand: String,
    @SerialName("start_date") val startDate: String,
)

@Serializable
data class HealthOpenCaseResponseDto(
    @SerialName("case_id") val caseId: String = "",
    @SerialName("first_session_id") val firstSessionId: String = "",
    @SerialName("session_count") val sessionCount: Int = 0,
    @SerialName("duration_days") val durationDays: Int = 3,
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)

@Serializable
data class HealthTreatmentStepDto(
    @SerialName("step_id") val stepId: String = "",
    @SerialName("day_no") val dayNo: Int = 0,
    val session: String = "",
    val seq: Int = 0,
    @SerialName("record_type") val recordType: String = "",
    @SerialName("medicine_name") val medicineName: String? = null,
    @SerialName("dosage_text") val dosageText: String? = null,
    @SerialName("dosage_denominator") val dosageDenominator: String? = null,
    @SerialName("medicine_route") val medicineRoute: String? = null,
    val instruction: String? = null,
    @SerialName("critical_action_type") val criticalActionType: String? = null,
    val status: String = "",
)

@Serializable
data class HealthWorkItemDetailDto(
    @SerialName("health_session_id") val healthSessionId: String = "",
    @SerialName("case_id") val caseId: String = "",
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("goat_display_id") val goatDisplayId: String = "",
    @SerialName("disease_key") val diseaseKey: String = "",
    @SerialName("disease_name") val diseaseName: String = "",
    @SerialName("age_band") val ageBand: String = "",
    @SerialName("day_no") val dayNo: Int = 0,
    @SerialName("duration_days") val durationDays: Int = 3,
    @SerialName("business_date") val businessDate: String = "",
    val session: String = "",
    @SerialName("due_at") val dueAt: String = "",
    val status: String = "",
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("shed_label") val shedLabel: String = "",
    @SerialName("step_count") val stepCount: Int = 0,
    @SerialName("medication_count") val medicationCount: Int = 0,
    @SerialName("has_critical_step") val hasCriticalStep: Boolean = false,
    val steps: List<HealthTreatmentStepDto> = emptyList(),
    /** Backend-derived caller capabilities (mirror health.execute / health.diagnose). Display
     * gating only — the route permission is the enforcement. Defaults false so an older backend
     * fails safe (actions hidden) rather than rendering a 403-doomed button. */
    @SerialName("can_complete") val canComplete: Boolean = false,
    @SerialName("can_close_case") val canCloseCase: Boolean = false,
)

@Serializable
data class HealthCompleteRequestDto(@SerialName("proof_ref") val proofRef: String = "")

@Serializable
data class HealthCloseCaseRequestDto(
    val outcome: String,
    val note: String? = null,
)

@Serializable
data class HealthCloseCaseResponseDto(
    @SerialName("case_id") val caseId: String = "",
    val status: String = "",
    @SerialName("closed_at") val closedAt: String = "",
    @SerialName("canceled_session_count") val canceledSessionCount: Int = 0,
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)

@Serializable
data class HealthCompleteResponseDto(
    @SerialName("health_session_id") val healthSessionId: String = "",
    val status: String = "",
    @SerialName("completed_at") val completedAt: String = "",
    @SerialName("medication_count") val medicationCount: Int = 0,
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)
