package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsWorkBoard
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.data.WorkBoardRepository
import sg.mesha.goatos.feature.workboard.WorkBoardDetailEvent
import sg.mesha.goatos.feature.workboard.WorkBoardDetailUiState
import javax.inject.Inject

/**
 * ONE board row's detail (`/work/item/{rowKey}`), maintainer decision 2026-09-10. Reads the row
 * Room already holds through [WorkBoardRepository.observeRow] — never a second network call — so
 * the drill opens instantly from the list's own cache and survives a signal drop in the shed.
 *
 * Whether the "Open" button appears is decided by the NavHost, which knows which backend `href`
 * shapes this build can route; the ViewModel only carries the row.
 */
@HiltViewModel
class WorkBoardDetailViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    repository: WorkBoardRepository,
    private val analytics: AnalyticsPort,
) : ViewModel() {

    /** The board `row_key`, decoded by Navigation from the URL-encoded path segment. */
    val rowKey: String = savedStateHandle.get<String>(ARG_ROW_KEY).orEmpty()

    val state: StateFlow<WorkBoardDetailUiState> = repository.observeRow(rowKey)
        .map { row -> WorkBoardDetailUiState(loading = false, row = row?.toRowUi()) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WorkBoardDetailUiState())

    fun onEvent(event: WorkBoardDetailEvent) {
        when (event) {
            WorkBoardDetailEvent.Open -> analytics.track(
                AnalyticsEventsWorkBoard.ROW_FOLLOWED,
                mapOf(
                    "row_key" to rowKey,
                    AnalyticsEvents.Params.MODULE_KEY to state.value.row?.module.orEmpty(),
                ),
            )
            WorkBoardDetailEvent.Back -> Unit
        }
    }

    companion object {
        /** Must match `Routes.WORK_ITEM_ARG` in the NavHost. */
        const val ARG_ROW_KEY = "rowKey"
    }
}
