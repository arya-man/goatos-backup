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
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.jsonObject
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.TaskDetail
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.ROSTER_SCAN_FIELD_KEY
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.data.capture.ScannedGoatRow
import sg.mesha.goatos.core.data.forms.FormField
import sg.mesha.goatos.core.data.forms.FormFieldType
import sg.mesha.goatos.core.data.forms.FormRule
import sg.mesha.goatos.core.data.forms.FormRuleType
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ShedCompletionSummaryDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto
import sg.mesha.goatos.feature.submit.FieldKindUi
import sg.mesha.goatos.feature.submit.FormFieldUi
import sg.mesha.goatos.feature.submit.FormPickerOptionUi
import sg.mesha.goatos.feature.submit.FormRunnerState
import sg.mesha.goatos.feature.submit.ProofItemUi
import sg.mesha.goatos.feature.submit.ShedCompletionSummary
import sg.mesha.goatos.feature.submit.SubmitEvent
import sg.mesha.goatos.feature.submit.SubmitSummaryItem
import sg.mesha.goatos.feature.submit.SubmitUiState
import sg.mesha.goatos.feature.submit.SyncState
import sg.mesha.goatos.feature.submit.VaccineSummaryItem
import sg.mesha.goatos.rfid.ScanSource
import sg.mesha.goatos.ui.submitPlaceholder
import javax.inject.Inject

/**
 * Shed-record submit state holder. Offline-first (docs/decisions/android-offline-first.md,
 * MOB-001): the L2-selected task is Room-backed via [TasksRepository.observeTaskDetail] —
 * [state] renders whatever Room already has instantly (including on a cold start, process
 * death, or offline reopen) and re-renders after every successful
 * [TasksRepository.refreshTaskDetail]; a refresh failure only flips the state to a task-load
 * error when there is truly nothing cached yet, never on a cached re-entry.
 *
 * On [SubmitEvent.Submit] it ENQUEUES the write to the offline sync engine (see
 * [SyncRepository]) instead of calling the app-api inline: the outbox durably persists it and
 * returns immediately (optimistic UI), and [SyncEngine] (`:core:core-data`) performs the actual
 * `POST /app/tasks/{task_id}/submissions` in the background — surviving process restarts,
 * retrying with backoff, never duplicating the write (same idempotency key on every attempt).
 *
 * The banner reflects the REAL outbox item status, observed via [SyncRepository.observeStatus]
 * filtered to this submission's enqueued id: QUEUED → SYNCING (in-flight or auto-retrying) →
 * ACKED (accepted) / CONFLICT (server rejected — e.g. missing required answers/proof) /
 * DEAD_LETTER (transport retries exhausted). It never reports a fake success.
 *
 * When no task is assigned to the current principal (e.g. a leadership user), submit is
 * disabled and the banner says so. [SubmitEvent.Retry] rebuilds a failed shed payload from the
 * current durable form/scan/proof state, replaces the failed local row, and preserves the SAME
 * idempotency key. A client contract fix can therefore repair a rejected draft without risking a
 * duplicate if the server accepted an earlier attempt before its response was lost.
 *
 * MOB-002 (docs/mobile/proof-capture-sync-and-e2e.md): the task's SOP `form_dsl` is parsed
 * into a [FormRunnerState] and rendered inline. `goat_scan`/`video_proof` fields are Room-first:
 * a scan tag ([ScanCaptureRepository]) or a captured video ([ProofCaptureRepository]) is
 * persisted to Room BEFORE it is ever reflected in [state] — the UI only ever renders what
 * Room already has, never transient in-memory capture state. [SubmitEvent.FormToggle]/
 * [SubmitEvent.FormText]/[SubmitEvent.FormPick] still persist simple draft answers via
 * [SavedStateHandle] the same way. Role gate: capture/submit render only for a principal with
 * the explicit ground-operator role — verifier and leadership profiles remain read-only —
 * principal sees an honest role-blocked state instead (§5).
 */
@HiltViewModel
class SubmitViewModel @Inject constructor(
    private val repo: TasksRepository,
    private val syncRepository: SyncRepository,
    private val scanCaptureRepository: ScanCaptureRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val scanSource: ScanSource,
    private val proofCaptureSource: ProofCaptureSource,
    private val bootstrapRepository: BootstrapRepository,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val _state = MutableStateFlow(loadingState())
    val state: StateFlow<SubmitUiState> = _state.asStateFlow()

    // MOB-010: true while [state] has at least one active UI subscriber. `_state.subscriptionCount`
    // is the SAME counter `state` (its `asStateFlow()` view) increments/decrements — asStateFlow()
    // is a read-only wrapper over the identical underlying shared flow, it does not fork a second
    // counter. The three Room observers below (`observeCaptureState`'s two flows + `load()`'s task
    // detail flow) each gate their collection on this via collectLatest, so upstream collection
    // starts the moment the UI subscribes to [state] and is cancelled the moment it unsubscribes
    // (collectLatest cancels the previous branch before running the next) — replacing the
    // previous forever-collectors that permanently subscribed to an inner
    // stateIn(WhileSubscribed(5_000)), defeating it: that inner flow never saw zero subscribers,
    // so the Room streams were collected forever, never released when the screen backgrounded.
    private val uiSubscribed = _state.subscriptionCount.map { it > 0 }.distinctUntilChanged()

    private var currentTask: TaskSummaryDto? = null
    private var currentForm: FormSpec = FormSpec.Empty
    // R50-027: this task's SOP proof policy, driving capture limits/subject instead of hardcoded
    // client constants. Kept alongside currentTask/currentForm from the same TaskDetail resource.
    private var currentProofPolicy: ProofPolicy = ProofPolicy.Default
    private var currentShedCompletionSummary: ShedCompletionSummaryDto? = null
    private val selectedTaskId: String? = savedStateHandle.get<String>("taskId")?.takeIf { it.isNotBlank() }
    private val routeShedId: String? = savedStateHandle.get<String>("shedId")?.takeIf { it.isNotBlank() }
    private val routeSopVersionId: String? = savedStateHandle.get<String>("sopVersionId")?.takeIf { it.isNotBlank() }
    private val selectedShedId = MutableStateFlow(routeShedId)
    private var statusJob: Job? = null
    private var outboxRecoveryKey: String? = null
    private var scanTagJob: Job? = null

    // Room-first capture caches driven by the gated observers in [observeCaptureState] (MOB-010).
    private var currentScans: List<ScannedGoatRow> = emptyList()
    private var currentProofs: List<ProofCaptureRow> = emptyList()
    private var scanningFieldKey: String? = null

    // Guards a double-tap firing two concurrent video captures for the same field.
    private var captureInFlightKey: String? = null

    // Role gate (§5): null = not yet resolved; true = has an operator profile (ground
    // ground operator, capture allowed); false = viewer/verifier/leadership.
    private var captureAllowed: Boolean? = null

    // The signed-in operator id, stamped onto every captured/imported proof row as freshness and
    // attribution metadata (docs/mobile/proof-capture-sync-and-e2e.md "Capture-source rules").
    private var currentPrincipalId: String? = null

    // Set (synchronously, in load()) the moment a refresh has failed with truly nothing cached
    // for [selectedTaskId]. [applyTaskResource] consults this — rather than always rendering a
    // bare "loading" placeholder for a null Room emission — so the collectLatest observer (a
    // second, independently-scheduled coroutine) can never race the refresh outcome and clobber
    // an already-rendered honest error state back to "loading", regardless of which of the two
    // coroutines the dispatcher happens to run last.
    private var refreshFailedForTaskId: String? = null

    // Durable across process death via SavedStateHandle — NOT a plain var. A backgrounded Android
    // process is routinely killed; if the key lived only in memory, a ViewModel recreation could
    // mint a NEW key and re-enable submit for a drive that's already queued/in-flight, double-
    // submitting it with a key the backend can't dedupe. The key is scoped to the task row
    // version: retries/recreations of the same logical submission reuse it, while backend-driven
    // rework (which increments row_version) gets a fresh key and can submit corrected answers.
    // Persisting that key + the enqueued row id lets the recreated VM resume the existing banner.
    private var idempotencyKey: String?
        get() = savedStateHandle[KEY_IDEMPOTENCY]
        set(value) {
            if (value == null) savedStateHandle.remove<String>(KEY_IDEMPOTENCY) else savedStateHandle[KEY_IDEMPOTENCY] = value
        }

    private var outboxItemId: String?
        get() = savedStateHandle[KEY_OUTBOX_ITEM_ID]
        set(value) {
            if (value == null) savedStateHandle.remove<String>(KEY_OUTBOX_ITEM_ID) else savedStateHandle[KEY_OUTBOX_ITEM_ID] = value
        }

    // Recording-form answers keyed by `FormField.key` (MOB-002) — TEXT/BOOLEAN/PICKER only.
    // GOAT_SCAN/VIDEO_PROOF are Room-first (currentScans/currentProofs above), never mirrored
    // into this map: Room is their single source of truth, this map would just be a second,
    // driftable copy of the same fact. Durable across process death for the same reason
    // idempotencyKey is: a killed-and-recreated process must resume the exact draft the
    // operator was filling in, not silently drop it back to blank.
    private var formAnswers: Map<String, JsonElement>
        get() = decodeAnswers(savedStateHandle[KEY_FORM_ANSWERS])
        set(value) {
            savedStateHandle[KEY_FORM_ANSWERS] = encodeAnswers(value)
        }

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        val taskId = selectedTaskId
        if (taskId.isNullOrBlank()) {
            currentTask = null
            currentForm = FormSpec.Empty
            currentProofPolicy = ProofPolicy.Default
            clearSavedSubmission()
            _state.value = blockedState()
            return@launch
        }
        val profile = runCatching { bootstrapRepository.operatorProfile() }.getOrNull()
        captureAllowed = profile?.primaryRoleHint == OPERATOR_ROLE
        currentPrincipalId = profile?.operatorId?.ifBlank { null }
        // Cache-first: renders whatever Room already has (possibly nothing, on a cold install
        // or a task never opened before) immediately, then re-renders after every successful
        // refresh below — never blank on re-entry once a row exists for this task id.
        refreshFailedForTaskId = null
        observeCaptureState(taskId)
        // Lifecycle-safe observer (MOB-010): gated on [uiSubscribed] so the Room task-detail
        // stream is collected only while [state] has a subscriber — collectLatest cancels the
        // previous branch the instant `subscribed` flips, releasing the stream the moment the
        // screen backgrounds instead of running forever.
        viewModelScope.launch {
            uiSubscribed.collectLatest { subscribed ->
                if (subscribed) {
                    repo.observeTaskDetail(taskId).collect { resource -> applyTaskResource(resource) }
                }
            }
        }
        // Observe the Room-cached shed-completion summary (offline-first SSOT). Gated on
        // [uiSubscribed] like the other Room observers so the DB stream is released the instant
        // the screen backgrounds. The shed id is also a stream: if route arguments are lost on a
        // process/navigation restore, task detail derives scope_type=shed/scope_id and switches
        // this observer back to the shed-scoped cache instead of silently widening to task-wide
        // "Multiple sheds" summary.
        viewModelScope.launch {
            uiSubscribed.collectLatest { subscribed ->
                if (subscribed) {
                    selectedShedId
                        .flatMapLatest { shedId -> repo.observeShedCompletionSummary(taskId, shedId) }
                        .collect { summary ->
                            currentShedCompletionSummary = summary
                            renderDraft()
                        }
                }
            }
        }
        val refreshResult = repo.refreshTaskDetail(taskId)
        // Background refresh of the shed-completion summary; the observe() stream above re-emits
        // once Room is upserted. A failure leaves any cached summary on screen (offline-first).
        repo.refreshShedCompletionSummary(taskId, selectedShedId.value)
        if (refreshResult.isFailure && currentTask == null) {
            // Never synced, ever: no cache to fall back to. A stale cache (if any) stays on
            // screen instead — applyTaskResource already rendered it before this refresh ran.
            refreshFailedForTaskId = taskId
            currentTask = null
            currentForm = FormSpec.Empty
            currentProofPolicy = ProofPolicy.Default
            clearSavedSubmission()
            _state.value = errorState()
        }
    }

    /** Wires lifecycle-safe Room-first scan/proof observers for [taskId] (MOB-010) — every
     *  emission re-renders the draft so the scan count / proof-item list the operator sees is
     *  always exactly what Room holds, never a stale in-memory echo. Gated on [uiSubscribed] so
     *  collection runs only while [state] has a subscriber, releasing the DB streams the instant
     *  the screen backgrounds instead of running forever. */
    private fun observeCaptureState(taskId: String) {
        viewModelScope.launch {
            uiSubscribed.collectLatest { subscribed ->
                if (subscribed) {
                    scanCaptureRepository.observeAllForTask(taskId).collect { rows ->
                        currentScans = rows
                        renderDraft()
                    }
                }
            }
        }
        viewModelScope.launch {
            uiSubscribed.collectLatest { subscribed ->
                if (subscribed) {
                    proofCaptureRepository.observeProofs(taskId).collect { rows ->
                        currentProofs = rows
                        renderDraft()
                    }
                }
            }
        }
    }

    private fun applyTaskResource(resource: Resource<TaskDetail>) {
        val detail = resource.data
        if (detail == null) {
            currentTask = null
            currentForm = FormSpec.Empty
            currentProofPolicy = ProofPolicy.Default
            // See refreshFailedForTaskId's KDoc: render the honest error instead of a bare
            // loading placeholder when we already know this exact task's refresh failed with
            // nothing cached — never let this observer overwrite that with a stale "loading".
            _state.value = if (refreshFailedForTaskId == selectedTaskId) errorState() else loadingState()
            return
        }
        refreshFailedForTaskId = null
        if (detail.task.taskId.isBlank()) {
            currentTask = null
            currentForm = FormSpec.Empty
            currentProofPolicy = ProofPolicy.Default
            clearSavedSubmission()
            _state.value = blockedState()
            return
        }
        currentTask = detail.task.withRouteSopVersionFallback()
        currentForm = detail.form
        currentProofPolicy = detail.proofPolicy
        resolveShedScopeFromTask(currentTask ?: detail.task)
        if (shouldRenderTerminalAck(currentTask ?: detail.task)) {
            // The refreshed backend task is authoritative after a successful submit. Its row
            // version advances when the task enters review/accepted state, so trying to recover
            // the old pre-submit outbox key from the new row version would render a fresh,
            // editable form and could offer a second logical submission. Keep the completed
            // proof summary visible, but render the task as acknowledged and read-only.
            statusJob?.cancel()
            clearSavedSubmission()
            outboxRecoveryKey = null
            _state.value = terminalAckState(currentTask ?: detail.task, detail.form)
            return
        }
        bindSubmissionKey(currentTask ?: detail.task)
        val queuedItemId = outboxItemId
        val key = idempotencyKey
        if (key != null && outboxRecoveryKey != key) {
            // APK updates and some OEM process-recreation paths do not restore Activity
            // SavedState reliably. Always reconcile the saved row id against the stable key:
            // the saved id itself may name a failed row that a retry already replaced.
            outboxRecoveryKey = key
            _state.value = draftState(currentTask ?: detail.task, detail.form).copy(canSubmit = false)
            viewModelScope.launch {
                when (val recovered = syncRepository.findOutboxItemByIdempotencyKey(key)) {
                    is AppResult.Ok -> {
                        val item = recovered.value
                        if (item == null) {
                            outboxItemId = null
                            renderDraft()
                        } else {
                            outboxItemId = item.id
                            applyItemStatus(item)
                            observeOutboxItem(item.id)
                        }
                    }
                    is AppResult.Err -> {
                        if (queuedItemId != null) observeOutboxItem(queuedItemId) else renderDraft()
                    }
                }
            }
            return
        }
        if (queuedItemId != null) {
            // A submission for this task is already queued (it survived process death).
            // Resume its live banner instead of offering a fresh — duplicate — submit.
            _state.value = draftState(detail.task, detail.form).copy(canSubmit = false)
            observeOutboxItem(queuedItemId)
        } else {
            renderDraft()
        }
    }

    private fun resolveShedScopeFromTask(task: TaskSummaryDto) {
        if (!routeShedId.isNullOrBlank()) return
        val scopedShedId = task.scopeId.takeIf {
            task.scopeType.equals("shed", ignoreCase = true) && it.isNotBlank()
        } ?: return
        if (selectedShedId.value == scopedShedId) return
        selectedShedId.value = scopedShedId
        viewModelScope.launch {
            repo.refreshShedCompletionSummary(task.taskId, scopedShedId)
        }
    }

    fun onEvent(event: SubmitEvent) {
        when (event) {
            SubmitEvent.Submit -> submit()
            SubmitEvent.Retry -> retry()
            is SubmitEvent.FormToggle -> updateAnswer(event.key, JsonPrimitive(event.checked))
            is SubmitEvent.FormText -> updateAnswer(event.key, JsonPrimitive(event.value))
            is SubmitEvent.FormPick -> updateAnswer(event.key, JsonPrimitive(event.value))
            is SubmitEvent.ScanToggled -> toggleScan(event.key)
            is SubmitEvent.CaptureVideoRequested -> requestVideoCapture(event.key, event.source)
            is SubmitEvent.ProofCaptionChanged -> updateProofCaption(event.proofId, event.caption)
            is SubmitEvent.ProofRemoved -> removeProof(event.proofId)
            is SubmitEvent.ProofRetryRequested -> retryProofUpload(event.proofId)
        }
    }

    private fun updateAnswer(key: String, value: JsonElement) {
        // Once queued/syncing the payload is already enqueued — the form is locked; further
        // taps must not rewrite an answer map that already left the device.
        if (outboxItemId != null) return
        if (currentTask == null || captureAllowed == false) return
        formAnswers = formAnswers + (key to value)
        renderDraft()
    }

    private fun TaskSummaryDto.withRouteSopVersionFallback(): TaskSummaryDto =
        if (sopVersionId.isNotBlank()) this else copy(sopVersionId = routeSopVersionId.orEmpty())

    /** Starts/stops the BT-HID [scanSource] for [key]'s `goat_scan` field. Only one field
     *  scans at a time — Android owns a single BT-HID connection either way. Each completed tag
     *  is written to Room FIRST via [ScanCaptureRepository.recordScan]; the UI only updates once
     *  [observeCaptureState]'s collector re-emits from Room. */
    private fun toggleScan(key: String) {
        val task = currentTask ?: return
        if (outboxItemId != null || captureAllowed == false) return
        if (scanningFieldKey == key) {
            stopScanning()
            renderDraft()
            return
        }
        stopScanning()
        scanningFieldKey = key
        scanSource.start()
        scanTagJob = viewModelScope.launch {
            scanSource.tags.collect { tag ->
                scanCaptureRepository.recordScan(task.taskId, key, tag)
            }
        }
        renderDraft()
    }

    private fun stopScanning() {
        if (scanningFieldKey == null) return
        scanSource.stop()
        scanTagJob?.cancel()
        scanTagJob = null
        scanningFieldKey = null
    }

    /** Launches one video capture for [key]'s `video_proof` field via [proofCaptureSource]
     *  (production: the platform camera intent; tests: a fake). A cancelled capture (null
     *  result) is a silent no-op — the operator backed out, nothing to persist. A successful
     *  capture is written to Room FIRST ([ProofCaptureRepository.capture]), which also queues
     *  its background metadata-registration sync. */
    private fun requestVideoCapture(key: String, source: String) {
        val task = currentTask ?: return
        if (outboxItemId != null || captureAllowed == false) return
        if (captureInFlightKey != null) return // one capture at a time
        val subject = subjectForFieldKey(key)
        val policyMaxCount = if (currentProofPolicy.isShedLevelVideo && subject == ProofSubject.SHED) {
            currentProofPolicy.maximumCount
        } else {
            currentProofPolicy.maximumCountPerSubject
        }
        val activeCaptured = currentProofs.count {
            it.proofSubject == subject && it.syncStatus != CaptureSyncStatus.FAILED
        }
        if (activeCaptured >= policyMaxCount) return
        if (source == "gallery_picker" && !currentProofPolicy.allowsGalleryPicker) return
        captureInFlightKey = key
        viewModelScope.launch {
            try {
                val captured = if (source == "gallery_picker") proofCaptureSource.pickVideo() else proofCaptureSource.captureVideo()
                if (captured != null) {
                    val shedScopeId = selectedShedId.value
                        ?: task.scopeId.takeIf { task.scopeType.equals("shed", ignoreCase = true) && it.isNotBlank() }
                    proofCaptureRepository.capture(
                        taskId = task.taskId,
                        fieldKey = key,
                        subject = subject,
                        subjectId = if (subject == ProofSubject.SHED) shedScopeId else null,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = null,
                        scopeType = if (subject == ProofSubject.SHED && !shedScopeId.isNullOrBlank()) "shed" else "task",
                        scopeId = if (subject == ProofSubject.SHED && !shedScopeId.isNullOrBlank()) shedScopeId else task.taskId,
                        capturedStartMs = captured.startedAtMs,
                        capturedEndMs = captured.endedAtMs,
                        capturedByPrincipalId = currentPrincipalId,
                        proofPolicy = currentProofPolicy.copy(captureSource = captured.captureSource),
                    )
                }
            } finally {
                captureInFlightKey = null
            }
        }
    }

    private fun updateProofCaption(proofId: String, caption: String) {
        val task = currentTask ?: return
        if (outboxItemId != null) return
        viewModelScope.launch { proofCaptureRepository.updateCaption(task.taskId, proofId, caption) }
    }

    private fun removeProof(proofId: String) {
        val task = currentTask ?: return
        if (outboxItemId != null) return
        viewModelScope.launch {
            when (val result = proofCaptureRepository.remove(task.taskId, proofId)) {
                is AppResult.Ok -> {
                    _state.update { it.copy(lastError = null) }
                    repo.refreshShedCompletionSummary(task.taskId, selectedShedId.value)
                }
                is AppResult.Err -> _state.update {
                    it.copy(lastError = result.message.ifBlank { "Could not remove proof video. Try again." })
                }
            }
        }
    }

    private fun retryProofUpload(proofId: String) {
        val task = currentTask ?: return
        if (outboxItemId != null) return
        viewModelScope.launch { proofCaptureRepository.retryUpload(task.taskId, proofId) }
    }

    private fun renderDraft() {
        val task = currentTask ?: return
        // Room scan/proof/summary observers may emit after an outbox row has already reached
        // QUEUED, FAILED, or SUCCEEDED, or after the backend task itself reached a terminal
        // (submitted/needs_review/accepted) state. Those late emissions must not repaint the
        // screen as a fresh draft and erase the durable ACKED/submission lifecycle banner —
        // applyTaskResource is the authoritative terminal/outbox renderer.
        if (outboxItemId != null) return
        if (shouldRenderTerminalAck(task)) {
            _state.value = terminalAckState(task, currentForm)
            return
        }
        if (captureAllowed == false) {
            _state.value = submitPlaceholder().copy(isCaptureRoleBlocked = true)
            return
        }
        _state.value = draftState(task, currentForm)
    }

    private fun submit() {
        val current = currentTask ?: return
        if (captureAllowed == false) return
        // Already enqueued (e.g. a double tap, or a recreation that raced load()) — never enqueue
        // a second time; just follow the existing row. The outbox is also key-idempotent, so this
        // is belt-and-suspenders on top of the persisted key.
        outboxItemId?.let { existing ->
            observeOutboxItem(existing)
            return
        }
        // Defensive: the UI already disables the button while a required answer/proof is
        // missing (see buildFormRunnerState's blockedReason), but never enqueue a submission
        // that fails its own client-side gate even if this is reached some other way.
        // Vaccination shed acknowledgement: the backend owns the readiness gate. When a
        // shed-completion summary is present, never enqueue the acknowledgement unless the
        // backend reports submit_enabled — the empty SOP form otherwise has no client gate.
        if (currentShedCompletionSummary != null) {
            val proofReadiness = currentShedProofReadiness()
            val formBlock = buildFormRunnerState(currentForm, current)?.blockedReason
            val ready = if (currentProofPolicy.isShedLevelVideo) {
                currentShedCompletionSummary?.handledCount == currentShedCompletionSummary?.expectedCount &&
                    proofReadiness.blockingReason == null &&
                    formBlock == null
            } else {
                currentShedCompletionSummary?.submitEnabled == true && formBlock == null
            }
            if (!ready) return
        } else if (buildFormRunnerState(currentForm, current)?.blockedReason != null) {
            return
        }
        stopScanning()
        val sopVersionId = current.sopVersionId.ifBlank { routeSopVersionId.orEmpty() }
        if (sopVersionId.isBlank()) {
            _state.update {
                it.copy(
                    syncState = SyncState.CONFLICT,
                    syncLabel = "Task version missing. Please reopen this shed.",
                    canSubmit = false,
                    lastError = "Task version missing. Please reopen this shed.",
                    isQueueFailed = true,
                    isRetryFailed = false,
                )
            }
            return
        }
        val activeShedId = activeShedScopeId(current)
        val key = idempotencyKey ?: stableSubmissionKey(current, activeShedId).also { idempotencyKey = it }
        statusJob?.cancel()
        viewModelScope.launch {
            _state.update {
                it.copy(
                    syncState = SyncState.QUEUED,
                    syncLabel = "",
                    syncProgress = 0.2f,
                    canSubmit = false,
                    attemptCount = 0,
                    maxAttempts = 0,
                    lastError = null,
                    isQueueFailed = false,
                    isRetryFailed = false,
                )
            }
            // groupKey = the shed/scope this submission belongs to, so the outbox drains all
            // of a shed's writes in order (TRD: outbox is "ordered per shed").
            val groupKey = activeShedId ?: current.scopeId.ifBlank { current.taskId }
            val request = SubmitTaskRequestDto(
                sopVersionId = sopVersionId,
                idempotencyKey = key,
                answers = answersForSubmission(currentForm, formAnswers, currentScans),
                proofRefs = proofRefsForSubmission(currentProofs),
            )
            when (
                val result = syncRepository.enqueueShedSubmit(
                    taskId = current.taskId,
                    groupKey = groupKey,
                    idempotencyKey = key,
                    request = request,
                )
            ) {
                is AppResult.Ok -> {
                    outboxItemId = result.value
                    observeOutboxItem(result.value)
                }
                is AppResult.Err -> {
                    // Keep it honest: a write that can't even be queued is a visible error,
                    // never a silent drop.
                    _state.update {
                        it.copy(
                            syncState = SyncState.DEAD_LETTER,
                            syncLabel = "",
                            canSubmit = false,
                            attemptCount = 0,
                            maxAttempts = 0,
                            isQueueFailed = true,
                        )
                    }
                }
            }
        }
    }

    private fun retry() {
        val itemId = outboxItemId
        when {
            itemId != null -> viewModelScope.launch {
                when (syncRepository.retry(itemId)) {
                    is AppResult.Ok -> Unit
                    is AppResult.Err -> _state.update {
                        it.copy(syncLabel = "", syncState = SyncState.DEAD_LETTER, attemptCount = 0, maxAttempts = 0, isRetryFailed = true)
                    }
                }
            }
            currentTask != null -> viewModelScope.launch {
                // SavedStateHandle is normally restored after process death, but an APK update
                // can recreate the Activity without restoring the previous outbox row id. Recover
                // by the stable task/row key; only a terminal FAILED row is eligible for removal.
                val key = idempotencyKey
                if (key == null) {
                    submit()
                    return@launch
                }
                when (syncRepository.deleteFailedOutboxItemByIdempotencyKey(key)) {
                    is AppResult.Ok -> submit()
                    is AppResult.Err -> _state.update {
                        it.copy(syncLabel = "", syncState = SyncState.DEAD_LETTER, attemptCount = 0, maxAttempts = 0, isRetryFailed = true)
                    }
                }
            }
            else -> load()
        }
    }

    private fun bindSubmissionKey(task: TaskSummaryDto) {
        val submissionScope = submissionScope(task, activeShedScopeId(task))
        val previousScope = savedStateHandle.get<String>(KEY_SUBMISSION_SCOPE)
        if (previousScope != submissionScope) {
            savedStateHandle[KEY_SUBMISSION_SCOPE] = submissionScope
            idempotencyKey = stableSubmissionKey(task, activeShedScopeId(task))
            outboxItemId = null
            outboxRecoveryKey = null
            formAnswers = emptyMap()
            return
        }
        if (idempotencyKey == null) {
            idempotencyKey = stableSubmissionKey(task, activeShedScopeId(task))
        }
    }

    private fun activeShedScopeId(task: TaskSummaryDto): String? =
        selectedShedId.value
            ?.takeIf { it.isNotBlank() }
            ?: task.scopeId.takeIf { task.scopeType.equals("shed", ignoreCase = true) && it.isNotBlank() }

    private fun clearSavedSubmission() {
        savedStateHandle.remove<String>(KEY_SUBMISSION_SCOPE)
        idempotencyKey = null
        outboxItemId = null
        formAnswers = emptyMap()
    }

    private fun observeOutboxItem(itemId: String) {
        statusJob?.cancel()
        statusJob = viewModelScope.launch {
            // R50-030: observe the specific outbox row by id, not the bounded recent-terminal
            // window in observeStatus() — otherwise a close can wait forever once the row ages out
            // of the window before its terminal status is seen.
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)
                .collect { item -> item?.let { applyItemStatus(it) } }
        }
    }

    private fun applyItemStatus(item: SyncQueueItem) {
        when {
            item.status == SyncItemStatus.QUEUED -> _state.update {
                it.copy(syncState = SyncState.QUEUED, syncLabel = "", syncProgress = 0.2f, canSubmit = false, attemptCount = 0, maxAttempts = 0, lastError = null, isQueueFailed = false, isRetryFailed = false)
            }
            item.status == SyncItemStatus.IN_FLIGHT -> _state.update {
                it.copy(syncState = SyncState.SYNCING, syncLabel = "", syncProgress = 0.6f, canSubmit = false, attemptCount = 0, maxAttempts = 0, lastError = null, isQueueFailed = false, isRetryFailed = false)
            }
            item.status == SyncItemStatus.SUCCEEDED -> {
                val task = currentTask
                if (task != null) {
                    _state.value = terminalAckState(task, currentForm)
                } else {
                    outboxItemId = null
                    renderDraft()
                }
            }
            item.conflict -> _state.update {
                it.copy(syncState = SyncState.CONFLICT, syncLabel = "", canSubmit = false, lastError = item.lastError, attemptCount = 0, maxAttempts = 0, isQueueFailed = false, isRetryFailed = false)
            }
            item.isDeadLetter -> _state.update {
                it.copy(
                    syncState = SyncState.DEAD_LETTER,
                    syncLabel = "",
                    canSubmit = false,
                    attemptCount = item.attemptCount,
                    maxAttempts = item.maxAttempts,
                    lastError = item.lastError,
                    isQueueFailed = false,
                    isRetryFailed = false,
                )
            }
            else -> _state.update {
                // FAILED but still inside its retry budget — SyncEngine will auto-retry with
                // backoff; render this as still-syncing, not a hard failure.
                it.copy(
                    syncState = SyncState.SYNCING,
                    syncLabel = "",
                    syncProgress = 0.4f,
                    canSubmit = false,
                    attemptCount = item.attemptCount,
                    maxAttempts = item.maxAttempts,
                    lastError = item.lastError,
                    isQueueFailed = false,
                    isRetryFailed = false,
                )
            }
        }
    }

    // MOB-005: interim state builders now derive from the honest submitPlaceholder() — no
    // fabricated shed/cohort/date identity survives while loading, blocked, or errored.
    private fun loadingState(): SubmitUiState = submitPlaceholder().copy(isLoadingTask = true)

    private fun draftState(task: TaskSummaryDto, form: FormSpec): SubmitUiState {
        val summary = currentShedCompletionSummary
        // Vaccination shed completion is an acknowledgement screen, but SOP may require a
        // shed-level proof-video field on the submit screen. Hide the form only for the legacy
        // per-goat proof mode where proof capture lives on each goat row.
        val formRunner = if (summary != null && currentProofPolicy.isPerGoatVideo) null else buildFormRunnerState(form, task)
        val goatIds = currentScans
            .mapNotNull { it.goatId?.takeIf(String::isNotBlank) }
            .distinct()
        // R50-029: group once instead of re-filtering the full proof list 3x per goat below.
        val proofsByGoat = currentProofs.filter { it.proofSubject == ProofSubject.GOAT }.groupBy { it.subjectId }
        fun proofsFor(goatId: String): List<ProofCaptureRow> = proofsByGoat[goatId].orEmpty()
        val syncedProofs = goatIds.count { goatId -> proofsFor(goatId).any { it.isCompletedProofRef() } }
        val failedProofs = goatIds.count { goatId ->
            val proofs = proofsFor(goatId)
            proofs.none { it.isCompletedProofRef() } && proofs.any { it.syncStatus == CaptureSyncStatus.FAILED }
        }
        val uploadingProofs = goatIds.count { goatId ->
            val proofs = proofsFor(goatId)
            proofs.none { it.isCompletedProofRef() } &&
                proofs.none { it.syncStatus == CaptureSyncStatus.FAILED } &&
                proofs.any {
                    it.syncStatus == CaptureSyncStatus.PENDING ||
                        it.syncStatus == CaptureSyncStatus.IN_FLIGHT
                }
        }
        val shedProofReadiness = currentShedProofReadiness()
        val proofSummary = if (currentProofPolicy.isShedLevelVideo) {
            ProofSummaryState(
                title = "Shed proof videos",
                label = "${shedProofReadiness.synced} of ${shedProofReadiness.maximum} shed videos synced · ${shedProofReadiness.required} required",
                finalizeHint = "Finalize checks the shed-level video proof. It does not upload files.",
                requiredForComplete = shedProofReadiness.required,
                synced = shedProofReadiness.synced,
                uploading = shedProofReadiness.uploading,
                failed = shedProofReadiness.failed,
            )
        } else {
            val expectedProofCount = summary?.expectedCount ?: goatIds.size
            val readyProofCount = summary?.proofReadyCount ?: syncedProofs
            ProofSummaryState(
                title = "Goat camera proof",
                label = "$readyProofCount of $expectedProofCount goats synced",
                finalizeHint = "Finalize checks the synced records. It does not upload files.",
                requiredForComplete = expectedProofCount,
                synced = readyProofCount,
                uploading = if (summary != null) 0 else uploadingProofs,
                failed = if (summary != null) 0 else failedProofs,
            )
        }
        val shedCompletionSummary = if (summary != null) {
            ShedCompletionSummary(
                taskId = summary.taskId,
                shedName = summary.shedName,
                driveName = summary.driveName,
                expectedCount = summary.expectedCount,
                handledCount = summary.handledCount,
                proofReadyCount = if (currentProofPolicy.isShedLevelVideo) {
                    shedProofReadiness.synced
                } else {
                    summary.proofReadyCount
                },
                proofMode = summary.proofMode,
                submitState = summary.submitState,
            )
        } else null
        val vaccineBreakdown = summary?.vaccineBreakdown.orEmpty()
            .map { VaccineSummaryItem(vaccine = it.vaccine, count = it.count) }
        val summaryReady = if (summary != null && currentProofPolicy.isShedLevelVideo) {
            summary.handledCount == summary.expectedCount && shedProofReadiness.blockingReason == null
        } else {
            summary?.submitEnabled ?: true
        }
        val summaryBlock = if (summary != null && currentProofPolicy.isShedLevelVideo) {
            when {
                summary.handledCount != summary.expectedCount -> summary.blockingReason ?: "${summary.expectedCount - summary.handledCount} animals not yet scanned."
                shedProofReadiness.blockingReason != null -> shedProofReadiness.blockingReason
                else -> null
            }
        } else {
            summary?.blockingReason
        }
        return submitPlaceholder().copy(
            eyebrow = task.presentation?.eyebrow.orEmpty(),
            title = task.presentation?.title.orEmpty(),
            shed = "",
            cohort = "",
            date = "",
            summaryItems = task.presentation?.summaryItems.orEmpty()
                .filter { it.label.isNotBlank() && it.value.isNotBlank() }
                .map { SubmitSummaryItem(it.key, it.label, it.value) },
            groups = emptyList(),
            formRunner = formRunner,
            syncState = SyncState.DRAFT,
            syncLabel = "",
            // Vaccination shed acknowledgement: when a backend shed-completion summary is present,
            // Submit is gated on its submit_enabled flag (all expected animals handled + proof
            // ready) AND any residual form gate. Generic tasks with no shed summary keep the
            // pre-existing form-only gate.
            canSubmit = summaryReady && formRunner?.blockedReason == null,
            blockingReason = summaryBlock ?: formRunner?.blockedReason,
            syncProgress = 0f,
            proofSummaryTitle = proofSummary.title,
            proofSummarySyncedLabel = proofSummary.label,
            proofSummaryFinalizeHint = proofSummary.finalizeHint,
            proofTotal = proofSummary.requiredForComplete,
            proofSynced = proofSummary.synced,
            proofUploading = proofSummary.uploading,
            proofFailed = proofSummary.failed,
            attemptCount = 0,
            maxAttempts = 0,
            shedCompletionSummary = shedCompletionSummary,
            vaccineBreakdown = vaccineBreakdown,
        )
    }

    private data class ShedProofReadiness(
        val required: Int,
        val maximum: Int,
        val synced: Int,
        val uploading: Int,
        val failed: Int,
        val blockingReason: String?,
    )

    private fun currentShedProofReadiness(): ShedProofReadiness {
        val required = currentProofPolicy.minimumCount.coerceAtLeast(1)
        val maximum = currentProofPolicy.maximumCount.coerceAtLeast(required)
        val shedProofs = currentProofs.filter { it.proofSubject == ProofSubject.SHED }
        val localSynced = shedProofs.count { it.isCompletedProofRef() }
        val serverSynced = currentShedCompletionSummary?.proofReadyCount ?: 0
        val synced = maxOf(localSynced, serverSynced)
        val failed = shedProofs.count { !it.isCompletedProofRef() && it.syncStatus == CaptureSyncStatus.FAILED }
        val uploading = shedProofs.count {
            !it.isCompletedProofRef() &&
                it.syncStatus != CaptureSyncStatus.FAILED &&
                (it.syncStatus == CaptureSyncStatus.PENDING || it.syncStatus == CaptureSyncStatus.IN_FLIGHT)
        }
        val blockingReason = when {
            synced >= required -> null
            uploading > 0 -> "Wait for proof upload to finish before submitting."
            failed > 0 -> "Proof upload failed. Record or choose this shed video again before submitting."
            else -> "Add at least $required shed video before submitting."
        }
        return ShedProofReadiness(
            required = required,
            maximum = maximum,
            synced = synced,
            uploading = uploading,
            failed = failed,
            blockingReason = blockingReason,
        )
    }

    private fun shouldRenderTerminalAck(task: TaskSummaryDto): Boolean {
        if (!task.state.isSubmissionTerminal()) return false
        if (!currentProofPolicy.isShedLevelVideo) return true
        if (currentShedCompletionSummary?.submitState?.isSubmissionTerminal() != true) return false
        val readiness = currentShedProofReadiness()
        if (readiness.uploading > 0 || readiness.failed > 0) return false
        return readiness.blockingReason == null
    }

    private fun terminalAckState(task: TaskSummaryDto, form: FormSpec): SubmitUiState =
        draftState(task, form).copy(
            formRunner = null,
            syncState = SyncState.ACKED,
            syncLabel = "",
            syncProgress = 1f,
            canSubmit = false,
            blockingReason = null,
        )

    private fun blockedState(): SubmitUiState = submitPlaceholder().copy(isNoTaskAssigned = true)

    private fun errorState(): SubmitUiState = submitPlaceholder().copy(syncState = SyncState.DEAD_LETTER, isTaskLoadFailed = true)

    private data class ProofSummaryState(
        val title: String,
        val label: String,
        val finalizeHint: String,
        val requiredForComplete: Int,
        val synced: Int,
        val uploading: Int,
        val failed: Int,
    )

    override fun onCleared() {
        // Release the BT-HID capture and the delegate-bound video-capture launcher when this
        // ViewModel goes away — never leave hardware capture "on" for a screen that no longer
        // exists (performance/memory: no leaked capture listeners).
        stopScanning()
    }

    /** Generic non-vaccination forms may still map named media subjects. Vaccination goat
     *  clips are captured from the scan row with [ProofSubject.GOAT] and never use this mapper. */
    // R50-027: derive subject from the task's proof policy expectedSubjects, not hardcoded.
    // Falls back to per-key hardcoded mapping only when policy has no explicit mapping.
    private fun subjectForFieldKey(key: String): ProofSubject {
        currentForm.fields.firstOrNull { it.key == key }?.proofSubject?.let { raw ->
            ProofSubject.entries.firstOrNull { it.wireValue == raw }?.let { return it }
        }
        // First check if policy provides an expected subject set for this key
        val expectedFromPolicy = currentProofPolicy.expectedSubjects
            .mapNotNull { raw -> ProofSubject.entries.firstOrNull { it.wireValue == raw } }
        if (expectedFromPolicy.isNotEmpty()) {
            // Use first expected subject from policy; special-case goat to always check scanned context
            return if (ProofSubject.GOAT in expectedFromPolicy) ProofSubject.GOAT else expectedFromPolicy.first()
        }
        // Fall back to historical per-key mapping for backward compatibility
        return when (key) {
            "shed_video" -> ProofSubject.SHED
            "vial_lot_video" -> ProofSubject.VIAL_LOT
            "administration_video" -> ProofSubject.ADMINISTRATION
            else -> ProofSubject.EXTRA
        }
    }

    /** Builds the render-ready form + computes the honest submit gate: the first unmet
     *  required field's reason, or null when nothing blocks submission.
     *  R50-027: enforces minimum count + per-subject caps from proofPolicy. */
    private fun buildFormRunnerState(form: FormSpec, task: TaskSummaryDto): FormRunnerState? {
        if (form.isEmpty) return null
        val conditionallyRequired = form.rules
            .filter { it.type == FormRuleType.REQUIRED_IF && it.conditionMatches() }
            .mapNotNullTo(mutableSetOf()) { it.field }
        val conditionallyVisible = form.rules
            .filter { it.type == FormRuleType.REQUIRED_IF }
            .groupBy { it.field }
        val fields = form.fields
            .filter { field ->
                val visibilityRules = conditionallyVisible[field.key].orEmpty()
                field.required || visibilityRules.isEmpty() || visibilityRules.any { it.conditionMatches() }
            }
            .map { field -> field.toFieldUi(requiredOverride = field.required || field.key in conditionallyRequired) }
        val requiredUnansweredProofKeys = form.requiredUnansweredVideoProofKeys()
        val unreadyProof = currentProofs.firstOrNull { it.blocksSubmission(requiredUnansweredProofKeys) }

        // R50-027: check minimum count enforcement
        val minimumCountViolation = if (currentProofPolicy.minimumCount > 0) {
            if (currentProofPolicy.isShedLevelVideo) {
                currentShedProofReadiness().blockingReason
            } else {
                val completedCount = currentProofs.count { it.isCompletedProofRef() }
                if (completedCount < currentProofPolicy.minimumCount) {
                    "A minimum of ${currentProofPolicy.minimumCount} video proof(s) are required before submitting."
                } else null
            }
        } else null

        // R50-029: pre-index once (which goat ids already have a completed GOAT proof) instead of
        // an O(goats * proofs) currentProofs.none{...} scan per scanned goat.
        val goatIdsWithCompletedProof = currentProofs
            .filter { it.proofSubject == ProofSubject.GOAT && it.isCompletedProofRef() }
            .mapNotNullTo(mutableSetOf()) { it.subjectId }
        val missingGoatProof = if (currentProofPolicy.isPerGoatVideo) {
            currentScans
                .mapNotNull { it.goatId?.takeIf(String::isNotBlank) }
                .distinct()
                .firstOrNull { goatId -> goatId !in goatIdsWithCompletedProof }
        } else null
        val unmet = form.fields.firstOrNull { field -> !field.isAnswered(field.required || field.key in conditionallyRequired) }
        val activeBlock = form.rules.firstOrNull {
            it.type == FormRuleType.BLOCK_SUBMISSION_IF && it.conditionMatches()
        }
        return FormRunnerState(
            title = "Recording form",
            subtitle = task.presentation?.title.orEmpty(),
            fields = fields,
            submitLabel = "Submit",
            blockedReason = unreadyProof?.let { blockedReasonForProofUpload(it) }
                ?: minimumCountViolation
                ?: missingGoatProof?.let { "Add and sync a camera clip for every scanned goat before submitting." }
                ?: activeBlock?.message?.takeIf(String::isNotBlank)
                ?: unmet?.let { blockedReasonFor(it) },
        )
    }

    private fun FormField.isAnswered(requiredOverride: Boolean = required): Boolean {
        if (!requiredOverride) return true
        val answer = formAnswers[key]
        return when (type) {
            FormFieldType.BOOLEAN -> (answer as? JsonPrimitive)?.booleanOrNull != null
            FormFieldType.NUMBER, FormFieldType.TEXT, FormFieldType.DATE_TIME ->
                !(answer as? JsonPrimitive)?.content.isNullOrBlank()
            FormFieldType.SELECT, FormFieldType.VACCINE_BATCH_PICKER, FormFieldType.LOCATION_PICKER ->
                !(answer as? JsonPrimitive)?.content.isNullOrBlank()
            FormFieldType.GOAT_SCAN -> {
                val summary = currentShedCompletionSummary
                when {
                    summary != null && summary.expectedCount > 0 ->
                        summary.handledCount >= summary.expectedCount
                    else -> currentScans.any { it.matchesGoatScanField(key, currentForm.rosterScanTargetFieldKey()) }
                }
            }
            FormFieldType.VIDEO_PROOF ->
                if (currentProofPolicy.isShedLevelVideo && subjectForFieldKey(key) == ProofSubject.SHED) {
                    currentShedProofReadiness().blockingReason == null
                } else {
                    currentProofs.any { it.fieldKey == key && it.isCompletedProofRef() }
                }
            FormFieldType.UNKNOWN -> false
        }
    }

    /** Executes the backend-authored v1 rule against durable draft/capture state. Unknown
     * operators fail closed (not matched); the app never invents a condition result. */
    private fun FormRule.conditionMatches(): Boolean {
        val key = conditionField ?: return false
        val actual = conditionValue(key)
        return when (operator?.trim()?.lowercase()) {
            "equals" -> actual == value
            "not_equals" -> actual != value
            "empty" -> actual.isEmptyValue()
            "not_empty" -> !actual.isEmptyValue()
            "in" -> (value as? JsonArray)?.contains(actual) == true
            "not_in" -> (value as? JsonArray)?.contains(actual) == false
            "gt" -> actual.asDoubleOrNull()?.let { left -> value.asDoubleOrNull()?.let { left > it } } == true
            "gte" -> actual.asDoubleOrNull()?.let { left -> value.asDoubleOrNull()?.let { left >= it } } == true
            "lt" -> actual.asDoubleOrNull()?.let { left -> value.asDoubleOrNull()?.let { left < it } } == true
            "lte" -> actual.asDoubleOrNull()?.let { left -> value.asDoubleOrNull()?.let { left <= it } } == true
            else -> false
        }
    }

    private fun conditionValue(key: String): JsonElement? {
        formAnswers[key]?.let { return it }
        val field = currentForm.fields.firstOrNull { it.key == key }
        return when (field?.type) {
            FormFieldType.GOAT_SCAN -> JsonArray(
                currentScans
                    .filter { it.matchesGoatScanField(key, currentForm.rosterScanTargetFieldKey()) }
                    .map { JsonPrimitive(it.goatId ?: it.tag) },
            )
            FormFieldType.VIDEO_PROOF -> JsonArray(
                currentProofs.filter { it.fieldKey == key && it.isCompletedProofRef() }
                    .mapNotNull { it.serverProofId?.let(::JsonPrimitive) },
            )
            else -> null
        }
    }

    private fun JsonElement?.isEmptyValue(): Boolean = when (this) {
        null -> true
        is JsonPrimitive -> isString && content.isBlank()
        is JsonArray -> isEmpty()
        is JsonObject -> isEmpty()
        else -> false
    }

    private fun JsonElement?.asDoubleOrNull(): Double? =
        (this as? JsonPrimitive)?.content?.toDoubleOrNull()

    private fun ProofCaptureRow.isCompletedProofRef(): Boolean =
        syncStatus == CaptureSyncStatus.SYNCED && !serverProofId.isNullOrBlank()

    private fun FormSpec.requiredUnansweredVideoProofKeys(): Set<String> =
        fields
            .filter { it.type == FormFieldType.VIDEO_PROOF && it.required && !it.isAnswered() }
            .map { it.key }
            .toSet()

    private fun ProofCaptureRow.blocksSubmission(requiredUnansweredProofKeys: Set<String>): Boolean {
        if (isCompletedProofRef()) return false
        if (proofSubject == ProofSubject.GOAT) return true
        return when (syncStatus) {
            CaptureSyncStatus.FAILED -> fieldKey in requiredUnansweredProofKeys
            CaptureSyncStatus.PENDING, CaptureSyncStatus.IN_FLIGHT, CaptureSyncStatus.SYNCED -> true
        }
    }

    private fun blockedReasonForProofUpload(proof: ProofCaptureRow): String =
        if (proof.syncStatus == CaptureSyncStatus.FAILED) {
            proof.lastError?.takeIf { it.isNotBlank() } ?: "Proof upload failed. Record this video again before submitting."
        } else {
            "Wait for proof upload to finish before submitting."
        }

    private fun blockedReasonFor(field: FormField): String = field.disabledReason ?: when (field.type) {
        FormFieldType.GOAT_SCAN -> "Scan the required goats (\"${field.label}\") before submitting."
        FormFieldType.VIDEO_PROOF -> "Record the required video (\"${field.label}\") before submitting."
        FormFieldType.UNKNOWN -> "Update the app to record \"${field.label}\" before submitting."
        FormFieldType.BOOLEAN -> "Confirm \"${field.label}\" before submitting."
        FormFieldType.NUMBER, FormFieldType.TEXT -> "Enter \"${field.label}\" before submitting."
        FormFieldType.DATE_TIME -> "Set \"${field.label}\" before submitting."
        FormFieldType.SELECT, FormFieldType.VACCINE_BATCH_PICKER, FormFieldType.LOCATION_PICKER -> "Select \"${field.label}\" before submitting."
    }

    private fun FormField.toFieldUi(requiredOverride: Boolean = required): FormFieldUi = when (type) {
        FormFieldType.BOOLEAN -> FormFieldUi(
            key = key,
            label = label,
            kind = FieldKindUi.BOOLEAN,
            required = requiredOverride,
            checked = (formAnswers[key] as? JsonPrimitive)?.booleanOrNull == true,
            booleanValue = (formAnswers[key] as? JsonPrimitive)?.booleanOrNull,
            helpText = helpText,
        )
        FormFieldType.NUMBER -> FormFieldUi(
            key = key,
            label = label,
            kind = FieldKindUi.NUMBER,
            required = requiredOverride,
            helpText = helpText,
            text = (formAnswers[key] as? JsonPrimitive)?.content.orEmpty(),
        )
        FormFieldType.TEXT -> FormFieldUi(
            key = key,
            label = label,
            kind = FieldKindUi.TEXT,
            required = requiredOverride,
            helpText = helpText,
            text = (formAnswers[key] as? JsonPrimitive)?.content.orEmpty(),
        )
        FormFieldType.DATE_TIME -> FormFieldUi(
            key = key,
            label = label,
            kind = FieldKindUi.DATE_TIME,
            required = requiredOverride,
            helpText = helpText,
            text = (formAnswers[key] as? JsonPrimitive)?.content.orEmpty(),
        )
        FormFieldType.SELECT, FormFieldType.VACCINE_BATCH_PICKER, FormFieldType.LOCATION_PICKER -> {
            val selectedValue = (formAnswers[key] as? JsonPrimitive)?.content.orEmpty()
            val selectedLabel = options.firstOrNull { it.value == selectedValue }?.label ?: selectedValue
            FormFieldUi(
                key = key,
                label = label,
                kind = FieldKindUi.PICKER,
                required = requiredOverride,
                helpText = helpText,
                selectedLabel = selectedLabel,
                options = options.map {
                    FormPickerOptionUi(
                        value = it.value,
                        label = it.label,
                        enabled = !it.disabled,
                        disabledReason = it.disabledReason,
                    )
                },
                error = disabledReason,
            )
        }
        FormFieldType.GOAT_SCAN -> {
            val matchingScans = currentScans.filter { it.matchesGoatScanField(key, currentForm.rosterScanTargetFieldKey()) }
            val summary = currentShedCompletionSummary
            val count = if (summary != null && summary.expectedCount > 0) {
                maxOf(matchingScans.size, summary.handledCount)
            } else {
                matchingScans.size
            }
            FormFieldUi(
                key = key,
                label = label,
                kind = FieldKindUi.GOAT_SCAN,
                required = requiredOverride,
                helpText = helpText,
                scannedCount = count,
                latestScanAtMs = matchingScans.maxOfOrNull { it.capturedAtMs },
                scanning = scanningFieldKey == key,
            )
        }
        FormFieldType.VIDEO_PROOF -> {
            val items = currentProofs.filter { it.fieldKey == key }
            val isExtraSlot = repeat
            // R50-027: respect the per-subject cap from policy, not hardcoded MAX_PROOFS_PER_TASK
            val fieldSubject = proofSubject?.let { raw -> ProofSubject.entries.firstOrNull { it.wireValue == raw } } ?: subjectForFieldKey(key)
            val activeCaptured = currentProofs.count {
                it.proofSubject == fieldSubject && it.syncStatus != CaptureSyncStatus.FAILED
            }
            val shedReadiness = if (currentProofPolicy.isShedLevelVideo && fieldSubject == ProofSubject.SHED) {
                currentShedProofReadiness()
            } else {
                null
            }
            val serverProofItems = if (items.isEmpty() && shedReadiness != null && shedReadiness.synced > 0) {
                (1..shedReadiness.synced).map { index ->
                    ProofItemUi(
                        id = "server-shed-proof-$index",
                        label = "Shed proof video $index",
                        syncStatus = "SYNCED",
                    )
                }
            } else {
                emptyList()
            }
            val policyMaxCount = if (currentProofPolicy.isShedLevelVideo && fieldSubject == ProofSubject.SHED) {
                currentProofPolicy.maximumCount
            } else {
                currentProofPolicy.maximumCountPerSubject
            }
            val allowMultiple = isExtraSlot || (currentProofPolicy.isShedLevelVideo && fieldSubject == ProofSubject.SHED)
            FormFieldUi(
                key = key,
                label = label,
                kind = FieldKindUi.VIDEO_PROOF,
                required = requiredOverride,
                helpText = helpText,
                proofCaptured = items.isNotEmpty() || serverProofItems.isNotEmpty(),
                proofItems = items.map { it.toProofItemUi(label, isExtraSlot) } + serverProofItems,
                canCaptureMore = if (allowMultiple) {
                    maxOf(activeCaptured, shedReadiness?.synced ?: 0) < policyMaxCount
                } else {
                    items.none { it.syncStatus != CaptureSyncStatus.FAILED }
                },
                allowGalleryPicker = currentProofPolicy.isShedLevelVideo && fieldSubject == ProofSubject.SHED && currentProofPolicy.allowsGalleryPicker,
            )
        }
        FormFieldType.UNKNOWN -> FormFieldUi(key = key, label = label, kind = FieldKindUi.UNKNOWN, required = requiredOverride)
    }

    private fun ProofCaptureRow.toProofItemUi(fieldLabel: String, isExtraSlot: Boolean): ProofItemUi = ProofItemUi(
        id = id,
        label = if (isExtraSlot) caption?.ifBlank { null } ?: "Extra video" else fieldLabel,
        caption = caption.orEmpty(),
        editableCaption = isExtraSlot,
        retryable = syncStatus == CaptureSyncStatus.FAILED,
        syncStatus = syncStatus.name,
    )

    internal companion object {
        // SavedStateHandle keys — survive process death so the idempotency key + enqueued row id
        // + draft answers are never lost to a ViewModel recreation (which would otherwise double-
        // submit or silently drop the operator's in-progress form).
        const val KEY_SUBMISSION_SCOPE = "submit.submissionScope"
        const val KEY_IDEMPOTENCY = "submit.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "submit.outboxItemId"
        const val KEY_FORM_ANSWERS = "submit.formAnswers"

        val answersJson = Json { ignoreUnknownKeys = true }

        fun stableSubmissionKey(task: TaskSummaryDto): String = "shed-submit:${submissionScope(task)}"

        fun stableSubmissionKey(task: TaskSummaryDto, activeShedId: String?): String =
            "shed-submit:${submissionScope(task, activeShedId)}"

        fun submissionScope(task: TaskSummaryDto): String =
            submissionScope(task, activeShedId = null)

        fun submissionScope(task: TaskSummaryDto, activeShedId: String?): String {
            val scopeId = activeShedId?.takeIf { it.isNotBlank() } ?: task.scopeId.ifBlank { task.taskId }
            return "${task.taskId}:scope:$scopeId:rv:${task.rowVersion}"
        }

        fun String.isSubmissionTerminal(): Boolean = when (lowercase()) {
            "submitted", "needs_review", "accepted", "verified", "closed", "completed" -> true
            else -> false
        }

        fun encodeAnswers(answers: Map<String, JsonElement>): String = JsonObject(answers).toString()

        fun decodeAnswers(raw: String?): Map<String, JsonElement> {
            if (raw.isNullOrBlank()) return emptyMap()
            return runCatching { answersJson.parseToJsonElement(raw).jsonObject }.getOrDefault(JsonObject(emptyMap()))
        }

        /** Only the answers this form actually declares — defensive against stale saved-state
         *  keys surviving a form/SOP version change. GOAT_SCAN answers come from Room
         *  ([currentScans]), never from the local draft map (Room is their SSOT); VIDEO_PROOF
         *  contributes nothing here — captured proof travels in [SubmitTaskRequestDto.proofRefs]
         *  instead. */
        fun answersForSubmission(
            form: FormSpec,
            answers: Map<String, JsonElement>,
            scans: List<ScannedGoatRow>,
        ): Map<String, JsonElement> = form.fields.mapNotNull { field ->
            when (field.type) {
                FormFieldType.GOAT_SCAN -> {
                    val goatIdentifiers = scans
                        .filter { it.matchesGoatScanField(field.key, form.rosterScanTargetFieldKey()) }
                        // The canonical vaccination contract and proof subjects use goat UUIDs,
                        // not the RFID text that located the goat. Keep the tag fallback only for
                        // generic legacy forms whose scan repository has no resolved goat row.
                        .map { it.goatId?.takeIf(String::isNotBlank) ?: it.tag }
                        .distinct()
                    if (goatIdentifiers.isEmpty()) null else {
                        field.key to JsonArray(goatIdentifiers.map { JsonPrimitive(it) })
                    }
                }
                FormFieldType.VIDEO_PROOF -> null
                else -> answers[field.key]?.let { field.key to it }
            }
        }.toMap()

        /** Every completed backend proof (across every `video_proof` field) as the wire proof-ref
         *  list. Submit gating requires [serverProofId] to be present; never send a local Room id
         *  as a proof ref, because the backend review path requires completed server proof rows. */
        fun proofRefsForSubmission(proofs: List<ProofCaptureRow>): List<ProofReferenceDto> = proofs.mapNotNull { row ->
            val serverProofId = row.serverProofId
                ?.takeIf { row.syncStatus == CaptureSyncStatus.SYNCED && it.isNotBlank() }
                ?: return@mapNotNull null
            ProofReferenceDto(
                proofId = serverProofId,
                proofType = "video",
                subjectType = row.proofSubject.wireValue,
                subjectId = row.subjectId,
                uploadState = "completed",
                metadata = buildMap {
                    put("field_key", JsonPrimitive(row.fieldKey))
                    row.caption?.takeIf { it.isNotBlank() }?.let { put("caption", JsonPrimitive(it)) }
                },
            )
        }
    }
}

private const val OPERATOR_ROLE = "operator"

private fun FormSpec.rosterScanTargetFieldKey(): String? =
    fields.singleOrNull { it.type == FormFieldType.GOAT_SCAN }?.key

private fun ScannedGoatRow.matchesGoatScanField(scanFieldKey: String, rosterScanTargetFieldKey: String?): Boolean =
    fieldKey == scanFieldKey || (fieldKey == ROSTER_SCAN_FIELD_KEY && scanFieldKey == rosterScanTargetFieldKey)
