package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure UI models; MarketSurveyViewModel / MarketCityEntryViewModel (in :app)
// own the market_* AnalyticsEventsMarket + CrashReporter wiring.

/**
 * Market survey (maintainer decision 2026-09-14): the Procurement module's Market tab. One card
 * per configured city for the business day; the reporter opens a card and types the day's goat
 * and sheep prices against the questions the sales desk configured. Every label on a card --
 * city name, question, unit, status -- is BACKEND config rendered verbatim.
 */
data class MarketCityCardUi(
    val cityId: String,
    val cityName: String,
    /** `pending` | `done`, the SERVER's judgement. */
    val status: String,
    val statusLabel: String,
    val statusTone: VendorsTone,
    /** "4 of 6 answered", composed from the server's counts. */
    val progressLine: String,
)

data class MarketSurveyUiState(
    val title: String = "",
    /** The business day the cards are for, rendered DD/MM/YYYY; blank until the first read. */
    val dateLine: String = "",
    val summaryLine: String = "",
    val cards: List<MarketCityCardUi> = emptyList(),
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    /** False for a reader who may see the day but not record it (sales.market.entry). */
    val canRecord: Boolean = false,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
)

sealed interface MarketSurveyEvent {
    data object Refresh : MarketSurveyEvent
    data class OpenCity(val cityId: String) : MarketSurveyEvent
}

/** One question on the entry form: the configured label and unit, and the typed figure. */
data class MarketQuestionFieldUi(
    val questionId: String,
    val label: String,
    val unitLabel: String,
    val value: String,
    val error: String? = null,
)

data class MarketCityEntryUiState(
    val cityName: String = "",
    val dateLine: String = "",
    val fields: List<MarketQuestionFieldUi> = emptyList(),
    val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
    val writeMessage: String = "",
    val closeAfterSave: Boolean = false,
    val submitInFlight: Boolean = false,
    /** True when the card is not in the cached day (config changed, or nothing cached yet). */
    val missing: Boolean = false,
    val canRecord: Boolean = true,
)

sealed interface MarketCityEntryEvent {
    data class PriceChanged(val questionId: String, val value: String) : MarketCityEntryEvent
    data object Save : MarketCityEntryEvent
    data object Back : MarketCityEntryEvent
}
