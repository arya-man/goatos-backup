package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.DiagnosisQueueFilters
import sg.mesha.goatos.core.data.HealthRepository
import sg.mesha.goatos.core.network.dto.HealthDiagnosisQueueItemDto
import sg.mesha.goatos.feature.health.DiagnosisQueueEvent
import sg.mesha.goatos.feature.health.DiagnosisQueueRow
import sg.mesha.goatos.feature.health.DiagnosisQueueState

/** IST. Every business date the farm reads is in the farm's own day. */
private val FARM_ZONE: ZoneId = ZoneId.of("Asia/Kolkata")

@HiltViewModel
class DiagnosisQueueViewModel @Inject constructor(
    private val healthRepository: HealthRepository,
) : ViewModel() {

    // Blank status = the SERVER's default, which is work awaiting a decision. The
    // default lives in one place rather than being restated on the client.
    private val filters = DiagnosisQueueFilters()

    private val _state = MutableStateFlow(DiagnosisQueueState())
    val state: StateFlow<DiagnosisQueueState> = _state.asStateFlow()

    val rows: Flow<PagingData<DiagnosisQueueRow>> =
        healthRepository.diagnosisQueue(filters)
            .map { page -> page.map { it.toQueueRow() } }
            .cachedIn(viewModelScope)

    init {
        viewModelScope.launch {
            healthRepository.observeDiagnosisQueueMeta(filters).collect { meta ->
                // Absent meta means the queue has never loaded. Defaulting to false
                // hides the decision rather than offering one that would 403.
                _state.value = _state.value.copy(mayConfirm = meta?.mayConfirm ?: false)
            }
        }
    }

    fun onEvent(event: DiagnosisQueueEvent) {
        when (event) {
            DiagnosisQueueEvent.Refresh -> Unit // Paging owns the refresh; the screen calls it.
            is DiagnosisQueueEvent.Open -> Unit // Navigation only.
            DiagnosisQueueEvent.Back -> Unit
        }
    }

    /** Reflects the paging refresh in the sync affordance. */
    fun setRefreshing(refreshing: Boolean) {
        if (_state.value.refreshing != refreshing) {
            _state.value = _state.value.copy(refreshing = refreshing)
        }
    }
}

/**
 * Maps a wire row onto what the queue shows.
 *
 * The problem list keeps the backend's order: it is ranked severity first, then
 * confidence, and re-sorting it here would put a mild certainty above a serious
 * maybe. The ids are turned into farm words, because a register token like
 * `pregnancy_toxemia` must never reach a screen.
 */
internal fun HealthDiagnosisQueueItemDto.toQueueRow(): DiagnosisQueueRow = DiagnosisQueueRow(
    diagnosisRunId = diagnosisRunId,
    goatDisplayId = goatDisplayId,
    location = operationalLocationDisplay,
    seen = relativeBusinessDate(businessDate),
    problems = problems.map { diagnosisLabel(it) },
    emergencyCount = emergencyCount,
    unexplainedCount = unexplainedCount,
)

/**
 * "Today", "Yesterday", or the date.
 *
 * Compared in IST, because the farm's day is the business day: an observation
 * recorded at 23:30 IST is still today's work, and a UTC comparison would call it
 * tomorrow's for the last five and a half hours of every day.
 *
 * An unparseable date falls back to the raw value rather than blank — showing the
 * server's string is honest, showing nothing hides which day the animal was seen.
 */
internal fun relativeBusinessDate(
    businessDate: String,
    today: LocalDate = OffsetDateTime.now().atZoneSameInstant(FARM_ZONE).toLocalDate(),
): String {
    // exception:exempt display formatter; an unparseable date is shown verbatim rather than hidden.
    val parsed = runCatching { LocalDate.parse(businessDate) }.getOrNull() ?: return businessDate
    return when (parsed) {
        today -> "Today"
        today.minusDays(1) -> "Yesterday"
        else -> parsed.format(DateTimeFormatter.ofPattern("d MMM"))
    }
}
