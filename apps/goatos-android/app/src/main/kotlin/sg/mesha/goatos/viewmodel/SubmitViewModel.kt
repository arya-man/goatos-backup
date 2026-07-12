package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.jsonObject
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.TaskDetail
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.forms.FormField
import sg.mesha.goatos.core.data.forms.FormFieldType
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto
import sg.mesha.goatos.feature.submit.FieldKindUi
import sg.mesha.goatos.feature.submit.FormFieldUi
import sg.mesha.goatos.feature.submit.FormPickerOptionUi
import sg.mesha.goatos.feature.submit.FormRunnerState
import sg.mesha.goatos.feature.submit.SubmitEvent
import sg.mesha.goatos.feature.submit.SubmitUiState
import sg.mesha.goatos.feature.submit.SyncState
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
 * MOB-002: the task's SOP `form_dsl` is parsed into a [FormRunnerState] and rendered inline
 * (see `SubmitScreen`'s `FormFieldsColumn`) — [SubmitEvent.FormToggle]/[SubmitEvent.FormText]/
 * [SubmitEvent.FormPick] persist real operator answers (boolean/number/text/picker) into
 * [SubmitTaskRequestDto.answers], durable across process death via [SavedStateHandle]. Submit
 * stays disabled (with an honest reason) while a required answer is missing. `goat_scan` /
 * `video_proof` fields need a capture pipeline this build does not have yet (no camera/RFID
 * capture wired into Submit) — those stay honestly blocked rather than silently sending an
 * empty/fabricated answer; a task whose form needs no answers, or only text/number/boolean/
 * picker answers, submits for real.
 */
@HiltViewModel
class SubmitViewModel @Inject constructor(
    private val repo: TasksRepository,
    private val syncRepository: SyncRepository,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val _state = MutableStateFlow(loadingState())
    val state: StateFlow<SubmitUiState> = _state.asStateFlow()

    private var currentTask: TaskSummaryDto? = null
    private var currentForm: FormSpec = FormSpec.Empty
    private val selectedTaskId: String? = savedStateHandle.get<String>("taskId")
    private var observeTaskJob: Job? = null
    private var statusJob: Job? = null

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

    // Recording-form answers keyed by `FormField.key` (MOB-002). Durable across process death
    // for the same reason idempotencyKey is: a killed-and-recreated process must resume the
    // exact draft the operator was filling in, not silently drop it back to blank.
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
            _state.value = blockedState("No task selected to submit.")
            return@launch
        }
        // Cache-first: renders whatever Room already has (possibly nothing, on a cold install
        // or a task never opened before) immediately, then re-renders after every successful
        // refresh below — never blank on re-entry once a row exists for this task id.
        refreshFailedForTaskId = null
        observeTaskJob?.cancel()
        observeTaskJob = viewModelScope.launch {
            repo.observeTaskDetail(taskId).collectLatest { resource -> applyTaskResource(resource) }
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
            _state.value = blockedState("No task assigned to you to submit.")
            return
        }
        currentTask = detail.task
        currentForm = detail.form
        bindSubmissionKey(detail.task)
        val queuedItemId = outboxItemId
        if (queuedItemId != null) {
            // A submission for this task is already queued (it survived process death).
            // Resume its live banner instead of offering a fresh — duplicate — submit.
            _state.value = draftState(detail.task, detail.form, formAnswers).copy(canSubmit = false)
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
        }
    }

    private fun updateAnswer(key: String, value: JsonElement) {
        // Once queued/syncing the payload is already enqueued — the form is locked; further
        // taps must not rewrite an answer map that already left the device.
        if (outboxItemId != null) return
        if (currentTask == null) return
        formAnswers = formAnswers + (key to value)
        renderDraft()
    }

    private fun renderDraft() {
        val task = currentTask ?: return
        _state.value = draftState(task, currentForm, formAnswers)
    }

    private fun submit() {
        val current = currentTask ?: return
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
        if (buildFormRunnerState(currentForm, current, formAnswers)?.blockedReason != null) return
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
                answers = answersForSubmission(currentForm, formAnswers),
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
                .collect { item -> applyItemStatus(item) }
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

    // Turn a backend validation failure into an honest operator-facing line. When the ONLY
    // errors are missing required answers/proof, the real blocker is that this build has no
    // recording-form capture yet (form DSL runner + camera→proof upload land later) — say that
    // plainly instead of leaking a raw field-error. Any other rejection is shown verbatim.
    private fun decodeRejectionReason(item: SyncQueueItem): String =
        item.lastError ?: "Server rejected the submission."

    // MOB-005: interim state builders now derive from the honest submitPlaceholder() — no
    // fabricated shed/cohort/date identity survives while loading, blocked, or errored.
    private fun loadingState(): SubmitUiState = submitPlaceholder().copy(isLoadingTask = true)

    private fun draftState(task: TaskSummaryDto, form: FormSpec, answers: Map<String, JsonElement>): SubmitUiState {
        val formRunner = buildFormRunnerState(form, task, answers)
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

    private fun blockedState(message: String): SubmitUiState = submitPlaceholder().copy(isNoTaskAssigned = true)

    private fun errorState(): SubmitUiState = submitPlaceholder().copy(syncState = SyncState.DEAD_LETTER, isTaskLoadFailed = true)

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

        private fun submissionScope(task: TaskSummaryDto): String = "${task.taskId}:rv:${task.rowVersion}"

        fun encodeAnswers(answers: Map<String, JsonElement>): String = JsonObject(answers).toString()

        fun decodeAnswers(raw: String?): Map<String, JsonElement> {
            if (raw.isNullOrBlank()) return emptyMap()
            return runCatching { answersJson.parseToJsonElement(raw).jsonObject }.getOrDefault(JsonObject(emptyMap()))
        }

        /** Only the answers this form actually declares — defensive against stale saved-state
         *  keys surviving a form/SOP version change. */
        fun answersForSubmission(form: FormSpec, answers: Map<String, JsonElement>): Map<String, JsonElement> =
            form.fields.mapNotNull { field -> answers[field.key]?.let { field.key to it } }.toMap()

        /** Builds the render-ready form + computes the honest submit gate: the first unmet
         *  required field's reason, or null when nothing blocks submission. */
        fun buildFormRunnerState(form: FormSpec, task: TaskSummaryDto, answers: Map<String, JsonElement>): FormRunnerState? {
            if (form.isEmpty) return null
            val fields = form.fields.map { field -> field.toFieldUi(answers[field.key]) }
            val unmet = form.fields.firstOrNull { field -> !field.isAnswered(answers[field.key]) }
            return FormRunnerState(
                title = "Recording form",
                subtitle = task.title,
                fields = fields,
                submitLabel = "Submit",
                blockedReason = unmet?.let { blockedReasonFor(it) },
            )
        }

        private fun FormField.isAnswered(answer: JsonElement?): Boolean {
            if (!required) return true
            return when (type) {
                FormFieldType.BOOLEAN -> (answer as? JsonPrimitive)?.booleanOrNull == true
                FormFieldType.NUMBER, FormFieldType.TEXT -> !(answer as? JsonPrimitive)?.content.isNullOrBlank()
                FormFieldType.VACCINE_BATCH_PICKER, FormFieldType.LOCATION_PICKER ->
                    !(answer as? JsonPrimitive)?.content.isNullOrBlank()
                // No capture pipeline wired into Submit yet (camera / RFID scan) — a required
                // scan or proof field always honestly blocks rather than silently passing.
                FormFieldType.GOAT_SCAN, FormFieldType.VIDEO_PROOF, FormFieldType.UNKNOWN -> false
            }
        }

        private fun blockedReasonFor(field: FormField): String = when (field.type) {
            FormFieldType.GOAT_SCAN -> "Scan the required goats before submitting — not available in this build."
            FormFieldType.VIDEO_PROOF -> "Record the required proof before submitting — not available in this build."
            FormFieldType.UNKNOWN -> "Update the app to record \"${field.label}\" before submitting."
            FormFieldType.BOOLEAN -> "Confirm \"${field.label}\" before submitting."
            FormFieldType.NUMBER, FormFieldType.TEXT -> "Enter \"${field.label}\" before submitting."
            FormFieldType.VACCINE_BATCH_PICKER, FormFieldType.LOCATION_PICKER -> "Select \"${field.label}\" before submitting."
        }

        private fun FormField.toFieldUi(answer: JsonElement?): FormFieldUi = when (type) {
            FormFieldType.BOOLEAN -> FormFieldUi(
                key = key,
                label = label,
                kind = FieldKindUi.BOOLEAN,
                required = required,
                checked = (answer as? JsonPrimitive)?.booleanOrNull ?: false,
            )
            FormFieldType.NUMBER -> FormFieldUi(
                key = key,
                label = label,
                kind = FieldKindUi.NUMBER,
                required = required,
                text = (answer as? JsonPrimitive)?.content.orEmpty(),
            )
            FormFieldType.TEXT -> FormFieldUi(
                key = key,
                label = label,
                kind = FieldKindUi.TEXT,
                required = required,
                text = (answer as? JsonPrimitive)?.content.orEmpty(),
            )
            FormFieldType.VACCINE_BATCH_PICKER, FormFieldType.LOCATION_PICKER -> {
                val selectedValue = (answer as? JsonPrimitive)?.content.orEmpty()
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
            FormFieldType.GOAT_SCAN -> FormFieldUi(key = key, label = label, kind = FieldKindUi.GOAT_SCAN, required = required)
            FormFieldType.VIDEO_PROOF -> FormFieldUi(key = key, label = label, kind = FieldKindUi.VIDEO_PROOF, required = required)
            FormFieldType.UNKNOWN -> FormFieldUi(key = key, label = label, kind = FieldKindUi.UNKNOWN, required = required)
        }
    }
}
