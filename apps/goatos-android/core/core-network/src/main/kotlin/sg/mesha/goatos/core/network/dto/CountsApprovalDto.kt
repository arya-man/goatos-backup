package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

/**
 * Counts lifecycle-approval wire DTOs — the approver's queue read plus the two decision writes.
 *
 * Field names are taken verbatim from `contracts/openapi/app-api.yaml`
 * (`CountsApprovalListItem`, `CountsApprovalListResponse`, `CountsApprovalDecisionRequest`,
 * `CountsApprovalDecisionResponse`). Nothing here invents a shape, and every field carries a
 * default so a contract addition never breaks decode of an already-cached Room row.
 *
 * ### Authority is server-side, and this client must not re-derive it
 * `GET /app/counts/approvals` returns ONLY the request types the caller may decide — a park_head
 * sees shifting, a ceo_internal sees birth and death, an admin sees all three, and a caller with
 * neither approval permission gets an empty list. The filter is derived from the caller's
 * permissions, never from a query parameter. So the app renders whatever arrives; it must never
 * gate rows or actions on a locally-inspected role (that would be the banned `role ==` client
 * check, and it would drift from the server's own type-bound check on decide).
 */

/**
 * One row of the approver's queue: who raised what, and when.
 *
 * [summary] is the submitted payload echoed back so a row renders without a second fetch. It is
 * held as a raw [JsonElement] rather than a typed union because its shape varies by
 * [requestType] (a birth payload is a goat-creation body, a shifting payload is a movement) and
 * the backend owns that shape — decoding it into a client-invented struct would be the client
 * asserting a contract it does not own.
 */
@Serializable
data class CountsApprovalListItemDto(
    @SerialName("approval_request_id") val approvalRequestId: String = "",
    @SerialName("request_type") val requestType: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("raised_by_user_id") val raisedByUserId: String = "",
    @SerialName("raised_at") val raisedAt: String = "",
    /**
     * BACKEND-OWNED DISPLAY COPY. Render verbatim; never compose a label from [raisedByUserId] or
     * from [summary].
     *
     * Both fields were added 2026-08-05 with the mobile Approvals module, to close a copy-firewall
     * defect: this screen used to render "Raised by 7f3a91c2-4d18-…" and build its own
     * "12 animal(s) · to shed 0b4e-…" line from the payload, because the phone has no name source
     * for a user id or a shed id. The backend now resolves those ids and authors both lines, so the
     * phone and admin-web cannot drift on what the same row says.
     *
     * Either may be absent (nothing resolvable). When absent the UI DROPS that line — it must never
     * fall back to the id.
     */
    @SerialName("raised_by_name") val raisedByName: String? = null,
    @SerialName("summary_line") val summaryLine: String? = null,
    @SerialName("shifting_event_id") val shiftingEventId: String? = null,
    @SerialName("subject_goat_id") val subjectGoatId: String? = null,
    /**
     * Present only for a death request whose animal resolves to a real park/shed. Same
     * BACKEND-OWNED park/shed/partition fact already folded into [summaryLine] (this queue card
     * renders [summaryLine] verbatim and needs no separate rendering of this field today); declared
     * so the DTO stays contract-true and a future structured death card can use it directly instead
     * of parsing [summaryLine].
     */
    @SerialName("subject_animal_location") val subjectAnimalLocation: String? = null,
    @SerialName("summary") val summary: JsonElement? = null,
    @SerialName("decided_by_user_id") val decidedByUserId: String? = null,
    @SerialName("decided_at") val decidedAt: String? = null,
    @SerialName("decision_reason") val decisionReason: String? = null,
)

/**
 * One keyset page of the queue. [nextCursor] is absent on the last page.
 *
 * Keyset, not offset, and deliberately so: the queue is appended to continuously, so an offset
 * page would skip or repeat rows as new requests arrive while an approver pages through it.
 */
@Serializable
data class CountsApprovalListResponseDto(
    @SerialName("items") val items: List<CountsApprovalListItemDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)

/**
 * The decision body. [reason] is REQUIRED when rejecting — a rejection with no reason is not
 * actionable by the operator who raised it — and is enforced by the database as well as the API.
 * The client blocks a blank rejection reason before enqueueing so the operator sees it
 * immediately, but the server stays the authority.
 */
@Serializable
data class CountsApprovalDecisionRequestDto(
    @SerialName("reason") val reason: String? = null,
)

/**
 * The decision result. [idempotentReplay] is true when this came from a previous identical
 * decision rather than a new write — the outbox replays a decision under its stored key on every
 * retry, so this is the normal, expected outcome of a resend, not an error.
 */
@Serializable
data class CountsApprovalDecisionResponseDto(
    @SerialName("approval_request_id") val approvalRequestId: String = "",
    @SerialName("request_type") val requestType: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("decided_by_user_id") val decidedByUserId: String? = null,
    @SerialName("decided_at") val decidedAt: String? = null,
    @SerialName("decision_reason") val decisionReason: String? = null,
    @SerialName("applied_result_type") val appliedResultType: String? = null,
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)
