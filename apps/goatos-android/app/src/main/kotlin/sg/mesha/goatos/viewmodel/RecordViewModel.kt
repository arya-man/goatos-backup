package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
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

    // Upstream Room flow, lifecycle-aware via WhileSubscribed(5_000)
    private val observedResource: StateFlow<Resource<VaccinationExecutionShedDrilldownDto>> =
        (if (shedId != null) repo.observeShed(shedId) else flowOf(Resource())).stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource()
        )

    // Transient flags for manual updates
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)

    // Combines observed resource with transient flags; lifecycle-aware
    val state: StateFlow<RecordUiState> = combine(
        observedResource,
        _isRefreshing,
        _isOffline
    ) { resource, isRefreshing, isOffline ->
        val dto = resource.data
        val base = dto?.toRecordUiState()
            ?: if (resource.hasData) recordPlaceholder("No record to show for this shed.") else recordPlaceholder("Loading…")
        base.copy(
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        recordPlaceholder("Loading record…")
    )

    init {
        refresh()
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observedResource]
     *  StateFlow re-emits and updates [state]); on failure it only flips [_isOffline] —
     *  cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        if (shedId != null) {
            val result = repo.refreshShed(shedId)
            _isRefreshing.value = false
            _isOffline.value = result.isFailure
        } else {
            _isRefreshing.value = false
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
