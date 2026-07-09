package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import sg.mesha.goatos.feature.calendar.CalendarEvent
import sg.mesha.goatos.feature.calendar.CalendarUiState
import sg.mesha.goatos.ui.sampleCalendarState
import javax.inject.Inject

/**
 * Calendar screen state holder. Seeds the interim [sampleCalendarState] fixture and
 * owns the one purely-local interaction — switching the week/month/history segment.
 * Drill events ([CalendarEvent.TapDay] / [CalendarEvent.TapItem]) are navigation and
 * are routed by the nav host, not mutated here.
 *
 * TODO: replace the fake seed with the mobile bootstrap + `GET calendar` reads via AppApi.
 */
@HiltViewModel
class CalendarViewModel @Inject constructor() : ViewModel() {

    private val _state = MutableStateFlow(sampleCalendarState())
    val state: StateFlow<CalendarUiState> = _state.asStateFlow()

    fun onEvent(event: CalendarEvent) {
        when (event) {
            is CalendarEvent.SelectSegment ->
                _state.update { it.copy(selectedSegmentId = event.segmentId) }
            // Day/item taps are navigation — handled by the nav host.
            is CalendarEvent.TapDay,
            is CalendarEvent.TapItem -> Unit
        }
    }
}
