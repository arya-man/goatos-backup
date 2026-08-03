package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.VerificationDecision
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationStatus
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.feature.verify.VerifyContextKind
import sg.mesha.goatos.feature.verify.VerifyContextRow
import sg.mesha.goatos.feature.verify.VerifyDecisionUnavailableReason
import sg.mesha.goatos.feature.verify.VerifyDetailEvent
import sg.mesha.goatos.feature.verify.VerifyDetailUiState
import sg.mesha.goatos.feature.verify.VerifyMediaItem
import sg.mesha.goatos.feature.verify.VideoPlaybackAction
import javax.inject.Inject

/** Transient (non-Room) UI flags, combined with the Room-observed item below. */
private data class VerifyDetailFlags(
    val isRefreshing: Boolean = false,
    val isOffline: Boolean = false,
    val isSubmitting: Boolean = false,
    val errorMessage: String? = null,
    val awaitingBackendDecision: Boolean = false,
    val autoCloseAfterDecision: Boolean = false,
    /** Proof ids whose player actually failed to load on this screen. A signed URL string is NOT
     *  evidence that the video exists — the object behind it can be gone while the link still
     *  resolves — so a real playback failure is the honest client-side "she cannot see this". */
    val unplayableProofIds: Set<String> = emptySet(),
)

private const val VERIFY_DETAIL_PAGE_SIZE = 20

/**
 * The standalone Verifier section's detail state holder (context/architecture/
 * verifier-app-and-flow.md). Offline-first: this screen is ALWAYS opened from a queue row the
 * verifier just tapped, so the item is guaranteed to already be in that category scope's Room
 * cache ([VerificationRepository.observeQueue]) — no separate "get one item" network call is
 * needed for the initial render, matching the design doc's shape (the queue response already
 * carries each item's full media + context). [refresh] still re-pulls the first page of that
 * scope so a stale/offline banner is honest here too.
 *
 * Approve/Reject go through the offline-sync outbox ([SyncRepository.enqueueVerificationVerdict])
 * exactly like every other write in this app — durable, idempotent, retried with backoff. The
 * verdict is not visually completed until the outbox has drained and a queue refresh confirms
 * the item is no longer pending. That keeps the verifier on this screen while the network call
 * is real, then auto-returns them to the reduced queue.
 */
@HiltViewModel
class VerifyDetailViewModel @Inject constructor(
    private val repo: VerificationRepository,
    private val syncRepo: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val itemId: String = savedStateHandle.get<String>("itemId").orEmpty()
    private val category: String? = savedStateHandle.get<String>("category")
    private val isActionMode: Boolean = savedStateHandle.get<Boolean>("actionMode") ?: false
    private val parkId: String? = savedStateHandle.get<String>("parkId")
    private val shedId: String? = savedStateHandle.get<String>("shedId")

    private val _flags = MutableStateFlow(VerifyDetailFlags())
    private val watchTimeByProof = mutableMapOf<String, Long>()
    private var trackedItemOpened = false
    private val observedQueue: Flow<Resource<VerificationQueueResponseDto>> =
        if (isActionMode) {
            repo.observeActionQueue(category = category, parkId = parkId, shedId = shedId, limit = VERIFY_DETAIL_PAGE_SIZE)
        } else {
            repo.observeQueue(category = category, parkId = parkId, shedId = shedId, limit = VERIFY_DETAIL_PAGE_SIZE)
        }

    // Cache-first: the tapped row's category scope Room cache already holds this item's full
    // media + context (docs/decisions/android-offline-first.md), lifecycle-aware via
    // WhileSubscribed(5_000) like every other observed-Room StateFlow in this app.
    private val observedItem: StateFlow<VerificationQueueItem?> =
        observedQueue
            .map { resource -> resource.data?.items?.firstOrNull { it.itemId == itemId } }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    val state: StateFlow<VerifyDetailUiState> = combine(
        observedItem,
        _flags,
    ) { item, flags ->
        item.toUiState(flags = flags)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VerifyDetailUiState(itemId = itemId))

    init {
        viewModelScope.launch {
            observedItem.collect { item ->
                if (!trackedItemOpened && item != null) {
                    trackedItemOpened = true
                    AnalyticsFunnels.trackVerifyItemOpened(
                        analytics = analytics,
                        itemId = itemId,
                        category = item.category.ifBlank { category.orEmpty() },
                    )
                }
            }
        }
        refresh()
    }

    fun onEvent(event: VerifyDetailEvent) {
        when (event) {
            VerifyDetailEvent.Close -> Unit // navigation — handled by the nav host.
            VerifyDetailEvent.Refresh -> refresh()
            VerifyDetailEvent.Approve -> submitVerdict(VerificationDecision.APPROVED, reason = null)
            is VerifyDetailEvent.Reject -> submitVerdict(VerificationDecision.REJECTED, reason = event.reason)
            is VerifyDetailEvent.VideoPlayback -> trackVideoPlayback(event)
        }
    }

    private fun refresh() = viewModelScope.launch {
        _flags.update { it.copy(isRefreshing = true) }
        val result = if (isActionMode) {
            repo.refreshActionQueue(category = category, parkId = parkId, shedId = shedId, limit = VERIFY_DETAIL_PAGE_SIZE)
        } else {
            repo.refreshQueue(category = category, parkId = parkId, shedId = shedId, limit = VERIFY_DETAIL_PAGE_SIZE)
        }
        _flags.update { it.copy(isRefreshing = false, isOffline = result.isFailure) }
    }

    private fun submitVerdict(decision: String, reason: String?) = viewModelScope.launch {
        // Belt-and-braces guard mirroring the reject dialog's own mandatory-reason validation —
        // a malformed event can never enqueue a reason-less reject.
        if (decision == VerificationDecision.REJECTED && reason.isNullOrBlank()) return@launch
        // Same belt-and-braces shape for the irreversible side: an approve can never be enqueued
        // from a screen state where the evidence is not watchable, whatever produced the event.
        if (decision == VerificationDecision.APPROVED && !state.value.isApproveEnabled) return@launch

        val rowVersion = observedItem.value?.rowVersion ?: 1
        _flags.update { it.copy(isSubmitting = true, awaitingBackendDecision = false, autoCloseAfterDecision = false, errorMessage = null) }
        AnalyticsFunnels.trackVerifyVerdictAttempted(analytics, itemId, decision, totalWatchTimeMs())
        val result = syncRepo.enqueueVerificationVerdict(
            itemId = itemId,
            decision = decision,
            reason = reason,
            rowVersion = rowVersion,
        )
        when (result) {
            is AppResult.Ok -> {
                _flags.update { it.copy(awaitingBackendDecision = true) }
                val waitError = waitForBackendDecision(result.value)
                if (waitError == null) {
                    _flags.update { it.copy(isSubmitting = false, awaitingBackendDecision = false, autoCloseAfterDecision = true) }
                    AnalyticsFunnels.trackVerifyVerdictSucceeded(analytics, itemId, decision, totalWatchTimeMs())
                    watchTimeByProof.clear()
                } else {
                    _flags.update {
                        it.copy(
                            isSubmitting = false,
                            awaitingBackendDecision = false,
                            errorMessage = waitError,
                        )
                    }
                }
            }
            is AppResult.Err -> {
                _flags.update { it.copy(isSubmitting = false, awaitingBackendDecision = false, errorMessage = result.message) }
                result.cause?.let { error ->
                    runCatching { crashReporter.recordException(error, "verification verdict enqueue failed") }
                }
                AnalyticsFunnels.trackVerifyVerdictFailed(analytics, itemId, decision, result.message)
                watchTimeByProof.clear()
            }
        }
    }

    private fun trackVideoPlayback(event: VerifyDetailEvent.VideoPlayback) {
        when (event.action) {
            VideoPlaybackAction.PLAY_STARTED ->
                AnalyticsFunnels.trackVerifyVideoPlayStarted(
                    analytics = analytics,
                    itemId = itemId,
                    proofId = event.proofSubject,
                    mimeType = event.mimeType,
                    durationMs = event.durationMs,
                )
            VideoPlaybackAction.WATCH_SUMMARY -> {
                watchTimeByProof[event.proofSubject] = (watchTimeByProof[event.proofSubject] ?: 0L) + event.watchTimeMs.coerceAtLeast(0)
                AnalyticsFunnels.trackVerifyVideoWatchSummary(
                    analytics = analytics,
                    itemId = itemId,
                    proofId = event.proofSubject,
                    mimeType = event.mimeType,
                    watchTimeMs = event.watchTimeMs,
                    durationMs = event.durationMs,
                    positionMs = event.positionMs,
                    percentWatched = event.percentWatched,
                    seekCount = event.seekCount,
                    replayCount = event.replayCount,
                    bufferingTimeMs = event.bufferingTimeMs,
                )
            }
            VideoPlaybackAction.PLAYBACK_ERROR -> {
                val reason = event.reason ?: "unknown"
                // The player told us the truth the URL could not: this proof will not play. Approve
                // must go dead for this item; Reject/rework stays open (see toUiState).
                _flags.update { it.copy(unplayableProofIds = it.unplayableProofIds + event.proofSubject) }
                runCatching { crashReporter.recordException(IllegalStateException(reason), "verification video playback failed") }
                AnalyticsFunnels.trackVerifyVideoPlaybackError(analytics, itemId, event.proofSubject, reason)
            }
            VideoPlaybackAction.FULLSCREEN_OPENED -> Unit
        }
    }

    private fun totalWatchTimeMs(): Long = watchTimeByProof.values.sum()

    private suspend fun waitForBackendDecision(outboxItemId: String): String? {
        repeat(30) {
            syncRepo.triggerDrain()
            delay(250)
            when (val outbox = syncRepo.findOutboxItem(outboxItemId)) {
                is AppResult.Ok -> {
                    val item = outbox.value
                    when {
                        item?.status == SyncItemStatus.SUCCEEDED -> {
                            if (!isActionMode) {
                                repo.markVerificationItemDecidedLocally(itemId)
                            }
                            refresh()
                            return null
                        }
                        item?.status == SyncItemStatus.FAILED && (item.conflict || item.isDeadLetter) ->
                            return item.lastError ?: "Backend rejected the verification decision."
                    }
                }
                is AppResult.Err -> Unit
            }
            val result = if (isActionMode) repo.refreshActionQueue(category = category) else repo.refreshQueue(category = category)
            _flags.update { flags -> flags.copy(isOffline = result.isFailure) }
            val current = observedItem.value
            if (current == null || current.status != VerificationStatus.PENDING) {
                if (!isActionMode) {
                    repo.markVerificationItemDecidedLocally(itemId)
                }
                return null
            }
            delay(320)
        }
        return "Decision saved locally; waiting for backend sync."
    }

    private fun VerificationQueueItem?.toUiState(flags: VerifyDetailFlags): VerifyDetailUiState {
        if (this == null) {
            return VerifyDetailUiState(
                itemId = itemId,
                isCloseMode = isActionMode,
                isRefreshing = flags.isRefreshing,
                isOffline = flags.isOffline,
                isSubmitting = flags.isSubmitting,
                errorMessage = flags.errorMessage,
                isApproveEnabled = false,
                isRejectEnabled = false,
                autoCloseAfterDecision = flags.autoCloseAfterDecision,
            )
        }
        val effectiveStatus = status
        // What "evidence available" honestly means on this screen:
        //  - the server resolved a link for EVERY media_ref (any() would have passed an item whose
        //    second proof silently vanished), and
        //  - no proof on this screen has actually failed to play.
        // downloadUrl.isNotBlank() alone proves nothing: the string is present even when the stored
        // object is gone, which is exactly how Approve stayed enabled over evidence that no longer
        // existed. The backend runs the authoritative existence check at verdict time; this is the
        // honest client half of it.
        val everyProofLinked = media.isNotEmpty() && media.all { it.downloadUrl.isNotBlank() }
        val nothingFailedToPlay = media.none { flags.unplayableProofIds.contains(it.proofId) }
        val evidenceIsWatchable = evidenceAvailable && everyProofLinked && nothingFailedToPlay
        val isOpen = !isActionMode && effectiveStatus == VerificationStatus.PENDING
        // Approve needs watchable evidence. Reject/rework must stay available on an open item even
        // when the video will not load — that is the only correct move left, and blocking it would
        // strand the verifier.
        val canApprove = isOpen && evidenceIsWatchable
        val canReject = isOpen
        return VerifyDetailUiState(
            itemId = itemId,
            category = category,
            categoryLabel = humanizeCategory(category),
            subjectLabel = subjectLabel?.takeIf { it.isNotBlank() },
            media = media.map {
                VerifyMediaItem(
                    signedUrl = absoluteDownloadUrl(it.downloadUrl),
                    mimeType = it.mimeType ?: "",
                    proofSubject = it.proofId,
                    taskTitle = it.label?.takeIf(String::isNotBlank),
                    answer = it.answer?.takeIf(String::isNotBlank),
                )
            },
            context = buildContext(this),
            statusTone = statusTone(effectiveStatus),
            rowVersion = rowVersion,
            isCloseMode = isActionMode,
            isCloseEnabled = false,
            verdictReason = verdictReason,
            // R50-017: the backend now fails evidence resolution closed instead of silently
            // omitting media, so a verdict with no resolvable evidence must stay disabled even
            // though the item itself is still PENDING.
            isApproveEnabled = canApprove,
            isRejectEnabled = canReject,
            decisionUnavailableReason = when {
                isActionMode || canApprove -> VerifyDecisionUnavailableReason.NONE
                isOpen -> VerifyDecisionUnavailableReason.EVIDENCE_UNAVAILABLE
                else -> VerifyDecisionUnavailableReason.ALREADY_DECIDED
            },
            isSubmitting = flags.isSubmitting,
            isRefreshing = flags.isRefreshing,
            lastSyncedAt = null,
            isOffline = flags.isOffline,
            errorMessage = flags.errorMessage,
            autoCloseAfterDecision = flags.autoCloseAfterDecision,
        )
    }

    private fun absoluteDownloadUrl(url: String): String {
        val trimmed = url.trim()
        if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) return trimmed
        if (!trimmed.startsWith("/")) return trimmed
        return BuildConfig.API_BASE_URL.trimEnd('/') + trimmed
    }

    private fun buildContext(item: VerificationQueueItem): List<VerifyContextRow> {
        // Backend-owned display labels: never render raw UUIDs (STATUS-003).
        return listOfNotNull(
            item.shedLabel?.takeIf { it.isNotBlank() }?.let { VerifyContextRow(VerifyContextKind.SHED, it) },
            item.parkLabel?.takeIf { it.isNotBlank() }?.let { VerifyContextRow(VerifyContextKind.PARK, it) },
            item.operatorName?.takeIf { it.isNotBlank() }?.let { VerifyContextRow(VerifyContextKind.OPERATOR, it) },
            item.capturedAt.takeIf { it.isNotBlank() }?.let { VerifyContextRow(VerifyContextKind.CAPTURED_AT, it) },
        )
    }
}
