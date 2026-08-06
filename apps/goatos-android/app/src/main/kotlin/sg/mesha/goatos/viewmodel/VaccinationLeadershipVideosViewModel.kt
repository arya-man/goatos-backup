package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import javax.inject.Inject
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.vaccination.leadership.VACCINATION_LEADERSHIP_MAX_WINDOW
import sg.mesha.goatos.core.data.vaccination.leadership.VACCINATION_LEADERSHIP_PAGE_SIZE
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideoPlaybackAction
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideoPlaybackEvent
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideosUiState

/**
 * The vaccination leadership videos gallery, rendered FROM ROOM.
 *
 * It observes cached verification items, showing the full evidence trail (pending/approved/rejected/closed).
 * The network refresh only writes into Room; a failed refresh leaves the cached gallery on screen
 * with a staleness note. Leadership sees all statuses as an audit surface with no verdict capability.
 */
@HiltViewModel
class VaccinationLeadershipVideosViewModel @Inject constructor(
    private val repository: VerificationRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {
    /**
     * How many cached items the gallery observes. Grows ONE page at a time on scroll and is held
     * to the cache's ceiling, so the observed Room read stays a bounded window.
     */
    private val window = MutableStateFlow(VACCINATION_LEADERSHIP_PAGE_SIZE)
    private val loading = MutableStateFlow(false)
    private val loadingMore = MutableStateFlow(false)
    private val failure = MutableStateFlow<String?>(null)

    @OptIn(ExperimentalCoroutinesApi::class)
    private val cached: StateFlow<List<VaccinationLeadershipItemUi>> =
        window.flatMapLatest { size -> repository.observeLeadershipVideos("vaccination_proof", size) }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    val state: StateFlow<VaccinationLeadershipVideosUiState> =
        combine(cached, loading, loadingMore, failure) { items, isLoading, isAppending, error ->
            VaccinationLeadershipVideosUiState(
                loading = isLoading,
                loadingMore = isAppending,
                items = items,
                // With nothing cached the failure IS the answer; with a cached gallery it is only a
                // staleness note, so a bad network never clears the reader's screen.
                error = error?.takeIf { items.isEmpty() },
                staleNotice = error?.takeIf { items.isNotEmpty() }
                    ?.let { "Showing the last saved videos. $it" }
                    .orEmpty(),
            )
        }.stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            VaccinationLeadershipVideosUiState(loading = true),
        )

    init {
        analytics.track(AnalyticsEvents.VACCINATION_LEADERSHIP_VIDEO_VIEWED)
        refresh()
    }

    /** Re-reads the FIRST page into Room. Cached items stay on screen throughout. */
    fun refresh() {
        if (loading.value) return
        loading.value = true
        window.value = VACCINATION_LEADERSHIP_PAGE_SIZE
        viewModelScope.launch {
            try {
                when (val result = repository.refreshLeadershipVideos("vaccination_proof", windowSize = window.value, reset = true)) {
                    is AppResult.Ok -> failure.value = null
                    is AppResult.Err -> {
                        failure.value = result.message
                        crashReporter.recordException(
                            result.cause ?: IllegalStateException(result.message),
                            "vaccination leadership videos load failed"
                        )
                    }
                }
            } finally {
                loading.value = false
            }
        }
    }

    /**
     * Scroll-driven prefetch: the gallery reports the item it just composed, and only an item
     * inside the tail window asks for the next page — of the network read and of the observed Room
     * window together. One page per trigger, never a tappable "Load more".
     */
    fun onItemVisible(index: Int) {
        val loaded = cached.value.size
        if (loaded == 0 || index < loaded - LIST_PREFETCH_DISTANCE) return
        if (window.value < VACCINATION_LEADERSHIP_MAX_WINDOW) {
            window.value = (window.value + VACCINATION_LEADERSHIP_PAGE_SIZE).coerceAtMost(VACCINATION_LEADERSHIP_MAX_WINDOW)
        }
        if (loading.value || loadingMore.value) return
        loadingMore.value = true
        viewModelScope.launch {
            try {
                when (val result = repository.refreshLeadershipVideos("vaccination_proof", windowSize = window.value, reset = false)) {
                    is AppResult.Ok -> failure.value = null
                    is AppResult.Err -> failure.value = result.message
                }
            } finally {
                loadingMore.value = false
            }
        }
    }

    /** Forwarded from the screen's player listener for telemetry and crash reporting. */
    fun onPlayback(event: VaccinationLeadershipVideoPlaybackEvent) {
        when (event.action) {
            VaccinationLeadershipVideoPlaybackAction.PLAY_STARTED ->
                AnalyticsFunnels.trackVaccinationLeadershipVideoPlayStarted(
                    analytics = analytics,
                    proofId = event.proofId,
                    mimeType = event.mimeType,
                    durationMs = event.durationMs,
                )
            VaccinationLeadershipVideoPlaybackAction.WATCH_SUMMARY ->
                AnalyticsFunnels.trackVaccinationLeadershipVideoWatchSummary(
                    analytics = analytics,
                    proofId = event.proofId,
                    mimeType = event.mimeType,
                    watchTimeMs = event.watchTimeMs,
                    durationMs = event.durationMs,
                    positionMs = event.positionMs,
                    percentWatched = event.percentWatched,
                    seekCount = event.seekCount,
                    replayCount = event.replayCount,
                    bufferingTimeMs = event.bufferingTimeMs,
                )
            VaccinationLeadershipVideoPlaybackAction.PLAYBACK_ERROR -> {
                val reason = event.reason ?: "unknown"
                crashReporter.recordException(
                    IllegalStateException(reason),
                    "vaccination leadership video playback failed",
                )
                AnalyticsFunnels.trackVaccinationLeadershipVideoPlaybackError(analytics, event.proofId, reason)
            }
        }
    }

    private companion object {
        const val LIST_PREFETCH_DISTANCE = 3
    }
}
