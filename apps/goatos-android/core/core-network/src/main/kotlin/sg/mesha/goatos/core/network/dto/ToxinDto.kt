package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Toxin module (aflatoxin strip test, maintainer decision 2026-08-25): one 7-step guided test
 * task per purchased feed load, created SERVER-side. Steps 1/2/3/5/6 are one in-app-camera VIDEO
 * each, step 4 is a server-enforced settling wait, step 7 is a strip PHOTO + reading.
 *
 * ALL business copy (titles, instructions, chips, context lines, reading guide, outcome labels)
 * is BACKEND-OWNED and rendered verbatim. Step STATES are composed against the SERVER clock —
 * the phone renders them and never derives its own gate logic (`available_at` is display-countdown
 * input only; the server re-checks every write).
 *
 * Wire contract of record: backend/internal/toxin/adapters/http/payloads.go.
 */
@Serializable
data class ToxinStepDto(
    @SerialName("step_no") val stepNo: Int,
    /** `video` | `wait` | `photo_reading`. */
    @SerialName("kind") val kind: String,
    /** Backend-owned farm copy, rendered verbatim. */
    @SerialName("title") val title: String,
    /** Backend-owned farm copy, rendered verbatim. */
    @SerialName("instruction") val instruction: String,
    /** `done` | `available` | `waiting` | `locked` — SERVER-composed, rendered verbatim. */
    @SerialName("state") val state: String,
    @SerialName("wait_minutes") val waitMinutes: Int = 0,
    /** RFC3339 server instant the step unlocks; set only while [state] is `waiting`. */
    @SerialName("available_at") val availableAt: String = "",
    @SerialName("proof_ref") val proofRef: String = "",
    /** Backend-resolved display name of whoever completed the step (steps are person-independent). */
    @SerialName("completed_by") val completedBy: String = "",
    @SerialName("completed_at") val completedAt: String = "",
)

/** One toxin test round (list-row shape). */
@Serializable
data class ToxinTaskDto(
    @SerialName("task_id") val taskId: String,
    @SerialName("feed_purchase_id") val feedPurchaseId: String = "",
    @SerialName("round_no") val roundNo: Int = 1,
    @SerialName("origin") val origin: String = "",
    /** Backend-owned farm copy for a retest round; blank on a first round. */
    @SerialName("origin_line") val originLine: String = "",
    @SerialName("farm_label") val farmLabel: String = "",
    @SerialName("feed_item_label") val feedItemLabel: String = "",
    @SerialName("vendor") val vendor: String = "",
    @SerialName("batch_no") val batchNo: Int = 0,
    @SerialName("purchase_date") val purchaseDate: String = "",
    @SerialName("quantity_kg") val quantityKg: Double = 0.0,
    /** `in_progress` | `pending_review` | `accepted` | `cancelled`. */
    @SerialName("status") val status: String = "",
    /** Backend-composed chip copy, rendered VERBATIM. */
    @SerialName("status_chip") val statusChip: String = "",
    @SerialName("outcome") val outcome: String = "",
    @SerialName("outcome_label") val outcomeLabel: String = "",
    @SerialName("strip_photo_ref") val stripPhotoRef: String = "",
    @SerialName("submitted_by") val submittedBy: String = "",
    @SerialName("submitted_at") val submittedAt: String = "",
    @SerialName("reviewed_at") val reviewedAt: String = "",
    @SerialName("review_reason") val reviewReason: String = "",
    @SerialName("cancel_reason") val cancelReason: String = "",
    @SerialName("steps_done") val stepsDone: Int = 0,
    @SerialName("steps_total") val stepsTotal: Int = 0,
    @SerialName("row_version") val rowVersion: Long = 0,
    @SerialName("created_at") val createdAt: String = "",
    /** Backend-composed card subtitle (feed, vendor, load, date in one farm line), VERBATIM. */
    @SerialName("context_line") val contextLine: String = "",
    /**
     * Whether THIS signed-in person may run the test. False for a CEO/CXO, who watches the
     * round and casts the verdict but never films a step (maintainer decision 2026-08-26).
     * Defaults false so an older payload without the field is treated as watch-only rather
     * than offering a capture the server would refuse.
     */
    @SerialName("can_execute") val canExecute: Boolean = false,
)

@Serializable
data class ToxinTaskPageDto(
    @SerialName("tasks") val tasks: List<ToxinTaskDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
    /** Whole-tenant aggregates per status, never page-local sums. */
    @SerialName("status_counts") val statusCounts: Map<String, Int> = emptyMap(),
)

@Serializable
data class ToxinOutcomeOptionDto(
    @SerialName("value") val value: String,
    /** Backend-owned label ("Negative"), rendered verbatim. */
    @SerialName("label") val label: String,
)

/**
 * The detail payload: the Go handler embeds the task payload, so its fields arrive FLATTENED
 * beside `steps` / `reading_guide` / `outcome_options`. [toTask] projects the task half back out
 * for list-row reconciliation.
 */
@Serializable
data class ToxinTaskDetailDto(
    @SerialName("task_id") val taskId: String,
    @SerialName("feed_purchase_id") val feedPurchaseId: String = "",
    @SerialName("round_no") val roundNo: Int = 1,
    @SerialName("origin") val origin: String = "",
    @SerialName("origin_line") val originLine: String = "",
    @SerialName("farm_label") val farmLabel: String = "",
    @SerialName("feed_item_label") val feedItemLabel: String = "",
    @SerialName("vendor") val vendor: String = "",
    @SerialName("batch_no") val batchNo: Int = 0,
    @SerialName("purchase_date") val purchaseDate: String = "",
    @SerialName("quantity_kg") val quantityKg: Double = 0.0,
    @SerialName("status") val status: String = "",
    @SerialName("status_chip") val statusChip: String = "",
    @SerialName("outcome") val outcome: String = "",
    @SerialName("outcome_label") val outcomeLabel: String = "",
    @SerialName("strip_photo_ref") val stripPhotoRef: String = "",
    @SerialName("submitted_by") val submittedBy: String = "",
    @SerialName("submitted_at") val submittedAt: String = "",
    @SerialName("reviewed_at") val reviewedAt: String = "",
    @SerialName("review_reason") val reviewReason: String = "",
    @SerialName("cancel_reason") val cancelReason: String = "",
    @SerialName("steps_done") val stepsDone: Int = 0,
    @SerialName("steps_total") val stepsTotal: Int = 0,
    @SerialName("row_version") val rowVersion: Long = 0,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("context_line") val contextLine: String = "",
    /**
     * Whether THIS signed-in person may run the test. False for a CEO/CXO, who watches the
     * round and casts the verdict but never films a step (maintainer decision 2026-08-26).
     * Defaults false so an older payload without the field is treated as watch-only rather
     * than offering a capture the server would refuse.
     */
    @SerialName("can_execute") val canExecute: Boolean = false,
    @SerialName("steps") val steps: List<ToxinStepDto> = emptyList(),
    /** Backend-owned strip reading guide lines, rendered verbatim. */
    @SerialName("reading_guide") val readingGuide: List<String> = emptyList(),
    /** Backend-owned outcome vocabulary, rendered verbatim — never a client-invented set. */
    @SerialName("outcome_options") val outcomeOptions: List<ToxinOutcomeOptionDto> = emptyList(),
) {
    /** The embedded task half, for reconciling cached list rows after a detail write. */
    fun toTask(): ToxinTaskDto = ToxinTaskDto(
        taskId = taskId,
        feedPurchaseId = feedPurchaseId,
        roundNo = roundNo,
        origin = origin,
        originLine = originLine,
        farmLabel = farmLabel,
        feedItemLabel = feedItemLabel,
        vendor = vendor,
        batchNo = batchNo,
        purchaseDate = purchaseDate,
        quantityKg = quantityKg,
        status = status,
        statusChip = statusChip,
        outcome = outcome,
        outcomeLabel = outcomeLabel,
        stripPhotoRef = stripPhotoRef,
        submittedBy = submittedBy,
        submittedAt = submittedAt,
        reviewedAt = reviewedAt,
        reviewReason = reviewReason,
        cancelReason = cancelReason,
        stepsDone = stepsDone,
        stepsTotal = stepsTotal,
        rowVersion = rowVersion,
        createdAt = createdAt,
        contextLine = contextLine,
        canExecute = canExecute,
    )
}

@Serializable
data class ToxinStepCompleteRequestDto(
    @SerialName("proof_ref") val proofRef: String,
)

@Serializable
data class ToxinSubmitRequestDto(
    @SerialName("outcome") val outcome: String,
    @SerialName("strip_photo_ref") val stripPhotoRef: String,
)
