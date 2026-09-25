package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Work Board (maintainer decision 2026-09-10): every operational module's work for ONE park and
 * ONE business day, normalised to one row shape and served through a global keyset. The phone's
 * My Work screen and the admin-web board read the SAME two endpoints
 * (`GET /work-board/rows`, `GET /work-board/summary`).
 *
 * Scope is decided SERVER-side: an operator without `work_board.oversee` is served only their own
 * rows (`own_rows_only`), a park head their park. `lane` and `work_state` are server-derived —
 * the phone never moves a card between columns and never filters rows by role.
 *
 * Wire contract of record: contracts/openapi/app-api.yaml (`WorkBoardRow`, `WorkBoardRowsPage`,
 * `WorkBoardSummary`) and backend/internal/workboard/adapters/http/handler.go.
 */

/** The pen a row belongs to. `operational_location_display` is composed ONCE on the server. */
@Serializable
data class WorkBoardPenDto(
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("shed_name") val shedName: String = "",
    @SerialName("partition_label") val partitionLabel: String = "",
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
)

/** Who owns the row; every field may be blank (see [WorkBoardRowDto.ownerState]). */
@Serializable
data class WorkBoardOwnerDto(
    @SerialName("user_id") val userId: String = "",
    @SerialName("workforce_member_id") val workforceMemberId: String = "",
    @SerialName("name") val name: String = "",
)

/** The row's own done / pending / needs-attention counts, at the module's grain. */
@Serializable
data class WorkBoardCountsDto(
    @SerialName("done") val done: Int = 0,
    @SerialName("pending") val pending: Int = 0,
    @SerialName("needs_attention") val needsAttention: Int = 0,
    /** Of pending, units handed in and waiting for a verdict; 0 when the source does not say. */
    @SerialName("in_review") val inReview: Int = 0,
    /** Of pending, units nobody has started (feed pens not filmed); 0 when the source does not say. */
    @SerialName("not_started") val notStarted: Int = 0,
)

/** ONE board row. Every string is backend-composed and rendered verbatim. */
@Serializable
data class WorkBoardRowDto(
    /** feed | health | vaccination | weighing | counts | milk | pc_care | toxin | procurement | verification */
    @SerialName("module") val module: String = "",
    @SerialName("source_type") val sourceType: String = "",
    @SerialName("source_id") val sourceId: String = "",
    /** `module|source_type|source_id` — the stable identity AND the cursor value. */
    @SerialName("row_key") val rowKey: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_name") val parkName: String = "",
    @SerialName("pen") val pen: WorkBoardPenDto = WorkBoardPenDto(),
    @SerialName("business_date") val businessDate: String = "",
    /** RFC3339 instant; blank when the module has no clock. */
    @SerialName("due_at") val dueAt: String = "",
    /** Farm wording for the clock, rendered verbatim. */
    @SerialName("clock_label") val clockLabel: String = "",
    /** One of the eleven process-integrity states — reused verbatim, never re-derived here. */
    @SerialName("work_state") val workState: String = "",
    /** todo | in_progress | in_review | done — SERVER-derived from [workState]. */
    @SerialName("lane") val lane: String = "",
    /** ok | watch | at_risk | broken */
    @SerialName("severity") val severity: String = "",
    @SerialName("owner") val owner: WorkBoardOwnerDto = WorkBoardOwnerDto(),
    /** assigned | missing | pool */
    @SerialName("owner_state") val ownerState: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("subtitle") val subtitle: String = "",
    @SerialName("counts") val counts: WorkBoardCountsDto = WorkBoardCountsDto(),
    /** Where the module's own screen opens this row; blank when the module has none yet. */
    @SerialName("href") val href: String = "",
)

/** One page of `GET /work-board/rows`. */
@Serializable
data class WorkBoardRowsPageDto(
    @SerialName("rows") val rows: List<WorkBoardRowDto> = emptyList(),
    /** Keyset cursor for the next page; absent on the last page. */
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("business_date") val businessDate: String = "",
    @SerialName("park_id") val parkId: String = "",
    /** The modules the caller MAY see, in board order, after any module filter. Nullable because
     *  the Go slice serialises as `null` when empty. */
    @SerialName("modules") val modules: List<String>? = null,
    /** True when the read was clamped to the caller's own rows (no oversee). */
    @SerialName("own_rows_only") val ownRowsOnly: Boolean = false,
    @SerialName("trace_id") val traceId: String = "",
)

/** `GET /work-board/summary` — WHOLE-FILTER counts, never page-local sums. */
@Serializable
data class WorkBoardSummaryDto(
    @SerialName("total") val total: Int = 0,
    /** Every lane present, keyed todo | in_progress | in_review | done. */
    @SerialName("by_lane") val byLane: Map<String, Int>? = null,
    @SerialName("by_state") val byState: Map<String, Int>? = null,
    @SerialName("by_module") val byModule: Map<String, Int>? = null,
    /**
     * Every module's per-state counts over the same whole filter, so the tiles can count the
     * selection the chips narrow to (a module, a lane) without a second read. Null from an older
     * server; the tiles then fall back to the whole board.
     */
    @SerialName("by_module_state") val byModuleState: Map<String, Map<String, Int>>? = null,
    @SerialName("needs_attention") val needsAttention: Int = 0,
    @SerialName("modules") val modules: List<String>? = null,
    @SerialName("lanes") val lanes: List<String>? = null,
    @SerialName("business_date") val businessDate: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("own_rows_only") val ownRowsOnly: Boolean = false,
    @SerialName("trace_id") val traceId: String = "",
) {
    /** The whole-filter count for one lane, 0 when the server listed none. */
    fun laneCount(lane: String): Int = byLane?.get(lane) ?: 0
}

/** One link of a subtask's chain ("Film", "Verify"), in a closed seven-state vocabulary. */
@Serializable
data class WorkBoardStepDto(
    @SerialName("name") val name: String = "",
    /** todo | in_progress | in_review | done | rework | needs_attention | locked */
    @SerialName("state") val state: String = "",
    @SerialName("detail") val detail: String = "",
)

/**
 * One unit of a board row's work (a pen, an animal, a step, a proof) for the drill-down. Every
 * string is backend-composed and rendered verbatim.
 */
@Serializable
data class WorkBoardSubtaskDto(
    @SerialName("key") val key: String = "",
    @SerialName("name") val name: String = "",
    @SerialName("subtitle") val subtitle: String = "",
    @SerialName("work_state") val workState: String = "",
    @SerialName("lane") val lane: String = "",
    @SerialName("owner") val owner: WorkBoardOwnerDto = WorkBoardOwnerDto(),
    @SerialName("needs_attention") val needsAttention: Boolean = false,
    @SerialName("steps") val steps: List<WorkBoardStepDto> = emptyList(),
)

/** `GET /work-board/rows/{row_key}/subtasks` — one keyset page, worst first; [total] is the whole row. */
@Serializable
data class WorkBoardSubtaskPageDto(
    @SerialName("subtasks") val subtasks: List<WorkBoardSubtaskDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("total") val total: Int = 0,
    @SerialName("row_key") val rowKey: String = "",
)
