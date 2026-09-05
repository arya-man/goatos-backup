package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsVendors
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.VendorRegisterSide
import sg.mesha.goatos.core.data.VendorsRepository
import sg.mesha.goatos.core.network.dto.VendorDto
import sg.mesha.goatos.feature.vendors.VendorCardUi
import sg.mesha.goatos.feature.vendors.VendorsFilterUi
import sg.mesha.goatos.feature.vendors.VendorsListEvent
import sg.mesha.goatos.feature.vendors.VendorsListUiState
import javax.inject.Inject

/**
 * The vendor register's list state holder (maintainer decisions 2026-09-03 and 2026-09-05).
 * Offline-first: rows come from the Room-backed Pager in [VendorsRepository.vendors]; the cached
 * ~20-row window renders instantly and the network refresh writes THROUGH Room. Every visible word
 * on a card is backend-owned and passed through verbatim.
 *
 * ONE holder serves BOTH halves of the register — Procurement > Vendors and Sales > Vendors. The
 * SIDE arrives through [bind], from the route the screen was registered on, because which half is
 * on screen is a fact about where the person tapped and not something the payload can say. Nothing
 * is read until it arrives: a default would fetch the buying desk's rows for a frame on the
 * selling page.
 */
@HiltViewModel
class VendorsListViewModel @Inject constructor(
    private val repository: VendorsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Scope(
        /** Null until [bind] names the register half; nothing is read before then. */
        val side: VendorRegisterSide? = null,
        val search: String = "",
        /** Selected status VALUE; "" = every status. */
        val status: String = "",
        val title: String = "",
        val refreshNonce: Int = 0,
    )

    private val scope = MutableStateFlow(Scope())
    private val typed = MutableStateFlow("")
    private val _isRefreshing = MutableStateFlow(false)

    init {
        // The search box is debounced HERE so a typist does not fire a network page per keystroke.
        viewModelScope.launch {
            @OptIn(FlowPreview::class)
            typed.debounce(SEARCH_DEBOUNCE_MS).distinctUntilChanged().collect { text ->
                if (scope.value.search != text) {
                    scope.value = scope.value.copy(search = text)
                    analytics.track(AnalyticsEventsVendors.VENDORS_LIST_FILTERED, mapOf(AnalyticsEvents.Params.REASON to "search"))
                }
            }
        }
    }

    /** Names the register half this screen is showing, and the backend nav label above it. */
    fun bind(side: VendorRegisterSide, title: String) {
        if (scope.value.side == side && scope.value.title == title) return
        scope.value = scope.value.copy(side = side, title = title)
        viewModelScope.launch { repository.refreshCatalog(side) }
        analytics.track(AnalyticsEventsVendors.VENDORS_LIST_VIEWED, mapOf(AnalyticsEvents.Params.REASON to side.wireValue))
    }

    /** The bound side, once [bind] has named one. Both reads below hang off it. */
    private val boundSide = scope.map { it.side }.distinctUntilChanged()

    /** The bound side's vocabulary; empty until a side is bound, never the other side's. */
    @OptIn(ExperimentalCoroutinesApi::class)
    private val catalog = boundSide
        .flatMapLatest { side -> if (side == null) flowOf(null) else repository.observeCatalog(side) }

    /** That side's whole-filter count, never the sibling tab's. */
    @OptIn(ExperimentalCoroutinesApi::class)
    private val total = boundSide
        .flatMapLatest { side -> if (side == null) flowOf(0) else repository.vendorTotal(side) }

    val state: StateFlow<VendorsListUiState> = combine(
        _isRefreshing,
        scope,
        typed,
        catalog,
        total,
    ) { refreshing, current, text, catalog, total ->
        val statuses = catalog?.statuses.orEmpty().filter { it.isActive }
        VendorsListUiState(
            title = current.title,
            isRefreshing = refreshing,
            search = text,
            filters = listOf(VendorsFilterUi(key = "", label = FILTER_ALL, selected = current.status.isBlank())) +
                statuses.map { VendorsFilterUi(key = it.value, label = it.label, selected = it.value == current.status) },
            countLine = if (total > 0) "$total ${if (total == 1) COUNT_ONE else COUNT_MANY}" else "",
            emptyMessage = if (current.search.isNotBlank() || current.status.isNotBlank()) EMPTY_FILTERED else EMPTY_MESSAGE,
            canAdd = true,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VendorsListUiState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<VendorCardUi>> = scope
        .flatMapLatest { current ->
            val side = current.side ?: return@flatMapLatest flowOf(PagingData.empty<VendorCardUi>())
            repository.vendors(side, current.search, current.status).map { page -> page.map { it.toCardUi() } }
        }
        .cachedIn(viewModelScope)

    fun onEvent(event: VendorsListEvent) {
        when (event) {
            VendorsListEvent.Refresh -> refresh()
            is VendorsListEvent.SearchChanged -> typed.value = event.text
            is VendorsListEvent.SelectStatus -> selectStatus(event.key)
            is VendorsListEvent.OpenVendor -> analytics.track(AnalyticsEventsVendors.VENDORS_VENDOR_OPENED)
            VendorsListEvent.AddVendor -> analytics.track(AnalyticsEventsVendors.VENDORS_ADD_OPENED)
        }
    }

    private fun selectStatus(key: String) {
        if (scope.value.status == key) return
        scope.value = scope.value.copy(status = key)
        analytics.track(AnalyticsEventsVendors.VENDORS_LIST_FILTERED, mapOf(AnalyticsEvents.Params.REASON to key.ifBlank { "all" }))
    }

    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "vendor list page load failed")
        analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS)))
    }

    /** Non-blocking by contract: a failure leaves the cached rows on screen. */
    private fun refresh() {
        viewModelScope.launch {
            _isRefreshing.value = true
            val side = scope.value.side
            try {
                if (side != null) {
                    // exception:exempt local cache-marker delete; a failure just leaves the TTL skip
                    runCatching { repository.invalidateVendors(side, scope.value.search, scope.value.status) }
                    repository.refreshCatalog(side)
                }
                scope.value = scope.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    private companion object {
        const val SEARCH_DEBOUNCE_MS = 350L
        const val MAX_REASON_CHARS = 120
        const val FILTER_ALL = "All"
        const val COUNT_ONE = "vendor"
        const val COUNT_MANY = "vendors"
        const val EMPTY_MESSAGE = "No vendors recorded yet"
        const val EMPTY_FILTERED = "No vendors match this search"
    }
}

/** Maps one backend register row to its list card. Backend copy is rendered verbatim. */
internal fun VendorDto.toCardUi(): VendorCardUi = VendorCardUi(
    listKey = vendorId,
    vendorId = vendorId,
    name = displayName.ifBlank { businessName },
    typeLine = typeLine(),
    capacityLine = capacityDisplay,
    statusLabel = statusLabel.ifBlank { status },
    statusTone = vendorStatusTone(status),
    hasVoiceNote = !voiceNoteProofRef.isNullOrBlank(),
)
