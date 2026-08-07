package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.util.Locale
import javax.inject.Inject
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.weighing.WEIGHING_LEADERSHIP_MAX_WINDOW
import sg.mesha.goatos.core.data.weighing.WEIGHING_LEADERSHIP_PAGE_SIZE
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShed
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShedCache
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.weighingCacheAgeNotice
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.weighing.leadership.WeighingShedDetailUiState
import sg.mesha.goatos.feature.weighing.leadership.WeighingShedFieldUiRow
import sg.mesha.goatos.feature.weighing.leadership.WeighingShedRecordUiRow
import sg.mesha.goatos.feature.weighing.shedStateLabel
import sg.mesha.goatos.ui.Routes

/**
 * Leadership's read of ONE shed bucket, rendered FROM ROOM.
 *
 * The screen renders whatever the cache holds — instantly, on re-entry, with no loading wall — and
 * the network refresh only writes into Room, which re-emits. A failed refresh therefore leaves the
 * cached bucket on screen with a staleness notice instead of blanking it.
 *
 * The park, the weigh date and the assignee name are COLUMNS on the cached shed row, because the
 * shed read itself answers them. None of this screen's chrome travels as a route argument any more,
 * so a cold deep link renders a real eyebrow instead of a blank one.
 *
 * Scoped to its own hosted destination, so the shed it renders is the one the route names — it
 * never inherits an operator's capture scope, and it exposes no capture write. The only write is
 * the SAME leadership reopen the task detail already offers.
 */
@HiltViewModel
class WeighingShedDetailViewModel @Inject constructor(
    private val repository: WeighingRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {
    private val campaignId = savedStateHandle.get<String>(Routes.WEIGHING_CAMPAIGN_ARG).orEmpty()
    private val campaignShedId = savedStateHandle.get<String>(Routes.WEIGHING_CAMPAIGN_SHED_ARG).orEmpty()

    /**
     * How many cached records the screen observes. Grows ONE page at a time on scroll and is held
     * to the cache's own ceiling: the observed Room read is a bounded window, never the whole
     * bucket, so the over-fetch cannot move from the network into the database.
     */
    private val recordWindow = MutableStateFlow(WEIGHING_LEADERSHIP_PAGE_SIZE)
    private val loading = MutableStateFlow(false)
    private val busy = MutableStateFlow(false)
    private val message = MutableStateFlow<String?>(null)
    private val failure = MutableStateFlow<String?>(null)

    @OptIn(ExperimentalCoroutinesApi::class)
    private val cache: StateFlow<WeighingLeadershipShedCache> =
        if (campaignId.isBlank() || campaignShedId.isBlank()) {
            flowOf(WeighingLeadershipShedCache())
        } else {
            recordWindow.flatMapLatest { window ->
                repository.observeLeadershipShed(campaignId, campaignShedId, window)
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingLeadershipShedCache())

    val state: StateFlow<WeighingShedDetailUiState> =
        combine(cache, loading, busy, message, failure) { cached, isLoading, isBusy, note, error ->
            cached.toUiState(loading = isLoading, busy = isBusy, message = note, failure = error)
        }.stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            WeighingShedDetailUiState(loading = true),
        )

    init {
        analytics.track(AnalyticsEvents.WEIGHING_SHED_DETAIL_VIEWED)
        refresh()
    }

    /**
     * Re-reads the FIRST page into Room. The cached bucket stays on screen while this runs, and
     * stays on screen if it fails.
     */
    fun refresh() {
        if (campaignId.isBlank() || campaignShedId.isBlank()) return
        if (loading.value) return
        loading.value = true
        recordWindow.value = WEIGHING_LEADERSHIP_PAGE_SIZE
        viewModelScope.launch {
            try {
                when (val result = repository.refreshLeadershipShed(campaignId, campaignShedId, reset = true)) {
                    is AppResult.Ok -> failure.value = null
                    is AppResult.Err -> {
                        failure.value = result.message
                        analytics.track(
                            AnalyticsEvents.WEIGHING_SHED_DETAIL_LOAD_FAILED,
                            mapOf(AnalyticsEvents.Params.REASON to (result.message ?: "unknown"))
                        )
                        crashReporter.recordException(
                            result.cause ?: IllegalStateException(result.message),
                            "weighing shed detail load failed"
                        )
                    }
                }
            } finally {
                loading.value = false
            }
        }
    }

    /**
     * Scroll-driven prefetch: composing a row inside the tail window asks for ONE more page — of
     * the network read AND of the observed Room window together. Never a tappable "Load more", and
     * never a drain loop.
     */
    fun onRecordRowVisible(index: Int) {
        val visible = state.value.records.size
        if (visible == 0 || index < visible - RECORDS_PREFETCH_DISTANCE) return
        if (recordWindow.value < WEIGHING_LEADERSHIP_MAX_WINDOW) {
            recordWindow.value =
                (recordWindow.value + WEIGHING_LEADERSHIP_PAGE_SIZE).coerceAtMost(WEIGHING_LEADERSHIP_MAX_WINDOW)
        }
        if (!cache.value.canLoadMoreRecords) return
        if (loading.value) return
        loading.value = true
        viewModelScope.launch {
            try {
                when (val result = repository.refreshLeadershipShed(campaignId, campaignShedId, reset = false)) {
                    is AppResult.Ok -> failure.value = null
                    is AppResult.Err -> failure.value = result.message
                }
            } finally {
                loading.value = false
            }
        }
    }

    /**
     * The leadership reopen. The ONLY write this screen exposes.
     *
     * No reason is sent: nothing on this screen asks the planner for one, and minting a sentence
     * they never said would put client-authored copy into a durable record. The backend owns what
     * a reopen with no stated reason is recorded as.
     */
    fun reopen() {
        val current = state.value
        if (!current.canReopen || busy.value) return
        busy.value = true
        message.value = null
        analytics.track(AnalyticsEvents.WEIGHING_SHED_DETAIL_REOPEN_ATTEMPTED)
        viewModelScope.launch {
            try {
                when (val result = repository.reopenScope(campaignId, campaignShedId, "")) {
                    is AppResult.Ok -> {
                        message.value = "${current.shedName.ifBlank { "This shed" }} is back with the operator."
                        analytics.track(AnalyticsEvents.WEIGHING_SHED_DETAIL_REOPEN_SUCCEEDED)
                        refresh()
                    }
                    is AppResult.Err -> {
                        failure.value = result.message
                        analytics.track(
                            AnalyticsEvents.WEIGHING_SHED_DETAIL_REOPEN_FAILED,
                            mapOf(AnalyticsEvents.Params.REASON to (result.message ?: "unknown"))
                        )
                        crashReporter.recordException(
                            result.cause ?: IllegalStateException(result.message),
                            "weighing shed detail reopen failed"
                        )
                    }
                }
            } finally {
                busy.value = false
            }
        }
    }

    private fun WeighingLeadershipShedCache.toUiState(
        loading: Boolean,
        busy: Boolean,
        message: String?,
        failure: String?,
    ): WeighingShedDetailUiState {
        val cachedShed = shed
            ?: return WeighingShedDetailUiState(
                found = false,
                loading = loading,
                busy = busy,
                // With nothing cached there is nothing to keep on screen, so the failure IS the
                // answer here rather than a staleness note beside stale rows.
                error = failure,
            )
        val lumpSum = cachedShed.category.equals(LUMP_SUM_CATEGORY, ignoreCase = true)
        val status = cachedShed.status.trim().lowercase(Locale.US)
        // "Not assigned yet" is a business claim, so only the BACKEND may make it: an EMPTY
        // operator id is the one thing that means nobody owns this bucket. A blank display name
        // with an operator actually assigned is a missing label, not an empty assignment.
        val operatorName = cachedShed.operatorDisplayName.trim()
        val operatorValue = when {
            operatorName.isNotBlank() -> operatorName
            cachedShed.operatorUserId.isBlank() -> "Not assigned yet"
            else -> EMPTY_VALUE
        }
        val records = if (lumpSum) {
            emptyList()
        } else {
            cachedShed.animals.map { animal ->
                WeighingShedRecordUiRow(
                    // Key on the backend's own record id. A positional key re-keys every row past
                    // any change when the list is refreshed after a reopen.
                    id = animal.observationId.ifBlank { "${cachedShed.campaignShedId}:${animal.rfid}" },
                    tagLabel = animal.rfid,
                    hasTag = animal.rfid.isNotBlank(),
                    hasVideo = animal.videos.isNotEmpty(),
                    weightLabel = formatWeight(animal.weightKg),
                )
            }
        }
        val fields = buildList {
            add(
                WeighingShedFieldUiRow(
                    label = "Mode",
                    value = if (lumpSum) "Lump-sum" else "Individual",
                    pill = true,
                    pillFg = if (lumpSum) MeshaColors.Purple else MeshaColors.BrandD,
                    pillBg = if (lumpSum) MeshaColors.PurpleX else MeshaColors.Surf3,
                ),
            )
            add(WeighingShedFieldUiRow(label = "Operator", value = operatorValue))
            if (lumpSum) {
                add(WeighingShedFieldUiRow("Total weight", cachedShed.totalWeightKg?.let(::formatWeight) ?: EMPTY_VALUE))
                add(WeighingShedFieldUiRow("Animal count recorded", cachedShed.animalCount?.toString() ?: EMPTY_VALUE))
                add(
                    WeighingShedFieldUiRow(
                        "Average weight",
                        cachedShed.averageWeightKg?.let(::formatWeight) ?: EMPTY_VALUE,
                    ),
                )
                // "N of M": M is the backend's group-video allowance, never a client constant.
                add(
                    WeighingShedFieldUiRow(
                        label = "Shed videos",
                        value = if (cachedShed.maxShedVideos > 0) {
                            "${cachedShed.videos.size} of ${cachedShed.maxShedVideos}"
                        } else {
                            cachedShed.videos.size.toString()
                        },
                    ),
                )
            } else {
                // Counts the records CACHED so far, which is exactly what this screen is showing.
                // The shed read carries no whole-bucket record total, so no denominator is invented.
                add(WeighingShedFieldUiRow("Records captured", records.size.toString()))
                add(WeighingShedFieldUiRow("Videos", records.count { it.hasVideo }.toString()))
            }
            add(
                WeighingShedFieldUiRow(
                    label = "State",
                    value = shedStateLabel(cachedShed.status),
                    pill = true,
                    pillFg = stateFg(status),
                    pillBg = stateBg(status),
                ),
            )
            // The shed's herd estimate. Weighing has no roster, so this is the only sense of how
            // much of the shed the captured records cover. Never a completeness denominator.
            if (cachedShed.estimatedAnimalCount > 0) {
                add(
                    WeighingShedFieldUiRow(
                        "Estimated herd size",
                        "~${cachedShed.estimatedAnimalCount}",
                        faint = true,
                    ),
                )
            }
        }
        return WeighingShedDetailUiState(
            shedName = cachedShed.operationalLocationDisplay.ifBlank { cachedShed.shedName },
            contextLabel = cachedShed.contextLabel(),
            found = true,
            isLumpSum = lumpSum,
            fields = fields,
            records = records,
            // Nothing is held back in the heap any more: another page exists exactly when the
            // cached cursor says so, and scrolling fetches it.
            moreRecords = 0,
            canReopen = status in REOPENABLE_STATUSES,
            stateLabel = shedStateLabel(cachedShed.status),
            // Only a real display name goes in the confirm; with none, the sheet says "its
            // operator" rather than naming somebody the app cannot resolve.
            operatorLabel = operatorName,
            reopenBlockedReason = if (status == "in_progress") "Already back with the operator." else "",
            loading = loading,
            busy = busy,
            message = message,
            // Cached rows are on screen, so a failed refresh is a quiet staleness note beside them
            // rather than an error page that throws the reader out of the bucket.
            // Two independent staleness signals: a failed refresh in THIS session, and the age of
            // the cached answer itself (an offline cold start has no failure to report).
            staleNotice = listOf(
                failure?.let { "Showing the last saved reading. $it" }.orEmpty(),
                weighingCacheAgeNotice(cachedAt),
            ).filter { it.isNotBlank() }.joinToString(" "),
        )
    }

    /** "<Park> · <weigh date>" built from the shed read's OWN answers, never from a route argument. */
    private fun WeighingLeadershipShed.contextLabel(): String {
        val date = runCatching {
            // exception:exempt date display fallback; unparseable date shows raw ISO string
            LocalDate.parse(weighDate, ISO_DATE).format(DAY_FORMAT)
        }.getOrDefault(weighDate)
        return listOf(parkName, date).filter { it.isNotBlank() }.joinToString(" · ")
    }

    private fun stateFg(status: String) = when (status) {
        "closed" -> MeshaColors.Ok
        "completed" -> MeshaColors.Warn
        "in_progress" -> MeshaColors.BrandD
        else -> MeshaColors.Muted
    }

    private fun stateBg(status: String) = when (status) {
        "closed" -> MeshaColors.OkX
        "completed" -> MeshaColors.WarnX
        else -> MeshaColors.Surf3
    }

    private fun formatWeight(value: Double): String =
        String.format(Locale.US, "%.2f kg", value).replace(".00 kg", " kg")

    private companion object {
        const val LUMP_SUM_CATEGORY = "per_shed_partition"
        const val RECORDS_PREFETCH_DISTANCE = 3
        const val EMPTY_VALUE = "—"
        val REOPENABLE_STATUSES = setOf("completed", "closed")
        val ISO_DATE: DateTimeFormatter = DateTimeFormatter.ISO_LOCAL_DATE
        val DAY_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("EEE d MMM", Locale.ENGLISH)
    }
}
