package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonPrimitive
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsApprovalRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
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
                    // Drop the decided row from the cached queue the moment the decision is
                    // DURABLE. Two reasons: the approver sees their action take effect offline,
                    // and the same request cannot be decided a second time while the first
                    // decision is still draining.
                    approvalRepository.forgetDecided(requestId)
                    analytics.track(
                        AnalyticsEvents.COUNTS_APPROVAL_DECIDED,
                        mapOf(
                            AnalyticsEvents.Params.DECISION to if (approve) "approved" else "rejected",
                        ),
                    )
                    _state.update {
                        it.copy(
                            decidingRequestId = null,
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
        const val QUEUED_MESSAGE = "Decision saved on this phone. It will apply automatically."
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

private fun CountsApprovalListItemDto.toRowUi(): ApprovalRowUi = ApprovalRowUi(
    requestId = approvalRequestId,
    typeLabel = requestTypeLabel(requestType),
    requestType = requestType,
    raisedBy = raisedByUserId,
    raisedAt = formatRaisedAt(raisedAt),
    summaryLine = summaryLine(requestType, summary),
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

private fun formatRaisedAt(raisedAt: String): String = runCatching {
    RAISED_AT_FORMAT.format(Instant.parse(raisedAt))
}.getOrDefault(raisedAt)

/**
 * A one-line description of what the request contains, read from the echoed payload.
 *
 * Only fields the contract defines for that request type are read, and anything missing simply
 * drops out of the line — the summary is `additionalProperties: true`, so it must be treated as
 * data to display, never as a shape to assert.
 */
private fun summaryLine(requestType: String, summary: JsonElement?): String {
    val obj = summary as? JsonObject ?: return ""
    fun str(key: String): String? =
        (obj[key] as? JsonPrimitive)?.jsonPrimitive?.contentOrNullSafe()?.takeIf { it.isNotBlank() }

    return when (requestType) {
        "birth" -> listOfNotNull(
            str("animal_identifier_1")?.let { "Tag $it" },
            str("sex"),
            str("breed"),
            str("dob")?.let { "born $it" },
        ).joinToString(" · ")
        "death" -> listOfNotNull(
            str("goat_id")?.let { "Animal $it" },
            str("reason"),
        ).joinToString(" · ")
        "shifting" -> listOfNotNull(
            (obj["goat_ids"] as? kotlinx.serialization.json.JsonArray)?.size?.let { "$it animal(s)" },
            str("destination_shed_id")?.let { "to shed $it" },
            str("category"),
        ).joinToString(" · ")
        else -> ""
    }
}

/** `content` on a JSON null primitive is the literal "null"; treat that as absent. */
private fun JsonPrimitive.contentOrNullSafe(): String? = if (this is kotlinx.serialization.json.JsonNull) null else content
