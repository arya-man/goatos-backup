package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import javax.inject.Inject

/**
 * Shell-level connectivity + outbox status for the passive offline banner and the sync sheet.
 * It consumes the SAME hot [SyncRepository.observeStatus] flow the engine already drives — no new
 * connectivity detection here, this only surfaces the existing signal to the UI.
 */
@HiltViewModel
class SyncStatusViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
) : ViewModel() {

    /** Live outbox snapshot: connectivity + queue counts + the per-item list. */
    val status: StateFlow<SyncStatus> = syncRepository.observeStatus()

    /**
     * Debounced "show the offline banner" signal. Going OFFLINE is delayed by [OFFLINE_GRACE_MS]
     * so a brief blip (a tunnel, a Wi-Fi→cellular handoff) never flashes the bar; coming back
     * ONLINE clears it immediately. `flatMapLatest` cancels the pending show if we reconnect
     * inside the grace window.
     */
    @OptIn(ExperimentalCoroutinesApi::class)
    val showOfflineBanner: StateFlow<Boolean> =
        status
            .map { !it.online }
            .distinctUntilChanged()
            .flatMapLatest { offline ->
                if (offline) flow { delay(OFFLINE_GRACE_MS); emit(true) } else flowOf(false)
            }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(STOP_TIMEOUT_MS), false)

    /** Re-arms every FAILED / dead-letter row (same key, fresh budget) and kicks a drain. */
    fun retryAll() {
        viewModelScope.launch {
            status.value.items
                .filter { it.status == SyncItemStatus.FAILED }
                .forEach { syncRepository.retry(it.id) }
            syncRepository.triggerDrain()
        }
    }

    private companion object {
        const val OFFLINE_GRACE_MS = 2_500L
        const val STOP_TIMEOUT_MS = 5_000L
    }
}
