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
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.core.ui.operationalLocationLabel
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
 * This surface is READ-ONLY and has NO verify/rework capability. Leadership verify/rework is
 * fully implemented in the VERIFY_DETAIL surface (VerifyDetailViewModel + approve/reject flow).
 * When a shed has due work but no scannable task yet (all rows lack sopTaskId), the record
 * honestly displays "No scannable task yet — awaiting task assignment" instead of a silent
 * dead-end, making clear why no scan/submit is possible.
 */
@HiltViewModel
class RecordViewModel @Inject constructor(
    private val repo: ExecutionRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val shedId: String? = savedStateHandle.get<String>("shedId")

    // Fires funnel_vaccination_capture_completed at most once per shed session, the first time
    // the drilldown reports the shed's vaccination work as fully done — see [toRecordUiState].
    private var captureCompletedTracked = false

    // Upstream Room flow, lifecycle-aware via WhileSubscribed(5_000)
    private val observedResource: StateFlow<Resource<VaccinationExecutionShedDrilldownDto>> =
        (if (shedId != null) repo.observeShed(shedId, partitionLabel = null) else flowOf(Resource(data = null))).stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null)
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
        shedId?.let {
            analytics.track(
                AnalyticsEvents.VACCINATION_RECORD_OPENED,
                mapOf(AnalyticsEvents.Params.SHED_ID to it),
            )
            // Answers: did this operator actually start looking at the shed's vaccination
            // capture, or did the screen never really open (distinguishes that from a scan that
            // starts but the record view is never reached).
            AnalyticsFunnels.trackVaccinationCaptureStarted(analytics, it)
        }
        refresh()
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observedResource]
     *  StateFlow re-emits and updates [state]); on failure it only flips [_isOffline] —
     *  cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        if (shedId != null) {
            val result = repo.refreshShed(shedId, partitionLabel = null)
            _isRefreshing.value = false
            _isOffline.value = result.isFailure
            result.exceptionOrNull()?.let {
                crashReporter.recordException(it, "vaccination record refresh failed")
            }
        } else {
            _isRefreshing.value = false
        }
    }

    fun onEvent(event: RecordEvent) {
        when (event) {
            RecordEvent.Close -> {
                // Track abandonment: if the vaccination capture was not marked as complete,
                // the user is leaving with work still pending.
                val dto = observedResource.value.data
                val complete = dto?.summary?.let { it.total > 0 && it.completed >= it.total } ?: false
                if (!complete && shedId != null && !captureCompletedTracked) {
                    captureCompletedTracked = true
                    AnalyticsFunnels.trackVaccinationCaptureCompleted(analytics, shedId, "abandon")
                }
            }
            RecordEvent.Refresh -> refresh()
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
        // Answers: did the vaccination-capture stage for this shed actually finish, vs. the
        // operator leaving the record screen with work still pending — fires once per session.
        if (complete && !captureCompletedTracked && shedId != null) {
            captureCompletedTracked = true
            AnalyticsFunnels.trackVaccinationCaptureCompleted(analytics, shedId, "done")
        }

        // BUG-001: Detect shed with due work but no scannable task (all rows have blank sopTaskId).
        // In this case, show an explicit "awaiting task assignment" message instead of a silent dead-end.
        val hasWorkDue = summary.total > summary.completed
        val allRowsLackTaskId = rows.isNotEmpty() && rows.all { it.sopTaskId.isNullOrBlank() }
        val hasNoScannableTask = hasWorkDue && allRowsLackTaskId

        // Operational location. Prefer the BACKEND-COMPOSED display so this screen cannot drift
        // from every other surface. The legacy `partition` field is deliberately NOT read: it
        // carries the 'whole' sentinel, which is a matching key and never user copy (its own DTO
        // comment says so). `partitionLabel` is the sanctioned raw field and is only used as a
        // fallback for older API responses that predate the composed value.
        val firstRow = rows.firstOrNull()
        val locationLabel = operationalLocationDisplay
            .ifBlank { firstRow?.operationalLocationDisplay.orEmpty() }
            .ifBlank { operationalLocationLabel(shedName, null) }
            .ifBlank { shedName }
        return base.copy(
            title = "$locationLabel · record",
            subtitle = "${summary.completed} / ${summary.total} done",
            // Real drives only — a shed with no drives renders empty, never the sample rows.
            groups = groups,
            countLabel = "${summary.completed} doses",
            statusLabel = when {
                hasNoScannableTask -> "No scannable task yet — awaiting task assignment"
                complete -> "Done"
                else -> "In progress"
            },
            statusTone = when {
                hasNoScannableTask -> RecordTone.WARN
                complete -> RecordTone.OK
                else -> RecordTone.WARN
            },
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
