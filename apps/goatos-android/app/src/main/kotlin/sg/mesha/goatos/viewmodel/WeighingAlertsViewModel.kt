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
import sg.mesha.goatos.core.data.WeighingAlertsRepository
import sg.mesha.goatos.core.network.dto.WeighingAlertPageResponseDto
import sg.mesha.goatos.feature.profile.AlertRow
import sg.mesha.goatos.feature.profile.AlertTone
import sg.mesha.goatos.feature.profile.AlertsEvent
import sg.mesha.goatos.feature.profile.AlertsUiState
import javax.inject.Inject

/**
 * WEIGHING alerts state holder.
 *
 * This is the surface the sibling [AlertsViewModel] documented as missing: its own class doc says
 * "there is no notification-history read in the app, so a weighing/feed/counts push exists only as
 * a system-tray entry and is unrecoverable once swiped away" and that weighing alerts would live
 * in weighing "until a module-scoped notification history exists". It now does --
 * `GET /app/weighing/alerts` -- and this ViewModel renders it. [AlertsViewModel] is untouched and
 * keeps naming its own vaccination scope.
 *
 * WHAT IT SHOWS: the weighing work-state transitions routed to THIS person -- work assigned to
 * them (downstream), a shed they submitted going up for verification, a proof sent back for
 * rework (downstream again), a shed reopened, work closed.
 *
 * COPY IS BACKEND-OWNED. The screen title, the empty-state sentence, and every row's headline and
 * body come from the response. Nothing weighing-specific is hardcoded here, so the feed can be
 * renamed or re-scoped without an app release. Only the transient loading/offline states are
 * client-side, because they describe THIS DEVICE and no server can author them.
 *
 * Offline-first, same shape as [AlertsViewModel]: [state] is fed by
 * [WeighingAlertsRepository.observeAlerts], a cache-first Room flow that emits instantly and
 * re-emits after a background refresh. A refresh failure WITH cached data keeps rendering the
 * cache and only flips [AlertsUiState.isOffline]; it never blanks the screen. The single
 * WhileSubscribed(5_000) on [state] makes the whole chain lifecycle-aware (MOB-010).
 *
 * ISOLATION: weighing is free-flow and fully herd-isolated. No row here resolves or renders a
 * goat identity, and no count is shown as a share of an expected roster, because free-flow
 * weighing has no expected animal set to be a share of.
 */
@HiltViewModel
class WeighingAlertsViewModel @Inject constructor(
    private val repo: WeighingAlertsRepository,
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

    private val observed: Flow<Resource<WeighingAlertPageResponseDto>> = repo.observeAlerts()

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
        analytics.track(AnalyticsEvents.WEIGHING_ALERTS_VIEWED)
        refresh()
    }

    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        analytics.track(AnalyticsEvents.WEIGHING_ALERTS_REFRESH_ATTEMPTED)
        val result = repo.refreshAlerts()
        _isRefreshing.value = false
        _isOffline.value = result.isFailure
        if (result.isSuccess) {
            analytics.track(AnalyticsEvents.WEIGHING_ALERTS_REFRESH_SUCCEEDED)
        } else {
            analytics.track(
                AnalyticsEvents.WEIGHING_ALERTS_REFRESH_FAILED,
                mapOf(AnalyticsEvents.Params.REASON to (result.exceptionOrNull()?.message ?: "unknown"))
            )
            result.exceptionOrNull()?.let {
                crashReporter.recordException(it, "weighing alerts refresh failed")
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

    private fun WeighingAlertPageResponseDto.toUiState(): AlertsUiState = AlertsUiState(
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
