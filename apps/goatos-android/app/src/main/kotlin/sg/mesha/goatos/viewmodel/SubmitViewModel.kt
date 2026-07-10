package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto
import sg.mesha.goatos.feature.submit.SubmitEvent
import sg.mesha.goatos.feature.submit.SubmitUiState
import sg.mesha.goatos.feature.submit.SyncState
import sg.mesha.goatos.ui.sampleSubmitState
import javax.inject.Inject

/**
 * Shed-record submit state holder. Loads the operator's assigned task from
 * [TasksRepository.tasks], then on [SubmitEvent.Submit] ENQUEUES the write to the offline
 * sync engine (see [SyncRepository]) instead of calling the app-api inline: the outbox
 * durably persists it and returns immediately (optimistic UI), and [SyncEngine]
 * (`:core:core-data`) performs the actual `POST /app/tasks/{task_id}/submissions` in the
 * background — surviving process restarts, retrying with backoff, never duplicating the
 * write (same idempotency key on every attempt).
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
 * NOTE: answers/proof payload mapping (form DSL runner + camera→signed-URL proof upload) is
 * not wired yet; the submission is sent with empty answers. A task whose form needs no
 * answers ACKs; one that requires answers/proof is rejected — the sync engine substitutes
 * an honest "recording form lands in a later build" message (see
 * `SyncEngine.rejectionReason`) rather than leaking a raw field-error code, and
 * [decodeRejectionReason] just renders it verbatim — never a fabricated ACK.
 */
@HiltViewModel
class SubmitViewModel @Inject constructor(
    private val repo: TasksRepository,
    private val syncRepository: SyncRepository,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val _state = MutableStateFlow(loadingState())
    val state: StateFlow<SubmitUiState> = _state.asStateFlow()

    private var task: TaskSummaryDto? = null
    private var statusJob: Job? = null

    // Durable across process death via SavedStateHandle — NOT a plain var. A backgrounded Android
    // process is routinely killed; if the key lived only in memory, a ViewModel recreation could
    // mint a NEW key and re-enable submit for a drive that's already queued/in-flight, double-
    // submitting it with a key the backend can't dedupe. Persisting a task-derived key + the
    // enqueued row id lets the recreated VM reuse the same key and resume the existing banner.
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

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        runCatching { repo.tasks(limit = 1) }
            .onSuccess { response ->
                val next = response.items.firstOrNull()
                if (next == null) {
                    task = null
                    clearSavedSubmission()
                    _state.value = blockedState("No task assigned to you to submit.")
                } else {
                    task = next
                    bindSubmissionKey(next.taskId)
                    val queuedItemId = outboxItemId
                    if (queuedItemId != null) {
                        // A submission for this task is already queued (it survived process death).
                        // Resume its live banner instead of offering a fresh — duplicate — submit.
                        _state.value = draftState(next).copy(canSubmit = false)
                        observeOutboxItem(queuedItemId)
                    } else {
                        _state.value = draftState(next)
                    }
                }
            }
            .onFailure {
                task = null
                clearSavedSubmission()
                _state.value = errorState("Couldn't load your task. Tap retry.")
            }
    }

    fun onEvent(event: SubmitEvent) {
        when (event) {
            SubmitEvent.Submit -> submit()
            SubmitEvent.Retry -> retry()
        }
    }

    private fun submit() {
        val current = task ?: return
        // Already enqueued (e.g. a double tap, or a recreation that raced load()) — never enqueue
        // a second time; just follow the existing row. The outbox is also key-idempotent, so this
        // is belt-and-suspenders on top of the persisted key.
        outboxItemId?.let { existing ->
            observeOutboxItem(existing)
            return
        }
        val key = idempotencyKey ?: stableSubmissionKey(current.taskId).also { idempotencyKey = it }
        statusJob?.cancel()
        viewModelScope.launch {
            _state.update {
                it.copy(
                    syncState = SyncState.QUEUED,
                    syncLabel = "Queued — will sync…",
                    syncProgress = 0.2f,
                    canSubmit = false,
                )
            }
            // groupKey = the shed/scope this submission belongs to, so the outbox drains all
            // of a shed's writes in order (TRD: outbox is "ordered per shed").
            val groupKey = current.scopeId.ifBlank { current.taskId }
            val request = SubmitTaskRequestDto(sopVersionId = current.sopVersionId, idempotencyKey = key)
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
                            syncLabel = "Couldn't queue the submission — tap retry.",
                            canSubmit = false,
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
                        it.copy(syncLabel = "Couldn't retry — tap retry again.", syncState = SyncState.DEAD_LETTER)
                    }
                }
            }
            task != null -> submit()
            else -> load()
        }
    }

    private fun bindSubmissionKey(taskId: String) {
        val previousTaskId = savedStateHandle.get<String>(KEY_TASK_ID)
        if (previousTaskId != taskId) {
            savedStateHandle[KEY_TASK_ID] = taskId
            idempotencyKey = stableSubmissionKey(taskId)
            outboxItemId = null
            return
        }
        if (idempotencyKey == null) {
            idempotencyKey = stableSubmissionKey(taskId)
        }
    }

    private fun clearSavedSubmission() {
        savedStateHandle.remove<String>(KEY_TASK_ID)
        idempotencyKey = null
        outboxItemId = null
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
                it.copy(syncState = SyncState.QUEUED, syncLabel = "Queued — will sync…", syncProgress = 0.2f, canSubmit = false)
            }
            item.status == SyncItemStatus.IN_FLIGHT -> _state.update {
                it.copy(syncState = SyncState.SYNCING, syncLabel = "Submitting shed record…", syncProgress = 0.6f, canSubmit = false)
            }
            item.status == SyncItemStatus.SUCCEEDED -> _state.update {
                it.copy(syncState = SyncState.ACKED, syncLabel = "Synced · record on file", syncProgress = 1f, canSubmit = false)
            }
            item.conflict -> _state.update {
                it.copy(syncState = SyncState.CONFLICT, syncLabel = decodeRejectionReason(item), canSubmit = false)
            }
            item.isDeadLetter -> _state.update {
                it.copy(
                    syncState = SyncState.DEAD_LETTER,
                    syncLabel = "Sync failed after ${item.attemptCount} attempts — tap retry.",
                    canSubmit = false,
                )
            }
            else -> _state.update {
                // FAILED but still inside its retry budget — SyncEngine will auto-retry with
                // backoff; render this as still-syncing, not a hard failure.
                it.copy(
                    syncState = SyncState.SYNCING,
                    syncLabel = "Retrying… (attempt ${item.attemptCount}/${item.maxAttempts})",
                    syncProgress = 0.4f,
                    canSubmit = false,
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

    // Interim state builders: reuse the sample only for stable chrome labels (eyebrow/
    // title). No fabricated vaccine groups — the due-group breakdown needs the form
    // contract, which isn't wired yet, so groups stay empty until it is.
    private fun loadingState(): SubmitUiState = sampleSubmitState().copy(
        groups = emptyList(),
        syncState = SyncState.DRAFT,
        syncLabel = "Loading…",
        canSubmit = false,
        syncProgress = 0f,
    )

    private fun draftState(t: TaskSummaryDto): SubmitUiState = sampleSubmitState().copy(
        shed = t.title.ifBlank { t.sopCode },
        cohort = t.description,
        date = t.dueAt.orEmpty(),
        groups = emptyList(),
        syncState = SyncState.DRAFT,
        syncLabel = "Ready to submit",
        canSubmit = true,
        syncProgress = 0f,
    )

    private fun blockedState(message: String): SubmitUiState = sampleSubmitState().copy(
        groups = emptyList(),
        syncState = SyncState.DRAFT,
        syncLabel = message,
        canSubmit = false,
        syncProgress = 0f,
    )

    private fun errorState(message: String): SubmitUiState = sampleSubmitState().copy(
        groups = emptyList(),
        syncState = SyncState.DEAD_LETTER,
        syncLabel = message,
        canSubmit = false,
        syncProgress = 0f,
    )

    private companion object {
        // SavedStateHandle keys — survive process death so the idempotency key + enqueued row id
        // are never lost to a ViewModel recreation (which would otherwise double-submit).
        const val KEY_TASK_ID = "submit.taskId"
        const val KEY_IDEMPOTENCY = "submit.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "submit.outboxItemId"

        fun stableSubmissionKey(taskId: String): String = "shed-submit:$taskId"
    }
}
