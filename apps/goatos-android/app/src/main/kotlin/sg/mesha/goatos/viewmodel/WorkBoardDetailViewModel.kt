package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsWorkBoard
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.WorkBoardRepository
import sg.mesha.goatos.core.data.WorkBoardSubtaskRequest
import sg.mesha.goatos.core.network.dto.WorkBoardSubtaskDto
import sg.mesha.goatos.core.network.dto.WorkBoardSubtaskPageDto
import sg.mesha.goatos.feature.workboard.WorkBoardDetailEvent
import sg.mesha.goatos.feature.workboard.WorkBoardDetailUiState
import sg.mesha.goatos.feature.workboard.WorkBoardStepUi
import sg.mesha.goatos.feature.workboard.WorkBoardSubtaskUi
import javax.inject.Inject

/**
 * ONE board row's detail (`/work/item/{rowKey}`), maintainer decision 2026-09-10. Reads the row
 * Room already holds through [WorkBoardRepository.observeRow] — never a second network call for
 * it — so the drill opens instantly from the list's own cache and survives a signal drop.
 *
 * Beneath it, the row's SUBTASKS (maintainer review 2026-09-25: the phone showed three numbers
 * where the web drawer lists every pen): Room-first pages of
 * `GET /work-board/rows/{row_key}/subtasks`, refreshed when the screen opens or comes back into
 * view, the next page fetched as the reader nears the end. Only the pages the reader reached are
 * held, each one bounded screen-page.
 *
 * Whether the "Open" button appears is decided by the NavHost, which knows which backend `href`
 * shapes this build can route; the ViewModel only carries the row.
 */
@HiltViewModel
class WorkBoardDetailViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repository: WorkBoardRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    /** The board `row_key`, decoded by Navigation from the URL-encoded path segment. */
    val rowKey: String = savedStateHandle.get<String>(ARG_ROW_KEY).orEmpty()

    /** The scope the subtasks are read in: the row's own park and day. */
    private data class Scope(val parkId: String?, val businessDate: String)

    private val row = repository.observeRow(rowKey)
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    private val scope = row
        .map { dto -> dto?.let { Scope(parkId = it.parkId.ifBlank { null }, businessDate = it.businessDate) } }
        .distinctUntilChanged()

    /** The page cursors the reader has reached, the first page ("") always included. */
    private val cursors = MutableStateFlow(listOf(""))
    private val failed = MutableStateFlow(false)

    @OptIn(ExperimentalCoroutinesApi::class)
    private val pages = combine(scope, cursors) { s, loaded -> s to loaded }
        .flatMapLatest { (s, loaded) ->
            if (s == null) {
                flowOf(emptyList())
            } else {
                combine(loaded.map { cursor -> repository.observeSubtaskPage(request(s, cursor)) }) { it.toList() }
            }
        }

    val state: StateFlow<WorkBoardDetailUiState> = combine(row, pages, failed) { dto, loadedPages, readFailed ->
        val cached = loadedPages.mapNotNull(Resource<WorkBoardSubtaskPageDto>::data)
        WorkBoardDetailUiState(
            loading = false,
            row = dto?.toRowUi(),
            subtasks = cached.flatMap { page -> page.subtasks.map { it.toSubtaskUi() } }.distinctBy { it.key },
            subtaskTotal = cached.firstOrNull()?.total ?: 0,
            subtasksLoading = cached.isEmpty() && !readFailed,
            subtasksFailed = cached.isEmpty() && readFailed,
            hasMoreSubtasks = !loadedPages.lastOrNull()?.data?.nextCursor.isNullOrBlank(),
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WorkBoardDetailUiState())

    init {
        viewModelScope.launch {
            // The first page as soon as the row (and so its park and day) is known.
            refreshPage(scope.filterNotNull().first(), "")
        }
    }

    fun onEvent(event: WorkBoardDetailEvent) {
        when (event) {
            WorkBoardDetailEvent.Open -> analytics.track(
                AnalyticsEventsWorkBoard.ROW_FOLLOWED,
                mapOf(
                    "row_key" to rowKey,
                    AnalyticsEvents.Params.MODULE_KEY to state.value.row?.module.orEmpty(),
                ),
            )
            WorkBoardDetailEvent.Refresh -> viewModelScope.launch {
                val s = scope.first() ?: return@launch
                // Every page the reader reached, in order: a pen that changed state moves between
                // pages, so re-reading only the first would leave the list stale further down.
                cursors.value.forEach { cursor -> refreshPage(s, cursor) }
            }
            WorkBoardDetailEvent.LoadMoreSubtasks -> loadMore()
            WorkBoardDetailEvent.Back -> Unit
        }
    }

    private fun loadMore() {
        if (!state.value.hasMoreSubtasks) return
        viewModelScope.launch {
            val s = scope.first() ?: return@launch
            val lastPage = repository.observeSubtaskPage(request(s, cursors.value.last())).first().data
            val cursor = lastPage?.nextCursor?.takeIf { it.isNotBlank() } ?: return@launch
            // A cursor already reached (a second scroll event) is never fetched twice.
            if (cursor in cursors.value) return@launch
            cursors.value = cursors.value + cursor
            refreshPage(s, cursor)
        }
    }

    private suspend fun refreshPage(s: Scope, cursor: String) {
        repository.refreshSubtaskPage(request(s, cursor))
            .onSuccess { failed.value = false }
            .onFailure { error ->
                failed.value = true
                crashReporter.recordException(error, "work board subtasks load failed")
                analytics.track(
                    AnalyticsEventsWorkBoard.FAILURE,
                    mapOf(
                        AnalyticsEvents.Params.KIND to "subtasks",
                        AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS),
                    ),
                )
            }
    }

    private fun request(s: Scope, cursor: String) = WorkBoardSubtaskRequest(
        rowKey = rowKey,
        parkId = s.parkId,
        businessDate = s.businessDate,
        cursor = cursor,
    )

    companion object {
        /** Must match `Routes.WORK_ITEM_ARG` in the NavHost. */
        const val ARG_ROW_KEY = "rowKey"
        private const val MAX_REASON_CHARS = 120
    }
}

internal fun WorkBoardSubtaskDto.toSubtaskUi(): WorkBoardSubtaskUi = WorkBoardSubtaskUi(
    key = key,
    name = name,
    subtitle = subtitle,
    workState = workState,
    lane = lane,
    ownerName = owner.name,
    needsAttention = needsAttention,
    steps = steps.map { WorkBoardStepUi(name = it.name, state = it.state, detail = it.detail) },
)
