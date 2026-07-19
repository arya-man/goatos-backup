package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.VerificationDecision
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationStatus
import sg.mesha.goatos.feature.verify.VerifyContextKind
import sg.mesha.goatos.feature.verify.VerifyContextRow
import sg.mesha.goatos.feature.verify.VerifyDetailEvent
import sg.mesha.goatos.feature.verify.VerifyDetailUiState
import sg.mesha.goatos.feature.verify.VerifyMediaItem
import javax.inject.Inject

/** Transient (non-Room) UI flags, combined with the Room-observed item below. */
private data class VerifyDetailFlags(
    val isRefreshing: Boolean = false,
    val isOffline: Boolean = false,
    val isSubmitting: Boolean = false,
    val errorMessage: String? = null,
)

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
 * verdict is optimistic: once queued, the buttons disable immediately (verdict already recorded
 * from the verifier's point of view, [_localDecision]); a queue failure re-enables them with an
 * honest error and clears the optimistic flip.
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

    private val _flags = MutableStateFlow(VerifyDetailFlags())
    /** Optimistic local override once a verdict is queued — cleared by the next successful
     *  refresh (the cached item then reflects the server's own status). */
    private val _localDecision = MutableStateFlow<String?>(null)

    // Cache-first: the tapped row's category scope Room cache already holds this item's full
    // media + context (docs/decisions/android-offline-first.md), lifecycle-aware via
    // WhileSubscribed(5_000) like every other observed-Room StateFlow in this app.
    private val observedItem: StateFlow<VerificationQueueItem?> =
        repo.observeQueue(category = category)
            .map { resource -> resource.data?.items?.firstOrNull { it.itemId == itemId } }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    val state: StateFlow<VerifyDetailUiState> = combine(
        observedItem,
        _localDecision,
        _flags,
    ) { item, localDecision, flags ->
        item.toUiState(localDecision = localDecision, flags = flags)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VerifyDetailUiState(itemId = itemId))

    init {
        refresh()
    }

    fun onEvent(event: VerifyDetailEvent) {
        when (event) {
            VerifyDetailEvent.Close -> Unit // navigation — handled by the nav host.
            VerifyDetailEvent.Refresh -> refresh()
            VerifyDetailEvent.Approve -> submitVerdict(VerificationDecision.APPROVED, reason = null)
            is VerifyDetailEvent.Reject -> submitVerdict(VerificationDecision.REJECTED, reason = event.reason)
        }
    }

    private fun refresh() = viewModelScope.launch {
        _flags.update { it.copy(isRefreshing = true) }
        val result = repo.refreshQueue(category = category)
        _flags.update { it.copy(isRefreshing = false, isOffline = result.isFailure) }
    }

    private fun submitVerdict(decision: String, reason: String?) = viewModelScope.launch {
        // Belt-and-braces guard mirroring the reject dialog's own mandatory-reason validation —
        // a malformed event can never enqueue a reason-less reject.
        if (decision == VerificationDecision.REJECTED && reason.isNullOrBlank()) return@launch

        val rowVersion = observedItem.value?.rowVersion ?: 1
        _flags.update { it.copy(isSubmitting = true, errorMessage = null) }
        AnalyticsFunnels.trackVerifyVerdictAttempted(analytics, itemId, decision)
        val result = syncRepo.enqueueVerificationVerdict(
            itemId = itemId,
            decision = decision,
            reason = reason,
            rowVersion = rowVersion,
        )
        when (result) {
            is AppResult.Ok -> {
                _localDecision.value = decision
                _flags.update { it.copy(isSubmitting = false) }
                AnalyticsFunnels.trackVerifyVerdictSucceeded(analytics, itemId, decision)
            }
            is AppResult.Err -> {
                _flags.update { it.copy(isSubmitting = false, errorMessage = result.message) }
                result.cause?.let { crashReporter.recordException(it, "verification verdict enqueue failed") }
                AnalyticsFunnels.trackVerifyVerdictFailed(analytics, itemId, decision, result.message)
            }
        }
    }

    private fun VerificationQueueItem?.toUiState(localDecision: String?, flags: VerifyDetailFlags): VerifyDetailUiState {
        if (this == null) {
            return VerifyDetailUiState(
                itemId = itemId,
                isRefreshing = flags.isRefreshing,
                isOffline = flags.isOffline,
                isSubmitting = flags.isSubmitting,
                errorMessage = flags.errorMessage,
            )
        }
        val effectiveStatus = localDecision ?: status
        return VerifyDetailUiState(
            itemId = itemId,
            categoryLabel = humanizeCategory(category),
            media = media.map { VerifyMediaItem(signedUrl = it.downloadUrl, mimeType = it.mimeType ?: "", proofSubject = it.proofId ?: "") },
            context = buildContext(this),
            statusTone = statusTone(effectiveStatus),
            rowVersion = rowVersion,
            // R50-017: the backend now fails evidence resolution closed instead of silently
            // omitting media, so a verdict with no resolvable evidence must stay disabled even
            // though the item itself is still PENDING.
            isDecisionEnabled = effectiveStatus == VerificationStatus.PENDING && evidenceAvailable,
            isSubmitting = flags.isSubmitting,
            isRefreshing = flags.isRefreshing,
            lastSyncedAt = null,
            isOffline = flags.isOffline,
            errorMessage = flags.errorMessage,
        )
    }

    private fun buildContext(item: VerificationQueueItem): List<VerifyContextRow> {
        // Backend-owned display labels: never render raw UUIDs (STATUS-003).
        return listOfNotNull(
            (item.shedLabel ?: item.shedId)?.let { VerifyContextRow(VerifyContextKind.SHED, it) },
            (item.parkLabel ?: item.parkId)?.let { VerifyContextRow(VerifyContextKind.PARK, it) },
            (item.operatorName ?: item.operatorId)?.let { VerifyContextRow(VerifyContextKind.OPERATOR, it) },
            item.capturedAt.takeIf { it.isNotBlank() }?.let { VerifyContextRow(VerifyContextKind.CAPTURED_AT, it) },
        )
    }
}
