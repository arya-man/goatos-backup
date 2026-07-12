package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.data.VaccinationInsightsRepository
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationCoverageResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationGapsResponseDto
import sg.mesha.goatos.feature.leadership.DataGapPill
import sg.mesha.goatos.feature.leadership.DecisionRow
import sg.mesha.goatos.feature.leadership.KpiTile
import sg.mesha.goatos.feature.leadership.LeadershipEvent
import sg.mesha.goatos.feature.leadership.LeadershipUiState
import sg.mesha.goatos.feature.leadership.Tone
import sg.mesha.goatos.ui.GapRow
import sg.mesha.goatos.ui.GivenRow
import sg.mesha.goatos.ui.leadershipPlaceholder
import sg.mesha.goatos.ui.sampleLeadershipState
import javax.inject.Inject

/**
 * Leadership overview state holder — offline-first cache-first pattern (see CalendarViewModel).
 * Room is the UI's single source of truth:
 *  - [state] is fed by [ControlTowerRepository.observeSummary]; [refresh] drives
 *    [ControlTowerRepository.refreshSummary].
 *  - the data-gaps and doses-given drill overlays are fed by
 *    [VaccinationInsightsRepository.observeGaps] / [VaccinationInsightsRepository.observeCoverage]
 *    (collected in [init]); [loadGaps] / [loadDosesGiven] drive the matching refreshX when their
 *    sheet opens (the collectors keep [gapsState] / [dosesState] populated from Room, so the
 *    sheet renders cached rows instantly — never a blank/loading wall when a cache exists).
 * Every observe collector emits instantly from Room and re-emits after a successful refresh; a
 * refresh failure keeps cached data on screen and flips isOffline, and a failure with no cache
 * ever observed shows an honest error state. The DTO -> UiState mappings are unchanged.
 *
 * Note: adherence is intentionally NOT a source here — process-integrity coverage is derived
 * from the control-tower summary alone (see [toLeadershipUiState]); the adherence % flip-flopped
 * the hero across reloads and was dropped as a coverage source, so no AdherenceRepository read
 * is wired.
 */
@HiltViewModel
class LeadershipViewModel @Inject constructor(
    private val controlTower: ControlTowerRepository,
    private val insights: VaccinationInsightsRepository,
) : ViewModel() {

    private var gapsNextCursor: String? = null

    // Upstream Room flows, lifecycle-aware via WhileSubscribed(5_000)
    private val observedSummaryResource: StateFlow<Resource<ControlTowerResponseDto>> =
        controlTower.observeSummary().stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null)
        )

    private val observedGapsResource: StateFlow<Resource<VaccinationGapsResponseDto>> =
        insights.observeGaps(limit = GAPS_LIMIT).stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null)
        )

    private val observedCoverageResource: StateFlow<Resource<VaccinationCoverageResponseDto>> =
        insights.observeCoverage(limit = COVERAGE_LIMIT).stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null)
        )

    // Transient flags for manual updates
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _gapsIsRefreshing = MutableStateFlow(false)
    private val _gapsIsLoading = MutableStateFlow(false)
    private val _gapsErrorMessage = MutableStateFlow<String?>(null)
    private val _gapsIsOffline = MutableStateFlow(false)
    private val _gapsIsLoadingMore = MutableStateFlow(false)
    private val _dosesIsRefreshing = MutableStateFlow(false)
    private val _dosesIsLoading = MutableStateFlow(false)
    private val _dosesErrorMessage = MutableStateFlow<String?>(null)
    private val _dosesIsOffline = MutableStateFlow(false)

    // Main state: combines observed summary with transient flags; lifecycle-aware
    val state: StateFlow<LeadershipUiState> = combine(
        observedSummaryResource,
        _isRefreshing,
        _isOffline
    ) { resource, isRefreshing, isOffline ->
        val dto = resource.data
        val base = dto?.toLeadershipUiState()
            ?: if (resource.hasData) leadershipPlaceholder("No overview data") else leadershipPlaceholder("Loading overview…")
        base.copy(
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        leadershipPlaceholder("Loading overview…")
    )

    // Gaps overlay state: combines observed gaps with transient flags.
    // 6 flows > Kotlin's typed combine limit (5), so use the vararg Array<*> form and cast.
    @Suppress("UNCHECKED_CAST")
    val gapsState: StateFlow<OverlayLoadState<GapRow>> = combine(
        observedGapsResource,
        _gapsIsRefreshing,
        _gapsIsLoading,
        _gapsErrorMessage,
        _gapsIsOffline,
        _gapsIsLoadingMore
    ) { values: Array<Any?> ->
        val resource = values[0] as Resource<VaccinationGapsResponseDto>
        val isRefreshing = values[1] as Boolean
        val isLoading = values[2] as Boolean
        val errorMessage = values[3] as String?
        val isOffline = values[4] as Boolean
        val isLoadingMore = values[5] as Boolean
        val dto = resource.data
        gapsNextCursor = dto?.nextCursor  // Update pagination cursor for loadMoreGaps()
        val items = dto?.toGapRows() ?: emptyList()
        OverlayLoadState(
            items = items,
            isLoading = if (resource.hasData) false else isLoading,
            errorMessage = if (resource.hasData) null else errorMessage,
            hasMore = !dto?.nextCursor.isNullOrBlank(),
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt,
            isOffline = if (resource.hasData) false else isOffline,
            isLoadingMore = isLoadingMore,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        OverlayLoadState()
    )

    // Doses overlay state: combines observed coverage with transient flags
    val dosesState: StateFlow<OverlayLoadState<GivenRow>> = combine(
        observedCoverageResource,
        _dosesIsRefreshing,
        _dosesIsLoading,
        _dosesErrorMessage,
        _dosesIsOffline
    ) { resource, isRefreshing, isLoading, errorMessage, isOffline ->
        val dto = resource.data
        val items = dto?.toGivenRows() ?: emptyList()
        OverlayLoadState(
            items = items,
            isLoading = if (resource.hasData) false else isLoading,
            errorMessage = if (resource.hasData) null else errorMessage,
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt,
            isOffline = if (resource.hasData) false else isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        OverlayLoadState()
    )

    init {
        refresh()
    }

    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        val summaryResult = controlTower.refreshSummary()
        _isRefreshing.value = false
        _isOffline.value = summaryResult.isFailure
    }

    fun onEvent(event: LeadershipEvent) {
        when (event) {
            LeadershipEvent.Refresh -> refresh()
            // Everything else is either navigation (routed by the host) or not yet backed.
            else -> Unit
        }
    }

    /**
     * Data-gaps overlay — cache-first. The [observedGapsResource] keeps [gapsState] fed from
     * Room, so the sheet renders cached gaps instantly on open; this only drives the background
     * [VaccinationInsightsRepository.refreshGaps] upsert (Room re-emits via the flow).
     * A refresh failure keeps cached gaps on screen and flips isOffline; a failure with no cache
     * ever observed shows an honest error. Preserves the trigger point — still called when the
     * sheet opens.
     */
    fun loadGaps() = viewModelScope.launch {
        // Blocking loading state only on a cold cache; when cache exists keep it and refresh silently.
        val hasData = gapsState.value.items.isNotEmpty() || gapsState.value.lastSyncedAt != null
        _gapsIsRefreshing.value = true
        _gapsIsLoading.value = !hasData
        _gapsErrorMessage.value = null

        val result = insights.refreshGaps(limit = GAPS_LIMIT)
        _gapsIsRefreshing.value = false
        _gapsIsLoading.value = false

        when {
            result.isSuccess -> _gapsIsOffline.value = false
            // A cache was already rendered — keep it on screen, just surface offline.
            hasData -> _gapsIsOffline.value = true
            // Never cached, ever: no fallback, so the honest error is the only truthful thing.
            else -> {
                _gapsIsOffline.value = true
                _gapsErrorMessage.value = result.exceptionOrNull()?.message ?: "Couldn't load data gaps."
            }
        }
    }

    /** Appends one bounded keyset page into the same Room-backed gaps cache. */
    fun loadMoreGaps() = viewModelScope.launch {
        val cursor = gapsNextCursor ?: return@launch
        if (_gapsIsLoadingMore.value) return@launch
        _gapsIsLoadingMore.value = true
        val result = insights.appendGaps(cursor = cursor, limit = GAPS_LIMIT)
        _gapsIsLoadingMore.value = false
        if (result.isFailure) {
            _gapsIsOffline.value = true
        }
    }

    /**
     * Doses-given (per-vaccine coverage) overlay — cache-first, mirrors [loadGaps].
     * The [observedCoverageResource] keeps [dosesState] fed from Room; this drives the
     * background [VaccinationInsightsRepository.refreshCoverage] upsert. Preserves the trigger
     * point — still called when the sheet opens.
     */
    fun loadDosesGiven() = viewModelScope.launch {
        val hasData = dosesState.value.items.isNotEmpty() || dosesState.value.lastSyncedAt != null
        _dosesIsRefreshing.value = true
        _dosesIsLoading.value = !hasData
        _dosesErrorMessage.value = null

        val result = insights.refreshCoverage(limit = COVERAGE_LIMIT)
        _dosesIsRefreshing.value = false
        _dosesIsLoading.value = false

        when {
            result.isSuccess -> _dosesIsOffline.value = false
            hasData -> _dosesIsOffline.value = true
            else -> {
                _dosesIsOffline.value = true
                _dosesErrorMessage.value = result.exceptionOrNull()?.message ?: "Couldn't load vaccination coverage."
            }
        }
    }

    private fun ControlTowerResponseDto.toLeadershipUiState(): LeadershipUiState {
        val base = sampleLeadershipState()
        // Process integrity is ONE deterministic value from the control-tower summary (the
        // hero's own source): intact → 100, else reduced by open-gap count. It must NOT be
        // prefixed with the adherence % — that is a different metric that intermittently
        // returned null, which flipped the hero between ~39% and 100% across reloads.
        val coverage = if (summary.processIntact) 100 else (100 - summary.openGapCount).coerceIn(0, 100)
        val decisions = alerts.map { alert ->
            DecisionRow(
                id = alert.obligationId.ifBlank { alert.rowId },
                title = alert.title,
                subtitle = alert.detail,
                actionLabel = alert.nextAction.ifBlank { "Review" },
                tone = leadershipTone(alert.severity),
            )
        }
        return base.copy(
            hero = base.hero.copy(
                coverageLabel = "Process integrity",
                coveragePercent = coverage,
                // Doses line / trend / animals total are NOT carried by the control-tower
                // summary — clear the sample's fabricated values rather than show them as real.
                dosesLine = "",
                dosesTrend = emptyList(),
                animalsLabel = "",
                scopePill = null,
                dataGapPill = DataGapPill(
                    label = if (summary.openGapCount > 0) "${summary.openGapCount} open gaps" else "Data gaps",
                    hasGaps = summary.openGapCount > 0,
                    openGapsCount = summary.openGapCount,
                ),
            ),
            kpis = listOf(
                KpiTile("critical", summary.criticalCount.toString(), "Critical", Tone.DANGER),
                KpiTile("warnings", summary.warningCount.toString(), "Warnings", Tone.WARN),
                KpiTile("open_gaps", summary.openGapCount.toString(), "Open gaps", Tone.NEUTRAL),
            ),
            // Empty real decisions must render as empty — never fall back to sample rows.
            needsDecision = decisions,
        )
    }
}

data class OverlayLoadState<T>(
    val items: List<T> = emptyList(),
    val isLoading: Boolean = false,
    val errorMessage: String? = null,
    // Offline-first cache sync state (carried on the sub-state; the sheet renders items/
    // isLoading/errorMessage today and can surface these later without a VM change).
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    val isLoadingMore: Boolean = false,
    val hasMore: Boolean = false,
)

/** Overlay read page sizes — must match between observeX (init collectors) and refreshX
 *  (loadGaps/loadDosesGiven) so the cache-first stream and the refresh share one cache key. */
private const val GAPS_LIMIT = 20
private const val COVERAGE_LIMIT = 50 // mobile-guard:ignore: bounded vaccine protocol catalog, not an animal list

// Data gaps are strictly per-animal: one card per goat (display id + physical tags + reason).
// No rows → empty list → the sheet shows its empty state. There is no by-reason aggregate card.
private fun VaccinationGapsResponseDto.toGapRows(): List<GapRow> =
    rows.map { row ->
        GapRow(
            // Display id headlines the card; the two physical tags render below it (— when absent).
            displayId = row.displayId.ifBlank { row.goatId },
            // Location only — the reason is the pill, so the card leads with WHERE, not WHY.
            location = listOfNotNull(
                row.parkName.takeIf { it.isNotBlank() },
                row.shedName,
            ).joinToString(" · "),
            reason = row.reasonLabel,
            tag1 = row.animalIdentifier1,
            tag2 = row.animalIdentifier2,
        )
    }

private fun VaccinationCoverageResponseDto.toGivenRows(): List<GivenRow> =
    protocols.map { protocol ->
        GivenRow(
            vaccine = protocol.name,
            given = "${protocol.givenCount}/${protocol.totalCount}",
            coverage = "${protocol.coveragePercent}%",
            percent = protocol.coveragePercent,
        )
    }

private fun leadershipTone(severity: String): Tone = when (severity.lowercase()) {
    "critical", "high" -> Tone.DANGER
    "warn", "warning" -> Tone.WARN
    "ok" -> Tone.OK
    else -> Tone.NEUTRAL
}
