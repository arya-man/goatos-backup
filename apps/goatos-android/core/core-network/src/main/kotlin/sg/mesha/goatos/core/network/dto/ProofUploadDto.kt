package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

/**
 * Captured-proof registration (TRD §6 `/app/proofs/uploads` signed-upload flow, metadata step).
 * The offline sync engine's outbox routes proof-upload writes through
 * `AppApi.registerProof` with an `Idempotency-Key` header, matching the shed-submit /
 * reschedule write paths. The binary PUT to the returned signed URL is a separate pass.
 *
 * [legacyShedId] / [legacyTaskId] are decode-only compatibility fields for outbox rows
 * queued before the backend route changed. [forCreateUpload] clears them before Retrofit
 * serializes the request so the strict backend schema never receives unknown fields.
 */
@Serializable
data class ProofUploadRequestDto(
    @SerialName("proof_type") val proofType: String = "photo",
    @SerialName("mime_type") val mimeType: String = "image/jpeg",
    @SerialName("scope_type") val scopeType: String = "shed",
    @SerialName("scope_id") val scopeId: String = "",
    @SerialName("subject_type") val subjectType: String = "shed",
    @SerialName("subject_id") val subjectId: String? = null,
    @SerialName("metadata") val metadata: Map<String, JsonElement> = emptyMap(),
    @SerialName("shed_id") val legacyShedId: String? = null,
    @SerialName("task_id") val legacyTaskId: String? = null,
)

@Serializable
data class ProofUploadResponseDto(
    @SerialName("proof") val proof: ProofReferenceDto = ProofReferenceDto(),
    @SerialName("upload_url") val uploadUrl: String = "",
    @SerialName("upload_method") val uploadMethod: String = "",
    @SerialName("headers") val headers: Map<String, String> = emptyMap(),
    @SerialName("expires_at") val expiresAt: String = "",
    @SerialName("trace_id") val traceId: String = "",
)

fun ProofUploadRequestDto.forCreateUpload(): ProofUploadRequestDto {
    val normalizedScopeType = scopeType.ifBlank { if (!legacyTaskId.isNullOrBlank()) "task" else "shed" }
    val normalizedScopeId = scopeId.ifBlank {
        when (normalizedScopeType) {
            "task" -> legacyTaskId.orEmpty()
            else -> legacyShedId.orEmpty()
        }
    }
    return copy(
        scopeType = normalizedScopeType,
        scopeId = normalizedScopeId,
        subjectType = subjectType.ifBlank { normalizedScopeType },
        legacyShedId = null,
        legacyTaskId = null,
    )
}
