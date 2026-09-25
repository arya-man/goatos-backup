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
import sg.mesha.goatos.boot.NavStateRefreshSignal
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.datetime.GoatOsDates
import sg.mesha.goatos.core.data.CountsApprovalRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.CountsApprovalListItemDto
import sg.mesha.goatos.feature.counts.ApprovalCardMessage
import sg.mesha.goatos.feature.counts.ApprovalCaptureMediaUi
import sg.mesha.goatos.feature.counts.ApprovalCaptureRowUi
import sg.mesha.goatos.feature.counts.ApprovalEvent
import sg.mesha.goatos.feature.counts.ApprovalRowUi
import sg.mesha.goatos.feature.counts.ApprovalUiState
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
    /**
     * Re-reads the drawer/bar badges once a decision lands: the Approvals badge counts pending
     * requests, so it must drop the moment one is decided (live phone run 2026-09-25: it stayed
     * at 22 while the server said 21 until some other screen happened to refresh navigation).
     */
    private val navRefresh: NavStateRefreshSignal = NavStateRefreshSignal(),
) : ViewModel() {

    /** One status observer per request whose decision is on its way (each is its own outbox lane). */
    private val decisionStatusJobs = mutableMapOf<String, Job>()

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
        // Re-attach to every decision still on its way after a recreation / process death.
        pendingDecisions().forEach { (requestId, itemId) ->
            _state.update { it.copy(decidingRequestIds = it.decidingRequestIds + requestId) }
            observeDecision(itemId, requestId)
        }
    }

    fun onEvent(event: ApprovalEvent) {
        when (event) {
            is ApprovalEvent.Approve -> decide(event.requestId, approve = true, reason = null)
            is ApprovalEvent.OpenReject ->
                _state.update {
                    it.copy(
                        rejectingRequestId = event.requestId,
                        rejectReason = "",
                        cardMessages = it.cardMessages - event.requestId,
                    )
                }
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
                    showOnCard(requestId, REASON_REQUIRED_TEXT, isError = true)
                    return
                }
                decide(requestId, approve = false, reason = current.rejectReason)
            }
            ApprovalEvent.Refresh -> _state.update { it.copy(message = null, isError = false) }
            is ApprovalEvent.OpenCaptureMedia -> openCaptureMedia(event.proofId)
            is ApprovalEvent.CapturePreviewAction -> analytics.track(
                AnalyticsEvents.COUNTS_APPROVAL_CAPTURE_PREVIEW_ACTION,
                mapOf(AnalyticsEvents.Params.PROOF_ID to event.proofId, AnalyticsEvents.Params.ACTION to event.action),
            )
        }
    }

    /**
     * Loads ONE report proof when the approver taps it: a signed URL is short-lived, so nothing is
     * fetched until asked and nothing is cached beyond this screen. An opened proof is not
     * re-fetched; a failure is shown on that proof and a second tap retries.
     */
    private fun openCaptureMedia(proofId: String) {
        val current = _state.value
        if (proofId.isBlank() || current.openedMediaUrls.containsKey(proofId) || current.loadingMediaId != null) return
        _state.update { it.copy(loadingMediaId = proofId, failedMediaIds = it.failedMediaIds - proofId) }
        analytics.track(AnalyticsEvents.COUNTS_APPROVAL_CAPTURE_MEDIA_OPENED, mapOf(AnalyticsEvents.Params.PROOF_ID to proofId))
        viewModelScope.launch {
            val url = try {
                approvalRepository.proofDownloadUrl(proofId)
            } catch (error: Exception) {
                crashReporter.recordException(error, "counts approval capture proof url failed")
                null
            }
            if (url.isNullOrBlank()) {
                analytics.track(AnalyticsEvents.COUNTS_CAPTURE_FAILURE, mapOf(AnalyticsEvents.Params.KIND to "approval_media", AnalyticsEvents.Params.PROOF_ID to proofId))
                _state.update { it.copy(loadingMediaId = null, failedMediaIds = it.failedMediaIds + proofId) }
            } else {
                _state.update { it.copy(loadingMediaId = null, openedMediaUrls = it.openedMediaUrls + (proofId to url)) }
            }
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

    /**
     * Queues ONE request's decision. Decisions are independent: each request is its own outbox
     * lane (`groupKey = requestId`), so an approver working offline can decide card after card —
     * a tap on one card is never swallowed because another card's decision has not sent yet. Only
     * a second tap on the SAME card is ignored while its decision is on its way.
     */
    private fun decide(requestId: String, approve: Boolean, reason: String?) {
        if (requestId in _state.value.decidingRequestIds) return
        _state.update {
            it.copy(
                decidingRequestIds = it.decidingRequestIds + requestId,
                cardMessages = it.cardMessages - requestId,
            )
        }
        viewModelScope.launch {
            val result = syncRepository.enqueueCountsApprovalDecision(
                requestId = requestId,
                approve = approve,
                reason = reason,
                idempotencyKey = decisionKey(requestId, approve),
            )
            when (result) {
                is AppResult.Ok -> {
                    rememberPendingDecision(requestId, result.value)
                    observeDecision(result.value, requestId)
                    analytics.track(
                        AnalyticsEvents.COUNTS_APPROVAL_DECIDED,
                        mapOf(
                            AnalyticsEvents.Params.DECISION to if (approve) "approved" else "rejected",
                        ),
                    )
                    _state.update {
                        it.copy(
                            rejectingRequestId = if (it.rejectingRequestId == requestId) null else it.rejectingRequestId,
                            rejectReason = if (it.rejectingRequestId == requestId) "" else it.rejectReason,
                            cardMessages = it.cardMessages + (requestId to ApprovalCardMessage(QUEUED_MESSAGE, isError = false)),
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
                    _state.update { it.copy(decidingRequestIds = it.decidingRequestIds - requestId) }
                    // The enqueue error is the phone's own technical sentence; the approver is
                    // told the farm version, on the card they tapped.
                    showOnCard(requestId, DECISION_FAILED_TEXT, isError = true)
                }
            }
        }
    }

    private fun observeDecision(itemId: String, requestId: String) {
        decisionStatusJobs.remove(requestId)?.cancel()
        decisionStatusJobs[requestId] = viewModelScope.launch {
            syncRepository.observeItem(itemId).filterNotNull().collect { item ->
                when {
                    item.status == SyncItemStatus.SUCCEEDED -> {
                        approvalRepository.forgetDecided(requestId)
                        forgetPendingDecision(requestId)
                        navRefresh.request()
                        _state.update {
                            it.copy(
                                decidingRequestIds = it.decidingRequestIds - requestId,
                                cardMessages = it.cardMessages - requestId,
                            )
                        }
                        decisionStatusJobs.remove(requestId)?.cancel()
                    }
                    item.isTerminalFailure -> {
                        forgetPendingDecision(requestId)
                        analytics.track(
                            AnalyticsEvents.COUNTS_APPROVAL_FAILURE,
                            mapOf(AnalyticsEvents.Params.REASON to (item.lastErrorCode ?: "unknown")),
                        )
                        _state.update { it.copy(decidingRequestIds = it.decidingRequestIds - requestId) }
                        // ON THE CARD that was decided, in farm words keyed on the server's CODE —
                        // never the server's raw sentence, and never only at the top of a list the
                        // approver has scrolled away from.
                        showOnCard(requestId, approvalRefusalMessage(item.lastErrorCode), isError = true)
                        decisionStatusJobs.remove(requestId)?.cancel()
                    }
                }
            }
        }
    }

    private fun showOnCard(requestId: String, message: String, isError: Boolean) {
        _state.update { it.copy(cardMessages = it.cardMessages + (requestId to ApprovalCardMessage(message, isError))) }
    }

    /** requestId -> outbox row id of every decision on its way, persisted across process death. */
    private fun pendingDecisions(): Map<String, String> {
        val stored = savedStateHandle.get<ArrayList<String>>(KEY_PENDING_DECISIONS).orEmpty()
            .mapNotNull { entry -> entry.split(PENDING_SEPARATOR, limit = 2).takeIf { it.size == 2 }?.let { it[0] to it[1] } }
            .toMap(LinkedHashMap())
        // A decision queued by the previous build (one decision at a time) is still re-attached.
        val legacyItem = savedStateHandle.get<String>(KEY_OUTBOX_ITEM_ID)
        val legacyRequest = savedStateHandle.get<String>(KEY_PENDING_REQUEST_ID)
        if (legacyItem != null && legacyRequest != null && legacyRequest !in stored) stored[legacyRequest] = legacyItem
        return stored
    }

    private fun writePendingDecisions(next: Map<String, String>) {
        savedStateHandle[KEY_PENDING_DECISIONS] = ArrayList(next.map { (request, item) -> "$request$PENDING_SEPARATOR$item" })
        savedStateHandle.remove<String>(KEY_OUTBOX_ITEM_ID)
        savedStateHandle.remove<String>(KEY_PENDING_REQUEST_ID)
    }

    private fun rememberPendingDecision(requestId: String, itemId: String) =
        writePendingDecisions(pendingDecisions() + (requestId to itemId))

    private fun forgetPendingDecision(requestId: String) =
        writePendingDecisions(pendingDecisions() - requestId)

    /**
     * The stable idempotency key for deciding ONE request ONE way.
     *
     * Keyed per request AND per direction, and persisted in [SavedStateHandle] so a ViewModel
     * recreation (rotation, process death) resends the SAME key rather than minting a new one —
     * which is exactly what stops a server-committed-but-client-unrecorded approval from being
     * applied a second time. Deliberately NOT the `"$id-verdict-${System.currentTimeMillis()}"`
     * shape used by the older verify/rework writes: a timestamped key makes every retry a NEW
     * logical write, which for an approval means applying its effect again.
     *
     * A second tap after a TERMINAL refusal re-uses this key safely: the outbox re-opens the dead
     * row in place with the latest payload (a corrected reject reason included) and re-sends it,
     * and the server records a decision key only on a decision it actually applied
     * (CountsApprovalDecisionRetryTest).
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
        const val KEY_PENDING_DECISIONS = "countsApproval.pendingDecisions"
        const val PENDING_SEPARATOR = "|"
        const val QUEUED_MESSAGE = "Decision saved on this phone. It will apply automatically."
    }
}

// ---------------------------------------------------------------------------
// Wire -> row mapping
// ---------------------------------------------------------------------------

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
internal fun CountsApprovalListItemDto.toRowUi(): ApprovalRowUi = ApprovalRowUi(
    requestId = approvalRequestId,
    typeLabel = requestTypeLabel(requestType),
    requestType = requestType,
    // Blank, never the id, when the backend resolved no name: the screen then omits the line.
    raisedBy = raisedByName.orEmpty(),
    raisedAt = formatRaisedAt(raisedAt),
    summaryLine = summaryLine.orEmpty(),
    // The capture snapshot is carried VERBATIM: rows, titles and the note are backend copy.
    captureRows = capture?.rows.orEmpty().filter { it.label.isNotBlank() && it.value.isNotBlank() }
        .map { ApprovalCaptureRowUi(label = it.label, value = it.value, group = it.group.orEmpty()) },
    captureMedia = capture?.media.orEmpty().filter { it.proofId.isNotBlank() }
        .map { ApprovalCaptureMediaUi(proofId = it.proofId, label = it.label, isPhoto = it.kind == "photo") },
    captureMissingNote = capture?.missingNote.orEmpty(),
    captureReviewStatus = captureReviewStatus.orEmpty(),
    captureReviewReason = captureReviewReason.orEmpty(),
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
 * Raised-at display formatting: `DD/MM/YYYY HH:MM` in IST through the ONE phone date helper
 * (AGENTS.md "Every Visible Date Is DD/MM/YYYY"). An unparseable value renders VERBATIM rather
 * than being dropped, so an approver can still see and act on the row — a timestamp this cannot
 * parse is a backend format change, plainly visible on screen.
 */
internal fun formatRaisedAt(raisedAt: String): String = GoatOsDates.fromWireInstant(raisedAt)

/**
 * The farm sentence for a refused decision, keyed on the server's stable error CODE (kept on the
 * outbox row as `lastErrorCode`), never on the server's own lowercase sentence.
 */
internal fun approvalRefusalMessage(code: String?): String = when (code) {
    "death_evidence_incomplete" -> "All the death report steps must be recorded before it can be approved."
    "death_already_applied" -> "This animal's death was already approved on another report. Reject this one as a duplicate."
    "approval_already_decided", "idempotency_conflict", "approval_request_not_found", "approval_payload_not_applicable" ->
        "This request was already decided or changed."
    "permission_denied", "park_scope_forbidden" -> "You can't decide this request."
    "missing_reason" -> REASON_REQUIRED_TEXT
    else -> DECISION_FAILED_TEXT
}

private const val DECISION_FAILED_TEXT = "Couldn't save the decision. Try again."
private const val REASON_REQUIRED_TEXT = "Add a reason — the operator who raised this needs to know what to fix."
