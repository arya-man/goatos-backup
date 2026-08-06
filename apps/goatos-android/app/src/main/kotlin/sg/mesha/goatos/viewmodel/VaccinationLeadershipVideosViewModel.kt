package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.launchIn
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.vaccination.leadership.VACCINATION_LEADERSHIP_MAX_WINDOW
import sg.mesha.goatos.core.data.vaccination.leadership.VACCINATION_LEADERSHIP_PAGE_SIZE
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipDriveClosureUi
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipLocationOptionUi
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipMediaUi
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideoEvent
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideoPlaybackAction
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideoPlaybackEvent
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideosUiState
import sg.mesha.goatos.core.network.dto.VerificationDriveClosureDto
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto

/** Producer category for vaccination proof verification items (matches VACCINATION_CATEGORY in
 *  VerifyQueueViewModel — this screen and the verifier's queue read the SAME backend category,
 *  just through separate routes/composables/ViewModels, per the leadership-vs-verifier lock. */
private const val VACCINATION_PROOF_CATEGORY = "vaccination_proof"

/**
 * The vaccination leadership videos gallery, rendered FROM ROOM.
 *
 * It observes cached verification items for the FULL evidence trail (status=all: pending,
 * approved, rejected, closed) plus this category's park/shed filter options and any
 * drive-closures ready for leadership `verification.act` (never `verification.verdict` -- no
 * approve/reject exists on this surface, see docs/decisions/leadership-vs-verifier-surface-separation.md).
 * The network refresh only writes into Room; a failed refresh leaves the cached gallery on screen
 * with a staleness note.
 *
 * This ViewModel reads the SAME generic [VerificationRepository.observeQueue]/[VerificationRepository.refreshQueue]
 * the verifier's queue uses (shared DATA-layer infra, not UI) so it gets filter options and
 * drive closures the narrower observeLeadershipVideos()/refreshLeadershipVideos() helpers (still
 * used by Weighing's leadership screen) do not expose. It does NOT import or depend on anything
 * under sg.mesha.goatos.feature.verify.
 */
@HiltViewModel
class VaccinationLeadershipVideosViewModel @Inject constructor(
    private val repository: VerificationRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val syncRepo: SyncRepository,
) : ViewModel() {
    private val window = MutableStateFlow(VACCINATION_LEADERSHIP_PAGE_SIZE)
    private val selectedParkId = MutableStateFlow<String?>(null)
    private val selectedShedId = MutableStateFlow<String?>(null)

    private val _uiState = MutableStateFlow(VaccinationLeadershipVideosUiState(loading = true))
    val state: StateFlow<VaccinationLeadershipVideosUiState> = _uiState.asStateFlow()

    /** Re-entrancy guard for [refresh]. Not the same signal as [VaccinationLeadershipVideosUiState.loading]:
     *  the state starts `loading = true` before the first [refresh] call runs, so guarding on the
     *  state field would make that very first call a no-op. */
    private var refreshInFlight = false

    @OptIn(ExperimentalCoroutinesApi::class)
    private val queueResource: StateFlow<Resource<VerificationQueueResponseDto>> =
        combine(window, selectedParkId, selectedShedId) { w, park, shed -> Triple(w, park, shed) }
            .flatMapLatest { (w, park, shed) ->
                repository.observeQueue(
                    category = VACCINATION_PROOF_CATEGORY,
                    status = "all", // Full trail -- see class doc. Never narrowed to open/pending.
                    parkId = park,
                    shedId = shed,
                    limit = w,
                )
            }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Resource(data = null, lastSyncedAt = null))

    init {
        analytics.track(AnalyticsEvents.VACCINATION_LEADERSHIP_VIDEO_VIEWED)
        queueResource.onEach { applyResource(it) }.launchIn(viewModelScope)
        refresh()
    }

    private fun applyResource(resource: Resource<VerificationQueueResponseDto>) {
        val data = resource.data
        _uiState.update { current ->
            if (data == null) {
                current
            } else {
                current.copy(
                    title = data.filterOptions.moduleLabel,
                    items = data.items.mapIndexed { index, item -> item.toUi(index) },
                    parkOptions = data.filterOptions.parks
                        ?.map { VaccinationLeadershipLocationOptionUi(it.id, it.label) }
                        .orEmpty(),
                    shedOptions = data.filterOptions.sheds
                        ?.map { VaccinationLeadershipLocationOptionUi(it.id, it.label) }
                        .orEmpty(),
                    driveClosures = data.driveClosures.map { it.toUi() },
                )
            }
        }
    }

    fun onEvent(event: VaccinationLeadershipVideoEvent) {
        when (event) {
            is VaccinationLeadershipVideoEvent.Refresh -> refresh()
            is VaccinationLeadershipVideoEvent.ItemVisible -> onItemVisible(event.index)
            is VaccinationLeadershipVideoEvent.PlaybackEvent -> onPlayback(event.event)
            is VaccinationLeadershipVideoEvent.SelectPark -> {
                selectedParkId.value = event.parkId
                selectedShedId.value = null
                window.value = VACCINATION_LEADERSHIP_PAGE_SIZE
                _uiState.update { it.copy(selectedParkId = event.parkId, selectedShedId = null) }
                refresh()
            }
            is VaccinationLeadershipVideoEvent.SelectShed -> {
                selectedShedId.value = event.shedId
                window.value = VACCINATION_LEADERSHIP_PAGE_SIZE
                _uiState.update { it.copy(selectedShedId = event.shedId) }
                refresh()
            }
            is VaccinationLeadershipVideoEvent.OpenItem -> _uiState.update { it.copy(selectedItemId = event.itemId) }
            is VaccinationLeadershipVideoEvent.CloseItem -> _uiState.update { it.copy(selectedItemId = null) }
            is VaccinationLeadershipVideoEvent.CloseDrive -> closeDrive(event.batchId)
        }
    }

    /** Re-reads the FIRST page into Room. Cached items stay on screen throughout. */
    private fun refresh() {
        if (refreshInFlight) return
        refreshInFlight = true
        _uiState.update { it.copy(loading = true) }
        window.value = VACCINATION_LEADERSHIP_PAGE_SIZE
        viewModelScope.launch {
            val outcome = repository.refreshQueue(
                category = VACCINATION_PROOF_CATEGORY,
                status = "all",
                parkId = selectedParkId.value,
                shedId = selectedShedId.value,
                limit = window.value,
            )
            outcome.fold(
                onSuccess = { _uiState.update { it.copy(error = null, staleNotice = "") } },
                onFailure = { t ->
                    crashReporter.recordException(t, "vaccination leadership videos load failed")
                    _uiState.update { state ->
                        if (state.items.isEmpty()) {
                            state.copy(error = t.message ?: "Failed to load videos")
                        } else {
                            state.copy(staleNotice = "Showing the last saved videos. ${t.message.orEmpty()}".trim())
                        }
                    }
                },
            )
            refreshInFlight = false
            _uiState.update { it.copy(loading = false) }
        }
    }

    /**
     * Scroll-driven prefetch: the gallery reports the item it just composed, and only an item
     * inside the tail window asks for the next page — of the network read and of the observed Room
     * window together. One page per trigger, never a tappable "Load more".
     */
    private fun onItemVisible(index: Int) {
        val loaded = _uiState.value.items.size
        if (loaded == 0 || index < loaded - LIST_PREFETCH_DISTANCE) return
        if (window.value < VACCINATION_LEADERSHIP_MAX_WINDOW) {
            window.value = (window.value + VACCINATION_LEADERSHIP_PAGE_SIZE).coerceAtMost(VACCINATION_LEADERSHIP_MAX_WINDOW)
        }
        if (_uiState.value.loading || _uiState.value.loadingMore) return
        _uiState.update { it.copy(loadingMore = true) }
        viewModelScope.launch {
            val outcome = repository.refreshQueue(
                category = VACCINATION_PROOF_CATEGORY,
                status = "all",
                parkId = selectedParkId.value,
                shedId = selectedShedId.value,
                limit = window.value,
            )
            outcome.onFailure { crashReporter.recordException(it, "vaccination leadership videos page load failed") }
            _uiState.update { it.copy(loadingMore = false) }
        }
    }

    /** Leadership's own `verification.act` closure -- see class doc. Reuses the SAME outbox write
     *  (`SyncRepository.enqueueVerificationBatchClose`) the verifier action queue uses for this
     *  endpoint; that write is generic idempotent DATA-layer infra, not verifier UI. */
    private fun closeDrive(batchId: String) {
        val trimmed = batchId.trim()
        if (trimmed.isEmpty()) return
        _uiState.update { it.copy(closingBatchId = trimmed, closeErrorBatchId = null, closeErrorMessage = null) }
        AnalyticsFunnels.trackVerifyDriveCloseAttempted(analytics, trimmed)
        viewModelScope.launch {
            when (val result = syncRepo.enqueueVerificationBatchClose(trimmed)) {
                is AppResult.Ok -> {
                    val error = waitForCloseSync(result.value)
                    _uiState.update { it.copy(closingBatchId = null) }
                    if (error == null) {
                        AnalyticsFunnels.trackVerifyDriveCloseSucceeded(analytics, trimmed)
                        refresh()
                    } else {
                        _uiState.update { it.copy(closeErrorBatchId = trimmed, closeErrorMessage = error) }
                        AnalyticsFunnels.trackVerifyDriveCloseFailed(analytics, trimmed, error)
                    }
                }
                is AppResult.Err -> {
                    _uiState.update {
                        it.copy(closingBatchId = null, closeErrorBatchId = trimmed, closeErrorMessage = result.message)
                    }
                    result.cause?.let { crashReporter.recordException(it, "vaccination leadership drive close enqueue failed") }
                    AnalyticsFunnels.trackVerifyDriveCloseFailed(analytics, trimmed, result.message)
                }
            }
        }
    }

    private suspend fun waitForCloseSync(outboxItemId: String): String? {
        repeat(30) {
            syncRepo.triggerDrain()
            delay(250)
            when (val item = syncRepo.findOutboxItem(outboxItemId)) {
                is AppResult.Ok -> {
                    val row = item.value
                    when {
                        row?.status == SyncItemStatus.SUCCEEDED -> return null
                        row?.status == SyncItemStatus.FAILED && (row.conflict || row.isDeadLetter) ->
                            return row.lastError ?: "Backend rejected the close action."
                    }
                }
                is AppResult.Err -> Unit
            }
            delay(320)
        }
        return "Close saved locally; waiting for backend sync."
    }

    /** Forwarded from the screen's player listener for telemetry and crash reporting. */
    private fun onPlayback(event: VaccinationLeadershipVideoPlaybackEvent) {
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

private fun VerificationQueueItem.toUi(index: Int): VaccinationLeadershipItemUi {
    val tone = when (status.lowercase()) {
        "pending_verification", "pending" -> "neutral"
        "approved" -> "success"
        "rework", "rejected" -> "error"
        "closed" -> "neutral"
        else -> "neutral"
    }
    return VaccinationLeadershipItemUi(
        id = itemId,
        title = subjectLabel?.ifEmpty { "Proof ${index + 1}" } ?: "Proof ${index + 1}",
        status = status,
        statusLabel = formatLeadershipStatus(status),
        statusTone = tone,
        timestamp = capturedAt.takeIf { it.isNotEmpty() }?.let { formatLeadershipCapturedAt(it) } ?: "Unknown time",
        proofCount = media.size.coerceAtLeast(1),
        videoUrls = media.map { it.downloadUrl },
        summary = subjectLabel.orEmpty(),
        shedLabel = shedLabel.orEmpty(),
        parkLabel = parkLabel.orEmpty(),
        operatorLabel = operatorName.orEmpty(),
        media = media.map {
            VaccinationLeadershipMediaUi(proofId = it.proofId, url = it.downloadUrl, mimeType = it.mimeType.orEmpty())
        },
    )
}

private fun VerificationDriveClosureDto.toUi() = VaccinationLeadershipDriveClosureUi(
    batchId = batchId,
    driveLabel = driveLabel,
    batchLabel = batchLabel,
    totalCount = totalCount,
    shedCount = shedCount,
    videoCount = videoCount,
    approvedVideos = approvedVideos,
    rejectedVideos = rejectedVideos,
    pendingVideos = pendingVideos,
    ready = ready,
)

private fun formatLeadershipStatus(status: String): String = when (status.lowercase()) {
    "pending_verification", "pending" -> "Pending Review"
    "approved" -> "Approved"
    "rejected" -> "Sent back"
    "rework" -> "Needs Rework"
    "closed" -> "Closed"
    else -> status
}

/** Farm-readable capture time in IST -- Goat OS business meaning is always Asia/Kolkata, never
 *  UTC. Unparseable input falls back to the original string rather than blanking the row. */
private fun formatLeadershipCapturedAt(raw: String): String = runCatching {
    java.time.Instant.parse(raw)
        .atZone(java.time.ZoneId.of("Asia/Kolkata"))
        .format(java.time.format.DateTimeFormatter.ofPattern("d MMM yyyy, h:mm a"))
}.getOrElse { raw }
