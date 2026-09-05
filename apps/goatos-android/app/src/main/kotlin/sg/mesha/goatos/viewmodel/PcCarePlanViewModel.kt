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
import kotlinx.coroutines.Job
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
import sg.mesha.goatos.core.network.dto.PcCareCreateRoundRequestDto
import sg.mesha.goatos.core.network.dto.PcCareRoundPenDto
import sg.mesha.goatos.core.network.userFacingMessage
import sg.mesha.goatos.core.network.dto.PcCarePlannerCatalogDto
import sg.mesha.goatos.core.network.dto.PcCareRoundCardDto
import sg.mesha.goatos.feature.pccare.PcCareRoundCardUi
import sg.mesha.goatos.feature.pccare.PcCareRoundsTab
import sg.mesha.goatos.feature.pccare.PcCareRoundPenUi
import sg.mesha.goatos.feature.pccare.PcCarePlanEvent
import sg.mesha.goatos.feature.pccare.PcCarePlanOption
import sg.mesha.goatos.feature.pccare.PcCarePlanPenUi
import sg.mesha.goatos.feature.pccare.PcCarePlanStep
import sg.mesha.goatos.feature.pccare.PcCarePlanUiState
import sg.mesha.goatos.feature.pccare.PcCareTaskCardUi
import sg.mesha.goatos.feature.pccare.selectionKey
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

    /** Which of the planner list's two tabs is showing. No date rides this: weighing has none. */
    private val roundsFilter = MutableStateFlow(PcCareRoundsTab.ACTIVE)

    /** Collects the OPEN card's pens; cancelled when another card opens or this one closes. */
    private var openRoundJob: Job? = null

    /** One key per wizard session, reused across retries of the SAME planned task. */
    private var createIdempotencyKey: String = UUID.randomUUID().toString()
    private var lastTrackedStep: PcCarePlanStep? = null

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
        // The planner's list is ROUND-grained (maintainer decision 2026-09-05): one card per
        // round, because the planner ticked those pens as ONE piece of work. Room renders it and
        // the refresh runs behind, so re-entering the screen never shows a blank wall.
        viewModelScope.launch {
            combine(monitorSelection, roundsFilter) { sel, tab -> sel.category to tab }
                .filter { (category, _) -> category.isNotBlank() }
                .flatMapLatest { (category, tab) -> repository.observeRoundCards(category, tab.wireFilter()) }
                .collect { cards ->
                    _state.update { it.copy(roundCards = cards.map { dto -> dto.toRoundCardUi() }) }
                }
        }
        viewModelScope.launch {
            combine(monitorSelection, roundsFilter) { sel, tab -> sel.category to tab }
                .filter { (category, _) -> category.isNotBlank() }
                .collect { (category, tab) -> repository.refreshRoundCards(category, tab.wireFilter()) }
        }
    }

    /**
     * Opens a round card to show its pens, or closes the open one. The pens are fetched on
     * DEMAND rather than ridden on every card, so a day's list stays one bounded read.
     */
    private fun toggleRoundCard(cardKey: String, roundId: String) {
        if (_state.value.openRoundCardKey == cardKey) {
            openRoundJob?.cancel()
            _state.update { it.copy(openRoundCardKey = "", openRoundPens = emptyList()) }
            return
        }
        _state.update { it.copy(openRoundCardKey = cardKey, openRoundPens = emptyList(), openRoundLoading = true) }
        openRoundJob?.cancel()
        openRoundJob = viewModelScope.launch {
            launch { repository.refreshRoundPens(roundId) }
            // Room renders the pens; the refresh above fills it in behind, so re-opening a card
            // shows what is cached at once instead of a blank wait.
            repository.observeRoundPens(roundId).collect { pens ->
                _state.update { current ->
                    if (current.openRoundCardKey != cardKey) {
                        current
                    } else {
                        current.copy(
                            openRoundLoading = false,
                            openRoundPens = pens.map { pen ->
                                PcCareRoundPenUi(
                                    taskId = pen.taskId,
                                    penLabel = pen.operationalLocationDisplay.ifBlank { pen.shedLabel },
                                    statusLabel = pcCareCardStatusLabel(pen.workState, pen.status),
                                    reopenable = pen.workState == "closed",
                                )
                            },
                        )
                    }
                }
            }
        }
    }

    fun onEvent(event: PcCarePlanEvent) {
        when (event) {
            PcCarePlanEvent.Refresh -> refresh()
            is PcCarePlanEvent.SelectMonitorDate -> selectMonitorDate(event.date)
            is PcCarePlanEvent.AskCloseCard -> _state.update {
                it.copy(closingTaskId = event.cardKey, closingRoundId = event.roundId, closingSingleTaskId = event.singleTaskId, closeReason = "")
            }
            is PcCarePlanEvent.CloseCard -> closeCard(event.roundId, event.singleTaskId, event.reason)
            is PcCarePlanEvent.DismissCloseTask -> _state.update {
                it.copy(closingTaskId = "", closingRoundId = "", closingSingleTaskId = "", closeReason = "")
            }
            is PcCarePlanEvent.CloseReasonChanged -> _state.update { it.copy(closeReason = event.reason) }
            is PcCarePlanEvent.CloseTask -> closeTask(event.taskId, event.reason)
            is PcCarePlanEvent.ReopenTask -> reopenTask(event.taskId)
            is PcCarePlanEvent.ToggleRoundCard -> toggleRoundCard(event.cardKey, event.roundId)
            is PcCarePlanEvent.SelectRoundsTab -> {
                _state.update { it.copy(roundsTab = event.tab, openRoundCardKey = "", openRoundPens = emptyList()) }
                roundsFilter.value = event.tab
            }
            PcCarePlanEvent.CloseCreate -> trackWizardInteraction("close_create")
            is PcCarePlanEvent.SelectDate -> selectCreateDate(event.date)
            is PcCarePlanEvent.SelectPark -> selectPark(event.parkId)
            is PcCarePlanEvent.TogglePen -> togglePen(event.shedId, event.partitionLabel)
            PcCarePlanEvent.LoadMorePens -> {
                trackWizardInteraction("load_more_pens")
                loadPens(append = true)
            }
            is PcCarePlanEvent.ToggleOperator -> toggleOperator(event.userId)
            PcCarePlanEvent.ToggleFeedRemoval -> toggleFeedRemoval()
            is PcCarePlanEvent.ToggleRemovalOperator -> toggleRemovalOperator(event.userId)
            PcCarePlanEvent.NextStep -> nextStep()
            PcCarePlanEvent.PreviousStep -> previousStep()
            PcCarePlanEvent.Create -> create()
            PcCarePlanEvent.DismissMessage -> {
                trackWizardInteraction("dismiss_message")
                _state.update { it.copy(message = null) }
            }
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
        lastTrackedStep = null
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
                selectedPenKeys = emptySet(),
                selectedOperatorIds = emptySet(),
                // Feed & water removal is a DEWORMING question only (maintainer decision
                // 2026-09-03): tablets given in feed need feed & water removed the evening
                // before; injection deworming and every other category never see the toggle.
                feedRemovalOffered = categoryKey == CATEGORY_DEWORMING,
                feedRemovalRequired = false,
                selectedRemovalOperatorIds = emptySet(),
                minSelectableDateIso = "",
                creating = false,
                createdTaskId = "",
                message = null,
            )
        }
        analytics.track(AnalyticsEvents.PC_CARE_PLAN_WIZARD_VIEWED, mapOf(AnalyticsEvents.Params.KIND to categoryKey))
        trackStepReached(PcCarePlanStep.DATE, categoryKey)
    }

    private fun trackStepReached(step: PcCarePlanStep, category: String = _state.value.selectedCategoryKey) {
        if (step == PcCarePlanStep.LIST || lastTrackedStep == step) return
        lastTrackedStep = step
        analytics.track(
            AnalyticsEvents.PC_CARE_PLAN_WIZARD_STEP_REACHED,
            mapOf(
                AnalyticsEvents.Params.KIND to category,
                AnalyticsEvents.Params.FIELD to step.name.lowercase(),
            ),
        )
    }

    private fun trackWizardInteraction(action: String, count: Int? = null) {
        val params = mutableMapOf(
            AnalyticsEvents.Params.KIND to _state.value.selectedCategoryKey,
            AnalyticsEvents.Params.ACTION to action,
        )
        if (count != null) params[AnalyticsEvents.Params.COUNT] = count.toString()
        analytics.track(AnalyticsEvents.PC_CARE_PLAN_WIZARD_INTERACTION, params)
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

    // END and START AGAIN are PC Care's only two verbs, on par with weighing (maintainer
    // decision 2026-09-05, retiring cancel). Ending is not erasing: the pen-day stays taken
    // and the reason stays readable, which is why a reason is asked for and not optional.
    /**
     * Ends a card's work. A ROUND ends as a whole — every pen and the evening's removal card —
     * because the planner planned them as one piece of work; a round of ONE ends its single
     * task. Both refuse a blank reason before any write.
     */
    private fun closeCard(roundId: String, singleTaskId: String, reason: String) {
        if (reason.isBlank()) {
            _state.update { it.copy(message = "Say why this work is being ended") }
            return
        }
        viewModelScope.launch {
            try {
                if (roundId.isNotBlank()) {
                    repository.closeRound(roundId, reason.trim())
                } else {
                    repository.closeTask(singleTaskId, reason.trim())
                }
                _state.update {
                    it.copy(message = "Work ended", closingTaskId = "", closingRoundId = "", closingSingleTaskId = "", closeReason = "")
                }
                refresh()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                crashReporter.recordException(error, "pc care planner close failed")
                analytics.track(
                    AnalyticsEvents.PC_CARE_FAILURE,
                    mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "close_failed").take(MAX_REASON_CHARS)),
                )
                // The SERVER's own sentence is surfaced verbatim where one exists — the close
                // gate's "waiting for a video review" names the way out.
                _state.update { it.copy(message = error.userFacingMessage("Couldn't end this work. Try again.")) }
            }
        }
    }

    private fun closeTask(taskId: String, reason: String) {
        if (reason.isBlank()) {
            _state.update { it.copy(message = "Say why this work is being ended") }
            return
        }
        viewModelScope.launch {
            try {
                repository.closeTask(taskId, reason.trim())
                _state.update { it.copy(message = "Work ended", closingTaskId = "", closeReason = "") }
                refresh()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                crashReporter.recordException(error, "pc care planner close failed")
                analytics.track(
                    AnalyticsEvents.PC_CARE_FAILURE,
                    mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "close_failed").take(MAX_REASON_CHARS)),
                )
                // The SERVER's own sentence is surfaced verbatim where one exists — the close
                // gate's "waiting for a video review" names the way out, and inventing local
                // copy for it would hide that.
                _state.update {
                    it.copy(message = error.userFacingMessage("Couldn't end this work. Try again."))
                }
            }
        }
    }

    private fun reopenTask(taskId: String) {
        viewModelScope.launch {
            try {
                repository.reopenTask(taskId)
                _state.update { it.copy(message = "Work started again") }
                refresh()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                crashReporter.recordException(error, "pc care planner reopen failed")
                analytics.track(
                    AnalyticsEvents.PC_CARE_FAILURE,
                    mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "reopen_failed").take(MAX_REASON_CHARS)),
                )
                _state.update {
                    it.copy(message = error.userFacingMessage("Couldn't start this work again. Try again."))
                }
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
        // With the removal toggle ON, the chosen day must still have a removal evening ahead of
        // it (client mirror of the server's 20:00 IST rule; the server still refuses with its
        // own farm copy).
        val minIso = _state.value.minSelectableDateIso
        if (_state.value.feedRemovalRequired && minIso.isNotBlank() && date.toString() < minIso) return
        trackWizardInteraction("select_date")
        _state.update {
            it.copy(
                selectedDate = date.toString(),
                pens = emptyList(),
                selectedShedId = "",
                selectedPartitionLabel = "",
                selectedPenLabel = "",
                selectedPenKeys = emptySet(),
            )
        }
    }

    private fun selectPark(parkId: String) {
        val label = _state.value.parks.firstOrNull { it.key == parkId }?.label.orEmpty()
        trackWizardInteraction("select_park")
        _state.update {
            it.copy(
                selectedParkId = parkId,
                selectedParkLabel = label,
                pens = emptyList(),
                selectedShedId = "",
                selectedPartitionLabel = "",
                selectedPenLabel = "",
                selectedPenKeys = emptySet(),
                // A different farm has different people: the operator step filters to the
                // chosen park's mapping, so choices made under another park cannot carry over.
                selectedOperatorIds = emptySet(),
                selectedRemovalOperatorIds = emptySet(),
            )
        }
    }

    private fun togglePen(shedId: String, partitionLabel: String) {
        // Pens are one row PER PARTITION, so shedId alone is not unique (Castro 1/2/3 share it).
        val pen = _state.value.pens.firstOrNull { it.shedId == shedId && it.partitionLabel == partitionLabel } ?: return
        if (pen.existingTaskId.isNotBlank()) return
        _state.update { current ->
            val key = pen.selectionKey()
            val selectedKeys = if (key in current.selectedPenKeys) current.selectedPenKeys - key else current.selectedPenKeys + key
            val selectedPens = current.pens.filter { it.selectionKey() in selectedKeys }
            val selectedLabel = when (selectedPens.size) {
                0 -> ""
                1 -> selectedPens.first().locationDisplay
                else -> "${selectedPens.size} pens selected"
            }
            trackWizardInteraction(if (key in current.selectedPenKeys) "remove_pen" else "add_pen", selectedKeys.size)
            current.copy(
                selectedShedId = selectedPens.firstOrNull()?.shedId.orEmpty(),
                selectedPartitionLabel = selectedPens.firstOrNull()?.partitionLabel.orEmpty(),
                selectedPenLabel = selectedLabel,
                selectedPenKeys = selectedKeys,
            )
        }
    }

    private fun toggleOperator(userId: String) {
        _state.update {
            val selected = it.selectedOperatorIds
            val next = if (userId in selected) selected - userId else selected + userId
            trackWizardInteraction(if (userId in selected) "remove_operator" else "add_operator", next.size)
            it.copy(selectedOperatorIds = next)
        }
    }

    /**
     * Flips "Feed removed before deworming?" (maintainer decision 2026-09-03). Turning it ON
     * applies the 20:00 IST picker rule: a selected day whose removal evening has already begun
     * is MOVED to the earliest allowed day, and the move is said out loud rather than silently
     * applied — the server would refuse the old day anyway (422, its own farm copy).
     */
    private fun toggleFeedRemoval() {
        val current = _state.value
        if (!current.feedRemovalOffered) return
        val next = !current.feedRemovalRequired
        if (!next) {
            trackWizardInteraction("feed_removal_off")
            _state.update {
                it.copy(feedRemovalRequired = false, selectedRemovalOperatorIds = emptySet(), minSelectableDateIso = "")
            }
            return
        }
        val earliest = earliestPlannableDateWithFeedRemoval(java.time.ZonedDateTime.now(ZoneId.of(INDIA_ZONE)))
        val earliestIso = earliest.toString()
        val selected = current.selectedDate
        val bumped = selected.isNotBlank() && selected < earliestIso
        trackWizardInteraction("feed_removal_on")
        _state.update {
            it.copy(
                feedRemovalRequired = true,
                minSelectableDateIso = earliestIso,
                selectedDate = if (bumped) earliestIso else it.selectedDate,
                message = if (bumped) {
                    "Feed & water must be removed the evening before, so the day moved to the earliest possible one."
                } else {
                    it.message
                },
            )
        }
    }

    private fun toggleRemovalOperator(userId: String) {
        _state.update {
            val selected = it.selectedRemovalOperatorIds
            val next = if (userId in selected) selected - userId else selected + userId
            trackWizardInteraction(if (userId in selected) "remove_removal_operator" else "add_removal_operator", next.size)
            it.copy(selectedRemovalOperatorIds = next)
        }
    }

    private fun nextStep() {
        val current = _state.value
        val next = when (current.step) {
            PcCarePlanStep.DATE -> if (current.selectedDate.isBlank()) null else PcCarePlanStep.PARK
            PcCarePlanStep.PARK -> if (current.selectedParkId.isBlank()) null else PcCarePlanStep.PEN
            PcCarePlanStep.PEN -> if (current.selectedPenKeys.isEmpty()) null else PcCarePlanStep.OPERATORS
            PcCarePlanStep.OPERATORS -> when {
                current.selectedOperatorIds.isEmpty() -> null
                current.feedRemovalRequired && current.selectedRemovalOperatorIds.isEmpty() -> {
                    _state.update { it.copy(message = "Pick who removes feed & water the evening before") }
                    return
                }
                else -> PcCarePlanStep.REVIEW
            }
            else -> null
        }
        if (next == null) {
            trackWizardInteraction("next_step_blocked")
            _state.update { it.copy(message = "Choose one to continue") }
            return
        }
        trackWizardInteraction("next_step")
        _state.update { it.copy(step = next, message = null) }
        trackStepReached(next, current.selectedCategoryKey)
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
        trackWizardInteraction("previous_step")
        _state.update { it.copy(step = previous, message = null) }
        trackStepReached(previous)
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
            current.selectedPenKeys.isEmpty() || current.selectedOperatorIds.isEmpty()
        ) {
            _state.update { it.copy(message = "Complete every step first") }
            return
        }
        if (current.feedRemovalRequired && current.selectedRemovalOperatorIds.isEmpty()) {
            _state.update { it.copy(message = "Pick who removes feed & water the evening before") }
            return
        }
        analytics.track(
            AnalyticsEvents.PC_CARE_PLAN_CREATE_ATTEMPTED,
            mapOf(
                AnalyticsEvents.Params.KIND to current.selectedCategoryKey,
                AnalyticsEvents.Params.COUNT to current.selectedPenKeys.size.toString(),
                AnalyticsEvents.Params.OUTCOME to if (current.feedRemovalRequired) "with_feed_removal" else "without_feed_removal",
            ),
        )
        _state.update { it.copy(creating = true) }
        viewModelScope.launch {
            try {
                val selectedPens = current.pens.filter { it.selectionKey() in current.selectedPenKeys }
                // ONE write for every ticked pen (maintainer decision 2026-09-05). This used to
                // loop and fire one create per pen, which is why a four-pen plan came out as four
                // unrelated cards — and, with the removal toggle on, four more. The server now
                // plans a ROUND holding one task per pen, and either the whole round lands or none
                // of it does: a partially planned round is work nobody knows is missing.
                val round = repository.createRound(
                    // REUSED on retry: a network blip plus a second tap replays the SAME round
                    // rather than planning it twice. The pen SET rides the server's request
                    // fingerprint, so replaying this key with a different pen list is refused
                    // instead of silently planning a different round.
                    idempotencyKey = createIdempotencyKey,
                    request = PcCareCreateRoundRequestDto(
                        category = current.selectedCategoryKey,
                        parkId = current.selectedParkId,
                        pens = selectedPens.map {
                            PcCareRoundPenDto(shedId = it.shedId, partitionLabel = it.partitionLabel)
                        },
                        plannedBusinessDate = current.selectedDate,
                        assigneeUserIds = current.selectedOperatorIds.toList(),
                        // Sent ONLY when the toggle was offered and turned on; null keeps every
                        // other category's payload byte-identical to before this feature.
                        feedRemovalRequired = if (current.feedRemovalRequired) true else null,
                        removalOperatorUserIds = current.selectedRemovalOperatorIds
                            .takeIf { current.feedRemovalRequired }
                            ?.toList(),
                    ),
                )
                val firstCreatedTaskId = round.pens.firstOrNull()?.taskId.orEmpty()
                analytics.track(
                    AnalyticsEvents.PC_CARE_PLAN_TASK_CREATED,
                    mapOf(
                        AnalyticsEvents.Params.KIND to current.selectedCategoryKey,
                        AnalyticsEvents.Params.COUNT to round.penCount.coerceAtLeast(selectedPens.size).toString(),
                        AnalyticsEvents.Params.OUTCOME to if (current.feedRemovalRequired) "with_feed_removal" else "without_feed_removal",
                    ),
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
                        createdTaskId = firstCreatedTaskId.ifBlank { "created" },
                        message = null,
                    )
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                crashReporter.recordException(error, "pc care planner create failed")
                analytics.track(
                    AnalyticsEvents.PC_CARE_FAILURE,
                    mapOf(
                        AnalyticsEvents.Params.REASON to (error.message ?: "create_failed").take(MAX_REASON_CHARS),
                        AnalyticsEvents.Params.KIND to current.selectedCategoryKey,
                        AnalyticsEvents.Params.COUNT to current.selectedPenKeys.size.toString(),
                    ),
                )
                // The key is deliberately KEPT: retrying is the same planned task. The SERVER's
                // own sentence is surfaced verbatim where one exists (fasting_window_closed,
                // removal_operators_required, feed_removal_not_applicable all carry farm copy).
                _state.update {
                    it.copy(
                        creating = false,
                        message = error.userFacingMessage("Couldn't create the task. Try again."),
                    )
                }
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
        const val CATEGORY_DEWORMING = "deworming"
        const val PAST_WINDOW_DAYS = 30L
        const val FUTURE_WINDOW_DAYS = 14L
        const val MAX_REASON_CHARS = 96
        const val EMPTY_MESSAGE = "No care tasks planned for this day"
    }
}

/** The tab's CONTRACT token. Never shown to anyone; the label lives on the screen. */
internal fun PcCareRoundsTab.wireFilter(): String =
    if (this == PcCareRoundsTab.COMPLETED) "completed" else "active"

/** Backend status token -> the planner's chip copy. Farm words, never a raw token. */
internal fun pcCareStatusLabel(status: String): String = when (status) {
    "open" -> "Open"
    "pending_verification" -> "In review"
    "completed" -> "Done"
    "rework" -> "Send back"
    else -> status
}

/** Maps a backend round card onto the planner's card. Every label is backend-owned. */
internal fun PcCareRoundCardDto.toRoundCardUi(): PcCareRoundCardUi = PcCareRoundCardUi(
    cardKey = cardKey,
    roundId = roundId,
    singleTaskId = singleTaskId,
    // The chip reads WORK STATE first: closing leaves every pen's status at 'open' and moves
    // only its work_state, so a status-only chip called ended work "Open".
    statusLabel = pcCareCardStatusLabel(workState, status),
    dateLabel = dueBusinessDate,
    pensLabel = penLabels.joinToString(" · "),
    penCountLabel = if (penCount == 1) "1 pen" else "$penCount pens",
    parkAndCrewLabel = listOf(parkName, assigneeNames.joinToString(", "))
        .filter { it.isNotBlank() }
        .joinToString(" · "),
    animalCountLabel = if (animalCount > 0) "$animalCount animals" else "",
    expandable = roundId.isNotBlank() && penCount > 1,
    // Work still owed can be ENDED; work already ended or finished cannot.
    closable = workState.isBlank(),
    // A round of ONE reopens from its card; a multi-pen round reopens pen by pen inside it.
    reopenable = workState == "closed" && roundId.isBlank() && singleTaskId.isNotBlank(),
)

/**
 * The card's chip. WORK STATE wins when the card is terminal, because "Ended" and "Done" are
 * what a reader needs; the verification status is only meaningful while the work is live.
 */
internal fun pcCareCardStatusLabel(workState: String, status: String): String = when (workState) {
    "closed" -> "Ended"
    "completed" -> "Done"
    else -> pcCareStatusLabel(status)
}
