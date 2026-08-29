package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsApprovalRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.dto.CountsApprovalListItemDto
import sg.mesha.goatos.feature.counts.ApprovalEvent
import sg.mesha.goatos.feature.counts.ApprovalRowUi
import sg.mesha.goatos.feature.counts.ApprovalUiState
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject

/**
 * The Counts approver's queue (`/counts/approvals`).
 *
 * ### Reads: Room is the source of truth
 * [rows] is a Room-backed Paging flow ([CountsApprovalRepository.approvals]); the RemoteMediator
 * refreshes it in the background. Re-entering the tab renders the cached queue immediately, and a
 * failed refresh leaves those rows on screen with a stale banner rather than blanking the list.
 *
 * ### Writes: the durable outbox, under a STABLE key
 * A decision never calls the API inline. It is enqueued to the same outbox every other Counts write
 * uses, under an idempotency key derived ONCE PER REQUEST and persisted in [SavedStateHandle] (the
 * [DraftIdempotencyKey] pattern `SubmitViewModel` established). That key stability is the whole
 * safety property here: approving APPLIES the effect — a birth creates a kid and generates its
 * vaccination obligations, a death exits an animal and cancels its obligations, a shifting
 * relocates real animals — so a resend under a fresh key would apply it twice. Under the stored key
 * the backend returns the original decision and applies nothing.
 *
 * ### Authority stays server-side
 * The queue already contains only the request types this caller may decide, and the server
 * re-checks per row on decide. This ViewModel never inspects a role to show, hide, or enable an
 * action.
 */
@HiltViewModel
class ApprovalViewModel @Inject constructor(
    private val approvalRepository: CountsApprovalRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val decisionOutboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)
    private var decisionStatusJob: Job? = null

    private val _state = MutableStateFlow(ApprovalUiState())
    val state: StateFlow<ApprovalUiState> = _state.asStateFlow()

    /**
     * The paged queue, mapped to rows off the Main thread and `cachedIn`'d so a configuration
     * change re-uses the already-loaded pages instead of re-fetching them.
     */
    val rows: Flow<PagingData<ApprovalRowUi>> = approvalRepository
        .approvals()
        .map { page -> page.map { it.toRowUi() } }
        .flowOn(Dispatchers.Default)
        .cachedIn(viewModelScope)

    init {
        analytics.track(AnalyticsEvents.COUNTS_APPROVAL_QUEUE_VIEWED)
        val pendingRequestId = savedStateHandle.get<String>(KEY_PENDING_REQUEST_ID)
        decisionOutboxItemId.value?.let { itemId ->
            if (pendingRequestId != null) {
                _state.update { it.copy(decidingRequestId = pendingRequestId) }
                observeDecision(itemId, pendingRequestId)
            }
        }
    }

    fun onEvent(event: ApprovalEvent) {
        when (event) {
            is ApprovalEvent.Approve -> decide(event.requestId, approve = true, reason = null)
            is ApprovalEvent.OpenReject ->
                _state.update { it.copy(rejectingRequestId = event.requestId, rejectReason = "", message = null) }
            is ApprovalEvent.EditRejectReason -> _state.update { it.copy(rejectReason = event.value) }
            ApprovalEvent.CancelReject ->
                _state.update { it.copy(rejectingRequestId = null, rejectReason = "") }
            ApprovalEvent.ConfirmReject -> {
                val current = _state.value
                val requestId = current.rejectingRequestId ?: return
                // A reason is REQUIRED server-side and in the database. Blocking it here means the
                // approver is told immediately instead of the decision sitting in the outbox until
                // it fails terminally.
                if (current.rejectReason.isBlank()) {
                    _state.update { it.copy(message = REASON_REQUIRED_MESSAGE, isError = true) }
                    return
                }
                decide(requestId, approve = false, reason = current.rejectReason)
            }
            ApprovalEvent.Refresh -> _state.update { it.copy(message = null, isError = false) }
        }
    }

    /** Surfaces a Paging load failure without wiping the cached rows the screen is showing. */
    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "counts approval queue load failed")
        analytics.track(
            AnalyticsEvents.COUNTS_READ_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to "approval_queue",
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
            ),
        )
    }

    private fun decide(requestId: String, approve: Boolean, reason: String?) {
        if (_state.value.decidingRequestId != null) return // one decision in flight at a time
        _state.update { it.copy(decidingRequestId = requestId, message = null, isError = false) }
        viewModelScope.launch {
            val result = syncRepository.enqueueCountsApprovalDecision(
                requestId = requestId,
                approve = approve,
                reason = reason,
                idempotencyKey = decisionKey(requestId, approve),
            )
            when (result) {
                is AppResult.Ok -> {
                    decisionOutboxItemId.value = result.value
                    savedStateHandle[KEY_PENDING_REQUEST_ID] = requestId
                    observeDecision(result.value, requestId)
                    analytics.track(
                        AnalyticsEvents.COUNTS_APPROVAL_DECIDED,
                        mapOf(
                            AnalyticsEvents.Params.DECISION to if (approve) "approved" else "rejected",
                        ),
                    )
                    _state.update {
                        it.copy(
                            rejectingRequestId = null,
                            rejectReason = "",
                            message = QUEUED_MESSAGE,
                            isError = false,
                        )
                    }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "counts approval decision enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.COUNTS_APPROVAL_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.DECISION to if (approve) "approved" else "rejected",
                            AnalyticsEvents.Params.REASON to result.message,
                        ),
                    )
                    _state.update {
                        it.copy(decidingRequestId = null, message = result.message, isError = true)
                    }
                }
            }
        }
    }

    private fun observeDecision(itemId: String, requestId: String) {
        decisionStatusJob?.cancel()
        decisionStatusJob = viewModelScope.launch {
            syncRepository.observeItem(itemId).filterNotNull().collect { item ->
                when {
                    item.status == SyncItemStatus.SUCCEEDED -> {
                        approvalRepository.forgetDecided(requestId)
                        clearDecisionTracking()
                        _state.update { it.copy(decidingRequestId = null) }
                    }
                    item.isTerminalFailure -> {
                        clearDecisionTracking()
                        _state.update {
                            it.copy(
                                decidingRequestId = null,
                                message = item.lastError ?: DECISION_FAILED_MESSAGE,
                                isError = true,
                            )
                        }
                    }
                }
            }
        }
    }

    private fun clearDecisionTracking() {
        decisionOutboxItemId.value = null
        savedStateHandle[KEY_PENDING_REQUEST_ID] = null
    }

    /**
     * The stable idempotency key for deciding ONE request ONE way.
     *
     * Keyed per request AND per direction, and persisted in [SavedStateHandle] so a ViewModel
     * recreation (rotation, process death) resends the SAME key rather than minting a new one —
     * which is exactly what stops a server-committed-but-client-unrecorded approval from being
     * applied a second time. Deliberately NOT the `"$id-verdict-${System.currentTimeMillis()}"`
     * shape used by the older verify/rework writes: a timestamped key makes every retry a NEW
     * logical write, which for an approval means applying its effect again.
     */
    private fun decisionKey(requestId: String, approve: Boolean): String {
        val direction = if (approve) "approve" else "reject"
        val stateKey = "$KEY_IDEMPOTENCY.$requestId.$direction"
        savedStateHandle.get<String>(stateKey)?.let { return it }
        val minted = "counts-approval-$direction:$requestId"
        savedStateHandle[stateKey] = minted
        return minted
    }

    private companion object {
        const val KEY_IDEMPOTENCY = "countsApproval.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "countsApproval.outboxItemId"
        const val KEY_PENDING_REQUEST_ID = "countsApproval.pendingRequestId"
        const val QUEUED_MESSAGE = "Decision saved on this phone. It will apply automatically."
        const val DECISION_FAILED_MESSAGE = "This decision did not go through. Review it and try again."
        const val REASON_REQUIRED_MESSAGE =
            "Add a reason — the operator who raised this needs to know what to fix."
    }
}

// ---------------------------------------------------------------------------
// Wire -> row mapping
// ---------------------------------------------------------------------------

/** IST, per AGENTS.md: every business meaning derived from an instant is India-business-calendar. */
private val RAISED_AT_FORMAT: DateTimeFormatter =
    DateTimeFormatter.ofPattern("d MMM, h:mm a").withZone(ZoneId.of("Asia/Kolkata"))

/**
 * Wire row -> screen row.
 *
 * This mapping is deliberately THIN. It carries backend copy across and formats one timestamp for
 * the local calendar; it composes no business text of its own.
 *
 * It used to do more, and that was the defect: it read the echoed payload and built its own
 * "12 animal(s) · to shed <id>" line, and passed `raised_by_user_id` through as the raiser. The
 * phone has no name source for a user id or a shed id, so both rendered as UUIDs at an approver
 * while admin-web — which could resolve them — showed names. Two surfaces, two authors, two
 * different answers for the same row. Both lines are now composed by the backend
 * (`summary_line`, `raised_by_name`) and rendered verbatim, per the golden frontend rule.
 */
private fun CountsApprovalListItemDto.toRowUi(): ApprovalRowUi = ApprovalRowUi(
    requestId = approvalRequestId,
    typeLabel = requestTypeLabel(requestType),
    requestType = requestType,
    // Blank, never the id, when the backend resolved no name: the screen then omits the line.
    raisedBy = raisedByName.orEmpty(),
    raisedAt = formatRaisedAt(raisedAt),
    summaryLine = summaryLine.orEmpty(),
)

/**
 * Request-type display label.
 *
 * The contract's `request_type` is a closed enum (`birth`/`death`/`shifting`) with no localized
 * label alongside it, so this is the one place the client titles a backend value. An unknown value
 * renders verbatim rather than being dropped — a future request type must be visible in the queue
 * even before the app knows its name, or an approver would silently never see that work.
 */
private fun requestTypeLabel(requestType: String): String = when (requestType) {
    "birth" -> "Birth"
    "death" -> "Death"
    "shifting" -> "Shifting"
    else -> requestType
}

/**
 * Raised-at display formatting, with the raw backend value as the fallback.
 *
 * Same philosophy as [requestTypeLabel] above: an unrecognised value renders VERBATIM rather than
 * being dropped or blanked, so an approver can still see and act on the row. A timestamp this cannot
 * parse is a backend format change, not a per-row data fault — it would fail for every row at once,
 * it is plainly visible on screen as an unformatted timestamp, and the approver is not blocked by it.
 */
// exception:exempt pure display formatter; the parse failure is fully surfaced to the user as the
// raw value, and this is a top-level mapper with no CrashReporter in scope — threading one through
// it to report a systematic, self-evident formatting fallback would add coupling for no new signal.
private fun formatRaisedAt(raisedAt: String): String = runCatching {
    RAISED_AT_FORMAT.format(Instant.parse(raisedAt))
}.getOrDefault(raisedAt)
