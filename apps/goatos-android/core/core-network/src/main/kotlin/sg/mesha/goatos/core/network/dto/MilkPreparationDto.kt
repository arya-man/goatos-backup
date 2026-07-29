package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class MilkPreparationProofsDto(
    @SerialName("goat_milk_quantity_proof_ref") val goatMilkQuantityProofRef: String? = null,
    @SerialName("boiling_temperature_proof_ref") val boilingTemperatureProofRef: String? = null,
    @SerialName("cooled_temperature_proof_ref") val cooledTemperatureProofRef: String? = null,
    @SerialName("uht_milk_quantity_proof_ref") val uhtMilkQuantityProofRef: String,
    @SerialName("citric_acid_mixing_proof_ref") val citricAcidMixingProofRef: String,
)

@Serializable
data class MilkPreparationSubmissionRequestDto(
    @SerialName("park_id") val parkId: String,
    @SerialName("preparation_date") val preparationDate: String,
    @SerialName("goat_milk_used") val goatMilkUsed: Boolean,
    @SerialName("proofs") val proofs: MilkPreparationProofsDto,
)

@Serializable
data class MilkPreparationSubmissionResponseDto(
    @SerialName("completion_id") val completionId: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("attempt_no") val attemptNo: Int = 0,
    @SerialName("row_version") val rowVersion: Int = 0,
)
