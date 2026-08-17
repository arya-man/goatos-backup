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
import sg.mesha.goatos.core.analytics.AnalyticsEventsVerification
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.network.isConnectivityFailure
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.feature.profile.AlertRow
import sg.mesha.goatos.feature.profile.AlertTone
import sg.mesha.goatos.feature.profile.AlertsEvent
import sg.mesha.goatos.feature.profile.AlertsUiState
import sg.mesha.goatos.ui.alertsPlaceholder
import sg.mesha.goatos.ui.sampleAlertsState
import javax.inject.Inject

/**
 * Vaccination-alerts state holder — the offline-first REFERENCE for every other screen-read
 * ViewModel (docs/decisions/android-offline-first.md). Room is the UI's single source of
 * truth: [state] is fed by [ControlTowerRepository.observeSummary], a cache-first [kotlinx.coroutines.flow.Flow]
 * that emits instantly from Room (cached data survives process restarts and screen
 * re-entry) and re-emits the moment a background [ControlTowerRepository.refreshSummary] upserts
 * new data. [refresh] never writes into [state] directly — it only drives the network call
 * and the transient [AlertsUiState.isRefreshing]/[AlertsUiState.isOffline] flags; the
 * DTO -> UiState mapping in [toAlertsUiState] is unchanged from the network-only version.
 * An empty real response shows an honest empty state; a refresh failure with NO cache ever
 * observed shows an honest error state; a refresh failure WITH cached data keeps rendering
 * that cache and only flips [AlertsUiState.isOffline] — never a blank/loading wall.
 * Rows are marked read locally so the list feels live: [AlertsEvent.MarkAllRead] clears
 * every unread flag, [AlertsEvent.OpenAlert] clears the tapped row.
 *
 * SCOPE, STATED HONESTLY: the only upstream this screen has is
 * [ControlTowerRepository.observeSummary], i.e. the vaccination control-tower summary. It can
 * therefore only ever show VACCINATION alerts. There is no notification-history read in the app,
 * so a weighing/feed/counts push exists only as a system-tray entry and is unrecoverable once
 * swiped away, and [ControlTowerRepository.observeSummary] carries no module dimension at all —
 * "weighing alerts live in weighing" is not expressible here yet. Until a module-scoped
 * notification history exists, every visible label on this surface names the vaccination scope
 * rather than promising all-module alerts.
 */
@HiltViewModel
class AlertsViewModel @Inject constructor(
    private val repo: ControlTowerRepository,
    private val analytics: AnalyticsPort = NoopAnalytics(),
) : ViewModel() {

    // NO copy here. The screen is titled just "Alerts" and the reader reached it from
    // vaccination's own bar, so the feature name adds nothing; more importantly, a literal
    // here is English-only, while the screen resolves the localized default. Blank = "use the
    // screen's own label".
    private val screenTitle = ""

    // Upstream Room flow. Kept as a cold Flow and folded into [state] below; the single
    // WhileSubscribed(5_000) on [state] makes the whole chain lifecycle-aware, so this flow is
    // collected only while the UI is subscribed (MOB-010).
    private val observedResource: Flow<Resource<ControlTowerResponseDto>> = repo.observeSummary()

    // Transient flags for manual updates
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _localReadState = MutableStateFlow<Set<String>>(emptySet())

    // Fires once for this ViewModel's lifetime — the FIRST time the screen settles out of its
    // initial loading placeholder, whether that lands on real rows or an honest empty state.
    // Before this, Alerts had zero analytics: a CEO/operator could sit on this screen reading
    // vaccination alerts and nothing beyond `bootstrap_loaded` recorded she was ever here.
    //
    // Deliberately fired FROM the [combine] transform below rather than a separate
    // `state.collect` in init: [state] is WhileSubscribed(5_000) (MOB-010) — a standing collector
    // here would keep [observedResource] (and the underlying Room flow) permanently warm even
    // with no UI subscriber, defeating the whole point of WhileSubscribed. The transform below
    // only RUNS while something is actually collecting [state], so the side effect inherits the
    // same lifecycle-aware guarantee for free.
    private var trackedScreenViewed = false

    // Combines observed resource with transient flags; lifecycle-aware
    val state: StateFlow<AlertsUiState> = combine(
        observedResource,
        _isRefreshing,
        _isOffline,
        _localReadState
    ) { resource, isRefreshing, isOffline, readSet ->
        val dto = resource.data
        val base = dto?.toAlertsUiState()
            ?: if (resource.hasData || isOffline) {
                vaccinationAlertsPlaceholder("")
            } else {
                vaccinationAlertsPlaceholder("Loading…")
            }
        val next = base.copy(
            rows = base.rows.map { it.copy(unread = it.id !in readSet) },
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = isOffline,
        )
        if (!trackedScreenViewed && !isRefreshing) {
            trackedScreenViewed = true
            runCatching {
                analytics.track(
                    AnalyticsEventsVerification.ALERTS_VIEWED,
                    mapOf(AnalyticsEventsVerification.Params.ROW_COUNT to next.rows.size.toString()),
                )
            }
        }
        next
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        vaccinationAlertsPlaceholder("Loading vaccination alerts…")
    )

    init {
        refresh()
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observedResource]
     *  StateFlow re-emits and updates [state]); on failure it only flips [isOffline] —
     *  cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        runCatching { analytics.track(AnalyticsEventsVerification.ALERTS_REFRESH_ATTEMPTED) }
        val result = repo.refreshSummary()
        _isRefreshing.value = false
        _isOffline.value = result.exceptionOrNull().isConnectivityFailure()
        runCatching {
            if (result.isSuccess) {
                analytics.track(AnalyticsEventsVerification.ALERTS_REFRESH_SUCCEEDED)
            } else {
                analytics.track(
                    AnalyticsEventsVerification.ALERTS_REFRESH_FAILED,
                    mapOf(
                        AnalyticsEventsVerification.Params.REASON to
                            (result.exceptionOrNull()?.let { it::class.simpleName } ?: "unknown"),
                    ),
                )
            }
        }
    }

    fun onEvent(event: AlertsEvent) {
        when (event) {
            AlertsEvent.MarkAllRead -> {
                // Mark all rows as read in local state (not persisted to backend)
                val unreadIds = state.value.rows.mapNotNull { if (it.unread) it.id else null }.toSet()
                _localReadState.value = unreadIds
                runCatching {
                    analytics.track(
                        AnalyticsEventsVerification.ALERT_MARK_ALL_READ,
                        mapOf(AnalyticsEventsVerification.Params.UNREAD_COUNT to unreadIds.size.toString()),
                    )
                }
            }
            is AlertsEvent.OpenAlert -> {
                // Mark tapped row as read in local state
                _localReadState.value = _localReadState.value + event.id
                runCatching {
                    analytics.track(
                        AnalyticsEventsVerification.ALERT_TAPPED,
                        mapOf(AnalyticsEventsVerification.Params.ALERT_ID to event.id),
                    )
                }
            }
            AlertsEvent.Refresh -> refresh()
        }
    }

    /** Honest placeholder: same shape as [alertsPlaceholder], titled for the vaccination scope. */
    private fun vaccinationAlertsPlaceholder(message: String): AlertsUiState =
        alertsPlaceholder(message).copy(title = screenTitle)

    private fun ControlTowerResponseDto.toAlertsUiState(): AlertsUiState? {
        if (alerts.isEmpty()) return vaccinationAlertsPlaceholder("")
        val base = sampleAlertsState().copy(title = screenTitle)
        val rows = alerts.map { alert ->
            AlertRow(
                id = alert.rowId,
                title = alert.title,
                body = alert.detail,
                timeLabel = "",
                tone = when (alert.severity.lowercase()) {
                    "critical", "high", "broken" -> AlertTone.CRITICAL
                    "warn", "warning", "watch", "at_risk" -> AlertTone.WARN
                    else -> AlertTone.INFO
                },
                unread = true,
            )
        }
        return base.copy(rows = rows)
    }
}
