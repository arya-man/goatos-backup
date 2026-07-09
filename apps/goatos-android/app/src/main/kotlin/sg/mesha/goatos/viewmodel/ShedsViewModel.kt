package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import sg.mesha.goatos.feature.sheds.ShedsEvent
import sg.mesha.goatos.feature.sheds.ShedsUiState
import sg.mesha.goatos.ui.sampleShedsState
import javax.inject.Inject

/**
 * Today's-sheds / drive-status state holder. Seeds the interim [sampleShedsState]
 * fixture. [ShedsEvent.Refresh] re-emits the state (a real reload lands with the
 * scoped drive read); [ShedsEvent.OpenShedRecord] is navigation, routed by the host.
 *
 * TODO: replace the fake seed with the scoped `GET sheds` drive read via AppApi.
 */
@HiltViewModel
class ShedsViewModel @Inject constructor() : ViewModel() {

    private val _state = MutableStateFlow(sampleShedsState())
    val state: StateFlow<ShedsUiState> = _state.asStateFlow()

    fun onEvent(event: ShedsEvent) {
        when (event) {
            ShedsEvent.Refresh -> _state.value = sampleShedsState()
            is ShedsEvent.OpenShedRecord -> Unit // navigation — handled by the nav host.
        }
    }
}
