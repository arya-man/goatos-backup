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

/**
 * Where an animal currently sits. [operationalLocationDisplay] is backend-composed copy, rendered
 * verbatim.
 *
 * The `display` alias was retired on 2026-08-06: this payload used to carry the same string under
 * two names, which is how one gets updated and the other silently does not.
 */
@Serializable
data class GoatLocationPathDto(
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("park_name") val parkName: String? = null,
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("shed_name") val shedName: String? = null,
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
)

/**
 * One matched animal. [goatId] is what a shifting write actually carries — the tag the operator
 * scanned is an identifier for a goat, never the goat's own id, so the picker resolves one to the
 * other rather than letting an RFID string reach `goat_ids`.
 *
 * [rowVersion] is the animal's current optimistic-concurrency token, now REQUIRED on `GoatSummary`.
 * The death flow round-trips it from this lookup straight into the death write, so an operator never
 * types a record version by hand — a stale token is caught server-side instead of silently
 * overwriting a concurrent edit.
 */
@Serializable
data class GoatSearchItemDto(
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("display_id") val displayId: String = "",
    @SerialName("animal_identifier_1") val animalIdentifier1: String = "",
    @SerialName("animal_identifier_2") val animalIdentifier2: String? = null,
    @SerialName("breed") val breed: String? = null,
    @SerialName("sex") val sex: String = "",
    @SerialName("age_band") val ageBand: String? = null,
    @SerialName("lifecycle_status") val lifecycleStatus: String = "",
    @SerialName("row_version") val rowVersion: Int = 0,
    @SerialName("location_path") val locationPath: GoatLocationPathDto = GoatLocationPathDto(),
)

@Serializable
data class GoatSearchResponseDto(
    @SerialName("items") val items: List<GoatSearchItemDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)
