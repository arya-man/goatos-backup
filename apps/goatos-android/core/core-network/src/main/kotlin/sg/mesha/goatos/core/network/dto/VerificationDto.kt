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
 * DTOs reconciled against `contracts/openapi/app-api.yaml` (GET /verification/queue,
 * POST /verification/items/{id}/verdict). ignoreUnknownKeys on the shared
 * [sg.mesha.goatos.core.network.dto] json config makes this forward-compatible
 * with fields the backend adds later.
 */

/** Back-pointer to the module/task/submission that produced this verification item. */
@Serializable
data class VerificationSourceRef(
    @SerialName("module") val module: String = "",
    @SerialName("ref_type") val refType: String = "",
    @SerialName("ref_id") val refId: String = "",
    @SerialName("task_id") val taskId: String? = null,
    @SerialName("submission_id") val submissionId: String? = null,
)

/**
 * One piece of proof media on a verification item. [downloadUrl] is a short-lived streamed
 * URL (never proxied through the app-api, never persisted past its TTL — see the media
 * verification-module-design.md §2.5 scale rule) that a video player streams directly.
 */
@Serializable
data class VerificationMediaItem(
    @SerialName("proof_id") val proofId: String = "",
    @SerialName("label") val label: String? = null,
    @SerialName("answer") val answer: String? = null,
    @SerialName("download_url") val downloadUrl: String = "",
    @SerialName("mime_type") val mimeType: String? = null,
    @SerialName("duration_ms") val durationMs: Long? = null,
)

/**
 * One row in the verifier's queue. The backend owns all display composition;
 * the app never derives labels from ids (TRD dumb-renderer rule). [category]
 * is the module-agnostic filter dimension (e.g. `vaccine`, `feed_direction`,
 * `diagnosis`, `death_post_mortem`, `breeding`, …) driven by the backend's
 * verification type registry, never a client-hardcoded enum.
 */
@Serializable
data class VerificationQueueItem(
    @SerialName("item_id") val itemId: String = "",
    @SerialName("vertical") val vertical: String = "",
    @SerialName("module") val module: String = "",
    @SerialName("category") val category: String = "",
    @SerialName("subject_label") val subjectLabel: String? = null,
    @SerialName("status") val status: String = "",
    @SerialName("captured_at") val capturedAt: String = "",
    @SerialName("row_version") val rowVersion: Int = 1,
    @SerialName("media") val media: List<VerificationMediaItem> = emptyList(),
    @SerialName("source") val source: VerificationSourceRef = VerificationSourceRef(),
    @SerialName("verdict_reason") val verdictReason: String? = null,
    @SerialName("operator_id") val operatorId: String? = null,
    @SerialName("operator_name") val operatorName: String? = null,
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("shed_label") val shedLabel: String? = null,
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("park_label") val parkLabel: String? = null,
    @SerialName("verified_by") val verifiedBy: String? = null,
    @SerialName("verified_at") val verifiedAt: String? = null,
    @SerialName("closed_by") val closedBy: String? = null,
    @SerialName("closed_at") val closedAt: String? = null,
    /** R50-017: false when the backend's evidence resolver could not produce a signed/servable
     *  proof for this item (missing object or signing failure) — the backend now fails that
     *  resolution closed rather than silently omitting media, so the app must disable a verdict
     *  decision instead of letting the verifier approve/reject on no evidence. Defaults `true`
     *  for forward/backward compatibility with a backend that has not shipped this field yet. */
    @SerialName("evidence_available") val evidenceAvailable: Boolean = true,
)

/** GET /verification/queue — keyset-paginated, category-filtered (~20/page, TRD §14). */
@Serializable
data class VerificationQueueResponseDto(
    @SerialName("items") val items: List<VerificationQueueItem> = emptyList(),
    @SerialName("filter_options") val filterOptions: VerificationFilterOptionsDto = VerificationFilterOptionsDto(),
    @SerialName("drive_closures") val driveClosures: List<VerificationDriveClosureDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("trace_id") val traceId: String = "",
)

@Serializable
data class VerificationFilterOptionsDto(
    @SerialName("parks") val parks: List<VerificationLocationOptionDto>? = null,
    @SerialName("sheds") val sheds: List<VerificationLocationOptionDto>? = null,
)

@Serializable
data class VerificationLocationOptionDto(
    @SerialName("id") val id: String = "",
    @SerialName("label") val label: String = "",
)

@Serializable
data class VerificationDriveClosureDto(
    @SerialName("batch_id") val batchId: String = "",
    @SerialName("drive_key") val driveKey: String = "",
    @SerialName("drive_label") val driveLabel: String = "",
    @SerialName("batch_label") val batchLabel: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("start_date") val startDate: String = "",
    @SerialName("end_date") val endDate: String = "",
    @SerialName("total_count") val totalCount: Int = 0,
    @SerialName("approved_count") val approvedCount: Int = 0,
    @SerialName("rejected_count") val rejectedCount: Int = 0,
    @SerialName("pending_count") val pendingCount: Int = 0,
    @SerialName("video_count") val videoCount: Int = 0,
    @SerialName("approved_videos") val approvedVideos: Int = 0,
    @SerialName("rejected_videos") val rejectedVideos: Int = 0,
    @SerialName("pending_videos") val pendingVideos: Int = 0,
    @SerialName("shed_count") val shedCount: Int = 0,
    @SerialName("ready") val ready: Boolean = false,
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
    @SerialName("item") val item: VerificationQueueItem = VerificationQueueItem(),
    @SerialName("trace_id") val traceId: String = "",
)

@Serializable
data class VerificationCloseRequestDto(
    @SerialName("row_version") val rowVersion: Int,
)

@Serializable
data class VerificationCloseSubmissionResponseDto(
    @SerialName("items") val items: List<VerificationQueueItem> = emptyList(),
    @SerialName("trace_id") val traceId: String = "",
)

/** [VerificationVerdictRequestDto.decision] values — never inline string-literal-compared. */
object VerificationDecision {
    const val APPROVED = "approved"
    const val REJECTED = "rejected"
}

/** [VerificationQueueItem.status] values. */
object VerificationStatus {
    const val PENDING = "pending"
    const val APPROVED = "approved"
    const val REJECTED = "rejected"
}
