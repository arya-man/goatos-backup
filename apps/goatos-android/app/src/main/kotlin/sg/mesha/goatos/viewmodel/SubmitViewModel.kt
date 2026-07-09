package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto
import sg.mesha.goatos.core.network.dto.ValidationReportDto
import sg.mesha.goatos.feature.submit.SubmitEvent
import sg.mesha.goatos.feature.submit.SubmitUiState
import sg.mesha.goatos.feature.submit.SyncState
import sg.mesha.goatos.ui.sampleSubmitState
import java.util.UUID
import javax.inject.Inject

/**
 * Shed-record submit state holder. Loads the operator's assigned task from
 * [TasksRepository.tasks] and, on [SubmitEvent.Submit], performs a REAL idempotent
 * write via [TasksRepository.submit] (POST /app/tasks/{task_id}/submissions). The
 * banner reflects the actual outcome: SYNCING → ACKED on an accepted submission,
 * CONFLICT on a server-rejected one, DEAD_LETTER on a network/transport failure.
 *
 * When no task is assigned to the current principal (e.g. a leadership user), submit
 * is disabled and the banner says so — the screen NEVER reports a fake success.
 * [SubmitEvent.Retry] re-attempts the same submission (reusing the idempotency key so
 * a retry is safe) or reloads the task when there is nothing to submit.
 *
 * NOTE: answers/proof payload mapping (form DSL runner + camera→signed-URL proof upload) is
 * not wired yet; the submission is sent with empty answers. A task whose form needs no
 * answers ACKs; one that requires answers/proof is rejected — surfaced honestly via
 * [rejectionReason] as "recording form lands in a later build", never a fabricated ACK.
 */
@HiltViewModel
class SubmitViewModel @Inject constructor(
    private val repo: TasksRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(loadingState())
    val state: StateFlow<SubmitUiState> = _state.asStateFlow()

    private var task: TaskSummaryDto? = null
    private var idempotencyKey: String? = null
    private var syncJob: Job? = null

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        runCatching { repo.tasks(limit = 1) }
            .onSuccess { response ->
                val next = response.items.firstOrNull()
                if (next == null) {
                    task = null
                    idempotencyKey = null
                    _state.value = blockedState("No task assigned to you to submit.")
                } else {
                    task = next
                    idempotencyKey = UUID.randomUUID().toString()
                    _state.value = draftState(next)
                }
            }
            .onFailure {
                task = null
                idempotencyKey = null
                _state.value = errorState("Couldn't load your task. Tap retry.")
            }
    }

    fun onEvent(event: SubmitEvent) {
        when (event) {
            SubmitEvent.Submit -> submit()
            SubmitEvent.Retry -> if (task != null) submit() else load()
        }
    }

    private fun submit() {
        val current = task ?: return
        val key = idempotencyKey ?: UUID.randomUUID().toString().also { idempotencyKey = it }
        syncJob?.cancel()
        syncJob = viewModelScope.launch {
            _state.update {
                it.copy(
                    syncState = SyncState.SYNCING,
                    syncLabel = "Submitting shed record…",
                    syncProgress = 0.5f,
                    canSubmit = false,
                )
            }
            runCatching {
                repo.submit(
                    taskId = current.taskId,
                    request = SubmitTaskRequestDto(
                        sopVersionId = current.sopVersionId,
                        idempotencyKey = key,
                    ),
                )
            }.onSuccess { response ->
                val report = response.submission.validationReport
                if (report.valid) {
                    _state.update {
                        it.copy(
                            syncState = SyncState.ACKED,
                            syncLabel = "Synced · record on file",
                            syncProgress = 1f,
                            canSubmit = false,
                        )
                    }
                } else {
                    _state.update {
                        it.copy(
                            syncState = SyncState.CONFLICT,
                            syncLabel = rejectionReason(report),
                            canSubmit = false,
                        )
                    }
                }
            }.onFailure {
                _state.update {
                    it.copy(
                        syncState = SyncState.DEAD_LETTER,
                        syncLabel = "Sync failed — tap retry.",
                        canSubmit = false,
                    )
                }
            }
        }
    }

    // Turn a backend validation failure into an honest operator-facing line. When the ONLY
    // errors are missing required answers/proof, the real blocker is that this build has no
    // recording-form capture yet (form DSL runner + camera→proof upload land later) — say that
    // plainly instead of leaking a raw field-error. Any other rejection is shown verbatim.
    private fun rejectionReason(report: ValidationReportDto): String {
        val codes = report.errors.map { it.code }
        val onlyFormGaps = codes.isNotEmpty() && codes.all {
            it == "required" || it == "proof_required" || it == "proof_subject_required"
        }
        return if (onlyFormGaps) {
            "This drive needs the recording form before it can be submitted — form capture lands in a later build."
        } else {
            report.errors.firstOrNull()?.message ?: "Server rejected the submission."
        }
    }

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
}
