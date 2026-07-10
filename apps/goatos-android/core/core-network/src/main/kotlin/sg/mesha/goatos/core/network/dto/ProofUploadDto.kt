package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Captured-proof registration (TRD §6 `/app/proofs` signed-upload flow, metadata step).
 * The offline sync engine's outbox routes proof-upload writes through
 * `AppApi.registerProof` with an `Idempotency-Key` header, matching the shed-submit /
 * reschedule write paths. The binary PUT to a signed URL is a separate, later pass once
 * the contract's signed-URL response shape is finalized — this models the metadata/
 * confirm step only, following the same hand-mapped-DTO convention as the rest of this
 * file (replaced 1:1 by the OpenAPI-generated client next).
 */
@Serializable
data class ProofUploadRequestDto(
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("task_id") val taskId: String? = null,
    @SerialName("subject_type") val subjectType: String = "",
    @SerialName("subject_id") val subjectId: String? = null,
    @SerialName("proof_type") val proofType: String = "photo",
)

@Serializable
data class ProofUploadResponseDto(
    @SerialName("proof") val proof: ProofReferenceDto = ProofReferenceDto(),
    @SerialName("trace_id") val traceId: String = "",
)
