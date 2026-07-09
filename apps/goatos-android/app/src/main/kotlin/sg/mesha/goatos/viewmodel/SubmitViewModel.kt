package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.feature.submit.SubmitEvent
import sg.mesha.goatos.feature.submit.SubmitUiState
import sg.mesha.goatos.feature.submit.SyncState
import sg.mesha.goatos.ui.sampleSubmitState
import javax.inject.Inject

/**
 * Shed-record submit state holder. Seeds the interim [sampleSubmitState] fixture and
 * drives the write-path lifecycle locally so the sync banner animates: [SubmitEvent.Submit]
 * advances DRAFT → SYNCING (progress ramp) → ACKED; [SubmitEvent.Retry] resets to DRAFT.
 *
 * TODO: replace the simulated sync with the Room outbox / sync-engine writes via AppApi.
 */
@HiltViewModel
class SubmitViewModel @Inject constructor() : ViewModel() {

    // TODO: wire when operator endpoints exist (no scoped operator read for the current user yet).
    private val _state = MutableStateFlow(sampleSubmitState().copy(syncState = SyncState.DRAFT))
    val state: StateFlow<SubmitUiState> = _state.asStateFlow()

    private var syncJob: Job? = null

    fun onEvent(event: SubmitEvent) {
        when (event) {
            SubmitEvent.Submit -> startSync()
            SubmitEvent.Retry -> reset()
        }
    }

    private fun startSync() {
        syncJob?.cancel()
        syncJob = viewModelScope.launch {
            _state.update {
                it.copy(
                    syncState = SyncState.SYNCING,
                    syncLabel = "Syncing shed record…",
                    syncProgress = 0f,
                    canSubmit = false,
                )
            }
            val steps = listOf(0.25f, 0.5f, 0.75f, 1f)
            for (p in steps) {
                delay(250)
                _state.update { it.copy(syncProgress = p) }
            }
            delay(200)
            _state.update {
                it.copy(
                    syncState = SyncState.ACKED,
                    syncLabel = "Synced · record on file",
                    syncProgress = 1f,
                    canSubmit = false,
                )
            }
        }
    }

    private fun reset() {
        syncJob?.cancel()
        _state.update {
            it.copy(
                syncState = SyncState.DRAFT,
                syncLabel = "Ready to submit",
                syncProgress = 0f,
                canSubmit = true,
            )
        }
    }
}
