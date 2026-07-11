package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
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

    private val _state = MutableStateFlow(leadershipPlaceholder("Loading overview…"))
    val state: StateFlow<LeadershipUiState> = _state.asStateFlow()

    private val _gapsState = MutableStateFlow(OverlayLoadState<GapRow>())
    val gapsState: StateFlow<OverlayLoadState<GapRow>> = _gapsState.asStateFlow()

    private val _dosesState = MutableStateFlow(OverlayLoadState<GivenRow>())
    val dosesState: StateFlow<OverlayLoadState<GivenRow>> = _dosesState.asStateFlow()

    init {
        // Cache-first: renders whatever Room already has immediately, then re-renders
        // after every successful refresh below.
        viewModelScope.launch {
            controlTower.observeSummary().collectLatest { summaryResource ->
                applyResource(summaryResource)
            }
        }
        // Drill-overlay caches are observed for the VM's lifetime so opening a sheet renders
        // Room's cached rows instantly; the network refresh is lazy (driven by loadGaps /
        // loadDosesGiven when the sheet actually opens). Args match refreshGaps/refreshCoverage
        // below so the observe + refresh share the same cache key.
        viewModelScope.launch {
            insights.observeGaps(limit = GAPS_LIMIT).collectLatest { gapsResource ->
                applyGapsResource(gapsResource)
            }
        }
        viewModelScope.launch {
            insights.observeCoverage(limit = COVERAGE_LIMIT).collectLatest { coverageResource ->
                applyCoverageResource(coverageResource)
            }
        }
        refresh()
    }

    fun refresh() = viewModelScope.launch {
        _state.update { it.copy(isRefreshing = true) }
        val summaryResult = controlTower.refreshSummary()
        _state.update { it.copy(isRefreshing = false, isOffline = summaryResult.isFailure) }
    }

    private fun applyResource(resource: Resource<ControlTowerResponseDto>) {
        val dto = resource.data
        val base = dto?.toLeadershipUiState()
            ?: if (resource.hasData) leadershipPlaceholder("No overview data") else leadershipPlaceholder("Loading overview…")
        _state.update { current ->
            base.copy(
                isRefreshing = current.isRefreshing,
                lastSyncedAt = resource.lastSyncedAt ?: current.lastSyncedAt,
                isOffline = current.isOffline,
            )
        }
    }

    fun onEvent(event: LeadershipEvent) {
        when (event) {
            LeadershipEvent.Refresh -> refresh()
            // Everything else is either navigation (routed by the host) or not yet backed.
            else -> Unit
        }
    }

    /**
     * Data-gaps overlay — cache-first. The [insights].observeGaps collector in [init] keeps
     * [gapsState] fed from Room, so the sheet renders cached gaps instantly on open; this only
     * drives the background [VaccinationInsightsRepository.refreshGaps] upsert (Room re-emits
     * via that collector). A refresh failure keeps cached gaps on screen and flips isOffline; a
     * failure with no cache ever observed shows an honest error. Preserves the trigger point —
     * still called when the sheet opens.
     */
    fun loadGaps() = viewModelScope.launch {
        _gapsState.update { current ->
            // Blocking loading state only on a cold cache; when cache exists keep it and refresh silently.
            current.copy(isRefreshing = true, isLoading = current.items.isEmpty(), errorMessage = null)
        }
        val result = insights.refreshGaps(limit = GAPS_LIMIT)
        _gapsState.update { current ->
            when {
                result.isSuccess -> current.copy(isRefreshing = false, isLoading = false, isOffline = false)
                // A cache was already rendered — keep it on screen, just surface offline.
                current.items.isNotEmpty() || current.lastSyncedAt != null ->
                    current.copy(isRefreshing = false, isLoading = false, isOffline = true)
                // Never cached, ever: no fallback, so the honest error is the only truthful thing.
                else -> current.copy(
                    isRefreshing = false,
                    isLoading = false,
                    isOffline = true,
                    errorMessage = result.exceptionOrNull()?.message ?: "Couldn't load data gaps.",
                )
            }
        }
    }

    /**
     * Doses-given (per-vaccine coverage) overlay — cache-first, mirrors [loadGaps]. The
     * observeCoverage collector in [init] keeps [dosesState] fed from Room; this drives the
     * background [VaccinationInsightsRepository.refreshCoverage] upsert. Preserves the trigger
     * point — still called when the sheet opens.
     */
    fun loadDosesGiven() = viewModelScope.launch {
        _dosesState.update { current ->
            current.copy(isRefreshing = true, isLoading = current.items.isEmpty(), errorMessage = null)
        }
        val result = insights.refreshCoverage(limit = COVERAGE_LIMIT)
        _dosesState.update { current ->
            when {
                result.isSuccess -> current.copy(isRefreshing = false, isLoading = false, isOffline = false)
                current.items.isNotEmpty() || current.lastSyncedAt != null ->
                    current.copy(isRefreshing = false, isLoading = false, isOffline = true)
                else -> current.copy(
                    isRefreshing = false,
                    isLoading = false,
                    isOffline = true,
                    errorMessage = result.exceptionOrNull()?.message ?: "Couldn't load vaccination coverage.",
                )
            }
        }
    }

    /** Cache emission for the gaps overlay: render Room's rows, clear loading/error when data
     *  is present, and carry the sync clock — never blow away cached rows on a null emission. */
    private fun applyGapsResource(resource: Resource<VaccinationGapsResponseDto>) {
        val dto = resource.data
        _gapsState.update { current ->
            val items = dto?.toGapRows() ?: current.items
            current.copy(
                items = items,
                isLoading = if (resource.hasData) false else current.isLoading,
                errorMessage = if (resource.hasData) null else current.errorMessage,
                lastSyncedAt = resource.lastSyncedAt ?: current.lastSyncedAt,
            )
        }
    }

    /** Cache emission for the doses-given overlay — mirrors [applyGapsResource]. */
    private fun applyCoverageResource(resource: Resource<VaccinationCoverageResponseDto>) {
        val dto = resource.data
        _dosesState.update { current ->
            val items = dto?.toGivenRows() ?: current.items
            current.copy(
                items = items,
                isLoading = if (resource.hasData) false else current.isLoading,
                errorMessage = if (resource.hasData) null else current.errorMessage,
                lastSyncedAt = resource.lastSyncedAt ?: current.lastSyncedAt,
            )
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
)

/** Overlay read page sizes — must match between observeX (init collectors) and refreshX
 *  (loadGaps/loadDosesGiven) so the cache-first stream and the refresh share one cache key. */
private const val GAPS_LIMIT = 50
private const val COVERAGE_LIMIT = 50

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
