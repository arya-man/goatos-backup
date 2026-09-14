package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Market survey (maintainer decision 2026-09-14): the morning calls to a handful of markets for
 * goat and sheep prices. The phone renders ONE CARD PER CITY for the business day, every card
 * carrying the questions the sales desk configured (label + the unit the price is quoted in), and
 * the reporter types the answers in. Everything shown is BACKEND-OWNED config rendered verbatim:
 * the client never composes a question, a unit or a status.
 *
 * Wire contract of record: backend/internal/market/adapters/http/handler.go.
 */
@Serializable
data class MarketSurveyCardQuestionDto(
    @SerialName("question_id") val questionId: String,
    /** Backend-owned config, rendered verbatim. */
    @SerialName("label") val label: String,
    /** Backend-owned config ("₹/kg", "₹/500 g"), rendered verbatim. */
    @SerialName("unit_label") val unitLabel: String,
    /** Null until recorded for this day. */
    @SerialName("price") val price: Double? = null,
)

@Serializable
data class MarketSurveyCardDto(
    @SerialName("city_id") val cityId: String,
    @SerialName("city_name") val cityName: String,
    /** `pending` | `done` — SERVER-composed: done only when every question has a price. */
    @SerialName("status") val status: String,
    @SerialName("answered") val answered: Int = 0,
    @SerialName("total") val total: Int = 0,
    @SerialName("questions") val questions: List<MarketSurveyCardQuestionDto> = emptyList(),
)

@Serializable
data class MarketSurveyDayDto(
    @SerialName("business_date") val businessDate: String,
    @SerialName("cards") val cards: List<MarketSurveyCardDto> = emptyList(),
    /** Whole-day counts (grain: city), never page-local. */
    @SerialName("pending") val pending: Int = 0,
    @SerialName("done") val done: Int = 0,
    /** Whether THIS caller holds sales.market.entry — the server's answer, never a role string. */
    @SerialName("can_record") val canRecord: Boolean = false,
    /** Whether the day's calls have opened; before the configured call time [cards] is empty. */
    @SerialName("open") val open: Boolean = true,
    /** The configured call time, "HH:MM" IST — rendered verbatim in the closed state. */
    @SerialName("opens_at") val opensAt: String = "",
)

@Serializable
data class MarketSurveyAnswerDto(
    @SerialName("question_id") val questionId: String,
    @SerialName("price") val price: Double,
)

@Serializable
data class MarketSurveyEntryRequestDto(
    /** Blank means today's IST business day. */
    @SerialName("business_date") val businessDate: String = "",
    @SerialName("answers") val answers: List<MarketSurveyAnswerDto>,
)
