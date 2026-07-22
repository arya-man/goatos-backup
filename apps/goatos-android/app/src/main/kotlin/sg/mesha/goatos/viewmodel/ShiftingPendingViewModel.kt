package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto
import sg.mesha.goatos.feature.counts.ShiftingPendingEvent
import sg.mesha.goatos.feature.counts.ShiftingPendingFilterOption
import sg.mesha.goatos.feature.counts.ShiftingPendingFilterUi
import sg.mesha.goatos.feature.counts.ShiftingPendingRowUi
import sg.mesha.goatos.feature.counts.ShiftingPendingUiState
import java.util.Locale
import javax.inject.Inject

/**
 * The Shifting "Pending" tab — the operator's queue of web-approved movements awaiting execution
 * (`GET /app/counts/shifting-events/pending-execution`).
 *
 * Offline-first (docs/decisions/android-offline-first.md): [rows] is a Paging 3 flow whose
 * RemoteMediator fills Room page-by-page and the UI observes Room, so re-entering the tab renders
 * the cached queue immediately. The farm -> shed filter narrows by the movement's SOURCE location;
 * its vocabulary is the backend destinations catalog (the same parks/sheds physically exist, since a
 * movement stays within one park), never a list this screen invents.
 */
@HiltViewModel
class ShiftingPendingViewModel @Inject constructor(
    private val repo: ShiftingPendingRepository,
    private val countsRepository: CountsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val _filters = MutableStateFlow(Selection())
    private val _catalog = MutableStateFlow(CountsShiftingDestinationsResponseDto())
    private val _isOffline = MutableStateFlow(false)
    private val _lastSyncedAt = MutableStateFlow<Long?>(null)

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<ShiftingPendingRowUi>> = _filters
        .flatMapLatest { selection -> repo.pending(parkId = selection.parkId, shedId = selection.shedId) }
        .map { page -> page.map { it.toRowUi() } }
        .cachedIn(viewModelScope)

    val state: StateFlow<ShiftingPendingUiState> = combine(
        _filters,
        _catalog,
        _isOffline,
        _lastSyncedAt,
    ) { selection, catalog, isOffline, lastSyncedAt ->
        ShiftingPendingUiState(
            filters = catalog.toFilterUi(selection),
            emptyMessage = if (isOffline) OFFLINE_EMPTY else EMPTY_MESSAGE,
            isErrorEmpty = isOffline,
            isRefreshing = false,
            lastSyncedAt = lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ShiftingPendingUiState())

    init {
        analytics.track(AnalyticsEvents.COUNTS_SHIFTING_PENDING_VIEWED)
        observeCatalog()
        refreshCatalog()
    }

    fun onRowsLoadFailed(error: Throwable) {
        _isOffline.value = true
        crashReporter.recordException(error, "shifting pending page load failed")
        analytics.track(
            AnalyticsEvents.COUNTS_READ_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to "shifting_pending",
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
            ),
        )
    }

    fun onRowsLoaded() {
        _isOffline.value = false
        _lastSyncedAt.value = System.currentTimeMillis()
    }

    fun onEvent(event: ShiftingPendingEvent) {
        when (event) {
            ShiftingPendingEvent.Refresh -> _filters.value = _filters.value.copy()
            is ShiftingPendingEvent.SelectPark -> selectPark(event.parkId)
            is ShiftingPendingEvent.SelectShed -> selectShed(event.shedId)
            ShiftingPendingEvent.ClearFilters -> clearFilters()
            is ShiftingPendingEvent.OpenMovement -> Unit // navigation — handled by the nav host.
        }
    }

    private fun selectPark(parkId: String) {
        val current = _filters.value
        if (current.parkId == parkId) return
        // A shed belongs to exactly one park, so a park change RESETS the shed.
        _filters.value = current.copy(parkId = parkId, shedId = "")
        trackFilter(DIMENSION_FARM, parkId)
    }

    private fun selectShed(shedId: String) {
        val current = _filters.value
        if (current.shedId == shedId) return
        _filters.value = current.copy(shedId = shedId)
        trackFilter(DIMENSION_SHED, shedId)
    }

    private fun clearFilters() {
        val current = _filters.value
        if (current.parkId.isBlank() && current.shedId.isBlank()) return
        _filters.value = Selection()
        trackFilter(DIMENSION_ALL, "")
    }

    private fun trackFilter(dimension: String, value: String) {
        analytics.track(
            AnalyticsEvents.COUNTS_SHIFTING_PENDING_FILTER_APPLIED,
            mapOf(
                AnalyticsEvents.Params.DIMENSION to dimension,
                AnalyticsEvents.Params.ACTION to if (value.isBlank()) ACTION_CLEARED else ACTION_SET,
            ),
        )
    }

    private fun observeCatalog() {
        viewModelScope.launch {
            countsRepository.observeShiftingDestinations().collect { resource ->
                resource.data?.let { _catalog.value = it }
            }
        }
    }

    private fun refreshCatalog() {
        viewModelScope.launch {
            countsRepository.refreshShiftingDestinations().onFailure { error ->
                crashReporter.recordException(error, "shifting pending filter catalog refresh failed")
            }
        }
    }

    private fun CountsShiftingDestinationsResponseDto.toFilterUi(selection: Selection): ShiftingPendingFilterUi {
        val parkOptions = parks.map { ShiftingPendingFilterOption(it.parkId, it.name) }
        val shedOptions = parks.firstOrNull { it.parkId == selection.parkId }
            ?.sheds
            ?.map { ShiftingPendingFilterOption(it.shedId, it.name) }
            .orEmpty()
        return ShiftingPendingFilterUi(
            parks = parkOptions,
            selectedParkId = selection.parkId,
            selectedParkLabel = parkOptions.firstOrNull { it.key == selection.parkId }?.label,
            sheds = shedOptions,
            selectedShedId = selection.shedId,
            selectedShedLabel = shedOptions.firstOrNull { it.key == selection.shedId }?.label,
        )
    }

    private fun CountsShiftingPendingExecutionItemDto.toRowUi(): ShiftingPendingRowUi = ShiftingPendingRowUi(
        shiftingEventId = shiftingEventId,
        sourceLabel = (sourceShedName ?: sourceParkName)?.takeIf { it.isNotBlank() } ?: UNKNOWN_LOCATION,
        destinationLabel = destinationShedName.takeIf { it.isNotBlank() }
            ?: destinationParkName.takeIf { it.isNotBlank() } ?: UNKNOWN_LOCATION,
        priority = priority.titleCase(),
        category = category.titleCase(),
        animalCount = animalCount,
        // approvedAtIst is already an Asia/Kolkata RFC3339 string; take the date portion for a stable,
        // timezone-safe label without re-parsing an instant on device.
        approvedAtLabel = approvedAtIst?.take(10).orEmpty(),
    )

    private fun String.titleCase(): String =
        if (isEmpty()) this else replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() }

    private data class Selection(val parkId: String = "", val shedId: String = "")

    private companion object {
        const val EMPTY_MESSAGE = "No approved movements waiting. Approved shiftings show up here to execute."
        const val OFFLINE_EMPTY = "Couldn't load the pending queue. It will appear once you're back online."
        const val UNKNOWN_LOCATION = "—"
        const val DIMENSION_FARM = "farm"
        const val DIMENSION_SHED = "shed"
        const val DIMENSION_ALL = "all"
        const val ACTION_SET = "set"
        const val ACTION_CLEARED = "cleared"
    }
}
