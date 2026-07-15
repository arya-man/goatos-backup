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
import sg.mesha.goatos.core.data.capture.MAX_PROOFS_PER_TASK
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.data.capture.ScannedGoatRow
import sg.mesha.goatos.core.data.forms.FormField
import sg.mesha.goatos.core.data.forms.FormFieldType
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto
import sg.mesha.goatos.feature.submit.FieldKindUi
import sg.mesha.goatos.feature.submit.FormFieldUi
import sg.mesha.goatos.feature.submit.FormPickerOptionUi
import sg.mesha.goatos.feature.submit.FormRunnerState
import sg.mesha.goatos.feature.submit.ProofItemUi
import sg.mesha.goatos.feature.submit.SubmitEvent
import sg.mesha.goatos.feature.submit.SubmitUiState
import sg.mesha.goatos.feature.submit.SyncState
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
 * disabled and the banner says so. [SubmitEvent.Retry] re-arms the SAME outbox row (same
 * idempotency key, same payload) rather than minting a new one.
 *
 * MOB-002 (docs/mobile/proof-capture-sync-and-e2e.md): the task's SOP `form_dsl` is parsed
 * into a [FormRunnerState] and rendered inline. `goat_scan`/`video_proof` fields are Room-first:
 * a scan tag ([ScanCaptureRepository]) or a captured video ([ProofCaptureRepository]) is
 * persisted to Room BEFORE it is ever reflected in [state] — the UI only ever renders what
 * Room already has, never transient in-memory capture state. [SubmitEvent.FormToggle]/
 * [SubmitEvent.FormText]/[SubmitEvent.FormPick] still persist simple draft answers via
 * [SavedStateHandle] the same way. Role gate: capture/submit render only for a principal with
 * an operator profile (ground operator / park manager) — an approver-only (leadership)
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
    private val selectedTaskId: String? = savedStateHandle.get<String>("taskId")
    private var statusJob: Job? = null
    private var scanTagJob: Job? = null

    // Room-first capture caches driven by the gated observers in [observeCaptureState] (MOB-010).
    private var currentScans: List<ScannedGoatRow> = emptyList()
    private var currentProofs: List<ProofCaptureRow> = emptyList()
    private var scanningFieldKey: String? = null

    // Guards a double-tap firing two concurrent video captures for the same field.
    private var captureInFlightKey: String? = null

    // Role gate (§5): null = not yet resolved; true = has an operator profile (ground
    // operator/park manager, capture allowed); false = approver-only (leadership).
    private var captureAllowed: Boolean? = null

    // The signed-in operator id, stamped onto every captured proof row as freshness/anti-fraud
    // attribution metadata (docs/mobile/proof-capture-sync-and-e2e.md "Camera-only capture").
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
            clearSavedSubmission()
            _state.value = blockedState()
            return@launch
        }
        val profile = runCatching { bootstrapRepository.operatorProfile() }.getOrNull()
        captureAllowed = profile != null
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
        val refreshResult = repo.refreshTaskDetail(taskId)
        if (refreshResult.isFailure && currentTask == null) {
            // Never synced, ever: no cache to fall back to. A stale cache (if any) stays on
            // screen instead — applyTaskResource already rendered it before this refresh ran.
            refreshFailedForTaskId = taskId
            currentTask = null
            currentForm = FormSpec.Empty
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
            clearSavedSubmission()
            _state.value = blockedState()
            return
        }
        currentTask = detail.task
        currentForm = detail.form
        bindSubmissionKey(detail.task)
        val queuedItemId = outboxItemId
        if (queuedItemId != null) {
            // A submission for this task is already queued (it survived process death).
            // Resume its live banner instead of offering a fresh — duplicate — submit.
            _state.value = draftState(detail.task, detail.form).copy(canSubmit = false)
            observeOutboxItem(queuedItemId)
        } else {
            renderDraft()
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
            is SubmitEvent.CaptureVideoRequested -> requestVideoCapture(event.key)
            is SubmitEvent.ProofCaptionChanged -> updateProofCaption(event.proofId, event.caption)
            is SubmitEvent.ProofRemoved -> removeProof(event.proofId)
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
    private fun requestVideoCapture(key: String) {
        val task = currentTask ?: return
        if (outboxItemId != null || captureAllowed == false) return
        if (captureInFlightKey != null) return // one capture at a time
        val totalCaptured = currentProofs.size
        if (totalCaptured >= MAX_PROOFS_PER_TASK) return
        captureInFlightKey = key
        viewModelScope.launch {
            try {
                val captured = proofCaptureSource.captureVideo()
                if (captured != null) {
                    proofCaptureRepository.capture(
                        taskId = task.taskId,
                        fieldKey = key,
                        subject = subjectForFieldKey(key),
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = null,
                        scopeType = "task",
                        scopeId = task.taskId,
                        capturedStartMs = captured.startedAtMs,
                        capturedEndMs = captured.endedAtMs,
                        capturedByPrincipalId = currentPrincipalId,
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
        viewModelScope.launch { proofCaptureRepository.remove(task.taskId, proofId) }
    }

    private fun renderDraft() {
        val task = currentTask ?: return
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
        if (buildFormRunnerState(currentForm, current)?.blockedReason != null) return
        stopScanning()
        val key = idempotencyKey ?: stableSubmissionKey(current).also { idempotencyKey = it }
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
                )
            }
            // groupKey = the shed/scope this submission belongs to, so the outbox drains all
            // of a shed's writes in order (TRD: outbox is "ordered per shed").
            val groupKey = current.scopeId.ifBlank { current.taskId }
            val request = SubmitTaskRequestDto(
                sopVersionId = current.sopVersionId,
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
                    is AppResult.Ok -> observeOutboxItem(itemId)
                    is AppResult.Err -> _state.update {
                        it.copy(syncLabel = "", syncState = SyncState.DEAD_LETTER, attemptCount = 0, maxAttempts = 0, isRetryFailed = true)
                    }
                }
            }
            currentTask != null -> submit()
            else -> load()
        }
    }

    private fun bindSubmissionKey(task: TaskSummaryDto) {
        val submissionScope = submissionScope(task)
        val previousScope = savedStateHandle.get<String>(KEY_SUBMISSION_SCOPE)
        if (previousScope != submissionScope) {
            savedStateHandle[KEY_SUBMISSION_SCOPE] = submissionScope
            idempotencyKey = stableSubmissionKey(task)
            outboxItemId = null
            formAnswers = emptyMap()
            return
        }
        if (idempotencyKey == null) {
            idempotencyKey = stableSubmissionKey(task)
        }
    }

    private fun clearSavedSubmission() {
        savedStateHandle.remove<String>(KEY_SUBMISSION_SCOPE)
        idempotencyKey = null
        outboxItemId = null
        formAnswers = emptyMap()
    }

    private fun observeOutboxItem(itemId: String) {
        statusJob?.cancel()
        statusJob = viewModelScope.launch {
            syncRepository.observeStatus()
                .map { status -> status.items.firstOrNull { it.id == itemId } }
                .filterNotNull()
                .distinctUntilChanged()
                .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)
                .collect { item -> item?.let { applyItemStatus(it) } }
        }
    }

    private fun applyItemStatus(item: SyncQueueItem) {
        when {
            item.status == SyncItemStatus.QUEUED -> _state.update {
                it.copy(syncState = SyncState.QUEUED, syncLabel = "", syncProgress = 0.2f, canSubmit = false, attemptCount = 0, maxAttempts = 0)
            }
            item.status == SyncItemStatus.IN_FLIGHT -> _state.update {
                it.copy(syncState = SyncState.SYNCING, syncLabel = "", syncProgress = 0.6f, canSubmit = false, attemptCount = 0, maxAttempts = 0)
            }
            item.status == SyncItemStatus.SUCCEEDED -> _state.update {
                it.copy(syncState = SyncState.ACKED, syncLabel = "", syncProgress = 1f, canSubmit = false, attemptCount = 0, maxAttempts = 0)
            }
            item.conflict -> _state.update {
                it.copy(syncState = SyncState.CONFLICT, syncLabel = "", canSubmit = false, lastError = item.lastError, attemptCount = 0, maxAttempts = 0)
            }
            item.isDeadLetter -> _state.update {
                it.copy(
                    syncState = SyncState.DEAD_LETTER,
                    syncLabel = "",
                    canSubmit = false,
                    attemptCount = item.attemptCount,
                    maxAttempts = item.maxAttempts,
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
                )
            }
        }
    }

    // MOB-005: interim state builders now derive from the honest submitPlaceholder() — no
    // fabricated shed/cohort/date identity survives while loading, blocked, or errored.
    private fun loadingState(): SubmitUiState = submitPlaceholder().copy(isLoadingTask = true)

    private fun draftState(task: TaskSummaryDto, form: FormSpec): SubmitUiState {
        val formRunner = buildFormRunnerState(form, task)
        // No fabricated vaccine groups — the due-group breakdown needs a read model this
        // build doesn't have yet, so groups stay empty until it is wired.
        return submitPlaceholder().copy(
            shed = task.title.ifBlank { task.sopCode },
            cohort = task.description,
            date = task.dueAt.orEmpty(),
            groups = emptyList(),
            formRunner = formRunner,
            syncState = SyncState.DRAFT,
            syncLabel = "",
            canSubmit = formRunner?.blockedReason == null,
            syncProgress = 0f,
            attemptCount = 0,
            maxAttempts = 0,
        )
    }

    private fun blockedState(): SubmitUiState = submitPlaceholder().copy(isNoTaskAssigned = true)

    private fun errorState(): SubmitUiState = submitPlaceholder().copy(syncState = SyncState.DEAD_LETTER, isTaskLoadFailed = true)

    override fun onCleared() {
        // Release the BT-HID capture and the delegate-bound video-capture launcher when this
        // ViewModel goes away — never leave hardware capture "on" for a screen that no longer
        // exists (performance/memory: no leaked capture listeners).
        stopScanning()
    }

    /** Named-subject convention documented in the SOP (docs/mobile/proof-capture-sync-and-e2e.md
     *  §2 table): `shed_video`/`vial_lot_video`/`administration_video` map to their fixed
     *  subject; any other field key (an operator-added extra) is [ProofSubject.EXTRA]. Only the
     *  MAPPING is a client convention — whether each is required/what it proves stays server-
     *  driven via `FormField.required`/`helpText`. */
    private fun subjectForFieldKey(key: String): ProofSubject = when (key) {
        "shed_video" -> ProofSubject.SHED
        "vial_lot_video" -> ProofSubject.VIAL_LOT
        "administration_video" -> ProofSubject.ADMINISTRATION
        else -> ProofSubject.EXTRA
    }

    /** Builds the render-ready form + computes the honest submit gate: the first unmet
     *  required field's reason, or null when nothing blocks submission. */
    private fun buildFormRunnerState(form: FormSpec, task: TaskSummaryDto): FormRunnerState? {
        if (form.isEmpty) return null
        val fields = form.fields.map { field -> field.toFieldUi() }
        val unmet = form.fields.firstOrNull { field -> !field.isAnswered() }
        return FormRunnerState(
            title = "Recording form",
            subtitle = task.title,
            fields = fields,
            submitLabel = "Submit",
            blockedReason = unmet?.let { blockedReasonFor(it) },
        )
    }

    private fun FormField.isAnswered(): Boolean {
        if (!required) return true
        val answer = formAnswers[key]
        return when (type) {
            FormFieldType.BOOLEAN -> (answer as? JsonPrimitive)?.booleanOrNull == true
            FormFieldType.NUMBER, FormFieldType.TEXT -> !(answer as? JsonPrimitive)?.content.isNullOrBlank()
            FormFieldType.VACCINE_BATCH_PICKER, FormFieldType.LOCATION_PICKER ->
                !(answer as? JsonPrimitive)?.content.isNullOrBlank()
            FormFieldType.GOAT_SCAN -> currentScans.any { it.fieldKey == key }
            FormFieldType.VIDEO_PROOF -> currentProofs.any { it.fieldKey == key }
            FormFieldType.UNKNOWN -> false
        }
    }

    private fun blockedReasonFor(field: FormField): String = when (field.type) {
        FormFieldType.GOAT_SCAN -> "Scan the required goats (\"${field.label}\") before submitting."
        FormFieldType.VIDEO_PROOF -> "Record the required video (\"${field.label}\") before submitting."
        FormFieldType.UNKNOWN -> "Update the app to record \"${field.label}\" before submitting."
        FormFieldType.BOOLEAN -> "Confirm \"${field.label}\" before submitting."
        FormFieldType.NUMBER, FormFieldType.TEXT -> "Enter \"${field.label}\" before submitting."
        FormFieldType.VACCINE_BATCH_PICKER, FormFieldType.LOCATION_PICKER -> "Select \"${field.label}\" before submitting."
    }

    private fun FormField.toFieldUi(): FormFieldUi = when (type) {
        FormFieldType.BOOLEAN -> FormFieldUi(
            key = key,
            label = label,
            kind = FieldKindUi.BOOLEAN,
            required = required,
            checked = (formAnswers[key] as? JsonPrimitive)?.booleanOrNull ?: false,
        )
        FormFieldType.NUMBER -> FormFieldUi(
            key = key,
            label = label,
            kind = FieldKindUi.NUMBER,
            required = required,
            text = (formAnswers[key] as? JsonPrimitive)?.content.orEmpty(),
        )
        FormFieldType.TEXT -> FormFieldUi(
            key = key,
            label = label,
            kind = FieldKindUi.TEXT,
            required = required,
            text = (formAnswers[key] as? JsonPrimitive)?.content.orEmpty(),
        )
        FormFieldType.VACCINE_BATCH_PICKER, FormFieldType.LOCATION_PICKER -> {
            val selectedValue = (formAnswers[key] as? JsonPrimitive)?.content.orEmpty()
            val selectedLabel = options.firstOrNull { it.value == selectedValue }?.label ?: selectedValue
            FormFieldUi(
                key = key,
                label = label,
                kind = FieldKindUi.PICKER,
                required = required,
                selectedLabel = selectedLabel,
                options = options.map { FormPickerOptionUi(it.value, it.label) },
            )
        }
        FormFieldType.GOAT_SCAN -> {
            val count = currentScans.count { it.fieldKey == key }
            FormFieldUi(
                key = key,
                label = label,
                kind = FieldKindUi.GOAT_SCAN,
                required = required,
                helpText = helpText,
                scannedCount = count,
                scanning = scanningFieldKey == key,
            )
        }
        FormFieldType.VIDEO_PROOF -> {
            val items = currentProofs.filter { it.fieldKey == key }
            val isExtraSlot = repeat
            val totalCaptured = currentProofs.size
            FormFieldUi(
                key = key,
                label = label,
                kind = FieldKindUi.VIDEO_PROOF,
                required = required,
                helpText = helpText,
                proofCaptured = items.isNotEmpty(),
                proofItems = items.map { it.toProofItemUi(label, isExtraSlot) },
                canCaptureMore = if (isExtraSlot) totalCaptured < MAX_PROOFS_PER_TASK else items.isEmpty(),
            )
        }
        FormFieldType.UNKNOWN -> FormFieldUi(key = key, label = label, kind = FieldKindUi.UNKNOWN, required = required)
    }

    private fun ProofCaptureRow.toProofItemUi(fieldLabel: String, isExtraSlot: Boolean): ProofItemUi = ProofItemUi(
        id = id,
        label = if (isExtraSlot) caption?.ifBlank { null } ?: "Extra video" else fieldLabel,
        caption = caption.orEmpty(),
        editableCaption = isExtraSlot,
        syncStatus = syncStatus.name,
    )

    private companion object {
        // SavedStateHandle keys — survive process death so the idempotency key + enqueued row id
        // + draft answers are never lost to a ViewModel recreation (which would otherwise double-
        // submit or silently drop the operator's in-progress form).
        const val KEY_SUBMISSION_SCOPE = "submit.submissionScope"
        const val KEY_IDEMPOTENCY = "submit.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "submit.outboxItemId"
        const val KEY_FORM_ANSWERS = "submit.formAnswers"

        val answersJson = Json { ignoreUnknownKeys = true }

        fun stableSubmissionKey(task: TaskSummaryDto): String = "shed-submit:${submissionScope(task)}"

        fun submissionScope(task: TaskSummaryDto): String = "${task.taskId}:rv:${task.rowVersion}"

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
                    val tags = scans.filter { it.fieldKey == field.key }.map { it.tag }
                    if (tags.isEmpty()) null else field.key to JsonArray(tags.map { JsonPrimitive(it) })
                }
                FormFieldType.VIDEO_PROOF -> null
                else -> answers[field.key]?.let { field.key to it }
            }
        }.toMap()

        /** Every captured proof (across every `video_proof` field) as the wire proof-ref list.
         *  [ProofReferenceDto.proofId] prefers the backend-registered id once the metadata
         *  registration has synced; a row still PENDING/IN_FLIGHT sends its stable local id so
         *  the backend at least sees a consistent reference for retries. */
        fun proofRefsForSubmission(proofs: List<ProofCaptureRow>): List<ProofReferenceDto> = proofs.map { row ->
            ProofReferenceDto(
                proofId = row.serverProofId ?: row.id,
                proofType = "video",
                subjectType = row.proofSubject.wireValue,
                subjectId = null,
                uploadState = row.syncStatus.name.lowercase(),
                metadata = buildMap {
                    put("field_key", JsonPrimitive(row.fieldKey))
                    row.caption?.takeIf { it.isNotBlank() }?.let { put("caption", JsonPrimitive(it)) }
                },
            )
        }
    }
}
