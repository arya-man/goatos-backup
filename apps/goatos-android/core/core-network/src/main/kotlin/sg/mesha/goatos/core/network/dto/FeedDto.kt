package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonTransformingSerializer

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
 *  fields the phone surfaces are bound.
 *
 *  [message] is the backend-owned operator sentence and is REQUIRED for the gated case: before a
 *  workflow's dispatch clock fires (normal 07:00, experiment 14:00) the sheet serves no rows, and
 *  this sentence is the only thing that tells the crew when it arrives. Rendering the empty list
 *  without it is an unexplained blank screen. Copy stays backend-owned — the phone never composes
 *  its own wording for this. */
@Serializable
data class FeedLifecycleDto(
    @SerialName("state") val state: String = "",
    @SerialName("message") val message: String = "",
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
    // Compatibility only. Current backend rows identify the exact shed in shedId/shedLabel and send
    // operationalLocationDisplay for the UI to render verbatim.
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    @SerialName("shed_tag") val shedTag: String = "",
    @SerialName("breed") val breed: String = "",
    @SerialName("ration_group") val rationGroup: String = "",
    @SerialName("experiment_arm") val experimentArm: String = "",
    @SerialName("session_no") val sessionNo: Int = 0,
    @SerialName("session_label") val sessionLabel: String = "",
    @SerialName("head_count") val headCount: Long = 0,
    @SerialName("head_count_informational") val headCountInformational: Boolean = false,
    @SerialName("workflow") val workflow: String = "",
    @Serializable(with = NullAsEmptyFeedItemQuantityListSerializer::class)
    @SerialName("items") val items: List<FeedItemQuantityDto> = emptyList(),
    @SerialName("session_total_kg") val sessionTotalKg: String = "",
    @SerialName("blocked") val blocked: Boolean = false,
    @SerialName("overdue_pending") val overduePending: Boolean = false,
    // True when this shed-session has a recorded completion (feed.direction.completed). Backend-owned;
    // a whole shed-session is completed at once, so every grain of the same (shed, session) carries it.
    @SerialName("completed") val completed: Boolean = false,
    // Verification-lifecycle bucket: pending | pending_verification | completed. Backend-owned, the
    // finer state `completed` collapses (completed iff this == "completed"). Empty defaults to pending.
    @SerialName("lifecycle_status") val lifecycleStatus: String = "",
) {
    /**
     * Stable identity of this row within a filter scope. Used as the Room primary key and the
     * LazyColumn item key so a re-page never reorders or duplicates a row. Derived from the
     * grouping columns the backend generates on — NOT from list position.
     */
    val grainKey: String
        get() = listOf(
            feedOperationalLocationIdentityKey(shedId, shedLabel, partitionLabel, operationalLocationDisplay),
            workflow,
            rationGroup,
            experimentArm,
            shedTag,
            sessionNo.toString(),
        ).joinToString("|")
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

/**
 * One PEN-SESSION packing line — the bag a packer fills and films.
 *
 * The session is part of this row's identity (maintainer decision 2026-08-11, reverting the
 * 2026-08-10 pen-day card). A pen's morning and evening shares are two separate bags: two cards, two
 * videos, two verification items. One clip cannot prove two bags.
 */
@Serializable
data class FeedPackingRowDto(
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("shed_label") val shedLabel: String = "",
    // One bag per operational location. Two "Castro" lines with no partition leave the packer
    // unable to tell which pen either bag is for, and the quantities can differ sharply when some
    // partitions run an authored experiment and the rest the per-head grid.
    @SerialName("partition_label") val partitionLabel: String? = null,
    // Backend-composed shed+pen label; render verbatim rather than re-joining the two halves.
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    @SerialName("session_no") val sessionNo: Int = 0,
    @SerialName("session_label") val sessionLabel: String = "",
    @SerialName("workflow") val workflow: String = "",
    @SerialName("experiment_arm") val experimentArm: String = "",
    // The pen's animals — the DENOMINATOR this session's ration was computed from, not a quantity.
    // The same figure repeats on the pen's other session; never sum it across them.
    @SerialName("head_count") val headCount: Long = 0,
    @Serializable(with = NullAsEmptyFeedItemQuantityListSerializer::class)
    @SerialName("items") val items: List<FeedItemQuantityDto> = emptyList(),
    // This session's total.
    @SerialName("total_kg") val totalKg: String = "",
    @SerialName("status") val status: String = "",
    // Orthogonal to [status]: a completed line was still ready/blocked/empty underneath. Backend-owned.
    @SerialName("completed") val completed: Boolean = false,
    // Verification-lifecycle bucket: pending | pending_verification | completed. Orthogonal to [status]
    // (the ration state). Backend-owned; empty defaults to pending.
    @SerialName("lifecycle_status") val lifecycleStatus: String = "",
    // Why this line came back to the packer, present only while it is in rework — which surfaces as
    // lifecycleStatus "pending", so the STATUS ALONE CANNOT SAY WHY. Two different things land there:
    // a verifier rejected the video, or the afternoon feed correction changed how many animals the
    // pen feeds and the recorded video no longer proves the right quantity. A correction reopens BOTH
    // of a pen's sessions, so expect it on each of the pen's cards. Backend-composed farm copy;
    // render verbatim and never compose a local sentence from the status.
    @SerialName("rework_reason") val reworkReason: String = "",
    @SerialName("blocked_reasons") val blockedReasons: List<FeedBlockedReasonDto> = emptyList(),
) {
    val grainKey: String
        get() = listOf(
            feedOperationalLocationIdentityKey(shedId, shedLabel, partitionLabel, operationalLocationDisplay),
            workflow,
            sessionNo.toString(),
        ).joinToString("|")

    companion object {
        const val STATUS_READY = "ready"
        const val STATUS_BLOCKED = "blocked"
        const val STATUS_EMPTY = "empty"
    }
}

internal fun feedOperationalLocationIdentityKey(
    shedId: String,
    shedLabel: String,
    partitionLabel: String?,
    operationalLocationDisplay: String,
): String {
    val partition = partitionIdentityToken(partitionLabel) ?: return shedId
    val shedName = shedLabel.trim()
    val display = operationalLocationDisplay.trim()
    val visible = display.ifBlank { shedName }
    if (visible.isNotBlank() && visible.equals(shedName, ignoreCase = true) && shedNameEncodesPartition(shedName, partition)) {
        return shedId
    }
    if (display.isNotBlank() && !display.equals(shedName, ignoreCase = true)) {
        return listOf(shedId, "legacy-display", display.lowercase()).joinToString("|")
    }
    if (shedNameEncodesPartition(shedName, partition)) {
        return shedId
    }
    return listOf(shedId, "legacy-partition", partition).joinToString("|")
}

private fun partitionIdentityToken(raw: String?): String? {
    val trimmed = raw?.trim().orEmpty()
    if (trimmed.isBlank() || trimmed.equals("whole", ignoreCase = true)) return null
    return trimmed.lowercase()
        .replace(Regex("^part\\s+"), "")
        .replace(Regex("[^a-z0-9]+"), " ")
        .trim()
        .ifBlank { null }
}

private fun shedNameEncodesPartition(shedName: String, partitionToken: String): Boolean {
    val normalized = shedName.lowercase()
        .replace(Regex("[^a-z0-9]+"), " ")
        .trim()
    if (normalized.isBlank()) return false
    if (Regex("""\bpart\s+${Regex.escape(partitionToken)}\b""").containsMatchIn(normalized)) return true
    if ("part" in normalized) return false
    val numberedShedFamily = Regex("""^(?:castro|gandhi|(?:new\s+)?yashoda|old\s+yashoda)\s+""")
        .containsMatchIn(normalized)
    return numberedShedFamily && normalized.endsWith(" $partitionToken")
}

private object NullAsEmptyFeedItemQuantityListSerializer :
    JsonTransformingSerializer<List<FeedItemQuantityDto>>(ListSerializer(FeedItemQuantityDto.serializer())) {
    override fun transformDeserialize(element: JsonElement): JsonElement =
        if (element is JsonNull) JsonArray(emptyList()) else element
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

@Serializable
data class FeedTransportTaskDto(
    @SerialName("task_id") val taskId: String,
    @SerialName("park_id") val parkId: String,
    @SerialName("park_label") val parkLabel: String,
    @SerialName("shed_id") val shedId: String,
    @SerialName("shed_label") val shedLabel: String,
    // The backend composes the operational location and marks it REQUIRED on this schema.
    // Render operationalLocationDisplay VERBATIM -- shedLabel alone drops the partition, so a
    // task in "Godel 1 - Part 3" reads as bare "Godel 1" on the phone. Defaults keep an older
    // backend decodable.
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    @SerialName("business_date") val businessDate: String,
    @SerialName("status") val status: String,
    @SerialName("operator_id") val operatorId: String? = null,
    @SerialName("rework_reason") val reworkReason: String? = null,
    @SerialName("scheduled_at") val scheduledAt: String,
)

@Serializable
data class FeedTransportFilterOptionDto(
    @SerialName("id") val id: String,
    @SerialName("label") val label: String,
    @SerialName("partition_label") val partitionLabel: String? = null,
)

@Serializable
data class FeedTransportFilterOptionsDto(
    @SerialName("parks") val parks: List<FeedTransportFilterOptionDto> = emptyList(),
    @SerialName("sheds") val sheds: List<FeedTransportFilterOptionDto> = emptyList(),
)

@Serializable
data class FeedTransportTaskPageDto(
    @SerialName("items") val items: List<FeedTransportTaskDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("filters") val filters: FeedTransportFilterOptionsDto = FeedTransportFilterOptionsDto(),
)
@Serializable data class FeedTransportSubmitRequestDto(@SerialName("proof_ref") val proofRef: String)
@Serializable data class FeedTransportSubmitResponseDto(@SerialName("attempt_id") val attemptId: String, @SerialName("status") val status: String, @SerialName("attempt_no") val attemptNo: Int, @SerialName("newly_pending") val newlyPending: Boolean)

// ---------------------------------------------------------------------------
// WRITE — POST /feed-direction/distribution/complete (verifier-gated)
// ---------------------------------------------------------------------------

/**
 * The gated feed-DISTRIBUTION completion body (docs/decisions/feed-distribution-verification.md).
 * This is a DIFFERENT record from the packing/direction [FeedDirectionCompleteRequestDto] path: the
 * DIRECTION operator records THREE MANDATORY proofs — a feed-weight PHOTO, a feed-distribution VIDEO
 * and a water-distribution VIDEO — which flips the shed-session to `pending_verification` and
 * enqueues a verification item. Nothing is completed until a verifier approves the set.
 *
 * Grain is (park, shed, session, target_date, workflow). [parkId] may be null (server resolves the
 * default park). ALL THREE proof refs are REQUIRED — a blank one is rejected `422 proof_required`
 * server-side (the client also gates on all three being uploaded), and so is one of the WRONG MEDIA
 * KIND. The Idempotency-Key header, not the body, carries the replay key.
 */
@Serializable
data class FeedDistributionCompleteRequestDto(
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String,
    /** Legacy compatibility only. Live identity is the exact shed_id; new clients send null. */
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("session_no") val sessionNo: Int,
    @SerialName("target_date") val targetDate: String,
    @SerialName("workflow") val workflow: String,
    /** The feed-weight PHOTO. Must be a photo captured by the LIVE in-app camera; the server checks
     *  proof_type, mime and `capture_source`. */
    @SerialName("feed_weight_proof_ref") val feedWeightProofRef: String,
    @SerialName("distribution_proof_ref") val distributionProofRef: String,
    /** The water-distribution VIDEO. Video-only since 2026-08-11 — a photo is rejected. */
    @SerialName("water_proof_ref") val waterProofRef: String,
)

/**
 * The gated-completion result. The session is NOT completed here — it moves to
 * `pending_verification` and a verification item is enqueued. [status] is the new completion status
 * (`pending_verification`); [newlyPending] is false on an idempotent replay of an already-pending
 * completion.
 */
@Serializable
data class FeedDistributionCompleteResponseDto(
    @SerialName("completion_id") val completionId: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("newly_pending") val newlyPending: Boolean = false,
)

// ---------------------------------------------------------------------------
// WRITE — POST /feed-direction/packing/complete (verifier-gated)
// ---------------------------------------------------------------------------

/**
 * The gated feed-PACKING completion body. Distinct from both [FeedDirectionCompleteRequestDto]
 * (the untouched instant packing/direction path) and [FeedDistributionCompleteRequestDto] (the
 * two-proof distribution path): packing needs only ONE MANDATORY packing video. Recording it flips
 * the shed-session to `pending_verification` and enqueues a verification item — nothing is
 * completed until a verifier approves.
 *
 * Grain is (park, shed, session, target_date, workflow). [parkId] may be null (server resolves the
 * default park). [packingProofRef] is REQUIRED — a blank value is rejected `422 proof_required`
 * server-side (the client also gates Submit on it being uploaded). The Idempotency-Key header, not
 * the body, carries the replay key.
 */
@Serializable
data class FeedPackingCompleteRequestDto(
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String,
    /** Legacy compatibility only. Live identity is the exact shed_id; new clients send null. */
    @SerialName("partition_label") val partitionLabel: String? = null,
    /** The feeding session packed and filmed. REQUIRED and part of the completion's IDENTITY
     *  (maintainer decision 2026-08-11): one video proves one session's bag, and the route rejects a
     *  missing or zero value rather than guessing which bag it covers. */
    @SerialName("session_no") val sessionNo: Int,
    @SerialName("target_date") val targetDate: String,
    @SerialName("workflow") val workflow: String,
    @SerialName("packing_proof_ref") val packingProofRef: String,
)

/**
 * The gated-completion result. The session is NOT completed here — it moves to
 * `pending_verification` and a verification item is enqueued. [status] is the new completion status
 * (`pending_verification`); [newlyPending] is false on an idempotent replay of an already-pending
 * completion.
 */
@Serializable
data class FeedPackingCompleteResponseDto(
    @SerialName("completion_id") val completionId: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("newly_pending") val newlyPending: Boolean = false,
)

/** The two dispatch workflows a feed row can belong to. `""` (unset filter) means both. */
object FeedWorkflow {
    const val NORMAL = "normal"
    const val EXPERIMENT = "experiment"
}
