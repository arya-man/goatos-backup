package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class MilkPreparationDirectionLineDto(
    @SerialName("management_stage") val managementStage: String,
    @SerialName("head_count") val headCount: Int,
    @SerialName("per_head_ml") val perHeadMl: Int,
    @SerialName("session_count") val sessionCount: Int,
    @SerialName("required_ml") val requiredMl: Long,
)

@Serializable
data class MilkPreparationFarmTaskDto(
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("cohort_count") val cohortCount: Int = 0,
    @SerialName("head_count") val headCount: Int = 0,
    @SerialName("total_required_ml") val totalRequiredMl: Long = 0,
    @SerialName("milk_direction") val milkDirection: List<MilkPreparationDirectionLineDto> = emptyList(),
    @SerialName("citric_acid_grams_per_litre") val citricAcidGramsPerLitre: Double = 5.5,
    @SerialName("citric_acid_grams") val citricAcidGrams: Double = 0.0,
    @SerialName("verification_status") val verificationStatus: String = "not_submitted",
    @SerialName("completion_id") val completionId: String = "",
    @SerialName("attempt_no") val attemptNo: Int = 0,
    @SerialName("rework_reason") val reworkReason: String = "",
)

@Serializable
data class MilkPreparationSummaryDto(
    @SerialName("scope") val scope: String = "filtered",
    @SerialName("shed_count") val shedCount: Int = 0,
    @SerialName("cohort_count") val cohortCount: Int = 0,
    @SerialName("head_count") val headCount: Int = 0,
    @SerialName("total_required_ml") val totalRequiredMl: Long = 0,
    @SerialName("citric_acid_grams") val citricAcidGrams: Double = 0.0,
    @SerialName("blocked_row_count") val blockedRowCount: Int = 0,
    @SerialName("park_count") val parkCount: Int = 0,
    @SerialName("not_submitted_farm_count") val notSubmittedFarmCount: Int = 0,
    @SerialName("pending_verification_farm_count") val pendingVerificationFarmCount: Int = 0,
    @SerialName("completed_farm_count") val completedFarmCount: Int = 0,
    @SerialName("rework_farm_count") val reworkFarmCount: Int = 0,
)

@Serializable
data class MilkPreparationPageDto(
    @SerialName("preparation_date") val preparationDate: String = "",
    @SerialName("feeding_date") val feedingDate: String = "",
    @SerialName("generated_at") val generatedAt: String = "",
    @SerialName("farm_tasks") val farmTasks: List<MilkPreparationFarmTaskDto> = emptyList(),
    @SerialName("summary") val summary: MilkPreparationSummaryDto = MilkPreparationSummaryDto(),
    @SerialName("limit") val limit: Int = 20,
    @SerialName("offset") val offset: Int = 0,
    @SerialName("has_more") val hasMore: Boolean = false,
)

@Serializable
data class MilkPreparationProofsDto(
    @SerialName("goat_milk_quantity_proof_ref") val goatMilkQuantityProofRef: String? = null,
    @SerialName("boiling_temperature_proof_ref") val boilingTemperatureProofRef: String? = null,
    @SerialName("cooled_temperature_proof_ref") val cooledTemperatureProofRef: String? = null,
    @SerialName("uht_milk_quantity_proof_ref") val uhtMilkQuantityProofRef: String,
    @SerialName("citric_acid_mixing_proof_ref") val citricAcidMixingProofRef: String,
)

@Serializable
data class MilkPreparationAnswersDto(
    @SerialName("morning_milk_collected_litres") val morningMilkCollectedLitres: Double,
    @SerialName("evening_milk_collected_litres") val eveningMilkCollectedLitres: Double,
    @SerialName("goat_milk_quantity_litres") val goatMilkQuantityLitres: Double = 0.0,
    @SerialName("boiling_temperature_c") val boilingTemperatureC: Double = 0.0,
    @SerialName("cooled_temperature_c") val cooledTemperatureC: Double = 0.0,
    @SerialName("uht_milk_quantity_litres") val uhtMilkQuantityLitres: Double,
    @SerialName("citric_acid_grams") val citricAcidGrams: Double,
)

@Serializable
data class MilkPreparationSubmissionRequestDto(
    @SerialName("park_id") val parkId: String,
    @SerialName("preparation_date") val preparationDate: String,
    @SerialName("goat_milk_used") val goatMilkUsed: Boolean,
    @SerialName("answers") val answers: MilkPreparationAnswersDto,
    @SerialName("proofs") val proofs: MilkPreparationProofsDto,
)

@Serializable
data class MilkPreparationSubmissionResponseDto(
    @SerialName("completion_id") val completionId: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("attempt_no") val attemptNo: Int = 0,
    @SerialName("row_version") val rowVersion: Int = 0,
)

@Serializable
data class MilkFeedingWatchlistKidDto(
    @SerialName("goat_id") val goatId: String,
    @SerialName("consecutive_yes") val consecutiveYes: Int = 0,
    @SerialName("remarks") val remarks: String = "",
    @SerialName("added_date") val addedDate: String = "",
    @SerialName("added_session") val addedSession: Int = 0,
)

@Serializable
data class MilkFeedingTaskDto(
    @SerialName("task_id") val taskId: String,
    @SerialName("park_id") val parkId: String,
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("feeding_date") val feedingDate: String,
    @SerialName("session_no") val sessionNo: Int,
    @SerialName("due_time") val dueTime: String,
    @SerialName("head_count") val headCount: Int = 0,
    @SerialName("verification_status") val verificationStatus: String = "not_submitted",
    @SerialName("attempt_no") val attemptNo: Int = 0,
    @SerialName("rework_reason") val reworkReason: String = "",
    @SerialName("watchlist") val watchlist: List<MilkFeedingWatchlistKidDto> = emptyList(),
    @SerialName("available") val available: Boolean = false,
    @SerialName("available_at") val availableAt: String = "",
    @SerialName("blocked_reason") val blockedReason: String = "",
)

@Serializable
data class MilkFeedingPageDto(
    @SerialName("feeding_date") val feedingDate: String = "",
    @SerialName("generated_at") val generatedAt: String = "",
    @SerialName("items") val items: List<MilkFeedingTaskDto> = emptyList(),
    @SerialName("limit") val limit: Int = 20,
    @SerialName("offset") val offset: Int = 0,
    @SerialName("has_more") val hasMore: Boolean = false,
)

@Serializable data class MilkFeedingWatchlistAnswerDto(@SerialName("goat_id") val goatId: String, @SerialName("drank_milk") val drankMilk: Boolean)
@Serializable data class MilkFeedingNewRefusalDto(@SerialName("goat_id") val goatId: String, @SerialName("remarks") val remarks: String = "")
@Serializable data class MilkFeedingAnswersDto(
    @SerialName("watchlist_answers") val watchlistAnswers: List<MilkFeedingWatchlistAnswerDto>,
    @SerialName("total_kids_fed") val totalKidsFed: Int,
    @SerialName("attempt_1_not_drinking") val attempt1NotDrinking: Int,
    @SerialName("attempt_2_not_drinking") val attempt2NotDrinking: Int,
    @SerialName("new_refusals") val newRefusals: List<MilkFeedingNewRefusalDto>,
    @SerialName("udder_milk_not_drinking") val udderMilkNotDrinking: Int,
    @SerialName("ors_not_drinking") val orsNotDrinking: Int,
)
@Serializable data class MilkFeedingProofsDto(@SerialName("clean_bottles_proof_ref") val cleanBottlesProofRef: String, @SerialName("mixing_and_filling_proof_ref") val mixingAndFillingProofRef: String)
@Serializable data class MilkFeedingSubmitRequestDto(
    @SerialName("park_id") val parkId: String,
    @SerialName("feeding_date") val feedingDate: String, @SerialName("session_no") val sessionNo: Int,
    @SerialName("answers") val answers: MilkFeedingAnswersDto, @SerialName("proofs") val proofs: MilkFeedingProofsDto,
)
@Serializable data class MilkFeedingSubmitResponseDto(@SerialName("completion_id") val completionId: String = "", @SerialName("status") val status: String = "", @SerialName("attempt_no") val attemptNo: Int = 0, @SerialName("row_version") val rowVersion: Int = 0)
