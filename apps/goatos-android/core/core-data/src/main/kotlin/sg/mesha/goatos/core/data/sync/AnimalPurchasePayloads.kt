package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalCreateRequestDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadCreateRequestDto

/**
 * Outbox payloads and the ONE definition of every Animal-purchases outbox key (maintainer decision
 * 2026-09-13, docs/decisions/animal-purchases.md). Every idempotency key and group key an
 * animal-purchase write uses is built HERE and nowhere else, for the reason ToxinPayloads.kt's
 * header gives.
 *
 * [draftKey] is the form's STABLE draft key — minted ONCE when the form opens and persisted in
 * SavedStateHandle (`DraftIdempotencyKey`), so a retry, a double tap or a process death replays
 * the SAME record instead of recording a second load or a second animal. Never timestamp-suffixed.
 */

/** Group key for one load's create — its own lane; nothing precedes it. */
fun animalPurchaseLoadCreateGroupKey(draftKey: String): String = "animal-purchase:load-create:$draftKey"

/** STABLE per draft; sent VERBATIM as the backend's required `Idempotency-Key`. */
fun animalPurchaseLoadCreateIdempotencyKey(draftKey: String): String = "animal-purchase:load:$draftKey"

/**
 * Group key for ONE animal draft's writes: the video's PROOF_UPLOAD row and the
 * ANIMAL_PURCHASE_ANIMAL_CREATE that references it share this FIFO lane, so the upload drains
 * strictly BEFORE the create that resolves it. The capture path must pass this SAME value as its
 * `uploadGroupKey` when enqueueing the animal's video.
 *
 * PER DRAFT, not per load, on purpose (device-proven 2026-09-13). The outbox holds a lane at its
 * oldest dead row so a shed's Submit can never leapfrog a lost scan -- right for vaccination,
 * where one shed is one lane and every scan matters. On a per-LOAD lane that same rule meant
 * two dead uploads from ABANDONED drafts (recorded offline, never saved, retry budget spent)
 * sat at the head of the load's lane and blocked every later animal's create on that load
 * forever. One animal's video has nothing to do with another animal's save; an orphaned draft
 * may only ever hold itself.
 */
fun animalPurchaseDraftGroupKey(draftKey: String): String = "animal-purchase:animal:$draftKey"

/** STABLE per (draft, proof row): a retry replays for free, while a re-shoot (a new PROOF_UPLOAD
 *  row) is a genuinely different act under a different key. */
fun animalPurchaseAnimalCreateIdempotencyKey(draftKey: String, proofOutboxItemId: String): String =
    "animal-purchase:animal:$draftKey:$proofOutboxItemId"

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.ANIMAL_PURCHASE_LOAD_CREATE]. */
@Serializable
data class AnimalPurchaseLoadCreatePayload(
    @SerialName("draft_key") val draftKey: String,
    @SerialName("request") val request: AnimalPurchaseLoadCreateRequestDto,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.ANIMAL_PURCHASE_ANIMAL_CREATE].
 * The video rides by REFERENCE to its PROOF_UPLOAD outbox row ([proofOutboxItemId]) on the same
 * load group, so the upload drains first and the dispatcher resolves the uploaded server proof id
 * into `video_proof_ref`; [request] therefore carries a blank `video_proof_ref` at rest.
 */
@Serializable
data class AnimalPurchaseAnimalCreatePayload(
    @SerialName("draft_key") val draftKey: String,
    @SerialName("load_id") val loadId: String,
    @SerialName("request") val request: AnimalPurchaseAnimalCreateRequestDto,
    @SerialName("proof_outbox_item_id") val proofOutboxItemId: String,
)
