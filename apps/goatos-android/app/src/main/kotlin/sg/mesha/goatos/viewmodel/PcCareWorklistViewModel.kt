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
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.PcCareRepository
import sg.mesha.goatos.core.data.PcCareWorklistQuery
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.data.sync.submittedGrainKey
import sg.mesha.goatos.core.network.dto.PcCareTaskDto
import sg.mesha.goatos.feature.pccare.PcCareInventoryRequirementUi
import sg.mesha.goatos.feature.pccare.PcCareStatusTone
import sg.mesha.goatos.feature.pccare.PcCareTaskCardUi
import sg.mesha.goatos.feature.pccare.PcCareWorklistUiState
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

/**
 * PC Care per-category worklist state holder (module pc_care, maintainer decision 2026-08-21).
 * One instance per L0 tab route: the tab's composable binds its category constant + backend nav
 * label once via [bind] (the four tab routes are constant L0 hrefs and carry no nav arguments).
 *
 * Offline-first: rows come from the Room-backed Pager in [PcCareRepository.worklistRows]; the
 * "In review" overlay comes from the OUTBOX-derived submitted-for-review grains, matched by
 * [PcCareTaskDto.submittedGrainKey] — never a grain key rebuilt here (SubmittedGrainKeys.kt).
 */
@HiltViewModel
class PcCareWorklistViewModel @Inject constructor(
    private val repository: PcCareRepository,
    private val submittedGrains: SubmittedGrainsSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Selection(
        val category: String = "",
        val title: String = "",
        val moduleLabel: String = "Preventive Care",
        val showDateBar: Boolean = true,
        val date: String = LocalDate.now(ZoneId.of(INDIA_ZONE)).toString(),
        /** Bumped by refresh so an unchanged selection is still a NEW value (StateFlow conflates). */
        val refreshNonce: Int = 0,
    )

    private val selection = MutableStateFlow(Selection())
    private val _isRefreshing = MutableStateFlow(false)

    /** Binds this instance to its tab. Idempotent — recomposition may call it again. */
    fun bind(category: String, title: String, moduleLabel: String = "Preventive Care", showDateBar: Boolean = true) {
        val current = selection.value
        if (
            current.category == category &&
            current.title == title &&
            current.moduleLabel == moduleLabel &&
            current.showDateBar == showDateBar
        ) return
        selection.value = current.copy(category = category, title = title, moduleLabel = moduleLabel, showDateBar = showDateBar)
        analytics.track(
            AnalyticsEvents.PC_CARE_WORKLIST_VIEWED,
            mapOf(AnalyticsEvents.Params.KIND to category),
        )
    }

    val state: StateFlow<PcCareWorklistUiState> = combine(selection, _isRefreshing) { sel, refreshing ->
        PcCareWorklistUiState(
            title = sel.title,
            moduleLabel = sel.moduleLabel,
            dateLabel = sel.date,
            today = LocalDate.now(ZoneId.of(INDIA_ZONE)).toString(),
            isRefreshing = refreshing,
            emptyMessage = EMPTY_MESSAGE,
            showDateBar = sel.showDateBar,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), PcCareWorklistUiState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<PcCareTaskCardUi>> =
        combine(selection, submittedGrains.observe()) { sel, submitted -> sel to submitted }
            .filter { (sel, _) -> sel.category.isNotBlank() }
            .flatMapLatest { (sel, submitted) ->
                repository.worklistRows(PcCareWorklistQuery(category = sel.category, date = sel.date))
                    .map { page -> page.map { dto -> dto.toCardUi(submitted) } }
            }
            .cachedIn(viewModelScope)

    fun onEvent(event: sg.mesha.goatos.feature.pccare.PcCareWorklistEvent) {
        when (event) {
            sg.mesha.goatos.feature.pccare.PcCareWorklistEvent.Refresh -> refresh()
            is sg.mesha.goatos.feature.pccare.PcCareWorklistEvent.SelectDate -> selectDate(event.date)
            is sg.mesha.goatos.feature.pccare.PcCareWorklistEvent.OpenTask ->
                analytics.track(AnalyticsEvents.PC_CARE_TASK_OPENED, mapOf(AnalyticsEvents.Params.KIND to selection.value.category))
        }
    }

    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "pc care worklist page load failed")
        analytics.track(
            AnalyticsEvents.PC_CARE_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to selection.value.category,
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
            ),
        )
    }

    private fun refresh() {
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                // Drop the freshness marker FIRST so the re-created pager refetches instead of
                // TTL-skipping — an explicit refresh means "show me the server's list now".
                val sel = selection.value
                if (sel.category.isNotBlank()) {
                    // exception:exempt local cache-marker delete; a failure just leaves the TTL skip
                    runCatching {
                        repository.invalidateWorklist(PcCareWorklistQuery(category = sel.category, date = sel.date))
                    }
                }
                // A NEW value, not an equal one: MutableStateFlow conflates on equality.
                selection.value = selection.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    /** Operator/director worklist stays on current and future work; old finished cards live in monitor. */
    private fun selectDate(date: LocalDate) {
        val today = LocalDate.now(ZoneId.of(INDIA_ZONE))
        if (date > today.plusDays(FUTURE_WINDOW_DAYS) || date < today.minusDays(PAST_WINDOW_DAYS)) return
        val current = selection.value
        val iso = date.toString()
        if (current.date == iso) return
        selection.value = current.copy(date = iso)
    }

    private companion object {
        const val INDIA_ZONE = "Asia/Kolkata"
        const val PAST_WINDOW_DAYS = 0L
        const val FUTURE_WINDOW_DAYS = 7L
        const val EMPTY_MESSAGE = "No care tasks for this day"
    }
}

/** Maps one backend task row to its worklist card. Backend copy is rendered verbatim. */
internal fun PcCareTaskDto.toCardUi(locallySubmittedForReview: Set<String>): PcCareTaskCardUi {
    // The SAME key builder the outbox projection uses — the two cannot disagree.
    val isLocallySubmitted = locallySubmittedForReview.contains(submittedGrainKey())
    val effectiveStatus = when {
        // Offline-first overlay: a just-queued submit shows "in review" ahead of the next refresh,
        // converging on the backend bucket once the write syncs. Never overlays a rework/terminal
        // status the backend already decided.
        status == PC_CARE_STATUS_OPEN && isLocallySubmitted -> PC_CARE_STATUS_PENDING_VERIFICATION
        else -> status
    }
    val (label, tone) = when (effectiveStatus) {
        PC_CARE_STATUS_PENDING_VERIFICATION -> "In review" to PcCareStatusTone.REVIEW
        PC_CARE_STATUS_REWORK -> "Needs another video" to PcCareStatusTone.DANGER
        PC_CARE_STATUS_COMPLETED -> "Done" to PcCareStatusTone.DONE
        else -> when (workState) {
            "delayed" -> "Delayed" to PcCareStatusTone.DANGER
            else -> "Open" to PcCareStatusTone.NEUTRAL
        }
    }
    return PcCareTaskCardUi(
        listKey = taskId,
        taskId = taskId,
        category = category,
        statusLabel = label,
        statusTone = tone,
        // Backend-composed pen display, verbatim; degrade to the bare shed label only when the
        // backend sent no composed display at all.
        locationDisplay = operationalLocationDisplay.ifBlank { shedLabel },
        parkLabel = parkLabel,
        dueDateLabel = dueBusinessDate,
        assigneeLine = assigneeNames.joinToString(", "),
        animalCountLabel = if (animalCount > 0) "$animalCount animals" else "",
        inventoryRequirements = inventoryRequirements.map {
            PcCareInventoryRequirementUi(
                vaccineLabel = it.vaccineLabel,
                requiredDosesLabel = if (it.requiredDoses == 1) "1 dose" else "${it.requiredDoses} doses",
            )
        },
        reworkReason = if (effectiveStatus == PC_CARE_STATUS_REWORK) reworkReason else "",
        cancellable = effectiveStatus == PC_CARE_STATUS_OPEN,
        // Submitted rows still open the detail record; the detail screen owns the read-only lock.
        openable = true,
    )
}

internal const val PC_CARE_STATUS_OPEN = "open"
internal const val PC_CARE_STATUS_PENDING_VERIFICATION = "pending_verification"
internal const val PC_CARE_STATUS_REWORK = "rework"
internal const val PC_CARE_STATUS_COMPLETED = "completed"
