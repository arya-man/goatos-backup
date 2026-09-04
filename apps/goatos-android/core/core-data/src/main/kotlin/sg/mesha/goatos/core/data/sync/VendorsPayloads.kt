package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import sg.mesha.goatos.core.network.dto.FeedPurchaseWriteDto
import sg.mesha.goatos.core.network.dto.SalesBenchmarkWriteDto
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealPaymentWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealStatusWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto
import sg.mesha.goatos.core.network.dto.SalesFpoLeadWriteDto
import sg.mesha.goatos.core.network.dto.SalesLeadStatusWriteDto
import sg.mesha.goatos.core.network.dto.SalesSoldTagsWriteDto
import sg.mesha.goatos.core.network.dto.SalesWeightCheckWriteDto
import sg.mesha.goatos.core.network.dto.VendorWriteDto

/**
 * Outbox payloads and the ONE definition of every Vendors-module outbox key (maintainer decision
 * 2026-09-03). Every idempotency key and group key a vendor or feed-purchase write uses is built
 * HERE and nowhere else, for the reason ToxinPayloads.kt's header gives.
 *
 * [clientId] is a UUID the add form mints ONCE when it opens and keeps across retries, so a
 * double tap or a lost response replays the SAME record instead of recording a second vendor with
 * the same details. It is never timestamp-suffixed.
 */

/** Group key for one vendor's voice-note PROOF_UPLOAD and its VENDOR_CREATE: one FIFO lane per
 *  recorded vendor, so the audio drains strictly BEFORE the create that references it. The
 *  capture path must pass this SAME value as its `uploadGroupKey`. */
fun vendorCreateGroupKey(clientId: String): String = "vendors:vendor:$clientId"

/** STABLE per client id; a retry replays for free. */
fun vendorCreateIdempotencyKey(clientId: String): String = "vendors:create:$clientId"

/** Group key for one feed purchase's create — its own lane; nothing precedes it. */
fun feedPurchaseCreateGroupKey(clientId: String): String = "vendors:purchase:$clientId"

/** STABLE per client id; sent VERBATIM as the backend's required `Idempotency-Key`, so the
 *  server's own replay contract (exact replay returns the original row) holds end to end. */
fun feedPurchaseCreateIdempotencyKey(clientId: String): String = "vendors:purchase-create:$clientId"

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.VENDOR_CREATE]. The
 * voice note rides by REFERENCE to its PROOF_UPLOAD outbox row ([voiceNoteOutboxItemId], blank
 * when no note was recorded); the dispatcher resolves the uploaded server proof id into
 * `voice_note_proof_ref` at drain time.
 */
@Serializable
data class VendorCreatePayload(
    @SerialName("client_id") val clientId: String,
    @SerialName("request") val request: VendorWriteDto,
    @SerialName("voice_note_outbox_item_id") val voiceNoteOutboxItemId: String = "",
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.FEED_PURCHASE_CREATE]. */
@Serializable
data class FeedPurchaseCreatePayload(
    @SerialName("client_id") val clientId: String,
    @SerialName("request") val request: FeedPurchaseWriteDto,
)

/** Group key for one recorded sale's create — its own lane; nothing precedes it. */
fun salesDealCreateGroupKey(clientId: String): String = "sales:deal:$clientId"

/** STABLE per client id; sent VERBATIM as the backend's required `Idempotency-Key`. */
fun salesDealCreateIdempotencyKey(clientId: String): String = "sales:deal-create:$clientId"

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.SALES_DEAL_CREATE]. */
@Serializable
data class SalesDealCreatePayload(
    @SerialName("client_id") val clientId: String,
    @SerialName("request") val request: SalesDealWriteDto,
)

// ---------------------------------------------------------------------------------------------
// Editing a recorded sale, and pipeline/evidence entry (maintainer instruction 2026-09-04)
// ---------------------------------------------------------------------------------------------

/** One lane per DEAL, so a receipt and a status change on the same sale drain in the order the
 *  operator made them and never race each other's `payment_balance`. */
fun salesDealEditGroupKey(dealId: String): String = "sales:deal-edit:$dealId"

/** STABLE per client id: a retry replays the SAME receipt instead of paying the buyer twice. */
fun salesDealPaymentIdempotencyKey(clientId: String): String = "sales:deal-payment:$clientId"

/** STABLE per client id. */
fun salesDealStatusIdempotencyKey(clientId: String): String = "sales:deal-status:$clientId"

/** One lane for pipeline/evidence entry; these rows are independent of any deal. */
fun salesPipelineGroupKey(clientId: String): String = "sales:pipeline:$clientId"

/** STABLE per client id; sent VERBATIM as the backend's required `Idempotency-Key`. */
fun salesPipelineIdempotencyKey(clientId: String): String = "sales:pipeline-write:$clientId"

/** Which of the three receipt verbs a [SalesDealPaymentPayload] carries. */
object SalesPaymentOp {
    const val CREATE = "create"
    const val UPDATE = "update"
    const val DELETE = "delete"
}

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.SALES_DEAL_PAYMENT_WRITE].
 * [paymentId] is blank for [SalesPaymentOp.CREATE] and required for the other two; [request] is
 * absent on a delete, which carries no body.
 */
@Serializable
data class SalesDealPaymentPayload(
    @SerialName("client_id") val clientId: String,
    @SerialName("deal_id") val dealId: String,
    @SerialName("op") val op: String,
    @SerialName("payment_id") val paymentId: String = "",
    @SerialName("request") val request: SalesDealPaymentWriteDto? = null,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.SALES_DEAL_STATUS_SET]. */
@Serializable
data class SalesDealStatusPayload(
    @SerialName("client_id") val clientId: String,
    @SerialName("deal_id") val dealId: String,
    @SerialName("request") val request: SalesDealStatusWriteDto,
)

/** Which panel a [SalesPipelinePayload] is for. */
object SalesPipelineKind {
    const val BUYER_LEAD = "buyer_lead"
    const val BUYER_LEAD_STATUS = "buyer_lead_status"
    const val FPO_LEAD = "fpo_lead"
    const val FPO_LEAD_STATUS = "fpo_lead_status"
    const val BENCHMARK = "benchmark"
    const val SOLD_TAGS = "sold_tags"
    const val WEIGHT_CHECK = "weight_check"
}

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.SALES_PIPELINE_WRITE].
 * Exactly ONE of the request fields is set, named by [kind]; [leadId] carries the target of a
 * status change. Keeping them as separate typed fields rather than a raw JSON blob means a
 * renamed wire field breaks the build here instead of at the operator's phone.
 */
@Serializable
data class SalesPipelinePayload(
    @SerialName("client_id") val clientId: String,
    @SerialName("kind") val kind: String,
    @SerialName("lead_id") val leadId: String = "",
    @SerialName("buyer_lead") val buyerLead: SalesBuyerLeadWriteDto? = null,
    @SerialName("fpo_lead") val fpoLead: SalesFpoLeadWriteDto? = null,
    @SerialName("lead_status") val leadStatus: SalesLeadStatusWriteDto? = null,
    @SerialName("benchmark") val benchmark: SalesBenchmarkWriteDto? = null,
    @SerialName("sold_tags") val soldTags: SalesSoldTagsWriteDto? = null,
    @SerialName("weight_check") val weightCheck: SalesWeightCheckWriteDto? = null,
)
