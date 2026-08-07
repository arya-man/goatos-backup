package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * ONE row of the vaccination module's own lifecycle alerts feed (GET /app/vaccination/alerts).
 *
 * The twin of [WeighingAlertDto]. It is NOT the vaccination process-integrity / control-tower
 * feed: control tower reports process GAPS, while this reports transitions that already happened
 * and were routed to this person -- a proof approved, a proof sent back for rework, a record
 * closed.
 *
 * Every visible string is BACKEND-OWNED. Branch on [kind] for iconography if you like, but never
 * rebuild the sentence from it.
 */
@Serializable
data class VaccinationAlertDto(
    @SerialName("alert_id") val alertId: String = "",
    @SerialName("kind") val kind: String = "",
    /** "downstream" (landed on the person who must act) or "upstream" (on the person who
     *  oversees). Which way this alert travelled FOR THIS RECIPIENT. */
    @SerialName("direction") val direction: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("body") val body: String = "",
    /** "normal" or "high". */
    @SerialName("severity") val severity: String = "",
    /** In-app destination this row opens, chosen by the backend (e.g. "/vaccination"). */
    @SerialName("target") val target: String = "",
    @SerialName("shed_label") val shedLabel: String? = null,
    @SerialName("drive_id") val driveId: String? = null,
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("occurred_at") val occurredAt: String = "",
)

/** ONE keyset page of the caller's vaccination alerts, newest first. */
@Serializable
data class VaccinationAlertPageResponseDto(
    @SerialName("items") val items: List<VaccinationAlertDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    /** Backend-owned screen title. Render it; do not hardcode a vaccination string -- hardcoding
     *  "Vaccination alerts" in the ViewModel is the recorded copy-firewall violation. */
    @SerialName("title") val title: String = "",
    /** Backend-owned empty-state sentence, shown when [items] is empty. */
    @SerialName("empty_message") val emptyMessage: String = "",
    @SerialName("trace_id") val traceId: String? = null,
)
