package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * GET /vaccination/execution -> VaccinationExecutionResponse and
 * GET /vaccination/execution/sheds/{shed_id} -> VaccinationExecutionShedDrilldown.
 *
 * NOTE: this endpoint family serializes in camelCase on the wire (parkId, shedName,
 * workState, ...), unlike the snake_case Calendar/Control-Tower/Adherence/Tasks
 * endpoints. @SerialName is pinned explicitly so the binding survives any Json
 * naming-strategy change. Only mobile-rendered fields are modeled; the rest are
 * dropped by ignoreUnknownKeys.
 */
@Serializable
data class VaccinationExecutionOwnerDto(
    @SerialName("operatorName") val operatorName: String? = null,
    @SerialName("parkHeadName") val parkHeadName: String? = null,
    @SerialName("verifierName") val verifierName: String? = null,
)

@Serializable
data class VaccinationExecutionRowDto(
    @SerialName("parkId") val parkId: String = "",
    @SerialName("parkName") val parkName: String = "",
    @SerialName("shedId") val shedId: String = "",
    @SerialName("shedName") val shedName: String = "",
    @SerialName("physicalShed") val physicalShed: String = "",
    // `partition` is the LEGACY raw value and carries the 'whole' sentinel, which is a
    // matching key and must never be shown. Render operationalLocationDisplay instead --
    // the backend composes it with oploc.Display() so every surface agrees.
    @SerialName("partition") val partition: String = "",
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("source_shed_name") val sourceShedName: String? = null,
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    @SerialName("animalStage") val animalStage: String = "",
    @SerialName("targetCount") val targetCount: Int = 0,
    @SerialName("openCount") val openCount: Int = 0,
    @SerialName("doneCount") val doneCount: Int = 0,
    @SerialName("acceptedCount") val acceptedCount: Int? = null,
    @SerialName("reviewCount") val reviewCount: Int? = null,
    @SerialName("driveId") val driveId: String? = null,
    @SerialName("driveName") val driveName: String? = null,
    @SerialName("dueDate") val dueDate: String? = null,
    // Enums modeled as String (see VaccinationExecutionWorkState / *Severity / *SOPStatus /
    // *ProofStatus / *VerificationStatus in app-api.yaml). Kept as String so an
    // unknown/new enum value never breaks the mobile parse.
    @SerialName("workState") val workState: String = "",
    @SerialName("severity") val severity: String = "",
    @SerialName("owner") val owner: VaccinationExecutionOwnerDto? = null,
    @SerialName("blockerReason") val blockerReason: String? = null,
    @SerialName("sopStatus") val sopStatus: String = "",
    @SerialName("proofStatus") val proofStatus: String = "",
    @SerialName("verificationStatus") val verificationStatus: String = "",
    @SerialName("nextAction") val nextAction: String = "",
    @SerialName("primaryActionKey") val primaryActionKey: String = "",
    @SerialName("obligationId") val obligationId: String? = null,
    @SerialName("batchId") val batchId: String? = null,
    @SerialName("sopTaskId") val sopTaskId: String? = null,
    @SerialName("sopVersionId") val sopVersionId: String? = null,
    @SerialName("sopTaskRowVersion") val sopTaskRowVersion: Int? = null,
    @SerialName("completionId") val completionId: String? = null,
)

/**
 * Current operator-day schedule date for vaccination execution surfaces.
 *
 * The OpenAPI `VaccinationExecutionRow` contract currently exposes only `dueDate`.
 * Assignment-aware schedule fields must be added to backend/OpenAPI/generated clients
 * before Android consumes them.
 */
val VaccinationExecutionRowDto.currentScheduleDate: String?
    get() = dueDate

@Serializable
data class VaccinationExecutionResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("rows") val rows: List<VaccinationExecutionRowDto> = emptyList(),
    @SerialName("totalCount") val totalCount: Int = 0,
    @SerialName("nextCursor") val nextCursor: String? = null,
    // Backend marks a leadership OVERSIGHT read (park-scoped, all sheds, not operator-assigned):
    // the client shows the shed list read-only and must NOT open a shed into the scan/execute
    // loop. Operators get false and keep the normal open→scan flow.
    @SerialName("viewerReadOnly") val viewerReadOnly: Boolean = false,
    // Backend-owned "vaccines to carry" totals, per business day, over the WHOLE day (not the
    // paginated page). The client renders these verbatim — it never sums shed rows.
    @SerialName("carrySummary") val carrySummary: CarrySummaryDto? = null,
    @SerialName("filterOptions") val filterOptions: ExecutionFilterOptionsDto? = null,
)

@Serializable
data class ExecutionFilterOptionsDto(
    @SerialName("parks") val parks: List<ExecutionParkOptionDto> = emptyList(),
)

@Serializable
data class ExecutionParkOptionDto(
    @SerialName("parkId") val parkId: String = "",
    @SerialName("code") val code: String = "",
    @SerialName("name") val name: String = "",
)

@Serializable
data class VaccineCarrySummaryDto(
    @SerialName("vaccineLabel") val vaccineLabel: String = "",
    @SerialName("remainingDoses") val remainingDoses: Int = 0,
)

@Serializable
data class CarryDayDto(
    @SerialName("date") val date: String = "",
    @SerialName("vaccineBreakdown") val vaccineBreakdown: List<VaccineCarrySummaryDto> = emptyList(),
    @SerialName("totalRemaining") val totalRemaining: Int = 0,
)

@Serializable
data class CarrySummaryDto(
    @SerialName("carryByDay") val carryByDay: List<CarryDayDto> = emptyList(),
)

@Serializable
data class VaccinationExecutionDriveSummaryDto(
    @SerialName("driveId") val driveId: String? = null,
    @SerialName("driveName") val driveName: String? = null,
    @SerialName("workState") val workState: String = "",
    @SerialName("severity") val severity: String = "",
)

@Serializable
data class VaccinationExecutionShedSummaryDto(
    @SerialName("total") val total: Int = 0,
    @SerialName("due") val due: Int = 0,
    @SerialName("overdue") val overdue: Int = 0,
    @SerialName("proofPending") val proofPending: Int = 0,
    @SerialName("verificationPending") val verificationPending: Int = 0,
    @SerialName("rejected") val rejected: Int = 0,
    @SerialName("deferred") val deferred: Int = 0,
    @SerialName("missed") val missed: Int = 0,
    @SerialName("blocked") val blocked: Int = 0,
    @SerialName("completed") val completed: Int = 0,
)

@Serializable
data class VaccinationExecutionShedDrilldownDto(
    @SerialName("parkId") val parkId: String = "",
    @SerialName("parkName") val parkName: String = "",
    @SerialName("shedId") val shedId: String = "",
    @SerialName("shedName") val shedName: String = "",
    @SerialName("partitionLabel") val partitionLabel: String? = null,
    @SerialName("operationalLocationDisplay") val operationalLocationDisplay: String = "",
    @SerialName("animalStages") val animalStages: List<String> = emptyList(),
    @SerialName("drives") val drives: List<VaccinationExecutionDriveSummaryDto> = emptyList(),
    @SerialName("rows") val rows: List<VaccinationExecutionRowDto> = emptyList(),
    @SerialName("summary") val summary: VaccinationExecutionShedSummaryDto = VaccinationExecutionShedSummaryDto(),
)
