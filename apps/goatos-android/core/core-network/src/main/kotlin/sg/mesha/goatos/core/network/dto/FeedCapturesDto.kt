package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * `GET /feed-direction/distribution/captures` — which of a pen-session's three proof slots are
 * ALREADY recorded, by ANY operator.
 *
 * A pen-session's three proofs (feed weight photo, feed video, water video) may be shot by THREE
 * DIFFERENT operators on three phones. Before this read a proof was discoverable only on the device
 * that shot it, so the others could not tell a slot was done AND no single phone held all three
 * references — the pen could not be submitted at all.
 *
 * Carries no media url and no uploader name by design: the footage stays a verifier surface.
 */
@Serializable
data class FeedDistributionCapturesDto(
    @SerialName("items") val items: List<FeedDistributionCapturedSlotDto> = emptyList(),
)

@Serializable
data class FeedDistributionCapturedSlotDto(
    /** The slot this proof fills — the same `field_key` this app sends when capturing. */
    @SerialName("field_key") val fieldKey: String,
    /**
     * The SERVER proof id.
     *
     * This is what makes a split pen-session submittable: a phone that did not shoot this proof has
     * no local outbox row for it, so it cannot resolve a reference the usual way. It sends this id
     * verbatim instead.
     */
    @SerialName("proof_ref") val proofRef: String,
    @SerialName("captured_at") val capturedAt: String,
    @SerialName("mime_type") val mimeType: String? = null,
    /**
     * The display name of the operator who captured this proof.
     * May be empty if the uploader's workforce record was not found.
     */
    @SerialName("captured_by_name") val capturedByName: String = "",
)
