package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.feature.leadership.DateOption
import sg.mesha.goatos.feature.leadership.LeadershipEvent
import sg.mesha.goatos.feature.leadership.RescheduleUiState
import sg.mesha.goatos.feature.leadership.RescheduleConfirmKind
import sg.mesha.goatos.feature.leadership.RescheduleNoteKind
import sg.mesha.goatos.ui.sampleRescheduleState
import java.time.OffsetDateTime
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.TextStyle
import java.util.Locale
import javax.inject.Inject

/**
 * Reschedule-form state holder. Renders the reschedule form shell and owns the two local
 * form interactions: [LeadershipEvent.SegmentSelected] switches the action segment and
 * [LeadershipEvent.DateSelected] picks a date. [LeadershipEvent.Back] is navigation.
 *
 * Confirm is enabled only when a control-tower/overdue row supplied an obligation id and
 * the operator selected a future due date. The write is queued through [SyncRepository],
 * so retries reuse the same idempotency key and backend policy remains authoritative.
 */
@HiltViewModel
class RescheduleViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val obligationId: String? = savedStateHandle[ARG_OBLIGATION_ID]

    private val _state = MutableStateFlow(initialState(obligationId))
    val state: StateFlow<RescheduleUiState> = _state.asStateFlow()

    fun onEvent(event: LeadershipEvent) {
        when (event) {
            is LeadershipEvent.SegmentSelected ->
                _state.update { it.copy(selectedSegmentId = event.id) }
            is LeadershipEvent.DateSelected ->
                _state.update {
                    it.copy(
                        selectedDateId = event.id,
                        confirmEnabled = !obligationId.isNullOrBlank() && event.id.isNotBlank(),
                    )
                }
            LeadershipEvent.ConfirmReschedule -> confirm()
            else -> Unit // navigation / inert — handled by the nav host.
        }
    }

    private fun confirm() {
        val target = obligationId
        val dueAt = state.value.selectedDateId
        if (target.isNullOrBlank() || dueAt.isNullOrBlank()) return
        viewModelScope.launch {
            _state.update { it.copy(confirmEnabled = false, noteKind = RescheduleNoteKind.QUEUEING) }
            val request = RescheduleObligationRequestDto(dueAt = dueAt)
            val key = "mobile-reschedule:$target:$dueAt"
            when (
                syncRepository.enqueueReschedule(
                    obligationId = target,
                    groupKey = target,
                    idempotencyKey = key,
                    request = request,
                )
            ) {
                is AppResult.Ok -> _state.update {
                    it.copy(confirmKind = RescheduleConfirmKind.QUEUED, noteKind = RescheduleNoteKind.QUEUED)
                }
                is AppResult.Err -> _state.update {
                    it.copy(confirmEnabled = true, noteKind = RescheduleNoteKind.ERROR)
                }
            }
        }
    }

    private fun initialState(obligationId: String?): RescheduleUiState {
        val options = dateOptions()
        return sampleRescheduleState().copy(
            dateOptions = options,
            selectedDateId = null,
            confirmEnabled = false,
            confirmKind = RescheduleConfirmKind.CONFIRM,
            noteKind = if (obligationId.isNullOrBlank()) {
                RescheduleNoteKind.NO_OBLIGATION
            } else {
                RescheduleNoteKind.SELECT_DATE
            },
        )
    }

    private fun dateOptions(): List<DateOption> =
        listOf(1L, 2L, 3L, 5L).map { days ->
            val dueAt = OffsetDateTime.now(ZoneOffset.UTC)
                .plusDays(days)
                .withHour(9)
                .withMinute(0)
                .withSecond(0)
                .withNano(0)
            DateOption(
                id = dueAt.format(DateTimeFormatter.ISO_OFFSET_DATE_TIME),
                dayLabel = dueAt.dayOfMonth.toString(),
                label = "${dueAt.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)}, ${dueAt.dayOfMonth} ${dueAt.month.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)}",
                sub = if (days == 1L) "Tomorrow · backend validates buffer" else "Backend validates buffer",
                inBuffer = true,
                tomorrow = days == 1L,
            )
        }

    private companion object {
        const val ARG_OBLIGATION_ID = "obligationId"
    }
}
