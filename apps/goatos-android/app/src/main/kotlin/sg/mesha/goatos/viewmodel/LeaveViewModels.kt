package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsClock
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.ClockRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.LeaveRequestDto
import sg.mesha.goatos.feature.clock.LeaveApprovalEvent
import sg.mesha.goatos.feature.clock.LeaveApprovalUiState
import sg.mesha.goatos.feature.clock.LeaveRequestFormUi
import sg.mesha.goatos.feature.clock.LeaveRowUi
import java.util.UUID
import javax.inject.Inject

/**
 * Leave requests (docs/features/leave-requests/plan.md, maintainer decisions 2026-09-10):
 * the Request leave form and the approver's Leave queue. Both write through the durable outbox
 * under STABLE idempotency keys persisted in [SavedStateHandle], so a rotation or process death
 * resends the SAME key and the server replays instead of raising or deciding twice.
 */

internal fun LeaveRequestDto.toRowUi(): LeaveRowUi = LeaveRowUi(
    listKey = "$leaveRequestId:$status:$rowVersion",
    requestId = leaveRequestId,
    personName = personName,
    parkLabel = parkLabel,
    datesLabel = datesLabel,
    reason = reason,
    statusLabel = statusLabel,
    statusLine = statusLine,
    status = status,
    raisedAtLabel = raisedAtLabel,
    canWithdraw = canWithdraw,
    mySlotLabel = mySlotLabel,
)

/** The Request leave form (route `/clock/leave/new`). Copy comes from the cached status blob. */
@HiltViewModel
class LeaveRequestViewModel @Inject constructor(
    private val repo: ClockRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val _form = MutableStateFlow(
        LeaveRequestFormUi(
            startsOn = savedStateHandle[KEY_STARTS] ?: "",
            endsOn = savedStateHandle[KEY_ENDS] ?: "",
            reason = savedStateHandle[KEY_REASON] ?: "",
        ),
    )
    private var watch: Job? = null

    val state: StateFlow<LeaveRequestFormUi> = kotlinx.coroutines.flow.combine(
        repo.observeStatus().map { it?.leaveCopy.orEmpty() },
        _form,
    ) { copy, form ->
        form.copy(
            title = copy["request.title"].orEmpty(),
            fromLabel = copy["request.from"].orEmpty(),
            toLabel = copy["request.to"].orEmpty(),
            reasonLabel = copy["request.reason"].orEmpty(),
            reasonHint = copy["request.reason_hint"].orEmpty(),
            submitLabel = copy["request.submit"].orEmpty(),
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), _form.value)

    fun pickStart(date: String) {
        savedStateHandle[KEY_STARTS] = date
        _form.update { f ->
            // The last day can never sit before the first: pull it along.
            val ends = if (f.endsOn.isBlank() || f.endsOn < date) date else f.endsOn
            savedStateHandle[KEY_ENDS] = ends
            f.copy(startsOn = date, endsOn = ends, message = "", isError = false)
        }
    }

    fun pickEnd(date: String) {
        savedStateHandle[KEY_ENDS] = date
        _form.update { it.copy(endsOn = date, message = "", isError = false) }
    }

    fun editReason(value: String) {
        savedStateHandle[KEY_REASON] = value
        _form.update { it.copy(reason = value, message = "", isError = false) }
    }

    fun submit() {
        val form = _form.value
        if (form.submitting || form.startsOn.isBlank() || form.endsOn.isBlank() || form.reason.isBlank()) return
        _form.update { it.copy(submitting = true, message = "", isError = false) }
        viewModelScope.launch {
            when (val result = repo.requestLeave(submissionKey(), form.startsOn, form.endsOn, form.reason.trim())) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsClock.CLOCK_LEAVE_REQUESTED)
                    watchOutbox(result.value)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "leave request enqueue failed") }
                    analytics.track(AnalyticsEventsClock.CLOCK_LEAVE_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message))
                    _form.update { it.copy(submitting = false, message = result.message, isError = true) }
                }
            }
        }
    }

    /**
     * Hold the form until the outbox row lands or fails terminally: a same-day overlap or a past
     * date is a definitive server answer the person must see HERE, on the form, not later as a
     * silent absence from the list.
     */
    private fun watchOutbox(itemId: String) {
        watch?.cancel()
        watch = viewModelScope.launch {
            syncRepository.observeItem(itemId).filterNotNull().collect { item ->
                when {
                    item.status == SyncItemStatus.SUCCEEDED -> {
                        clearDraft()
                        _form.update { it.copy(submitting = false, submitted = true) }
                        watch?.cancel()
                    }
                    item.isTerminalFailure -> {
                        // The key is spent; the next attempt mints a fresh one.
                        savedStateHandle.remove<String>(KEY_IDEMPOTENCY)
                        analytics.track(AnalyticsEventsClock.CLOCK_LEAVE_FAILURE, mapOf(AnalyticsEvents.Params.REASON to (item.lastError ?: "rejected")))
                        _form.update { it.copy(submitting = false, message = item.lastError ?: "", isError = true) }
                        watch?.cancel()
                    }
                    // Still queued (offline): the write is durable, so let the person go. The Clock
                    // screen shows the request once the status refreshes after drain.
                    else -> {
                        clearDraft()
                        _form.update { it.copy(submitting = false, submitted = true) }
                        watch?.cancel()
                    }
                }
            }
        }
    }

    private fun submissionKey(): String {
        savedStateHandle.get<String>(KEY_IDEMPOTENCY)?.let { return it }
        val minted = "leave-request:${UUID.randomUUID()}"
        savedStateHandle[KEY_IDEMPOTENCY] = minted
        return minted
    }

    private fun clearDraft() {
        savedStateHandle.remove<String>(KEY_IDEMPOTENCY)
        savedStateHandle.remove<String>(KEY_STARTS)
        savedStateHandle.remove<String>(KEY_ENDS)
        savedStateHandle.remove<String>(KEY_REASON)
    }

    private companion object {
        const val KEY_IDEMPOTENCY = "leaveRequest.idempotencyKey"
        const val KEY_STARTS = "leaveRequest.startsOn"
        const val KEY_ENDS = "leaveRequest.endsOn"
        const val KEY_REASON = "leaveRequest.reason"
    }
}

/**
 * The approver's Leave queue (route `/leave/approvals`, the Leave tab of the Approvals module).
 * Keyset pages of ~20 appended on scroll; the first page is blob-cached by the repository.
 */
@HiltViewModel
class LeaveApprovalViewModel @Inject constructor(
    private val repo: ClockRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val _state = MutableStateFlow(LeaveApprovalUiState())
    val state: StateFlow<LeaveApprovalUiState> = _state.asStateFlow()

    private var nextCursor: String = ""
    private var decisionWatch: Job? = null
    private val decisionOutboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    init {
        analytics.track(AnalyticsEventsClock.CLOCK_LEAVE_QUEUE_VIEWED)
        val pending = savedStateHandle.get<String>(KEY_PENDING_REQUEST_ID)
        decisionOutboxItemId.value?.let { itemId ->
            if (pending != null) {
                _state.update { it.copy(decidingRequestId = pending) }
                observeDecision(itemId, pending)
            }
        }
        refresh()
    }

    fun onEvent(event: LeaveApprovalEvent) {
        when (event) {
            LeaveApprovalEvent.Refresh -> refresh()
            LeaveApprovalEvent.LoadMore -> loadMore()
            is LeaveApprovalEvent.Approve -> decide(event.requestId, approve = true, reason = null)
            is LeaveApprovalEvent.OpenReject ->
                _state.update { it.copy(rejectingRequestId = event.requestId, rejectReason = "", message = "", isError = false) }
            is LeaveApprovalEvent.EditRejectReason -> _state.update { it.copy(rejectReason = event.value) }
            LeaveApprovalEvent.CancelReject -> _state.update { it.copy(rejectingRequestId = null, rejectReason = "") }
            LeaveApprovalEvent.ConfirmReject -> {
                val current = _state.value
                val requestId = current.rejectingRequestId ?: return
                if (current.rejectReason.isBlank()) return
                decide(requestId, approve = false, reason = current.rejectReason.trim())
            }
        }
    }

    fun refresh() {
        if (_state.value.isRefreshing) return
        _state.update { it.copy(isRefreshing = true) }
        viewModelScope.launch {
            repo.fetchLeaveQueue(cursor = null)
                .onSuccess { dto ->
                    nextCursor = dto.nextCursor
                    val copy = dto.copy
                    _state.update {
                        it.copy(
                            title = copy["queue.title"].orEmpty(),
                            empty = copy["queue.empty"].orEmpty(),
                            approveLabel = copy["approve"].orEmpty(),
                            rejectLabel = copy["reject"].orEmpty(),
                            rejectReasonLabel = copy["reject.reason"].orEmpty(),
                            rejectReasonHint = copy["reject.reason_hint"].orEmpty(),
                            cancelLabel = copy["action.cancel"] ?: "Cancel",
                            rows = dto.items.map { row -> row.toRowUi() },
                            hasMore = dto.nextCursor.isNotBlank(),
                            hasData = true,
                            lastSyncedAt = System.currentTimeMillis(),
                            isRefreshing = false,
                        )
                    }
                }
                .onFailure { failure ->
                    crashReporter.recordException(failure, "leave queue refresh failed")
                    _state.update { it.copy(isRefreshing = false, message = failure.message.orEmpty(), isError = true) }
                }
        }
    }

    private fun loadMore() {
        val cursor = nextCursor
        if (cursor.isBlank() || _state.value.loadingMore) return
        _state.update { it.copy(loadingMore = true) }
        viewModelScope.launch {
            repo.fetchLeaveQueue(cursor = cursor)
                .onSuccess { dto ->
                    nextCursor = dto.nextCursor
                    _state.update { s ->
                        val seen = s.rows.mapTo(mutableSetOf()) { it.requestId }
                        s.copy(
                            rows = s.rows + dto.items.filter { it.leaveRequestId !in seen }.map { it.toRowUi() },
                            hasMore = dto.nextCursor.isNotBlank(),
                            loadingMore = false,
                        )
                    }
                }
                .onFailure { failure ->
                    crashReporter.recordException(failure, "leave queue page failed")
                    _state.update { it.copy(loadingMore = false) }
                }
        }
    }

    private fun decide(requestId: String, approve: Boolean, reason: String?) {
        if (_state.value.decidingRequestId != null) return
        _state.update { it.copy(decidingRequestId = requestId, message = "", isError = false) }
        viewModelScope.launch {
            when (val result = repo.decideLeave(requestId, approve, reason, decisionKey(requestId, approve))) {
                is AppResult.Ok -> {
                    decisionOutboxItemId.value = result.value
                    savedStateHandle[KEY_PENDING_REQUEST_ID] = requestId
                    analytics.track(
                        AnalyticsEventsClock.CLOCK_LEAVE_DECIDED,
                        mapOf(AnalyticsEvents.Params.DECISION to if (approve) "approved" else "rejected"),
                    )
                    _state.update { it.copy(rejectingRequestId = null, rejectReason = "") }
                    observeDecision(result.value, requestId)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "leave decision enqueue failed") }
                    analytics.track(AnalyticsEventsClock.CLOCK_LEAVE_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message))
                    _state.update { it.copy(decidingRequestId = null, message = result.message, isError = true) }
                }
            }
        }
    }

    private fun observeDecision(itemId: String, requestId: String) {
        decisionWatch?.cancel()
        decisionWatch = viewModelScope.launch {
            syncRepository.observeItem(itemId).filterNotNull().collect { item ->
                when {
                    item.status == SyncItemStatus.SUCCEEDED -> {
                        clearDecisionTracking()
                        // The row leaves the queue: drop it locally at once, then re-read.
                        _state.update { s -> s.copy(decidingRequestId = null, rows = s.rows.filterNot { it.requestId == requestId }) }
                        refresh()
                    }
                    item.isTerminalFailure -> {
                        clearDecisionTracking()
                        _state.update { it.copy(decidingRequestId = null, message = item.lastError.orEmpty(), isError = true) }
                        refresh()
                    }
                }
            }
        }
    }

    private fun clearDecisionTracking() {
        decisionOutboxItemId.value = null
        savedStateHandle[KEY_PENDING_REQUEST_ID] = null
    }

    private fun decisionKey(requestId: String, approve: Boolean): String {
        val direction = if (approve) "approve" else "reject"
        val stateKey = "$KEY_IDEMPOTENCY.$requestId.$direction"
        savedStateHandle.get<String>(stateKey)?.let { return it }
        val minted = "leave-$direction:$requestId"
        savedStateHandle[stateKey] = minted
        return minted
    }

    private companion object {
        const val KEY_IDEMPOTENCY = "leaveApproval.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "leaveApproval.outboxItemId"
        const val KEY_PENDING_REQUEST_ID = "leaveApproval.pendingRequestId"
    }
}
