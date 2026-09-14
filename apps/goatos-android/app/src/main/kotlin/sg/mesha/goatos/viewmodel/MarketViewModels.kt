package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.util.UUID
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsMarket
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.MarketRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.MarketSurveyAnswerDto
import sg.mesha.goatos.core.network.dto.MarketSurveyCardDto
import sg.mesha.goatos.core.network.dto.MarketSurveyDayDto
import sg.mesha.goatos.core.network.dto.MarketSurveyEntryRequestDto
import sg.mesha.goatos.feature.vendors.MarketCityCardUi
import sg.mesha.goatos.feature.vendors.MarketCityEntryEvent
import sg.mesha.goatos.feature.vendors.MarketCityEntryUiState
import sg.mesha.goatos.feature.vendors.MarketQuestionFieldUi
import sg.mesha.goatos.feature.vendors.MarketSurveyEvent
import sg.mesha.goatos.feature.vendors.MarketSurveyUiState
import sg.mesha.goatos.feature.vendors.VendorsTone
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus

/** The cached-day key the Market tab reads: blank means "today as the server last resolved it". */
internal const val MARKET_TODAY_KEY = ""
private const val KEY_TYPED = "market_city_entry.typed"
private const val KEY_TYPED_BUSINESS_DATE = "market_city_entry.typed_business_date"
private const val KEY_TYPED_CITY_ID = "market_city_entry.typed_city_id"
private const val KEY_SAVE_CLIENT_ID = "market_city_entry.save_client_id"
private const val KEY_SAVE_BUSINESS_DATE = "market_city_entry.save_business_date"
private const val KEY_SAVE_CITY_ID = "market_city_entry.save_city_id"
private const val KEY_SAVE_ANSWERS = "market_city_entry.save_answers"

/**
 * The Market tab's L0 (`/vendors/market`, maintainer decision 2026-09-14): today's city cards
 * from the cached day blob, refreshed on open/resume. Every label -- city, status, counts -- is
 * the server's; this only composes the progress sentence from the server's two integers.
 */
@HiltViewModel
class MarketSurveyViewModel @Inject constructor(
    private val repository: MarketRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Local(
        val title: String = "",
        val isRefreshing: Boolean = false,
        val lastSyncedAt: Long? = null,
        val loadError: Boolean = false,
    )

    private val local = MutableStateFlow(Local())

    init {
        analytics.track(AnalyticsEventsMarket.DAY_VIEWED)
        viewModelScope.launch {
            syncRepository.observePendingMarketSurveyRecords().collect { records ->
                repository.applyPendingSurveyRecords(records)
            }
        }
    }

    fun bind(title: String) {
        if (local.value.title == title) return
        local.update { it.copy(title = title) }
    }

    val state: StateFlow<MarketSurveyUiState> = combine(local, repository.observeDay(MARKET_TODAY_KEY)) { l, day ->
        composeDayState(l.title, day, l.isRefreshing, l.lastSyncedAt, l.loadError)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), MarketSurveyUiState())

    fun onEvent(event: MarketSurveyEvent) {
        when (event) {
            MarketSurveyEvent.Refresh -> refresh()
            is MarketSurveyEvent.OpenCity -> analytics.track(AnalyticsEventsMarket.CITY_OPENED)
        }
    }

    private fun refresh() {
        if (local.value.isRefreshing) return
        viewModelScope.launch {
            local.update { it.copy(isRefreshing = true) }
            repository.refreshDay(MARKET_TODAY_KEY)
                .onSuccess {
                    repository.applyPendingSurveyRecords(syncRepository.observePendingMarketSurveyRecords().first())
                    local.update { it.copy(isRefreshing = false, lastSyncedAt = System.currentTimeMillis(), loadError = false) }
                }
                .onFailure { error ->
                    crashReporter.recordException(error, "market day refresh failed")
                    analytics.track(AnalyticsEventsMarket.FAILURE, mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "refresh").take(120)))
                    // Non-blocking: the cached day keeps serving; only an EMPTY cache shows the error.
                    local.update { it.copy(isRefreshing = false, loadError = true) }
                }
        }
    }

    companion object {
        const val EMPTY_NO_CITIES = "No cities on the call list yet. They are added on Sales Config."
        const val EMPTY_LOAD_FAILED = "Could not load today's market calls. Pull to refresh."
        const val EMPTY_NOT_OPEN_PREFIX = "Today's market calls open at "
        const val LABEL_DONE = "Done"
        const val LABEL_PENDING = "Pending"
    }
}

/** Pure composition, unit-tested: the server's day into the tab's rows. */
internal fun composeDayState(
    title: String,
    day: MarketSurveyDayDto?,
    isRefreshing: Boolean,
    lastSyncedAt: Long?,
    loadError: Boolean,
): MarketSurveyUiState {
    val cards = day?.cards.orEmpty().map { card ->
        val done = card.status == "done"
        MarketCityCardUi(
            cityId = card.cityId,
            cityName = card.cityName,
            status = card.status,
            statusLabel = if (done) MarketSurveyViewModel.LABEL_DONE else MarketSurveyViewModel.LABEL_PENDING,
            statusTone = if (done) VendorsTone.OK else VendorsTone.WARN,
            progressLine = "${card.answered} of ${card.total} answered",
        )
    }
    val empty = when {
        cards.isNotEmpty() -> null
        day == null && loadError -> MarketSurveyViewModel.EMPTY_LOAD_FAILED
        // Before the configured call time the server holds the cards back; say when they open
        // (the time is the server's, rendered verbatim) rather than "no cities".
        day != null && !day.open -> MarketSurveyViewModel.EMPTY_NOT_OPEN_PREFIX + day.opensAt
        day != null -> MarketSurveyViewModel.EMPTY_NO_CITIES
        else -> null
    }
    return MarketSurveyUiState(
        title = title,
        dateLine = day?.businessDate?.let(::farmDate).orEmpty(),
        summaryLine = day?.let { if (it.open) "${it.done} of ${it.cards.size} cities done" else "${it.pending} cities to call" }.orEmpty(),
        cards = cards,
        isRefreshing = isRefreshing,
        lastSyncedAt = lastSyncedAt,
        canRecord = day?.canRecord ?: false,
        emptyMessage = empty,
        isErrorEmpty = empty == MarketSurveyViewModel.EMPTY_LOAD_FAILED,
    )
}

/**
 * One city's entry form (`/vendors/market/city/{city_id}`). The questions come from the cached
 * card; the typed figures live in this state holder until Save, which queues ONE outbox write
 * carrying every field that holds a number (prefilled or typed -- what the reporter SAW is what
 * is saved; a blank field is not an answer), overlays them onto the cached card at once, and
 * follows the row to say Saved / still queued / refused.
 */
@HiltViewModel
class MarketCityEntryViewModel @Inject constructor(
    private val savedStateHandle: SavedStateHandle,
    private val repository: MarketRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Local(
        val cityId: String = "",
        /** Typed values by question id; absent = untouched (the cached price shows). */
        val typed: Map<String, String> = emptyMap(),
        val errors: Map<String, String> = emptyMap(),
        val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
        val writeMessage: String = "",
        val closeAfterSave: Boolean = false,
        val submitInFlight: Boolean = false,
    )

    private val local = MutableStateFlow(
        Local(
            cityId = savedStateHandle.get<String>(ARG_CITY_ID).orEmpty(),
            typed = readDraftTyped(savedStateHandle),
        ),
    )

    /** The business date of the cached day the form was composed from; the save names it. */
    @Volatile
    private var businessDate: String = ""

    val state: StateFlow<MarketCityEntryUiState> = combine(local, repository.observeDay(MARKET_TODAY_KEY)) { l, day ->
        day?.businessDate?.let { businessDate = it }
        composeEntryState(day, l.cityId, l.typed, l.errors, l)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), MarketCityEntryUiState())

    private fun composeEntryState(day: MarketSurveyDayDto?, cityId: String, typed: Map<String, String>, errors: Map<String, String>, l: Local): MarketCityEntryUiState {
        val card = day?.cards?.firstOrNull { it.cityId == cityId }
        if (day != null && typed.isNotEmpty() && !typedDraftMatches(day.businessDate, cityId)) {
            clearTypedDraft()
            viewModelScope.launch {
                local.update { it.copy(typed = emptyMap()) }
            }
            return composeEntryState(day, cityId, emptyMap(), errors, l.copy(typed = emptyMap()))
        }
        return MarketCityEntryUiState(
            cityName = card?.cityName.orEmpty(),
            dateLine = day?.businessDate?.let(::farmDate).orEmpty(),
            fields = card.fields(typed, errors),
            writeStatus = l.writeStatus,
            writeMessage = l.writeMessage,
            closeAfterSave = l.closeAfterSave,
            submitInFlight = l.submitInFlight,
            missing = day != null && card == null,
            canRecord = day?.canRecord ?: true,
        )
    }

    fun onEvent(event: MarketCityEntryEvent) {
        when (event) {
            is MarketCityEntryEvent.PriceChanged -> local.update {
                val typed = it.typed + (event.questionId to event.value)
                savedStateHandle[KEY_TYPED] = marketJson.encodeToString(marketTypedSerializer, typed)
                savedStateHandle[KEY_TYPED_BUSINESS_DATE] = businessDate
                savedStateHandle[KEY_TYPED_CITY_ID] = it.cityId
                clearStableSaveClientId()
                it.copy(typed = typed, errors = it.errors - event.questionId)
            }
            MarketCityEntryEvent.Save -> save()
            MarketCityEntryEvent.Back -> Unit
        }
    }

    private fun save() {
        val current = state.value
        val (answers, errors) = parseAnswers(current.fields)
        if (errors.isNotEmpty()) {
            local.update { it.copy(errors = errors) }
            return
        }
        if (answers.isEmpty()) {
            local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_NOTHING_TO_SAVE) }
            return
        }
        // The day the cached card belongs to. Sending it explicitly (rather than blank = "today")
        // keeps a save queued at 23:58 and drained at 00:02 on the morning it was typed for.
        val date = businessDate
        viewModelScope.launch {
            local.update { it.copy(submitInFlight = true) }
            val cityId = local.value.cityId
            val stableSave = stableSave(cityId, date, answers)
            val request = MarketSurveyEntryRequestDto(businessDate = date, answers = stableSave.answers)
            val clientId = stableSave.clientId
            when (val result = syncRepository.enqueueMarketSurveyRecord(clientId, cityId, date, request)) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsMarket.CITY_QUEUED)
                    repository.applyLocalAnswers(date, cityId, stableSave.answers)
                    clearTypedDraft()
                    local.update {
                        it.copy(
                            typed = emptyMap(),
                            submitInFlight = false,
                            writeStatus = VendorsWriteStatus.QUEUED,
                            writeMessage = MESSAGE_SAVING,
                        )
                    }
                    followWrite(result.value, date)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "market survey enqueue failed") }
                    analytics.track(AnalyticsEventsMarket.FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)))
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_NOT_SAVED) }
                }
            }
        }
    }

    init {
        viewModelScope.launch {
            syncRepository.observePendingMarketSurveyRecords().collect { records ->
                repository.applyPendingSurveyRecords(records)
                val currentDate = businessDate
                val cityId = local.value.cityId
                val active = records.lastOrNull { it.cityId == cityId && (currentDate.isBlank() || it.businessDate == currentDate) }
                if (active != null) {
                    savedStateHandle[KEY_SAVE_CLIENT_ID] = active.clientId
                    savedStateHandle[KEY_SAVE_CITY_ID] = active.cityId
                    savedStateHandle[KEY_SAVE_BUSINESS_DATE] = active.businessDate
                    savedStateHandle[KEY_SAVE_ANSWERS] = marketJson.encodeToString(marketAnswersSerializer, active.request.answers)
                }
            }
        }
    }

    private fun followWrite(itemId: String, businessDate: String) {
        viewModelScope.launch {
            syncRepository.followQueuedWrite(itemId).collect { outcome ->
                analytics.track(
                    AnalyticsEventsMarket.WRITE_OUTCOME,
                    mapOf(AnalyticsEvents.Params.REASON to outcome::class.simpleName.orEmpty().lowercase()),
                )
                if (outcome is QueuedWriteOutcome.Rejected) {
                    repository.refreshDayAndTodayAliasIfCurrent(businessDate)
                }
                local.update {
                    when (outcome) {
                        QueuedWriteOutcome.Saved -> {
                            clearStableSaveClientId()
                            it.copy(writeStatus = VendorsWriteStatus.SYNCED, writeMessage = MESSAGE_SAVED, closeAfterSave = true)
                        }
                        QueuedWriteOutcome.StillQueued -> it.copy(writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_QUEUED, closeAfterSave = true)
                        is QueuedWriteOutcome.Rejected -> {
                            clearStableSaveClientId()
                            it.copy(
                                writeStatus = VendorsWriteStatus.FAILED,
                                writeMessage = outcome.reason?.takeIf { r -> r.isNotBlank() } ?: MESSAGE_NOT_SAVED,
                            )
                        }
                    }
                }
            }
        }
    }

    companion object {
        const val ARG_CITY_ID = "city_id"
        const val MESSAGE_SAVING = "Saving prices…"
        const val MESSAGE_SAVED = "Prices saved."
        const val MESSAGE_QUEUED = "Prices saved on the phone. They reach the farm when the phone is online."
        const val MESSAGE_NOT_SAVED = "Could not save the prices. Check the figures and try again."
        const val MESSAGE_NOTHING_TO_SAVE = "Enter at least one price before saving."
        const val ERROR_NOT_A_NUMBER = "Enter a number"
        const val ERROR_NEGATIVE = "Cannot be negative"
    }

    private fun typedDraftMatches(businessDate: String, cityId: String): Boolean =
        savedStateHandle.get<String>(KEY_TYPED_BUSINESS_DATE) == businessDate &&
            savedStateHandle.get<String>(KEY_TYPED_CITY_ID) == cityId

    private fun clearTypedDraft() {
        savedStateHandle[KEY_TYPED] = marketJson.encodeToString(marketTypedSerializer, emptyMap())
        savedStateHandle[KEY_TYPED_BUSINESS_DATE] = ""
        savedStateHandle[KEY_TYPED_CITY_ID] = ""
    }

    private data class StableMarketSave(
        val clientId: String,
        val answers: List<MarketSurveyAnswerDto>,
    )

    private fun stableSave(
        cityId: String,
        businessDate: String,
        answers: List<MarketSurveyAnswerDto>,
    ): StableMarketSave {
        val existing = savedStateHandle.get<String>(KEY_SAVE_CLIENT_ID)
        val existingCity = savedStateHandle.get<String>(KEY_SAVE_CITY_ID)
        val existingDate = savedStateHandle.get<String>(KEY_SAVE_BUSINESS_DATE)
        if (!existing.isNullOrBlank() && existingCity == cityId && existingDate == businessDate) {
            val savedAnswers = readSaveAnswers(savedStateHandle).takeIf { it.isNotEmpty() } ?: answers
            return StableMarketSave(existing, savedAnswers)
        }
        val next = UUID.randomUUID().toString()
        savedStateHandle[KEY_SAVE_CLIENT_ID] = next
        savedStateHandle[KEY_SAVE_CITY_ID] = cityId
        savedStateHandle[KEY_SAVE_BUSINESS_DATE] = businessDate
        savedStateHandle[KEY_SAVE_ANSWERS] = marketJson.encodeToString(marketAnswersSerializer, answers)
        return StableMarketSave(next, answers)
    }

    private fun clearStableSaveClientId() {
        savedStateHandle[KEY_SAVE_CLIENT_ID] = ""
        savedStateHandle[KEY_SAVE_CITY_ID] = ""
        savedStateHandle[KEY_SAVE_BUSINESS_DATE] = ""
        savedStateHandle[KEY_SAVE_ANSWERS] = ""
    }
}

/** The fields the form shows: the cached price unless the reporter typed over it. */
internal fun MarketSurveyCardDto?.fields(typed: Map<String, String>, errors: Map<String, String>): List<MarketQuestionFieldUi> =
    this?.questions.orEmpty().map { q ->
        MarketQuestionFieldUi(
            questionId = q.questionId,
            label = q.label,
            unitLabel = q.unitLabel,
            value = typed[q.questionId] ?: q.price?.let(::plainNumber).orEmpty(),
            error = errors[q.questionId],
        )
    }

/**
 * Blank fields are NOT sent (a card may be answered in two sittings); a field that holds text
 * but not a number, or a negative, is refused on the phone before anything is queued.
 */
internal fun parseAnswers(fields: List<MarketQuestionFieldUi>): Pair<List<MarketSurveyAnswerDto>, Map<String, String>> {
    // One pass over the card's fields (bounded by the configured question count), each mapped to
    // either an answer or the error naming why it is not one; blank fields map to neither.
    val parsed = fields
        .filter { it.value.isNotBlank() }
        .map { field ->
            val number = field.value.trim().replace(",", "").toDoubleOrNull()
            field.questionId to when {
                number == null || number.isNaN() -> Result.failure<Double>(IllegalArgumentException(MarketCityEntryViewModel.ERROR_NOT_A_NUMBER))
                number < 0 -> Result.failure(IllegalArgumentException(MarketCityEntryViewModel.ERROR_NEGATIVE))
                else -> Result.success(number)
            }
        }
    val answers = parsed.mapNotNull { (id, r) -> r.getOrNull()?.let { MarketSurveyAnswerDto(questionId = id, price = it) } }
    val errors = parsed.mapNotNull { (id, r) -> r.exceptionOrNull()?.message?.let { id to it } }.toMap()
    return answers to errors
}

private fun plainNumber(value: Double): String =
    if (value == Math.floor(value) && value < 1e15) value.toLong().toString() else value.toString()

private val marketJson = Json { ignoreUnknownKeys = true }
private val marketTypedSerializer = MapSerializer(String.serializer(), String.serializer())
private val marketAnswersSerializer = ListSerializer(MarketSurveyAnswerDto.serializer())

private fun readDraftTyped(savedStateHandle: SavedStateHandle): Map<String, String> =
    savedStateHandle.get<String>(KEY_TYPED)?.let { raw ->
        // exception:exempt corrupt saved draft payload is discarded and the form repopulates from the cached/server market day.
        runCatching { marketJson.decodeFromString(marketTypedSerializer, raw) }.getOrNull()
    }.orEmpty()

private fun readSaveAnswers(savedStateHandle: SavedStateHandle): List<MarketSurveyAnswerDto> =
    savedStateHandle.get<String>(KEY_SAVE_ANSWERS)?.let { raw ->
        // exception:exempt corrupt pending-save payload drops the in-flight overlay; durable outbox/server state remains authoritative.
        runCatching { marketJson.decodeFromString(marketAnswersSerializer, raw) }.getOrNull()
    }.orEmpty()
