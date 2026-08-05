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
import sg.mesha.goatos.core.network.GrowthSummaryDto
import sg.mesha.goatos.feature.weighing.WeighingGrowthHeadlineUi
import sg.mesha.goatos.feature.weighing.WeighingGrowthShedUi
import sg.mesha.goatos.feature.weighing.WeighingGrowthTrendUi
import sg.mesha.goatos.feature.weighing.WeighingGrowthUiState

/**
 * Leadership growth (ADG) from WEIGHING DATA ONLY.
 *
 * Emits raw values and counts; every user-visible string is resolved in the Composable via
 * stringResource, so this screen ships in en/hi/kn/te like the rest of the app.
 *
 * The one rule worth restating: a null ADG means NOT COMPUTABLE (the tag has only ever been
 * weighed once) and must travel as null all the way to the renderer. Substituting 0.0 anywhere in
 * this file would turn "we cannot know" into "the herd is not growing".
 */
@HiltViewModel
class WeighingGrowthViewModel @Inject constructor(
    private val repository: WeighingRepository,
    private val crashReporter: CrashReporter,
    private val analytics: AnalyticsPort,
) : ViewModel() {

    private val loading = MutableStateFlow(false)
    private val error = MutableStateFlow<String?>(null)
    private val data = MutableStateFlow<GrowthSummaryDto?>(null)
    private val selectedParkId = MutableStateFlow<String?>(null)
    /** Park id -> display name, learned from the shed leaderboard as answers arrive. */
    private val parkNames = MutableStateFlow<Map<String, String>>(emptyMap())
    private val showLosing = MutableStateFlow(false)

    val state: StateFlow<WeighingGrowthUiState> = combine(
        loading, error, data, selectedParkId, parkNames, showLosing,
    ) { values ->
        @Suppress("UNCHECKED_CAST")
        buildState(
            isLoading = values[0] as Boolean,
            err = values[1] as String?,
            dto = values[2] as GrowthSummaryDto?,
            parkId = values[3] as String?,
            names = values[4] as Map<String, String>,
            expandLosing = values[5] as Boolean,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        WeighingGrowthUiState(isLoading = true),
    )

    init {
        analytics.track(AnalyticsEvents.WEIGHING_GROWTH_VIEWED)
        refresh()
    }

    fun refresh() {
        if (loading.value) return
        loading.value = true
        error.value = null
        viewModelScope.launch {
            try {
                // parkId null = every park this person may see; the backend applies their scope.
                when (
                    val result = repository.fetchGrowthSummary(
                        parkId = selectedParkId.value,
                        from = null,
                        to = null,
                    )
                ) {
                    is AppResult.Ok -> {
                        data.value = result.value
                        // REAL park names from the payload. Never a positional label like
                        // "Park 1" -- a made-up name is indistinguishable from a real one on
                        // screen, and the reader would scope the whole herd figure by it.
                        val seen = parkNames.value.toMutableMap()
                        result.value.parks.forEach { park ->
                            if (park.park_id.isNotBlank() && park.name.isNotBlank()) {
                                seen[park.park_id] = park.name
                            }
                        }
                        parkNames.value = seen
                        error.value = null
                    }
                    is AppResult.Err -> {
                        // Never swallowed: a failure a reader can see AND a non-fatal we can find.
                        error.value = result.message
                        analytics.track(
                            AnalyticsEvents.WEIGHING_GROWTH_LOAD_FAILED,
                            mapOf(AnalyticsEvents.Params.REASON to (result.message ?: "unknown"))
                        )
                        crashReporter.recordException(
                            result.cause ?: IllegalStateException(result.message),
                            "WeighingGrowthViewModel.refresh",
                        )
                    }
                }
            } finally {
                loading.value = false
            }
        }
    }

    /** Toggles the inline list behind the losing-weight tile. */
    fun onToggleLosing() {
        showLosing.value = !showLosing.value
    }

    fun onSelectPark(parkId: String?) {
        if (selectedParkId.value == parkId) return
        selectedParkId.value = parkId
        refresh()
    }

    private fun buildState(
        isLoading: Boolean,
        err: String?,
        dto: GrowthSummaryDto?,
        parkId: String?,
        names: Map<String, String>,
        expandLosing: Boolean,
    ): WeighingGrowthUiState {
        if (dto == null) {
            return WeighingGrowthUiState(
                isLoading = isLoading,
                hasError = err != null,
                errorText = err.orEmpty(),
            )
        }
        val h = dto.headline
        return WeighingGrowthUiState(
            headline = WeighingGrowthHeadlineUi(
                medianAdgGPerDay = h.median_adg_g_per_day,
                deltaGPerDay = h.delta_g_per_day,
                positivePercent = h.positive_adg_percent,
                // The ANIMAL count, matching the list this tile opens. The pair count
                // (negative_adg_count) answers a different question and would contradict it.
                negativeCount = h.losing_animal_count,
                animalsWithTwoPlusWeighs = dto.eligibility.animals_with_two_plus_weighs,
                totalAnimalsWeighed = dto.eligibility.total_animals_weighed,
            ),
            // A trend point with no computable median is DROPPED, not plotted at zero: an absent
            // measurement is not a measurement of nothing.
            trend = dto.trend.mapNotNull { point ->
                point.median_adg_g_per_day?.let {
                    WeighingGrowthTrendUi(label = shortDate(point.week_start), medianAdgGPerDay = it)
                }
            },
            // SORTED here, because the heading promises "best to worst" and the backend returns
            // them in query order. Sheds with no computable growth sink to the bottom rather than
            // being ranked as if zero.
            sheds = dto.shed_leaderboard.sortedWith(
                compareByDescending<sg.mesha.goatos.core.network.GrowthShedDto> { it.median_adg_g_per_day != null }
                    .thenByDescending { it.median_adg_g_per_day ?: Double.NEGATIVE_INFINITY },
            ).map { shed ->
                WeighingGrowthShedUi(
                    locationId = shed.location_id,
                    displayName = shed.display_name,
                    animalCount = shed.n,
                    medianAdgGPerDay = shed.median_adg_g_per_day,
                    medianWeightKg = shed.median_weight_kg,
                )
            },
            parkOptions = names.entries
                .sortedBy { it.value }
                .map { (id, name) ->
                    sg.mesha.goatos.feature.weighing.WeighingFilterChipUiRow(
                        id = id,
                        label = name,
                        selected = parkId == id,
                    )
                },
            selectedParkId = parkId,
            herdMedianAdgGPerDay = h.median_adg_g_per_day,
            // Group-weighed sheds travel in their own list. A lump-sum weighing yields a shed
            // average and a head count -- never an animal's growth -- so it must never be folded
            // into the ADG figures above.
            lumpSumSheds = dto.lump_sum.shed_week_trend.map { point ->
                sg.mesha.goatos.feature.weighing.WeighingGrowthLumpSumUi(
                    locationId = point.location_id,
                    displayName = point.display_name,
                    averageWeightKg = point.average_weight_kg,
                    headCount = point.head_count,
                    weekLabel = shortDate(point.week_start),
                )
            },
            losingAnimals = dto.losing_animals.map { a ->
                sg.mesha.goatos.feature.weighing.WeighingGrowthLosingUi(
                    scannedIdentifier = a.scanned_identifier,
                    shedName = a.shed_display_name,
                    previousWeightKg = a.previous_weight_kg,
                    latestWeightKg = a.latest_weight_kg,
                    adgGPerDay = a.adg_g_per_day,
                )
            },
            showLosing = expandLosing,
            unverifiedCount = h.unverified_observation_count,
            isLoading = isLoading,
            hasError = err != null,
            errorText = err.orEmpty(),
        )
    }

    /** "2026-07-06" -> "6 Jul". Pure formatting; no locale-specific words. */
    private fun shortDate(iso: String): String {
        val parts = iso.split("-")
        if (parts.size != 3) return iso
        val month = MONTHS.getOrNull(parts[1].toIntOrNull()?.minus(1) ?: -1) ?: return iso
        return "${parts[2].trimStart('0')} $month"
    }

    private companion object {
        val MONTHS = listOf("Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec")
    }
}
