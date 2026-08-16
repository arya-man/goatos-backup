package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.data.WorkflowVideoDraft
import sg.mesha.goatos.core.data.capture.buildWorkflowEvidenceSlot
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.feature.counts.WorkflowActionSection
import sg.mesha.goatos.feature.counts.WorkflowActionUi
import sg.mesha.goatos.feature.counts.WorkflowAnswerOptionUi
import sg.mesha.goatos.feature.counts.WorkflowDetailEvent
import sg.mesha.goatos.feature.counts.WorkflowDetailUiState
import sg.mesha.goatos.feature.counts.WorkflowStatusTone
import java.time.Duration
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.io.File
import java.net.URI
import java.util.UUID
import javax.inject.Inject

/**
 * The drill-in for one Birth/Death workflow (docs/decisions/birth-death-workflows.md).
 *
 * Reads are offline-first: the screen renders the Room-cached detail immediately
 * ([WorkflowsRepository.observeDetail]) and a background [refresh] reconciles. Writes are
 * offline-first through the durable outbox with DETERMINISTIC per-action idempotency keys
 * (`wf-answer:<action_id>` / `wf-complete:<action_id>`): an action completes at most once, so the
 * key needs no draft lifecycle — a resend after process death collapses onto the original write,
 * and the outbox's same-key/different-payload guard surfaces a second, different answer instead of
 * silently replacing the first. On enqueue the cached detail flips optimistically (Room re-emits).
 *
 * A `requires_video` completion mirrors the shifting execute flow: capture through the shared
 * [ProofCaptureSource], enqueue the PROOF_UPLOAD on the WORKFLOW's outbox group so it drains
 * FIRST, then enqueue the completion carrying the proof outbox item id — the sync engine resolves
 * the uploaded proof_id into `proof_ref`. `tag_the_kid` keeps the existing promote flow as the
 * single identifier writer, but remains open until the operator also records its mandatory video.
 */
@HiltViewModel
class WorkflowDetailViewModel @Inject constructor(
    private val repo: WorkflowsRepository,
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val workflowId: String = savedStateHandle[ARG_WORKFLOW_ID] ?: ""

    /** Canonical slot grain for a workflow action-video capture. identity.taskId/fieldKey resolve
     *  to the SAME strings ([workflowId] passthrough / the existing [workflowProofFieldKey]
     *  literal) already produced, so routing captures through this slot changes no on-disk value
     *  while keying these rows into the shared retirement/recovery/referee machinery. */
    internal fun workflowEvidenceSlot(actionId: String, goatId: String): EvidenceSlot =
        buildWorkflowEvidenceSlot(workflowId, goatId, actionId)

    /**
     * The lens this drill-in was opened through, supplied by the route.
     *
     * Empty for Birth/Death: the full action list. `colostrum` + a business date narrows the rows
     * to that day's feeds and re-counts the header at the day grain, so the detail agrees with the
     * Colostrum card that opened it (docs/decisions/colostrum-milk-module.md). The narrowing is the
     * BACKEND's — the client passes the lens through and renders what comes back, including a
     * blocked reason naming work this screen does not itself show.
     */
    private val lens: String = savedStateHandle[ARG_LENS] ?: ""
    private val lensDate: String = savedStateHandle[ARG_DATE] ?: ""

    private val _state = MutableStateFlow(WorkflowDetailUiState(workflowId = workflowId))
    val state: StateFlow<WorkflowDetailUiState> = _state.asStateFlow()
    private var finalizedDeathSubmission = false

    init {
        observeDetail()
        refresh()
    }

    fun onEvent(event: WorkflowDetailEvent) {
        when (event) {
            WorkflowDetailEvent.Refresh -> refresh()
            is WorkflowDetailEvent.Answer -> answer(event.actionId, event.value)
            is WorkflowDetailEvent.Complete -> complete(event.actionId)
            is WorkflowDetailEvent.RecordVideo -> captureAndComplete(event.actionId)
            WorkflowDetailEvent.SubmitDeath -> submitDeath()
            WorkflowDetailEvent.NavigationHandled -> _state.update { it.copy(returnToList = false) }
            is WorkflowDetailEvent.OpenPromote -> analytics.track(AnalyticsEvents.COUNTS_RFID_PROMOTE_OPENED)
            WorkflowDetailEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    private fun observeDetail() {
        viewModelScope.launch {
            combine(
                repo.observeDetail(workflowId, lens, lensDate),
                repo.observeVideoDrafts(workflowId),
                syncRepository.observeStatus(),
            ) { detail, drafts, sync -> Triple(detail, drafts, sync) }
                .collect { (detail, drafts, sync) ->
                if (detail != null) {
                    val submitting = drafts.any { it.syncStatus == DRAFT_STATUS_SUBMITTING }
                    val earliestDraft = drafts.minOfOrNull { it.startedAtMs } ?: Long.MAX_VALUE
                    val submissionItems = sync.items.filter { item ->
                        item.groupKey == workflowId &&
                            item.createdAt >= earliestDraft - SUBMISSION_ITEM_CLOCK_TOLERANCE_MS &&
                            (item.opType == OP_PROOF_UPLOAD || item.opType == OP_WORKFLOW_ACTION_COMPLETE)
                    }
                    val uploadFailed = submitting && submissionItems.any { it.status == SyncItemStatus.FAILED }
                    val completionsSucceeded = submissionItems.count {
                        it.opType == OP_WORKFLOW_ACTION_COMPLETE && it.status == SyncItemStatus.SUCCEEDED
                    }
                    _state.update { detail.toUiState(it, drafts, uploadFailed) }
                    if (submitting && completionsSucceeded >= 2 && !finalizedDeathSubmission) {
                        finalizedDeathSubmission = true
                        detail.actions
                            .filter { it.actionKey == ACTION_KEY_DEATH_VIDEO || it.actionKey == ACTION_KEY_POST_MORTEM_VIDEO }
                            .forEach { repo.markActionCompleted(workflowId, it.actionId, inReview = false) }
                        repo.clearVideoDrafts(workflowId)
                        repo.refreshDetail(workflowId, lens, lensDate)
                        _state.update {
                            it.copy(
                                message = SUBMITTED_MESSAGE,
                                isErrorMessage = false,
                                // Both videos are server-confirmed and the death now waits on an
                                // approver, so return the operator to the Death list and carry the
                                // acknowledgement there. A still-uploading or failed submit keeps
                                // its banner on this screen.
                                returnToList = true,
                                submissionNotice = SUBMITTED_MESSAGE,
                            )
                        }
                    }
                }
            }
        }
    }

    private fun refresh() {
        if (_state.value.isRefreshing) return
        _state.update { it.copy(isRefreshing = true) }
        viewModelScope.launch {
            repo.refreshDetail(workflowId, lens, lensDate)
                .onSuccess {
                    _state.update { it.copy(isRefreshing = false, loading = false) }
                }
                .onFailure { error ->
                    crashReporter.recordException(error, "workflow detail refresh failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "workflow_detail",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                    _state.update { current ->
                        current.copy(
                            isRefreshing = false,
                            loading = false,
                            // Only a NEVER-cached workflow is a dead end; a cached one keeps serving.
                            notFound = current.displayId.isBlank() && current.actions.isEmpty(),
                        )
                    }
                }
        }
    }

    // -----------------------------------------------------------------------
    // Action writes — durable outbox, deterministic per-action keys
    // -----------------------------------------------------------------------

    private fun answer(actionId: String, value: String) {
        if (_state.value.actions.firstOrNull { it.actionId == actionId }?.requiresVideo == true) {
            captureAndComplete(actionId, answerValue = value)
            return
        }
        viewModelScope.launch {
            val result = syncRepository.enqueueWorkflowActionAnswer(
                groupKey = workflowId,
                idempotencyKey = "wf-answer:$actionId",
                workflowId = workflowId,
                actionId = actionId,
                answerValue = value,
            )
            when (result) {
                is AppResult.Ok -> {
                    // Optimistic: Room flips the action to completed and re-emits; the next
                    // refresh reconciles with the backend counters.
                    repo.markActionAnswered(workflowId, actionId, value)
                    analytics.track(AnalyticsEvents.WORKFLOW_ACTION_ANSWERED)
                    _state.update { it.copy(message = QUEUED_MESSAGE, isErrorMessage = false) }
                }
                is AppResult.Err -> onWriteFailed("workflow_answer", result)
            }
        }
    }

    private fun complete(actionId: String) {
        viewModelScope.launch {
            val result = syncRepository.enqueueWorkflowActionComplete(
                groupKey = workflowId,
                idempotencyKey = "wf-complete:$actionId",
                workflowId = workflowId,
                actionId = actionId,
            )
            when (result) {
                is AppResult.Ok -> {
                    repo.markActionCompleted(workflowId, actionId, inReview = false)
                    analytics.track(AnalyticsEvents.WORKFLOW_ACTION_COMPLETED)
                    _state.update { it.copy(message = QUEUED_MESSAGE, isErrorMessage = false) }
                }
                is AppResult.Err -> onWriteFailed("workflow_complete", result)
            }
        }
    }

    /**
     * The MANDATORY video for a `requires_video` action: capture with the live in-app camera, enqueue
     * the PROOF_UPLOAD on the workflow's group (drains first), then enqueue the completion carrying
     * the proof outbox item id. A completion without its proof never reaches the backend — and the
     * backend re-rejects one with 422 `proof_required`.
     */
    private fun captureAndComplete(actionId: String, answerValue: String? = null) {
        val current = _state.value
        if (current.isCapturingVideo) return
        val action = current.actions.firstOrNull { it.actionId == actionId }
        val goatId = current.subjectGoatId
        val prompt = when (action?.actionKey) {
            ACTION_KEY_DEATH_VIDEO -> ProofCapturePrompt.DEATH
            ACTION_KEY_POST_MORTEM_VIDEO -> ProofCapturePrompt.POST_MORTEM
            else -> ProofCapturePrompt.BIRTH
        }
        _state.update { it.copy(isCapturingVideo = true, message = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(
                    ProofCaptureContext(
                        title = workflowProofCaption(current, action),
                        primaryTag = current.subjectLocationDisplay.ifBlank { current.displayId },
                        secondaryTag = current.displayId.takeIf { it.isNotBlank() },
                        workLabel = action?.title.orEmpty(),
                        prompt = prompt,
                        headerTitle = action?.title,
                    ),
                )
            } catch (error: Exception) {
                crashReporter.recordException(error, "workflow video capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingVideo = false) }
                return@launch
            }
            if (current.isDeath) {
                val previous = repo.replaceVideoDraft(
                    WorkflowVideoDraft(
                        id = UUID.randomUUID().toString(),
                        workflowId = workflowId,
                        actionId = actionId,
                        subjectGoatId = goatId,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        startedAtMs = captured.startedAtMs,
                        endedAtMs = captured.endedAtMs,
                        captureSource = captured.captureSource,
                    ),
                )
                previous?.localUri?.let(::deletePrivateDraftFile)
                analytics.track(
                    AnalyticsEvents.WORKFLOW_VIDEO_CAPTURED,
                    mapOf(
                        AnalyticsEvents.Params.ITEM_ID to workflowId,
                        AnalyticsEvents.Params.ACTION to "death_draft",
                    ),
                )
                _state.update {
                    it.copy(
                        isCapturingVideo = false,
                        message = "Video saved as draft. You can re-record it before Submit.",
                        isErrorMessage = false,
                    )
                }
                return@launch
            }
            val slot = workflowEvidenceSlot(actionId, goatId)
            val proofResult = proofCaptureRepository.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.GOAT,
                subjectId = goatId,
                localUri = captured.localUri,
                mimeType = captured.mimeType,
                caption = workflowProofCaption(current, action),
                scopeType = "goat",
                scopeId = goatId,
                capturedStartMs = captured.startedAtMs,
                capturedEndMs = captured.endedAtMs,
                capturedByPrincipalId = null,
                proofPolicy = workflowGoatProofPolicy(captured.captureSource),
                awaitUploadEnqueue = true,
                uploadGroupKey = workflowId,
            )
            val proofItemId = when (proofResult) {
                is AppResult.Ok -> proofResult.value.outboxItemId
                is AppResult.Err -> {
                    _state.update { it.copy(isCapturingVideo = false) }
                    onWriteFailed("workflow_video", proofResult)
                    return@launch
                }
            } ?: run {
                _state.update { it.copy(isCapturingVideo = false, message = "Video upload could not be queued.", isErrorMessage = true) }
                return@launch
            }
            analytics.track(
                AnalyticsEvents.WORKFLOW_VIDEO_CAPTURED,
                mapOf(
                    AnalyticsEvents.Params.ITEM_ID to workflowId,
                    AnalyticsEvents.Params.ACTION to actionId,
                ),
            )
            val writeResult = if (answerValue != null) {
                syncRepository.enqueueWorkflowActionAnswer(
                    groupKey = workflowId,
                    idempotencyKey = workflowVideoAnswerKey(actionId, proofItemId),
                    workflowId = workflowId,
                    actionId = actionId,
                    answerValue = answerValue,
                    proofOutboxItemId = proofItemId,
                )
            } else {
                syncRepository.enqueueWorkflowActionComplete(
                    groupKey = workflowId,
                    // Couple the completion idempotency to this durable proof item. Exact retries
                    // collapse; a verifier-requested re-shoot gets a new command key.
                    idempotencyKey = workflowVideoCompletionKey(actionId, proofItemId),
                    workflowId = workflowId,
                    actionId = actionId,
                    proofOutboxItemId = proofItemId,
                )
            }
            when (writeResult) {
                is AppResult.Ok -> {
                    if (answerValue != null) {
                        repo.markActionAnswered(workflowId, actionId, answerValue)
                        analytics.track(AnalyticsEvents.WORKFLOW_ACTION_ANSWERED)
                    } else {
                        repo.markActionCompleted(workflowId, actionId, inReview = false)
                        analytics.track(AnalyticsEvents.WORKFLOW_ACTION_COMPLETED)
                    }
                    _state.update {
                        it.copy(isCapturingVideo = false, message = VIDEO_QUEUED_MESSAGE, isErrorMessage = false)
                    }
                }
                is AppResult.Err -> {
                    _state.update { it.copy(isCapturingVideo = false) }
                    onWriteFailed("workflow_action_with_video", writeResult)
                }
            }
        }
    }

    private fun submitDeath() {
        val current = _state.value
        if (!current.deathSubmissionEnabled) return
        _state.update { it.copy(isSubmittingDeath = true, message = null) }
        viewModelScope.launch {
            val drafts = repo.listVideoDrafts(workflowId).associateBy { it.actionId }
            val actions = _state.value.actions.sortedBy { it.actionKey != ACTION_KEY_DEATH_VIDEO }
            if (actions.size != 2 || actions.any { drafts[it.actionId] == null }) {
                _state.update { it.copy(isSubmittingDeath = false, message = "Record both videos before submitting.", isErrorMessage = true) }
                return@launch
            }
            for (action in actions) {
                val draft = drafts.getValue(action.actionId)
                val slot = workflowEvidenceSlot(action.actionId, draft.subjectGoatId)
                val proof = proofCaptureRepository.captureReplacingLatest(
                    slot = slot,
                    subject = ProofSubject.GOAT,
                    subjectId = draft.subjectGoatId,
                    localUri = draft.localUri,
                    mimeType = draft.mimeType,
                    caption = workflowProofCaption(current, action),
                    scopeType = "goat",
                    scopeId = draft.subjectGoatId,
                    capturedStartMs = draft.startedAtMs,
                    capturedEndMs = draft.endedAtMs,
                    capturedByPrincipalId = null,
                    proofPolicy = workflowGoatProofPolicy(draft.captureSource),
                    awaitUploadEnqueue = true,
                    uploadGroupKey = workflowId,
                )
                val proofId = when (proof) {
                    is AppResult.Ok -> proof.value.outboxItemId
                    is AppResult.Err -> {
                        _state.update { it.copy(isSubmittingDeath = false) }
                        onWriteFailed("workflow_video_submit", proof)
                        return@launch
                    }
                }
                if (proofId.isNullOrBlank()) {
                    _state.update {
                        it.copy(
                            isSubmittingDeath = false,
                            message = "Video upload could not be queued.",
                            isErrorMessage = true,
                        )
                    }
                    return@launch
                }
                val complete = syncRepository.enqueueWorkflowActionComplete(
                    groupKey = workflowId,
                    idempotencyKey = workflowVideoCompletionKey(action.actionId, proofId),
                    workflowId = workflowId,
                    actionId = action.actionId,
                    proofOutboxItemId = proofId,
                )
                if (complete is AppResult.Err) {
                    _state.update { it.copy(isSubmittingDeath = false) }
                    onWriteFailed("workflow_complete_submit", complete)
                    return@launch
                }
            }
            repo.markVideoDraftsSubmitting(workflowId)
            analytics.track(AnalyticsEvents.WORKFLOW_ACTION_COMPLETED)
            _state.update { it.copy(isSubmittingDeath = false, message = "Uploading both videos to the backend…", isErrorMessage = false) }
        }
    }

    private fun deletePrivateDraftFile(uri: String) {
        runCatching {
            val parsed = URI(uri)
            if (parsed.scheme == "file") File(parsed).delete()
        }
    }

    private fun workflowProofCaption(current: WorkflowDetailUiState, action: WorkflowActionUi?): String =
        proofOverlayContextLine(
            feature = current.templateLine.ifBlank { "Workflow proof" },
            locationLabel = current.subjectLocationDisplay,
            extraLabel = action?.title,
        )

    private fun onWriteFailed(kind: String, error: AppResult.Err) {
        error.cause?.let { crashReporter.recordException(it, "workflow $kind enqueue failed") }
        analytics.track(
            AnalyticsEvents.COUNTS_WRITE_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to kind,
                AnalyticsEvents.Params.REASON to error.message,
            ),
        )
        _state.update { it.copy(message = error.message, isErrorMessage = true) }
    }

    // -----------------------------------------------------------------------
    // DTO -> UI mapping (presentation only; every business value is backend-owned)
    // -----------------------------------------------------------------------

    private fun WorkflowDetailResponseDto.toUiState(
        current: WorkflowDetailUiState,
        drafts: List<WorkflowVideoDraft>,
        deathUploadFailed: Boolean,
    ): WorkflowDetailUiState {
        val now = Instant.now()
        val mainActions = operatorVisibleWorkflowActions(actions)
        // Death is the one TWO-DRAFT flow: neither video leaves the phone until Submit, so the
        // backend correctly reports both actions `pending` and the second one blocked behind its
        // predecessor. Rendering that verbatim strands the operator after the first recording —
        // pre-submit, a durable draft IS their finished work for that row.
        val isDeathModule = module == MODULE_DEATH
        val draftedActionIds = if (isDeathModule) drafts.map { it.actionId }.toSet() else emptySet()
        val draftsSubmitting = drafts.any { it.syncStatus == DRAFT_STATUS_SUBMITTING }
        return current.copy(
            loading = false,
            notFound = false,
            isDeath = module == MODULE_DEATH,
            displayId = if (templateKey == TEMPLATE_KEY_BIRTH_MOTHER) {
                subject.tag.ifBlank { subject.displayId }
            } else {
                subject.displayId.ifBlank { subject.tag }
            },
            roleLabel = subject.roleLabel,
            templateLine = listOf(
                if (module == MODULE_DEATH) TEMPLATE_DEATH else TEMPLATE_BIRTH,
                shedLabel,
            ).filter { it.isNotBlank() }.joinToString(" · "),
            facts = facts.map { it.label to it.value },
            // `in_review` is finished from the operator's perspective: verification is internal
            // and must not make a completed upload read as 0/N or suppress the exit control. A
            // death draft counts the same way — the operator recorded that video.
            actionsDone = mainActions.count {
                operatorFinishedWorkflowStatus(it.status) || it.actionId in draftedActionIds
            },
            // Backend truth, kept separate so the submission gate cannot read a draft as sent.
            deathBackendActionsDone = mainActions.count { operatorFinishedWorkflowStatus(it.status) },
            actionsTotal = mainActions.size,
            actions = mainActions.sortedBy(::workflowDisplayOrder).map { action ->
                val hasDraft = drafts.any { it.actionId == action.actionId }
                val locallyRecorded = action.actionId in draftedActionIds
                val predecessorsReady = workflowPredecessorsReady(action, mainActions) { previous ->
                    operatorFinishedWorkflowStatus(previous.status) || previous.actionId in draftedActionIds
                }
                val blocked = workflowBlockedForOperator(action, isDeathModule, predecessorsReady)
                action.toActionUi(now, blocked, locallyRecorded).copy(
                    hasVideoDraft = hasDraft,
                    canRecordVideo = canRecordWorkflowVideo(action, blocked, draftsSubmitting, predecessorsReady),
                )
            }.sortedBy { it.sectionOrder() },
            subjectGoatId = subject.goatId,
            subjectGoatRowVersion = subject.rowVersion,
            subjectTemporaryIdentifier = subject.tag,
            subjectLocationDisplay = listOf(parkLabel, shedLabel).filter { it.isNotBlank() }.joinToString(" / "),
            deathDraftCount = drafts.count { draft -> mainActions.any { it.actionId == draft.actionId } },
            deathDraftsSubmitting = drafts.any { it.syncStatus == DRAFT_STATUS_SUBMITTING },
            deathUploadFailed = deathUploadFailed,
        )
    }

    private fun WorkflowActionUi.sectionOrder(): Int = when (section) {
        WorkflowActionSection.OVERDUE -> 0
        WorkflowActionSection.SCHEDULED -> 1
        WorkflowActionSection.COMPLETED -> 2
    }

    /**
     * [blocked] is the OPERATOR-effective block, not the raw backend flag: a death row's
     * `previous_action` block is stale while its predecessor's video is still a local draft the
     * backend has not seen. [locallyRecorded] presents such a draft as finished work — it leaves
     * the live sections but keeps its control, because a draft stays re-recordable until Submit.
     */
    private fun WorkflowActionDto.toActionUi(
        now: Instant,
        blocked: Boolean,
        locallyRecorded: Boolean,
    ): WorkflowActionUi {
        val due = dueAt?.let { runCatching { Instant.parse(it) }.getOrNull() }
        val numericAnswerUnit = workflowNumericAnswerUnit(this)
        val isOverdueNow = status == STATUS_PENDING && !blocked && !locallyRecorded &&
            due != null && due.isBefore(now)
        val section = when {
            status == STATUS_COMPLETED || status == STATUS_IN_REVIEW || locallyRecorded ->
                WorkflowActionSection.COMPLETED
            isOverdueNow -> WorkflowActionSection.OVERDUE
            else -> WorkflowActionSection.SCHEDULED
        }
        val statusTone = when {
            status == STATUS_COMPLETED || locallyRecorded -> WorkflowStatusTone.DONE
            status == STATUS_IN_REVIEW -> WorkflowStatusTone.IN_REVIEW
            blocked -> WorkflowStatusTone.BLOCKED
            isOverdueNow -> WorkflowStatusTone.OVERDUE
            else -> WorkflowStatusTone.SCHEDULED
        }
        val accessLabel = workflowAccessLabel(blockedReason, due, now)
        val statusLabel = when {
            status == STATUS_COMPLETED -> LABEL_DONE
            status == STATUS_IN_REVIEW -> LABEL_IN_REVIEW
            status == STATUS_REWORK -> LABEL_REWORK
            // A recorded draft's chip is owned by the screen ("Recorded"); nothing scheduled,
            // blocked or late may speak for a row the operator has already shot.
            locallyRecorded -> LABEL_DONE
            accessLabel != null -> accessLabel
            blocked -> LABEL_BLOCKED
            isOverdueNow -> lateLabel(due, now)
            due != null -> dueLabel(due)
            else -> LABEL_SCHEDULED
        }
        val actionable = (status == STATUS_PENDING || status == STATUS_REWORK) && !blocked
        val isQuestion = actionType == TYPE_QUESTION || actionType == TYPE_QUESTION_SELECT
        val opensPromote = actionKey == ACTION_KEY_TAG_THE_KID &&
            workflowTagNeedsPermanentIdentifier(answerValue)
        return WorkflowActionUi(
            actionId = actionId,
            actionKey = actionKey,
            title = title,
            detail = detail,
            typeLabel = when (actionType) {
                TYPE_QUESTION -> if (numericAnswerUnit != null) TAG_NUMBER else TAG_QUESTION
                TYPE_QUESTION_SELECT -> TAG_QUESTION_SELECT
                TYPE_APPROVAL -> TAG_APPROVAL
                else -> TAG_ACTION
            },
            glyph = when {
                status == STATUS_COMPLETED || locallyRecorded -> GLYPH_DONE
                actionType == TYPE_QUESTION -> GLYPH_QUESTION
                actionType == TYPE_QUESTION_SELECT -> GLYPH_SELECT
                actionType == TYPE_APPROVAL -> GLYPH_APPROVAL
                else -> GLYPH_ACTION
            },
            requiresVideo = requiresVideo,
            options = if (actionType == TYPE_QUESTION_SELECT) {
                // The backend's own bands, verbatim: the band string is both value and label.
                options.map { WorkflowAnswerOptionUi(value = it, label = it) }
            } else if (numericAnswerUnit == null) {
                listOf(
                    WorkflowAnswerOptionUi(ANSWER_YES_VALUE, ANSWER_YES_LABEL),
                    WorkflowAnswerOptionUi(ANSWER_NO_VALUE, ANSWER_NO_LABEL),
                )
            } else emptyList(),
            numericAnswerUnit = numericAnswerUnit,
            statusLabel = statusLabel,
            statusTone = statusTone,
            section = section,
            canAnswer = actionable && isQuestion && !opensPromote,
            canComplete = actionable && actionType == TYPE_ACTION && !requiresVideo && !opensPromote,
            canRecordVideo = actionable && requiresVideo,
            opensPromote = opensPromote && actionable,
            // A blocked row must say WHY. On the Colostrum lens the prerequisite is not even on
            // screen (1st Colostrum waits on four birth steps that live in Birth), so a bare
            // "Blocked" chip is a dead end for the person holding the phone.
            footer = completedByLabel.orEmpty().ifBlank { workflowBlockedNote(blocked, blockedReason) },
            answerValue = answerValue,
        )
    }

    private fun lateLabel(due: Instant, now: Instant): String {
        val late = Duration.between(due, now)
        return when {
            late.toDays() >= 1 -> "${late.toDays()}d late"
            late.toHours() >= 1 -> "${late.toHours()}h late"
            else -> "${late.toMinutes().coerceAtLeast(1)}m late"
        }
    }

    private fun dueLabel(due: Instant): String {
        val zoned = due.atZone(IST)
        return if (zoned.toLocalDate() == LocalDate.now(IST)) {
            zoned.format(TIME_FORMAT)
        } else {
            zoned.format(DATE_TIME_FORMAT)
        }
    }

    companion object {
        const val ARG_WORKFLOW_ID = "workflow_id"
        const val ARG_LENS = "lens"
        const val ARG_DATE = "date"

        private val IST: ZoneId = ZoneId.of("Asia/Kolkata")
        private val TIME_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm")
        private val DATE_TIME_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM · HH:mm")
        private const val MODULE_DEATH = "death"
        private const val TEMPLATE_KEY_BIRTH_MOTHER = "birth_mother"
        private const val TYPE_QUESTION = "question"
        private const val TYPE_QUESTION_SELECT = "question_select"
        private const val TYPE_ACTION = "action"
        private const val TYPE_APPROVAL = "approval"
        private const val STATUS_PENDING = "pending"
        private const val STATUS_IN_REVIEW = "in_review"
        private const val STATUS_COMPLETED = "completed"
        private const val STATUS_REWORK = "rework"
        private const val DRAFT_STATUS_SUBMITTING = "SUBMITTING"
        private const val OP_PROOF_UPLOAD = "PROOF_UPLOAD"
        private const val OP_WORKFLOW_ACTION_COMPLETE = "WORKFLOW_ACTION_COMPLETE"
        private const val SUBMISSION_ITEM_CLOCK_TOLERANCE_MS = 5_000L
        private const val ACTION_KEY_TAG_THE_KID = "tag_the_kid"
        private const val ACTION_KEY_DEATH_VIDEO = "death_video"
        private const val ACTION_KEY_POST_MORTEM_VIDEO = "post_mortem_video"

        private const val TEMPLATE_BIRTH = "Birth"
        private const val TEMPLATE_DEATH = "Death"
        private const val TAG_QUESTION = "Question"
        private const val TAG_NUMBER = "Enter kg"
        private const val TAG_QUESTION_SELECT = "Pick a value"
        private const val TAG_ACTION = "Do & confirm"
        private const val TAG_APPROVAL = "Approval"
        private const val GLYPH_QUESTION = "?"
        private const val GLYPH_SELECT = "≡"
        private const val GLYPH_ACTION = "▣"
        private const val GLYPH_APPROVAL = "⚖"
        private const val GLYPH_DONE = "✓"
        private const val LABEL_DONE = "Done"
        private const val LABEL_IN_REVIEW = "In review"
        private const val LABEL_REWORK = "Rework"
        private const val LABEL_BLOCKED = "Blocked"
        private const val LABEL_SCHEDULED = "Scheduled"
        private const val ANSWER_YES_VALUE = "yes"
        private const val ANSWER_YES_LABEL = "Yes"
        private const val ANSWER_NO_VALUE = "no"
        private const val ANSWER_NO_LABEL = "No"

        private const val SUBMITTED_MESSAGE = "Submitted. Both videos are saved to the backend."
        private const val QUEUED_MESSAGE = "Saved on this phone. It will sync automatically."
        private const val VIDEO_QUEUED_MESSAGE =
            "Video saved on this phone. It will upload and submit automatically."
    }
}

/**
 * Whether a row is blocked FOR THE OPERATOR, which is not always the backend's `blocked` flag.
 *
 * Death is the exception, and only for `previous_action`. Its two videos are held as local drafts
 * and uploaded together at Submit, so until then the backend has seen no completion and correctly
 * reports the second video blocked behind the first (`tasks/domain.OperatorActionBlocked`).
 * Honouring that verbatim leaves the operator with a recorded first video and no next control —
 * the reported field failure. [predecessorsReady] is already draft-aware, so it is the local answer
 * to the same question the backend answered without the drafts.
 *
 * Every other reason still blocks, and every other module blocks on `previous_action` too: Birth
 * uploads each video on capture, so its block is live backend truth about work genuinely not done.
 */
internal fun workflowBlockedForOperator(
    action: WorkflowActionDto,
    isDeath: Boolean,
    predecessorsReady: Boolean,
): Boolean = action.blocked &&
    !(isDeath && action.blockedReason == WORKFLOW_BLOCKED_PREVIOUS_ACTION && predecessorsReady)

internal fun canRecordWorkflowVideo(
    action: WorkflowActionDto,
    blocked: Boolean,
    draftsSubmitting: Boolean,
    predecessorsReady: Boolean,
): Boolean = action.actionType == "action" &&
    action.requiresVideo &&
    !operatorFinishedWorkflowStatus(action.status) &&
    !blocked &&
    !draftsSubmitting &&
    predecessorsReady

internal fun operatorVisibleWorkflowActions(actions: List<WorkflowActionDto>): List<WorkflowActionDto> =
    actions.filter { it.actionType != "approval" }

internal fun workflowPredecessorsReady(
    action: WorkflowActionDto,
    actions: List<WorkflowActionDto>,
    isFinished: (WorkflowActionDto) -> Boolean,
): Boolean {
    if (action.actionKey == WORKFLOW_ACTION_KEY_TAG_THE_KID) {
        return actions
            .filter { it.actionId != action.actionId && it.actionType != "approval" }
            .all(isFinished)
    }
    if (action.section == WORKFLOW_SECTION_COLOSTRUM) {
        val firstColostrum = actions.firstOrNull { it.actionKey == WORKFLOW_ACTION_KEY_FIRST_COLOSTRUM }
            ?: return false
        if (!isFinished(firstColostrum)) return false
    }
    return actions
        .filter { it.section == action.section && it.seq < action.seq }
        .all(isFinished)
}

internal fun workflowDisplayOrder(action: WorkflowActionDto): Int =
    if (action.actionKey == WORKFLOW_ACTION_KEY_TAG_THE_KID) Int.MAX_VALUE else action.seq

internal fun workflowTagNeedsPermanentIdentifier(answerValue: String?): Boolean =
    answerValue.isNullOrBlank()

internal fun workflowAccessLabel(blockedReason: String?, due: Instant?, now: Instant): String? {
    if (blockedReason != "not_yet_due" || due == null || !now.isBefore(due)) return null
    return "Available ${due.atZone(WORKFLOW_IST).format(WORKFLOW_DATE_TIME_FORMAT)}"
}

/**
 * The farm-readable reason a row cannot be started yet, shown under the row when there is no
 * completion attribution to show instead.
 *
 * `previous_action` is the one that matters here. On Birth the blocking step is visible right above,
 * so the chip alone reads fine; on the Colostrum lens it is NOT on screen at all — 1st Colostrum
 * sits behind kid-clean, iodine dipping, front teeth and suck reflex, which live in Birth. Without
 * this line the operator sees "Blocked" with nothing to act on, taps anyway, and gets a rejection
 * (docs/decisions/colostrum-milk-module.md).
 */
internal fun workflowBlockedNote(blocked: Boolean, blockedReason: String?): String {
    if (!blocked) return ""
    return when (blockedReason) {
        WORKFLOW_BLOCKED_PREVIOUS_ACTION -> "Finish the earlier birth steps for this kid first."
        "signoff" -> "Waiting for the videos this step signs off."
        else -> ""
    }
}

internal fun workflowNumericAnswerUnit(action: WorkflowActionDto): String? =
    if (action.actionKey == "take_weight" && action.actionType == "question") "kg" else null

internal fun workflowNumericAnswerValid(value: String): Boolean {
    val trimmed = value.trim()
    if (trimmed.isEmpty() || trimmed.count { it == '.' } > 1 || trimmed.any { !it.isDigit() && it != '.' }) {
        return false
    }
    return trimmed.toDoubleOrNull()?.let { it > 0.0 && it.isFinite() } == true
}

internal fun operatorFinishedWorkflowStatus(status: String): Boolean =
    status == "completed" || status == "in_review"

internal fun workflowProofUploadKey(actionId: String, capturedStartedAtMs: Long): String =
    "wf-proof:$actionId:$capturedStartedAtMs"

// The fieldKey vocabulary stays backend-declared per workflow definition (actionId is
// open-ended and server-defined), but the IDENTITY wrapping it is now canonical: see
// workflowEvidenceSlot() below, which builds an EvidenceSlot(ProofIdentity(flow =
// ProofFlow.WORKFLOW_DETAIL, taskId = workflowId), fieldKey = workflowProofFieldKey(actionId)).
// taskId/fieldKey resolve to the SAME strings this function already produced, so shared
// retirement/recovery/referee machinery now keys these rows like every other flow with no
// change to any on-disk value.
internal fun workflowProofFieldKey(actionId: String): String =
    "workflow_${actionId}_video"

private fun workflowGoatProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(captureSource = captureSource)

internal fun workflowVideoCompletionKey(actionId: String, proofOutboxItemId: String): String =
    "wf-complete:$actionId:$proofOutboxItemId"

internal fun workflowVideoAnswerKey(actionId: String, proofOutboxItemId: String): String =
    "wf-answer:$actionId:$proofOutboxItemId"

private val WORKFLOW_IST: ZoneId = ZoneId.of("Asia/Kolkata")
private val WORKFLOW_DATE_TIME_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM · HH:mm")
private const val WORKFLOW_SECTION_COLOSTRUM = "colostrum_session"
internal const val WORKFLOW_BLOCKED_PREVIOUS_ACTION = "previous_action"
private const val WORKFLOW_ACTION_KEY_FIRST_COLOSTRUM = "first_colostrum"
private const val WORKFLOW_ACTION_KEY_TAG_THE_KID = "tag_the_kid"
