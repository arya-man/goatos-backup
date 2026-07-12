package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Generic, module-agnostic video-verification queue (context/architecture/
 * verification-module-design.md + context/architecture/verifier-app-and-flow.md).
 * A producer module (vaccination, diagnosis, death/post-mortem, feed-direction,
 * breeding, …) emits a `verification_item`; the standalone mobile Verifier section
 * renders it category-filtered and lets the verifier approve or reject + reason.
 *
 * CONTRACT NOTE: the backend `verification` bounded context (`GET /verification/queue`,
 * `POST /verification/items/{id}/verdict`) is being built in parallel with this client.
 * These DTOs are hand-shaped to the design doc's `verification_item` contract
 * (`ignoreUnknownKeys` on the shared [sg.mesha.goatos.core.network.dto] json config makes
 * this forward-compatible with fields the backend adds later). TODO(verification-contract):
 * once `contracts/openapi/app-api.yaml` publishes the real `verification` paths, swap
 * [sg.mesha.goatos.core.network.AppApiService]'s verification endpoints + these DTOs for
 * the generated client 1:1, the same way every other `AppApi` method is planned to migrate.
 */

/** Back-pointer to the module/task/submission that produced this verification item. */
@Serializable
data class VerificationSourceRefDto(
    @SerialName("module") val module: String = "",
    @SerialName("task_id") val taskId: String? = null,
    @SerialName("submission_id") val submissionId: String? = null,
)

/**
 * One piece of proof media on a verification item. [signedUrl] is a short-lived streamed
 * URL (never proxied through the app-api, never persisted past its TTL — see the media
 * verification-module-design.md §2.5 scale rule) that a video player streams directly.
 */
@Serializable
data class VerificationMediaDto(
    @SerialName("proof_subject") val proofSubject: String = "",
    @SerialName("signed_url") val signedUrl: String = "",
    @SerialName("mime_type") val mimeType: String = "video/mp4",
    @SerialName("captured_start_ms") val capturedStartMs: Long? = null,
    @SerialName("captured_end_ms") val capturedEndMs: Long? = null,
    @SerialName("duration_ms") val durationMs: Long? = null,
    @SerialName("captured_by_principal_id") val capturedByPrincipalId: String? = null,
)

/** The verifier's decision, once made. Null on a still-pending item. */
@Serializable
data class VerificationVerdictDto(
    @SerialName("verifier_principal_id") val verifierPrincipalId: String? = null,
    @SerialName("decided_at") val decidedAt: String? = null,
    @SerialName("reason") val reason: String? = null,
)

/**
 * One row in the verifier's queue. [shedLabel]/[parkLabel]/[operatorName]/[capturedAt] are
 * backend-composed DISPLAY strings (TRD dumb-renderer rule — the app never derives labels
 * from raw ids); [category] is the module-agnostic filter dimension (`vaccine`,
 * `feed_direction`, `diagnosis`, `death_post_mortem`, `breeding`, …) driven by the backend's
 * verification type registry, never a client-hardcoded enum.
 */
@Serializable
data class VerificationItemDto(
    @SerialName("id") val id: String = "",
    @SerialName("tenant_id") val tenantId: String = "",
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("vertical") val vertical: String = "",
    @SerialName("module") val module: String = "",
    @SerialName("category") val category: String = "",
    @SerialName("source_ref") val sourceRef: VerificationSourceRefDto = VerificationSourceRefDto(),
    @SerialName("media") val media: List<VerificationMediaDto> = emptyList(),
    @SerialName("status") val status: String = "",
    @SerialName("verdict") val verdict: VerificationVerdictDto? = null,
    @SerialName("shed_label") val shedLabel: String? = null,
    @SerialName("park_label") val parkLabel: String? = null,
    @SerialName("operator_name") val operatorName: String? = null,
    @SerialName("captured_at") val capturedAt: String? = null,
    @SerialName("row_version") val rowVersion: Int = 1,
)

/** GET /verification/queue — keyset-paginated, category-filtered (~20/page, TRD §14). */
@Serializable
data class VerificationQueueResponseDto(
    @SerialName("items") val items: List<VerificationItemDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("trace_id") val traceId: String = "",
)

/** Request body for POST /verification/items/{item_id}/verdict. [reason] is mandatory for a
 *  `rejected` decision (enforced client-side before enqueue AND server-side); optional for
 *  `approved`. [rowVersion] is the optimistic-concurrency guard against a stale verdict. */
@Serializable
data class VerificationVerdictRequestDto(
    @SerialName("decision") val decision: String,
    @SerialName("reason") val reason: String? = null,
    @SerialName("row_version") val rowVersion: Int,
)

@Serializable
data class VerificationVerdictResponseDto(
    @SerialName("item") val item: VerificationItemDto = VerificationItemDto(),
    @SerialName("trace_id") val traceId: String = "",
)

/** [VerificationVerdictRequestDto.decision] values — never inline string-literal-compared. */
object VerificationDecision {
    const val APPROVED = "approved"
    const val REJECTED = "rejected"
}

/** [VerificationItemDto.status] values. */
object VerificationStatus {
    const val PENDING = "pending_verification"
    const val APPROVED = "approved"
    const val REJECTED = "rejected"
}
