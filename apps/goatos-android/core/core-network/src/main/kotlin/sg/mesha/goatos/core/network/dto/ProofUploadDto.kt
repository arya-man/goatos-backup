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
    @SerialName("upload_protocol") val uploadProtocol: String = "simple_put",
    @SerialName("chunk_size_bytes") val chunkSizeBytes: Long? = null,
    @SerialName("trace_id") val traceId: String = "",
)

@Serializable
data class UploadedProofListResponseDto(
    @SerialName("proofs") val proofs: List<ProofArtifactDto> = emptyList(),
)

/** Request body for `POST /app/proofs/{proof_id}/complete` — the completion step of the
 *  signed-upload flow, called once the video bytes have actually been PUT to [ProofUploadResponseDto.uploadUrl]
 *  (see [sg.mesha.goatos.core.network.ProofBlobUploader]). [contentHash]/[sizeBytes] are the
 *  CLIENT's own streamed measurement; the backend re-derives/validates them from the stored
 *  object where it can (GCS HEAD, local file re-hash) so a forged client value can't corrupt
 *  the record. */
@Serializable
data class ProofCompleteRequestDto(
    @SerialName("content_hash") val contentHash: String = "",
    @SerialName("mime_type") val mimeType: String = "",
    @SerialName("size_bytes") val sizeBytes: Long = 0,
    @SerialName("duration_ms") val durationMs: Long? = null,
    @SerialName("metadata") val metadata: Map<String, JsonElement> = emptyMap(),
)

@Serializable
data class ProofCompleteResponseDto(
    @SerialName("proof") val proof: ProofArtifactDto = ProofArtifactDto(),
)

/** The completed/updated proof artifact as returned by the complete-upload endpoint. */
@Serializable
data class ProofArtifactDto(
    @SerialName("proof_id") val proofId: String = "",
    @SerialName("storage_provider") val storageProvider: String = "",
    @SerialName("proof_type") val proofType: String = "",
    @SerialName("subject_type") val subjectType: String = "",
    @SerialName("subject_id") val subjectId: String? = null,
    @SerialName("upload_state") val uploadState: String = "",
    @SerialName("mime_type") val mimeType: String = "",
    @SerialName("size_bytes") val sizeBytes: Long = 0,
    @SerialName("duration_ms") val durationMs: Long? = null,
    @SerialName("content_hash") val contentHash: String = "",
    @SerialName("metadata") val metadata: Map<String, JsonElement> = emptyMap(),
    @SerialName("download_url") val downloadUrl: String = "",
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
