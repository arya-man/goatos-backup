package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * `GET /goats/search` — the scope-filtered goat lookup behind the shifting screen's animal picker.
 *
 * Field names are taken verbatim from `contracts/openapi/app-api.yaml` (`GoatSearchResponse`,
 * `GoatSummary`, `LocationPath`). Only the slice the picker renders is bound; the full
 * `GoatSummary` also carries age band, reproductive status, growth cohort, health status, and
 * warnings that a tag lookup does not need. Every field carries a default so a contract addition
 * never breaks decode.
 *
 * The endpoint is gated on `goat.read`, which every ground-capture role already holds — the picker
 * needs no new permission.
 */

/** Where an animal currently sits. `display` is backend-composed copy, rendered verbatim. */
@Serializable
data class GoatLocationPathDto(
    @SerialName("display") val display: String = "",
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("park_name") val parkName: String? = null,
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("shed_name") val shedName: String? = null,
)

/**
 * One matched animal. [goatId] is what a shifting write actually carries — the tag the operator
 * scanned is an identifier for a goat, never the goat's own id, so the picker resolves one to the
 * other rather than letting an RFID string reach `goat_ids`.
 */
@Serializable
data class GoatSearchItemDto(
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("display_id") val displayId: String = "",
    @SerialName("animal_identifier_1") val animalIdentifier1: String = "",
    @SerialName("animal_identifier_2") val animalIdentifier2: String? = null,
    @SerialName("breed") val breed: String? = null,
    @SerialName("sex") val sex: String = "",
    @SerialName("lifecycle_status") val lifecycleStatus: String = "",
    @SerialName("location_path") val locationPath: GoatLocationPathDto = GoatLocationPathDto(),
)

@Serializable
data class GoatSearchResponseDto(
    @SerialName("items") val items: List<GoatSearchItemDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)
