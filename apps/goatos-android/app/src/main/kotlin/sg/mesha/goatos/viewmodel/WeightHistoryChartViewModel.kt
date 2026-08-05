package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.network.WeightHistoryResponseDto
import sg.mesha.goatos.core.network.WeightSeriesDto
import sg.mesha.goatos.feature.weighing.WeighingFilterChipUiRow
import sg.mesha.goatos.feature.weighing.WeightHistoryChartUiPoint
import sg.mesha.goatos.feature.weighing.WeightHistoryChartUiRow
import sg.mesha.goatos.feature.weighing.WeightHistoryChartUiState

/**
 * Leadership weight history.
 *
 * The state this emits IS [WeightHistoryChartUiState] from the feature module -- the type the
 * screen renders. It was briefly a second, identically-named class in this package, which
 * compiled fine and could never be passed to the screen; one name, two types, no call site. The
 * screen owns the contract, the ViewModel fills it.
 */
private data class WeightHistoryFilter(
    val captureKind: String? = null,
    val parkId: String? = null,
    val shedId: String? = null,
    val search: String = "",
)

/**
 * Above this many animal series the screen refuses to render a flat list and asks the reader to
 * pick a shed or search a tag first.
 *
 * A park can hold ~100 sheds of ~80 animals. Scrolling 8,000 chart cards to find one animal is not
 * a feature; it is a way of making the answer unreachable while looking busy. The reader always
 * has a cheaper path -- narrow to a shed, or type part of the tag.
 */
private const val MAX_ROWS_WITHOUT_NARROWING = 120

/**
 * Points drawn per series. Each point costs three text-layout passes (value + date labels), and a
 * LazyColumn keeps off-screen cards composed for prefetch, so an animal with a long history would
 * multiply that across every visible card. The most RECENT weigh days are the ones that answer
 * "is this animal growing", so the cap keeps the tail and says so on screen.
 */
private const val MAX_POINTS_PER_SERIES = 20

@HiltViewModel
class WeightHistoryChartViewModel @Inject constructor(
    private val repository: WeighingRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {
    private val loading = MutableStateFlow(false)
    private val error = MutableStateFlow<String?>(null)
    private val data = MutableStateFlow<WeightHistoryResponseDto?>(null)
    private val filterState = MutableStateFlow(WeightHistoryFilter())

    val state: StateFlow<WeightHistoryChartUiState> = combine(
        loading,
        error,
        data,
        filterState,
    ) { isLoading, err, dto, filter ->
        buildState(isLoading, err, dto, filter)
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        WeightHistoryChartUiState(isLoading = true),
    )

    init {
        analytics.track(AnalyticsEvents.WEIGHT_HISTORY_VIEWED)
        refresh()
    }

    fun refresh() {
        if (loading.value) return
        loading.value = true
        error.value = null
        val filter = filterState.value
        val campaignShedId = resolveCampaignShedId(filter.shedId)
        viewModelScope.launch {
            try {
                when (val result = repository.fetchWeightHistory(parkId = filter.parkId, campaignShedId = campaignShedId)) {
                    is AppResult.Ok -> {
                        data.value = result.value
                        error.value = null
                    }
                    is AppResult.Err -> {
                        error.value = result.message
                        data.value = null
                        analytics.track(
                            AnalyticsEvents.WEIGHT_HISTORY_LOAD_FAILED,
                            mapOf(AnalyticsEvents.Params.REASON to (result.message ?: "unknown"))
                        )
                        crashReporter.recordException(
                            result.cause ?: IllegalStateException(result.message),
                            "weight history load failed"
                        )
                    }
                }
            } finally {
                loading.value = false
            }
        }
    }

    /**
     * The shed picker is keyed on LOCATION (see the picker-dedupe comment in [buildState]), but
     * the backend's `campaign_shed_id` query param narrows to exactly ONE weigh-day bucket. A
     * location with more than one weighed bucket (the common case -- a shed weighed on several
     * days) has no single bucket id that would answer "everything at this location", so sending
     * one would silently DROP the other weigh days from the server response. Only narrow the
     * network query when the location maps to exactly one bucket in what is already loaded;
     * otherwise the request stays park-scoped and [matches] keeps doing the location-level
     * narrowing client-side over that (already bounded, park-scoped) payload.
     */
    private fun resolveCampaignShedId(locationId: String?): String? {
        if (locationId == null) return null
        val buckets = data.value?.sheds
            ?.filter { it.location_id == locationId }
            ?.map { it.campaign_shed_id }
            ?.distinct()
            .orEmpty()
        return buckets.singleOrNull()
    }

    fun onSelectKind(kind: String?) {
        // Not a backend query param (GetWeightHistory only accepts park_id/campaign_shed_id) --
        // capture kind narrows client-side only, same as search below.
        filterState.value = filterState.value.copy(captureKind = kind)
    }

    fun onSelectPark(parkId: String?) {
        // Changing park drops the shed selection: a shed chip from the previous park would filter
        // every series away and read as "no weights recorded" rather than "wrong shed selected".
        filterState.value = filterState.value.copy(parkId = parkId, shedId = null)
        refresh()
    }

    fun onSelectShed(shedId: String?) {
        filterState.value = filterState.value.copy(shedId = shedId)
        refresh()
    }

    fun onSearch(query: String) {
        // Tag search stays a client-side filter over whatever is already loaded -- it is not a
        // backend query param, and re-querying the network on every keystroke would be its own
        // unbounded-request problem.
        filterState.value = filterState.value.copy(search = query)
    }

    private fun buildState(
        isLoading: Boolean,
        err: String?,
        dto: WeightHistoryResponseDto?,
        filter: WeightHistoryFilter,
    ): WeightHistoryChartUiState {
        if (dto == null) {
            return WeightHistoryChartUiState(
                isLoading = isLoading,
                errorMessage = err,
                hasData = false,
            )
        }

        // Sheds are park-scoped, so the shed chips must narrow with the park selection. Offering
        // every shed in the tenant under a chosen park invites a filter pair that can only ever
        // return nothing.
        val shedsInScope = dto.sheds.filter { shed ->
            filter.parkId == null || shed.park_id == filter.parkId
        }
        val shedIdsInScope = shedsInScope.map { it.campaign_shed_id }.toSet()

        // A weighing task mints a NEW bucket per shed per weigh day, so one physical shed has as
        // many campaign_shed_ids as it has been weighed. Keyed on the bucket, the picker listed
        // "Godel 1" three times -- three identical rows the reader cannot tell apart, each one
        // silently hiding the other weigh days. The picker is keyed on the LOCATION, which is the
        // shed the reader actually means.
        val locationOfBucket = dto.sheds.associate { it.campaign_shed_id to it.location_id }
        val shedOptions = shedsInScope
            .distinctBy { it.location_id }
            .map { shed ->
                WeighingFilterChipUiRow(
                    id = shed.location_id,
                    label = shed.display_name,
                    selected = filter.shedId == shed.location_id,
                )
            }

        val rows = dto.series
            .filter { series -> matches(series, filter, shedIdsInScope, locationOfBucket) }
            .map { it.toUiRow() }

        val perSeriesCapped = dto.series.any { it.points.size > MAX_POINTS_PER_SERIES }
        val mustNarrow = rows.size > MAX_ROWS_WITHOUT_NARROWING
        val selectedParkName = dto.parks.firstOrNull { it.park_id == filter.parkId }?.name
        val selectedShedName = dto.sheds.firstOrNull { it.location_id == filter.shedId }?.display_name

        return WeightHistoryChartUiState(
            selectedCaptureKind = filter.captureKind,
            parkChips = dto.parks.map { park ->
                WeighingFilterChipUiRow(
                    id = park.park_id,
                    label = park.name,
                    selected = filter.parkId == park.park_id,
                )
            },
            shedChips = shedOptions,
            // The wall replaces the rows; sending both would render the list behind the message.
            rows = if (mustNarrow) emptyList() else rows,
            mustNarrow = mustNarrow,
            matchCount = rows.size,
            selectedParkName = selectedParkName,
            selectedShedName = selectedShedName,
            searchQuery = filter.search,
            hasData = true,
            seriesEmpty = dto.series.isEmpty(),
            errorMessage = err,
            payloadTruncated = dto.truncated,
            perSeriesCapped = perSeriesCapped,
            isLoading = isLoading,
        )
    }

    private fun matches(
        series: WeightSeriesDto,
        filter: WeightHistoryFilter,
        shedIdsInScope: Set<String>,
        locationOfBucket: Map<String, String>,
    ): Boolean {
        if (filter.captureKind != null && series.capture_kind != filter.captureKind) return false
        val query = filter.search.trim()
        if (query.isNotEmpty()) {
            val tag = series.scanned_identifier.orEmpty()
            // Matches ANYWHERE in the tag: an operator reading a tag off an ear reads the last few
            // digits, not the herd prefix every animal shares.
            val hit = tag.contains(query, ignoreCase = true) ||
                series.shed_display_name.contains(query, ignoreCase = true)
            if (!hit) return false
        }
        // Compared by LOCATION id, never by display name: two parks may both run a shed called
        // "Part 1", and one shed spans many weigh-day buckets.
        if (filter.shedId != null && locationOfBucket[series.campaign_shed_id] != filter.shedId) return false
        if (filter.parkId != null && series.campaign_shed_id !in shedIdsInScope) return false
        return true
    }

    private fun WeightSeriesDto.toUiRow(): WeightHistoryChartUiRow {
        val ordered = points.takeLast(MAX_POINTS_PER_SERIES)
        return WeightHistoryChartUiRow(
            label = scanned_identifier?.takeIf { it.isNotBlank() } ?: shed_display_name,
            shedName = shed_display_name,
            captureKind = capture_kind,
            points = ordered.mapNotNull { point ->
                // A lump-sum point carries total_weight_kg, an individual one carries weight_kg.
                // A point carrying NEITHER is dropped rather than drawn as 0.0: a zero bar reads
                // as "this animal weighed nothing", which is a fact the payload never stated.
                val value = when (capture_kind) {
                    KIND_INDIVIDUAL -> point.weight_kg
                    KIND_LUMP_SUM -> point.total_weight_kg
                    else -> null
                } ?: return@mapNotNull null
                WeightHistoryChartUiPoint(
                    dateLabel = point.weigh_date,
                    value = value,
                    sentBack = point.verification_status == STATUS_REJECTED,
                    awaitingCheck = point.verification_status == STATUS_PENDING,
                )
            },
        )
    }

    private companion object {
        // "individual" / "lump_sum" — raw capture-kind tokens from the backend, matched against
        // literal strings on the render side too (see WeightHistoryChartScreen), never translated:
        // the screen owns the display label for these, the ViewModel only ever passes the token.
        const val KIND_INDIVIDUAL = "individual"
        const val KIND_LUMP_SUM = "lump_sum"
        const val STATUS_REJECTED = "rejected"
        const val STATUS_PENDING = "pending"
    }
}
