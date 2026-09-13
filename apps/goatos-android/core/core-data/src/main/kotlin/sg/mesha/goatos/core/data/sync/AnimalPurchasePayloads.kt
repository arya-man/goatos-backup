package sg.mesha.goatos.core.data.sync

import java.security.MessageDigest
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

/**
 * STABLE per (draft, the exact set of proof rows): a retry replays for free, while a re-shoot or
 * an added/removed capture (a different set of PROOF_UPLOAD rows) is a genuinely different act
 * under a different key. The set is folded to a short digest so the key stays bounded however
 * many captures the questionnaire's slots hold; order-insensitive, so the same captures always
 * fold to the same key.
 */
fun animalPurchaseAnimalCreateIdempotencyKey(draftKey: String, proofOutboxItemIds: Collection<String>): String {
    val joined = proofOutboxItemIds.map { it.trim() }.filter { it.isNotBlank() }.sorted().joinToString("\u0000")
    val digest = MessageDigest.getInstance("SHA-256").digest(joined.toByteArray(Charsets.UTF_8))
    val hex = digest.joinToString("") { "%02x".format(it) }.take(PROOF_SET_DIGEST_CHARS)
    return "animal-purchase:animal:$draftKey:$hex"
}

private const val PROOF_SET_DIGEST_CHARS = 24

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.ANIMAL_PURCHASE_LOAD_CREATE]. */
@Serializable
data class AnimalPurchaseLoadCreatePayload(
    @SerialName("draft_key") val draftKey: String,
    @SerialName("request") val request: AnimalPurchaseLoadCreateRequestDto,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.ANIMAL_PURCHASE_ANIMAL_CREATE].
 * Every capture rides by REFERENCE to its own PROOF_UPLOAD outbox row, keyed by questionnaire
 * SLOT in capture order ([proofOutboxItemIds]), all on the same draft lane, so every upload
 * drains first and the dispatcher resolves each uploaded server proof id into `media[slot]`;
 * [request] therefore carries an empty `media` map at rest.
 */
@Serializable
data class AnimalPurchaseAnimalCreatePayload(
    @SerialName("draft_key") val draftKey: String,
    @SerialName("load_id") val loadId: String,
    @SerialName("request") val request: AnimalPurchaseAnimalCreateRequestDto,
    @SerialName("proof_outbox_item_ids") val proofOutboxItemIds: Map<String, List<String>> = emptyMap(),
) {
    /** Every referenced upload row, slot order then capture order. */
    val allProofOutboxItemIds: List<String> get() = proofOutboxItemIds.values.flatten()
}
