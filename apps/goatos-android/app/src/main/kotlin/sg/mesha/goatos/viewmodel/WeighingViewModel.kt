package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.weighingScopeKey
import sg.mesha.goatos.feature.weighing.WeighingDraftUiRow
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingUiState
import sg.mesha.goatos.ui.Routes
import javax.inject.Inject

@HiltViewModel
class WeighingViewModel @Inject constructor(
    private val repository: WeighingRepository,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {
    private val campaignId = savedStateHandle.get<String>(Routes.WEIGHING_CAMPAIGN_ARG).orEmpty()
    private val workGroupId = savedStateHandle.get<String>(Routes.WEIGHING_WORK_GROUP_ARG).orEmpty()
    private val campaignShedId = savedStateHandle.get<String>(Routes.WEIGHING_CAMPAIGN_SHED_ARG).orEmpty()
    private val routeTitle = savedStateHandle.get<String>(Routes.EXECUTION_SCAN_TITLE_ARG).orEmpty()
    private val scopeKey = listOf(campaignId, workGroupId, campaignShedId)
        .takeIf { parts -> parts.all { it.isNotBlank() } }
        ?.let { weighingScopeKey(campaignId, workGroupId, campaignShedId) }

    @OptIn(ExperimentalCoroutinesApi::class)
    val state: StateFlow<WeighingUiState> =
        flowOf(scopeKey).flatMapLatest { key ->
            if (key == null) {
                flowOf(WeighingUiState())
            } else {
                repository.observeScope(key, ROSTER_WINDOW_SIZE).map { scope ->
                    WeighingUiState(
                        title = routeTitle.ifBlank { "Weighing" },
                        scopeLabel = "Campaign $campaignId - Work group $workGroupId - Scope $campaignShedId",
                        hasScope = true,
                        totalExpected = scope.totalExpected,
                        visibleRows = scope.rosterWindow.map { row ->
                            WeighingRosterUiRow(
                                id = row.id,
                                displayAnimalId = row.displayAnimalId,
                                expectedLocationLabel = row.expectedLocationLabel,
                                actualLocationLabel = row.actualLocationLabel,
                                status = row.status,
                                availabilityStatus = row.availabilityStatus,
                                wrongShed = !row.actualLocationId.isNullOrBlank() &&
                                    row.actualLocationId != row.expectedLocationId,
                            )
                        },
                        individualDrafts = scope.individualDrafts.map { draft ->
                            WeighingDraftUiRow(
                                id = draft.observationId,
                                label = "${draft.animalId} - ${draft.weightKg} kg",
                                proofReady = draft.proofReady,
                                readyToSubmit = draft.readyToSubmit,
                            )
                        },
                        shedDrafts = scope.shedDrafts.map { draft ->
                            WeighingDraftUiRow(
                                id = draft.shedObservationId,
                                label = "Shed / partition result",
                                proofReady = draft.proofReady,
                                readyToSubmit = draft.readyToSubmit,
                            )
                        },
                    )
                }
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingUiState())

    private companion object {
        const val ROSTER_WINDOW_SIZE = 40
    }
}
