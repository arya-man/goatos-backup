package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.ProofPreviewActionTrace
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.data.WorkflowVideoDraft
import sg.mesha.goatos.core.data.capture.buildWorkflowEvidenceSlot
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.WorkflowProofOutboxRef
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.feature.counts.WorkflowActionSection
import sg.mesha.goatos.feature.counts.WorkflowActionUi
import sg.mesha.goatos.feature.counts.WorkflowAnswerOptionUi
import sg.mesha.goatos.feature.counts.WorkflowDetailEvent
import sg.mesha.goatos.feature.counts.WorkflowProofSavedKind
import sg.mesha.goatos.feature.counts.WorkflowDetailUiState
import sg.mesha.goatos.feature.counts.WorkflowProofUi
import sg.mesha.goatos.feature.counts.WorkflowStatusTone
import java.time.Duration
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.io.File
import java.net.URI
import java.util.UUID
import sg.mesha.goatos.core.data.workflowDeathDraftFieldKey
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
    private val savedStateHandle: SavedStateHandle,
    private val photoCaptureSource: PhotoCaptureSource,
    private val countsRepository: CountsRepository,
) : ViewModel() {

    /** The pen catalog for the `record_pen` step (same Room-cached source the Add-birth form uses). */
    private val penCatalog = MutableStateFlow<List<CountsDestinationParkDto>>(emptyList())

    /**
     * SOP-DRIVEN MULTI-PROOF (docs/decisions/sop-driven-herd-operations.md). A step authored with
     * "2 videos + 1 photo" is captured one proof at a time: every capture is enqueued as its own
     * PROOF_UPLOAD immediately (durable), and its outbox id + kind is remembered here per action
     * until the step's minimums are met, when ONE completion / answer is enqueued carrying all of
     * them by reference. The pending list is mirrored to SavedStateHandle so a process death
     * before the completion write does not orphan already queued proof uploads.
     */
    private val pendingProofs = MutableStateFlow(readPendingProofs())
    private val pendingAnswers = readPendingAnswers().toMutableMap()
    private val submittedMultiProofKeys = mutableSetOf<String>()

    private val workflowId: String = savedStateHandle[ARG_WORKFLOW_ID] ?: ""

    private fun readPendingProofs(): Map<String, List<WorkflowProofOutboxRef>> =
        savedStateHandle.get<String>(KEY_PENDING_PROOFS)?.let { raw ->
            // exception:exempt corrupt restored proof refs are treated as absent; durable proof outbox rows remain the recovery source.
            runCatching { workflowJson.decodeFromString(pendingProofsSerializer, raw) }.getOrNull()
        }.orEmpty()

    private fun readPendingAnswers(): Map<String, String> =
        savedStateHandle.get<String>(KEY_PENDING_ANSWERS)?.let { raw ->
            // exception:exempt corrupt restored answers are treated as absent; incomplete actions stay visible instead of replaying bad state.
            runCatching { workflowJson.decodeFromString(pendingAnswersSerializer, raw) }.getOrNull()
        }.orEmpty()

    private fun rememberPendingProofs(next: Map<String, List<WorkflowProofOutboxRef>>) {
        pendingProofs.value = next
        savedStateHandle[KEY_PENDING_PROOFS] = workflowJson.encodeToString(pendingProofsSerializer, next)
    }

    private fun rememberPendingAnswer(actionId: String, value: String) {
        pendingAnswers[actionId] = value
        savedStateHandle[KEY_PENDING_ANSWERS] = workflowJson.encodeToString(pendingAnswersSerializer, pendingAnswers)
    }

    private fun clearPendingAction(actionId: String) {
        rememberPendingProofs(pendingProofs.value - actionId)
        pendingAnswers.remove(actionId)
        savedStateHandle[KEY_PENDING_ANSWERS] = workflowJson.encodeToString(pendingAnswersSerializer, pendingAnswers)
    }

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
        viewModelScope.launch {
            countsRepository.observeShiftingDestinations().collect { resource ->
                resource.data?.parks?.let { parks -> penCatalog.value = parks }
            }
        }
        viewModelScope.launch { countsRepository.refreshShiftingDestinations() }
        observeDurableMultiProofCaptures()
    }

    fun onEvent(event: WorkflowDetailEvent) {
        when (event) {
            WorkflowDetailEvent.Refresh -> refresh()
            is WorkflowDetailEvent.Answer -> answer(event.actionId, event.value)
            is WorkflowDetailEvent.Complete -> complete(event.actionId)
            is WorkflowDetailEvent.RecordVideo -> onRecordVideo(event.actionId)
            is WorkflowDetailEvent.TakePhoto -> captureProof(event.actionId, kind = PROOF_KIND_PHOTO)
            is WorkflowDetailEvent.ProofPreviewAction -> {
                val trace = ProofPreviewActionTrace.from(event.action)
                analytics.track(
                    AnalyticsEvents.WORKFLOW_PROOF_PREVIEW_ACTION,
                    mapOf(
                        AnalyticsEvents.Params.ITEM_ID to workflowId,
                        AnalyticsEvents.Params.PROOF_ID to "${event.actionId}:${event.proofRef}",
                        AnalyticsEvents.Params.KIND to event.mediaKind,
                        AnalyticsEvents.Params.ACTION to trace.action,
                        AnalyticsEvents.Params.OUTCOME to trace.outcome,
                        AnalyticsEvents.Params.REASON to trace.reason.orEmpty(),
                    ),
                )
            }
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
                combine(
                    repo.observeVideoDrafts(workflowId),
                    repo.observeStepDraftAnswers(workflowId),
                    savedStateHandle.getStateFlow(KEY_DEATH_SUBMITTED_WRITES, emptyList<String>()),
                ) { d, a, w -> Triple(d, a, w) },
                syncRepository.observeStatus(),
                pendingProofs,
                penCatalog,
            ) { detail, draftsAndAnswers, sync, _, _ -> Triple(detail, draftsAndAnswers, sync) }
                .collect { (detail, draftsAndAnswers, sync) ->
                if (detail != null) {
                    val (drafts, draftAnswers, submittedWrites) = draftsAndAnswers
                    // A death is being submitted when its video drafts are marked SUBMITTING -- or,
                    // for a death whose authored steps are ALL answers (no drafts to mark), when the
                    // Submit recorded the step writes it queued. The step writes are then matched
                    // by their exact outbox ids, not by a draft-derived clock.
                    val submitting = drafts.any { it.syncStatus == DRAFT_STATUS_SUBMITTING } || submittedWrites.isNotEmpty()
                    val earliestDraft = drafts.minOfOrNull { it.startedAtMs } ?: Long.MAX_VALUE
                    val submissionItems = sync.items.filter { item ->
                        item.groupKey == workflowId &&
                            (item.id in submittedWrites || item.createdAt >= earliestDraft - SUBMISSION_ITEM_CLOCK_TOLERANCE_MS) &&
                            (item.opType == OP_PROOF_UPLOAD || item.opType == OP_WORKFLOW_ACTION_COMPLETE || item.opType == OP_WORKFLOW_ACTION_ANSWER)
                    }
                    val uploadFailed = submitting && submissionItems.any { it.status == SyncItemStatus.FAILED }
                    val stepWrites = submissionItems.filter { it.opType == OP_WORKFLOW_ACTION_COMPLETE || it.opType == OP_WORKFLOW_ACTION_ANSWER }
                    val stepWritesSucceeded = (if (submittedWrites.isNotEmpty()) stepWrites.filter { it.id in submittedWrites } else stepWrites)
                        .count { it.status == SyncItemStatus.SUCCEEDED }
                    _state.update { detail.toUiState(it, drafts, draftAnswers, uploadFailed, submittedWrites.isNotEmpty()) }
                    // Every step the Submit sent must be server-confirmed before the death reads as
                    // submitted -- the count Submit recorded, or every operator step when a
                    // process death lost it (never fewer, so it cannot finish early).
                    val expectedStepWrites = submittedWrites.size.takeIf { it > 0 }
                        ?: savedStateHandle.get<Int>(KEY_DEATH_SUBMITTED_STEPS)
                        ?: operatorVisibleWorkflowActions(detail.actions).size
                    if (submitting && expectedStepWrites > 0 && stepWritesSucceeded >= expectedStepWrites && !finalizedDeathSubmission) {
                        finalizedDeathSubmission = true
                        operatorVisibleWorkflowActions(detail.actions)
                            .filterNot { operatorFinishedWorkflowStatus(it.status) }
                            .forEach { repo.markActionCompleted(workflowId, it.actionId, inReview = false) }
                        repo.clearVideoDrafts(workflowId)
                        repo.clearStepDraftAnswers(workflowId)
                        savedStateHandle.remove<Int>(KEY_DEATH_SUBMITTED_STEPS)
                        savedStateHandle[KEY_DEATH_SUBMITTED_WRITES] = emptyList<String>()
                        repo.refreshDetail(workflowId, lens, lensDate)
                        _state.update {
                            it.copy(
                                message = SUBMITTED_MESSAGE,
                                isErrorMessage = false,
                                // Every step is server-confirmed and the death now waits on an
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

    private fun observeDurableMultiProofCaptures() {
        viewModelScope.launch {
            combine(
                repo.observeDetail(workflowId, lens, lensDate),
                proofCaptureRepository.observeProofs(workflowId),
            ) { detail, proofs -> detail to proofs }
                .collect { (detail, proofs) ->
                    // Death sends every step from its one Submit; recovering its uploads here as
                    // auto-submits would send a multi-proof step a second time.
                    if (detail?.module == MODULE_DEATH) return@collect
                    // A live capture owns its own step write. Recovering its row here while the
                    // capture call is still returning queued the step a second time with the same
                    // proof appended twice under a different key (Realme E2E 2026-09-17): the server
                    // refused it 409 and the dead row held the workflow's lane. Recovery exists for a
                    // process death between capture and write, never for a capture in flight.
                    if (_state.value.isCapturingVideo) return@collect
                    val actions = detail?.actions.orEmpty()
                    val recovered = recoverMultiProofRefs(actions, proofs)
                    if (recovered.isNotEmpty()) {
                        val merged = pendingProofs.value + recovered
                        if (merged != pendingProofs.value) rememberPendingProofs(merged)
                    }
                    actions.forEach { dto ->
                        if (operatorFinishedWorkflowStatus(dto.status)) return@forEach
                        val action = _state.value.actions.firstOrNull { it.actionId == dto.actionId } ?: return@forEach
                        val captured = autoSubmittableProofRefs(dto, pendingProofs.value[action.actionId].orEmpty())
                        if (captured.isNotEmpty() && proofsSatisfied(action, captured)) {
                            if (captured != pendingProofs.value[action.actionId].orEmpty()) {
                                rememberPendingProofs(pendingProofs.value + (action.actionId to captured))
                            }
                            val needsAnswer = action.answerKind != "none" && action.canAnswer
                            val answerValue = pendingAnswers[action.actionId] ?: dto.answerValue
                            if (!needsAnswer || !answerValue.isNullOrBlank()) {
                                submitMultiProof(action.actionId, answerValue)
                            }
                        }
                    }
                }
        }
    }

    private fun recoverMultiProofRefs(
        actions: List<WorkflowActionDto>,
        proofs: List<ProofCaptureRow>,
    ): Map<String, List<WorkflowProofOutboxRef>> =
        actions
            .filter { it.proofMinPhotos > 0 || it.proofMinVideos > 1 }
            .filterNot { operatorFinishedWorkflowStatus(it.status) }
            .associate { action ->
                val prefix = workflowProofFieldKey(action.actionId) + "_"
                val backendProofRefs = action.proofRefs.mapNotNull { it.ref.takeIf(String::isNotBlank) }.toSet()
                action.actionId to proofs
                    .asSequence()
                    .filter { row ->
                        row.syncStatus != CaptureSyncStatus.FAILED &&
                            (!row.outboxItemId.isNullOrBlank() || !row.serverProofId.isNullOrBlank()) &&
                            row.fieldKey.startsWith(prefix) &&
                            (action.status != STATUS_REWORK || row.serverProofId.isNullOrBlank() || row.serverProofId !in backendProofRefs)
                    }
                    .sortedBy { it.capturedAtMs }
                    .mapNotNull { row ->
                        val kind = row.fieldKey.removePrefix(prefix).substringBefore("_")
                        if (kind == PROOF_KIND_PHOTO || kind == PROOF_KIND_VIDEO) {
                            WorkflowProofOutboxRef(
                                outboxItemId = row.outboxItemId.orEmpty(),
                                proofRef = row.serverProofId.orEmpty(),
                                kind = kind,
                            )
                        } else {
                            null
                        }
                    }
                    .toList()
            }
            .filterValues { it.isNotEmpty() }

    private fun autoSubmittableProofRefs(
        action: WorkflowActionDto,
        proofs: List<WorkflowProofOutboxRef>,
    ): List<WorkflowProofOutboxRef> {
        if (action.status != STATUS_REWORK) return proofs
        val backendProofRefs = action.proofRefs.mapNotNull { it.ref.takeIf(String::isNotBlank) }.toSet()
        return proofs.filter { it.proofRef.isBlank() || it.proofRef !in backendProofRefs }
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
        val action = _state.value.actions.firstOrNull { it.actionId == actionId }
        if (_state.value.isDeath && action != null) {
            // Death holds every step on the phone until its one Submit: the answer is a draft.
            viewModelScope.launch {
                repo.putStepDraftAnswer(workflowId, actionId, value)
                analytics.track(
                    AnalyticsEvents.WORKFLOW_DEATH_DRAFT_ANSWERED,
                    mapOf(AnalyticsEvents.Params.ITEM_ID to workflowId, AnalyticsEvents.Params.ACTION to actionId),
                )
            }
            // A one-video question still records its video through the answer, as a draft too.
            if (action.requiresVideo && !action.isMultiProof) captureAndComplete(actionId, answerValue = value)
            return
        }
        if (action != null && action.isMultiProof) {
            // The answer waits with the captures; the step submits once every proof is in.
            rememberPendingAnswer(actionId, value)
            if (proofsSatisfied(action, pendingProofs.value[actionId].orEmpty())) {
                submitMultiProof(actionId, value)
            } else {
                // The kept answer shows as chosen on the step while its proofs are still owed.
                _state.update { current ->
                    current.copy(
                        actions = current.actions.map { if (it.actionId == actionId) it.copy(answerValue = value) else it },
                        message = MULTI_PROOF_ANSWER_KEPT_MESSAGE,
                        proofSaved = null,
                        isErrorMessage = false,
                    )
                }
            }
            return
        }
        if (action?.requiresVideo == true) {
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
                    observeActionWrite(result.value, actionId)
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
                    observeActionWrite(result.value, actionId)
                    analytics.track(AnalyticsEvents.WORKFLOW_ACTION_COMPLETED)
                    _state.update { it.copy(message = QUEUED_MESSAGE, isErrorMessage = false) }
                }
                is AppResult.Err -> onWriteFailed("workflow_complete", result)
            }
        }
    }

    private fun observeActionWrite(itemId: String, actionId: String) = viewModelScope.launch {
        val item = syncRepository.observeItem(itemId).first { candidate ->
            candidate?.status == SyncItemStatus.SUCCEEDED || candidate?.isTerminalFailure == true
        } ?: return@launch
        if (item.isTerminalFailure) {
            repo.rollbackAction(workflowId, actionId)
            _state.update {
                it.copy(
                    message = item.lastError ?: ACTION_FAILED_MESSAGE,
                    isErrorMessage = true,
                )
            }
        } else {
            repo.refreshDetail(workflowId, lens, lensDate)
        }
    }

    private fun onRecordVideo(actionId: String) {
        val action = _state.value.actions.firstOrNull { it.actionId == actionId }
        if (action != null && action.isMultiProof) {
            captureProof(actionId, kind = PROOF_KIND_VIDEO)
        } else {
            captureAndComplete(actionId)
        }
    }

    private val WorkflowActionUi.isMultiProof: Boolean
        get() = proofMinPhotos > 0 || proofMinVideos > 1

    private fun proofsSatisfied(action: WorkflowActionUi, captured: List<WorkflowProofOutboxRef>): Boolean {
        val videos = captured.count { it.kind == PROOF_KIND_VIDEO }
        val photos = captured.count { it.kind == PROOF_KIND_PHOTO }
        return videos >= maxOf(action.proofMinVideos, if (action.requiresVideo && action.proofMinVideos == 0) 1 else 0) &&
            photos >= action.proofMinPhotos
    }

    /**
     * Captures ONE proof (video or photo) for a multi-proof step, queues its upload at once, and
     * submits the step when the authored minimums are met. A question step also waits for its
     * answer (answer() keeps it in [pendingAnswers]).
     */
    private fun captureProof(actionId: String, kind: String) {
        val current = _state.value
        if (current.isCapturingVideo) return
        val action = current.actions.firstOrNull { it.actionId == actionId } ?: return
        if (current.isDeath) {
            captureDeathDraft(action, kind)
            return
        }
        val goatId = current.subjectGoatId
        _state.update { it.copy(isCapturingVideo = true, message = null, proofSaved = null) }
        viewModelScope.launch {
            val captured: Pair<String, Triple<String, Long, Long>>? = try {
                if (kind == PROOF_KIND_PHOTO) {
                    photoCaptureSource.capturePhoto(
                        PhotoCaptureContext(title = action.title, instruction = action.detail, prompt = ProofCapturePrompt.BIRTH),
                    )?.let { it.localUri to Triple(it.mimeType, it.capturedAtMs, it.capturedAtMs) }
                } else {
                    proofCaptureSource.captureVideo(
                        ProofCaptureContext(
                            title = workflowProofCaption(current, action),
                            primaryTag = current.subjectLocationDisplay.ifBlank { current.displayId },
                            secondaryTag = current.displayId.takeIf { it.isNotBlank() },
                            workLabel = action.title,
                            prompt = ProofCapturePrompt.BIRTH,
                            headerTitle = action.title,
                        ),
                    )?.let { it.localUri to Triple(it.mimeType, it.startedAtMs, it.endedAtMs) }
                }
            } catch (error: Exception) {
                crashReporter.recordException(error, "workflow $kind capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingVideo = false) }
                return@launch
            }
            val (localUri, meta) = captured
            val (mimeType, startedAtMs, endedAtMs) = meta
            val ordinal = pendingProofs.value[actionId].orEmpty().size + 1
            val slot = EvidenceSlot(
                identity = workflowEvidenceSlot(actionId, goatId).identity,
                fieldKey = workflowProofFieldKey(actionId) + "_" + kind + "_" + ordinal,
            )
            val proofResult = proofCaptureRepository.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.GOAT,
                subjectId = goatId,
                localUri = localUri,
                mimeType = mimeType,
                caption = workflowProofCaption(current, action),
                scopeType = "goat",
                scopeId = goatId,
                capturedStartMs = startedAtMs,
                capturedEndMs = endedAtMs,
                capturedByPrincipalId = null,
                proofPolicy = workflowGoatProofPolicy("in_app_camera"),
                awaitUploadEnqueue = true,
                uploadGroupKey = workflowId,
            )
            val proofItemId = when (proofResult) {
                is AppResult.Ok -> proofResult.value.outboxItemId
                is AppResult.Err -> {
                    _state.update { it.copy(isCapturingVideo = false) }
                    onWriteFailed("workflow_$kind", proofResult)
                    return@launch
                }
            } ?: run {
                _state.update { it.copy(isCapturingVideo = false, message = "Upload could not be queued.", isErrorMessage = true) }
                return@launch
            }
            analytics.track(
                workflowCaptureEvent(kind),
                mapOf(AnalyticsEvents.Params.ITEM_ID to workflowId, AnalyticsEvents.Params.ACTION to "$actionId:$kind"),
            )
            val queued = distinctWorkflowProofRefs(
                pendingProofs.value[actionId].orEmpty() + WorkflowProofOutboxRef(outboxItemId = proofItemId, kind = kind),
            )
            rememberPendingProofs(pendingProofs.value + (actionId to queued))
            val latest = _state.value.actions.firstOrNull { it.actionId == actionId } ?: action
            val needsAnswer = latest.answerKind != "none" && latest.canAnswer
            if (proofsSatisfied(latest, queued) && (!needsAnswer || pendingAnswers[actionId] != null)) {
                // The capture is over; the step write is durable in the outbox from here.
                _state.update { it.copy(isCapturingVideo = false) }
                submitMultiProof(actionId, pendingAnswers[actionId])
            } else {
                _state.update { it.copy(isCapturingVideo = false, message = MULTI_PROOF_QUEUED_MESSAGE, isErrorMessage = false) }
            }
        }
    }

    private fun submitMultiProof(actionId: String, answerValue: String?) {
        // One capture is one proof: never send the same queued proof twice, and key the write on
        // the distinct set so the same proofs always produce the same command.
        val proofs = distinctWorkflowProofRefs(pendingProofs.value[actionId].orEmpty())
        if (proofs.isEmpty()) return
        val fingerprint = proofs.joinToString(",") { it.outboxItemId }.hashCode().toUInt().toString(16)
        val submittedKey = "$actionId:$fingerprint:${answerValue.orEmpty()}"
        if (!submittedMultiProofKeys.add(submittedKey)) return
        viewModelScope.launch {
            val writeResult = if (answerValue != null) {
                syncRepository.enqueueWorkflowActionAnswer(
                    groupKey = workflowId,
                    idempotencyKey = "wf-answer:$actionId:$fingerprint",
                    workflowId = workflowId,
                    actionId = actionId,
                    answerValue = answerValue,
                    proofOutboxItems = proofs,
                )
            } else {
                syncRepository.enqueueWorkflowActionComplete(
                    groupKey = workflowId,
                    idempotencyKey = "wf-complete:$actionId:$fingerprint",
                    workflowId = workflowId,
                    actionId = actionId,
                    proofOutboxItems = proofs,
                )
            }
            when (writeResult) {
                is AppResult.Ok -> {
                    observeActionWrite(writeResult.value, actionId)
                    if (answerValue != null) {
                        repo.markActionAnswered(workflowId, actionId, answerValue)
                        analytics.track(AnalyticsEvents.WORKFLOW_ACTION_ANSWERED)
                    } else {
                        repo.markActionCompleted(workflowId, actionId, inReview = false)
                        analytics.track(AnalyticsEvents.WORKFLOW_ACTION_COMPLETED)
                    }
                    clearPendingAction(actionId)
                    val savedKind = workflowProofSavedKind(proofs.map { it.kind })
                    _state.update { it.copy(isCapturingVideo = false, message = null, proofSaved = savedKind, isErrorMessage = false) }
                }
                is AppResult.Err -> {
                    submittedMultiProofKeys.remove(submittedKey)
                    _state.update { it.copy(isCapturingVideo = false) }
                    onWriteFailed("workflow_multi_proof", writeResult)
                }
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
        val prompt = workflowCapturePrompt(current.isDeath, action)
        _state.update { it.copy(isCapturingVideo = true, message = null, proofSaved = null) }
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
                    observeActionWrite(writeResult.value, actionId)
                    if (answerValue != null) {
                        repo.markActionAnswered(workflowId, actionId, answerValue)
                        analytics.track(AnalyticsEvents.WORKFLOW_ACTION_ANSWERED)
                    } else {
                        repo.markActionCompleted(workflowId, actionId, inReview = false)
                        analytics.track(AnalyticsEvents.WORKFLOW_ACTION_COMPLETED)
                    }
                    _state.update {
                        it.copy(isCapturingVideo = false, message = null, proofSaved = WorkflowProofSavedKind.VIDEO, isErrorMessage = false)
                    }
                }
                is AppResult.Err -> {
                    _state.update { it.copy(isCapturingVideo = false) }
                    onWriteFailed("workflow_action_with_video", writeResult)
                }
            }
        }
    }

    /**
     * A death step's photo or video from the live camera, held as a local DRAFT until Submit. The
     * step's first video keeps the bare action-id slot; each further proof of the step gets its own
     * slot, and a capture once the step's authored count is reached replaces the last one.
     */
    private fun captureDeathDraft(action: WorkflowActionUi, kind: String) {
        val current = _state.value
        val goatId = current.subjectGoatId
        _state.update { it.copy(isCapturingVideo = true, message = null, proofSaved = null) }
        viewModelScope.launch {
            val captured: WorkflowVideoDraft? = try {
                if (kind == PROOF_KIND_PHOTO) {
                    photoCaptureSource.capturePhoto(
                        PhotoCaptureContext(title = action.title, instruction = action.detail, prompt = ProofCapturePrompt.DEATH),
                    )?.let { photo ->
                        WorkflowVideoDraft(
                            id = UUID.randomUUID().toString(), workflowId = workflowId, actionId = action.actionId,
                            subjectGoatId = goatId, localUri = photo.localUri, mimeType = photo.mimeType,
                            startedAtMs = photo.capturedAtMs, endedAtMs = photo.capturedAtMs, captureSource = photo.captureSource,
                        )
                    }
                } else {
                    proofCaptureSource.captureVideo(
                        ProofCaptureContext(
                            title = workflowProofCaption(current, action),
                            primaryTag = current.subjectLocationDisplay.ifBlank { current.displayId },
                            secondaryTag = current.displayId.takeIf { it.isNotBlank() },
                            workLabel = action.title,
                            prompt = workflowCapturePrompt(true, action),
                            headerTitle = action.title,
                        ),
                    )?.let { video ->
                        WorkflowVideoDraft(
                            id = UUID.randomUUID().toString(), workflowId = workflowId, actionId = action.actionId,
                            subjectGoatId = goatId, localUri = video.localUri, mimeType = video.mimeType,
                            startedAtMs = video.startedAtMs, endedAtMs = video.endedAtMs, captureSource = video.captureSource,
                        )
                    }
                }
            } catch (error: Exception) {
                crashReporter.recordException(error, "workflow death $kind capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingVideo = false) }
                return@launch
            }
            val sameKind = repo.listVideoDrafts(workflowId).count { it.actionId == action.actionId && it.isPhoto == (kind == PROOF_KIND_PHOTO) }
            val needed = if (kind == PROOF_KIND_PHOTO) action.proofMinPhotos else action.proofMinVideos
            val n = if (sameKind < needed) sameKind + 1 else maxOf(needed, 1)
            val previous = repo.replaceVideoDraft(captured.copy(fieldKey = workflowDeathDraftFieldKey(action.actionId, kind, n)))
            previous?.localUri?.let(::deletePrivateDraftFile)
            analytics.track(
                workflowCaptureEvent(kind),
                mapOf(AnalyticsEvents.Params.ITEM_ID to workflowId, AnalyticsEvents.Params.ACTION to "death_draft"),
            )
            _state.update {
                it.copy(isCapturingVideo = false, message = DEATH_DRAFT_SAVED_MESSAGE, isErrorMessage = false)
            }
        }
    }

    /**
     * The death's ONE Submit: for every step the operator still owes, queue each drafted proof's
     * upload, then the step's answer or completion carrying those proofs by reference. A step with a
     * single video keeps the exact write it always had (one proof item id on a completion).
     */
    private fun submitDeath() {
        val current = _state.value
        if (!current.deathSubmissionEnabled) return
        _state.update { it.copy(isSubmittingDeath = true, message = null, proofSaved = null) }
        viewModelScope.launch {
            val drafts = repo.listVideoDrafts(workflowId)
            val answers = repo.observeStepDraftAnswers(workflowId).first()
            val detail = repo.observeDetail(workflowId, lens, lensDate).first()
            val steps = operatorVisibleWorkflowActions(detail?.actions.orEmpty())
                .filterNot { operatorFinishedWorkflowStatus(it.status) }
                .sortedBy { it.seq }
            if (steps.isEmpty() || steps.any { !deathStepSatisfied(it, drafts, answers) }) {
                _state.update { it.copy(isSubmittingDeath = false, message = DEATH_INCOMPLETE_MESSAGE, isErrorMessage = true) }
                return@launch
            }
            val stepWriteIds = mutableListOf<String>() // mobile-guard:ignore: bounded by the death's authored steps
            for (step in steps) {
                val ui = _state.value.actions.firstOrNull { it.actionId == step.actionId }
                val stepDrafts = drafts.filter { it.actionId == step.actionId }
                    .sortedWith(compareBy<WorkflowVideoDraft> { it.isPhoto }.thenBy { it.fieldKey })
                val refs = mutableListOf<WorkflowProofOutboxRef>() // mobile-guard:ignore: bounded by one step's authored proofs
                for (draft in stepDrafts) {
                    val kind = if (draft.isPhoto) PROOF_KIND_PHOTO else PROOF_KIND_VIDEO
                    val slot = if (draft.fieldKey == step.actionId) {
                        workflowEvidenceSlot(step.actionId, draft.subjectGoatId)
                    } else {
                        EvidenceSlot(
                            identity = workflowEvidenceSlot(step.actionId, draft.subjectGoatId).identity,
                            fieldKey = workflowProofFieldKey(step.actionId) + "_" + kind + "_" + draft.fieldKey.substringAfterLast('|'),
                        )
                    }
                    val proof = proofCaptureRepository.captureReplacingLatest(
                        slot = slot,
                        subject = ProofSubject.GOAT,
                        subjectId = draft.subjectGoatId,
                        localUri = draft.localUri,
                        mimeType = draft.mimeType,
                        caption = workflowProofCaption(current, ui),
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
                        _state.update { it.copy(isSubmittingDeath = false, message = "Upload could not be queued.", isErrorMessage = true) }
                        return@launch
                    }
                    refs += WorkflowProofOutboxRef(outboxItemId = proofId, kind = kind)
                }
                val fingerprint = refs.joinToString(",") { it.outboxItemId }.hashCode().toUInt().toString(16)
                val answer = answers[step.actionId]?.takeIf { deathStepNeedsAnswer(step) }
                val write = when {
                    answer != null -> syncRepository.enqueueWorkflowActionAnswer(
                        groupKey = workflowId,
                        idempotencyKey = if (refs.isEmpty()) "wf-answer:${step.actionId}" else "wf-answer:${step.actionId}:$fingerprint",
                        workflowId = workflowId,
                        actionId = step.actionId,
                        answerValue = answer,
                        proofOutboxItems = refs,
                    )
                    refs.size == 1 && refs.single().kind == PROOF_KIND_VIDEO -> syncRepository.enqueueWorkflowActionComplete(
                        groupKey = workflowId,
                        idempotencyKey = workflowVideoCompletionKey(step.actionId, refs.single().outboxItemId),
                        workflowId = workflowId,
                        actionId = step.actionId,
                        proofOutboxItemId = refs.single().outboxItemId,
                    )
                    refs.isEmpty() -> syncRepository.enqueueWorkflowActionComplete(
                        groupKey = workflowId,
                        idempotencyKey = "wf-complete:${step.actionId}",
                        workflowId = workflowId,
                        actionId = step.actionId,
                    )
                    else -> syncRepository.enqueueWorkflowActionComplete(
                        groupKey = workflowId,
                        idempotencyKey = "wf-complete:${step.actionId}:$fingerprint",
                        workflowId = workflowId,
                        actionId = step.actionId,
                        proofOutboxItems = refs,
                    )
                }
                when (write) {
                    is AppResult.Err -> {
                        _state.update { it.copy(isSubmittingDeath = false) }
                        onWriteFailed("workflow_complete_submit", write)
                        return@launch
                    }
                    is AppResult.Ok -> stepWriteIds += write.value
                }
            }
            savedStateHandle[KEY_DEATH_SUBMITTED_STEPS] = steps.size
            savedStateHandle[KEY_DEATH_SUBMITTED_WRITES] = ArrayList(stepWriteIds)
            repo.markVideoDraftsSubmitting(workflowId)
            analytics.track(AnalyticsEvents.WORKFLOW_ACTION_COMPLETED)
            _state.update { it.copy(isSubmittingDeath = false, message = DEATH_UPLOADING_MESSAGE, isErrorMessage = false) }
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
        draftAnswers: Map<String, String>,
        deathUploadFailed: Boolean,
        deathStepWritesQueued: Boolean = false,
    ): WorkflowDetailUiState {
        val now = Instant.now()
        val mainActions = operatorVisibleWorkflowActions(actions)
        // Death is the one TWO-DRAFT flow: neither video leaves the phone until Submit, so the
        // backend correctly reports both actions `pending` and the second one blocked behind its
        // predecessor. Rendering that verbatim strands the operator after the first recording —
        // pre-submit, a durable draft IS their finished work for that row.
        val isDeathModule = module == MODULE_DEATH
        // A death step is recorded once its drafts (and draft answer) cover what the SOP asks of it.
        val draftedActionIds = if (isDeathModule) {
            mainActions.filter { deathStepSatisfied(it, drafts, draftAnswers) }.map { it.actionId }.toSet()
        } else {
            emptySet()
        }
        // An answer-only death has no drafts to mark: its queued step writes are the in-flight signal.
        val draftsSubmitting = drafts.any { it.syncStatus == DRAFT_STATUS_SUBMITTING } || deathStepWritesQueued
        return current.copy(
            loading = false,
            notFound = false,
            isDeath = module == MODULE_DEATH,
            // Death (like the birth-mother header) headlines the physical RFID the operator can
            // actually read on the animal; the passport id is only a fallback when no tag exists.
            displayId = if (templateKey == TEMPLATE_KEY_BIRTH_MOTHER || module == MODULE_DEATH) {
                subject.tag.ifBlank { subject.displayId }
            } else {
                subject.displayId.ifBlank { subject.tag }
            },
            roleLabel = subject.roleLabel,
            templateLine = listOf(
                templateLabel.ifBlank { if (module == MODULE_DEATH) TEMPLATE_DEATH else TEMPLATE_BIRTH },
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
                val stepDrafts = if (isDeathModule) drafts.filter { it.actionId == action.actionId } else emptyList()
                val hasDraft = stepDrafts.isNotEmpty()
                val locallyRecorded = action.actionId in draftedActionIds
                val predecessorsReady = workflowPredecessorsReady(action, mainActions) { previous ->
                    operatorFinishedWorkflowStatus(previous.status) || previous.actionId in draftedActionIds
                }
                val blocked = workflowBlockedForOperator(action, isDeathModule, predecessorsReady)
                val ui = action.toActionUi(now, blocked, locallyRecorded, this.parkLabel, module)
                // A one-video step keeps its legacy control (a question records its video through
                // Yes/No); a SOP step with photos or several videos captures each proof explicitly.
                val multiProof = ui.proofMinPhotos > 0 || ui.proofMinVideos > 1
                val base = ui.copy(
                    hasVideoDraft = hasDraft,
                    canRecordVideo = if (multiProof) ui.canRecordVideo && !draftsSubmitting else canRecordWorkflowVideo(action, blocked, draftsSubmitting),
                )
                if (!isDeathModule && multiProof && action.actionId in pendingAnswers) {
                    // A multi-proof step's answer is kept on the phone until its proofs are in.
                    base.copy(answerValue = pendingAnswers[action.actionId])
                } else if (isDeathModule) {
                    base.copy(
                        proofVideosCaptured = stepDrafts.count { !it.isPhoto },
                        proofPhotosCaptured = stepDrafts.count { it.isPhoto },
                        canTakePhoto = base.canTakePhoto && !draftsSubmitting,
                        canAnswer = base.canAnswer && !draftsSubmitting,
                        answerValue = draftAnswers[action.actionId] ?: base.answerValue,
                    )
                } else {
                    base
                }
            }.sortedBy { it.sectionOrder() },
            subjectGoatId = subject.goatId,
            subjectGoatRowVersion = subject.rowVersion,
            subjectTemporaryIdentifier = subject.tag,
            subjectLocationDisplay = listOf(parkLabel, shedLabel).filter { it.isNotBlank() }.joinToString(" / "),
            deathStepsReady = isDeathModule && mainActions.any { !operatorFinishedWorkflowStatus(it.status) } &&
                mainActions.all { operatorFinishedWorkflowStatus(it.status) || it.actionId in draftedActionIds },
            deathStepsMissing = if (isDeathModule) {
                mainActions.sortedBy(::workflowDisplayOrder)
                    .filterNot { operatorFinishedWorkflowStatus(it.status) || it.actionId in draftedActionIds }
                    .map { it.title }
                    .filter { it.isNotBlank() }
            } else {
                emptyList()
            },
            deathDraftsSubmitting = draftsSubmitting,
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
        parkLabelForPens: String = "",
        moduleForCopy: String = WORKFLOW_MODULE_BIRTH,
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
        val answerKind = answerType.ifBlank {
            when {
                actionType == TYPE_QUESTION_SELECT -> "select"
                numericAnswerUnit != null -> "number"
                actionType == TYPE_QUESTION -> "yes_no"
                else -> "none"
            }
        }
        val isRecordPen = taskType == "record_pen" || (taskType.isBlank() && actionKey == "record_shed")
        val penOptions = if (isRecordPen) {
            penCatalog.value.firstOrNull { it.name == parkLabelForPens }?.sheds.orEmpty().map { shed ->
                WorkflowAnswerOptionUi(
                    value = shed.shedId + "|" + shed.partitionLabel.orEmpty(),
                    label = shed.operationalLocationDisplay.ifBlank { shed.name },
                )
            }
        } else emptyList()
        val minVideos = if (proofMinVideos == 0 && requiresVideo) 1 else proofMinVideos
        val captured = pendingProofs.value[actionId].orEmpty()
        val uploaded = proofRefs.filter { it.ref.isNotBlank() }.map { item ->
            WorkflowProofUi(ref = item.ref, path = workflowProofDownloadUrl(item.ref), kind = item.kind)
        }.ifEmpty {
            // A row written before proof_refs existed still carries its one video in proof_ref.
            proofRef?.takeIf { it.isNotBlank() }?.let { listOf(WorkflowProofUi(ref = it, path = workflowProofDownloadUrl(it), kind = "video")) }.orEmpty()
        }
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
            options = when {
                // Record pen: the operator picks a pen of the kid's park; the answer is the
                // "<shed_id>|<partition_label>" key the backend's placement resolves.
                isRecordPen -> penOptions
                // The backend's own bands, verbatim: the band string is both value and label.
                actionType == TYPE_QUESTION_SELECT -> options.map { WorkflowAnswerOptionUi(value = it, label = it) }
                answerKind == "yes_no" -> listOf(
                    WorkflowAnswerOptionUi(ANSWER_YES_VALUE, ANSWER_YES_LABEL),
                    WorkflowAnswerOptionUi(ANSWER_NO_VALUE, ANSWER_NO_LABEL),
                )
                else -> emptyList()
            },
            numericAnswerUnit = numericAnswerUnit ?: if (answerKind == "number") "" else null,
            statusLabel = statusLabel,
            statusTone = statusTone,
            section = section,
            canAnswer = actionable && isQuestion && !opensPromote,
            canComplete = actionable && actionType == TYPE_ACTION && minVideos == 0 && proofMinPhotos == 0 && !opensPromote,
            canRecordVideo = actionable && minVideos > 0 && captured.count { it.kind == PROOF_KIND_VIDEO } < minVideos,
            canTakePhoto = actionable && proofMinPhotos > 0 && captured.count { it.kind == PROOF_KIND_PHOTO } < proofMinPhotos && !opensPromote,
            answerKind = if (isRecordPen) "select" else answerKind,
            proofMinVideos = minVideos,
            proofMinPhotos = proofMinPhotos,
            proofVideosCaptured = captured.count { it.kind == PROOF_KIND_VIDEO },
            proofPhotosCaptured = captured.count { it.kind == PROOF_KIND_PHOTO },
            uploadedProofs = if (status == STATUS_COMPLETED || status == STATUS_IN_REVIEW || status == STATUS_REWORK) uploaded else emptyList(),
            opensPromote = opensPromote && actionable,
            // A blocked row must say WHY. On the Colostrum lens the prerequisite is not even on
            // screen (1st Colostrum waits on four birth steps that live in Birth), so a bare
            // "Blocked" chip is a dead end for the person holding the phone.
            // A step sent back carries the verifier's reason so the operator knows what to re-shoot.
            footer = if (status == STATUS_REWORK && reworkReason.isNotBlank()) reworkReason
            else completedByLabel.orEmpty().ifBlank { workflowBlockedNote(blocked, blockedReason, moduleForCopy) },
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
        private val DATE_TIME_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("dd/MM/yyyy · HH:mm")
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
        private const val PROOF_KIND_VIDEO = "video"
        private const val PROOF_KIND_PHOTO = "photo"
        private const val MULTI_PROOF_QUEUED_MESSAGE = "Saved. Record the rest to finish this step."
        private const val MULTI_PROOF_ANSWER_KEPT_MESSAGE = "Answer kept. Record the proof to finish this step."
        private const val OP_PROOF_UPLOAD = "PROOF_UPLOAD"
        private const val OP_WORKFLOW_ACTION_COMPLETE = "WORKFLOW_ACTION_COMPLETE"
        private const val OP_WORKFLOW_ACTION_ANSWER = "WORKFLOW_ACTION_ANSWER"
        private const val KEY_DEATH_SUBMITTED_STEPS = "workflowDetail.deathSubmittedSteps"
        /** The outbox ids of the step writes the death's Submit queued (answer-only deaths hold no drafts). */
        private const val KEY_DEATH_SUBMITTED_WRITES = "workflowDetail.deathSubmittedWrites"
        private const val DEATH_DRAFT_SAVED_MESSAGE = "Saved as a draft. You can retake it before Submit."
        private const val DEATH_INCOMPLETE_MESSAGE = "Record every step before submitting."
        private const val DEATH_UPLOADING_MESSAGE = "Uploading the recorded proofs…"
        private const val SUBMISSION_ITEM_CLOCK_TOLERANCE_MS = 5_000L
        private const val ACTION_KEY_TAG_THE_KID = "tag_the_kid"

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

        private const val SUBMITTED_MESSAGE = "Submitted. The recorded proofs are saved."
        private const val QUEUED_MESSAGE = "Saved on this phone. It will sync automatically."
        private const val ACTION_FAILED_MESSAGE = "This action did not go through. Review it and try again."
    }
}

/** The step's queued proofs with each proof once: keyed by its upload outbox id, or by its server
 *  id when the outbox row is already gone. Capture order is kept. */
internal fun distinctWorkflowProofRefs(refs: List<WorkflowProofOutboxRef>): List<WorkflowProofOutboxRef> =
    refs.distinctBy { ref -> ref.outboxItemId.ifBlank { "ref:" + ref.proofRef.ifBlank { ref.hashCode().toString() } } }

/** The capture event for a proof of [kind]: a photo is reported as a photo, never as a video. */
internal fun workflowCaptureEvent(kind: String): String =
    if (kind == "photo") AnalyticsEvents.WORKFLOW_PHOTO_CAPTURED else AnalyticsEvents.WORKFLOW_VIDEO_CAPTURED

/** Names what a queued step carried: all photos, all videos, or a mix of both. */
internal fun workflowProofSavedKind(kinds: Collection<String>): WorkflowProofSavedKind = when {
    kinds.isNotEmpty() && kinds.all { it == "photo" } -> WorkflowProofSavedKind.PHOTO
    kinds.any { it == "photo" } -> WorkflowProofSavedKind.PHOTOS_AND_VIDEOS
    else -> WorkflowProofSavedKind.VIDEO
}

/** The videos a step asks for: a `requires_video` step with no authored count asks for one. */
internal fun workflowStepMinVideos(action: WorkflowActionDto): Int =
    if (action.proofMinVideos == 0 && action.requiresVideo) 1 else action.proofMinVideos

/** Whether a step asks the operator for an answer (a question, or an authored answer type). */
internal fun deathStepNeedsAnswer(action: WorkflowActionDto): Boolean =
    (action.answerType.isNotBlank() && action.answerType != "none") ||
        action.actionType == "question" || action.actionType == "question_select"

/**
 * A death step the operator has finished ON THE PHONE: its drafts cover the SOP's videos and
 * photos, and its draft answer is given when it asks one. Nothing here is backend truth -- the
 * step is only sent at Submit.
 */
internal fun deathStepSatisfied(
    action: WorkflowActionDto,
    drafts: List<WorkflowVideoDraft>,
    answers: Map<String, String>,
): Boolean {
    val own = drafts.filter { it.actionId == action.actionId }
    return own.count { !it.isPhoto } >= workflowStepMinVideos(action) &&
        own.count { it.isPhoto } >= action.proofMinPhotos &&
        (!deathStepNeedsAnswer(action) || !answers[action.actionId].isNullOrBlank())
}

/**
 * The recorder's briefing for a step: a death step films the animal, except the seeded
 * post-mortem step whose copy asks for the post-mortem work to be visible.
 */
internal fun workflowCapturePrompt(isDeath: Boolean, action: WorkflowActionUi?): ProofCapturePrompt = when {
    !isDeath -> ProofCapturePrompt.BIRTH
    action?.actionKey == WORKFLOW_ACTION_KEY_POST_MORTEM -> ProofCapturePrompt.POST_MORTEM
    else -> ProofCapturePrompt.DEATH
}

private const val WORKFLOW_ACTION_KEY_POST_MORTEM = "post_mortem_video"

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

/**
 * Whether this row may open the camera.
 *
 * The sequencing answer is [blocked] and NOTHING ELSE. [workflowPredecessorsReady] must not be
 * ANDed in here: it is computed over the rows THIS SCREEN RENDERS, and the Colostrum lens renders
 * only one business date's feeds. `1st Colostrum` is a `main`-section row due at birth time, so on
 * every colostrum day AFTER the birth date it is absent from the rendered list, the predecessor
 * lookup returns null, and the whole day's feeds lost their record control — while the backend
 * correctly reported `blocked=false` for them.
 *
 * Observed on kid G-005335 (born 13 Aug 22:58 IST): its five feeds fall on 14 Aug, `first_colostrum`
 * on 13 Aug. `GET /app/workflows/{id}?lens=colostrum&date=2026-08-14` returns
 * `colostrum_day_2_1500` as `pending, blocked=false, requires_video=true`, yet the row rendered with
 * no camera. The same feed uploaded fine from Birth, where the unlensed read returns all 13 rows.
 *
 * The backend already computes `blocked` against the kid's COMPLETE action set on both lenses —
 * `tasks/app.ColostrumDetail` carries `Detail.Actions` (full) alongside `Visible` (the day) for
 * exactly this reason. Re-deriving it client-side from the truncated list contradicted the
 * backend-owns-the-contract rule and could only ever be wrong.
 *
 * Death still reaches its camera: its relaxation lives in [workflowBlockedForOperator], which turns
 * `blocked` off once drafts make the predecessors ready, so the draft-awareness is preserved without
 * a second gate here.
 */
internal fun canRecordWorkflowVideo(
    action: WorkflowActionDto,
    blocked: Boolean,
    draftsSubmitting: Boolean,
): Boolean = action.actionType == "action" &&
    action.requiresVideo &&
    !operatorFinishedWorkflowStatus(action.status) &&
    !blocked &&
    !draftsSubmitting

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
internal fun workflowBlockedNote(blocked: Boolean, blockedReason: String?, module: String = WORKFLOW_MODULE_BIRTH): String {
    if (!blocked) return ""
    return when (blockedReason) {
        // Only a kid's Birth track has "birth steps"; a Death or Reconcile card said the same
        // sentence about work that has nothing to do with a birth (Realme E2E 2026-09-17).
        WORKFLOW_BLOCKED_PREVIOUS_ACTION ->
            if (module == WORKFLOW_MODULE_BIRTH) "Finish the earlier birth steps for this kid first." else "Finish the earlier steps first."
        "signoff" -> "Waiting for the videos this step signs off."
        else -> ""
    }
}

// The weigh step's unit; any other authored number step renders a plain number field. The weigh
// step is recognised by its task type (registry) with the legacy key match as the fallback for
// rows stamped before the SOP attributes existed.
internal fun workflowNumericAnswerUnit(action: WorkflowActionDto): String? = when {
    action.taskType == "weigh" -> "kg"
    action.taskType.isBlank() && action.actionKey == "take_weight" && action.actionType == "question" -> "kg"
    action.answerType == "number" -> ""
    else -> null
}

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

/** The authenticated download path of an uploaded proof (the PC Care preview shape). */
internal fun workflowProofDownloadUrl(proofRef: String): String =
    BuildConfig.API_BASE_URL.trimEnd('/') + "/app/proofs/" + proofRef.trim() + "/download"

internal fun workflowVideoCompletionKey(actionId: String, proofOutboxItemId: String): String =
    "wf-complete:$actionId:$proofOutboxItemId"

internal fun workflowVideoAnswerKey(actionId: String, proofOutboxItemId: String): String =
    "wf-answer:$actionId:$proofOutboxItemId"

private val WORKFLOW_IST: ZoneId = ZoneId.of("Asia/Kolkata")
private val WORKFLOW_DATE_TIME_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("dd/MM/yyyy · HH:mm")
private const val WORKFLOW_SECTION_COLOSTRUM = "colostrum_session"
internal const val WORKFLOW_BLOCKED_PREVIOUS_ACTION = "previous_action"
private const val WORKFLOW_MODULE_BIRTH = "birth"
private const val WORKFLOW_ACTION_KEY_FIRST_COLOSTRUM = "first_colostrum"
private const val WORKFLOW_ACTION_KEY_TAG_THE_KID = "tag_the_kid"
private const val KEY_PENDING_PROOFS = "workflow_detail.pending_proofs"
private const val KEY_PENDING_ANSWERS = "workflow_detail.pending_answers"
private val workflowJson = Json { ignoreUnknownKeys = true }
private val pendingProofsSerializer = MapSerializer(String.serializer(), ListSerializer(WorkflowProofOutboxRef.serializer()))
private val pendingAnswersSerializer = MapSerializer(String.serializer(), String.serializer())
