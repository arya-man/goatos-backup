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
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.feature.feed.FeedCompleteEvent
import sg.mesha.goatos.feature.feed.FeedCompleteResultUi
import sg.mesha.goatos.feature.feed.FeedCompleteStatus
import sg.mesha.goatos.feature.feed.FeedCompleteUiState
import java.util.Locale
import javax.inject.Inject

/**
 * The feed-direction completion detail (`/feed/complete/...`) — an operator marking one shed-session
 * as fed, reached by tapping a Feed Direction or Feed Packing row.
 *
 * Two independent, offline-first writes, mirroring the Shifting execute screen:
 *  - **Optional video** ([SyncRepository.enqueueProofUpload]): registered against the shed, uploaded
 *    to GCS through the same signed-URL path as vaccination proof. OPTIONAL; never gates completion.
 *  - **Mark done** ([SyncRepository.enqueueFeedDirectionComplete]): records the shed-session
 *    completion. A STABLE `SavedStateHandle`-persisted idempotency key collapses a resend after
 *    process death onto the original completion, and the shed-session natural key makes a second
 *    completion a backend no-op.
 *
 * On enqueue the shed-session is recorded in [FeedCompletionLocalStore] so the list row shows
 * completed immediately (offline-first), converging on the backend `completed` flag once synced.
 */
@HiltViewModel
class FeedCompleteViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val feedCompletionStore: FeedCompletionLocalStore,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    // "-" is the blank-park sentinel the route helper uses (a path arg cannot be empty); it maps back
    // to "" so the backend resolves the default park.
    private val parkId: String = savedStateHandle.get<String>(ARG_PARK_ID)?.takeIf { it != "-" }.orEmpty()
    private val shedId: String = savedStateHandle.get<String>(ARG_SHED_ID).orEmpty()
    private val sessionNo: Int = savedStateHandle.get<String>(ARG_SESSION_NO)?.toIntOrNull() ?: 0
    private val workflow: String = savedStateHandle.get<String>(ARG_WORKFLOW).orEmpty()
    private val targetDate: String = savedStateHandle.get<String>(ARG_TARGET_DATE).orEmpty()
    private val shedLabel: String = savedStateHandle.get<String>(ARG_SHED_LABEL).orEmpty()
    private val sessionLabel: String = savedStateHandle.get<String>(ARG_SESSION_LABEL).orEmpty()
    private val parkLabel: String = savedStateHandle.get<String>(ARG_PARK_LABEL).orEmpty()

    private val completionKey = FeedCompletionLocalStore.key(shedId, null, sessionNo, workflow)

    private val completeKey = DraftIdempotencyKey(savedStateHandle, KEY_COMPLETE_IDEMPOTENCY, "feed-direction-complete")
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    private val _state = MutableStateFlow(
        FeedCompleteUiState(
            shedLabel = shedLabel,
            sessionLabel = sessionLabel,
            workflowLabel = workflow.replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() },
        ),
    )
    val state: StateFlow<FeedCompleteUiState> = _state.asStateFlow()

    private var statusJob: Job? = null

    init {
        analytics.track(AnalyticsEvents.FEED_COMPLETE_OPENED)
        outboxItemId.value?.let(::observeOutboxItem)
    }

    fun onEvent(event: FeedCompleteEvent) {
        when (event) {
            FeedCompleteEvent.RecordVideo -> recordVideo()
            FeedCompleteEvent.MarkDone -> markDone()
            FeedCompleteEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    /** Optional video. Captures a clip through the shared [ProofCaptureSource] and enqueues a
     *  PROOF_UPLOAD registered against the shed. Failure never blocks completion. */
    private fun recordVideo() {
        if (_state.value.isCapturingVideo || shedId.isBlank()) return
        _state.update { it.copy(isCapturingVideo = true, videoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(
                    ProofCaptureContext(
                        title = feedCompleteProofCaption(),
                        primaryTag = shedLabel.ifBlank { shedId },
                        workLabel = sessionLabel.ifBlank { "Session $sessionNo" },
                        prompt = ProofCapturePrompt.FEED_DISTRIBUTION,
                    ),
                )
            } catch (error: Exception) {
                crashReporter.recordException(error, "feed complete video capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingVideo = false) }
                return@launch
            }
            val result = proofCaptureRepository.capture(
                taskId = shedId,
                fieldKey = FIELD_FEED_COMPLETE_VIDEO,
                subject = ProofSubject.SHED,
                subjectId = shedId,
                localUri = captured.localUri,
                mimeType = captured.mimeType,
                caption = feedCompleteProofCaption(),
                scopeType = "shed",
                scopeId = shedId,
                capturedStartMs = captured.startedAtMs,
                capturedEndMs = captured.endedAtMs,
                capturedByPrincipalId = null,
                proofPolicy = feedShedProofPolicy(captured.captureSource),
                awaitUploadEnqueue = true,
                uploadGroupKey = completionKey,
            )
            when (result) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEvents.FEED_COMPLETE_VIDEO_CAPTURED)
                    _state.update { it.copy(isCapturingVideo = false, videoCaptured = true, videoMessage = VIDEO_QUEUED) }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "feed complete proof enqueue failed") }
                    _state.update { it.copy(isCapturingVideo = false, videoMessage = VIDEO_FAILED) }
                }
            }
        }
    }

    private fun markDone() {
        if (!_state.value.canComplete) return
        viewModelScope.launch {
            val result = syncRepository.enqueueFeedDirectionComplete(
                // The shed-session key partitions ordering so two completions of the same shed-session
                // drain strictly oldest-first.
                groupKey = completionKey,
                idempotencyKey = completeKey.current(),
                parkId = parkId,
                shedId = shedId,
                sessionNo = sessionNo,
                targetDate = targetDate,
                workflow = workflow,
            )
            when (result) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    // Optimistic offline overlay: the list row shows completed immediately.
                    feedCompletionStore.markCompleted(completionKey)
                    analytics.track(AnalyticsEvents.FEED_DIRECTION_COMPLETED)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "feed complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_COMPLETE_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update {
                        it.copy(result = FeedCompleteResultUi(FeedCompleteStatus.FAILED, result.message), canComplete = true)
                    }
                }
            }
        }
    }

    private fun observeOutboxItem(itemId: String) {
        statusJob?.cancel()
        statusJob = viewModelScope.launch {
            syncRepository.observeStatus()
                .map { status -> status.items.firstOrNull { it.id == itemId } }
                .filterNotNull()
                .distinctUntilChanged()
                .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)
                .collect { item ->
                    item ?: return@collect
                    _state.update {
                        val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                        it.copy(
                            result = FeedCompleteResultUi(writeResult.status.toFeedStatus(), writeResult.message.orEmpty()),
                            canComplete = !writeResult.isCommitted,
                        )
                    }
                }
        }
    }

    private fun sg.mesha.goatos.feature.counts.CountsWriteStatus.toFeedStatus(): FeedCompleteStatus = when (this) {
        sg.mesha.goatos.feature.counts.CountsWriteStatus.SYNCED -> FeedCompleteStatus.SYNCED
        sg.mesha.goatos.feature.counts.CountsWriteStatus.FAILED -> FeedCompleteStatus.FAILED
        else -> FeedCompleteStatus.QUEUED
    }

    private fun feedCompleteProofCaption(): String =
        proofOverlayContextLine(
            feature = "Feed direction",
            parkLabel = parkLabel.ifBlank { parkId },
            locationLabel = shedLabel.ifBlank { shedId },
            extraLabel = sessionLabel.ifBlank { "Session $sessionNo" },
        )

    companion object {
        const val ARG_PARK_ID = "park_id"
        const val ARG_SHED_ID = "shed_id"
        const val ARG_SESSION_NO = "session_no"
        const val ARG_WORKFLOW = "workflow"
        const val ARG_TARGET_DATE = "target_date"
        const val ARG_SHED_LABEL = "shed_label"
        const val ARG_SESSION_LABEL = "session_label"
        const val ARG_PARK_LABEL = "park_label"

        private const val KEY_COMPLETE_IDEMPOTENCY = "feedComplete.completeKey"
        private const val KEY_OUTBOX_ITEM_ID = "feedComplete.outboxItemId"
        private const val FIELD_FEED_COMPLETE_VIDEO = "feed_complete_video"
        private const val QUEUED_MESSAGE = "Saved on this phone. The completion will sync automatically."
        private const val SYNCED_MESSAGE = "Feed direction completed for this shed session."
        private const val VIDEO_QUEUED = "Video saved on this phone. It will upload automatically."
        private const val VIDEO_FAILED = "Couldn't save the video. You can still mark this feeding done."
    }
}

/**
 * The proof policy every feed capture uses.
 *
 * [ProofPolicy.maximumCountPerField] = 1 because a feed slot holds exactly ONE proof: the weight
 * photo, the feed video and the water video are distinct required steps, not repeat takes, and
 * replacing one goes through re-capture (discard, then capture). Without it all three counted
 * against the SHED's shared cap of five, so a pen that had been re-captured a few times refused
 * every further capture and wrote no row — the proof appeared to vanish (2026-08-13, Castro - 1
 * session 2, found holding three weight photos and two videos).
 */
internal fun feedShedProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "shed_level_video",
        subjectScope = ProofSubject.SHED.wireValue,
        expectedSubjects = listOf(ProofSubject.SHED.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
    )
