package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import sg.mesha.goatos.core.network.dto.MarketSurveyEntryRequestDto

// ---------------------------------------------------------------------------------------------
// Market survey (maintainer decision 2026-09-14): a city's morning prices, recorded on the phone.
// ---------------------------------------------------------------------------------------------

/** One lane per (city, day): two sittings on the same card drain in the order they were saved. */
fun marketSurveyGroupKey(cityId: String, businessDate: String): String = "market:survey:$businessDate:$cityId"

/**
 * STABLE per client id; sent VERBATIM as the backend's required `Idempotency-Key`. A retry of the
 * same save replays for free; a second save on the same card is a new client id, because the
 * backend keys idempotency on the PAYLOAD fingerprint and the second sitting carries new figures.
 */
fun marketSurveyIdempotencyKey(clientId: String): String = "market:survey:$clientId"

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.MARKET_SURVEY_RECORD]. */
@Serializable
data class MarketSurveyRecordPayload(
    @SerialName("client_id") val clientId: String,
    @SerialName("city_id") val cityId: String,
    /** The resolved business date the card was opened for, so the reconcile updates that day. */
    @SerialName("business_date") val businessDate: String,
    @SerialName("request") val request: MarketSurveyEntryRequestDto,
)
