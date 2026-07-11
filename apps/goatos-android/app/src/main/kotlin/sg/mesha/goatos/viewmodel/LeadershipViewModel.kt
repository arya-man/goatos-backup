package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.AdherenceRepository
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.data.VaccinationInsightsRepository
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.core.network.dto.ProtocolAdherenceResponseDto
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
 * Leadership overview state holder. Shows a loading placeholder first, then loads real
 * process-integrity data via [ControlTowerRepository.summary] (hero + KPIs + decisions)
 * plus a real coverage % from [AdherenceRepository.adherence], mapped in
 * [toLeadershipUiState]. A failure shows an honest error state — the sample overview is
 * NEVER shown as if it were live data. Drill events are navigation, routed by the host;
 * [LeadershipEvent.Refresh] reloads.
 */
@HiltViewModel
class LeadershipViewModel @Inject constructor(
    private val controlTower: ControlTowerRepository,
    private val adherence: AdherenceRepository,
    private val insights: VaccinationInsightsRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(leadershipPlaceholder("Loading overview…"))
    val state: StateFlow<LeadershipUiState> = _state.asStateFlow()

    private val _gapsState = MutableStateFlow(OverlayLoadState<GapRow>())
    val gapsState: StateFlow<OverlayLoadState<GapRow>> = _gapsState.asStateFlow()

    private val _dosesState = MutableStateFlow(OverlayLoadState<GivenRow>())
    val dosesState: StateFlow<OverlayLoadState<GivenRow>> = _dosesState.asStateFlow()

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        val summaryResult = runCatching { controlTower.summary() }
        val adherenceResult = runCatching { adherence.adherence() }
        summaryResult
            .onSuccess { dto -> _state.value = dto.toLeadershipUiState(adherenceResult.getOrNull()) }
            .onFailure { _state.value = leadershipPlaceholder("Couldn't load overview. Tap refresh to retry.") }
    }

    fun onEvent(event: LeadershipEvent) {
        when (event) {
            LeadershipEvent.Refresh -> load()
            // Everything else is either navigation (routed by the host) or not yet backed.
            else -> Unit
        }
    }

    fun loadGaps() = viewModelScope.launch {
        _gapsState.value = OverlayLoadState(isLoading = true)
        runCatching { insights.gaps(limit = 50) }
            .onSuccess { dto -> _gapsState.value = OverlayLoadState(items = dto.toGapRows()) }
            .onFailure { error ->
                _gapsState.value = OverlayLoadState(errorMessage = error.message ?: "Couldn't load data gaps.")
            }
    }

    fun loadDosesGiven() = viewModelScope.launch {
        _dosesState.value = OverlayLoadState(isLoading = true)
        runCatching { insights.coverage(limit = 50) }
            .onSuccess { dto -> _dosesState.value = OverlayLoadState(items = dto.toGivenRows()) }
            .onFailure { error ->
                _dosesState.value = OverlayLoadState(errorMessage = error.message ?: "Couldn't load vaccination coverage.")
            }
    }

    private fun ControlTowerResponseDto.toLeadershipUiState(
        adherenceDto: ProtocolAdherenceResponseDto?,
    ): LeadershipUiState {
        val base = sampleLeadershipState()
        val coverage = adherenceDto?.summary?.adherencePercent?.toInt()
            ?: if (summary.processIntact) 100 else (100 - summary.openGapCount).coerceIn(0, 100)
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
)

private fun VaccinationGapsResponseDto.toGapRows(): List<GapRow> =
    if (rows.isNotEmpty()) {
        rows.map { row ->
            GapRow(
                title = row.displayId.ifBlank { row.goatId },
                detail = listOfNotNull(
                    row.parkName.takeIf { it.isNotBlank() },
                    row.shedName,
                    row.reasonLabel.takeIf { it.isNotBlank() },
                ).joinToString(" · "),
                count = "1",
            )
        }
    } else {
        reasons.map { reason ->
            GapRow(
                title = reason.reasonLabel.ifBlank { reason.reasonCode },
                detail = reason.reasonCode,
                count = reason.count.toString(),
            )
        }
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
