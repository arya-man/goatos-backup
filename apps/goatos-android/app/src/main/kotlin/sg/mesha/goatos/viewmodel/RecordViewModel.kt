package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.feature.record.RecordEvent
import sg.mesha.goatos.feature.record.RecordTone
import sg.mesha.goatos.feature.record.RecordUiState
import sg.mesha.goatos.feature.record.VaccineGroupRow
import sg.mesha.goatos.ui.sampleRecordState
import javax.inject.Inject

/**
 * Read-only shed/drive record state holder — offline-first (docs/decisions/android-offline-first.md).
 * Room is the UI's single source of truth: [state] is fed by [ExecutionRepository.observeShed],
 * a cache-first Flow that emits instantly from Room and re-emits after every successful
 * [ExecutionRepository.refreshShed] upsert. [refresh] drives the network call and transient
 * [RecordUiState.isRefreshing]/[RecordUiState.isOffline] flags; the DTO → UiState mapping is
 * unchanged. An empty real response shows an honest empty state; a refresh failure with NO cache
 * ever observed shows an honest error state; a refresh failure WITH cached data keeps rendering
 * that cache and only flips [RecordUiState.isOffline] — never a blank/loading wall.
 *
 * Verify/Rework actions (C35-011) are enqueued to the outbox for offline-first delivery.
 */
@HiltViewModel
class RecordViewModel @Inject constructor(
    private val repo: ExecutionRepository,
    private val syncRepo: SyncRepository,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val shedId: String? = savedStateHandle.get<String>("shedId")

    private val _state = MutableStateFlow(recordPlaceholder("Loading record…"))
    val state: StateFlow<RecordUiState> = _state.asStateFlow()

    init {
        // Cache-first OFFLINE-FIRST FIX (C35-019): observe directly from the shedId
        // route parameter. Do NOT call repo.rows() to derive the ID — that duplicates
        // the network read and breaks a cold offline launch when the cache is empty.
        // Room is the single source of truth; refresh upserts Room in the background.
        viewModelScope.launch {
            shedId?.let {
                repo.observeShed(it).collectLatest { resource -> applyResource(resource) }
            }
        }
        refresh()
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observeShed]
     *  collector above re-emits and updates [state]); on failure it only flips
     *  [RecordUiState.isOffline] — cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        _state.update { it.copy(isRefreshing = true) }
        if (shedId != null) {
            val result = repo.refreshShed(shedId)
            _state.update { it.copy(isRefreshing = false, isOffline = result.isFailure) }
        } else {
            _state.update { current ->
                recordPlaceholder("No record to show for this shed.").copy(isRefreshing = false)
            }
        }
    }

    private fun applyResource(resource: Resource<VaccinationExecutionShedDrilldownDto>) {
        val dto = resource.data
        val base = dto?.toRecordUiState()
            ?: if (resource.hasData) recordPlaceholder("No record to show for this shed.") else recordPlaceholder("Loading…")
        _state.update { current ->
            base.copy(
                isRefreshing = current.isRefreshing,
                lastSyncedAt = resource.lastSyncedAt ?: current.lastSyncedAt,
                isOffline = current.isOffline,
            )
        }
    }

    fun onEvent(event: RecordEvent) {
        when (event) {
            RecordEvent.Close -> Unit // navigation — handled by the nav host.
            is RecordEvent.Verify -> {
                // Enqueue verify task to outbox for offline-first delivery.
                viewModelScope.launch {
                    syncRepo.enqueueVerifyTask(event.taskId, event.reason, event.rowVersion)
                }
            }
            is RecordEvent.Rework -> {
                // Enqueue rework task to outbox for offline-first delivery.
                viewModelScope.launch {
                    syncRepo.enqueueReworkTask(event.taskId, event.reason, event.rowVersion)
                }
            }
        }
    }

    private fun VaccinationExecutionShedDrilldownDto.toRecordUiState(): RecordUiState {
        val base = sampleRecordState()
        val groups = drives.map { drive ->
            VaccineGroupRow(
                vaccine = drive.driveName ?: drive.driveId.orEmpty(),
                given = summary.completed,
                due = summary.total,
                dose = "",
            )
        }
        val complete = summary.total > 0 && summary.completed >= summary.total
        return base.copy(
            title = "$shedName · record",
            subtitle = "${summary.completed} / ${summary.total} done",
            // Real drives only — a shed with no drives renders empty, never the sample rows.
            groups = groups,
            countLabel = "${summary.completed} doses",
            statusLabel = if (complete) "Done" else "In progress",
            statusTone = if (complete) RecordTone.OK else RecordTone.WARN,
        )
    }

    // Honest non-live state: reuse the sample only for stable chrome, clear the fabricated
    // drive rows, and carry the real message in the subtitle.
    private fun recordPlaceholder(message: String): RecordUiState = sampleRecordState().copy(
        subtitle = message,
        groups = emptyList(),
        countLabel = "",
        statusLabel = "",
        statusTone = RecordTone.WARN,
    )
}
