package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.PenReconciliationRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCardDto
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.feature.counts.PenReconciliationEvent
import sg.mesha.goatos.feature.counts.PenReconciliationRowUi
import sg.mesha.goatos.feature.counts.PenReconciliationStatusUi
import sg.mesha.goatos.feature.counts.PenReconciliationTone
import sg.mesha.goatos.feature.counts.PenReconciliationUiState
import javax.inject.Inject

/**
 * Offline-first, status-scoped renderer for the Herd Operations Reconcile tab
 * (docs/decisions/pen-reconciliation.md). Rows are a Room-backed Paging window; the status chip
 * counts are whole-filter backend truth carried on every page response, never page-local sums.
 */
@HiltViewModel
class PenReconciliationViewModel @Inject constructor(
    private val repo: PenReconciliationRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {
    private val _selection = MutableStateFlow(Selection(STATUS_ALL))
    private val _isOffline = MutableStateFlow(false)
    private val _lastSyncedAt = MutableStateFlow<Long?>(null)
    private val _submittedOutboxItem = MutableStateFlow<SubmittedOutboxNotice?>(null)

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<PenReconciliationRowUi>> = _selection
        .flatMapLatest { repo.cards(status = it.status) }
        .map { page -> page.map { it.toRowUi() } }
        .cachedIn(viewModelScope)

    @OptIn(ExperimentalCoroutinesApi::class)
    private val submittedWriteResult: Flow<CountsWriteResultUi?> = _submittedOutboxItem.flatMapLatest { submitted ->
        if (submitted == null) {
            flowOf(null)
        } else {
            syncRepository.observeStatus().map { status ->
                val item = status.items.firstOrNull { it.id == submitted.outboxItemId }
                item?.toWriteResult(submitted.queuedMessage, SYNCED_MESSAGE)
                    ?: CountsWriteResultUi(CountsWriteStatus.QUEUED, submitted.queuedMessage)
            }
        }
    }

    val state: StateFlow<PenReconciliationUiState> = combine(
        _selection, _isOffline, _lastSyncedAt, repo.meta, submittedWriteResult,
    ) { selection, isOffline, lastSyncedAt, meta, submissionNotice ->
        PenReconciliationUiState(
            statuses = STATUSES.map { (key, label) ->
                PenReconciliationStatusUi(
                    key = key,
                    label = label,
                    selected = key == selection.status,
                    count = when (key) {
                        "all" -> meta.counts.all
                        "open" -> meta.counts.open
                        "pending_verification" -> meta.counts.pendingVerification
                        "rework" -> meta.counts.rework
                        else -> meta.counts.completed
                    },
                )
            },
            emptyMessage = if (isOffline) OFFLINE_EMPTY else EMPTY_MESSAGE,
            isErrorEmpty = isOffline,
            lastSyncedAt = lastSyncedAt,
            isOffline = isOffline,
            submissionNotice = submissionNotice,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), PenReconciliationUiState())

    init {
        analytics.track(AnalyticsEvents.COUNTS_PEN_RECONCILIATION_VIEWED)
    }

    fun onRowsLoadFailed(error: Throwable) {
        _isOffline.value = true
        crashReporter.recordException(error, "pen reconciliation page load failed")
    }

    fun onRowsLoaded() {
        _isOffline.value = false
        _lastSyncedAt.value = System.currentTimeMillis()
    }

    /** Follow a completion the execute screen just queued, so its banner shows on the list. */
    fun followSubmittedOutboxItem(outboxItemId: String, queuedMessage: String?) {
        _submittedOutboxItem.value = SubmittedOutboxNotice(
            outboxItemId = outboxItemId,
            queuedMessage = queuedMessage ?: QUEUED_MESSAGE,
        )
    }

    fun onEvent(event: PenReconciliationEvent) {
        when (event) {
            PenReconciliationEvent.Refresh ->
                // Bump the nonce so an equal Selection still re-emits (MutableStateFlow conflates
                // on equality — the silent-refresh defect ShiftingPendingViewModel documents).
                _selection.value = _selection.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
            is PenReconciliationEvent.SelectStatus -> if (STATUSES.any { it.first == event.status }) {
                _selection.value = _selection.value.copy(status = event.status)
            }
            is PenReconciliationEvent.OpenCard, PenReconciliationEvent.Back -> Unit // nav — host-handled.
        }
    }

    private fun CountsPenReconciliationCardDto.toRowUi() = PenReconciliationRowUi(
        cardId = cardId,
        scannedIdentifier = scannedIdentifier,
        goatDisplayId = goatDisplayId,
        // Backend-composed pen labels, rendered verbatim (operational-location convention).
        foundLabel = foundOperationalLocationDisplay.ifBlank { UNKNOWN_LOCATION },
        belongsLabel = registeredOperationalLocationDisplay.ifBlank { registeredShedName.ifBlank { UNKNOWN_LOCATION } },
        statusLabel = when (status) {
            "open" -> "Ready to return"
            "pending_verification" -> "Waiting for review"
            "rework" -> "Needs a new video"
            "completed" -> "Completed"
            else -> ""
        },
        statusTone = when (status) {
            "open", "rework" -> PenReconciliationTone.Action
            "pending_verification" -> PenReconciliationTone.Waiting
            "completed" -> PenReconciliationTone.Done
            else -> PenReconciliationTone.Neutral
        },
        primaryActionKey = primaryActionKey,
        raisedAtLabel = raisedAtIst.take(10),
        reworkReason = reworkReason?.takeIf { it.isNotBlank() },
    )

    /**
     * The selected status bucket, plus a [refreshNonce] whose only job is to make a refresh a NEW
     * value — a bare copy() of an equal data class emits nothing through a conflating StateFlow.
     */
    private data class Selection(val status: String, val refreshNonce: Int = 0)

    private data class SubmittedOutboxNotice(
        val outboxItemId: String,
        val queuedMessage: String,
    )

    private companion object {
        const val STATUS_ALL = "all"
        val STATUSES = listOf(
            "all" to "All", "open" to "Open", "pending_verification" to "In review",
            "rework" to "Rework", "completed" to "Completed",
        )
        const val EMPTY_MESSAGE = "No animals need returning right now."
        const val OFFLINE_EMPTY = "Couldn't refresh. Cached cards will appear when available."
        const val QUEUED_MESSAGE = "Saved on this phone. It will sync automatically."
        const val SYNCED_MESSAGE = "Return recorded. The video is with the reviewer."
        const val UNKNOWN_LOCATION = "—"
    }
}
