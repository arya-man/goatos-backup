package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import sg.mesha.goatos.core.network.dto.FeedPurchaseWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto
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
