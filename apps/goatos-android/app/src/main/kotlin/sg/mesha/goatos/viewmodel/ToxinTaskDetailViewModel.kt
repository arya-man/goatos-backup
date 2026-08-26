package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import sg.mesha.goatos.R
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsToxin
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.ToxinRepository
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.toxinTaskGroupKey
import sg.mesha.goatos.core.network.dto.ToxinStepDto
import sg.mesha.goatos.core.network.dto.ToxinTaskDetailDto
import sg.mesha.goatos.feature.toxin.ToxinOutcomeOptionUi
import sg.mesha.goatos.feature.toxin.ToxinStepKind
import sg.mesha.goatos.feature.toxin.ToxinStepState
import sg.mesha.goatos.feature.toxin.ToxinStepUi
import sg.mesha.goatos.feature.toxin.ToxinTaskDetailEvent
import sg.mesha.goatos.feature.toxin.ToxinTaskDetailUiState
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import javax.inject.Inject

/**
 * ONE aflatoxin test round's guided screen state holder (module toxin, maintainer decision
 * 2026-08-25 — `docs/decisions/toxin-testing-module.md`).
 *
 * Room is the single source of truth: the screen renders from [ToxinRepository.observeTaskDetail]
 * merged with this phone's own in-flight capture state. The SERVER owns every step state and
 * every visible sentence; this class maps the payload one-to-one and adds nothing.
 *
 * THE DEVICE CLOCK DECIDES NOTHING. `available_at` is carried through as display-countdown input
 * only — a step is actionable if and only if the server said `available`, and the server re-checks
 * on the write. That is why a capture is never allowed to "unlock" a waiting step optimistically:
 * the write would come back refused and the operator would have shot a clip for nothing.
 *
 * Every capture rides the standard proof pipeline (durable Room row -> upload outbox -> the step
 * write that references it), all on ONE FIFO task lane ([toxinTaskGroupKey]) so a step's upload
 * always drains before the completion that resolves it.
 */
@HiltViewModel
class ToxinTaskDetailViewModel @Inject constructor(
    private val repository: ToxinRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val photoCaptureSource: PhotoCaptureSource,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    @ApplicationContext private val appContext: Context,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val taskId: String = savedStateHandle.get<String>(ARG_TASK_ID).orEmpty()

    /** This phone's own transient state: what is mid-capture, and step 7's un-sent halves. */
    private data class Local(
        val workingStepNo: Int? = null,
        val stripPhotoWorking: Boolean = false,
        /** The PROOF_UPLOAD outbox row id of the captured strip photo; blank until captured. */
        val stripPhotoOutboxItemId: String = "",
        val selectedOutcome: String = "",
        val submitInFlight: Boolean = false,
        val submitQueued: Boolean = false,
        val message: String? = null,
        val isRefreshing: Boolean = false,
    )

    private val local = MutableStateFlow(Local())

    /**
     * The DURABLE slot the strip photo occupies.
     *
     * The capture used to be remembered only in [Local], in memory. Two things followed, and a
     * tester hit both: the screen's ONLY sign that a photo had landed was a button label, and any
     * ViewModel death (leaving the drill, a low-memory kill) forgot the capture even though the
     * upload was durably queued — so the photo read as never taken and got shot again, and again.
     * Observing the slot makes the capture survive, and gives the screen the image to SHOW.
     */
    private val stripSlot = EvidenceSlot(
        identity = ProofIdentity(flow = ProofFlow.TOXIN, taskId = taskId, subjectKey = STRIP_PHOTO_SUBJECT_KEY),
        fieldKey = STRIP_PHOTO_SUBJECT_KEY,
    )

    val state: StateFlow<ToxinTaskDetailUiState> =
        combine(
            repository.observeTaskDetail(taskId),
            local,
            proofCaptureRepository.observeLatest(stripSlot),
        ) { detail, own, stripPhoto -> toUiState(detail, own, stripPhoto) }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ToxinTaskDetailUiState())

    init {
        refresh()
    }

    fun onEvent(event: ToxinTaskDetailEvent) {
        when (event) {
            ToxinTaskDetailEvent.Refresh -> refresh()
            ToxinTaskDetailEvent.Back -> Unit
            is ToxinTaskDetailEvent.RecordStepVideo -> recordStepVideo(event.stepNo)
            ToxinTaskDetailEvent.CaptureStripPhoto -> captureStripPhoto()
            is ToxinTaskDetailEvent.SelectOutcome -> local.update { it.copy(selectedOutcome = event.value) }
            ToxinTaskDetailEvent.SubmitReading -> submitReading()
            ToxinTaskDetailEvent.DismissMessage -> local.update { it.copy(message = null) }
        }
    }

    /** Non-blocking by contract: on failure the cached detail keeps rendering. */
    private fun refresh() {
        if (taskId.isBlank()) return
        viewModelScope.launch {
            local.update { it.copy(isRefreshing = true) }
            try {
                repository.refreshTaskDetail(taskId)
            } finally {
                local.update { it.copy(isRefreshing = false) }
            }
        }
    }

    /**
     * Records one guided VIDEO step (steps 1/2/3/5/6).
     *
     * Re-checks the SERVER's state for that step immediately before opening the camera: the screen
     * may have been sitting open while a teammate completed it, or while a wait had not actually
     * elapsed. Recording first and discovering that afterwards wastes the one thing that cannot be
     * redone cheaply — the operator's trip to the sample.
     */
    private fun recordStepVideo(stepNo: Int) {
        if (taskId.isBlank() || local.value.workingStepNo != null) return
        viewModelScope.launch {
            val detail = repository.observeTaskDetail(taskId).first()
            val step = detail?.steps?.firstOrNull { it.stepNo == stepNo }
            if (step == null || step.state != WIRE_STATE_AVAILABLE) {
                // The server has moved on. Pull its truth rather than guessing what changed.
                refresh()
                return@launch
            }
            local.update { it.copy(workingStepNo = stepNo, message = null) }
            try {
                val captured = try {
                    proofCaptureSource.captureVideo(
                        ProofCaptureContext(
                            // Backend-owned step title + instruction lead the recorder chrome, so
                            // the words on the camera are the words on the card.
                            title = step.title,
                            primaryTag = detail.contextLine,
                            workLabel = step.instruction,
                            headerTitle = appContext.getString(R.string.proof_video_toxin_step_header),
                        ),
                    )
                } catch (error: Exception) {
                    reportFailure(error, "toxin step video capture failed", stepNo)
                    null
                } ?: return@launch

                // Everything after a REAL recording is durable bookkeeping. NonCancellable so
                // backing out of the screen (which cancels this scope) can never orphan a clip the
                // operator actually shot — the same defect PC Care hit on 2026-08-21.
                withContext(NonCancellable) {
                    val proofOutboxId = captureProof(
                        subjectKey = stepSubjectKey(stepNo),
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = step.title,
                        captureSource = captured.captureSource,
                        startMs = captured.startedAtMs,
                        endMs = captured.endedAtMs,
                        proofMode = PROOF_MODE_STEP_VIDEO,
                        failureContext = "toxin step video",
                        stepNo = stepNo,
                    ) ?: return@withContext

                    analytics.track(
                        AnalyticsEventsToxin.TOXIN_STEP_VIDEO_CAPTURED,
                        mapOf(AnalyticsEventsToxin.Params.STEP_NO to stepNo.toString()),
                    )
                    when (
                        val queued = syncRepository.enqueueToxinStepComplete(
                            taskId = taskId,
                            stepNo = stepNo,
                            proofOutboxItemId = proofOutboxId,
                        )
                    ) {
                        is AppResult.Ok -> analytics.track(
                            AnalyticsEventsToxin.TOXIN_STEP_SUBMITTED,
                            mapOf(AnalyticsEventsToxin.Params.STEP_NO to stepNo.toString()),
                        )
                        is AppResult.Err -> {
                            queued.cause?.let { crashReporter.recordException(it, "toxin step completion enqueue failed") }
                            analytics.track(
                                AnalyticsEventsToxin.TOXIN_FAILURE,
                                mapOf(
                                    AnalyticsEventsToxin.Params.STEP_NO to stepNo.toString(),
                                    AnalyticsEvents.Params.REASON to queued.message.take(MAX_REASON_CHARS),
                                ),
                            )
                            local.update { it.copy(message = MESSAGE_STEP_NOT_ATTACHED) }
                        }
                    }
                }
            } finally {
                local.update { it.copy(workingStepNo = null) }
            }
        }
    }

    /**
     * Step 7's strip PHOTO. Unlike a video step it does NOT complete the step on capture: the
     * photo and the reading are sent together by [submitReading], because a strip photo with no
     * reading records nothing a reviewer can act on.
     */
    private fun captureStripPhoto() {
        if (taskId.isBlank() || local.value.stripPhotoWorking || local.value.submitQueued) return
        viewModelScope.launch {
            local.update { it.copy(stripPhotoWorking = true, message = null) }
            try {
                val detail = repository.observeTaskDetail(taskId).first()
                val step = detail?.steps?.firstOrNull { it.kind == WIRE_KIND_PHOTO_READING }
                if (step == null || step.state != WIRE_STATE_AVAILABLE) {
                    refresh()
                    return@launch
                }
                val captured = try {
                    photoCaptureSource.capturePhoto(
                        PhotoCaptureContext(
                            title = appContext.getString(R.string.proof_photo_toxin_strip_title),
                            instruction = appContext.getString(R.string.proof_photo_toxin_strip_instruction),
                        ),
                    )
                } catch (error: Exception) {
                    reportFailure(error, "toxin strip photo capture failed", step.stepNo)
                    null
                } ?: return@launch

                withContext(NonCancellable) {
                    val proofOutboxId = captureProof(
                        subjectKey = STRIP_PHOTO_SUBJECT_KEY,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = step.title,
                        captureSource = captured.captureSource,
                        startMs = captured.capturedAtMs,
                        endMs = captured.capturedAtMs,
                        proofMode = PROOF_MODE_STRIP_PHOTO,
                        failureContext = "toxin strip photo",
                        stepNo = step.stepNo,
                    ) ?: return@withContext
                    local.update { it.copy(stripPhotoOutboxItemId = proofOutboxId) }
                    analytics.track(AnalyticsEventsToxin.TOXIN_STRIP_PHOTO_CAPTURED)
                }
            } finally {
                local.update { it.copy(stripPhotoWorking = false) }
            }
        }
    }

    /** Sends step 7: the captured strip photo plus the BACKEND-OWNED outcome value picked. */
    private fun submitReading() {
        val own = local.value
        if (taskId.isBlank() || own.submitInFlight || own.submitQueued) return
        if (own.selectedOutcome.isBlank()) return
        viewModelScope.launch {
            // Read the capture from its DURABLE slot at submit time, so a photo taken before a
            // ViewModel death still sends instead of the round looking un-photographed.
            val stripPhotoOutboxItemId = proofCaptureRepository.observeLatest(stripSlot).first()
                ?.outboxItemId
                .orEmpty()
            if (stripPhotoOutboxItemId.isBlank()) {
                local.update { it.copy(message = MESSAGE_STRIP_PHOTO_MISSING) }
                return@launch
            }
            local.update { it.copy(submitInFlight = true, message = null) }
            try {
                when (
                    val queued = syncRepository.enqueueToxinSubmit(
                        taskId = taskId,
                        outcome = own.selectedOutcome,
                        stripPhotoOutboxItemId = stripPhotoOutboxItemId,
                    )
                ) {
                    is AppResult.Ok -> {
                        analytics.track(
                            AnalyticsEventsToxin.TOXIN_READING_SUBMITTED,
                            mapOf(AnalyticsEvents.Params.OUTCOME to own.selectedOutcome),
                        )
                        local.update { it.copy(submitQueued = true) }
                    }
                    is AppResult.Err -> {
                        queued.cause?.let { crashReporter.recordException(it, "toxin reading submit enqueue failed") }
                        analytics.track(
                            AnalyticsEventsToxin.TOXIN_FAILURE,
                            mapOf(AnalyticsEvents.Params.REASON to queued.message.take(MAX_REASON_CHARS)),
                        )
                        local.update { it.copy(message = MESSAGE_READING_NOT_SENT) }
                    }
                }
            } finally {
                local.update { it.copy(submitInFlight = false) }
            }
        }
    }

    /**
     * Writes one durable proof row and returns the id of the PROOF_UPLOAD outbox row the step
     * write must reference, or null when the clip never reached the outbox.
     *
     * The settle wait mirrors PC Care's: the upload-row id can land in Room a beat AFTER the
     * capture result is composed, so a blank id here is usually a read race, not a lost clip.
     * Only a row still without an upload id after the wait is a genuine failure.
     */
    private suspend fun captureProof(
        subjectKey: String,
        localUri: String,
        mimeType: String,
        caption: String,
        captureSource: String,
        startMs: Long,
        endMs: Long,
        proofMode: String,
        failureContext: String,
        stepNo: Int,
    ): String? {
        val slot = EvidenceSlot(
            identity = ProofIdentity(flow = ProofFlow.TOXIN, taskId = taskId, subjectKey = subjectKey),
            fieldKey = subjectKey,
        )
        val result = proofCaptureRepository.captureReplacingLatest(
            slot = slot,
            subject = ProofSubject.OTHER,
            // The proof platform requires a UUID subject/scope; the test ROUND is that subject and
            // the step rides the field key. There is no animal here at all — a feed load is not a goat.
            subjectId = taskId,
            localUri = localUri,
            mimeType = mimeType,
            caption = caption,
            scopeType = SCOPE_TYPE_TASK,
            scopeId = taskId,
            capturedStartMs = startMs,
            capturedEndMs = endMs,
            capturedByPrincipalId = null,
            proofPolicy = toxinProofPolicy(proofMode, captureSource),
            awaitUploadEnqueue = true,
            // ONE FIFO lane per round: the upload drains BEFORE the step completion that resolves
            // it, and before the final reading submit (ToxinPayloads.kt).
            uploadGroupKey = toxinTaskGroupKey(taskId),
        )
        when (result) {
            is AppResult.Err -> {
                result.cause?.let { crashReporter.recordException(it, "$failureContext capture write failed") }
                analytics.track(
                    AnalyticsEventsToxin.TOXIN_FAILURE,
                    mapOf(
                        AnalyticsEventsToxin.Params.STEP_NO to stepNo.toString(),
                        AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON_CHARS),
                    ),
                )
                local.update { it.copy(message = MESSAGE_CAPTURE_NOT_SAVED) }
                return null
            }
            is AppResult.Ok -> Unit
        }
        var proofOutboxId = result.value.outboxItemId
        var waited = 0L
        while (proofOutboxId.isNullOrBlank() && waited < PROOF_ROW_SETTLE_MAX_MS) {
            delay(PROOF_ROW_SETTLE_STEP_MS)
            waited += PROOF_ROW_SETTLE_STEP_MS
            proofOutboxId = proofCaptureRepository.observeProofs(taskId).first()
                .firstOrNull { it.id == result.value.id }
                ?.outboxItemId
        }
        if (proofOutboxId.isNullOrBlank()) {
            crashReporter.recordException(
                IllegalStateException("toxin clip ${result.value.id} has no upload row after ${waited}ms"),
                "$failureContext never enqueued its upload",
            )
            analytics.track(
                AnalyticsEventsToxin.TOXIN_FAILURE,
                mapOf(AnalyticsEventsToxin.Params.STEP_NO to stepNo.toString()),
            )
            local.update { it.copy(message = MESSAGE_CAPTURE_NOT_SAVED) }
            return null
        }
        return proofOutboxId
    }

    private fun reportFailure(error: Throwable, context: String, stepNo: Int) {
        if (error is CancellationException) throw error
        crashReporter.recordException(error, context)
        analytics.track(
            AnalyticsEventsToxin.TOXIN_FAILURE,
            mapOf(
                AnalyticsEventsToxin.Params.STEP_NO to stepNo.toString(),
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS),
            ),
        )
    }

    private fun toUiState(
        detail: ToxinTaskDetailDto?,
        own: Local,
        stripPhoto: ProofCaptureRow?,
    ): ToxinTaskDetailUiState {
        if (detail == null) {
            return ToxinTaskDetailUiState(
                isRefreshing = own.isRefreshing,
                message = own.message,
            )
        }
        val steps = detail.steps
            .sortedBy { it.stepNo }
            .map { it.toStepUi(workingStepNo = own.workingStepNo, stripPhotoWorking = own.stripPhotoWorking) }
        val readingStepOpen = detail.steps.any {
            it.kind == WIRE_KIND_PHOTO_READING && it.state == WIRE_STATE_AVAILABLE
        }
        return ToxinTaskDetailUiState(
            contextLine = detail.contextLine,
            statusChip = detail.statusChip,
            statusTone = detail.statusTone,
            isOverdue = detail.isOverdue,
            originLine = detail.originLine,
            reviewReason = detail.reviewReason,
            cancelReason = detail.cancelReason,
            outcomeLabel = detail.outcomeLabel,
            steps = steps,
            stepsDone = detail.stepsDone,
            stepsTotal = detail.stepsTotal,
            readingGuide = detail.readingGuide,
            outcomeOptions = detail.outcomeOptions.map { ToxinOutcomeOptionUi(value = it.value, label = it.label) },
            selectedOutcome = own.selectedOutcome,
            // The DURABLE capture, not a remembered id: survives leaving the screen and a
            // process death, so a queued photo is never re-shot because the UI forgot it.
            stripPhotoCaptured = stripPhoto != null,
            stripPhotoUri = stripPhoto?.localUri.orEmpty(),
            stripPhotoWorking = own.stripPhotoWorking,
            // BOTH halves, and only while the SERVER still says the reading step is open.
            submitEnabled = readingStepOpen &&
                stripPhoto?.outboxItemId?.isNotBlank() == true &&
                own.selectedOutcome.isNotBlank(),
            submitInFlight = own.submitInFlight,
            submitQueued = own.submitQueued,
            isRefreshing = own.isRefreshing,
            message = own.message,
        )
    }

    private fun ToxinStepDto.toStepUi(workingStepNo: Int?, stripPhotoWorking: Boolean): ToxinStepUi {
        val kind = ToxinStepKind.from(kind)
        return ToxinStepUi(
            stepNo = stepNo,
            kind = kind,
            state = ToxinStepState.from(state),
            title = title,
            instruction = instruction,
            completedLine = listOf(completedBy, farmInstant(completedAt))
                .filter { it.isNotBlank() }
                .joinToString(" · "),
            availableAtEpochMs = epochMillis(availableAt),
            working = workingStepNo == stepNo ||
                (kind == ToxinStepKind.PHOTO_READING && stripPhotoWorking),
        )
    }

    private companion object {
        const val ARG_TASK_ID = "task_id"
        const val SCOPE_TYPE_TASK = "task"
        const val STRIP_PHOTO_SUBJECT_KEY = "strip-photo"
        const val PROOF_MODE_STEP_VIDEO = "toxin_step_video"
        const val PROOF_MODE_STRIP_PHOTO = "toxin_strip_photo"
        /** The wire vocabulary this screen keys off, from backend/internal/toxin/adapters/http. */
        const val WIRE_STATE_AVAILABLE = "available"
        const val WIRE_KIND_PHOTO_READING = "photo_reading"
        const val PROOF_ROW_SETTLE_MAX_MS = 3_000L
        const val PROOF_ROW_SETTLE_STEP_MS = 100L
        const val MAX_REASON_CHARS = 120
        const val MESSAGE_CAPTURE_NOT_SAVED = "That didn't save. Record it again."
        const val MESSAGE_STEP_NOT_ATTACHED = "Saved, but couldn't be attached. Tap refresh to try again."
        const val MESSAGE_READING_NOT_SENT = "The reading couldn't be sent. Try again."
        const val MESSAGE_STRIP_PHOTO_MISSING = "Take the strip photo before sending the reading."
        const val INDIA_ZONE = "Asia/Kolkata"
    }
}

/** One capture identity per guided step of one round. */
internal fun stepSubjectKey(stepNo: Int): String = "step-$stepNo"

/**
 * A toxin capture's proof policy.
 *
 * `maximumCountPerField = 1` is the load-bearing part: the seven steps all carry the ROUND as
 * their proof subject, so a pooled per-subject budget would let re-shoots of one step exhaust the
 * budget for the others — the exact defect feed distribution hit on 2026-08-13. One row per step,
 * replaced on a re-shoot.
 */
internal fun toxinProofPolicy(proofMode: String, captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = proofMode,
        subjectScope = ProofSubject.OTHER.wireValue,
        expectedSubjects = listOf(ProofSubject.OTHER.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
        // Six captures per round plus headroom for the transient replacement rows a re-shoot makes.
        maximumCountPerSubject = 60,
    )

/** RFC3339 -> epoch millis; 0 when absent or malformed (the countdown then simply renders nothing). */
internal fun epochMillis(rfc3339: String): Long {
    if (rfc3339.isBlank()) return 0L
    // exception:exempt a malformed server instant degrades to "no countdown"; the step's own
    // backend instruction still explains the wait, and the gate is the server's regardless.
    return runCatching { Instant.parse(rfc3339).toEpochMilli() }.getOrDefault(0L)
}

/** RFC3339 -> a farm-readable IST instant ("21 Aug, 4:10 pm"); blank when absent or malformed. */
internal fun farmInstant(rfc3339: String): String {
    if (rfc3339.isBlank()) return ""
    // exception:exempt a malformed server instant renders no attribution time; the name still shows
    return runCatching {
        TOXIN_INSTANT_FORMAT.format(Instant.parse(rfc3339).atZone(ZoneId.of("Asia/Kolkata")))
    }.getOrDefault("")
}

private val TOXIN_INSTANT_FORMAT: DateTimeFormatter =
    DateTimeFormatter.ofPattern("d MMM, h:mm a", Locale.ENGLISH)
