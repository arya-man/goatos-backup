package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Leave requests (docs/features/leave-requests/plan.md, maintainer decisions 2026-09-10). The
 * operator asks for leave from the Clock screen; the park head AND HR both approve; either one
 * rejecting ends it; a pending request can be withdrawn. EVERY visible word -- the date window,
 * the day count, the status line, the slot names, the form copy -- is BACKEND-OWNED and rendered
 * verbatim. Wire contract of record: contracts/openapi/app-api.yaml, tag "Leave".
 *
 * Every field carries a default so a cached blob written before a contract addition still decodes.
 */
@Serializable
data class LeaveTodaySummaryDto(
    @SerialName("on_leave") val onLeave: Boolean = false,
    @SerialName("label") val label: String = "",
)

@Serializable
data class LeaveSlotDecisionDto(
    /** `park_head` | `hr`. */
    @SerialName("slot") val slot: String = "",
    /** `approved` | `rejected`. */
    @SerialName("decision") val decision: String = "",
    @SerialName("decided_by_name") val decidedByName: String = "",
    @SerialName("decided_at") val decidedAt: String = "",
    @SerialName("note") val note: String = "",
)

@Serializable
data class LeaveRequestDto(
    @SerialName("leave_request_id") val leaveRequestId: String,
    @SerialName("workforce_member_id") val workforceMemberId: String = "",
    @SerialName("person_name") val personName: String = "",
    @SerialName("designation") val designation: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("starts_on") val startsOn: String = "",
    @SerialName("ends_on") val endsOn: String = "",
    @SerialName("day_count") val dayCount: Int = 0,
    /** Backend-composed "12–14 Sep 2026 · 3 days", rendered verbatim. */
    @SerialName("dates_label") val datesLabel: String = "",
    @SerialName("reason") val reason: String = "",
    /** `pending` | `approved` | `rejected` | `withdrawn`. */
    @SerialName("status") val status: String = "",
    @SerialName("status_label") val statusLabel: String = "",
    /** Backend-composed "where it stands" line ("Park head approved · waiting for HR"). */
    @SerialName("status_line") val statusLine: String = "",
    @SerialName("park_head_required") val parkHeadRequired: Boolean = false,
    @SerialName("hr_required") val hrRequired: Boolean = false,
    @SerialName("park_head") val parkHead: LeaveSlotDecisionDto? = null,
    @SerialName("hr") val hr: LeaveSlotDecisionDto? = null,
    @SerialName("raised_at") val raisedAt: String = "",
    @SerialName("raised_at_label") val raisedAtLabel: String = "",
    @SerialName("decided_at") val decidedAt: String? = null,
    /** True for the requester while pending: the Withdraw button follows this, never a status check. */
    @SerialName("can_withdraw") val canWithdraw: Boolean = false,
    /** The slot the CALLER decides on this row (approver queue only). */
    @SerialName("my_slot") val mySlot: String = "",
    @SerialName("my_slot_label") val mySlotLabel: String = "",
    @SerialName("row_version") val rowVersion: Int = 0,
)

@Serializable
data class LeaveRequestCreateDto(
    @SerialName("idempotency_key") val idempotencyKey: String,
    @SerialName("starts_on") val startsOn: String,
    @SerialName("ends_on") val endsOn: String,
    @SerialName("reason") val reason: String,
)

@Serializable
data class LeaveDecisionRequestDto(
    /** REQUIRED when rejecting; optional when approving. */
    @SerialName("reason") val reason: String? = null,
)

@Serializable
data class LeaveRequestResponseDto(
    @SerialName("request") val request: LeaveRequestDto,
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
    @SerialName("trace_id") val traceId: String = "",
)

@Serializable
data class LeaveRequestListResponseDto(
    @SerialName("items") val items: List<LeaveRequestDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
    @SerialName("copy") val copy: Map<String, String> = emptyMap(),
    @SerialName("trace_id") val traceId: String = "",
)
