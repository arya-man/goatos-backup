package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VaccinationAlertsRepository
import sg.mesha.goatos.core.network.dto.VaccinationAlertPageResponseDto
import sg.mesha.goatos.feature.profile.AlertRow
import sg.mesha.goatos.feature.profile.AlertTone
import sg.mesha.goatos.feature.profile.AlertsEvent
import sg.mesha.goatos.feature.profile.AlertsUiState
import javax.inject.Inject

/**
 * VACCINATION alerts state holder -- the module's OWN lifecycle feed.
 *
 * WHY IT EXISTS: /vaccination/alerts was hosted and bound to the generic [AlertsViewModel], whose
 * own doc says it "carries no module dimension at all" because it reads the control-tower GAP
 * summary. Control tower reports process gaps, not transitions, so every vaccination lifecycle
 * notification was invisible: on 2026-08-08 there were 41 unread notification rows (10 of them
 * proof rejections) against an empty Alerts tab on three phones. Weighing was given its own feed
 * on 2026-08-03; this is the vaccination twin.
 *
 * WHAT IT SHOWS: the vaccination work-state transitions routed to THIS person -- a proof sent
 * back for rework (downstream), a proof approved, a record closed, a proof going up for
 * verification (upstream). Scoped to vaccination alone by the backend's message_key
 * discriminator, exactly as weighing is scoped to its own.
 *
 * COPY IS BACKEND-OWNED. Screen title, empty-state sentence and every row's headline and body
 * come from the response. Nothing is hardcoded here -- and specifically NOT the string
 * "Vaccination alerts", which AGENTS.md records as the copy-firewall violation that lived in
 * [AlertsViewModel]. Only transient loading/offline state is client-side, because it describes
 * THIS DEVICE and no server can author it.
 *
 * Offline-first: [state] is fed by [VaccinationAlertsRepository.observeAlerts], a cache-first Room
 * flow that emits instantly and re-emits after a background refresh. A refresh failure WITH cached
 * data keeps rendering the cache and only flips [AlertsUiState.isOffline]; it never blanks the
 * screen. The single WhileSubscribed(5_000) makes the whole chain lifecycle-aware (MOB-010).
 */
@HiltViewModel
class VaccinationAlertsViewModel @Inject constructor(
    private val repo: VaccinationAlertsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    /**
     * Pre-first-emission placeholder. Both strings are EMPTY on purpose: the backend owns this
     * surface's title and empty copy, and inventing "Weighing alerts" here would be a client
     * string that silently outlives a backend rename. The screen renders a blank header for the
     * few frames before Room's first emission, then the real backend copy.
     */
    private val loadingState = AlertsUiState(title = "", emptyLabel = "")

    private val observed: Flow<Resource<VaccinationAlertPageResponseDto>> = repo.observeAlerts()

    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _localReadState = MutableStateFlow<Set<String>>(emptySet())

    val state: StateFlow<AlertsUiState> = combine(
        observed,
        _isRefreshing,
        _isOffline,
        _localReadState,
    ) { resource, isRefreshing, isOffline, readSet ->
        val base = resource.data?.toUiState() ?: loadingState
        base.copy(
            rows = base.rows.map { it.copy(unread = it.id !in readSet) },
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        loadingState,
    )

    init {
        analytics.track(AnalyticsEvents.VACCINATION_ALERTS_VIEWED)
        refresh()
    }

    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        analytics.track(AnalyticsEvents.VACCINATION_ALERTS_REFRESH_ATTEMPTED)
        val result = repo.refreshAlerts()
        _isRefreshing.value = false
        _isOffline.value = result.isFailure
        if (result.isSuccess) {
            analytics.track(AnalyticsEvents.VACCINATION_ALERTS_REFRESH_SUCCEEDED)
        } else {
            analytics.track(
                AnalyticsEvents.VACCINATION_ALERTS_REFRESH_FAILED,
                mapOf(AnalyticsEvents.Params.REASON to (result.exceptionOrNull()?.message ?: "unknown"))
            )
            result.exceptionOrNull()?.let {
                crashReporter.recordException(it, "vaccination alerts refresh failed")
            }
        }
    }

    fun onEvent(event: AlertsEvent) {
        when (event) {
            AlertsEvent.MarkAllRead ->
                _localReadState.value = state.value.rows.mapNotNull { if (it.unread) it.id else null }.toSet()
            is AlertsEvent.OpenAlert -> _localReadState.value = _localReadState.value + event.id
            AlertsEvent.Refresh -> refresh()
        }
    }

    private fun VaccinationAlertPageResponseDto.toUiState(): AlertsUiState = AlertsUiState(
        title = title,
        emptyLabel = emptyMessage,
        rows = items.map { alert ->
            AlertRow(
                id = alert.alertId,
                title = alert.title,
                body = alert.body,
                // No client-side time formatting: the backend has not authored a relative-time
                // label for this feed, and composing one here ("2h ago") would be app-invented
                // copy on a backend-owned surface. Rows arrive newest-first already.
                timeLabel = "",
                tone = when (alert.severity.lowercase()) {
                    "high" -> AlertTone.WARN
                    else -> AlertTone.INFO
                },
                unread = true,
            )
        },
    )
}
