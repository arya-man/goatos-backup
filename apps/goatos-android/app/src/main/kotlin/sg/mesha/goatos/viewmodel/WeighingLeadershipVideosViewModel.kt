package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.Instant
import java.time.LocalDate
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
import sg.mesha.goatos.core.data.weighing.WEIGHING_LEADERSHIP_PAGE_SIZE
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShed
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipAnimalUi
import sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipShedUi
import sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipVideoPlaybackAction
import sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipVideoPlaybackEvent
import sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipVideoUi
import sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipVideosUiState

/**
 * The leadership videos gallery, rendered FROM ROOM.
 *
 * It observes the SAME cached shed rows the shed detail screen reads, so the gallery and a bucket
 * opened from it can never show two answers for one shed. The network refresh only writes into
 * Room; a failed refresh leaves the cached gallery on screen with a staleness note.
 */
@HiltViewModel
class WeighingLeadershipVideosViewModel @Inject constructor(
    private val repository: WeighingRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {
    private val loading = MutableStateFlow(false)
    private val loadingMore = MutableStateFlow(false)
    private val failure = MutableStateFlow<String?>(null)

    @OptIn(ExperimentalCoroutinesApi::class)
    // Fixed-size keyset window over Room. The window does NOT grow: scrolling appends the next
    // page by CURSOR (see onShedVisible), matching VerifyQueueViewModel and the three sibling
    // weighing screens. The old growing window was clamped at WEIGHING_LEADERSHIP_MAX_WINDOW
    // (PAGE_SIZE * 5 = 100), so a park with more than 100 buckets had rows sitting in Room that
    // the gallery could never scroll to.
    private val cached: StateFlow<List<WeighingLeadershipShed>> =
        repository.observeLeadershipVideos(WEIGHING_LEADERSHIP_PAGE_SIZE)
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    val state: StateFlow<WeighingLeadershipVideosUiState> =
        combine(cached, loading, loadingMore, failure) { sheds, isLoading, isAppending, error ->
            WeighingLeadershipVideosUiState(
                loading = isLoading,
                loadingMore = isAppending,
                sheds = sheds.map(::toUi),
                // With nothing cached the failure IS the answer; with a cached gallery it is only a
                // staleness note, so a bad network never clears the reader's screen.
                error = error?.takeIf { sheds.isEmpty() },
                staleNotice = error?.takeIf { sheds.isNotEmpty() }
                    ?.let { "Showing the last saved videos. $it" }
                    .orEmpty(),
            )
        }.stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            WeighingLeadershipVideosUiState(loading = true),
        )

    init {
        analytics.track(AnalyticsEvents.WEIGHING_LEADERSHIP_VIDEO_VIEWED)
        refresh()
    }

    /** Re-reads the FIRST page into Room. Cached buckets stay on screen throughout. */
    fun refresh() {
        if (loading.value) return
        loading.value = true
        // No window to reset: the observed Room read is a fixed keyset page, and reset = true
        // clears the cursor so appends restart from the first page.
        viewModelScope.launch {
            try {
                when (val result = repository.refreshLeadershipVideos(reset = true)) {
                    is AppResult.Ok -> failure.value = null
                    is AppResult.Err -> {
                        failure.value = result.message
                        crashReporter.recordException(
                            result.cause ?: IllegalStateException(result.message),
                            "weighing leadership videos load failed"
                        )
                    }
                }
            } finally {
                loading.value = false
            }
        }
    }

    /**
     * Scroll-driven prefetch: the gallery reports the bucket it just composed, and only a bucket
     * inside the tail window asks for the next page — of the network read and of the observed Room
     * window together. One page per trigger, never a tappable "Load more".
     */
    fun onShedVisible(index: Int) {
        val loaded = cached.value.size
        if (loaded == 0 || index < loaded - LIST_PREFETCH_DISTANCE) return
        if (loading.value || loadingMore.value) return
        loadingMore.value = true
        viewModelScope.launch {
            try {
                when (val result = repository.appendLeadershipVideos()) {
                    is AppResult.Ok -> failure.value = null
                    is AppResult.Err -> failure.value = result.message
                }
            } finally {
                loadingMore.value = false
            }
        }
    }

    /** Forwarded synchronously from [sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipVideosScreen]'s
     *  player listener (mirrors VerifyDetailViewModel.trackVideoPlayback, `:app`). Never swallowed:
     *  a real playback failure is recorded as a non-fatal, matching the verifier surface's own
     *  crashReporter contract for the same failure class. */
    fun onPlayback(event: WeighingLeadershipVideoPlaybackEvent) {
        when (event.action) {
            WeighingLeadershipVideoPlaybackAction.PLAY_STARTED ->
                AnalyticsFunnels.trackWeighingLeadershipVideoPlayStarted(
                    analytics = analytics,
                    proofId = event.proofId,
                    mimeType = event.mimeType,
                    durationMs = event.durationMs,
                )
            WeighingLeadershipVideoPlaybackAction.WATCH_SUMMARY ->
                AnalyticsFunnels.trackWeighingLeadershipVideoWatchSummary(
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
            WeighingLeadershipVideoPlaybackAction.PLAYBACK_ERROR -> {
                val reason = event.reason ?: "unknown"
                crashReporter.recordException(
                    IllegalStateException(reason),
                    "weighing leadership video playback failed",
                )
                AnalyticsFunnels.trackWeighingLeadershipVideoPlaybackError(analytics, event.proofId, reason)
            }
        }
    }

    private fun toUi(shed: WeighingLeadershipShed): WeighingLeadershipShedUi {
        val shedUiId = shed.leadershipVideosIdentityKey()
        return WeighingLeadershipShedUi(
            id = shedUiId,
            campaignShedId = shed.campaignShedId,
            name = shed.operationalLocationDisplay.ifBlank { shed.shedName },
            status = shed.status,
            periodLabel = formatPeriodLabel(shed.periodLabel),
            category = shed.category,
            animals = shed.animals.map { animal ->
                WeighingLeadershipAnimalUi(
                    // The backend's own record id keys the row; a positional key re-keys every row
                    // past any change when a fresh page lands.
                    id = animal.observationId.ifBlank { "$shedUiId:${animal.rfid}" },
                    rfid = animal.rfid,
                    weight = formatWeight(animal.weightKg),
                    timestamp = formatTimestamp(animal.acceptedAt),
                    videos = animal.videos.mapIndexed { videoIndex, video ->
                        WeighingLeadershipVideoUi(
                            "$shedUiId:${video.proofId}:$videoIndex",
                            "Video ${videoIndex + 1}",
                            video.downloadUrl.toAbsoluteApiUrl(),
                        )
                    },
                )
            },
            animalCount = shed.animalCount?.toString(),
            totalWeight = shed.totalWeightKg?.let(::formatWeight),
            averageWeight = shed.averageWeightKg?.let(::formatWeight),
            videos = shed.videos.mapIndexed { index, video ->
                WeighingLeadershipVideoUi("$shedUiId:${video.proofId}:$index", "Video ${index + 1}", video.downloadUrl.toAbsoluteApiUrl())
            },
        )
    }

    private fun WeighingLeadershipShed.leadershipVideosIdentityKey(): String =
        listOf(shedKey, category, periodLabel, status)
            .joinToString(":")

    private fun formatWeight(value: Double): String =
        String.format(Locale.US, "%.2f kg", value).replace(".00 kg", " kg")

    private fun formatTimestamp(value: String): String =
        runCatching {
            // exception:exempt UI display fallback; unparseable timestamp displays raw value
            timestampFormatter.format(Instant.parse(value))
        }.getOrDefault(value)

    private fun formatPeriodLabel(value: String): String {
        val parts = value.split(" - ")
        if (parts.size != 2) return value
        val start = runCatching {
            // exception:exempt UI display fallback; unparseable date returns raw value
            LocalDate.parse(parts[0].trim())
        }.getOrNull() ?: return value
        val end = runCatching {
            // exception:exempt UI display fallback; unparseable date returns raw value
            LocalDate.parse(parts[1].trim())
        }.getOrNull() ?: return value
        return "${periodFormatter.format(start)} - ${periodFormatter.format(end)}"
    }

    private fun String.toAbsoluteApiUrl(): String =
        when {
            startsWith("http://") || startsWith("https://") -> this
            startsWith("/") -> BuildConfig.API_BASE_URL.trimEnd('/') + this
            else -> this
        }

    private companion object {
        const val LIST_PREFETCH_DISTANCE = 3
        val timestampFormatter: DateTimeFormatter =
            DateTimeFormatter.ofPattern("dd MMM yyyy, h:mm a").withZone(ZoneId.of("Asia/Kolkata"))
        val periodFormatter: DateTimeFormatter =
            DateTimeFormatter.ofPattern("d MMM", Locale.US)
    }
}
