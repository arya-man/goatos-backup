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
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.AwaitingRfidFilter
import sg.mesha.goatos.core.data.AwaitingRfidRepository
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.TemporaryTaggedGoatDto
import sg.mesha.goatos.feature.counts.AwaitingRfidEvent
import sg.mesha.goatos.feature.counts.AwaitingRfidRowUi
import sg.mesha.goatos.feature.counts.AwaitingRfidUiState
import sg.mesha.goatos.feature.counts.ShiftingParkUi
import javax.inject.Inject

/**
 * The "Awaiting RFID" list — the operator's queue of goats still carrying a temporary tag, waiting to
 * be promoted to a permanent RFID (`GET /app/counts/goats/temporary-tagged`).
 *
 * Offline-first (docs/decisions/android-offline-first.md): [rows] is a Paging 3 flow whose
 * RemoteMediator fills Room page-by-page and the UI observes Room, so re-entering the list renders the
 * cached rows immediately behind a sync indicator rather than a blank wall.
 *
 * Location filter (park -> shed cascade): the vocabulary is the backend-owned shifting-destinations
 * catalog — the same one the birth/shifting pickers use — so this never invents parks or sheds. The
 * filter narrows the list SERVER-SIDE: a change re-creates the Pager ([flatMapLatest]), which triggers
 * a REFRESH that swaps the cached page for the new location's page. Both the network fetch and the
 * observed Room read stay bounded to one keyset page (docs/decisions/mobile-data-fetch-anti-patterns.md).
 */
@OptIn(ExperimentalCoroutinesApi::class)
@HiltViewModel
class AwaitingRfidViewModel @Inject constructor(
    private val repo: AwaitingRfidRepository,
    private val countsRepository: CountsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val _isOffline = MutableStateFlow(false)
    private val _lastSyncedAt = MutableStateFlow<Long?>(null)

    // Location filter selection (ids from the destinations catalog). Empty = unfiltered.
    private val _parkId = MutableStateFlow("")
    private val _shedId = MutableStateFlow("")
    // The park/shed vocabulary, cached and re-emitted from the destinations catalog.
    private val _destinationParks = MutableStateFlow<List<ShiftingParkUi>>(emptyList())

    private val filter: Flow<AwaitingRfidFilter> =
        combine(_parkId, _shedId) { park, shed ->
            AwaitingRfidFilter(parkId = park.ifBlank { null }, shedId = shed.ifBlank { null })
        }.distinctUntilChanged()

    val rows: Flow<PagingData<AwaitingRfidRowUi>> = filter
        .flatMapLatest { active -> repo.awaiting(active).map { page -> page.map { it.toRowUi() } } }
        .cachedIn(viewModelScope)

    val state: StateFlow<AwaitingRfidUiState> = combine(
        _isOffline,
        _lastSyncedAt,
        _parkId,
        _shedId,
        _destinationParks,
    ) { isOffline, lastSyncedAt, parkId, shedId, parks ->
        val filtered = parkId.isNotBlank() || shedId.isNotBlank()
        AwaitingRfidUiState(
            emptyMessage = when {
                isOffline -> OFFLINE_EMPTY
                filtered -> FILTERED_EMPTY
                else -> EMPTY_MESSAGE
            },
            isErrorEmpty = isOffline,
            isRefreshing = false,
            lastSyncedAt = lastSyncedAt,
            isOffline = isOffline,
            destinationParks = parks,
            parkId = parkId,
            shedId = shedId,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), AwaitingRfidUiState())

    init {
        analytics.track(AnalyticsEvents.COUNTS_AWAITING_RFID_VIEWED)
        observeDestinations()
        refreshDestinations()
    }

    /**
     * Renders the cached destinations catalog immediately and re-renders whenever a refresh upserts
     * Room. A refresh that drops the currently-chosen park or shed re-validates the selection and
     * clears what is gone, so the filter can never name a location the catalog no longer offers.
     */
    private fun observeDestinations() {
        viewModelScope.launch {
            countsRepository.observeShiftingDestinations().collect { resource ->
                val parks = resource.data?.parks?.map(CountsDestinationParkDto::toShiftingParkUi).orEmpty()
                _destinationParks.value = parks
                // Re-validate the current selection against the fresh catalog.
                val parkStillOffered = parks.any { it.parkId == _parkId.value }
                if (_parkId.value.isNotBlank() && !parkStillOffered) {
                    _parkId.value = ""
                    _shedId.value = ""
                } else {
                    val shedStillOffered = parks
                        .firstOrNull { it.parkId == _parkId.value }
                        ?.sheds
                        ?.any { it.shedId == _shedId.value } == true
                    if (_shedId.value.isNotBlank() && !shedStillOffered) {
                        _shedId.value = ""
                    }
                }
            }
        }
    }

    private fun refreshDestinations() {
        viewModelScope.launch {
            countsRepository.refreshShiftingDestinations()
                .onFailure { error ->
                    crashReporter.recordException(error, "awaiting rfid destinations refresh failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "awaiting_rfid_destinations",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                }
        }
    }

    fun onRowsLoadFailed(error: Throwable) {
        _isOffline.value = true
        crashReporter.recordException(error, "awaiting rfid page load failed")
        analytics.track(
            AnalyticsEvents.COUNTS_READ_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to "awaiting_rfid",
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
            ),
        )
    }

    fun onRowsLoaded() {
        _isOffline.value = false
        _lastSyncedAt.value = System.currentTimeMillis()
    }

    fun onEvent(event: AwaitingRfidEvent) {
        when (event) {
            AwaitingRfidEvent.Back -> Unit // navigation — handled by the nav host.
            is AwaitingRfidEvent.OpenGoat -> Unit // navigation — handled by the nav host.
            // Choosing a park RESETS the shed: a shed id belongs to exactly one park, so carrying the
            // old shed forward would filter on a park/shed pairing that does not exist.
            is AwaitingRfidEvent.SelectPark -> {
                if (_parkId.value != event.parkId) {
                    _parkId.value = event.parkId
                    _shedId.value = ""
                }
            }
            is AwaitingRfidEvent.SelectShed -> {
                // Guard the pairing: only a shed that belongs to the chosen park applies.
                val belongs = _destinationParks.value
                    .firstOrNull { it.parkId == _parkId.value }
                    ?.sheds
                    ?.any { it.shedId == event.shedId } == true
                if (belongs) _shedId.value = event.shedId
            }
            AwaitingRfidEvent.ClearFilter -> {
                _parkId.update { "" }
                _shedId.update { "" }
            }
        }
    }

    private fun TemporaryTaggedGoatDto.toRowUi(): AwaitingRfidRowUi = AwaitingRfidRowUi(
        goatId = goatId,
        displayId = displayId,
        temporaryIdentifier = temporaryIdentifier,
        locationDisplay = locationDisplay,
    )

    private companion object {
        const val EMPTY_MESSAGE = "No goats are waiting for a permanent RFID. Temporary-tagged kids show up here to promote."
        const val FILTERED_EMPTY = "No goats are waiting for a permanent RFID in this location. Try clearing the filter."
        const val OFFLINE_EMPTY = "Couldn't load the awaiting-RFID list. It will appear once you're back online."
    }
}
