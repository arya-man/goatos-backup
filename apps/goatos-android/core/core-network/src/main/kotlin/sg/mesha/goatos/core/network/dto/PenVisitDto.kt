package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Pen visits (maintainer decision 2026-09-07): the day after vaccination or PC Care work in a
 * pen, the park head goes to that pen, records ONE live in-app-camera video and submits it. No
 * scan, no roster, no verifier. The list is the park head's own tasks — the "For me" tab of the
 * Tasks module.
 *
 * ALL visible copy (`title`, `operational_location_display`, `reason_line`, `state_chip`,
 * `instruction`, `done_line`, the filter labels and empty messages, the page title) is
 * BACKEND-OWNED and rendered verbatim. The phone never composes a chip, a reason line, a pen
 * label or a date of its own.
 *
 * Wire contract of record: backend/internal/penvisits/adapters/http/payloads.go.
 */
@Serializable
data class PenVisitDto(
    @SerialName("task_id") val taskId: String = "",
    /** Backend-composed ("Visit Castro 2 · Coimbatore"), rendered VERBATIM. */
    @SerialName("title") val title: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_name") val parkName: String = "",
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("shed_name") val shedName: String = "",
    @SerialName("partition_label") val partitionLabel: String = "",
    /** The pen label as the backend composed it — rendered VERBATIM, never re-composed. */
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    @SerialName("reasons") val reasons: List<String> = emptyList(),
    @SerialName("reason_labels") val reasonLabels: List<String> = emptyList(),
    /** Backend-composed ("Vaccination yesterday"), rendered VERBATIM. */
    @SerialName("reason_line") val reasonLine: String = "",
    @SerialName("source_business_date") val sourceBusinessDate: String = "",
    @SerialName("planned_business_date") val plannedBusinessDate: String = "",
    @SerialName("due_business_date") val dueBusinessDate: String = "",
    /** `scheduled` | `delayed` | `completed` | `canceled`. */
    @SerialName("work_state") val workState: String = "",
    /** Backend-composed chip copy ("Due today" / "Delayed since 5 Sep" / "Done"), VERBATIM. */
    @SerialName("state_chip") val stateChip: String = "",
    /** `info` | `danger` | `success` | `muted` — the chip's colour only, never its words. */
    @SerialName("state_tone") val stateTone: String = "",
    @SerialName("instruction") val instruction: String = "",
    @SerialName("done_line") val doneLine: String = "",
    /** The caller may submit this visit — resolved by the backend against the stored row. */
    @SerialName("can_submit") val canSubmit: Boolean = false,
    @SerialName("proof_ref") val proofRef: String? = null,
    @SerialName("submitted_at") val submittedAt: String? = null,
    @SerialName("row_version") val rowVersion: Int = 0,
)

@Serializable
data class PenVisitDetailDto(
    @SerialName("task") val task: PenVisitDto = PenVisitDto(),
    @SerialName("trace_id") val traceId: String = "",
)

/** One filter chip on the list. Labels, counts and the empty line are backend-composed. */
@Serializable
data class PenVisitFilterDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("count") val count: Int = 0,
    @SerialName("selected") val selected: Boolean = false,
    @SerialName("empty_message") val emptyMessage: String = "",
)

@Serializable
data class PenVisitPageDto(
    /** The backend's own page title ("For me"), rendered VERBATIM. */
    @SerialName("title") val title: String = "",
    @SerialName("rows") val rows: List<PenVisitDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("filters") val filters: List<PenVisitFilterDto> = emptyList(),
    /** Whole-list count of visits still owed by the caller; never a page-local sum. */
    @SerialName("open_count") val openCount: Int = 0,
    @SerialName("trace_id") val traceId: String = "",
)

/**
 * `POST /app/pen-visits/{task_id}/submit` body. [proofRef] is the SERVER proof id of the
 * uploaded in-app-camera video; [rowVersion] is the optimistic-concurrency token the screen last
 * rendered (409 `stale_task` when it moved).
 */
@Serializable
data class PenVisitSubmitRequestDto(
    @SerialName("proof_ref") val proofRef: String,
    @SerialName("row_version") val rowVersion: Int,
)
