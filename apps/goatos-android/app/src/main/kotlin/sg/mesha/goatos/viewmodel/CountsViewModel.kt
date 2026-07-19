package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CountsBreakdownQuery
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.feature.counts.CountsBreakdownRowUi
import sg.mesha.goatos.feature.counts.CountsEvent
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
     * The unfiltered live-herd scope. Filters are a follow-up: the backend already accepts them
     * and [CountsBreakdownQuery] already models them, so adding a filter bar changes only the
     * query held here — cache keying and paging are scope-aware already.
     */
    private val query = CountsBreakdownQuery()

    private val observedTotals: StateFlow<Resource<CountsBreakdownResponseDto>> =
        repo.observeBreakdownTotals(query).stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null),
        )

    private val observedSummary: StateFlow<Resource<HerdRegisterSummaryResponseDto>> =
        repo.observeHerdSummary().stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null),
        )

    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)

    val state: StateFlow<CountsUiState> = combine(
        observedTotals,
        observedSummary,
        _isRefreshing,
        _isOffline,
    ) { totalsResource, summaryResource, isRefreshing, isOffline ->
        val totalsDto = totalsResource.data
        val hasTotals = totalsDto != null
        CountsUiState(
            title = TITLE,
            scopeLabel = summaryResource.data?.untaggedKidLabel().orEmpty(),
            totals = totalsDto?.toTotalsUi() ?: CountsTotalsUi(),
            hasTotals = hasTotals,
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
    val rows: Flow<PagingData<CountsBreakdownRowUi>> =
        repo.breakdownRows(query)
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
        _isRefreshing.value = true
        val result = repo.refreshHerdSummary()
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
        }
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
     */
    private fun HerdRegisterSummaryResponseDto.untaggedKidLabel(): String? {
        val untagged = items.sumOf { it.untaggedKidCount }
        return if (untagged > 0) "$untagged untagged kids" else null
    }

    private companion object {
        const val TITLE = "Counts"
        const val LOADING_MESSAGE = "Loading counts…"
        const val EMPTY_MESSAGE = "No animals match this scope"
        const val ERROR_MESSAGE = "Couldn't load counts. Tap refresh to retry."
    }
}
