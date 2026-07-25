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
import kotlinx.coroutines.flow.scan
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CountsBreakdownQuery
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.network.dto.CountsBreakdownFacetsDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.feature.counts.CountsBreakdownRowUi
import sg.mesha.goatos.feature.counts.CountsEvent
import sg.mesha.goatos.feature.counts.CountsFilterOptionUi
import sg.mesha.goatos.feature.counts.CountsFiltersUi
import sg.mesha.goatos.feature.counts.CountsTotalsUi
import sg.mesha.goatos.feature.counts.CountsUiState
import javax.inject.Inject

/**
 * Counts census read screen state holder — the offline-first pattern
 * (docs/decisions/android-offline-first.md).
 *
 * Room is the UI's single source of truth. [state] is fed by
 * [CountsRepository.observeBreakdownTotals] and [CountsRepository.observeHerdSummary], which emit
 * instantly from Room (so a re-entry renders cached numbers, never a blank wall) and re-emit the
 * moment a background refresh upserts. [refresh] never writes into [state] directly — it drives
 * the network call and the transient refreshing/offline flags only.
 *
 * [rows] is a Paging 3 flow whose `RemoteMediator` fills Room page-by-page; the screen renders a
 * bounded window from Room, so neither the network nor the DB ever materializes the whole herd.
 *
 * KPI integrity: the totals rendered here come from the backend's whole-result envelope. They are
 * NEVER summed from the paged rows — a card computed from the ~20 rows currently in memory would
 * silently under-report the herd.
 */
@HiltViewModel
class CountsViewModel @Inject constructor(
    private val repo: CountsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    /**
     * The operator's current census filter selection — the ONE piece of screen state this holder
     * owns. Everything downstream (the totals envelope, the summary rollup, and the paged rows)
     * is derived from it with `flatMapLatest`, so applying a filter re-subscribes all three to the
     * new scope rather than mutating anything in place.
     *
     * A blank field means "no filter on this dimension", which is what [CountsBreakdownQuery]
     * turns into an omitted query parameter.
     */
    private val _filters = MutableStateFlow(CountsFilterSelection())

    /**
     * The breakdown envelope for the ACTIVE filter scope, carrying the last usable facet
     * vocabulary forward.
     *
     * The carry-forward is the point of the `scan`. Room caches the envelope per filter scope
     * (`CountsBreakdownQuery.roomKey()`), so the first time a scope is selected its cache is cold
     * and the envelope arrives null. Without carrying, every dropdown would empty itself the
     * instant a filter was applied — the operator would pick "CBE", watch the filter bar go blank,
     * and have nothing to pick a shed from. Facets describe the whole unfiltered herd vocabulary
     * and are explicitly NOT narrowed by the active filter (backend:
     * `countsBreakdownFacetsSQL`), so the previous scope's vocabulary is not a stale approximation
     * of the new one — it is the same vocabulary. An empty facet set never overwrites a populated
     * one.
     */
    @OptIn(ExperimentalCoroutinesApi::class)
    private val observedBreakdown: StateFlow<CountsBreakdownEnvelope> = _filters
        .flatMapLatest { selection -> repo.observeBreakdownTotals(selection.toQuery()) }
        .scan(CountsBreakdownEnvelope()) { carried, resource ->
            val fresh = resource.data?.facets
            CountsBreakdownEnvelope(
                resource = resource,
                facets = if (fresh != null && fresh.hasAnyDimension) fresh else carried.facets,
            )
        }
        .stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            CountsBreakdownEnvelope(),
        )

    /**
     * The herd-register rollup, scoped to the filters this endpoint actually supports.
     *
     * It accepts `park_id` and `breed` but has no shed dimension, which is why the untagged-kid
     * label it feeds is SUPPRESSED while a shed filter is active (see [untaggedKidLabel]) — a
     * park-wide gap count printed next to a single shed's totals would be read as that shed's.
     */
    @OptIn(ExperimentalCoroutinesApi::class)
    private val observedSummary: StateFlow<Resource<HerdRegisterSummaryResponseDto>> = _filters
        .flatMapLatest { selection ->
            repo.observeHerdSummary(
                parkId = selection.parkId.takeIf { it.isNotBlank() },
                breed = selection.breed.takeIf { it.isNotBlank() },
            )
        }
        .stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null),
        )

    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)

    val state: StateFlow<CountsUiState> = combine(
        observedBreakdown,
        observedSummary,
        _filters,
        _isRefreshing,
        _isOffline,
    ) { breakdown, summaryResource, selection, isRefreshing, isOffline ->
        val totalsResource = breakdown.resource
        val totalsDto = totalsResource.data
        val hasTotals = totalsDto != null
        CountsUiState(
            title = TITLE,
            scopeLabel = summaryResource.data?.untaggedKidLabel(selection).orEmpty(),
            totals = totalsDto?.toTotalsUi() ?: CountsTotalsUi(),
            hasTotals = hasTotals,
            filters = breakdown.facets.toFiltersUi(selection),
            // Distinguishes an honest zero ("synced, nothing matches") from a cold-cache failure
            // ("never synced AND the refresh failed") from a genuine first load. None is a blank
            // wall, and none of them hide cached rows when cached rows exist.
            emptyMessage = when {
                hasTotals && totalsDto.totalRows == 0 -> EMPTY_MESSAGE
                !hasTotals && isOffline -> ERROR_MESSAGE
                !hasTotals -> LOADING_MESSAGE
                else -> null
            },
            isErrorEmpty = !hasTotals && isOffline,
            isRefreshing = isRefreshing,
            lastSyncedAt = totalsResource.lastSyncedAt ?: summaryResource.lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        CountsUiState(title = TITLE, emptyMessage = LOADING_MESSAGE),
    )

    /**
     * The paged breakdown. `cachedIn` keeps loaded pages alive across configuration changes
     * without re-fetching. The DTO -> UI transform runs inside the Paging pipeline the repository
     * already put on `Dispatchers.Default`, so each field is parsed exactly once, off Main.
     */
    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<CountsBreakdownRowUi>> =
        _filters
            // Applying a filter swaps in a NEW Pager for the new scope, which resets pagination to
            // the first page on both layers at once: the RemoteMediator issues a REFRESH at
            // offset 0, and the Room PagingSource it reads is keyed by that scope's own
            // `roomKey()`. Rows from the previous scope can never be shown under the new filter,
            // and no page offset survives the change.
            .flatMapLatest { selection -> repo.breakdownRows(selection.toQuery()) }
            .map { page ->
                page.map { dto ->
                    CountsBreakdownRowUi(
                        grainKey = dto.grainKey,
                        farmLabel = dto.parkLabel,
                        shedLabel = dto.shedLabel,
                        managementStage = dto.managementStage,
                        breed = dto.breed,
                        sex = dto.sex,
                        count = dto.count,
                        shedId = dto.shedId.orEmpty(),
                    )
                }
            }
            .cachedIn(viewModelScope)

    init {
        analytics.track(AnalyticsEvents.COUNTS_VIEWED)
        refresh()
    }

    /**
     * Network half of stale-while-revalidate for the summary rollup. The breakdown's refresh is
     * owned by Paging's `RemoteMediator`; driving it from here too would double-fetch its first
     * page on every screen open.
     */
    fun refresh() = viewModelScope.launch {
        val selection = _filters.value
        _isRefreshing.value = true
        // Refreshes the scope the operator is actually looking at, not the whole herd — otherwise
        // the manual Refresh on a filtered census would revalidate a rollup that is not on screen.
        val result = repo.refreshHerdSummary(
            parkId = selection.parkId.takeIf { it.isNotBlank() },
            breed = selection.breed.takeIf { it.isNotBlank() },
        )
        _isRefreshing.value = false
        _isOffline.value = result.isFailure
        result.exceptionOrNull()?.let { error ->
            crashReporter.recordException(error, "counts herd summary refresh failed")
            analytics.track(
                AnalyticsEvents.COUNTS_READ_FAILURE,
                mapOf(
                    AnalyticsEvents.Params.KIND to "herd_summary",
                    AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                ),
            )
        }
    }

    /** Reports a Paging load failure. Cached rows keep rendering either way. */
    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "counts breakdown page load failed")
        analytics.track(
            AnalyticsEvents.COUNTS_READ_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to "breakdown",
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
            ),
        )
    }

    fun onEvent(event: CountsEvent) {
        when (event) {
            CountsEvent.Refresh -> refresh()
            is CountsEvent.SelectPark -> selectPark(event.parkId)
            is CountsEvent.SelectShed -> selectShed(event.shedId)
            is CountsEvent.SelectBreed -> selectBreed(event.breed)
            is CountsEvent.SelectLifecycle -> selectLifecycle(event.lifecycle)
            CountsEvent.ClearFilters -> clearFilters()
        }
    }

    /**
     * Applies a park filter (blank = "All parks") and RESETS the shed.
     *
     * The reset is a correctness requirement, not tidiness: a shed belongs to exactly one park, so
     * a shed id carried across a park change filters the census to a cohort in the park the
     * operator just navigated away from. It is silent because it is the direct consequence of the
     * one action the operator took — a second "shed cleared" analytics event would double-count a
     * single interaction.
     */
    private fun selectPark(parkId: String) {
        val current = _filters.value
        if (current.parkId == parkId) return
        _filters.value = current.copy(parkId = parkId, shedId = "")
        trackFilter(DIMENSION_PARK, parkId)
    }

    private fun selectShed(shedId: String) {
        val current = _filters.value
        if (current.shedId == shedId) return
        _filters.value = current.copy(shedId = shedId)
        trackFilter(DIMENSION_SHED, shedId)
    }

    private fun selectBreed(breed: String) {
        val current = _filters.value
        if (current.breed == breed) return
        _filters.value = current.copy(breed = breed)
        trackFilter(DIMENSION_BREED, breed)
    }

    /**
     * Applies a lifecycle filter (blank = backend default, the live herd).
     *
     * Deliberately does NOT reset park/shed/breed — lifecycle is an independent dimension, unlike
     * park->shed which cascades. Choosing "Sold" while a park is selected still means "sold
     * animals in that park", not a reset back to the whole herd.
     */
    private fun selectLifecycle(lifecycle: String) {
        val current = _filters.value
        if (current.lifecycleStatus == lifecycle) return
        _filters.value = current.copy(lifecycleStatus = lifecycle)
        trackFilter(DIMENSION_LIFECYCLE, lifecycle)
    }

    private fun clearFilters() {
        if (!_filters.value.hasAnyFilter) return
        _filters.value = CountsFilterSelection()
        trackFilter(DIMENSION_ALL, value = "")
    }

    /** A blank value is a CLEAR of that dimension; anything else is a selection. */
    private fun trackFilter(dimension: String, value: String) {
        analytics.track(
            AnalyticsEvents.COUNTS_FILTER_APPLIED,
            mapOf(
                AnalyticsEvents.Params.DIMENSION to dimension,
                AnalyticsEvents.Params.ACTION to if (value.isBlank()) ACTION_CLEARED else ACTION_SET,
            ),
        )
    }

    /**
     * Projects the backend facet vocabulary into the filter bar's option lists.
     *
     * Two things are deliberate here. Every option keeps the facet's own `key` as its identity —
     * a park/shed uuid, or the breed's own value — because shed LABELS repeat across parks and a
     * label-keyed option would filter to the wrong shed. And the shed list is narrowed to the
     * SELECTED park: an entry with no park attribution is offered under no park at all rather than
     * under the wrong one, which is also what makes an unattributed `sheds` facet degrade to a
     * disabled dropdown instead of an ambiguous one.
     */
    private fun CountsBreakdownFacetsDto.toFiltersUi(selection: CountsFilterSelection): CountsFiltersUi {
        val parkOptions = parks.map { CountsFilterOptionUi(it.key, it.label, it.count) }
        val breedOptions = breeds.map { CountsFilterOptionUi(it.key, it.label, it.count) }
        val lifecycleOptions = lifecycle.map { CountsFilterOptionUi(it.key, it.label, it.count) }
        // The shed dropdown is cascaded to the selected park for correctness: shed names repeat
        // across parks, so a flat list is ambiguous. Selecting a park resets the shed, and a shed
        // id left over from another park would filter to the wrong cohort.
        val shedOptions = sheds
            .filter { it.parkId.isNotBlank() && it.parkId == selection.parkId }
            .map { CountsFilterOptionUi(it.key, it.label, it.count) }
        // The shed subtotals are the FULL shed list, not narrowed by park. The subtotal divider
        // renders a shed's head count regardless of the current park selection, so it looks up from
        // this full list. On the all-parks view, this allows subtotals to render even when
        // shedOptions is empty (because no park is selected).
        val shedSubtotals = sheds.map { CountsFilterOptionUi(it.key, it.label, it.count) }
        return CountsFiltersUi(
            parks = parkOptions,
            sheds = shedOptions,
            breeds = breedOptions,
            lifecycles = lifecycleOptions,
            selectedParkId = selection.parkId,
            selectedShedId = selection.shedId,
            selectedBreed = selection.breed,
            selectedLifecycle = selection.lifecycleStatus,
            // Resolved here so the screen never has to map a key back to a label. Null when
            // nothing is selected, which is what makes the field render its "All …" placeholder.
            selectedParkLabel = parkOptions.firstOrNull { it.key == selection.parkId }?.label,
            selectedShedLabel = shedOptions.firstOrNull { it.key == selection.shedId }?.label,
            selectedBreedLabel = breedOptions.firstOrNull { it.key == selection.breed }?.label,
            selectedLifecycleLabel = lifecycleOptions.firstOrNull { it.key == selection.lifecycleStatus }?.label,
            // Supported only once the backend ships sheds WITH park attribution — without it the
            // cascade cannot be built and the dropdown stays disabled rather than ambiguous.
            shedFilterSupported = sheds.any { it.parkId.isNotBlank() },
            shedSubtotals = shedSubtotals,
        )
    }

    private fun CountsBreakdownResponseDto.toTotalsUi(): CountsTotalsUi = CountsTotalsUi(
        totalCount = totalCount,
        totalKids = totalKids,
        totalAdults = totalAdults,
        totalRows = totalRows,
        projectedAt = projectedAt,
    )

    /**
     * Untagged kids are the census's own data-gap signal — animals counted but not yet
     * identifiable, which is exactly what a Counts operator needs to see next to the totals.
     * Summed across the rollup's scope grains (a fixed-size list, not a page), and omitted
     * entirely when there are none rather than showing a decorative zero.
     *
     * SUPPRESSED while a shed filter is active. `/herd-register/summary` has no shed dimension, so
     * the only honest options are to hide the number or to print a park-wide count beside a single
     * shed's totals, where it would be read as that shed's. It comes back the moment the shed
     * filter is cleared.
     */
    private fun HerdRegisterSummaryResponseDto.untaggedKidLabel(selection: CountsFilterSelection): String? {
        if (selection.shedId.isNotBlank()) return null
        val untagged = items.sumOf { it.untaggedKidCount }
        return if (untagged > 0) "$untagged untagged kids" else null
    }

    /**
     * The active census filter scope.
     *
     * A blank field is "no filter on this dimension" and becomes an OMITTED query parameter — the
     * distinction matters because an empty-string `breed` would filter to animals whose breed is
     * literally blank, which is a real and very different cohort.
     */
    private data class CountsFilterSelection(
        val parkId: String = "",
        val shedId: String = "",
        val breed: String = "",
        /** Blank = backend default (the live herd); never sent as an explicit "" filter. */
        val lifecycleStatus: String = "",
    ) {
        val hasAnyFilter: Boolean
            get() = parkId.isNotBlank() || shedId.isNotBlank() || breed.isNotBlank() ||
                lifecycleStatus.isNotBlank()

        fun toQuery(): CountsBreakdownQuery = CountsBreakdownQuery(
            parkId = parkId.takeIf { it.isNotBlank() },
            shedId = shedId.takeIf { it.isNotBlank() },
            breed = breed.takeIf { it.isNotBlank() },
            lifecycleStatus = lifecycleStatus.takeIf { it.isNotBlank() },
        )
    }

    /**
     * The active scope's envelope plus the last usable facet vocabulary. Pairing them is what lets
     * the filter bar keep its options while a newly-selected scope's cache is still cold — see
     * [observedBreakdown].
     */
    private data class CountsBreakdownEnvelope(
        val resource: Resource<CountsBreakdownResponseDto> = Resource(data = null),
        val facets: CountsBreakdownFacetsDto = CountsBreakdownFacetsDto(),
    )

    private companion object {
        const val TITLE = "Counts"
        const val LOADING_MESSAGE = "Loading counts…"
        const val EMPTY_MESSAGE = "No animals match this scope"
        const val ERROR_MESSAGE = "Couldn't load counts. Tap refresh to retry."

        const val DIMENSION_PARK = "park"
        const val DIMENSION_SHED = "shed"
        const val DIMENSION_BREED = "breed"
        const val DIMENSION_LIFECYCLE = "lifecycle"
        const val DIMENSION_ALL = "all"
        const val ACTION_SET = "set"
        const val ACTION_CLEARED = "cleared"
    }
}
