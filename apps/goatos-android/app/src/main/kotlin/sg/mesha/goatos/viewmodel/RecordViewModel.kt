package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import sg.mesha.goatos.feature.record.RecordEvent
import sg.mesha.goatos.feature.record.RecordUiState
import sg.mesha.goatos.ui.sampleRecordState
import javax.inject.Inject

/**
 * Read-only shed/drive record state holder. Seeds the interim [sampleRecordState] fixture.
 * The record is read-only, so the only event ([RecordEvent.Close]) is navigation, routed
 * by the host.
 *
 * TODO: replace the fake seed with `GET shed-record/{shed_id}` (or the drive record) via AppApi.
 */
@HiltViewModel
class RecordViewModel @Inject constructor() : ViewModel() {

    private val _state = MutableStateFlow(sampleRecordState())
    val state: StateFlow<RecordUiState> = _state.asStateFlow()

    fun onEvent(event: RecordEvent) {
        when (event) {
            RecordEvent.Close -> Unit // navigation — handled by the nav host.
        }
    }
}
