package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.util.UUID
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsWorkInstructions
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.WorkInstructionsRepository
import sg.mesha.goatos.feature.counts.WorkInstructionRowUi
import sg.mesha.goatos.feature.counts.WorkInstructionsEvent
import sg.mesha.goatos.feature.counts.WorkInstructionsUiState

/**
 * The Work instructions list (SOP studio phase 2, docs/decisions/sop-studio.md): the general
 * SOPs a person may start by hand. Renders from Room (the repository's cached list) and refreshes
 * in the background; a start is an online write whose returned run id the host navigates to.
 */
@HiltViewModel
class WorkInstructionsViewModel @Inject constructor(
    private val repository: WorkInstructionsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Local(
        val title: String = "",
        val refreshing: Boolean = false,
        val startingCode: String = "",
        val message: String = "",
        /** The run just started; the host navigates to it and then clears it. */
        val openWorkflowId: String = "",
        val loadedOnce: Boolean = false,
    )

    private val local = MutableStateFlow(Local())

    /** Binds the backend nav label the shell routed with. Idempotent. */
    fun bind(title: String) {
        if (local.value.title == title) return
        local.value = local.value.copy(title = title)
        analytics.track(AnalyticsEventsWorkInstructions.LIST_OPENED, mapOf(AnalyticsEvents.Params.SOURCE to "work_instructions_tab"))
    }

    val state: StateFlow<WorkInstructionsUiState> = combine(local, repository.observeStartable()) { l, sops ->
        WorkInstructionsUiState(
            title = l.title,
            isRefreshing = l.refreshing,
            loadedOnce = l.loadedOnce,
            rows = sops.map { WorkInstructionRowUi(code = it.code, name = it.name, description = it.description, stepCount = it.stepCount, starting = it.code == l.startingCode) },
            message = l.message,
            openWorkflowId = l.openWorkflowId,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WorkInstructionsUiState())

    fun onEvent(event: WorkInstructionsEvent) {
        when (event) {
            WorkInstructionsEvent.Refresh -> refresh()
            is WorkInstructionsEvent.Start -> start(event.code)
            WorkInstructionsEvent.NavigationHandled -> local.value = local.value.copy(openWorkflowId = "")
            WorkInstructionsEvent.DismissMessage -> local.value = local.value.copy(message = "")
        }
    }

    /** Non-blocking: a failure leaves the cached list on screen. */
    private fun refresh() {
        viewModelScope.launch {
            local.value = local.value.copy(refreshing = true)
            repository.refresh().onFailure { e ->
                crashReporter.recordException(e, "work instructions refresh failed")
                analytics.track(AnalyticsEventsWorkInstructions.FAILURE, mapOf(AnalyticsEvents.Params.REASON to (e.message ?: "unknown").take(MAX_REASON_CHARS)))
            }
            local.value = local.value.copy(refreshing = false, loadedOnce = true)
        }
    }

    private fun start(code: String) {
        if (local.value.startingCode.isNotBlank()) return
        local.value = local.value.copy(startingCode = code, message = "")
        viewModelScope.launch {
            // One fresh key per tap: a retried request with the same key is the same run.
            val runKey = "work-instruction-run:" + UUID.randomUUID()
            repository.start(code, runKey)
                .onSuccess { workflowId ->
                    analytics.track(AnalyticsEventsWorkInstructions.START, mapOf(AnalyticsEvents.Params.RESULT to "started", "sop_code" to code))
                    local.value = local.value.copy(startingCode = "", openWorkflowId = workflowId)
                }
                .onFailure { e ->
                    crashReporter.recordException(e, "work instruction start failed")
                    analytics.track(AnalyticsEventsWorkInstructions.START, mapOf(AnalyticsEvents.Params.RESULT to "failed", AnalyticsEvents.Params.REASON to (e.message ?: "unknown").take(MAX_REASON_CHARS)))
                    local.value = local.value.copy(startingCode = "", message = START_FAILED_MESSAGE)
                }
        }
    }

    private companion object {
        const val MAX_REASON_CHARS = 120
        const val START_FAILED_MESSAGE = "Could not start. Check the network and try again."
    }
}
