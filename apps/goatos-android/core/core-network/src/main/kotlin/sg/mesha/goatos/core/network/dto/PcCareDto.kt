package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * PC Care (module pc_care, maintainer decision 2026-08-21): planner-assigned deworming / ticks
 * removal / hoof trimming / hair trimming tasks with per-animal live-camera video proof and a
 * verifier gate.
 *
 * `expectedSlots` is the BACKEND-OWNED proof contract for the task's category (one `video` slot
 * for deworming/ticks removal; `before_video`/`during_video`/`after_video` for the trimming
 * categories), and `captureMode` is the BACKEND-OWNED capture flow (scan_record vs roster_pick).
 * The client iterates/branches on both verbatim — it never hardcodes a category→slot or
 * category→mode map.
 */
@Serializable
data class PcCareSlotDto(
    @SerialName("field_key") val fieldKey: String,
    /** Backend-owned farm copy, rendered verbatim ("Before trimming"). */
    @SerialName("label") val label: String,
    /** Backend-owned farm copy saying what this video must show, rendered verbatim. */
    @SerialName("description") val description: String = "",
    /** Recorder-chrome GUIDANCE (the ~10 s "while trimming" clip), never a client-enforced cap. */
    @SerialName("min_duration_hint_seconds") val minDurationHintSeconds: Int = 0,
)

@Serializable
data class PcCareTaskDto(
    @SerialName("task_id") val taskId: String,
    @SerialName("category") val category: String,
    @SerialName("park_id") val parkId: String,
    @SerialName("park_label") val parkLabel: String,
    @SerialName("shed_id") val shedId: String,
    @SerialName("shed_label") val shedLabel: String,
    @SerialName("partition_label") val partitionLabel: String = "",
    /** Backend-composed pen display ("Castro - 2") — rendered VERBATIM, never re-derived. */
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    // Backend-owned card headline: the pen display for shed tasks, the vaccine name for
    // per-vaccine stock tasks. Rendered verbatim; falls back to the location fields on old servers.
    @SerialName("task_label") val taskLabel: String = "",
    @SerialName("vaccine_label") val vaccineLabel: String = "",
    @SerialName("planned_business_date") val plannedBusinessDate: String,
    @SerialName("due_business_date") val dueBusinessDate: String,
    /** Kernel dimension: scheduled | delayed | completed | closed | canceled. */
    @SerialName("work_state") val workState: String,
    /** Gate dimension: open | pending_verification | completed | rework. */
    @SerialName("status") val status: String,
    /** The verifier's rejection sentence, rendered VERBATIM (backend-owned copy). */
    @SerialName("rework_reason") val reworkReason: String = "",
    @SerialName("row_version") val rowVersion: Int,
    @SerialName("submitted_at") val submittedAt: String? = null,
    @SerialName("assignee_user_ids") val assigneeUserIds: List<String> = emptyList(),
    @SerialName("assignee_names") val assigneeNames: List<String> = emptyList(),
    @SerialName("animal_count") val animalCount: Int = 0,
    /**
     * Backend-owned capture flow: `scan_record` (scan a tag → the recorder opens immediately) or
     * `roster_pick` (tap an RFID off the pen roster → record). Blank (an older cached row)
     * renders as scan_record.
     */
    @SerialName("capture_mode") val captureMode: String = "",
    @SerialName("expected_slots") val expectedSlots: List<PcCareSlotDto> = emptyList(),
    @SerialName("inventory_requirements") val inventoryRequirements: List<PcCareInventoryRequirementDto> = emptyList(),
    @SerialName("task_proofs") val taskProofs: List<PcCareTaskProofDto> = emptyList(),
)

@Serializable
data class PcCareInventoryRequirementDto(
    @SerialName("vaccine_label") val vaccineLabel: String,
    @SerialName("required_doses") val requiredDoses: Int,
    @SerialName("source_batch_ids") val sourceBatchIds: List<String> = emptyList(),
)

@Serializable
data class PcCareTaskProofDto(
    @SerialName("slot_key") val slotKey: String,
    @SerialName("proof_ref") val proofRef: String = "",
    @SerialName("captured_by") val capturedBy: String = "",
    @SerialName("captured_by_name") val capturedByName: String = "",
    @SerialName("captured_at") val capturedAt: String? = null,
)

/** One keyset page of the pen's resident RFIDs — the roster_pick tap list (read-only). */
@Serializable
data class PcCareTaskRosterDto(
    @SerialName("identifiers") val identifiers: List<String> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
)

@Serializable
data class PcCareTaskPageDto(
    @SerialName("items") val items: List<PcCareTaskDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
)

/** One slot's server-side state on one scanned animal, with capture attribution ("Captured by X"). */
@Serializable
data class PcCareAnimalSlotDto(
    @SerialName("field_key") val fieldKey: String,
    @SerialName("proof_ref") val proofRef: String = "",
    @SerialName("captured_by") val capturedBy: String = "",
    @SerialName("captured_by_name") val capturedByName: String = "",
    @SerialName("captured_at") val capturedAt: String? = null,
)

@Serializable
data class PcCareAnimalRowDto(
    @SerialName("animal_row_id") val animalRowId: String,
    /** The tag exactly as scanned — VERBATIM, never resolved against the herd. */
    @SerialName("scanned_identifier") val scannedIdentifier: String,
    @SerialName("scanned_by") val scannedBy: String = "",
    @SerialName("scanned_by_name") val scannedByName: String = "",
    @SerialName("scanned_at") val scannedAt: String? = null,
    @SerialName("slots") val slots: List<PcCareAnimalSlotDto> = emptyList(),
)

/**
 * The peer-visibility poll: which animals are scanned and which slots each already holds, by ANY
 * assignee. It is what lets several assigned phones split one task's videos.
 */
@Serializable
data class PcCareCapturesDto(
    @SerialName("animals") val animals: List<PcCareAnimalRowDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
)

@Serializable
data class PcCareScanRequestDto(
    @SerialName("scanned_identifier") val scannedIdentifier: String,
)

@Serializable
data class PcCareScanResponseDto(
    @SerialName("animal_row_id") val animalRowId: String,
)

@Serializable
data class PcCareSlotProofRequestDto(
    @SerialName("proof_ref") val proofRef: String,
)

/**
 * The PC Director's decision on a submitted vaccine-stock task (maintainer decision 2026-09-02).
 * `verdict` is "approve" or "reject"; `reason` is mandatory on reject and is shown verbatim to
 * the operators who must re-record the fridge.
 */
@Serializable
data class PcCareStockVerdictRequestDto(
    @SerialName("verdict") val verdict: String,
    @SerialName("reason") val reason: String = "",
)

@Serializable
data class PcCareSubmitResponseDto(
    @SerialName("task_id") val taskId: String,
    @SerialName("status") val status: String,
    @SerialName("row_version") val rowVersion: Int,
    @SerialName("animal_count") val animalCount: Int = 0,
)

// ---------------------------------------------------------------------------
// Planner (CEO create wizard)
// ---------------------------------------------------------------------------

@Serializable
data class PcCarePlannerCatalogDto(
    @SerialName("parks") val parks: List<PcCarePlannerParkDto> = emptyList(),
    @SerialName("operators") val operators: List<PcCarePlannerOperatorDto> = emptyList(),
    @SerialName("categories") val categories: List<PcCareCategoryDto> = emptyList(),
)

@Serializable
data class PcCareCategoryDto(
    @SerialName("key") val key: String,
    @SerialName("label") val label: String,
)

@Serializable
data class PcCarePlannerParkDto(
    @SerialName("park_id") val parkId: String,
    @SerialName("park_label") val parkLabel: String,
)

@Serializable
data class PcCarePlannerOperatorDto(
    @SerialName("user_id") val userId: String,
    @SerialName("display_name") val displayName: String,
    /** Empty means every park (a cross-park director). */
    @SerialName("park_ids") val parkIds: List<String> = emptyList(),
)

@Serializable
data class PcCarePlannerShedDto(
    @SerialName("shed_id") val shedId: String,
    @SerialName("shed_label") val shedLabel: String,
    @SerialName("partition_label") val partitionLabel: String = "",
    /** Backend-composed pen display — rendered VERBATIM. */
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    /** Non-empty when a live task already covers this pen for the chosen category+date. */
    @SerialName("existing_task_id") val existingTaskId: String = "",
)

@Serializable
data class PcCarePlannerShedsDto(
    @SerialName("sheds") val sheds: List<PcCarePlannerShedDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
)

@Serializable
data class PcCareCreateTaskRequestDto(
    @SerialName("category") val category: String,
    @SerialName("park_id") val parkId: String,
    @SerialName("shed_id") val shedId: String,
    @SerialName("partition_label") val partitionLabel: String = "",
    @SerialName("planned_business_date") val plannedBusinessDate: String,
    @SerialName("assignee_user_ids") val assigneeUserIds: List<String>,
    /**
     * Deworming only (maintainer decision 2026-09-03): tablets given in feed need feed & water
     * removed the evening before. True makes the SAME write also create the linked
     * feed_water_removal task for the evening before the deworming date. On any other category
     * the server refuses with 422 feed_removal_not_applicable; injection deworming simply omits
     * it (null is dropped by explicitNulls=false, so an older payload shape is unchanged).
     */
    @SerialName("feed_removal_required") val feedRemovalRequired: Boolean? = null,
    /** Who removes feed & water the evening before. Required (server 422
     *  removal_operators_required) when [feedRemovalRequired] is true. */
    @SerialName("removal_operator_user_ids") val removalOperatorUserIds: List<String>? = null,
)
