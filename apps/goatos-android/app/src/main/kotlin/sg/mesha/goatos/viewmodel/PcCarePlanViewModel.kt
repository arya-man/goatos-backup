package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.PcCareRepository
import sg.mesha.goatos.core.data.PcCareWorklistQuery
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.network.dto.PcCareCreateTaskRequestDto
import sg.mesha.goatos.core.network.dto.PcCarePlannerCatalogDto
import sg.mesha.goatos.feature.pccare.PcCarePlanEvent
import sg.mesha.goatos.feature.pccare.PcCarePlanOption
import sg.mesha.goatos.feature.pccare.PcCarePlanPenUi
import sg.mesha.goatos.feature.pccare.PcCarePlanStep
import sg.mesha.goatos.feature.pccare.PcCarePlanUiState
import sg.mesha.goatos.feature.pccare.PcCareTaskCardUi
import java.time.LocalDate
import java.time.ZoneId
import java.util.UUID
import javax.inject.Inject

/**
 * Planner state holder for BOTH faces of a PC Care category (module pc_care, maintainer decision
 * 2026-08-21): the monitor list a category tab shows to a non-executor (bindMonitor), and the
 * plan wizard drill (bindWizard) whose category is fixed by the launching tab. Nav offers and the
 * pc_care_execute / pc_care_plan capability flags are backend-composed — no role checks here.
 *
 * The wizard's create idempotency key is minted ONCE per wizard session and REUSED on retry, so a
 * flaky-network double-tap can never create two tasks; a fresh wizard session mints a new key.
 */
@HiltViewModel
class PcCarePlanViewModel @Inject constructor(
    private val repository: PcCareRepository,
    private val submittedGrains: SubmittedGrainsSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val _state = MutableStateFlow(
        PcCarePlanUiState(
            monitorDate = todayIso(),
            today = todayIso(),
            selectedDate = todayIso(),
            emptyMessage = EMPTY_MESSAGE,
        ),
    )
    val state: StateFlow<PcCarePlanUiState> = _state.asStateFlow()

    private data class MonitorSelection(val category: String = "", val date: String = "", val refreshNonce: Int = 0)

    private val monitorSelection = MutableStateFlow(MonitorSelection(date = todayIso()))

    /** One key per wizard session, reused across retries of the SAME planned task. */
    private var createIdempotencyKey: String = UUID.randomUUID().toString()

    private var catalog: PcCarePlannerCatalogDto? = null
    private var pensCursor: String? = null
    private var pensLoadInFlight = false

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<PcCareTaskCardUi>> =
        combine(monitorSelection, submittedGrains.observe()) { sel, submitted -> sel to submitted }
            .filter { (sel, _) -> sel.category.isNotBlank() && sel.date.isNotBlank() }
            .flatMapLatest { (sel, submitted) ->
                repository.worklistRows(PcCareWorklistQuery(category = sel.category, date = sel.date, monitor = true))
                    .map { page -> page.map { dto -> dto.toCardUi(submitted) } }
            }
            .cachedIn(viewModelScope)

    init {
        analytics.track(AnalyticsEvents.PC_CARE_WORKLIST_VIEWED, mapOf(AnalyticsEvents.Params.KIND to "planner"))
        loadCatalog()
    }

    fun onEvent(event: PcCarePlanEvent) {
        when (event) {
            PcCarePlanEvent.Refresh -> refresh()
            is PcCarePlanEvent.SelectMonitorDate -> selectMonitorDate(event.date)
            is PcCarePlanEvent.CancelTask -> cancelTask(event.taskId)
            PcCarePlanEvent.CloseCreate -> Unit // navigation-owned: the wizard screen pops itself
            is PcCarePlanEvent.SelectDate -> selectCreateDate(event.date)
            is PcCarePlanEvent.SelectPark -> selectPark(event.parkId)
            is PcCarePlanEvent.SelectPen -> selectPen(event.shedId, event.partitionLabel)
            PcCarePlanEvent.LoadMorePens -> loadPens(append = true)
            is PcCarePlanEvent.ToggleOperator -> toggleOperator(event.userId)
            PcCarePlanEvent.NextStep -> nextStep()
            PcCarePlanEvent.PreviousStep -> previousStep()
            PcCarePlanEvent.Create -> create()
            PcCarePlanEvent.DismissMessage -> _state.update { it.copy(message = null) }
        }
    }

    // ---- Binding -----------------------------------------------------------------------------

    /** The monitor face of one category tab. Category + title come from the backend-composed tab. */
    fun bindMonitor(categoryKey: String, title: String) {
        _state.update {
            it.copy(
                title = title,
                step = PcCarePlanStep.LIST,
                monitorCategoryKey = categoryKey,
                monitorCategoryLabel = title,
            )
        }
        monitorSelection.value = monitorSelection.value.copy(category = categoryKey)
    }

    /** The plan-wizard drill. The launching tab fixes the category; the wizard starts at DATE. */
    fun bindWizard(categoryKey: String, title: String) {
        // A NEW wizard session is a NEW act: fresh key, cleared choices.
        createIdempotencyKey = UUID.randomUUID().toString()
        _state.update {
            it.copy(
                title = title,
                step = PcCarePlanStep.DATE,
                selectedCategoryKey = categoryKey,
                selectedCategoryLabel = title,
                selectedDate = todayIso(),
                selectedParkId = "",
                selectedParkLabel = "",
                pens = emptyList(),
                pensEndReached = true,
                selectedShedId = "",
                selectedPartitionLabel = "",
                selectedPenLabel = "",
                selectedOperatorIds = emptySet(),
                creating = false,
                createdTaskId = "",
                message = null,
            )
        }
    }

    // ---- Monitor -----------------------------------------------------------------------------

    private fun refresh() {
        loadCatalog()
        viewModelScope.launch {
            // Drop the freshness marker FIRST so the re-created pager refetches instead of
            // TTL-skipping — an explicit refresh means "show me the server's list now".
            val sel = monitorSelection.value
            if (sel.category.isNotBlank() && sel.date.isNotBlank()) {
                // exception:exempt local cache-marker delete; a failure just leaves the TTL skip
                runCatching {
                    repository.invalidateWorklist(
                        PcCareWorklistQuery(category = sel.category, date = sel.date, monitor = true),
                    )
                }
            }
            monitorSelection.value = monitorSelection.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
        }
    }

    private fun selectMonitorDate(date: LocalDate) {
        val today = LocalDate.now(ZoneId.of(INDIA_ZONE))
        if (date > today.plusDays(FUTURE_WINDOW_DAYS) || date < today.minusDays(PAST_WINDOW_DAYS)) return
        val iso = date.toString()
        _state.update { it.copy(monitorDate = iso) }
        monitorSelection.value = monitorSelection.value.copy(date = iso)
    }

    private fun cancelTask(taskId: String) {
        viewModelScope.launch {
            try {
                repository.cancelTask(taskId)
                _state.update { it.copy(message = "Task canceled") }
                refresh()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                crashReporter.recordException(error, "pc care planner cancel failed")
                analytics.track(
                    AnalyticsEvents.PC_CARE_FAILURE,
                    mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "cancel_failed").take(MAX_REASON_CHARS)),
                )
                _state.update { it.copy(message = "Couldn't cancel the task. Try again.") }
            }
        }
    }

    private fun loadCatalog() {
        if (_state.value.isRefreshing) return
        _state.update { it.copy(isRefreshing = true) }
        viewModelScope.launch {
            try {
                val loaded = repository.plannerCatalog()
                catalog = loaded
                val categories = loaded.categories.map { PcCarePlanOption(it.key, it.label) }
                _state.update { current ->
                    current.copy(
                        categories = categories,
                        parks = loaded.parks.map { PcCarePlanOption(it.parkId, it.parkLabel) },
                        operators = loaded.operators.map { PcCarePlanOption(it.userId, it.displayName, it.parkIds) },
                    )
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                crashReporter.recordException(error, "pc care planner catalog load failed")
                analytics.track(
                    AnalyticsEvents.PC_CARE_FAILURE,
                    mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "catalog_failed").take(MAX_REASON_CHARS)),
                )
                _state.update { it.copy(message = "Couldn't load the planner. Tap refresh to retry.") }
            } finally {
                _state.update { it.copy(isRefreshing = false) }
            }
        }
    }

    // ---- Create wizard -----------------------------------------------------------------------

    private fun selectCreateDate(date: LocalDate) {
        val today = LocalDate.now(ZoneId.of(INDIA_ZONE))
        if (date < today || date > today.plusDays(FUTURE_WINDOW_DAYS)) return
        _state.update { it.copy(selectedDate = date.toString(), pens = emptyList(), selectedShedId = "", selectedPartitionLabel = "", selectedPenLabel = "") }
    }

    private fun selectPark(parkId: String) {
        val label = _state.value.parks.firstOrNull { it.key == parkId }?.label.orEmpty()
        _state.update {
            it.copy(
                selectedParkId = parkId,
                selectedParkLabel = label,
                pens = emptyList(),
                selectedShedId = "",
                selectedPartitionLabel = "",
                selectedPenLabel = "",
                // A different farm has different people: the operator step filters to the
                // chosen park's mapping, so choices made under another park cannot carry over.
                selectedOperatorIds = emptySet(),
            )
        }
    }

    private fun selectPen(shedId: String, partitionLabel: String) {
        // Pens are one row PER PARTITION, so shedId alone is not unique (Castro 1/2/3 share it).
        val pen = _state.value.pens.firstOrNull { it.shedId == shedId && it.partitionLabel == partitionLabel } ?: return
        if (pen.existingTaskId.isNotBlank()) return
        _state.update {
            it.copy(
                selectedShedId = shedId,
                selectedPartitionLabel = pen.partitionLabel,
                selectedPenLabel = pen.locationDisplay,
            )
        }
    }

    private fun toggleOperator(userId: String) {
        _state.update {
            val selected = it.selectedOperatorIds
            it.copy(selectedOperatorIds = if (userId in selected) selected - userId else selected + userId)
        }
    }

    private fun nextStep() {
        val current = _state.value
        val next = when (current.step) {
            PcCarePlanStep.DATE -> if (current.selectedDate.isBlank()) null else PcCarePlanStep.PARK
            PcCarePlanStep.PARK -> if (current.selectedParkId.isBlank()) null else PcCarePlanStep.PEN
            PcCarePlanStep.PEN -> if (current.selectedShedId.isBlank()) null else PcCarePlanStep.OPERATORS
            PcCarePlanStep.OPERATORS -> if (current.selectedOperatorIds.isEmpty()) null else PcCarePlanStep.REVIEW
            else -> null
        }
        if (next == null) {
            _state.update { it.copy(message = "Choose one to continue") }
            return
        }
        _state.update { it.copy(step = next, message = null) }
        if (next == PcCarePlanStep.PEN) loadPens(append = false)
    }

    private fun previousStep() {
        val previous = when (_state.value.step) {
            PcCarePlanStep.DATE -> PcCarePlanStep.DATE
            PcCarePlanStep.PARK -> PcCarePlanStep.DATE
            PcCarePlanStep.PEN -> PcCarePlanStep.PARK
            PcCarePlanStep.OPERATORS -> PcCarePlanStep.PEN
            PcCarePlanStep.REVIEW -> PcCarePlanStep.OPERATORS
            PcCarePlanStep.LIST -> PcCarePlanStep.LIST
        }
        _state.update { it.copy(step = previous, message = null) }
    }

    /** One ~20-row pen page per call; the screen's passive footer asks for the next page. */
    private fun loadPens(append: Boolean) {
        val current = _state.value
        if (current.selectedParkId.isBlank() || current.selectedCategoryKey.isBlank()) return
        if (pensLoadInFlight) return
        if (append && current.pensEndReached) return
        pensLoadInFlight = true
        if (!append) pensCursor = null
        _state.update { it.copy(pensLoading = true) }
        viewModelScope.launch {
            try {
                val page = repository.plannerParkSheds(
                    parkId = current.selectedParkId,
                    category = current.selectedCategoryKey,
                    date = current.selectedDate,
                    cursor = if (append) pensCursor else null,
                )
                pensCursor = page.nextCursor.ifBlank { null }
                val mapped = page.sheds.map { shed ->
                    PcCarePlanPenUi(
                        shedId = shed.shedId,
                        locationDisplay = shed.operationalLocationDisplay.ifBlank { shed.shedLabel },
                        partitionLabel = shed.partitionLabel,
                        existingTaskId = shed.existingTaskId,
                    )
                }
                _state.update {
                    it.copy(
                        pens = if (append) it.pens + mapped else mapped,
                        pensEndReached = pensCursor == null,
                        pensLoading = false,
                    )
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                crashReporter.recordException(error, "pc care planner pens load failed")
                analytics.track(
                    AnalyticsEvents.PC_CARE_FAILURE,
                    mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "pens_failed").take(MAX_REASON_CHARS)),
                )
                _state.update { it.copy(pensLoading = false, message = "Couldn't load the pens. Try again.") }
            } finally {
                pensLoadInFlight = false
            }
        }
    }

    private fun create() {
        val current = _state.value
        if (current.creating) return
        if (current.selectedCategoryKey.isBlank() || current.selectedParkId.isBlank() ||
            current.selectedShedId.isBlank() || current.selectedOperatorIds.isEmpty()
        ) {
            _state.update { it.copy(message = "Complete every step first") }
            return
        }
        _state.update { it.copy(creating = true) }
        viewModelScope.launch {
            try {
                val created = repository.createTask(
                    // REUSED on retry: a network blip + second tap replays the SAME planned task.
                    idempotencyKey = createIdempotencyKey,
                    request = PcCareCreateTaskRequestDto(
                        category = current.selectedCategoryKey,
                        parkId = current.selectedParkId,
                        shedId = current.selectedShedId,
                        partitionLabel = current.selectedPartitionLabel,
                        plannedBusinessDate = current.selectedDate,
                        assigneeUserIds = current.selectedOperatorIds.toList(),
                    ),
                )
                analytics.track(
                    AnalyticsEvents.PC_CARE_PLAN_TASK_CREATED,
                    mapOf(AnalyticsEvents.Params.KIND to current.selectedCategoryKey),
                )
                // The monitor list for the planned day must show this task immediately: drop its
                // cache marker so the next pager load refetches instead of TTL-skipping.
                // exception:exempt local cache-marker delete; the create itself already succeeded
                runCatching {
                    repository.invalidateWorklist(
                        PcCareWorklistQuery(
                            category = current.selectedCategoryKey,
                            date = current.selectedDate,
                            monitor = true,
                        ),
                    )
                }
                _state.update {
                    it.copy(
                        creating = false,
                        createdTaskId = created.taskId.ifBlank { "created" },
                        message = null,
                    )
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                crashReporter.recordException(error, "pc care planner create failed")
                analytics.track(
                    AnalyticsEvents.PC_CARE_FAILURE,
                    mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "create_failed").take(MAX_REASON_CHARS)),
                )
                // The key is deliberately KEPT: retrying is the same planned task.
                _state.update { it.copy(creating = false, message = "Couldn't create the task. Try again.") }
            }
        }
    }

    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "pc care planner list page load failed")
        analytics.track(
            AnalyticsEvents.PC_CARE_FAILURE,
            mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS)),
        )
    }

    private fun todayIso(): String = LocalDate.now(ZoneId.of(INDIA_ZONE)).toString()

    private companion object {
        const val INDIA_ZONE = "Asia/Kolkata"
        const val PAST_WINDOW_DAYS = 30L
        const val FUTURE_WINDOW_DAYS = 14L
        const val MAX_REASON_CHARS = 96
        const val EMPTY_MESSAGE = "No care tasks planned for this day"
    }
}
