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
import sg.mesha.goatos.core.analytics.AnalyticsEventsVerification
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.DeadControlWatchdog
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
import sg.mesha.goatos.feature.verify.VerifyDetailEntryUiState
import sg.mesha.goatos.feature.verify.VerifyDetailEvent
import sg.mesha.goatos.feature.verify.VerifyDetailUiState
import sg.mesha.goatos.feature.verify.VerifyMediaItem
import sg.mesha.goatos.feature.verify.VerifyTone
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
    private val status: String? = savedStateHandle.get<String>("status")
    private val businessDate: String? = savedStateHandle.get<String>("businessDate")
    private val missed: Boolean = savedStateHandle.get<Boolean>("missed") ?: false

    /**
     * Status to re-query the queue with when resolving THIS item.
     *
     * The backend silently defaults a blank/absent status to `pending`
     * (verification/app/service.go: `if params.Status == "" && !params.IncludeAllStatuses`), and the
     * nav route does not forward the queue's selected status. So opening an ALREADY-APPROVED item
     * from the "Approved" filter re-queried with no status, got back only pending rows, found
     * nothing, and rendered "No video attached to this item" -- on an item whose media was intact
     * at every layer (verified: the queue endpoint returns full media arrays with download_url for
     * every approved item). Pending items only worked because the accidental default matched.
     *
     * "all" is the backend's documented explicit no-filter sentinel (handler.go `statusAll`). The
     * detail screen resolves ONE item by group key out of whatever page it fetches, so it must
     * never inherit the queue's ambient "pending" default.
     */
    private val effectiveStatus: String = status?.takeIf { it.isNotBlank() } ?: "all"

    private val _flags = MutableStateFlow(VerifyDetailFlags())
    private val watchTimeByProof = mutableMapOf<String, Long>()
    private var trackedItemOpened = false

    // Item ids for which [AnalyticsEventsVerification.VERIFY_DECISION_UNAVAILABLE] has already
    // fired with EVIDENCE_UNAVAILABLE this screen visit — the entry map is recomputed on every
    // Room emission, so without this the event would fire once per recomposition/emission
    // instead of once per genuine transition into "stuck" state.
    private val trackedEvidenceUnavailableItemIds = mutableSetOf<String>() // mobile-guard:ignore: bounded by this screen's item set (single shed/queue scope), cleared with the ViewModel on screen exit

    // Dead-control watchdog for the play/pause control (docs/observability/
    // TELEMETRY_GUARDRAILS.md): one per proof id so two clips in the same shed group never share
    // (and falsely clear) each other's pending watchdog. Cancelled in [onCleared] so a screen exit
    // never fires a report against a torn-down ViewModel.
    private val playWatchdogs = mutableMapOf<String, DeadControlWatchdog>()

    private fun playWatchdogFor(proofId: String): DeadControlWatchdog =
        playWatchdogs.getOrPut(proofId) {
            AnalyticsFunnels.newVerifyVideoPlayWatchdog(analytics, crashReporter, viewModelScope)
        }
    private val observedQueue: Flow<Resource<VerificationQueueResponseDto>> =
        if (isActionMode) {
            repo.observeActionQueue(category = category, parkId = parkId, shedId = shedId, limit = VERIFY_DETAIL_PAGE_SIZE)
        } else {
            repo.observeQueue(
                category = category,
                status = effectiveStatus,
                businessDate = businessDate,
                missed = missed,
                parkId = parkId,
                shedId = shedId,
                limit = VERIFY_DETAIL_PAGE_SIZE,
            )
        }

    // Cache-first: the tapped row's category scope Room cache already holds this GROUP's full
    // media + context (docs/decisions/android-offline-first.md), lifecycle-aware via
    // WhileSubscribed(5_000) like every other observed-Room StateFlow in this app.
    //
    // The route's `itemId` arg is really a GROUP key: the backend now emits one
    // verification_item PER GOAT (source_ref_type=vaccination_goat), and every per-goat item
    // produced from one shed submission shares one `source.submissionId`. That is the grouping
    // key here — with the item's OWN itemId as the fallback for a legacy bundled item
    // (ref_type=sop_submission, several clips under one verdict) or any item with no siblings,
    // so a lone item still resolves to a group of exactly itself.
    private val observedGroup: StateFlow<List<VerificationQueueItem>> =
        observedQueue
            .map { resource -> resource.data?.items?.filter { it.verificationGroupKey() == itemId }.orEmpty() }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    val state: StateFlow<VerifyDetailUiState> = combine(
        observedGroup,
        _flags,
    ) { items, flags ->
        items.toUiState(flags = flags)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VerifyDetailUiState(itemId = itemId))

    init {
        viewModelScope.launch {
            observedGroup.collect { items ->
                val first = items.firstOrNull()
                if (!trackedItemOpened && first != null) {
                    trackedItemOpened = true
                    AnalyticsFunnels.trackVerifyItemOpened(
                        analytics = analytics,
                        itemId = itemId,
                        category = first.category.ifBlank { category.orEmpty() },
                    )
                }
            }
        }
        viewModelScope.launch {
            state.collect { current -> trackDecisionUnavailable(current.entries) }
        }
        refresh()
    }

    /**
     * Emits [AnalyticsEventsVerification.VERIFY_DECISION_UNAVAILABLE] the FIRST time each entry
     * in this group is observed with [VerifyDecisionUnavailableReason.EVIDENCE_UNAVAILABLE] —
     * i.e. Approve is rendered disabled because the evidence could not be confirmed watchable.
     * This is a real operator-facing dead end (see [VerifyDetailViewModel] class doc / R50-017),
     * not merely a loading placeholder, so it must be visible in analytics.
     */
    private fun trackDecisionUnavailable(entries: List<VerifyDetailEntryUiState>) {
        entries
            .filter { it.decisionUnavailableReason == VerifyDecisionUnavailableReason.EVIDENCE_UNAVAILABLE }
            .forEach { entry ->
                if (trackedEvidenceUnavailableItemIds.add(entry.itemId)) {
                    runCatching {
                        analytics.track(
                            AnalyticsEventsVerification.VERIFY_DECISION_UNAVAILABLE,
                            mapOf(
                                AnalyticsFunnels.Params.ITEM_ID to entry.itemId,
                                AnalyticsEventsVerification.Params.REASON to
                                    VerifyDecisionUnavailableReason.EVIDENCE_UNAVAILABLE.name,
                            ),
                        )
                    }
                }
            }
    }

    fun onEvent(event: VerifyDetailEvent) {
        when (event) {
            VerifyDetailEvent.Close -> trackClosed()
            VerifyDetailEvent.Refresh -> refresh()
            is VerifyDetailEvent.Approve ->
                submitVerdict(event.itemId ?: itemId, VerificationDecision.APPROVED, reason = null)
            is VerifyDetailEvent.Reject ->
                submitVerdict(event.itemId ?: itemId, VerificationDecision.REJECTED, reason = event.reason)
            is VerifyDetailEvent.VideoPlayback -> trackVideoPlayback(event)
            is VerifyDetailEvent.RejectDialogOpened -> AnalyticsFunnels.trackVerifyRejectDialogOpened(analytics, event.itemId)
            is VerifyDetailEvent.RejectDialogCancelled -> AnalyticsFunnels.trackVerifyRejectDialogCancelled(analytics, event.itemId)
            is VerifyDetailEvent.RejectBlockedEmptyReason ->
                AnalyticsFunnels.trackVerifyRejectBlockedEmptyReason(analytics, event.itemId)
            is VerifyDetailEvent.ApproveDialogOpened -> AnalyticsFunnels.trackVerifyApproveDialogOpened(analytics, event.itemId)
            is VerifyDetailEvent.ApproveDialogCancelled -> AnalyticsFunnels.trackVerifyApproveDialogCancelled(analytics, event.itemId)
        }
    }

    /** Fires once per screen exit (nav host calls this before popping back). [reason] is
     *  `fully_decided` when every entry in this group is terminal, `abandoned` otherwise — the
     *  signal that distinguishes a shed the verifier finished from one she walked away from
     *  mid-review. */
    private fun trackClosed() {
        val entries = state.value.entries
        val reason = if (entries.isNotEmpty() && entries.none { it.statusTone == VerifyTone.PENDING }) {
            "fully_decided"
        } else {
            "abandoned"
        }
        AnalyticsFunnels.trackVerifyItemClosed(analytics, itemId, reason)
    }

    private fun refresh() = viewModelScope.launch {
        _flags.update { it.copy(isRefreshing = true) }
        val result = if (isActionMode) {
            repo.refreshActionQueue(category = category, parkId = parkId, shedId = shedId, limit = VERIFY_DETAIL_PAGE_SIZE)
        } else {
            repo.refreshQueue(
                category = category,
                status = effectiveStatus,
                businessDate = businessDate,
                missed = missed,
                parkId = parkId,
                shedId = shedId,
                limit = VERIFY_DETAIL_PAGE_SIZE,
            )
        }
        _flags.update { it.copy(isRefreshing = false, isOffline = result.isFailure) }
    }

    /**
     * [targetItemId] is the ONE animal's item this verdict decides. Every other item sharing
     * this shed's group stays exactly as it was — no shared verdict, no shared enable/disable
     * state, matching [SyncRepository.enqueueVerificationVerdict]'s own per-item_id outbox key.
     */
    private fun submitVerdict(targetItemId: String, decision: String, reason: String?) = viewModelScope.launch {
        // Belt-and-braces guard mirroring the reject dialog's own mandatory-reason validation —
        // a malformed event can never enqueue a reason-less reject.
        if (decision == VerificationDecision.REJECTED && reason.isNullOrBlank()) return@launch
        val targetEntry = state.value.entries.firstOrNull { it.itemId == targetItemId }
        // Same belt-and-braces shape for the irreversible side: an approve can never be enqueued
        // from a screen state where THIS animal's evidence is not watchable, whatever produced
        // the event — a sibling animal's healthy evidence must never let this one through.
        if (decision == VerificationDecision.APPROVED && targetEntry?.isApproveEnabled != true) return@launch

        val rowVersion = observedGroup.value.firstOrNull { it.itemId == targetItemId }?.rowVersion ?: 1
        _flags.update { it.copy(isSubmitting = true, awaitingBackendDecision = false, autoCloseAfterDecision = false, errorMessage = null) }
        AnalyticsFunnels.trackVerifyVerdictAttempted(analytics, targetItemId, decision, totalWatchTimeMs())
        val result = syncRepo.enqueueVerificationVerdict(
            itemId = targetItemId,
            decision = decision,
            reason = reason,
            rowVersion = rowVersion,
        )
        when (result) {
            is AppResult.Ok -> {
                _flags.update { it.copy(awaitingBackendDecision = true) }
                val waitError = waitForBackendDecision(targetItemId, result.value)
                if (waitError == null) {
                    // Auto-close the SCREEN only once every animal in this group has a terminal
                    // verdict — a single legacy/bundled item (group of one) closes immediately,
                    // same as before; a multi-animal shed keeps the verifier here to work through
                    // the rest, exactly the fix this task exists for (one reject must not evict
                    // her from the shed's other, still-pending, animals).
                    val stillPending = observedGroup.value.any { it.status == VerificationStatus.PENDING }
                    _flags.update {
                        it.copy(isSubmitting = false, awaitingBackendDecision = false, autoCloseAfterDecision = !stillPending)
                    }
                    AnalyticsFunnels.trackVerifyVerdictSucceeded(analytics, targetItemId, decision, totalWatchTimeMs())
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
                AnalyticsFunnels.trackVerifyVerdictFailed(analytics, targetItemId, decision, result.message)
                watchTimeByProof.clear()
            }
        }
    }

    private fun trackVideoPlayback(event: VerifyDetailEvent.VideoPlayback) {
        when (event.action) {
            VideoPlaybackAction.PLAY_INTENT -> {
                val props = mapOf(
                    AnalyticsFunnels.Params.ITEM_ID to itemId,
                    AnalyticsFunnels.Params.PROOF_ID to event.proofSubject,
                    AnalyticsFunnels.Params.PLAYER_STATE to (event.playerState ?: "unknown"),
                    AnalyticsFunnels.Params.ARMED to (event.armed?.toString() ?: "unknown"),
                    AnalyticsFunnels.Params.TARGET_ACTION to (event.targetAction ?: "unknown"),
                )
                playWatchdogFor(event.proofSubject)
                    .armIntent(props, AnalyticsFunnels.VERIFY_VIDEO_PLAY_WATCHDOG_TIMEOUT_MS)
            }
            VideoPlaybackAction.PLAY_OUTCOME -> playWatchdogFor(event.proofSubject).disarm()
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
            VideoPlaybackAction.FULLSCREEN_OPENED ->
                AnalyticsFunnels.trackVerifyVideoFullscreenOpened(analytics, itemId, event.proofSubject)
            VideoPlaybackAction.FULLSCREEN_EXITED ->
                AnalyticsFunnels.trackVerifyVideoFullscreenExited(analytics, itemId, event.proofSubject)
        }
    }

    private fun totalWatchTimeMs(): Long = watchTimeByProof.values.sum()

    override fun onCleared() {
        super.onCleared()
        // Screen exit is not a dead control — cancel every pending watchdog so leaving mid-play
        // never fires a false-positive report against this now-cleared ViewModel.
        playWatchdogs.values.forEach { it.cancel() }
        playWatchdogs.clear()
    }

    private suspend fun waitForBackendDecision(targetItemId: String, outboxItemId: String): String? {
        repeat(30) {
            syncRepo.triggerDrain()
            delay(250)
            when (val outbox = syncRepo.findOutboxItem(outboxItemId)) {
                is AppResult.Ok -> {
                    val item = outbox.value
                    when {
                        item?.status == SyncItemStatus.SUCCEEDED -> {
                            if (!isActionMode) {
                                repo.markVerificationItemDecidedLocally(targetItemId)
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
            val current = observedGroup.value.firstOrNull { it.itemId == targetItemId }
            if (current == null || current.status != VerificationStatus.PENDING) {
                if (!isActionMode) {
                    repo.markVerificationItemDecidedLocally(targetItemId)
                }
                return null
            }
            delay(320)
        }
        return "Decision saved locally; waiting for backend sync."
    }

    private fun VerificationQueueItem.toEntry(flags: VerifyDetailFlags): VerifyDetailEntryUiState {
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
        // Scoped to THIS animal's own proof ids only — a sibling animal's playback failure must
        // never gate this one's Approve.
        val nothingFailedToPlay = media.none { flags.unplayableProofIds.contains(it.proofId) }
        val evidenceIsWatchable = evidenceAvailable && everyProofLinked && nothingFailedToPlay
        val isOpen = !isActionMode && effectiveStatus == VerificationStatus.PENDING
        // Approve needs watchable evidence. Reject/rework must stay available on an open item even
        // when the video will not load — that is the only correct move left, and blocking it would
        // strand the verifier.
        val canApprove = isOpen && evidenceIsWatchable
        val canReject = isOpen
        return VerifyDetailEntryUiState(
            itemId = itemId,
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
            statusTone = statusTone(effectiveStatus),
            rowVersion = rowVersion,
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
            isSubmitting = false,
        )
    }

    private fun List<VerificationQueueItem>.toUiState(flags: VerifyDetailFlags): VerifyDetailUiState {
        val first = firstOrNull()
        if (first == null) {
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
                entries = emptyList(),
                isGroupFullyDecided = false,
            )
        }
        // rowVersion order is stable regardless of refresh re-ordering, matching the queue's own
        // shed-card ordering: subject label (falls back to the item's own display order otherwise).
        val entries = sortedBy { it.subjectLabel ?: it.itemId }.map { it.toEntry(flags) }
        // Server-confirmed only: every entry in this group carries a status that is no longer
        // PENDING in the Room-observed queue snapshot — never a locally-guessed "must be done"
        // before the backend's own read confirms it.
        val isGroupFullyDecided = entries.isNotEmpty() && entries.none { it.statusTone == VerifyTone.PENDING }
        val singleEntry = entries.singleOrNull()
        return VerifyDetailUiState(
            itemId = itemId,
            category = first.category,
            categoryLabel = humanizeCategory(first.category),
            subjectLabel = first.subjectLabel?.takeIf { it.isNotBlank() },
            media = singleEntry?.media.orEmpty(),
            context = buildContext(first),
            statusTone = singleEntry?.statusTone ?: statusTone(first.status),
            rowVersion = singleEntry?.rowVersion ?: first.rowVersion,
            isCloseMode = isActionMode,
            isCloseEnabled = false,
            verdictReason = singleEntry?.verdictReason,
            isApproveEnabled = singleEntry?.isApproveEnabled ?: false,
            isRejectEnabled = singleEntry?.isRejectEnabled ?: false,
            decisionUnavailableReason = singleEntry?.decisionUnavailableReason ?: VerifyDecisionUnavailableReason.NONE,
            isSubmitting = flags.isSubmitting,
            isRefreshing = flags.isRefreshing,
            lastSyncedAt = null,
            isOffline = flags.isOffline,
            errorMessage = flags.errorMessage,
            autoCloseAfterDecision = flags.autoCloseAfterDecision,
            entries = entries,
            isGroupFullyDecided = isGroupFullyDecided,
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
            // Last, and only when present: the operator's reason for raising this work, so the
            // reviewer reads it beside the video instead of judging the evidence without it.
            item.subjectNote?.takeIf { it.isNotBlank() }?.let { VerifyContextRow(VerifyContextKind.RAISED_NOTE, it) },
        )
    }
}
