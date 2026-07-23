package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Feed vertical wire DTOs — the two READ surfaces the phone renders:
 *   - `GET /feed-direction/preview`  -> [FeedDirectionPreviewPageDto] (the generated feed sheet)
 *   - `GET /feed-packing/worklist`   -> [FeedPackingWorklistPageDto]  (the per-shed bag worklist)
 *
 * Field names are taken verbatim from the backend contract
 * (`backend/internal/feeddirection/domain/types.go`: `PreviewPage`/`DirectionRow`/`ItemQuantity`/
 * `PreviewSummary`, `PackingPage`/`PackingRow`/`PackingSummary`). Nothing here invents a shape.
 *
 * TWO SAFETY PROPERTIES ARE CARRIED VERBATIM FROM THE BACKEND, NOT RE-DERIVED ON DEVICE:
 *
 *  1. BLOCKED IS NOT ZERO. [FeedItemQuantityDto.quantityKg] is NULLABLE and is null iff
 *     [FeedItemQuantityDto.status] is `blocked`. An unauthored ration is `"quantity_kg": null` plus
 *     a reason, never a plausible-looking 0. A renderer must show blocked as blocked and must never
 *     substitute 0 — that is a shed quietly going unfed on a complete-looking sheet.
 *  2. THE WHOLE-SCOPE TOTALS LIVE IN THE SUMMARY, NOT IN THE PAGE. `summary.total_kg_by_feed_item`
 *     and the blocked counts are rolled up by the backend over the FULL filtered set and are
 *     independent of `limit`/`offset`. The mobile cache stores the summary separately from the
 *     paged rows for exactly this reason: a KPI is never re-derived by summing the ~20 rows in
 *     memory.
 *
 * Every field carries a default so a contract addition never breaks decode of an already-cached
 * Room row (the lenient-decode rule every other read model here follows).
 */

// ---------------------------------------------------------------------------
// Shared quantity + blocked shapes
// ---------------------------------------------------------------------------

/** A machine-stable block code plus the sentence an operator acts on. */
@Serializable
data class FeedBlockedReasonDto(
    @SerialName("code") val code: String = "",
    @SerialName("detail") val detail: String = "",
)

/**
 * One feed item's quantity for one row. [quantityKg] is null iff [status] == `blocked` — the
 * pointer-is-the-contract rule from the backend, preserved so nothing here can turn a missing
 * ration into a number.
 */
@Serializable
data class FeedItemQuantityDto(
    @SerialName("feed_item") val feedItem: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("quantity_kg") val quantityKg: String? = null,
    @SerialName("grams_per_head") val gramsPerHead: String? = null,
    @SerialName("shed_factor") val shedFactor: String? = null,
    @SerialName("blocked_reason") val blockedReason: FeedBlockedReasonDto? = null,
) {
    val isBlocked: Boolean get() = status == STATUS_BLOCKED

    companion object {
        const val STATUS_RESOLVED = "resolved"
        const val STATUS_BLOCKED = "blocked"
    }
}

/** One feed item's whole-scope total, carrying its own blocked-cell count so a column total is
 *  never read as complete when part of it is missing. */
@Serializable
data class FeedItemTotalDto(
    @SerialName("feed_item") val feedItem: String = "",
    @SerialName("quantity_kg") val quantityKg: String = "",
    @SerialName("blocked_cells") val blockedCells: Int = 0,
)

/** Issue lifecycle metadata (issued/amended/locked/pending/not_issued/draft). Lenient — only the
 *  state string the phone surfaces is bound. */
@Serializable
data class FeedLifecycleDto(
    @SerialName("state") val state: String = "",
)

/** One park (farm) the caller may generate a sheet for — the farm filter vocabulary. */
@Serializable
data class FeedFilterParkDto(
    @SerialName("park_id") val parkId: String = "",
    @SerialName("label") val label: String = "",
)

/** One shed within the served park — the shed filter vocabulary. */
@Serializable
data class FeedFilterShedDto(
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("park_id") val parkId: String = "",
)

/** One feeding session in the served park's split — the session filter vocabulary. [sessionNo] is
 *  what the client sends back as the `session` query param; [label] is the authored session name. */
@Serializable
data class FeedFilterSessionDto(
    @SerialName("session_no") val sessionNo: Int = 0,
    @SerialName("label") val label: String = "",
)

/**
 * The backend-owned park/shed filter vocabulary plus the served park id. The screen renders its
 * farm/shed pickers from this and holds no location list of its own (the golden frontend rule).
 * [servedParkId] is the park this response was generated for — the requested park, or the default
 * park the server picked when the request omitted one — so the screen shows the right park as active
 * on first load.
 */
@Serializable
data class FeedFilterOptionsDto(
    @SerialName("served_park_id") val servedParkId: String = "",
    @SerialName("parks") val parks: List<FeedFilterParkDto> = emptyList(),
    @SerialName("sheds") val sheds: List<FeedFilterShedDto> = emptyList(),
    @SerialName("sessions") val sessions: List<FeedFilterSessionDto> = emptyList(),
)

// ---------------------------------------------------------------------------
// READ — GET /feed-direction/preview
// ---------------------------------------------------------------------------

/**
 * One generated instruction: what one ration grain in one shed gets in one session.
 *
 * `ration_group` is EMPTY on an experiment row (an absolute hand-authored kg never consults the
 * breed -> ration-group map); [experimentArm] is populated instead. [headCountInformational] is
 * true when the head count did NOT drive the quantity (experiment sheds), so a reader never
 * multiplies it.
 */
@Serializable
data class FeedDirectionRowDto(
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("shed_label") val shedLabel: String = "",
    @SerialName("shed_tag") val shedTag: String = "",
    @SerialName("breed") val breed: String = "",
    @SerialName("ration_group") val rationGroup: String = "",
    @SerialName("experiment_arm") val experimentArm: String = "",
    @SerialName("session_no") val sessionNo: Int = 0,
    @SerialName("session_label") val sessionLabel: String = "",
    @SerialName("head_count") val headCount: Long = 0,
    @SerialName("head_count_informational") val headCountInformational: Boolean = false,
    @SerialName("workflow") val workflow: String = "",
    @SerialName("items") val items: List<FeedItemQuantityDto> = emptyList(),
    @SerialName("session_total_kg") val sessionTotalKg: String = "",
    @SerialName("blocked") val blocked: Boolean = false,
    @SerialName("overdue_pending") val overduePending: Boolean = false,
    // True when this shed-session has a recorded completion (feed.direction.completed). Backend-owned;
    // a whole shed-session is completed at once, so every grain of the same (shed, session) carries it.
    @SerialName("completed") val completed: Boolean = false,
) {
    /**
     * Stable identity of this row within a filter scope. Used as the Room primary key and the
     * LazyColumn item key so a re-page never reorders or duplicates a row. Derived from the
     * grouping columns the backend generates on — NOT from list position.
     */
    val grainKey: String
        get() = listOf(shedId, workflow, rationGroup, experimentArm, shedTag, sessionNo.toString())
            .joinToString("|")
}

/** Whole-filtered-scope rollup (invariant to limit/offset). */
@Serializable
data class FeedPreviewSummaryDto(
    @SerialName("scope") val scope: String = "",
    @SerialName("shed_count") val shedCount: Int = 0,
    @SerialName("row_count") val rowCount: Int = 0,
    @SerialName("total_kg_by_feed_item") val totalKgByFeedItem: List<FeedItemTotalDto> = emptyList(),
    @SerialName("blocked_count") val blockedCount: Int = 0,
    @SerialName("blocked_shed_count") val blockedShedCount: Int = 0,
)

@Serializable
data class FeedDirectionPreviewPageDto(
    @SerialName("items") val items: List<FeedDirectionRowDto> = emptyList(),
    @SerialName("summary") val summary: FeedPreviewSummaryDto = FeedPreviewSummaryDto(),
    @SerialName("lifecycle") val lifecycle: FeedLifecycleDto = FeedLifecycleDto(),
    @SerialName("draft") val draft: Boolean = false,
    @SerialName("filters") val filters: FeedFilterOptionsDto = FeedFilterOptionsDto(),
    @SerialName("target_date") val targetDate: String = "",
    @SerialName("limit") val limit: Int = 0,
    @SerialName("offset") val offset: Int = 0,
    @SerialName("has_more") val hasMore: Boolean = false,
)

// ---------------------------------------------------------------------------
// READ — GET /feed-packing/worklist
// ---------------------------------------------------------------------------

/** One shed/session packing line — a packer fills one bag per item per shed. */
@Serializable
data class FeedPackingRowDto(
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("shed_label") val shedLabel: String = "",
    @SerialName("session_no") val sessionNo: Int = 0,
    @SerialName("session_label") val sessionLabel: String = "",
    @SerialName("workflow") val workflow: String = "",
    @SerialName("experiment_arm") val experimentArm: String = "",
    @SerialName("head_count") val headCount: Long = 0,
    @SerialName("items") val items: List<FeedItemQuantityDto> = emptyList(),
    @SerialName("total_kg") val totalKg: String = "",
    @SerialName("status") val status: String = "",
    // Orthogonal to [status]: a completed line was still ready/blocked/empty underneath. Backend-owned.
    @SerialName("completed") val completed: Boolean = false,
    @SerialName("blocked_reasons") val blockedReasons: List<FeedBlockedReasonDto> = emptyList(),
) {
    val grainKey: String
        get() = listOf(shedId, workflow, sessionNo.toString()).joinToString("|")

    companion object {
        const val STATUS_READY = "ready"
        const val STATUS_BLOCKED = "blocked"
        const val STATUS_EMPTY = "empty"
    }
}

@Serializable
data class FeedPackingSummaryDto(
    @SerialName("scope") val scope: String = "",
    @SerialName("shed_count") val shedCount: Int = 0,
    @SerialName("line_count") val lineCount: Int = 0,
    @SerialName("total_kg_by_feed_item") val totalKgByFeedItem: List<FeedItemTotalDto> = emptyList(),
    @SerialName("blocked_count") val blockedCount: Int = 0,
    @SerialName("blocked_shed_count") val blockedShedCount: Int = 0,
    @SerialName("blocked_line_count") val blockedLineCount: Int = 0,
)

@Serializable
data class FeedPackingWorklistPageDto(
    @SerialName("items") val items: List<FeedPackingRowDto> = emptyList(),
    @SerialName("summary") val summary: FeedPackingSummaryDto = FeedPackingSummaryDto(),
    @SerialName("lifecycle") val lifecycle: FeedLifecycleDto = FeedLifecycleDto(),
    @SerialName("draft") val draft: Boolean = false,
    @SerialName("filters") val filters: FeedFilterOptionsDto = FeedFilterOptionsDto(),
    @SerialName("target_date") val targetDate: String = "",
    @SerialName("limit") val limit: Int = 0,
    @SerialName("offset") val offset: Int = 0,
    @SerialName("has_more") val hasMore: Boolean = false,
)

// ---------------------------------------------------------------------------
// WRITE — POST /feed-direction/complete
// ---------------------------------------------------------------------------

/**
 * One OPTIONAL video-proof reference on a completion. The proof itself is minted and uploaded through
 * the generic app/proofs upload pipeline; only the returned [proofId] is carried here.
 */
@Serializable
data class FeedProofRefDto(
    @SerialName("proof_id") val proofId: String,
    @SerialName("proof_type") val proofType: String? = null,
    @SerialName("subject_type") val subjectType: String? = null,
    @SerialName("subject_id") val subjectId: String? = null,
    @SerialName("upload_state") val uploadState: String? = null,
)

/**
 * The completion body: which shed-session, on which feed day and workflow, was carried out. Grain is
 * (park, shed, session, target_date, workflow). [parkId] may be null (the server resolves the default
 * park). [proofRefs] is optional (video optional). The Idempotency-Key header, not the body, carries
 * the replay key.
 */
@Serializable
data class FeedDirectionCompleteRequestDto(
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String,
    @SerialName("session_no") val sessionNo: Int,
    @SerialName("target_date") val targetDate: String,
    @SerialName("workflow") val workflow: String,
    @SerialName("proof_refs") val proofRefs: List<FeedProofRefDto> = emptyList(),
)

/** The completion result. [applied] is false on an idempotent replay or an already-completed session. */
@Serializable
data class FeedDirectionCompleteResponseDto(
    @SerialName("completion_id") val completionId: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("applied") val applied: Boolean = false,
)

/** The two dispatch workflows a feed row can belong to. `""` (unset filter) means both. */
object FeedWorkflow {
    const val NORMAL = "normal"
    const val EXPERIMENT = "experiment"
}
